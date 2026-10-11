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
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// The call history over HTTP is the journey a listener has today on the listener socket, taken off
// the socket: search the history, read one call, fetch one recording. The socket is reserved for
// Saubeo Solutions; the HTTP REST surface is the part of the server the access policy leaves
// unrestricted, so this is the surface a third-party client — and the Model Context Protocol surface
// built on it — reaches the history through.
//
// These journeys run against a server with a real database, a real upload that put the call in it,
// and the real access list, because the thing being proved is the seam: the paging the history
// search applies, the recording the store keeps beside its call, and the credential rules the access
// list states, all reached through the composed server.

const (
	historyKey       = "history-upload-key"
	historyOpenCode  = "history-open"
	historyScopedKey = "history-scoped"

	// historyAttemptsMax is the access-code guess budget one address is given, stated here in the
	// business terms the operator states it in. The application layer holds the constant; a journey
	// states the rule, and a change to the bound that leaves this rule standing is a change the
	// journey is there to catch.
	historyAttemptsMax = 3
)

// A call recorded by an upload is searchable, readable, and listenable over HTTP, with no credential
// in sight: a server with no access list opens its history the way it opens the listener socket.
func TestTheCallHistoryIsSearchedAndRetrievedOverRest(t *testing.T) {
	srv := journeyServerIn(t, t.TempDir())

	seedUploadTarget(t, srv.db, 0, historyKey)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if answered := srv.uploadRecording(t, uploadedRecording{
		key:       historyKey,
		systemRef: 10001,
		talkgroup: 1514000,
		filename:  "history.wav",
		audioName: "history.wav",
		audio:     recording(2048),
		at:        time.Now().Add(-time.Minute).Truncate(time.Second),
	}); answered.Code != http.StatusOK {
		t.Fatalf("the upload was answered %d: %s", answered.Code, answered.Body.String())
	}

	rec := srv.post("/api/calls/search", `{"limit":10}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/calls/search = %d, want the page: %s", rec.Code, rec.Body.String())
	}

	var page struct {
		Count   uint `json:"count"`
		Results []struct {
			ID          uint64  `json:"id"`
			System      uint    `json:"system"`
			Talkgroup   uint    `json:"talkgroup"`
			Timestamp   string  `json:"dateTime"`
			AudioLength float64 `json:"audioLength"`
		} `json:"results"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("the search answer is not a page: %v", err)
	}

	if page.Count != 1 || len(page.Results) != 1 {
		t.Fatalf("the search found %d calls in %d rows, want the one call the upload recorded: %s",
			page.Count, len(page.Results), rec.Body.String())
	}

	if page.Results[0].System != 10001 || page.Results[0].Talkgroup != 1514000 {
		t.Errorf("the row names system %d talkgroup %d, want 10001 and 1514000: %s",
			page.Results[0].System, page.Results[0].Talkgroup, rec.Body.String())
	}

	id := page.Results[0].ID

	// One call, in full, and without its recording: the metadata answer is asked far more often
	// than a recording is listened to.
	metadata := srv.get(historyCallPath(id))

	if metadata.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want the call: %s", historyCallPath(id), metadata.Code, metadata.Body.String())
	}

	var answered map[string]any

	if err := json.Unmarshal(metadata.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the call answer is not an object: %v", err)
	}

	if _, ok := answered["audio"]; ok {
		t.Errorf("the metadata answer carries the recording's bytes; it is fetched at %s: %s",
			historyAudioPath(id), metadata.Body.String())
	}

	if answered["audioUrl"] != historyAudioPath(id) {
		t.Errorf("the call answer names audioUrl %v, want %s", answered["audioUrl"], historyAudioPath(id))
	}

	if answered["audioName"] != "history.wav" {
		t.Errorf("the call answer names the recording %v, want history.wav: %s",
			answered["audioName"], metadata.Body.String())
	}

	if answered["audioType"] != "audio/wav" {
		t.Errorf("the call answer names the recording type %v, want audio/wav: %s",
			answered["audioType"], metadata.Body.String())
	}

	// The recording, as the store kept it beside its call.
	audio := srv.get(historyAudioPath(id))

	if audio.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want the recording: %s", historyAudioPath(id), audio.Code, audio.Body.String())
	}

	if got := audio.Body.Len(); got != 2048 {
		t.Errorf("the recording is %d bytes, want the 2048 bytes the upload posted", got)
	}

	if got, want := audio.Header().Get("Content-Type"), "audio/wav"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	if got, want := audio.Header().Get("Content-Disposition"), `attachment; filename="history.wav"`; got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}

	// A range request is answered as the server answers a range for any other content: the slice,
	// with the total named.
	partial := srv.getRange(historyAudioPath(id), "bytes=0-99")

	if partial.Code != http.StatusPartialContent {
		t.Fatalf("GET %s with a range = %d, want %d", historyAudioPath(id), partial.Code, http.StatusPartialContent)
	}

	if got := partial.Body.Len(); got != 100 {
		t.Errorf("the range answer is %d bytes, want the 100 asked for", got)
	}

	// A call that is not there is refused as a call that is not there, and so is its recording.
	if missing := srv.get(historyCallPath(4812)); missing.Code != http.StatusNotFound {
		t.Errorf("GET %s = %d, want %d", historyCallPath(4812), missing.Code, http.StatusNotFound)
	}

	if missing := srv.get(historyAudioPath(4812)); missing.Code != http.StatusNotFound {
		t.Errorf("GET %s = %d, want %d", historyAudioPath(4812), missing.Code, http.StatusNotFound)
	}

	// A method the entry point does not serve is refused as a method that is not allowed, not as a
	// path that does not exist.
	if method := srv.get("/api/calls/search"); method.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/calls/search = %d, want %d", method.Code, http.StatusMethodNotAllowed)
	}

	if method := srv.post(historyCallPath(id), `{}`); method.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %s = %d, want %d", historyCallPath(id), method.Code, http.StatusMethodNotAllowed)
	}

	// An unreadable search body is the caller's mistake, and is answered as one.
	if unreadable := srv.post("/api/calls/search", `{"limit"`); unreadable.Code != http.StatusBadRequest {
		t.Errorf("POST /api/calls/search with an unreadable body = %d, want %d",
			unreadable.Code, http.StatusBadRequest)
	}

	// An administrator session opens the same history, and is the credential the administrator's
	// own page already presents.
	token := srv.loginAnswered(t)

	if asAdmin := srv.postAuthorized("/api/calls/search", token, `{"limit":10}`); asAdmin.Code != http.StatusOK {
		t.Errorf("an administrator session was answered %d, want the page: %s",
			asAdmin.Code, asAdmin.Body.String())
	}
}

