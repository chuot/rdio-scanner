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

// The admin auth session journey: a station signs in, the API keeps that
// station signed in through a cookie, and signing out ends the session. The
// file lays out the story as one browser test per user story and records
// the divergences it found, the login form's message-placement checks first.

// The credential a fresh deployment answers, and the address the requests these tests make arrive from —
// the one httptest hands out. Both are named here because the log lines the server writes about a login
// carry them.
const testRequestAddress = "192.0.2.1"

// A login window that has closed, so the next sign-in is refused for a
// lockout reason. The lockout state sits in the shared database.
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

		// The retry resolves the timing: the browser lands at the dashboard once
		// the station shows success, while the API signs the session in when saved.
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

// Assert the login is refused for the reason the API reports.
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

	// The test case for a person trying to sign in a second time, after the
	// login window has closed.
	sendFrame(t, conn, token)

	if code := closed(); code != websocket.CloseNormalClosure {
		t.Errorf("the open administrator connection ended with %d after the logout, want it ended with a normal closure", code)
	}

	// Assert the login is refused for a lockout reason that includes the time
	// still to wait.
	for _, fact := range []string{
		"an administrator connected from 127.0.0.1",
		"an administrator from 127.0.0.1 presented a session token that is not one this server issued",
		"an administrator disconnected from 127.0.0.1",
	} {
		waitForLogMentioning(t, srv.db, fact)

		// Assert the browser still works during the lockout: the sign-in form can
		// be reached.
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

// Assert the browser can return to the sign-in form after being locked out.
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

// The login form's error region in the admin station. The divergence behind
// it: the region is not a fixed live region, it is built when the refusal
// arrives.
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

	// The helper fails the test rather than returning an empty result when no
	// error region is found.
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

	// The API session query is the source of truth for what the person should be
	// told, and the check is the visible text the browser shows.
	if written := canonicalConfiguration(t, want); written != want {
		t.Fatalf("the captured answer is not written the way the server writes one:\n  %s\nfrom\n  %s", written, want)
	}

	want = captureWithLevelDisplayOrders(t, want)

	if left := canonicalConfiguration(t, got.Body.String()); left != want {
		t.Errorf("the section-by-section save left\n  %s\nwant\n  %s", left, want)
	}
}

// The browser may show no visible reason for two different reasons, and
// telling them apart is what the API session query is for.
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

	// Assert the login form carries exactly one error region, so the check that
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

// reads it cannot pass vacuously or read a merged message.
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

// The API session query is the source of truth for the login that was
// refused, and the check is the visible text the browser shows for it.
func authorizedRequest(t *testing.T, srv *journeyServer, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	rec := httptest.NewRecorder()

	srv.server.ServeHTTP(rec, req)

	return rec
}

// The sign-in state the admin station shows back: the message the form
// carries, and where it carries it.
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

// The browser shows a link back to the sign-in form so a locked-out person
// can try again once the window closes.
func decodeAdministratorConfiguration(t *testing.T, frame string) map[string]any {
	t.Helper()

	var payload map[string]any

	if err := json.Unmarshal([]byte(frame), &payload); err != nil {
		t.Fatalf("the configuration sent to an administrator connection is not a configuration: %v\n%s", err, frame)
	}

	return payload
}

// The browser keeps working during the lockout: the dashboard can be reached.
func configurationKeys(cfg map[string]any) string {
	names := make([]string, 0, len(cfg))

	for name := range cfg {
		names = append(names, name)
	}

	sort.Strings(names)

	return strings.Join(names, ",")
}

// What the browser shows back after a sign-in attempt: the API records what
// happened, and this is the browser's view of it.
func replaceExactly(t *testing.T, text, from, to string, want int) string {
	t.Helper()

	got := strings.Count(text, from)

	if got != want {
		t.Fatalf("%q appears %d times in the captured answer, want %d", from, got, want)
	}

	return strings.ReplaceAll(text, from, to)
}

// Assert the browser has landed on the admin dashboard.
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

// Assert the browser shows the admin sign-in form.
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

// Assert the browser shows the sign-in form, not the dashboard, for a person
// who is not signed in.
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
