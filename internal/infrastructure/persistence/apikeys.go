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
	"fmt"
	"strings"
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Apikeys is the server's upload credential list, held the same way the access list is held:
// loaded once, replaced by a save before it is stored, and read back after a save that worked.
type Apikeys struct {
	store store

	// warn is the credential list's way of telling the operator about a stored
	// value it had to clamp (CA-017.2), wired by the server to its log.
	warn func(message string)

	mutex  sync.Mutex
	loaded bool
	live   domain.Apikeys
}

var _ ports.ApikeyRepository = (*Apikeys)(nil)

const (
	apikeySelectQuery    = `SELECT "apikeyId", "disabled", "ident", "key", "order", "systems" FROM "apikeys"`
	apikeySelectIDsQuery = `SELECT "apikeyId" FROM "apikeys"`
	apikeyInsertQuery    = `INSERT INTO "apikeys" ("disabled", "ident", "key", "order", "systems") VALUES (?, ?, ?, ?, ?)`
	apikeyUpdateQuery    = `UPDATE "apikeys" SET "disabled" = ?, "ident" = ?, "key" = ?, "order" = ?, "systems" = ? WHERE "apikeyId" = ?`
)

// OnWarn sets the listener that hears about stored values the read had to clamp,
// the same seam the access list has.
func (apikeys *Apikeys) OnWarn(report func(message string)) { apikeys.warn = report }

// NewApikeys returns the apikey repository for one database.
func NewApikeys(db *sql.DB, dialect Dialect) *Apikeys {
	return &Apikeys{store: newStore(db, dialect)}
}

// AllApikeys returns the upload credentials the server is running with.
func (apikeys *Apikeys) AllApikeys(ctx context.Context) (domain.Apikeys, error) {
	// The cache answers under the mutex, and the read that fills an empty cache
	// runs outside it (CA-014.4), as in `Accesses.AllAccesses`.
	apikeys.mutex.Lock()

	if apikeys.loaded {
		list := apikeys.live.Ordered()

		apikeys.mutex.Unlock()

		return list, nil
	}

	apikeys.mutex.Unlock()

	list, clamped, err := apikeys.read(ctx)
	if err != nil {
		return nil, err
	}

	apikeys.mutex.Lock()
	apikeys.live = list
	apikeys.loaded = true
	apikeys.mutex.Unlock()

	apikeys.reportClamps(clamped)

	return list.Ordered(), nil
}

// SaveApikeys reconciles the stored credentials with the given ones and makes them the
// credentials the server accepts uploads against, with the same order of operations as
// `SaveAccesses`: replace what is in effect, then store it, then read the stored rows back.
func (apikeys *Apikeys) SaveApikeys(ctx context.Context, next domain.Apikeys) error {
	// The assignment takes the mutex; the statements run outside it (CA-014.4),
	// as in `Accesses.SaveAccesses`.
	apikeys.mutex.Lock()
	apikeys.live = next.Ordered()
	apikeys.loaded = true
	apikeys.mutex.Unlock()

	if err := apikeys.write(ctx, next); err != nil {
		return err
	}

	reloaded, clamped, err := apikeys.read(ctx)
	if err != nil {
		return err
	}

	apikeys.mutex.Lock()
	apikeys.live = reloaded
	apikeys.mutex.Unlock()

	apikeys.reportClamps(clamped)

	return nil
}

