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
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// TestVacuumCompactsTheDeploymentThroughTheCompositionRoot runs the task the
// operator asks for from the composition root: the sqlite file the server
// holds is compacted in place, the free pages measured before the statement
// are gone after it, and the rows that were not deleted read back unchanged.
func TestVacuumCompactsTheDeploymentThroughTheCompositionRoot(t *testing.T) {
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

	if _, err := srv.db.Exec(`CREATE TABLE vacuum_probe (id INTEGER PRIMARY KEY, body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	for i := range 500 {
		body := strings.Repeat("x", 200)

		if _, err := srv.db.Exec(`INSERT INTO vacuum_probe (body) VALUES (?)`, body); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	if _, err := srv.db.Exec(`DELETE FROM vacuum_probe WHERE id % 2 = 0`); err != nil {
		t.Fatalf("delete every other row: %v", err)
	}

	before, after, err := srv.Vacuum(ctx)
	if err != nil {
		t.Fatalf("Vacuum: %v", err)
	}

	if before.FreelistCount == 0 {
		t.Error("the vacuum reports no free pages before the statement, want the deleted rows' pages on record")
	}

	if after.FreelistCount != 0 {
		t.Errorf("the vacuum reports %d free pages after the statement, want them all reclaimed", after.FreelistCount)
	}

	if after.PageCount >= before.PageCount {
		t.Errorf("the vacuum reports %d pages after and %d before, want the file shrink", after.PageCount, before.PageCount)
	}

	if after.PageSize != before.PageSize || after.PageSize == 0 {
		t.Errorf("the vacuum reports page size %d after and %d before, want the file's own page size on both sides", after.PageSize, before.PageSize)
	}

	var kept int

	if err := srv.db.QueryRow(`SELECT COUNT(*) FROM vacuum_probe`).Scan(&kept); err != nil {
		t.Fatalf("count the kept rows: %v", err)
	}

	if kept != 250 {
		t.Errorf("the compacted file holds %d rows, want the 250 that were not deleted", kept)
	}
}

// TestVacuumRefusesADatabaseTheServerDoesNotCompact keeps the composition
// root honest when the operation is called directly: a server wired to a
// backend it cannot compact is refused by name, and the database is not
// touched — the pool behind a non-sqlite server stays nil, and the refusal
// never reaches for it.
func TestVacuumRefusesADatabaseTheServerDoesNotCompact(t *testing.T) {
	ctx := context.Background()

	for _, dialect := range []persistence.Dialect{
		persistence.DialectMariadb,
		persistence.DialectMysql,
		persistence.DialectPostgresql,
	} {
		srv := &Server{dialect: dialect}

		_, _, err := srv.Vacuum(ctx)
		if err == nil {
			t.Errorf("%s: the vacuum ran against a backend it cannot compact, want a refusal", dialect)

			continue
		}

		if !strings.Contains(err.Error(), string(dialect)) {
			t.Errorf("%s: the refusal says %q, want it to name the backend the server is wired to", dialect, err)
		}
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

// TestASecondServerAgainstALiveDeploymentIsRefused claims the
// deployment before the database is opened: a second composition root
// pointed at the same files is refused as in use, and the refusal
// opens no database and writes nothing into the deployment.
func TestASecondServerAgainstALiveDeploymentIsRefused(t *testing.T) {
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

	if _, err := os.Stat(filepath.Join(base, "rdio-scanner.lock")); err != nil {
		t.Errorf("the running server holds no deployment lock where the base directory says: %v", err)
	}

	before, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("reading the deployment folder: %v", err)
	}

	second, err := New(testConfigIn(base, io.Discard))
	if err == nil {
		_ = second.Close()
		t.Fatal("a second server against a live deployment started, want it refused as in use")
	}

	if !errors.Is(err, ErrInUse) {
		t.Errorf("the refusal is %v, want it to say the deployment is in use", err)
	}

	if second != nil {
		t.Error("the refused server came back assembled, want nothing assembled")
	}

	after, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("reading the deployment folder after the refusal: %v", err)
	}

	if len(after) != len(before) {
		t.Errorf("the refused server left %d files in the deployment where it found %d, want it to open no database of its own", len(after), len(before))
	}
}

// TestANewServerStartsAfterTheDeploymentIsClosed releases the claim
// when the server closes: a new server over the same files starts, so
// the lock belongs to the running server and not to the folder.
func TestANewServerStartsAfterTheDeploymentIsClosed(t *testing.T) {
	base := t.TempDir()

	srv, err := New(testConfigIn(base, io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := New(testConfigIn(base, io.Discard))
	if err != nil {
		t.Fatalf("New after Close: %v", err)
	}

	defer func() {
		if err := second.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
}
