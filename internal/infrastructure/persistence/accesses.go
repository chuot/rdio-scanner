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
	"strings"
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Accesses is the server's listener access list: the configuration in effect, not a
// database view — loaded once from the `accesses` table, and a save replaces what the
// server enforces before it stores it. That a save takes effect before the store
// is the parity behavior; the reason lives in the
// access-and-apikey-configuration-in-effect maintainer note.
type Accesses struct {
	store store

	// live is the access list in effect. loaded says whether it has been read from the
	// table yet, which happens at most once.
	mutex  sync.Mutex
	loaded bool
	live   domain.Accesses

	// warn tells the operator about a stored value the read had to clamp. The server
	// wires it to its log; without it the repository clamps silently.
	warn func(message string)
}

// OnWarn sets the listener that hears about stored values the read had to clamp.
// The server wires it at startup to file the line as a warning in the log view.
func (accesses *Accesses) OnWarn(report func(message string)) { accesses.warn = report }

var _ ports.AccessRepository = (*Accesses)(nil)

// The statements are the legacy server's, with the values replaced by placeholders: the
// legacy `Write` built its `INSERT`, `UPDATE`, and `DELETE... IN (...)` with `fmt.Sprintf`
// and escaped the interpolated text by hand.
const (
	accessSelectQuery    = `SELECT "accessId", "code", "expiration", "ident", "limit", "order", "systems" FROM "accesses"`
	accessSelectIDsQuery = `SELECT "accessId" FROM "accesses"`
	accessInsertQuery    = `INSERT INTO "accesses" ("code", "expiration", "ident", "limit", "order", "systems") VALUES (?, ?, ?, ?, ?, ?)`
	accessUpdateQuery    = `UPDATE "accesses" SET "code" = ?, "expiration" = ?, "ident" = ?, "limit" = ?, "order" = ?, "systems" = ? WHERE "accessId" = ?`
)

// NewAccesses returns the access repository for one database. Nothing is read here: the
// list loads the first time it is asked for.
func NewAccesses(db *sql.DB, dialect Dialect) *Accesses {
	return &Accesses{store: newStore(db, dialect)}
}

// AllAccesses returns the access list the server is running with.
func (accesses *Accesses) AllAccesses(ctx context.Context) (domain.Accesses, error) {
	// The cache answers under the mutex; the database statements of a read or a save
	// run outside it, so the listener checks keep answering through the whole save.
	// Why the mutex does not cover the statements lives in the
	// access-and-apikey-configuration-in-effect maintainer note.
	accesses.mutex.Lock()

	if accesses.loaded {
		list := accesses.live.Ordered()

		accesses.mutex.Unlock()

		return list, nil
	}

	accesses.mutex.Unlock()

	list, clamped, err := accesses.read(ctx)
	if err != nil {
		return nil, err
	}

	accesses.mutex.Lock()
	accesses.live = list
	accesses.loaded = true
	accesses.mutex.Unlock()

	accesses.reportClamps(clamped)

	// Ordered is a copy: the list the server enforces with is not the slice every
	// caller gets to edit.
	return list.Ordered(), nil
}

// SaveAccesses reconciles the stored accesses with the given ones and makes them the
// accesses the server runs with. The submitted list takes effect before it is stored —
// a save that cannot reach the store still changes what the server enforces — and after
// a write that succeeded the stored rows are read back and cached. That order of
// operations is pinned by tests; why it is the parity behavior lives in the
// access-and-apikey-configuration-in-effect maintainer note.
func (accesses *Accesses) SaveAccesses(ctx context.Context, next domain.Accesses) error {
	// A copy, for the same reason as the read path: the submitted slice stays the
	// caller's to do with as the caller likes. The assignment takes
	// the mutex; the statements run outside it, so the server keeps
	// enforcing from the cache while the save walks the database.
	accesses.mutex.Lock()
	accesses.live = next.Ordered()
	accesses.loaded = true
	accesses.mutex.Unlock()

	if err := accesses.write(ctx, next); err != nil {
		return err
	}

	reloaded, clamped, err := accesses.read(ctx)
	if err != nil {
		return err
	}

	accesses.mutex.Lock()
	accesses.live = reloaded
	accesses.mutex.Unlock()

	accesses.reportClamps(clamped)

	return nil
}

