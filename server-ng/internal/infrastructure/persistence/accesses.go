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

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

// Accesses is the server's listener access list.
//
// It is the configuration in effect, not a database view: the list is loaded from the
// `accesses` table once, every read answers from what was loaded, and a save replaces what
// the server runs with before it stores it — so a list that could not be stored is still the
// list the server enforces for the rest of the run. That is the legacy server's behavior
// ([server/access.go](../../../../server/access.go) `Read` / `Write`, driven by
// [server/admin.go](../../../../server/admin.go) `ConfigHandler`), and it is what
// [decision-internal-ports.md](../../../../docs/specs/FEAT-003-server-ng-refactor/decision-internal-ports.md)
// §9 requires of every configuration repository here.
type Accesses struct {
	store store

	// live is the access list in effect. loaded says whether it has been read from the
	// table yet, which happens at most once.
	mutex  sync.Mutex
	loaded bool
	live   domain.Accesses
}

var _ ports.AccessRepository = (*Accesses)(nil)

// The statements are the legacy server's, with the values taken out of them. The legacy
// `Write` builds its `INSERT`, `UPDATE`, and `DELETE ... IN (...)` with `fmt.Sprintf` and
// escapes the text it interpolates by hand; here the same statements carry placeholders.
const (
	accessSelectQuery    = `SELECT "accessId", "code", "expiration", "ident", "limit", "order", "systems" FROM "accesses"`
	accessSelectIDsQuery = `SELECT "accessId" FROM "accesses"`
	accessCountQuery     = `SELECT COUNT(*) FROM "accesses" WHERE "accessId" = ?`
	accessInsertQuery    = `INSERT INTO "accesses" ("code", "expiration", "ident", "limit", "order", "systems") VALUES (?, ?, ?, ?, ?, ?)`
	accessUpdateQuery    = `UPDATE "accesses" SET "code" = ?, "expiration" = ?, "ident" = ?, "limit" = ?, "order" = ?, "systems" = ? WHERE "accessId" = ?`
)

// NewAccesses returns the access repository for one database. Nothing is read here: the list
// is loaded the first time it is asked for, which is the point in the start sequence the
// legacy server reads it.
func NewAccesses(db *sql.DB, dialect Dialect) *Accesses {
	return &Accesses{store: newStore(db, dialect)}
}

// AllAccesses returns the access list the server is running with.
func (accesses *Accesses) AllAccesses(ctx context.Context) (domain.Accesses, error) {
	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	if !accesses.loaded {
		list, err := accesses.read(ctx)
		if err != nil {
			return nil, err
		}

		accesses.live = list
		accesses.loaded = true
	}

	return accesses.live, nil
}

// SaveAccesses reconciles the stored accesses with the given ones and makes them the accesses
// the server runs with.
//
// The submitted list takes effect first. If the store cannot be reached, the submitted list is
// still what the server enforces and the error is returned — the legacy server's order of
// operations, recorded as Q13 in
// [decision-preserved-quirks.md](../../../../docs/specs/FEAT-003-server-ng-refactor/decision-preserved-quirks.md).
// After a write that succeeded, the stored rows are read back and cached, which is the `Read`
// the legacy `ConfigHandler` runs after a successful `Write`.
func (accesses *Accesses) SaveAccesses(ctx context.Context, next domain.Accesses) error {
	accesses.mutex.Lock()
	defer accesses.mutex.Unlock()

	accesses.live = next
	accesses.loaded = true

	if err := accesses.write(ctx, next); err != nil {
		return err
	}

	reloaded, err := accesses.read(ctx)
	if err != nil {
		return err
	}

	accesses.live = reloaded

	return nil
}

