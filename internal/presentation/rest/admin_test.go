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

package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

// The administrator endpoints are tested against the captures taken from the legacy server, so that a
// status code or a body is compared with what the legacy server answered and not with what this server's
// author remembers it answering.

var captureTime = time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)

const (
	captureAddress    = "203.0.113.7"
	captureCredential = "capture-administrator-password"
	captureToken      = "capture-admin-token"

	// captureVersion is the version the server that produced the captures reported, and it is part of
	// what those captures say.
	captureVersion = "8.0.0"
)

// adminFixture is a server whose configuration, log, sessions, and watchers are the in-memory stand-ins
// the application tests use, reached through the real endpoints.
type adminFixture struct {
	accesses    *fakes.Accesses
	apikeys     *fakes.Apikeys
	dirwatches  *fakes.Dirwatches
	downstreams *fakes.Downstreams
	groups      *fakes.Groups
	options     *fakes.Options
	systems     *fakes.Systems
	tags        *fakes.Tags

	logs     *fakes.Logs
	hub      *fakes.Hub
	sessions *fakes.AdministratorSessions
	watches  *fakes.Dirwatchers
	tokens   *fakes.Tokens
	password *fakes.PasswordVerifier
	hashes   *fakes.PasswordHasher
	clock    *fakes.Clock

	// Docker is whether the server thinks it is running in a container, which is the one thing the
	// configuration answer reports that is not configuration.
	Docker bool

	// configSocket is what the configuration path hands a request that asks to become a websocket
	// connection to. Nothing in this suite is a socket, so it is left unset except by the tests that ask
	// about the handover.
	configSocket http.Handler

	socketRequests []*http.Request

	routes http.Handler
}

func newAdminFixture() *adminFixture {
	fixture := &adminFixture{
		accesses:    &fakes.Accesses{},
		apikeys:     &fakes.Apikeys{},
		dirwatches:  &fakes.Dirwatches{},
		downstreams: &fakes.Downstreams{},
		groups:      &fakes.Groups{},
		options:     &fakes.Options{Stored: domain.DefaultOptions()},
		systems:     &fakes.Systems{},
		tags:        &fakes.Tags{},

		logs:     &fakes.Logs{},
		hub:      fakes.NewHub(),
		sessions: fakes.NewAdministratorSessions(),
		watches:  fakes.NewDirwatchers(),
		tokens:   fakes.NewTokens(),
		password: fakes.NewPasswordVerifier(),
		hashes:   fakes.NewPasswordHasher(),
		clock:    fakes.NewClock(captureTime),
	}

	// The capture's administrator is already logged in: the captures of the configuration and the log
	// view were taken with a session open, and the login itself is captured separately.
	fixture.tokens.Window = []string{captureToken}

	fixture.routes = fixture.buildRoutes()

	return fixture
}

// buildRoutes is the routing the composition root installs, and it is part of the frozen contract: which
// path answers which way, including the whole-configuration path and the section subtree hanging off it.
func (fixture *adminFixture) buildRoutes() http.Handler {
	mux := http.NewServeMux()

	NewAdmin(Deps{
		AddUser: application.NewAddAccessUserHandler(application.AccessUserPorts{
			ConfigurationRepositories: fixture.repositories(),
			Logs:                      fixture.logs,
			Admin:                     fixture.sessions,
			Clock:                     fixture.clock,
		}),
		Configuration:        application.NewUpdateConfigurationHandler(fixture.configurationPorts()),
		ConfigurationSection: application.NewUpdateConfigurationSectionHandler(fixture.configurationPorts()),
		Config:               application.NewGetConfigHandler(fixture.repositories()),
		ConfigSocket:         fixture.configSocket,
		Docker:               fixture.Docker,
		Logout:               application.NewAdminLogoutHandler(fixture.tokens, fixture.logs, fixture.clock),
		Logs:                 application.NewSearchLogsHandler(fixture.logs, fixture.clock),
		Login:                application.NewAdminLoginHandler(fixture.options, fixture.logs, fixture.tokens, fixture.password, fixture.clock),
		Password:             application.NewChangeAdminPasswordHandler(fixture.options, fixture.password, fixture.hashes, fixture.logs, fixture.clock),
		RemoveUser:           application.NewRemoveAccessUserHandler(application.AccessUserPorts{ConfigurationRepositories: fixture.repositories(), Logs: fixture.logs, Admin: fixture.sessions, Clock: fixture.clock}),
		Session:              application.NewCheckAdminSessionHandler(fixture.tokens),
		Version:              captureVersion,
	}).Register(mux)

	return mux
}

func (fixture *adminFixture) repositories() application.ConfigurationRepositories {
	return application.ConfigurationRepositories{
		Accesses:    fixture.accesses,
		Apikeys:     fixture.apikeys,
		Dirwatches:  fixture.dirwatches,
		Downstreams: fixture.downstreams,
		Groups:      fixture.groups,
		Options:     fixture.options,
		Systems:     fixture.systems,
		Tags:        fixture.tags,
	}
}

