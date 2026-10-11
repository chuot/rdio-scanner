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
	"bytes"
	"testing"
	"time"
)

func validCall() Call {
	return Call{
		Audio:     bytes.Repeat([]byte{0}, 45),
		Timestamp: time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
		System:    System{ID: 7, SystemRef: 4},
		Talkgroup: Talkgroup{ID: 21, TalkgroupRef: 1514000},
	}
}

func TestNewCallStartsWithEmptyCollectionsNotEmpty(t *testing.T) {
	call := NewCall()

	// The wire contract marshals these as [] and never as null.
	if call.Frequencies == nil || call.PatchTalkgroupRefs == nil || call.Units == nil {
		t.Errorf("NewCall = %+v, want non-nil collections so the payload marshals as []", call)
	}

	if call.Meta.TalkgroupGroup == nil || call.Meta.UnitRefs == nil || call.Meta.UnitLabels == nil {
		t.Errorf("NewCall meta = %+v, want non-nil collections", call.Meta)
	}
}

func TestCallIsValidAcceptsACompleteCall(t *testing.T) {
	if err := validCall().IsValid(); err != nil {
		t.Fatalf("IsValid = %v, want a complete call to be valid", err)
	}
}

func TestCallIsValidRejectsMissingAudio(t *testing.T) {
	call := validCall()
	call.Audio = bytes.Repeat([]byte{0}, 44)

	if err := call.IsValid(); err == nil {
		t.Fatal("a 44-byte audio payload must be rejected; the legacy server requires more than a bare WAV header")
	}

	call.Audio = bytes.Repeat([]byte{0}, 45)

	if err := call.IsValid(); err != nil {
		t.Errorf("IsValid = %v, want 45 bytes to be accepted", err)
	}

	call.Audio = nil

	if err := call.IsValid(); err == nil {
		t.Error("nil audio must be rejected")
	}
}

func TestCallIsValidRejectsMissingTimestamp(t *testing.T) {
	call := validCall()
	call.Timestamp = time.UnixMilli(0)

	if err := call.IsValid(); err == nil {
		t.Fatal("a call stamped at the epoch must be rejected as having no timestamp")
	}
}

func TestCallIsValidRejectsACallWithNoSystem(t *testing.T) {
	call := validCall()
	call.System = System{}
	call.Meta = IngestMeta{}

	if err := call.IsValid(); err == nil {
		t.Fatal("a call with no resolved system and no system claim must be rejected")
	}
}

func TestCallIsValidRejectsACallWithNoTalkgroup(t *testing.T) {
	call := validCall()
	call.Talkgroup = Talkgroup{}
	call.Meta = IngestMeta{}

	if err := call.IsValid(); err == nil {
		t.Fatal("a call with no resolved talkgroup and no talkgroup claim must be rejected")
	}
}

// TestCallIsValidAcceptsAnySystemClaim pins the legacy IsValid disjunction: an
// unresolved system is acceptable as long as the ingest source claimed one of the
// three ways to name it.
func TestCallIsValidAcceptsAnySystemClaim(t *testing.T) {
	claims := []IngestMeta{
		{SystemID: 7},
		{SystemRef: 4},
		{SystemLabel: "Gaston County Conventional"},
	}

	for _, meta := range claims {
		call := validCall()
		call.System = System{}
		call.Meta = meta

		if err := call.IsValid(); err != nil {
			t.Errorf("IsValid with claim %+v = %v, want the claim to satisfy the requirement", meta, err)
		}
	}
}

func TestCallIsValidAcceptsAnyTalkgroupClaim(t *testing.T) {
	claims := []IngestMeta{
		{TalkgroupID: 21},
		{TalkgroupRef: 1514000},
		{TalkgroupLabel: "GASTON-FIRE"},
	}

	for _, meta := range claims {
		call := validCall()
		call.Talkgroup = Talkgroup{}
		call.Meta = meta

		if err := call.IsValid(); err != nil {
			t.Errorf("IsValid with claim %+v = %v, want the claim to satisfy the requirement", meta, err)
		}
	}
}

// TestIsDuplicateOfUsesTheLegacyRule pins Calls.CheckDuplicate: the same system
// identifier, the same talkgroup identifier, and a timestamp within the window. Unit
// sets, labels and audio are irrelevant to the rule.
func TestIsDuplicateOfUsesTheLegacyRule(t *testing.T) {
	window := DefaultOptions().DuplicateDetectionWindow()

	first := validCall()

	same := validCall()
	if !same.IsDuplicateOf(first, window) {
		t.Error("an identical call at the same instant must be a duplicate")
	}

	earlier := validCall()
	earlier.Timestamp = first.Timestamp.Add(-400 * time.Millisecond)

	if !earlier.IsDuplicateOf(first, window) {
		t.Error("a call 400 ms earlier, inside the 500 ms window, must be a duplicate")
	}

	later := validCall()
	later.Timestamp = first.Timestamp.Add(400 * time.Millisecond)

	if !later.IsDuplicateOf(first, window) {
		t.Error("the window is symmetric; a call 400 ms later must also be a duplicate")
	}

	outside := validCall()
	outside.Timestamp = first.Timestamp.Add(600 * time.Millisecond)

	if outside.IsDuplicateOf(first, window) {
		t.Error("a call 600 ms away, outside the 500 ms window, must not be a duplicate")
	}

	boundary := validCall()
	boundary.Timestamp = first.Timestamp.Add(window)

	if !boundary.IsDuplicateOf(first, window) {
		t.Error("the window boundary is inclusive; a call exactly one window away is a duplicate")
	}

	otherSystem := validCall()
	otherSystem.System = System{ID: 8, SystemRef: 5}

	if otherSystem.IsDuplicateOf(first, window) {
		t.Error("a call on another system must not be a duplicate")
	}

	otherTalkgroup := validCall()
	otherTalkgroup.Talkgroup = Talkgroup{ID: 22, TalkgroupRef: 1514001}

	if otherTalkgroup.IsDuplicateOf(first, window) {
		t.Error("a call on another talkgroup must not be a duplicate")
	}

	unitsDiffering := validCall()
	unitsDiffering.Units = []CallUnit{{UnitRef: 710618, Offset: 0.5}}

	if !unitsDiffering.IsDuplicateOf(first, window) {
		t.Error("unit content is not part of the duplicate rule")
	}
}

