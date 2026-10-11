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

package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

var ingestTime = time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

type ingestFixture struct {
	calls     *fakes.Calls
	systems   *fakes.Systems
	groups    *fakes.Groups
	tags      *fakes.Tags
	options   *fakes.Options
	logs      *fakes.Logs
	converter *fakes.Converter
	hub       *fakes.Hub
	delayer   *fakes.Delayer
	clock     *fakes.Clock

	downstreams *fakes.Downstreams
	forwarder   *fakes.Forwarder

	handler *IngestCallHandler
}

func newIngestFixture() *ingestFixture {
	fixture := &ingestFixture{
		calls:   fakes.NewCalls(),
		systems: &fakes.Systems{},
		groups: &fakes.Groups{Stored: domain.Groups{
			{ID: 1, Label: "Fire"},
			{ID: 2, Label: "EMS"},
		}},
		tags: &fakes.Tags{Stored: domain.Tags{
			{ID: 10, Label: "Untagged"},
		}},
		options:     &fakes.Options{Stored: domain.DefaultOptions()},
		logs:        &fakes.Logs{},
		converter:   fakes.NewConverter(),
		hub:         fakes.NewHub(),
		delayer:     &fakes.Delayer{Now: ingestTime},
		clock:       fakes.NewClock(ingestTime),
		downstreams: &fakes.Downstreams{},
		forwarder:   &fakes.Forwarder{},
	}

	fixture.handler = NewIngestCallHandler(
		fixture.calls,
		fixture.systems,
		fixture.groups,
		fixture.tags,
		fixture.options,
		fixture.logs,
		fixture.converter,
		fixture.hub,
		fixture.delayer,
		fixture.downstreams,
		fixture.forwarder,
		fixture.clock,
	)

	return fixture
}

// seedKnownSystem installs a system the server already stores, with one talkgroup on it.
func (fixture *ingestFixture) seedKnownSystem() domain.System {
	system := domain.System{
		ID:        1,
		SystemRef: 1,
		Label:     "Test System",
	}

	system.Talkgroups = domain.Talkgroups{
		{ID: 10, TalkgroupRef: 123, Label: "Test Talkgroup", Name: "Test Talkgroup", TagID: 10},
	}

	fixture.systems.Stored = append(fixture.systems.Stored, system)

	return system
}

func validIngestCall() domain.Call {
	call := domain.NewCall()

	call.Audio = make([]byte, 45)
	call.AudioFilename = "test.wav"
	call.AudioMime = "audio/wav"
	call.Timestamp = ingestTime

	return call
}

// listen opens a listener that has asked for the fixture's system and talkgroup, which is what a
// delivered call has to reach.
func (fixture *ingestFixture) listen() {
	fixture.hub.Open("listener", "10.0.0.1")
	fixture.hub.SetLivefeed("listener", domain.ParseLivefeed(map[string]any{
		"1": map[string]any{"123": true},
	}))
}

func (fixture *ingestFixture) ingest(call domain.Call) (IngestCallResult, error) {
	fixture.listen()

	return fixture.handler.Handle(context.Background(), IngestCallCommand{Call: call})
}

func (fixture *ingestFixture) logMessages() []string {
	out := []string{}

	for _, entry := range fixture.logs.Recorded() {
		out = append(out, entry.Message)
	}

	return out
}

// configEntries returns what the ingest wrote about the administrator's configuration: the things this
// recording created that were not there before. A call that created nothing has nothing to say, so the
// tests that expect silence read this.
func (fixture *ingestFixture) configEntries() []string {
	out := []string{}

	for _, entry := range fixture.logs.Recorded() {
		if entry.Source == domain.LogSourceConfig {
			out = append(out, entry.Message)
		}
	}

	return out
}

// reposted returns what the downstream fan-out re-posted.
//
// The fan-out runs on its own goroutine — that is what it is for — so the ingest returning
// does not mean it has finished. It only touches memory in these tests, so it counts as
// finished once its count has stopped growing.
func (fixture *ingestFixture) reposted() []fakes.ForwardedCall {
	deadline := time.Now().Add(2 * time.Second)

	current := fixture.forwarder.Reposts()
	stable := 0

	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)

		next := fixture.forwarder.Reposts()

		if len(next) != len(current) {
			current, stable = next, 0

			continue
		}

		stable++

		if stable >= 5 {
			return next
		}
	}

	return current
}

// loggedAs reports whether the ingest wrote an entry naming that part of the server and saying the
// fragment. The ingest path writes under four different sources — the call decisions under ingest, the
// conversion under ffmpeg, the hold under delayer, the re-posts under downstream — so the source is part
// of what these tests pin.
func (fixture *ingestFixture) loggedAs(source domain.LogSource, fragment string) bool {
	for _, entry := range fixture.logs.Recorded() {
		if entry.Source == source && strings.Contains(entry.Message, fragment) {
			return true
		}
	}

	return false
}

