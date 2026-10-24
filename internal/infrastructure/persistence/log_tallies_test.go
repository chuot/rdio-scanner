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
	"testing"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The tally the status view's log-health card is built from, kept apart from the log view's
// search: the search pages and filters entries for an operator reading them, and this counts
// what the server complained about for an operator watching a page.

func TestTallyLogLevelsKeepsTheLevelsApartAndGroupsThemBySubsystem(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	for _, entry := range []struct {
		offset time.Duration
		level  string
		source domain.LogSource
	}{
		{0 * time.Minute, domain.LogLevelError, domain.LogSourceDirwatch},
		{1 * time.Minute, domain.LogLevelError, domain.LogSourceDirwatch},
		{2 * time.Minute, domain.LogLevelError, domain.LogSourceFFmpeg},
		{3 * time.Minute, domain.LogLevelWarn, domain.LogSourceDirwatch},
		{4 * time.Minute, domain.LogLevelWarn, domain.LogSourceDownstream},
		{5 * time.Minute, domain.LogLevelWarn, domain.LogSourceDownstream},
	} {
		appendLogEntry(t, logs, base.Add(entry.offset), entry.level, entry.source)
	}

	tallies, err := logs.TallyLogLevels(context.Background(), base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("TallyLogLevels: %v", err)
	}

	got := tallyIndex(tallies)

	for _, want := range []struct {
		level  string
		source domain.LogSource
		count  uint64
	}{
		{domain.LogLevelError, domain.LogSourceDirwatch, 2},
		{domain.LogLevelError, domain.LogSourceFFmpeg, 1},
		{domain.LogLevelWarn, domain.LogSourceDirwatch, 1},
		{domain.LogLevelWarn, domain.LogSourceDownstream, 2},
	} {
		key := want.level + "/" + string(want.source)

		if got[key] != want.count {
			t.Errorf("the tally for %s is %d, want %d", key, got[key], want.count)
		}
	}

	// A subsystem that wrote at two levels is two tallies and not one merged total: the view
	// shows the errors and the warnings as the two different things they are.
	if len(tallies) != 4 {
		t.Errorf("TallyLogLevels returned %d tallies, want the 4 the four level-and-subsystem pairs make: %v", len(tallies), tallies)
	}

	dirwatch := 0

	for _, tally := range tallies {
		if tally.Source == domain.LogSourceDirwatch {
			dirwatch++
		}
	}

	if dirwatch != 2 {
		t.Errorf("%s appears in %d tallies, want 2: its error entries and its warning entry are not one total",
			domain.LogSourceDirwatch, dirwatch)
	}
}

// The entries that complain about nothing are never tallied: an hour of ordinary notices is
// not a reason to say a subsystem needs attention.
func TestTallyLogLevelsNeverTalliesTheEntriesThatComplainAboutNothing(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	for i := range 9 {
		appendLogEntry(t, logs, base.Add(time.Duration(i)*time.Minute), domain.LogLevelInfo, domain.LogSourceServer)
	}

	appendLogEntry(t, logs, base.Add(10*time.Minute), domain.LogLevelWarn, domain.LogSourcePrune)

	tallies, err := logs.TallyLogLevels(context.Background(), base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("TallyLogLevels: %v", err)
	}

	if len(tallies) != 1 {
		t.Fatalf("TallyLogLevels returned %d tallies, want the 1 warning: %v", len(tallies), tallies)
	}

	if tallies[0].Level != domain.LogLevelWarn || tallies[0].Source != domain.LogSourcePrune {
		t.Errorf("the tally is %s from %s, want the single warning written by %s",
			tallies[0].Level, tallies[0].Source, domain.LogSourcePrune)
	}
}

