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
	"time"

	"rdio-scanner/internal/application/ports"
)

// The tallies and the newest rows the status view draws, kept apart from the call
// history's page: the history pages calls for a scoped caller, the status view counts
// for the operator of the whole server, and neither depends on the other. Every query
// here joins the `delayed` table with its `d."callId" IS NULL` filter so calls still
// held are absent from the status view; why the two stay apart is in the status-view-
// tallies maintainer note.

var _ ports.CallActivityRepository = (*Calls)(nil)

const (
	// hourMillis is one whole hour in the unit every timestamp in this schema is stored in.
	hourMillis = int64(3_600_000)

	// historySection names this repository in the failure it reports, the way every other statement
	// in this package names the operation that failed.
	historySection = "calls"
)

// CountCallsSince counts the recorded calls at or after the given instant.
func (calls *Calls) CountCallsSince(ctx context.Context, from time.Time) (uint64, error) {
	const query = `SELECT COUNT(*) ` + callHistoryFrom + ` WHERE d."callId" IS NULL AND c."timestamp" >= ?`

	var count int64

	if err := calls.queryRow(ctx, query, millisOf(from)).Scan(&count); err != nil {
		return 0, statementError(historySection, "countcalls", err, query)
	}

	return clampNonNegative(count), nil
}

// CountCallsByHour counts the recorded calls grouped into whole hours, oldest first.
// Hours with no calls get no row: filling the quiet hours with a zero is the query
// handler's job, so the chart's shape does not depend on what the database held.
func (calls *Calls) CountCallsByHour(ctx context.Context, from time.Time) ([]ports.CallHourCount, error) {
	bucket := hourBucketExpression(calls.dialect, `c."timestamp"`)

	query := `SELECT ` + bucket + ` AS "bucket", COUNT(*) ` + callHistoryFrom +
		` WHERE d."callId" IS NULL AND c."timestamp" >= ? GROUP BY "bucket" ORDER BY "bucket" ASC`

	rows, err := calls.query(ctx, query, millisOf(from))
	if err != nil {
		return nil, statementError(historySection, "hourcalls", err, query)
	}

	defer rows.Close()

	counts := []ports.CallHourCount{}

	for rows.Next() {
		var (
			index int64
			count int64
		)

		if err := rows.Scan(&index, &count); err != nil {
			return nil, operationError(historySection, "hourcalls", err)
		}

		counts = append(counts, ports.CallHourCount{Hour: hourOfBucket(index), Count: clampNonNegative(count)})
	}

	if err := rows.Err(); err != nil {
		return nil, operationError(historySection, "hourcalls", err)
	}

	return counts, nil
}