// loggedAsEventually is loggedAs for the downstream fan-out, which writes on its own goroutine.
func (fixture *ingestFixture) loggedAsEventually(source domain.LogSource, fragment string) bool {
	deadline := time.Now().Add(2 * time.Second)

	for {
		for _, entry := range fixture.logs.Recorded() {
			if entry.Source == source && strings.Contains(entry.Message, fragment) {
				return true
			}
		}

		if time.Now().After(deadline) {
			return false
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func storedSystemByRef(systems domain.Systems, ref uint) (domain.System, bool) {
	for _, system := range systems {
		if system.SystemRef == ref {
			return system, true
		}
	}

	return domain.System{}, false
}

func storedUnitByRef(system domain.System, ref uint) (domain.Unit, bool) {
	for _, unit := range system.Units {
		if unit.UnitRef == ref {
			return unit, true
		}
	}

	return domain.Unit{}, false
}

// The five cases ported from the legacy ingest tests
// (`server/controller_test.go`), which run against a real temporary database. Here they run
// against the in-memory repositories, which is what makes the rules they pin testable
// without a database.

func TestIngestCallAddsOneLabeledUnit(t *testing.T) {
	fixture := newIngestFixture()
	fixture.options.Stored.AutoPopulate = false

	system := fixture.seedKnownSystem()
	system.AutoPopulate = true
	fixture.systems.Stored[0] = system

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]
	call.Meta.UnitRefs = []uint{710618}
	call.Meta.UnitLabels = []string{"Gastonia PD Dispatch"}

	if _, err := fixture.ingest(call); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	stored, ok := storedSystemByRef(fixture.systems.Stored, 1)
	if !ok {
		t.Fatal("the ingested system was not reloaded")
	}

	unit, ok := storedUnitByRef(stored, 710618)
	if !ok {
		t.Fatalf("unit 710618 was not added: %+v", stored.Units)
	}

	if unit.Label != "Gastonia PD Dispatch" {
		t.Errorf("unit label = %q, want the label the ingest source claimed", unit.Label)
	}

	got := fixture.configEntries()

	want := []string{
		`the recording test.wav added the unit "Gastonia PD Dispatch" (unit 710618) to the system "Test System"`,
	}

	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("the configuration entries are %v, want %v", got, want)
	}
}

func TestIngestCallAddsTwoLabeledUnits(t *testing.T) {
	fixture := newIngestFixture()
	fixture.options.Stored.AutoPopulate = false

	system := fixture.seedKnownSystem()
	system.AutoPopulate = true
	fixture.systems.Stored[0] = system

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]
	call.Meta.UnitRefs = []uint{100, 200}
	call.Meta.UnitLabels = []string{"Engine 1", "Engine 2"}

	if _, err := fixture.ingest(call); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	stored, _ := storedSystemByRef(fixture.systems.Stored, 1)

	want := map[uint]string{100: "Engine 1", 200: "Engine 2"}

	for ref, label := range want {
		unit, ok := storedUnitByRef(stored, ref)
		if !ok {
			t.Fatalf("unit %d was not added: %+v", ref, stored.Units)
		}

		if unit.Label != label {
			t.Errorf("unit %d label = %q, want %q", ref, unit.Label, label)
		}
	}

	entries := fixture.configEntries()

	expected := []string{
		`the recording test.wav added the unit "Engine 1" (unit 100) to the system "Test System"`,
		`the recording test.wav added the unit "Engine 2" (unit 200) to the system "Test System"`,
	}

	if len(entries) != len(expected) {
		t.Fatalf("the configuration entries are %v, want %v", entries, expected)
	}

	for i, message := range expected {
		if entries[i] != message {
			t.Errorf("configuration entry %d is %q, want %q", i+1, entries[i], message)
		}
	}
}

func TestIngestCallMissingLabelDoesNotCreateBogusUnit(t *testing.T) {
	fixture := newIngestFixture()
	fixture.options.Stored.AutoPopulate = false

	system := fixture.seedKnownSystem()
	system.AutoPopulate = true
	fixture.systems.Stored[0] = system

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]
	call.Meta.UnitRefs = []uint{100, 200}
	call.Meta.UnitLabels = []string{"Engine 1"}

	if _, err := fixture.ingest(call); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	stored, _ := storedSystemByRef(fixture.systems.Stored, 1)

	unit, ok := storedUnitByRef(stored, 100)
	if !ok || unit.Label != "Engine 1" {
		t.Fatalf("the labeled unit is missing or wrong: %+v", stored.Units)
	}

	if _, ok := storedUnitByRef(stored, 200); ok {
		t.Errorf("a reference with no label created a unit: %+v", stored.Units)
	}
}

func TestIngestCallPreservesExistingUnit(t *testing.T) {
	fixture := newIngestFixture()
	fixture.options.Stored.AutoPopulate = false

	system := fixture.seedKnownSystem()
	system.AutoPopulate = true
	system.Units = domain.Units{{ID: 50, SystemID: 1, UnitRef: 100, Label: "Existing Engine"}}
	fixture.systems.Stored[0] = system

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]
	call.Meta.UnitRefs = []uint{100}
	call.Meta.UnitLabels = []string{"Existing Engine"}

	if _, err := fixture.ingest(call); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	stored, _ := storedSystemByRef(fixture.systems.Stored, 1)

	if len(stored.Units) != 1 {
		t.Fatalf("the existing unit was duplicated: %+v", stored.Units)
	}

	if stored.Units[0].UnitRef != 100 || stored.Units[0].Label != "Existing Engine" {
		t.Errorf("the existing unit changed: %+v", stored.Units[0])
	}
}

func TestIngestCallRepairsGeneratedTalkgroupLabel(t *testing.T) {
	fixture := newIngestFixture()
	fixture.options.Stored.AutoPopulate = false

	system := domain.System{
		ID:           1,
		SystemRef:    4,
		Label:        "Gaston County Conventional",
		AutoPopulate: true,
	}

	system.Talkgroups = domain.Talkgroups{
		{ID: 10, TalkgroupRef: 1514000, Label: "1514000", Name: "Talkgroup 1514000"},
	}

	fixture.systems.Stored = append(fixture.systems.Stored, system)

	call := validIngestCall()
	call.AudioFilename = "conventional.wav"
	call.System = system
	call.Talkgroup = system.Talkgroups[0]
	call.Meta.TalkgroupLabel = "GASTON-FIRE"

	if _, err := fixture.handler.Handle(context.Background(), IngestCallCommand{Call: call}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	stored, ok := storedSystemByRef(fixture.systems.Stored, 4)
	if !ok {
		t.Fatal("the ingested system was not reloaded")
	}

	talkgroup, ok := stored.Talkgroups.ByRef(1514000)
	if !ok {
		t.Fatalf("the talkgroup is gone: %+v", stored.Talkgroups)
	}

	if talkgroup.Label != "GASTON-FIRE" {
		t.Errorf("talkgroup label = %q, want the claimed label to repair the generated placeholder", talkgroup.Label)
	}

	got := fixture.configEntries()

	want := []string{
		`the recording conventional.wav replaced the placeholder talkgroup 1514000 with "GASTON-FIRE" on the ` +
			`system "Gaston County Conventional"`,
	}

	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("the configuration entries are %v, want %v", got, want)
	}
}

func TestIngestCallNeverRepairsAConfiguredLabel(t *testing.T) {
	fixture := newIngestFixture()
	fixture.options.Stored.AutoPopulate = false

	system := domain.System{ID: 1, SystemRef: 4, AutoPopulate: true}
	system.Talkgroups = domain.Talkgroups{{ID: 10, TalkgroupRef: 1514000, Label: "Dispatch 1", Name: "Dispatch 1"}}
	fixture.systems.Stored = append(fixture.systems.Stored, system)

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]
	call.Meta.TalkgroupLabel = "Something Else"

	if _, err := fixture.handler.Handle(context.Background(), IngestCallCommand{Call: call}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	stored, _ := storedSystemByRef(fixture.systems.Stored, 4)

	if talkgroup, _ := stored.Talkgroups.ByRef(1514000); talkgroup.Label != "Dispatch 1" {
		t.Errorf("talkgroup label = %q, want the configured label left alone", talkgroup.Label)
	}

	if got := fixture.configEntries(); len(got) != 0 {
		t.Errorf("the configuration entries are %v, want nothing: a label the server was not allowed to "+
			"change is not a change", got)
	}
}

