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
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// These tests hold a started server to the administrator paths: the credential it accepts, the sessions
// its logins hand out, the way a save reaches an open administrator connection, and the log line a save
// leaves behind. The individual handlers are pinned against the captured answers in
// [internal/presentation/rest](../../internal/presentation/rest); what is pinned here is that the server a
// deployment runs wires them to the same database, the same sessions, and the same socket.

// The credential a fresh deployment answers, and the address the requests these tests make arrive from —
// the one httptest hands out. Both are named here because the log lines the server writes about a login
// carry them.
const (
	defaultAdminCredential = "rdio-scanner"
	testRequestAddress     = "192.0.2.1"
)

// Three wrong credentials are answered on their merits. The fourth is refused before the credential is
// looked at, and it is the only refusal the server says anything about.
//
// This is Q10 repaired, seen through the server a deployment runs: the attempts are counted inside a
// ten-minute window, and the guesses below are all made inside one window, so the address has spent its
// budget and the correct credential that follows is refused with it. The window itself — a count that ages
// out, and the budget that comes back when it does — is pinned in
// [internal/application/login_logout_test.go](../../internal/application/login_logout_test.go).
func TestAdministratorLoginAttemptsAreCountedAgainstTheAddress(t *testing.T) {
	srv := startedTestServer(t)

	for guess := range 3 {
		rec := srv.post("/api/admin/login", `{"password":"not-the-credential"}`)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("guess %d was answered %d, want 401: %s", guess+1, rec.Code, rec.Body.String())
		}
	}

	if got := countLogLines(t, srv.db, "a login attempt from "+testRequestAddress+" was refused"); got != 3 {
		t.Fatalf("the server wrote %d refusal lines, want one for each of the three guesses", got)
	}

	rec := srv.post("/api/admin/login", `{"password":"not-the-credential"}`)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("the fourth guess was answered %d, want 401", rec.Code)
	}

	if got := countLogLines(t, srv.db, "a login attempt from "+testRequestAddress+" was refused"); got != 3 {
		t.Errorf("the fourth guess wrote another refusal line (%d in all), want the fourth refused before the credential is looked at", got)
	}

	if got := countLogLines(t, srv.db, "too many login attempts from "+testRequestAddress); got != 1 {
		t.Errorf("the server wrote %d lines about the limit, want exactly one, for the attempt that reached it", got)
	}

	rec = srv.post("/api/admin/login", `{"password":"not-the-credential"}`)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("the fifth guess was answered %d, want 401", rec.Code)
	}

	if got := countLogLines(t, srv.db, "too many login attempts from "+testRequestAddress); got != 1 {
		t.Errorf("the server wrote %d lines about the limit, want the attempts past the limit refused without saying anything", got)
	}

	// The credential that would have worked is refused, because the count is what is checked first and all
	// four guesses fall inside one attempt window.
	rec = srv.post("/api/admin/login", `{"password":"`+defaultAdminCredential+`"}`)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("the correct credential was answered %d after four attempts inside one attempt window, want 401", rec.Code)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// A login hands out a session, and only the five most recent ones are sessions. The sixth ends the first.
//
// Repeated logins from one address are not stopped by the attempt count, because a login that succeeds
// clears the counts of every address — the half of Q10 the repair keeps, for the reason recorded in
// docs/specs/FEAT-004-legacy-defect-repairs/decision-login-attempt-window.md.
func TestASixthAdministratorSessionEndsTheFirstOne(t *testing.T) {
	srv := startedTestServer(t)

	tokens := []string{}

	for session := range 6 {
		tokens = append(tokens, administratorToken(t, srv))

		// Each session is used before the next login, so that the session being tested is one the server
		// handed out and has not yet forgotten.
		if rec := authorizedRequest(t, srv, http.MethodGet, "/api/admin/config", tokens[session], ""); rec.Code != http.StatusOK {
			t.Fatalf("session %d was refused %d while it was still the most recent one: %s", session+1, rec.Code, rec.Body.String())
		}
	}

	for session, token := range tokens {
		rec := authorizedRequest(t, srv, http.MethodGet, "/api/admin/config", token, "")

		if session == 0 {
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("the first of six sessions is still open (%d), want the sixth to have ended it", rec.Code)
			}

			continue
		}

		if rec.Code != http.StatusOK {
			t.Errorf("session %d of six answers %d, want it still open", session+1, rec.Code)
		}
	}
}

