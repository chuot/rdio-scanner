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

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"rdio-scanner/server-ng/internal/application"
	"rdio-scanner/server-ng/internal/application/fakes"
	"rdio-scanner/server-ng/internal/domain"
	"rdio-scanner/server-ng/internal/infrastructure/persistence"
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
// legacy server generates a hash of it at every startup and writes nothing, which is also why the signing
// secret a session was signed with is regenerated at the next startup.
func TestServerAcceptsTheDefaultCredentialWithoutStoringIt(t *testing.T) {
	srv := startedTestServer(t)

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "options" WHERE "key" = 'adminPassword'`); rows != 0 {
		t.Errorf("the options table holds %d adminPassword rows after a fresh start, want none: writing the "+
			"initial credential would write the signing secret with it, and a server that stores its secret "+
			"keeps sessions across a restart the legacy server does not keep", rows)
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

	err := srv.start(context.Background())
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

	if greeting["version"] != Version {
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

	srv, err := newServer(&Config{
		BaseDir: t.TempDir(),
		DbFile:  "rdio-scanner.db",
		DbType:  DbTypeSqlite,
		Listen:  ":3000",
	}, console, nil)
	if err != nil {
		t.Fatalf("newServer: %v", err)
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

	srv, err := newServer(&Config{
		BaseDir: t.TempDir(),
		DbFile:  "rdio-scanner.db",
		DbType:  DbTypeSqlite,
		Listen:  ":80",
	}, console, nil)
	if err != nil {
		t.Fatalf("newServer: %v", err)
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

// The base folder is where the server writes, and the legacy server names it when it was told one.
func TestServerAnnouncesTheBaseFolderItWasGiven(t *testing.T) {
	base := t.TempDir()

	console := &bytes.Buffer{}

	srv, err := newServer(&Config{
		BaseDir: base,
		DbFile:  "rdio-scanner.db",
		DbType:  DbTypeSqlite,
		Listen:  ":3000",
	}, console, nil)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if !strings.Contains(console.String(), "base folder is "+base) {
		t.Errorf("the server wrote:\n%s\nwant the base folder it was given", console.String())
	}
}

// The server's own startup notice goes into the log view, not to the terminal: it is the first thing an
// administrator sees when they open the log.
func TestServerRecordsThatItStartedInTheLogView(t *testing.T) {
	srv := startedTestServer(t)

	if !hasLogMessage(t, srv.db, "server started") {
		t.Errorf("the log view does not record that the server started")
	}
}

// The -admin_password mode is a one-shot: it stores the credential, says so in the log view, and stops
// without starting a server.
func TestAdminPasswordModeStoresTheCredentialAndStops(t *testing.T) {
	base := t.TempDir()

	out := &bytes.Buffer{}

	code := run([]string{"-base_dir", base, "-admin_password", "a-chosen-one"}, out)
	if code != 0 {
		t.Fatalf("the admin password mode exited %d, want 0: %s", code, out.String())
	}

	if strings.Contains(out.String(), "Rdio Scanner v") {
		t.Errorf("the admin password mode printed the banner:\n%s\nwant the banner kept for a server that is "+
			"actually starting", out.String())
	}

	db := openTestDatabase(t, base)

	if stored := readOptionValue(t, db, "adminPasswordNeedChange"); stored != "false" {
		t.Errorf("the stored adminPasswordNeedChange is %s, want false: a credential the administrator chose "+
			"is not the one the server ships with", stored)
	}

	if !hasLogMessage(t, db, "admin password changed.") {
		t.Errorf("the log view does not record the password change")
	}

	if _, err := db.Exec(`DELETE FROM "options" WHERE "key" = 'adminPassword'`); err != nil {
		t.Fatalf("clearing the stored credential: %v", err)
	}

	if status := loginAnswered(t, base, "a-chosen-one"); status != http.StatusUnauthorized {
		t.Errorf("after the stored credential was removed the chosen credential was answered %d, want it to "+
			"stop working: only the default credential is regenerated at startup", status)
	}
}

// The credential the -admin_password mode stored is the one the server then accepts.
func TestAdminPasswordModeIsTheCredentialTheServerAccepts(t *testing.T) {
	base := t.TempDir()

	if code := run([]string{"-base_dir", base, "-admin_password", "a-chosen-one"}, io.Discard); code != 0 {
		t.Fatalf("the admin password mode failed, exit %d", code)
	}

	srv, err := newServer(&Config{
		BaseDir: base,
		DbFile:  "rdio-scanner.db",
		DbType:  DbTypeSqlite,
		Listen:  ":3000",
	}, io.Discard, nil)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	defer srv.stop(context.Background())

	if rec := srv.post("/api/admin/login", `{"password":"rdio-scanner"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("the credential the server ships with was answered %d after an administrator chose another "+
			"one, want it refused", rec.Code)
	}

	if rec := srv.post("/api/admin/login", `{"password":"a-chosen-one"}`); rec.Code != http.StatusOK {
		t.Errorf("the credential the administrator chose was answered %d, want it accepted: %s",
			rec.Code, rec.Body.String())
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

// The stop is what says the server terminated, and two goroutines can ask for it at the same moment: the
// one that was waiting for the operator's interrupt, and the one that was serving and is unblocked by it.
// The line has to be said once, and the goroutine that arrives second has to wait for the one that got
// there first — otherwise the process ends while the first stop is still saying so, and an operator who
// interrupted the server sees a server that stopped without ever saying it did.
func TestServerSaysItTerminatedOnceEvenWhenTwoGoroutinesStopIt(t *testing.T) {
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

	if said := strings.Count(written, "terminated"); said != 1 {
		t.Errorf("the server said it terminated %d times, want once, wrote:\n%s", said, written)
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

	srv, err := newServer(&Config{
		BaseDir: t.TempDir(),
		DbFile:  "rdio-scanner.db",
		DbType:  DbTypeSqlite,
		Listen:  ":3000",
	}, console, nil)
	if err != nil {
		t.Fatalf("newServer: %v", err)
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

// The composition root is where the legacy server's startup rule lives: a server whose stored
// configuration has no administrator credential accepts the one the server ships with.
func TestServerReadsItsConfigurationBeforeItStartsAnswering(t *testing.T) {
	srv := startedTestServer(t)

	options, err := srv.options.ReadOptions(context.Background())
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if options.AdminPasswordHash == "" {
		t.Fatalf("the server is running with no administrator credential at all")
	}

	// A deployment that has never saved its configuration has no signing secret on record, and the legacy
	// server generates one only when a stored secret is there and the wrong length — not when there is no
	// secret at all. A fresh server therefore signs with nothing, in both servers, until the administrator
	// saves the configuration once.
	if options.Secret != "" {
		t.Errorf("the server generated a signing secret of its own, want the legacy server's behavior of not "+
			"generating one until a stored secret needs replacing: %q", options.Secret)
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
func testServer(t *testing.T) *server {
	t.Helper()

	return testServerOn(t, io.Discard)
}

// testServerOn is the same server, with the console it writes out loud to chosen by the test.
func testServerOn(t *testing.T, out io.Writer) *server {
	t.Helper()

	srv, err := newServer(testConfig(t), out, nil)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	return srv
}

// startedTestServer is a server that has been through the start sequence the legacy server goes through.
func startedTestServer(t *testing.T) *server {
	t.Helper()

	srv := testServer(t)

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	return srv
}

func testConfig(t *testing.T) *Config {
	t.Helper()

	return &Config{
		BaseDir: t.TempDir(),
		DbFile:  "rdio-scanner.db",
		DbType:  DbTypeSqlite,
		Listen:  ":3000",
	}
}

func (s *server) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	s.routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	return rec
}

func (s *server) post(path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	s.routes.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))

	return rec
}

// put sends an authenticated request to an administrator endpoint, which is how these tests install a
// configuration the way an administrator installs one.
func (s *server) put(path, token, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Authorization", token)

	s.routes.ServeHTTP(rec, req)

	return rec
}

// dial opens a listener socket against the server's own routes, which is how these tests reach a path
// that can be either an endpoint or a socket.
func (s *server) dial(t *testing.T, path string) *websocket.Conn {
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

// loginAnswered starts a server against a base directory the test has already prepared and reports how a
// login was answered.
func loginAnswered(t *testing.T, base, credential string) int {
	t.Helper()

	srv, err := newServer(&Config{
		BaseDir: base,
		DbFile:  "rdio-scanner.db",
		DbType:  DbTypeSqlite,
		Listen:  ":3000",
	}, io.Discard, nil)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	defer srv.stop(context.Background())

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	body := fmt.Sprintf(`{"password":%q}`, credential)

	rec := httptest.NewRecorder()

	srv.routes.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(body)))

	return rec.Code
}
