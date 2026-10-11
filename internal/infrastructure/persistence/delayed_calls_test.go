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
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/ports"
)

// openDelayedCallTestDatabase opens an empty database with the legacy schema applied, for the tests
// that write.
func openDelayedCallTestDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db, err := Open(Settings{
		Dialect:  DialectSqlite,
		FilePath: filepath.Join(t.TempDir(), "rdio-scanner.db"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	return db
}

// delayTestDelay reproduces `server/delayer.go` `Delayer.getDelay` as test setup: the talkgroup's own
// delay when it has one, the system's when it does not, and nothing when neither has one. The delay
// is in minutes.
//
// It belongs in the tests and not in the adapter: deciding whether a call is delayed is the delay
// rule, and the repository only stores the answer the application layer reached.
func delayTestDelay(talkgroupDelay, systemDelay uint) uint {
	if talkgroupDelay > 0 {
		return talkgroupDelay
	}

	if systemDelay > 0 {
		return systemDelay
	}

	return 0
}

// delayTestTimestamp reproduces `server/delayer.go` `Delayer.getTimestamp` as test setup: the instant
// a delayed call's delay runs out is its timestamp plus the delay, in minutes.
func delayTestTimestamp(timestamp time.Time, delay uint) time.Time {
	return timestamp.Add(time.Duration(delay) * time.Minute)
}

// seedDelayTestTalkgroup seeds one system and one talkgroup carrying the given delays, and returns
// the talkgroup's identifier. A call cannot be recorded without them: `calls` has foreign keys to
// both.
func seedDelayTestTalkgroup(t *testing.T, db *sql.DB, systemDelay, talkgroupDelay uint) uint64 {
	t.Helper()

	var tagID, systemID, talkgroupRef int64

	res, err := db.Exec(`INSERT INTO "tags" ("label") VALUES ('Delay Test Tag')`)
	if err != nil {
		t.Fatalf("seeding a tag: %v", err)
	}

	if tagID, err = res.LastInsertId(); err != nil {
		t.Fatalf("the seeded tag: %v", err)
	}

	if res, err = db.Exec(`INSERT INTO "systems" ("label", "systemRef", "delay") VALUES (?, ?, ?)`, "Delay Test System", 20001, systemDelay); err != nil {
		t.Fatalf("seeding a system: %v", err)
	}

	if systemID, err = res.LastInsertId(); err != nil {
		t.Fatalf("the seeded system: %v", err)
	}

	if res, err = db.Exec(`INSERT INTO "talkgroups" ("label", "name", "systemId", "tagId", "talkgroupRef", "delay") VALUES (?, ?, ?, ?, ?, ?)`,
		"Delay Test Talkgroup", "Delay Test Talkgroup", systemID, tagID, 2000100, talkgroupDelay); err != nil {
		t.Fatalf("seeding a talkgroup: %v", err)
	}

	if talkgroupRef, err = res.LastInsertId(); err != nil {
		t.Fatalf("the seeded talkgroup: %v", err)
	}

	return uint64(talkgroupRef)
}

// seedDelayTestCall records a call on the given talkgroup at the given instant and returns its
// identifier.
func seedDelayTestCall(t *testing.T, db *sql.DB, talkgroupID uint64, timestamp time.Time) uint64 {
	t.Helper()

	var systemID uint64

	if err := db.QueryRow(`SELECT "systemId" FROM "talkgroups" WHERE "talkgroupId" = ?`, talkgroupID).Scan(&systemID); err != nil {
		t.Fatalf("the seeded talkgroup: %v", err)
	}

	res, err := db.Exec(`INSERT INTO "calls" ("audio", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES (?, ?, ?, ?, ?, ?, ?)`,
		[]byte("audio"), "delay-test.wav", "audio/wav", 0, systemID, talkgroupID, millisOf(timestamp))
	if err != nil {
		t.Fatalf("recording a call: %v", err)
	}

	callID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("the recorded call: %v", err)
	}

	return uint64(callID)
}

// TestAllDelayedReadsTheCallTheLegacyServerStored is the the criterion half for this adapter: the call the
// legacy server put in the delayed table reads back with its identifier and its instant, which the
// table stores in milliseconds since the epoch.
func TestAllDelayedReadsTheCallTheLegacyServerStored(t *testing.T) {
	db := openLegacyDatabase(t)

	if got := countRows(t, db, "delayed"); got == 0 {
		t.Fatal("the database written by the legacy server stores no delayed call")
	}

	delayed, err := NewDelayedCalls(db, DialectSqlite).AllDelayed(context.Background())
	if err != nil {
		t.Fatalf("AllDelayed: %v", err)
	}

	if len(delayed) != 1 {
		t.Fatalf("AllDelayed returned %d calls, want the one the legacy server left waiting", len(delayed))
	}

	if delayed[0].CallID != 2 {
		t.Errorf("CallID = %d, want 2: the call the legacy server delayed", delayed[0].CallID)
	}

	if want := time.UnixMilli(1893461400000); !delayed[0].Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want %v: the stored 1893461400000 read as milliseconds", delayed[0].Timestamp, want)
	}
}

