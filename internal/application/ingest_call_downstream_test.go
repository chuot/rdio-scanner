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
	"errors"
	"testing"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

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
