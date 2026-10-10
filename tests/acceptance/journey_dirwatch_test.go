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

package acceptance

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/websocket"
)

// The run this test reads was captured from the legacy server with its time zone set to UTC, which is
// the time zone every capture in this repository was taken in. It matters here because a
// watched recording is dated by a clock read in the server's own time zone: the six digits at the front of
// a dsd+ recording name a local time, and the server's time zone is what turns it into a moment.
func init() {
	os.Setenv("TZ", "UTC")
}

// TestWatchedRecordingsBecomeCallsThatReachALiveListener is the whole path a recording takes: a watch
// reads a file a recording tool left behind, the call it describes is written to the database, and the
// listener that asked for that talkgroup is told about it while it is still live.
//
// The expectation is what the legacy server did in the same run, frame for frame and column for column:
// capturedRun under the name dirwatch-end-to-end.json. Both servers were given the same configuration,
// which this
// test seeds into a database the server itself created, because the run is about a recording becoming a
// call and not about what a server creates when it has never seen a system before — that run is the
// auto-populate test.
//
// The two recordings are dropped one at a time, the second only after the first has been broadcast, so
// that the frames arrive in an order both servers can be expected to produce.
func TestWatchedRecordingsBecomeCallsThatReachALiveListener(t *testing.T) {
	captured := capturedRun(t, "dirwatch-end-to-end.json")

	if _, ok := captured["frames"]; !ok {
		t.Fatalf("the captured run has no frames to compare against")
	}

	srv := journeyServerIn(t, t.TempDir())

	watched := filepath.Join(srv.base, "watch")

	dsd := filepath.Join(watched, "dsd")
	trunk := filepath.Join(watched, "trunk")

	// The dated directory is the one dsd+ writes into, and it is there before the server starts watching,
	// which is how a deployment that has been recording for a while looks.
	for _, directory := range []string{dsd, filepath.Join(dsd, "20261006"), trunk} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("preparing the watched directory %s: %v", directory, err)
		}
	}

	seedEndToEndConfiguration(t, srv.db, dsd, trunk)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	conn := srv.dial(t, "/")

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`["CFG"]`)); err != nil {
		t.Fatalf("asking for the configuration: %v", err)
	}

	// A listener is only a listener once it has asked for the configuration, and a call is only broadcast
	// to a listener that asked for the talkgroup it lands on. The references the listener asks for are the
	// ones the configuration it was just given names.
	//
	// Both answers belong in the frames being compared: the capture recorded everything the listener was
	// sent, in the order it was sent.
	observed := []map[string]any{}

	frame := observedFrame(t, conn)
	if frame["command"] != "CFG" {
		t.Fatalf("the first frame is %v, want the configuration the server was asked for", frame)
	}

	observed = append(observed, frame)

	livefeed, err := json.Marshal([]any{"LFM", map[string]any{
		"10001": map[string]any{"1514000": true},
		"4660":  map[string]any{"1514000": true},
	}})
	if err != nil {
		t.Fatalf("asking for the talkgroups: %v", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, livefeed); err != nil {
		t.Fatalf("asking for the talkgroups: %v", err)
	}

	frame = observedFrame(t, conn)
	if frame["command"] != "LFM" || frame["payload"] != true {
		t.Fatalf("the livefeed answer is %v, want the server to say it is feeding something", frame)
	}

	observed = append(observed, frame)

	// The trunk-recorder pair first: the recording and the sidecar that says what it is. This one names
	// its system by label, and the configuration already has a system with that label.
	dropRecording(t, trunk, "call.wav", recording(2092))
	dropRecording(t, trunk, "call.json", []byte(trunkRecorderSidecar))

	observed = append(observed, readFramesUntilCall(t, conn, 1)...)

	// The dsd+ recording second: this one names its system by reference, and it names a radio the
	// configuration has never heard of, which is the part of the run that changes the configuration and
	// makes the server re-send it.
	dropRecording(t, filepath.Join(dsd, "20261006"), dsdRecordingName, recording(2048))

	observed = append(observed, readFramesUntilCall(t, conn, 1)...)

	// The listener-count report is written five seconds after the count changes, and it counts the
	// listeners that are there when it is written rather than the ones that were there when it was
	// scheduled. Hanging up before that moment would put a different count in the log.
	waitForLogMessage(t, srv.db, "there is 1 listener")

	conn.Close()

	waitForLogMessage(t, srv.db, "a listener left from 127.0.0.1")

	compareCanonical(t, "the frames the listener saw", captured["frames"], observed)

	// The watched recordings are gone from the directories: both watches delete after ingesting, which is
	// what keeps a recording tool's output directory from filling up.
	for _, directory := range []string{dsd, trunk} {
		left := findFiles(t, directory)

		if len(left) > 0 {
			t.Errorf("the watch on %s left %v behind, want the recording deleted after ingesting it", directory, left)
		}
	}

	for _, name := range []string{"calls", "logs", "groups", "tags", "talkgroups", "units", "systems"} {
		compareCanonical(t, "the "+name+" the server left behind", captured[name], rowsOf(t, srv.db, srv.base, name))
	}

	assertEveryServerLogEntry(t, srv.db)
}

