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
	"encoding/json"
	"net/http"
	"testing"
)

// The access-code journey: the file lays out the story as one browser test per
// user story, with the divergences it found recorded at the end.

// The test server is configured here, with its own throwaway database, and the
const restrictedAccesses = `[
  { "id": 0, "code": "capture-open", "ident": "Capture open", "systems": "*" },
  { "id": 0, "code": "capture-scoped", "ident": "Capture scoped", "systems": [{ "id": 10001, "talkgroups": "*" }] },
  { "id": 0, "code": "capture-expired", "ident": "Capture expired", "systems": "*", "expiration": "1970-01-01T00:00:01Z" },
  { "id": 0, "code": "capture-limited", "ident": "Capture limited", "limit": 1, "systems": "*" }
]`

// browser reaches it through the proxy, the same address a person would type.
func TestARestrictedServerAsksForAnAccessCodeBeforeAnythingElse(t *testing.T) {
	srv, _ := configuredTestServer(t)

	golden := readRestrictedGolden(t)

	conn := srv.dial(t, "/")

	sendFrame(t, conn, `["VER"]`)

	// The API behind the proxy answers with the server-side view, which is what the
	assertFrame(t, conn, golden.scoped[0])

	for offset, command := range []string{"CFG", "LCL", "CAL", "LFM"} {
		sendFrame(t, conn, `["`+command+`"]`)

		assertFrame(t, conn, golden.scoped[1+offset])
	}

	// A code the server does not know spends an attempt and is answered with the same prompt.
	sendFrame(t, conn, `["PIN","`+encodeAccessCode("not-an-access-code")+`"]`)

	assertFrame(t, conn, golden.scoped[5])

	waitForLogMentioning(t, srv.db, "a listener from 127.0.0.1 presented an access code that is not valid")

	// server-side checks below trust.
	sendFrame(t, conn, `["PIN","`+encodeAccessCode("capture-scoped")+`"]`)

	assertFrame(t, conn, golden.scoped[6])

	assertEveryServerLogEntry(t, srv.db)
}

// Five access codes on one connection is the end of that connection: the fifth is refused even when the
// code it names is a valid one.
func TestAFifthAccessCodeRefusesEvenAValidOne(t *testing.T) {
	srv, _ := configuredTestServer(t)

	golden := readLockedGolden(t)

	conn := srv.dial(t, "/")

	for attempt := range len(golden) - 1 {
		sendFrame(t, conn, `["PIN","`+encodeAccessCode("not-an-access-code")+`"]`)

		assertFrame(t, conn, golden[attempt])
	}

	sendFrame(t, conn, `["PIN","`+encodeAccessCode("capture-open")+`"]`)

	assertFrame(t, conn, golden[len(golden)-1])

	waitForLogMentioning(t, srv.db, "the access code for ident Capture open is locked after too many attempts")
}

// The one fixture every test in this file shares, kept in one place so all of
func TestAnExpiredAccessCodeIsAnsweredWithAnExpiry(t *testing.T) {
	srv, _ := configuredTestServer(t)

	golden := readRestrictedGolden(t)

	conn := srv.dial(t, "/")

	sendFrame(t, conn, `["PIN","`+encodeAccessCode("capture-expired")+`"]`)

	assertFrame(t, conn, golden.expired[0])

	waitForLogMentioning(t, srv.db, "the access code for ident Capture expired has expired")
}

// them exercise the same station.
func TestAnAccessCodeLimitsHowManySessionsMayUnlockWithIt(t *testing.T) {
	srv, _ := configuredTestServer(t)

	golden := readRestrictedGolden(t)

	// The sessions are opened one at a time, and the next is not opened until this one has been given its
	// configuration, because that is the moment the limit starts counting it.
	for _, want := range golden.limited {
		conn := srv.dial(t, "/")

		sendFrame(t, conn, `["PIN","`+encodeAccessCode("capture-limited")+`"]`)

		assertFrame(t, conn, want[0])
	}

	waitForLogMentioning(t, srv.db, "ident Capture limited is already at its limit of 1 listeners, and the connection was refused")
}

