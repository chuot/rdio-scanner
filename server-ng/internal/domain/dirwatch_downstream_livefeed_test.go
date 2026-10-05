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

func TestNewDirwatchCarriesTheLegacyDefaults(t *testing.T) {
	dirwatch := NewDirwatch()

	if dirwatch.Delay != 2000 {
		t.Errorf("Delay = %d, want the legacy default of 2000 ms", dirwatch.Delay)
	}

	if !dirwatch.DeleteAfter {
		t.Error("DeleteAfter must default to true")
	}

	if dirwatch.Kind != DirwatchTypeDefault {
		t.Errorf("Kind = %q, want the legacy default %q", dirwatch.Kind, DirwatchTypeDefault)
	}
}

// TestDirwatchApplyMapReadsTheTypeKey pins the key drift: the administrator payload
// names the parser "type", while the server calls it Kind everywhere else.
func TestDirwatchApplyMapReadsTheTypeKey(t *testing.T) {
	dirwatch := NewDirwatch().ApplyMap(map[string]any{
		"id":          float64(2),
		"delay":       float64(5000),
		"deleteAfter": false,
		"directory":   "/var/spool/dsdplus",
		"disabled":    true,
		"extension":   ".wav",
		"frequency":   float64(100),
		"type":        DirwatchTypeDSDPlus,
		"mask":        "{TYPE} {TGID} {UID} {TIME}",
		"order":       float64(3),
		"siteId":      float64(11),
		"systemId":    float64(7),
		"talkgroupId": float64(21),
	})

	if dirwatch.Kind != DirwatchTypeDSDPlus {
		t.Errorf("Kind = %q, want %q read from the type key", dirwatch.Kind, DirwatchTypeDSDPlus)
	}

	if dirwatch.ID != 2 || dirwatch.Delay != 5000 || dirwatch.DeleteAfter || !dirwatch.Disabled ||
		dirwatch.Directory != "/var/spool/dsdplus" || dirwatch.Extension != ".wav" || dirwatch.Frequency != 100 ||
		dirwatch.Mask != "{TYPE} {TGID} {UID} {TIME}" || dirwatch.Order != 3 ||
		dirwatch.SiteID != 11 || dirwatch.SystemID != 7 || dirwatch.TalkgroupID != 21 {
		t.Errorf("ApplyMap = %+v, want every submitted field carried through", dirwatch)
	}
}

func TestDirwatchWatchesNeedsADirectoryAndNotBeingDisabled(t *testing.T) {
	if !(Dirwatch{Directory: "/tmp/x"}).Watches() {
		t.Error("an enabled watch with a directory must be watched")
	}

	if (Dirwatch{}).Watches() {
		t.Error("a watch with no directory watches nothing")
	}

	if (Dirwatch{Directory: "/tmp/x", Disabled: true}).Watches() {
		t.Error("a disabled watch must not be watched")
	}
}

func TestDirwatchesApplyListReplacesTheSetAndKeepsDefaults(t *testing.T) {
	previous := Dirwatches{Dirwatch{Directory: "/gone"}, Dirwatch{Directory: "/kept"}}

	next := previous.ApplyList([]any{
		map[string]any{"directory": "/kept"},
		map[string]any{"directory": "/new", "type": DirwatchTypeTrunkRecorder},
		"not a watch",
	})

	if len(next) != 2 {
		t.Fatalf("ApplyList produced %d watches, want the two usable entries of the payload", len(next))
	}

	if next[0].Directory != "/kept" || next[1].Directory != "/new" {
		t.Errorf("ApplyList = %+v, want the set replaced by the payload", next)
	}

	if next[0].Delay != 2000 || next[0].Kind != DirwatchTypeDefault {
		t.Errorf("ApplyList = %+v, want the defaults applied to entries that do not override them", next[0])
	}

	if next[1].Kind != DirwatchTypeTrunkRecorder {
		t.Errorf("Kind = %q, want the submitted parser", next[1].Kind)
	}
}

func TestDirwatchesEnabledSkipsTheUnwatchable(t *testing.T) {
	all := Dirwatches{
		Dirwatch{Directory: "/a"},
		Dirwatch{Directory: "/b", Disabled: true},
		Dirwatch{},
	}

	enabled := all.Enabled()

	if len(enabled) != 1 || enabled[0].Directory != "/a" {
		t.Errorf("Enabled = %+v, want only the enabled watch with a directory", enabled)
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