func (fixture *adminFixture) configurationPorts() application.ConfigurationPorts {
	return application.ConfigurationPorts{
		ConfigurationRepositories: fixture.repositories(),
		Logs:                      fixture.logs,
		Hub:                       fixture.hub,
		Admin:                     fixture.sessions,
		Dirwatchers:               fixture.watches,
		Clock:                     fixture.clock,
	}
}

// adminRequest is one request to one administrator endpoint. The token is left out of the struct when the
// test is about a request that does not carry one, because several of these endpoints answer differently
// depending on whether they looked at the credential before or after something else.
type adminRequest struct {
	body   string
	method string
	path   string
	token  string

	// Forwarded is the forwarding header, when the test is about which machine the server thinks a
	// request came from.
	Forwarded string

	// Upgrade is whether the request asks to become a websocket connection.
	Upgrade bool
}

func (fixture *adminFixture) serve(request adminRequest) *httptest.ResponseRecorder {
	req := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))

	if request.token != "" {
		req.Header.Set("Authorization", request.token)
	}

	if request.Forwarded != "" {
		req.Header.Set("X-Forwarded-For", request.Forwarded)
	}

	if request.Upgrade {
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
	}

	rec := httptest.NewRecorder()

	fixture.routes.ServeHTTP(rec, req)

	return rec
}

// socketHandoverMarker is what the stand-in socket answers with, so a test can tell a request the endpoint
// answered from a request the socket was given.
const socketHandoverMarker = "handed to the configuration socket"

// recordingSocket is the configuration socket as these tests see it: it records what it was handed and
// answers with a line the endpoint half would never write.
func recordingSocket(fixture *adminFixture) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.socketRequests = append(fixture.socketRequests, r)

		w.Write([]byte(socketHandoverMarker))
	})
}

// authorized is a request from the logged-in administrator at the address the capture used.
func (fixture *adminFixture) authorized(method, path, body string) *httptest.ResponseRecorder {
	return fixture.serve(adminRequest{
		body:   body,
		method: method,
		path:   path,
		token:  captureToken,
	})
}

// decodedBody reads what the server answered as the JSON it is, so that a test can look at one field
// without depending on the order the fields are written in.
func decodedBody(t *testing.T, rec *httptest.ResponseRecorder) any {
	t.Helper()

	var body any

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the answer is not the JSON the endpoint is supposed to answer with: %v\n%s", err, rec.Body.String())
	}

	return body
}

// decodedSection reads an answer that is a list of entries.
func decodedSection(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()

	body := decodedBody(t, rec)

	entries, ok := body.([]any)
	if !ok {
		t.Fatalf("the answer is not a list of entries: %s", rec.Body.String())
	}

	out := []map[string]any{}

	for _, entry := range entries {
		object, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("the answer holds an entry that is not an object: %s", rec.Body.String())
		}

		out = append(out, object)
	}

	return out
}

// decodedLogEntries reads the entries out of a log view answer, which is an envelope and not a list: the
// entries travel inside it with the total, the span and the criteria that were echoed back.
func decodedLogEntries(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()

	body, ok := decodedBody(t, rec).(map[string]any)
	if !ok {
		t.Fatalf("the log view answer is not a set of answers: %s", rec.Body.String())
	}

	list, ok := body["logs"].([]any)
	if !ok {
		t.Fatalf("the log view answer carries no entries: %s", rec.Body.String())
	}

	out := []map[string]any{}

	for _, entry := range list {
		object, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("the log view holds an entry that is not an entry: %s", rec.Body.String())
		}

		out = append(out, object)
	}

	return out
}

// loggedContaining reports whether the server wrote a log entry mentioning the given text, which is how
// these tests pin the wording the legacy server's log view has always shown.
func (fixture *adminFixture) loggedContaining(fragment string) bool {
	return fixture.countLogged(fragment) > 0
}

// countLogged reports how many log entries mention the given text, which is how these tests tell an
// attempt that was examined from an address that was refused before its credential was looked at.
func (fixture *adminFixture) countLogged(fragment string) int {
	count := 0

	for _, entry := range fixture.logs.Stored {
		if strings.Contains(entry.Message, fragment) {
			count++
		}
	}

	return count
}

