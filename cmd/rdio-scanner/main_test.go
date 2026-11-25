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
	"errors"
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

// A mode that opens the deployment is refused over a database file the account
// cannot write before anything is asked of that file: the refusal names it, the
// run ends 1, the file keeps its bytes and its mode, and no claim sidecar is left
// where a start that reached the claim would have written one. The maintenance run
// and its dry run, the administrator credential mode, and the server the service
// manager is told to run are each asked for over the same file.
func TestAModeThatOpensTheDatabaseIsRefusedBeforeItClaimsTheFile(t *testing.T) {
	for _, flags := range [][]string{
		{"-vacuum"},
		{"-vacuum", "-vacuum_dry_run"},
		{"-admin_password", "a-chosen-one"},
		{"-service", serviceActionRun},
	} {
		base := t.TempDir()

		dbPath := filepath.Join(base, "rdio-scanner.db")

		seedDatabaseFile(t, dbPath, 0o444)

		claim := dbPath + ".lock"

		if _, err := os.Stat(claim); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: a deployment claim was found before the run: %v", flags[0], err)
		}

		before, err := os.ReadFile(dbPath)
		if err != nil {
			t.Fatalf("reading %s: %v", dbPath, err)
		}

		stdout := &bytes.Buffer{}

		code := run(append([]string{"-base_dir", base, "-listen", freeListenAddr(t)}, flags...), stdout, noBundlesUnbuilt)

		if code != 1 {
			t.Fatalf("%s exited %d over an unwritable database file, want 1:\n%s", flags[0], code, stdout.String())
		}

		// the operator reads the file they have to fix, the mode it is at, who
		// owns it, and the uid their start runs as, all on the one line
		said := stdout.String()

		for _, part := range []string{
			"error: no write permissions in the database file ",
			dbPath,
			"(mode 0444, owned by uid ",
			"; the server runs as uid ",
		} {
			if !strings.Contains(said, part) {
				t.Errorf("%s did not say %q over a file it cannot write:\n%s", flags[0], part, said)
			}
		}

		after, err := os.ReadFile(dbPath)
		if err != nil {
			t.Fatalf("reading %s after the run: %v", dbPath, err)
		}

		if !bytes.Equal(before, after) {
			t.Errorf("%s changed the database file it refused to work with", flags[0])
		}

		info, err := os.Stat(dbPath)
		if err != nil {
			t.Fatalf("%s: reading %s after the run: %v", flags[0], dbPath, err)
		}

		if info.Mode().Perm() != 0o444 {
			t.Errorf("%s left the database file at %04o, want 0444", flags[0], info.Mode().Perm())
		}

		// the refusal is what stops the start, so it is the claim the run never
		// reached that proves the file was named before anything was asked of it
		if _, err := os.Stat(claim); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left a deployment claim behind over a file it cannot write: %v", flags[0], err)
		}
	}
}

// A mode that opens no database keeps the answer it gives today over a file the
// account cannot write: the version, the saved configuration file, the command
// modes that drive a server that is already running, and the actions this process
// asks the host's service manager to take. Each is answered with the words and the
// code it answers with now, none of them reaches the deployment, and the file they
// were given keeps its bytes and its mode. The service action is named as one no
// manager is asked to take, so the run reaches the manager's failure path rather
// than a service installed on the host running the test.
func TestAModeThatOpensNoDatabaseIsAnsweredOverAnUnwritableDatabaseFile(t *testing.T) {
	for _, asked := range []struct {
		flags []string
		code  int
		said  string
	}{
		{flags: []string{"-version"}, code: 0, said: Version},
		{flags: []string{"-config_save"}, code: 0, said: "file created"},
		{flags: []string{"-cmd", "help"}, code: 0, said: "Available Commands:"},
		{flags: []string{"-cmd", "config-get"}, code: 1, said: "missing +out"},
		{flags: []string{"-service", "bogus"}, code: 1, said: "bogus"},
	} {
		base := t.TempDir()

		dbPath := filepath.Join(base, "rdio-scanner.db")

		seedDatabaseFile(t, dbPath, 0o444)

		claim := dbPath + ".lock"

		if _, err := os.Stat(claim); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%v: a deployment claim was found before the run: %v", asked.flags, err)
		}

		before, err := os.ReadFile(dbPath)
		if err != nil {
			t.Fatalf("reading %s: %v", dbPath, err)
		}

		stdout := &bytes.Buffer{}

		logOut := &bytes.Buffer{}

		previous := log.Writer()
		log.SetOutput(logOut)

		defer log.SetOutput(previous)

		code := run(append([]string{"-base_dir", base, "-listen", freeListenAddr(t)}, asked.flags...), stdout, noBundlesUnbuilt)

		// both streams are read together: the version and the usage are printed,
		// and a failure the host's manager answers is said through the logger
		said := stdout.String() + logOut.String()

		if code != asked.code {
			t.Fatalf("%v answered with code %d, want the code it answers with today:\n%s", asked.flags, code, said)
		}

		if !strings.Contains(said, asked.said) {
			t.Errorf("%v no longer says %q:\n%s", asked.flags, asked.said, said)
		}

		if strings.Contains(said, "no write permissions") {
			t.Errorf("%v reported a refused file it was never asked to write:\n%s", asked.flags, said)
		}

		if strings.Contains(said, dbPath) {
			t.Errorf("%v named the database file it never opened: %s", asked.flags, said)
		}

		if _, err := os.Stat(claim); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%v reached the deployment and left a claim behind: %v", asked.flags, err)
		}

		after, err := os.ReadFile(dbPath)
		if err != nil {
			t.Fatalf("reading %s after the run: %v", dbPath, err)
		}

		if !bytes.Equal(before, after) {
			t.Errorf("%v changed the database file it was given", asked.flags)
		}

		info, err := os.Stat(dbPath)
		if err != nil {
			t.Fatalf("%s after the run: %v", dbPath, err)
		}

		if info.Mode().Perm() != 0o444 {
			t.Errorf("%v left the database file at %04o, want 0444", asked.flags, info.Mode().Perm())
		}
	}
}

