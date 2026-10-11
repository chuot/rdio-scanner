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
