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

package delayer

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

// heldAt is the moment the delayer in these tests is asked about a call, and the moment the calls it
// is asked about happened.
var heldAt = time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC)

// A call that is held back is written down with the instant it stops being held back, is armed for
// that instant, and is handed over carrying the mark that says it was held back — and none of it
// happens until that instant.
func TestACallWithADelayIsHeldAndHandedBackWhenTheHoldRunsOut(t *testing.T) {
	fixture := newDelayerFixture()

	call := heldCall(7, heldAt, 1)

	held, err := fixture.delayer.Hold(context.Background(), call)
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}

	if !held {
		t.Fatalf("a call on a talkgroup delayed 1 minute was not held, want it held")
	}

	// Written down, with the instant it stops being held back.
	if got := fixture.delayed.Stored; len(got) != 1 {
		t.Fatalf("the waiting list holds %d rows, want the one call being held back", len(got))
	} else if !got[0].Timestamp.Equal(heldAt.Add(time.Minute)) {
		t.Errorf("the waiting list says call %d stops being held back at %s, want %s", got[0].CallID, got[0].Timestamp, heldAt.Add(time.Minute))
	}

	if got := fixture.scheduler.PendingCount(); got != 1 {
		t.Errorf("the delayer armed %d holds, want the one it took", got)
	}

	if got := delivered(fixture.delayer); len(got) != 0 {
		t.Errorf("the delayer handed over %d calls before the hold ran out, want none", len(got))
	}

	fixture.scheduler.RunDue(time.Minute)

	got := delivered(fixture.delayer)

	if len(got) != 1 {
		t.Fatalf("the delayer handed over %d calls when the hold ran out, want the one it held", len(got))
	}

	if !got[0].Delayed {
		t.Errorf("the call handed over does not say it was held back, want it marked: a listener's history shows that mark")
	}

	if got[0].ID != 7 {
		t.Errorf("the call handed over is call %d, want the call that was held", got[0].ID)
	}

	if left := fixture.delayed.Stored; len(left) != 0 {
		t.Errorf("the waiting list still holds %d rows after the hold ran out, want the call taken off it", len(left))
	}
}

// A recording that arrived after its own hold had run out is not held: the hold is measured from the
// moment the call happened, and that moment plus the hold is already behind it.
func TestACallWhoseHoldRanOutBeforeItArrivedIsNotHeld(t *testing.T) {
	fixture := newDelayerFixture()

	call := heldCall(7, heldAt.Add(-10*time.Minute), 1)

	held, err := fixture.delayer.Hold(context.Background(), call)
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}

	if held {
		t.Errorf("a call that happened 10 minutes ago with a 1 minute hold was held, want it delivered now")
	}

	if got := fixture.delayed.Stored; len(got) != 0 {
		t.Errorf("the waiting list holds %d rows, want nothing written down for a call that is not being held back", len(got))
	}

	if got := fixture.scheduler.PendingCount(); got != 0 {
		t.Errorf("the delayer armed %d holds, want none", got)
	}
}

// A call whose talkgroup and system name no hold is never held, however recent it is.
func TestACallWithNoDelayIsNeverHeld(t *testing.T) {
	fixture := newDelayerFixture()

	call := heldCall(7, heldAt, 0)

	held, err := fixture.delayer.Hold(context.Background(), call)
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}

	if held {
		t.Errorf("a call with no hold named was held, want the caller to deliver it")
	}
}

// A hold that cannot be written down is reported, and the call is left undelivered: the server cannot
// promise a delivery it has no way to remember.
func TestAHoldThatCannotBeWrittenDownIsReportedAndTheCallLeftUndelivered(t *testing.T) {
	fixture := newDelayerFixture()

	fixture.delayed.Err = errors.New("the waiting list is unreadable")

	held, err := fixture.delayer.Hold(context.Background(), heldCall(7, heldAt, 1))

	if err == nil {
		t.Fatalf("a hold that could not be written down reported no failure, want the failure the ingest path reports")
	}

	if held {
		t.Errorf("a hold that could not be written down claims to have taken the call, want it to have taken nothing")
	}

	if got := fixture.scheduler.PendingCount(); got != 0 {
		t.Errorf("the delayer armed %d holds, want none armed for a hold that failed", got)
	}

	if got := delivered(fixture.delayer); len(got) != 0 {
		t.Errorf("the delayer handed over %d calls after a failed hold, want none", len(got))
	}

	// The failure is named for the part of the delayer that hit it, which is the shape of the line the
	// legacy server writes and the shape of every repository failure here.
	if got := err.Error(); !strings.HasPrefix(got, "delayer.delay: ") {
		t.Errorf("the failure reads %q, want it named for the delayer step that hit it", got)
	}
}

