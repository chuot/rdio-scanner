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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestConfig parses args with a controlled executable path and base
// dir, so tests do not depend on where the test binary lives.
func newTestConfig(t *testing.T, args ...string) (*Config, *bytes.Buffer) {
	t.Helper()

	baseDir := t.TempDir()
	out := &bytes.Buffer{}

	cfg, _, err := newConfig(append([]string{"-base_dir", baseDir}, args...), "", out)
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}

	return cfg, out
}

func TestDefaultsMatchTheLegacyServer(t *testing.T) {
	cfg, _ := newTestConfig(t)

	checks := map[string]struct{ got, want any }{
		"DbType":     {cfg.DbType, DbTypeSqlite},
		"DbFile":     {cfg.DbFile, "rdio-scanner.db"},
		"DbHost":     {cfg.DbHost, "localhost"},
		"DbPort":     {cfg.DbPort, uint(3306)},
		"Listen":     {cfg.Listen, ":3000"},
		"ConfigFile": {cfg.ConfigFile, "rdio-scanner.ini"},
	}

	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
}

func TestIniFileOverridesFlags(t *testing.T) {
	baseDir := t.TempDir()
	writeFile(t, filepath.Join(baseDir, "overridden.ini"), `
db_type = postgresql
db_host = db.internal
db_port = 5433
db_name = rdio
db_user = rdio
db_pass = secret
listen = :9000
ssl_auto_cert = radio.example.com
ssl_cert_file = cert.pem
ssl_key_file = key.pem
ssl_listen = :9443
`)

	cfg, _, err := newConfig([]string{"-base_dir", baseDir, "-listen", ":8080", "-config", "overridden.ini"}, "", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}

	checks := map[string]struct{ got, want any }{
		"DbType":      {cfg.DbType, DbTypePostgresql},
		"DbHost":      {cfg.DbHost, "db.internal"},
		"DbPort":      {cfg.DbPort, uint(5433)},
		"DbName":      {cfg.DbName, "rdio"},
		"DbUsername":  {cfg.DbUsername, "rdio"},
		"DbPassword":  {cfg.DbPassword, "secret"},
		"Listen":      {cfg.Listen, ":9000"},
		"SslAutoCert": {cfg.SslAutoCert, "radio.example.com"},
		"SslCertFile": {cfg.SslCertFile, "cert.pem"},
		"SslKeyFile":  {cfg.SslKeyFile, "key.pem"},
		"SslListen":   {cfg.SslListen, ":9443"},
	}

	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
}

func TestPostgresqlPortDefaultWhenIniOmitsIt(t *testing.T) {
	baseDir := t.TempDir()
	writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "db_type = postgresql\n")

	cfg, _, err := newConfig([]string{"-base_dir", baseDir}, "", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}

	if cfg.DbPort != 5432 {
		t.Errorf("DbPort = %d, want 5432 for postgresql", cfg.DbPort)
	}
}

func TestUnknownDatabaseTypeIsRejected(t *testing.T) {
	cfg, _ := newTestConfig(t)

	writeFile(t, filepath.Join(cfg.BaseDir, "rdio-scanner.ini"), "db_type = oracle\n")

	if _, _, err := newConfig([]string{"-base_dir", cfg.BaseDir}, "", &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error for an unknown database type")
	}
}

func TestConfigSaveWritesTheLegacyIniLayout(t *testing.T) {
	cfg, out := newTestConfig(t, "-config_save")

	if got, want := out.String(), "rdio-scanner.ini file created\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}

	data, err := os.ReadFile(filepath.Join(cfg.BaseDir, "rdio-scanner.ini"))
	if err != nil {
		t.Fatalf("read saved ini: %v", err)
	}

	const want = "db_file = rdio-scanner.db\ndb_type = sqlite\nlisten = :3000\n"
	if string(data) != want {
		t.Errorf("saved ini =\n%q\nwant\n%q", string(data), want)
	}
}

func TestConfigSaveWritesHostSectionForServerDatabases(t *testing.T) {
	cfg, _ := newTestConfig(t, "-db_type", "mysql", "-db_host", "db.internal", "-db_port", "3307",
		"-db_name", "rdio", "-db_user", "rdio", "-db_pass", "secret", "-ssl_listen", ":3443", "-config_save")

	data, err := os.ReadFile(filepath.Join(cfg.BaseDir, "rdio-scanner.ini"))
	if err != nil {
		t.Fatalf("read saved ini: %v", err)
	}

	const want = "db_host = db.internal\ndb_name = rdio\ndb_pass = secret\ndb_port = 3307\ndb_type = mysql\ndb_user = rdio\nlisten = :3000\nssl_listen = :3443\n"
	if string(data) != want {
		t.Errorf("saved ini =\n%q\nwant\n%q", string(data), want)
	}
}