// read is the legacy `Accesses.Read`: every row, sorted by display order.
//
// The legacy server clears its in-memory list before it queries, so a read that fails leaves
// the server with no accesses at all — and an empty access list is what makes a server
// unrestricted. Here the value already in effect is left alone, because §9 makes the adapter's
// live list the configuration the server is running with, and silently un-restricting a server
// on a failed query is not a parity behavior anyone chose.
func (accesses *Accesses) read(ctx context.Context) (domain.Accesses, error) {
	rows, err := accesses.store.query(ctx, accessSelectQuery)
	if err != nil {
		return nil, statementError("accesses", "read", err, accessSelectQuery)
	}

	defer rows.Close()

	list := domain.Accesses{}

	for rows.Next() {
		var (
			id         int64
			code       string
			expiration int64
			ident      string
			limit      int64
			order      int64
			systems    string
		)

		if err := rows.Scan(&id, &code, &expiration, &ident, &limit, &order, &systems); err != nil {
			return nil, operationError("accesses", "read", err)
		}

		list = append(list, domain.Access{
			ID:         uint64(id),
			Code:       code,
			Expiration: uint64(expiration),
			Ident:      ident,
			Limit:      uint(limit),
			Order:      uint(order),
			Scope:      parseScope(systems),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("accesses", "read", err)
	}

	return list.Ordered(), nil
}

// write is the legacy `Accesses.Write`, in one transaction: select the stored identities,
// delete the ones the submitted list no longer names, then insert or update the rest.
func (accesses *Accesses) write(ctx context.Context, list domain.Accesses) error {
	tx, err := accesses.store.begin(ctx)
	if err != nil {
		return operationError("accesses", "write", err)
	}

	rows, err := tx.query(ctx, accessSelectIDsQuery)
	if err != nil {
		tx.rollback()
		return statementError("accesses", "write", err, accessSelectIDsQuery)
	}

	stale := []uint64{}

	for rows.Next() {
		var id int64

		if err := rows.Scan(&id); err != nil {
			rows.Close()
			tx.rollback()
			return operationError("accesses", "write", err)
		}

		if !accessIsNamed(list, uint64(id)) {
			stale = append(stale, uint64(id))
		}
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		tx.rollback()
		return operationError("accesses", "write", err)
	}

	if len(stale) > 0 {
		args := make([]any, 0, len(stale))
		marks := make([]string, 0, len(stale))

		for _, id := range stale {
			args = append(args, int64(id))
			marks = append(marks, "?")
		}

		query := `DELETE FROM "accesses" WHERE "accessId" IN (` + strings.Join(marks, ", ") + `)`

		if _, err := tx.exec(ctx, query, args...); err != nil {
			tx.rollback()
			return statementError("accesses", "write", err, query)
		}
	}

	for _, access := range list {
		var count int64

		if access.ID > 0 {
			if err := tx.queryRow(ctx, accessCountQuery, int64(access.ID)).Scan(&count); err != nil {
				tx.rollback()
				return statementError("accesses", "write", err, accessCountQuery)
			}
		}

		values := []any{
			access.Code,
			int64(access.Expiration),
			access.Ident,
			int64(access.Limit),
			int64(access.Order),
			scopeText(access.Scope),
		}

		if count == 0 {
			// The identity is never written: the table assigns it, which is how an access
			// the administrator just added gets its id, and how a submitted id that names no
			// stored row becomes a new access rather than an edit of a row that is not there.
			if _, err := tx.exec(ctx, accessInsertQuery, values...); err != nil {
				tx.rollback()
				return statementError("accesses", "write", err, accessInsertQuery)
			}

			continue
		}

		if _, err := tx.exec(ctx, accessUpdateQuery, append(values, int64(access.ID))...); err != nil {
			tx.rollback()
			return statementError("accesses", "write", err, accessUpdateQuery)
		}
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError("accesses", "write", err)
	}

	return nil
}

// accessIsNamed reports whether the submitted list still names a stored access.
//
// The legacy `Accesses.Write` stops its scan at the first submitted access whose id is zero, so a
// save that adds a user silently keeps every user the list removed. server-ng repairs that rule;
// see Q33 in decision-preserved-quirks.md.
func accessIsNamed(list domain.Accesses, id uint64) bool {
	for _, access := range list {
		if access.ID == id {
			return true
		}
	}

	return false
}