// A server with an access list is restricted, and the call history is listener-facing: a caller that
// proves nothing is refused, an access code opens exactly what that access grants, and a call outside
// the grant is refused as a call that is not there.
func TestARestrictedServerOpensTheCallHistoryToAnAccessCodeAndToNothingElse(t *testing.T) {
	srv := journeyServerIn(t, t.TempDir())

	seedUploadTarget(t, srv.db, 0, historyKey)
	seedSecondSystem(t, srv.db)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	for _, upload := range []uploadedRecording{
		{key: historyKey, systemRef: 10001, talkgroup: 1514000, filename: "on-granted.wav", audio: recording(2048), at: time.Now().Add(-3 * time.Minute)},
		{key: historyKey, systemRef: 10002, talkgroup: 1515000, filename: "outside.wav", audio: recording(1024), at: time.Now().Add(-2 * time.Minute)},
	} {
		if answered := srv.uploadRecording(t, upload); answered.Code != http.StatusOK {
			t.Fatalf("the upload for system %d was answered %d: %s", upload.systemRef, answered.Code, answered.Body.String())
		}
	}

	token := srv.loginAnswered(t)

	// The administrator installs the access list the way an administrator installs it, and the
	// server becomes restricted by it.
	if saved := srv.put("/api/admin/config/access", token, `[
	  { "id": 0, "code": "`+historyOpenCode+`", "ident": "History open", "systems": "*" },
	  { "id": 0, "code": "`+historyScopedKey+`", "ident": "History scoped", "systems": [{ "id": 10001, "talkgroups": "*" }] }
	]`); saved.Code != http.StatusOK {
		t.Fatalf("saving the access list was answered %d: %s", saved.Code, saved.Body.String())
	}

	// The history is now closed to a caller that proves nothing.
	for _, path := range []string{"/api/calls/search", historyCallPath(1), historyAudioPath(1)} {
		var refused *httptest.ResponseRecorder

		if path == "/api/calls/search" {
			refused = srv.post(path, `{}`)
		} else {
			refused = srv.get(path)
		}

		if refused.Code != http.StatusUnauthorized {
			t.Errorf("%s with no credential = %d, want %d: a restricted server asks a caller to prove "+
				"itself", path, refused.Code, http.StatusUnauthorized)
		}
	}

	// The scoped code sees its system and nothing else.
	scoped := srv.codeRequest(http.MethodPost, "/api/calls/search", historyScopedKey, `{"limit":10}`)

	if scoped.Code != http.StatusOK {
		t.Fatalf("a scoped access code was answered %d, want the page: %s", scoped.Code, scoped.Body.String())
	}

	var page struct {
		Count   uint `json:"count"`
		Results []struct {
			ID     uint64 `json:"id"`
			System uint   `json:"system"`
		} `json:"results"`
	}

	if err := json.Unmarshal(scoped.Body.Bytes(), &page); err != nil {
		t.Fatalf("the search answer is not a page: %v", err)
	}

	if page.Count != 1 || len(page.Results) != 1 {
		t.Fatalf("the scoped caller found %d calls in %d rows, want the one on the system its access "+
			"names: %s", page.Count, len(page.Results), scoped.Body.String())
	}

	if page.Results[0].System != 10001 {
		t.Errorf("the scoped caller was shown system %d, which its access does not name: %s",
			page.Results[0].System, scoped.Body.String())
	}

	granted := page.Results[0].ID

	// The recording of a call the caller may see is served to that caller.
	if clip := srv.codeRequest(http.MethodGet, historyAudioPath(granted), historyScopedKey, ""); clip.Code != http.StatusOK {
		t.Errorf("the recording of a granted call = %d, want the bytes: %s", clip.Code, clip.Body.String())
	}

	// The call on the other system is refused as a call that is not there. The scoped caller has
	// seen both systems in its searches, so the server has to find it an id to ask for: the
	// wildcard code is asked first, and the id the scoped caller is not entitled to is the one the
	// wildcard answer carries and the scoped answer does not.
	opened := srv.codeRequest(http.MethodPost, "/api/calls/search", historyOpenCode, `{"limit":10}`)

	if opened.Code != http.StatusOK {
		t.Fatalf("the open access code was answered %d, want the page: %s", opened.Code, opened.Body.String())
	}

	var both struct {
		Count   uint `json:"count"`
		Results []struct {
			ID     uint64 `json:"id"`
			System uint   `json:"system"`
		} `json:"results"`
	}

	if err := json.Unmarshal(opened.Body.Bytes(), &both); err != nil {
		t.Fatalf("the search answer is not a page: %v", err)
	}

	if both.Count != 2 {
		t.Fatalf("the open access code found %d calls, want both: %s", both.Count, opened.Body.String())
	}

	var outside uint64

	for _, row := range both.Results {
		if row.System == 10002 {
			outside = row.ID
		}
	}

	if outside == 0 {
		t.Fatalf("the open access code found no call on system 10002: %s", opened.Body.String())
	}

	for _, path := range []string{historyCallPath(outside), historyAudioPath(outside)} {
		if refused := srv.codeRequest(http.MethodGet, path, historyScopedKey, ""); refused.Code != http.StatusNotFound {
			t.Errorf("a scoped caller asking for %s = %d, want %d: the caller is not told the call "+
				"exists", path, refused.Code, http.StatusNotFound)
		}
	}

	// A search naming a system the caller cannot see answers with no rows, and says nothing about
	// what exists outside the grant.
	if filtered := srv.codeRequest(http.MethodPost, "/api/calls/search", historyScopedKey,
		`{"system":10002,"talkgroup":1515000}`); filtered.Code != http.StatusOK {
		t.Fatalf("a scoped caller searching a system it cannot see = %d, want the page: %s",
			filtered.Code, filtered.Body.String())
	} else if count := countSearchRows(filtered); count != 0 {
		t.Errorf("a scoped caller searching a system it cannot see got %d rows, want none: %s",
			count, filtered.Body.String())
	}

	// An administrator session is not scoped by the listener access list.
	if asAdmin := srv.postAuthorized("/api/calls/search", token, `{"limit":10}`); asAdmin.Code != http.StatusOK {
		t.Fatalf("an administrator session was answered %d, want the page: %s", asAdmin.Code, asAdmin.Body.String())
	} else if count := countSearchRows(asAdmin); count != 2 {
		t.Errorf("an administrator session found %d calls, want both: %s", count, asAdmin.Body.String())
	}

	// The administrator's own credential opens the call the listener's scoped code was refused.
	if asAdmin := srv.getAuthorized(historyCallPath(outside), token); asAdmin.Code != http.StatusOK {
		t.Errorf("an administrator session asking for the call outside the scoped grant = %d, want "+
			"the call: %s", asAdmin.Code, asAdmin.Body.String())
	}
}