// CountCallsByTalkgroup counts the recorded calls per talkgroup over the window, most
// first, cut at the limit. Each tally carries the talkgroup's name and label.
func (calls *Calls) CountCallsByTalkgroup(ctx context.Context, from time.Time, limit uint) ([]ports.CallTalkgroupCount, error) {
	query := `SELECT s."systemRef", t."talkgroupRef", t."name", t."label", COUNT(*) ` + callHistoryFrom +
		` WHERE d."callId" IS NULL AND c."timestamp" >= ?` +
		` GROUP BY s."systemRef", t."talkgroupRef", t."name", t."label"` +
		` ORDER BY COUNT(*) DESC, t."talkgroupRef" ASC LIMIT ?`

	rows, err := calls.query(ctx, query, millisOf(from), limit)
	if err != nil {
		return nil, statementError(historySection, "talkgroupcalls", err, query)
	}

	defer rows.Close()

	tallies := []ports.CallTalkgroupCount{}
	clamped := []string{}

	for rows.Next() {
		var (
			systemRef    int64
			talkgroupRef int64
			name         sql.NullString
			label        sql.NullString
			count        int64
		)

		if err := rows.Scan(&systemRef, &talkgroupRef, &name, &label, &count); err != nil {
			return nil, operationError(historySection, "talkgroupcalls", err)
		}

		// The joined references are signed columns: a negative one is clamped to
		// zero and reported, the signed-column clamp posture the configuration
		// repositories already take.
		values := []string{}

		if systemRef < 0 {
			values = append(values, fmt.Sprintf("systemRef %d read as 0", systemRef))
		}

		if talkgroupRef < 0 {
			values = append(values, fmt.Sprintf("talkgroupRef %d read as 0", talkgroupRef))
		}

		if len(values) > 0 {
			clamped = append(clamped, fmt.Sprintf("calls.talkgroupcalls: talkgroup %d %q holds %s: these are not values this server can hold, and it enforces the clamped ones",
				talkgroupRef, name.String, strings.Join(values, ", ")))
		}

		tallies = append(tallies, ports.CallTalkgroupCount{
			SystemRef:    uint(clampNonNegative(systemRef)),
			TalkgroupRef: uint(clampNonNegative(talkgroupRef)),
			Name:         name.String,
			Label:        label.String,
			Count:        clampNonNegative(count),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, operationError(historySection, "talkgroupcalls", err)
	}

	calls.reportClamps(clamped)

	return tallies, nil
}

// RecentCalls returns the newest recorded calls, newest first, as the same summaries the
// history page is handed — never with the audio, which this polled view must not carry.
func (calls *Calls) RecentCalls(ctx context.Context, limit uint) ([]ports.CallSummary, error) {
	const query = `SELECT c."callId", c."timestamp", c."audioLength", s."systemRef", t."talkgroupRef" ` +
		callHistoryFrom + ` WHERE d."callId" IS NULL ORDER BY c."timestamp" DESC, c."callId" DESC LIMIT ?`

	rows, err := calls.query(ctx, query, limit)
	if err != nil {
		return nil, statementError(historySection, "recentcalls", err, query)
	}

	defer rows.Close()

	recent := []ports.CallSummary{}
	clamped := []string{}

	for rows.Next() {
		var (
			id           int64
			timestamp    int64
			audioLength  int64
			systemRef    int64
			talkgroupRef int64
		)

		if err := rows.Scan(&id, &timestamp, &audioLength, &systemRef, &talkgroupRef); err != nil {
			return nil, operationError(historySection, "recentcalls", err)
		}

		// The joined references are signed columns: a negative one is clamped to
		// zero and reported. The identity keeps its stored bit pattern, so the row
		// the view hands out stays openable by the same number.
		values := []string{}

		if systemRef < 0 {
			values = append(values, fmt.Sprintf("systemRef %d read as 0", systemRef))
		}

		if talkgroupRef < 0 {
			values = append(values, fmt.Sprintf("talkgroupRef %d read as 0", talkgroupRef))
		}

		if len(values) > 0 {
			clamped = append(clamped, fmt.Sprintf("calls.recentcalls: call %d holds %s: these are not values this server can hold, and it enforces the clamped ones",
				id, strings.Join(values, ", ")))
		}

		recent = append(recent, ports.CallSummary{
			ID:           uint64(id),
			SystemRef:    uint(clampNonNegative(systemRef)),
			TalkgroupRef: uint(clampNonNegative(talkgroupRef)),
			Timestamp:    instantOf(timestamp),
			AudioLength:  durationOf(audioLength),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, operationError(historySection, "recentcalls", err)
	}

	calls.reportClamps(clamped)

	return recent, nil
}

// hourBucketExpression is the hour bucket written for the dialect it is asked for:
// MySQL and MariaDB need `DIV` to truncate, SQLite and PostgreSQL truncate `/`.
// Why the operators differ is in the hour-bucket-dialect-divide maintainer note, and
// the divisor is the same hour `hourOfBucket` and `bucketOfHour` divide by on the Go
// side, which the test that names both holds.
func hourBucketExpression(dialect Dialect, column string) string {
	switch dialect {
	case DialectMysql, DialectMariadb:
		return column + ` DIV 3600000`
	default:
		return column + ` / 3600000`
	}
}

// bucketOfHour is the index of the whole hour an instant falls in. Timestamps here are
// recording instants, never before the epoch, so the backends' truncation and Go's agree.
func bucketOfHour(instant time.Time) int64 {
	return instant.UnixMilli() / hourMillis
}

// hourOfBucket turns the index a bucket is reported as back into the instant that hour begins. The
// multiplication is done here and not in SQL, which is what keeps the four backends' date functions out
// of the read entirely.
func hourOfBucket(bucket int64) time.Time {
	return time.UnixMilli(bucket * hourMillis)
}
