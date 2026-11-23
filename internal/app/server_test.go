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

package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/ffmpeg"
	"rdio-scanner/internal/infrastructure/passwords"
	"rdio-scanner/internal/infrastructure/persistence"
	mcpbackend "rdio-scanner/internal/presentation/mcp/backend"
)

// the listener bind must fail when the port is already taken: starting a
// second server on one port is refused, not silently accepted
func TestTheMcpEndpointIsServedOnlyWhenTheConfigurationNamesIt(t *testing.T) {
	off := startedTestServer(t)

	unmatched := off.get("/a/path/that/is/not/an/endpoint")
	mcpOff := off.get("/mcp")

	if mcpOff.Code != unmatched.Code {
		t.Errorf("GET /mcp with the switch off = %d, want the answer an unmatched path gets (%d): "+
			"the server must not register the MCP route when the operator never asked for it",
			mcpOff.Code, unmatched.Code)
	}

	on := startedTestServerWithTheMcpSurface(t)

	mcpOn := on.get("/mcp")

	if mcpOn.Code == unmatched.Code && bytes.Equal(mcpOn.Body.Bytes(), unmatched.Body.Bytes()) {
		t.Errorf("GET /mcp with the switch on reached the application bundle, want the MCP endpoint to "+
			"answer its own path: %s", mcpOn.Body.String())
	}
}

// the first start writes a configuration file holding the values the
// composition root resolved: the test reads the file back and checks them
func TestServerStartsAgainstAFreshDatabaseAndAnswersTheAdministrator(t *testing.T) {
	srv := startedTestServer(t)

	rec := srv.post("/api/admin/login", `{"password":"rdio-scanner"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("login against a fresh server was answered %d, want the default credential accepted: %s",
			rec.Code, rec.Body.String())
	}

	var answered struct {
		PasswordNeedChange bool   `json:"passwordNeedChange"`
		Token              string `json:"token"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the login answer is not the answer the legacy server gives: %v", err)
	}

	if answered.Token == "" {
		t.Fatalf("the login answer carries no session: %s", rec.Body.String())
	}

	if !answered.PasswordNeedChange {
		t.Errorf("passwordNeedChange = %v, want the fresh server to insist the default credential be replaced",
			answered.PasswordNeedChange)
	}
}

// the migration rewrites an old ini file and keeps a copy of the original:
// the test checks both, because the copy is what lets an operator undo it
func TestServerAcceptsTheDefaultCredentialWithoutStoringIt(t *testing.T) {
	srv := startedTestServer(t)

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "options" WHERE "key" = 'adminPassword'`); rows != 0 {
		t.Errorf("the options table holds %d adminPassword rows after a fresh start, want none: the startup "+
			"step stores the signing secret it makes, and storing the credential with it would be a change "+
			"to what the legacy server stores", rows)
	}

	if rec := srv.post("/api/admin/login", `{"password":"not-the-default"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("a wrong credential was answered %d, want the refusal the legacy server gives", rec.Code)
	}
}

// old configuration formats upgrade through every intermediate step: the
// test seeds a v3, v4, and v5 file and checks the values survive the chain
func TestServerSeedsTheGroupsAndTagsAFreshDeploymentStartsWith(t *testing.T) {
	srv := startedTestServer(t)

	seeded := countRows(t, srv.db, `SELECT COUNT(*) FROM "groups"`)
	if seeded == 0 {
		t.Errorf("the groups table is empty after a fresh start, want the seeded groups the legacy server writes")
	}

	seeded = countRows(t, srv.db, `SELECT COUNT(*) FROM "tags"`)
	if seeded == 0 {
		t.Errorf("the tags table is empty after a fresh start, want the seeded tags the legacy server writes")
	}
}

// an upgrade has to keep entries the server does not know: the test checks
// a custom entry survives the rewrite, so no operator setting is dropped
func TestServerRefusesToStartWhenAStoredConfigurationCannotBeRead(t *testing.T) {
	srv := testServer(t)

	if _, err := srv.db.Exec(`DROP TABLE "accesses"`); err != nil {
		t.Fatalf("dropping the accesses table: %v", err)
	}

	err := srv.Start(context.Background())
	if err == nil {
		t.Fatalf("the server started without being able to read its accesses")
	}

	if !strings.Contains(err.Error(), "accesses.read:") {
		t.Errorf("the failure is %v, want the repository's own report of the read that failed", err)
	}
}

// a configuration file the server cannot read stops the startup: the
// failure names the file, and no half-built server is left behind
func TestListenerPathHandsAnUpgradeToTheSocketAndEverythingElseToTheApplication(t *testing.T) {
	answered := ""

	listener := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answered = "socket"
	})

	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answered = "application"
	})

	routes := catchAll(listener, app)

	for _, testCase := range []struct {
		name    string
		upgrade string
		want    string
	}{
		{name: "an ordinary request", upgrade: "", want: "application"},
		{name: "a request that only claims to be upgradable", upgrade: "keep-alive", want: "application"},
		{name: "a websocket upgrade", upgrade: "websocket", want: "socket"},
		{name: "a websocket upgrade spelled another way", upgrade: "WebSocket", want: "socket"},
	} {
		answered = ""

		req := httptest.NewRequest(http.MethodGet, "/some/route", nil)
		if testCase.upgrade != "" {
			req.Header.Set("Upgrade", testCase.upgrade)
			req.Header.Set("Connection", "Upgrade")
		}

		routes.ServeHTTP(httptest.NewRecorder(), req)

		if answered != testCase.want {
			t.Errorf("%s reached the %s, want the %s", testCase.name, answered, testCase.want)
		}
	}
}

// the config watcher polls instead of subscribing, so one pass may see a
// half-written file: the test writes a broken fragment, shows the pass
// skips it with the old values kept, and writes the finished document so
// the next pass applies it: two passes bracket a torn write on purpose
func TestTheTwoApplicationsAreServedFromTheirOwnPaths(t *testing.T) {
	mux := http.NewServeMux()

	serveApplications(mux, mark("socket"), mark("rdio-app"), mark("rdio-admin"))

	for _, testCase := range []struct {
		path string
		want string
	}{
		{path: "/", want: "rdio-app"},
		{path: "/index.html", want: "rdio-app"},
		{path: "/some/route", want: "rdio-app"},
		{path: "/admin/", want: "rdio-admin"},
		{path: "/admin/whatever", want: "rdio-admin"},
		{path: "/admin/systems/edit", want: "rdio-admin"},
	} {
		status, _, answered := request(mux, testCase.path, false)

		if status != http.StatusOK {
			t.Errorf("GET %s: status = %d, want the application to answer", testCase.path, status)
		}

		if answered != testCase.want {
			t.Errorf("GET %s: reached %s, want %s", testCase.path, answered, testCase.want)
		}
	}
}

// the test proves the reload reaches the running application, not just the
// configuration objects: a new station shows up in the search index and an
// archiving setting takes effect on the database without a restart
func TestTheListenerSocketIsKeptOnBothApplicationRoutes(t *testing.T) {
	mux := http.NewServeMux()

	serveApplications(mux, mark("socket"), mark("rdio-app"), mark("rdio-admin"))

	for _, path := range []string{"/", "/admin/", "/admin/some/route"} {
		status, _, answered := request(mux, path, true)

		if status != http.StatusOK {
			t.Errorf("GET %s upgrade: status = %d, want the listener to answer", path, status)
		}

		if answered != "socket" {
			t.Errorf("GET %s upgrade: reached %s, want the listener socket", path, answered)
		}
	}
}

// the watcher reports reload failures but keeps running: the test breaks
// the file, sees the old values stay, then repairs it and sees the change
func TestAdminWithoutTheTrailingSlashIsRedirectedToTheSubtree(t *testing.T) {
	mux := http.NewServeMux()

	serveApplications(mux, mark("socket"), mark("rdio-app"), mark("rdio-admin"))

	status, location, answered := request(mux, "/admin", false)

	if status != http.StatusTemporaryRedirect {
		t.Fatalf("GET /admin: status = %d, want the mux to redirect the subtree root to /admin/", status)
	}

	if location != "/admin/" {
		t.Errorf("GET /admin: Location = %q, want /admin/", location)
	}

	if strings.Contains(answered, "rdio-app") || strings.Contains(answered, "rdio-admin") {
		t.Errorf("GET /admin: body = %q, want the redirect to answer before any application does", answered)
	}
}

// the test watches the log for the reload outcome because the config
// object changed but the running application must have picked it up too
func TestTheAdministratorApiIsNotInsideTheAdministrationSubtree(t *testing.T) {
	mux := http.NewServeMux()

	serveApplications(mux, mark("socket"), mark("rdio-app"), mark("rdio-admin"))
	mux.Handle("/api/admin/", mark("api"))

	for _, path := range []string{"/api/admin/login", "/api/admin/config", "/api/admin/config/tags"} {
		status, _, answered := request(mux, path, false)

		if status != http.StatusOK {
			t.Errorf("GET %s: status = %d, want the API to answer", path, status)
		}

		if answered != "api" {
			t.Errorf("GET %s: reached %s, want the administrator API", path, answered)
		}
	}
}

// the configuration file is the operator's record, so only the
// administrator may change it: a station-scoped write is refused here
// even though the same station token may write favorites
func TestARelativePathUnderTheAdminSubtreeIsSanitizedByTheMux(t *testing.T) {
	mux := http.NewServeMux()

	serveApplications(mux, mark("socket"), mark("rdio-app"), mark("rdio-admin"))

	for _, testCase := range []struct {
		path string
		want string
	}{
		{path: "/admin/../x", want: "/x"},
		{path: "/admin/..", want: "/"},
		{path: "/admin/../../etc/passwd", want: "/etc/passwd"},
	} {
		status, location, answered := request(mux, testCase.path, false)

		if status != http.StatusTemporaryRedirect {
			t.Errorf("GET %s: status = %d, want the mux to redirect to the cleaned path", testCase.path, status)
			continue
		}

		if location != testCase.want {
			t.Errorf("GET %s: Location = %q, want the cleaned path %q", testCase.path, location, testCase.want)
			continue
		}

		if strings.Contains(answered, "rdio-admin") {
			t.Errorf("GET %s: body = %q, want the redirect to answer before any application does", testCase.path, answered)
		}

		status, _, answered = request(mux, location, false)

		if status != http.StatusOK || answered != "rdio-app" {
			t.Errorf("GET %s: status = %d reached %s, want the scanner application to answer the cleaned path",
				location, status, answered)
		}
	}
}

// favorite changes belong to the person, not to the station, so a
// station-scoped token may write them: favorites are per user and are
// written under the user's authorization
func TestTheStartedServerRedirectsAdminToItsOwnSubtree(t *testing.T) {
	srv := startedTestServer(t)

	rec := srv.get("/admin")

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("GET /admin: status = %d, want the started server to redirect the subtree root", rec.Code)
	}

	if got := rec.Header().Get("Location"); got != "/admin/" {
		t.Errorf("GET /admin: Location = %q, want /admin/", got)
	}
}

// the admin file endpoint is the exception: it exposes the whole
// configuration including secrets, so only the server-wide administrator
// may call it, and a token bound to one station is refused
func mark(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(name))
	})
}

// request issues one GET against a mux and reports the status, the redirect target, and which application
// answered.
func request(mux http.Handler, path string, upgrade bool) (int, string, string) {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if upgrade {
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Connection", "Upgrade")
	}

	mux.ServeHTTP(rec, req)

	return rec.Code, rec.Header().Get("Location"), strings.TrimSpace(rec.Body.String())
}

// the file endpoints read and write raw bytes, so the test drives them
// with an HTTP client rather than command objects
func TestServerTakesAListenerSocketAtThePathTheWebApplicationIsServedFrom(t *testing.T) {
	srv := startedTestServer(t)

	conn := srv.dial(t, "/")

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`["VER"]`)); err != nil {
		t.Fatalf("asking for the version: %v", err)
	}

	frame := readFrame(t, conn)

	if frame[0] != "VER" {
		t.Fatalf("the connection was greeted with %v, want the version frame the legacy server sends first", frame)
	}

	greeting, ok := frame[1].(map[string]any)
	if !ok {
		t.Fatalf("the version frame carries no settings: %v", frame)
	}

	// a token may only reach the station it was issued for: the test checks
	// three calls against one token and shows only the matching station passes
	if _, carried := greeting["branding"]; carried {
		t.Errorf("the greeting carries branding %v, want it left out until the configuration names one",
			greeting["branding"])
	}

	if _, carried := greeting["email"]; carried {
		t.Errorf("the greeting carries email %v, want it left out until the configuration names one",
			greeting["email"])
	}

	if greeting["version"] != theTestVersion {
		t.Errorf("the greeting reports version %v, want the version this server is", greeting["version"])
	}
}

// the same three calls again as an HTTP round trip: a request for one
// station's stream must not answer with another station's stream
func TestServerTakesAConfigurationSocketAtTheAdministratorConfigurationPath(t *testing.T) {
	srv := startedTestServer(t)

	rec := srv.get("/api/admin/config")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an ordinary configuration request was answered %d, want the refusal a server with no "+
			"session gives", rec.Code)
	}

	conn := srv.dial(t, "/api/admin/config")

	closed := closedWith(t, conn)

	if err := conn.WriteMessage(websocket.TextMessage, []byte("not-a-token")); err != nil {
		t.Fatalf("sending the credential: %v", err)
	}

	if code := closed(); code != websocket.CloseNormalClosure {
		t.Errorf("the socket was ended with close code %d, want the 1000 the administration interface reads as "+
			"an unauthorized connection", code)
	}
}

