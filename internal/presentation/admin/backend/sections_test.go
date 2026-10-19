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

package backend

import (
	"encoding/json"
	"errors"
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

// The dashboard is a section of the administrator's page and not of the configuration: it reads the
// server and replaces nothing. So the configuration path answers it exactly as it answers a name it has
// never heard of, and an administrator's page that ever tried to save it is told there is nothing there
// to save rather than being handed an empty list it would then write over the server's own state.
func TestSectionEndpointRefusesTheDashboardBecauseItSavesNothing(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/dashboard", `[]`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a name that is not a configuration section: %s", rec.Code, rec.Body.String())
	}

	rec = fixture.authorized(http.MethodGet, "/api/admin/config/dashboard", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: the dashboard has no configuration list to answer with", rec.Code)
	}

	// Refused before the credential and before the method, exactly as an unknown name is: the
	// dashboard is not a section this server knows, and that is decided before anything else is.
	rec = fixture.serve(adminRequest{method: http.MethodPut, path: "/api/admin/config/dashboard", body: `[]`})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 and not 401: the name is decided before the credential", rec.Code)
	}

	if len(fixture.sessions.Announced) != 0 {
		t.Error("a name that is not a section never began a save, and must not reach the open administrator connections")
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

// The Systems section edits the system row's own fields, and its save stores those fields. The
// sites, talkgroups, and units under a system are saved by the section for that level, which shows
// one system at a time, so the systems-section save leaves them as the store holds them — and the
// answer carries them as the store holds them, which is what the page that saved reads back.
func TestSectionSaveSystemsWritesTheSystemRowAndAnswersTheLevelsAsTheStoreHoldsThem(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	stored, ok := fixture.systems.Stored.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored by the capture")
	}

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/systems",
		`[{"id":1,"label":"Renamed Metro","systemRef":10001,"order":1,"autoPopulate":true,"blacklists":"1514000"}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("systems save status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.systems.Stored) != 1 {
		t.Fatalf("the server runs with %v, want the one system the submitted list names", fixture.systems.Stored)
	}

	system := fixture.systems.Stored[0]

	if system.Label != "Renamed Metro" || system.SystemRef != 10001 || system.Blacklists != domain.Blacklists("1514000") {
		t.Errorf("the system is %#v, want the fields the submitted list names", system)
	}

	if len(system.Talkgroups) != len(stored.Talkgroups) {
		t.Errorf("the system carries %d talkgroups, want the %d the store held: the systems-section save wrote the levels",
			len(system.Talkgroups), len(stored.Talkgroups))
	}

	if len(system.Sites) != len(stored.Sites) {
		t.Errorf("the system carries %d sites, want the %d the store held: the systems-section save wrote the levels",
			len(system.Sites), len(stored.Sites))
	}

	entries := decodedSection(t, rec)

	if len(entries) != 1 {
		t.Fatalf("the answer is %s, want the one system that was saved", rec.Body.String())
	}

	talkgroups, ok := entries[0]["talkgroups"].([]any)
	if !ok || len(talkgroups) != len(stored.Talkgroups) {
		t.Errorf("the answer carries %v, want the talkgroups as the store holds them", entries[0]["talkgroups"])
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
		// is the approved divergence.
		if capture.section == "systems" {
			want = replaceExactly(t, want, `"systemId":0`, `"systemId":1`, 2)
		}

		if got != want {
			t.Errorf("%s answer =\n  %s\nwant\n  %s", capture.section, got, want)
		}
	}
}

// The per-level endpoints save one level of one system: sites, talkgroups or units.
// They answer the same way a section does — 401 for a credential the server does not
// accept, 404 for a system this server does not store, 400 for a body it cannot read,
// 417 for a save that could not be stored — and a successful save is answered with the
// level as re-read, and reaches every open administrator connection.

// compactJSON marshals a decoded value so two answers can be compared as text.
func compactJSON(t *testing.T, body string) string {
	t.Helper()

	var value any

	if err := json.Unmarshal([]byte(body), &value); err != nil {
		t.Fatalf("the answer is not JSON: %v", err)
	}

	out, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("the answer cannot be re-marshalled: %v", err)
	}

	return string(out)
}

func TestSubSectionEndpointShowsTheSitesOfOneSystem(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodGet, "/api/admin/config/systems/1/sites", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	want := `[{"id":1,"label":"Metro East","siteRef":101,"systemId":1},{"id":2,"label":"Metro West","siteRef":102,"systemId":1}]`

	if compactJSON(t, rec.Body.String()) != want {
		t.Errorf("sites answer = %s, want the sites stored under system 1", rec.Body.String())
	}
}

func TestSubSectionEndpointShowsTheTalkgroupsOfOneSystem(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodGet, "/api/admin/config/systems/1/talkgroups", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	want := `[{"frequency":151400000,"groupIds":[7],"id":1,"label":"Ground","name":"Dispatch 1","tagId":3,"talkgroupRef":1514000,"type":"Aggregate"},` +
		`{"groupIds":[3,7],"id":2,"label":"Ground","name":"Dispatch 2","tagId":4,"talkgroupRef":1514100},` +
		`{"groupIds":[1],"id":3,"label":"Air","name":"Air 1","tagId":1,"talkgroupRef":1514200}]`

	if compactJSON(t, rec.Body.String()) != want {
		t.Errorf("talkgroups answer = %s, want the talkgroups stored under system 1", rec.Body.String())
	}
}

func TestSubSectionEndpointShowsTheUnitsOfOneSystem(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodGet, "/api/admin/config/systems/1/units", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	want := `[{"id":1001,"label":"Car 1","unitRef":1001},{"id":1002,"label":"Car 2","unitRef":1002}]`

	if compactJSON(t, rec.Body.String()) != want {
		t.Errorf("units answer = %s, want the units stored under system 1", rec.Body.String())
	}
}

func TestSubSectionEndpointRejectsAnUnknownSystem(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodGet, "/api/admin/config/systems/99/sites", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a system this server does not store: %s", rec.Code, rec.Body.String())
	}
}

func TestSubSectionEndpointRejectsAnUnknownLevel(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/config/systems/1/widgets"})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 and not 401: the level is decided before the credential", rec.Code)
	}
}

func TestSubSectionEndpointRequiresACredentialTheServerAccepts(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/config/systems/1/sites", token: "not-a-token-the-server-issued"})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a credential the server does not accept", rec.Code)
	}
}

func TestSubSectionSaveReplacesTheSitesOfOneSystem(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/systems/1/sites",
		`[{"id":1,"label":"Renamed Site","siteRef":101},{"label":"New Site","siteRef":103}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	want := `[{"id":1,"label":"Renamed Site","order":1,"siteRef":101,"systemId":1},{"id":3,"label":"New Site","order":2,"siteRef":103,"systemId":1}]`

	if compactJSON(t, rec.Body.String()) != want {
		t.Errorf("sites answer = %s, want the submitted rows renumbered 1-based as re-read", rec.Body.String())
	}

	if fixture.systems.SubSectionLevel != "sites" || fixture.systems.SubSectionSystemID != 1 {
		t.Errorf("the store was asked for level %q of system %d, want the sites of system 1",
			fixture.systems.SubSectionLevel, fixture.systems.SubSectionSystemID)
	}

	if len(fixture.sessions.Announced) == 0 {
		t.Error("the save did not reach the open administrator connections")
	}
}

func TestSubSectionSaveRejectsAnUndecodableBody(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/systems/1/sites", `[{"label":`)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a body this endpoint cannot read", rec.Code)
	}
}