// TestIsDuplicateOfRequiresTheSameSite pins the site rule: calls on the same system
// and talkgroup are duplicates only when they share the site. A call with no site
// (SiteRef 0) is a duplicate only of site-less calls; a site-bearing call never
// matches a site-less one, in either direction.
func TestIsDuplicateOfRequiresTheSameSite(t *testing.T) {
	window := DefaultOptions().DuplicateDetectionWindow()

	first := validCall()
	first.SiteRef = 3

	sameSite := validCall()
	sameSite.SiteRef = 3

	if !sameSite.IsDuplicateOf(first, window) {
		t.Error("a call on the same site, system and talkgroup must be a duplicate")
	}

	otherSite := validCall()
	otherSite.SiteRef = 4

	if otherSite.IsDuplicateOf(first, window) {
		t.Error("the same talkgroup on another site is a real call, not a duplicate")
	}

	siteless := validCall()

	if siteless.IsDuplicateOf(first, window) {
		t.Error("a site-less call must not be reported as a duplicate of a site-bearing call")
	}

	if first.IsDuplicateOf(siteless, window) {
		t.Error("a site-bearing call must not be reported as a duplicate of a site-less call")
	}

	pair := validCall()

	if !pair.IsDuplicateOf(siteless, window) {
		t.Error("two site-less calls on the same system and talkgroup are duplicates of each other")
	}
}

func TestIsDuplicateOfNeverMatchesWithoutResolvedIdentities(t *testing.T) {
	window := DefaultOptions().DuplicateDetectionWindow()

	// Two calls that both failed to resolve their system and talkgroup share the
	// zero identifiers; matching on those would wrongly suppress real calls.
	unresolved := validCall()
	unresolved.System = System{}
	unresolved.Talkgroup = Talkgroup{}

	other := validCall()
	other.System = System{}
	other.Talkgroup = Talkgroup{}

	if unresolved.IsDuplicateOf(other, window) {
		t.Error("calls whose system and talkgroup are not resolved must not be reported as duplicates of each other")
	}
}

func TestIsDuplicateOfWithAZeroWindowDetectsNothing(t *testing.T) {
	options := DefaultOptions()
	options.DisableDuplicateDetection = true

	if validCall().IsDuplicateOf(validCall(), options.DuplicateDetectionWindow()) {
		t.Error("with detection disabled the window is zero and nothing may be reported as a duplicate")
	}
}

func TestDuplicateWindowIsSymmetricAroundTheCall(t *testing.T) {
	call := validCall()
	window := 500 * time.Millisecond

	from, to := call.DuplicateWindow(window)

	if want := call.Timestamp.Add(-window); !from.Equal(want) {
		t.Errorf("window start = %v, want %v", from, want)
	}

	if want := call.Timestamp.Add(window); !to.Equal(want) {
		t.Errorf("window end = %v, want %v", to, want)
	}
}

func TestCallFrequencyAndUnitCarryTheirOwnValues(t *testing.T) {
	call := validCall()
	call.Frequencies = []CallFrequency{
		{Dbm: -85, Errors: 1, Frequency: 151400, Offset: 0.25, Spikes: 3},
	}
	call.Units = []CallUnit{{UnitRef: 710618, Offset: 0.5}}
	call.PatchTalkgroupRefs = []uint{1514001}

	if call.Frequencies[0].Frequency != 151400 || call.Frequencies[0].Dbm != -85 {
		t.Errorf("frequencies = %+v, want the submitted values", call.Frequencies)
	}

	if call.Units[0].UnitRef != 710618 || call.Units[0].Offset != 0.5 {
		t.Errorf("units = %+v, want the submitted values", call.Units)
	}

	if len(call.PatchTalkgroupRefs) != 1 || call.PatchTalkgroupRefs[0] != 1514001 {
		t.Errorf("patches = %v, want the submitted talkgroup reference", call.PatchTalkgroupRefs)
	}
}

// A call arrives without knowing how long its recording is: the length is something the server
// learns later, from the program that converts the audio or the program that probes it, and a
// call it never learned says so by carrying no length.
func TestNewCallKnowsNoAudioLength(t *testing.T) {
	call := NewCall()

	if call.AudioLength != 0 {
		t.Errorf("AudioLength = %v, want a new call to know no length", call.AudioLength)
	}
}

// The length is a fact the server learns, not a fact a call needs: a call whose length was
// never measured is as valid as one whose length is known.
func TestCallIsValidNeverRejectsAnUnknownAudioLength(t *testing.T) {
	unknown := validCall()

	if err := unknown.IsValid(); err != nil {
		t.Errorf("IsValid = %v, want a call with no known length to be valid", err)
	}

	known := validCall()
	known.AudioLength = 3500 * time.Millisecond

	if err := known.IsValid(); err != nil {
		t.Errorf("IsValid = %v, want a call with a known length to be valid", err)
	}
}
