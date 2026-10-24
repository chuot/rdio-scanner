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
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Logs is the server's own event log: the entries the administrator's log view reads.
// It holds nothing in memory: the log is not a configuration the server runs on.
type Logs struct {
	store store
}

var _ ports.LogRepository = (*Logs)(nil)

// errUnknownLogSource is what AppendLog reports for an entry that does not name a part of this server.
// The vocabulary is owned by the Domain; this is the boundary the entries are written through.
var errUnknownLogSource = errors.New("the log entry names no part of this server")

// The legacy server's statements, parameterized: the legacy insert was built with
// `fmt.Sprintf` and put the message in raw, so a message with a single quote — every
// line naming a failed statement — produced broken SQL, the insert failed, and the
// error was discarded. Parameterizing is the fix: those lines are stored and the log
// view shows them, per the log statement construction maintainer note.
const (
	logInsertQuery = `INSERT INTO "logs" ("level", "source", "message", "timestamp") VALUES (?, ?, ?, ?)`
	logDeleteQuery = `DELETE FROM "logs" WHERE "timestamp" < ?`
)

// NewLogs returns the log repository for one database.
func NewLogs(db *sql.DB, dialect Dialect) *Logs {
	return &Logs{store: newStore(db, dialect)}
}

// AppendLog records one entry: the entry's own timestamp in the Unix milliseconds the
// column carries, and the part of the server that wrote it.
//
// A source that is a name this server does not have is refused: the vocabulary is owned
// by the Domain and every writer reaches it through a constant. Rows with no source
// exist permanently — the other server's insert never mentions the column — and the
// read side files them under legacy, per the log-source write and read posture
// maintainer note.
func (logs *Logs) AppendLog(ctx context.Context, entry domain.LogEntry) error {
	if !domain.IsKnownLogSource(entry.Source) {
		return operationError("logs", "logevent", fmt.Errorf("%w: %q", errUnknownLogSource, entry.Source))
	}

	if _, err := logs.store.exec(ctx, logInsertQuery, entry.Level, string(entry.Source), entry.Message, millisOf(entry.DateTime)); err != nil {
		return statementError("logs", "logevent", err, logInsertQuery)
	}

	return nil
}