// the MCP surface stays off at the flag, the ini file, and the saved
// file unless the operator asks for it.
func TestMcpServerIsOffUnlessTheOperatorAsksForIt(t *testing.T) {
	t.Run("the flag default is off", func(t *testing.T) {
		cfg, _ := newTestConfig(t)

		if cfg.McpServer {
			t.Errorf("McpServer = %v, want off: the surface is opt-in", cfg.McpServer)
		}
	})

	t.Run("the flag turns it on", func(t *testing.T) {
		cfg, _ := newTestConfig(t, "-mcp_server")

		if !cfg.McpServer {
			t.Errorf("McpServer = %v, want on: -mcp_server asked for the surface", cfg.McpServer)
		}
	})

	t.Run("the ini file turns it on", func(t *testing.T) {
		baseDir := t.TempDir()
		writeFile(t, filepath.Join(baseDir, "mcp.ini"), "mcp_server = true\n")

		cfg, _, err := newConfig([]string{"-base_dir", baseDir, "-config", "mcp.ini"}, "", &bytes.Buffer{})
		if err != nil {
			t.Fatalf("newConfig: %v", err)
		}

		if !cfg.McpServer {
			t.Errorf("McpServer = %v, want on: the ini file names mcp_server = true", cfg.McpServer)
		}
	})

	t.Run("an ini file without the key leaves it off", func(t *testing.T) {
		baseDir := t.TempDir()
		writeFile(t, filepath.Join(baseDir, "plain.ini"), "listen = :9000\n")

		cfg, _, err := newConfig([]string{"-base_dir", baseDir, "-config", "plain.ini"}, "", &bytes.Buffer{})
		if err != nil {
			t.Fatalf("newConfig: %v", err)
		}

		if cfg.McpServer {
			t.Errorf("McpServer = %v, want off: the ini file says nothing about the surface", cfg.McpServer)
		}
	})
}

// -config_save writes the mcp_server key only when the switch is on.
// see the ini-file-compatibility maintainer note.
func TestConfigSaveWritesTheMcpSwitchOnlyWhenItIsOn(t *testing.T) {
	off, _ := newTestConfig(t, "-config_save")

	data, err := os.ReadFile(filepath.Join(off.BaseDir, "rdio-scanner.ini"))
	if err != nil {
		t.Fatalf("read saved ini: %v", err)
	}

	if strings.Contains(string(data), "mcp_server") {
		t.Errorf("saved ini =\n%q\nwant no mcp_server line: the operator never asked for the surface", string(data))
	}

	on, _ := newTestConfig(t, "-mcp_server", "-config_save")

	data, err = os.ReadFile(filepath.Join(on.BaseDir, "rdio-scanner.ini"))
	if err != nil {
		t.Fatalf("read saved ini: %v", err)
	}

	if !strings.Contains(string(data), "mcp_server = true") {
		t.Errorf("saved ini =\n%q\nwant an mcp_server = true line", string(data))
	}
}

// the saved file round-trips the MCP switch: an on deployment keeps the
// surface after a restart, an off one keeps its absence.
func TestTheMcpSwitchSurvivesTheSavedFileRoundTrip(t *testing.T) {
	baseDir := t.TempDir()

	first, _, err := newConfig([]string{"-base_dir", baseDir, "-mcp_server", "-config_save"}, "", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("first start: %v", err)
	}

	second, _, err := newConfig([]string{"-base_dir", baseDir}, "", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("second start: %v", err)
	}

	if !first.McpServer || !second.McpServer {
		t.Errorf("McpServer after the round trip = first %v, second %v; want both on",
			first.McpServer, second.McpServer)
	}

	other := t.TempDir()

	if _, _, err := newConfig([]string{"-base_dir", other, "-config_save"}, "", &bytes.Buffer{}); err != nil {
		t.Fatalf("third start: %v", err)
	}

	quiet, _, err := newConfig([]string{"-base_dir", other}, "", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("fourth start: %v", err)
	}

	if quiet.McpServer {
		t.Errorf("McpServer = %v, want off: a deployment that never asked for the surface stays without it",
			quiet.McpServer)
	}
}

