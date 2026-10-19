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

// The status read is the one thing the administration page asks for, and it asks every few seconds for as
// long as an operator is looking at it. What one read costs is therefore not one operator's cost: it is
// that cost multiplied by every administration page open on the server, all day. These time the read as
// the server composes it — the same use case, the same persistence adapters, the same live-state answer
// the composition root builds — against a database holding a month of calls and log lines at the size a
// working deployment holds.
//
// They are run on purpose, and not by the test suite:
//
//	go test ./internal/app/ -bench 'StatusRead' -benchtime=200x -count=3

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

// BenchmarkTenConcurrentStatusReads times ten reads made at the same moment, which is what ten
// administration pages open across a shift do to the server when their polls land together. One benchmark
// iteration is one such moment, so the figure per read is the reported figure over ten.
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

// BenchmarkOneStatusReadOnAFreshDatabase times the same read against a database that has recorded nothing
// yet: the floor the same benchmark measures against a deployment-sized history is compared with. It is
// the same composition, the same statements, and no seeded rows.
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

// BenchmarkStatusReadOneStatement times each of the read's statements on its own, against the same seeded
// database, so that a read which costs more than it should says which of its tallies costs it rather than
// leaving that to be found in a diff.
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

// statusReadAgainstASeededServer starts a server on a directory of its own, puts a month of calls and log
// lines on the database it opened, and hands back the status read exactly as the server's own wiring
// builds it. The history is seeded after the start, so that none of the server's own startup work is
// inside the timed loop.
func statusReadAgainstASeededServer(b *testing.B) *application.GetRuntimeStatusHandler {
	b.Helper()

	srv := startedTestServerIn(b, b.TempDir())

	seedStatusReadHistory(b, srv.db)

	calls := persistence.NewCalls(srv.db, srv.dialect)
	logs := persistence.NewLogs(srv.db, srv.dialect)

	return application.NewGetRuntimeStatusHandler(srv.runtime, calls, logs, srv.clock)
}

// The history the read is timed against. The row counts are the ones a working deployment holds — read out
// of one before these lines were written — spread over the 30 days such a deployment records, so that the
// read is timed against a table that has grown rather than one that has just been made. The 24 hours the
// read counts are inside that month, and the newest calls it names are the newest of the month.
const (
	statusReadHistoryCalls = 57_000
	statusReadHistoryLogs  = 58_000

	// The rows go in in chunks of a few hundred. One statement per row would spend the setup making a
	// commit — and a disk sync — for every call, which is a cost of the benchmark rather than of the read.
	statusReadHistoryChunk = 400
)

// The talkgroups the month is recorded on, in the order they are stored: four on the first system and two
// on the second, with the identifiers the calls name out.
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

	// The history is laid out backwards from the moment the benchmark runs, because the read counts the
	// 24 hours it is made in: a month recorded in May would sit outside every window the read asks about,
	// and the benchmark would be timing a scan that finds nothing.
	base := time.Now().UTC()

	// The tag is the one a fresh server puts on record itself when it finds a database with no tags in
	// it: the benchmark uses the deployment's own rather than adding a second one beside it.
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

	// A call every 45 seconds, which is a month of them at a working deployment's rate, landing on the
	// talkgroups in rotation so that no one talkgroup is the whole ranking, and on the system its
	// talkgroup is on.
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

	// The log lines a month of running writes: mostly info, with a warning and an error every twenty-fifth
	// line under the subsystems that have a reason to complain, which is what the log tally counts.
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

// chunkedStatements turns one row-per-number shape into statements that each carry a chunk of the rows:
// the whole history on record, in a number of round trips a setup can afford.
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
