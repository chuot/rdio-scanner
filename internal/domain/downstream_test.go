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

package domain

import (
	"testing"
	"time"
)

func scopedIngestCall() Call {
	return Call{
		System:    System{ID: 7, SystemRef: 4},
		Talkgroup: Talkgroup{ID: 21, TalkgroupRef: 1514000},
		Timestamp: time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
	}
}

func TestNewDownstreamHasNoScopeAndForwardsNothing(t *testing.T) {
	// The legacy NewDownstream leaves the scope unset, and a downstream with no scope
	// forwards nothing. The wildcard default belongs to the seeded rows, not to a
	// freshly built one.
	downstream := Downstream{}

	if downstream.Accepts(scopedIngestCall()) {
		t.Error("a downstream with no scope must accept nothing")
	}

	if downstream.Forwards() {
		t.Error("a downstream with no URL cannot be posted to")
	}
}

func TestDownstreamApplyMap(t *testing.T) {
	downstream := Downstream{}.ApplyMap(map[string]any{
		"id":       float64(4),
		"apikey":   "upload-key",
		"disabled": false,
		"order":    float64(2),
		"systems":  "*",
		"url":      "https://other.example/api/call-upload",
	})

	if downstream.ID != 4 || downstream.Apikey != "upload-key" || downstream.Order != 2 || downstream.URL != "https://other.example/api/call-upload" {
		t.Errorf("ApplyMap = %+v, want the submitted fields", downstream)
	}

	if !downstream.Accepts(scopedIngestCall()) {
		t.Error("an unrestricted downstream must accept the call")
	}

	if !downstream.Forwards() {
		t.Error("an enabled downstream with a URL must forward")
	}
}

func TestDownstreamAcceptsRespectsScopeAndDisabledFlag(t *testing.T) {
	scoped := Downstream{}.ApplyMap(map[string]any{
		"systems": []any{map[string]any{"id": float64(9), "talkgroups": "*"}},
		"url":     "https://other.example/api/call-upload",
	})

	if scoped.Accepts(scopedIngestCall()) {
		t.Error("a downstream scoped to another system must not accept the call")
	}

	disabled := scoped
	disabled.Disabled = true

	if disabled.Accepts(scopedIngestCall()) {
		t.Error("a disabled downstream must accept nothing")
	}

	if disabled.Forwards() {
		t.Error("a disabled downstream must not forward")
	}
}

func TestDownstreamsAcceptingSelectsTheMatchingOnes(t *testing.T) {
	all := Downstreams{
		Downstream{}.ApplyMap(map[string]any{"systems": "*", "url": "https://a.example"}),
		Downstream{}.ApplyMap(map[string]any{"systems": "*", "url": "https://b.example", "disabled": true}),
		Downstream{}.ApplyMap(map[string]any{"systems": "*", "url": ""}),
		Downstream{}.ApplyMap(map[string]any{"systems": []any{map[string]any{"id": float64(4), "talkgroups": []any{float64(1514000)}}}, "url": "https://c.example"}),
	}

	matching := all.Accepting(scopedIngestCall())

	if len(matching) != 2 {
		t.Fatalf("Accepting produced %d downstreams, want the unrestricted one and the scoped match", len(matching))
	}

	if matching[0].URL != "https://a.example" || matching[1].URL != "https://c.example" {
		t.Errorf("Accepting = %+v, want the disabled and URL-less downstreams left out", matching)
	}
}

func TestDownstreamsApplyListAndOrder(t *testing.T) {
	list := Downstreams{}.ApplyList([]any{
		map[string]any{"url": "https://second.example", "order": float64(5)},
		"not a downstream",
		map[string]any{"url": "https://first.example", "order": float64(1)},
	})

	if len(list) != 2 {
		t.Fatalf("ApplyList produced %d downstreams, want the two usable entries", len(list))
	}

	ordered := list.Ordered()

	if ordered[0].URL != "https://first.example" || ordered[1].URL != "https://second.example" {
		t.Errorf("Ordered = %+v, want the list sorted by display order", ordered)
	}
}

// TestDownstreamsValidateRefusesANonHttpAddress: the
// forwarder posts recordings and the apikey to an address the administrator typed. An
// address that is not http or https — ftp://, gopher://, a name that is not an address
// at all — is refused at apply, before it can be stored and read by the forwarder. An
// empty address is a downstream that forwards nothing, which the list already allows.
func TestDownstreamsValidateRefusesANonHttpAddress(t *testing.T) {
	for _, url := range []string{"ftp://peer.example/upload", "gopher://peer.example", "not-an-address", "https:"} {
		stored := Downstreams{{ID: 1, URL: url}}

		if err := stored.Validate(); err == nil {
			t.Errorf("Validate = nil for the downstream address %q, want it refused", url)
		}
	}

	for _, url := range []string{"http://peer.example", "https://peer.example", "https://peer.example/api/call-upload", ""} {
		stored := Downstreams{{ID: 1, URL: url}}

		if err := stored.Validate(); err != nil {
			t.Errorf("Validate = %v for the downstream address %q, want it accepted", err, url)
		}
	}
}