// TestACallThatCreatesNothingSaysNothingAboutTheConfiguration is the other half of the requirement. An
// ordinary call against a configuration that already knows everything it names changes nothing, and an
// entry for it would teach an operator to ignore the entries that mean something.
func TestACallThatCreatesNothingSaysNothingAboutTheConfiguration(t *testing.T) {
	fixture := newIngestFixture()
	fixture.options.Stored.AutoPopulate = false

	system := fixture.seedKnownSystem()
	system.Units = domain.Units{{ID: 50, SystemID: 1, UnitRef: 100, Label: "Engine 1"}}
	fixture.systems.Stored[0] = system

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]
	call.Meta.UnitRefs = []uint{100}
	call.Meta.UnitLabels = []string{"Engine 1"}

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Fatalf("outcome = %q, want %q", result.Outcome, IngestCallAccepted)
	}

	if got := fixture.configEntries(); len(got) != 0 {
		t.Errorf("the configuration entries are %v, want nothing: the call named a system, a talkgroup and "+
			"a unit the server already had", got)
	}

	if !fixture.loggedAs(domain.LogSourceIngest, "success") {
		t.Errorf("the call's own entry is missing from %v", fixture.logMessages())
	}
}

func TestIngestCallRejectsAnIncompleteCallBeforeDoingAnything(t *testing.T) {
	fixture := newIngestFixture()

	call := validIngestCall()
	call.Audio = make([]byte, 44)

	result, err := fixture.ingest(call)

	if result.Outcome != IngestCallIncomplete {
		t.Errorf("outcome = %q, want %q", result.Outcome, IngestCallIncomplete)
	}

	if err == nil {
		t.Error("an incomplete call must report why it is incomplete, which is what the upload endpoint answers with")
	}

	if len(fixture.calls.Saved) != 0 {
		t.Error("an incomplete call must not be stored")
	}

	if len(fixture.logs.Stored) != 0 {
		t.Error("an incomplete call is refused by the ingest boundary, not logged as an ingest decision")
	}
}

func TestIngestCallRefusesABlacklistedTalkgroup(t *testing.T) {
	fixture := newIngestFixture()

	system := fixture.seedKnownSystem()
	system.Blacklists = "123"
	fixture.systems.Stored[0] = system

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallBlacklisted {
		t.Errorf("outcome = %q, want %q", result.Outcome, IngestCallBlacklisted)
	}

	if len(fixture.calls.Saved) != 0 {
		t.Error("a blacklisted call must not be stored")
	}

	if !fixture.loggedAs(domain.LogSourceIngest, "call from system 1 talkgroup 123 blacklisted, test.wav") {
		t.Errorf("logs = %v, want the blacklisted decision recorded, naming the call it is about", fixture.logMessages())
	}
}

func TestIngestCallReportsUnknownAggregatesWhenAutoPopulateIsOff(t *testing.T) {
	fixture := newIngestFixture()
	fixture.options.Stored.AutoPopulate = false

	call := validIngestCall()
	call.Meta.SystemLabel = "A System Nobody Configured"
	call.Meta.TalkgroupRef = 1514000

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallUnknown {
		t.Errorf("outcome = %q, want %q", result.Outcome, IngestCallUnknown)
	}

	if len(fixture.calls.Saved) != 0 {
		t.Error("a call the server cannot place must not be stored")
	}

	if !fixture.loggedAs(domain.LogSourceIngest, "the recording test.wav does not match any system or talkgroup the server knows") {
		t.Errorf("logs = %v, want the call the server could not place, named by the recording it came from", fixture.logMessages())
	}

	if len(fixture.systems.Stored) != 0 {
		t.Error("auto-populate being off must not invent a system")
	}
}

func TestIngestCallRejectsADuplicateWithinTheWindow(t *testing.T) {
	fixture := newIngestFixture()

	system := fixture.seedKnownSystem()

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]

	earlier := validIngestCall()
	earlier.ID = 900
	earlier.System = system
	earlier.Talkgroup = system.Talkgroups[0]
	earlier.Timestamp = ingestTime.Add(-300 * time.Millisecond)

	fixture.calls.Stored[900] = earlier

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallDuplicate {
		t.Errorf("outcome = %q, want %q", result.Outcome, IngestCallDuplicate)
	}

	if len(fixture.calls.Saved) != 0 {
		t.Error("a duplicate call must not be stored")
	}

	if !fixture.loggedAs(domain.LogSourceIngest, "call from system 1 talkgroup 123 duplicate call rejected, test.wav") {
		t.Errorf("logs = %v, want the duplicate decision recorded, naming the call it is about", fixture.logMessages())
	}

	if len(fixture.converter.Modes) != 0 {
		t.Error("a duplicate is rejected before its audio is converted")
	}
}

// TestIngestCallStoresTheSameCallOnADifferentSite pins the site rule at the ingest seam:
// the same talkgroup heard on two sites is two real calls, so the second one is stored.
func TestIngestCallStoresTheSameCallOnADifferentSite(t *testing.T) {
	fixture := newIngestFixture()

	system := fixture.seedKnownSystem()

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]
	call.SiteRef = 2

	earlier := validIngestCall()
	earlier.ID = 900
	earlier.System = system
	earlier.Talkgroup = system.Talkgroups[0]
	earlier.SiteRef = 1
	earlier.Timestamp = ingestTime.Add(-300 * time.Millisecond)

	fixture.calls.Stored[900] = earlier

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Errorf("outcome = %q, want %q: the same talkgroup on another site is a real call", result.Outcome, IngestCallAccepted)
	}

	if len(fixture.calls.Saved) != 1 {
		t.Error("the call on the other site must be stored")
	}
}

