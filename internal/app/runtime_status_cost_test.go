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

package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/infrastructure/persistence"
)

// The status read is what every open administration page polls every few seconds, so its
// cost is that cost times every page open all day. These time the read, its real use case,
// adapters, and live-state answer, against a month of calls and log lines at deployment
// size. They run by hand, not with the suite.

// BenchmarkOneStatusRead times a single read: what one administration page pays for one of its reads.
func BenchmarkOneStatusRead(b *testing.B) {
	handler := statusReadAgainstASeededServer(b)

	ctx := context.Background()

	b.ReportAllocs()

	for range b.N {
		if _, err := handler.Handle(ctx, application.GetRuntimeStatusQuery{}); err != nil {
			b.Fatalf("status read: %v", err)
		}
	}
}

// BenchmarkTenConcurrentStatusReads times ten reads made at the same moment, what ten open
// administration pages do when their polls land together; the figure per read is the reported
func BenchmarkTenConcurrentStatusReads(b *testing.B) {
	handler := statusReadAgainstASeededServer(b)

	ctx := context.Background()

	b.ReportAllocs()

	for range b.N {
		var moment sync.WaitGroup

		for range 10 {
			moment.Add(1)

			go func() {
				defer moment.Done()

				if _, err := handler.Handle(ctx, application.GetRuntimeStatusQuery{}); err != nil {
					b.Errorf("status read: %v", err)
				}
			}()
		}

		moment.Wait()
	}
}

// BenchmarkOneStatusReadOnAFreshDatabase times the same read against a recorded-empty
// database, the floor the deployment-sized history figure is compared with.
func BenchmarkOneStatusReadOnAFreshDatabase(b *testing.B) {
	srv := startedTestServerIn(b, b.TempDir())

	handler := application.NewGetRuntimeStatusHandler(
		srv.runtime,
		persistence.NewCalls(srv.db, srv.dialect),
		persistence.NewLogs(srv.db, srv.dialect),
		srv.clock,
	)

	ctx := context.Background()

	b.ReportAllocs()

	for range b.N {
		if _, err := handler.Handle(ctx, application.GetRuntimeStatusQuery{}); err != nil {
			b.Fatalf("status read: %v", err)
		}
	}
}

// BenchmarkStatusReadOneStatement times each of the read's statements on its own, so an
// expensive read names the tally that costs it.
func BenchmarkStatusReadOneStatement(b *testing.B) {
	srv := startedTestServerIn(b, b.TempDir())

	seedStatusReadHistory(b, srv.db)

	calls := persistence.NewCalls(srv.db, srv.dialect)
	logs := persistence.NewLogs(srv.db, srv.dialect)

	ctx := context.Background()

	now := time.Now().UTC()

	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	windowStart := now.Truncate(time.Hour).Add(-23 * time.Hour)

	b.ReportAllocs()

	b.Run("the calls today", func(b *testing.B) {
		for range b.N {
			if _, err := calls.CountCallsSince(ctx, dayStart); err != nil {
				b.Fatalf("CountCallsSince: %v", err)
			}
		}
	})

	b.Run("the calls in the last hour", func(b *testing.B) {
		for range b.N {
			if _, err := calls.CountCallsSince(ctx, now.Add(-time.Hour)); err != nil {
				b.Fatalf("CountCallsSince: %v", err)
			}
		}
	})

	b.Run("the 24 hours", func(b *testing.B) {
		for range b.N {
			if _, err := calls.CountCallsByHour(ctx, windowStart); err != nil {
				b.Fatalf("CountCallsByHour: %v", err)
			}
		}
	})

	b.Run("the busiest talkgroups", func(b *testing.B) {
		for range b.N {
			if _, err := calls.CountCallsByTalkgroup(ctx, windowStart, 5); err != nil {
				b.Fatalf("CountCallsByTalkgroup: %v", err)
			}
		}
	})

	b.Run("the newest calls", func(b *testing.B) {
		for range b.N {
			if _, err := calls.RecentCalls(ctx, 10); err != nil {
				b.Fatalf("RecentCalls: %v", err)
			}
		}
	})

	b.Run("the log tally", func(b *testing.B) {
		for range b.N {
			if _, err := logs.TallyLogLevels(ctx, windowStart); err != nil {
				b.Fatalf("TallyLogLevels: %v", err)
			}
		}
	})
}

// statusReadAgainstASeededServer starts a server on its own directory, seeds a month of calls
// and log lines after the start, and hands back the status read exactly as the server's own
// wiring builds it.
func statusReadAgainstASeededServer(b *testing.B) *application.GetRuntimeStatusHandler {
	b.Helper()

	srv := startedTestServerIn(b, b.TempDir())

	seedStatusReadHistory(b, srv.db)

	calls := persistence.NewCalls(srv.db, srv.dialect)
	logs := persistence.NewLogs(srv.db, srv.dialect)

	return application.NewGetRuntimeStatusHandler(srv.runtime, calls, logs, srv.clock)
}

