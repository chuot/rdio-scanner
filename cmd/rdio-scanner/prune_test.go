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

package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"rdio-scanner/internal/application"
)

// The retention is the server's own schedule, and what it discards is the history the server itself wrote.
// The pruning use case is pinned apart from the server, and the hourly schedule is pinned apart from the
// database; what these tests add is the two of them together: the retention the stored configuration names,
// applied to the recordings and the log entries a running server produced.

// pruneKey is the credential these tests post a recording with while testing what becomes of it.
const pruneKey = "a-key-that-ages-out"

// TestTheServerDiscardsTheHistoryOlderThanTheRetentionItIsConfiguredWith is the retention on a running
// server: two recordings posted the way an uploader posts them, one outside the retention and one inside it,
// and the pruning that the server's own schedule runs keeping only the second.
//
// The retention these tests store is three days, which is not the default, so that what is being proved is
// that the number the configuration holds is the number the pruning uses.
func TestTheServerDiscardsTheHistoryOlderThanTheRetentionItIsConfiguredWith(t *testing.T) {
	srv := testServer(t)

	seedUploadTarget(t, srv.db, 0, pruneKey)
	seedRetention(t, srv.db, 3)
	seedOldLogEntries(t, srv.db, 3)

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The upload endpoint is what names the moment a call happened, which is how a recording that is five
	// days old is made without a time machine.
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

	result, err := srv.prune.Handle(context.Background(), application.PruneOldCallsCommand{})
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

	// What is left is the recording inside the retention, and it is the same recording: the pruning discards
	// whole calls rather than trimming anything.
	if rows := countRows(t, srv.db, fmt.Sprintf(`SELECT COUNT(*) FROM "calls" WHERE "timestamp" < %d`, older.Add(time.Second).UnixMilli())); rows != 0 {
		t.Errorf("the server still holds %d calls older than the retention, want none", rows)
	}

	if rows := countRows(t, srv.db, fmt.Sprintf(`SELECT COUNT(*) FROM "calls" WHERE "timestamp" = %d`, recent.UnixMilli())); rows != 1 {
		t.Errorf("the server holds %d calls from inside the retention, want the one it accepted two days ago", rows)
	}

	// The log entries that were older than the retention are gone, and the ones the running server wrote
	// today are not — including the notice the pruning gave about itself, which is written with the current
	// instant so that a pruning does not discard its own report.
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
}

// TestTheServerKeepsItsHistoryWhenTheRetentionIsSwitchedOff is the other half of the option: a retention of
// nothing means the server touches nothing, and it does not even say that it is pruning, which is what a
// deployment with the retention off sees in its log view.
func TestTheServerKeepsItsHistoryWhenTheRetentionIsSwitchedOff(t *testing.T) {
	srv := testServer(t)

	seedUploadTarget(t, srv.db, 0, pruneKey)
	seedRetention(t, srv.db, 0)
	seedOldLogEntries(t, srv.db, 3)

	before := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs"`)

	result, err := srv.prune.Handle(context.Background(), application.PruneOldCallsCommand{})
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

// seedOldLogEntries writes log entries that are five days old, which is what a retention of three days is
// meant to discard. They are written directly because the point is the age of a log entry, and the server
// writes its own entries with the current instant.
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
