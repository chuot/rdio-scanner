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
	"sort"
	"strings"
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Dirwatches is the stored directory-watch configuration: the configuration in
// effect, not a view of the `dirwatches` table — read once, kept in memory, and
// replaced by what a save submits. The posture, including a save that takes
// effect before it is stored, is the live configuration in effect maintainer note.
type Dirwatches struct {
	store store

	// mutex guards the live watches: a configuration save and a running
	// watcher read the same list.
	mutex sync.Mutex

	// live is the set of watches in effect. After a save that persisted, it
	// is what the table holds; after one that did not, it is what was submitted.
	live domain.Dirwatches

	// liveIsKnown says whether the live watches have been read yet; the first
	// read is the startup read.
	liveIsKnown bool
}

var _ ports.DirwatchRepository = (*Dirwatches)(nil)

// NewDirwatches builds the repository for one opened database.
func NewDirwatches(db *sql.DB, dialect Dialect) *Dirwatches {
	return &Dirwatches{store: newStore(db, dialect)}
}

// AllDirwatches returns the watches the server is running with, in their display order. The
// table is read once; later calls report the cached configuration.
func (dirwatches *Dirwatches) AllDirwatches(ctx context.Context) (domain.Dirwatches, error) {
	dirwatches.mutex.Lock()
	defer dirwatches.mutex.Unlock()

	if !dirwatches.liveIsKnown {
		loaded, err := dirwatches.read(ctx)
		if err != nil {
			return nil, err
		}

		dirwatches.live = loaded
		dirwatches.liveIsKnown = true
	}

	return dirwatches.live, nil
}

// SaveDirwatches makes the given watches what the server runs with and then stores
// them. The submitted watches take effect before the store is reached, so a save
// that cannot be persisted still leaves the submitted directories being watched;
// why that order is the parity behavior lives in the live configuration in effect
// maintainer note.
func (dirwatches *Dirwatches) SaveDirwatches(ctx context.Context, next domain.Dirwatches) error {
	dirwatches.mutex.Lock()
	defer dirwatches.mutex.Unlock()

	dirwatches.live = next
	dirwatches.liveIsKnown = true

	if err := dirwatches.write(ctx, next); err != nil {
		return err
	}

	// A save that persisted is normalized through the stored rows, which gives a
	// newly added watch the identifier the table assigned it.
	loaded, err := dirwatches.read(ctx)
	if err != nil {
		return err
	}

	dirwatches.live = loaded

	return nil
}

// dirwatchColumns are the columns the legacy server's `Dirwatches.Read` selects, in the order it
// selects them.
const dirwatchColumns = `"dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type"`

// read loads the whole stored collection. It is the legacy `Dirwatches.Read`, with the values
// bound instead of formatted into the statement.
func (dirwatches *Dirwatches) read(ctx context.Context) (domain.Dirwatches, error) {
	query := `SELECT ` + dirwatchColumns + ` FROM "dirwatches"`

	rows, err := dirwatches.store.query(ctx, query)
	if err != nil {
		return nil, statementError("dirwatches", "read", err, query)
	}

	defer rows.Close()

	out := domain.Dirwatches{}

	for rows.Next() {
		dirwatch := domain.NewDirwatch()

		if err := rows.Scan(
			&dirwatch.ID,
			&dirwatch.Delay,
			&dirwatch.DeleteAfter,
			&dirwatch.Directory,
			&dirwatch.Disabled,
			&dirwatch.Extension,
			&dirwatch.Frequency,
			&dirwatch.Mask,
			&dirwatch.Order,
			&dirwatch.SiteID,
			&dirwatch.SystemID,
			&dirwatch.TalkgroupID,
			&dirwatch.Kind,
		); err != nil {
			return nil, statementError("dirwatches", "read", err, query)
		}

		out = append(out, dirwatch)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("dirwatches", "read", err)
	}

	// The legacy server sorts the list it read by `order` rather than asking the
	// database for it, and the two servers have to agree about the order the
	// administrator arranged. The legacy sort was unstable; sorting stably is the
	// same rule made repeatable.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out, nil
}