func TestIngestCallStoresASecondCallWhenDuplicateDetectionIsDisabled(t *testing.T) {
	fixture := newIngestFixture()
	fixture.options.Stored.DisableDuplicateDetection = true

	system := fixture.seedKnownSystem()

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]

	earlier := validIngestCall()
	earlier.ID = 900
	earlier.System = system
	earlier.Talkgroup = system.Talkgroups[0]
	earlier.Timestamp = ingestTime

	fixture.calls.Stored[900] = earlier

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Errorf("outcome = %q, want %q", result.Outcome, IngestCallAccepted)
	}

	if len(fixture.calls.Saved) != 1 {
		t.Errorf("stored %d calls, want the identical call stored as well", len(fixture.calls.Saved))
	}
}

func TestIngestCallStoresAndDeliversAKnownCall(t *testing.T) {
	fixture := newIngestFixture()

	system := fixture.seedKnownSystem()

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Errorf("outcome = %q, want %q", result.Outcome, IngestCallAccepted)
	}

	if result.CallID == 0 {
		t.Error("an accepted call must report the identifier it was stored under")
	}

	stored := fixture.calls.Saved[0]

	if stored.System.ID != 1 || stored.Talkgroup.ID != 10 {
		t.Errorf("stored call is attached to system %d talkgroup %d, want the stored aggregates", stored.System.ID, stored.Talkgroup.ID)
	}

	if stored.AudioFilename != "test.m4a" || stored.AudioMime != "audio/mp4" {
		t.Errorf("stored audio = %q %q, want the converted name and media type", stored.AudioFilename, stored.AudioMime)
	}

	if !fixture.loggedAs(domain.LogSourceIngest, "call from system 1 talkgroup 123 success, test.m4a") {
		t.Errorf("logs = %v, want the accepted call recorded, naming the call and the audio it was stored with", fixture.logMessages())
	}

	if len(fixture.hub.DeliveredCalls["listener"]) != 1 {
		t.Error("an accepted call must be delivered to a listener that asked for it")
	}
}

// TestIngestCallReportsACallItCouldNotStore is the one ingest line that carries the failure's own text:
// the repository refused the write, and the statement that failed is the part an operator needs.
func TestIngestCallReportsACallItCouldNotStore(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.calls.Err = errors.New(`calls.save: no such table: calls in INSERT INTO "calls" ("systemId", "talkgroupId") VALUES (1, 10)`)

	call := validIngestCall()
	call.System = fixture.systems.Stored[0]
	call.Talkgroup = fixture.systems.Stored[0].Talkgroups[0]

	result, err := fixture.ingest(call)
	if err == nil {
		t.Fatalf("ingest returned %v, want the failure the repository reported", result)
	}

	if result.Outcome != IngestCallFailed {
		t.Errorf("outcome = %q, want %q", result.Outcome, IngestCallFailed)
	}

	if !fixture.loggedAs(domain.LogSourceIngest, `the call could not be ingested: calls.save: no such table: calls in INSERT INTO "calls" ("systemId", "talkgroupId") VALUES (1, 10)`) {
		t.Errorf("logs = %v, want what the server tried to do and the failure's own text after it", fixture.logMessages())
	}
}

func TestIngestCallKeepsTheOriginalAudioWhenConversionFails(t *testing.T) {
	fixture := newIngestFixture()
	fixture.converter.Err = errors.New("ffmpeg is not available, no audio conversion will be performed")

	system := fixture.seedKnownSystem()

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Errorf("outcome = %q, want %q: a conversion failure must not lose the call", result.Outcome, IngestCallAccepted)
	}

	stored := fixture.calls.Saved[0]

	if stored.AudioFilename != "test.wav" {
		t.Errorf("stored audio filename = %q, want the original audio kept", stored.AudioFilename)
	}

	if !fixture.loggedAs(domain.LogSourceFFmpeg, "the recording could not be converted: ffmpeg is not available, no audio conversion will be performed") {
		t.Errorf("logs = %v, want the conversion failure recorded under the converter", fixture.logMessages())
	}
}

func TestIngestCallLeavesAudioAloneWhenConversionIsDisabled(t *testing.T) {
	fixture := newIngestFixture()
	fixture.options.Stored.AudioConversion = domain.AudioConversionDisabled

	system := fixture.seedKnownSystem()

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]

	if _, err := fixture.ingest(call); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if len(fixture.converter.Modes) != 0 {
		t.Error("with conversion disabled the converter must not be invoked at all")
	}

	if fixture.calls.Saved[0].AudioFilename != "test.wav" {
		t.Errorf("stored audio filename = %q, want the recorded audio kept", fixture.calls.Saved[0].AudioFilename)
	}
}

func TestIngestCallAutoPopulatesSystemTalkgroupGroupAndTag(t *testing.T) {
	fixture := newIngestFixture()

	call := validIngestCall()
	call.Meta.SystemLabel = "Gaston County Conventional"
	call.Meta.TalkgroupRef = 1514000
	call.Meta.TalkgroupLabel = "GASTON-FIRE"
	call.Meta.TalkgroupGroup = []string{"Unknown"}
	call.Meta.TalkgroupTag = "Fire"

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Fatalf("outcome = %q, want %q", result.Outcome, IngestCallAccepted)
	}

	if len(fixture.systems.Stored) != 1 {
		t.Fatalf("stored %d systems, want the claimed system invented", len(fixture.systems.Stored))
	}

	system := fixture.systems.Stored[0]

	if system.SystemRef != 1 {
		t.Errorf("system reference = %d, want the next free reference", system.SystemRef)
	}

	if system.Label != "Gaston County Conventional" {
		t.Errorf("system label = %q, want the claimed label", system.Label)
	}

	talkgroup, ok := system.Talkgroups.ByRef(1514000)
	if !ok {
		t.Fatalf("talkgroup was not created: %+v", system.Talkgroups)
	}

	if talkgroup.Label != "GASTON-FIRE" {
		t.Errorf("talkgroup label = %q, want the claimed label", talkgroup.Label)
	}

	if talkgroup.Name != "Talkgroup 1514000" {
		t.Errorf("talkgroup name = %q, want the generated name", talkgroup.Name)
	}

	if talkgroup.TagID != 11 {
		t.Errorf("talkgroup tag = %d, want the tag auto-populate created for it", talkgroup.TagID)
	}

	if len(talkgroup.GroupIDs) != 1 || talkgroup.GroupIDs[0] != 3 {
		t.Errorf("talkgroup groups = %v, want the group auto-populate created for it", talkgroup.GroupIDs)
	}

	if len(fixture.groups.Stored) != 3 || fixture.groups.Stored[2].Label != "Unknown" {
		t.Errorf("groups = %+v, want the claimed group added to the configured ones", fixture.groups.Stored)
	}

	if len(fixture.tags.Stored) != 2 || fixture.tags.Stored[1].Label != "Fire" {
		t.Errorf("tags = %+v, want the claimed tag added to the configured ones", fixture.tags.Stored)
	}

	if len(fixture.hub.BroadcastConfigs) != 1 {
		t.Error("auto-populate must broadcast the new configuration before the call is delivered")
	}

	// Auto-populate changed four things in the administrator's configuration, and each one is an entry
	// naming the recording that caused it. A system appearing in the configuration that nobody created is
	// a support request this product gets, and the recording that named it is the thing an operator has to
	// be able to find.
	want := []string{
		`the recording test.wav created the system "Gaston County Conventional" (system 1)`,
		`the recording test.wav created the group "Unknown"`,
		`the recording test.wav created the tag "Fire"`,
		`the recording test.wav created the talkgroup "GASTON-FIRE" (talkgroup 1514000) on the system ` +
			`"Gaston County Conventional"`,
	}

	got := fixture.configEntries()

	if len(got) != len(want) {
		t.Fatalf("the configuration entries are %v, want %v", got, want)
	}

	for i, message := range want {
		if got[i] != message {
			t.Errorf("configuration entry %d is %q, want %q", i+1, got[i], message)
		}
	}
}

