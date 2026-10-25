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
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/persistence"
)

// The configuration is the whole of what a deployment says about itself,
// and the composition root is the only thing that reads it: the paths,
// resolved against the installation directory, the backend names an ini
// file and a command line carry, and the credential-setting script mode.

// TestTheConfigurationResolvesEveryPathAgainstTheBaseDirectory joins
// relative paths onto the base directory and leaves absolute paths alone.
func TestTheConfigurationResolvesEveryPathAgainstTheBaseDirectory(t *testing.T) {
	config := Config{
		BaseDir:     "/srv/rdio",
		DbFile:      "rdio-scanner.db",
		SslCertFile: "cert.pem",
		SslKeyFile:  "key.pem",
	}

	for _, c := range []struct{ got, want string }{
		{config.GetDbFilePath(), "/srv/rdio/rdio-scanner.db"},
		{config.GetSslCertFilePath(), "/srv/rdio/cert.pem"},
		{config.GetSslKeyFilePath(), "/srv/rdio/key.pem"},
		{config.GetAutocertCachePath(), "/srv/rdio/autocert"},
	} {
		if c.got != c.want {
			t.Errorf("the path is %q, want %q", c.got, c.want)
		}
	}

	config.SslCertFile = "/etc/ssl/cert.pem"

	if got := config.GetSslCertFilePath(); got != "/etc/ssl/cert.pem" {
		t.Errorf("an absolute certificate path resolves to %q, want the path the operator gave", got)
	}
}

// TestTheFfmpegFolderIsTheServersOwnInTempDirectory puts the carried
// programs in the server's own folder in the system temp directory.
// see the ffmpeg-carried-by-the-server-and-hosted-by-the-operator maintainer note.
func TestTheFfmpegFolderIsTheServersOwnInTempDirectory(t *testing.T) {
	config := Config{BaseDir: "/srv/rdio"}

	want := filepath.Join(os.TempDir(), "rdio-scanner.ffmpeg")

	if got := config.GetFFmpegDirPath(); got != want {
		t.Errorf("the ffmpeg folder is %q, want %q", got, want)
	}
}

// TestTheDatabaseBackendsAreTheNamesAConfigurationCarries pins the
// backend names an ini file and a command line carry, which the server
// refuses to start without.
func TestTheDatabaseBackendsAreTheNamesAConfigurationCarries(t *testing.T) {
	for _, name := range []string{DbTypeSqlite, DbTypeMariadb, DbTypeMysql, DbTypePostgresql} {
		if _, err := dialectOf(name); err != nil {
			t.Errorf("%s is a backend a configuration carries that the server cannot open: %v", name, err)
		}
	}

	if _, err := dialectOf("not-a-backend"); err == nil {
		t.Error("a backend the configuration does not carry opens a database, want it refused")
	}
}

// TestChangeAdminPasswordStoresTheCredentialAndSaysSoInTheLogView stores
// the chosen credential and says so in the log view, and it marks the
// shipped default as needing replacement.
func TestChangeAdminPasswordStoresTheCredentialAndSaysSoInTheLogView(t *testing.T) {
	ctx := context.Background()

	srv, err := New(testConfigIn(t.TempDir(), io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	defer func() {
		if err := srv.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if err := srv.ChangeAdminPassword(ctx, "a-credential-the-operator-chose"); err != nil {
		t.Fatalf("ChangeAdminPassword: %v", err)
	}

	options, err := persistence.NewOptions(srv.db, srv.dialect).ReadOptions(ctx)
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if options.AdminPasswordHash == "" {
		t.Error("the credential the operator chose is not on record")
	}

	if options.AdminPasswordNeedChange {
		t.Error("a credential the operator chose is marked as needing replacement, want it marked as settled")
	}

	if !hasLogMessage(t, srv.db, "the administrator password was changed") {
		t.Error("the log view does not say the credential was changed")
	}

	if err := srv.ChangeAdminPassword(ctx, domain.DefaultAdminPassword); err != nil {
		t.Fatalf("ChangeAdminPassword with the default credential: %v", err)
	}

	options, err = persistence.NewOptions(srv.db, srv.dialect).ReadOptions(ctx)
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}

	if !options.AdminPasswordNeedChange {
		t.Error("the credential the server ships with is not marked as needing replacement")
	}
}

// TestAServerPointedAtAFreshDirectoryPutsItsFilesWhereTheBaseDirectorySays writes
// its own database into a directory it has never written to.
func TestAServerPointedAtAFreshDirectoryPutsItsFilesWhereTheBaseDirectorySays(t *testing.T) {
	base := t.TempDir()

	srv, err := New(testConfigIn(base, io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	defer func() {
		if err := srv.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if _, err := srv.db.Exec(`SELECT "logid" FROM "logs"`); err != nil {
		t.Errorf("the server opened a database at %q that does not hold the log view: %v", base, err)
	}

	if _, err := (os.Stat(filepath.Join(base, "rdio-scanner.db"))); err != nil {
		t.Errorf("the database the server opened is not the file the configuration names: %v", err)
	}
}