// The history the read is timed against: row counts read off a working deployment, spread
// over the 30 days it records, so the read is timed against a grown table rather than a new
// one. The day the read counts sits inside that month.
const (
	statusReadHistoryCalls = 57_000
	statusReadHistoryLogs  = 58_000

	// Rows go in in chunks of a few hundred: one statement per row would make the setup pay a
	// commit per call, a cost of the benchmark rather than of the read.
	statusReadHistoryChunk = 400
)

// The talkgroups the month is recorded on, in storage order: four on the first system, two
var statusReadHistoryTalkgroups = []struct {
	talkgroupID uint64
	systemID    uint64
	systemRef   uint
}{
	{201, 101, 1514000},
	{202, 101, 1514001},
	{203, 101, 1514002},
	{204, 101, 1514003},
	{205, 102, 1514100},
	{206, 102, 1514101},
}

func seedStatusReadHistory(b *testing.B, db *sql.DB) {
	b.Helper()

	// The history is laid out backwards from when the benchmark runs, because the read counts
	// only the day before it runs.
	base := time.Now().UTC()

	// The tag is one a fresh server writes itself into a tagless database:
	// the benchmark uses the deployment's own, not a second one beside it.
	seed(b, db,
		seedStatement{query: `INSERT INTO "systems" ("label", "systemId", "systemRef") VALUES ('Harbour Dispatch', 101, 10001)`},
		seedStatement{query: `INSERT INTO "systems" ("label", "systemId", "systemRef") VALUES ('County Fire', 102, 10002)`},
	)

	talkgroups := make([]string, 0, len(statusReadHistoryTalkgroups))

	for n, talkgroup := range statusReadHistoryTalkgroups {
		talkgroups = append(talkgroups, fmt.Sprintf(
			`('Dispatch %d', 'Dispatch %d', %d, %d, 1, %d, %d)`,
			n+1, n+1, n, talkgroup.systemID, talkgroup.talkgroupID, talkgroup.systemRef,
		))
	}

	seed(b, db, seedStatement{
		query: `INSERT INTO "talkgroups" ("label", "name", "order", "systemId", "tagId", "talkgroupId", "talkgroupRef") VALUES ` +
			strings.Join(talkgroups, ", "),
	})

	// A call every 45 seconds, a month of them at a working deployment's rate, landing on the
	// talkgroups in rotation and on the system each talkgroup belongs to.
	seed(b, db, chunkedStatements(
		`INSERT INTO "calls" ("audio", "audioFilename", "audioLength", "audioMime", "systemId", "talkgroupId", "timestamp") VALUES `,
		statusReadHistoryCalls,
		func(n int) string {
			at := base.Add(-time.Duration(n) * 45 * time.Second)

			talkgroup := statusReadHistoryTalkgroups[n%len(statusReadHistoryTalkgroups)]

			return fmt.Sprintf(
				`(X'524946462d617564696f', 'call.wav', %d, 'audio/wav', %d, %d, %d)`,
				2_000_000_000+n, talkgroup.systemID, talkgroup.talkgroupID, at.UnixMilli(),
			)
		},
	)...,
	)

	// The log lines a month of running writes: mostly info, a warning and an error every
	// twenty-fifth line under the complaining subsystems, which is what the log tally counts.
	sources := []string{"dirwatch", "downstream", "delayer", "ffmpeg", "access", "prune", "server"}

	seed(b, db, chunkedStatements(
		`INSERT INTO "logs" ("level", "message", "source", "timestamp") VALUES `,
		statusReadHistoryLogs,
		func(n int) string {
			at := base.Add(-time.Duration(n) * 45 * time.Second)

			level := "info"

			switch n % 25 {
			case 3:
				level = "warn"
			case 11:
				level = "error"
			}

			source := sources[n%len(sources)]

			return fmt.Sprintf(
				`('%s', 'the %s subsystem has something to say', '%s', %d)`,
				level, source, source, at.UnixMilli(),
			)
		},
	)...,
	)
}

// chunkedStatements turns one row-per-number shape into statements each carrying a chunk of
// the rows: the whole history in a number of round trips a setup can afford.
func chunkedStatements(prefix string, total int, row func(int) string) []seedStatement {
	statements := []seedStatement{}

	for start := 0; start < total; start += statusReadHistoryChunk {
		end := min(start+statusReadHistoryChunk, total)

		rows := make([]string, 0, end-start)

		for n := start; n < end; n++ {
			rows = append(rows, row(n))
		}

		statements = append(statements, seedStatement{query: prefix + strings.Join(rows, ", ")})
	}

	return statements
}
