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
)

func TestLivefeedStartsAllOff(t *testing.T) {
	livefeed := NewLivefeed()

	if !livefeed.AllOff() {
		t.Error("a fresh livefeed selection must be all off")
	}

	if livefeed.Watches(scopedIngestCall()) {
		t.Error("an all-off selection must watch nothing")
	}
}

func TestParseLivefeedReadsReferencesNotIdentifiers(t *testing.T) {
	livefeed := ParseLivefeed(map[string]any{
		"4": map[string]any{"1514000": true},
	})

	if !livefeed.Watches(scopedIngestCall()) {
		t.Fatal("the selection is keyed by system reference and talkgroup reference")
	}

	otherSystem := scopedIngestCall()
	otherSystem.System = System{ID: 8, SystemRef: 5}

	if livefeed.Watches(otherSystem) {
		t.Error("a selection on another system must not watch the call")
	}

	otherTalkgroup := scopedIngestCall()
	otherTalkgroup.Talkgroup = Talkgroup{ID: 22, TalkgroupRef: 1514001}

	if livefeed.Watches(otherTalkgroup) {
		t.Error("a selection on another talkgroup must not watch the call")
	}
}

func TestParseLivefeedSwitchesOff(t *testing.T) {
	on := ParseLivefeed(map[string]any{"4": map[string]any{"1514000": true}})

	if on.AllOff() {
		t.Fatal("the fixture must start switched on")
	}

	off := ParseLivefeed(map[string]any{"4": map[string]any{"1514000": false}})

	if !off.AllOff() {
		t.Error("a selection switched off must report all off")
	}

	if off.Watches(scopedIngestCall()) {
		t.Error("a talkgroup switched off must not be watched")
	}
}

func TestParseLivefeedReplacesThePreviousSelection(t *testing.T) {
	first := ParseLivefeed(map[string]any{"4": map[string]any{"1514000": true}})

	if !first.Watches(scopedIngestCall()) {
		t.Fatal("the fixture must start watching the call")
	}

	second := ParseLivefeed(map[string]any{"4": map[string]any{"1514001": true}})

	if second.Watches(scopedIngestCall()) {
		t.Error("a new selection must replace the previous one rather than add to it")
	}
}

func TestParseLivefeedIgnoresUnusableEntries(t *testing.T) {
	livefeed := ParseLivefeed(map[string]any{
		"not a number": map[string]any{"1514000": true},
		"4":            "not a talkgroup map",
		"5":            map[string]any{"1514000": "not a boolean"},
		"6":            map[string]any{"not a number": true},
	})

	if !livefeed.AllOff() {
		t.Errorf("ParseLivefeed produced %+v, want nothing switched on", livefeed.Matrix)
	}

	if ParseLivefeed("not a map at all").Matrix == nil {
		t.Error("a selection that is not a map must still produce a usable empty selection")
	}
}

func TestLivefeedWatchesPatchedTalkgroups(t *testing.T) {
	call := scopedIngestCall()
	call.Talkgroup = Talkgroup{ID: 22, TalkgroupRef: 1514001}
	call.PatchTalkgroupRefs = []uint{1514000}

	livefeed := ParseLivefeed(map[string]any{"4": map[string]any{"1514000": true}})

	if !livefeed.Watches(call) {
		t.Error("a call patched into a watched talkgroup must be watched")
	}

	otherSystem := call
	otherSystem.System = System{ID: 8, SystemRef: 5}

	if livefeed.Watches(otherSystem) {
		t.Error("a patch must be watched only on the system it belongs to")
	}
}
