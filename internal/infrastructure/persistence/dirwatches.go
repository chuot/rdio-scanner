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

	// warn tells the operator about a stored value the read had to clamp. The server
	// wires it to its log; without it the repository clamps silently.
	warn func(message string)
}

// OnWarn sets the listener that hears about stored values the read had to clamp.
// The server wires it at startup to file the line as a warning in the log view.
func (dirwatches *Dirwatches) OnWarn(report func(message string)) { dirwatches.warn = report }

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
		loaded, clamped, err := dirwatches.read(ctx)
		if err != nil {
			return nil, err
		}

		dirwatches.reportClamps(clamped)

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
	loaded, clamped, err := dirwatches.read(ctx)
	if err != nil {
		return err
	}

	dirwatches.reportClamps(clamped)

	dirwatches.live = loaded

	return nil
}

// dirwatchColumns are the columns the legacy server's `Dirwatches.Read` selects, in the order it
// selects them.
const dirwatchColumns = `"dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type"`

// read loads the whole stored collection. It is the legacy `Dirwatches.Read`, with the values
// bound instead of formatted into the statement.
func (dirwatches *Dirwatches) read(ctx context.Context) (domain.Dirwatches, []string, error) {
	query := `SELECT ` + dirwatchColumns + ` FROM "dirwatches"`

	rows, err := dirwatches.store.query(ctx, query)
	if err != nil {
		return nil, nil, statementError("dirwatches", "read", err, query)
	}

	defer rows.Close()

	out := domain.Dirwatches{}

	clamped := []string{}

	for rows.Next() {
		dirwatch := domain.NewDirwatch()

		var (
			id        int64
			delay     int64
			frequency int64
			order     int64
		)

		if err := rows.Scan(
			&id,
			&delay,
			&dirwatch.DeleteAfter,
			&dirwatch.Directory,
			&dirwatch.Disabled,
			&dirwatch.Extension,
			&frequency,
			&dirwatch.Mask,
			&order,
			&dirwatch.SiteID,
			&dirwatch.SystemID,
			&dirwatch.TalkgroupID,
			&dirwatch.Kind,
		); err != nil {
			return nil, nil, statementError("dirwatches", "read", err, query)
		}

		dirwatch.ID = uint64(id)

		// The signed delay, frequency, and order columns are clamped to zero on the
		// way into the unsigned fields; why, recorded in the signed-column-clamp
		// maintainer note. The deleteAfter column is boolean in every schema and the
		// field is a Go bool, so it has no signed value to clamp.
		clamps := []string{}

		if delay < 0 {
			clamps = append(clamps, fmt.Sprintf("delay %d read as 0", delay))
		}

		if frequency < 0 {
			clamps = append(clamps, fmt.Sprintf("frequency %d read as 0", frequency))
		}

		if order < 0 {
			clamps = append(clamps, fmt.Sprintf("order %d read as 0", order))
		}

		if len(clamps) > 0 {
			clamped = append(clamped, fmt.Sprintf("dirwatches.read: dirwatch %d %q holds %s: these are not values this server can hold, and it enforces the clamped ones",
				id, dirwatch.Directory, strings.Join(clamps, ", ")))
		}

		dirwatch.Delay = uint(clampNonNegative(delay))
		dirwatch.Frequency = uint(clampNonNegative(frequency))
		dirwatch.Order = uint(clampNonNegative(order))

		out = append(out, dirwatch)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, operationError("dirwatches", "read", err)
	}

	// The legacy server sorts the list it read by `order` rather than asking the
	// database for it, and the two servers have to agree about the order the
	// administrator arranged. The legacy sort was unstable; sorting stably is the
	// same rule made repeatable.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	// The clamp lines are collected, not said, inside the loop: the read holds the
	// table open while it walks it, and a line said here is a write into the table
	// the cursor is reading — the server's own read blocks its own report.
	return out, clamped, nil
}

// reportClamps says the collected clamp lines, once the read that collected them
// has closed its cursor.
func (dirwatches *Dirwatches) reportClamps(clamped []string) {
	if dirwatches.warn == nil {
		return
	}

	for _, line := range clamped {
		dirwatches.warn(line)
	}
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