func TestIngestCallUsesTheClaimedSystemReferenceRatherThanTheNextFreeOne(t *testing.T) {
	fixture := newIngestFixture()

	call := validIngestCall()
	call.Meta.SystemRef = 42
	call.Meta.TalkgroupRef = 1514000

	result, err := fixture.handler.Handle(context.Background(), IngestCallCommand{Call: call})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Fatalf("outcome = %q, want %q", result.Outcome, IngestCallAccepted)
	}

	system := fixture.systems.Stored[0]

	if system.SystemRef != 42 {
		t.Errorf("system reference = %d, want the reference the ingest source claimed", system.SystemRef)
	}

	if system.Label != "System 42" {
		t.Errorf("system label = %q, want the generated placeholder", system.Label)
	}

	talkgroup, _ := system.Talkgroups.ByRef(1514000)

	if talkgroup.Label != "1514000" {
		t.Errorf("talkgroup label = %q, want the numeric placeholder the legacy server generates", talkgroup.Label)
	}
}

func TestIngestCallResolvesByStoredIdentifiersBeforeLabels(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	other := domain.System{ID: 2, SystemRef: 9, Label: "Test System"}
	other.Talkgroups = domain.Talkgroups{{ID: 20, TalkgroupRef: 123}}
	fixture.systems.Stored = append(fixture.systems.Stored, other)

	call := validIngestCall()
	call.Meta.SystemID = 1
	call.Meta.TalkgroupID = 10
	call.Meta.SystemLabel = "Test System"

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Fatalf("outcome = %q, want %q", result.Outcome, IngestCallAccepted)
	}

	stored := fixture.calls.Saved[0]

	if stored.System.ID != 1 {
		t.Errorf("call was recorded against system %d, want the identifier the source claimed rather than the ambiguous label", stored.System.ID)
	}

	if stored.Talkgroup.ID != 10 {
		t.Errorf("call was recorded against talkgroup %d, want the identifier the source claimed", stored.Talkgroup.ID)
	}
}

func TestIngestCallReportsFailureWhenARepositoryFails(t *testing.T) {
	fixture := newIngestFixture()
	fixture.systems.Err = errors.New("database is locked")

	call := validIngestCall()
	call.Meta.SystemLabel = "Test System"
	call.Meta.TalkgroupRef = 123

	result, err := fixture.ingest(call)

	if result.Outcome != IngestCallFailed {
		t.Errorf("outcome = %q, want %q", result.Outcome, IngestCallFailed)
	}

	if err == nil {
		t.Error("a repository failure must reach the caller")
	}
}

// The server stores one call at a time.
//
// The legacy server gets this for free: every call — from a watched directory, from an upload, from a
// downstream server — goes to one queue with one worker, so a call is never compared against the history
// before an earlier call has been written. That is what makes its duplicate detection mean anything, and
// what makes two calls naming a system the server does not know create that system once and not twice.
//
// Here the watchers run on their own goroutines and an upload is answered on the connection that posted
// it, so the rule has to be held on purpose.
func TestIngestCallHandlerStoresOneCallAtATime(t *testing.T) {
	fixture := newIngestFixture()
	fixture.seedKnownSystem()

	// The store signals every write it begins and holds the first one open, so that a second write
	// starting while the first is still open is something the test can see rather than infer.
	fixture.calls.Entered = make(chan struct{}, 8)
	fixture.calls.Hold = make(chan struct{})

	first := validIngestCall()
	first.AudioFilename = "first.wav"
	first.Meta.SystemRef = 1
	first.Meta.TalkgroupRef = 123

	second := validIngestCall()
	second.AudioFilename = "second.wav"
	second.Timestamp = ingestTime.Add(time.Second)
	second.Meta.SystemRef = 1
	second.Meta.TalkgroupRef = 123

	firstDone := make(chan IngestCallResult, 1)

	go func() {
		result, _ := fixture.handler.Handle(context.Background(), IngestCallCommand{Call: first})

		firstDone <- result
	}()

	select {
	case <-fixture.calls.Entered:

	case <-time.After(5 * time.Second):
		t.Fatal("the first call never reached the store")
	}

	secondDone := make(chan IngestCallResult, 1)

	go func() {
		result, _ := fixture.handler.Handle(context.Background(), IngestCallCommand{Call: second})

		secondDone <- result
	}()

	// The second call is given a full second to reach the store, which is far more than the store itself
	// takes. A server that does not serialize its ingests reaches it in microseconds.
	started := 0

observing:
	for {
		select {
		case <-fixture.calls.Entered:
			started++

		case <-time.After(time.Second):
			break observing
		}
	}

	if started != 0 {
		t.Fatalf("%d calls reached the store while the first was still being stored: a call is compared "+
			"against the history the server has already written, and a call that has not been written yet is "+
			"invisible to it", started)
	}

	close(fixture.calls.Hold)

	for name, done := range map[string]<-chan IngestCallResult{"first": firstDone, "second": secondDone} {
		select {
		case result := <-done:
			if result.Outcome != IngestCallAccepted {
				t.Errorf("%s call outcome = %q, want accepted", name, result.Outcome)
			}

		case <-time.After(5 * time.Second):
			t.Fatalf("the %s call never finished", name)
		}
	}

	if len(fixture.calls.Saved) != 2 {
		t.Fatalf("the server stored %v, want both calls", fixture.calls.Saved)
	}

	firstStored, secondStored := fixture.calls.Saved[0].AudioFilename, fixture.calls.Saved[1].AudioFilename

	if !strings.HasPrefix(firstStored, "first") || !strings.HasPrefix(secondStored, "second") {
		t.Errorf("the calls were stored as %s then %s, want them stored one at a time in the order they arrived",
			firstStored, secondStored)
	}
}