// SearchLogs returns the total the criteria match and the page of entries they select,
// in the requested order from the requested offset. The criteria is already resolved by
// the query handler, and the date window is the one `domain.LogSearch` models. The
// filter is assembled from fixed fragments and placeholders; the only text joined into
// the statement is the sort direction, chosen from two constants, per the log statement
// construction maintainer note.
func (logs *Logs) SearchLogs(ctx context.Context, search domain.LogSearch) (ports.LogSearchPage, error) {
	where := []string{`TRUE`}
	args := []any{}

	if search.HasLevel() {
		where = append(where, `"level" = ?`)
		args = append(args, search.Level)
	}

	// The message search is a case-insensitive substring with the submitted text escaped so
	// a % or an _ matches itself; lower on both sides keeps the statement case-insensitive
	// on backends where LIKE is not, per the log statement construction maintainer note.
	if search.HasText() {
		where = append(where, `lower("message") LIKE ? ESCAPE '!'`)
		args = append(args, "%"+escapeLikeText(strings.ToLower(search.Text))+"%")
	}

	if search.HasDate() {
		from, to, _ := search.DateWindow()

		where = append(where, `("timestamp" BETWEEN ? AND ?)`)
		args = append(args, millisOf(from), millisOf(to))
	}

	// The per-source counts are computed from the criteria as submitted except for the
	// source itself: they have to describe the whole match while the operator is still
	// choosing, per the log search page posture maintainer note.
	countsWhere := append([]string{}, where...)
	countsArgs := append([]any{}, args...)

	if search.HasSource() {
		// A row stored without a source is the same thing the log view calls legacy, so
		// asking for legacy finds both spellings.
		if search.Source == domain.LogSourceLegacy {
			where = append(where, `("source" = '' OR "source" = ?)`)
		} else {
			where = append(where, `"source" = ?`)
		}

		args = append(args, string(search.Source))
	}

	filter := strings.Join(where, " AND ")

	order := "ASC"
	if !search.Ascending {
		order = "DESC"
	}

	page := ports.LogSearchPage{}

	// The span of everything the criteria match is read first, in the legacy read order:
	// matching timestamps ascending, then descending, then the count, then the page. The
	// two ends belong to the whole match, not the page, per the log search page posture
	// maintainer note.
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

	// The count is scanned as the 64-bit integer the database answers and
	// clamped, not cast: a cast of a negative is the largest count
	// there is, and on a 32-bit build a plain `int` target truncates in
	// silence.
	page.Count = clampNonNegative(count)

	// The per-subsystem counts are read over the whole match and not over the page, so
	// they stay the same while the log view pages, per the log search page posture
	// maintainer note.
	countsQuery := `SELECT "source", COUNT(*) FROM "logs" WHERE ` + strings.Join(countsWhere, " AND ") + ` GROUP BY "source"`

	countRows, err := logs.store.query(ctx, countsQuery, countsArgs...)
	if err != nil {
		return ports.LogSearchPage{}, statementError("logs", "search", err, countsQuery)
	}

	defer countRows.Close()

	counts := map[domain.LogSource]uint64{}

	for countRows.Next() {
		var (
			source sql.NullString
			n      int64
		)

		if err := countRows.Scan(&source, &n); err != nil {
			continue
		}

		// The two spellings a row without a source can carry — the blank the other
		// server's insert leaves, and the name this server files it under — are one
		// group in the log view, so they are one group here.
		counts[logSourceOf(source)] += uint64(n)
	}

	if err := countRows.Err(); err != nil {
		return ports.LogSearchPage{}, statementError("logs", "search", err, countsQuery)
	}

	for source, n := range counts {
		page.SourceCounts = append(page.SourceCounts, ports.LogSourceCount{Source: source, Count: n})
	}

	// Where the noise is, first. Ties are broken by the name so the same search always
	// answers the same list.
	sort.Slice(page.SourceCounts, func(i, j int) bool {
		if page.SourceCounts[i].Count != page.SourceCounts[j].Count {
			return page.SourceCounts[i].Count > page.SourceCounts[j].Count
		}

		return page.SourceCounts[i].Source < page.SourceCounts[j].Source
	})

	rowsQuery := `SELECT "logId", "level", "source", "message", "timestamp" FROM "logs" WHERE ` + filter + ` ORDER BY "timestamp" ` + order + ` LIMIT ? OFFSET ?`

	rows, err := logs.store.query(ctx, rowsQuery, append(args, search.Limit, search.Offset)...)
	if err != nil {
		return ports.LogSearchPage{}, statementError("logs", "search", err, rowsQuery)
	}

	defer rows.Close()

	for rows.Next() {
		var (
			id        sql.NullInt64
			level     sql.NullString
			source    sql.NullString
			message   sql.NullString
			timestamp sql.NullInt64
		)

		if err := rows.Scan(&id, &level, &source, &message, &timestamp); err != nil {
			// A row whose scan fails is dropped as the legacy drops a damaged row —
			// unless the read itself failed, per the damaged log row parity
			// maintainer note: a cancelled context is felt at the scan and not at
			// the iterator, and a short page answered as a success hides that.
			if readErr := rows.Err(); readErr != nil {
				return ports.LogSearchPage{}, statementError("logs", "search", readErr, rowsQuery)
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				return ports.LogSearchPage{}, statementError("logs", "search", ctxErr, rowsQuery)
			}

			continue
		}

		// A row that carries nothing to show — no identity, no level, no message, or
		// a timestamp that is not a real instant — is dropped from the results while
		// the count still includes it, as the legacy does, per the damaged log row
		// parity maintainer note.
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
			Source:   logSourceOf(source),
			Message:  message.String,
		})
	}

	// A read that ended early — a cancelled context, a connection lost mid-page —
	// is a search that did not complete. The loop above skips a row whose
	// scan failed, which is what the legacy does for a damaged row; the read's own
	// failure is not a damaged row, and answering a short page as a success is how a
	// cancelled read goes unnoticed.
	if err := rows.Err(); err != nil {
		return ports.LogSearchPage{}, statementError("logs", "search", err, rowsQuery)
	}

	return page, nil
}

// logSourceOf reads the source a row carries. A row written without one is filed under
// legacy — a read-side mapping, never a rewrite of another server's rows, per the
// log-source write and read posture maintainer note.
func logSourceOf(source sql.NullString) domain.LogSource {
	if !source.Valid || source.String == "" {
		return domain.LogSourceLegacy
	}

	return domain.LogSource(source.String)
}

// escapeLikeText prepares submitted text for a LIKE comparison so the characters the
// operator typed match themselves, escaping with `!` because `ESCAPE '\'` is not one
// statement on every backend, per the log statement construction maintainer note.
func escapeLikeText(text string) string {
	out := strings.Builder{}

	for _, r := range text {
		switch r {
		case '!', '%', '_':
			out.WriteByte('!')
		}

		out.WriteRune(r)
	}

	return out.String()
}

// searchInstant reads one instant out of the entries the criteria match — the same
// filter ordered from one end, so the first row is the span end. A search that matches
// nothing leaves that end unset rather than failing, as the legacy tolerates here.
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

// DeleteLogsBefore removes the entries older than the given instant and reports how many
// went — the legacy prune reported nothing, so its own log entry never knew the count.
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
