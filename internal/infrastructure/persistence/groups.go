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

// Groups is the stored set of talkgroup groups: the configuration in effect, not a
// view of the `groups` table — loaded once, and a save replaces it before storing
// it. A save that stored is followed by reading the stored rows back so that the
// identifiers the table assigned are what the next save and the configuration
// broadcast work from. That posture, including a save that takes effect before it
// is stored, is the live configuration in effect maintainer note.
type Groups struct {
	store store

	mutex sync.Mutex
	live  domain.Groups
	known bool

	// warn tells the operator about a stored value the read had to clamp. The server
	// wires it to its log; without it the repository clamps silently.
	warn func(message string)
}

// OnWarn sets the listener that hears about stored values the read had to clamp.
// The server wires it at startup to file the line as a warning in the log view.
func (groups *Groups) OnWarn(report func(message string)) { groups.warn = report }

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

	loaded, clamped, err := readGroups(ctx, groups.store)
	if err != nil {
		return nil, err
	}

	groups.reportClamps(clamped)

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

	loaded, clamped, err := readGroups(ctx, groups.store)
	if err != nil {
		return err
	}

	groups.reportClamps(clamped)

	groups.live = loaded

	return nil
}

// readGroups is the legacy `Groups.Read`, stored rows sorted by display order.
func readGroups(ctx context.Context, s store) (domain.Groups, []string, error) {
	const query = `SELECT "groupId", "alert", "label", "led", "order" FROM "groups"`

	rows, err := s.query(ctx, query)
	if err != nil {
		return nil, nil, statementError("groups", "read", err, query)
	}

	defer rows.Close()

	out := domain.Groups{}

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
			return nil, nil, operationError("groups", "read", err)
		}

		// The signed order column is clamped to zero on the way into the unsigned
		// field; why, recorded in the signed-column-clamp maintainer note.
		if order < 0 {
			clamped = append(clamped, fmt.Sprintf("groups.read: group %d %q holds order %d: that is not a display order this server can hold, and it is read as 0",
				id, label, order))
		}

		out = append(out, domain.Group{
			ID:    uint64(id),
			Alert: alert,
			Label: label,
			LED:   led,
			Order: uint(clampNonNegative(order)),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, nil, operationError("groups", "read", err)
	}

	// The clamp lines are collected, not said, inside the loop: the read holds the
	// table open while it walks it, and a line said here is a write into the table
	// the cursor is reading — the server's own read blocks its own report.
	return out.Ordered(), clamped, nil
}

// reportClamps says the collected clamp lines, once the read that collected them
// has closed its cursor.
func (groups *Groups) reportClamps(clamped []string) {
	if groups.warn == nil {
		return
	}

	for _, line := range clamped {
		groups.warn(line)
	}
}

// writeGroups reconciles the stored groups with the given ones in one
// transaction: select the stored identifiers, delete the ones the submitted list
// no longer names, insert or update the rest. A submitted group with no identity
// yet is inserted without the existence check. The delete check carries the
// repaired keep rule — a submitted zero id does not shield stored rows — as
// recorded in the reconciling-saves maintainer note.
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