// TestIngestCallStoresAndDeliversACallWhenTheServerLogCannotBeWritten is CA-007 for the ingest path: the
// call is the thing the server exists to store, and the line it writes about the call is a report. A log
// view that cannot be written must not lose a recording, must not stop it reaching a listener, and must not
// stop it reaching the downstream servers.
func TestIngestCallStoresAndDeliversACallWhenTheServerLogCannotBeWritten(t *testing.T) {
	fixture := newIngestFixture()
	fixture.logs.Err = errors.New("no such table: logs")

	system := fixture.seedKnownSystem()

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Errorf("outcome = %q, want the call accepted even though the log could not be written", result.Outcome)
	}

	if len(fixture.calls.Saved) != 1 {
		t.Fatalf("stored calls = %v, want the recording stored even though the log could not be written", fixture.calls.Saved)
	}

	if len(fixture.hub.DeliveredCalls["listener"]) != 1 {
		t.Error("the accepted call was not delivered to a listener that asked for it, want the log failure to change nothing about it")
	}

	if len(fixture.logs.Stored) != 0 {
		t.Errorf("logs = %v, want nothing stored by a log view that refuses to store anything", fixture.logs.Stored)
	}
}

// A talkgroup or a system can hold its calls back from the operators who read them. The call is
// recorded and reported like any other, and it is the telling that is held back: neither the other
// servers this one re-posts to, nor the listeners watching the talkgroup, hear about it until the
// hold has run out.
//
// The delay is set on the system the server stores, because it is the stored system and talkgroup
// that a call is resolved against, and therefore the ones whose delay decides what happens to it.

func TestIngestCallHoldsBackACallOnADelayedTalkgroup(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()
	fixture.systems.Stored[0].Talkgroups[0].Delay = 1

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:     1,
		Apikey: "downstream-key",
		URL:    "http://downstream.example",
		Scope:  domain.AccessScope{Present: true, Wildcard: true},
	}}

	call := delayedIngestCall(fixture.systems.Stored[0])

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Fatalf("outcome = %q, want %q: a held call is still a call this server accepted", result.Outcome, IngestCallAccepted)
	}

	// Recorded, and reported as recorded: the hold is about who is told, not about whether the
	// recording was kept.
	if got := fixture.calls.Saved; len(got) != 1 {
		t.Fatalf("the server stored %d calls, want the one it accepted", len(got))
	}

	if !fixture.loggedAs(domain.LogSourceIngest, "call from system 1 talkgroup 123 success, test.m4a") {
		t.Errorf("logs = %v, want the success line for a call that was recorded", fixture.logMessages())
	}

	// Handed over to the part that holds it.
	if got := fixture.delayer.Held; len(got) != 1 {
		t.Fatalf("the delayer took %d calls, want the one on a talkgroup delayed 1 minute", len(got))
	}

	// And nobody has been told about it yet.
	if got := fixture.reposted(); len(got) != 0 {
		t.Errorf("the server re-posted %d calls, want none re-posted while the call is being held back", len(got))
	}

	if got := fixture.hub.DeliveredCalls["listener"]; len(got) != 0 {
		t.Errorf("a listener watching the talkgroup received %d calls, want none while the call is being held back", len(got))
	}
}

// A system's delay holds back its calls, but only for the talkgroups that name no delay of their
// own.
func TestIngestCallHoldsBackACallOnADelayedSystemWhenItsTalkgroupNamesNone(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()
	fixture.systems.Stored[0].Delay = 1

	call := delayedIngestCall(fixture.systems.Stored[0])

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Fatalf("outcome = %q, want %q", result.Outcome, IngestCallAccepted)
	}

	if got := fixture.delayer.Held; len(got) != 1 {
		t.Fatalf("the delayer took %d calls, want the one on a system delayed 1 minute", len(got))
	}

	if got := fixture.hub.DeliveredCalls["listener"]; len(got) != 0 {
		t.Errorf("a listener watching the talkgroup received %d calls, want none while the call is being held back", len(got))
	}
}

// A recording that arrived after its own hold had run out is not held back: the hold is measured from
// the moment the call happened, and that moment plus the hold is already behind it.
func TestIngestCallDeliversACallWhoseHoldRanOutBeforeItArrived(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()
	fixture.systems.Stored[0].Talkgroups[0].Delay = 1

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:     1,
		Apikey: "downstream-key",
		URL:    "http://downstream.example",
		Scope:  domain.AccessScope{Present: true, Wildcard: true},
	}}

	call := delayedIngestCall(fixture.systems.Stored[0])
	call.Timestamp = ingestTime.Add(-10 * time.Minute)

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Fatalf("outcome = %q, want %q", result.Outcome, IngestCallAccepted)
	}

	if got := fixture.delayer.Held; len(got) != 0 {
		t.Fatalf("the delayer took %d calls, want none: this call's 1 minute hold ran out 9 minutes before it arrived", len(got))
	}

	if got := fixture.reposted(); len(got) != 1 {
		t.Errorf("the server re-posted %d calls, want the one whose hold had already run out", len(got))
	}

	if got := fixture.hub.DeliveredCalls["listener"]; len(got) != 1 {
		t.Errorf("a listener watching the talkgroup received %d calls, want the one whose hold had already run out", len(got))
	}
}

