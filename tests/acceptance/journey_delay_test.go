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
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The delay tests: a call on a talkgroup that holds its calls back
// is told to nobody until the hold runs out, and a new server picks
// up the calls the old one was holding.
// see the delay-journey maintainer note.

// heldUploadKey is the credential the delay tests upload with,
// scoped to every system so the run is about the hold
const heldUploadKey = "a-key-that-holds-nothing-back"

// heldTalkgroupDelay is the hold the seeded talkgroup names, in the unit the configuration stores it in.
const heldTalkgroupDelay = 1

// TestUploadedCallOnADelayedTalkgroupReachesAListenerOnlyWhen
// ItsHoldRunsOut is the live half: the recording is stored and
// written down as waiting, the listener is told nothing, and
// the call arrives when the hold runs out, marked as held
func TestUploadedCallOnADelayedTalkgroupReachesAListenerOnlyWhenItsHoldRunsOut(t *testing.T) {
	srv := journeyServerIn(t, t.TempDir())

	seedDelayedTalkgroup(t, srv.db)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	conn := srv.dial(t, "/")

	listenForHeldCalls(t, conn)

	// the recording happened 55 seconds ago and the talkgroup holds
	// for a minute, so five seconds remain: the hold runs from when
	// the call happened, so a late upload waits out only what is left
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

	assertEveryServerLogEntry(t, srv.db)
}

// TestTheCallsAStoppedServerWasHoldingBackReachTheNextServers
// Listener is the restart half: waiting calls are read back at
// startup, the expired ones delivered at once, the others
// re-armed, all through the same delivery step every call takes
func TestTheCallsAStoppedServerWasHoldingBackReachTheNextServersListener(t *testing.T) {
	// A database left behind by a server that was holding two calls back.
	seededAt := time.Now()

	srv := journeyServerIn(t, t.TempDir())

	seedDelayedTalkgroup(t, srv.db)

	// Call 1 was being held back until a moment that has already gone by. Call 2 until one that has not.
	seedHeldCall(t, srv.db, 1, seededAt.Add(-3*time.Minute), seededAt.Add(-1*time.Minute))
	seedHeldCall(t, srv.db, 2, seededAt.Add(-58*time.Second), seededAt.Add(3*time.Second))

	// the listener connects before the startup, so a call that was
	// already waiting can be seen delivered by the new server
	conn := srv.dial(t, "/")

	listenForHeldCalls(t, conn)

	if err := srv.server.Start(context.Background()); err != nil {
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

	// both carry the held mark and arrive in waiting-list order:
	// the expired one first, the re-armed one when its hold ends
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

// TestAStoredHoldIsArmedAgainForTheDelayTheConfiguration
// NamesNow is why startup re-arms a waiting call instead of
// trusting the stored moment: calls waiting follow the delay set now
func TestAStoredHoldIsArmedAgainForTheDelayTheConfigurationNamesNow(t *testing.T) {
	seededAt := time.Now()

	srv := journeyServerIn(t, t.TempDir())

	seedDelayedTalkgroup(t, srv.db)

	// stored under a ten-minute hold, the talkgroup now names a
	// minute, and the call happened long enough ago for a second left
	seedHeldCall(t, srv.db, 1, seededAt.Add(-59*time.Second), seededAt.Add(10*time.Minute))

	if until := heldUntil(t, srv.db); !until.Equal(seededAt.Add(10 * time.Minute).Truncate(time.Millisecond)) {
		t.Fatalf("the waiting list holds %s before the startup, want the moment the previous server wrote down", until)
	}

	conn := srv.dial(t, "/")

	listenForHeldCalls(t, conn)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// the startup rewrote the hold for the delay the talkgroup
	// names now, not the ten minutes stored beside it
	payload := readHeldCall(t, conn)

	if payload["id"] != float64(1) {
		t.Fatalf("the listener was told about call %v, want the call the previous server was holding back", payload["id"])
	}

	if waited := time.Since(seededAt); waited > 30*time.Second {
		t.Errorf("the call was delivered %v after it was written down, want the hold re-armed for the delay the configuration names now", waited)
	}
}

// seedDelayedTalkgroup gives the delay tests their system and
// talkgroup: a one-minute hold, no system-level hold, an all-
// systems upload credential, and audio left as recorded so
// the run is about the hold and not about ffmpeg
func seedDelayedTalkgroup(t *testing.T, db *sql.DB) {
	t.Helper()

	seedUploadTarget(t, db, heldTalkgroupDelay, heldUploadKey)
}

// seedHeldCall writes a recorded call and its waiting-list row
// until the given moment, as a stopping server leaves them
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

// heldUntil reads when the waiting list says the held call is
// released, insisting there is exactly one
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

// listenForHeldCalls connects a listener socket watching the
// delay tests' talkgroup, finishing the exchange first so a
// call delivered afterwards has somewhere to go
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

// readHeldCall waits for a broadcast call and returns its payload:
// a held call arrives when its hold runs out, not when recorded
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