// seedCaptures installs the configuration the capture left in the legacy server, read back through the
// same parsers an administrator's save goes through.
//
// Seeding this way makes the byte-for-byte comparisons below a round trip: what the legacy server stored,
// read by this server and written by this server, is the same sequence of bytes.
func seedCaptures(t *testing.T, fixture *adminFixture) {
	t.Helper()

	fixture.accesses.Stored = domain.ParseAccesses(decodeGolden(t, "rest-section-access.json"))
	fixture.apikeys.Stored = domain.ParseApikeys(decodeGolden(t, "rest-section-apikeys.json"))
	fixture.dirwatches.Stored = domain.ParseDirwatches(decodeGolden(t, "rest-section-dirwatch.json"))
	fixture.downstreams.Stored = domain.ParseDownstreams(decodeGolden(t, "rest-section-downstreams.json"))
	fixture.groups.Stored = domain.ParseGroups(decodeGolden(t, "rest-section-groups.json"))
	fixture.tags.Stored = domain.ParseTags(decodeGolden(t, "rest-section-tags.json"))

	submitted, ok := decodeGolden(t, "rest-section-options.json").(map[string]any)
	if !ok {
		t.Fatalf("the captured options section is not a set of settings")
	}

	options := domain.DefaultOptions().ApplyMap(submitted)
	options.AdminPasswordNeedChange = true

	fixture.options.Stored = options

	systems := domain.ParseSystems(decodeGolden(t, "rest-section-systems.json"))

	// The capture records every site as belonging to no system at all, because the legacy server's query
	// for sites never asks for the system a site is on. This server reports the system it is stored under,
	// which is the approved divergence recorded as Q58 in decision-parity-verification.md, and it is what
	// a read of the stored database produces. The comparison below states the divergence instead of
	// skipping the capture.
	for i := range systems {
		for j := range systems[i].Sites {
			systems[i].Sites[j].SystemID = systems[i].ID
		}
	}

	fixture.systems.Stored = systems
}

func decodeGolden(t *testing.T, name string) any {
	t.Helper()

	var value any

	if err := json.Unmarshal([]byte(readGolden(t, name)), &value); err != nil {
		t.Fatalf("the capture %s is not JSON: %v", name, err)
	}

	return value
}

func readGolden(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("../../../testdata/golden", name))
	if err != nil {
		t.Fatalf("reading the golden capture %s: %v", name, err)
	}

	return string(b)
}

// replaceExactly rewrites a captured payload to state a divergence, and insists that the divergence is
// still the one the capture recorded: a capture that no longer says what this server disagrees with is a
// capture that has changed, and the test would rather stop than quietly agree with something new.
func replaceExactly(t *testing.T, body, from, to string, times int) string {
	t.Helper()

	if n := strings.Count(body, from); n != times {
		t.Fatalf("the captured payload says %q %d times, want %d: the divergence this test states is no "+
			"longer the one the capture recorded", from, n, times)
	}

	return strings.ReplaceAll(body, from, to)
}

// login

// A login is answered with the credential that stands for the new session and the flag the administrator's
// page acts on. The captured credential is a placeholder, because a session credential is different on
// every run of the capture.
func TestLoginEndpointAnswersTheCapturedLogin(t *testing.T) {
	fixture := newAdminFixture()

	fixture.options.Stored.AdminPasswordHash = captureCredential

	rec := fixture.serve(adminRequest{
		body:   `{"password":"` + captureCredential + `"}`,
		method: http.MethodPost,
		path:   "/api/admin/login",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := replaceExactly(t, readGolden(t, "rest-login.json"), "<token>", "token-1", 1)

	if got != want {
		t.Errorf("login answer =\n  %s\nwant\n  %s", got, want)
	}
}

// A wrong credential is refused with nothing said back, and the only trace of it is a warning in the log
// view naming the machine that tried.
func TestLoginEndpointRefusesTheWrongCredentialWithTheCapturedStatus(t *testing.T) {
	fixture := newAdminFixture()

	fixture.options.Stored.AdminPasswordHash = captureCredential

	rec := fixture.serve(adminRequest{
		body:      `{"password":"wrong credential"}`,
		method:    http.MethodPost,
		path:      "/api/admin/login",
		Forwarded: captureAddress + ":8123",
	})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.Len() != 0 {
		t.Errorf("a refused login answers %q, want nothing", rec.Body.String())
	}

	if !fixture.loggedContaining("a login attempt from " + captureAddress + " was refused") {
		t.Errorf("the refused attempt is not in the log view naming the address it came from: %v", fixture.logs.Stored)
	}
}

// An address gets three login attempts and the fourth is refused before its credential is looked at, so
// that a locked-out machine is not told whether its credential would have worked. The address is the one
// in the forwarding header, because a server behind a proxy would otherwise lock out the proxy for
// everybody behind it.
func TestLoginEndpointRefusesTheFourthAttemptFromOneAddressWhateverItCarries(t *testing.T) {
	fixture := newAdminFixture()

	fixture.options.Stored.AdminPasswordHash = captureCredential

	for attempt := 1; attempt <= 3; attempt++ {
		rec := fixture.serve(adminRequest{
			body:      `{"password":"wrong credential"}`,
			method:    http.MethodPost,
			path:      "/api/admin/login",
			Forwarded: captureAddress + ":8123",
		})

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt, rec.Code)
		}
	}

	rec := fixture.serve(adminRequest{
		body:      `{"password":"` + captureCredential + `"}`,
		method:    http.MethodPost,
		path:      "/api/admin/login",
		Forwarded: captureAddress + ":8123",
	})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("the fourth attempt status = %d, want 401 even though the credential is the right one", rec.Code)
	}

	if !fixture.loggedContaining("too many login attempts from " + captureAddress) {
		t.Errorf("the lockout is not in the log view naming the address it is about: %v", fixture.logs.Stored)
	}
}