// read is the legacy `Apikeys.Read`: every row, sorted by display order.
//
// `disabled` is scanned straight into a Go bool, which is what the legacy server does, and it is
// what makes both directions work: the legacy server writes the flag with `%t`, sqlite stores
// that as the integer 1 or 0, and a row written by either server reads back as the boolean it
// means. A row that carries the text `true` or `false` — which is what a database restored from
// a text dump can hold — reads back the same way.
func (apikeys *Apikeys) read(ctx context.Context) (domain.Apikeys, []string, error) {
	rows, err := apikeys.store.query(ctx, apikeySelectQuery)
	if err != nil {
		return nil, nil, statementError("apikeys", "read", err, apikeySelectQuery)
	}

	defer rows.Close()

	list := domain.Apikeys{}

	clamped := []string{}

	for rows.Next() {
		var (
			id       int64
			disabled bool
			ident    string
			key      string
			order    int64
			systems  string
		)

		if err := rows.Scan(&id, &disabled, &ident, &key, &order, &systems); err != nil {
			return nil, nil, operationError("apikeys", "read", err)
		}

		// The order column is signed and the field is not (CA-017.2): a stored -3
		// read as unsigned is the largest display order there is, and the credential
		// moves to the end of every list the operator sees. Zero is the first order,
		// and the clamp is said.
		if order < 0 {
			clamped = append(clamped, fmt.Sprintf("apikeys.read: apikey %d %q holds order %d: that is not a display order this server can hold, and it is read as 0",
				id, ident, order))
		}

		list = append(list, domain.Apikey{
			ID:       uint64(id),
			Disabled: disabled,
			Ident:    ident,
			Key:      key,
			Order:    uint(clampNonNegative(order)),
			Scope:    parseScope(systems),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, nil, operationError("apikeys", "read", err)
	}

	// The clamp lines are collected, not said, inside the loop: the read holds the
	// table open while it walks it, and a line said here is a write into the table
	// the cursor is reading — the server's own read blocks its own report.
	return list.Ordered(), clamped, nil
}

// reportClamps says the collected clamp lines, once the read that collected them
// has closed its cursor.
func (apikeys *Apikeys) reportClamps(clamped []string) {
	if apikeys.warn == nil {
		return
	}

	for _, line := range clamped {
		apikeys.warn(line)
	}
}

// write is the legacy `Apikeys.Write`, in one transaction, with the values out of the statements.
//
// The legacy `Write` interpolates `ident` and `key` with no escaping at all — unlike
// `Accesses.Write`, which escapes them — so a credential named with an apostrophe has always
// failed the whole save. Parameterizing the statements fixes that, which is the same class of
// fix Q21 records for the log insert.
func (apikeys *Apikeys) write(ctx context.Context, list domain.Apikeys) error {
	tx, err := apikeys.store.begin(ctx)
	if err != nil {
		return operationError("apikeys", "write", err)
	}

	// The identities the submitted list names, collected once (CA-017.3), with the
	// legacy zero-id short-circuit repaired as described on `Accesses.write`.
	named := make(map[uint64]struct{}, len(list))

	for _, apikey := range list {
		named[apikey.ID] = struct{}{}
	}

	rows, err := tx.query(ctx, apikeySelectIDsQuery)
	if err != nil {
		tx.rollback()
		return statementError("apikeys", "write", err, apikeySelectIDsQuery)
	}

	// The cursor closes on the way out of the function, whatever the way out is
	// (CA-020.1), as in `Accesses`.
	defer rows.Close()

	stale := []uint64{}

	for rows.Next() {
		var id int64

		if err := rows.Scan(&id); err != nil {
			tx.rollback()
			return operationError("apikeys", "write", err)
		}

		if _, ok := named[uint64(id)]; !ok {
			stale = append(stale, uint64(id))
		}
	}

	if err := rows.Err(); err != nil {
		tx.rollback()
		return operationError("apikeys", "write", err)
	}

	if len(stale) > 0 {
		args := make([]any, 0, len(stale))
		marks := make([]string, 0, len(stale))

		for _, id := range stale {
			args = append(args, int64(id))
			marks = append(marks, "?")
		}

		query := `DELETE FROM "apikeys" WHERE "apikeyId" IN (` + strings.Join(marks, ", ") + `)`

		if _, err := tx.exec(ctx, query, args...); err != nil {
			tx.rollback()
			return statementError("apikeys", "write", err, query)
		}
	}

	// Which of the submitted identities are stored, in one statement (CA-017.3),
	// as in `Accesses.write`.
	ids := make([]int64, 0, len(list))

	for _, apikey := range list {
		if apikey.ID > 0 {
			ids = append(ids, int64(apikey.ID))
		}
	}

	stored, err := storedIdentities(ctx, tx, "apikeys", "write", "apikeys", "apikeyId", ids)
	if err != nil {
		tx.rollback()
		return err
	}

	for _, apikey := range list {
		values := []any{
			apikey.Disabled,
			apikey.Ident,
			apikey.Key,
			int64(apikey.Order),
			scopeText(apikey.Scope),
		}

		if _, ok := stored[apikey.ID]; !ok {
			if _, err := tx.exec(ctx, apikeyInsertQuery, values...); err != nil {
				tx.rollback()
				return statementError("apikeys", "write", err, apikeyInsertQuery)
			}

			continue
		}

		if _, err := tx.exec(ctx, apikeyUpdateQuery, append(values, int64(apikey.ID))...); err != nil {
			tx.rollback()
			return statementError("apikeys", "write", err, apikeyUpdateQuery)
		}
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError("apikeys", "write", err)
	}

	return nil
}
