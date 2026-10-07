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
	"errors"
	"strings"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Logs is the server's own event log: the entries the administrator's log view reads.
//
// Unlike the configuration repositories, it holds nothing in memory. Every call goes to the
// table, because the log is not a configuration the server runs on.
type Logs struct {
	store store
}

var _ ports.LogRepository = (*Logs)(nil)

// The legacy server's statements, with the values taken out of them.
//
// `Logs.LogEvent` builds its insert with `fmt.Sprintf` and puts the message in raw, so a message
// containing a single quote — which is every line that names a failed statement — produces broken
// SQL, the insert fails, and the error is discarded by every caller. That is Q21, and the
// parameterized insert here is its fix: those lines are stored, and the administrator's log view
// shows them.
const (
	logInsertQuery = `INSERT INTO "logs" ("level", "message", "timestamp") VALUES (?, ?, ?)`
	logDeleteQuery = `DELETE FROM "logs" WHERE "timestamp" < ?`
)

// NewLogs returns the log repository for one database.
func NewLogs(db *sql.DB, dialect Dialect) *Logs {
	return &Logs{store: newStore(db, dialect)}
}

// AppendLog records one entry. The entry's own timestamp is what is stored, in the Unix
// milliseconds the column carries.
func (logs *Logs) AppendLog(ctx context.Context, entry domain.LogEntry) error {
	if _, err := logs.store.exec(ctx, logInsertQuery, entry.Level, entry.Message, millisOf(entry.DateTime)); err != nil {
		return statementError("logs", "logevent", err, logInsertQuery)
	}

	return nil
}

// SearchLogs is the legacy `Logs.Search`: the total the criteria matches and the page of entries
// the criteria selects, in the requested order, from the requested offset.
//
// The criteria is already resolved by the query handler and the date window is already the one
// `domain.LogSearch` models — the ascending window runs forward a full day from the submitted
// instant truncated to the minute, the descending one runs back a day and then back again by the
// submitted hour and minute.
//
// The filter is assembled from fixed fragments and placeholders. The only text joined into the
// statement is the sort direction, chosen from two constants, exactly as the legacy server
// chooses it.
func (logs *Logs) SearchLogs(ctx context.Context, search domain.LogSearch) (ports.LogSearchPage, error) {
	where := []string{`TRUE`}
	args := []any{}

	if search.HasLevel() {
		where = append(where, `"level" = ?`)
		args = append(args, search.Level)
	}

	if search.HasDate() {
		from, to, _ := search.DateWindow()

		where = append(where, `("timestamp" BETWEEN ? AND ?)`)
		args = append(args, millisOf(from), millisOf(to))
	}

	filter := strings.Join(where, " AND ")

	order := "ASC"
	if !search.Ascending {
		order = "DESC"
	}

	page := ports.LogSearchPage{}

	// The span of everything the criteria match is read first, in the order the legacy server reads
	// it: the matching timestamps ascending, then descending, then the count, then the page. The two
	// ends belong to the whole match and not to the page, because the log view pages through them.
	startQuery := `SELECT "timestamp" FROM "logs" WHERE ` + filter + ` ORDER BY "timestamp" ASC`

	if at, ok, err := logs.searchInstant(ctx, startQuery, args); err != nil {
		return ports.LogSearchPage{}, statementError("logs", "search", err, startQuery)
	} else if ok {
		page.DateStart = at
	}

	stopQuery := `SELECT "timestamp" FROM "logs" WHERE ` + filter + ` ORDER BY "timestamp" DESC`

	if at, ok, err := logs.searchInstant(ctx, stopQuery, args); err != nil {
		return ports.LogSearchPage{}, statementError("logs", "search", err, stopQuery)
	} else if ok {
		page.DateStop = at
	}

	var count int64

	countQuery := `SELECT COUNT(*) FROM "logs" WHERE ` + filter
	if err := logs.store.queryRow(ctx, countQuery, args...).Scan(&count); err != nil {
		return ports.LogSearchPage{}, statementError("logs", "search", err, countQuery)
	}

	page.Count = uint64(count)

	rowsQuery := `SELECT "logId", "level", "message", "timestamp" FROM "logs" WHERE ` + filter + ` ORDER BY "timestamp" ` + order + ` LIMIT ? OFFSET ?`

	rows, err := logs.store.query(ctx, rowsQuery, append(args, search.Limit, search.Offset)...)
	if err != nil {
		return ports.LogSearchPage{}, statementError("logs", "search", err, rowsQuery)
	}

	defer rows.Close()

	for rows.Next() {
		var (
			id        sql.NullInt64
			level     sql.NullString
			message   sql.NullString
			timestamp sql.NullInt64
		)

		if err := rows.Scan(&id, &level, &message, &timestamp); err != nil {
			continue
		}

		// The legacy server drops a row that carries nothing to show — no identity, no level,
		// no message, or a timestamp that is not a real instant — from the results, while the
		// count it reports still includes it. Reproduced, because a log table written by the
		// other server is not guaranteed to have none of those.
		if !id.Valid {
			continue
		}

		if !level.Valid || len(level.String) == 0 {
			continue
		}

		if !message.Valid || len(message.String) == 0 {
			continue
		}

		if !timestamp.Valid || timestamp.Int64 <= 0 {
			continue
		}

		page.Results = append(page.Results, domain.LogEntry{
			ID:       uint64(id.Int64),
			DateTime: instantOf(timestamp.Int64),
			Level:    level.String,
			Message:  message.String,
		})
	}

	return page, nil
}

// searchInstant reads one instant out of the entries the criteria match, which is how the two ends of
// the log view's span are found: the same filter, ordered from one end, and the first row is the end.
//
// A search that matches nothing is not a failure and is reported as one: the legacy server tolerates
// the no-rows answer here and leaves that end of the span unset, so an empty log view is an empty log
// view rather than an error the administrator is answered with a status code.
func (logs *Logs) searchInstant(ctx context.Context, query string, args []any) (time.Time, bool, error) {
	var timestamp sql.NullInt64

	if err := logs.store.queryRow(ctx, query, args...).Scan(&timestamp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false, nil
		}

		return time.Time{}, false, err
	}

	if !timestamp.Valid {
		return time.Time{}, false, nil
	}

	return instantOf(timestamp.Int64), true, nil
}

// DeleteLogsBefore is the prune: remove the entries older than the given instant and report how
// many went. The legacy `Logs.Prune` reports nothing, so the prune's own log entry has always
// been written without knowing how many entries it removed.
func (logs *Logs) DeleteLogsBefore(ctx context.Context, before time.Time) (int, error) {
	res, err := logs.store.exec(ctx, logDeleteQuery, millisOf(before))
	if err != nil {
		return 0, statementError("logs", "prune", err, logDeleteQuery)
	}

	removed, err := res.RowsAffected()
	if err != nil {
		return 0, operationError("logs", "prune", err)
	}

	return int(removed), nil
}
