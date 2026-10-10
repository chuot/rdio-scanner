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
	"strings"
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Apikeys is the server's upload credential list, held the same way the access list is held:
// loaded once, replaced by a save before it is stored, and read back after a save that worked.
type Apikeys struct {
	store store

	mutex  sync.Mutex
	loaded bool
	live   domain.Apikeys
}

var _ ports.ApikeyRepository = (*Apikeys)(nil)

const (
	apikeySelectQuery    = `SELECT "apikeyId", "disabled", "ident", "key", "order", "systems" FROM "apikeys"`
	apikeySelectIDsQuery = `SELECT "apikeyId" FROM "apikeys"`
	apikeyCountQuery     = `SELECT COUNT(*) FROM "apikeys" WHERE "apikeyId" = ?`
	apikeyInsertQuery    = `INSERT INTO "apikeys" ("disabled", "ident", "key", "order", "systems") VALUES (?, ?, ?, ?, ?)`
	apikeyUpdateQuery    = `UPDATE "apikeys" SET "disabled" = ?, "ident" = ?, "key" = ?, "order" = ?, "systems" = ? WHERE "apikeyId" = ?`
)

// NewApikeys returns the apikey repository for one database.
func NewApikeys(db *sql.DB, dialect Dialect) *Apikeys {
	return &Apikeys{store: newStore(db, dialect)}
}

// AllApikeys returns the upload credentials the server is running with.
func (apikeys *Apikeys) AllApikeys(ctx context.Context) (domain.Apikeys, error) {
	apikeys.mutex.Lock()
	defer apikeys.mutex.Unlock()

	if !apikeys.loaded {
		list, err := apikeys.read(ctx)
		if err != nil {
			return nil, err
		}

		apikeys.live = list
		apikeys.loaded = true
	}

	return apikeys.live, nil
}

// SaveApikeys reconciles the stored credentials with the given ones and makes them the
// credentials the server accepts uploads against, with the same order of operations as
// `SaveAccesses`: replace what is in effect, then store it, then read the stored rows back.
func (apikeys *Apikeys) SaveApikeys(ctx context.Context, next domain.Apikeys) error {
	apikeys.mutex.Lock()
	defer apikeys.mutex.Unlock()

	apikeys.live = next
	apikeys.loaded = true

	if err := apikeys.write(ctx, next); err != nil {
		return err
	}

	reloaded, err := apikeys.read(ctx)
	if err != nil {
		return err
	}

	apikeys.live = reloaded

	return nil
}

// read is the legacy `Apikeys.Read`: every row, sorted by display order.
//
// `disabled` is scanned straight into a Go bool, which is what the legacy server does, and it is
// what makes both directions work: the legacy server writes the flag with `%t`, sqlite stores
// that as the integer 1 or 0, and a row written by either server reads back as the boolean it
// means. A row that carries the text `true` or `false` — which is what a database restored from
// a text dump can hold — reads back the same way.
func (apikeys *Apikeys) read(ctx context.Context) (domain.Apikeys, error) {
	rows, err := apikeys.store.query(ctx, apikeySelectQuery)
	if err != nil {
		return nil, statementError("apikeys", "read", err, apikeySelectQuery)
	}

	defer rows.Close()

	list := domain.Apikeys{}

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
			return nil, operationError("apikeys", "read", err)
		}

		list = append(list, domain.Apikey{
			ID:       uint64(id),
			Disabled: disabled,
			Ident:    ident,
			Key:      key,
			Order:    uint(order),
			Scope:    parseScope(systems),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("apikeys", "read", err)
	}

	return list.Ordered(), nil
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

	rows, err := tx.query(ctx, apikeySelectIDsQuery)
	if err != nil {
		tx.rollback()
		return statementError("apikeys", "write", err, apikeySelectIDsQuery)
	}

	stale := []uint64{}

	for rows.Next() {
		var id int64

		if err := rows.Scan(&id); err != nil {
			rows.Close()
			tx.rollback()
			return operationError("apikeys", "write", err)
		}

		if !apikeyIsNamed(list, uint64(id)) {
			stale = append(stale, uint64(id))
		}
	}

	rows.Close()

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

	for _, apikey := range list {
		var count int64

		if apikey.ID > 0 {
			if err := tx.queryRow(ctx, apikeyCountQuery, int64(apikey.ID)).Scan(&count); err != nil {
				tx.rollback()
				return statementError("apikeys", "write", err, apikeyCountQuery)
			}
		}

		values := []any{
			apikey.Disabled,
			apikey.Ident,
			apikey.Key,
			int64(apikey.Order),
			scopeText(apikey.Scope),
		}

		if count == 0 {
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

// apikeyIsNamed reports whether the submitted list still names a stored credential, with the
// legacy zero-id short-circuit repaired as described on `accessIsNamed`.
func apikeyIsNamed(list domain.Apikeys, id uint64) bool {
	for _, apikey := range list {
		if apikey.ID == id {
			return true
		}
	}

	return false
}