// seedEndToEndConfiguration is the configuration the end-to-end run is given. The systems and talkgroups
// are there before the recordings arrive, so that a call reaching a listener does not depend on the server
// inventing them on the way; the audio conversion is turned off, so that the recording a watch hands over
// is the recording the test wrote, byte for byte, and the recorded run pins the server passing it along rather
// than pinning what one version of ffmpeg makes of it. Conversion is compared where ffmpeg is the subject.
func seedEndToEndConfiguration(t *testing.T, db *sql.DB, dsd, trunk string) {
	t.Helper()

	seedWatches(t, db, dsd, trunk)

	// The two systems and their talkgroups, named the way the recordings name them: the trunk-recorder
	// recording matches its system by label and the dsd+ recording matches its system by reference, which
	// together are both ways a call is matched.
	seed(t, db,
		seedStatement{
			query: `INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (1, 0, '', 0, 'Grand Metro', '', 0, 10001, '')`,
		},
		seedStatement{
			query: `INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (2, 0, '', 0, 'System 4660', '', 0, 4660, '')`,
		},

		seedStatement{
			query: `INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (1, '', 0, 0, 'Main', '', 'Dispatch 1', 0, 1, 9, 1514000, '')`,
		},
		seedStatement{
			query: `INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (2, '', 0, 0, 'Dispatch', '', 'Talkgroup 1514000', 0, 2, 9, 1514000, '')`,
		},
	)
}

// seedWatches gives the server the two watches and the one option that keeps ffmpeg out of the run. It is
// the whole configuration the auto-populate run is given: everything else that run ends up with is named
// by the recordings, and is what the server is expected to create from them.
func seedWatches(t *testing.T, db *sql.DB, dsd, trunk string) {
	t.Helper()

	seed(t, db,
		seedStatement{
			query: `INSERT INTO "dirwatches" ("dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type") VALUES (1, 2000, 1, ?, 0, '', 0, '', 1, 0, 0, 0, 'dsdplus')`,
			args:  []any{dsd},
		},
		seedStatement{
			query: `INSERT INTO "dirwatches" ("dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type") VALUES (2, 2000, 1, ?, 0, '', 0, '', 2, 0, 0, 0, 'trunk-recorder')`,
			args:  []any{trunk},
		},

		seedStatement{
			query: `INSERT INTO "options" ("key", "value") VALUES ('audioConversion', '0')`,
		},
	)
}

// dsdRecordingName is a recording the way dsd+ names one: the clock first, then the talkgroup, then the
// system as a net and a WACN, then the talkgroup again with its label, then the radio with its label.
const dsdRecordingName = "120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3"

// trunkRecorderSidecar is the JSON a trunk-recorder instance writes beside its recording, and it is the
// only way that recording says what it is.
const trunkRecorderSidecar = `{
  "short_name": "Grand Metro",
  "talkgroup": 1514000,
  "talkgroup_description": "Dispatch 1",
  "talkgroup_group": "Ground",
  "talkgroup_group_tag": "Dispatch",
  "talkgroup_tag": "Main",
  "timestamp": 1791288000000
}
`

// dropRecording puts a recording in a watched directory, and the sidecar beside it, the way a recording
// tool does.
func dropRecording(t *testing.T, directory, name string, content []byte) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(directory, name), content, 0o644); err != nil {
		t.Fatalf("dropping %s into the watched directory: %v", name, err)
	}
}

// readFramesUntilCall reads frames until the server has broadcast the given number of calls, and returns
// all of them: a server that changes its configuration on the way to a call sends the configuration first,
// and the order it sends them in is part of what the capture pinned.
func readFramesUntilCall(t *testing.T, conn *websocket.Conn, calls int) []map[string]any {
	t.Helper()

	frames := []map[string]any{}
	seen := 0

	for {
		frame := observedFrame(t, conn)

		frames = append(frames, frame)

		if frame["command"] == "CAL" {
			seen++

			if seen == calls {
				return frames
			}
		}
	}
}

// observedFrame reads one frame and puts it in the shape the recorded run holds it in.
func observedFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()

	frame := readFrame(t, conn)

	command, _ := frame[0].(string)

	observed := map[string]any{"command": command}

	// The configuration a listener is sent is the whole configuration, and it is pinned by the
	// recorded configurations rather than being repeated here.
	if command == "CFG" || len(frame) < 2 {
		return observed
	}

	payload := frame[1]

	if command == "CAL" {
		if call, ok := payload.(map[string]any); ok {
			payload = withoutRecording(call)
		}
	}

	observed["payload"] = payload

	return observed
}

// withoutRecording replaces the recording inside a call frame with how many bytes it was. The recording is
// the file the test put in the watched directory itself, so what the recorded run has to pin is that the server
// passed it along whole, and not the bytes a second time.
func withoutRecording(call map[string]any) map[string]any {
	audio, ok := call["audio"].(map[string]any)
	if !ok {
		return call
	}

	data, _ := audio["data"].([]any)

	delete(call, "audio")
	call["audioBytes"] = len(data)

	return call
}
