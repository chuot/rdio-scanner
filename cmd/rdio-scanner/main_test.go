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

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/app"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/infrastructure/dblock"
	"rdio-scanner/internal/infrastructure/ffmpeg"
	"rdio-scanner/internal/infrastructure/persistence"
)

// the command-line tests drive run itself from the arguments an operator

// an embedded bundle with no application shell warns at startup, names the
func TestTheServerWarnsAboutAnUnbuiltBundleAndNamesTheCommand(t *testing.T) {
	base := t.TempDir()

	out := &bytes.Buffer{}

	code := run([]string{"-base_dir", base, "-listen", "not-an-address"}, out, func() []string { return []string{"scanner"} })
	if code == 0 {
		t.Fatal("a server that cannot listen exited 0")
	}

	text := out.String()

	if !strings.Contains(text, "scanner") || !strings.Contains(text, "go run ./cmd/build client") {
		t.Errorf("the output does not warn about the unbuilt scanner bundle and name the build command:\n%s", text)
	}
}

// A server whose bundles are built says nothing about bundles.
func TestTheServerSaysNothingWhenBothBundlesAreBuilt(t *testing.T) {
	base := t.TempDir()

	out := &bytes.Buffer{}

	code := run([]string{"-base_dir", base, "-listen", "not-an-address"}, out, func() []string { return nil })
	if code == 0 {
		t.Fatal("a server that cannot listen exited 0")
	}

	if strings.Contains(out.String(), "go run ./cmd/build client") {
		t.Errorf("a server whose bundles are built warned about bundles:\n%s", out.String())
	}
}

// The warning names every application it says is unbuilt, and the one command that builds them.
func TestTheBundleWarningNamesEveryUnbuiltApplication(t *testing.T) {
	line := bundleWarning([]string{"scanner", "admin"})

	if !strings.Contains(line, "scanner") || !strings.Contains(line, "admin") || !strings.Contains(line, "go run ./cmd/build client") {
		t.Errorf("the warning %q does not name both applications and the command that builds them", line)
	}
}

// a switch the command layer parses and the composition shape does not carry
// across is a switch the server never hears, so these pin the two opt-in
// switches no address or file path can stand in for.
func TestTheOperatorsSwitchesReachTheServerTheyWereTypedFor(t *testing.T) {
	cfg, _ := newTestConfig(t, "-mcp_server", "-trusted_proxies", "10.0.0.0/8, 127.0.0.1")

	served := serverConfig(cfg, io.Discard, nil)

	if !served.McpServer {
		t.Fatalf("the server's configuration says McpServer = %v, want true: -mcp_server was typed by the "+
			"operator and the server is built from this configuration, so the endpoint is not served on a "+
			"deployment that asked for it", served.McpServer)
	}

	if served.TrustedProxies != cfg.TrustedProxies {
		t.Fatalf("the server's configuration carries %q for the proxy addresses, want %q: -trusted_proxies "+
			"was typed by the operator and the server is built from this configuration, so the login budget "+
			"and the access warnings keep naming a proxied request by the proxy's address",
			served.TrustedProxies, cfg.TrustedProxies)
	}

	// the switches are opt-in, and a copy turning the default into something else
	plain, _ := newTestConfig(t)

	quiet := serverConfig(plain, io.Discard, nil)

	if quiet.McpServer {
		t.Errorf("the server's configuration says McpServer = %v for a deployment that asked for nothing, "+
			"want off: the surface is opt-in at every entry point", quiet.McpServer)
	}

	if quiet.TrustedProxies != "" {
		t.Errorf("the server's configuration carries %q for the proxy addresses of a deployment that named "+
			"none, want nothing: a default proxy list would trust addresses the operator never named",
			quiet.TrustedProxies)
	}
}

