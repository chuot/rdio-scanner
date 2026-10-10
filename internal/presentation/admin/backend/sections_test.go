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

package backend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

// The per-section administrator endpoints, ported from the suite the legacy server carries for them.
// They are the half of the configuration surface an administrator's page actually uses: it saves one
// section at a time, and it is answered with that section and not with the whole configuration.

// A section this server does not have is refused before anything else is looked at, including whether the
// request carries a credential: the endpoint does not know what it would be protecting, and an
// administrator's page that asks for a section that does not exist is told which of its assumptions is
// wrong.
func TestSectionEndpointRejectsASectionThisServerDoesNotHave(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodGet, "/api/admin/config/bogus", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}

	rec = fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/config/bogus"})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 and not 401: the section is decided before the credential", rec.Code)
	}

	rec = fixture.authorized(http.MethodGet, "/api/admin/config/", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a path that names no section: %s", rec.Code, rec.Body.String())
	}
}

// A section is only shown to a request carrying a credential the server accepts.
func TestSectionEndpointRequiresACredentialTheServerAccepts(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/config/groups", token: "not-a-token-the-server-issued"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.Len() != 0 {
		t.Errorf("an unauthorized section request answers %q, want nothing", rec.Body.String())
	}
}

// A section read is answered with that section and nothing else, which is what lets an administrator's
// page fetch the one list it is showing.
func TestSectionEndpointAnswersOnlyTheSectionItIsAskedFor(t *testing.T) {
	fixture := newAdminFixture()

	fixture.groups.Stored = domain.Groups{{ID: 1, Label: "Dispatch"}}
	fixture.tags.Stored = domain.Tags{{ID: 2, Label: "Law"}}

	rec := fixture.authorized(http.MethodGet, "/api/admin/config/groups", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("groups status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	entries := decodedSection(t, rec)

	if len(entries) != 1 || entries[0]["label"] != "Dispatch" {
		t.Fatalf("groups answer = %s, want the one group the server has", rec.Body.String())
	}

	rec = fixture.authorized(http.MethodGet, "/api/admin/config/tags", "")

	entries = decodedSection(t, rec)

	if len(entries) != 1 || entries[0]["label"] != "Law" {
		t.Errorf("tags answer = %s, want the one tag the server has", rec.Body.String())
	}
}

// A section save replaces that section and leaves every other section running. An administrator's page
// saves one list at a time, and a server that replaced the whole configuration from a partial submission
// would empty the sections the page did not show.
func TestSectionSaveReplacesOnlyTheSectionItSaves(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	before := fixture.systems.Stored

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/groups", `[{"id":1,"label":"Renamed Group"},{"label":"New Group"}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("groups save status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.groups.Stored) != 2 {
		t.Errorf("the server runs with %v, want the two groups the submitted list names", fixture.groups.Stored)
	}

	if fixture.groups.Stored[0].Label != "Renamed Group" || fixture.groups.Stored[1].Label != "New Group" {
		t.Errorf("the groups the server runs with are %v, want the two the submitted list names", fixture.groups.Stored)
	}

	if len(fixture.systems.Stored) != len(before) {
		t.Errorf("the systems the server runs with changed to %v, want the %d it had", fixture.systems.Stored, len(before))
	}

	entries := decodedSection(t, rec)

	if len(entries) != 2 || entries[0]["label"] != "Renamed Group" {
		t.Errorf("the answer is %s, want the section that was just saved", rec.Body.String())
	}

	if !fixture.loggedContaining("configuration changed") {
		t.Errorf("the save is not in the log view as the legacy server wrote it: %v", fixture.logs.Stored)
	}

	if len(fixture.sessions.Announced) == 0 {
		t.Errorf("the open administrator sessions were not told the configuration changed")
	}
}

// A section save stops the watched directories and starts them again, whatever section it saves: a save
// can change the settings a watcher reads, and a watcher left running against a configuration that no
// longer describes it keeps importing calls under rules the administrator just removed.
func TestSectionSaveOfAnotherSectionKeepsTheWatchedDirectoriesRunning(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/dirwatch",
		`[{"directory":"/tmp/opencode/capture-watch","mask":"#S#TG#YYYY-MM-DDTHH-mm-ss","type":"default"}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("dirwatch save status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.dirwatches.Stored) != 1 || fixture.dirwatches.Stored[0].Directory != "/tmp/opencode/capture-watch" {
		t.Fatalf("the server runs with %v, want the one directory the submitted list names", fixture.dirwatches.Stored)
	}

	rec = fixture.authorized(http.MethodPut, "/api/admin/config/groups", `[{"label":"Renamed Group"}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("groups save status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.dirwatches.Stored) != 1 {
		t.Errorf("the watched directories changed to %v, want the one the earlier save left", fixture.dirwatches.Stored)
	}

	if fixture.watches.Stops != 2 {
		t.Errorf("the watched directories were stopped %d times, want once per save", fixture.watches.Stops)
	}

	if len(fixture.watches.Starts) != 2 {
		t.Fatalf("the watched directories were started %d times, want once per save", len(fixture.watches.Starts))
	}

	if len(fixture.watches.Starts[1]) != 1 || fixture.watches.Starts[1][0].Directory != "/tmp/opencode/capture-watch" {
		t.Errorf("the directories started after the groups save are %v, want the watched directory the server runs with",
			fixture.watches.Starts[1])
	}
}

// A system carries its talkgroups, sites, and units inside it, and a save of the systems section is where
// all of them are replaced. A save that only read the system's own fields would quietly drop the
// talkgroups an administrator just named, and the calls that arrive for them would find nowhere to go.
func TestSectionSaveSystemsKeepsWhatEachSystemCarriesInsideIt(t *testing.T) {
	fixture := newAdminFixture()

	fixture.tags.Stored = domain.Tags{{ID: 3, Label: "Law"}}

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/systems",
		`[{"label":"Test System","systemRef":10,"talkgroups":[{"label":"TG1","talkgroupRef":101,"order":1,"tagId":3}]}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("systems save status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.systems.Stored) != 1 {
		t.Fatalf("the server runs with %v, want the one system the submitted list names", fixture.systems.Stored)
	}

	system := fixture.systems.Stored[0]

	if system.Label != "Test System" || system.SystemRef != 10 {
		t.Errorf("the system is %#v, want the label and reference the submitted list names", system)
	}

	if len(system.Talkgroups) != 1 || system.Talkgroups[0].Label != "TG1" || system.Talkgroups[0].TalkgroupRef != 101 {
		t.Errorf("the system carries %v, want the one talkgroup it names", system.Talkgroups)
	}

	entries := decodedSection(t, rec)

	if len(entries) != 1 {
		t.Fatalf("the answer is %s, want the one system that was saved", rec.Body.String())
	}

	talkgroups, ok := entries[0]["talkgroups"].([]any)
	if !ok || len(talkgroups) != 1 {
		t.Errorf("the answer carries %v, want the talkgroup the saved system names", entries[0]["talkgroups"])
	}
}

// The settings are merged into the settings in effect, because the legacy server applies the submitted
// settings onto the ones it is running with and an administrator's page submits only the settings that
// page shows. The other sections are left running.
func TestSectionSaveOptionsMergesIntoTheSettingsInEffect(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	before := fixture.groups.Stored

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/options", `{"branding":"Renamed Dispatch","maxClients":120}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("options save status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if fixture.options.Stored.Branding != "Renamed Dispatch" || fixture.options.Stored.MaxClients != 120 {
		t.Errorf("the settings in effect are %#v, want the two the submitted settings name", fixture.options.Stored)
	}

	if fixture.options.Stored.Email != "dispatch@example.com" {
		t.Errorf("the email in effect is %q, want the one the server had: a setting that was not submitted must "+
			"survive a save that names other settings", fixture.options.Stored.Email)
	}

	if len(fixture.groups.Stored) != len(before) {
		t.Errorf("the groups the server runs with changed to %v, want the %d it had", fixture.groups.Stored, len(before))
	}

	body := decodedBody(t, rec)

	settings, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("the answer is not the settings that were saved: %s", rec.Body.String())
	}

	if settings["branding"] != "Renamed Dispatch" {
		t.Errorf("the answer is %s, want the settings that were just saved", rec.Body.String())
	}
}

// A section save with a body the endpoint cannot read is answered with 400 — and the watched directories
// are still stopped and started, because the legacy server stops them before it reads the body and its
// deferred restart runs whatever happens to the read. A server that answered 400 without restarting them
// would leave the watchers stopped by a request that changed nothing.
func TestSectionSaveRejectsABodyItCannotReadAndStillRestartsTheWatchedDirectories(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	before := len(fixture.groups.Stored)

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/groups", `[{"label":`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("groups save status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	if fixture.watches.Stops != 1 {
		t.Errorf("the watched directories were stopped %d times, want once: the legacy server stops them before "+
			"it reads what was submitted", fixture.watches.Stops)
	}

	if len(fixture.watches.Starts) != 1 {
		t.Fatalf("the watched directories were started %d times, want once after the refused save", len(fixture.watches.Starts))
	}

	if len(fixture.groups.Stored) != before {
		t.Errorf("the groups the server runs with are %v, want the %d the capture left: a body that could not be "+
			"read saves nothing", fixture.groups.Stored, before)
	}

	if len(fixture.sessions.Announced) != 0 {
		t.Errorf("the open administrator sessions were told the configuration changed for a save that changed nothing")
	}

	if fixture.loggedContaining("admin.sectionhandler.put") {
		t.Errorf("a body that could not be read is written in the log view as a failed save: %v", fixture.logs.Stored)
	}
}

// A section save that could not read its body files the fact with the administrator's own part of the log,
// naming the section's path — and it is not filed as a failed save, because the save never started.
//
// The settings are included because they are read a different way from a list: they are merged into the
// settings in effect, so their read is a separate one, and an operator whose settings page stopped saving
// should not have to guess which half of the section path was at fault.
func TestSectionSavesFileARequestBodyTheyCouldNotRead(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	for _, section := range []struct {
		path string
		body string
	}{
		{"/api/admin/config/groups", `{"label":"not a list"}`},
		{"/api/admin/config/options", `{"maxClients": `},
	} {
		before := len(filedUnder(fixture, domain.LogSourceAdmin))

		rec := fixture.serve(adminRequest{
			body:      section.body,
			method:    http.MethodPut,
			path:      section.path,
			token:     captureToken,
			Forwarded: captureAddress + ":8123",
		})

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s answered %d for a body it could not read, want 400: %s", section.path, rec.Code,
				rec.Body.String())
		}

		wrote := filedUnder(fixture, domain.LogSourceAdmin)[before:]

		if len(wrote) != 1 {
			t.Fatalf("%s wrote %v to the log view, want one entry naming the request it could not read: %v",
				section.path, wrote, fixture.logs.Stored)
		}

		entry := wrote[0]

		if !strings.HasPrefix(entry.Message, "the administrator's request to "+section.path+" from "+
			captureAddress+" could not be read: ") {
			t.Errorf("%s wrote %q, want it to name the path, the address, and the reason the body could not "+
				"be read", section.path, entry.Message)
		}

		if entry.Level != domain.LogLevelWarn {
			t.Errorf("%s filed the entry at %q, want a warning", section.path, entry.Level)
		}

		if got := entry.DateTime; !got.Equal(captureTime) {
			t.Errorf("%s stamped the entry %v, want the instant the server's clock holds", section.path, got)
		}
	}
}