// A successful login clears what the address has spent. Without the clearing, an address that has ever
// mistyped its credential would run out of attempts for as long as it keeps connecting.
func TestLoginEndpointClearsTheAttemptsAfterASuccessfulLogin(t *testing.T) {
	fixture := newAdminFixture()

	fixture.options.Stored.AdminPasswordHash = captureCredential

	// Two attempts spent, and the third one carries the right credential.
	for attempt := 1; attempt <= 2; attempt++ {
		fixture.serve(adminRequest{body: `{"password":"wrong credential"}`, method: http.MethodPost, path: "/api/admin/login"})
	}

	rec := fixture.serve(adminRequest{body: `{"password":"` + captureCredential + `"}`, method: http.MethodPost, path: "/api/admin/login"})

	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// The address has a full budget again, so three more refused attempts are still answered on their
	// merits. A refused attempt is written in the log view as a refused credential; an address that has
	// spent its budget is refused before its credential is looked at and writes no such line.
	for attempt := 1; attempt <= 3; attempt++ {
		fixture.serve(adminRequest{body: `{"password":"wrong credential"}`, method: http.MethodPost, path: "/api/admin/login"})
	}

	if got := fixture.countLogged("a login attempt from "); got != 5 {
		t.Errorf("the log view holds %d refused credentials, want the five that were made: an address whose budget "+
			"was cleared is still being asked what its credential is", got)
	}
}

// logout

// A logout is answered with the captured status when the credential is not one the server accepts, which
// is what an administrator's page sees when its session has already gone away.
func TestLogoutEndpointRefusesATokenTheServerDoesNotAcceptWithTheCapturedStatus(t *testing.T) {
	fixture := newAdminFixture()

	rec := fixture.serve(adminRequest{method: http.MethodPost, path: "/api/admin/logout", token: "not-a-token-the-server-issued"})

	want := strings.TrimSpace(readGolden(t, "rest-logout-status.txt"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("logout status = %d, want the captured %s", rec.Code, want)
	}
}

// A logout ends the session its credential stands for, and the credential stops working.
func TestLogoutEndpointEndsTheSessionItsCredentialStandsFor(t *testing.T) {
	fixture := newAdminFixture()

	rec := fixture.authorized(http.MethodPost, "/api/admin/logout", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	rec = fixture.authorized(http.MethodGet, "/api/admin/config", "")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("the configuration after the logout answers %d, want 401: the session is still open", rec.Code)
	}
}

// configuration

// The whole configuration an administrator is shown, byte for byte.
//
// The capture says something this server does not: every site is recorded as belonging to no system,
// because the legacy server's query for sites never asks for that column. This server reports the system
// the site is stored under, which is the approved divergence recorded as Q58, and the test states it by
// rewriting the capture rather than by loosening the comparison.
func TestConfigEndpointAnswersTheCapturedConfiguration(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodGet, "/api/admin/config", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("config status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := replaceExactly(t, readGolden(t, "rest-config.json"), `"systemId":0`, `"systemId":1`, 2)

	if got != want {
		t.Errorf("configuration answer =\n  %s\nwant\n  %s", got, want)
	}
}

// A request with no credential is answered with the captured status and nothing else: no configuration,
// no explanation.
func TestConfigEndpointRefusesARequestWithoutACredentialWithTheCapturedStatus(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/config"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("config status = %d, want the captured %s", rec.Code, strings.TrimSpace(readGolden(t, "rest-config-unauthorized-status.txt")))
	}

	if rec.Body.Len() != 0 {
		t.Errorf("an unauthorized configuration request answers %q, want nothing", rec.Body.String())
	}
}

// The configuration path is both an endpoint and a socket, and the upgrade header is what decides which.
//
// The socket is handed the request before any credential is asked for, which is the legacy server's order:
// its one handler looks at the upgrade header first, and only the endpoint half of it looks at a token. A
// configuration socket authenticates by the frames it sends, and the credential a request could carry in a
// header is not what the socket is checked against.
func TestConfigEndpointHandsAnUpgradeRequestToTheSocketWithoutAskingForACredential(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	fixture.configSocket = recordingSocket(fixture)

	fixture.routes = fixture.buildRoutes()

	rec := fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/config", Upgrade: true})

	if len(fixture.socketRequests) != 1 {
		t.Fatalf("the configuration socket was handed %d requests, want the one that asked to become a socket",
			len(fixture.socketRequests))
	}

	if got, want := rec.Body.String(), socketHandoverMarker; got != want {
		t.Errorf("the configuration path answered %q, want what the socket answers: the endpoint half is not "+
			"supposed to answer at all", got)
	}

	// The ordinary half of the same path is untouched. The same path without the upgrade header is answered
	// as an endpoint, and the socket is not involved.
	plain := fixture.authorized(http.MethodGet, "/api/admin/config", "")

	if len(fixture.socketRequests) != 1 {
		t.Errorf("an ordinary configuration request was handed to the socket as well")
	}

	if plain.Code != http.StatusOK {
		t.Errorf("the ordinary configuration request was answered %d, want it answered as the endpoint it is",
			plain.Code)
	}
}

// An upgrade request that carries a credential is still a socket: the credential in a header is what the
// endpoint half is checked against, and the socket is not the thing that reads it.
func TestConfigEndpointHandsAnUpgradeRequestToTheSocketEvenWhenItCarriesACredential(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	fixture.configSocket = recordingSocket(fixture)

	fixture.routes = fixture.buildRoutes()

	fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/config", token: captureToken, Upgrade: true})

	if len(fixture.socketRequests) != 1 {
		t.Fatalf("the configuration socket was handed %d requests, want the one that asked to become a socket",
			len(fixture.socketRequests))
	}
}

// The container flag is the one thing the configuration answer reports that is not configuration, and it
// is only there when the server thinks it is in a container.
func TestConfigEndpointCarriesTheContainerFlagOnlyWhenTheServerThinksItIsInOne(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodGet, "/api/admin/config", "")

	if strings.Contains(rec.Body.String(), `"docker"`) {
		t.Errorf("the configuration answer carries a docker key when the server does not think it is in a container: %s", rec.Body.String())
	}

	fixture.Docker = true
	fixture.routes = fixture.buildRoutes()

	rec = fixture.authorized(http.MethodGet, "/api/admin/config", "")

	if !strings.Contains(rec.Body.String(), `"docker":true`) {
		t.Errorf("the configuration answer =\n  %s\nwant it to carry \"docker\":true", rec.Body.String())
	}
}