// A maintenance run clears the records the migration left in problem and then
// compacts the file: it prints one line per removal, a count line for each class
// it cleared, and the pages the compaction took back, and it exits 0. What an
// operator wrote on purpose stays where it is, the deployment records each removal
// under the maintenance subsystem, and each removal is said once.
func TestMaintenanceRunClearsTheDamageThenCompactsTheFile(t *testing.T) {
	base := t.TempDir()

	// a server run builds the deployment, then a direct connection writes the
	// records a migration leaves behind beside the rows a deployment named
	srv, err := app.New(testConfigIn(base))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db := openTestDatabase(t, base)

	seedMaintenanceDamageFixture(t, db)

	// a held port stands in for a running server on the configured address: the
	// run never reaches for it
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}

	defer ln.Close()

	out := &bytes.Buffer{}

	logOut := &bytes.Buffer{}

	previous := log.Writer()
	log.SetOutput(logOut)

	defer log.SetOutput(previous)

	code := run([]string{"-base_dir", base, "-listen", ln.Addr().String(), "-vacuum"}, out, noBundlesUnbuilt)
	if code != 0 {
		t.Fatalf("the maintenance run exited %d, want 0: %s", code, out.String())
	}

	text := out.String()

	for _, want := range []string{
		`removed unit "9001" carrying the radio number 9001, of system 901`,
		`removed unit "9002" carrying the radio number 9002, of system 901`,
		`removed 2 units labelled with their own radio numbers`,
		"dropped the table rdioScannerDirWatches, which held 2 rows",
		"dropped the migration archive, which held 1 row the migration could not carry",
		"removed the archived row rdioScannerDirWatches 603: it holds no directory",
		"vacuumed: ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the run's report does not carry %q:\n%s", want, text)
		}
	}

	// the state the run left is read through a connection opened after it, the way
	// an operator reads a deployment they were told to come back and look at
	verify := openTestDatabase(t, base)

	// the deployment recorded each removal the run printed, under the maintenance
	// subsystem, and the terminal did not print it a second time
	logged := maintenanceLogMessages(t, verify)

	for _, want := range []string{
		`removed unit "9001" carrying the radio number 9001, of system 901`,
		`removed unit "9002" carrying the radio number 9002, of system 901`,
		"removed 2 units labelled with their own radio numbers",
		"dropped the table rdioScannerDirWatches, which held 2 rows",
		"dropped the migration archive, which held 1 row the migration could not carry",
	} {
		if !strings.Contains(logged, "warn "+want) {
			t.Errorf("the deployment does not record %q at warn level under the maintenance subsystem:\n%s", want, logged)
		}

		if count := strings.Count(text, want); count != 1 {
			t.Errorf("the run printed %q %d times, want once: the log entry is the record, the console is the one reading\n%s", want, count, text)
		}

		if strings.Contains(logOut.String(), want) {
			t.Errorf("the run said %q through the logger as well as its report:\n%s", want, logOut.String())
		}
	}

	// the rows a deployment named are untouched, and the rows the migration made
	// are gone
	for _, unitID := range []int{9001, 9002} {
		if count := countRows(t, verify, "units", `"unitId" = ?`, unitID); count != 0 {
			t.Errorf("the damaged unit %d is still stored %d times, want it removed", unitID, count)
		}
	}

	for _, unitID := range []int{9003, 9004, 9005} {
		if count := countRows(t, verify, "units", `"unitId" = ?`, unitID); count != 1 {
			t.Errorf("unit %d is stored %d times, want the row an operator wrote untouched", unitID, count)
		}
	}

	if count := countTables(t, verify, "rdioScannerDirWatches"); count != 0 {
		t.Errorf("the legacy table the run named is still standing %d times, want it dropped", count)
	}

	if count := countTables(t, verify, migrationArchiveProbeTable); count != 0 {
		t.Errorf("the migration archive the run named is still standing %d times, want it dropped", count)
	}

	// the compaction reclaimed the pages the dropped table and the removed rows
	// released
	var before, after, reclaimed int

	reportLine := ""

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "vacuumed:") {
			reportLine = line
			break
		}
	}

	if reportLine == "" {
		t.Fatalf("the run's report does not carry the compaction line:\n%s", text)
	}

	if _, err := fmt.Sscanf(reportLine, "vacuumed: %d free pages, %d free pages, %d bytes reclaimed", &before, &after, &reclaimed); err != nil {
		t.Fatalf("the compaction line does not match the report format (%v):\n%s", err, text)
	}

	if before == 0 {
		t.Errorf("the report says 0 free pages before the compaction, want the pages the clearing released on record:\n%s", text)
	}

	if after != 0 || reclaimed <= 0 {
		t.Errorf("the report says %d free pages and %d bytes reclaimed after the compaction, want none left and a positive figure", after, reclaimed)
	}

	if err := verify.QueryRow(`PRAGMA freelist_count`).Scan(&after); err != nil {
		t.Fatalf("read the freelist back: %v", err)
	}

	if after != 0 {
		t.Errorf("the file still carries %d free pages after the run, want them reclaimed", after)
	}

	// a second run over the same deployment has nothing left to name: the legacy
	// table and the archive it re-read on every start are both gone
	out = &bytes.Buffer{}

	if code := run([]string{"-base_dir", base, "-listen", ln.Addr().String(), "-vacuum"}, out, noBundlesUnbuilt); code != 0 {
		t.Fatalf("the second maintenance run exited %d, want 0: %s", code, out.String())
	}

	text = out.String()

	for _, unwanted := range []string{"removed ", "dropped ", "would remove", "would drop"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("the second run names removals (%q) on a deployment the first run already cleared:\n%s", unwanted, text)
		}
	}
}