// A hold that could not be written down is reported, and the call is not delivered: the server has no
// way to remember the delivery it would be promising.
func TestIngestCallReportsACallItCouldNotHoldBack(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()
	fixture.systems.Stored[0].Talkgroups[0].Delay = 1

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:     1,
		Apikey: "downstream-key",
		URL:    "http://downstream.example",
		Scope:  domain.AccessScope{Present: true, Wildcard: true},
	}}

	fixture.delayer.Failure = errors.New(`delayer.push: no such table: delayed in INSERT INTO "delayed" ("callId", "timestamp") VALUES (7, 1791288060000)`)

	result, err := fixture.ingest(delayedIngestCall(fixture.systems.Stored[0]))
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Fatalf("outcome = %q, want %q: the recording was kept even though the hold was not", result.Outcome, IngestCallAccepted)
	}

	if !fixture.loggedAs(domain.LogSourceDelayer, "the call could not be held for delivery: delayer.push") {
		t.Errorf("logs = %v, want the hold that did not happen, with the failure's own text after it", fixture.logMessages())
	}

	if got := fixture.reposted(); len(got) != 0 {
		t.Errorf("the server re-posted %d calls, want none: a call nobody can hold is not a call to deliver now", len(got))
	}

	if got := fixture.hub.DeliveredCalls["listener"]; len(got) != 0 {
		t.Errorf("a listener watching the talkgroup received %d calls, want none", len(got))
	}
}

// A call whose hold has run out is delivered by the step an ingested call ends with, which is what
// makes a call that waited reach the same people as one that did not, and by the same code.
func TestDeliveringACallWhoseHoldHasRunOutReachesEveryoneAnIngestedCallReaches(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()
	fixture.systems.Stored[0].Talkgroups[0].Delay = 1

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:     1,
		Apikey: "downstream-key",
		URL:    "http://downstream.example",
		Scope:  domain.AccessScope{Present: true, Wildcard: true},
	}}

	fixture.listen()

	call := delayedIngestCall(fixture.systems.Stored[0])
	call.ID = 7
	call.Timestamp = ingestTime.Add(-10 * time.Minute)
	call.Delayed = true

	fixture.handler.DeliverCall(context.Background(), call)

	if got := fixture.hub.DeliveredCalls["listener"]; len(got) != 1 {
		t.Errorf("a listener watching the talkgroup received %d calls, want the one whose hold ran out", len(got))
	}

	if got := fixture.reposted(); len(got) != 1 {
		t.Errorf("the server re-posted %d calls, want the one whose hold ran out", len(got))
	}

	// The hold is not taken again: a call that has already waited is not made to wait again.
	if got := fixture.delayer.Held; len(got) != 0 {
		t.Errorf("the delayer took %d calls a second time, want none", len(got))
	}
}

// delayedIngestCall is a call on the fixture's system and talkgroup that happened half a minute ago,
// so that a hold of a minute has half a minute left to run.
func delayedIngestCall(system domain.System) domain.Call {
	call := validIngestCall()

	call.System = system
	call.Talkgroup = system.Talkgroups[0]
	call.Timestamp = ingestTime.Add(-30 * time.Second)

	return call
}

// A downstream server is another Rdio Scanner this one re-posts its calls to. Which calls it
// gets is decided by the scope configured on it, and what it gets is the call as it was stored.

// claimedIngestCall is a call that names the system and talkgroup the fixture stores, so that
// what gets forwarded is a call the server resolved rather than one it invented.
func claimedIngestCall() domain.Call {
	call := validIngestCall()

	call.Meta.SystemRef = 1
	call.Meta.TalkgroupRef = 123

	return call
}

func TestIngestCallForwardsAcceptedCallToDownstreamCoveringEverySystem(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:     1,
		Apikey: "downstream-key",
		URL:    "http://downstream.example",
		Scope:  domain.AccessScope{Present: true, Wildcard: true},
	}}

	result, err := fixture.ingest(claimedIngestCall())
	if err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Fatalf("outcome = %v, want accepted", result.Outcome)
	}

	reposted := fixture.reposted()

	if len(reposted) != 1 {
		t.Fatalf("re-posts = %d, want one", len(reposted))
	}

	want := ports.DownstreamTarget{URL: "http://downstream.example", Apikey: "downstream-key"}

	if reposted[0].Target != want {
		t.Errorf("target = %+v, want %+v", reposted[0].Target, want)
	}

	if reposted[0].Call.ID != result.CallID {
		t.Errorf("re-posted call id = %d, want the stored call %d", reposted[0].Call.ID, result.CallID)
	}
}

func TestIngestCallForwardsOnlyToDownstreamsWhoseScopeCoversTheCall(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.downstreams.Stored = domain.Downstreams{
		{
			ID:     1,
			Apikey: "covers-the-call",
			URL:    "http://covering.example",
			Scope: domain.AccessScope{
				Present: true,
				Systems: []domain.ScopedSystem{{SystemRef: 1, TalkgroupRefs: []uint{123}}},
			},
		},
		{
			ID:     2,
			Apikey: "covers-another-talkgroup",
			URL:    "http://other-talkgroup.example",
			Scope: domain.AccessScope{
				Present: true,
				Systems: []domain.ScopedSystem{{SystemRef: 1, TalkgroupRefs: []uint{456}}},
			},
		},
		{
			ID:     3,
			Apikey: "covers-another-system",
			URL:    "http://other-system.example",
			Scope: domain.AccessScope{
				Present: true,
				Systems: []domain.ScopedSystem{{SystemRef: 2, TalkgroupsWildcard: true}},
			},
		},
	}

	if _, err := fixture.ingest(claimedIngestCall()); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	reposted := fixture.reposted()

	if len(reposted) != 1 {
		t.Fatalf("re-posts = %d, want only the downstream whose scope covers the call", len(reposted))
	}

	if reposted[0].Target.Apikey != "covers-the-call" {
		t.Errorf("re-posted to %q, want the downstream scoped to this system and talkgroup", reposted[0].Target.Apikey)
	}
}

func TestIngestCallDoesNotForwardToADisabledDownstream(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:       1,
		Apikey:   "disabled-key",
		Disabled: true,
		URL:      "http://disabled.example",
		Scope:    domain.AccessScope{Present: true, Wildcard: true},
	}}

	if _, err := fixture.ingest(claimedIngestCall()); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	if got := len(fixture.reposted()); got != 0 {
		t.Fatalf("re-posts = %d, want none for a disabled downstream", got)
	}
}

func TestIngestCallDoesNotForwardToADownstreamWithNoAddress(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:     1,
		Apikey: "no-url",
		Scope:  domain.AccessScope{Present: true, Wildcard: true},
	}}

	if _, err := fixture.ingest(claimedIngestCall()); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	if got := len(fixture.reposted()); got != 0 {
		t.Fatalf("re-posts = %d, want none for a downstream with no address to post to", got)
	}
}