func TestSubSectionSaveReportsAnUnknownSystem(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/systems/99/sites", `[{"label":"Nowhere"}]`)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a system this server does not store", rec.Code)
	}

	if len(fixture.sessions.Announced) != 0 {
		t.Error("a save that did not work must not reach the open administrator connections")
	}
}

func TestSubSectionSaveReportsAPersistenceFailure(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	fixture.systems.SaveErr = errors.New("no such table: sites")

	rec := fixture.authorized(http.MethodPut, "/api/admin/config/systems/1/sites", `[{"label":"Metro East"}]`)

	if rec.Code != http.StatusExpectationFailed {
		t.Errorf("status = %d, want 417 for a save that could not be stored", rec.Code)
	}
}

// The talkgroups level takes the bare array of rows it has always taken, and the object that
// carries its rows beside the blacklist the section edits with them. Both are answered with the
// level as the store holds it, and the object's blacklist is stored by the same save.
func TestSubSectionSaveOfTalkgroupsAnswersTheLevelForBothBodies(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "the bare array of rows", body: `[{"id":2,"label":"Ground","name":"Dispatch 2","tagId":4,"talkgroupRef":1514100,"groupIds":[3,7]}]`},
		{name: "the object carrying the blacklist", body: `{"talkgroups":[{"id":2,"label":"Ground","name":"Dispatch 2","tagId":4,"talkgroupRef":1514100,"groupIds":[3,7]}],"blacklists":"1514000"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAdminFixture()

			seedCaptures(t, fixture)

			rec := fixture.authorized(http.MethodPut, "/api/admin/config/systems/1/talkgroups", tc.body)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 for the talkgroups of a stored system: %s", rec.Code, rec.Body.String())
			}

			answered := fixture.authorized(http.MethodGet, "/api/admin/config/systems/1/talkgroups", "")

			if compactJSON(t, rec.Body.String()) != compactJSON(t, answered.Body.String()) {
				t.Errorf("the save answered %s, want the level as the store holds it: %s",
					rec.Body.String(), answered.Body.String())
			}

			if len(fixture.systems.Stored) == 0 {
				t.Fatalf("no system is stored")
			}

			if len(fixture.systems.Stored[0].Talkgroups) != 1 {
				t.Errorf("the system carries %v, want the one talkgroup the submitted list names",
					fixture.systems.Stored[0].Talkgroups)
			}

			want := domain.Blacklists("")

			if strings.Contains(tc.body, "blacklists") {
				want = domain.Blacklists("1514000")
			}

			if fixture.systems.Stored[0].Blacklists != want {
				t.Errorf("the system stores blacklist %q, want %q: the save that names a blacklist stores it with the rows",
					fixture.systems.Stored[0].Blacklists, want)
			}
		})
	}
}

// A body the level cannot read is refused before the save starts. The object body belongs to the
// talkgroups level alone: the sites and units levels take the list of their rows, and a body that
// is neither a list nor the shape this level takes names nothing this endpoint can store.
func TestSubSectionSaveRejectsABodyItCannotReadForTheShapeItIs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level string
		body  string
	}{
		{name: "an object at the talkgroups level with no rows", level: "talkgroups", body: `{"blacklists":"1514000"}`},
		{name: "a string at the talkgroups level", level: "talkgroups", body: `"1514000"`},
		{name: "rows that are not a list", level: "talkgroups", body: `{"talkgroups":"1514000"}`},
		{name: "the object body at the sites level", level: "sites", body: `{"talkgroups":[],"blacklists":"1514000"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAdminFixture()

			seedCaptures(t, fixture)

			rec := fixture.authorized(http.MethodPut, "/api/admin/config/systems/1/"+tc.level, tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d for body %s, want 400 for a body this endpoint cannot read", rec.Code, tc.body)
			}

			if len(fixture.sessions.Announced) != 0 {
				t.Error("a body that was refused never began a save, and must not reach the open administrator connections")
			}
		})
	}
}

func TestSubSectionEndpointRefusesAMethodItDoesNotAnswer(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodDelete, "/api/admin/config/systems/1/sites", "")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 for a method this endpoint does not answer", rec.Code)
	}

	rec = fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/config/systems/1/sites", token: captureToken, Upgrade: true})

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 for a request asking to become a websocket connection", rec.Code)
	}
}
