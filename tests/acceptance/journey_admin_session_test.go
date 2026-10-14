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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
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
const testRequestAddress = "192.0.2.1"

// Three wrong credentials are answered on their merits. The fourth is refused before the credential is
// looked at, and it is the only refusal the server says anything about.
//
// This is Q10 repaired, seen through the server a deployment runs: the attempts are counted inside a
// ten-minute window, and the guesses below are all made inside one window, so the address has spent its
// budget and the correct credential that follows is refused with it. The window itself — a count that ages
// out, and the budget that comes back when it does — is pinned in
// [internal/application/login_logout_test.go](../../internal/application/login_logout_test.go).
func TestAdministratorLoginAttemptsAreCountedAgainstTheAddress(t *testing.T) {
	srv := startedJourneyServer(t)

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
// clears the counts of every address — the half of Q10 the repair keeps.
func TestASixthAdministratorSessionEndsTheFirstOne(t *testing.T) {
	srv := startedJourneyServer(t)

	tokens := []string{}

	for session := range 6 {
		tokens = append(tokens, srv.loginAnswered(t))

		// Each session is used before the next login, so that the session being tested is one the server
		// handed out and has not yet forgotten.
		if rec := srv.authorizedRequest(t, http.MethodGet, "/api/admin/config", tokens[session], ""); rec.Code != http.StatusOK {
			t.Fatalf("session %d was refused %d while it was still the most recent one: %s", session+1, rec.Code, rec.Body.String())
		}
	}

	for session, token := range tokens {
		rec := srv.authorizedRequest(t, http.MethodGet, "/api/admin/config", token, "")

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
	srv := startedJourneyServer(t)

	token := srv.loginAnswered(t)

	if rec := srv.authorizedRequest(t, http.MethodGet, "/api/admin/config", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("a session that was just handed out answers %d: %s", rec.Code, rec.Body.String())
	}

	conn := srv.dial(t, "/api/admin/config")

	closed := closedWith(t, conn)

	sendFrame(t, conn, token)

	rec := srv.authorizedRequest(t, http.MethodPost, "/api/admin/logout", token, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("logging out was answered %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if rec := srv.authorizedRequest(t, http.MethodGet, "/api/admin/config", token, ""); rec.Code != http.StatusUnauthorized {
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
		"an administrator connected from 127.0.0.1",
		"an administrator from 127.0.0.1 presented a session token that is not one this server issued",
		"an administrator disconnected from 127.0.0.1",
	} {
		waitForLogMentioning(t, srv.db, fact)

		// The exact sentence, not a sentence that merely contains it: the address is the part an operator
		// searches for, and it has to read the same way here as it does in every other entry the server
		// writes about a machine.
		if !hasLogMessage(t, srv.db, fact) {
			t.Errorf("the log holds no entry reading exactly %q", fact)
		}

		entry := storedLogMentioning(t, srv.db, fact)

		if got := logSourceOfMessage(t, srv.db, entry); got != "admin" {
			t.Errorf("%q is filed under %q, want the administrator's own part of the server", entry, got)
		}
	}

	assertEveryServerLogEntry(t, srv.db)
}

// TestAnAdministratorRequestTheServerCannotReadIsInTheLogView is the composition root's half of the rule:
// the entry reaches the log view an operator actually searches, filed under the administrator's own part of
// the server, and is said out loud at the same time. The address it names is the one the server names for a
// request, which on a server behind a proxy is the machine that asked and not the proxy.
func TestAnAdministratorRequestTheServerCannotReadIsInTheLogView(t *testing.T) {
	// The terminal copy is what this test reads, so the server is built on a console the test owns
	// rather than on the harness's discarded one.
	base := t.TempDir()

	console := &bytes.Buffer{}

	srv := consoleJourneyServerIn(t, base, console)

	rec := srv.authorizedRequest(t, http.MethodPut, "/api/admin/config", srv.loginAnswered(t),
		`{"groups": [`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a save with a body the server could not read was answered %d, want 400: %s", rec.Code,
			rec.Body.String())
	}

	const sentence = "the administrator's request to /api/admin/config from "

	waitForLogMentioning(t, srv.db, sentence)

	entry := storedLogMentioning(t, srv.db, sentence)

	if got := logSourceOfMessage(t, srv.db, entry); got != "admin" {
		t.Errorf("%q is filed under %q, want the administrator's own part of the server", entry, got)
	}

	if !strings.Contains(entry, "could not be read: ") {
		t.Errorf("%q does not name the reason the body could not be read, which is the difference between a "+
			"client that sent no JSON and one that sent the wrong shape", entry)
	}

	if !strings.Contains(console.String(), "warn admin: "+sentence) {
		t.Errorf("the terminal holds %q, want the refused request said out loud", console.String())
	}

	assertEveryServerLogEntry(t, srv.db)
}

// Saving the whole configuration and saving each section one at a time reach the same configuration, and
// both reach the one the legacy server reached from the same fixture: the answer is compared byte for byte
// against the captured answer in fixtures_test.go.
//
// The capture's answer says every site belongs to no system, because the legacy server's query for sites
// never asks for that column. This server reports the system the site is stored under, which is the approved
// divergence Q58, so the comparison states it by rewriting the capture rather than by loosening itself.
//
// The section-by-section path saves each system's sites, talkgroups, and units through the level that shows
// them, because the Systems section saves the system row and leaves those rows to their own sections. That
// is the path the administration page takes, and it is the reason the two paths are not answered with the
// same bytes: see the comparison below.
func TestSavingTheWholeConfigurationAndSavingEachSectionReachTheSameConfiguration(t *testing.T) {
	fixture := readCaptureFixture(t)

	// The whole configuration in one save.
	whole := startedJourneyServer(t)

	token := whole.loginAnswered(t)

	if rec := whole.put("/api/admin/config", token, string(fixture.raw)); rec.Code != http.StatusOK {
		t.Fatalf("saving the whole configuration was answered %d: %s", rec.Code, rec.Body.String())
	}

	got := authorizedRequest(t, whole, http.MethodGet, "/api/admin/config", token, "")

	if got.Code != http.StatusOK {
		t.Fatalf("reading the configuration back was answered %d: %s", got.Code, got.Body.String())
	}

	want := replaceExactly(t, capturedRestConfig, `"systemId":0`, `"systemId":1`, 2)

	if got.Body.String() != want {
		t.Errorf("the whole-configuration save left\n  %s\nwant\n  %s", got.Body.String(), want)
	}

	// The same configuration, section by section, in the order a whole-configuration save performs
	// them, and with each system's levels saved by the level section that shows them: the Systems
	// section saves the system row, so the rows under a system arrive through the sites, talkgroups,
	// and units saves, which is where an operator edits them.
	sections := startedJourneyServer(t)

	token = administratorToken(t, sections)

	for _, name := range []string{"access", "apikeys", "dirwatch", "downstreams", "groups", "options", "systems", "tags"} {
		rec := sections.put("/api/admin/config/"+name, token, string(fixture.section[name]))

		if rec.Code != http.StatusOK {
			t.Fatalf("saving the %s section was answered %d: %s", name, rec.Code, rec.Body.String())
		}
	}

	saveEachSystemLevel(t, sections, token, fixture.section["systems"])

	got = authorizedRequest(t, sections, http.MethodGet, "/api/admin/config", token, "")

	if got.Code != http.StatusOK {
		t.Fatalf("reading the configuration back was answered %d: %s", got.Code, got.Body.String())
	}

	// The same answer, once it is given the display orders the per-level saves assign to the rows they
	// store. A whole-configuration save takes each row's display order from the payload, and the
	// capture's payload names none, so its answer writes none; a per-level save numbers the rows it
	// stores from one, and its answer writes that number. The rows, and the order they are shown in,
	// are the same through both paths — only the number the answer writes beside them is not — so the
	// capture is rewritten to say the number, and the comparison stays byte for byte.
	//
	// The capture is written the way the server writes an answer, so the rewrite adds only the keys it
	// names, and the comparison below is of the bytes the answers carry.
	if written := canonicalConfiguration(t, want); written != want {
		t.Fatalf("the captured answer is not written the way the server writes one:\n  %s\nfrom\n  %s", written, want)
	}

	want = captureWithLevelDisplayOrders(t, want)

	if left := canonicalConfiguration(t, got.Body.String()); left != want {
		t.Errorf("the section-by-section save left\n  %s\nwant\n  %s", left, want)
	}
}

// A save reaches the administrator connections that are open, with the configuration that save produced,
// and writes the line that says the configuration changed.
func TestAConfigurationSaveReachesEveryOpenAdministratorConnection(t *testing.T) {
	srv := startedJourneyServer(t)

	token := srv.loginAnswered(t)

	conn := srv.dial(t, "/api/admin/config")

	pushed := presentedCredential(t, conn, token)

	rec := srv.put("/api/admin/config/options", token, `{"maxClients":60}`)

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
	asked := srv.authorizedRequest(t, http.MethodGet, "/api/admin/config", token, "")

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

	if options["maxClients"] != float64(60) {
		t.Errorf("the configuration sent to the open connection holds maxClients %v, want the value the save stored", options["maxClients"])
	}

	if got := countLogLines(t, srv.db, "the options configuration changed"); got != 1 {
		t.Errorf("the server wrote %d lines about the change, want one naming the section that was saved", got)
	}
}

// administratorToken logs in with the credential a fresh deployment answers with and returns the session
// the login handed out.
func administratorToken(t *testing.T, srv *journeyServer) string {
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
func authorizedRequest(t *testing.T, srv *journeyServer, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	rec := httptest.NewRecorder()

	srv.server.ServeHTTP(rec, req)

	return rec
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

// saveEachSystemLevel stores each system's sites, talkgroups, and units through the level endpoint that
// shows them, with the rows the capture's payload nests under that system. The capture's rows carry no
// identifiers — they are the rows a form submits before the store has named them — so each system is
// matched to the store by the order the systems section answers them in, which is the order the save
// stored them in.
func saveEachSystemLevel(t *testing.T, srv *journeyServer, token string, systemsBody []byte) {
	t.Helper()

	stored := systemsSection(t, readAuthorized(t, srv, token, "/api/admin/config/systems"))

	var nested []map[string]any

	if err := json.Unmarshal(systemsBody, &nested); err != nil {
		t.Fatalf("the capture's systems payload is not the list of systems it installs: %v\n%s", err, systemsBody)
	}

	if len(stored) != len(nested) {
		t.Fatalf("the systems section save stored %d systems, want the %d the capture's payload names",
			len(stored), len(nested))
	}

	for i, system := range stored {
		id, ok := system["id"].(float64)

		if !ok {
			t.Fatalf("the systems section answers a system with no identifier: %v", system)
		}

		for _, level := range []string{"sites", "talkgroups", "units"} {
			body, err := json.Marshal(nested[i][level])

			if err != nil {
				t.Fatalf("writing the %s rows the capture nests under system %d: %v", level, uint64(id), err)
			}

			path := fmt.Sprintf("/api/admin/config/systems/%d/%s", uint64(id), level)

			if rec := srv.put(path, token, string(body)); rec.Code != http.StatusOK {
				t.Fatalf("saving the %s of system %d was answered %d: %s", level, uint64(id), rec.Code, rec.Body.String())
			}
		}
	}
}

// canonicalConfiguration writes a configuration answer back the way the server writes it: the keys in the
// order the server writes them, the numbers as they were written, and no spacing. Both sides of a
// byte-for-byte comparison pass through it, so the comparison is of the bytes the answers carry while the
// shape of an answer is stated once.
func canonicalConfiguration(t *testing.T, answer string) string {
	t.Helper()

	return writeConfiguration(t, readConfigurationAnswer(t, answer))
}

// captureWithLevelDisplayOrders is the captured answer with the display orders the per-level saves assign
// to the rows they store, numbered from one the way those saves number them, and written under the key
// each level's answer uses for that number.
func captureWithLevelDisplayOrders(t *testing.T, answer string) string {
	t.Helper()

	body := readConfigurationAnswer(t, answer)

	config, ok := body["config"].(map[string]any)

	if !ok {
		t.Fatalf("the captured answer carries no configuration: %s", answer)
	}

	systems, ok := config["systems"].([]any)

	if !ok {
		t.Fatalf("the captured configuration carries no systems: %s", answer)
	}

	for _, entry := range systems {
		system, ok := entry.(map[string]any)

		if !ok {
			t.Fatalf("the captured configuration answers a system that is not a system: %v", entry)
		}

		numberLevelRows(t, system, "sites", "order")
		numberLevelRows(t, system, "talkgroups", "talkgroup")
		numberLevelRows(t, system, "units", "order")
	}

	return writeConfiguration(t, body)
}

// numberLevelRows numbers one level's rows from one, the way the save of that level numbers the rows it
// stores, under the key the answer writes a row's display order under.
func numberLevelRows(t *testing.T, system map[string]any, level, key string) {
	t.Helper()

	rows, ok := system[level].([]any)

	if !ok {
		t.Fatalf("the captured configuration carries no %s under a system: %v", level, system)
	}

	for i, entry := range rows {
		row, ok := entry.(map[string]any)

		if !ok {
			t.Fatalf("the captured configuration answers a %s row that is not a row: %v", level, entry)
		}

		row[key] = json.Number(strconv.Itoa(i + 1))
	}
}

// readConfigurationAnswer reads a configuration answer keeping every number as the bytes it was written
// with, so that writing it back cannot turn 1514000 into 1.514e+06.
func readConfigurationAnswer(t *testing.T, answer string) map[string]any {
	t.Helper()

	reading := json.NewDecoder(strings.NewReader(answer))

	reading.UseNumber()

	var body map[string]any

	if err := reading.Decode(&body); err != nil {
		t.Fatalf("the configuration answer is not the answer the server writes: %v\n%s", err, answer)
	}

	return body
}

// writeConfiguration writes a configuration answer out. Marshalling a map writes its keys in sorted order,
// which is the order the server writes an answer's keys in.
func writeConfiguration(t *testing.T, body map[string]any) string {
	t.Helper()

	written, err := json.Marshal(body)

	if err != nil {
		t.Fatalf("writing the configuration answer back: %v", err)
	}

	return string(written)
}
