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
	"time"

	"rdio-scanner/internal/application/ports"
)

// DelayedCalls stores the calls that are waiting out a talkgroup or system delay.
//
// Unlike the configuration repositories, this one is not held in memory: the legacy server reads
// the `delayed` table at startup to re-arm the calls that were waiting when it stopped, and writes
// a row when a call starts waiting and deletes it when its delay has run out. Nothing here decides
// whether a call is delayed — that is the delay rule, and it lives in the application layer.
type DelayedCalls struct {
	store store
}

var _ ports.DelayedCallRepository = (*DelayedCalls)(nil)

// NewDelayedCalls builds the repository for one opened database.
func NewDelayedCalls(db *sql.DB, dialect Dialect) *DelayedCalls {
	return &DelayedCalls{store: newStore(db, dialect)}
}

// AllDelayed returns every call still waiting, which is what the server re-arms at startup.
//
// The legacy server reads the table into a map, so it has no defined order for the result; the
// list this port returns is ordered by call identifier so that the same table always answers the
// same way.
func (delayed *DelayedCalls) AllDelayed(ctx context.Context) ([]ports.DelayedCall, error) {
	query := `SELECT "callId", "timestamp" FROM "delayed" ORDER BY "callId"`

	rows, err := delayed.store.query(ctx, query)
	if err != nil {
		return nil, statementError("delayer", "restore", err, query)
	}

	defer rows.Close()

	out := []ports.DelayedCall{}

	for rows.Next() {
		var (
			callID    uint64
			timestamp int64
		)

		if err := rows.Scan(&callID, &timestamp); err != nil {
			return nil, statementError("delayer", "restore", err, query)
		}

		out = append(out, ports.DelayedCall{
			CallID:    callID,
			Timestamp: instantOf(timestamp),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("delayer", "restore", err)
	}

	return out, nil
}

// ScheduleDelayed records that a call is waiting until the given instant, storing the instant in
// the milliseconds the column means.
func (delayed *DelayedCalls) ScheduleDelayed(ctx context.Context, callID uint64, until time.Time) error {
	query := `INSERT INTO "delayed" ("callId", "timestamp") VALUES (?, ?)`

	if _, err := delayed.store.exec(ctx, query, callID, millisOf(until)); err != nil {
		return statementError("delayer", "push", err, query)
	}

	return nil
}

// UnscheduleDelayed removes one call from the waiting list once its delay has run.
//
// It removes the waiting row only. The `delayed` table cascades to `calls`, so a call that is
// deleted loses its waiting row by itself; removing both would be deleting the same thing twice.
// Removing a call that is not waiting is not a failure, exactly as the legacy `pop` of a DELETE
// that matches nothing is not one.
func (delayed *DelayedCalls) UnscheduleDelayed(ctx context.Context, callID uint64) error {
	query := `DELETE FROM "delayed" WHERE "callId" = ?`

	if _, err := delayed.store.exec(ctx, query, callID); err != nil {
		return statementError("delayer", "pop", err, query)
	}

	return nil
}

// ClearDelayed drops every waiting call, which is the step the legacy `Delayer.Start` runs after it
// has read the table and before it re-arms each call. The calls themselves are left alone.
func (delayed *DelayedCalls) ClearDelayed(ctx context.Context) error {
	query := `DELETE FROM "delayed"`

	if _, err := delayed.store.exec(ctx, query); err != nil {
		return statementError("delayer", "restore", err, query)
	}

	return nil
}
