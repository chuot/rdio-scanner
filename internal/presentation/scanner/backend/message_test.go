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

package backend

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The frame is a JSON array of up to three elements: a command, an optional payload, and an
// optional flag. It is frozen by the protocol contract and consumed by the native applications, so
// what is written here is what those applications already parse.

func TestFrameCarriesCommandPayloadAndFlag(t *testing.T) {
	frame := Message{
		Command: CommandCall,
		Payload: map[string]any{"id": 7},
		Flag:    "d",
	}

	got := marshal(t, frame)
	want := `["CAL",{"id":7},"d"]`

	if got != want {
		t.Errorf("frame = %s, want %s", got, want)
	}
}

func TestFrameCarriesCommandAndPayloadWithoutAFlag(t *testing.T) {
	got := marshal(t, Message{Command: CommandVersion, Payload: map[string]any{"version": "8.0.0"}})
	want := `["VER",{"version":"8.0.0"}]`

	if got != want {
		t.Errorf("frame = %s, want %s", got, want)
	}
}

func TestFrameIsOnlyTheCommandWhenThereIsNothingToSay(t *testing.T) {
	got := marshal(t, Message{Command: CommandAccessCode})
	want := `["PIN"]`

	if got != want {
		t.Errorf("frame = %s, want %s", got, want)
	}
}

// An empty string is not a payload, and the legacy server leaves it out. Every payload the server
// actually sends is an object, a number, or a boolean, so this is the rule that keeps `["PIN"]` a
// two-character frame rather than `["PIN",""]`.
func TestFrameLeavesOutAnEmptyStringPayload(t *testing.T) {
	got := marshal(t, Message{Command: CommandCall, Payload: ""})
	want := `["CAL"]`

	if got != want {
		t.Errorf("frame = %s, want %s", got, want)
	}
}

func TestFrameLeavesOutAnEmptyStringFlag(t *testing.T) {
	got := marshal(t, Message{Command: CommandCall, Payload: map[string]any{"id": 7}, Flag: ""})
	want := `["CAL",{"id":7}]`

	if got != want {
		t.Errorf("frame = %s, want %s", got, want)
	}
}

// A payload that is present but says "no" is still a payload. The livefeed ack is `["LFM",false]`,
// and dropping a false answer would leave the client waiting for one.
func TestFrameKeepsAPayloadThatIsPresentButFalse(t *testing.T) {
	got := marshal(t, Message{Command: CommandLivefeedMap, Payload: false})
	want := `["LFM",false]`

	if got != want {
		t.Errorf("frame = %s, want %s", got, want)
	}
}

func TestFrameKeepsAPayloadThatIsPresentButZero(t *testing.T) {
	got := marshal(t, Message{Command: CommandListenersCount, Payload: 0})
	want := `["LSC",0]`

	if got != want {
		t.Errorf("frame = %s, want %s", got, want)
	}
}

func TestFrameKeepsAnEmptyObjectOrArrayPayload(t *testing.T) {
	got := marshal(t, Message{Command: CommandCallList, Payload: []any{}})
	want := `["LCL",[]]`

	if got != want {
		t.Errorf("frame = %s, want %s", got, want)
	}
}

// The flag is appended on its own, without looking at whether a payload was there. No frame the
// server sends needs this — every frame with a flag has a payload — but a client that reads the
// array by position depends on the flag landing in the third place, not the second.
func TestFrameKeepsTheFlagInItsOwnPlace(t *testing.T) {
	got := marshal(t, Message{Command: CommandAccessCode, Flag: "x"})
	want := `["PIN","x"]`

	if got != want {
		t.Errorf("frame = %s, want %s", got, want)
	}
}

func TestReadingAFrameTakesTheCommandPayloadAndFlag(t *testing.T) {
	message, err := ReadMessage([]byte(`["CAL","7","d"]`))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	if message.Command != CommandCall {
		t.Errorf("command = %v, want %s", message.Command, CommandCall)
	}

	if message.Payload != "7" {
		t.Errorf("payload = %v, want the string the client wrote", message.Payload)
	}

	if message.Flag != "d" {
		t.Errorf("flag = %v, want d", message.Flag)
	}
}

func TestReadingAFrameLeavesWhatIsNotThereUnset(t *testing.T) {
	message, err := ReadMessage([]byte(`["VER"]`))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	if message.Command != CommandVersion {
		t.Errorf("command = %v, want %s", message.Command, CommandVersion)
	}

	if message.Payload != nil {
		t.Errorf("payload = %v, want nothing", message.Payload)
	}

	if message.Flag != nil {
		t.Errorf("flag = %v, want nothing", message.Flag)
	}
}

