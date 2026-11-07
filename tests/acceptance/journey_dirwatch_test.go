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

// The run this test reads was captured from the legacy server
// with its time zone set to UTC, the time zone of every capture
// here: a watched recording is dated by a local clock.
func init() {
	os.Setenv("TZ", "UTC")
}

// TestWatchedRecordingsBecomeCallsThatReachALiveListener is the
// whole path: a watch ingests a recording and a live listener
// hears it, against the `dirwatch-end-to-end` capture.
func TestWatchedRecordingsBecomeCallsThatReachALiveListener(t *testing.T) {
	captured := capturedRun(t, "dirwatch-end-to-end.json")

	if _, ok := captured["frames"]; !ok {
		t.Fatalf("the captured run has no frames to compare against")
	}

	srv := journeyServerIn(t, t.TempDir())

	watched := filepath.Join(srv.base, "watch")

	dsd := filepath.Join(watched, "dsd")
	trunk := filepath.Join(watched, "trunk")

	// the dated directory is the one dsd+ writes into, created
	// before the watch starts, as a deployed recorder looks
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

	// a listener is only a listener once it asks for the configuration,
	// and calls reach only listeners that asked for their talkgroup:
	// it asks using the references the configuration just handed it,
	// and both answers belong in the frames the capture recorded
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

	// the trunk-recorder pair first: recording and sidecar, naming
	// its system by a label the configuration already has
	dropRecording(t, trunk, "call.wav", recording(2092))
	dropRecording(t, trunk, "call.json", []byte(trunkRecorderSidecar))

	observed = append(observed, readFramesUntilCall(t, conn, 1)...)

	// the dsd+ recording second: it names its system by reference
	// and a radio the configuration has never heard, which is
	// what changes the configuration and makes the server re-send
	dropRecording(t, filepath.Join(dsd, "20261006"), dsdRecordingName, recording(2048))

	observed = append(observed, readFramesUntilCall(t, conn, 1)...)

	// the listener-count report is written five seconds after the
	// count changes and counts the listeners present when written,
	// so hanging up earlier would log a different count
	waitForLogMessage(t, srv.db, "there is 1 listener")

	conn.Close()

	waitForLogMessage(t, srv.db, "a listener left from 127.0.0.1")

	compareCanonical(t, "the frames the listener saw", captured["frames"], observed)

	// the watched recordings are gone: both watches delete after
	// ingesting, so the recording tool's directory does not fill up
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

// seedEndToEndConfiguration gives the run its configuration:
// systems and talkgroups seeded before the recordings arrive, so
// a call reaching a listener does not depend on inventing them,
// and conversion off, so the pinned recording is the file written
func seedEndToEndConfiguration(t *testing.T, db *sql.DB, dsd, trunk string) {
	t.Helper()

	seedWatches(t, db, dsd, trunk)

	// the two systems and their talkgroups, named the way the
	// recordings name them: one matched by label, the other by
	// reference, the two ways a call is matched
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

// seedWatches gives the server its two watches and the option
// that keeps ffmpeg out; everything else the auto-populate
// run ends up with comes from the recordings themselves
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

// dsdRecordingName is a recording the way dsd+ names one:
// the clock, the talkgroup, the system as net and WACN, the
const dsdRecordingName = "120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3"

// trunkRecorderSidecar is the JSON a trunk-recorder instance
// writes beside its recording, the only way it names the call
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

// readFramesUntilCall reads frames until the given number of
// calls have been broadcast and returns all of them: a server
// sends a changed configuration first, the order the capture pinned
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

	// the configuration sent to a listener is the whole
	// configuration, pinned by the recorded configurations
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

// withoutRecording replaces a call frame's recording with its
// byte count: the recording is the file the test wrote, so
// what the run pins is that it was passed along whole
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
