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
	"os"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"rdio-scanner/internal/app"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/infrastructure/ffmpeg"
	"rdio-scanner/internal/infrastructure/persistence"
)

// These are the tests of the command line itself: the modes the program reaches before it starts a server,
// and the exit codes it ends with. Everything below drives `run`, the whole program, from the arguments an
// operator types.

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

	if code := run([]string{"-base_dir", base, "-admin_password", "a-chosen-one"}, io.Discard); code != 0 {
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

// testConfigIn is the configuration a test builds a server from on a folder the test owns: the same
// settings the command line produces for a plain deployment, with the version the recorded runs were
// captured against and the ffmpeg the host installs.
func testConfigIn(base string) app.Config {
	return app.Config{
		BaseDir: base,
		DbFile:  "rdio-scanner.db",
		DbType:  app.DbTypeSqlite,
		Listen:  ":3000",
		Version: theCommandVersion,
		Out:     io.Discard,

		// The converter is the ffmpeg the host installs: the one the command-line tests' server
		// converts with, so that what a test proves is the program and not what one build carries.
		Converter: func(dir string) (ports.AudioConverter, string) {
			return ffmpeg.New(), ""
		},
	}
}

// commandTestConfig is the configuration a test builds a command-mode server from: the settings the
// command line produces for a plain deployment, with the ffmpeg the host installs, because the converter
// the command line chooses is a fact about the binary being built and a command-mode test is about the
// program talking to a server.
func commandTestConfig(t *testing.T) app.Config {
	config := testConfigIn(t.TempDir())

	config.Converter = func(dir string) (ports.AudioConverter, string) {
		return ffmpeg.New(), ""
	}

	return config
}

// Every capture in this repository was taken with the server's time zone set to UTC, and the recorded
// answers are compared against what a server in UTC wrote. A server started in a machine's own time zone
// reads the timestamps its fixtures name as different moments, so the tests in this package run in UTC.
func init() {
	os.Setenv("TZ", "UTC")
}

// theCommandVersion is the version a test's server reports. The version a build injects is a fact about the
// build, not a fact about a server, so these tests name the version they configure the server with.
const theCommandVersion = "8.0.0"

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

// loginAnswered starts a server against a base directory the test has already prepared and reports how a
// login was answered.
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