// A client that sends something that is not an array is not answered and is not disconnected: the
// legacy server writes one line to its console and reads the next frame.
func TestReadingAFrameThatIsNotAnArrayIsRefused(t *testing.T) {
	for _, body := range []string{`{"command":"VER"}`, `"VER"`, `7`, ``} {
		if _, err := ReadMessage([]byte(body)); err == nil {
			t.Errorf("ReadMessage(%s) succeeded, want a refusal", body)
		}
	}
}

// An empty frame is not a refusal. `null` and `[]` unmarshal as an array with nothing in it, the
// legacy server matches no command, and the client is left connected and unanswered.
func TestReadingAnEmptyFrameStillReads(t *testing.T) {
	for _, body := range []string{`null`, `[]`} {
		message, err := ReadMessage([]byte(body))
		if err != nil {
			t.Errorf("ReadMessage(%s): %v", body, err)

			continue
		}

		if message.Command != nil {
			t.Errorf("ReadMessage(%s) has command %v, want nothing", body, message.Command)
		}
	}
}

// A command the server does not know is read without complaint and answered with nothing, which is
// what a client that sends one today gets.
func TestReadingAFrameWhoseCommandIsNotKnownStillReads(t *testing.T) {
	message, err := ReadMessage([]byte(`["NONSENSE",1]`))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	if message.Command != "NONSENSE" {
		t.Errorf("command = %v, want NONSENSE", message.Command)
	}
}

// A frame whose first element is not a string is still a frame: the legacy server compares the
// command against each known one and matches none, so the client is answered with nothing and stays
// connected. Refusing to read it would be a different server.
func TestReadingAFrameWhoseCommandIsNotAStringStillReads(t *testing.T) {
	message, err := ReadMessage([]byte(`[7]`))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	if message.Command != float64(7) {
		t.Errorf("command = %v, want the number the client sent", message.Command)
	}
}

// The commands are the wire values the native applications send and parse, and they are the one part
// of this layer that cannot change.
func TestTheCommandConstantsAreTheWireValues(t *testing.T) {
	for command, want := range map[string]string{
		CommandCall:           "CAL",
		CommandConfig:         "CFG",
		CommandAccessExpired:  "XPR",
		CommandIOS:            "IOS",
		CommandCallList:       "LCL",
		CommandListenersCount: "LSC",
		CommandLivefeedMap:    "LFM",
		CommandAccessLimit:    "MAX",
		CommandAccessCode:     "PIN",
		CommandPushID:         "PID",
		CommandServer:         "SRV",
		CommandVersion:        "VER",
	} {
		if command != want {
			t.Errorf("command constant = %q, want the wire value %q", command, want)
		}
	}
}

// protocolCaptures is the completeness guard for the listener protocol: every command constant this
// package freezes is named here with the capture its frame appears in and the test that compares this
// server's framing against that capture. A command with no capture in this package says so, with the
// reason: the two access-code answers the server sends before it closes are frozen by the access-code
// journey in tests/acceptance, and the three native-application commands are never sent to a web client
// at all, so the gap is a recorded one and not an accident of which sessions this capture happened to
// open. A command that quietly lost its capture fails this guard rather than leaving the framing test
// with nothing to compare.
var protocolCaptures = map[string]struct {
	// capture is the recorded run the frame appears in, and label the session of that run.
	capture string
	label   string

	// test is the test that compares this server's framing against the capture.
	test string

	// outside names the proof a command has outside this package, and never the reason it has none.
	outside string
}{
	CommandCall: {
		capture: "ws-frames-open.json",
		label:   "listener",
		test:    "TestFramingReproducesTheCapturedFrames",
	},
	CommandConfig: {
		capture: "ws-frames-open.json",
		label:   "listener",
		test:    "TestFramingReproducesTheCapturedFrames",
	},
	CommandCallList: {
		capture: "ws-frames-open.json",
		label:   "listener",
		test:    "TestCapturedCallListRowsCarryTheirFieldsInWrittenOrder",
	},
	CommandListenersCount: {
		capture: "ws-frames-open.json",
		label:   "listener",
		test:    "TestFramingReproducesTheCapturedFrames",
	},
	CommandLivefeedMap: {
		capture: "ws-frames-open.json",
		label:   "listener",
		test:    "TestFramingReproducesTheCapturedFrames",
	},
	CommandAccessCode: {
		capture: "ws-frames-restricted.json",
		label:   "scoped",
		test:    "TestFramingReproducesTheCapturedFrames",
	},
	CommandVersion: {
		capture: "ws-frames-open.json",
		label:   "listener",
		test:    "TestFramingReproducesTheCapturedFrames",
	},

	// The server sends these two and closes the connection, which is why they are not in a session this
	// package captured. The access-code journey drives them against a running server.
	CommandAccessExpired: {
		outside: "TestAnExpiredAccessCodeIsAnsweredWithAnExpiry, in tests/acceptance",
	},
	CommandAccessLimit: {
		outside: "TestAnAccessCodeLimitsHowManySessionsMayUnlockWithIt, in tests/acceptance",
	},

	// The protocol lists these three as native-application only, never handled by the web client, so no
	// capture of a web session contains them and the guard asserts that they are absent.
	CommandIOS:    {outside: "the iOS applications only: a web session never receives it, and no capture carries it"},
	CommandPushID: {outside: "the mobile applications only: a web session never receives it, and no capture carries it"},
	CommandServer: {outside: "the native applications only: a web session never receives it, and no capture carries it"},
}

