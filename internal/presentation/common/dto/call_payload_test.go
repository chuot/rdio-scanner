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

package dto

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// The recording every captured call carries, byte for byte as the capture wrote it.
var capturedRecording = capturedRecordingBytes()

func capturedRecordingBytes() []byte {
	header, err := hex.DecodeString("524946462400000057415645666d74201000000001000100401f0000803e0000020010006461746100000000")
	if err != nil {
		panic(err)
	}

	return append(header, []byte("a recording long enough to be a recording, and then some more of it")...)
}

// TestCallPayloadMatchesTheCapturedCall is the byte-for-byte half of CA-004.1 for a call: the payload
// the legacy server sent for the upload that names everything an upload can name.
func TestCallPayloadMatchesTheCapturedCall(t *testing.T) {
	got := marshal(t, CallPayload{Call: capturedCall()})
	want := readGolden(t, "payload-cal-detailed.json")

	if got != want {
		t.Errorf("call payload =\n  %s\nwant\n  %s", got, want)
	}
}

// TestCallPayloadMatchesTheCapturedCallWithNothingToSay covers the other shape the same call takes:
// the one an upload of a single audio part leaves behind, with no frequencies, no site, no units, no
// signal level, and no audio type because the upload did not name one.
func TestCallPayloadMatchesTheCapturedCallWithNothingToSay(t *testing.T) {
	call := domain.NewCall()

	call.ID = 4
	call.Audio = capturedRecording
	call.AudioFilename = "capture.wav"
	call.Timestamp = time.Date(2026, 10, 6, 12, 4, 0, 0, time.UTC)
	call.PatchTalkgroupRefs = nil
	call.System = domain.System{ID: 1, SystemRef: 10001}
	call.Talkgroup = domain.Talkgroup{ID: 1, TalkgroupRef: 1514000, Label: "Ground", Name: "Dispatch 1"}

	got := marshal(t, CallPayload{Call: call})
	want := readGolden(t, "payload-cal-fetch.json")

	if got != want {
		t.Errorf("call payload =\n  %s\nwant\n  %s", got, want)
	}
}

// A call read back out of the database has no patches list at all and comes out as `null`; a call
// that is being made has an empty one and comes out as `[]`. Both are what the legacy server sends,
// and a client that iterates the list is the one that has to know the difference.
func TestCallPayloadKeepsAbsentPatchesAsNullAndEmptyPatchesAsAnEmptyList(t *testing.T) {
	call := domain.Call{ID: 1, Audio: capturedRecording}

	if got := marshal(t, CallPayload{Call: call}); !carriesKey(t, got, "patches", "null") {
		t.Errorf("a call with no patches list = %s, want \"patches\":null", got)
	}

	call.PatchTalkgroupRefs = []uint{}

	if got := marshal(t, CallPayload{Call: call}); !carriesKey(t, got, "patches", "[]") {
		t.Errorf("a call with an empty patches list = %s, want \"patches\":[]", got)
	}
}

// The audio is the number of each byte, under a wrapper that says it is a buffer, because that is the
// shape the clients already read. A call with no audio has an empty list of them rather than no audio
// at all.
func TestCallPayloadCarriesTheAudioAsByteNumbersUnderABuffer(t *testing.T) {
	call := domain.Call{ID: 1, Audio: []byte{82, 73, 70, 70}, PatchTalkgroupRefs: []uint{}}

	if got := marshal(t, CallPayload{Call: call}); !carriesKey(t, got, "audio", `{"data":[82,73,70,70],"type":"Buffer"}`) {
		t.Errorf("a call with four audio bytes = %s, want the four numbers under a Buffer", got)
	}

	call.Audio = nil

	if got := marshal(t, CallPayload{Call: call}); !carriesKey(t, got, "audio", `{"data":[],"type":"Buffer"}`) {
		t.Errorf("a call with no audio = %s, want an empty list of bytes", got)
	}
}

// A frequency is on the wire only when the call has one, and its signal level only when the level is
// greater than zero. Quirk Q41: the server stores a level that is zero and drops one that is negative,
// and then emits only a level above zero — so a level of zero is stored, carried in the database, and
// never shown to anybody.
func TestCallPayloadCarriesFrequenciesAndOnlyASignalLevelAboveZero(t *testing.T) {
	call := domain.Call{
		ID:                 1,
		Audio:              capturedRecording,
		PatchTalkgroupRefs: []uint{},
		Frequencies: []domain.CallFrequency{
			{Errors: 1, Frequency: 151400000, Offset: 0.5, Spikes: 2, Dbm: 0},
			{Errors: 2, Frequency: 151500000, Offset: 1.5, Spikes: 3, Dbm: 48},
		},
	}

	want := `[{"errorCount":1,"freq":151400000,"pos":0.5,"spikeCount":2},` +
		`{"dbm":48,"errorCount":2,"freq":151500000,"pos":1.5,"spikeCount":3}]`

	if got := marshal(t, CallPayload{Call: call}); !carriesKey(t, got, "frequencies", want) {
		t.Errorf("frequencies = %s, want %s", got, want)
	}

	call.Frequencies = nil

	if got := marshal(t, CallPayload{Call: call}); carriesKey(t, got, "frequencies", "") {
		t.Errorf("a call with no frequencies = %s, want no frequencies key at all", got)
	}
}