// An absent subsystem is a subsystem that had nothing to complain about, and listing all
// sixteen of them with a zero each would bury the one that did.
func TestTallyLogLevelsLeavesOutASubsystemThatWroteNeither(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	appendLogEntry(t, logs, base, domain.LogLevelError, domain.LogSourceDatabase)

	tallies, err := logs.TallyLogLevels(context.Background(), base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("TallyLogLevels: %v", err)
	}

	if len(tallies) != 1 {
		t.Fatalf("TallyLogLevels returned %d tallies, want 1: %v", len(tallies), tallies)
	}

	for _, source := range domain.KnownLogSources() {
		if source == domain.LogSourceDatabase {
			continue
		}

		for _, tally := range tallies {
			if tally.Source == source {
				t.Errorf("%s is reported with %d entries, when it wrote nothing", source, tally.Count)
			}
		}
	}
}

// TestTallyLogLevelsWithNothingInTheWindowAnswersEmpty: a day without a complaint is the good case,
// and it is reported as nothing and not as a failure.
func TestTallyLogLevelsWithNothingInTheWindowAnswersEmpty(t *testing.T) {
	logs := NewLogs(openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options"), DialectSqlite)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	appendLogEntry(t, logs, base.Add(-40*time.Hour), domain.LogLevelError, domain.LogSourceDirwatch)

	tallies, err := logs.TallyLogLevels(context.Background(), base.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("TallyLogLevels: %v", err)
	}

	if len(tallies) != 0 {
		t.Errorf("TallyLogLevels returned %d tallies, want none for a window the log has nothing in", len(tallies))
	}
}

// A row written without a subsystem is tallied as the log view reports it, under `legacy`: the
// same entry counted one way on the status page and another way on the log page is two numbers
// the operator cannot reconcile.
func TestTallyLogLevelsReportsARowWrittenWithoutASubsystemAsTheLogViewReportsIt(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "accesses", "apikeys", "logs", "options")
	logs := NewLogs(db, DialectSqlite)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	// The two spellings a nameless row carries: the blank another server's insert leaves, and
	// the column the row never had at all.
	if _, err := db.Exec(`INSERT INTO "logs" ("level", "source", "message", "timestamp") VALUES (?, ?, ?, ?)`,
		string(domain.LogLevelError), "", "a row written without a subsystem", base.UnixMilli()); err != nil {
		t.Fatalf("storing the blank-sourced entry: %v", err)
	}

	var source any = sql.NullString{}

	if _, err := db.Exec(`INSERT INTO "logs" ("level", "message", "timestamp") VALUES (?, ?, ?)`,
		string(domain.LogLevelError), "a row whose subsystem column is missing", base.Add(time.Minute).UnixMilli()); err != nil {
		t.Fatalf("storing the sourceless entry: %v", err)
	}

	_ = source

	tallies, err := logs.TallyLogLevels(context.Background(), base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("TallyLogLevels: %v", err)
	}

	if len(tallies) != 1 {
		t.Fatalf("TallyLogLevels returned %d tallies, want the 1 the two nameless rows make together: %v", len(tallies), tallies)
	}

	if tallies[0].Source != domain.LogSourceLegacy {
		t.Errorf("the nameless rows are tallied under %q, want %q, which is how the log view files them",
			tallies[0].Source, domain.LogSourceLegacy)
	}

	if tallies[0].Count != 2 {
		t.Errorf("the %q tally carries %d entries, want 2", domain.LogSourceLegacy, tallies[0].Count)
	}
}

func appendLogEntry(t *testing.T, logs *Logs, at time.Time, level string, source domain.LogSource) {
	t.Helper()

	if err := logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: at,
		Level:    level,
		Source:   source,
		Message:  "harness entry",
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
}

// tallyIndex keys the tallies by level and subsystem, so a test can name the one it means instead of
// depending on the order the grouping happens to come back in.
func tallyIndex(tallies []ports.LogLevelTally) map[string]uint64 {
	index := map[string]uint64{}

	for _, tally := range tallies {
		index[string(tally.Level)+"/"+string(tally.Source)] = tally.Count
	}

	return index
}