// TestEveryProtocolCommandIsCapturedOrSaysWhyNot is the completeness half: the command constants the
// server freezes and the captures that freeze their wire shapes have to agree. A constant missing from
// the table, a capture named by a constant that holds no frame of that command, a capture with no test
// that reads it, and a command with neither a capture nor a reason are each a failure.
func TestEveryProtocolCommandIsCapturedOrSaysWhyNot(t *testing.T) {
	constants := []string{
		CommandCall,
		CommandConfig,
		CommandAccessExpired,
		CommandIOS,
		CommandCallList,
		CommandListenersCount,
		CommandLivefeedMap,
		CommandAccessLimit,
		CommandAccessCode,
		CommandPushID,
		CommandServer,
		CommandVersion,
	}

	if len(protocolCaptures) != len(constants) {
		t.Errorf("protocolCaptures declares %d commands, the server freezes %d", len(protocolCaptures), len(constants))
	}

	for _, command := range constants {
		entry, ok := protocolCaptures[command]

		if !ok {
			t.Errorf("command %s is frozen by this package and is not in protocolCaptures, so nothing compares its wire shape", command)

			continue
		}

		if entry.capture == "" {
			if entry.outside == "" {
				t.Errorf("command %s has no capture and no reason it has none", command)
			}

			continue
		}

		if entry.label == "" {
			t.Errorf("command %s names the capture %s and does not say which session of it", command, entry.capture)

			continue
		}

		if entry.test == "" {
			t.Errorf("command %s has a capture and no test that compares it", command)
		}

		found := false

		for _, frame := range capturedFrames(entry.capture, entry.label) {
			if commandOf(frame) == command {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("command %s is declared in the capture %s/%s, which holds no frame with that command", command, entry.capture, entry.label)
		}
	}
}

// TestNoWebCaptureCarriesANativeAppCommand keeps the three native-application commands out of the
// captured web sessions: the protocol lists them as never sent to a web client, and a capture that
// gained one would mean this server says something a web client is not written to parse.
func TestNoWebCaptureCarriesANativeAppCommand(t *testing.T) {
	for _, command := range []string{CommandIOS, CommandPushID, CommandServer} {
		for _, session := range []struct {
			capture string
			label   string
		}{
			{capture: "ws-frames-open.json", label: "listener"},
			{capture: "ws-frames-restricted.json", label: "scoped"},
		} {
			for _, frame := range capturedFrames(session.capture, session.label) {
				if commandOf(frame) == command {
					t.Errorf("the captured session %s/%s carries %s, which the protocol lists as native-application only", session.capture, session.label, command)
				}
			}
		}
	}
}

// commandOf reads the command out of a frame exactly as the frame was written: a JSON array whose
// first element is the command.
func commandOf(frame string) string {
	const open = `["`

	if len(frame) < len(open)+1 || frame[:len(open)] != open {
		return ""
	}

	end := len(open)

	for end < len(frame) && frame[end] != '"' {
		end++
	}

	if end == len(open) || end == len(frame) {
		return ""
	}

	return frame[len(open):end]
}

// TestFramingReproducesTheCapturedFrames is the golden half: a frame taken out of a capture is put
// back together and has to come out as the bytes the legacy server sent.
//
// The frame is decoded with UseNumber so that its numbers go back out as they came in. That is a
// property of this test and not of the server: the server marshals typed values, and a number that
// arrived here as a float64 would leave as 1e+07 where the server writes 10000000.
//
// It covers every command except the call search, which is the one payload the legacy server does not
// marshal from a map — see TestCapturedCallListRowsCarryTheirFieldsInWrittenOrder.
func TestFramingReproducesTheCapturedFrames(t *testing.T) {
	for _, captured := range []struct {
		file  string
		label string
	}{
		{file: "ws-frames-open.json", label: "listener"},
		{file: "ws-frames-restricted.json", label: "scoped"},
	} {
		for _, frame := range capturedFrames(captured.file, captured.label) {
			decoder := json.NewDecoder(strings.NewReader(frame))
			decoder.UseNumber()

			var values []any

			if err := decoder.Decode(&values); err != nil {
				t.Fatalf("%s/%s: reading the captured frame %s: %v", captured.file, captured.label, frame, err)
			}

			if len(values) == 0 {
				t.Fatalf("%s/%s: captured frame %s has no command", captured.file, captured.label, frame)
			}

			if values[0] == CommandCallList {
				continue
			}

			message := Message{}

			if len(values) > 0 {
				message.Command = values[0]
			}

			if len(values) > 1 {
				message.Payload = values[1]
			}

			if len(values) > 2 {
				message.Flag = values[2]
			}

			if got := marshal(t, message); got != frame {
				t.Errorf("%s/%s: re-framed as %s, want %s", captured.file, captured.label, got, frame)
			}
		}
	}
}

// TestCapturedCallListRowsCarryTheirFieldsInWrittenOrder records the one payload in the protocol that
// is not marshalled from a map, which the framing above cannot reproduce and which the payload layer
// therefore has to get right on its own.
//
// The legacy `server/call.go` `CallsSearchResult` and `CallsSearchResults` are structs, so their keys come out in
// the order the fields are declared — `id`, `system`, `talkgroup`, `dateTime` — while a call payload
// is a map and comes out sorted. A search result row and a call of the same shape therefore disagree
// about the order of the same four keys, and a client that compares the bytes of the two sees it.
//
// This is what T035 has to reproduce.
func TestCapturedCallListRowsCarryTheirFieldsInWrittenOrder(t *testing.T) {
	frames := capturedFrames("ws-frames-open.json", "listener")

	var frame string

	for _, candidate := range frames {
		if commandOf(candidate) == CommandCallList {
			frame = candidate

			break
		}
	}

	if frame == "" {
		t.Fatalf("no captured listener session contains a %s frame", CommandCallList)
	}

	at := strings.Index(frame, `"results":[`)
	if at < 0 {
		t.Fatalf("the captured %s frame has no results: %s", CommandCallList, frame)
	}

	want := []string{"id", "system", "talkgroup", "dateTime"}

	if got := objectKeys(t, frame[at+len(`"results":[`):]); !slices.Equal(got, want) {
		t.Errorf("a search result row carries %v, want %v: the legacy marshals it from a struct, not from a map", got, want)
	}

	// The envelope is the same kind of thing, and its order is the order its fields are declared.
	if got := objectKeys(t, frame[len(`["LCL",`):]); !slices.Equal(got, []string{"count", "dateStart", "dateStop", "options", "results"}) {
		t.Errorf("the search envelope carries %v, want count, dateStart, dateStop, options, results", got)
	}
}

func marshal(t *testing.T, message Message) string {
	t.Helper()

	b, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshaling the frame: %v", err)
	}

	return string(b)
}

// objectKeys lists the keys of the JSON object that starts at the beginning of body, in the order they
// were written. Nothing else in this capture is read by key order, and nothing here re-encodes what it
// reads.
func objectKeys(t *testing.T, body string) []string {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(body))

	token, err := decoder.Token()
	if err != nil {
		t.Fatalf("reading an object at %s: %v", truncate(body), err)
	}

	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		t.Fatalf("want an object at %s", truncate(body))
	}

	var (
		keys      = []string{}
		depth     = 1
		expectKey = true
	)

	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			t.Fatalf("reading an object at %s: %v", truncate(body), err)
		}

		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{', '[':
				if expectKey {
					t.Fatalf("want a key at %s", truncate(body))
				}

				depth++

			case '}', ']':
				depth--

				if depth == 1 {
					expectKey = true
				}
			}

			continue
		}

		if depth != 1 {
			continue
		}

		if !expectKey {
			expectKey = true

			continue
		}

		key, ok := token.(string)
		if !ok {
			t.Fatalf("want a key at %s", truncate(body))
		}

		keys = append(keys, key)
		expectKey = false
	}

	return keys
}

func truncate(body string) string {
	if len(body) > 60 {
		return body[:60] + "…"
	}

	return body
}


