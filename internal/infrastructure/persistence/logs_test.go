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
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// TestAppendLogStoresAMessageContainingSingleQuotes is Q21, the one defect this task is allowed to
// fix.
//
// The legacy `server/log.go` `LogEvent` interpolates the message into its `INSERT` with `fmt.Sprintf`, so a
// message containing a single quote produces broken SQL. Every line that names a failed statement
// contains the statement, and therefore contains quotes: the insert fails, `LogEvent` returns the
// error, and every caller discards it — so the administrator's log view is missing exactly the
// entries that explain a failed save. The parameterized insert stores them.
func TestAppendLogStoresAMessageContainingSingleQuotes(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options")
	logs := NewLogs(db, DialectSqlite)

	const message = `accesses.write: constraint failed: FOREIGN KEY constraint failed (787) in INSERT INTO "accesses" ("ident") VALUES ('O'Brien')`

	if err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
		Level:    domain.LogLevelError,
		Source:   domain.LogSourceServer,
		Message:  message,
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if len(page.Results) != 1 {
		t.Fatalf("the log view has %d entries, want the one entry whose message is full of quotes", len(page.Results))
	}

	if page.Results[0].Message != message {
		t.Errorf("the stored message is %q, want the submitted one unchanged", page.Results[0].Message)
	}
}

// TestAppendLogStoresAMessageContainingSingleQuotesAgainstALegacyDatabase runs the same fix over a
// database the legacy server wrote, which is the case REQ-007 is about: the log view a deployment
// already has, and the line that has never been able to appear in it.
func TestAppendLogStoresAMessageContainingSingleQuotesAgainstALegacyDatabase(t *testing.T) {
	db := openMigratedLegacyDatabase(t)
	logs := NewLogs(db, DialectSqlite)

	before, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	const message = `systems.writetx: constraint failed: FOREIGN KEY constraint failed (787) in INSERT INTO "talkgroups" ("tagId") VALUES ('9')`

	if err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
		Level:    domain.LogLevelError,
		Source:   domain.LogSourceServer,
		Message:  message,
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	after, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if after.Count != before.Count+1 {
		t.Fatalf("the log view went from %d entries to %d, want one more", before.Count, after.Count)
	}

	var found bool

	for _, entry := range after.Results {
		if entry.Message == message {
			found = true

			break
		}
	}

	if !found {
		t.Error("the entry with quotes in its message is not in the log view read from a legacy-written database")
	}
}

func TestSearchLogsReturnsTheEntriesTheLegacyServerStored(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t), DialectSqlite)

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 5 || len(page.Results) != 5 {
		t.Fatalf("SearchLogs returned %d entries of %d, want the five the legacy server stored", len(page.Results), page.Count)
	}

	for i := 1; i < len(page.Results); i++ {
		if page.Results[i].DateTime.Before(page.Results[i-1].DateTime) {
			t.Errorf("entries %d and %d are out of order: %v then %v", i-1, i, page.Results[i-1].DateTime, page.Results[i].DateTime)
		}
	}

	if first, last := page.Results[0], page.Results[len(page.Results)-1]; first.Message != "server started" || !strings.HasPrefix(last.Message, "newcall: system=10001 talkgroup=1514002") {
		t.Errorf("the log view runs %q to %q, want the stored first and last entries", first.Message, last.Message)
	}

	warns, infos := 0, 0

	for _, entry := range page.Results {
		switch entry.Level {
		case domain.LogLevelWarn:
			warns++
		case domain.LogLevelInfo:
			infos++
		}
	}

	if warns != 3 || infos != 2 {
		t.Errorf("the stored entries carry %d warn and %d info levels, want 3 and 2", warns, infos)
	}
}

// TestAppendLogReadsBackTheInstantItStored pins the millisecond rule: every timestamp in this
// schema is Unix milliseconds, and an entry's own instant is what is stored.
func TestAppendLogReadsBackTheInstantItStored(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	at := time.Date(2026, 3, 14, 9, 30, 15, 500_000_000, time.UTC)

	if err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: at,
		Level:    domain.LogLevelInfo,
		Source:   domain.LogSourceServer,
		Message:  "stored with its own instant",
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if len(page.Results) != 1 {
		t.Fatalf("the log view has %d entries, want 1", len(page.Results))
	}

	if !page.Results[0].DateTime.Equal(at) {
		t.Errorf("the entry reads back at %v, want %v down to the millisecond", page.Results[0].DateTime, at)
	}
}

