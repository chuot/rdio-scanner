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

package fakes

import (
	"testing"

	"rdio-scanner/internal/domain"
)

func TestHubDeliversOnlyToSessionsThatGrantedAndAskedForTheCall(t *testing.T) {
	hub := NewHub()
	hub.Restricted = true

	hub.Open("watcher", "10.0.0.1")
	hub.Open("silent", "10.0.0.2")
	hub.Open("unscoped", "10.0.0.3")

	scoped := domain.Access{}.ApplyMap(map[string]any{"code": "c", "ident": "dispatch", "systems": "*"})

	hub.GrantAccess("watcher", scoped)
	hub.GrantAccess("silent", scoped)

	call := testCall(1, 4, 1514000, baseTime())

	hub.SetLivefeed("watcher", domain.ParseLivefeed(map[string]any{"4": map[string]any{"1514000": true}}))

	hub.BroadcastCall(call)

	if len(hub.DeliveredCalls["watcher"]) != 1 {
		t.Error("a session that granted the call and asked for it must receive it")
	}

	if len(hub.DeliveredCalls["silent"]) != 0 {
		t.Error("a session that asked for nothing must receive nothing")
	}

	if len(hub.DeliveredCalls["unscoped"]) != 0 {
		t.Error("a restricted server must not deliver to a session holding no access")
	}
}

func TestHubCountsSessionsPerAccess(t *testing.T) {
	hub := NewHub()

	shared := domain.Access{ID: 1, Code: "a", Ident: "dispatch", Scope: domain.AccessScope{Present: true, Wildcard: true}}
	other := domain.Access{ID: 2, Code: "b", Ident: "dispatch", Scope: domain.AccessScope{Present: true, Wildcard: true}}

	hub.Open("one", "10.0.0.1")
	hub.Open("two", "10.0.0.2")
	hub.Open("three", "10.0.0.3")

	hub.GrantAccess("one", shared)
	hub.GrantAccess("two", shared)
	hub.GrantAccess("three", other)

	if got := hub.AccessSessionCount(1); got != 2 {
		t.Errorf("AccessSessionCount(1) = %d, want the two sessions holding that access", got)
	}

	if got := hub.AccessSessionCount(2); got != 1 {
		t.Errorf("AccessSessionCount(2) = %d, want one session: two accesses sharing a label are not one access", got)
	}

	if got := hub.ListenerCount(); got != 3 {
		t.Errorf("ListenerCount = %d, want 3", got)
	}

	if got := hub.RemoteAddrOf("two"); got != "10.0.0.2" {
		t.Errorf("RemoteAddrOf = %q, want the address the session connected from", got)
	}
}

func TestHubCountsAccessAttemptsPerSession(t *testing.T) {
	hub := NewHub()

	hub.Open("one", "10.0.0.1")
	hub.Open("two", "10.0.0.2")

	for want := uint(1); want <= 3; want++ {
		if got := hub.RecordAccessAttempt("one"); got != want {
			t.Errorf("RecordAccessAttempt = %d, want attempt %d", got, want)
		}
	}

	if got := hub.RecordAccessAttempt("two"); got != 1 {
		t.Errorf("RecordAccessAttempt for another session = %d, want 1: attempts are counted per session", got)
	}

	hub.CloseClient("one", 1000)

	if got := hub.RecordAccessAttempt("one"); got != 1 {
		t.Errorf("RecordAccessAttempt after the session closed = %d, want the count to start over for a new session", got)
	}
}

func TestHubLivefeedAckReflectsTheSelection(t *testing.T) {
	hub := NewHub()
	hub.Open("watcher", "10.0.0.1")

	if !hub.LivefeedWatchesNothing("watcher") {
		t.Error("a session that has made no selection watches nothing")
	}

	hub.SetLivefeed("watcher", domain.ParseLivefeed(map[string]any{"4": map[string]any{"1514000": true}}))

	if hub.LivefeedWatchesNothing("watcher") {
		t.Error("a session that switched a talkgroup on is not watching nothing")
	}
}
