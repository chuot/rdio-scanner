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
	"path/filepath"
	"testing"
)

// newTestConfig parses args with a controlled executable path and base dir, so the
// tests never depend on where the test binary happens to live.
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

// TestConfigSaveCreatesTheIniFileForItsOwnerOnly (REQ-013, CA-013.2): the ini file
// holds the database password, and on a shared host a world-readable ini hands it
// to every account on the machine. The save creates it readable and writable by
// its owner alone.
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

// TestBcryptCostComesFromTheIniFileAndDefaultsToTen (REQ-013, CA-013.5): the cost
// is an ini option; the default is the legacy server's cost, so a deployment that
// says nothing hashes exactly as it does today.
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

// TestABcryptCostOutsideTheRangeIsRejected (REQ-013, CA-013.5): the range is the
// operator's contract — 10 is today's cost, 14 is the ceiling this server will
// hash at — and a cost outside it is a typo. A server that started with the
// typo's cost would hash the administrator credential at a cost the operator
// never asked for, so the typo is refused at startup.
func TestABcryptCostOutsideTheRangeIsRejected(t *testing.T) {
	for _, cost := range []string{"9", "15"} {
		baseDir := t.TempDir()

		writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "bcrypt_cost = "+cost+"\n")

		if _, _, err := newConfig([]string{"-base_dir", baseDir}, "", &bytes.Buffer{}); err == nil {
			t.Errorf("newConfig accepted bcrypt_cost %s, want a rejection", cost)
		}
	}
}

// TestDatabaseTransportOptionsComeFromTheIniFile (REQ-014, CA-014.1, D12): the
// transport options are ini options the operator opts in to; the defaults say
// nothing, and a deployment that says nothing connects exactly as it connects
// today — plaintext, driver's own timing.
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

// TestAnUnparseableConnectTimeoutIsRejected (REQ-014, CA-014.1, D12): the connect
// timeout is a duration, and "5" is not one — a server that started with a typo's
// timeout would connect on a timing the operator never named.
func TestAnUnparseableConnectTimeoutIsRejected(t *testing.T) {
	for _, timeout := range []string{"5", "bogus"} {
		baseDir := t.TempDir()

		writeFile(t, filepath.Join(baseDir, "rdio-scanner.ini"), "db_connect_timeout = "+timeout+"\n")

		if _, _, err := newConfig([]string{"-base_dir", baseDir}, "", &bytes.Buffer{}); err == nil {
			t.Errorf("newConfig accepted db_connect_timeout %s, want a rejection", timeout)
		}
	}
}