// A hold that ran out but cannot be taken off the waiting list is reported, and the call is delivered
// anyway: the recording is finished and its hold is over, and a row left behind is not a reason to
// keep it from the operators who were waiting for it.
func TestAHoldThatCannotBeClearedIsReportedAndTheCallStillDelivered(t *testing.T) {
	fixture := newDelayerFixture()

	// The same waiting list, except that the row it is asked to clear will not go away.
	holding := New(Deps{
		Delayed:   &stuckDelayed{DelayedCalls: fixture.delayed},
		Calls:     fixture.calls,
		Scheduler: fixture.scheduler,
		Clock:     fixture.clock,
		Logs:      fixture.logs,
	})

	if held, err := holding.Hold(context.Background(), heldCall(7, heldAt, 1)); err != nil || !held {
		t.Fatalf("Hold reported held=%v err=%v, want the call held", held, err)
	}

	fixture.scheduler.RunDue(time.Minute)

	got := delivered(holding)

	if len(got) != 1 {
		t.Fatalf("the delayer handed over %d calls when the hold ran out, want the one it held", len(got))
	}

	reported := fixture.logs.Recorded()

	if len(reported) != 1 {
		t.Fatalf("the server wrote %d lines about the hold that would not clear, want one", len(reported))
	}

	if reported[0].Level != domain.LogLevelError {
		t.Errorf("the failure was written at level %q, want an error", reported[0].Level)
	}

	if reported[0].Message != "delayer.pop: the row will not go away" {
		t.Errorf("the failure was written as %q, want the legacy server's own line", reported[0].Message)
	}
}

// The calls this server was holding when it last stopped are picked up: the ones whose hold ran out
// while the server was not running are handed over at once, and the ones still holding are armed
// again and written down again.
func TestTheCallsHeldWhenTheServerLastStoppedArePickedUp(t *testing.T) {
	fixture := newDelayerFixture()

	// Call 7 was being held back until a moment that has already gone by. Call 8 until one that has
	// not. Call 9 was written down but is no longer in the recording history.
	fixture.delayed.Stored = []ports.DelayedCall{
		{CallID: 7, Timestamp: heldAt.Add(-1 * time.Minute)},
		{CallID: 8, Timestamp: heldAt.Add(2 * time.Minute)},
		{CallID: 9, Timestamp: heldAt.Add(2 * time.Minute)},
	}

	fixture.calls.Stored[7] = heldCall(7, heldAt.Add(-3*time.Minute), 1)
	fixture.calls.Stored[8] = heldCall(8, heldAt.Add(-1*time.Minute), 3)

	if err := fixture.delayer.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// The hold that ran out while the server was down is handed over, marked as held back.
	ran := delivered(fixture.delayer)

	if len(ran) != 1 {
		t.Fatalf("Restore handed over %d calls, want the one whose hold ran out while the server was down", len(ran))
	}

	if ran[0].ID != 7 || !ran[0].Delayed {
		t.Errorf("Restore handed over call %d marked %v, want call 7 marked as held back", ran[0].ID, ran[0].Delayed)
	}

	// The hold still running is armed again, for the instant its delay names now, and written down
	// again with that instant. The call that is no longer in the history is left behind without
	// stopping the startup.
	if got := fixture.scheduler.PendingCount(); got != 1 {
		t.Fatalf("Restore armed %d holds, want the one still running", got)
	}

	if got := fixture.delayed.Stored; len(got) != 1 || got[0].CallID != 8 {
		t.Fatalf("the waiting list holds %v after the startup, want only the call still being held back", fixture.delayed.Stored)
	}

	fixture.scheduler.RunDue(2 * time.Minute)

	ran = delivered(fixture.delayer)

	if len(ran) != 1 || ran[0].ID != 8 {
		t.Fatalf("the re-armed hold handed over %v, want call 8 when its 2 minutes were up", ran)
	}

	if got := fixture.delayed.Stored; len(got) != 0 {
		t.Errorf("the waiting list holds %v once the hold has run out, want the call taken off it", fixture.delayed.Stored)
	}
}

// A hold that was written down under one delay is re-armed for the delay the configuration names now,
// which is what lets an administrator change a talkgroup's delay and have the calls already waiting
// follow the new one.
func TestAStoredHoldIsRewrittenWithTheDelayTheConfigurationNamesNow(t *testing.T) {
	fixture := newDelayerFixture()

	// Written down when the talkgroup's hold was 10 minutes; the talkgroup now names 1.
	fixture.delayed.Stored = []ports.DelayedCall{{CallID: 7, Timestamp: heldAt.Add(10 * time.Minute)}}

	fixture.calls.Stored[7] = heldCall(7, heldAt, 1)

	if err := fixture.delayer.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if got := delivered(fixture.delayer); len(got) != 0 {
		t.Fatalf("Restore handed over %d calls, want this one still holding", len(got))
	}

	if got := fixture.delayed.Stored; len(got) != 1 || !got[0].Timestamp.Equal(heldAt.Add(time.Minute)) {
		t.Errorf("the waiting list holds %v, want the hold rewritten for the 1 minute the talkgroup names now", fixture.delayed.Stored)
	}

	fixture.scheduler.RunDue(time.Minute)

	got := delivered(fixture.delayer)

	if len(got) != 1 || got[0].ID != 7 {
		t.Errorf("the rewritten hold handed over %v, want call 7 when the minute the configuration names was up", got)
	}
}