func TestPathsResolveAgainstBaseDir(t *testing.T) {
	cfg, _ := newTestConfig(t)

	if got, want := cfg.GetDbFilePath(), filepath.Join(cfg.BaseDir, "rdio-scanner.db"); got != want {
		t.Errorf("GetDbFilePath = %q, want %q", got, want)
	}

	if got, want := cfg.GetConfigFilePath(), filepath.Join(cfg.BaseDir, "rdio-scanner.ini"); got != want {
		t.Errorf("GetConfigFilePath = %q, want %q", got, want)
	}

	if got, want := cfg.GetPath("/abs/path"), "/abs/path"; got != want {
		t.Errorf("absolute GetPath = %q, want %q", got, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// the saved ini file, which holds the database password, is created
// readable and writable by its owner alone.
func TestConfigSaveCreatesTheIniFileForItsOwnerOnly(t *testing.T) {
	cfg, _ := newTestConfig(t, "-config_save")

	info, err := os.Stat(filepath.Join(cfg.BaseDir, "rdio-scanner.ini"))
	if err != nil {
		t.Fatalf("stat saved ini: %v", err)
	}

	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("the saved ini file is created %o, want 0600: the ini holds the database password", mode)
	}
}

// the bcrypt cost comes from the ini file and defaults to 10.
func TestBcryptCostComesFromTheIniFileAndDefaultsToTen(t *testing.T) {
	cfg, _ := newTestConfig(t)

	if got, want := cfg.BcryptCost, 10; got != want {
		t.Errorf("default bcrypt cost = %d, want %d: the legacy server hashed at 10", got, want)
	}

	baseDir := t.TempDir()

	writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "bcrypt_cost = 12\n")

	out := &bytes.Buffer{}

	cfg, _, err := newConfig([]string{"-base_dir", baseDir}, "", out)
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}

	if got, want := cfg.BcryptCost, 12; got != want {
		t.Errorf("bcrypt cost from the ini = %d, want %d", got, want)
	}

	flagged, _, err := newConfig([]string{"-base_dir", t.TempDir(), "-bcrypt_cost", "13"}, "", out)
	if err != nil {
		t.Fatalf("newConfig with -bcrypt_cost: %v", err)
	}

	if got, want := flagged.BcryptCost, 13; got != want {
		t.Errorf("bcrypt cost from the flag = %d, want %d", got, want)
	}
}

// a bcrypt cost outside the 10 to 14 contract is refused at startup:
// a typo there would hash the administrator credential at a cost the
// operator never asked for.
func TestABcryptCostOutsideTheRangeIsRejected(t *testing.T) {
	for _, cost := range []string{"9", "15"} {
		baseDir := t.TempDir()

		writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "bcrypt_cost = "+cost+"\n")

		if _, _, err := newConfig([]string{"-base_dir", baseDir}, "", &bytes.Buffer{}); err == nil {
			t.Errorf("newConfig accepted bcrypt_cost %s, want a rejection", cost)
		}
	}
}

// database transport options are opt-in ini options, and a deployment
// that says nothing connects with plaintext and the driver's timing.
func TestDatabaseTransportOptionsComeFromTheIniFile(t *testing.T) {
	cfg, _ := newTestConfig(t)

	if cfg.DbTLS != "" || cfg.DbSslMode != "" || cfg.DbConnectTimeout != "" {
		t.Errorf("the default transport options are tls %q, sslmode %q, timeout %q, want all empty: the defaults must not change how a deployment connects", cfg.DbTLS, cfg.DbSslMode, cfg.DbConnectTimeout)
	}

	baseDir := t.TempDir()

	writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "db_tls = true\ndb_sslmode = require\ndb_connect_timeout = 5s\n")

	out := &bytes.Buffer{}

	cfg, _, err := newConfig([]string{"-base_dir", baseDir}, "", out)
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}

	if got, want := cfg.DbTLS, "true"; got != want {
		t.Errorf("db_tls = %q, want %q", got, want)
	}

	if got, want := cfg.DbSslMode, "require"; got != want {
		t.Errorf("db_sslmode = %q, want %q", got, want)
	}

	if got, want := cfg.DbConnectTimeout, "5s"; got != want {
		t.Errorf("db_connect_timeout = %q, want %q", got, want)
	}
}

