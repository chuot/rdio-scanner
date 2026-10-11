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
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/persistence"
)

// The configuration is the whole of what a deployment says about itself, and the composition root is the
// only thing that reads it. Two facts about it belong here: the paths, which the server resolves against
// the directory it was installed in, and the backend names, which are the strings an ini file and a
// command line carry. The third thing this file proves is the one mode the composition root runs that is
// not a server: setting the administrator's credential from a script, which opens the database, writes
// the whole configuration, and closes it again.

// Every path the server uses is resolved against the base directory, and an absolute path is the path the
// operator gave. A relative path joined twice is a file nobody can find; an absolute path joined onto the
// base directory is a file nobody can find.
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

// The folder the server writes the programs it carries into is not a path the operator configures:
// it is the server's own folder in the system's temp directory, disposable by rule — a host that
// clears its temp dir costs the server nothing beyond writing the programs out again at the next
// startup — and shared with the probe files the length probe writes and removes.
func TestTheFfmpegFolderIsTheServersOwnInTempDirectory(t *testing.T) {
	config := Config{BaseDir: "/srv/rdio"}

	want := filepath.Join(os.TempDir(), "rdio-scanner.ffmpeg")

	if got := config.GetFFmpegDirPath(); got != want {
		t.Errorf("the ffmpeg folder is %q, want %q", got, want)
	}
}

// The backend names are part of the configuration contract: they appear in rdio-scanner.ini and on the
// command line of deployed instances. A name the server cannot open a database under is a configuration
// it refuses to start with.
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

// The -admin_password mode is the way a deployment sets its credential from a script, and the way an
// operator recovers a server whose credential was lost. It writes the whole configuration, which is also
// what stores the signing secret the options read generated, and it says in the log view what it did. The
// credential the server ships with is one the web application insists on replacing, so the mode marks it
// as needing replacement and marks a credential the operator chose as not needing one.
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

	// The credential the server ships with is the one the web application insists on replacing, so the
	// mode that stores it marks it as needing replacement.
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

// The base directory is the directory the server was installed in, and the paths it resolves are the
// paths a deployment's files are at. A server pointed at a directory it has never written to opens its
// own database there, which is what makes the base directory the one fact a deployment has to state.
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
