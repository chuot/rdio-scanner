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
	"rdio-scanner/internal/domain"
	"testing"
	"time"
)

// The status view is a page an operator leaves open: the administration interface reads it again every
// few seconds while the operator is looking at it, and stops when they are not. What one read costs is
// therefore not one operator's cost — it is that cost multiplied by every open administration page on
// the server, all day.
//
// These are the statements the read costs against the history, counted by the same driver wrapper the
// package counts a save's round trips with. The live block of the same read costs nothing here at all:
// it reads counters the server already holds, which is why it is not in this test.

// TestStatusReadCostsOneStatementPerTally names the whole read: two headline counts, the 24 hours, the
// busiest talkgroups, the newest calls, and the log tally. Six tallies, six statements, whatever the
// history holds.
func TestStatusReadCostsOneStatementPerTally(t *testing.T) {
	db, counted := openCountedSchemaDatabase(t)

	fixture := &callsFixture{
		db:         db,
		calls:      NewCalls(db, DialectSqlite),
		systems:    map[uint]uint64{},
		talkgroups: map[uint]uint64{},
	}

	fixture.storeSystem(t, 10001, 1514000, 1514001, 1514002)

	const window = 24 * time.Hour

	base := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)

	// A history with something in every hour of the window and on more than one talkgroup, so no tally
	// here is measuring an empty table.
	for hour := 0; hour < 24; hour++ {
		at := base.Add(-time.Duration(hour) * time.Hour)

		for n := 0; n <= hour%3; n++ {
			fixture.storeCall(t, 10001, []uint{1514000, 1514001, 1514002}[n], at.Add(time.Duration(n)*time.Minute))
		}
	}

	// One call still waiting out its hold. Every tally here is built on the join that leaves held calls
	// out of the history, and the read costs the same statements whether or not a hold is in the way.
	held := fixture.storeCall(t, 10001, 1514000, base.Add(10*time.Minute))

	fixture.delay(t, held.ID, base.Add(90*time.Minute))

	logs := NewLogs(db, DialectSqlite)

	appendLogEntry(t, logs, base, domain.LogLevelError, domain.LogSourceDirwatch)
	appendLogEntry(t, logs, base.Add(time.Minute), domain.LogLevelWarn, domain.LogSourceDownstream)

	counted.reset()

	ctx := context.Background()

	if _, err := fixture.calls.CountCallsSince(ctx, time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location())); err != nil {
		t.Fatalf("CountCallsSince (the day): %v", err)
	}

	if _, err := fixture.calls.CountCallsSince(ctx, base.Add(-time.Hour)); err != nil {
		t.Fatalf("CountCallsSince (the last hour): %v", err)
	}

	if _, err := fixture.calls.CountCallsByHour(ctx, base.Add(-window)); err != nil {
		t.Fatalf("CountCallsByHour: %v", err)
	}

	if _, err := fixture.calls.CountCallsByTalkgroup(ctx, base.Add(-window), 5); err != nil {
		t.Fatalf("CountCallsByTalkgroup: %v", err)
	}

	if _, err := fixture.calls.RecentCalls(ctx, 10); err != nil {
		t.Fatalf("RecentCalls: %v", err)
	}

	if _, err := logs.TallyLogLevels(ctx, base.Add(-window)); err != nil {
		t.Fatalf("TallyLogLevels: %v", err)
	}

	// The whole read, and each tally named on its own so that a read which grows says which one grew
	// rather than leaving an operator to find it in a diff.
	if total := counted.total(); total != 6 {
		t.Errorf("one status read costs %d statements against the history, want 6: the read answers six tallies, and a page an operator leaves open must not cost more round trips than the figures it answers with", total)
	}

	for _, want := range []struct {
		what   string
		prefix string
		wanted int
	}{
		{"the two headline counts", `SELECT COUNT(*) FROM "calls" AS c`, 2},
		{"the 24 hours", `SELECT ` + hourBucketExpression(DialectSqlite, `c."timestamp"`) + ` AS "bucket"`, 1},
		{"the busiest talkgroups", `SELECT s."systemRef"`, 1},
		{"the newest calls", `SELECT c."callId"`, 1},
		{"the log tally", `SELECT "level", "source"`, 1},
	} {
		if n := counted.count(want.prefix); n != want.wanted {
			t.Errorf("the status read ran %d statements for %s, wanting %d; the statement began %s", n, want.what, want.wanted, want.prefix)
		}
	}
}

// TestStatusReadCostsTheSameStatementsOnAnEmptyHistory is the same read against a server that has
// recorded nothing: the shape of the read does not depend on how much the history holds, so a quiet
// deployment is not cheaper in a way that hides what a busy one costs.
func TestStatusReadCostsTheSameStatementsOnAnEmptyHistory(t *testing.T) {
	db, counted := openCountedSchemaDatabase(t)

	fixture := &callsFixture{
		db:         db,
		calls:      NewCalls(db, DialectSqlite),
		systems:    map[uint]uint64{},
		talkgroups: map[uint]uint64{},
	}

	fixture.storeSystem(t, 10001, 1514000)

	base := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)

	counted.reset()

	ctx := context.Background()

	if _, err := fixture.calls.CountCallsSince(ctx, base.Add(-24*time.Hour)); err != nil {
		t.Fatalf("CountCallsSince: %v", err)
	}

	if _, err := fixture.calls.CountCallsSince(ctx, base.Add(-time.Hour)); err != nil {
		t.Fatalf("CountCallsSince: %v", err)
	}

	if _, err := fixture.calls.CountCallsByHour(ctx, base.Add(-24*time.Hour)); err != nil {
		t.Fatalf("CountCallsByHour: %v", err)
	}

	if _, err := fixture.calls.CountCallsByTalkgroup(ctx, base.Add(-24*time.Hour), 5); err != nil {
		t.Fatalf("CountCallsByTalkgroup: %v", err)
	}

	if _, err := fixture.calls.RecentCalls(ctx, 10); err != nil {
		t.Fatalf("RecentCalls: %v", err)
	}

	if _, err := NewLogs(db, DialectSqlite).TallyLogLevels(ctx, base.Add(-24*time.Hour)); err != nil {
		t.Fatalf("TallyLogLevels: %v", err)
	}

	if total := counted.total(); total != 6 {
		t.Errorf("one status read against an empty history costs %d statements, wanting the same 6 a full history costs", total)
	}
}