// the -admin_password mode stores the credential, says so in the log view, and
func TestAdminPasswordModeStoresTheCredentialAndStops(t *testing.T) {
	base := t.TempDir()

	out := &bytes.Buffer{}

	code := run([]string{"-base_dir", base, "-admin_password", "a-chosen-one"}, out, noBundlesUnbuilt)
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

	if !hasLogMessage(t, db, "the administrator password was changed") {
		t.Errorf("the log view does not record the password change")
	}

	if got := logSourceOfMessage(t, db, "the administrator password was changed"); got != "admin" {
		t.Errorf("the password change is filed under %q, want it filed with the administrator account", got)
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

	if code := run([]string{"-base_dir", base, "-admin_password", "a-chosen-one"}, io.Discard, noBundlesUnbuilt); code != 0 {
		t.Fatalf("the admin password mode failed, exit %d", code)
	}

	srv, err := app.New(testConfigIn(base))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	defer srv.Close()

	if rec := postTo(srv, "/api/admin/login", `{"password":"rdio-scanner"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("the credential the server ships with was answered %d after an administrator chose another "+
			"one, want it refused", rec.Code)
	}

	if rec := postTo(srv, "/api/admin/login", `{"password":"a-chosen-one"}`); rec.Code != http.StatusOK {
		t.Errorf("the credential the administrator chose was answered %d, want it accepted: %s",
			rec.Code, rec.Body.String())
	}
}

// The -vacuum mode compacts the deployment in place and stops: the run exits 0,
// reports the free pages it reclaimed, keeps the database file at its path, and
// never binds the listen address a server would use — the port stays free for
// whoever holds it while the task runs.
func TestVacuumModeCompactsTheDeploymentAndStops(t *testing.T) {
	base := t.TempDir()

	// a server run migrates the deployment, then a direct connection seeds the
	// probe table and deletes half of its rows, so the file carries a freelist
	// the task can reclaim
	srv, err := app.New(testConfigIn(base))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db := openTestDatabase(t, base)

	if _, err := db.Exec(`CREATE TABLE vacuum_probe (id INTEGER PRIMARY KEY, body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	for i := range 500 {
		if _, err := db.Exec(`INSERT INTO vacuum_probe (body) VALUES (?)`, strings.Repeat("x", 200)); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	if _, err := db.Exec(`DELETE FROM vacuum_probe WHERE id % 2 = 0`); err != nil {
		t.Fatalf("delete every other row: %v", err)
	}

	// a held port stands in for a running server on the configured address: the
	// task never reaches for it, so the run succeeds over it
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}

	defer ln.Close()

	out := &bytes.Buffer{}

	code := run([]string{"-base_dir", base, "-listen", ln.Addr().String(), "-vacuum"}, out, noBundlesUnbuilt)
	if code != 0 {
		t.Fatalf("the vacuum mode exited %d, want 0: %s", code, out.String())
	}

	text := out.String()

	dbPath := filepath.Join(base, "rdio-scanner.db")

	if !strings.Contains(text, "vacuuming "+dbPath) {
		t.Errorf("the output does not name the database file being compacted:\n%s", text)
	}

	var before, after, reclaimed int

	reportLine := ""

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "vacuumed:") {
			reportLine = line
			break
		}
	}

	if reportLine == "" {
		t.Fatalf("the output does not carry the report line:\n%s", text)
	}

	if _, err := fmt.Sscanf(reportLine, "vacuumed: %d free pages, %d free pages, %d bytes reclaimed", &before, &after, &reclaimed); err != nil {
		t.Fatalf("the report line does not match the report format (%v):\n%s", err, text)
	}

	if before == 0 {
		t.Errorf("the report says 0 free pages before the task, want the deleted rows' pages on record:\n%s", text)
	}

	if after != 0 {
		t.Errorf("the report says %d free pages after the task, want them all reclaimed:\n%s", after, text)
	}

	if reclaimed <= 0 {
		t.Errorf("the report says %d bytes reclaimed, want a positive figure for the pages taken back:\n%s", reclaimed, text)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("the database file is not at its path after the task: %v", err)
	}

	check := openTestDatabase(t, base)

	var kept int
	if err := check.QueryRow(`SELECT COUNT(*) FROM vacuum_probe`).Scan(&kept); err != nil {
		t.Fatalf("count the kept rows: %v", err)
	}

	if kept != 250 {
		t.Errorf("the compacted file holds %d rows, want the 250 that were not deleted", kept)
	}

	var freelist int
	if err := check.QueryRow(`PRAGMA freelist_count`).Scan(&freelist); err != nil {
		t.Fatalf("read the freelist back: %v", err)
	}

	if freelist != 0 {
		t.Errorf("the file still carries %d free pages after the task, want them reclaimed", freelist)
	}
}

// The vacuum mode refuses a deployment another process holds: the run prints
// the in-use error and exits 1 without opening the database — the file is not
// even created for a fresh base directory whose lock the test already holds.
func TestVacuumModeRefusesADeploymentAnotherProcessHolds(t *testing.T) {
	base := t.TempDir()

	handle, err := dblock.Acquire(filepath.Join(base, "rdio-scanner.db"))
	if err != nil {
		t.Fatalf("dblock.Acquire: %v", err)
	}

	defer func() {
		if err := handle.Close(); err != nil {
			t.Errorf("release the held lock: %v", err)
		}
	}()

	// the refusal is said through the standard logger, so the test watches
	// where that logger writes for the duration of the run
	logOut := &bytes.Buffer{}

	previous := log.Writer()
	log.SetOutput(logOut)

	defer log.SetOutput(previous)

	code := run([]string{"-base_dir", base, "-vacuum"}, io.Discard, noBundlesUnbuilt)
	if code != 1 {
		t.Fatalf("the vacuum mode against a held deployment exited %d, want 1", code)
	}

	text := logOut.String()

	if !strings.Contains(text, "the database is in use — stop the server before vacuuming") {
		t.Errorf("the failure output does not refuse the held deployment:\n%s", text)
	}

	if _, err := os.Stat(filepath.Join(base, "rdio-scanner.db")); !os.IsNotExist(err) {
		t.Errorf("the run opened a database behind the held lock: stat says %v", err)
	}
}

// A busy database reaches the operator through the shared one-shot error
// path: with a second connection holding the write lock, the run waits out
// the busy timeout, prints sqlite's own lock message, and exits 1. The
// migration pass is the first writer the run sends at the deployment, so the
// held lock surfaces there before the compaction statement is reached; the
// failure the operator sees names sqlite's lock either way.
func TestVacuumModeReportsABusyDatabaseThroughTheErrorPath(t *testing.T) {
	base := t.TempDir()

	dbPath := filepath.Join(base, "rdio-scanner.db")

	// the holding connection stands in for the operator's sqlite3 session:
	// its own busy timeout is short, but the write lock it holds is what
	// the run has to wait out
	holder, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys=on&_pragma=busy_timeout=500&_txlock=immediate")
	if err != nil {
		t.Fatalf("open the holding connection: %v", err)
	}

	defer holder.Close()

	// one connection, so the write transaction sits on the connection that
	// carries the short timeout
	holder.SetMaxOpenConns(1)

	if _, err := holder.Exec(`CREATE TABLE vacuum_probe (id INTEGER PRIMARY KEY, body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	tx, err := holder.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := tx.Exec(`INSERT INTO vacuum_probe (body) VALUES ('in flight')`); err != nil {
		tx.Rollback()
		t.Fatalf("insert inside the transaction: %v", err)
	}

	defer tx.Rollback()

	// the failure is said through the standard logger, so the test watches
	// where that logger writes for the duration of the run
	logOut := &bytes.Buffer{}

	previous := log.Writer()
	log.SetOutput(logOut)

	defer log.SetOutput(previous)

	start := time.Now()

	code := run([]string{"-base_dir", base, "-vacuum"}, io.Discard, noBundlesUnbuilt)

	elapsed := time.Since(start)

	if code != 1 {
		t.Fatalf("the vacuum mode against a busy database exited %d, want 1:\n%s", code, logOut.String())
	}

	if !strings.Contains(logOut.String(), "database is locked") {
		t.Errorf("the failure output does not name sqlite's lock:\n%s", logOut.String())
	}

	// the run's own ten second wait is what expires here: the failure lands
	// with the busy timeout waited out, not before it and not past it
	if elapsed < time.Second {
		t.Errorf("the run failed after %v, want the busy database waited out first", elapsed)
	}

	if elapsed > 30*time.Second {
		t.Errorf("the run failed after %v, want the failure inside the ten second busy timeout", elapsed)
	}
}

// The task holds the deployment lock for its own whole run, so a second
// vacuum run beside the first is refused the way a server-held deployment
// is: the first run stalls waiting out the busy timeout against a held write
// transaction while it holds the lock, and the second run meets the refusal
// while that wait is still running.
func TestVacuumModeRefusesASecondRunWhileTheFirstHoldsTheLock(t *testing.T) {
	base := t.TempDir()

	dbPath := filepath.Join(base, "rdio-scanner.db")

	// the held write transaction keeps the first run inside its busy wait —
	// and so inside the deployment lock it took before opening the database
	holder, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys=on&_pragma=busy_timeout=500&_txlock=immediate")
	if err != nil {
		t.Fatalf("open the holding connection: %v", err)
	}

	defer holder.Close()

	holder.SetMaxOpenConns(1)

	if _, err := holder.Exec(`CREATE TABLE vacuum_probe (id INTEGER PRIMARY KEY, body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	tx, err := holder.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := tx.Exec(`INSERT INTO vacuum_probe (body) VALUES ('in flight')`); err != nil {
		tx.Rollback()
		t.Fatalf("insert inside the transaction: %v", err)
	}

	defer tx.Rollback()

	firstOut := &bytes.Buffer{}

	firstDone := make(chan int, 1)

	go func() {
		firstDone <- run([]string{"-base_dir", base, "-vacuum"}, firstOut, noBundlesUnbuilt)
	}()

	// the second run waits its place: the first takes the lock just before
	// it opens the database, so the lock is watched for until it is seen held
	deadline := time.Now().Add(30 * time.Second)

	for {
		probe, err := dblock.Acquire(dbPath)
		if err != nil {
			break
		}

		probe.Close()

		if time.Now().After(deadline) {
			t.Fatal("the first vacuum run never took the deployment lock")
		}

		time.Sleep(50 * time.Millisecond)
	}

	// the standard logger carries both runs' failures, so it is watched once
	logOut := &bytes.Buffer{}

	previous := log.Writer()
	log.SetOutput(logOut)

	defer log.SetOutput(previous)

	if code := run([]string{"-base_dir", base, "-vacuum"}, io.Discard, noBundlesUnbuilt); code != 1 {
		t.Fatalf("the second vacuum run exited %d, want 1", code)
	}

	if !strings.Contains(logOut.String(), "the database is in use — stop the server before vacuuming") {
		t.Errorf("the second run was not refused as in use:\n%s", logOut.String())
	}

	if strings.Contains(firstOut.String(), "vacuumed:") {
		t.Errorf("the first run reported a compaction it never finished:\n%s", firstOut.String())
	}

	select {
	case code := <-firstDone:
		if code != 1 {
			t.Errorf("the first vacuum run exited %d, want 1 against the busy database", code)
		}
	case <-time.After(time.Minute):
		t.Fatal("the first vacuum run never finished")
	}
}

// testConfigIn is the configuration a test builds a server from on a folder
func testConfigIn(base string) app.Config {
	return app.Config{
		BaseDir: base,
		DbFile:  "rdio-scanner.db",
		DbType:  app.DbTypeSqlite,
		Listen:  ":3000",
		Version: theCommandVersion,
		Out:     io.Discard,

		// the converter is the host's ffmpeg, so a test proves the program and not
		Converter: func(dir string) (ports.AudioConverter, ports.AudioLengthProber, string) {
			return ffmpeg.New(), ffmpeg.NewProber(dir), ""
		},
	}
}

// commandTestConfig is the configuration for a command-mode test: the plain
func commandTestConfig(t *testing.T) app.Config {
	config := testConfigIn(t.TempDir())

	config.Converter = func(dir string) (ports.AudioConverter, ports.AudioLengthProber, string) {
		return ffmpeg.New(), ffmpeg.NewProber(dir), ""
	}

	return config
}

// the recorded answers were captured with the server in UTC, so these tests
func init() {
	os.Setenv("TZ", "UTC")
}

// theCommandVersion is the version these tests configure their server with:
const theCommandVersion = "8.0.0"

// noBundlesUnbuilt is the stand-in bundle answer for tests reaching modes that
func noBundlesUnbuilt() []string {
	return nil
}

// postTo sends a request to a server the test built and reports how it answered.
func postTo(srv *app.Server, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))

	return rec
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

func readOptionValue(t *testing.T, db *sql.DB, key string) string {
	t.Helper()

	const query = `SELECT "value" FROM "options" WHERE "key" = ?`

	var value string
	if err := db.QueryRow(query, key).Scan(&value); err != nil {
		t.Fatalf("%s (%s): %v", query, key, err)
	}

	return value
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

func logSourceOfMessage(t *testing.T, db *sql.DB, message string) string {
	t.Helper()

	const query = `SELECT "source" FROM "logs" WHERE "message" = ? ORDER BY "logid" DESC LIMIT 1`

	var source string
	if err := db.QueryRow(query, message).Scan(&source); err != nil {
		t.Fatalf("%s (%s): %v", query, message, err)
	}

	return source
}

// loginAnswered starts a server against a prepared base directory and reports
func loginAnswered(t *testing.T, base, credential string) int {
	t.Helper()

	srv, err := app.New(testConfigIn(base))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	defer srv.Close()

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	return postTo(srv, "/api/admin/login", `{"password":"`+credential+`"}`).Code
}
