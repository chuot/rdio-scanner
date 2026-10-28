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

package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Tags is the stored set of talkgroup tags, and the configuration in effect like
// the systems catalog and the groups are: loaded once and re-read after a save
// persisted, per the live configuration in effect maintainer note.
type Tags struct {
	store store

	mutex sync.Mutex
	live  domain.Tags
	known bool

	// warn tells the operator about a stored value the read had to clamp. The server
	// wires it to its log; without it the repository clamps silently.
	warn func(message string)
}

// OnWarn sets the listener that hears about stored values the read had to clamp.
// The server wires it at startup to file the line as a warning in the log view.
func (tags *Tags) OnWarn(report func(message string)) { tags.warn = report }

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

	loaded, clamped, err := readTags(ctx, tags.store)
	if err != nil {
		return nil, err
	}

	tags.reportClamps(clamped)

	tags.live = loaded
	tags.known = true

	return loaded, nil
}

// SaveTags reconciles the stored tags with the given ones and leaves the reconciled
// result as what the server runs with, including when the store could not be written
// at all. Dropping a tag drops the talkgroups that name it: `talkgroups.tagId`
// references `tags` ON DELETE CASCADE.
func (tags *Tags) SaveTags(ctx context.Context, next domain.Tags) error {
	tags.mutex.Lock()
	defer tags.mutex.Unlock()

	tags.live = next
	tags.known = true

	if err := writeTags(ctx, tags.store, next); err != nil {
		return err
	}

	loaded, clamped, err := readTags(ctx, tags.store)
	if err != nil {
		return err
	}

	tags.reportClamps(clamped)

	tags.live = loaded

	return nil
}

// readTags reads the stored rows sorted by display order, labels exactly as stored —
// trailing spaces included.
func readTags(ctx context.Context, s store) (domain.Tags, []string, error) {
	const query = `SELECT "tagId", "alert", "label", "led", "order" FROM "tags"`

	rows, err := s.query(ctx, query)
	if err != nil {
		return nil, nil, statementError("tags", "read", err, query)
	}

	defer rows.Close()

	out := domain.Tags{}

	clamped := []string{}

	for rows.Next() {
		var (
			id    int64
			alert string
			label string
			led   string
			order int64
		)

		if err := rows.Scan(&id, &alert, &label, &led, &order); err != nil {
			return nil, nil, operationError("tags", "read", err)
		}

		// The signed order column is clamped to zero on the way into the unsigned
		// field; why, recorded in the signed-column-clamp maintainer note.
		if order < 0 {
			clamped = append(clamped, fmt.Sprintf("tags.read: tag %d %q holds order %d: that is not a display order this server can hold, and it is read as 0",
				id, label, order))
		}

		out = append(out, domain.Tag{
			ID:    uint64(id),
			Alert: alert,
			Label: label,
			LED:   led,
			Order: uint(clampNonNegative(order)),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, nil, operationError("tags", "read", err)
	}

	// The clamp lines are collected, not said, inside the loop: the read holds the
	// table open while it walks it, and a line said here is a write into the table
	// the cursor is reading — the server's own read blocks its own report.
	return out.Ordered(), clamped, nil
}

// reportClamps says the collected clamp lines, once the read that collected them
// has closed its cursor.
func (tags *Tags) reportClamps(clamped []string) {
	if tags.warn == nil {
		return
	}

	for _, line := range clamped {
		tags.warn(line)
	}
}

// writeTags performs the reconcile: select the stored identifiers, delete the ones
// the submitted list no longer names, insert or update the rest, in one transaction.
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
