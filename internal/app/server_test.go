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

package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/ffmpeg"
	"rdio-scanner/internal/infrastructure/persistence"
)

// A server pointed at a database it has never seen starts that database, reads its configuration out of
// it, and answers the paths the legacy server answers.
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

// The default credential is what a fresh server accepts, and it is not what a fresh server stores. The
// legacy server generates a hash of it at every startup and writes nothing.
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

// Starting the database is the legacy server's `NewDatabase`: the schema, and then the groups and tags a
// fresh deployment is expected to have.
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

// The legacy server reads its configuration out of the database before it starts, and a configuration it
// cannot read stops the server from starting at all. A server that started anyway would answer requests
// with a configuration it had not loaded.
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

// The path the web application is served from is also the path a listener connects to, and the upgrade
// header decides which. This is the one place in the server where a request can be either.
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

// The two applications the binary carries are served from two paths, and the paths are the whole difference
// between them: the scanner application from the root, the administration application from /admin/. The
// handlers here say which application they are, because a checkout that has never built the client embeds
// nothing but a placeholder in each bundle, and a routing decision is not something a missing bundle should
// be able to hide.
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

// The listener endpoint is kept on both application routes. A native application opens its connection on
// whatever path it was on, and the upgrade header — not the path — is what decides.
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

// `/admin` is deliberately not registered. A subtree pattern redirects a request for the subtree root by
// adding the trailing slash, and that redirect is what makes every relative URL in the admin shell resolve
// inside the admin bundle instead of at the root of the server.
//
// The status is 307, not 301: Go's ServeMux redirects a cleaned path and a subtree root with a Temporary
// Redirect, and the body it writes is its own redirect notice rather than an application's shell.
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

// The administrator's API lives under /api/admin, which is a different subtree from the administration
// application's /admin/. Registering the application is what could have disturbed the API, so this pins
// that it does not.
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

// A relative path under the admin subtree is sanitized by the mux before any handler sees it, and what
// survives is a path at the root of the server. This records that: the request is redirected to the cleaned
// path, and the cleaned path belongs to the scanner application. Nothing reaches outside a bundle, because
// no handler was given the path at all.
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

// The routing the tests above describe is the routing the started server installs. This is the one case
// that goes through the real routes, and it is the case that does not depend on a client having been built:
// the redirect happens before either bundle is read.
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

// mark returns a handler that answers with the name of the application it stands for, which is how these
// routing tests say which handler a request reached without building a client.
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

// A listener that connects to the path the web application is served from is taken over by the listener
// endpoint, and is greeted the way the legacy server greets it.
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

	// The legacy server says branding and email only when the configuration carries them, so the greeting a
	// fresh server gives carries the version and nothing else.
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

// The administrator's configuration path answers both an endpoint and a socket, and the socket is the one
// the web application holds open.
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
		t.Errorf("the socket was ended with close code %d, want the 1000 the web application reads as "+
			"an unauthorized connection", code)
	}
}

// The legacy server prunes once an hour, reads the retention it is configured with at the moment it runs,
// and starts nothing else on that timer.
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

// A retention pass that fails is the failure the schedule exists for: a server whose retention has been
// broken for a month fills up, and the only thing its operator can look at is the log view. The pass names
// what it was trying to do, and the next hour is still scheduled.
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

// The listening address the configuration names is split the way the legacy server splits it: a name with
// no port gets 3000, and an address with nothing in it means every interface.
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

// The lines a server says before it starts listening are how an operator finds it, and the legacy server
// says them in a particular order and with a particular wording.
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

