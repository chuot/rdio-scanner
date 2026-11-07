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
	"net/http"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/domain"
)

// The server log view must name the part of the server that wrote
// each entry and say what happened, not code names: the journey
// drives each writer and the guard checks the log view.
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

	// one level of one system, saved as the Sites section saves
	// it: the entry names the system by the number the operator
	// reads and types, not the store's row identity
	if rec := srv.put("/api/admin/config/systems/1/sites", token, `[{"id":1,"label":"Metro East","siteRef":101},{"id":2,"label":"Metro West","siteRef":102}]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the sites of a system was answered %d: %s", rec.Code, rec.Body.String())
	}

	// a recording the server accepts, posted as an uploader posts
	// one, stamped now so the retention pass has nothing to shed
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

	// a listener presents an unknown code, unlocks with a known
	// one, then asks how many listeners are watching
	conn := srv.dial(t, "/")

	sendFrame(t, conn, `["PIN","`+encodeAccessCode("not-an-access-code")+`"]`)
	sendFrame(t, conn, `["PIN","`+encodeAccessCode("capture-open")+`"]`)
	sendFrame(t, conn, `["LSC"]`)

	// socket handlers write on their own schedule, so the journey
	// waits for the invalid code and the debounced listener count
	waitForLogMentioning(t, srv.db, "presented an access code that is not valid")
	waitForLogMentioning(t, srv.db, "there is 1 listener")

	// the retention pass, wired as the composition root wires it
	// and handed the command the hourly schedule hands it:
	// nothing here is old enough to discard, and it reports that
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

	// each writer this journey aimed at, with the sentence it must put
	// in the log view; the source is read back, so wrong part fails
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

	// the guard over all the running server wrote, unaimed entries too
	assertEveryServerLogEntry(t, srv.db)
}