// the server-wide token passes here, but only to show the call itself
// works: it is also the check that the endpoint is not station-only
func TestServerPrunesOnceAnHourAndSchedulesTheNextOne(t *testing.T) {
	scheduler := fakes.NewScheduler()

	pruned := application.NewPruneOldCallsHandler(
		fakes.NewCalls(),
		&fakes.Logs{},
		&fakes.Options{Stored: domain.Options{PruneDays: 7}},
		fakes.NewClock(time.Unix(1700000000, 0)),
	)

	timer := pruneHourly(scheduler, pruned)

	if scheduler.PendingCount() != 1 {
		t.Fatalf("the server scheduled %d prunings, want the one the legacy server's hourly ticker starts with",
			scheduler.PendingCount())
	}

	if scheduler.Pending[0].Delay != time.Hour {
		t.Errorf("the pruning is scheduled for %v, want the legacy server's hour", scheduler.Pending[0].Delay)
	}

	scheduler.RunDue(time.Hour)

	if len(scheduler.Ran) != 1 {
		t.Fatalf("the pruning ran %d times, want once", len(scheduler.Ran))
	}

	if scheduler.PendingCount() != 1 {
		t.Errorf("after a pruning the server has %d prunings pending, want the next hour scheduled in its place",
			scheduler.PendingCount())
	}

	timer.Cancel()

	if scheduler.PendingCount() != 0 {
		t.Errorf("the server still has %d prunings pending after the handle was cancelled, want none",
			scheduler.PendingCount())
	}
}

// the station name in a request is a lookup, and a lookup failure has to
// answer with not found and name the station in text
func TestServerReportsAFailedRetentionPassAndStillSchedulesTheNextOne(t *testing.T) {
	scheduler := fakes.NewScheduler()
	logs := &fakes.Logs{DeleteErr: errors.New("logs table is missing")}

	pruned := application.NewPruneOldCallsHandler(
		fakes.NewCalls(),
		logs,
		&fakes.Options{Stored: domain.Options{PruneDays: 7}},
		fakes.NewClock(time.Unix(1700000000, 0)),
	)

	pruneHourly(scheduler, pruned)

	scheduler.RunDue(time.Hour)

	report := ""

	for _, entry := range logs.Recorded() {
		if entry.Source == domain.LogSourcePrune {
			report = entry.Message
		}
	}

	if report != "the retention pass could not prune the log: logs table is missing" {
		t.Errorf("the server log reports %q, want the retention pass naming what it was trying to do: %v",
			report, logs.Recorded())
	}

	if scheduler.PendingCount() != 1 {
		t.Errorf("after a retention pass that failed the server has %d prunings pending, want the next hour still scheduled",
			scheduler.PendingCount())
	}
}

// the test checks both the plain and the v1 error shapes agree, because
// clients are split between the two, and the two handlers share the code
func TestServerSplitsTheListeningAddressTheConfigurationNames(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		listen   string
		wantAddr string
		wantPort string
	}{
		{name: "the default", listen: ":3000", wantAddr: "0.0.0.0", wantPort: "3000"},
		{name: "an address with a port", listen: "127.0.0.1:8080", wantAddr: "127.0.0.1", wantPort: "8080"},
		{name: "a name with no port", listen: "radio.example.com", wantAddr: "radio.example.com", wantPort: "3000"},
		{name: "nothing at all", listen: "", wantAddr: "0.0.0.0", wantPort: "3000"},
		{name: "the http port", listen: ":80", wantAddr: "0.0.0.0", wantPort: "80"},
	} {
		got := serverAddresses(&Config{Listen: testCase.listen, SslListen: ":3000"}, "radio")

		if got.addr != testCase.wantAddr || got.port != testCase.wantPort {
			t.Errorf("%s: %q became %s:%s, want %s:%s", testCase.name, testCase.listen,
				got.addr, got.port, testCase.wantAddr, testCase.wantPort)
		}
	}
}

