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
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The delay tests put a server through the two things the delayer is for: a call recorded on a talkgroup
// that holds its calls back is not told to anybody until the hold has run out, and the calls a server was
// holding back when it stopped are picked up by the server that starts again.
//
// Both are run against a server that is actually assembled and answering, because the thing being proved
// is the wiring: that the hold is decided in the one delivery step every call goes through, and that what
// the delayer hands back goes into that same step rather than into a copy of it written for delayed calls.

// heldUploadKey is the credential the delay tests upload a recording with. Its scope covers every system,
// so that what these tests are about is the hold and not the gate in front of the ingest.
const heldUploadKey = "a-key-that-holds-nothing-back"

// heldTalkgroupDelay is the hold the seeded talkgroup names, in the unit the configuration stores it in.
const heldTalkgroupDelay = 1

// TestUploadedCallOnADelayedTalkgroupReachesAListenerOnlyWhenItsHoldRunsOut is the live half of the
// delayer on a running server: the recording is stored and written down as waiting, the listener that
// asked for that talkgroup is told nothing, and the call arrives when its hold has run out carrying the
// mark that says it was held back.
func TestUploadedCallOnADelayedTalkgroupReachesAListenerOnlyWhenItsHoldRunsOut(t *testing.T) {
	srv := testServer(t)

	seedDelayedTalkgroup(t, srv.db)

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	conn := srv.dial(t, "/")

	listenForHeldCalls(t, conn)

	// The recording happened 55 seconds ago, and the talkgroup holds its calls for a minute, so this one
	// has five seconds left to wait. The hold is measured from the moment the call happened, which is what
	// makes a recording that arrived late wait out only what is left of it.
	postedAt := time.Now()
	happenedAt := postedAt.Add(-55 * time.Second)

	answered := srv.uploadRecording(t, uploadedRecording{
		key:       heldUploadKey,
		systemRef: 10001,
		talkgroup: 1514000,
		filename:  "held.wav",
		audio:     recording(2048),
		at:        happenedAt,
	})

	if answered.Code != http.StatusOK {
		t.Fatalf("the upload was answered %d: %s", answered.Code, answered.Body.String())
	}

	// The recording was kept, and it was written down as waiting, with the moment it stops waiting.
	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "calls"`); rows != 1 {
		t.Fatalf("the server stored %d calls, want the one it accepted", rows)
	}

	until := heldUntil(t, srv.db)

	if want := happenedAt.Add(heldTalkgroupDelay * time.Minute).Truncate(time.Millisecond); !until.Equal(want) {
		t.Errorf("the waiting list says the call stops being held back at %s, want %s: the moment it happened plus the minute its talkgroup names", until, want)
	}

	// The listener heard nothing until the hold had run out, and what it heard says the call was held back.
	payload := readHeldCall(t, conn)

	arrivedAt := time.Now()

	if held, ok := payload["delayed"].(bool); !ok || !held {
		t.Errorf("the call the listener received says it was held back %v, want it marked: the mark is what a listener's history shows", payload["delayed"])
	}

	if payload["audioName"] != "held.wav" {
		t.Errorf("the call the listener received carries %v, want the recording that was uploaded", payload["audioName"])
	}

	if waited := arrivedAt.Sub(postedAt); waited < 4*time.Second {
		t.Errorf("the listener was told about the call %v after it was uploaded, want not before its hold had run out", waited)
	}

	// And the call is off the waiting list: a call that has been delivered is not waiting for anything.
	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "delayed"`); rows != 0 {
		t.Errorf("the waiting list holds %d rows after the call was delivered, want none", rows)
	}
}

