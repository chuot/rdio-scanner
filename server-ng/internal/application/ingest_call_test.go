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

	"rdio-scanner/server-ng/internal/application/fakes"
	"rdio-scanner/server-ng/internal/domain"
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

// loggedEventually waits for a log line that the ingest wrote on another goroutine — the
// downstream fan-out — and gives up after a moment that no local log write needs.
func (fixture *ingestFixture) loggedEventually(want string) bool {
	deadline := time.Now().Add(2 * time.Second)

	for {
		for _, message := range fixture.logMessages() {
			if message == want {
				return true
			}
		}

		if time.Now().After(deadline) {
			return false
		}

		time.Sleep(10 * time.Millisecond)
	}
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

func (fixture *ingestFixture) loggedContaining(fragment string) bool {
	for _, message := range fixture.logMessages() {
		if strings.Contains(message, fragment) {
			return true
		}
	}

	return false
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
// (server/controller_test.go), which run against a real temporary database. Here they run
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

	if !fixture.loggedContaining("blacklisted") {
		t.Errorf("logs = %v, want the blacklisted decision recorded", fixture.logMessages())
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

	if !fixture.loggedContaining("no matching system/talkgroup") {
		t.Errorf("logs = %v, want the legacy warning the legacy server writes here", fixture.logMessages())
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

	if !fixture.loggedContaining("duplicate call rejected") {
		t.Errorf("logs = %v, want the duplicate decision recorded", fixture.logMessages())
	}

	if len(fixture.converter.Modes) != 0 {
		t.Error("a duplicate is rejected before its audio is converted")
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

	if !fixture.loggedContaining("success") {
		t.Errorf("logs = %v, want the legacy success line", fixture.logMessages())
	}

	if len(fixture.hub.DeliveredCalls["listener"]) != 1 {
		t.Error("an accepted call must be delivered to a listener that asked for it")
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

	if !fixture.loggedContaining("ffmpeg is not available") {
		t.Errorf("logs = %v, want the conversion failure recorded", fixture.logMessages())
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
