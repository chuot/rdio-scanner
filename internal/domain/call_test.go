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

// TestCallIsValidAcceptsAnyTalkgroupClaim pins the IsValid disjunction: an unresolved
// talkgroup is acceptable as long as the ingest source claimed it by number, either the
// stored identity or the client-facing reference. A name is not a number, and a call is
// only ever filed under one.
func TestCallIsValidAcceptsAnyTalkgroupClaim(t *testing.T) {
	claims := []IngestMeta{
		{TalkgroupID: 21},
		{TalkgroupRef: 1514000},
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

// TestCallIsValidRefusesACallWhoseOnlyTalkgroupClaimIsAName is the mask shape that reads
// a talkgroup by name and nothing else: the recording carries a label where a number
// belongs, and 6.6.3 refused it for the same reason it refuses a recording that names no
// talkgroup at all. The label is what auto-populate writes after a call is filed, never
// what a call is filed under, so a name beside a number stays a label.
func TestCallIsValidRefusesACallWhoseOnlyTalkgroupClaimIsAName(t *testing.T) {
	call := validCall()
	call.Talkgroup = Talkgroup{}
	call.Meta = IngestMeta{
		SystemRef:      4,
		TalkgroupLabel: "GASTON-FIRE",
	}

	err := call.IsValid()

	if err == nil {
		t.Fatal("a call whose only talkgroup claim is a name was accepted, want it refused: the server files a call under a talkgroup number")
	}

	if err.Error() != "no talkgroup" {
		t.Errorf("IsValid = %v, want the reason to say the call names no talkgroup", err)
	}

	call.Meta.TalkgroupRef = 1514000

	if err := call.IsValid(); err != nil {
		t.Errorf("IsValid with a number beside the name = %v, want the number to satisfy the requirement and the name to stay a label", err)
	}
}

// TestCallIsValidNamesTheLastDeficiencyTheCallHas pins which reason a call that fails
// more than one check is refused for: the last check that fails, out of the four the
// checks always run in — audio beyond its container header, a stated moment, a system,
// a talkgroup. A submission that is short in several ways is one call, and the sentence
// an ingest client or an operator reads names the last thing it was missing.
func TestCallIsValidNamesTheLastDeficiencyTheCallHas(t *testing.T) {
	noAudio := func(call *Call) { call.Audio = bytes.Repeat([]byte{0}, MinimumAudioLength) }
	noMoment := func(call *Call) { call.Timestamp = time.UnixMilli(0) }
	noSystem := func(call *Call) { call.System = System{} }
	noTalkgroup := func(call *Call) { call.Talkgroup = Talkgroup{} }

	both := func(first, second func(*Call)) func(*Call) {
		return func(call *Call) {
			first(call)
			second(call)
		}
	}

	all := func(first, second, third func(*Call)) func(*Call) {
		return func(call *Call) {
			first(call)
			second(call)
			third(call)
		}
	}

	cases := []struct {
		missing string
		strip   func(call *Call)
		want    string
	}{
		{missing: "audio", strip: noAudio, want: "no audio"},
		{missing: "a moment", strip: noMoment, want: "no timestamp"},
		{missing: "a system", strip: noSystem, want: "no system"},
		{missing: "a talkgroup", strip: noTalkgroup, want: "no talkgroup"},

		{missing: "audio and a moment", strip: both(noAudio, noMoment), want: "no timestamp"},
		{missing: "audio and a system", strip: both(noAudio, noSystem), want: "no system"},
		{missing: "audio and a talkgroup", strip: both(noAudio, noTalkgroup), want: "no talkgroup"},
		{missing: "a moment and a system", strip: both(noMoment, noSystem), want: "no system"},
		{missing: "a moment and a talkgroup", strip: both(noMoment, noTalkgroup), want: "no talkgroup"},
		{missing: "a system and a talkgroup", strip: both(noSystem, noTalkgroup), want: "no talkgroup"},

		{missing: "audio, a moment and a system", strip: all(noAudio, noMoment, noSystem), want: "no system"},
		{missing: "audio, a moment and a talkgroup", strip: all(noAudio, noMoment, noTalkgroup), want: "no talkgroup"},
		{missing: "audio, a system and a talkgroup", strip: all(noAudio, noSystem, noTalkgroup), want: "no talkgroup"},
		{missing: "a moment, a system and a talkgroup", strip: all(noMoment, noSystem, noTalkgroup), want: "no talkgroup"},

		{
			missing: "audio, a moment, a system and a talkgroup",
			strip:   func(call *Call) { noAudio(call); noMoment(call); noSystem(call); noTalkgroup(call) },
			want:    "no talkgroup",
		},
	}

	for _, testCase := range cases {
		call := validCall()
		testCase.strip(&call)

		err := call.IsValid()

		if err == nil {
			t.Errorf("IsValid for a call missing %s = nil, want it refused for %q", testCase.missing, testCase.want)
			continue
		}

		if err.Error() != testCase.want {
			t.Errorf("IsValid for a call missing %s reports %q, want %q: the checks run in the order audio, "+
				"moment, system, talkgroup, and the refusal names the last one that fails",
				testCase.missing, err.Error(), testCase.want)
		}
	}
}

// TestCallIsValidAcceptsACallThatStatesNoMomentAndRefusesOneStampedAtTheEpoch keeps the
// two halves of the timestamp guard apart: a call that names no moment at all is accepted
// and dated to the year 1, and only a call that names the epoch itself is refused. The
// last reading is the guard's own failure passing behind a later one, which is what an
// ingest client sees when a submission is short in more than one way.
func TestCallIsValidAcceptsACallThatStatesNoMomentAndRefusesOneStampedAtTheEpoch(t *testing.T) {
	unstamped := validCall()
	unstamped.Timestamp = time.Time{}

	if err := unstamped.IsValid(); err != nil {
		t.Errorf("IsValid for a complete call naming no moment = %v, want it accepted and dated to the year 1", err)
	}

	if unstamped.Timestamp.Year() != 1 {
		t.Errorf("a call naming no moment is dated to the year %d, want the year 1 the guard was written to catch",
			unstamped.Timestamp.Year())
	}

	epoch := validCall()
	epoch.Timestamp = time.UnixMilli(0)

	if err := epoch.IsValid(); err == nil || err.Error() != "no timestamp" {
		t.Errorf("IsValid for a call stamped at the epoch = %v, want it refused for no timestamp", err)
	}

	epochNamesNoSystem := epoch
	epochNamesNoSystem.System = System{}

	if err := epochNamesNoSystem.IsValid(); err == nil || err.Error() != "no system" {
		t.Errorf("IsValid for a call stamped at the epoch that names no system = %v, want it refused for no "+
			"system: the epoch is only one of the four checks, and a later failure is what the refusal names",
			err)
	}
}

// TestACallCarryingTheCheckClaimAndNoRecordingIsRecognisedAsAConnectionCheck pins what the server
// reads a submission that announces itself as a connection check as. The announcement alone, or
// the announcement beside nothing but an audio container header, is a connection check: a
// submission that asks whether this server will take a recording and brings no recording to it.
// The same announcement beside audio past that header is a recording, because a connection check
// never brings audio, and a submission that announces nothing is never a check however short it
// is.
func TestACallCarryingTheCheckClaimAndNoRecordingIsRecognisedAsAConnectionCheck(t *testing.T) {
	cases := []struct {
		name  string
		meta  IngestMeta
		audio []byte
		want  bool
	}{
		{
			name: "announces a connection check and carries no audio part",
			meta: IngestMeta{ConnectionCheck: true, SystemRef: 10001},
			want: true,
		},
		{
			name:  "announces a connection check and carries audio no longer than a container header",
			meta:  IngestMeta{ConnectionCheck: true, SystemRef: 10001},
			audio: bytes.Repeat([]byte{0}, MinimumAudioLength),
			want:  true,
		},
		{
			name:  "announces a connection check and carries audio past a container header",
			meta:  IngestMeta{ConnectionCheck: true, SystemRef: 10001},
			audio: bytes.Repeat([]byte{0}, MinimumAudioLength+1),
			want:  false,
		},
		{
			name:  "announces a connection check and carries a recording beside the talkgroup it names",
			meta:  IngestMeta{ConnectionCheck: true, SystemRef: 10001, TalkgroupRef: 1514000},
			audio: bytes.Repeat([]byte{0}, 4096),
			want:  false,
		},
		{
			name: "announces nothing and carries no audio part",
			meta: IngestMeta{SystemRef: 10001},
			want: false,
		},
		{
			name:  "announces nothing and carries audio no longer than a container header",
			meta:  IngestMeta{SystemRef: 10001},
			audio: bytes.Repeat([]byte{0}, MinimumAudioLength),
			want:  false,
		},
	}

	for _, testCase := range cases {
		call := NewCall()
		call.Meta = testCase.meta
		call.Audio = testCase.audio
		call.Timestamp = time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

		if got := call.IsConnectionCheck(); got != testCase.want {
			t.Errorf("IsConnectionCheck for a submission that %s = %t, want %t: the announcement and the "+
				"absence of a recording together decide it, and audio past a container header always makes "+
				"one, whatever the submission announced", testCase.name, got, testCase.want)
		}
	}
}

// TestTheCheckClaimDoesNotMakeACallOutOfASubmissionThatIsShortOfOne pins that announcing a
// connection check is only an announcement. A submission shaped like one is refused for the
// deficiency it has, by the same four guards in the same order naming the same last deficiency as
// every other submission, and a submission that carries everything a recording needs is accepted
// whatever it announces and is never read as a check.
func TestTheCheckClaimDoesNotMakeACallOutOfASubmissionThatIsShortOfOne(t *testing.T) {
	check := NewCall()
	check.Meta = IngestMeta{ConnectionCheck: true, SystemRef: 10001}
	check.Timestamp = time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

	if err := check.IsValid(); err == nil || err.Error() != "no talkgroup" {
		t.Errorf("IsValid for a submission shaped like a connection check = %v, want it refused for "+
			"\"no talkgroup\": the deficiency decides the refusal, and a submission that names a system "+
			"and no talkgroup is short of a talkgroup whatever it announces", err)
	}

	recording := validCall()
	recording.Meta = IngestMeta{ConnectionCheck: true}

	if err := recording.IsValid(); err != nil {
		t.Errorf("IsValid for a complete submission that announces a connection check = %v, want it accepted", err)
	}

	if recording.IsConnectionCheck() {
		t.Error("a complete submission that announces a connection check is read as a connection check, want " +
			"it read as the recording it carries")
	}

	noAudio := func(call *Call) { call.Audio = bytes.Repeat([]byte{0}, MinimumAudioLength) }
	noMoment := func(call *Call) { call.Timestamp = time.UnixMilli(0) }
	noSystem := func(call *Call) { call.System = System{} }
	noTalkgroup := func(call *Call) { call.Talkgroup = Talkgroup{} }

	both := func(first, second func(*Call)) func(*Call) {
		return func(call *Call) {
			first(call)
			second(call)
		}
	}

	all := func(first, second, third func(*Call)) func(*Call) {
		return func(call *Call) {
			first(call)
			second(call)
			third(call)
		}
	}

	// Every way a submission can be short, with the announcement set on each one: the guards run in
	// the order audio, moment, system, talkgroup, and the refusal names the last one that fails, for
	// a submission that announces a connection check exactly as it does for one that announces nothing.
	cases := []struct {
		missing string
		strip   func(call *Call)
		want    string
	}{
		{missing: "audio", strip: noAudio, want: "no audio"},
		{missing: "a moment", strip: noMoment, want: "no timestamp"},
		{missing: "a system", strip: noSystem, want: "no system"},
		{missing: "a talkgroup", strip: noTalkgroup, want: "no talkgroup"},

		{missing: "audio and a moment", strip: both(noAudio, noMoment), want: "no timestamp"},
		{missing: "audio and a system", strip: both(noAudio, noSystem), want: "no system"},
		{missing: "audio and a talkgroup", strip: both(noAudio, noTalkgroup), want: "no talkgroup"},
		{missing: "a moment and a system", strip: both(noMoment, noSystem), want: "no system"},
		{missing: "a moment and a talkgroup", strip: both(noMoment, noTalkgroup), want: "no talkgroup"},
		{missing: "a system and a talkgroup", strip: both(noSystem, noTalkgroup), want: "no talkgroup"},

		{missing: "audio, a moment and a system", strip: all(noAudio, noMoment, noSystem), want: "no system"},
		{missing: "audio, a moment and a talkgroup", strip: all(noAudio, noMoment, noTalkgroup), want: "no talkgroup"},
		{missing: "audio, a system and a talkgroup", strip: all(noAudio, noSystem, noTalkgroup), want: "no talkgroup"},
		{missing: "a moment, a system and a talkgroup", strip: all(noMoment, noSystem, noTalkgroup), want: "no talkgroup"},

		{
			missing: "audio, a moment, a system and a talkgroup",
			strip:   func(call *Call) { noAudio(call); noMoment(call); noSystem(call); noTalkgroup(call) },
			want:    "no talkgroup",
		},
	}

	for _, testCase := range cases {
		call := validCall()
		call.Meta = IngestMeta{ConnectionCheck: true}
		testCase.strip(&call)

		err := call.IsValid()

		if err == nil {
			t.Errorf("IsValid for a submission announcing a connection check and missing %s = nil, want it refused for %q",
				testCase.missing, testCase.want)
			continue
		}

		if err.Error() != testCase.want {
			t.Errorf("IsValid for a submission announcing a connection check and missing %s reports %q, want %q: "+
				"the guards run in the order audio, moment, system, talkgroup and name the last one that "+
				"fails, and the announcement changes which submission is a check without changing which "+
				"deficiency the refusal names", testCase.missing, err.Error(), testCase.want)
		}
	}
}

// TestIsDuplicateOfUsesTheLegacyRule pins the duplicate rule the server has
// always used: same system identifier, same talkgroup identifier, timestamp
// within the window; unit sets, labels and audio are irrelevant.
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