// TestAllDelayedOnALegacyDatabaseMatchesTheDelayTheLegacyServerApplied checks the stored instant
// against the rule that produced it, rather than against a number this test invents.
//
// `server/delayer.go` only stores a call in the delayed table when its timestamp plus the talkgroup
// delay — or, failing that, the system delay — is still in the future (`CanDelay`), and the delay is
// in minutes (`getTimestamp`). The fixture's delayed call is on a talkgroup with a 90 minute delay.
func TestAllDelayedOnALegacyDatabaseMatchesTheDelayTheLegacyServerApplied(t *testing.T) {
	db := openLegacyDatabase(t)

	var (
		callTimestamp  int64
		talkgroupDelay uint
		systemDelay    uint
	)

	const seeded = `SELECT c."timestamp", t."delay", s."delay" FROM "calls" AS c ` +
		`JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" ` +
		`JOIN "systems" AS s ON s."systemId" = c."systemId" ` +
		`JOIN "delayed" AS d ON d."callId" = c."callId"`

	if err := db.QueryRow(seeded).Scan(&callTimestamp, &talkgroupDelay, &systemDelay); err != nil {
		t.Fatalf("reading the delayed call and the delays stored for it: %v", err)
	}

	delay := delayTestDelay(talkgroupDelay, systemDelay)

	if delay == 0 {
		t.Fatal("the fixture's delayed call names no delay at all, so it would never have been delayed")
	}

	delayed, err := NewDelayedCalls(db, DialectSqlite).AllDelayed(context.Background())
	if err != nil {
		t.Fatalf("AllDelayed: %v", err)
	}

	if len(delayed) != 1 {
		t.Fatalf("AllDelayed returned %d calls, want 1", len(delayed))
	}

	want := delayTestTimestamp(instantOf(callTimestamp), delay)

	if !delayed[0].Timestamp.Equal(want) {
		t.Errorf("the stored instant is %v, want the call's timestamp plus the %d minute delay: %v", delayed[0].Timestamp, delay, want)
	}
}

// TestAllDelayedReportsNoCallWhenNothingIsWaiting pins the empty table.
func TestAllDelayedReportsNoCallWhenNothingIsWaiting(t *testing.T) {
	db := openDelayedCallTestDatabase(t)

	delayed, err := NewDelayedCalls(db, DialectSqlite).AllDelayed(context.Background())
	if err != nil {
		t.Fatalf("AllDelayed: %v", err)
	}

	if len(delayed) != 0 {
		t.Errorf("AllDelayed returned %v, want nothing", delayed)
	}
}

// TestScheduleDelayedStoresTheInstantInMilliseconds pins the column: the instant the delay runs out is
// stored as the milliseconds the schema means, which is what the startup re-arm reads back.
func TestScheduleDelayedStoresTheInstantInMilliseconds(t *testing.T) {
	db := openDelayedCallTestDatabase(t)

	const delay = uint(90)

	talkgroupID := seedDelayTestTalkgroup(t, db, 0, delay)

	timestamp := time.UnixMilli(1893456000000)

	callID := seedDelayTestCall(t, db, talkgroupID, timestamp)

	// The instant the legacy server would have stored, from the delay rule.
	until := delayTestTimestamp(timestamp, delay)

	if err := NewDelayedCalls(db, DialectSqlite).ScheduleDelayed(context.Background(), callID, until); err != nil {
		t.Fatalf("ScheduleDelayed: %v", err)
	}

	var (
		stored    int64
		storedAs  string
		storedFor uint64
	)

	if err := db.QueryRow(`SELECT "callId", "timestamp", typeof("timestamp") FROM "delayed"`).Scan(&storedFor, &stored, &storedAs); err != nil {
		t.Fatalf("reading the stored delayed call: %v", err)
	}

	if storedFor != callID {
		t.Errorf("the row waits for call %d, want %d", storedFor, callID)
	}

	if stored != millisOf(until) {
		t.Errorf("the instant is stored as %d, want %d: the milliseconds the column means", stored, millisOf(until))
	}

	if storedAs != "integer" {
		t.Errorf("the instant is stored as %s, want integer as the legacy server stores it", storedAs)
	}

	delayed, err := NewDelayedCalls(db, DialectSqlite).AllDelayed(context.Background())
	if err != nil {
		t.Fatalf("AllDelayed: %v", err)
	}

	if len(delayed) != 1 || !delayed[0].Timestamp.Equal(until) {
		t.Errorf("AllDelayed returned %v, want the call waiting until %v", delayed, until)
	}
}

