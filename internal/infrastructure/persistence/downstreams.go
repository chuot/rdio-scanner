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
	"strings"
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Downstreams is the stored downstream-server configuration.
//
// Like the directory watches, it is the configuration the server is running with rather than a
// view of the table: the list is read once, kept in memory, and replaced by what a save submits.
type Downstreams struct {
	store store

	// mutex guards the live list, which the forwarding path reads while a save replaces it.
	mutex sync.Mutex

	// live is the list in effect: what was read at startup, what a save submitted when the save
	// could not be persisted, and what the table holds when it could.
	live domain.Downstreams

	// liveIsKnown reports whether the live list has been read yet.
	liveIsKnown bool
}

var _ ports.DownstreamRepository = (*Downstreams)(nil)

// NewDownstreams builds the repository for one opened database.
func NewDownstreams(db *sql.DB, dialect Dialect) *Downstreams {
	return &Downstreams{store: newStore(db, dialect)}
}

// AllDownstreams returns the downstreams the server is running with, in their display order. The
// table is read once; later calls report the cached configuration.
func (downstreams *Downstreams) AllDownstreams(ctx context.Context) (domain.Downstreams, error) {
	downstreams.mutex.Lock()
	defer downstreams.mutex.Unlock()

	if !downstreams.liveIsKnown {
		loaded, err := downstreams.read(ctx)
		if err != nil {
			return nil, err
		}

		downstreams.live = loaded
		downstreams.liveIsKnown = true
	}

	return downstreams.live, nil
}

// SaveDownstreams makes the given downstreams what the server runs with and then stores them.
//
// The submitted list is in effect before anything is written, so a downstream save that fails to
// persist still forwards to the submitted servers for the rest of the run.
func (downstreams *Downstreams) SaveDownstreams(ctx context.Context, next domain.Downstreams) error {
	downstreams.mutex.Lock()
	defer downstreams.mutex.Unlock()

	downstreams.live = next
	downstreams.liveIsKnown = true

	if err := downstreams.write(ctx, next); err != nil {
		return err
	}

	loaded, err := downstreams.read(ctx)
	if err != nil {
		return err
	}

	downstreams.live = loaded

	return nil
}

// read loads the whole stored list. It is the legacy `Downstreams.Read`, with the values bound
// instead of formatted into the statement.
func (downstreams *Downstreams) read(ctx context.Context) (domain.Downstreams, error) {
	query := `SELECT "downstreamId", "apikey", "disabled", "order", "systems", "url" FROM "downstreams"`

	rows, err := downstreams.store.query(ctx, query)
	if err != nil {
		return nil, statementError("downstreams", "read", err, query)
	}

	defer rows.Close()

	out := domain.Downstreams{}

	for rows.Next() {
		var (
			downstream domain.Downstream
			systems    string
		)

		if err := rows.Scan(&downstream.ID, &downstream.Apikey, &downstream.Disabled, &downstream.Order, &systems, &downstream.URL); err != nil {
			return nil, statementError("downstreams", "read", err, query)
		}

		// The scope is stored as JSON text. An empty column, a `null`, and text that is not JSON
		// all mean the same thing they mean in the legacy server: no scope was supplied, which is
		// the state that forwards nothing.
		downstream.Scope = parseScope(systems)

		out = append(out, downstream)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("downstreams", "read", err)
	}

	// The legacy server sorts the list it read by `order`; `Downstreams.Ordered` is that rule, and
	// it is stable where the legacy `sort.Slice` is not.
	return out.Ordered(), nil
}

// write reconciles the stored downstreams with the given ones in one transaction: select the
// stored identifiers, delete the ones the submitted list no longer names, then insert or update
// the rest.
func (downstreams *Downstreams) write(ctx context.Context, next domain.Downstreams) error {
	var query string

	tx, err := downstreams.store.begin(ctx)
	if err != nil {
		return operationError("downstreams", "write", err)
	}

	obsolete, err := downstreams.selectObsolete(ctx, tx, next)
	if err != nil {
		tx.rollback()
		return err
	}

	if len(obsolete) > 0 {
		placeholders := make([]string, 0, len(obsolete))
		args := make([]any, 0, len(obsolete))

		for _, id := range obsolete {
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}

		query = `DELETE FROM "downstreams" WHERE "downstreamId" IN (` + strings.Join(placeholders, ", ") + `)`

		if _, err := tx.exec(ctx, query, args...); err != nil {
			tx.rollback()
			return statementError("downstreams", "write", err, query)
		}
	}

	for _, downstream := range next {
		var count uint

		query = `SELECT COUNT(*) FROM "downstreams" WHERE "downstreamId" = ?`

		if err := tx.queryRow(ctx, query, downstream.ID).Scan(&count); err != nil {
			tx.rollback()
			return statementError("downstreams", "write", err, query)
		}

		if count == 0 {
			query = `INSERT INTO "downstreams" ("apikey", "disabled", "order", "systems", "url") VALUES (?, ?, ?, ?, ?)`

			if _, err := tx.exec(ctx, query, downstreamValues(downstream)...); err != nil {
				tx.rollback()
				return statementError("downstreams", "write", err, query)
			}

			continue
		}

		query = `UPDATE "downstreams" SET "apikey" = ?, "disabled" = ?, "order" = ?, "systems" = ?, "url" = ? WHERE "downstreamId" = ?`

		if _, err := tx.exec(ctx, query, append(downstreamValues(downstream), downstream.ID)...); err != nil {
			tx.rollback()
			return statementError("downstreams", "write", err, query)
		}
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError("downstreams", "write", err)
	}

	return nil
}

// selectObsolete returns the stored identifiers the submitted list no longer names.
func (downstreams *Downstreams) selectObsolete(ctx context.Context, tx transaction, next domain.Downstreams) ([]uint64, error) {
	query := `SELECT "downstreamId" FROM "downstreams"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		return nil, statementError("downstreams", "write", err, query)
	}

	defer rows.Close()

	obsolete := []uint64{}

	for rows.Next() {
		var id uint64

		if err := rows.Scan(&id); err != nil {
			return nil, statementError("downstreams", "write", err, query)
		}

		if !downstreamNamed(next, id) {
			obsolete = append(obsolete, id)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("downstreams", "write", err)
	}

	return obsolete, nil
}

// downstreamNamed reports whether the submitted list still names a stored downstream.
//
// The legacy `Downstreams.Write` stops its scan at the first submitted downstream whose id is zero,
// so a save that adds a downstream silently keeps every downstream the list removed. This server
// repairs that rule;
func downstreamNamed(next domain.Downstreams, id uint64) bool {
	for _, downstream := range next {
		if downstream.ID == id {
			return true
		}
	}

	return false
}

// downstreamValues are the downstream's columns in the order both statements list them.
//
// The scope is written as the JSON text the column carries, through the same helper the accesses
// and apikeys use, so a scope read out of a legacy-written row is written back as the text the
// legacy server wrote.
func downstreamValues(downstream domain.Downstream) []any {
	return []any{
		downstream.Apikey,
		downstream.Disabled,
		downstream.Order,
		scopeText(downstream.Scope),
		downstream.URL,
	}
}