func TestSearchLogsCountsEveryEntryTheCriteriaMatches(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	for i := range 5 {
		if err := logs.AppendLog(context.Background(), domain.LogEntry{
			DateTime: base.Add(time.Duration(i) * time.Minute),
			Level:    domain.LogLevelInfo,
			Source:   domain.LogSourceServer,
			Message:  "entry",
		}); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: 2, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 5 {
		t.Errorf("Count = %d, want the 5 entries the criteria matches, not the page size", page.Count)
	}

	if len(page.Results) != 2 {
		t.Errorf("the page has %d entries, want the requested 2", len(page.Results))
	}
}

func TestSearchLogsOrdersByTheRequestedDirection(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	for _, entry := range []struct {
		offset  time.Duration
		message string
	}{
		{0 * time.Minute, "first"},
		{1 * time.Minute, "second"},
		{2 * time.Minute, "third"},
	} {
		if err := logs.AppendLog(context.Background(), domain.LogEntry{
			DateTime: base.Add(entry.offset),
			Level:    domain.LogLevelInfo,
			Source:   domain.LogSourceServer,
			Message:  entry.message,
		}); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	for _, tc := range []struct {
		name      string
		ascending bool
		want      []string
	}{
		{"ascending", true, []string{"first", "second", "third"}},
		{"descending", false, []string{"third", "second", "first"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := logs.SearchLogs(context.Background(), domain.LogSearch{
				Limit:     domain.DefaultLogSearchLimit,
				Ascending: tc.ascending,
			})
			if err != nil {
				t.Fatalf("SearchLogs: %v", err)
			}

			if len(page.Results) != len(tc.want) {
				t.Fatalf("the page has %d entries, want %d", len(page.Results), len(tc.want))
			}

			for i, message := range tc.want {
				if page.Results[i].Message != message {
					t.Errorf("entry %d is %q, want %q", i, page.Results[i].Message, message)
				}
			}
		})
	}
}

func TestSearchLogsStartsAtTheRequestedOffset(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	for i := range 4 {
		if err := logs.AppendLog(context.Background(), domain.LogEntry{
			DateTime: base.Add(time.Duration(i) * time.Minute),
			Level:    domain.LogLevelInfo,
			Source:   domain.LogSourceServer,
			Message:  "entry",
		}); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: 2, Offset: 2, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 4 {
		t.Errorf("Count = %d, want the 4 entries the criteria matches", page.Count)
	}

	if len(page.Results) != 2 {
		t.Fatalf("the page has %d entries, want 2", len(page.Results))
	}

	want := base.Add(2 * time.Minute)
	if !page.Results[0].DateTime.Equal(want) {
		t.Errorf("the page starts at %v, want the third entry at %v", page.Results[0].DateTime, want)
	}
}

// TestSearchLogsReturnsNoRowsForAnExplicitZeroLimit pins the paging rule the fakes carry: a limit
// of zero asks for no rows at all, which is what the stored query does with it, while the count is
// still the whole match.
func TestSearchLogsReturnsNoRowsForAnExplicitZeroLimit(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	if err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC),
		Level:    domain.LogLevelInfo,
		Source:   domain.LogSourceServer,
		Message:  "entry",
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: 0})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 1 {
		t.Errorf("Count = %d, want 1", page.Count)
	}

	if len(page.Results) != 0 {
		t.Errorf("the page has %d entries, want none", len(page.Results))
	}
}

func TestSearchLogsKeepsOnlyTheRequestedLevel(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	for i, level := range []string{domain.LogLevelInfo, domain.LogLevelWarn, domain.LogLevelError, domain.LogLevelWarn} {
		if err := logs.AppendLog(context.Background(), domain.LogEntry{
			DateTime: base.Add(time.Duration(i) * time.Minute),
			Level:    level,
			Source:   domain.LogSourceServer,
			Message:  level + " entry",
		}); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{
		Level: domain.LogLevelWarn,
		Limit: domain.DefaultLogSearchLimit,
	})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 2 || len(page.Results) != 2 {
		t.Fatalf("the warn search matched %d of %d entries, want 2", len(page.Results), page.Count)
	}

	for _, entry := range page.Results {
		if entry.Level != domain.LogLevelWarn {
			t.Errorf("the level filter returned %+v, want only warn entries", entry)
		}
	}
}