// TestScheduleDelayedRefusesACallThatWasNotRecorded pins the cascading foreign key the legacy schema
// puts on this table: a waiting row has to belong to a recorded call.
func TestScheduleDelayedRefusesACallThatWasNotRecorded(t *testing.T) {
	db := openDelayedCallTestDatabase(t)

	err := NewDelayedCalls(db, DialectSqlite).ScheduleDelayed(context.Background(), 4242, time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("a waiting call that was never recorded is a failure the caller is told about")
	}

	if !strings.HasPrefix(err.Error(), "delayer.push:") {
		t.Errorf("error = %v, want the legacy delayer.push shape", err)
	}

	if !strings.Contains(err.Error(), `INSERT INTO "delayed"`) {
		t.Errorf("error = %v, want it to name the statement that failed", err)
	}
}

// TestUnscheduleDelayedRemovesTheWaitingCallAndKeepsTheCall pins `pop`: the call stops waiting, and
// the recording itself is left alone.
func TestUnscheduleDelayedRemovesTheWaitingCallAndKeepsTheCall(t *testing.T) {
	db := openDelayedCallTestDatabase(t)

	talkgroupID := seedDelayTestTalkgroup(t, db, 0, 90)

	timestamp := time.UnixMilli(1893456000000)
	callID := seedDelayTestCall(t, db, talkgroupID, timestamp)

	repo := NewDelayedCalls(db, DialectSqlite)

	if err := repo.ScheduleDelayed(context.Background(), callID, delayTestTimestamp(timestamp, 90)); err != nil {
		t.Fatalf("ScheduleDelayed: %v", err)
	}

	if err := repo.UnscheduleDelayed(context.Background(), callID); err != nil {
		t.Fatalf("UnscheduleDelayed: %v", err)
	}

	delayed, err := repo.AllDelayed(context.Background())
	if err != nil {
		t.Fatalf("AllDelayed: %v", err)
	}

	if len(delayed) != 0 {
		t.Errorf("AllDelayed returned %v, want nothing: the call's delay has run", delayed)
	}

	if got := countRows(t, db, "calls"); got != 1 {
		t.Errorf("the database stores %d calls, want 1: removing a waiting call does not remove the recording", got)
	}
}

// TestUnscheduleDelayedOnACallThatIsNotWaitingIsNotAFailure pins the legacy `pop`, which runs a DELETE
// and reports only what the database says about it: a row that is already gone is not an error.
func TestUnscheduleDelayedOnACallThatIsNotWaitingIsNotAFailure(t *testing.T) {
	db := openDelayedCallTestDatabase(t)

	talkgroupID := seedDelayTestTalkgroup(t, db, 0, 90)
	callID := seedDelayTestCall(t, db, talkgroupID, time.UnixMilli(1893456000000))

	if err := NewDelayedCalls(db, DialectSqlite).UnscheduleDelayed(context.Background(), callID); err != nil {
		t.Errorf("UnscheduleDelayed for a call that is not waiting: %v, want no failure", err)
	}
}