// Logging out ends that session everywhere the server would honor it, including on an administrator
// connection that is already open: the socket checks the credential on every frame it is given, so the
// next frame it is given is the one that closes it.
func TestAnAdministratorLogoutEndsThatSessionEverywhere(t *testing.T) {
	srv := startedTestServer(t)

	token := administratorToken(t, srv)

	if rec := authorizedRequest(t, srv, http.MethodGet, "/api/admin/config", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("a session that was just handed out answers %d: %s", rec.Code, rec.Body.String())
	}

	conn := srv.dial(t, "/api/admin/config")

	closed := closedWith(t, conn)

	sendFrame(t, conn, token)

	rec := authorizedRequest(t, srv, http.MethodPost, "/api/admin/logout", token, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("logging out was answered %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if rec := authorizedRequest(t, srv, http.MethodGet, "/api/admin/config", token, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("the credential still opens the configuration after the logout (%d), want 401", rec.Code)
	}

	// The open socket is only as current as the frame it was last given: it is asked to prove itself
	// again on every frame, so the next frame it is given is the one that ends it.
	sendFrame(t, conn, token)

	if code := closed(); code != websocket.CloseNormalClosure {
		t.Errorf("the open administrator connection ended with %d after the logout, want it ended with a normal closure", code)
	}

	// The three things the listener socket has always said and this socket said nothing about, read out of
	// the log view the server actually writes to: the administrator's configuration socket opened, the
	// credential it stopped accepting, and the connection ended. This is the composition root's half of the
	// criterion — the socket's own tests prove the sentences, this proves they reach the log an operator
	// searches and are filed under the administrator's own part of the server.
	for _, fact := range []string{
		"an administrator connected from 127.0.0.1:",
		"an administrator from 127.0.0.1:",
		"an administrator disconnected from 127.0.0.1:",
	} {
		waitForLogMentioning(t, srv.db, fact)

		entry := storedLogMentioning(t, srv.db, fact)

		if got := logSourceOfMessage(t, srv.db, entry); got != "admin" {
			t.Errorf("%q is filed under %q, want the administrator's own part of the server", entry, got)
		}
	}

	assertEveryServerLogEntry(t, srv.db)
}

// Saving the whole configuration and saving each section one at a time reach the same configuration, and
// both reach the one the legacy server reached from the same fixture: the answer is compared byte for byte
// against testdata/golden/rest-config.json.
//
// The capture's answer says every site belongs to no system, because the legacy server's query for sites
// never asks for that column. This server reports the system the site is stored under, which is the approved
// divergence Q58, so the comparison states it by rewriting the capture rather than by loosening itself.
func TestSavingTheWholeConfigurationAndSavingEachSectionReachTheSameConfiguration(t *testing.T) {
	fixture := readCaptureFixture(t)

	// The whole configuration in one save.
	whole := startedTestServer(t)

	token := administratorToken(t, whole)

	if rec := whole.put("/api/admin/config", token, string(fixture.raw)); rec.Code != http.StatusOK {
		t.Fatalf("saving the whole configuration was answered %d: %s", rec.Code, rec.Body.String())
	}

	got := authorizedRequest(t, whole, http.MethodGet, "/api/admin/config", token, "")

	if got.Code != http.StatusOK {
		t.Fatalf("reading the configuration back was answered %d: %s", got.Code, got.Body.String())
	}

	want := replaceExactly(t, readGoldenText(t, "rest-config.json"), `"systemId":0`, `"systemId":1`, 2)

	if got.Body.String() != want {
		t.Errorf("the whole-configuration save left\n  %s\nwant\n  %s", got.Body.String(), want)
	}

	// The same configuration, section by section, in the order a whole-configuration save performs them.
	sections := startedTestServer(t)

	token = administratorToken(t, sections)

	for _, name := range []string{"access", "apikeys", "dirwatch", "downstreams", "groups", "options", "systems", "tags"} {
		rec := sections.put("/api/admin/config/"+name, token, string(fixture.section[name]))

		if rec.Code != http.StatusOK {
			t.Fatalf("saving the %s section was answered %d: %s", name, rec.Code, rec.Body.String())
		}
	}

	got = authorizedRequest(t, sections, http.MethodGet, "/api/admin/config", token, "")

	if got.Code != http.StatusOK {
		t.Fatalf("reading the configuration back was answered %d: %s", got.Code, got.Body.String())
	}

	if got.Body.String() != want {
		t.Errorf("the section-by-section save left\n  %s\nwant\n  %s", got.Body.String(), want)
	}
}

// A save reaches the administrator connections that are open, with the configuration that save produced,
// and writes the line that says the configuration changed.
func TestAConfigurationSaveReachesEveryOpenAdministratorConnection(t *testing.T) {
	srv := startedTestServer(t)

	token := administratorToken(t, srv)

	conn := srv.dial(t, "/api/admin/config")

	pushed := presentedCredential(t, conn, token)

	rec := srv.put("/api/admin/config/options", token, `{"dimmerDelay":6000}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("saving the options was answered %d: %s", rec.Code, rec.Body.String())
	}

	var frame string

	select {
	case frame = <-pushed:
	case <-time.After(10 * time.Second):
		t.Fatalf("no configuration reached the open administrator connection, want the save to push it")
	}

	pushedConfiguration := decodeAdministratorConfiguration(t, frame)

	// The frame is the configuration the server would hand to an administrator asking for it, with the
	// value the save changed in it.
	asked := authorizedRequest(t, srv, http.MethodGet, "/api/admin/config", token, "")

	if asked.Code != http.StatusOK {
		t.Fatalf("reading the configuration was answered %d: %s", asked.Code, asked.Body.String())
	}

	var reported struct {
		Config map[string]any `json:"config"`
	}

	if err := json.Unmarshal(asked.Body.Bytes(), &reported); err != nil {
		t.Fatalf("the configuration answer is not the answer the legacy server gives: %v", err)
	}

	if keys := configurationKeys(pushedConfiguration); keys != configurationKeys(reported.Config) {
		t.Errorf("the configuration sent to the open connection holds %s, want the same parts the configuration answer holds: %s", keys, configurationKeys(reported.Config))
	}

	options, ok := pushedConfiguration["options"].(map[string]any)
	if !ok {
		t.Fatalf("the configuration sent to the open connection carries no options: %v", pushedConfiguration)
	}

	if options["dimmerDelay"] != float64(6000) {
		t.Errorf("the configuration sent to the open connection holds dimmerDelay %v, want the value the save stored", options["dimmerDelay"])
	}

	if got := countLogLines(t, srv.db, "the options configuration changed"); got != 1 {
		t.Errorf("the server wrote %d lines about the change, want one naming the section that was saved", got)
	}
}

// administratorToken logs in with the credential a fresh deployment answers with and returns the session
// the login handed out.
func administratorToken(t *testing.T, srv *server) string {
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
// Authorization header raw, with no scheme in front of it, because that is how the web app sends it.
func authorizedRequest(t *testing.T, srv *server, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	rec := httptest.NewRecorder()

	srv.routes.ServeHTTP(rec, req)

	return rec
}

// countLogLines counts the server-log entries whose message is exactly this.
func countLogLines(t *testing.T, db *sql.DB, message string) int {
	t.Helper()

	rows, err := db.Query(`SELECT "message" FROM "logs" WHERE "message" = ?`, message)
	if err != nil {
		t.Fatalf("reading the server log: %v", err)
	}

	defer rows.Close()

	count := 0

	for rows.Next() {
		var found string

		if err := rows.Scan(&found); err != nil {
			t.Fatalf("reading a server log entry: %v", err)
		}

		count++
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the server log: %v", err)
	}

	return count
}

// presentedCredential hands a connection its credential and waits until the server has taken it, which is
// what makes that connection one a configuration is pushed to.
//
// The credential is taken by a frame the server reads on its own schedule, and a connection that has not
// been given one is told nothing (Q64). A ping sent after the credential is read by the same reader, so the
// answer to the ping is the point at which the credential is in. The frames the server sends afterwards come
// back on the channel, because the only way to see a frame is to be reading for one.
func presentedCredential(t *testing.T, conn *websocket.Conn, token string) <-chan string {
	t.Helper()

	frames := make(chan string, 8)

	go func() {
		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				close(frames)

				return
			}

			frames <- string(b)
		}
	}()

	answered := make(chan struct{}, 1)

	conn.SetPongHandler(func(string) error {
		select {
		case answered <- struct{}{}:
		default:
		}

		return nil
	})

	sendFrame(t, conn, token)

	if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
		t.Fatalf("asking the administrator connection to answer a ping: %v", err)
	}

	select {
	case <-answered:
	case <-time.After(10 * time.Second):
		t.Fatalf("the server never answered a ping on the administrator connection")
	}

	return frames
}

// decodeAdministratorConfiguration reads the configuration an administrator connection is sent, which
// arrives as the payload on its own rather than wrapped the way the REST answer wraps it.
func decodeAdministratorConfiguration(t *testing.T, frame string) map[string]any {
	t.Helper()

	var payload map[string]any

	if err := json.Unmarshal([]byte(frame), &payload); err != nil {
		t.Fatalf("the configuration sent to an administrator connection is not a configuration: %v\n%s", err, frame)
	}

	return payload
}

// configurationKeys names the parts of a configuration, in order, so that two of them can be compared.
func configurationKeys(cfg map[string]any) string {
	names := make([]string, 0, len(cfg))

	for name := range cfg {
		names = append(names, name)
	}

	sort.Strings(names)

	return strings.Join(names, ",")
}

// readGoldenText reads a captured answer as the bytes it holds.
func readGoldenText(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", name))
	if err != nil {
		t.Fatalf("reading the captured answer: %v", err)
	}

	return string(b)
}

// replaceExactly swaps one piece of a captured answer for another and insists the swap happened the number
// of times the test expects, so that a divergence is stated rather than absorbed.
func replaceExactly(t *testing.T, text, from, to string, want int) string {
	t.Helper()

	got := strings.Count(text, from)

	if got != want {
		t.Fatalf("%q appears %d times in the captured answer, want %d", from, got, want)
	}

	return strings.ReplaceAll(text, from, to)
}