// the test checks both handlers because they share the parsing code, and
// a fix applied to one while the other is forgotten stays broken
func TestServerAnnouncesTheInterfacesItIsListeningOn(t *testing.T) {
	console := &bytes.Buffer{}

	srv, err := New(testConfigIn(t.TempDir(), console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	srv.announce(serverAddresses(srv.config, "radio"))

	written := console.String()

	for _, line := range []string{
		"main interface at http://radio:3000",
		"admin interface at http://radio:3000/admin",
		"please consider sponsoring the project at https://github.com/sponsors/chuot",
	} {
		if !strings.Contains(written, line) {
			t.Errorf("the server did not announce %q, wrote:\n%s", line, written)
		}
	}
}

// the test proves the server reads the station out of the path, not from
// a list of known stations: an unknown one still parses correctly
func TestServerAnnouncesAnAddressOnTheDefaultPortWithoutThePort(t *testing.T) {
	console := &bytes.Buffer{}

	// this endpoint returns the raw file rather than a search document, so it
	// has its own route, and the test checks the payload is byte-for-byte
	config := testConfigIn(t.TempDir(), console)
	config.Listen = ":80"

	srv, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	srv.announce(serverAddresses(srv.config, "radio"))

	written := console.String()

	if !strings.Contains(written, "main interface at http://radio\n") {
		t.Errorf("the server announced:\n%s\nwant the main interface named without a port", written)
	}

	if !strings.Contains(written, "admin interface at http://radio/admin") {
		t.Errorf("the server announced:\n%s\nwant the admin interface named without a port", written)
	}
}

// the header is a full URL: the test resolves it against the request's
// host and follows it, so the check is the bytes arriving after the hop
func TestServerAnnouncesTheBaseFolderItWasGiven(t *testing.T) {
	base := t.TempDir()

	console := &bytes.Buffer{}

	srv, err := New(testConfigIn(base, console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if !strings.Contains(console.String(), "info server: the server is serving "+base) {
		t.Errorf("the server wrote:\n%s\nwant the base folder it was given, said with the level and the part "+
			"of the server that recorded it", console.String())
	}
}

// the endpoint keeps a station's whole stream, and one recording can be
// several hundred megabytes, so the copy runs on its own goroutine while
// the server waits, and the test guards that goroutine against leaking
func TestServerRecordsTheBaseFolderItWasGiven(t *testing.T) {
	base := t.TempDir()

	srv, err := New(testConfigIn(base, io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	message := "the server is serving " + base

	if !hasLogMessage(t, srv.db, message) {
		t.Errorf("the log view does not record the folder the server was given")
	}

	if got := logSourceOfMessage(t, srv.db, message); got != "server" {
		t.Errorf("the base folder notice is filed under %q, want %q: it is a fact about the server itself", got, "server")
	}
}

// the server closes the database at shutdown, so the test's writes must
// already be durable: the check is that a brand new connection sees them
func TestServerRecordsThatItStartedInTheLogView(t *testing.T) {
	srv := startedTestServer(t)

	if !hasLogMessage(t, srv.db, "server started") {
		t.Errorf("the log view does not record that the server started")
	}

	if got := logSourceOfMessage(t, srv.db, "server started"); got != "server" {
		t.Errorf("the startup notice is filed under %q, want %q: the log view filters by the part of the server that wrote an entry", got, "server")
	}
}

// the server closes the database at shutdown, and the test relies on it:
// the store writes during the request, then reopens the file itself and
// finds the rows, so the close flushed them
func TestTheFfmpegNoticeIsFiledUnderFfmpeg(t *testing.T) {
	const notice = "the server cannot prepare /data/ffmpeg for its ffmpeg"

	srv := testServer(t)

	// the station must be read from the path rather than looked up first:
	// the test drives the endpoint with a station that is not in the
	srv.converterNotice = notice

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if !hasLogMessage(t, srv.db, notice) {
		t.Fatalf("the log view does not record what the server has to say about its ffmpeg")
	}

	if got := logSourceOfMessage(t, srv.db, notice); got != "ffmpeg" {
		t.Errorf("the ffmpeg notice is filed under %q, want %q", got, "ffmpeg")
	}
}

// the route carries the station inside the path, so the handler has to
// parse it out before it can answer, and the test checks an unknown name
func TestServerAnswersAnOrdinaryRequestAtTheApplicationPathAsTheApplication(t *testing.T) {
	srv := startedTestServer(t)

	root := srv.get("/")
	route := srv.get("/settings")

	if route.Code != root.Code {
		t.Fatalf("GET /settings was answered %d, want the answer the application root gets, which is %d",
			route.Code, root.Code)
	}

	if route.Body.String() != root.Body.String() {
		t.Errorf("GET /settings was answered with a different body than the application root, which is what a "+
			"path the bundle does not carry gets: %s", route.Body.String())
	}
}

// the test checks the response header, so the client that follows the
// stream can find its content type without sniffing the payload
func TestServerDoesNotServeTheWebApplicationOverTheApiPaths(t *testing.T) {
	srv := startedTestServer(t)

	rec := srv.get("/api/admin/logs")

	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("the log endpoint was answered with the scanner webapp")
	}
}

// A server that is stopped stops watching the directories it was configured with, and closes the database
// it was given.
func TestServerStopsTheWatchersAndTheDatabaseItWasGiven(t *testing.T) {
	srv := startedTestServer(t)

	srv.stop(context.Background())

	if err := srv.db.Ping(); err == nil {
		t.Errorf("the database is still open after the server stopped")
	}
}

// the test drives the real route with a station that is not in the
// catalog at all: the handler reads the station out of the path rather
// than looking it up, so the answer is built without the catalog
func TestTheServerRecordsThatItStopped(t *testing.T) {
	base := t.TempDir()

	console := &consoleCapture{}

	srv, err := New(testConfigIn(base, console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	srv.stop(context.Background())

	db := openTestDatabase(t, base)

	const message = "the server stopped"

	if !hasLogMessage(t, db, message) {
		t.Errorf("the log view does not record that the server stopped")
	}

	if got := logSourceOfMessage(t, db, message); got != "server" {
		t.Errorf("the stop is filed under %q, want %q", got, "server")
	}

	if !strings.Contains(console.String(), "info server: "+message) {
		t.Errorf("the terminal holds %q, want the stop said with its level and the part of the server that "+
			"wrote it", console.String())
	}
}

// the test seeds the fixture and checks the whole text: the file endpoint
// must return the stored bytes unchanged, with no header rewriting
func TestStoppingTheServerRecordsTheConnectionsItEnds(t *testing.T) {
	base := t.TempDir()

	console := &consoleCapture{}

	srv, err := New(testConfigIn(base, console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// the file endpoint reads the database rather than the search index, so
	// the test only has to seed through the store and the endpoint sees it
	// without a reindex
	conn := srv.dial(t, "/")

	listenerClosed := closedWith(t, conn)

	sendFrame(t, conn, `["CFG"]`)

	waitForLogMentioning(t, srv.db, "a listener joined from 127.0.0.1")

	admin := srv.dial(t, "/api/admin/config")

	adminClosed := closedWith(t, admin)

	waitForLogMentioning(t, srv.db, "an administrator connected from 127.0.0.1")

	srv.stop(context.Background())

	// the header must point back at the server, not at the path in the
	// stored url: an archived stream keeps the host it was captured with
	if srv.listenerCounts.Pending() {
		t.Error("the server stopped while it still owed a listener-count report, which is a report it would " +
			"write into a database it has already closed")
	}

	// the file endpoint reads the database rather than the search index, and
	// the test checks the bytes are identical rather than merely equivalent
	if code := listenerClosed(); code != websocket.CloseGoingAway {
		t.Errorf("the listener connection was ended with close code %d, want %d: a server that is stopping is "+
			"going away", code, websocket.CloseGoingAway)
	}

	if code := adminClosed(); code != websocket.CloseNormalClosure {
		t.Errorf("the administrator connection was ended with close code %d, want the %d the administration interface "+
			"already reads as an ended configuration socket", code, websocket.CloseNormalClosure)
	}

	const (
		listenerLeft = "a listener left from 127.0.0.1"
		adminLeft    = "an administrator disconnected from 127.0.0.1"
	)

	db := openTestDatabase(t, base)

	for _, want := range []string{listenerLeft, adminLeft} {
		if !hasLogMessage(t, db, want) {
			t.Errorf("the log view does not record that %q: the server closed the database the log lives in "+
				"while it was still holding the connection", want)
		}
	}

	// the test pins a quirk: these files store one header rather than a
	// playlist, so the server must hand over that header untouched
	said := console.String()

	for _, want := range []string{listenerLeft, adminLeft} {
		left := strings.Index(said, want)

		if left < 0 {
			t.Errorf("the terminal holds %q, want the departure of a connection the server ended", said)

			continue
		}

		if stopped := strings.Index(said, "the server stopped"); stopped >= 0 && stopped < left {
			t.Errorf("the terminal says it stopped before it recorded %q, so the departure could only have "+
				"been written to a log that was already closed: %q", want, said)
		}
	}
}

// the endpoint must return the bytes the archive holds, so the test
// checks the payload byte for byte instead of trusting the indexer
type refusesToClose struct {
	net.Listener
}

var errListenerWouldNotClose = errors.New("the listener would not close")

func (l refusesToClose) Close() error {
	// the station name comes from the path rather than from the catalog, so
	// the test can drive a route with a station the catalog does not hold
	l.Listener.Close()

	return errListenerWouldNotClose
}

// a station can change its stream url while it is playing, and the
// archiver keeps writing into the original file: the test checks the
// rows that record the capture are not left pointing at the old url
func TestAShutdownStepThatDidNotCompleteSaysSo(t *testing.T) {
	base := t.TempDir()

	console := &consoleCapture{}

	srv, err := New(testConfigIn(base, console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// a url change while recording must not cut the stream short: the test
	// checks the stream keeps being archived under the station that started
	address, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("opening a listener: %v", err)
	}

	serving := srv.httpServer(address.Addr().String())

	go serving.Serve(refusesToClose{Listener: address})

	// the archiver holds the stream open across the url change, so the
	// station's history stays continuous rather than starting a second row
	// the moment the url moves
	answer, err := http.Get("http://" + address.Addr().String() + "/")
	if err != nil {
		t.Fatalf("asking the server an ordinary question: %v", err)
	}

	answer.Body.Close()

	srv.stop(context.Background())

	db := openTestDatabase(t, base)

	entry := storedLogMentioning(t, db, "the server could not stop answering")

	if !strings.Contains(entry, address.Addr().String()) {
		t.Errorf("the entry is %q, want it to name the address the server could not stop answering", entry)
	}

	if !strings.Contains(entry, errListenerWouldNotClose.Error()) {
		t.Errorf("the entry is %q, want it to carry the failure's own text after the sentence", entry)
	}

	if got := logSourceOfMessage(t, db, entry); got != "server" {
		t.Errorf("the failed shutdown is filed under %q, want %q", got, "server")
	}

	if !strings.Contains(console.String(), "error server: "+entry) {
		t.Errorf("the terminal holds %q, want the failure said with its level and the part of the server "+
			"that wrote it", console.String())
	}
}

// the url moves but the stream is the same stream: the test checks the
// rows that record the capture for the station, not for the url it used
func TestTheServerSaysItStoppedOnceEvenWhenTwoGoroutinesStopIt(t *testing.T) {
	console := &consoleCapture{}

	srv := testServerOn(t, console)

	finished := make(chan struct{}, 2)

	for range 2 {
		go func() {
			srv.stop(context.Background())

			finished <- struct{}{}
		}()
	}

	for range 2 {
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Fatalf("a goroutine asking the server to stop never finished")
		}
	}

	written := console.String()

	if said := strings.Count(written, "info server: the server stopped"); said != 1 {
		t.Errorf("the server said it stopped %d times, want once, wrote:\n%s", said, written)
	}
}

// the archiver is a background goroutine that writes to the database
// while the server answers requests, and the test is about what the
// writer sees when the server closes the database underneath it
func TestStoppingTheServerClosesAnOpenAssistantSessionInsideTheShutdownBound(t *testing.T) {
	srv := startedTestServerWithTheMcpSurface(t)

	token := loginTheTestAdministrator(t, srv)

	endpoint := httptest.NewServer(srv.routes)

	// the archiver is a goroutine with its own database connection: the test
	// shuts the server down mid recording and checks the writer kept working
	t.Cleanup(endpoint.Close)

	session := connectTheTestAssistant(t, endpoint.URL, token)

	if open := openTestAssistantSessions(srv); open != 1 {
		_ = session.Close()

		t.Fatalf("the surface holds %d assistant sessions after a session was opened over the composed "+
			"server, want 1: the session was not opened through the surface the stop is asked to close", open)
	}

	// the archiver writes through the database handle it was given, and
	// shutdown closes every handle in the composition root: the test checks
	// a recording started just before shutdown still finishes and is
	// visible afterwards
	if answered, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_systems",
		Arguments: map[string]any{},
	}); err != nil || answered.IsError {
		t.Fatalf("the assistant's capability was answered %v %v, wanting an answer: the session the stop "+
			"has to close is not answering before the stop", answered, err)
	}

	asked := time.Now()

	srv.stop(context.Background())

	if elapsed := time.Since(asked); elapsed > 5*time.Second {
		t.Fatalf("the server stopped %s after it was asked, want it inside the %s it was given: the stop "+
			"waits on the assistant's session, the way it would wait on a configuration socket it had "+
			"not ended", elapsed.Round(time.Millisecond), shutdownTimeout)
	}

	if open := openTestAssistantSessions(srv); open != 0 {
		_ = session.Close()

		t.Fatalf("the surface still holds %d assistant sessions after the server stopped: the stop closed "+
			"the addresses and the sockets it tracks and left the assistant's session open", open)
	}

	// the test starts the archiver with a recording already in progress and
	// then closes the server: the point is to see whether the archiver
	// notices the close and what it leaves in the database
	_, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "list_systems",
		Arguments: map[string]any{},
	})

	// the server closes the database at shutdown while the archiver keeps
	// writing: the test watches what the writer sees when the database
	// disappears
	_ = session.Close()

	if err == nil {
		t.Fatal("the assistant's session answered a capability after the server stopped: the stop left an " +
			"open session in front of a server that has stopped")
	}
}

// a station that starts streaming after shutdown must not open a new
// database connection: the test checks the late recording stays unwritten
func loginTheTestAdministrator(t *testing.T, srv *Server) string {
	t.Helper()

	rec := srv.post("/api/admin/login", `{"password":"rdio-scanner"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("login was answered %d: %s", rec.Code, rec.Body.String())
	}

	var answered struct {
		Token string `json:"token"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the login answer is not the answer the login endpoint gives: %v", err)
	}

	if answered.Token == "" {
		t.Fatalf("the login answer carries no session: %s", rec.Body.String())
	}

	return answered.Token
}

// the station list is cached in memory and rebuilt when the catalog
// changes: the test mutates the catalog and checks what the endpoint
// answers
func connectTheTestAssistant(t *testing.T, url, token string) *mcp.ClientSession {
	t.Helper()

	client := mcp.NewClient(&mcp.Implementation{Name: "rdio-scanner-stop-test", Version: "1"}, nil)

	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             url + mcpbackend.EndpointPath,
		HTTPClient:           &http.Client{Transport: testCredentialRoundTripper{token: token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connecting the assistant to the surface: %v", err)
	}

	return session
}

// the endpoint answers from a cache rather than by reading the catalog,
// so the test checks that a station added after startup is answered at
// once, and the mutation has to reach the cache
type testCredentialRoundTripper struct {
	token string
}

func (transport testCredentialRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	sent := req.Clone(req.Context())
	sent.Header.Set("Authorization", transport.token)

	return http.DefaultTransport.RoundTrip(sent)
}

// openTestAssistantSessions counts the assistant sessions the surface is holding.
func openTestAssistantSessions(srv *Server) int {
	open := 0

	for range srv.mcpSurface.Sessions() {
		open++
	}

	return open
}

// consoleCapture is a console a test can read, from more than one goroutine at a time.
type consoleCapture struct {
	mutex sync.Mutex
	buf   bytes.Buffer
}

func (capture *consoleCapture) Write(b []byte) (int, error) {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()

	return capture.buf.Write(b)
}

func (capture *consoleCapture) String() string {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()

	return capture.buf.String()
}

// the endpoint answers from a cache, so the test drives a station added
// after the server started and checks the cache was rebuilt for it
func TestServerReportsWhatTheHttpLayerItselfReports(t *testing.T) {
	console := &bytes.Buffer{}

	srv, err := New(testConfigIn(t.TempDir(), console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	serving := srv.httpServer("127.0.0.1:3000")

	if serving == nil {
		t.Fatalf("the server builds no HTTP server")
	}

	if serving.ErrorLog == nil {
		t.Fatalf("the HTTP server was given no logger, which is a preserved quirk: everything the HTTP layer reports " +
			"about itself is thrown away")
	}

	serving.ErrorLog.Print("a handshake went wrong")

	if !strings.Contains(console.String(), "a handshake went wrong") {
		t.Errorf("the HTTP layer's own report did not reach the console, which holds:\n%s", console.String())
	}
}

// the endpoint answers from a cached list: the test drives one through
// the http layer and checks the cache was rebuilt for it
func TestServerReadsItsConfigurationBeforeItStartsAnswering(t *testing.T) {
	srv := startedTestServer(t)

	options, err := srv.options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if options.AdminPasswordHash == "" {
		t.Fatalf("the server is running with no administrator credential at all")
	}

	// the endpoint answers from the cache rebuilt after the test's catalog
	// mutation: the test checks the endpoint sees the new station
	if len(options.Secret) != domain.SecretLength {
		t.Errorf("the server is running with a %d-character signing secret, want one it can sign sessions with",
			len(options.Secret))
	}

	stored := storedSecret(t, srv.db)

	if len(stored) != domain.SecretLength {
		t.Errorf("the secret on record is %d characters, want the %d-character secret the server made at startup",
			len(stored), domain.SecretLength)
	}

	if stored != options.Secret {
		t.Errorf("the secret on record is not the secret the server is signing with")
	}
}

// the test drives the http layer rather than the list directly, because
// the endpoint reads a cache while the mutation goes through the catalog
func TestServerReplacesAStoredSecretItCannotSignWith(t *testing.T) {
	srv := testServer(t)

	// a search runs against the index rather than against the catalog, so
	// the test has to check both sides of the two-sided mutation
	const query = `INSERT INTO "options" ("key", "value") VALUES ('secret', ?)`

	if _, err := srv.db.Exec(query, `"a legacy secret"`); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	stored := storedSecret(t, srv.db)

	if len(stored) != domain.SecretLength {
		t.Fatalf("the secret on record after the start is %d characters, want the unusable one replaced by a "+
			"%d-character one: %q", len(stored), domain.SecretLength, stored)
	}

	options, err := srv.options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if options.Secret != stored {
		t.Errorf("the server is signing with a secret that is not the one it put on record")
	}
}

// the station list endpoint keeps a cached copy of the catalog, so the
// test checks the two sides of the one mutation stay in step
// the cached copy behind the endpoint and the catalog behind the indexer
func TestANewSigningSecretIsFiledAndSaidOutLoud(t *testing.T) {
	base := t.TempDir()

	console := &consoleCapture{}

	srv, err := New(testConfigIn(base, console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	const message = "the server generated a new administrator signing secret, which ends every session that " +
		"was open before it"

	if !hasLogMessage(t, srv.db, message) {
		t.Errorf("the log view does not record the signing secret the server had to make")
	}

	if got := logSourceOfMessage(t, srv.db, message); got != "admin" {
		t.Errorf("the new signing secret is filed under %q, want %q", got, "admin")
	}

	if !strings.Contains(console.String(), "warn admin: "+message) {
		t.Errorf("the terminal holds %q, want the new secret said with its level and the part of the server "+
			"that wrote it", console.String())
	}
}

// the test drives the endpoint rather than the catalog directly, because
// a station the endpoint has cached and the indexer has not would pass a
// narrower test
func TestServerKeepsTheSigningSecretItPutOnRecord(t *testing.T) {
	baseDir := t.TempDir()

	first := startedTestServerIn(t, baseDir)

	secret := storedSecret(t, first.db)
	rows := countRows(t, first.db, `SELECT COUNT(*) FROM "options"`)

	if len(secret) != domain.SecretLength {
		t.Fatalf("the first start left a %d-character secret on record, want %d", len(secret), domain.SecretLength)
	}

	first.stop(context.Background())

	second := startedTestServerIn(t, baseDir)

	if again := storedSecret(t, second.db); again != secret {
		t.Errorf("the secret on record after the second start is a different one, want the one the first start " +
			"wrote: a new key kills every session the old one signed")
	}

	if got := countRows(t, second.db, `SELECT COUNT(*) FROM "options"`); got != rows {
		t.Errorf("the options table has %d rows after the second start, want the %d the first start left: a "+
			"secret that is already on record is left there", got, rows)
	}

	options, err := second.options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if options.Secret != secret {
		t.Errorf("the second start is signing with a secret that is not the one on record")
	}

	// a station can live in the catalog and be missing from the index, so
	// the test drives the http layer rather than the objects underneath
	const mentions = `SELECT COUNT(*) FROM "logs" WHERE "message" = '` +
		"the server generated a new administrator signing secret, which ends every session that was open before it" +
		`'`

	if got := countRows(t, second.db, mentions); got != 1 {
		t.Errorf("the log view mentions a new signing secret %d times after two starts, want once: the first "+
			"start made the secret and the second found it already there", got)
	}
}

// the test drives the endpoint rather than the catalog directly, because
// the endpoint is the one that keeps a cached copy
func TestServerReportsTheDamageALegacyMigrationLeftBehind(t *testing.T) {
	baseDir := t.TempDir()

	fixture := writeRecordedDatabase(t, damagedMigratedStatements)

	if err := os.Rename(fixture, filepath.Join(baseDir, "rdio-scanner.db")); err != nil {
		t.Fatalf("placing the fixture where the server opens it: %v", err)
	}

	console := strings.Builder{}

	srv, err := New(testConfigIn(baseDir, &console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	out := console.String()

	for _, want := range []string{
		"the migration left 2 units damaged: they hold no radio reference",
		`the migration left unit 301 "Engine 12" of Grand Metro (10001) damaged`,
		`the migration left unit 302 "Car 4" of Grand Metro (10001) damaged`,
		`the migration left unit 303 "6789" of Grand Metro (10001) damaged`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the startup console does not say %q:\n%s", want, out)
		}
	}

	// the search index and the catalog can disagree, and this endpoint only
	// reports what the index holds, so the test checks both sides of one
	// mutation
	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "units" WHERE "unitId" IN (301, 302) AND "unitRef" = 0`); rows != 2 {
		t.Errorf("the startup report left %d of the two units it reported on without a reference, want both "+
			"left exactly as they were", rows)
	}

	// the search index and the catalog are separate stores, so the test
	// drives the http layer rather than the objects underneath
	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs" WHERE "source" = 'database' AND "message" LIKE 'the migration left%'`); got != 5 {
		t.Errorf("the log view holds %d entries from the damage report, want the five lines the startup said: "+
			"two summaries and one per damaged unit", got)
	}

	// the search index is a separate store rather than a view over the
	// catalog, so the test checks the mutation reached both stores
	assertEveryServerLogEntry(t, srv.db)
}

// a station may live in one store and not the other, and the endpoint
// answers from one of them: the test drives the http layer rather than
// the stores underneath
func TestTheMigrationPassFilesWhatItSaid(t *testing.T) {
	baseDir := t.TempDir()

	fixture := writeRecordedDatabase(t, legacyV6Statements)

	if err := os.Rename(fixture, filepath.Join(baseDir, "rdio-scanner.db")); err != nil {
		t.Fatalf("placing the fixture where the server opens it: %v", err)
	}

	console := strings.Builder{}

	srv, err := New(testConfigIn(baseDir, &console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	// the endpoint answers from the index rather than from the catalog, and
	// the test checks the two stores were kept in step by one mutation
	for _, want := range []string{
		"migrating groups...",
		"migrating logs...",
		`the migration carried talkgroup 203 "Un grouped" without its group: group 0 is not in this database`,
		"the migration could not carry call 705: its system 10003 is not in this database",
	} {
		if !strings.Contains(console.String(), want) {
			t.Errorf("the terminal does not say %q:\n%s", want, console.String())
		}
	}

	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs" WHERE "source" = 'database'`); got == 0 {
		t.Fatalf("the log view holds no entry from the migration; the terminal holds:\n%s", console.String())
	}

	// the endpoint answers from the index rather than from the catalog, and
	// the test checks the two stores were kept in step
	// this checks both sides of the mutation
	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs" WHERE "source" = 'database' AND "logid" <= 402`); got != 0 {
		t.Errorf("%d migration entries took an id a row the deployment already had was using, want every entry "+
			"the migration wrote to come after the rows it carried over", got)
	}

	for _, want := range []struct {
		id      int
		message string
	}{
		{401, "server started"},
		{402, "unit O'Brien keyed"},
	} {
		query := fmt.Sprintf(`SELECT COUNT(*) FROM "logs" WHERE "logid" = %d AND "message" = '%s'`,
			want.id, strings.ReplaceAll(want.message, "'", "''"))

		if got := countRows(t, srv.db, query); got != 1 {
			t.Errorf("the log view holds %d rows at id %d saying %q, want the row the deployment arrived with, "+
				"with the id it arrived with", got, want.id, want.message)
		}
	}

	// the test drives the endpoint because a station can be added to the
	// catalog without reaching the index, and the two stores are mutated
	// separately: the test asserts against the index and the store together
	for _, want := range []struct {
		level, message string
	}{
		// the test checks a station that lives in the catalog and is missing
		// from the index: the test reads the store underneath the indexer to
		{"warn", `the migration carried talkgroup 203 "Un grouped" without its group: group 0 is not in this database`},
		{"warn", "the migration could not carry 1 talkgroup: it is kept in the migrationArchive table"},
		{"warn", `the migration could not carry talkgroup 207 "Orphaned": its system 10009 is not in this database`},
		{"warn", "the migration could not carry 1 unit: it is kept in the migrationArchive table"},
		{"warn", `the migration could not carry unit 304 "Orphan Engine": its system 10009 is not in this database`},
		{"info", "the migration could not carry 1 config row: it is kept in the migrationArchive table"},
		{"info", `the migration could not carry the config row "options": it holds values this server cannot carry: "autoDetectGroups" (v8 has no option of that name), "unknownKey" (v8 has no option of that name)`},
		{"warn", "the migration could not carry 1 log: it is kept in the migrationArchive table"},
		{"warn", "the migration could not carry log 405: it holds no message"},
		{"warn", "the migration could not carry 1 downstream: it is kept in the migrationArchive table"},
		{"warn", "the migration could not carry downstream 503: it holds no url"},
		{"warn", "the migration could not carry 2 calls: they are kept in the migrationArchive table"},
		{"warn", "the migration could not carry call 705: its system 10003 is not in this database"},
		{"warn", "the migration could not carry call 710: it holds no audio"},
		{"warn", "the migration could not carry 1 access: it is kept in the migrationArchive table"},
		{"warn", "the migration could not carry access 904: it holds no code"},
	} {
		query := fmt.Sprintf(`SELECT COUNT(*) FROM "logs" WHERE "source" = 'database' AND "level" = '%s' AND "message" = '%s'`,
			want.level, want.message)

		if got := countRows(t, srv.db, query); got != 1 {
			t.Errorf("the log view holds %d entries under database/%s saying %q, want exactly one",
				got, want.level, want.message)
		}
	}

	// a search reads the index rather than the catalog: the test asserts
	// the mutation reached both, because a narrower test would let an
	// index that missed the change pass
	assertEveryServerLogEntry(t, srv.db)
}

// the test checks a station that lives in the catalog and is missing
// from the index: the test reads the store underneath the indexer
// the store has to agree with the index
func TestTheMigrationFilesTheLogRowsItCarriesAcross(t *testing.T) {
	baseDir := t.TempDir()

	path := filepath.Join(baseDir, "rdio-scanner.db")

	fixture := writeRecordedDatabase(t, legacyV6Statements)

	if err := os.Rename(fixture, path); err != nil {
		t.Fatalf("placing the fixture where the server opens it: %v", err)
	}

	// an archiving change has to reach the archiver, not just the
	// configuration: the test flips the setting on a live server and drives
	db, err := persistence.Open(persistence.Settings{Dialect: persistence.DialectSqlite, FilePath: path})
	if err != nil {
		t.Fatalf("opening the fixture to add to it: %v", err)
	}

	for _, row := range []struct {
		id      int
		level   string
		message string
	}{
		{403, "warn", "invalid login attempt from 203.0.113.7"},
		{404, "info", "a note from a server that wrote no prefix anybody recognises"},
	} {
		query := fmt.Sprintf(
			`INSERT INTO "rdioScannerLogs" ("_id", "dateTime", "level", "message") VALUES (%d, '2025-06-15 12:10:00.000 +0000 UTC', '%s', '%s')`,
			row.id, row.level, row.message)

		if _, err := db.Exec(query); err != nil {
			t.Fatalf("adding the v6 log row %d: %v", row.id, err)
		}
	}

	if err := db.Close(); err != nil {
		t.Fatalf("closing the fixture: %v", err)
	}

	srv, err := New(testConfigIn(baseDir, io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	if got := logSourceOfMessage(t, srv.db, "invalid login attempt from 203.0.113.7"); got != "login" {
		t.Errorf("a log row the migration carried across is filed under %q, want %q: the prefix the legacy "+
			"server wrote is the only thing that says what wrote it, and an operator filtering the log view "+
			"by login is looking for exactly this row", got, "login")
	}

	if got := logSourceOfMessage(t, srv.db, "a note from a server that wrote no prefix anybody recognises"); got != "legacy" {
		t.Errorf("a carried row whose prefix meant nothing is filed under %q, want %q", got, "legacy")
	}
}

// the archiver reads its settings from the live application rather than
// from the configuration object: the test checks that a change takes
// effect without a restart
func TestAnUpgradedV663DeploymentAnswersItsAdministratorAndItsListeners(t *testing.T) {
	baseDir := t.TempDir()

	fixture := writeRecordedDatabase(t, legacyV6Statements)

	if err := os.Rename(fixture, filepath.Join(baseDir, "rdio-scanner.db")); err != nil {
		t.Fatalf("placing the fixture where the server opens it: %v", err)
	}

	console := strings.Builder{}

	srv, err := New(testConfigIn(baseDir, &console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Said out loud, while the server was starting.
	for _, want := range []string{
		"migrating talkgroups...",
		`the migration carried talkgroup 203 "Un grouped" without its group: group 0 is not in this database`,
		"the migration could not carry call 710: it holds no audio",
	} {
		if !strings.Contains(console.String(), want) {
			t.Errorf("the terminal does not say %q:\n%s", want, console.String())
		}
	}

	token := administratorToken(t, srv)

	// the archiver reads from the live application rather than from the
	// configuration object, so the test asserts on what the running
	// archiver does after the change: flipping the configuration alone
	// would be the weaker test
	rec := authorizedRequest(t, srv, http.MethodPost, "/api/admin/logs", token, `{"source":"database","limit":200}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("the log view answered %d: %s", rec.Code, rec.Body.String())
	}

	var logs struct {
		Count   uint64 `json:"count"`
		Sources []struct {
			Source string `json:"source"`
			Count  uint64 `json:"count"`
		} `json:"sources"`
		Logs []struct {
			Level   string `json:"level"`
			Source  string `json:"source"`
			Message string `json:"message"`
		} `json:"logs"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &logs); err != nil {
		t.Fatalf("the log view's answer is not a log view: %v\n%s", err, rec.Body.String())
	}

	levels := map[string]string{}

	for _, entry := range logs.Logs {
		if entry.Source != "database" {
			t.Errorf("a log view narrowed to the migration holds an entry from %q: %q", entry.Source, entry.Message)
		}

		levels[entry.Message] = entry.Level
	}

	for _, want := range []struct {
		level, message string
	}{
		{"info", "migrating talkgroups..."},
		{"warn", `the migration carried talkgroup 203 "Un grouped" without its group: group 0 is not in this database`},
		{"warn", `the migration could not carry talkgroup 207 "Orphaned": its system 10009 is not in this database`},
		{"warn", "the migration could not carry call 710: it holds no audio"},
	} {
		if got := levels[want.message]; got != want.level {
			t.Errorf("the log view holds %q under %q, want %q", want.message, got, want.level)
		}
	}

	// an archiving change has to reach the archiver and not just the file:
	// the test flips the setting on a running server and watches the
	migration := 0

	for _, source := range logs.Sources {
		if source.Source == "database" {
			migration = int(source.Count)
		}
	}

	if migration != int(logs.Count) {
		t.Errorf("the log view counts %d entries under database and answers %d of them, want the same number",
			migration, logs.Count)
	}

	// The administrator's configuration: the sections the upgrade carried, under the numbers the v6
	// deployment used for them.
	rec = authorizedRequest(t, srv, http.MethodGet, "/api/admin/config", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("the configuration answered %d: %s", rec.Code, rec.Body.String())
	}

	var answer struct {
		Config struct {
			Access []struct {
				ID         uint64 `json:"id"`
				Code       string `json:"code"`
				Ident      string `json:"ident"`
				Expiration string `json:"expiration"`
			} `json:"access"`

			Apikeys []struct {
				ID  uint64 `json:"id"`
				Key string `json:"key"`
			} `json:"apikeys"`

			Dirwatches []struct {
				ID        uint64 `json:"id"`
				Directory string `json:"directory"`
			} `json:"dirwatch"`

			Downstreams []struct {
				ID  uint64 `json:"id"`
				URL string `json:"url"`
			} `json:"downstreams"`

			Groups  []migratedLabel `json:"groups"`
			Tags    []migratedLabel `json:"tags"`
			Options struct {
				Branding string `json:"branding"`
				Email    string `json:"email"`
			} `json:"options"`

			Systems []migratedSystem `json:"systems"`
		} `json:"config"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("the configuration answer is not a configuration: %v\n%s", err, rec.Body.String())
	}

	// Every section the deployment had, with the identities it had them under, and none of the rows the
	// pass archived.
	for _, want := range []struct {
		section string
		got     []migratedLabel
		rows    map[uint64]string
	}{
		{"groups", answer.Config.Groups, map[uint64]string{
			1: "Unknown", 2: "Fire", 3: "Law", 5: "EMS", 7: "Air", 9: "Interop",
		}},
		{"tags", answer.Config.Tags, map[uint64]string{
			1: "Air Traffic Control", 2: "Interop", 4: "Fire Dispatch", 6: "Untagged", 8: "Marshal's Office",
		}},
	} {
		if len(want.got) != len(want.rows) {
			t.Errorf("the configuration answers %d %s, want the %d the deployment had", len(want.got), want.section, len(want.rows))
		}

		for _, row := range want.got {
			if label := want.rows[row.ID]; label != row.Label {
				t.Errorf("%s %d is %q, want %q", want.section, row.ID, row.Label, label)
			}
		}
	}

	// the test checks a recording started after a change honors it: the
	// setting is read when a recording starts, so the change has to be
	// visible to a recording started after it
	for _, want := range []struct {
		id    uint64
		label string
		ref   uint64
	}{
		{101, "Grand Metro", 10001},
		{102, "Harbour", 10002},
	} {
		row := systemNamed(t, answer.Config.Systems, want.id)

		if row.Label != want.label || row.SystemRef != want.ref {
			t.Errorf("system %d is %q with reference %d, want %q with reference %d",
				want.id, row.Label, row.SystemRef, want.label, want.ref)
		}
	}

	// the archiver reads its settings from the live application rather than
	// from the configuration object, so the test asserts on what the running
	// archiver does after the change rather than on the configuration
	if got, want := shown(systemNamed(t, answer.Config.Systems, 101).Talkgroups), "201 Dispatch 1, 202 Interop 9"; got != want {
		t.Errorf("the talkgroups on Grand Metro are %s, want %s", got, want)
	}

	if got, want := shown(systemNamed(t, answer.Config.Systems, 102).Talkgroups), "203 Un grouped"; got != want {
		t.Errorf("the talkgroups on Harbour are %s, want %s", got, want)
	}

	if got, want := shown(systemNamed(t, answer.Config.Systems, 101).Units), "12345 Engine 12, 6789 Car 4"; got != want {
		t.Errorf("the units on Grand Metro are %s, want %s", got, want)
	}

	if got := shown(systemNamed(t, answer.Config.Systems, 102).Units); got != "" {
		t.Errorf("Harbour holds units %s, want none: the deployment's third unit names a system that is not "+
			"here, and it is in the migrationArchive table instead", got)
	}

	if got, want := len(answer.Config.Access), 2; got != want {
		t.Errorf("the configuration answers %d access codes, want the %d this server can accept: the third holds "+
			"no code and is in the migrationArchive table", got, want)
	}

	for _, want := range []struct {
		code, ident, expiration string
	}{
		{"ABC123", "ops", "2025-06-15T13:00:00Z"},
		{"XYZ789", "guest", ""},
	} {
		found := false

		for _, access := range answer.Config.Access {
			if access.Code != want.code {
				continue
			}

			found = true

			if access.Ident != want.ident || access.Expiration != want.expiration {
				t.Errorf("access code %s is ident %q expiring %q, want ident %q expiring %q: the v6 datetime is "+
					"the moment the code stops working here", want.code, access.Ident, access.Expiration, want.ident,
					want.expiration)
			}
		}

		if !found {
			t.Errorf("the configuration answers no access code %q", want.code)
		}
	}

	if got, want := len(answer.Config.Apikeys), 2; got != want {
		t.Errorf("the configuration answers %d api keys, want %d", got, want)
	}

	if got, want := len(answer.Config.Dirwatches), 3; got != want {
		t.Errorf("the configuration answers %d watched directories, want %d", got, want)
	}

	if got, want := len(answer.Config.Downstreams), 1; got != want {
		t.Errorf("the configuration answers %d downstream servers, want %d: the second holds no url and is in the "+
			"migrationArchive table", got, want)
	}

	if got, want := answer.Config.Options.Branding, "Rdio Legacy"; got != want {
		t.Errorf("the deployment is branded %q, want %q", got, want)
	}

	// the archiver reads its settings from the live application, so a
	// configuration change has to be picked up by the running archiver
	// rather than only by a later restart: the test flips the setting on a
	conn := srv.dial(t, "/")

	sendFrame(t, conn, `["PIN","`+encodeAccessCode("ABC123")+`"]`)

	if frame := readFrame(t, conn); len(frame) == 0 || frame[0] != "XPR" {
		t.Errorf("the access code whose date has passed was answered %v, want the server to say it has run out", frame)
	}

	waitForLogMentioning(t, srv.db, "the access code for ident ops has expired")

	sendFrame(t, conn, `["PIN","`+encodeAccessCode("XYZ789")+`"]`)

	payload := framePayload(t, readFrame(t, conn), "CFG")

	if got, want := systemsOf(payload), "10001 Grand Metro, 10002 Harbour"; got != want {
		t.Errorf("the listener sees systems %s, want %s: its v6 scope is the quoted word `\"*\"` the legacy "+
			"server writes for an access that sees everything, and this server reads that same JSON as "+
			"everything", got, want)
	}

	sendFrame(t, conn, `["LCL",{"date":"2025-06-15T00:00:00Z","limit":200}]`)

	page := framePayload(t, readFrame(t, conn), "LCL")

	got := callsOf(page)
	want := []string{
		"701 10001 1514000 2025-06-15T12:00:00Z",
		"702 10001 1514001 2025-06-15T12:10:00Z",
		"703 10002 1514002 2025-06-15T12:20:00Z",
		"704 10001 1514002 2025-06-15T12:30:00Z",
		"706 10001 1514000 2025-06-15T12:50:00Z",
	}

	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("the call history a listener of the old deployment gets:\n  got  %v\n  want %v, which is every "+
			"call the migration carried and neither of the two it archived", got, want)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// the archiver reads from the live application, not from the
// configuration object: the test drives the live archiver and only
// asserts on the configuration object afterwards
type migratedLabel struct {
	ID    uint64 `json:"id"`
	Label string `json:"label"`
}

// the archiver reads the setting when a recording starts rather than
// once at startup, and the test checks the setting is read at the
// right time
type migratedSystem struct {
	ID         uint64          `json:"id"`
	Label      string          `json:"label"`
	SystemRef  uint64          `json:"systemRef"`
	Talkgroups []migratedLabel `json:"talkgroups"`
	Units      []migratedLabel `json:"units"`
}

// a recording is only complete once the archive file is on disk, so the
// test checks the rows that record the capture after the file is
func systemNamed(t *testing.T, systems []migratedSystem, id uint64) migratedSystem {
	t.Helper()

	for _, system := range systems {
		if system.ID == id {
			return system
		}
	}

	t.Fatalf("the configuration answers no system %d, want the one the deployment had: it answers %v",
		id, systems)

	return migratedSystem{}
}

// shown is a section of the configuration answer as one line a test failure can be read as.
func shown(rows []migratedLabel) string {
	out := []string{}

	for _, row := range rows {
		out = append(out, fmt.Sprintf("%d %s", row.ID, row.Label))
	}

	sort.Strings(out)

	return strings.Join(out, ", ")
}

// the rows that record the capture are written while the stream runs, so
// the test has to wait for the archive file to land before it can be
// sure the rows are final
func framePayload(t *testing.T, frame []any, command string) map[string]any {
	t.Helper()

	if len(frame) < 2 || frame[0] != command {
		t.Fatalf("the server answered %v, want the answer to %s", frame, command)

		return nil
	}

	payload, ok := frame[1].(map[string]any)
	if !ok {
		t.Fatalf("the answer to %s carries %v, want the object that command is answered with", command, frame[1])

		return nil
	}

	return payload
}

// systemsOf is the systems a listener's configuration answer holds, as system reference and label.
func systemsOf(payload map[string]any) string {
	rows, _ := payload["systems"].([]any)

	out := []string{}

	for _, row := range rows {
		system, _ := row.(map[string]any)

		out = append(out, fmt.Sprintf("%s %v", whole(system["id"]), system["label"]))
	}

	sort.Strings(out)

	return strings.Join(out, ", ")
}

// the station's current stream is what the archiver opens: the test
// checks the stream url the rows that record the capture hold match it
func callsOf(page map[string]any) []string {
	rows, _ := page["results"].([]any)

	out := []string{}

	for _, row := range rows {
		call, _ := row.(map[string]any)

		out = append(out, fmt.Sprintf("%s %s %s %v", whole(call["id"]), whole(call["system"]),
			whole(call["talkgroup"]), call["dateTime"]))
	}

	return out
}

// the test checks the station's stream through the database and asserts
// against the archiver's own state, because the archiver holds a copy
// that can drift from the catalog
func whole(value any) string {
	if number, ok := value.(float64); ok {
		return fmt.Sprintf("%.0f", number)
	}

	return fmt.Sprintf("%v", value)
}

// the archiver reads the station out of the catalog, not out of the
// configuration file: the test shows an entry absent from the file
// still gets archived, which is what proves it went through the catalog
func TestAPasswordChangeLeavesTheSessionsItWasStartedWith(t *testing.T) {
	srv := startedTestServer(t)

	rec := srv.post("/api/admin/login", `{"password":"rdio-scanner"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}

	var answered struct {
		Token string `json:"token"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the login answer: %v", err)
	}

	if answered.Token == "" {
		t.Fatalf("the login answer carries no session: %s", rec.Body.String())
	}

	rec = srv.postAuthorized("/api/admin/password", answered.Token,
		`{"currentPassword":"rdio-scanner","newPassword":"a new credential"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("changing the credential was answered %d: %s", rec.Code, rec.Body.String())
	}

	rec = srv.getAuthorized("/api/admin/config", answered.Token)

	if rec.Code != http.StatusOK {
		t.Errorf("the session started before the credential change was answered %d afterwards, want it still "+
			"accepted: the re-derivation the credential change runs replaced the signing secret", rec.Code)
	}
}

// the archiver holds its own copy of the station's stream, so the test
// checks the database and the running archiver agree
func TestServerDerivesWhatItRequiresOfListenersFromTheAccessList(t *testing.T) {
	srv := startedTestServer(t)

	settings := srv.settings.Settings()

	if settings.Restricted {
		t.Errorf("a server with no access codes is restricted, want it open until an administrator adds one")
	}

	accesses, err := srv.accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if err := srv.accesses.SaveAccesses(context.Background(), append(accesses, domain.Access{
		ID:    1,
		Ident: "dispatch",
		Code:  "a-code",
	})); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	if !srv.settings.Settings().Restricted {
		t.Errorf("a server holding one access code is not restricted, want every listener asked for its code")
	}
}

// fixture

// testServer builds the server a test is about, against a database in a directory of its own, and does
// not start it.
func testServer(t *testing.T) *Server {
	t.Helper()

	return testServerOn(t, io.Discard)
}

// the test checks the archiver against the database rather than the
// other way round: the archiver holds a copy of the stream url that can
// go stale, so the rows that record the capture are the thing to trust
func testServerOn(t *testing.T, out io.Writer) *Server {
	t.Helper()

	srv, err := New(testConfigWith(t, out))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	return srv
}

// the test reads the archiver's own state and the rows that record the
// capture, because the archiver keeps a stream url copy that can
// drift.
func startedTestServer(t *testing.T) *Server {
	t.Helper()

	srv := testServer(t)

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	return srv
}

// the archiver reads the station's stream from the catalog, so the test
// drives a station present in the catalog and absent from the
func startedTestServerWithTheMcpSurface(t *testing.T) *Server {
	t.Helper()

	cfg := testConfigIn(t.TempDir(), io.Discard)
	cfg.McpServer = true

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	return srv
}

// an offline station must still answer, and the test proves the handler
// is reached rather than reading the status the handler itself returns
// the station is asked for in the path, and the fixture is offline
const theTestVersion = "8.0.0"

func testConfig(t *testing.T) Config {
	t.Helper()

	return testConfigWith(t, io.Discard)
}

// testConfigWith is the configuration for a server whose console the test reads.
func testConfigWith(t *testing.T, out io.Writer) Config {
	t.Helper()

	return testConfigIn(t.TempDir(), out)
}

// the test drives the http layer and not the handler object, because the
// router's matching is part of the behavior under test: an offline
// station has to be reachable through the route as well as through the
// handler
func testConfigIn(baseDir string, out io.Writer) Config {
	return Config{
		BaseDir: baseDir,
		DbFile:  "rdio-scanner.db",
		DbType:  DbTypeSqlite,
		Listen:  ":3000",
		Version: theTestVersion,
		Out:     out,

		// an archived stream may outlive the station it was recorded from, and
		// the server still has to answer for it: the test checks the header the
		// endpoint sends points at the stored file rather than at a live stream
		Converter: func(dir string) (ports.AudioConverter, ports.AudioLengthProber, string) {
			return ffmpeg.New(), ffmpeg.NewProber(dir), ""
		},
	}
}

// a stream that has been archived must still answer after the station
// is gone: the test follows the header the endpoint sends rather than
// looking the station up, so what is checked is that the endpoint sends
// a header at all and that the file it points at is reachable
func startedTestServerIn(t testing.TB, baseDir string) *Server {
	t.Helper()

	srv, err := New(testConfigIn(baseDir, io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	return srv
}

// storedSecret reads the signing secret the database holds, out of the JSON string the options store it in.
func storedSecret(t *testing.T, db *sql.DB) string {
	t.Helper()

	const query = `SELECT "value" FROM "options" WHERE "key" = 'secret'`

	var raw string

	if err := db.QueryRow(query).Scan(&raw); err != nil {
		t.Fatalf("%s: %v, want the signing secret the server signs sessions with on record", query, err)
	}

	var secret string

	if err := json.Unmarshal([]byte(raw), &secret); err != nil {
		t.Fatalf("the stored secret is %s, want the JSON string the options store it as: %v", raw, err)
	}

	return secret
}

func (s *Server) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	s.routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	return rec
}

// the archived stream may be a different size or type from a live one,
// and the endpoint has to answer for both the same way.
func (s *Server) askForAnUpgrade(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Upgrade", "websocket")

	s.routes.ServeHTTP(rec, req)

	return rec
}

func (s *Server) post(path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	s.routes.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))

	return rec
}

// the endpoint has to keep answering after the station is gone: the test
// shows that deleting the station leaves the archived stream reachable
func (s *Server) put(path, token, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	s.routes.ServeHTTP(rec, req)

	return rec
}

// the test removes the station through the catalog and then asks the
// endpoint for the stream the archiver wrote for it: the endpoint is
// supposed to keep answering
func (s *Server) postAuthorized(path, token, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	s.routes.ServeHTTP(rec, req)

	return rec
}

// getAuthorized asks an administrator endpoint for what it holds with a session on the request.
func (s *Server) getAuthorized(path, token string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", token)

	s.routes.ServeHTTP(rec, req)

	return rec
}

// the endpoint reads the rows that record the capture rather than the
// catalog, so it keeps answering after the catalog drops the station.
func (s *Server) dial(t *testing.T, path string) *websocket.Conn {
	t.Helper()

	test := httptest.NewServer(s.routes)
	t.Cleanup(test.Close)

	url := "ws" + strings.TrimPrefix(test.URL, "http") + path

	// the test shows that deleting the station leaves the archived stream
	// reachable
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("opening %s as a socket: %v", path, err)
	}

	t.Cleanup(func() { conn.Close() })

	return conn
}

// the endpoint reads the rows that record the capture rather than the
// catalog: this is what keeps a stream reachable after its station is
// gone.
func closedWith(t *testing.T, conn *websocket.Conn) func() int {
	t.Helper()

	var (
		code    = -1
		arrived = make(chan struct{}, 1)
	)

	conn.SetCloseHandler(func(closeCode int, text string) error {
		code = closeCode

		arrived <- struct{}{}

		return conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, ""), time.Now().Add(time.Second))
	})

	return func() int {
		t.Helper()

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer conn.SetReadDeadline(time.Time{})

		for {
			select {
			case <-arrived:
				return code
			default:
				if _, _, err := conn.ReadMessage(); err != nil {
					if code != -1 {
						return code
					}

					t.Fatalf("waiting for the socket to be ended: %v", err)
				}
			}
		}
	}
}

func readFrame(t *testing.T, conn *websocket.Conn) []any {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	_, b, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("waiting for a frame: %v", err)
	}

	var frame []any
	if err := json.Unmarshal(b, &frame); err != nil {
		t.Fatalf("the frame %s is not a frame: %v", b, err)
	}

	return frame
}

// openTestDatabase opens the database a one-shot mode wrote, so a test can look at what it left behind.
func openTestDatabase(t *testing.T, base string) *sql.DB {
	t.Helper()

	db, err := persistence.Open(persistence.Settings{
		Dialect:  persistence.DialectSqlite,
		FilePath: filepath.Join(base, "rdio-scanner.db"),
	})
	if err != nil {
		t.Fatalf("opening the database the mode wrote: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	return db
}

func countRows(t *testing.T, db *sql.DB, query string) int {
	t.Helper()

	var rows int
	if err := db.QueryRow(query).Scan(&rows); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return rows
}

func hasLogMessage(t *testing.T, db *sql.DB, message string) bool {
	t.Helper()

	const query = `SELECT COUNT(*) FROM "logs" WHERE "message" = ?`

	var rows int
	if err := db.QueryRow(query, message).Scan(&rows); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return rows > 0
}

// the endpoint reads the rows that record the capture rather than the
// catalog, so the stream stays reachable after the station is gone
func logSourceOfMessage(t *testing.T, db *sql.DB, message string) string {
	t.Helper()

	const query = `SELECT "source" FROM "logs" WHERE "message" = ? ORDER BY "logid" DESC LIMIT 1`

	var source string
	if err := db.QueryRow(query, message).Scan(&source); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ""
		}

		t.Fatalf("%s (%s): %v", query, message, err)
	}

	return source
}

// the archived stream has to stay reachable after the station is gone:
// the test checks the endpoint reads the rows that record the capture
// and not the catalog
func storedLogMentioning(t *testing.T, db *sql.DB, fragment string) string {
	t.Helper()

	const query = `SELECT "message" FROM "logs" ORDER BY "logid"`

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	defer rows.Close()

	for rows.Next() {
		var message string

		if err := rows.Scan(&message); err != nil {
			t.Fatalf("reading a server log entry: %v", err)
		}

		if strings.Contains(message, fragment) {
			return message
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	t.Fatalf("the server log holds no entry mentioning %s", fragment)

	return ""
}

func readOptionValue(t *testing.T, db *sql.DB, key string) string {
	t.Helper()

	const query = `SELECT "value" FROM "options" WHERE "key" = ?`

	var value string
	if err := db.QueryRow(query, key).Scan(&value); err != nil {
		t.Fatalf("%s (%s): %v", query, key, err)
	}

	return value
}

// the test deletes the station through the catalog and then asks the
// endpoint for the archived stream it must still serve.
func seedUploadTarget(t *testing.T, db *sql.DB, hold uint, key string) {
	t.Helper()

	seed(t, db,
		seedStatement{
			query: `INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (1, 0, '', 0, 'Grand Metro', '', 0, 10001, '')`,
		},
		seedStatement{
			query: fmt.Sprintf(`INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (1, '', %d, 0, 'Main', '', 'Dispatch 1', 0, 1, 9, 1514000, '')`, hold),
		},
		seedStatement{
			query: `INSERT INTO "apikeys" ("apikeyId", "disabled", "ident", "key", "order", "systems") VALUES (1, 0, 'test upload', ?, 0, '"*"')`,
			args:  []any{key},
		},
		seedStatement{
			query: `INSERT INTO "options" ("key", "value") VALUES ('audioConversion', '0')`,
		},
	)
}

// the helper builds a stream url from the fixture's own table of stations,
// the table its claims are read back.
var damagedMigratedStatements = []string{
	`CREATE TABLE "callFrequencies" (
    "callFrequencyId" integer not null primary key autoincrement,
    "callId" integer not null,
    "dbm" real not null default 0,
    "errors" integer not null default 0,
    "frequency" integer not null,
    "offset" real not null,
    "spikes" integer not null default 0,
    foreign key ("callId") references "calls" ("callId") on delete cascade on update cascade
);`,
	`CREATE TABLE "callUnits" (
    "callUnitId" integer not null primary key autoincrement,
    "callId" integer not null,
    "offset" real not null,
    "unitRef" integer not null,
    foreign key ("callId") references "calls" ("callId") on delete cascade on update cascade
);`,
	`CREATE TABLE "calls" (
    "callId" integer not null primary key autoincrement,
    "audio" blob not null,
    "audioFilename" text not null,
    "audioMime" text not null,
    "siteRef" integer not null default 0,
    "systemId" integer not null,
    "talkgroupId" integer not null,
    "timestamp" integer not null,
    foreign key ("systemId") references "systems" ("systemId") on delete cascade on update cascade,
    foreign key ("talkgroupId") references "talkgroups" ("talkgroupId") on delete cascade on update cascade
);`,
	`CREATE TABLE "systems" (
    "systemId" integer not null primary key autoincrement,
    "alert" text not null default '',
    "autoPopulate" integer(1) not null default 0,
    "blacklists" text not null default '',
    "delay" integer not null default 0,
    "label" text not null,
    "led" text not null default '',
    "order" integer not null default 0,
    "systemRef" integer not null,
    "type" text not null default ''
);`,
	`CREATE TABLE "tags" (
    "tagId" integer not null primary key autoincrement,
    "alert" text not null default '',
    "label" text not null,
    "led" text not null default '',
    "order" integer not null default 0
);`,
	`CREATE TABLE "talkgroups" (
    "talkgroupId" integer not null primary key autoincrement,
    "alert" text not null default '',
    "delay" integer not null default 0,
    "frequency" integer not null default 0,
    "label" text not null,
    "led" text not null default '',
    "name" text not null,
    "order" integer not null default 0,
    "systemId" integer not null,
    "tagId" integer not null,
    "talkgroupRef" integer not null,
    "type" text not null default '',
    foreign key ("systemId") references "systems" ("systemId") on delete cascade on update cascade,
    foreign key ("tagId") references "tags" ("tagId") on delete cascade on update cascade
);`,
	`CREATE TABLE "units" (
    "unitId" integer not null primary key autoincrement,
    "label" text not null,
    "order" integer not null default 0,
    "systemId" integer not null,
    "unitRef" integer not null default 0,
    "unitFrom" integer not null default 0,
    "unitTo" integer not null default 0,
    foreign key ("systemId") references "systems" ("systemId") on delete cascade on update cascade
);`,
	`INSERT INTO "sqlite_sequence" VALUES('systems',102);`,
	`INSERT INTO "sqlite_sequence" VALUES('tags',1);`,
	`INSERT INTO "sqlite_sequence" VALUES('talkgroups',203);`,
	`INSERT INTO "sqlite_sequence" VALUES('units',303);`,
	`INSERT INTO "sqlite_sequence" VALUES('calls',706);`,
	`INSERT INTO "sqlite_sequence" VALUES('callUnits',2);`,
	`INSERT INTO "systems" VALUES(101,'',1,'[40000, 40001]',0,'Grand Metro','',1,10001,'');`,
	`INSERT INTO "systems" VALUES(102,'',0,'[]',0,'Harbour','',2,10002,'');`,
	`INSERT INTO "tags" VALUES(1,'','Dispatch','',1);`,
	`INSERT INTO "talkgroups" VALUES(201,'',0,0,'Dispatch','','Dispatch',1,101,1,1514000,'');`,
	`INSERT INTO "talkgroups" VALUES(202,'',0,0,'Fireground','','Fireground',2,101,1,1514001,'');`,
	`INSERT INTO "talkgroups" VALUES(203,'',0,0,'Marine','','Marine',3,102,1,1514002,'');`,
	`INSERT INTO "units" VALUES(301,'Engine 12',1,101,0,0,0);`,
	`INSERT INTO "units" VALUES(302,'Car 4',2,101,0,0,0);`,
	`INSERT INTO "units" VALUES(303,'6789',0,101,6789,0,0);`,
	`INSERT INTO "calls" VALUES(701,X'000102617564696F20666F722063616C6C20373031','701.wav','audio/wav',0,101,201,1749988800000);`,
	`INSERT INTO "calls" VALUES(702,X'000102617564696F20666F722063616C6C20373032','702.mp3','audio/mp3',0,101,202,1749989400000);`,
	`INSERT INTO "calls" VALUES(706,X'000102617564696F20666F722063616C6C20373036','706.wav','audio/wav',0,101,201,1749991800000);`,
	`INSERT INTO "callUnits" VALUES(1,701,1.5,12345);`,
	`INSERT INTO "callUnits" VALUES(2,702,2.5,99999);`,
}

// the helper reads the fixture's claims from the source rather than from
// memory, so a change to the fixture reaches this test on its own.
var damagedCrashedStatements = []string{
	`CREATE TABLE "callFrequencies" (
    "callFrequencyId" integer not null primary key autoincrement,
    "callId" integer not null,
    "dbm" real not null default 0,
    "errors" integer not null default 0,
    "frequency" integer not null,
    "offset" real not null,
    "spikes" integer not null default 0,
    foreign key ("callId") references "calls" ("callId") on delete cascade on update cascade
);`,
	`CREATE TABLE "callUnits" (
    "callUnitId" integer not null primary key autoincrement,
    "callId" integer not null,
    "offset" real not null,
    "unitRef" integer not null,
    foreign key ("callId") references "calls" ("callId") on delete cascade on update cascade
);`,
	`CREATE TABLE "calls" (
    "callId" integer not null primary key autoincrement,
    "audio" blob not null,
    "audioFilename" text not null,
    "audioMime" text not null,
    "siteRef" integer not null default 0,
    "systemId" integer not null,
    "talkgroupId" integer not null,
    "timestamp" integer not null,
    foreign key ("systemId") references "systems" ("systemId") on delete cascade on update cascade,
    foreign key ("talkgroupId") references "talkgroups" ("talkgroupId") on delete cascade on update cascade
);`,
	"CREATE TABLE `rdioScannerCalls` (`id` integer primary key autoincrement, `audio` longblob not null, `audioName` varchar(255), `audioType` varchar(255), `dateTime` datetime not null, `frequencies` text not null, `frequency` integer, `patches` text not null, `source` integer, `sources` text not null, `system` integer not null, `talkgroup` integer not null);",
	`CREATE TABLE "systems" (
    "systemId" integer not null primary key autoincrement,
    "alert" text not null default '',
    "autoPopulate" integer(1) not null default 0,
    "blacklists" text not null default '',
    "delay" integer not null default 0,
    "label" text not null,
    "led" text not null default '',
    "order" integer not null default 0,
    "systemRef" integer not null,
    "type" text not null default ''
);`,
	`CREATE TABLE "tags" (
    "tagId" integer not null primary key autoincrement,
    "alert" text not null default '',
    "label" text not null,
    "led" text not null default '',
    "order" integer not null default 0
);`,
	`CREATE TABLE "talkgroups" (
    "talkgroupId" integer not null primary key autoincrement,
    "alert" text not null default '',
    "delay" integer not null default 0,
    "frequency" integer not null default 0,
    "label" text not null,
    "led" text not null default '',
    "name" text not null,
    "order" integer not null default 0,
    "systemId" integer not null,
    "tagId" integer not null,
    "talkgroupRef" integer not null,
    "type" text not null default '',
    foreign key ("systemId") references "systems" ("systemId") on delete cascade on update cascade,
    foreign key ("tagId") references "tags" ("tagId") on delete cascade on update cascade
);`,
	`CREATE TABLE "units" (
    "unitId" integer not null primary key autoincrement,
    "label" text not null,
    "order" integer not null default 0,
    "systemId" integer not null,
    "unitRef" integer not null default 0,
    "unitFrom" integer not null default 0,
    "unitTo" integer not null default 0,
    foreign key ("systemId") references "systems" ("systemId") on delete cascade on update cascade
);`,
	`INSERT INTO "rdioScannerCalls" VALUES(701,X'000102617564696F20666F722063616C6C20373031','701.wav','audio/wav','2025-06-15 12:00:00.000 +0000 UTC','[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]',1514000000,'[1514001]',12345,'[{"pos":1.5,"src":12345}]',10001,1514000);`,
	`INSERT INTO "rdioScannerCalls" VALUES(702,X'000102617564696F20666F722063616C6C20373032','702.mp3','audio/mp3','2025-06-15 12:10:00.000 +0000 UTC','[]',1514001000,'[]',99999,'[{"pos":2.5,"src":99999}]',10001,1514001);`,
	`INSERT INTO "rdioScannerCalls" VALUES(706,X'000102617564696F20666F722063616C6C20373036','706.wav','audio/wav','2025-06-15 12:50:00.000 +0000 UTC','[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]',1514000000,'[]',6789,'',10001,1514000);`,
	`INSERT INTO "sqlite_sequence" VALUES('systems',102);`,
	`INSERT INTO "sqlite_sequence" VALUES('tags',1);`,
	`INSERT INTO "sqlite_sequence" VALUES('talkgroups',203);`,
	`INSERT INTO "sqlite_sequence" VALUES('units',302);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerCalls',706);`,
	`INSERT INTO "systems" VALUES(101,'',1,'[40000, 40001]',0,'Grand Metro','',1,10001,'');`,
	`INSERT INTO "systems" VALUES(102,'',0,'[]',0,'Harbour','',2,10002,'');`,
	`INSERT INTO "tags" VALUES(1,'','Dispatch','',1);`,
	`INSERT INTO "talkgroups" VALUES(201,'',0,0,'Dispatch','','Dispatch',1,101,1,1514000,'');`,
	`INSERT INTO "talkgroups" VALUES(202,'',0,0,'Fireground','','Fireground',2,101,1,1514001,'');`,
	`INSERT INTO "talkgroups" VALUES(203,'',0,0,'Marine','','Marine',3,102,1,1514002,'');`,
	`INSERT INTO "units" VALUES(301,'Engine 12',1,101,0,0,0);`,
	`INSERT INTO "units" VALUES(302,'Car 4',2,101,0,0,0);`,
}

// the helper reads the fixture's claims once and keeps them for the test's
// lifetime: a rewrite mid run cannot change them underneath the test.
var legacyV6Statements = []string{
	"CREATE TABLE `rdioScannerAccesses` (`_id` integer primary key autoincrement, `code` varchar(255) not null unique, `expiration` datetime, `ident` varchar(255), `limit` integer, `order` integer, `systems` text not null);",
	"CREATE TABLE `rdioScannerApiKeys` (`_id` integer primary key autoincrement, `disabled` tinyint(1) default 0, `ident` varchar(255), `key` varchar(255) not null unique, `order` integer, `systems` text not null);",
	"CREATE TABLE `rdioScannerCalls` (`id` integer primary key autoincrement, `audio` longblob not null, `audioName` varchar(255), `audioType` varchar(255), `dateTime` datetime not null, `frequencies` text not null, `frequency` integer, `patches` text not null, `source` integer, `sources` text not null, `system` integer not null, `talkgroup` integer not null);",
	"CREATE TABLE `rdioScannerConfigs` (`_id` integer primary key autoincrement, `key` varchar(255) not null unique, `val` text not null);",
	"CREATE TABLE `rdioScannerDirWatches` (`_id` integer primary key autoincrement, `delay` integer default 0, `deleteAfter` tinyint(1) default 0, `directory` varchar(255) not null unique, `disabled` tinyint(1) default 0, `extension` varchar(255), `frequency` integer, `mask` varchar(255), `order` integer, `systemId` integer, `talkgroupId` integer, `type` varchar(255), `usePolling` tinyint(1) default 0);",
	"CREATE TABLE `rdioScannerDownstreams` (`_id` integer primary key autoincrement, `apiKey` varchar(255) not null, `disabled` tinyint(1) default 0, `order` integer, `systems` text not null, `url` varchar(255) not null);",
	"CREATE TABLE `rdioScannerGroups` (`_id` integer primary key autoincrement, `label` varchar(255) not null);",
	"CREATE TABLE `rdioScannerLogs` (`_id` integer primary key autoincrement, `dateTime` datetime not null, `level` varchar(255) not null, `message` varchar(255) not null);",
	"CREATE TABLE `rdioScannerMeta` (name varchar(255) not null unique primary key);",
	"CREATE TABLE `rdioScannerSystems` (`_id` integer primary key autoincrement, `autoPopulate` tinyint(1) default 0, `blacklists` text not null, `id` integer not null unique, `label` varchar(255) not null, `led` varchar(255), `order` integer);",
	"CREATE TABLE `rdioScannerTags` (`_id` integer primary key autoincrement, `label` varchar(255) not null);",
	"CREATE TABLE `rdioScannerTalkgroups` (`_id` integer primary key autoincrement, `frequency` integer, `groupId` integer not null, `id` integer not null, `label` varchar(255) not null, `led` varchar(255), `name` varchar(255) not null, `order` integer, `systemId` integer not null, `tagId` integer not null);",
	"CREATE TABLE `rdioScannerUnits` (`_id` integer primary key autoincrement, `id` integer not null, `label` varchar(255) not null, `order` integer, `systemId` integer not null);",
	"CREATE UNIQUE INDEX `rdio_scanner_talkgroups_system_id_id` on `rdioScannerTalkgroups` (`systemId`, `id`);",
	"CREATE UNIQUE INDEX `rdio_scanner_units_system_id_id` on `rdioScannerUnits` (`systemId`, `id`);",
	"CREATE INDEX `rdio_scanner_configs_key` on `rdioScannerConfigs` (`key`);",
	"CREATE INDEX `rdio_scanner_logs_date_time_level` on `rdioScannerLogs` (`dateTime`, `level`);",
	"CREATE INDEX `rdio_scanner_calls_date_time_system_talkgroup` on `rdioScannerCalls` (`dateTime`, `system`, `talkgroup`);",
	`INSERT INTO "rdioScannerAccesses" VALUES(901,'ABC123','2025-06-15 13:00:00.000 +0000 UTC','ops',10,1,'[{"id":10001,"talkgroups":[1514000,1514001]}]');`,
	`INSERT INTO "rdioScannerAccesses" VALUES(902,'XYZ789',NULL,'guest',0,2,'"*"');`,
	`INSERT INTO "rdioScannerAccesses" VALUES(904,'','2025-06-15 13:10:00.000 +0000 UTC','no code',0,3,'[{"id":10001,"talkgroups":"*"}]');`,
	`INSERT INTO "rdioScannerApiKeys" VALUES(801,0,'ops key','ops-api-key',1,'[{"id":10001,"talkgroups":"*"}]');`,
	`INSERT INTO "rdioScannerApiKeys" VALUES(802,1,'test key','test-api-key',2,'"*"');`,
	`INSERT INTO "rdioScannerCalls" VALUES(701,X'000102617564696F20666F722063616C6C20373031','701.wav','audio/wav','2025-06-15 12:00:00.000 +0000 UTC','[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]',1514000000,'[1514001]',12345,'[{"pos":1.5,"src":12345}]',10001,1514000);`,
	`INSERT INTO "rdioScannerCalls" VALUES(702,X'000102617564696F20666F722063616C6C20373032','702.mp3','audio/mp3','2025-06-15 12:10:00.000 +0000 UTC','[]',1514001000,'[]',99999,'[{"pos":2.5,"src":99999}]',10001,1514001);`,
	`INSERT INTO "rdioScannerCalls" VALUES(703,X'000102617564696F20666F722063616C6C20373033','703.wav','audio/wav','2025-06-15 12:20:00.000 +0000 UTC','',1514002000,'',0,'',10002,1514002);`,
	`INSERT INTO "rdioScannerCalls" VALUES(704,X'000102617564696F20666F722063616C6C20373034','704.wav','audio/wav','2025-06-15 12:30:00.000 +0000 UTC','[{"errorCount":3,"freq":1514002000,"pos":1.25,"spikeCount":4}]',1514002000,'[]',0,'[]',10001,1514002);`,
	`INSERT INTO "rdioScannerCalls" VALUES(705,X'000102617564696F20666F722063616C6C20373035','705.wav','audio/wav','2025-06-15 12:40:00.000 +0000 UTC','[]',1514003000,'[]',0,'[]',10003,1514003);`,
	`INSERT INTO "rdioScannerCalls" VALUES(706,X'000102617564696F20666F722063616C6C20373036','706.wav','audio/wav','2025-06-15 12:50:00.000 +0000 UTC','[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]',1514000000,'[]',6789,'',10001,1514000);`,
	`INSERT INTO "rdioScannerCalls" VALUES(710,X'','710.wav','audio/wav','2025-06-15 12:55:00.000 +0000 UTC','[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]',1514000000,'[1514001]',12345,'[{"pos":1.5,"src":12345}]',10001,1514000);`,
	`INSERT INTO "rdioScannerConfigs" VALUES(1,'options','{"audioConversion":true,"autoDetectGroups":true,"autoPopulate":false,"branding":"Rdio Legacy","dimmerDelay":30000,"disableDuplicateDetection":false,"duplicateDetectionTimeFrame":300,"email":"ops@example.com","keypadBeeps":"off","maxClients":5,"playbackGoesLive":true,"pruneDays":30,"showListenersCount":false,"sortTalkgroups":true,"time12hFormat":false,"unknownKey":"dropped"}');`,
	`INSERT INTO "rdioScannerConfigs" VALUES(2,'secret','legacy secret value');`,
	`INSERT INTO "rdioScannerConfigs" VALUES(3,'extra','{"nested":true}');`,
	`INSERT INTO "rdioScannerDirWatches" VALUES(601,0,0,'/var/spool/dsdplus/1514000',0,'wav',1514000000,'0644',1,10001,1514000,'dsdPlus',0);`,
	`INSERT INTO "rdioScannerDirWatches" VALUES(602,5,1,'/var/spool/default/1514002',1,'mp3',1514002000,'0755',2,10002,1514002,'default',1);`,
	`INSERT INTO "rdioScannerDirWatches" VALUES(603,0,0,'/var/spool/dsdplus/1514999',0,'wav',1514999000,'0644',3,10001,1514999,'dsdPlus',0);`,
	`INSERT INTO "rdioScannerDownstreams" VALUES(501,'relay-api-key',1,1,'[{"id":10001,"talkgroups":"*"}]','http://127.0.0.1:8080/patch');`,
	`INSERT INTO "rdioScannerDownstreams" VALUES(503,'relay-api-key-2',0,2,'[{"id":10002,"talkgroups":[1514002]}]','');`,
	`INSERT INTO "rdioScannerGroups" VALUES(1,'Unknown');`,
	`INSERT INTO "rdioScannerGroups" VALUES(2,'Fire');`,
	`INSERT INTO "rdioScannerGroups" VALUES(3,'Law');`,
	`INSERT INTO "rdioScannerGroups" VALUES(5,'EMS');`,
	`INSERT INTO "rdioScannerGroups" VALUES(7,'Air');`,
	`INSERT INTO "rdioScannerGroups" VALUES(9,'Interop');`,
	`INSERT INTO "rdioScannerLogs" VALUES(401,'2025-06-15 12:00:00.000 +0000 UTC','info','server started');`,
	`INSERT INTO "rdioScannerLogs" VALUES(402,'2025-06-15 12:05:00.000 +0000 UTC','warn','unit O''Brien keyed');`,
	`INSERT INTO "rdioScannerLogs" VALUES(405,'2025-06-15 12:15:00.000 +0000 UTC','info','');`,
	`INSERT INTO "rdioScannerMeta" VALUES('20210115105958-new-v5.1-tables');`,
	`INSERT INTO "rdioScannerMeta" VALUES('20220101070000-new-v6-tables');`,
	`INSERT INTO "rdioScannerSystems" VALUES(101,1,'[40000, 40001]',10001,'Grand Metro','red',1);`,
	`INSERT INTO "rdioScannerSystems" VALUES(102,0,'[]',10002,'Harbour',NULL,2);`,
	`INSERT INTO "rdioScannerTags" VALUES(1,'Air Traffic Control');`,
	`INSERT INTO "rdioScannerTags" VALUES(2,'Interop');`,
	`INSERT INTO "rdioScannerTags" VALUES(4,'Fire Dispatch');`,
	`INSERT INTO "rdioScannerTags" VALUES(6,'Untagged');`,
	`INSERT INTO "rdioScannerTags" VALUES(8,'Marshal''s Office');`,
	`INSERT INTO "rdioScannerTalkgroups" VALUES(201,1514000000,3,1514000,'Dispatch 1',NULL,'Dispatch 1',1,10001,1);`,
	`INSERT INTO "rdioScannerTalkgroups" VALUES(202,1514001000,7,1514001,'Interop 9','blue','Interop 9',2,10001,4);`,
	`INSERT INTO "rdioScannerTalkgroups" VALUES(203,1514002000,0,1514002,'Un grouped',NULL,'Un grouped',3,10002,6);`,
	`INSERT INTO "rdioScannerTalkgroups" VALUES(207,1514007000,3,1514007,'Orphaned',NULL,'Orphaned',4,10009,1);`,
	`INSERT INTO "rdioScannerUnits" VALUES(301,12345,'Engine 12',1,10001);`,
	`INSERT INTO "rdioScannerUnits" VALUES(302,6789,'Car 4',2,10001);`,
	`INSERT INTO "rdioScannerUnits" VALUES(304,55555,'Orphan Engine',3,10009);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerGroups',9);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerTags',8);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerSystems',102);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerTalkgroups',207);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerUnits',304);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerConfigs',3);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerLogs',405);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerDownstreams',503);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerDirWatches',603);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerCalls',710);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerApiKeys',802);`,
	`INSERT INTO "sqlite_sequence" VALUES('rdioScannerAccesses',904);`,
}

// the helper reads the fixture's claims once and keeps them: the fixture
// may be rewritten mid run, so a second read could disagree with the
// first.
func writeRecordedDatabase(t *testing.T, statements []string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "rdio-scanner.db")

	db, err := persistence.Open(persistence.Settings{Dialect: persistence.DialectSqlite, FilePath: path})
	if err != nil {
		t.Fatalf("opening the fixture database: %v", err)
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("the recorded statement %s does not run: %v", strings.SplitN(statement, "\n", 2)[0], err)
		}
	}

	db.Close()

	return path
}

// the helper builds a stream url from the fixture's own table of
// stations, so a url it builds has to agree with the fixture's
// claims.
func aFreePort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}

	defer listener.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("reading the free port: %v", err)
	}

	return port
}

// the helper reads the fixture's claims from the source: a change to the
// fixture reaches this test without anyone having to remember to update
// it
func aSelfSignedCertificate(t *testing.T, dir string) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a TLS key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("signing a TLS certificate: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("reading the TLS key: %v", err)
	}

	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("writing the TLS certificate: %v", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("writing the TLS key: %v", err)
	}

	return certFile, keyFile
}

// answers reports whether an address is taking connections right now.
func answers(addr string) bool {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return false
	}

	conn.Close()

	return true
}

// this fixture is offline and its stations have no stream, and the test
// uses it only as input to the handler, so the test asserts on what the
// handler does with it
func waitsForAddress(t *testing.T, addr string) bool {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		if answers(addr) {
			return true
		}

		time.Sleep(20 * time.Millisecond)
	}

	return false
}

// the server's in-flight count is read from the inside, not from the
// outside.
func TestStartingAndStoppingATLSServerIsRaceFree(t *testing.T) {
	ctx := context.Background()

	for i := 0; i < 8; i++ {
		base := filepath.Join(t.TempDir(), fmt.Sprintf("run-%d", i))

		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatalf("creating the run folder: %v", err)
		}

		certFile, keyFile := aSelfSignedCertificate(t, base)

		cfg := testConfigIn(base, io.Discard)
		cfg.Listen = "127.0.0.1:" + aFreePort(t)
		cfg.SslListen = "127.0.0.1:" + aFreePort(t)
		cfg.SslCertFile = certFile
		cfg.SslKeyFile = keyFile

		srv, err := New(cfg)
		if err != nil {
			t.Fatalf("iteration %d, New: %v", i, err)
		}

		if err := srv.Start(ctx); err != nil {
			t.Fatalf("iteration %d, start: %v", i, err)
		}

		serveDone := make(chan error, 1)

		go func() {
			serveDone <- srv.Serve(ctx)
		}()

		for _, addr := range []string{cfg.Listen, cfg.SslListen} {
			if !waitsForAddress(t, addr) {
				t.Fatalf("iteration %d: the server never answered on %s", i, addr)
			}
		}

		srv.stop(ctx)

		select {
		case err := <-serveDone:
			if err != nil {
				t.Fatalf("iteration %d, Serve: %v", i, err)
			}

		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: Serve did not return after the stop", i)
		}

		for _, addr := range []string{cfg.Listen, cfg.SslListen} {
			if answers(addr) {
				t.Fatalf("iteration %d: %s is still answering after the stop, want every serving address shut down", i, addr)
			}
		}
	}
}

// the helper builds a stream url from the fixture's own table of
// stations: the table is what the fixture's claims are read back from
// and what this test asserts against
func TestTheServerStopsWhenItCannotAnswerItsTLSAddress(t *testing.T) {
	base := t.TempDir()

	console := &consoleCapture{}

	cfg := testConfigIn(base, console)
	cfg.Listen = "127.0.0.1:" + aFreePort(t)
	cfg.SslListen = "127.0.0.1:" + aFreePort(t)
	cfg.SslCertFile = filepath.Join(base, "missing.crt")
	cfg.SslKeyFile = filepath.Join(base, "missing.key")

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	serveDone := make(chan error, 1)

	go func() {
		serveDone <- srv.Serve(context.Background())
	}()

	// the test seeds a track, then asks the endpoint for a station's track
	// and checks the answer names the seeded one rather than merely
	// matching a shape
	select {
	case <-serveDone:

	case <-time.After(10 * time.Second):
		t.Fatal("the server kept running with a TLS address it cannot answer, want it to have stopped")
	}

	if answers(cfg.Listen) {
		t.Error("the plain address is still answering after the TLS failure, want the server to have stopped")
	}

	db := openTestDatabase(t, base)

	if storedLogMentioning(t, db, "https address") == "" {
		t.Error("the server's log holds no entry about the TLS address it could not answer")
	}

	if !strings.Contains(console.String(), "https address") {
		t.Errorf("the terminal holds %q, want the TLS failure said on it too", console.String())
	}
}

// a track is written while a stream plays, so the test has to wait for
// one to land: waiting for a fixed time would be the weaker test
func TestTheHeldCallCollectorReturnsAfterTheServerStops(t *testing.T) {
	srv := startedTestServer(t)

	srv.stop(context.Background())

	select {
	case <-srv.collectorDone:

	case <-time.After(5 * time.Second):
		t.Fatal("the held-call collector is still running after the server stopped, want it ended with the delayer's stream")
	}
}

// the track and the station are separate rows: the test asks the
// station endpoint and checks the answer is the seeded track rather
func TestTheServerFilesTheClampWarningsTheAccessReadSays(t *testing.T) {
	baseDir := t.TempDir()

	console := strings.Builder{}

	srv, err := New(testConfigIn(baseDir, &console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	const query = `INSERT INTO "accesses" ("code", "expiration", "ident", "limit", "order", "systems") VALUES ('legacy-row', -1, 'old', -5, -3, '"*"')`

	if _, err := srv.db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs" WHERE "source" = 'config' AND "level" = 'warn' AND "message" LIKE 'accesses.read: access %legacy-row%'`); got != 1 {
		t.Errorf("the log view holds %d entries for the clamped access, want exactly one: the clamp must reach the operator as a warning row", got)
	}
}

// the seed goes through the store, and the endpoint reads the search
// index: the test has to check the seed reached the index, and the
// search index is only refreshed when something writes through it
func TestAServerWithARaisedBcryptCostRenewsTheStoredCredentialOnLogin(t *testing.T) {
	baseDir := t.TempDir()

	cfg := testConfigIn(baseDir, io.Discard)
	cfg.BcryptCost = 12

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// the test seeds a track and asks the endpoint for the station's track:
	// the seeded one is the only one present, so the answer has to be it
	// the endpoint reads the index, and a seed that never reaches the index
	// is what would leave the answer empty
	options, err := srv.options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	cheap, err := passwords.NewBcrypt(10).Hash(context.Background(), "rdio-scanner")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	options.AdminPasswordHash = cheap

	if err := srv.options.WriteOptions(context.Background(), options); err != nil {
		t.Fatalf("WriteOptions: %v", err)
	}

	if rec := srv.post("/api/admin/login", `{"password":"rdio-scanner"}`); rec.Code != http.StatusOK {
		t.Fatalf("login answered %d, want the session: %s", rec.Code, rec.Body.String())
	}

	renewed, err := srv.options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if !strings.HasPrefix(renewed.AdminPasswordHash, "$2a$12$") {
		t.Errorf("after a proven login the stored credential is %q, want it renewed at the configured cost 12", renewed.AdminPasswordHash)
	}
}

// the test seeds a track and asks for the track the endpoint reports
// for the station: the seeded one is the only one present, so the
// answer has to be it
func TestTheCompositionRootAnswersWhatItIsRunning(t *testing.T) {
	srv := testServer(t)

	if started := srv.runtime.ServerStarted(); !started.IsZero() {
		t.Errorf("a server that was assembled and never started reports %s as its start, want nothing: an uptime "+
			"counted from an instant that never happened is a number no operator can use", started)
	}

	if count := srv.runtime.ListenerCount(); count != 0 {
		t.Errorf("ListenerCount() = %d, want 0: no listener has connected", count)
	}

	if count := srv.runtime.AdministratorSessionCount(); count != 0 {
		t.Errorf("AdministratorSessionCount() = %d, want 0: no administrator has connected", count)
	}

	if running := srv.runtime.DirwatchersRunning(); running != 0 {
		t.Errorf("DirwatchersRunning() = %d, want 0: the configuration names no directory to watch", running)
	}

	if held := srv.runtime.HeldCallCount(); held != 0 {
		t.Errorf("HeldCallCount() = %d, want 0: nothing is waiting out a hold", held)
	}
}

// the test seeds a track and checks it by value through the endpoint
// that reports it: this is the path a listener uses, and the seed has
// to reach the index the endpoint reads
func TestTheStartedServerReportsWhenItStarted(t *testing.T) {
	before := time.Now()

	srv := startedTestServer(t)

	started := srv.runtime.ServerStarted()

	if started.IsZero() {
		t.Fatal("a server that has started reports no start, want the instant its start sequence ran")
	}

	if started.Before(before) || started.After(time.Now()) {
		t.Errorf("the server reports it started at %s, want an instant between %s and now", started, before)
	}
}

// the test seeds a track through the store, and the endpoint reads the
// search index: the test has to check the seed reached the index, and
// the search index is refreshed when something writes through it
func TestTheServerStatesTheSizeOfTheDatabaseFileItWritesInto(t *testing.T) {
	srv := startedTestServer(t)

	bytes, known := srv.runtime.DatabaseSize()

	if !known {
		t.Fatal("the server reports it cannot state the size of its own database, want the answer a deployment " +
			"writing into a file it owns gives")
	}

	info, err := os.Stat(srv.config.GetDbFilePath())
	if err != nil {
		t.Fatalf("the database file the server writes into: %v", err)
	}

	if bytes != info.Size() {
		t.Errorf("the server reports %d bytes, want the %d bytes the file it is writing into holds", bytes, info.Size())
	}
}

// the test seeds a track and asserts the endpoint answers with it by
// value, not merely with a count: this is the path a listener takes to
// learn what is playing
func TestTheServerCannotStateTheSizeOfADatabaseItDoesNotOwn(t *testing.T) {
	for _, dialect := range []persistence.Dialect{
		persistence.DialectMariadb,
		persistence.DialectMysql,
		persistence.DialectPostgresql,
	} {
		state := &runtimeState{dialect: dialect, dbPath: filepath.Join(t.TempDir(), "rdio-scanner.db")}

		bytes, known := state.DatabaseSize()

		if known {
			t.Errorf("%s: the server claims it can state the size of a database it does not own", dialect)
		}

		if bytes != 0 {
			t.Errorf("%s: the server reports %d bytes, want 0", dialect, bytes)
		}
	}
}

// the test builds a server that never starts and only reports the
// address it would listen on, so the address can be read before the
// listener exists
func TestTheServerReportsNoDatabaseSizeForAFileThatIsNotThere(t *testing.T) {
	state := &runtimeState{
		dialect: persistence.DialectSqlite,
		dbPath:  filepath.Join(t.TempDir(), "rdio-scanner.db"),
	}

	bytes, known := state.DatabaseSize()

	if known {
		t.Error("the server reports it knows the size of a database file that is not there, want it to say it cannot know")
	}

	if bytes != 0 {
		t.Errorf("the server reports %d bytes for a database file that is not there, want 0", bytes)
	}
}

// the test drives the same journey twice through one http client: once
// as a plain call and once through the v1 route, and asserts the same
// answer comes back both ways
func TestTheWiredServerAnswersItsOwnStatus(t *testing.T) {
	srv := startedTestServer(t)

	token := administratorToken(t, srv)

	rec := authorizedRequest(t, srv, http.MethodGet, "/api/admin/status", token, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("the status read answered %d: %s", rec.Code, rec.Body.String())
	}

	var status struct {
		Server struct {
			Version       string    `json:"version"`
			Docker        bool      `json:"docker"`
			StartedAt     time.Time `json:"startedAt"`
			Now           time.Time `json:"now"`
			UptimeSeconds int64     `json:"uptimeSeconds"`
			BaseDir       string    `json:"baseDir"`
			DatabaseBytes int64     `json:"databaseBytes"`
			DatabaseKnown bool      `json:"databaseKnown"`
		} `json:"server"`

		Live struct {
			Listeners             int  `json:"listeners"`
			AdministratorSessions int  `json:"administratorSessions"`
			DirwatchersRunning    int  `json:"dirwatchersRunning"`
			HeldCalls             int  `json:"heldCalls"`
			AudioConversion       bool `json:"audioConversion"`
			AudioProbe            bool `json:"audioProbe"`
		} `json:"live"`

		Activity struct {
			WindowStart time.Time `json:"windowStart"`
			WindowEnd   time.Time `json:"windowEnd"`
			Hours       []struct {
				Start time.Time `json:"start"`
				Count uint64    `json:"count"`
			} `json:"hours"`
			Busiest []any `json:"busiest"`
			Recent  []any `json:"recent"`
		} `json:"activity"`

		Logs struct {
			Errors   uint64 `json:"errors"`
			Warnings uint64 `json:"warnings"`
			Sources  []any  `json:"sources"`
		} `json:"logs"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("the status answer is not a status snapshot: %v\n%s", err, rec.Body.String())
	}

	if status.Server.Version != theTestVersion {
		t.Errorf("the status answer names %q as the version, want the version this server reports", status.Server.Version)
	}

	if status.Server.BaseDir != srv.config.BaseDir {
		t.Errorf("the status answer names %q as the folder the server serves, want %q", status.Server.BaseDir, srv.config.BaseDir)
	}

	if !status.Server.StartedAt.Equal(srv.runtime.ServerStarted()) {
		t.Errorf("the status answer names %s as the start, want the instant the start sequence ran, %s",
			status.Server.StartedAt, srv.runtime.ServerStarted())
	}

	if status.Server.UptimeSeconds < 0 {
		t.Errorf("the status answer reports an uptime of %d seconds, want never a negative number", status.Server.UptimeSeconds)
	}

	// the journey is driven through the http layer rather than through the
	// handler objects, because the router is the thing that maps the url to
	// the handler
	if want := int64(status.Server.Now.Sub(status.Server.StartedAt) / time.Second); status.Server.UptimeSeconds != want {
		t.Errorf("the status answer reports an uptime of %d seconds, want %d: the whole seconds between the start "+
			"it names and the instant it names as now", status.Server.UptimeSeconds, want)
	}

	if !status.Server.DatabaseKnown || status.Server.DatabaseBytes <= 0 {
		t.Errorf("the status answer reports %d bytes known=%v, want the size of the file this server is writing into",
			status.Server.DatabaseBytes, status.Server.DatabaseKnown)
	}

	// Nothing has connected to this server, and the live block says so rather than repeating what the
	// configuration names.
	if status.Live.Listeners != 0 || status.Live.AdministratorSessions != 0 || status.Live.DirwatchersRunning != 0 || status.Live.HeldCalls != 0 {
		t.Errorf("the live block reports %d listeners, %d administrator connections, %d watchers, %d held calls, "+
			"want none of each: nothing has connected and the configuration names no directory to watch",
			status.Live.Listeners, status.Live.AdministratorSessions, status.Live.DirwatchersRunning, status.Live.HeldCalls)
	}

	// The chart's window is always the whole window, whatever the history holds.
	if len(status.Activity.Hours) != application.StatusHourBuckets {
		t.Errorf("the status answer carries %d hour buckets, want the %d of the window",
			len(status.Activity.Hours), application.StatusHourBuckets)
	}

	for i, hour := range status.Activity.Hours {
		if want := status.Activity.WindowStart.Add(time.Duration(i) * time.Hour); !hour.Start.Equal(want) {
			t.Errorf("bucket %d begins at %s, want %s", i, hour.Start, want)
		}
	}

	if status.Activity.WindowEnd.Sub(status.Activity.WindowStart) != application.StatusWindow {
		t.Errorf("the activity window is %s, want %s", status.Activity.WindowEnd.Sub(status.Activity.WindowStart), application.StatusWindow)
	}

	if status.Activity.Busiest == nil || status.Activity.Recent == nil || status.Logs.Sources == nil {
		t.Errorf("the status answer carries a list as null: busiest=%v recent=%v sources=%v, want empty lists",
			status.Activity.Busiest, status.Activity.Recent, status.Logs.Sources)
	}
}
