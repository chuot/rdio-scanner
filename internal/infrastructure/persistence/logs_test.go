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
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// A message containing single quotes stores: the legacy insert interpolated the
// message raw, so every line naming a failed statement produced broken SQL and the
// error was discarded.
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

// The same quote fix over a database the legacy server wrote.
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

// The entry's own instant reads back as Unix milliseconds.
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

// An explicit zero limit selects no rows; the count is still the whole match.
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

// The ascending date window: the submitted instant truncated to the minute, running
// forward a full day.
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

// The descending date window: a day back, then back again by the submitted hour and
// minute.
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

// Legacy read rule kept: a row with no identity, no level, no message, or an
// unrealistic timestamp is dropped from the results while the count still includes it.
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

// The span is the earliest and latest instants of everything the criteria match, not
// the ends of the page; the legacy reads it ascending then descending before counting or
// paging, and the same three questions come in the same order here.
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

	// The ends of the span are not the ends of the returned page.
	if first.DateStart.Equal(first.DateStop) {
		t.Errorf("the span of five entries is a single instant %v, want the ends of everything the criteria match", first.DateStart)
	}
}

// The span belongs to the filtered set: a level filter narrows it.
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

// With nothing to report both span ends stay at the zero time, not the Unix epoch.
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

// Searching by the source column and by a substring of the message.
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

// A source filter returns everything one subsystem said and nothing another one said.
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

// A maintenance run's removals are the record a deployment cleared months ago is explained from, so the
// subsystem they are filed under has to reach the log view the way every other subsystem's entries do:
// stored with that subsystem, returned by a filter for it, and counted beside the other subsystems.
func TestAppendLogStoresAMaintenanceEntryAndTheLogSearchFindsIt(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "logs"), DialectSqlite)

	store := []domain.LogEntry{
		{DateTime: time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceMaintenance, Message: `removed unit "6789" carrying the radio number 6789, of system 101`},
		{DateTime: time.Date(2026, 3, 14, 9, 1, 0, 0, time.UTC), Level: domain.LogLevelWarn, Source: domain.LogSourceMaintenance, Message: "dropped the table rdioScannerCalls, which held 8 rows"},
		{DateTime: time.Date(2026, 3, 14, 9, 2, 0, 0, time.UTC), Level: domain.LogLevelInfo, Source: domain.LogSourceDatabase, Message: "migrating the database from version 6.6.3"},
	}

	for _, entry := range store {
		if err := logs.AppendLog(context.Background(), entry); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{
		Source: domain.LogSourceMaintenance, Limit: domain.DefaultLogSearchLimit, Ascending: true,
	})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 2 || len(page.Results) != 2 {
		t.Fatalf("a search for %s matched %d entries and returned %d, want the 2 entries the run filed: %v",
			domain.LogSourceMaintenance, page.Count, len(page.Results), page.Results)
	}

	for _, entry := range page.Results {
		if entry.Source != domain.LogSourceMaintenance {
			t.Errorf("the log view returned an entry with source %q under a %s filter", entry.Source, domain.LogSourceMaintenance)
		}

		if entry.Level != domain.LogLevelWarn {
			t.Errorf("the entry %q carries level %q, want it stored at the warn level", entry.Message, entry.Level)
		}
	}

	countsBySource := map[domain.LogSource]uint64{}

	for _, count := range page.SourceCounts {
		countsBySource[count.Source] = count.Count
	}

	if countsBySource[domain.LogSourceMaintenance] != 2 {
		t.Errorf("the log view counts %d %s entries, want the 2 the run filed: %v",
			countsBySource[domain.LogSourceMaintenance], domain.LogSourceMaintenance, page.SourceCounts)
	}
}

// A text search matches a substring of the message, case-insensitively, and the % and _
// of the submitted text match themselves.
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

// Combined criteria: subsystem, day, level, text. Each row drops one criterion from the
// row above it, so a criterion that silently matched nothing shows as a count that did not move.
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

	// The ascending window runs forward a full day from the submitted instant.
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

// A source that is a name this server does not have is refused at the write boundary.
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

// An entry naming no source at all is refused at the write.
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

// A row the other server writes names no source; the read side files it under legacy.
func TestTheLogViewReadsBackARowWrittenWithoutTheSourceColumnAsLegacy(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "logs")
	logs := NewLogs(db, DialectSqlite)

	// The legacy server's insert, which does not know the column exists.
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

// An entry written without asking the clock stores the zero time as -62135596800000, the
// instant Go calls year one: the search drops it from the results while the count still
// includes it, and the span of the entries it does show reaches back to year one.
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

	// A day-window search cannot reach it at all: the entry is not in any day.
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

	// An undated search counts both entries and shows one, and the span begins in
	// year one.
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

// The per-source counts describe everything the criteria match and not the page, and the
// source criterion does not restrict them; the search reports the narrowed total and the
// wider total, the same number whenever no subsystem filter is submitted.
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
		// wantCount is the narrowed match; wantSum is the total of the wider match the
		// counts describe.
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

// A log search whose read is cancelled mid-iteration is a search that did not
// complete, not a short page answered as a success.
func TestSearchLogsReportsAContextCancelledMidIteration(t *testing.T) {
	db := openSchemaDatabase(t)

	defer db.Close()

	// Enough entries that the cancellation lands while the page is being read;
	// the table is filled by doubling, one statement per doubling.
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
		// The search counts before it reads the page, so the cancellation lands
		// mid-iteration and not on the count.
		time.Sleep(50 * time.Millisecond)

		cancel()
	}()

	logs := NewLogs(db, DialectSqlite)

	if _, err := logs.SearchLogs(ctx, domain.LogSearch{Limit: 200000, Ascending: true}); err == nil {
		t.Fatal("SearchLogs = nil for a search cancelled mid-iteration, want the failure of the read it did not finish")
	}
}