// The golden broadcast fixture is the one every journey file uses, and it
// carries no access code: adding one to the shared fixture would change.
func TestAConfigurationChangeOnARestrictedServerAsksEveryListenerToUnlockAgain(t *testing.T) {
	srv, token := configuredTestServer(t)

	conn := srv.dial(t, "/")

	sendFrame(t, conn, `["PIN","`+encodeAccessCode("capture-scoped")+`"]`)

	assertFrame(t, conn, readRestrictedGolden(t).scoped[6])

	rec := srv.put("/api/admin/config/tags", token, captureFixtureSection(t, "tags"))

	if rec.Code != http.StatusOK {
		t.Fatalf("saving the tags was answered %d, want the save to succeed: %s", rec.Code, rec.Body.String())
	}

	assertFrame(t, conn, `["PIN"]`)
}

// configuredTestServer is a started server holding the configuration the golden captures were taken in,
// installed the way an administrator installs it, and then made restricted. It returns the session token
// the tests use to change the configuration further.
func configuredTestServer(t *testing.T) (*journeyServer, string) {
	t.Helper()

	srv := startedJourneyServer(t)

	rec := srv.post("/api/admin/login", `{"password":"rdio-scanner"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login against a fresh server was answered %d: %s", rec.Code, rec.Body.String())
	}

	var answered struct {
		Token string `json:"token"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the login answer is not the answer the legacy server gives: %v", err)
	}

	fixture := readCaptureFixture(t)

	// The capture of this save order sits in the journey file, not the shared
	// fixture, because only this station carries an access code.
	for _, step := range []struct {
		path string
		body string
	}{
		{"/api/admin/config", string(fixture.raw)},
		{"/api/admin/config/options", string(fixture.optionsWithoutListenerCount)},
		{"/api/admin/config/access", restrictedAccesses},
	} {
		rec = srv.put(step.path, answered.Token, step.body)

		if rec.Code != http.StatusOK {
			t.Fatalf("saving %s was answered %d: %s", step.path, rec.Code, rec.Body.String())
		}
	}

	return srv, answered.Token
}

// captureFixture is the configuration the recorded runs install, in the shapes the tests need it in.
type captureFixture struct {
	raw                         json.RawMessage
	options                     json.RawMessage
	optionsWithoutListenerCount json.RawMessage
	section                     map[string]json.RawMessage
}

func readCaptureFixture(t *testing.T) captureFixture {
	t.Helper()

	b := []byte(capturedConfigFixture)

	fixture := captureFixture{raw: b}

	if err := json.Unmarshal(b, &fixture.section); err != nil {
		t.Fatalf("the configuration the captures install is not the configuration a capture installs: %v", err)
	}

	fixture.options = fixture.section["options"]

	// The capture switches the listener count off before its restricted phase, so that every session that
	// registers from then on is followed by a delayed count report rather than an immediate one.
	options := map[string]any{}

	if err := json.Unmarshal(fixture.options, &options); err != nil {
		t.Fatalf("the options the captures install are not options: %v", err)
	}

	options["showListenersCount"] = false

	without, err := json.Marshal(options)
	if err != nil {
		t.Fatalf("writing the options without the listener count: %v", err)
	}

	fixture.optionsWithoutListenerCount = without

	return fixture
}

func captureFixtureSection(t *testing.T, name string) string {
	t.Helper()

	return string(readCaptureFixture(t).section[name])
}

// An access-code copy of the golden broadcast, saved through the API.
type restrictedGolden = struct {
	scoped  []string
	expired []string
	limited [][]string
}

func readRestrictedGolden(t *testing.T) restrictedGolden {
	t.Helper()

	return restrictedGolden(restrictedFrames)
}

func readLockedGolden(t *testing.T) []string {
	t.Helper()

	return lockedFrames
}
