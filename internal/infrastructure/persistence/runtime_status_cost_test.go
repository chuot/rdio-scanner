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

// The status view is a page an operator leaves open, so what one read costs is multiplied
// by every open administration page on the server. These statements are counted by the same
// driver wrapper the package counts a save's round trips with; the live block costs nothing
// here because it reads counters the server already holds.

// The whole status read costs one statement per tally — two headline counts, the day
// window, the busiest talkgroups, the newest calls, and the log tally: six tallies, six
// statements, whatever the history holds.
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

	// A history with something in every hour of the window and on more than one
	// talkgroup, so no tally measures an empty table.
	for hour := 0; hour < 24; hour++ {
		at := base.Add(-time.Duration(hour) * time.Hour)

		for n := 0; n <= hour%3; n++ {
			fixture.storeCall(t, 10001, []uint{1514000, 1514001, 1514002}[n], at.Add(time.Duration(n)*time.Minute))
		}
	}

	// One call still waiting out its hold: every tally is built on the join that leaves
	// held calls out, and the read costs the same statements either way.
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

	// The whole read, and each tally named on its own: a read that grows says which
	// tally grew.
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

// The same read against a server that has recorded nothing costs the same statements: the
// shape of the read does not depend on how much the history holds.
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