// A hold whose delay has been shortened out from under it while the server was down is not held again:
// the call's own delay is what decides, and it has already run out.
func TestAStoredHoldWhoseDelayHasRunOutIsDeliveredAtStartup(t *testing.T) {
	fixture := newDelayerFixture()

	fixture.delayed.Stored = []ports.DelayedCall{{CallID: 7, Timestamp: heldAt.Add(10 * time.Minute)}}

	fixture.calls.Stored[7] = heldCall(7, heldAt.Add(-30*time.Minute), 1)

	if err := fixture.delayer.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	ran := delivered(fixture.delayer)

	if len(ran) != 1 || ran[0].ID != 7 {
		t.Fatalf("Restore handed over %v, want call 7 at once: its 1 minute hold ran out half an hour ago", ran)
	}

	if got := fixture.scheduler.PendingCount(); got != 0 {
		t.Errorf("Restore armed %d holds, want none for a call whose hold has run out", got)
	}
}

// A server that stops abandons the holds it armed and leaves what it wrote down where it is, which is
// how the next startup finds the calls it was holding.
func TestClosingTheDelayerAbandonsTheHoldsItArmed(t *testing.T) {
	fixture := newDelayerFixture()

	if held, err := fixture.delayer.Hold(context.Background(), heldCall(7, heldAt, 5)); err != nil || !held {
		t.Fatalf("Hold reported held=%v err=%v, want the call held", held, err)
	}

	fixture.delayer.Close()

	if got := fixture.scheduler.PendingCount(); got != 0 {
		t.Errorf("the delayer left %d holds armed after it was closed, want none", got)
	}

	fixture.scheduler.RunDue(time.Hour)

	if got := delivered(fixture.delayer); len(got) != 0 {
		t.Errorf("a closed delayer handed over %d calls, want nothing from a server that has stopped", len(got))
	}

	if held, err := fixture.delayer.Hold(context.Background(), heldCall(8, heldAt, 5)); err != nil || held {
		t.Errorf("a closed delayer took a call (held=%v err=%v), want it to take nothing", held, err)
	}

	if got := fixture.delayed.Stored; len(got) != 1 {
		t.Errorf("the waiting list holds %d rows after the delayer was closed, want the call still written down for the next startup", len(got))
	}
}

// delayerFixture is a delayer built out of the in-memory parts, so that a hold can be tested without
// a database and without waiting for a hold to run out.
type delayerFixture struct {
	delayed   *fakes.DelayedCalls
	calls     *fakes.Calls
	scheduler *fakes.Scheduler
	clock     *fakes.Clock
	logs      *fakes.Logs

	delayer *Delayer
}

func newDelayerFixture() *delayerFixture {
	fixture := &delayerFixture{
		delayed:   &fakes.DelayedCalls{},
		calls:     fakes.NewCalls(),
		scheduler: fakes.NewScheduler(),
		clock:     fakes.NewClock(heldAt),
		logs:      &fakes.Logs{},
	}

	fixture.delayer = New(Deps{
		Delayed:   fixture.delayed,
		Calls:     fixture.calls,
		Scheduler: fixture.scheduler,
		Clock:     fixture.clock,
		Logs:      fixture.logs,
	})

	return fixture
}

// heldCall is a recorded call on a talkgroup with a hold named in minutes.
func heldCall(id uint64, at time.Time, delayMinutes uint) domain.Call {
	call := domain.NewCall()

	call.ID = id
	call.Timestamp = at
	call.Audio = []byte("a recording long enough to be a recording")
	call.Talkgroup = domain.Talkgroup{ID: 1, Delay: delayMinutes}
	call.System = domain.System{ID: 1}

	return call
}

// delivered takes the calls the delayer has handed over and are waiting to be collected. It never
// waits: a delayer that has handed over nothing has handed over nothing.
func delivered(delayer *Delayer) []domain.Call {
	got := []domain.Call{}

	for {
		select {
		case call := <-delayer.Waiting():
			got = append(got, call)

		default:
			return got
		}
	}
}

// stuckDelayed is a waiting list that can be written to but never cleared, which is the failure a hold
// that has run out can meet.
type stuckDelayed struct {
	*fakes.DelayedCalls
}

func (stuck *stuckDelayed) UnscheduleDelayed(ctx context.Context, callID uint64) error {
	return errors.New("the row will not go away")
}
