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

package application

import (
	"context"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

func newLivefeedFixture() *livefeedFixture {
	fixture := &livefeedFixture{
		hub:       fakes.NewHub(),
		logs:      &fakes.Logs{},
		options:   &fakes.Options{Stored: domain.DefaultOptions()},
		scheduler: fakes.NewScheduler(),
		clock:     fakes.NewClock(time.Date(2026, 3, 14, 9, 30, 15, 500_000_000, time.UTC)),
	}

	fixture.livefeed = NewSetLivefeedHandler(fixture.hub)
	fixture.debouncer = NewListenerCountDebouncer(fixture.hub, fixture.logs, fixture.options, fixture.scheduler, fixture.clock)

	return fixture
}

type livefeedFixture struct {
	hub       *fakes.Hub
	logs      *fakes.Logs
	options   *fakes.Options
	scheduler *fakes.Scheduler
	clock     *fakes.Clock

	livefeed  *SetLivefeedHandler
	debouncer *ListenerCountDebouncer
}

func (fixture *livefeedFixture) setSelection(clientID string, payload map[string]any) (SetLivefeedResult, error) {
	return fixture.livefeed.Handle(context.Background(), SetLivefeedCommand{
		ClientID: clientID,
		Livefeed: domain.ParseLivefeed(payload),
	})
}

func TestSetLivefeedHandlerAnswersThatTheListenerIsWatching(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.hub.Open("listener", "10.0.0.1")

	result, err := fixture.setSelection("listener", map[string]any{
		"4": map[string]any{"1514000": true},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !result.Watching {
		t.Error("Watching = false, want true for a selection with a talkgroup switched on")
	}

	if len(fixture.hub.LivefeedAcks["listener"]) != 1 || !fixture.hub.LivefeedAcks["listener"][0] {
		t.Errorf("answers = %v, want the one affirmative answer the protocol asks for", fixture.hub.LivefeedAcks["listener"])
	}
}

func TestSetLivefeedHandlerAnswersThatNothingIsWatched(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.hub.Open("listener", "10.0.0.1")

	result, err := fixture.setSelection("listener", map[string]any{
		"4": map[string]any{"1514000": false},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Watching {
		t.Error("Watching = true, want false for a selection switched off")
	}

	if len(fixture.hub.LivefeedAcks["listener"]) != 1 || fixture.hub.LivefeedAcks["listener"][0] {
		t.Errorf("answers = %v, want the one negative answer the protocol asks for", fixture.hub.LivefeedAcks["listener"])
	}
}

func TestSetLivefeedHandlerReplacesThePreviousSelectionRatherThanAddingToIt(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.hub.Open("listener", "10.0.0.1")

	first, err := fixture.setSelection("listener", map[string]any{
		"4": map[string]any{"1514000": true},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !first.Watching {
		t.Fatal("the fixture must start watching")
	}

	second, err := fixture.setSelection("listener", map[string]any{
		"4": map[string]any{"1514001": true},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !second.Watching {
		t.Error("Watching = false, want true: the new selection switches another talkgroup on")
	}

	call := domain.NewCall()
	call.System = domain.System{ID: 1, SystemRef: 4}
	call.Talkgroup = domain.Talkgroup{ID: 10, TalkgroupRef: 1514000}

	fixture.hub.BroadcastCall(call)

	if len(fixture.hub.DeliveredCalls["listener"]) != 0 {
		t.Error("a talkgroup dropped by a later selection must no longer be delivered")
	}
}

func TestSetLivefeedHandlerTreatsAnUnusableSelectionAsNothingWatched(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.hub.Open("listener", "10.0.0.1")

	result, err := fixture.livefeed.Handle(context.Background(), SetLivefeedCommand{
		ClientID: "listener",
		Livefeed: domain.ParseLivefeed("not a selection at all"),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Watching {
		t.Error("Watching = true, want false: a selection the server cannot read switches nothing on")
	}
}

func TestSetLivefeedHandlerGatesLiveDeliveryOnTheSelection(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.hub.Open("watcher", "10.0.0.1")
	fixture.hub.Open("silent", "10.0.0.2")

	if _, err := fixture.setSelection("watcher", map[string]any{
		"4": map[string]any{"1514000": true},
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	call := domain.NewCall()
	call.System = domain.System{ID: 1, SystemRef: 4}
	call.Talkgroup = domain.Talkgroup{ID: 10, TalkgroupRef: 1514000}

	fixture.hub.BroadcastCall(call)

	if len(fixture.hub.DeliveredCalls["watcher"]) != 1 {
		t.Error("a listener that switched the talkgroup on must receive the call")
	}

	if len(fixture.hub.DeliveredCalls["silent"]) != 0 {
		t.Error("a listener that made no selection must receive nothing")
	}
}

func TestListenerCountReportDelayIsTheLegacyFiveSeconds(t *testing.T) {
	if ListenerCountReportDelay != 5*time.Second {
		t.Errorf("ListenerCountReportDelay = %v, want the legacy 5 s", ListenerCountReportDelay)
	}
}

func TestListenerCountDebouncerCollapsesABurstIntoOneReport(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.options.Stored.ShowListenersCount = true

	fixture.hub.Open("one", "10.0.0.1")
	fixture.debouncer.ListenerChanged()

	fixture.hub.Open("two", "10.0.0.2")
	fixture.debouncer.ListenerChanged()

	fixture.hub.Open("three", "10.0.0.3")
	fixture.debouncer.ListenerChanged()

	if got := fixture.scheduler.PendingCount(); got != 1 {
		t.Fatalf("scheduled %d reports, want the burst collapsed into one", got)
	}

	fixture.scheduler.RunDue(ListenerCountReportDelay)

	if len(fixture.logs.Stored) != 1 {
		t.Fatalf("logs = %v, want exactly one report for the burst", fixture.logs.Stored)
	}

	if fixture.logs.Stored[0].Source != domain.LogSourceLivefeed {
		t.Errorf("the report was written under %q, want it filed with the part of the server that feeds listeners", fixture.logs.Stored[0].Source)
	}

	if fixture.logs.Stored[0].Message != "there are 3 listeners" {
		t.Errorf("report = %q, want the count at the moment of the report", fixture.logs.Stored[0].Message)
	}

	if len(fixture.hub.ListenerCounts) != 1 || fixture.hub.ListenerCounts[0] != 3 {
		t.Errorf("listener-count reports = %v, want one report of 3", fixture.hub.ListenerCounts)
	}
}

// The count wording stays the approved wording even for a count of one.
func TestListenerCountReportReadsAsOneListenerWhenThereIsOne(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.options.Stored.ShowListenersCount = true

	fixture.hub.Open("one", "10.0.0.1")
	fixture.debouncer.ListenerChanged()

	fixture.scheduler.RunDue(ListenerCountReportDelay)

	if len(fixture.logs.Stored) != 1 {
		t.Fatalf("logs = %v, want one report", fixture.logs.Stored)
	}

	if got := fixture.logs.Stored[0].Message; got != "there is 1 listener" {
		t.Errorf("report = %q, want the count said in the words an operator reads", got)
	}
}

func TestListenerCountDebouncerCountsTheListenersPresentAtTheReport(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.options.Stored.ShowListenersCount = true

	fixture.hub.Open("one", "10.0.0.1")
	fixture.hub.Open("two", "10.0.0.2")
	fixture.hub.Open("three", "10.0.0.3")

	fixture.debouncer.ListenerChanged()

	fixture.hub.CloseClient("three", 1000)

	fixture.scheduler.RunDue(ListenerCountReportDelay)

	if len(fixture.hub.ListenerCounts) != 1 || fixture.hub.ListenerCounts[0] != 2 {
		t.Errorf("listener-count reports = %v, want the count as it stands when the report is written, not as it stood when it was scheduled", fixture.hub.ListenerCounts)
	}
}

func TestListenerCountDebouncerLogsTheCountEvenWhenTheOptionIsOff(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.options.Stored.ShowListenersCount = false

	fixture.hub.Open("one", "10.0.0.1")
	fixture.debouncer.ListenerChanged()
	fixture.scheduler.RunDue(ListenerCountReportDelay)

	if len(fixture.logs.Stored) != 1 {
		t.Fatalf("logs = %v, want the count written to the server log regardless of the option", fixture.logs.Stored)
	}

	if len(fixture.hub.ListenerCounts) != 0 {
		t.Errorf("listener-count reports = %v, want none while the option keeps the count hidden", fixture.hub.ListenerCounts)
	}
}

// The listener-count report is stamped with the instant it was written.
func TestListenerCountReportIsStampedWithTheInstantItWasWritten(t *testing.T) {
	fixture := newLivefeedFixture()

	fixture.hub.Open("one", "10.0.0.1")
	fixture.debouncer.ListenerChanged()

	fixture.scheduler.RunDue(ListenerCountReportDelay)

	if len(fixture.logs.Stored) != 1 {
		t.Fatalf("logs = %v, want the one report the burst produced", fixture.logs.Stored)
	}

	stored := fixture.logs.Stored[0]

	if !stored.DateTime.Equal(fixture.clock.At) {
		t.Errorf("the report is stored at %v, which is %d in the timestamp column, want the clock's %v",
			stored.DateTime, stored.DateTime.UnixMilli(), fixture.clock.At)
	}
}

func TestListenerCountDebouncerSchedulesANewReportAfterThePreviousOneRuns(t *testing.T) {
	fixture := newLivefeedFixture()

	fixture.hub.Open("one", "10.0.0.1")
	fixture.debouncer.ListenerChanged()
	fixture.scheduler.RunDue(ListenerCountReportDelay)

	if fixture.debouncer.Pending() {
		t.Fatal("a report that has run must not stay pending")
	}

	fixture.hub.Open("two", "10.0.0.2")
	fixture.debouncer.ListenerChanged()

	if !fixture.debouncer.Pending() {
		t.Fatal("a later burst must schedule its own report")
	}

	fixture.scheduler.RunDue(ListenerCountReportDelay)

	if len(fixture.logs.Stored) != 2 {
		t.Errorf("logs = %v, want one report per burst", fixture.logs.Stored)
	}
}

func TestListenerCountDebouncerDoesNotReportBeforeTheDelayHasRun(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.options.Stored.ShowListenersCount = true

	fixture.hub.Open("one", "10.0.0.1")
	fixture.debouncer.ListenerChanged()

	fixture.scheduler.RunDue(ListenerCountReportDelay - time.Millisecond)

	if len(fixture.logs.Stored) != 0 {
		t.Errorf("logs = %v, want nothing reported before the delay has passed", fixture.logs.Stored)
	}

	if !fixture.debouncer.Pending() {
		t.Error("the report must still be pending")
	}
}

// A stopping server cancels the pending listener-count report.
func TestListenerCountDebouncerStopsAPendingReportWhenAsked(t *testing.T) {
	fixture := newLivefeedFixture()
	fixture.options.Stored.ShowListenersCount = true

	fixture.hub.Open("one", "10.0.0.1")
	fixture.debouncer.ListenerChanged()

	fixture.debouncer.Cancel()

	if fixture.debouncer.Pending() {
		t.Error("the report is still pending after the server asked for it to be dropped")
	}

	fixture.scheduler.RunDue(ListenerCountReportDelay)

	if len(fixture.logs.Stored) != 0 {
		t.Errorf("logs = %v, want the pending report dropped rather than written", fixture.logs.Stored)
	}

	if got := fixture.scheduler.PendingCount(); got != 0 {
		t.Errorf("the scheduler still holds %d piece of work, want none", got)
	}
}
