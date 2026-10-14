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
	"net/http"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/domain"
)

// The wording rule is about what an operator reads in the log view: the entry says which part of the server
// wrote it, and it says what happened rather than the name of the code that wrote it. Each use case is
// pinned by the test of the use case that writes the sentence. What a unit test cannot prove is that the
// sentences reach the log view of a running server in the shape the rule asks for, which is what this journey
// does: it drives a server through the things the application layer's writers exist to report — a refused
// sign-in, a configuration save, a recording accepted, an access code that is not valid, a listener asking
// how many listeners there are, the retention pass, and a sign-out — and then reads the server's own log
// view, entry by entry.
//
// The guard reads every entry the server wrote, not only the ones this journey aimed at, so a writer that
// files an entry with no source, an unknown source, or a message that begins with the internal name of the
// code, fails here even when the journey never aimed at it. The journey also names each writer it reached,
// with the sentence it is meant to produce, so that the guard cannot pass by proving nothing: a writer that
// stopped writing is a failure of this test and not a quiet empty set.
func TestTheServerLogViewNamesThePartOfTheServerThatWroteEachEntry(t *testing.T) {
	srv, token := configuredTestServer(t)

	// A sign-in the server refused: the attempt is reported, and the credential is not looked at.
	if rec := srv.post("/api/admin/login", `{"password":"not-the-credential"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a refused sign-in was answered %d: %s", rec.Code, rec.Body.String())
	}

	// A configuration save, the way an administrator's form saves one section.
	if rec := srv.put("/api/admin/config/options", token, `{"maxClients":60}`); rec.Code != http.StatusOK {
		t.Fatalf("saving the options was answered %d: %s", rec.Code, rec.Body.String())
	}

	// One level of one system, saved the way the Sites section of the administration form saves it, with
	// the sites the store already holds. The entry this writes names the system by the number the
	// operator reads in the section and types into the fields, not by the identity the store gave the row.
	if rec := srv.put("/api/admin/config/systems/1/sites", token, `[{"id":1,"label":"Metro East","siteRef":101},{"id":2,"label":"Metro West","siteRef":102}]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the sites of a system was answered %d: %s", rec.Code, rec.Body.String())
	}

	// A recording the server accepts, posted the way an uploader posts one. The upload is stamped with the
	// current instant so that the retention pass this journey runs afterwards has nothing to discard.
	answered := srv.uploadRecording(t, uploadedRecording{
		key:       "capture-apikey-open",
		systemRef: 10001,
		talkgroup: 1514000,
		filename:  "wording.wav",
		audio:     recording(2048),
		at:        time.Now().Truncate(time.Millisecond),
	})

	if answered.Code != http.StatusOK {
		t.Fatalf("the recording was answered %d: %s", answered.Code, answered.Body.String())
	}

	// A listener that presents a code the server does not know, then unlocks with one it does, then asks
	// how many listeners are watching.
	conn := srv.dial(t, "/")

	sendFrame(t, conn, `["PIN","`+encodeAccessCode("not-an-access-code")+`"]`)
	sendFrame(t, conn, `["PIN","`+encodeAccessCode("capture-open")+`"]`)
	sendFrame(t, conn, `["LSC"]`)

	// The socket handlers write their entries on their own schedule, so the journey waits for the two the
	// listener produces: the code it presented, and the count the debouncer reports once a listener joined.
	waitForLogMentioning(t, srv.db, "presented an access code that is not valid")
	waitForLogMentioning(t, srv.db, "there is 1 listener")

	// The retention pass, built the way the composition root wires it and handed the command the hourly
	// schedule hands it. Nothing in this server is older than the retention, so the pass has nothing to
	// discard, and it reports that.
	result, err := srv.pruneHandler(t).Handle(context.Background(), application.PruneOldCallsCommand{})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.Outcome != application.PruneOldCallsDone {
		t.Fatalf("the pruning reported %q, want it done", result.Outcome)
	}

	// The sign-out, which is the entry an operator reads when an administrator's session ends.
	if rec := srv.postAuthorized("/api/admin/logout", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("the sign-out was answered %d: %s", rec.Code, rec.Body.String())
	}

	// Each writer this journey aimed at, with the sentence it is meant to put in the log view. The source is
	// read back from the stored entry, so a sentence filed under the wrong part of the server fails here.
	for _, writer := range []struct {
		source  domain.LogSource
		message string
	}{
		{domain.LogSourceLogin, "a login attempt from " + testRequestAddress + " was refused"},
		{domain.LogSourceConfig, "the options configuration changed"},
		{domain.LogSourceConfig, "the sites configuration of system 10001 changed"},
		{domain.LogSourceIngest, "call from system 10001 talkgroup 1514000 success, wording.wav"},
		{domain.LogSourceAccess, "a listener from 127.0.0.1 presented an access code that is not valid"},
		{domain.LogSourceLivefeed, "there is 1 listener"},
		{domain.LogSourcePrune, "the retention pass removed 0 calls and 0 log entries"},
		{domain.LogSourceLogin, "the administrator signed out"},
	} {
		if !hasLogMessage(t, srv.db, writer.message) {
			t.Errorf("the log view does not hold %q, the sentence %s writes", writer.message, writer.source)

			continue
		}

		if got := logSourceOfMessage(t, srv.db, writer.message); got != string(writer.source) {
			t.Errorf("%q is filed under %q, want %s", writer.message, got, writer.source)
		}
	}

	// The guard over everything the running server wrote, including the entries this journey never aimed at.
	assertEveryServerLogEntry(t, srv.db)
}