// A site, a system, a talkgroup, a channel type and the units heard are each on the wire only when the
// call has them, and a talkgroup's label and name only when they are not empty.
func TestCallPayloadLeavesOutWhatTheCallDoesNotHave(t *testing.T) {
	call := domain.Call{
		ID:                 1,
		Audio:              capturedRecording,
		PatchTalkgroupRefs: []uint{},
		SiteRef:            101,
		System:             domain.System{ID: 1, SystemRef: 10001},
		Talkgroup:          domain.Talkgroup{ID: 1, TalkgroupRef: 1514000},
		Meta:               domain.IngestMeta{ChannelType: "Chat"},
		Units:              []domain.CallUnit{{Offset: 0.5, UnitRef: 1001}},
	}

	got := marshal(t, CallPayload{Call: call})

	for key, want := range map[string]string{
		"site":        "101",
		"system":      "10001",
		"talkgroup":   "1514000",
		"channelType": `"Chat"`,
		"sources":     `[{"pos":0.5,"src":1001}]`,
	} {
		if !carriesKey(t, got, key, want) {
			t.Errorf("a call that has %s = %s, want %s:%s", key, got, key, want)
		}
	}

	if carriesKey(t, got, "talkgroupLabel", "") || carriesKey(t, got, "talkgroupName", "") {
		t.Errorf("a call on a talkgroup with no label or name = %s, want neither key", got)
	}

	call.SiteRef = 0
	call.Meta = domain.IngestMeta{}
	call.Units = nil

	got = marshal(t, CallPayload{Call: call})

	for _, key := range []string{"site", "channelType", "sources"} {
		if carriesKey(t, got, key, "") {
			t.Errorf("a call without %s = %s, want no %s key", key, got, key)
		}
	}
}

// The moment is rendered to the second, which is what every client already parses.
func TestCallPayloadRendersTheMomentToTheSecond(t *testing.T) {
	call := domain.Call{
		ID:                 1,
		Audio:              capturedRecording,
		PatchTalkgroupRefs: []uint{},
		Timestamp:          time.Date(2026, 10, 6, 12, 0, 0, 123, time.UTC),
	}

	got := marshal(t, CallPayload{Call: call})

	if !carriesKey(t, got, "dateTime", `"2026-10-06T12:00:00Z"`) {
		t.Errorf("a call at 12:00:00.123 = %s, want the moment without its milliseconds", got)
	}
}

// capturedCall is the call the capture uploaded first, rebuilt from what the capture named.
func capturedCall() domain.Call {
	return domain.Call{
		ID:                 1,
		Audio:              capturedRecording,
		AudioFilename:      "capture.wav",
		AudioMime:          "audio/wav",
		Frequencies:        []domain.CallFrequency{{Errors: 1, Frequency: 151400000, Offset: 0.5, Spikes: 2}},
		PatchTalkgroupRefs: []uint{1514100},
		SiteRef:            101,
		System:             domain.System{ID: 1, SystemRef: 10001},
		Talkgroup:          domain.Talkgroup{ID: 1, TalkgroupRef: 1514000, Label: "Ground", Name: "Dispatch 1"},
		Timestamp:          time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
		Units: []domain.CallUnit{
			{Offset: 0.5, UnitRef: 1001},
			{Offset: 1.5, UnitRef: 1002},
		},
	}
}

func marshal(t *testing.T, payload any) string {
	t.Helper()

	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshaling the payload: %v", err)
	}

	return string(b)
}

func readGolden(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("../../../../testdata/golden", name))
	if err != nil {
		t.Fatalf("reading the golden capture %s: %v", name, err)
	}

	return string(b)
}

// carriesKey reports whether the marshaled object carries key at its own top level. With a value it
// must be exactly that value; with none it only has to be there. It looks at the bytes rather than
// parsing, because nothing here should re-render what it is checking.
func carriesKey(t *testing.T, body string, key string, value string) bool {
	t.Helper()

	needle := `"` + key + `":`
	depth := 0

	for i := range body {
		switch body[i] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}

		if depth != 1 || !strings.HasPrefix(body[i:], needle) {
			continue
		}

		return value == "" || strings.HasPrefix(body[i+len(needle):], value)
	}

	return false
}
