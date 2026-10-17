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

package app

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"rdio-scanner/internal/domain"
)

// These are the helpers the server's own tests share: the way a test logs in, the way a test seeds a
// configuration in front of a server, and the guard over what the server writes to its log view. The
// journeys in tests/acceptance run against the same composition root through its exported API and keep
// their own copy of these, because a test-only package cannot be imported.

// Every capture in this repository was taken with the server's time zone set to UTC, and the recorded
// runs are compared against what a server in UTC wrote. A server started in a machine's own time zone
// reads the timestamps its fixtures name as different moments, so the tests in this package run in UTC.
func init() {
	os.Setenv("TZ", "UTC")
}

// defaultAdminCredential is the credential a fresh server accepts, and the one these tests log in with.
const defaultAdminCredential = "rdio-scanner"

// administratorToken logs in with the credential a fresh deployment answers with and returns the session
// the login handed out.
func administratorToken(t *testing.T, srv *Server) string {
	t.Helper()

	rec := srv.post("/api/admin/login", `{"password":"`+defaultAdminCredential+`"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("login against a fresh server was answered %d: %s", rec.Code, rec.Body.String())
	}

	var answered struct {
		Token string `json:"token"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the login answer is not the answer the legacy server gives: %v", err)
	}

	if answered.Token == "" {
		t.Fatalf("the login answer carries no session: %s", rec.Body.String())
	}

	return answered.Token
}

// authorizedRequest reaches a path that needs the session a login handed out. The credential goes in the
// Authorization header raw, with no scheme in front of it, because that is how the administration interface sends it.
func authorizedRequest(t *testing.T, srv *Server, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	rec := httptest.NewRecorder()

	srv.routes.ServeHTTP(rec, req)

	return rec
}

// sendFrame writes a frame exactly as the test names it.
func sendFrame(t *testing.T, conn *websocket.Conn, frame string) {
	t.Helper()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatalf("sending %s: %v", frame, err)
	}
}

// encodeAccessCode encodes an access code the way a client does, because the wire carries it that way.
func encodeAccessCode(code string) string {
	return base64.StdEncoding.EncodeToString([]byte(code))
}

// waitForLogMentioning waits for a log entry whose message contains this fragment, which is how a test
// waits for a run to have reached a particular point without repeating the parts of the line that the
// configuration decides.
func waitForLogMentioning(t *testing.T, db *sql.DB, fragment string) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)

	for {
		rows, err := db.Query(`SELECT "message" FROM "logs"`)
		if err != nil {
			t.Fatalf("reading the server log: %v", err)
		}

		for rows.Next() {
			var message string

			if err := rows.Scan(&message); err != nil {
				rows.Close()

				t.Fatalf("reading a server log entry: %v", err)
			}

			if strings.Contains(message, fragment) {
				rows.Close()

				return
			}
		}

		rows.Close()

		if err := rows.Err(); err != nil {
			t.Fatalf("reading the server log: %v", err)
		}

		if time.Now().After(deadline) {
			t.Fatalf("the server never logged %s", fragment)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// assertEveryServerLogEntry is the guard over a real server's log view: every row names the part of the
// server that wrote it, and no row begins with the internal name of the code that wrote it.
//
// The application layer's writers are guarded in internal/application/logs_wording_test.go. This is the same
// two rules at the level where the watched directories, the livefeed hub, the upload endpoint and the
// retention pass actually write, read back out of the store every entry is written through — which is also
// the only place a row the *other* server wrote can be told apart from one this server wrote.
//
// It is called at the end of the tests that already reach each subsystem, rather than from one test that
// tries to reach all of them, so a failure names the test that produced the entry.
func assertEveryServerLogEntry(t *testing.T, db *sql.DB) {
	t.Helper()

	const query = `SELECT "source", "message" FROM "logs" ORDER BY "logid"`

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("reading the server's own log view: %v", err)
	}

	defer rows.Close()

	written := 0

	for rows.Next() {
		var source, message string

		if err := rows.Scan(&source, &message); err != nil {
			t.Fatalf("reading an entry from the server's own log view: %v", err)
		}

		written++

		if message == "" {
			t.Errorf("the server wrote an entry with no message")
			continue
		}

		if source == "" {
			t.Errorf("the server wrote %q without naming the part of the server that wrote it", message)
			continue
		}

		if !domain.IsKnownLogSource(domain.LogSource(source)) {
			t.Errorf("the server wrote %q under %q, which is not a part of this server", message, source)
			continue
		}

		if prefix := internalPrefixAt(message); prefix != "" {
			t.Errorf("the server wrote %q, which begins with the internal name %q instead of saying what happened",
				message, prefix)
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the server's own log view: %v", err)
	}

	if written == 0 {
		t.Errorf("the server wrote nothing to its own log view, so this guard proves nothing about what it writes")
	}
}

// internalPrefixAt is the front-of-message shape the wording rule forbids: a token naming a section and an
// operation, or a bare section name, ending in a colon.
//
// Only the front counts. The failure's own text is kept verbatim after the sentence this feature puts in
// front of it, because that text names the statement or the syscall that failed, so `the log search failed:
// logs.search: no such table: logs` is the rule being followed and not broken.
func internalPrefixAt(message string) string {
	first := message

	if space := strings.Index(message, " "); space >= 0 {
		first = message[:space]
	}

	if strings.HasSuffix(first, ":") {
		return first
	}

	return ""
}

// seedStatement is one of the statements that put a configuration in front of the server, with the values
// it needs passed in rather than written into it.
type seedStatement struct {
	query string
	args  []any
}

// seed takes testing.TB rather than a test: a benchmark seeds the same history a test
// seeds, and the history is what makes the thing being timed worth timing.
func seed(t testing.TB, db *sql.DB, statements ...seedStatement) {
	t.Helper()

	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatalf("seeding %s: %v", statement.query, err)
		}
	}
}

// recording is audio for a watched file. Nothing in these tests looks inside the recording — a watch reads
// a recording's name and its sidecar, and the ingest path only asks whether there is more audio than a
// container header — so what matters is that it is as long as the recording the recorded run was captured
// from.
func recording(bytes int) []byte {
	audio := make([]byte, bytes)

	for i := range audio {
		audio[i] = byte(i % 251)
	}

	return audio
}
