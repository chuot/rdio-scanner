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
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Groups is the stored set of talkgroup groups.
//
// Like the systems catalog, it is the configuration in effect rather than a view of the `groups`
// table: it is loaded once, a save replaces it before storing it, and a save that stored is
// followed by reading the stored rows back so that the identifiers the table assigned are what the
// next save and the configuration broadcast work from.
type Groups struct {
	store store

	mutex sync.Mutex
	live  domain.Groups
	known bool
}

var _ ports.GroupRepository = (*Groups)(nil)

// NewGroups builds the adapter for one opened database.
func NewGroups(db *sql.DB, dialect Dialect) *Groups {
	return &Groups{store: newStore(db, dialect)}
}

// AllGroups returns the groups the server is running with. The collection is read from the
// database once and kept.
func (groups *Groups) AllGroups(ctx context.Context) (domain.Groups, error) {
	groups.mutex.Lock()
	defer groups.mutex.Unlock()

	if groups.known {
		return groups.live, nil
	}

	loaded, err := readGroups(ctx, groups.store)
	if err != nil {
		return nil, err
	}

	groups.live = loaded
	groups.known = true

	return loaded, nil
}

// SaveGroups reconciles the stored groups with the given ones and leaves the reconciled result as
// what the server runs with, including when the store could not be written at all.
func (groups *Groups) SaveGroups(ctx context.Context, next domain.Groups) error {
	groups.mutex.Lock()
	defer groups.mutex.Unlock()

	groups.live = next
	groups.known = true

	if err := writeGroups(ctx, groups.store, next); err != nil {
		return err
	}

	loaded, err := readGroups(ctx, groups.store)
	if err != nil {
		return err
	}

	groups.live = loaded

	return nil
}

// readGroups is the legacy `Groups.Read`, stored rows sorted by display order.
func readGroups(ctx context.Context, s store) (domain.Groups, error) {
	const query = `SELECT "groupId", "alert", "label", "led", "order" FROM "groups"`

	rows, err := s.query(ctx, query)
	if err != nil {
		return nil, statementError("groups", "read", err, query)
	}

	defer rows.Close()

	out := domain.Groups{}

	for rows.Next() {
		var group domain.Group

		if err := rows.Scan(&group.ID, &group.Alert, &group.Label, &group.LED, &group.Order); err != nil {
			return nil, operationError("groups", "read", err)
		}

		out = append(out, group)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("groups", "read", err)
	}

	return out.Ordered(), nil
}

// writeGroups is the legacy `Groups.Write`: select the stored identifiers, delete the ones the
// submitted list no longer names, insert or update the rest, in one transaction.
//
// The reconcile is the legacy one, including its keep rule: a submitted group that has no identity
// yet keeps every stored group.
func writeGroups(ctx context.Context, s store, next domain.Groups) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return operationError("groups", "write", err)
	}

	defer tx.rollback()

	const storedQuery = `SELECT "groupId" FROM "groups"`

	stored, err := readColumnIDs(ctx, tx, "groups", "write", storedQuery)
	if err != nil {
		return err
	}

	if removed := droppedIdentifiers(idsOfGroups(next), stored); len(removed) > 0 {
		query := `DELETE FROM "groups" WHERE "groupId" IN (` + placeholders(len(removed)) + `)`

		if _, err := tx.exec(ctx, query, valuesOf(removed)...); err != nil {
			return statementError("groups", "write", err, query)
		}
	}

	for _, group := range next {
		var (
			id    = group.ID
			count uint64
		)

		if id > 0 {
			const query = `SELECT COUNT(*) FROM "groups" WHERE "groupId" = ?`

			if err := tx.queryRow(ctx, query, id).Scan(&count); err != nil {
				return statementError("groups", "write", err, query)
			}
		}

		if count == 0 {
			const query = `INSERT INTO "groups" ("alert", "label", "led", "order") VALUES (?, ?, ?, ?)`

			if _, err := tx.exec(ctx, query, group.Alert, group.Label, group.LED, group.Order); err != nil {
				return statementError("groups", "write", err, query)
			}

		} else {
			const query = `UPDATE "groups" SET "alert" = ?, "label" = ?, "led" = ?, "order" = ? WHERE "groupId" = ?`

			if _, err := tx.exec(ctx, query, group.Alert, group.Label, group.LED, group.Order, id); err != nil {
				return statementError("groups", "write", err, query)
			}
		}
	}

	if err := tx.commit(); err != nil {
		return operationError("groups", "write", err)
	}

	return nil
}

func idsOfGroups(groups domain.Groups) []uint64 {
	out := make([]uint64, 0, len(groups))

	for _, group := range groups {
		out = append(out, group.ID)
	}

	return out
}
