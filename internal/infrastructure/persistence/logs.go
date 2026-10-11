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
	"fmt"
	"sort"
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

// errUnknownLogSource is what AppendLog reports for an entry that does not name a part of this server.
// The vocabulary is owned by the Domain; this is the boundary the entries are written through.
var errUnknownLogSource = errors.New("the log entry names no part of this server")

// The legacy server's statements, with the values taken out of them.
//
// `Logs.LogEvent` builds its insert with `fmt.Sprintf` and puts the message in raw, so a message
// containing a single quote — which is every line that names a failed statement — produces broken
// SQL, the insert fails, and the error is discarded by every caller. That is Q21, and the
// parameterized insert here is its fix: those lines are stored, and the administrator's log view
// shows them.
const (
	logInsertQuery = `INSERT INTO "logs" ("level", "source", "message", "timestamp") VALUES (?, ?, ?, ?)`
	logDeleteQuery = `DELETE FROM "logs" WHERE "timestamp" < ?`
)

// NewLogs returns the log repository for one database.
func NewLogs(db *sql.DB, dialect Dialect) *Logs {
	return &Logs{store: newStore(db, dialect)}
}

// AppendLog records one entry. The entry's own timestamp is what is stored, in the Unix
// milliseconds the column carries, and so is the part of the server that wrote it.
//
// A source that is a name this server does not have is refused. The vocabulary is owned by the Domain and
// every writer reaches it through a constant, so arriving here with a name that is not one means a writer
// was added with a misspelling, and a log table holding a misspelling is exactly the state this feature
// exists to remove.
//
// An entry with no source at all is filed under legacy rather than stored as a blank. That is the state the
// writers are in while this feature is being built — the repository gained the column before they gained
// their sources — and it is the state a row the other server writes is in permanently, because its insert
// names the columns it knows and never mentions this one. The guard that makes a writer without a source a
// failure is the one that lands once every writer names itself.
// Every entry this server writes names the part of the server that wrote it, and this is the one boundary
// every writer reaches. An entry that names nothing is refused rather than filed under legacy: a writer added
// later without a source fails at the write instead of quietly producing a row the log view can only call
// old.
//
// A row the *other* server writes is not a refusal and cannot be one. Its insert names the columns it knows
// and never mentions this one, so rows with no source exist permanently in a database this server inherited;
// the read side files them under legacy, which is what an operator filtering for legacy is looking for.
func (logs *Logs) AppendLog(ctx context.Context, entry domain.LogEntry) error {
	if !domain.IsKnownLogSource(entry.Source) {
		return operationError("logs", "logevent", fmt.Errorf("%w: %q", errUnknownLogSource, entry.Source))
	}

	if _, err := logs.store.exec(ctx, logInsertQuery, entry.Level, string(entry.Source), entry.Message, millisOf(entry.DateTime)); err != nil {
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

	// The message search is a substring, matched case-insensitively, with the submitted text escaped so
	// that a % or an _ in what the operator typed matches itself. lower() on both sides is what makes the
	// same statement case-insensitive on PostgreSQL, where LIKE is not.
	if search.HasText() {
		where = append(where, `lower("message") LIKE ? ESCAPE '!'`)
		args = append(args, "%"+escapeLikeText(strings.ToLower(search.Text))+"%")
	}

	if search.HasDate() {
		from, to, _ := search.DateWindow()

		where = append(where, `("timestamp" BETWEEN ? AND ?)`)
		args = append(args, millisOf(from), millisOf(to))
	}

	// The per-source counts are computed from the criteria as submitted except for the source itself:
	// asking "how many of each" while already filtered to one source answers one row, which is true and
	// useless. The log view offers the narrowing from these counts, so they have to describe the whole
	// match while the operator is still choosing.
	countsWhere := append([]string{}, where...)
	countsArgs := append([]any{}, args...)

	if search.HasSource() {
		// A row stored without a source — the other server's insert, which never mentions the column — is
		// the same thing the log view calls legacy, so asking for legacy has to find both spellings.
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

	// The count is scanned as the 64-bit integer the database answers and
	// clamped, not cast (CA-020.1): a cast of a negative is the largest count
	// there is, and on a 32-bit build a plain `int` target truncates in
	// silence.
	page.Count = clampNonNegative(count)

	// The per-subsystem counts are read over the whole match and not over the page, so they stay the same
	// while the log view pages through 200 entries at a time.
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

		// The two spellings a row without a source can carry — the blank the other server's insert
		// leaves, and the name this server files it under — are one group in the log view, so they are
		// one group here.
		counts[logSourceOf(source)] += uint64(n)
	}

	if err := countRows.Err(); err != nil {
		return ports.LogSearchPage{}, statementError("logs", "search", err, countsQuery)
	}

	for source, n := range counts {
		page.SourceCounts = append(page.SourceCounts, ports.LogSourceCount{Source: source, Count: n})
	}

	// Where the noise is, first. Ties are broken by the name so the same search always answers the same
	// list.
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
			// unless the read itself failed. A cancelled context is felt at the scan
			// and not at the iterator, and a short page answered as a success is how
			// a cancelled search goes unnoticed (CA-011.1).
			if readErr := rows.Err(); readErr != nil {
				return ports.LogSearchPage{}, statementError("logs", "search", readErr, rowsQuery)
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				return ports.LogSearchPage{}, statementError("logs", "search", ctxErr, rowsQuery)
			}

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
			Source:   logSourceOf(source),
			Message:  message.String,
		})
	}

	// A read that ended early — a cancelled context, a connection lost mid-page —
	// is a search that did not complete (CA-011.1). The loop above skips a row whose
	// scan failed, which is what the legacy does for a damaged row; the read's own
	// failure is not a damaged row, and answering a short page as a success is how a
	// cancelled read goes unnoticed.
	if err := rows.Err(); err != nil {
		return ports.LogSearchPage{}, statementError("logs", "search", err, rowsQuery)
	}

	return page, nil
}

// logSourceOf reads the source a row carries. A row written without one — by the legacy server, which
// inserts into this table by named columns and never mentions a column it does not know — is filed under
// legacy, so the log view never shows a blank and a filter on legacy finds those rows.
//
// This is a read-side mapping and not a write: this server does not rewrite rows another server is
// writing.
func logSourceOf(source sql.NullString) domain.LogSource {
	if !source.Valid || source.String == "" {
		return domain.LogSourceLegacy
	}

	return domain.LogSource(source.String)
}

// escapeLikeText prepares submitted text for a LIKE comparison so that the characters the operator typed
// match themselves: a directory mask, a talkgroup wildcard, an underscore in a filename.
//
// The escape character is `!` rather than a backslash because MySQL reads a backslash as an escape inside a
// string literal, and `ESCAPE '\'` is not the same statement on every backend this server supports.
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