// TestDeletingARecordedCallRemovesItsWaitingRowByItself pins the cascade the legacy schema declares
// on this table, and the reason the repository does not delete twice: the call's own row is what a
// pruned or deleted call goes through, and the waiting row follows it.
func TestDeletingARecordedCallRemovesItsWaitingRowByItself(t *testing.T) {
	db := openDelayedCallTestDatabase(t)

	talkgroupID := seedDelayTestTalkgroup(t, db, 0, 90)

	timestamp := time.UnixMilli(1893456000000)
	callID := seedDelayTestCall(t, db, talkgroupID, timestamp)

	repo := NewDelayedCalls(db, DialectSqlite)

	if err := repo.ScheduleDelayed(context.Background(), callID, delayTestTimestamp(timestamp, 90)); err != nil {
		t.Fatalf("ScheduleDelayed: %v", err)
	}

	if _, err := db.Exec(`DELETE FROM "calls" WHERE "callId" = ?`, callID); err != nil {
		t.Fatalf("deleting the recorded call: %v", err)
	}

	delayed, err := repo.AllDelayed(context.Background())
	if err != nil {
		t.Fatalf("AllDelayed: %v", err)
	}

	if len(delayed) != 0 {
		t.Errorf("AllDelayed returned %v, want nothing: the waiting row cascades with the call", delayed)
	}

	if got := countRows(t, db, "delayed"); got != 0 {
		t.Errorf("the delayed table stores %d rows, want 0", got)
	}

	// And the repository does not try to remove it a second time on the way through.
	if err := repo.UnscheduleDelayed(context.Background(), callID); err != nil {
		t.Errorf("UnscheduleDelayed after the cascade: %v, want no failure", err)
	}
}

// TestClearDelayedDropsEveryWaitingCallAndKeepsTheCalls pins `Clear`: the whole waiting list is
// dropped — which is what the startup re-arm does between reading the table and re-arming each call —
// and the recordings are left alone.
func TestClearDelayedDropsEveryWaitingCallAndKeepsTheCalls(t *testing.T) {
	db := openDelayedCallTestDatabase(t)

	talkgroupID := seedDelayTestTalkgroup(t, db, 0, 90)

	repo := NewDelayedCalls(db, DialectSqlite)

	for _, timestamp := range []int64{1893456000000, 1893542400000} {
		callID := seedDelayTestCall(t, db, talkgroupID, time.UnixMilli(timestamp))

		if err := repo.ScheduleDelayed(context.Background(), callID, delayTestTimestamp(time.UnixMilli(timestamp), 90)); err != nil {
			t.Fatalf("ScheduleDelayed: %v", err)
		}
	}

	if got := countRows(t, db, "delayed"); got != 2 {
		t.Fatalf("the delayed table stores %d rows, want 2 before the clear", got)
	}

	if err := repo.ClearDelayed(context.Background()); err != nil {
		t.Fatalf("ClearDelayed: %v", err)
	}

	delayed, err := repo.AllDelayed(context.Background())
	if err != nil {
		t.Fatalf("AllDelayed: %v", err)
	}

	if len(delayed) != 0 {
		t.Errorf("AllDelayed returned %v, want nothing", delayed)
	}

	if got := countRows(t, db, "calls"); got != 2 {
		t.Errorf("the database stores %d calls, want 2: clearing the waiting list does not remove the recordings", got)
	}
}

// TestDelayedCallFailuresKeepTheLegacyOperationNames pins the log-line shape of each failure. The
// read and the clear share the legacy `restore` name because both are steps of the startup re-arm,
// which is one operation in `server/delayer.go` `Delayer.Start`.
func TestDelayedCallFailuresKeepTheLegacyOperationNames(t *testing.T) {
	db := openDelayedCallTestDatabase(t)

	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	repo := ports.DelayedCallRepository(NewDelayedCalls(db, DialectSqlite))

	ctx := context.Background()

	late := time.Now().Add(time.Minute)

	for _, tc := range []struct {
		operation string
		run       func(ports.DelayedCallRepository) error
		want      string
	}{
		{"AllDelayed", func(r ports.DelayedCallRepository) error { _, err := r.AllDelayed(ctx); return err }, "delayer.restore:"},
		{"ScheduleDelayed", func(r ports.DelayedCallRepository) error { return r.ScheduleDelayed(ctx, 1, late) }, "delayer.push:"},
		{"UnscheduleDelayed", func(r ports.DelayedCallRepository) error { return r.UnscheduleDelayed(ctx, 1) }, "delayer.pop:"},
		{"ClearDelayed", func(r ports.DelayedCallRepository) error { return r.ClearDelayed(ctx) }, "delayer.restore:"},
	} {
		err := tc.run(repo)
		if err == nil {
			t.Errorf("%s against a store that cannot be reached: want a failure", tc.operation)

			continue
		}

		if !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%s error = %v, want the legacy %s shape", tc.operation, err, tc.want)
		}
	}
}