// A dry run lists the same records a maintenance run would clear, marks each line
// as what the run would do, and removes none of them: the damaged rows, the legacy
// table, and the archive it re-reads on every start are all still there afterwards,
// no compaction is reported, and nothing is filed in the log.
func TestMaintenanceDryRunListsTheDamageAndRemovesNothing(t *testing.T) {
	base := t.TempDir()

	srv, err := app.New(testConfigIn(base))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db := openTestDatabase(t, base)

	seedMaintenanceDamageFixture(t, db)

	out := &bytes.Buffer{}

	logOut := &bytes.Buffer{}

	previous := log.Writer()
	log.SetOutput(logOut)

	defer log.SetOutput(previous)

	code := run([]string{"-base_dir", base, "-vacuum_dry_run"}, out, noBundlesUnbuilt)
	if code != 0 {
		t.Fatalf("the dry run exited %d, want 0: %s", code, out.String())
	}

	text := out.String()

	for _, want := range []string{
		`would remove unit "9001" carrying the radio number 9001, of system 901`,
		`would remove unit "9002" carrying the radio number 9002, of system 901`,
		"would remove 2 units labelled with their own radio numbers",
		"would drop the table rdioScannerDirWatches, which held 2 rows",
		"would drop the migration archive, which held 1 row the migration could not carry",
		"would remove the archived row rdioScannerDirWatches 603: it holds no directory",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the dry run's listing does not carry %q:\n%s", want, text)
		}
	}

	// every line of the listing is written in the words of what a run would do: none
	// of them claims a removal that happened
	for _, line := range strings.Split(text, "\n") {
		for _, claimed := range []string{
			"removed unit ",
			"removed the archived row ",
			"removed 2 units ",
			"dropped the table ",
			"dropped the migration archive ",
		} {
			if strings.HasPrefix(line, claimed) {
				t.Errorf("the dry run claims %q, want the listing marked as what a run would do:\n%s", line, text)
			}
		}
	}

	if strings.Contains(text, "vacuumed:") {
		t.Errorf("the dry run reported a compaction, want the listing alone:\n%s", text)
	}

	verify := openTestDatabase(t, base)

	for _, unitID := range []int{9001, 9002, 9003, 9004, 9005} {
		if count := countRows(t, verify, "units", `"unitId" = ?`, unitID); count != 1 {
			t.Errorf("unit %d is stored %d times, want the dry run to leave every unit where it found it", unitID, count)
		}
	}

	if count := countTables(t, verify, "rdioScannerDirWatches"); count != 1 {
		t.Errorf("the legacy table is standing %d times after the dry run, want it untouched", count)
	}

	if count := countRows(t, verify, migrationArchiveProbeTable, `1 = 1`); count == 0 {
		t.Error("the dry run left no archived row behind, want the archive it read still there")
	}

	if count := countRows(t, verify, "logs", `"source" = ?`, "maintenance"); count != 0 {
		t.Errorf("the dry run filed %d log entries under the maintenance subsystem, want none: a listing an operator read is not a change that happened", count)
	}
}