func TestIngestCallDoesNotForwardACallItRejected(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:     1,
		Apikey: "downstream-key",
		URL:    "http://downstream.example",
		Scope:  domain.AccessScope{Present: true, Wildcard: true},
	}}

	if _, err := fixture.ingest(claimedIngestCall()); err != nil {
		t.Fatalf("first ingest failed: %v", err)
	}

	second, err := fixture.ingest(claimedIngestCall())
	if err != nil {
		t.Fatalf("second ingest failed: %v", err)
	}

	if second.Outcome != IngestCallDuplicate {
		t.Fatalf("second outcome = %v, want duplicate", second.Outcome)
	}

	if got := len(fixture.reposted()); got != 1 {
		t.Fatalf("re-posts = %d, want only the call that was accepted", got)
	}
}

func TestIngestCallLogsEachCallItForwarded(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:     1,
		Apikey: "downstream-key",
		URL:    "http://downstream.example",
		Scope:  domain.AccessScope{Present: true, Wildcard: true},
	}}

	if _, err := fixture.ingest(claimedIngestCall()); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	if !fixture.loggedAsEventually(domain.LogSourceDownstream, "call from system 1 talkgroup 123 sent to http://downstream.example: success") {
		t.Fatalf("logs = %q, want the re-post recorded under the downstream it was sent to", fixture.logMessages())
	}
}

func TestIngestCallLogsACallTheDownstreamRefused(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:     1,
		Apikey: "downstream-key",
		URL:    "http://downstream.example",
		Scope:  domain.AccessScope{Present: true, Wildcard: true},
	}}

	fixture.forwarder.Err = errors.New("downstream.send: bad status: 500 Internal Server Error")

	if _, err := fixture.ingest(claimedIngestCall()); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	if !fixture.loggedAsEventually(domain.LogSourceDownstream, "call from system 1 talkgroup 123 sent to http://downstream.example: downstream.send: bad status: 500 Internal Server Error") {
		t.Fatalf("logs = %q, want the refusal recorded against the downstream that gave it", fixture.logMessages())
	}

	// A downstream that refuses a call is not the ingest's failure: the call was stored and
	// delivered here, and the legacy server says nothing else about it.
	if !fixture.loggedAs(domain.LogSourceIngest, "call from system 1 talkgroup 123 success, test.m4a") {
		t.Errorf("logs = %q, want the call to have been accepted", fixture.logMessages())
	}
}

// TestIngestCallReportsDownstreamsItCouldNotRead is the line the re-post writes when it cannot even get
// the list: the call was stored and delivered here, and the only trace of the re-post that never happened
// is this entry.
func TestIngestCallReportsDownstreamsItCouldNotRead(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.downstreams.Err = errors.New(`downstreams.all: no such table: downstreams in SELECT "id" FROM "downstreams"`)

	if _, err := fixture.ingest(claimedIngestCall()); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	if !fixture.loggedAsEventually(domain.LogSourceDownstream, `the downstream servers could not be read: downstreams.all: no such table: downstreams in SELECT "id" FROM "downstreams"`) {
		t.Fatalf("logs = %v, want what the server could not read and the failure's own text after it", fixture.logMessages())
	}
}

func TestIngestCallDoesNotWaitForADownstreamThatTakesItsTime(t *testing.T) {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.downstreams.Stored = domain.Downstreams{{
		ID:     1,
		Apikey: "downstream-key",
		URL:    "http://slow.example",
		Scope:  domain.AccessScope{Present: true, Wildcard: true},
	}}

	// The re-post will not finish until the test lets it, which is how a downstream server
	// that takes thirty seconds is held here without holding the test with it.
	fixture.forwarder.Started = make(chan struct{})
	fixture.forwarder.Finished = make(chan struct{})
	fixture.forwarder.Release = make(chan struct{})

	done := make(chan error, 1)

	go func() {
		_, err := fixture.ingest(claimedIngestCall())

		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			close(fixture.forwarder.Release)

			t.Fatalf("ingest failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		close(fixture.forwarder.Release)

		t.Fatal("the ingest never finished while the downstream was still being posted to")
	}

	// The ingest is over. The re-post has been started, and is still not finished: the call
	// was stored, delivered, and acknowledged while the other server hangs. A deployment with
	// an unreachable downstream must not have its own calls held up by it.
	select {
	case <-fixture.forwarder.Started:
	case <-time.After(2 * time.Second):
		close(fixture.forwarder.Release)

		t.Fatal("the ingest never reached the downstream")
	}

	select {
	case <-fixture.forwarder.Finished:
		close(fixture.forwarder.Release)

		t.Fatal("the ingest waited for the downstream to answer")
	default:
	}

	close(fixture.forwarder.Release)
}

// TestIngestCallSanitizesSuppliedTextIntoSingleLogLines (REQ-016, CA-016.1): a label an
// upload supplies with a newline in it is a way to write a second line into the server's log
// view — the forged line reads like the server's own. The ingested call's text arrives as one
// line per decision, the stored labels carry no control characters, and the log view holds
// exactly the entries the decisions make.
func TestIngestCallSanitizesSuppliedTextIntoSingleLogLines(t *testing.T) {
	fixture := newIngestFixture()

	call := validIngestCall()
	call.AudioFilename = "recording.wav\nthe forge line"
	call.Meta.SystemLabel = "Grand Metro\nthe forge line"
	call.Meta.TalkgroupRef = 1514000
	call.Meta.TalkgroupLabel = "FIRE\tDispatch\x1b[31m"
	call.Meta.TalkgroupGroup = []string{"Fire\ngroup"}
	call.Meta.TalkgroupTag = "Fire\n"

	result, err := fixture.ingest(call)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if result.Outcome != IngestCallAccepted {
		t.Fatalf("outcome = %q, want %q", result.Outcome, IngestCallAccepted)
	}

	for _, message := range fixture.logMessages() {
		if strings.ContainsAny(message, "\r\n\t\x1b") {
			t.Errorf("log line = %q, want one line with no control characters: a posted string "+
				"must not forge a second entry", message)
		}
	}

	created := 0

	for _, message := range fixture.logMessages() {
		if strings.Contains(message, "created the system") {
			created++
		}
	}

	if created != 1 {
		t.Errorf("the log view holds %d system-creation lines, want exactly one: the forged "+
			"line must not read as a second decision", created)
	}

	if len(fixture.systems.Stored) != 1 {
		t.Fatalf("stored %d systems, want the claimed system invented once", len(fixture.systems.Stored))
	}

	if label := fixture.systems.Stored[0].Label; strings.ContainsAny(label, "\r\n\t\x1b") {
		t.Errorf("system label = %q, want the stored label free of control characters", label)
	}
}
