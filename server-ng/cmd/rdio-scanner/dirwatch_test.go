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

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The golden this test reads was captured from the legacy server with its time zone set to UTC, which is
// the time zone testdata/golden/manifest.json declares for every capture. It matters here because a
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
// testdata/golden/dirwatch-end-to-end.json. Both servers were given the same configuration, which this
// test seeds into a database the server itself created, because the run is about a recording becoming a
// call and not about what a server creates when it has never seen a system before — that run is the
// auto-populate test.
//
// The two recordings are dropped one at a time, the second only after the first has been broadcast, so
// that the frames arrive in an order both servers can be expected to produce.
func TestWatchedRecordingsBecomeCallsThatReachALiveListener(t *testing.T) {
	golden := readEndToEndGolden(t)

	srv := testServer(t)

	watched := filepath.Join(srv.config.BaseDir, "watch")

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

	if err := srv.start(context.Background()); err != nil {
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
	waitForLogMessage(t, srv.db, "listeners count is 1")

	conn.Close()

	waitForLogMessage(t, srv.db, "listener disconnected from ip 127.0.0.1")

	compareCanonical(t, "the frames the listener saw", golden["frames"], observed)

	// The watched recordings are gone from the directories: both watches delete after ingesting, which is
	// what keeps a recording tool's output directory from filling up.
	for _, directory := range []string{dsd, trunk} {
		left := findFiles(t, directory)

		if len(left) > 0 {
			t.Errorf("the watch on %s left %v behind, want the recording deleted after ingesting it", directory, left)
		}
	}

	for _, name := range []string{"calls", "logs", "groups", "tags", "talkgroups", "units", "systems"} {
		compareCanonical(t, "the "+name+" the server left behind", golden[name], endToEndRows(t, srv.db, name))
	}
}

// The configuration both servers were given. The systems and talkgroups are there before the recordings
// arrive, so that a call reaching a listener does not depend on the server inventing them on the way; the
// audio conversion is turned off, so that the recording a watch hands over is the recording the test wrote,
// byte for byte, and the golden pins the server passing it along rather than pinning what one version of
// ffmpeg makes of it. Conversion is compared where ffmpeg is the subject.
func seedEndToEndConfiguration(t *testing.T, db *sql.DB, dsd, trunk string) {
	t.Helper()

	statements := []struct {
		query string
		args  []any
	}{
		{
			`INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (1, 0, '', 0, 'Grand Metro', '', 0, 10001, '')`,
			nil,
		},
		{
			`INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (2, 0, '', 0, 'System 4660', '', 0, 4660, '')`,
			nil,
		},
		{
			`INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (1, '', 0, 0, 'Main', '', 'Dispatch 1', 0, 1, 9, 1514000, '')`,
			nil,
		},
		{
			`INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (2, '', 0, 0, 'Dispatch', '', 'Talkgroup 1514000', 0, 2, 9, 1514000, '')`,
			nil,
		},
		{
			`INSERT INTO "dirwatches" ("dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type") VALUES (1, 2000, 1, ?, 0, '', 0, '', 1, 0, 0, 0, 'dsdplus')`,
			[]any{dsd},
		},
		{
			`INSERT INTO "dirwatches" ("dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type") VALUES (2, 2000, 1, ?, 0, '', 0, '', 2, 0, 0, 0, 'trunk-recorder')`,
			[]any{trunk},
		},
		{
			`INSERT INTO "options" ("key", "value") VALUES ('audioConversion', '0')`,
			nil,
		},
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seeding %s: %v", statement.query, err)
		}
	}
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

// recording is audio for a watched file. Nothing in this run looks inside the recording — a watch reads a
// recording's name and its sidecar, and the ingest path only asks whether there is more audio than a
// container header — so what matters is that it is as long as the recording the golden was captured from.
func recording(bytes int) []byte {
	audio := make([]byte, bytes)

	for i := range audio {
		audio[i] = byte(i % 251)
	}

	return audio
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

// observedFrame reads one frame and puts it in the shape the golden holds it in.
func observedFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()

	frame := readFrame(t, conn)

	command, _ := frame[0].(string)

	observed := map[string]any{"command": command}

	// The configuration a listener is sent is the whole configuration, and it is pinned by the
	// configuration goldens rather than being repeated here.
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
// the file the test put in the watched directory itself, so what the golden has to pin is that the server
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

// endToEndRows reads back one of the tables the run is expected to have left behind.
func endToEndRows(t *testing.T, db *sql.DB, table string) []map[string]any {
	t.Helper()

	queries := map[string]string{
		"calls": `SELECT "callId", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp", length("audio") AS "audioBytes" FROM "calls" ORDER BY "callId"`,
		"logs":  `SELECT "level", "message" FROM "logs" WHERE "message" != 'server started' ORDER BY "logid"`,
	}

	query, ok := queries[table]
	if !ok {
		query = fmt.Sprintf(`SELECT * FROM "%s" ORDER BY 1`, table)
	}

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("reading the %s the server left behind: %v", table, err)
	}

	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("reading the %s the server left behind: %v", table, err)
	}

	found := []map[string]any{}

	for rows.Next() {
		values := make([]any, len(columns))
		holders := make([]any, len(columns))

		for i := range values {
			holders[i] = &values[i]
		}

		if err := rows.Scan(holders...); err != nil {
			t.Fatalf("reading a %s row: %v", table, err)
		}

		row := map[string]any{}

		for i, column := range columns {
			// A driver that hands back text as bytes is the same text as far as the golden is concerned.
			if text, ok := values[i].([]byte); ok {
				row[column] = string(text)

				continue
			}

			row[column] = values[i]
		}

		found = append(found, row)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the %s the server left behind: %v", table, err)
	}

	return found
}