// an unparseable connect timeout is rejected: a server must not connect
// on a timing the operator never named.
func TestAnUnparseableConnectTimeoutIsRejected(t *testing.T) {
	for _, timeout := range []string{"5", "bogus"} {
		baseDir := t.TempDir()

		writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "db_connect_timeout = "+timeout+"\n")

		if _, _, err := newConfig([]string{"-base_dir", baseDir}, "", &bytes.Buffer{}); err == nil {
			t.Errorf("newConfig accepted db_connect_timeout %s, want a rejection", timeout)
		}
	}
}

// the ini file overrides a setting only when it actually carries that setting:
// a value the operator typed on the command line survives an ini file that
// leaves it out, the way every other setting in this file behaves.
func TestAFlagSurvivesAnIniFileThatLeavesTheSettingOut(t *testing.T) {
	baseDir := t.TempDir()

	writeFile(t, filepath.Join(baseDir, "operator.ini"), "db_type = mysql\ndb_host = db.internal\ndb_name = rdio\n")

	cfg, _, err := newConfig([]string{"-base_dir", baseDir, "-config", "operator.ini",
		"-db_port", "6000", "-bcrypt_cost", "13"}, "", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}

	checks := map[string]struct{ got, want any }{
		"DbPort":     {cfg.DbPort, uint(6000)},
		"BcryptCost": {cfg.BcryptCost, 13},
		"DbHost":     {cfg.DbHost, "db.internal"},
	}

	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v: an ini file that leaves a setting out must leave the operator's flag alone", name, c.got, c.want)
		}
	}
}

// the port that comes with a database type is the port the operator gets whether
// or not an ini file is in the way: the postgresql default is the postgres port.
func TestAPostgresDatabaseWithoutAnIniFileGetsThePostgresPort(t *testing.T) {
	cfg, _ := newTestConfig(t, "-db_type", "postgresql", "-db_host", "db.internal", "-db_name", "rdio")

	if got, want := cfg.DbPort, uint(5432); got != want {
		t.Errorf("DbPort = %d, want %d: a postgresql database with no ini file uses the postgres port", got, want)
	}
}

// a saved file decides the port for every later boot, so a save must not write a
// port the server's own answer for the chosen database type contradicts.
func TestConfigSaveDoesNotPinTheOtherDatabasesPortForPostgres(t *testing.T) {
	cfg, _ := newTestConfig(t, "-db_type", "postgresql", "-db_host", "db.internal", "-db_name", "rdio", "-config_save")

	data, err := os.ReadFile(filepath.Join(cfg.BaseDir, "rdio-scanner.ini"))
	if err != nil {
		t.Fatalf("read saved ini: %v", err)
	}

	if strings.Contains(string(data), "db_port = 3306") {
		t.Errorf("saved ini =\n%q\nwant no db_port line: 3306 is the port of a different database type, and this file pins it for every later boot", string(data))
	}
}

// an ini file the server cannot read stops the start and names the file, the
// way the admin guide says it does; today the read failure is swallowed and the
// server boots on its built-in defaults while the operator believes their file
// is in force.
func TestAnUnreadableIniFileStopsTheStartAndNamesTheFile(t *testing.T) {
	baseDir := t.TempDir()

	writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "this line is not a setting\n")

	cfg, _, err := newConfig([]string{"-base_dir", baseDir}, "", &bytes.Buffer{})
	if err == nil {
		t.Fatalf("newConfig accepted an ini file it cannot read: listen = %q, want a failure naming rdio-scanner.ini", cfg.Listen)
	}

	if !strings.Contains(err.Error(), "rdio-scanner.ini") {
		t.Errorf("error = %q, want it to name rdio-scanner.ini: the operator has to know which file the server could not read", err.Error())
	}
}

// the file a save writes is the file the next start reads: a credential the
// operator chose has to come back out of the file unchanged, so the server must
// write it in a form its own ini reader reads whole.
func TestConfigSaveWritesTheCredentialTheServerReadsBack(t *testing.T) {
	for _, secret := range []string{"ses#am", "ses;am", "ses am"} {
		cfg, _ := newTestConfig(t, "-db_type", "mysql", "-db_host", "db.internal", "-db_name", "rdio",
			"-db_pass", secret, "-config_save")

		reread, _, err := newConfig([]string{"-base_dir", cfg.BaseDir}, "", &bytes.Buffer{})
		if err != nil {
			t.Fatalf("newConfig reading the file it saved for %q: %v", secret, err)
		}

		if reread.DbPassword != secret {
			t.Errorf("a saved ini reads back the database password %q, want %q: the server wrote a value its own reader cuts short", reread.DbPassword, secret)
		}
	}
}