// Guessing an access code over HTTP is bounded per address, on the model of the administrator login
// budget: the attempts up to the bound are answered on their merits, and the one past it is refused
// whatever it carries, with an answer that gives away nothing distinguishing it from a wrong code.
func TestGuessingAccessCodesOverRestIsBoundedPerAddress(t *testing.T) {
	srv := journeyServerIn(t, t.TempDir())

	seedUploadTarget(t, srv.db, 0, historyKey)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if answered := srv.uploadRecording(t, uploadedRecording{
		key:       historyKey,
		systemRef: 10001,
		talkgroup: 1514000,
		filename:  "bounded.wav",
		audio:     recording(2048),
		at:        time.Now().Add(-time.Minute),
	}); answered.Code != http.StatusOK {
		t.Fatalf("the upload was answered %d: %s", answered.Code, answered.Body.String())
	}

	token := srv.loginAnswered(t)

	if saved := srv.put("/api/admin/config/access", token, `[
	  { "id": 0, "code": "`+historyOpenCode+`", "ident": "History open", "systems": "*" }
	]`); saved.Code != http.StatusOK {
		t.Fatalf("saving the access list was answered %d: %s", saved.Code, saved.Body.String())
	}

	var first *httptest.ResponseRecorder

	for attempt := 1; attempt <= historyAttemptsMax; attempt++ {
		refused := srv.codeRequest(http.MethodPost, "/api/calls/search", "not-an-access-code", `{}`)

		if refused.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want %d: the attempts up to the bound are refused as refusals",
				attempt, refused.Code, http.StatusUnauthorized)
		}

		if first == nil {
			first = refused
		} else if refused.Body.String() != first.Body.String() || refused.Code != first.Code {
			t.Errorf("attempt %d is answered differently from attempt 1: a guessing caller must not be "+
				"told how far it got", attempt)
		}
	}

	// The correct code, arriving after the budget is spent, is refused — and the answer gives away
	// nothing that distinguishes it from the refusals above.
	spent := srv.codeRequest(http.MethodPost, "/api/calls/search", historyOpenCode, `{}`)

	if spent.Code != http.StatusUnauthorized {
		t.Fatalf("a correct code after the budget was spent = %d, want %d", spent.Code, http.StatusUnauthorized)
	}

	if spent.Body.String() != first.Body.String() {
		t.Errorf("the spent-budget answer is %q, want the byte-identical answer a wrong code gets (%q)",
			spent.Body.String(), first.Body.String())
	}

	// The refusals name the address, once per window, and never the code that was guessed.
	if lines := countLogLines(t, srv.db, "a caller from "+testRequestAddress+" presented an access code that is not valid"); lines != 1 {
		t.Errorf("the log carries %d lines about the guessing address, want exactly one per window", lines)
	}

	if lines := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs" WHERE "message" LIKE '%not-an-access-code%'`); lines != 0 {
		t.Errorf("the log files %d lines naming the credential that was guessed, want none", lines)
	}

	// The budget belongs to the address that spent it, and a journey reaching the server from
	// another address is answered on its merits.
	fromElsewhere := srv.codeRequestFrom(http.MethodPost, "/api/calls/search", historyOpenCode, `{}`, "198.51.100.4")

	if fromElsewhere.Code != http.StatusOK {
		t.Errorf("a quiet address presenting the valid code = %d, want the page: %s",
			fromElsewhere.Code, fromElsewhere.Body.String())
	}
}

// countSearchRows is how many rows a search answer carries.
func countSearchRows(rec *httptest.ResponseRecorder) int {
	var answered struct {
		Results []map[string]any `json:"results"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		return -1
	}

	return len(answered.Results)
}

func historyCallPath(id uint64) string {
	return "/api/calls/" + strconv.FormatUint(id, 10)
}

func historyAudioPath(id uint64) string {
	return historyCallPath(id) + "/audio"
}

// seedSecondSystem adds the system a scoped access does not name, so a scoped caller has something
// to be refused it.
func seedSecondSystem(t *testing.T, db *sql.DB) {
	t.Helper()

	seed(t, db,
		seedStatement{
			query: `INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (2, 0, '', 0, 'Harbour', '', 1, 10002, '')`,
		},
		seedStatement{
			query: `INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (2, '', 0, 0, 'Marine', '', 'Channel 16', 0, 2, 9, 1515000, '')`,
		},
	)
}
