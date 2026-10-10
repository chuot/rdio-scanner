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
	"encoding/json"
	"net/http"
	"testing"
)

// These tests put a restricted server together the way a deployment puts one together — a configuration
// saved by an administrator, read back from the database at start — and then hold a real listener socket
// against it. The frames are compared as the bytes the server wrote, against the bytes the legacy server
// wrote in the same configuration, which is what makes them parity tests rather than a description of what
// This server happens to do.
//
// The configuration is the one the golden captures were taken from, installed through the same administrator
// endpoints in the same order, so that the two servers are answering in a configuration the legacy server's
// answers were recorded in. It is the recorded configuration in fixtures_test.go: the harness that
// originally installed it is retired, so the bytes it wrote are kept in the package that still installs them.

// The access codes the capture configures for its restricted phase, and what each one is there to be
// tested for: one that names systems, one whose date has passed, and one that allows one session.
const restrictedAccesses = `[
  { "id": 0, "code": "capture-open", "ident": "Capture open", "systems": "*" },
  { "id": 0, "code": "capture-scoped", "ident": "Capture scoped", "systems": [{ "id": 10001, "talkgroups": "*" }] },
  { "id": 0, "code": "capture-expired", "ident": "Capture expired", "systems": "*", "expiration": "1970-01-01T00:00:01Z" },
  { "id": 0, "code": "capture-limited", "ident": "Capture limited", "limit": 1, "systems": "*" }
]`

// A restricted server answers a command it cannot place with the access-code prompt, and answers the
// version request even to a listener that has unlocked nothing.
func TestARestrictedServerAsksForAnAccessCodeBeforeAnythingElse(t *testing.T) {
	srv, _ := configuredTestServer(t)

	golden := readRestrictedGolden(t)

	conn := srv.dial(t, "/")

	sendFrame(t, conn, `["VER"]`)

	// The version is answered even before the access code: a client that has unlocked nothing still
	// learns what server it is talking to.
	assertFrame(t, conn, golden.scoped[0])

	for offset, command := range []string{"CFG", "LCL", "CAL", "LFM"} {
		sendFrame(t, conn, `["`+command+`"]`)

		assertFrame(t, conn, golden.scoped[1+offset])
	}

	// A code the server does not know spends an attempt and is answered with the same prompt.
	sendFrame(t, conn, `["PIN","`+encodeAccessCode("not-an-access-code")+`"]`)

	assertFrame(t, conn, golden.scoped[5])

	waitForLogMentioning(t, srv.db, "a listener from 127.0.0.1 presented an access code that is not valid")

	// A code the server does know brings that listener the configuration it is entitled to see, and
	// nothing else: the systems, groups and tags in it are the ones that access names.
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

// An access code whose date has passed is refused with the expiry rather than the prompt, so that a client
// can tell a code that is wrong from a code that has run out.
func TestAnExpiredAccessCodeIsAnsweredWithAnExpiry(t *testing.T) {
	srv, _ := configuredTestServer(t)

	golden := readRestrictedGolden(t)

	conn := srv.dial(t, "/")

	sendFrame(t, conn, `["PIN","`+encodeAccessCode("capture-expired")+`"]`)

	assertFrame(t, conn, golden.expired[0])

	waitForLogMentioning(t, srv.db, "the access code for ident Capture expired has expired")
}

// An access code carrying a limit counts the sessions already unlocked with it, and refuses one more than
// that. The count is of sessions that are listeners, which is why a code limited to one allows two
// connections and refuses the third: a session is not counted until it has been given its configuration.
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

// A configuration change on a restricted server does not push the configuration: it pushes the request to
// unlock again, to every listener, including one that has already unlocked. The change that prompted it may
// be the access that decides what a listener may see, and a server that re-sent the old view would be
// showing a listener something it may no longer be entitled to.
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

	// The administrator calls the capture made, in the same order, so that the configuration this server
	// answers in is the configuration the captured answers were recorded in.
	//
	// The capture also re-saved the tags and the groups after this point, and those two saves are left
	// out. Each of them names one row with no identity yet, which is the case Q33 is about: the legacy
	// server keeps every stored row it is not shown and inserts the new one again, so those saves left
	// duplicate groups and tags behind without changing anything a listener sees. This server reconciles
	// every section the same way — the list you save is the list you get — and running the same saves
	// here would drop the group the talkgroups are filed under and re-create it under a new identity,
	// which is a difference the golden has nothing to do with. What both servers reach from the
	// whole-configuration save alone is what this test compares.
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

// restrictedGolden is the shape the recorded restricted frames are read in: the frames the legacy server
// wrote during its restricted phase, in the order it wrote them.
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