// a save writes the configuration the operator will start from, so it must not
// write a file that the very next start refuses to read: the same database type
// the server rejects at startup cannot be saved.
func TestConfigSaveDoesNotWriteAFileTheServerRefusesToStartFrom(t *testing.T) {
	cfg, _ := newTestConfig(t, "-db_type", "oracle", "-db_host", "db.internal", "-db_name", "rdio", "-config_save")

	_, _, nextErr := newConfig([]string{"-base_dir", cfg.BaseDir}, "", &bytes.Buffer{})
	if nextErr == nil {
		return
	}

	saved, _ := os.ReadFile(filepath.Join(cfg.BaseDir, "rdio-scanner.ini"))
	t.Errorf("config_save wrote an rdio-scanner.ini the next start refuses: %v\nsaved ini =\n%q", nextErr, string(saved))
}

// a server run from source has no base directory to be given: the default
// refuses to call a Go build cache a deployment folder. The folder its database
// file, its configuration file and its state actually land in is the folder the
// operator started it in, because every relative path is joined onto the base
// directory and an empty one leaves the operating system to resolve the path
// from the working directory. So the configuration names that folder: the paths,
// the start-up notice and the dashboard's serving folder figure all read the one
// value, and a blank figure means the server does not know where it writes.
func TestAnEmptyBaseDirectoryBecomesTheFolderTheServerWasStartedIn(t *testing.T) {
	launch := t.TempDir()
	t.Chdir(launch)

	cfg, _, err := newConfig([]string{"-base_dir", ""}, "", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}

	if cfg.BaseDir != launch {
		t.Errorf("the serving folder is %q, want %q: the folder the server was started in is the folder it writes into", cfg.BaseDir, launch)
	}

	dbFile := cfg.GetDbFilePath()
	if filepath.Dir(dbFile) != launch {
		t.Errorf("the database file resolves to %q, want it inside %q: the file the server opens and the folder it reports are the same folder", dbFile, launch)
	}
}

// the guide says the base directory must be writable, and the start checks it
// by writing a temporary file there. With no base directory named, that check
// falls back to the system temporary directory and says nothing about the folder
// the server really writes into, so the check runs against the folder in use:
// an operator who starts the server somewhere they cannot write learns that at
// the start, and learns which folder they cannot write.
func TestALaunchFolderThatCannotBeWrittenStopsTheStartAndNamesTheFolder(t *testing.T) {
	launch := t.TempDir()

	if err := os.Chmod(launch, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chmod(launch, 0o700); err != nil {
			t.Errorf("chmod back: %v", err)
		}
	})

	t.Chdir(launch)

	_, _, err := newConfig([]string{"-base_dir", "", "-db_file", filepath.Join(t.TempDir(), "rdio-scanner.db")}, "", &bytes.Buffer{})
	if err == nil {
		t.Fatalf("newConfig started a server from a folder it cannot write: want the start refused")
	}

	if !strings.Contains(err.Error(), launch) {
		t.Errorf("error = %q, want it to name %q: an operator told only that a folder is unwritable is not told which folder", err.Error(), launch)
	}
}

// the admin guide says -config_save rewrites the file from what the server
// currently holds, and the precedence it states puts the ini file between the
// flags and the built-in defaults. The settings the file already held are part
// of what the server holds, so a save has to carry them into the file it writes:
// a save that never reads the file replaces the operator's whole configuration
// with the built-in defaults.
func TestConfigSaveKeepsTheSettingsTheIniFileAlreadyHolds(t *testing.T) {
	baseDir := t.TempDir()

	writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"),
		"db_type = mysql\ndb_host = db.internal\ndb_name = rdio\ndb_user = rdio\nlisten = :3001\n")

	cfg, _, err := newConfig([]string{"-base_dir", baseDir, "-config_save"}, "", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}

	saved, err := os.ReadFile(filepath.Join(cfg.BaseDir, "rdio-scanner.ini"))
	if err != nil {
		t.Fatalf("read saved ini: %v", err)
	}

	for _, setting := range []string{"db_type = mysql", "db_host = db.internal", "db_name = rdio", "listen = :3001"} {
		if !strings.Contains(string(saved), setting) {
			t.Errorf("saved ini =\n%q\nwant it to keep %q: a save that reads the file it is rewriting keeps the settings the operator's file already held", string(saved), setting)
		}
	}
}