// migrationArchiveProbeTable is the table the migration writes the rows it cannot
// carry into, named here so a test can look for it without reaching into the store.
const migrationArchiveProbeTable = "migrationArchive"

// seedMaintenanceDamageFixture fills a deployment built on sqlite with the records a
// legacy migration leaves in problem, and with the rows an operator wrote on purpose
// beside them: two units carrying their own radio numbers as their label, one unit
// whose label says what it is, one that carries a range beside its number, and one
// with no number at all. The legacy table holds one row this server cannot read —
// the shape that keeps the table standing after a migration and leaves an archived
// row for the next start to read again — and the probe table's deleted rows are the
// free pages the compaction takes back.
func seedMaintenanceDamageFixture(t *testing.T, db *sql.DB) {
	t.Helper()

	stmts := []string{
		`INSERT INTO "systems" ("systemId", "label", "systemRef") VALUES (901, 'Grand Metro', 7001)`,
		`INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef") VALUES (9001, '9001', 1, 901, 9001)`,
		`INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef") VALUES (9002, '9002', 2, 901, 9002)`,
		`INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef") VALUES (9003, 'Engine 12', 3, 901, 9003)`,
		`INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef", "unitFrom", "unitTo") VALUES (9004, '9004', 4, 901, 9004, 9004, 9010)`,
		`INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef") VALUES (9005, 'Mobile', 5, 901, 0)`,
		"CREATE TABLE `rdioScannerDirWatches` (`_id` integer primary key autoincrement, `delay` integer default 0, `deleteAfter` tinyint(1) default 0, `directory` varchar(255) not null unique, `disabled` tinyint(1) default 0, `extension` varchar(255), `frequency` integer, `mask` varchar(255), `order` integer, `systemId` integer, `talkgroupId` integer, `type` varchar(255), `usePolling` tinyint(1) default 0)",
		"INSERT INTO `rdioScannerDirWatches` VALUES(602,5,1,'/var/spool/default/1514002',1,'mp3',1514002000,'0755',2,10002,1514002,'default',1)",
		"INSERT INTO `rdioScannerDirWatches` VALUES(603,0,0,'',0,'wav',1514999000,'0644',3,10001,1514999,'dsdPlus',0)",
		"UPDATE `rdioScannerDirWatches` SET `delay` = 'not a delay' WHERE `_id` = 602",
		`CREATE TABLE maintenance_probe (id INTEGER PRIMARY KEY, body TEXT NOT NULL)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	for range 500 {
		if _, err := db.Exec(`INSERT INTO maintenance_probe (body) VALUES (?)`, strings.Repeat("x", 200)); err != nil {
			t.Fatalf("seed a probe row: %v", err)
		}
	}

	if _, err := db.Exec(`DELETE FROM maintenance_probe WHERE id % 2 = 0`); err != nil {
		t.Fatalf("delete every other probe row: %v", err)
	}
}

// maintenanceLogMessages reads back what the deployment recorded under the maintenance
// subsystem, one line per entry, each line naming the level it was filed at.
func maintenanceLogMessages(t *testing.T, db *sql.DB) string {
	t.Helper()

	rows, err := db.Query(`SELECT "level", "message" FROM "logs" WHERE "source" = ? ORDER BY "logid"`, "maintenance")
	if err != nil {
		t.Fatalf("read the maintenance log entries: %v", err)
	}

	defer rows.Close()

	var messages []string

	for rows.Next() {
		var level, message string

		if err := rows.Scan(&level, &message); err != nil {
			t.Fatalf("read a maintenance log entry: %v", err)
		}

		messages = append(messages, level+" "+message)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("read the maintenance log entries: %v", err)
	}

	return strings.Join(messages, "\n")
}

// countRows counts the rows one table answers a condition with.
func countRows(t *testing.T, db *sql.DB, table string, where string, args ...any) int {
	t.Helper()

	var count int

	query := fmt.Sprintf(`SELECT COUNT(*) FROM "%s" WHERE %s`, table, where)

	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count the rows of %s: %v", table, err)
	}

	return count
}

// countTables counts the tables of a given name the deployment holds.
func countTables(t *testing.T, db *sql.DB, name string) int {
	t.Helper()

	var count int

	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE "type" = 'table' AND "name" = ?`, name).Scan(&count); err != nil {
		t.Fatalf("look for the table %s: %v", name, err)
	}

	return count
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
