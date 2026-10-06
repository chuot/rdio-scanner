// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

package persistence

import (
	"context"
	"database/sql"
	"sync"

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

// Tags is the stored set of talkgroup tags.
//
// It is the configuration in effect, in the same sense the systems catalog and the groups are:
// loaded once, replaced by what a save submits, and re-read from the stored rows after a save that
// persisted.
type Tags struct {
	store store

	mutex sync.Mutex
	live  domain.Tags
	known bool
}

var _ ports.TagRepository = (*Tags)(nil)

// NewTags builds the adapter for one opened database.
func NewTags(db *sql.DB, dialect Dialect) *Tags {
	return &Tags{store: newStore(db, dialect)}
}

// AllTags returns the tags the server is running with. The collection is read from the database
// once and kept.
func (tags *Tags) AllTags(ctx context.Context) (domain.Tags, error) {
	tags.mutex.Lock()
	defer tags.mutex.Unlock()

	if tags.known {
		return tags.live, nil
	}

	loaded, err := readTags(ctx, tags.store)
	if err != nil {
		return nil, err
	}

	tags.live = loaded
	tags.known = true

	return loaded, nil
}

// SaveTags reconciles the stored tags with the given ones and leaves the reconciled result as what
// the server runs with, including when the store could not be written at all (quirk Q13).
//
// Dropping a tag drops the talkgroups that name it: `talkgroups.tagId` references `tags` ON DELETE
// CASCADE, which is what the legacy server relies on too.
func (tags *Tags) SaveTags(ctx context.Context, next domain.Tags) error {
	tags.mutex.Lock()
	defer tags.mutex.Unlock()

	tags.live = next
	tags.known = true

	if err := writeTags(ctx, tags.store, next); err != nil {
		return err
	}

	loaded, err := readTags(ctx, tags.store)
	if err != nil {
		return err
	}

	tags.live = loaded

	return nil
}

// readTags is the legacy `Tags.Read`, stored rows sorted by display order. Labels are read exactly
// as they are stored, including the trailing space the seeded "Emergency " label carries.
func readTags(ctx context.Context, s store) (domain.Tags, error) {
	const query = `SELECT "tagId", "alert", "label", "led", "order" FROM "tags"`

	rows, err := s.query(ctx, query)
	if err != nil {
		return nil, statementError("tags", "read", err, query)
	}

	defer rows.Close()

	out := domain.Tags{}

	for rows.Next() {
		var tag domain.Tag

		if err := rows.Scan(&tag.ID, &tag.Alert, &tag.Label, &tag.LED, &tag.Order); err != nil {
			return nil, operationError("tags", "read", err)
		}

		out = append(out, tag)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("tags", "read", err)
	}

	return out.Ordered(), nil
}

// writeTags is the legacy `Tags.Write`: select the stored identifiers, delete the ones the
// submitted list no longer names, insert or update the rest, in one transaction.
func writeTags(ctx context.Context, s store, next domain.Tags) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return operationError("tags", "write", err)
	}

	defer tx.rollback()

	const storedQuery = `SELECT "tagId" FROM "tags"`

	stored, err := readColumnIDs(ctx, tx, "tags", "write", storedQuery)
	if err != nil {
		return err
	}

	if removed := droppedIdentifiers(idsOfTags(next), stored); len(removed) > 0 {
		query := `DELETE FROM "tags" WHERE "tagId" IN (` + placeholders(len(removed)) + `)`

		if _, err := tx.exec(ctx, query, valuesOf(removed)...); err != nil {
			return statementError("tags", "write", err, query)
		}
	}

	for _, tag := range next {
		var (
			id    = tag.ID
			count uint64
		)

		if id > 0 {
			const query = `SELECT COUNT(*) FROM "tags" WHERE "tagId" = ?`

			if err := tx.queryRow(ctx, query, id).Scan(&count); err != nil {
				return statementError("tags", "write", err, query)
			}
		}

		if count == 0 {
			const query = `INSERT INTO "tags" ("alert", "label", "led", "order") VALUES (?, ?, ?, ?)`

			if _, err := tx.exec(ctx, query, tag.Alert, tag.Label, tag.LED, tag.Order); err != nil {
				return statementError("tags", "write", err, query)
			}

		} else {
			const query = `UPDATE "tags" SET "alert" = ?, "label" = ?, "led" = ?, "order" = ? WHERE "tagId" = ?`

			if _, err := tx.exec(ctx, query, tag.Alert, tag.Label, tag.LED, tag.Order, id); err != nil {
				return statementError("tags", "write", err, query)
			}
		}
	}

	if err := tx.commit(); err != nil {
		return operationError("tags", "write", err)
	}

	return nil
}

func idsOfTags(tags domain.Tags) []uint64 {
	out := make([]uint64, 0, len(tags))

	for _, tag := range tags {
		out = append(out, tag.ID)
	}

	return out
}