// -vacuum asks for the one-shot compacting task: the flag turns it on, and
// nothing turns it on by itself — an ini file has no key for it, because a
// saved deployment that vacuumed once must not vacuum on every start.
func TestTheVacuumFlagAsksForTheOneShotTask(t *testing.T) {
	cfg, _ := newTestConfig(t)

	if cfg.Vacuum {
		t.Errorf("Vacuum = %v, want off: the task runs only when the operator asks for it", cfg.Vacuum)
	}

	cfg, _ = newTestConfig(t, "-vacuum")

	if !cfg.Vacuum {
		t.Errorf("Vacuum = %v, want on: -vacuum asked for the task", cfg.Vacuum)
	}
}

// -config_save writes no vacuum key: the task is an action the operator takes,
// not a setting the deployment carries, like -admin_password and -cmd before it.
func TestConfigSaveWritesNoVacuumKey(t *testing.T) {
	cfg, _ := newTestConfig(t, "-vacuum", "-config_save")

	saved, err := os.ReadFile(filepath.Join(cfg.BaseDir, "rdio-scanner.ini"))
	if err != nil {
		t.Fatalf("read saved ini: %v", err)
	}

	if strings.Contains(string(saved), "vacuum") {
		t.Errorf("saved ini =\n%q\nwant no vacuum line: a deployment that vacuumed once must not vacuum on every start", string(saved))
	}
}

// the task rewrites one sqlite file, so a deployment on another database has
// nothing for it to compact: the run is refused, naming the type, before any
// connection opens. The decision waits for the ini overlay, because the type
// the server would use can come from the operator's file rather than the
// command line.
func TestVacuumIsRefusedOnANonSqliteDatabase(t *testing.T) {
	for _, dbType := range []string{DbTypeMariadb, DbTypeMysql, DbTypePostgresql} {
		t.Run(dbType+" named on the command line", func(t *testing.T) {
			_, _, err := newConfig([]string{"-base_dir", t.TempDir(), "-vacuum", "-db_type", dbType}, "", &bytes.Buffer{})
			if err == nil {
				t.Fatalf("newConfig with -vacuum -db_type %s: want a refusal, got none", dbType)
			}

			want := fmt.Sprintf("the vacuum task needs the sqlite database, not %s", dbType)
			if err.Error() != want {
				t.Errorf("refusal = %q, want %q", err.Error(), want)
			}
		})
	}

	t.Run("the ini file names the other type", func(t *testing.T) {
		baseDir := t.TempDir()
		writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "db_type = mysql\ndb_host = db.internal\ndb_name = rdio\n")

		_, _, err := newConfig([]string{"-base_dir", baseDir, "-vacuum"}, "", &bytes.Buffer{})
		if err == nil {
			t.Fatal("newConfig with -vacuum over an ini file that says db_type = mysql: want a refusal, got none")
		}

		want := "the vacuum task needs the sqlite database, not mysql"
		if err.Error() != want {
			t.Errorf("refusal = %q, want %q", err.Error(), want)
		}
	})

	t.Run("the command line cannot outvote the ini file", func(t *testing.T) {
		baseDir := t.TempDir()
		writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "db_type = postgresql\n")

		_, _, err := newConfig([]string{"-base_dir", baseDir, "-vacuum", "-db_type", DbTypeSqlite}, "", &bytes.Buffer{})
		if err == nil {
			t.Fatal("newConfig with -vacuum -db_type sqlite over an ini file that says db_type = postgresql: want a refusal, got none: the operator's file outranks the flag, so this deployment is postgresql")
		}

		want := "the vacuum task needs the sqlite database, not postgresql"
		if err.Error() != want {
			t.Errorf("refusal = %q, want %q", err.Error(), want)
		}
	})

	t.Run("an sqlite deployment the ini file names is accepted", func(t *testing.T) {
		baseDir := t.TempDir()
		writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "db_type = sqlite\ndb_file = custom.db\n")

		cfg, _, err := newConfig([]string{"-base_dir", baseDir, "-vacuum"}, "", &bytes.Buffer{})
		if err != nil {
			t.Fatalf("newConfig with -vacuum over an ini file that says db_type = sqlite: %v", err)
		}

		if !cfg.Vacuum {
			t.Errorf("Vacuum = %v, want on", cfg.Vacuum)
		}
	})
}