// An address on the default http port is announced without the port, which is how the legacy server
// announces a server a browser reaches without a port number.
func TestServerAnnouncesAnAddressOnTheDefaultPortWithoutThePort(t *testing.T) {
	console := &bytes.Buffer{}

	// The address is the one a browser reaches without typing a port, which is what this test is about;
	// everything else about the server is the configuration every test server is built with.
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

// The base folder is where the server writes, and the legacy server names it when it was told one. The
// console copy is the rendering of the entry below, not a separate line the startup prints.
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

// TestServerRecordsTheBaseFolderItWasGiven is the same fact in the log view. Until now the folder the server
// was told to use existed only on the terminal at the moment the server started: an operator diagnosing a
// recording that never appeared could not find out which folder the server had been given unless they had
// been watching it come up.
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

// The server's own startup notice goes into the log view, not to the terminal: it is the first thing an
// administrator sees when they open the log.
func TestServerRecordsThatItStartedInTheLogView(t *testing.T) {
	srv := startedTestServer(t)

	if !hasLogMessage(t, srv.db, "server started") {
		t.Errorf("the log view does not record that the server started")
	}

	if got := logSourceOfMessage(t, srv.db, "server started"); got != "server" {
		t.Errorf("the startup notice is filed under %q, want %q: the log view filters by the part of the server that wrote an entry", got, "server")
	}
}

// The notice about the ffmpeg this server converts with is the one an operator needs when a recording was
// never converted, and it has to be findable under ffmpeg rather than by remembering which line it was.
func TestTheFfmpegNoticeIsFiledUnderFfmpeg(t *testing.T) {
	const notice = "the server cannot prepare /data/ffmpeg for its ffmpeg"

	srv := testServer(t)

	// What the wiring produces when the server carries an ffmpeg it could not write out — see
	// TestAServerThatCannotWriteOutItsFfmpegSaysWhyAndConvertsWithTheHosts.
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

// The path the web application is served from answers an ordinary request as the application, and not as a
// socket. In a checkout the bundle the binary carries has not been built — the target that builds it is
// T055 — so what the application answers is the 404 it gives for a file it does not have. That is the
// application answering: a request handed to the listener endpoint would have been answered as an upgrade,
// and a request that reached nothing would not have been answered by this path at all.
// An ordinary path is answered by the web application, the same way the application root is answered: with the
// application shell when the binary carries one, and with nothing when it does not. A binary built from a
// repository that has never run `make webapp-ng-server` carries only the placeholder that keeps the embedded
// directory in the repository, so this compares the two answers instead of naming one.
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

// The API paths are answered by the API and not by the application the binary carries, which is what a
// single-page catch-all must not swallow.
func TestServerDoesNotServeTheWebApplicationOverTheApiPaths(t *testing.T) {
	srv := startedTestServer(t)

	rec := srv.get("/api/admin/logs")

	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("the log endpoint was answered with the web application")
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

// TestTheServerRecordsThatItStopped is the end of the timeline. The legacy server said `terminated` on a
// terminal and wrote nothing, so a log view that records everything else could not say when the server had
// stopped — which is the first question about any gap in it.
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

// TestStoppingTheServerRecordsTheConnectionsItEnds is the hole at the end of that timeline. Shutting an
// address down does not wait for a connection the server has taken over: the moment the listener path or the
// administrator's configuration path hands a connection to the server, the address that upgraded it stops
// tracking it. A server that stops therefore reaches the point where it closes the database the log lives in
// with connections still open, and the entries that those connections leaving are — the last things a running
// server writes — arrive at a log that has just been closed. What an operator sees is a connection that began
// and never ended, and what the database holds is a write that got half way through and left a rollback
// journal behind.
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

	// Both connections are opened and left open: nothing in this test closes them, because the server being
	// asked to stop while it is still holding them is the situation.
	conn := srv.dial(t, "/")

	listenerClosed := closedWith(t, conn)

	sendFrame(t, conn, `["CFG"]`)

	waitForLogMentioning(t, srv.db, "a listener joined from 127.0.0.1")

	admin := srv.dial(t, "/api/admin/config")

	adminClosed := closedWith(t, admin)

	waitForLogMentioning(t, srv.db, "an administrator connected from 127.0.0.1")

	srv.stop(context.Background())

	// The stop leaves nothing owed to its own scheduler. Each departure above is a listener change, and a
	// change schedules a count of listeners to be written five seconds later — into the database the stop has
	// just closed, which is a write that cannot finish.
	if srv.listenerCounts.Pending() {
		t.Error("the server stopped while it still owed a listener-count report, which is a report it would " +
			"write into a database it has already closed")
	}

	// The connections are ended by the stop itself, which is what lets a client reach the next server
	// instead of waiting out a read timeout on one that has already gone.
	if code := listenerClosed(); code != websocket.CloseGoingAway {
		t.Errorf("the listener connection was ended with close code %d, want %d: a server that is stopping is "+
			"going away", code, websocket.CloseGoingAway)
	}

	if code := adminClosed(); code != websocket.CloseNormalClosure {
		t.Errorf("the administrator connection was ended with close code %d, want the %d the web application "+
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

	// And it recorded them before it said it stopped, which is the only ordering that makes the departure
	// writable at all: the stop is where the database is closed.
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

// refusesToClose is a listener that reports a failure when it is asked to close. It is the thing a
// graceful shutdown can genuinely fail at: the shutdown closes the listeners it was given, and a listener
// that cannot be closed is a shutdown step that did not complete.
type refusesToClose struct {
	net.Listener
}

var errListenerWouldNotClose = errors.New("the listener would not close")

func (l refusesToClose) Close() error {
	// The socket underneath is closed, so the server's accept loop ends. What fails is the closing of the
	// address the server was answering on — a listener that lets go of the connection it holds and then
	// reports that it could not finish letting go.
	l.Listener.Close()

	return errListenerWouldNotClose
}

// TestAShutdownStepThatDidNotCompleteSaysSo is the shutdown that could not finish: an address the server
// was answering on, and a listener that would not close. Until now the only trace of that was a raw error
// on a terminal, and nothing at all on a deployment running as a service.
func TestAShutdownStepThatDidNotCompleteSaysSo(t *testing.T) {
	base := t.TempDir()

	console := &consoleCapture{}

	srv, err := New(testConfigIn(base, console))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// The listener is the test's own, on a port the operating system chose, so the test never has to hope
	// a port is free. Handing it to the server's own Serve is what makes it one of the listeners the
	// server's shutdown has to close.
	address, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("opening a listener: %v", err)
	}

	serving := srv.httpServer(address.Addr().String())

	go serving.Serve(refusesToClose{Listener: address})

	// The server is only asked to stop once it has already answered something on that address, which is
	// how the test knows the listener is one the shutdown knows about. Without this, the test would be
	// racing the server's own bookkeeping, and a shutdown with nothing to close is a shutdown that
	// succeeds.
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

// The stop is what says the server stopped, and two goroutines can ask for it at the same moment: the
// one that was waiting for the operator's interrupt, and the one that was serving and is unblocked by it.
// The entry has to be written once, and the goroutine that arrives second has to wait for the one that got
// there first — otherwise the process ends while the first stop is still saying so, and an operator who
// interrupted the server sees a server that stopped without ever saying it did.
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

// The HTTP server the legacy server builds throws away everything the HTTP layer itself reports. That is
// quirk Q52, repaired here: the lines reach the console the operator is reading.
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
		t.Fatalf("the HTTP server was given no logger, which is quirk Q52: everything the HTTP layer reports " +
			"about itself is thrown away")
	}

	serving.ErrorLog.Print("a handshake went wrong")

	if !strings.Contains(console.String(), "a handshake went wrong") {
		t.Errorf("the HTTP layer's own report did not reach the console, which holds:\n%s", console.String())
	}
}

// The composition root is where the legacy server's startup rules live: a server whose stored
// configuration has no administrator credential accepts the one the server ships with, and a server whose
// stored configuration has no signing secret makes one and puts it on record.
func TestServerReadsItsConfigurationBeforeItStartsAnswering(t *testing.T) {
	srv := startedTestServer(t)

	options, err := srv.options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if options.AdminPasswordHash == "" {
		t.Fatalf("the server is running with no administrator credential at all")
	}

	// A deployment that has never saved its configuration has no signing secret on record. The legacy
	// server signed with nothing in that case, and with a different random key on every start when a
	// stored secret was unusable. The startup step makes a secret once and puts it on record, so the key
	// in use is the key on record. See
	// docs/specs/FEAT-004-legacy-defect-repairs/decision-signing-secret-persisted.md.
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

// TestServerReplacesAStoredSecretItCannotSignWith is the legacy database's case: the stored secret is a
// short string an old version wrote, it cannot sign a session, and the replacement is written down rather
// than used once and forgotten.
func TestServerReplacesAStoredSecretItCannotSignWith(t *testing.T) {
	srv := testServer(t)

	// A fresh database has no option rows at all until an administrator saves the configuration, so the
	// legacy shape has to be written by hand: a `secret` row holding a short string.
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

// TestANewSigningSecretIsFiledAndSaidOutLoud is the line the legacy server printed on a terminal and never
// stored. A secret that changes ends every administrator session signed with the previous one, so this entry
// is what turns "everybody was logged out at six thirty" into a question the log view can answer.
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

// TestServerKeepsTheSigningSecretItPutOnRecord is what the repair is for: the second start on the same
// database finds the key the first one made, and renewing it in place writes no further row.
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

	// The second start found the secret already on record, so it has nothing to report: the entry is
	// written by the step that makes a secret, not by the step that looks for one.
	const mentions = `SELECT COUNT(*) FROM "logs" WHERE "message" = '` +
		"the server generated a new administrator signing secret, which ends every session that was open before it" +
		`'`

	if got := countRows(t, second.db, mentions); got != 1 {
		t.Errorf("the log view mentions a new signing secret %d times after two starts, want once: the first "+
			"start made the secret and the second found it already there", got)
	}
}

// TestServerReportsTheDamageALegacyMigrationLeftBehind is the operator's half of the startup repair:
// the report the migration pass writes reaches the console the operator is reading, the damaged rows
// are left exactly as they were, and the server starts anyway — a report is not a reason to refuse to
// run. See docs/specs/FEAT-004-legacy-defect-repairs/decision-migrated-database-repair.md.
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

	// Reported, not rewritten: the two units are left with no reference, because the numbers they were
	// migrated from are not in this database.
	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "units" WHERE "unitId" IN (301, 302) AND "unitRef" = 0`); rows != 2 {
		t.Errorf("the startup report left %d of the two units it reported on without a reference, want both "+
			"left exactly as they were", rows)
	}

	// And the report is in the log view as well as on the terminal. A deployment running as a service has
	// no terminal, and this is the one thing an operator of a legacy deployment needs told about its units
	// on every start — which is also why it is filed as a problem and not a notice.
	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs" WHERE "source" = 'database' AND "message" LIKE 'the migration left%'`); got != 5 {
		t.Errorf("the log view holds %d entries from the damage report, want the five lines the startup said: "+
			"two summaries and one per damaged unit", got)
	}

	// The damage report is the wording this feature changed the most: every one of these lines used to
	// begin with `migration.damage:`.
	assertEveryServerLogEntry(t, srv.db)
}

// TestTheMigrationPassFilesWhatItSaid is the migration half of CA-009: the lines an upgrade says while it
// runs exist only on the terminal today, which is no trace at all for the deployment running as a service
// and no trace the morning after for anybody else. They are in the log view as well, under the part of the
// server that wrote them — and they are written *after* the pass has finished carrying the deployment's own
// history across, so the ids this server's entries take are never the ids that history arrived with.
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

	// Still said out loud: the pass announces each v6 table it found, and reports what it could not carry
	// or could not link, exactly as it did before this feature — and, since FEAT-011, the rows it archived.
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

	// The order is the safety of the whole arrangement: the v6 log rows are re-inserted with the ids they
	// were written with, and an entry this server wrote during the pass could be sitting on one of them.
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

	// FEAT-011: every incoherence the pass finds in the deployment is filed the same way, as a problem, in
	// the order the pass says it. A deployment running as a service has no terminal, and these are the
	// lines that tell its operator the upgrade left something behind and where to go and look at it.
	for _, want := range []struct {
		level, message string
	}{
		// The talkgroup in no group is carried, and the group its v6 row named is not: the link is not
		// written, and the group it named is said rather than left to whatever the backend called the
		// refusal.
		{"warn", `the migration carried talkgroup 203 "Un grouped" without its group: group 0 is not in this database`},
		{"warn", "the migration could not carry 1 talkgroup: it is kept in the migrationArchive table"},
		{"warn", `the migration could not carry talkgroup 207 "Orphaned": its system 10009 is not in this database`},
		{"warn", "the migration could not carry 1 unit: it is kept in the migrationArchive table"},
		{"warn", `the migration could not carry unit 304 "Orphan Engine": its system 10009 is not in this database`},
		{"warn", "the migration could not carry 1 config row: it is kept in the migrationArchive table"},
		{"warn", `the migration could not carry the config row "options": it holds values this server cannot carry: "autoDetectGroups" (v8 has no option of that name), "unknownKey" (v8 has no option of that name)`},
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

	// The migration is the one writer whose wording this feature had to change rather than merely label:
	// its lines began with `migration.damage:` and `migration.migrateGroups:`. This is where the wording
	// guard reaches them.
	assertEveryServerLogEntry(t, srv.db)
}

// TestTheMigrationFilesTheLogRowsItCarriesAcross is REQ-004 for the deployment that upgrades through this
// server instead of through the legacy one. Its history arrives through the migration pass, and a row that
// arrives with nothing in the source column is a row the operator cannot filter for: the prefix the legacy
// server wrote is the only thing that says what wrote it, and it has to be read here the same way the
// add-column step reads it.
func TestTheMigrationFilesTheLogRowsItCarriesAcross(t *testing.T) {
	baseDir := t.TempDir()

	path := filepath.Join(baseDir, "rdio-scanner.db")

	fixture := writeRecordedDatabase(t, legacyV6Statements)

	if err := os.Rename(fixture, path); err != nil {
		t.Fatalf("placing the fixture where the server opens it: %v", err)
	}

	// Two rows the legacy server wrote: one with a prefix that names a part of the server, and one whose
	// message meant nothing recognisable.
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

// TestAnUpgradedV663DeploymentAnswersItsAdministratorAndItsListeners is the upgrade journey on the server a
// deployment actually runs: it starts on a copy of the v6.6.3 database, says out loud what it did while it
// upgraded, files the same lines for an operator who was not watching the terminal, answers its
// administrator endpoints with the configuration the upgrade carried, and answers a listener of the old
// deployment with the access codes that deployment issued and the calls it recorded.
//
// The last two are what no test at the persistence layer can show. A row carried with every column intact is
// still useless if the JSON in its scope column is a shape this server cannot read, or if the references a
// call names do not resolve to the rows the migration made for them: the history search is built on those
// references, and an access that sees nothing answers a search with an empty page.
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

	// Filed for the operator who was not watching the terminal, and reachable from the log view the
	// administrator's page opens.
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

	// The subsystem counts are what the log view offers before anything is narrowed. `database` has to be
	// one of them: it is how an operator finds the upgrade afterwards.
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

	// The systems, under the identity the migration gave them and the reference the deployment used for
	// them.
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

	// The talkgroups and units are answered under the system their reference resolved to, which is the
	// work the migration had to do. Unit 55555 is the archived one: it names a system this server does not
	// have.
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

	// A listener of the old deployment. The code whose date the v6 deployment gave it is still refused, at
	// the moment that moment arrived; the code that never expires still opens the server; and the calls the
	// old server recorded are still searchable, with the systems and talkgroups the migration resolved them
	// onto.
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

// migratedLabel is one row of the configuration answer, in the shape this test reads it: the identity the
// server hands out and the label the deployment gave it.
type migratedLabel struct {
	ID    uint64 `json:"id"`
	Label string `json:"label"`
}

// migratedSystem is one system of the configuration answer, with the talkgroups and units stored under it.
type migratedSystem struct {
	ID         uint64          `json:"id"`
	Label      string          `json:"label"`
	SystemRef  uint64          `json:"systemRef"`
	Talkgroups []migratedLabel `json:"talkgroups"`
	Units      []migratedLabel `json:"units"`
}

// systemNamed picks one system out of the configuration answer by the identity the server hands out for it.
// A deployment that answers no such system is not a deployment this test can go on reading, so it stops
// there rather than reporting a field of a system that is not there.
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

// framePayload is the payload of the frame a server answered a command with. A frame that is not the answer
// to that command is reported to the test and answered with nothing, which is what lets the test say which
// of its expectations the server missed.
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

// callsOf is one page of call history as identifier, system reference, talkgroup reference, and moment, in
// the order the server answered them.
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

// whole is a number the server answered with, written the way the deployment wrote it. Every JSON number is
// a float64 in an answer read this way, and 1514000 read out of a float64 is 1.514e+06 unless it is asked
// for as the whole number it is.
func whole(value any) string {
	if number, ok := value.(float64); ok {
		return fmt.Sprintf("%.0f", number)
	}

	return fmt.Sprintf("%v", value)
}

// TestAPasswordChangeLeavesTheSessionsItWasStartedWith is the re-derivation the legacy server ran against
// a secret it had never stored: the password change reset the options, the reset found nothing usable,
// and every session the server had started died with the key it was signed with.
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

// The listener settings the hub is asked for come from the configuration in effect, and the access list is
// what decides whether listeners are asked for a code.
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

// testServerOn is the same server, with the console it writes out loud to chosen by the test.
func testServerOn(t *testing.T, out io.Writer) *Server {
	t.Helper()

	srv, err := New(testConfigWith(t, out))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	return srv
}

// startedTestServer is a server that has been through the start sequence the legacy server goes through.
func startedTestServer(t *testing.T) *Server {
	t.Helper()

	srv := testServer(t)

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	return srv
}

// theTestVersion is the version a test server reports. The version a build injects is a fact about the
// build, not a fact about a server, so the tests name the version they configure the server with instead
// of inheriting what the binary being compiled happens to carry.
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

// testConfigIn is the configuration pointed at a directory a test has already chosen, which is how a test
// starts a second server on the database the first one wrote.
// testConfigIn is the configuration pointed at a directory a test has already chosen, which is how a test
// starts a second server on the database the first one wrote, with the console that test reads.
func testConfigIn(baseDir string, out io.Writer) Config {
	return Config{
		BaseDir: baseDir,
		DbFile:  "rdio-scanner.db",
		DbType:  DbTypeSqlite,
		Listen:  ":3000",
		Version: theTestVersion,
		Out:     out,

		// The converter a test server is given is the ffmpeg the host installs. Every test that reaches
		// the converter turns conversion off, so that what a test proves is the server and not what one
		// version of ffmpeg makes of a recording; conversion is compared where ffmpeg is the subject.
		Converter: func(dir string) (ports.AudioConverter, string) {
			return ffmpeg.New(), ""
		},
	}
}

// startedTestServerIn is a server that has been through the start sequence on a directory the test chose.
func startedTestServerIn(t *testing.T, baseDir string) *Server {
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

// askForAnUpgrade sends a request that claims to be a websocket upgrade and is not one: the Upgrade header
// is the one header this server routes on, and everything else Go's upgrader insists on is left out. It is
// how a test reaches the listener endpoint's refusal of a connection it would not take over.
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

// put sends an authenticated request to an administrator endpoint, which is how these tests install a
// configuration the way an administrator installs one.
func (s *Server) put(path, token, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	s.routes.ServeHTTP(rec, req)

	return rec
}

// postAuthorized is the same request with the verb an administrator's own form uses, which is how a test
// follows a session through the thing that re-derives the configuration.
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

// dial opens a listener socket against the server's own routes, which is how these tests reach a path
// that can be either an endpoint or a socket.
func (s *Server) dial(t *testing.T, path string) *websocket.Conn {
	t.Helper()

	test := httptest.NewServer(s.routes)
	t.Cleanup(test.Close)

	url := "ws" + strings.TrimPrefix(test.URL, "http") + path

	// The upgrade headers are the dialer's to write: a caller that writes them itself is refused by the
	// dialer for duplicating them.
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("opening %s as a socket: %v", path, err)
	}

	t.Cleanup(func() { conn.Close() })

	return conn
}

// closedWith installs the handler that reports how a connection was ended, and returns the thing that
// waits for it. A socket closed with the normal code is not reported by ReadMessage as a close code, which
// is why a test that cares about the code has to ask for it before reading.
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

// logSourceOfMessage is the subsystem a stored entry is filed under, or an empty string when no entry
// carries that message. A test that expects a source reads this, so a writer that names nothing fails as
// a missing subsystem rather than as a missing message.
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

// storedLogMentioning reads the first stored server log entry that mentions a fragment, from a database the
// test opened itself. It is how a test looks at what a stopped server left behind: the server's own
// connection is closed by then, so the only way in is through the file.
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

// seedUploadTarget installs what a test needs to put a call into the ingest path through the upload
// endpoint: the system and talkgroup the recording names, the credential the post is made with, and the
// switch that leaves the recording as it was uploaded so that what a test is about is the server and not
// ffmpeg. The hold is named in the unit the configuration stores it in, so a test can say whether the call
// it posts is one the server holds back.
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

// damagedMigratedStatements is a database a legacy server has already migrated, recorded as the
// statements the legacy pass ran: the schema, then the rows the pass left damaged in the units and
// the missing source links. They were taken from a database the legacy binary wrote, statement by
// statement, and they live in the package that consumes them, which is what REQ-007 asks for: no
// fixture directory, no binary database file, and nothing outside the test that proves it.
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

// damagedCrashedStatements is a database where the legacy calls migration was rolled back and the v6
// calls table is still there, recorded as the statements the legacy pass ran.
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

// legacyV6Statements are the schema and the rows a v6.6.3 deployment leaves behind, recorded as the
// statements the legacy server ran. They cover every table the v6 → v7 migration pass reads, and
// they live in the package that consumes them.
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

// writeRecordedDatabase builds a database from the recorded statements, in the folder the server is about to be opened on.
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