// waitForLogMessage waits for the server to have written a log entry, because some of what it writes is
// something that happens on its own schedule rather than the answer to a request.
func waitForLogMessage(t *testing.T, db *sql.DB, message string) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)

	for {
		var count int

		if err := db.QueryRow(`SELECT COUNT(*) FROM "logs" WHERE "message" = ?`, message).Scan(&count); err != nil {
			t.Fatalf("reading the server log: %v", err)
		}

		if count > 0 {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("the server never logged %q", message)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// findFiles lists the files left in a watched directory and the directories under it.
func findFiles(t *testing.T, directory string) []string {
	t.Helper()

	left := []string{}

	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() {
			left = append(left, path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("looking at what the watch left behind in %s: %v", directory, err)
	}

	return left
}

// readEndToEndGolden reads the captured run, keeping each table as it was written so that it can be
// compared in the same canonical shape the server's own rows are put in.
func readEndToEndGolden(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "dirwatch-end-to-end.json"))
	if err != nil {
		t.Fatalf("reading the captured run: %v", err)
	}

	golden := map[string]json.RawMessage{}

	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatalf("the captured run is not the run a capture writes: %v", err)
	}

	for _, name := range []string{"frames", "calls", "logs", "groups", "tags", "talkgroups", "units", "systems"} {
		if _, ok := golden[name]; !ok {
			t.Fatalf("the captured run has no %s to compare against", name)
		}
	}

	return golden
}

// compareCanonical compares two things by putting both in the shape JSON writes them in, with the keys of
// every object in the same order, so that a difference is a difference in what the servers did and not in
// what order a map happened to be walked.
func compareCanonical(t *testing.T, what string, want json.RawMessage, got any) {
	t.Helper()

	var decoded any

	if err := json.Unmarshal(want, &decoded); err != nil {
		t.Fatalf("the captured %s is not something JSON can hold: %v", what, err)
	}

	wanted, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("writing the captured %s: %v", what, err)
	}

	gotten, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("writing %s: %v", what, err)
	}

	if string(wanted) != string(gotten) {
		t.Errorf("%s\ncaptured: %s\nserver-ng: %s", what, wanted, gotten)
	}
}