// TestTheCallsAStoppedServerWasHoldingBackReachTheNextServersListener is the restart half: the calls a
// server was holding back are read back at startup, the ones whose hold ran out while the server was not
// running are delivered at once, and the ones still holding are armed again and delivered when their hold
// runs out — all of them through the delivery step every other call takes, which is what makes them reach
// a listener.
func TestTheCallsAStoppedServerWasHoldingBackReachTheNextServersListener(t *testing.T) {
	// A database left behind by a server that was holding two calls back.
	seededAt := time.Now()

	srv := testServerOnBase(t, t.TempDir())

	seedDelayedTalkgroup(t, srv.db)

	// Call 1 was being held back until a moment that has already gone by. Call 2 until one that has not.
	seedHeldCall(t, srv.db, 1, seededAt.Add(-3*time.Minute), seededAt.Add(-1*time.Minute))
	seedHeldCall(t, srv.db, 2, seededAt.Add(-58*time.Second), seededAt.Add(3*time.Second))

	// The listener is connected before the startup, which is how a call that was already waiting when the
	// server stopped can be seen being delivered by the server that picked it up.
	conn := srv.dial(t, "/")

	listenForHeldCalls(t, conn)

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	frames := readFramesUntilCall(t, conn, 2)

	held := []map[string]any{}

	for _, frame := range frames {
		if frame["command"] == "CAL" {
			held = append(held, frame["payload"].(map[string]any))
		}
	}

	if len(held) != 2 {
		t.Fatalf("the listener was told about %d calls, want the two the previous server was holding back", len(held))
	}

	// Both carry the mark, and they arrive in the order the waiting list names them: the one whose hold
	// ran out while the server was down first, the one still holding when its hold runs out second.
	for i, payload := range held {
		if held, marked := payload["delayed"].(bool); !marked || !held {
			t.Errorf("the %dth call says it was held back %v, want both calls marked", i+1, payload["delayed"])
		}
	}

	if held[0]["id"] != float64(1) || held[1]["id"] != float64(2) {
		t.Errorf("the listener received calls %v and %v, want call 1 at once and call 2 when its hold ran out", held[0]["id"], held[1]["id"])
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "delayed"`); rows != 0 {
		t.Errorf("the waiting list holds %d rows after both calls were delivered, want none", rows)
	}
}

// TestAStoredHoldIsArmedAgainForTheDelayTheConfigurationNamesNow is the reason the startup re-arms a
// waiting call instead of trusting the moment written down beside it: an administrator who changes a
// talkgroup's delay expects the calls already waiting to follow the new one.
func TestAStoredHoldIsArmedAgainForTheDelayTheConfigurationNamesNow(t *testing.T) {
	seededAt := time.Now()

	srv := testServerOnBase(t, t.TempDir())

	seedDelayedTalkgroup(t, srv.db)

	// Written down when the talkgroup held its calls for ten minutes; the talkgroup now names one, and
	// the call happened long enough ago for that minute to have a second left to run.
	seedHeldCall(t, srv.db, 1, seededAt.Add(-59*time.Second), seededAt.Add(10*time.Minute))

	if until := heldUntil(t, srv.db); !until.Equal(seededAt.Add(10 * time.Minute).Truncate(time.Millisecond)) {
		t.Fatalf("the waiting list holds %s before the startup, want the moment the previous server wrote down", until)
	}

	conn := srv.dial(t, "/")

	listenForHeldCalls(t, conn)

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The startup rewrote the hold for the minute the talkgroup names now, rather than keeping the ten
	// minutes that were written down.
	payload := readHeldCall(t, conn)

	if payload["id"] != float64(1) {
		t.Fatalf("the listener was told about call %v, want the call the previous server was holding back", payload["id"])
	}

	if waited := time.Since(seededAt); waited > 30*time.Second {
		t.Errorf("the call was delivered %v after it was written down, want the hold re-armed for the delay the configuration names now", waited)
	}
}

// seedDelayedTalkgroup gives the server the system and talkgroup the delay tests record onto: the talkgroup
// holds its calls for a minute, the system names no hold of its own, the credential these tests upload with
// covers every system, and audio is left as it was recorded so that the run is about the hold and not about
// ffmpeg.
func seedDelayedTalkgroup(t *testing.T, db *sql.DB) {
	t.Helper()

	seedUploadTarget(t, db, heldTalkgroupDelay, heldUploadKey)
}

// seedHeldCall writes a recorded call and the waiting-list row that says it is being held back until the
// given moment, which is exactly what a server leaves behind when it stops in the middle of a hold.
func seedHeldCall(t *testing.T, db *sql.DB, id uint64, happenedAt, until time.Time) {
	t.Helper()

	seed(t, db,
		seedStatement{
			query: `INSERT INTO "calls" ("callId", "audio", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES (?, ?, 'held.wav', '', 0, 1, 1, ?)`,
			args:  []any{id, recording(2048), happenedAt.UnixMilli()},
		},
		seedStatement{
			query: `INSERT INTO "delayed" ("callId", "timestamp") VALUES (?, ?)`,
			args:  []any{id, until.UnixMilli()},
		},
	)
}

// heldUntil reads the moment the waiting list says the held call stops being held back. It insists there is
// exactly one, because these tests hold exactly one call back.
func heldUntil(t *testing.T, db *sql.DB) time.Time {
	t.Helper()

	const query = `SELECT "timestamp" FROM "delayed" ORDER BY "callId"`

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	defer rows.Close()

	var (
		until  time.Time
		count  int
		millis int64
	)

	for rows.Next() {
		if err := rows.Scan(&millis); err != nil {
			t.Fatalf("%s: %v", query, err)
		}

		until, count = time.UnixMilli(millis), count+1
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	if count != 1 {
		t.Fatalf("the waiting list holds %d calls, want the one these tests held back", count)
	}

	return until
}

// uploadedRecording is a recording posted to the upload endpoint, with the moment it happened named the way
// an uploader names it.
type uploadedRecording struct {
	key       string
	systemRef uint
	talkgroup uint
	filename  string
	audio     []byte
	at        time.Time
}

// uploadRecording posts a recording the way an uploader posts one, and reports how the server answered. The
// upload is how these tests put a call into the ingest path without waiting for a watched directory to
// settle, which is what makes a hold of a few seconds testable.
func (s *server) uploadRecording(t *testing.T, upload uploadedRecording) *httptest.ResponseRecorder {
	t.Helper()

	body := bytes.Buffer{}
	writer := multipart.NewWriter(&body)

	fields := []struct{ name, value string }{
		{name: "key", value: upload.key},
		{name: "system", value: fmt.Sprintf("%d", upload.systemRef)},
		{name: "talkgroup", value: fmt.Sprintf("%d", upload.talkgroup)},
		{name: "timestamp", value: fmt.Sprintf("%d", upload.at.UnixMilli())},
	}

	for _, field := range fields {
		if err := writer.WriteField(field.name, field.value); err != nil {
			t.Fatalf("writing the %s field: %v", field.name, err)
		}
	}

	part, err := writer.CreateFormFile("audio", upload.filename)
	if err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	if _, err := part.Write(upload.audio); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("closing the multipart body: %v", err)
	}

	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/api/call-upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	s.routes.ServeHTTP(rec, req)

	return rec
}

// listenForHeldCalls registers a listener socket and asks it to watch the talkgroup the delay tests record
// onto. The exchange is finished before the test goes on, so that a call delivered afterwards has somewhere
// to go.
func listenForHeldCalls(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`["CFG"]`)); err != nil {
		t.Fatalf("asking for the configuration: %v", err)
	}

	if frame := readFrame(t, conn); frame[0] != "CFG" {
		t.Fatalf("the first frame is %v, want the configuration the server was asked for", frame)
	}

	livefeed, err := json.Marshal([]any{"LFM", map[string]any{
		"10001": map[string]any{"1514000": true},
	}})
	if err != nil {
		t.Fatalf("asking for the talkgroups: %v", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, livefeed); err != nil {
		t.Fatalf("asking for the talkgroups: %v", err)
	}

	if frame := readFrame(t, conn); frame[0] != "LFM" || frame[1] != true {
		t.Fatalf("the livefeed answer is %v, want the server to say it is feeding something", frame)
	}
}

// readHeldCall waits for the server to broadcast a call and hands back its payload. The wait is longer than
// a call takes to arrive, because a held call arrives when its hold has run out and not when it was recorded.
func readHeldCall(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for {
		conn.SetReadDeadline(deadline)

		_, b, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for the held call to be broadcast: %v", err)
		}

		var frame []any
		if err := json.Unmarshal(b, &frame); err != nil {
			t.Fatalf("the frame %s is not a frame: %v", b, err)
		}

		if command, _ := frame[0].(string); command != "CAL" || len(frame) < 2 {
			continue
		}

		payload, ok := frame[1].(map[string]any)
		if !ok {
			t.Fatalf("the call frame carries %v, want a call", frame[1])
		}

		return payload
	}
}

// testServerOnBase builds a server against a base directory the test owns, and does not start it, which is
// how a test looks at the state a previous server left behind.
func testServerOnBase(t *testing.T, base string) *server {
	t.Helper()

	srv, err := newServer(&Config{
		BaseDir: base,
		DbFile:  "rdio-scanner.db",
		DbType:  DbTypeSqlite,
		Listen:  ":3000",
	}, io.Discard, nil)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	return srv
}