// read is the legacy `Accesses.Read`: every row, sorted by display order. Unlike the
// legacy server, a read that fails leaves the list in effect alone; the reason
// the legacy clear-before-query is not mirrored lives in the
// access-and-apikey-configuration-in-effect maintainer note.
func (accesses *Accesses) read(ctx context.Context) (domain.Accesses, []string, error) {
	rows, err := accesses.store.query(ctx, accessSelectQuery)
	if err != nil {
		return nil, nil, statementError("accesses", "read", err, accessSelectQuery)
	}

	defer rows.Close()

	list := domain.Accesses{}

	clamped := []string{}

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
			return nil, nil, operationError("accesses", "read", err)
		}

		// The signed columns are clamped to zero on the way into the unsigned
		// fields: zero is the safe reading in both directions. Why, recorded in the
		// signed-column-clamp maintainer note.
		if expiration < 0 || limit < 0 || order < 0 {
			clamped = append(clamped, clampLine(id, code, expiration, limit, order))
		}

		list = append(list, domain.Access{
			ID:         uint64(id),
			Code:       code,
			Expiration: clampNonNegative(expiration),
			Ident:      ident,
			Limit:      uint(clampNonNegative(limit)),
			Order:      uint(clampNonNegative(order)),
			Scope:      parseScope(systems),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, nil, operationError("accesses", "read", err)
	}

	// The clamp lines are collected, not said, inside the loop: the read holds the
	// table open while it walks it, and a line said here is a write into the table
	// the cursor is reading — the server's own read blocks its own report.
	return list.Ordered(), clamped, nil
}

// clampLine words one access's stored negative values the way the operator needs
// to hear them: which row, which values, and what the server will enforce instead.
func clampLine(id int64, code string, expiration, limit, order int64) string {
	clamped := []string{}

	if expiration < 0 {
		clamped = append(clamped, fmt.Sprintf("expiration %d read as 0 (expires immediately)", expiration))
	}

	if limit < 0 {
		clamped = append(clamped, fmt.Sprintf("limit %d read as 0", limit))
	}

	if order < 0 {
		clamped = append(clamped, fmt.Sprintf("order %d read as 0", order))
	}

	return fmt.Sprintf("accesses.read: access %d %q holds %s: these are not values this server can hold, and it enforces the clamped ones",
		id, code, strings.Join(clamped, ", "))
}

// reportClamps says the collected clamp lines, once the read that collected them
// has closed its cursor.
func (accesses *Accesses) reportClamps(clamped []string) {
	if accesses.warn == nil {
		return
	}

	for _, line := range clamped {
		accesses.warn(line)
	}
}

// clampNonNegative turns a stored signed value into the unsigned one the list
// carries, keeping zero for every value below it.
func clampNonNegative(value int64) uint64 {
	if value < 0 {
		return 0
	}

	return uint64(value)
}

// write is the legacy `Accesses.Write`, in one transaction: select the stored identities,
// delete the ones the submitted list no longer names, then insert or update the rest.
func (accesses *Accesses) write(ctx context.Context, list domain.Accesses) error {
	tx, err := accesses.store.begin(ctx)
	if err != nil {
		return operationError("accesses", "write", err)
	}

	// The identities the submitted list names, collected once: every stored row is
	// checked against this map rather than a walk of the submitted list per row.
	//
	// The legacy `Accesses.Write` stopped its scan at the first submitted access whose
	// id was zero, so a save that adds a user kept every user the list removed; this
	// server repairs that rule, as recorded in the reconciling-saves maintainer note.
	named := make(map[uint64]struct{}, len(list))

	for _, access := range list {
		named[access.ID] = struct{}{}
	}

	rows, err := tx.query(ctx, accessSelectIDsQuery)
	if err != nil {
		tx.rollback()
		return statementError("accesses", "write", err, accessSelectIDsQuery)
	}

	// The cursor closes on the way out of the function, whatever the way out is: the
	// manual closes this loop used to have were two of them, and a function that gains a
	// third exit keeps the table open.
	defer rows.Close()

	stale := []uint64{}

	for rows.Next() {
		var id int64

		if err := rows.Scan(&id); err != nil {
			tx.rollback()
			return operationError("accesses", "write", err)
		}

		if _, ok := named[uint64(id)]; !ok {
			stale = append(stale, uint64(id))
		}
	}

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

	// Which of the submitted identities are stored, in one statement: the
	// legacy per-row COUNT cost a round trip per submitted row inside the
	// transaction that holds the table for the whole save.
	ids := make([]int64, 0, len(list))

	for _, access := range list {
		if access.ID > 0 {
			ids = append(ids, int64(access.ID))
		}
	}

	stored, err := storedIdentities(ctx, tx, "accesses", "write", "accesses", "accessId", ids)
	if err != nil {
		tx.rollback()
		return err
	}

	for _, access := range list {
		values := []any{
			access.Code,
			int64(access.Expiration),
			access.Ident,
			int64(access.Limit),
			int64(access.Order),
			scopeText(access.Scope),
		}

		if _, ok := stored[access.ID]; !ok {
			// The identity is never written: the table assigns it, so a submitted id
			// that names no stored row becomes a new access, not an edit of a row
			// that is not there.
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
