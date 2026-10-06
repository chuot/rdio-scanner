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
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

// The database written by the legacy server carries two calls, both recorded by its own upload
// endpoint (see testdata/legacydb/generate.sh):
//
//   - call 1, on system reference 10001 / talkgroup reference 1514000, with one frequency, one
//     source unit, one patch to talkgroup reference 1514001, and site reference 77;
//   - call 2, on talkgroup reference 1514002, which the legacy delayer put in the `delayed`
//     table because that talkgroup carries a 90 minute delay.
//
// The read tests below read those two calls rather than calls this package wrote, which is the
// half of REQ-007 that says server-ng opens a database the other server created.
const (
	fixtureCallID        = uint64(1)
	fixtureDelayedCallID = uint64(2)
)

// fixtureCallTimestamp is the instant call 1 was recorded at: the upload submitted
// `dateTime=1750000000`, which the legacy server reads as seconds since the epoch.
var fixtureCallTimestamp = time.Unix(1750000000, 0)

// fixtureDelayedTimestamp is the instant call 2 was recorded at, chosen by generate.sh so that
// adding the talkgroup's delay still lands in the future.
var fixtureDelayedTimestamp = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

func TestGetCallReadsACallTheLegacyServerWrote(t *testing.T) {
	calls := NewCalls(openLegacyDatabase(t), DialectSqlite)

	call, err := calls.GetCall(context.Background(), fixtureCallID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if call.ID != fixtureCallID {
		t.Errorf("GetCall returned call %d, want the call that was asked for", call.ID)
	}

	if !call.Timestamp.Equal(fixtureCallTimestamp) {
		t.Errorf("Timestamp = %v, want %v, which is what the legacy server stored", call.Timestamp, fixtureCallTimestamp)
	}

	if call.SiteRef != 77 {
		t.Errorf("SiteRef = %d, want the site reference the upload submitted", call.SiteRef)
	}

	if call.System.ID != 1 || call.System.SystemRef != 10001 {
		t.Errorf("System = %+v, want the stored system 1 with reference 10001", call.System)
	}

	if call.Talkgroup.ID != 1 || call.Talkgroup.TalkgroupRef != 1514000 {
		t.Errorf("Talkgroup = %+v, want the stored talkgroup 1 with reference 1514000", call.Talkgroup)
	}

	if call.Talkgroup.Label != "Dispatch 1" || call.Talkgroup.Name != "Dispatch 1" {
		t.Errorf("Talkgroup labels = %q / %q, want the labels the administrator configured", call.Talkgroup.Label, call.Talkgroup.Name)
	}
}

// TestGetCallReadsTheAudioBlobTheLegacyServerStoredByteForByte is the audio half of REQ-007: a
// recording written by one server is read back by the other without a byte moving.
func TestGetCallReadsTheAudioBlobTheLegacyServerStoredByteForByte(t *testing.T) {
	db := openLegacyDatabase(t)

	stored := storedAudioBlob(t, db, fixtureCallID)

	if len(stored) <= 44 {
		t.Fatalf("the fixture stores %d bytes of audio, which is a container header with no recording in it", len(stored))
	}

	call, err := NewCalls(db, DialectSqlite).GetCall(context.Background(), fixtureCallID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if !bytes.Equal(call.Audio, stored) {
		t.Errorf("GetCall returned %d bytes of audio, want the %d bytes the legacy server stored", len(call.Audio), len(stored))
	}
}

// TestGetCallReportsTheAudioMimeTheLegacyServerStored pins what the fixture actually carries.
//
// The upload sent its `audio` part with a content type of `audio/wav`, and the legacy server
// never looks at it: `ParseMultipartContent` records the part's filename only, so the stored
// `audioMime` is empty. Reading the call back must not invent a type that was never stored.
func TestGetCallReportsTheAudioMimeTheLegacyServerStored(t *testing.T) {
	calls := NewCalls(openLegacyDatabase(t), DialectSqlite)

	call, err := calls.GetCall(context.Background(), fixtureCallID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if call.AudioFilename != "audio.wav" {
		t.Errorf("AudioFilename = %q, want the name of the uploaded part", call.AudioFilename)
	}

	if call.AudioMime != "" {
		t.Errorf("AudioMime = %q, want the empty value the legacy server stored for an upload", call.AudioMime)
	}
}

func TestGetCallReadsTheFrequencyTheLegacyServerStored(t *testing.T) {
	calls := NewCalls(openLegacyDatabase(t), DialectSqlite)

	call, err := calls.GetCall(context.Background(), fixtureCallID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if len(call.Frequencies) != 1 {
		t.Fatalf("GetCall returned %d frequencies, want the one the upload stored", len(call.Frequencies))
	}

	freq := call.Frequencies[0]

	if freq.CallID != fixtureCallID {
		t.Errorf("frequency belongs to call %d, want %d", freq.CallID, fixtureCallID)
	}

	if freq.Frequency != 1514000000 {
		t.Errorf("Frequency = %d, want the carrier the upload submitted", freq.Frequency)
	}

	if freq.Dbm != 0 || freq.Errors != 0 || freq.Spikes != 0 || freq.Offset != 0 {
		t.Errorf("frequency measurements = %+v, want the zeros the upload left them at", freq)
	}
}

func TestGetCallReadsTheSourceUnitTheLegacyServerStored(t *testing.T) {
	calls := NewCalls(openLegacyDatabase(t), DialectSqlite)

	call, err := calls.GetCall(context.Background(), fixtureCallID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if len(call.Units) != 1 {
		t.Fatalf("GetCall returned %d units, want the one the upload stored", len(call.Units))
	}

	unit := call.Units[0]

	if unit.CallID != fixtureCallID {
		t.Errorf("unit belongs to call %d, want %d", unit.CallID, fixtureCallID)
	}

	if unit.UnitRef != 12345 {
		t.Errorf("UnitRef = %d, want the source the upload named", unit.UnitRef)
	}

	if unit.Offset != 0 {
		t.Errorf("Offset = %v, want the position the upload gave it", unit.Offset)
	}
}

func TestGetCallReadsThePatchTheLegacyServerStored(t *testing.T) {
	calls := NewCalls(openLegacyDatabase(t), DialectSqlite)

	call, err := calls.GetCall(context.Background(), fixtureCallID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if len(call.PatchTalkgroupRefs) != 1 || call.PatchTalkgroupRefs[0] != 1514001 {
		t.Errorf("PatchTalkgroupRefs = %v, want the one talkgroup reference the upload patched to", call.PatchTalkgroupRefs)
	}
}

// TestGetCallReportsACallThatWasNeverRecordedAsNotFound pins the port's answer. The legacy
// server reports the same situation as `cannot retrieve system id 0 for call id N`, because it
// carries on past its own no-rows result with every value still zero.
func TestGetCallReportsACallThatWasNeverRecordedAsNotFound(t *testing.T) {
	calls := NewCalls(openLegacyDatabase(t), DialectSqlite)

	_, err := calls.GetCall(context.Background(), 9999)

	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("GetCall error = %v, want ports.ErrNotFound", err)
	}
}

// TestGetCallLeavesTheDatabaseFreeForTheNextWrite pins the consequence the legacy leak had, which
// is the only reason a fetch of a call that is not there is interesting at all.
//
// server/call.go `GetCall` returns without rolling back when the call it found names a system or a
// talkgroup it cannot resolve — and a call id that does not exist falls through its own no-rows
// result with the system id still zero, so that is the ordinary case. The transaction stays open
// until the garbage collector collects it, and on a SQLite deployment it holds the read lock that
// blocks writers, so the next log insert or configuration save waits out the ten-second
// `busy_timeout` the connection string sets and then fails with `database is locked`. Observed
// while capturing the wire protocol: a client asking for call id 999999 is answered with nothing,
// and the listener-count broadcast that follows arrives ten seconds late. Recorded as Q56.
func TestGetCallLeavesTheDatabaseFreeForTheNextWrite(t *testing.T) {
	db := openLegacyDatabase(t)

	if _, err := NewCalls(db, DialectSqlite).GetCall(context.Background(), 999999); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetCall error = %v, want ports.ErrNotFound", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := NewLogs(db, DialectSqlite).AppendLog(ctx, domain.LogEntry{
		DateTime: time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
		Level:    domain.LogLevelWarn,
		Message:  "written immediately after a call fetch that found nothing",
	}); err != nil {
		t.Fatalf("AppendLog after a failed GetCall: %v", err)
	}
}

// TestGetCallDoesNotReportAStoredCallAsWaitingOutADelay pins what the legacy read does: the
// `delayed` table is the delayer's own state, and `Calls.GetCall` never consults it, so a call
// fetched out of the history carries no delayed flag. The delayer sets it on the call it hands
// to the hub.
func TestGetCallDoesNotReportAStoredCallAsWaitingOutADelay(t *testing.T) {
	db := openLegacyDatabase(t)

	if got := countRows(t, db, "delayed"); got != 1 {
		t.Fatalf("the fixture has %d delayed rows, want the one generate.sh produced", got)
	}

	call, err := NewCalls(db, DialectSqlite).GetCall(context.Background(), fixtureDelayedCallID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if call.Delayed {
		t.Error("GetCall reported the call as delayed, which is not a fact the calls table stores")
	}
}

func TestSearchCallsLeavesOutTheCallTheLegacyServerDelayed(t *testing.T) {
	db := openLegacyDatabase(t)

	if got := countRows(t, db, "calls"); got != 2 {
		t.Fatalf("the fixture has %d calls, want the two generate.sh recorded", got)
	}

	if got := countRows(t, db, "delayed"); got != 1 {
		t.Fatalf("the fixture has %d delayed rows, want the one generate.sh produced", got)
	}

	page, err := NewCalls(db, DialectSqlite).SearchCalls(context.Background(), ports.CallSearch{Limit: 200, Ascending: true})
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	if page.Count != 1 {
		t.Errorf("Count = %d, want the delayed call left out of the history", page.Count)
	}

	if len(page.Results) != 1 || page.Results[0].ID != fixtureCallID {
		t.Errorf("SearchCalls returned %v, want only the call that is not waiting out a delay", page.Results)
	}
}

// TestSearchCallsLeavesOutADelayedCallForAScopedCaller pins a deliberate difference from the
// legacy server, recorded in the task report: `Calls.Search` *replaces* its base filter with the
// caller's scope instead of adding to it, so a listener with an explicit scope loses the
// `delayed` exclusion and sees calls that have not been made visible yet.
func TestSearchCallsLeavesOutADelayedCallForAScopedCaller(t *testing.T) {
	db := openLegacyDatabase(t)

	scope, _ := domain.ParseAccessScope([]any{
		map[string]any{"id": float64(10001), "talkgroups": []any{float64(1514000), float64(1514002)}},
	})

	page, err := NewCalls(db, DialectSqlite).SearchCalls(context.Background(), ports.CallSearch{Scope: scope, Limit: 200, Ascending: true})
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	if page.Count != 1 || page.Results[0].ID != fixtureCallID {
		t.Errorf("SearchCalls returned %v with count %d, want the visible call only", page.Results, page.Count)
	}
}

func TestSearchCallsReadsALegacyWrittenCallAsASummary(t *testing.T) {
	calls := NewCalls(openLegacyDatabase(t), DialectSqlite)

	page, err := calls.SearchCalls(context.Background(), ports.CallSearch{Limit: 200, Ascending: true})
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	summary := page.Results[0]

	if summary.ID != fixtureCallID {
		t.Errorf("summary = %+v, want call %d", summary, fixtureCallID)
	}

	if summary.SystemRef != 10001 || summary.TalkgroupRef != 1514000 {
		t.Errorf("summary = %+v, want the references the history view filters and links on", summary)
	}

	if !summary.Timestamp.Equal(fixtureCallTimestamp) {
		t.Errorf("summary timestamp = %v, want %v", summary.Timestamp, fixtureCallTimestamp)
	}
}

func TestFindDuplicateCandidatesReadsTheCallTheLegacyServerDelayed(t *testing.T) {
	db := openLegacyDatabase(t)

	systemID, talkgroupID := storedAggregates(t, db, 10001, 1514002)

	candidates, err := NewCalls(db, DialectSqlite).FindDuplicateCandidates(context.Background(), systemID, talkgroupID, fixtureDelayedTimestamp.Add(-time.Hour), fixtureDelayedTimestamp.Add(time.Hour))
	if err != nil {
		t.Fatalf("FindDuplicateCandidates: %v", err)
	}

	if len(candidates) != 1 || candidates[0].ID != fixtureDelayedCallID {
		t.Errorf("FindDuplicateCandidates returned %v, want the delayed call: the legacy duplicate check counts every call in the window", candidates)
	}
}

func TestSaveCallReturnsTheIdentifierTheDatabaseAssigned(t *testing.T) {
	fixture := newCallsFixture(t)

	first := fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	second := fixture.storeCall(t, 10001, 1514000, callHistoryTime().Add(time.Second))

	if first.ID == 0 || second.ID == 0 {
		t.Fatalf("SaveCall returned identifiers %d and %d, want the ones the table assigned", first.ID, second.ID)
	}

	if first.ID == second.ID {
		t.Errorf("both calls got identifier %d", first.ID)
	}

	if second.ID <= first.ID {
		t.Errorf("identifiers %d then %d, want the history numbered in the order it was written", first.ID, second.ID)
	}
}

func TestSaveCallStoresTheFrequenciesUnitsAndPatchOfACall(t *testing.T) {
	fixture := newCallsFixture(t)

	call := fixture.newCall(10001, 1514001, callHistoryTime())

	call.SiteRef = 77
	call.AudioFilename = "engine-12.wav"
	call.AudioMime = "audio/mp4"
	call.Frequencies = []domain.CallFrequency{
		{Dbm: -95, Errors: 2, Frequency: 1514000000, Offset: 1.5, Spikes: 3},
		{Frequency: 1514001000},
	}
	call.PatchTalkgroupRefs = []uint{1514000, 1514002}
	call.Units = []domain.CallUnit{{Offset: 0.25, UnitRef: 12345}, {Offset: 1.75, UnitRef: 12346}}

	saved, err := fixture.calls.SaveCall(context.Background(), call)
	if err != nil {
		t.Fatalf("SaveCall: %v", err)
	}

	found, err := fixture.calls.GetCall(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if found.SiteRef != 77 || found.AudioFilename != "engine-12.wav" || found.AudioMime != "audio/mp4" {
		t.Errorf("found %+v, want the audio description the call carried", found)
	}

	if len(found.Frequencies) != 2 {
		t.Fatalf("GetCall returned %d frequencies, want the two the call carried", len(found.Frequencies))
	}

	if got := found.Frequencies[0]; got.Dbm != -95 || got.Errors != 2 || got.Frequency != 1514000000 || got.Offset != 1.5 || got.Spikes != 3 {
		t.Errorf("first frequency = %+v, want the measurements the call carried", got)
	}

	if len(found.Units) != 2 || found.Units[0].UnitRef != 12345 || found.Units[1].UnitRef != 12346 {
		t.Errorf("GetCall returned units %v, want the two source units the call carried", found.Units)
	}

	if len(found.PatchTalkgroupRefs) != 2 || found.PatchTalkgroupRefs[0] != 1514000 || found.PatchTalkgroupRefs[1] != 1514002 {
		t.Errorf("GetCall returned patches %v, want the two talkgroup references the call patched to", found.PatchTalkgroupRefs)
	}
}

func TestSaveCallRoundTripsTheAudioBlobByteForByte(t *testing.T) {
	fixture := newCallsFixture(t)

	call := fixture.newCall(10001, 1514000, callHistoryTime())
	call.Audio = testAudio

	saved, err := fixture.calls.SaveCall(context.Background(), call)
	if err != nil {
		t.Fatalf("SaveCall: %v", err)
	}

	found, err := fixture.calls.GetCall(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if !bytes.Equal(found.Audio, testAudio) {
		t.Errorf("SaveCall stored %v, want the recording written back unchanged", found.Audio)
	}
}

// TestSaveCallDropsAPatchThatNamesATalkgroupTheSystemDoesNotHave pins the legacy `WriteCall`,
// which looks each patched reference up on the call's own system and moves on when it is not
// there.
func TestSaveCallDropsAPatchThatNamesATalkgroupTheSystemDoesNotHave(t *testing.T) {
	fixture := newCallsFixture(t)

	call := fixture.newCall(10001, 1514000, callHistoryTime())
	call.PatchTalkgroupRefs = []uint{1514001, 1520000}

	saved, err := fixture.calls.SaveCall(context.Background(), call)
	if err != nil {
		t.Fatalf("SaveCall: %v", err)
	}

	found, err := fixture.calls.GetCall(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if len(found.PatchTalkgroupRefs) != 1 || found.PatchTalkgroupRefs[0] != 1514001 {
		t.Errorf("patches = %v, want only the talkgroup this system has", found.PatchTalkgroupRefs)
	}
}

func TestSaveCallReportsAFailedWriteInTheLegacyShape(t *testing.T) {
	fixture := newCallsFixture(t)

	call := fixture.newCall(10001, 1514000, callHistoryTime())
	call.Talkgroup.ID = 9999

	_, err := fixture.calls.SaveCall(context.Background(), call)

	if err == nil {
		t.Fatal("a call attached to a talkgroup that does not exist is a failure the caller is told about")
	}

	if !strings.HasPrefix(err.Error(), "calls.writecall: ") {
		t.Errorf("error = %v, want the legacy `calls.writecall: …` shape", err)
	}

	if !strings.Contains(err.Error(), ` in INSERT INTO "calls" `) {
		t.Errorf("error = %v, want it to name the statement that failed", err)
	}
}

// TestSaveCallRollsBackTheWholeSaveWhenAChildRowCannotBeStored pins the transaction: the call row
// is written first, so a save that fails afterwards must not leave a call with no children behind.
// The failure is arranged with a trigger, because nothing about a well-formed call makes the child
// inserts fail on their own.
func TestSaveCallRollsBackTheWholeSaveWhenAChildRowCannotBeStored(t *testing.T) {
	fixture := newCallsFixture(t)

	const trigger = `CREATE TRIGGER "test_reject_callUnits" BEFORE INSERT ON "callUnits" BEGIN SELECT RAISE(ABORT, 'test trigger'); END;`

	if _, err := fixture.db.Exec(trigger); err != nil {
		t.Fatalf("arranging a child insert that fails: %v", err)
	}

	call := fixture.newCall(10001, 1514000, callHistoryTime())
	call.Frequencies = []domain.CallFrequency{{Frequency: 1514000000}}
	call.PatchTalkgroupRefs = []uint{1514001}
	call.Units = []domain.CallUnit{{UnitRef: 12345}}

	if _, err := fixture.calls.SaveCall(context.Background(), call); err == nil {
		t.Fatal("SaveCall: expected the write to fail")
	}

	for _, table := range []string{"calls", "callFrequencies", "callPatches", "callUnits"} {
		if got := countRows(t, fixture.db, table); got != 0 {
			t.Errorf("%s has %d rows after a failed save, want none", table, got)
		}
	}
}

// TestSaveCallStoresAnAudioFilenameThatLooksLikeSQLAsItsOwnValue is the parameterization half of
// REQ-007: the legacy server pasted this value into its statement, so a filename like this one
// changed what the statement did.
func TestSaveCallStoresAnAudioFilenameThatLooksLikeSQLAsItsOwnValue(t *testing.T) {
	fixture := newCallsFixture(t)

	const name = `engine.wav'; DROP TABLE "calls"; --`

	call := fixture.newCall(10001, 1514000, callHistoryTime())
	call.AudioFilename = name

	saved, err := fixture.calls.SaveCall(context.Background(), call)
	if err != nil {
		t.Fatalf("SaveCall: %v", err)
	}

	found, err := fixture.calls.GetCall(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if found.AudioFilename != name {
		t.Errorf("AudioFilename = %q, want the submitted name kept as a value", found.AudioFilename)
	}

	if got := countRows(t, fixture.db, "calls"); got != 1 {
		t.Errorf("the calls table has %d rows, want the one call that was saved", got)
	}
}

// TestSaveCallDoesNotScheduleTheDelayItself pins the boundary between this repository and the
// delayed-call one: `Delayed` is a fact about the running server, and the row that keeps a call
// out of the history belongs to the delayer.
func TestSaveCallDoesNotScheduleTheDelayItself(t *testing.T) {
	fixture := newCallsFixture(t)

	call := fixture.newCall(10001, 1514002, callHistoryTime())
	call.Delayed = true

	if _, err := fixture.calls.SaveCall(context.Background(), call); err != nil {
		t.Fatalf("SaveCall: %v", err)
	}

	if got := countRows(t, fixture.db, "delayed"); got != 0 {
		t.Errorf("the delayed table has %d rows after SaveCall, want none", got)
	}
}

func TestSearchCallsSortsBothEndsOfTheHistory(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	fixture.storeCall(t, 10001, 1514000, callHistoryTime().Add(time.Hour))

	page := fixture.search(t, ports.CallSearch{Limit: 200})
	if got := resultIDs(page); len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Errorf("SearchCalls returned %v, want the newest call first when the criteria does not ask for ascending", got)
	}

	page = fixture.search(t, ports.CallSearch{Limit: 200, Ascending: true})
	if got := resultIDs(page); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("SearchCalls returned %v, want the oldest call first", got)
	}
}

func TestSearchCallsPagesWithLimitAndOffset(t *testing.T) {
	fixture := newCallsFixture(t)

	for i := range 5 {
		fixture.storeCall(t, 10001, 1514000, callHistoryTime().Add(time.Duration(i)*time.Minute))
	}

	page := fixture.search(t, ports.CallSearch{Limit: 2, Offset: 1, Ascending: true})

	if got := resultIDs(page); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("SearchCalls returned %v, want the second and third calls", got)
	}

	if page.Count != 5 {
		t.Errorf("Count = %d, want the whole match, not the page", page.Count)
	}

	page = fixture.search(t, ports.CallSearch{Limit: 2, Offset: 4, Ascending: true})
	if got := resultIDs(page); len(got) != 1 || got[0] != 5 {
		t.Errorf("SearchCalls returned %v, want the last call only", got)
	}

	page = fixture.search(t, ports.CallSearch{Limit: 2, Offset: 5, Ascending: true})
	if len(page.Results) != 0 {
		t.Errorf("SearchCalls returned %v, want nothing past the end of the history", page.Results)
	}
}

func TestSearchCallsAsksForNoRowsWhenTheLimitIsZeroButStillCountsTheMatch(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())

	page := fixture.search(t, ports.CallSearch{Offset: 0, Ascending: true})

	if len(page.Results) != 0 {
		t.Errorf("SearchCalls returned %v, want no rows at all for a zero limit", page.Results)
	}

	if page.Count != 1 {
		t.Errorf("Count = %d, want the match counted even though no page was asked for", page.Count)
	}
}

func TestSearchCallsReportsTheSpanOfTheWholeMatchingHistoryNotThePage(t *testing.T) {
	fixture := newCallsFixture(t)

	first := fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	fixture.storeCall(t, 10001, 1514001, callHistoryTime().Add(3*time.Hour))
	last := fixture.storeCall(t, 10001, 1514001, callHistoryTime().Add(6*time.Hour))

	page := fixture.search(t, ports.CallSearch{Limit: 1, Ascending: true})

	if len(page.Results) != 1 || page.Results[0].ID != first.ID {
		t.Fatalf("SearchCalls returned %v, want the first call only", page.Results)
	}

	if !page.DateStart.Equal(callHistoryTime()) {
		t.Errorf("DateStart = %v, want the earliest matching call", page.DateStart)
	}

	if !page.DateStop.Equal(last.Timestamp) {
		t.Errorf("DateStop = %v, want the latest matching call across the whole history, not the one on this page", page.DateStop)
	}
}

// TestSearchCallsReportsTheSpanOfTheBoundedMatch pins the choice this adapter makes rather than
// follows: the legacy `Calls.Search` reads the two ends of the history *before* it adds the
// submitted day to its filter, so `dateStart` and `dateStop` keep describing the whole history
// even for a search bounded to one day. Here they describe the match the criteria actually
// produced, which is what the in-memory repository the use cases run on already does.
func TestSearchCallsReportsTheSpanOfTheBoundedMatch(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	fixture.storeCall(t, 10001, 1514000, callHistoryTime().Add(48*time.Hour))

	from := callHistoryTime()
	to := callHistoryTime().Add(24*time.Hour - time.Millisecond)

	page := fixture.search(t, ports.CallSearch{From: from, To: to, Limit: 200, Ascending: true})

	if page.Count != 1 {
		t.Fatalf("Count = %d, want only the call inside the bounds", page.Count)
	}

	if !page.DateStart.Equal(from) || !page.DateStop.Equal(from) {
		t.Errorf("DateStart/DateStop = %v / %v, want the span of the bounded match", page.DateStart, page.DateStop)
	}
}

// TestSearchCallsWithNothingToReturnReportsNoSpan pins what an empty history page carries. The
// legacy server leaves its timestamp variable at zero and reports the Unix epoch for both ends,
// which the history view cannot tell apart from a call recorded on 1970-01-01.
func TestSearchCallsWithNothingToReturnReportsNoSpan(t *testing.T) {
	fixture := newCallsFixture(t)

	page := fixture.search(t, ports.CallSearch{Limit: 200, Ascending: true})

	if page.Count != 0 || len(page.Results) != 0 {
		t.Fatalf("SearchCalls returned %v with count %d, want an empty page", page.Results, page.Count)
	}

	if !page.DateStart.IsZero() || !page.DateStop.IsZero() {
		t.Errorf("DateStart/DateStop = %v / %v, want no span at all rather than the Unix epoch", page.DateStart, page.DateStop)
	}
}

func TestSearchCallsBoundsTheHistoryByFromAndToInclusively(t *testing.T) {
	fixture := newCallsFixture(t)

	at := callHistoryTime()

	fixture.storeCall(t, 10001, 1514000, at.Add(-time.Second))
	inside := fixture.storeCall(t, 10001, 1514000, at)
	fixture.storeCall(t, 10001, 1514000, at.Add(time.Second))

	page := fixture.search(t, ports.CallSearch{From: at, To: at, Limit: 200, Ascending: true})

	if page.Count != 1 || len(page.Results) != 1 || page.Results[0].ID != inside.ID {
		t.Errorf("SearchCalls returned %v with count %d, want the call sitting exactly on the bound", page.Results, page.Count)
	}
}

func TestSearchCallsFiltersBySystemAndTalkgroupReference(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeSystem(t, 10002, 1520000)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	onSystem := fixture.storeCall(t, 10001, 1514001, callHistoryTime().Add(time.Minute))
	onOtherSystem := fixture.storeCall(t, 10002, 1520000, callHistoryTime().Add(2*time.Minute))

	page := fixture.search(t, ports.CallSearch{SystemRef: 10001, Limit: 200, Ascending: true})
	if got := resultIDs(page); len(got) != 2 || got[0] != 1 || got[1] != onSystem.ID {
		t.Errorf("SearchCalls returned %v, want both calls on system reference 10001 and not %d", got, onOtherSystem.ID)
	}

	page = fixture.search(t, ports.CallSearch{SystemRef: 10001, TalkgroupRef: 1514001, Limit: 200, Ascending: true})
	if got := resultIDs(page); len(got) != 1 || got[0] != onSystem.ID {
		t.Errorf("SearchCalls returned %v, want only the named talkgroup on the named system", got)
	}

	page = fixture.search(t, ports.CallSearch{TalkgroupRef: 1514001, Limit: 200, Ascending: true})
	if len(page.Results) != 3 {
		t.Errorf("SearchCalls returned %v, want a talkgroup reference on its own to leave the search unfiltered", page.Results)
	}
}

func TestSearchCallsRestrictsTheHistoryToTheCallersScope(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeSystem(t, 10002, 1520000)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	fixture.storeCall(t, 10001, 1514001, callHistoryTime().Add(time.Minute))
	allowed := fixture.storeCall(t, 10002, 1520000, callHistoryTime().Add(2*time.Minute))

	scoped, _ := domain.ParseAccessScope([]any{
		map[string]any{"id": float64(10002), "talkgroups": []any{float64(1520000)}},
	})

	page := fixture.search(t, ports.CallSearch{Scope: scoped, Limit: 200, Ascending: true})

	if page.Count != 1 {
		t.Errorf("Count = %d, want the one call the caller is allowed to see, got %v", page.Count, page.Results)
	}

	if len(page.Results) != 1 || page.Results[0].ID != allowed.ID {
		t.Errorf("SearchCalls returned %v, want only call %d", page.Results, allowed.ID)
	}
}

func TestSearchCallsLeavesAWildcardScopeUnfiltered(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeSystem(t, 10002, 1520000)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	fixture.storeCall(t, 10002, 1520000, callHistoryTime().Add(time.Minute))

	wildcard, _ := domain.ParseAccessScope("*")

	page := fixture.search(t, ports.CallSearch{Scope: wildcard, Limit: 200, Ascending: true})

	if page.Count != 2 {
		t.Errorf("Count = %d, want the whole history for a caller allowed to see everything", page.Count)
	}
}

func TestSearchCallsWithAScopeThatNamesNoSystemsSeesNothing(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())

	for _, supplied := range []any{[]any{}, []any{map[string]any{"talkgroups": []any{float64(1514000)}}}} {
		scope, _ := domain.ParseAccessScope(supplied)

		if !scope.Present {
			t.Fatalf("%v is a scope that was supplied, or this test is not testing what it says", supplied)
		}

		page := fixture.search(t, ports.CallSearch{Scope: scope, Limit: 200, Ascending: true})

		if page.Count != 0 || len(page.Results) != 0 {
			t.Errorf("a scope supplied as %v produced %v with count %d, want nothing", supplied, page.Results, page.Count)
		}
	}
}

func TestSearchCallsFiltersByTheTalkgroupsAnExpandedLabelStandsFor(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeSystem(t, 10002, 1520000)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	inGroup := fixture.storeCall(t, 10001, 1514001, callHistoryTime().Add(time.Minute))
	fixture.storeCall(t, 10002, 1520000, callHistoryTime().Add(2*time.Minute))

	for _, tc := range []struct {
		name  string
		pairs []domain.TalkgroupScope
	}{
		{"group", []domain.TalkgroupScope{{SystemRef: 10001, TalkgroupRefs: []uint{1514001, 1514002}}}},
		{"tag", []domain.TalkgroupScope{{SystemRef: 10001, TalkgroupRefs: []uint{1514001}}}},
		{"several systems", []domain.TalkgroupScope{
			{SystemRef: 10002, TalkgroupRefs: []uint{1520000}},
			{SystemRef: 10001, TalkgroupRefs: []uint{1514001}},
		}},
	} {
		page := fixture.search(t, ports.CallSearch{
			Limit:           200,
			Ascending:       true,
			GroupTalkgroups: tc.pairs,
		})

		if tc.name == "several systems" {
			if page.Count != 2 {
				t.Errorf("%s: Count = %d, want the calls on both systems the label stands for, got %v", tc.name, page.Count, page.Results)
			}

			continue
		}

		if page.Count != 1 || len(page.Results) != 1 || page.Results[0].ID != inGroup.ID {
			t.Errorf("%s: SearchCalls returned %v with count %d, want only the talkgroup the label stands for", tc.name, page.Results, page.Count)
		}
	}
}

func TestSearchCallsAddsNoFilterWhenAnExpandedLabelStandsForNothing(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	fixture.storeCall(t, 10001, 1514001, callHistoryTime().Add(time.Minute))

	page := fixture.search(t, ports.CallSearch{Limit: 200, Ascending: true, GroupTalkgroups: []domain.TalkgroupScope{}})

	if page.Count != 2 {
		t.Errorf("Count = %d, want the whole history when the label stands for nothing, got %v", page.Count, page.Results)
	}
}

// TestSearchCallsCombinesTheScopeTheSystemAndTheLabelFilters runs every kind of filter at once,
// with more than one talkgroup in each `IN` list, which is what proves the placeholders built for
// those lists and the values passed with them agree.
func TestSearchCallsCombinesTheScopeTheSystemAndTheLabelFilters(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeSystem(t, 10002, 1520000)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	patched := fixture.storeCall(t, 10001, 1514001, callHistoryTime().Add(time.Minute))
	fixture.storeCall(t, 10002, 1520000, callHistoryTime().Add(2*time.Minute))

	scope, _ := domain.ParseAccessScope([]any{
		map[string]any{"id": float64(10001), "talkgroups": []any{float64(1514000), float64(1514001)}},
		map[string]any{"id": float64(10002), "talkgroups": "*"},
	})

	page := fixture.search(t, ports.CallSearch{
		Scope:           scope,
		SystemRef:       10001,
		Limit:           200,
		Ascending:       true,
		GroupTalkgroups: []domain.TalkgroupScope{{SystemRef: 10001, TalkgroupRefs: []uint{1514001, 1514002}}},
		TagTalkgroups:   []domain.TalkgroupScope{{SystemRef: 10001, TalkgroupRefs: []uint{1514000, 1514001}}},
	})

	if page.Count != 1 || len(page.Results) != 1 || page.Results[0].ID != patched.ID {
		t.Errorf("SearchCalls returned %v with count %d, want only the call every filter allows", page.Results, page.Count)
	}
}

func TestFindDuplicateCandidatesReturnsTheCallsInTheWindowOnTheSameTalkgroup(t *testing.T) {
	fixture := newCallsFixture(t)

	at := callHistoryTime()

	fixture.storeCall(t, 10001, 1514000, at.Add(-2*time.Second))
	earliest := fixture.storeCall(t, 10001, 1514000, at.Add(-time.Second))
	middle := fixture.storeCall(t, 10001, 1514000, at)
	latest := fixture.storeCall(t, 10001, 1514000, at.Add(time.Second))
	fixture.storeCall(t, 10001, 1514000, at.Add(2*time.Second))

	candidates, err := fixture.calls.FindDuplicateCandidates(context.Background(), fixture.systemID(10001), fixture.talkgroupID(1514000), at.Add(-time.Second), at.Add(time.Second))
	if err != nil {
		t.Fatalf("FindDuplicateCandidates: %v", err)
	}

	if got := candidateIDs(candidates); len(got) != 3 || got[0] != earliest.ID || got[1] != middle.ID || got[2] != latest.ID {
		t.Errorf("FindDuplicateCandidates returned %v, want the three calls inside the window, both ends included, ordered by timestamp", got)
	}
}

func TestFindDuplicateCandidatesLeavesOutCallsOnAnotherSystemOrTalkgroup(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeSystem(t, 10002, 1520000)

	at := callHistoryTime()

	fixture.storeCall(t, 10001, 1514001, at)
	fixture.storeCall(t, 10002, 1520000, at)

	candidates, err := fixture.calls.FindDuplicateCandidates(context.Background(), fixture.systemID(10001), fixture.talkgroupID(1514000), at.Add(-time.Second), at.Add(time.Second))
	if err != nil {
		t.Fatalf("FindDuplicateCandidates: %v", err)
	}

	if len(candidates) != 0 {
		t.Errorf("FindDuplicateCandidates returned %v, want nothing: the window is per system and per talkgroup", candidateIDs(candidates))
	}
}

// TestFindDuplicateCandidatesIncludesACallWaitingOutADelay pins the legacy `CheckDuplicate`,
// which counts every call in the window whether or not it has been made visible yet — a call
// that is still waiting is enough to make a new one a duplicate.
func TestFindDuplicateCandidatesIncludesACallWaitingOutADelay(t *testing.T) {
	fixture := newCallsFixture(t)

	at := callHistoryTime()

	waiting := fixture.storeCall(t, 10001, 1514000, at)
	fixture.delay(t, waiting.ID, at.Add(90*time.Minute))

	candidates, err := fixture.calls.FindDuplicateCandidates(context.Background(), fixture.systemID(10001), fixture.talkgroupID(1514000), at.Add(-time.Second), at.Add(time.Second))
	if err != nil {
		t.Fatalf("FindDuplicateCandidates: %v", err)
	}

	if len(candidates) != 1 || candidates[0].ID != waiting.ID {
		t.Errorf("FindDuplicateCandidates returned %v, want the call that is waiting out its delay", candidateIDs(candidates))
	}
}

func TestDeleteCallsBeforeRemovesTheOlderCallsAndReportsHowManyWent(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeCall(t, 10001, 1514000, callHistoryTime())
	fixture.storeCall(t, 10001, 1514000, callHistoryTime().Add(time.Hour))
	kept := fixture.storeCall(t, 10001, 1514000, callHistoryTime().Add(2*time.Hour))

	removed, err := fixture.calls.DeleteCallsBefore(context.Background(), callHistoryTime().Add(90*time.Minute))
	if err != nil {
		t.Fatalf("DeleteCallsBefore: %v", err)
	}

	if removed != 2 {
		t.Errorf("DeleteCallsBefore removed %d calls, want 2", removed)
	}

	if _, err := fixture.calls.GetCall(context.Background(), kept.ID); err != nil {
		t.Errorf("the newer call is gone: %v", err)
	}

	if got := countRows(t, fixture.db, "calls"); got != 1 {
		t.Errorf("the calls table has %d rows, want the one call the prune kept", got)
	}
}

func TestDeleteCallsBeforeKeepsACallRecordedAtTheInstant(t *testing.T) {
	fixture := newCallsFixture(t)

	at := callHistoryTime()

	fixture.storeCall(t, 10001, 1514000, at.Add(-time.Millisecond))
	kept := fixture.storeCall(t, 10001, 1514000, at)

	removed, err := fixture.calls.DeleteCallsBefore(context.Background(), at)
	if err != nil {
		t.Fatalf("DeleteCallsBefore: %v", err)
	}

	if removed != 1 {
		t.Errorf("DeleteCallsBefore removed %d calls, want the one older than the instant", removed)
	}

	if _, err := fixture.calls.GetCall(context.Background(), kept.ID); err != nil {
		t.Errorf("a call recorded exactly at the instant was pruned: %v", err)
	}
}

// TestDeleteCallsBeforeLeavesTheChildRowsToTheCascadingForeignKeys pins that the prune writes
// the calls table only: the schema's cascading keys take the frequency, patch, unit, and delayed
// rows with their call.
func TestDeleteCallsBeforeLeavesTheChildRowsToTheCascadingForeignKeys(t *testing.T) {
	fixture := newCallsFixture(t)

	call := fixture.newCall(10001, 1514000, callHistoryTime())
	call.Frequencies = []domain.CallFrequency{{Frequency: 1514000000}}
	call.PatchTalkgroupRefs = []uint{1514001}
	call.Units = []domain.CallUnit{{UnitRef: 12345}}

	saved, err := fixture.calls.SaveCall(context.Background(), call)
	if err != nil {
		t.Fatalf("SaveCall: %v", err)
	}

	fixture.delay(t, saved.ID, callHistoryTime().Add(90*time.Minute))

	removed, err := fixture.calls.DeleteCallsBefore(context.Background(), callHistoryTime().Add(time.Hour))
	if err != nil {
		t.Fatalf("DeleteCallsBefore: %v", err)
	}

	if removed != 1 {
		t.Fatalf("DeleteCallsBefore removed %d calls, want 1", removed)
	}

	for _, table := range []string{"callFrequencies", "callPatches", "callUnits", "delayed"} {
		if got := countRows(t, fixture.db, table); got != 0 {
			t.Errorf("%s has %d rows after the prune, want the cascade to have taken them with the call", table, got)
		}
	}
}

// testAudio is a recording in miniature, with the byte values a real one has: a NUL, a quote, and
// bytes above 0x7f, so a blob that survives the round trip survived all of them.
var testAudio = []byte{
	'R', 'I', 'F', 'F', 0x00, 0x24, 0x00, 0x00,
	'W', 'A', 'V', 'E', '\'', 0x00, 0x80, 0xff,
	0x7f, 0x81, 0x00, 0x00, 0xfe, 0xfe, 0xff, 0xff,
}

func callHistoryTime() time.Time {
	return time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)
}

// callsFixture is a fresh database with the legacy schema applied and the configuration a call
// needs already stored. The call tables have foreign keys into the systems and talkgroups tables,
// and those belong to a later task, so the rows are stored directly here.
type callsFixture struct {
	db         *sql.DB
	calls      *Calls
	systems    map[uint]uint64
	talkgroups map[uint]uint64
	tagID      int64
}

func newCallsFixture(t *testing.T) *callsFixture {
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

	fixture := &callsFixture{
		db:         db,
		calls:      NewCalls(db, DialectSqlite),
		systems:    map[uint]uint64{},
		talkgroups: map[uint]uint64{},
	}

	fixture.storeSystem(t, 10001, 1514000, 1514001, 1514002)

	return fixture
}

// storeSystem stores one system and the talkgroups on it, and remembers the identifiers the
// tables assigned.
func (fixture *callsFixture) storeSystem(t *testing.T, systemRef uint, talkgroupRefs ...uint) {
	t.Helper()

	tagID := fixture.storeTag(t)

	res, err := fixture.db.Exec(`INSERT INTO "systems" ("label", "systemRef") VALUES (?, ?)`, fmt.Sprintf("System %d", systemRef), systemRef)
	if err != nil {
		t.Fatalf("storing a system: %v", err)
	}

	systemID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("reading the identifier the systems table assigned: %v", err)
	}

	fixture.systems[systemRef] = uint64(systemID)

	for _, ref := range talkgroupRefs {
		res, err := fixture.db.Exec(
			`INSERT INTO "talkgroups" ("label", "name", "systemId", "tagId", "talkgroupRef") VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("%d", ref), fmt.Sprintf("Talkgroup %d", ref), systemID, tagID, ref,
		)
		if err != nil {
			t.Fatalf("storing a talkgroup: %v", err)
		}

		talkgroupID, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("reading the identifier the talkgroups table assigned: %v", err)
		}

		fixture.talkgroups[ref] = uint64(talkgroupID)
	}
}

// storeTag stores the tag every talkgroup needs, once per fixture.
func (fixture *callsFixture) storeTag(t *testing.T) int64 {
	t.Helper()

	if fixture.tagID != 0 {
		return fixture.tagID
	}

	res, err := fixture.db.Exec(`INSERT INTO "tags" ("label") VALUES (?)`, domain.DefaultTagLabel)
	if err != nil {
		t.Fatalf("storing a tag: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("reading the identifier the tags table assigned: %v", err)
	}

	fixture.tagID = id

	return id
}

func (fixture *callsFixture) systemID(systemRef uint) uint64 {
	return fixture.systems[systemRef]
}

func (fixture *callsFixture) talkgroupID(talkgroupRef uint) uint64 {
	return fixture.talkgroups[talkgroupRef]
}

// newCall builds a call attached to stored system and talkgroup rows, with audio but no children.
func (fixture *callsFixture) newCall(systemRef, talkgroupRef uint, at time.Time) domain.Call {
	call := domain.NewCall()

	call.Audio = testAudio
	call.AudioFilename = "audio.wav"
	call.Timestamp = at
	call.System = domain.System{ID: fixture.systemID(systemRef), SystemRef: systemRef}
	call.Talkgroup = domain.Talkgroup{ID: fixture.talkgroupID(talkgroupRef), TalkgroupRef: talkgroupRef}

	return call
}

// storeCall writes one call through the adapter and returns it with its assigned identity.
func (fixture *callsFixture) storeCall(t *testing.T, systemRef, talkgroupRef uint, at time.Time) domain.Call {
	t.Helper()

	saved, err := fixture.calls.SaveCall(context.Background(), fixture.newCall(systemRef, talkgroupRef, at))
	if err != nil {
		t.Fatalf("SaveCall: %v", err)
	}

	return saved
}

// delay puts a stored call in the delayed table. That table is the delayer's own repository,
// which is a later task, so the row is stored directly here.
func (fixture *callsFixture) delay(t *testing.T, callID uint64, until time.Time) {
	t.Helper()

	if _, err := fixture.db.Exec(`INSERT INTO "delayed" ("callId", "timestamp") VALUES (?, ?)`, callID, millisOf(until)); err != nil {
		t.Fatalf("storing a delayed call: %v", err)
	}
}

func (fixture *callsFixture) search(t *testing.T, search ports.CallSearch) ports.CallSearchPage {
	t.Helper()

	page, err := fixture.calls.SearchCalls(context.Background(), search)
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	return page
}

// storedAudioBlob reads a call's audio straight out of the database, as the reference the adapter
// is compared against.
func storedAudioBlob(t *testing.T, db *sql.DB, id uint64) []byte {
	t.Helper()

	var audio []byte

	if err := db.QueryRow(`SELECT "audio" FROM "calls" WHERE "callId" = ?`, id).Scan(&audio); err != nil {
		t.Fatalf("reading the stored audio: %v", err)
	}

	return audio
}

// storedAggregates reads the identifiers the fixture's system and talkgroup rows were stored
// under, so a test can name them by the references the client uses.
func storedAggregates(t *testing.T, db *sql.DB, systemRef, talkgroupRef uint) (systemID, talkgroupID uint64) {
	t.Helper()

	if err := db.QueryRow(`SELECT c."systemId", c."talkgroupId" FROM "calls" AS c JOIN "systems" AS s ON s."systemId" = c."systemId" JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" WHERE s."systemRef" = ? AND t."talkgroupRef" = ? LIMIT 1`, systemRef, talkgroupRef).Scan(&systemID, &talkgroupID); err != nil {
		t.Fatalf("reading the aggregates a stored call belongs to: %v", err)
	}

	return systemID, talkgroupID
}

func resultIDs(page ports.CallSearchPage) []uint64 {
	ids := []uint64{}

	for _, result := range page.Results {
		ids = append(ids, result.ID)
	}

	return ids
}

func candidateIDs(candidates []domain.Call) []uint64 {
	ids := []uint64{}

	for _, call := range candidates {
		ids = append(ids, call.ID)
	}

	return ids
}
