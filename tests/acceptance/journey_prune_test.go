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

package acceptance

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"rdio-scanner/internal/application"
)

// the retention prune journey pins the pruning use case and the hourly schedule
// together, on history a running server wrote itself, which neither pinning alone shows.

// pruneKey is the credential these tests post a recording with while testing what becomes of it.
const pruneKey = "a-key-that-ages-out"

// TestTheServerDiscardsTheHistoryOlderThanTheRetentionItIsConfiguredWith posts two
// recordings, one outside the retention and one inside, and the server's own schedule
// keeps only the second. The stored retention is three days, not the default, so the
// journey proves the configuration's number is the pruning's number.
func TestTheServerDiscardsTheHistoryOlderThanTheRetentionItIsConfiguredWith(t *testing.T) {
	srv := journeyServerIn(t, t.TempDir())

	seedUploadTarget(t, srv.db, 0, pruneKey)
	seedRetention(t, srv.db, 3)
	seedOldLogEntries(t, srv.db, 3)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// the upload endpoint names the moment a call happened, which is how a recording five
	// days old gets made without a time machine.
	older := time.Now().Add(-5 * 24 * time.Hour).Truncate(time.Millisecond)
	recent := time.Now().Add(-2 * 24 * time.Hour).Truncate(time.Millisecond)

	for _, at := range []time.Time{older, recent} {
		answered := srv.uploadRecording(t, uploadedRecording{
			key:       pruneKey,
			systemRef: 10001,
			talkgroup: 1514000,
			filename:  "retained.wav",
			audio:     recording(2048),
			at:        at,
		})

		if answered.Code != http.StatusOK {
			t.Fatalf("the recording from %s was answered %d: %s", at, answered.Code, answered.Body.String())
		}
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "calls"`); rows != 2 {
		t.Fatalf("the server holds %d calls before the pruning, want the two it accepted", rows)
	}

	result, err := srv.pruneHandler(t).Handle(context.Background(), application.PruneOldCallsCommand{})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.Outcome != application.PruneOldCallsDone {
		t.Fatalf("the pruning reported %q, want it done", result.Outcome)
	}

	if result.CallsPruned != 1 {
		t.Errorf("the pruning discarded %d calls, want the one recorded five days ago", result.CallsPruned)
	}

	if result.LogsPruned != 3 {
		t.Errorf("the pruning discarded %d log entries, want the three written before the retention", result.LogsPruned)
	}

	// what is left is the in-retention recording, whole: the pruning discards entire
	// calls rather than trimming anything.
	if rows := countRows(t, srv.db, fmt.Sprintf(`SELECT COUNT(*) FROM "calls" WHERE "timestamp" < %d`, older.Add(time.Second).UnixMilli())); rows != 0 {
		t.Errorf("the server still holds %d calls older than the retention, want none", rows)
	}

	if rows := countRows(t, srv.db, fmt.Sprintf(`SELECT COUNT(*) FROM "calls" WHERE "timestamp" = %d`, recent.UnixMilli())); rows != 1 {
		t.Errorf("the server holds %d calls from inside the retention, want the one it accepted two days ago", rows)
	}

	// log entries older than the retention are gone, today's are not, including the
	// pruning's own report, stamped with the current instant so it never ages itself out.
	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs"`); rows == 0 {
		t.Fatalf("the server's log view is empty after the pruning, want the entries the server wrote today")
	}

	if !hasLogMessage(t, srv.db, "the retention pass removed 1 call and 3 log entries") {
		t.Errorf("the log view holds no report of what the retention pass removed, want the counts it was given")
	}

	if got := logSourceOfMessage(t, srv.db, "the retention pass removed 1 call and 3 log entries"); got != "prune" {
		t.Errorf("the retention report is filed under %q, want it filed with the retention pass", got)
	}

	if rows := countRows(t, srv.db, fmt.Sprintf(`SELECT COUNT(*) FROM "logs" WHERE "timestamp" < %d`, older.Add(time.Second).UnixMilli())); rows != 0 {
		t.Errorf("the log view still holds %d entries older than the retention, want none", rows)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// TestTheServerKeepsItsHistoryWhenTheRetentionIsSwitchedOff is the option's other half:
// a retention of nothing leaves the server touching nothing and saying nothing about
// pruning in its log view.
func TestTheServerKeepsItsHistoryWhenTheRetentionIsSwitchedOff(t *testing.T) {
	srv := journeyServerIn(t, t.TempDir())

	seedUploadTarget(t, srv.db, 0, pruneKey)
	seedRetention(t, srv.db, 0)
	seedOldLogEntries(t, srv.db, 3)

	before := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs"`)

	result, err := srv.pruneHandler(t).Handle(context.Background(), application.PruneOldCallsCommand{})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.Outcome != application.PruneOldCallsSkipped {
		t.Errorf("the pruning reported %q, want it skipped: a retention of nothing is switched off, not a retention of no time", result.Outcome)
	}

	if result.CallsPruned != 0 || result.LogsPruned != 0 {
		t.Errorf("the pruning discarded %d calls and %d log entries, want nothing discarded", result.CallsPruned, result.LogsPruned)
	}

	if after := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs"`); after != before {
		t.Errorf("the log view holds %d entries after a pruning that was switched off, want the %d it held before", after, before)
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs" WHERE "source" = 'prune'`); rows != 0 {
		t.Errorf("the log view holds %d entries from the retention pass for a server whose retention is switched off, want none", rows)
	}
}

// seedRetention stores the retention the server prunes with, in days, which is the unit the configuration
// stores it in.
func seedRetention(t *testing.T, db *sql.DB, days uint) {
	t.Helper()

	seed(t, db, seedStatement{
		query: `INSERT INTO "options" ("key", "value") VALUES ('pruneDays', ?)`,
		args:  []any{fmt.Sprintf("%d", days)},
	})
}

// seedOldLogEntries writes log entries five days old, what a retention of three days
// discards: written directly because the server only ever stamps its own entries now.
// the age is the point.
func seedOldLogEntries(t *testing.T, db *sql.DB, count int) {
	t.Helper()

	at := time.Now().Add(-5 * 24 * time.Hour)

	statements := []seedStatement{}

	for i := 0; i < count; i++ {
		statements = append(statements, seedStatement{
			query: `INSERT INTO "logs" ("level", "message", "timestamp") VALUES ('info', ?, ?)`,
			args:  []any{fmt.Sprintf("an entry from five days ago %d", i), at.UnixMilli()},
		})
	}

	seed(t, db, statements...)
}