// A whole-configuration save replaces the sections it names and leaves the sections it does not. The
// answer is the whole configuration, because the administrator's page reloads it.
func TestConfigEndpointReplacesOnlyTheSectionsTheSubmittedConfigurationNames(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	before := len(fixture.tags.Stored)

	rec := fixture.authorized(http.MethodPut, "/api/admin/config", `{"groups":[{"id":1,"label":"Renamed Group"}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("config save status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.groups.Stored) != 1 || fixture.groups.Stored[0].Label != "Renamed Group" {
		t.Errorf("the groups the server runs with are %v, want the one the submitted configuration names", fixture.groups.Stored)
	}

	if len(fixture.tags.Stored) != before {
		t.Errorf("the tags the server runs with changed to %v, want the %d it had: the save did not name them", fixture.tags.Stored, before)
	}

	body := decodedBody(t, rec)

	answer, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("the answer is not the configuration answer object: %s", rec.Body.String())
	}

	configuration, ok := answer["config"].(map[string]any)
	if !ok {
		t.Fatalf("the answer does not carry the configuration it is showing: %s", rec.Body.String())
	}

	if _, ok := configuration["systems"]; !ok {
		t.Errorf("the answer is not the whole configuration: %s", rec.Body.String())
	}

	if answer["passwordNeedChange"] != true {
		t.Errorf("the answer reports passwordNeedChange as %v, want the flag the options carry", answer["passwordNeedChange"])
	}

	if !fixture.loggedContaining("configuration changed") {
		t.Errorf("the save is not in the log view as the legacy server wrote it: %v", fixture.logs.Stored)
	}
}

// The settings are merged into the settings in effect and not replaced, because the legacy server applies
// the submitted settings onto the ones it is running with, and an administrator's page submits only the
// settings that page shows.
func TestConfigEndpointMergesTheSettingsTheSubmittedConfigurationNames(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	rec := fixture.authorized(http.MethodPut, "/api/admin/config", `{"options":{"branding":"Renamed Dispatch"}}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("config save status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if fixture.options.Stored.Branding != "Renamed Dispatch" {
		t.Errorf("the branding in effect is %q, want the one the submitted configuration names", fixture.options.Stored.Branding)
	}

	if fixture.options.Stored.MaxClients != 200 {
		t.Errorf("the maximum number of listeners in effect is %d, want the 200 the server had: a setting that was "+
			"not submitted must survive a save that names other settings", fixture.options.Stored.MaxClients)
	}
}

// A whole-configuration save with a body the endpoint cannot read is answered with 400, and the watched
// directories are left alone: the legacy server reads the body before it stops anything, which is the
// opposite of what its per-section save does.
func TestConfigEndpointRejectsAWholeConfigurationItCannotReadWithoutTouchingTheWatchers(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	before := len(fixture.groups.Stored)

	rec := fixture.authorized(http.MethodPut, "/api/admin/config", `{"groups": [`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("config save status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	if fixture.watches.Stops != 0 {
		t.Errorf("the watched directories were stopped %d times, want them left running: the legacy server reads "+
			"the body before it stops anything", fixture.watches.Stops)
	}

	if len(fixture.groups.Stored) != before {
		t.Errorf("the groups the server runs with are %v, want the %d the capture left: a body that could not be "+
			"read saves nothing", fixture.groups.Stored, before)
	}
}

func TestConfigEndpointRefusesMethodsItDoesNotAnswer(t *testing.T) {
	fixture := newAdminFixture()

	seedCaptures(t, fixture)

	for _, method := range []string{http.MethodDelete, http.MethodPost} {
		rec := fixture.authorized(method, "/api/admin/config", "")

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /api/admin/config status = %d, want 405", method, rec.Code)
		}
	}
}

// alerts

// The nine alert sequences, byte for byte. They are not configuration, and the administrator's endpoint is
// where a client that shows alerts fetches them.
func TestAlertsEndpointAnswersTheCapturedAlerts(t *testing.T) {
	fixture := newAdminFixture()

	rec := fixture.authorized(http.MethodGet, "/api/admin/alerts", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("alerts status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := readGolden(t, "rest-alerts.json")

	if got != want {
		t.Errorf("alerts answer =\n  %s\nwant\n  %s", got, want)
	}
}

// The alerts endpoint asks for a session inside the method it answers, so a method it does not answer is
// refused without ever looking at the credential: the order is the legacy server's and it is what a client
// sees when it asks the wrong question of a server it is not logged in to.
func TestAlertsEndpointRefusesAMethodItDoesNotAnswerBeforeAskingForASession(t *testing.T) {
	fixture := newAdminFixture()

	rec := fixture.serve(adminRequest{method: http.MethodPost, path: "/api/admin/alerts"})

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("alerts status = %d, want 405 and not 401: the method is decided first", rec.Code)
	}

	rec = fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/alerts"})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("alerts status = %d, want 401 for a method the endpoint does answer", rec.Code)
	}
}

// log view

// The log view, byte for byte. The capture's timestamps are placeholders, and the test fills them back in
// with the instants its own log holds, which is what the capture's clock reported.
//
// The capture is the shape this server's log search contract declares, over the scenario the capture script
// ran: a deployment whose log holds the startup line the legacy server wrote — which is why it reads back
// as `legacy` — and the two configuration changes made afterwards.
func TestLogsEndpointAnswersTheCapturedLogView(t *testing.T) {
	fixture := newAdminFixture()

	fixture.logs.Stored = []domain.LogEntry{
		{ID: 1, DateTime: captureTime, Level: domain.LogLevelWarn, Message: "server started"},
	}

	fixture.authorized(http.MethodPut, "/api/admin/config/groups", `[{"id":1,"label":"Renamed Group"}]`)
	fixture.authorized(http.MethodPut, "/api/admin/config/options", `{"branding":"Renamed Dispatch"}`)

	rec := fixture.authorized(http.MethodPost, "/api/admin/logs", `{"level":"warn","limit":200,"offset":0,"sort":0}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("logs status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := replaceExactly(t, readGolden(t, "rest-logs.json"), "<datetime>", "2026-05-11T09:00:00Z", 5)

	if got != want {
		t.Errorf("log view answer =\n  %s\nwant\n  %s", got, want)
	}
}

// The log view asks for a session before it looks at the method, which is the opposite of the alerts
// endpoint. A page that asks for the log view with a GET and an expired session is told the session is
// gone, and not that the method is wrong.
func TestLogsEndpointAsksForASessionBeforeItLooksAtTheMethod(t *testing.T) {
	fixture := newAdminFixture()

	rec := fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/logs"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("logs status = %d, want 401 and not 405: the session is decided first", rec.Code)
	}

	rec = fixture.authorized(http.MethodGet, "/api/admin/logs", "")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("logs status = %d, want 405 for a method the endpoint does not answer", rec.Code)
	}
}

// The criteria are echoed back as they were submitted, including a limit larger than the most the search
// will ever return: the log view pages by resubmitting what it was last answered with.
func TestLogsEndpointEchoesTheCriteriaTheSearchWasAskedWith(t *testing.T) {
	fixture := newAdminFixture()

	rec := fixture.authorized(http.MethodPost, "/api/admin/logs", `{"limit":600}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("logs status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	body := decodedBody(t, rec)

	options, ok := body.(map[string]any)["options"]
	if !ok {
		t.Fatalf("the answer does not echo the criteria: %s", rec.Body.String())
	}

	echoed, ok := options.(map[string]any)
	if !ok {
		t.Fatalf("the echoed criteria are not a set of criteria: %s", rec.Body.String())
	}

	if echoed["limit"] != float64(600) {
		t.Errorf("the echoed limit is %v, want the 600 that was submitted and not the 500 that was used", echoed["limit"])
	}

	if _, ok := echoed["level"]; ok {
		t.Errorf("the echoed criteria carry a level the request did not name: %v", echoed)
	}
}

// A log view that cannot be read is answered with the failure status the frozen contract fixes, and the
// failure itself is written into the log view so that an administrator can tell a broken log apart from an
// empty one.
func TestLogsEndpointReportsAFailureToReadTheLogView(t *testing.T) {
	fixture := newAdminFixture()

	fixture.logs.SearchErr = errLogStoreUnreadable

	rec := fixture.authorized(http.MethodPost, "/api/admin/logs", `{}`)

	if rec.Code != http.StatusExpectationFailed {
		t.Fatalf("logs status = %d, want 417: %s", rec.Code, rec.Body.String())
	}

	if !fixture.loggedContaining("logs.search:") {
		t.Errorf("the failure to read the log view is not in the log view: %v", fixture.logs.Stored)
	}
}

// A subsystem this server does not have is answered as a bad request, and not as a log view that happens
// to be empty. An operator who types `dirwatches` and gets an empty page has been told that nothing
// happened, which is a different and much worse thing than being told they asked for something the server
// does not have.
func TestLogsEndpointRefusesASubsystemThisServerDoesNotHave(t *testing.T) {
	fixture := newAdminFixture()

	fixture.logs.Stored = []domain.LogEntry{
		{ID: 1, DateTime: captureTime, Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming stopped: no such file or directory"},
	}

	rec := fixture.authorized(http.MethodPost, "/api/admin/logs", `{"source":"dirwatches"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("logs status = %d, want 400 for a subsystem that is not a part of this server: %s",
			rec.Code, rec.Body.String())
	}

	// Refused outright: not a page, not an empty page.
	if body := rec.Body.String(); body != "" {
		t.Errorf("the answer to a refused subsystem is %q, want nothing: the caller asked a question this "+
			"server cannot answer, and a body would be an answer to some other question", body)
	}

	// And the search did not run with a criterion it could not honour, which is what an empty page would
	// have meant: the one entry the log holds is a dirwatch entry, and a search that ignored the submitted
	// name would have returned it.
	rec = fixture.authorized(http.MethodPost, "/api/admin/logs", `{"source":"dirwatch"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("logs status = %d, want 200 for a subsystem this server does have: %s", rec.Code, rec.Body.String())
	}

	if got := decodedLogEntries(t, rec); len(got) != 1 {
		t.Errorf("a search narrowed to %q holds %d entries, want the one entry the log holds", "dirwatch", len(got))
	}
}

// The subsystem of each entry, and the counts that let the operator decide what to narrow to, are answered
// by the endpoint and not only by the payload: the log view reads them out of the answer.
func TestLogsEndpointAnswersTheSubsystemOfEachEntryAndHowManyEachWrote(t *testing.T) {
	fixture := newAdminFixture()

	fixture.logs.Stored = []domain.LogEntry{
		{ID: 1, DateTime: captureTime, Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming stopped: no such file or directory"},
		{ID: 2, DateTime: captureTime.Add(time.Second), Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/outgoing stopped: no such file or directory"},
		{ID: 3, DateTime: captureTime.Add(2 * time.Second), Level: domain.LogLevelWarn, Source: domain.LogSourceLogin, Message: "a login attempt from 203.0.113.7 was refused"},
	}

	rec := fixture.authorized(http.MethodPost, "/api/admin/logs", `{"level":"warn"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("logs status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	body, ok := decodedBody(t, rec).(map[string]any)

	if !ok {
		t.Fatalf("the log view answer is not a set of answers: %s", rec.Body.String())
	}

	for _, entry := range decodedLogEntries(t, rec) {
		if entry["source"] == nil || entry["source"] == "" {
			t.Errorf("the entry %v carries no subsystem, and an entry that does not say who wrote it cannot be "+
				"narrowed to one", entry)
		}
	}

	counts, ok := body["sources"].([]any)

	if !ok {
		t.Fatalf("the answer carries no per-subsystem counts: %s", rec.Body.String())
	}

	if len(counts) != 2 {
		t.Fatalf("the answer reports %d subsystems, want the two that wrote these entries: %v", len(counts), counts)
	}

	first, ok := counts[0].(map[string]any)

	if !ok {
		t.Fatalf("a per-subsystem count is not a count: %v", counts[0])
	}

	// Most first, so the log view can put the noisiest part of the server where the operator will see it.
	if first["source"] != "dirwatch" || first["count"] != float64(2) {
		t.Errorf("the first per-subsystem count is %v, want dirwatch with 2: the counts are offered most first", first)
	}

	// The counts describe the match the criteria make, and the subsystem filter is not one of them: an
	// operator who has already narrowed to dirwatch is being offered the rest of the log to narrow to next.
	rec = fixture.authorized(http.MethodPost, "/api/admin/logs", `{"level":"warn","source":"login"}`)

	body, ok = decodedBody(t, rec).(map[string]any)

	if !ok {
		t.Fatalf("the log view answer is not a set of answers: %s", rec.Body.String())
	}

	counts, ok = body["sources"].([]any)

	if !ok {
		t.Fatalf("the answer carries no per-subsystem counts: %s", rec.Body.String())
	}

	if len(counts) != 2 {
		t.Errorf("a search narrowed to login reports %d subsystems, want the two the level matches: the counts "+
			"are what the operator narrows next with, and narrowing them to the one already chosen answers one "+
			"row that is true and useless: %v", len(counts), counts)
	}

	if got := body["count"]; got != float64(1) {
		t.Errorf("the total of a search narrowed to login is %v, want 1: the narrowed match is what the page "+
			"pages through", got)
	}
}

// password

// A changed password is answered with the flag the administrator's page acts on, byte for byte.
func TestPasswordEndpointAnswersTheCapturedPasswordChange(t *testing.T) {
	fixture := newAdminFixture()

	fixture.options.Stored.AdminPasswordHash = captureCredential
	fixture.options.Stored.AdminPasswordNeedChange = true

	rec := fixture.authorized(http.MethodPost, "/api/admin/password", `{"currentPassword":"`+captureCredential+`","newPassword":"a new credential"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("password status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	want := readGolden(t, "rest-password.json")

	if got != want {
		t.Errorf("password answer =\n  %s\nwant\n  %s", got, want)
	}

	if len(fixture.hashes.Hashed) != 1 || fixture.hashes.Hashed[0] != "a new credential" {
		t.Errorf("the credential the server was asked to store is %v, want the one the administrator submitted", fixture.hashes.Hashed)
	}
}

// A change whose current password is not the one on record is refused with the failure status, and the
// refusal is written in the log view with the wording the legacy server has always used.
func TestPasswordEndpointRefusesAChangeWhoseCurrentPasswordIsWrong(t *testing.T) {
	fixture := newAdminFixture()

	fixture.options.Stored.AdminPasswordHash = captureCredential

	rec := fixture.authorized(http.MethodPost, "/api/admin/password", `{"currentPassword":"not the one on record","newPassword":"a new credential"}`)

	if rec.Code != http.StatusExpectationFailed {
		t.Fatalf("password status = %d, want 417: %s", rec.Code, rec.Body.String())
	}

	if !fixture.loggedContaining("the administrator password was not changed: current password is invalid") {
		t.Errorf("the refusal is not in the log view naming the reason it was refused: %v", fixture.logs.Stored)
	}

	if len(fixture.hashes.Hashed) != 0 {
		t.Errorf("the server stored %v, want nothing stored for a refused change", fixture.hashes.Hashed)
	}
}

// A change that submits no new credential is not a change the server can make, and the endpoint says so
// with the status it uses for a body it cannot act on.
func TestPasswordEndpointRejectsAChangeThatNamesNoNewPassword(t *testing.T) {
	fixture := newAdminFixture()

	fixture.options.Stored.AdminPasswordHash = captureCredential

	rec := fixture.authorized(http.MethodPost, "/api/admin/password", `{"currentPassword":"`+captureCredential+`"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("password status = %d, want 400: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.hashes.Hashed) != 0 {
		t.Errorf("the server stored %v, want nothing stored for a change that named no new credential", fixture.hashes.Hashed)
	}
}

// access codes

// An added access code is the one the administrator described, and it is in the configuration afterwards.
func TestUserAddEndpointAddsTheAccessCodeTheAdministratorDescribed(t *testing.T) {
	fixture := newAdminFixture()

	rec := fixture.authorized(http.MethodPost, "/api/admin/user-add", `{"code":"dispatch-code","ident":"Dispatch","limit":5,"talkgroups":"*"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("user-add status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.accesses.Stored) != 1 {
		t.Fatalf("the server runs with %v, want the one code the administrator added", fixture.accesses.Stored)
	}

	access := fixture.accesses.Stored[0]

	if access.Code != "dispatch-code" || access.Ident != "Dispatch" || access.Limit != 5 {
		t.Errorf("the added code is %#v, want the code, name, and limit the administrator described", access)
	}

	rec = fixture.authorized(http.MethodGet, "/api/admin/config/access", "")

	entries := decodedSection(t, rec)

	if len(entries) != 1 || entries[0]["code"] != "dispatch-code" {
		t.Errorf("the access section answers %s, want the code that was just added", rec.Body.String())
	}

	if len(fixture.sessions.Announced) == 0 {
		t.Errorf("the open administrator sessions were not told the configuration changed")
	}
}

// Removing an access code that is not there is answered the way the legacy server has always answered it:
// with success and nothing else. The administrator's page reloads the list either way, and a server that
// answered this with a failure would show an error for a list that is already correct.
func TestUserRemoveEndpointAnswersSuccessWhenNothingMatched(t *testing.T) {
	fixture := newAdminFixture()

	rec := fixture.authorized(http.MethodPost, "/api/admin/user-remove", `{"ident":"A code that was never added"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("user-remove status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.Len() != 0 {
		t.Errorf("user-remove answers %q, want nothing", rec.Body.String())
	}
}

// An access code is removed by its name, which is what the administrator's page sends back, and every code
// sharing that name goes with it.
func TestUserRemoveEndpointRemovesTheCodesSharingTheNameTheAdministratorGave(t *testing.T) {
	fixture := newAdminFixture()

	fixture.accesses.Stored = domain.Accesses{
		{ID: 1, Code: "first-code", Ident: "Shared name", Limit: 1},
		{ID: 2, Code: "second-code", Ident: "Shared name", Limit: 2},
		{ID: 3, Code: "other-code", Ident: "Another name", Limit: 3},
	}

	rec := fixture.authorized(http.MethodPost, "/api/admin/user-remove", `{"ident":"Shared name"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("user-remove status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(fixture.accesses.Stored) != 1 || fixture.accesses.Stored[0].Ident != "Another name" {
		t.Errorf("the server runs with %v, want only the code whose name is different", fixture.accesses.Stored)
	}
}

// errLogStoreUnreadable is what a log store says when it cannot be read at all.
var errLogStoreUnreadable = &logStoreError{}

type logStoreError struct{}

func (err *logStoreError) Error() string {
	return "logs.search: no such table: logs in SELECT COUNT(*) FROM \"logs\" WHERE TRUE"
}
