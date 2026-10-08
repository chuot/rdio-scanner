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
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

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