// TestSearchLogsAppliesTheAscendingDateWindow pins the window `domain.LogSearch` models: the
// submitted instant truncated to the minute, running forward a full day.
func TestSearchLogsAppliesTheAscendingDateWindow(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	day := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

	for _, at := range []time.Time{
		day.Add(-time.Millisecond),
		day,
		day.Add(24*time.Hour - time.Millisecond),
		day.Add(24 * time.Hour),
	} {
		if err := logs.AppendLog(context.Background(), domain.LogEntry{
			DateTime: at,
			Level:    domain.LogLevelInfo,
			Source:   domain.LogSourceServer,
			Message:  at.Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{
		Date:      time.Date(2026, 3, 14, 9, 30, 15, 500_000_000, time.UTC),
		Limit:     domain.DefaultLogSearchLimit,
		Ascending: true,
	})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 2 || len(page.Results) != 2 {
		t.Fatalf("the ascending window matched %d of %d entries, want the two inside [%v, %v]", len(page.Results), page.Count, day, day.Add(24*time.Hour-time.Millisecond))
	}

	if !page.Results[0].DateTime.Equal(day) {
		t.Errorf("the window starts at %v, want the submitted instant truncated to the minute: %v", page.Results[0].DateTime, day)
	}
}

// TestSearchLogsAppliesTheDescendingDateWindow pins the other half of the asymmetry: a day back,
// and then back again by the submitted hour and minute.
func TestSearchLogsAppliesTheDescendingDateWindow(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	from := time.Date(2026, 3, 13, 0, 0, 0, 0, time.UTC)
	to := from.Add(24*time.Hour - time.Millisecond - 9*time.Hour - 30*time.Minute)

	for _, at := range []time.Time{
		from.Add(-time.Millisecond),
		from,
		to,
		to.Add(time.Millisecond),
	} {
		if err := logs.AppendLog(context.Background(), domain.LogEntry{
			DateTime: at,
			Level:    domain.LogLevelInfo,
			Source:   domain.LogSourceServer,
			Message:  at.Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{
		Date:      time.Date(2026, 3, 14, 9, 30, 15, 500_000_000, time.UTC),
		Limit:     domain.DefaultLogSearchLimit,
		Ascending: false,
	})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 2 || len(page.Results) != 2 {
		t.Fatalf("the descending window matched %d of %d entries, want the two inside [%v, %v]", len(page.Results), page.Count, from, to)
	}

	if !page.Results[0].DateTime.Equal(to) {
		t.Errorf("the descending page starts at %v, want the newest entry in the window: %v", page.Results[0].DateTime, to)
	}
}

// TestSearchLogsCountsARowTheLegacyServerLeftOutOfTheResults pins a legacy read rule rather than a
// fix: `server/log.go` `Search` counts every row the criteria matches, and then drops from the
// results any row that carries no identity, no level, no message, or a timestamp that is not a
// real instant.
func TestSearchLogsCountsARowTheLegacyServerLeftOutOfTheResults(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options")
	logs := NewLogs(db, DialectSqlite)

	const query = `INSERT INTO "logs" ("level", "message", "timestamp") VALUES (?, ?, ?)`

	if _, err := db.Exec(query, domain.LogLevelInfo, "", 1791217116088); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	if _, err := db.Exec(query, domain.LogLevelInfo, "no instant", 0); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	if err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC),
		Level:    domain.LogLevelInfo,
		Source:   domain.LogSourceServer,
		Message:  "a real entry",
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 3 {
		t.Errorf("Count = %d, want the 3 rows the criteria matches, including the two the legacy server would not show", page.Count)
	}

	if len(page.Results) != 1 || page.Results[0].Message != "a real entry" {
		t.Errorf("the log view has %+v, want only the entry that carries something to show", page.Results)
	}
}

func TestDeleteLogsBeforeRemovesTheOlderEntriesAndReportsHowManyWent(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options")
	logs := NewLogs(db, DialectSqlite)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	for i := range 4 {
		if err := logs.AppendLog(context.Background(), domain.LogEntry{
			DateTime: base.Add(time.Duration(i) * 24 * time.Hour),
			Level:    domain.LogLevelInfo,
			Source:   domain.LogSourceServer,
			Message:  "entry",
		}); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	removed, err := logs.DeleteLogsBefore(context.Background(), base.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("DeleteLogsBefore: %v", err)
	}

	if removed != 2 {
		t.Errorf("DeleteLogsBefore removed %d entries, want the 2 older than the given instant", removed)
	}

	if count := countRows(t, db, "logs"); count != 2 {
		t.Errorf("the table has %d entries after the prune, want 2", count)
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	for _, entry := range page.Results {
		if entry.DateTime.Before(base.Add(48 * time.Hour)) {
			t.Errorf("the prune left %v, which is older than the instant it was given", entry.DateTime)
		}
	}
}

func TestAppendLogReportsTheLegacyFailureShape(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options")

	if _, err := db.Exec(`DROP TABLE "logs"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	err := NewLogs(db, DialectSqlite).AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Now(),
		Level:    domain.LogLevelInfo,
		Source:   domain.LogSourceServer,
		Message:  "nothing to store",
	})

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "logs.logevent: ") {
		t.Errorf("error = %v, want the legacy logs.logevent shape", err)
	}
}

func TestSearchLogsReportsTheLegacyFailureShape(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options")

	if _, err := db.Exec(`DROP TABLE "logs"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	_, err := NewLogs(db, DialectSqlite).SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "logs.search: ") {
		t.Errorf("error = %v, want the legacy logs.search shape", err)
	}
}

func TestDeleteLogsBeforeReportsTheLegacyFailureShape(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options")

	if _, err := db.Exec(`DROP TABLE "logs"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	_, err := NewLogs(db, DialectSqlite).DeleteLogsBefore(context.Background(), time.Now())

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "logs.prune: ") {
		t.Errorf("error = %v, want a failure the log line can attribute to the prune", err)
	}
}

// TestSearchLogsReportsTheSpanOfEverythingTheCriteriaMatch is what the administrator's log view asks
// the server for: the earliest and latest instants among everything the criteria match, and not merely
// the ends of the page that was shown. Paging is how an administrator moves through that span, so a
// span taken from the page would move its ends every time the page changed.
//
// The legacy server reads the span with two queries of its own — the matching timestamps ascending and
// descending — before it counts or pages anything, and this server asks the same three questions in the
// same order.
func TestSearchLogsReportsTheSpanOfEverythingTheCriteriaMatch(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t), DialectSqlite)

	whole, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if whole.Count != 5 {
		t.Fatalf("the stored log holds %d entries, want the five the legacy server stored", whole.Count)
	}

	first, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: 1, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if first.Count != 5 || len(first.Results) != 1 {
		t.Fatalf("the first page holds %d entries of %d, want one entry of five", len(first.Results), first.Count)
	}

	if !first.DateStart.Equal(whole.DateStart) {
		t.Errorf("the span starts at %v, want %v: the earliest entry the criteria match, not the earliest entry on this page", first.DateStart, whole.DateStart)
	}

	if !first.DateStop.Equal(whole.DateStop) {
		t.Errorf("the span stops at %v, want %v: the latest entry the criteria match, not the latest entry on this page", first.DateStop, whole.DateStop)
	}

	// The span is the whole match and not the page, so the ends of the span are not the ends of the
	// entries that were returned.
	if first.DateStart.Equal(first.DateStop) {
		t.Errorf("the span of five entries is a single instant %v, want the ends of everything the criteria match", first.DateStart)
	}
}

// TestSearchLogsReportsTheSpanOfTheEntriesThatMatchTheLevelFilter pins the span against the same
// criteria the log view submits, because the span belongs to the filtered set: an administrator who
// asks for warnings only is shown the span of the warnings, and paging moves through those.
func TestSearchLogsReportsTheSpanOfTheEntriesThatMatchTheLevelFilter(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t), DialectSqlite)

	warns, err := logs.SearchLogs(context.Background(), domain.LogSearch{Level: domain.LogLevelWarn, Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if warns.Count < 2 {
		t.Fatalf("the stored log holds %d warnings, want at least two so that the span has two ends", warns.Count)
	}

	if warns.DateStart.After(warns.DateStop) {
		t.Errorf("the warnings span runs %v to %v, want the earliest warning first", warns.DateStart, warns.DateStop)
	}

	for _, entry := range warns.Results {
		if entry.DateTime.Before(warns.DateStart) || entry.DateTime.After(warns.DateStop) {
			t.Errorf("the entry at %v is outside the span %v to %v the same search reports", entry.DateTime, warns.DateStart, warns.DateStop)
		}
	}
}

// TestSearchLogsWithNothingToReportLeavesTheSpanUnset pins the shape an empty log view is answered
// with. The legacy server leaves both ends at the zero time when nothing matches — and not at the Unix
// epoch, which is what its call search leaves behind — and the administrator's log view shows that
// instant.
func TestSearchLogsWithNothingToReportLeavesTheSpanUnset(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t), DialectSqlite)

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Level: "not-a-level", Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 0 || len(page.Results) != 0 {
		t.Fatalf("a search for a level the log does not hold reports %d entries of %d, want nothing", len(page.Results), page.Count)
	}

	if !page.DateStart.IsZero() || !page.DateStop.IsZero() {
		t.Errorf("the span of nothing is %v to %v, want both ends unset", page.DateStart, page.DateStop)
	}
}

// T008: the log table stores which part of the server wrote an entry, and the log view can be filtered by
// it and by a substring of the message.
func TestAppendLogStoresTheSourceAndTheSearchReadsItBack(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "logs")
	logs := NewLogs(db, DialectSqlite)

	if err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
		Level:    domain.LogLevelError,
		Source:   domain.LogSourceDirwatch,
		Message:  "the watcher for /data/incoming stopped: no such file or directory",
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if len(page.Results) != 1 {
		t.Fatalf("the log view has %d entries, want the one entry that was just written", len(page.Results))
	}

	if page.Results[0].Source != domain.LogSourceDirwatch {
		t.Errorf("the entry carries source %q, want %q", page.Results[0].Source, domain.LogSourceDirwatch)
	}
}

// TestSearchLogsBySource is the filter the feature exists to give an operator: everything one subsystem
// said, and nothing another one said.
func TestSearchLogsBySource(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "logs"), DialectSqlite)

	store := []domain.LogEntry{
		{DateTime: time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC), Level: domain.LogLevelError, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming stopped"},
		{DateTime: time.Date(2026, 3, 14, 9, 1, 0, 0, time.UTC), Level: domain.LogLevelInfo, Source: domain.LogSourceDownstream, Message: "call sent to http://other:5002: success"},
		{DateTime: time.Date(2026, 3, 14, 9, 2, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Message: "the recording /data/incoming/audio.wav could not be ingested"},
	}

	for _, entry := range store {
		if err := logs.AppendLog(context.Background(), entry); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{
		Source: domain.LogSourceDirwatch, Limit: domain.DefaultLogSearchLimit, Ascending: true,
	})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 2 || len(page.Results) != 2 {
		t.Fatalf("a search for dirwatch matched %d entries and returned %d, want the 2 dirwatch entries", page.Count, len(page.Results))
	}

	for _, entry := range page.Results {
		if entry.Source != domain.LogSourceDirwatch {
			t.Errorf("the log view returned an entry with source %q under a dirwatch filter", entry.Source)
		}
	}
}

// TestSearchLogsByTextMatchesASubstringOfTheMessage is the other half of diagnosing by subsystem: the one
// path, call sign or URL the operator is chasing.
//
// The match is case-insensitive, and the % and _ of the submitted text match themselves: an operator
// searching a directory mask or a talkgroup wildcard is looking for those characters, not asking the
// database to interpret them.
func TestSearchLogsByTextMatchesASubstringOfTheMessage(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "logs"), DialectSqlite)

	store := []domain.LogEntry{
		{DateTime: time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC), Level: domain.LogLevelError, Source: domain.LogSourceDirwatch, Message: "the recording /data/incoming/audio.wav could not be ingested"},
		{DateTime: time.Date(2026, 3, 14, 9, 1, 0, 0, time.UTC), Level: domain.LogLevelInfo, Source: domain.LogSourceIngest, Message: "call from system 10001 talkgroup 1514000 success"},
		{DateTime: time.Date(2026, 3, 14, 9, 2, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceConfig, Message: "the configuration could not be saved: 100% of the systems failed"},
		{DateTime: time.Date(2026, 3, 14, 9, 3, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceConfig, Message: "the configuration could not be saved: a_b mask"},
	}

	for _, entry := range store {
		if err := logs.AppendLog(context.Background(), entry); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	for _, tc := range []struct {
		text string
		want uint64
	}{
		{"/data/incoming", 1},
		{"1514000", 1},
		{"COULD NOT", 3},
		{"could not be saved", 2},
		{"100%", 1},
		{"a_b", 1},
		{"audio.wav", 1},
		{"nothing like this", 0},
	} {
		page, err := logs.SearchLogs(context.Background(), domain.LogSearch{
			Text: tc.text, Limit: domain.DefaultLogSearchLimit, Ascending: true,
		})
		if err != nil {
			t.Fatalf("SearchLogs for %q: %v", tc.text, err)
		}

		if page.Count != tc.want || uint64(len(page.Results)) != tc.want {
			t.Errorf("a search for %q matched %d entries and returned %d, want %d", tc.text, page.Count, len(page.Results), tc.want)
		}
	}
}

// TestSearchLogsCombinesTheCriteriaTheOperatorActuallySubmits is the shape of a real diagnosis: this
// subsystem, this day, this bad, containing this word. Each row drops one criterion from the row above it,
// so a criterion that silently matched nothing shows up as a count that did not move.
func TestSearchLogsCombinesTheCriteriaTheOperatorActuallySubmits(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "logs"), DialectSqlite)

	store := []domain.LogEntry{
		{DateTime: time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC), Level: domain.LogLevelError, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming stopped"},
		{DateTime: time.Date(2026, 3, 14, 9, 1, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming stopped"},
		{DateTime: time.Date(2026, 3, 15, 9, 2, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming stopped"},
		{DateTime: time.Date(2026, 3, 15, 9, 3, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceIngest, Message: "the watcher for /data/incoming could not be ingested"},
	}

	for _, entry := range store {
		if err := logs.AppendLog(context.Background(), entry); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	// The ascending window runs forward a full day from the submitted instant, so 15 March 09:00 covers
	// the last two entries and not the first two.
	at, _ := time.Parse(time.RFC3339, "2026-03-15T09:00:00Z")

	for _, tc := range []struct {
		what   string
		search domain.LogSearch
		want   uint64
	}{
		{"the day alone", domain.LogSearch{Date: at}, 2},
		{"the day and the level", domain.LogSearch{Date: at, Level: domain.LogLevelWarn}, 2},
		{"the day, the level and the subsystem", domain.LogSearch{Date: at, Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch}, 1},
		{"the day, the level and the text", domain.LogSearch{Date: at, Level: domain.LogLevelWarn, Text: "ingested"}, 1},
		{"the level, the subsystem and the text, with no day", domain.LogSearch{Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Text: "/data/incoming"}, 2},
		{"every criterion at once", domain.LogSearch{Date: at, Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Text: "/data/incoming"}, 1},
	} {
		tc.search.Limit = domain.DefaultLogSearchLimit
		tc.search.Ascending = true

		page, err := logs.SearchLogs(context.Background(), tc.search)
		if err != nil {
			t.Fatalf("SearchLogs for %s: %v", tc.what, err)
		}

		if page.Count != tc.want || uint64(len(page.Results)) != tc.want {
			t.Errorf("%s matched %d entries and returned %d, want %d", tc.what, page.Count, len(page.Results), tc.want)
		}
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{
		Date: at, Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Text: "/data/incoming",
		Limit: domain.DefaultLogSearchLimit, Ascending: true,
	})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.DateStart.IsZero() || page.DateStop.IsZero() {
		t.Errorf("the span of everything the criteria match is %v to %v, want both ends of the matching entry", page.DateStart, page.DateStop)
	}
}

// TestAppendLogRefusesASourceThatIsNotANameOfThisServer keeps the vocabulary closed at the boundary the
// entries are written through. Every writer reaches the vocabulary through a domain constant, so arriving
// here with a name that is not one means a writer was added with a misspelling.
func TestAppendLogRefusesASourceThatIsNotANameOfThisServer(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "logs"), DialectSqlite)

	err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
		Level:    domain.LogLevelInfo,
		Source:   domain.LogSource("dirwatcher"),
		Message:  "an entry naming a subsystem this server does not have",
	})

	if err == nil {
		t.Fatal("a source outside the vocabulary was stored, and the vocabulary is closed")
	}

	if !strings.Contains(err.Error(), "dirwatcher") {
		t.Errorf("the failure says %q, want it to name the source it refused", err.Error())
	}
}

// Every entry this server writes names the part of the server that wrote it. The store is the one boundary
// every writer reaches, so refusing an entry that names nothing is where the vocabulary is enforced: a writer
// added later without a source fails at the write instead of quietly producing a row the log view can only
// file under legacy.
func TestAppendLogRefusesAnEntryThatNamesNoPartOfTheServer(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "logs")
	logs := NewLogs(db, DialectSqlite)

	err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
		Level:    domain.LogLevelInfo,
		Message:  "an entry written by a server that names no subsystem",
	})

	if err == nil {
		t.Fatal("an entry naming no part of this server was stored, and every writer is required to name one")
	}

	if !strings.Contains(err.Error(), "no part of this server") {
		t.Errorf("the failure says %q, want it to say the entry named no part of this server", err.Error())
	}

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "logs"`).Scan(&rows); err != nil {
		t.Fatalf("counting the log view: %v", err)
	}

	if rows != 0 {
		t.Errorf("the log view holds %d entries after a refused write, want nothing stored", rows)
	}
}

// A row the other server writes is not a refusal. Its insert names the columns it knows and never mentions
// this one, so rows with no source exist permanently in a database this server inherited, and the read side
// files them under legacy — which is exactly what an operator filtering for legacy is trying to find.
func TestTheLogViewReadsBackARowWrittenWithoutTheSourceColumnAsLegacy(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "logs")
	logs := NewLogs(db, DialectSqlite)

	// The legacy server's own insert, which does not know the column exists.
	if _, err := db.Exec(`INSERT INTO "logs" ("level", "message", "timestamp") VALUES (?, ?, ?)`,
		"warn", "written by the other server", int64(1760000000000)); err != nil {
		t.Fatalf("the legacy server's insert: %v", err)
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{
		Source: domain.LogSourceLegacy, Limit: domain.DefaultLogSearchLimit, Ascending: true,
	})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 1 || len(page.Results) != 1 {
		t.Fatalf("a search for legacy matched %d entries and returned %d, want the one written without a source", page.Count, len(page.Results))
	}

	for _, entry := range page.Results {
		if entry.Source != domain.LogSourceLegacy {
			t.Errorf("the log view reports source %q for a row stored without one, want %q", entry.Source, domain.LogSourceLegacy)
		}
	}
}

// TestSearchLogsCannotReachAnEntryStoredWithNoInstant is the consequence of writing an entry without
// asking the clock what time it is, which is what the legacy listener-count report does.
//
// The zero time reaches the column as -62135596800000, the instant Go calls year one. Two things then
// happen to that entry in the log view, and both were observed while capturing the wire protocol: the
// search drops it from the results, because the legacy read skips a row whose timestamp is not a real
// instant, while the count it reports still includes it; and the span of the matching entries it does
// show reaches back to year one, which is what makes a deployment's log view say the log begins in 1 AD.
// Those are the rows an operator sees a count for and cannot find.
func TestSearchLogsCannotReachAnEntryStoredWithNoInstant(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "logs"), DialectSqlite)

	unstamped := time.Time{}

	if err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: unstamped,
		Level:    domain.LogLevelInfo,
		Source:   domain.LogSourceLivefeed,
		Message:  "listeners count is 3",
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	if err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Date(2026, 3, 14, 9, 30, 15, 0, time.UTC),
		Level:    domain.LogLevelInfo,
		Source:   domain.LogSourceLivefeed,
		Message:  "listener joined",
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	if got := unstamped.UnixMilli(); got != -62135596800000 {
		t.Fatalf("an entry with no instant stores %d, want the year-one timestamp the deployments have", got)
	}

	// A search for the day the report was written cannot reach it at all: the window is a day, and the
	// entry is not in any day.
	at, _ := time.Parse(time.RFC3339, "2026-03-14T09:00:00Z")

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{
		Date: at, Limit: domain.DefaultLogSearchLimit, Ascending: true,
	})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 1 || len(page.Results) != 1 {
		t.Fatalf("a search for 14 March matched %d entries and returned %d, want only the entry that names its own instant", page.Count, len(page.Results))
	}

	if page.Results[0].Message != "listener joined" {
		t.Errorf("the search for 14 March returned %q, want the stamped entry and not the report", page.Results[0].Message)
	}

	// An undated search counts both entries and shows one. The report is in the log and not on the screen,
	// and the span the log view reports begins in year one.
	page, err = logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 2 {
		t.Errorf("an undated search counts %d entries, want both rows the log table has", page.Count)
	}

	if len(page.Results) != 1 {
		t.Fatalf("an undated search shows %d entries, want the one row with a real instant", len(page.Results))
	}

	if !page.DateStart.Equal(instantOf(-62135596800000)) {
		t.Errorf("the span of everything the criteria match begins at %v, want year one: the log view reports a log that begins in 1 AD", page.DateStart)
	}
}

// TestSearchLogsCountsTheEntriesOfEverySourceTheCriteriaMatch is the per-subsystem count the log view
// offers the operator: where the noise is, before narrowing to it.
//
// The counts describe everything the criteria match and not the page that was returned, because the log
// view pages through 200 entries at a time and the counts have to stay the same while the page moves. And
// the source criterion does not restrict them: asking "how many of each" while already filtered to one
// source answers one row, which is true and useless. So the search reports two totals — the narrowed match
// the page is paged through, and the wider match the counts describe — and they are the same number
// whenever no subsystem filter is submitted.
func TestSearchLogsCountsTheEntriesOfEverySourceTheCriteriaMatch(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "logs"), DialectSqlite)

	store := []domain.LogEntry{
		{DateTime: time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC), Level: domain.LogLevelError, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming stopped"},
		{DateTime: time.Date(2026, 3, 14, 9, 1, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming stopped"},
		{DateTime: time.Date(2026, 3, 14, 9, 2, 0, 0, time.UTC), Level: domain.LogLevelInfo, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming started"},
		{DateTime: time.Date(2026, 3, 14, 9, 3, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceIngest, Message: "the recording could not be ingested"},
		{DateTime: time.Date(2026, 3, 14, 9, 4, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceIngest, Message: "the recording could not be ingested"},
		{DateTime: time.Date(2026, 3, 14, 9, 5, 0, 0, time.UTC), Level: domain.LogLevelInfo, Source: domain.LogSourceConfig, Message: "the configuration section options was saved"},
		{DateTime: time.Date(2026, 3, 14, 9, 6, 0, 0, time.UTC), Level: domain.LogLevelInfo, Source: domain.LogSourceLivefeed, Message: "listeners count is 3"},
	}

	for _, entry := range store {
		if err := logs.AppendLog(context.Background(), entry); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	for _, tc := range []struct {
		what   string
		search domain.LogSearch
		want   []ports.LogSourceCount
		// wantCount is the narrowed match the page is paged through; wantSum is the total of the match
		// the counts describe. They are the same number whenever no subsystem filter is submitted.
		wantCount uint64
		wantSum   uint64
	}{
		{
			"everything, with a page far smaller than the match",
			domain.LogSearch{Limit: 2},
			[]ports.LogSourceCount{
				{Source: domain.LogSourceDirwatch, Count: 3},
				{Source: domain.LogSourceIngest, Count: 2},
				{Source: domain.LogSourceConfig, Count: 1},
				{Source: domain.LogSourceLivefeed, Count: 1},
			},
			7, 7,
		},
		{
			"everything, already narrowed to one subsystem",
			domain.LogSearch{Source: domain.LogSourceDirwatch, Limit: domain.DefaultLogSearchLimit},
			[]ports.LogSourceCount{
				{Source: domain.LogSourceDirwatch, Count: 3},
				{Source: domain.LogSourceIngest, Count: 2},
				{Source: domain.LogSourceConfig, Count: 1},
				{Source: domain.LogSourceLivefeed, Count: 1},
			},
			3, 7,
		},
		{
			"the warnings only",
			domain.LogSearch{Level: domain.LogLevelWarn, Limit: domain.DefaultLogSearchLimit},
			[]ports.LogSourceCount{
				{Source: domain.LogSourceIngest, Count: 2},
				{Source: domain.LogSourceDirwatch, Count: 1},
			},
			3, 3,
		},
		{
			"one subsystem and the text the operator typed",
			domain.LogSearch{Source: domain.LogSourceDirwatch, Text: "started", Limit: domain.DefaultLogSearchLimit},
			[]ports.LogSourceCount{
				{Source: domain.LogSourceDirwatch, Count: 1},
			},
			1, 1,
		},
		{"nothing at all", domain.LogSearch{Text: "no such entry", Limit: domain.DefaultLogSearchLimit}, nil, 0, 0},
	} {
		tc.search.Ascending = true

		page, err := logs.SearchLogs(context.Background(), tc.search)
		if err != nil {
			t.Fatalf("SearchLogs for %s: %v", tc.what, err)
		}

		if len(page.SourceCounts) != len(tc.want) {
			t.Fatalf("%s reports %d source counts %v, want %v", tc.what, len(page.SourceCounts), page.SourceCounts, tc.want)
		}

		var sum uint64

		for i, got := range page.SourceCounts {
			if got != tc.want[i] {
				t.Errorf("%s reports %s %d at position %d, want %s %d", tc.what, got.Source, got.Count, i, tc.want[i].Source, tc.want[i].Count)
			}

			sum += got.Count
		}

		if sum != tc.wantSum {
			t.Errorf("%s reports source counts summing to %d, want the %d entries the counts describe", tc.what, sum, tc.wantSum)
		}

		if page.Count != tc.wantCount {
			t.Errorf("%s reports %d matching entries, want %d", tc.what, page.Count, tc.wantCount)
		}
	}
}

// TestSearchLogsReportsAContextCancelledMidIteration (REQ-011, CA-011.1): a log search
// whose read is cancelled while it is running is a search that did not complete. Today
// the page loop treats the cancelled read as rows that had nothing to show — the scan
// failure is skipped as a damaged row — and the administrator is answered a short page
// with a success, as if the log held exactly that many entries.
func TestSearchLogsReportsAContextCancelledMidIteration(t *testing.T) {
	db := openSchemaDatabase(t)

	defer db.Close()

	// Enough entries that the cancellation lands while the page is being read: two
	// hundred-odd thousand rows take hundreds of milliseconds to scan, the count
	// and the source counts take milliseconds, and the cancel lands in between.
	// The table is filled by doubling — one statement per doubling.
	if _, err := db.Exec(`INSERT INTO "logs" ("level", "source", "message", "timestamp") VALUES ('info', 'server', 'entry under the cancelled search', 1791288000000)`); err != nil {
		t.Fatalf("seeding the log entries: %v", err)
	}

	for i := 0; i < 18; i++ {
		if _, err := db.Exec(`INSERT INTO "logs" ("level", "source", "message", "timestamp") SELECT "level", "source", "message", "timestamp" + 1 FROM "logs"`); err != nil {
			t.Fatalf("doubling the log entries: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		// The search counts the entries before it reads the page: the cancellation
		// has to land while the page is being read, not on the count. The count of
		// this table takes milliseconds; reading the twenty thousand entries the page
		// was asked for takes hundreds — the cancel lands in between, mid-iteration.
		time.Sleep(50 * time.Millisecond)

		cancel()
	}()

	logs := NewLogs(db, DialectSqlite)

	if _, err := logs.SearchLogs(ctx, domain.LogSearch{Limit: 200000, Ascending: true}); err == nil {
		t.Fatal("SearchLogs = nil for a search cancelled mid-iteration, want the failure of the read it did not finish")
	}
}
