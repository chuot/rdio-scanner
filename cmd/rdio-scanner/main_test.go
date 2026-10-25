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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rdio-scanner/internal/app"
	"rdio-scanner/internal/application/ports"
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
// switches no address or file path can stand in for
// see the operator-switches-reach-server maintainer note.
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