// A section submitted as nothing is a save that replaces the section with nothing, and it is different
// from a section submitted as something that is not a list at all: the legacy server looks at what was
// submitted under the section's name, does not recognize a value that is not a list, and saves nothing.
func TestSectionSaveOfNothingClearsTheSectionAndASectionThatIsNotAListSavesNothing(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/groups", `[]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("groups save status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.groups.Stored) != 0 {
		t.Errorf("the server runs with %v, want no groups at all: an empty list is a submission that clears the section",
			fixture.groups.Stored)
	}

	rec = fixture.authorized(http.MethodPut, "/api/admin/config/groups", `{"label":"not a list"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("groups save status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.groups.Stored) != 0 {
		t.Errorf("the server runs with %v, want the section the earlier save left it in", fixture.groups.Stored)
	}
}

// A method the endpoint does not answer is refused, and so is a request that arrives as a websocket
// upgrade: the whole-configuration path is where an administrator's socket is opened, and a section path
// that accepted one would answer a socket with a list.
func TestSectionEndpointRefusesOtherMethodsAndAWebsocketUpgrade(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		rec := fixture.authorized(method, "/api/admin/config/groups", "")

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want 405", method, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config/groups", nil)
	req.Header.Set("Authorization", captureToken)
	req.Header.Set("Upgrade", "websocket")

	rec := httptest.NewRecorder()

	fixture.routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("a websocket upgrade asked of a section endpoint status = %d, want 405: %s", rec.Code, rec.Body.String())
	}
}

// Every section an administrator reads, byte for byte, from the same captures the whole-configuration
// comparison uses. The fixture is seeded through the parsers a save goes through, so each comparison is a
// round trip through this server: what the legacy server stored, read by this server and written by this
// server, is the same sequence of bytes.
func TestSectionEndpointAnswersTheCapturedSections(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	captures := []struct {
		section string
		golden  string
	}{
		{section: "access", golden: "rest-section-access.json"},
		{section: "apikeys", golden: "rest-section-apikeys.json"},
		{section: "dirwatch", golden: "rest-section-dirwatch.json"},
		{section: "downstreams", golden: "rest-section-downstreams.json"},
		{section: "groups", golden: "rest-section-groups.json"},
		{section: "options", golden: "rest-section-options.json"},
		{section: "systems", golden: "rest-section-systems.json"},
		{section: "tags", golden: "rest-section-tags.json"},
	}

	for _, capture := range captures {
		rec := fixture.authorized(http.MethodGet, "/api/admin/config/"+capture.section, "")

		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200: %s", capture.section, rec.Code, rec.Body.String())

			continue
		}

		got := rec.Body.String()

		want := capturedAnswer(t, capture.golden)

		// The capture records every site as belonging to no system, because the legacy server's query for
		// sites never asks for that column. This server reports the system the site is stored under, which
		// is the approved divergence recorded as Q58 in decision-parity-verification.md.
		if capture.section == "systems" {
			want = replaceExactly(t, want, `"systemId":0`, `"systemId":1`, 2)
		}

		if got != want {
			t.Errorf("%s answer =\n  %s\nwant\n  %s", capture.section, got, want)
		}
	}
}