// write reconciles the stored watches with the given ones in one transaction: select the stored
// identifiers, delete the ones the submitted list no longer names, then insert or update the rest.
// That is the shape every legacy `Write` has.
func (dirwatches *Dirwatches) write(ctx context.Context, next domain.Dirwatches) error {
	var query string

	tx, err := dirwatches.store.begin(ctx)
	if err != nil {
		return operationError("dirwatches", "write", err)
	}

	obsolete, err := dirwatches.selectObsolete(ctx, tx, next)
	if err != nil {
		tx.rollback()
		return err
	}

	if len(obsolete) > 0 {
		// The identifiers go in as parameters, one placeholder each.
		placeholders := make([]string, 0, len(obsolete))
		args := make([]any, 0, len(obsolete))

		for _, id := range obsolete {
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}

		query = `DELETE FROM "dirwatches" WHERE "dirwatchId" IN (` + strings.Join(placeholders, ", ") + `)`

		if _, err := tx.exec(ctx, query, args...); err != nil {
			tx.rollback()
			return statementError("dirwatches", "write", err, query)
		}
	}

	for _, dirwatch := range next {
		var count uint

		query = `SELECT COUNT(*) FROM "dirwatches" WHERE "dirwatchId" = ?`

		if err := tx.queryRow(ctx, query, dirwatch.ID).Scan(&count); err != nil {
			tx.rollback()
			return statementError("dirwatches", "write", err, query)
		}

		if count == 0 {
			query = `INSERT INTO "dirwatches" ("delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

			if _, err := tx.exec(ctx, query, dirwatchValues(dirwatch)...); err != nil {
				tx.rollback()
				return statementError("dirwatches", "write", err, query)
			}

			continue
		}

		query = `UPDATE "dirwatches" SET "delay" = ?, "deleteAfter" = ?, "directory" = ?, "disabled" = ?, "extension" = ?, "frequency" = ?, "mask" = ?, "order" = ?, "siteId" = ?, "systemId" = ?, "talkgroupId" = ?, "type" = ? WHERE "dirwatchId" = ?`

		if _, err := tx.exec(ctx, query, append(dirwatchValues(dirwatch), dirwatch.ID)...); err != nil {
			tx.rollback()
			return statementError("dirwatches", "write", err, query)
		}
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError("dirwatches", "write", err)
	}

	return nil
}

// selectObsolete returns the stored identifiers the submitted list no longer names.
func (dirwatches *Dirwatches) selectObsolete(ctx context.Context, tx transaction, next domain.Dirwatches) ([]uint64, error) {
	query := `SELECT "dirwatchId" FROM "dirwatches"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		return nil, statementError("dirwatches", "write", err, query)
	}

	defer rows.Close()

	obsolete := []uint64{}

	for rows.Next() {
		var id uint64

		if err := rows.Scan(&id); err != nil {
			return nil, statementError("dirwatches", "write", err, query)
		}

		if !dirwatchNamed(next, id) {
			obsolete = append(obsolete, id)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("dirwatches", "write", err)
	}

	return obsolete, nil
}

// dirwatchNamed reports whether the submitted list still names a stored watch. The
// legacy write stops its scan at the first submitted watch whose id is zero, so a
// save that adds a watch silently keeps every watch the list removed; this server
// repairs that rule, as recorded in the reconciling-saves maintainer note.
func dirwatchNamed(next domain.Dirwatches, id uint64) bool {
	for _, dirwatch := range next {
		if dirwatch.ID == id {
			return true
		}
	}

	return false
}

// dirwatchValues are the watch's columns in the order both statements list them.
func dirwatchValues(dirwatch domain.Dirwatch) []any {
	return []any{
		dirwatch.Delay,
		dirwatch.DeleteAfter,
		dirwatch.Directory,
		dirwatch.Disabled,
		dirwatch.Extension,
		dirwatch.Frequency,
		dirwatch.Mask,
		dirwatch.Order,
		dirwatch.SiteID,
		dirwatch.SystemID,
		dirwatch.TalkgroupID,
		dirwatch.Kind,
	}
}
