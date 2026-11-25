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
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
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

// TestTheFfmpegFolderIsTheServersOwnInTempDirectory puts the carried programs in
// the server's own folder in the system temp directory.
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

// maintenanceDeploymentUnits is the catalog a maintenance run is tested against: two rows of the
// shape a legacy migration leaves — a radio reference, and a label that is that reference written
// out — beside three survivors that are each one shape a looser rule would wrongly remove: a unit
// with a described range, a unit with no radio reference at all, and a number an operator named.
var maintenanceDeploymentUnits = []domain.Unit{
	{ID: 301, Label: "Engine 12", Order: 1, SystemID: 101, UnitRef: 1234, UnitFrom: 1200, UnitTo: 1299},
	{ID: 302, Label: "Car 4", Order: 2, SystemID: 101},
	{ID: 303, Label: "6789", Order: 0, SystemID: 101, UnitRef: 6789},
	{ID: 304, Label: "1514000", Order: 0, SystemID: 101, UnitRef: 1514000},
	{ID: 305, Label: "6789 Engine", Order: 3, SystemID: 101, UnitRef: 6789},
}

const (
	// maintenanceDamagedUnits is how many of those rows are the shape a run clears.
	maintenanceDamagedUnits = 2

	// maintenanceLegacyTable is one of the tables the legacy server wrote, and maintenanceLegacyRows
	// is how many rows it holds while it stands. The rows are wide enough that dropping the table
	// returns pages to the file, which is what makes the order of a run's two halves observable: the
	// compaction measures a file the clearing has already released pages into. Only its name matters
	// to a clearing, which counts a legacy table and drops it.
	maintenanceLegacyTable = "rdioScannerCalls"
	maintenanceLegacyRows  = 800
)

// maintenanceRowsCarryingAReference is the maintenance catalog without the row that holds no radio
// reference. The two rows a run clears are among what is left, and no row that is left is one the startup
// report reads as damage: a deployment cleared by a run is a deployment a start has nothing to report.
func maintenanceRowsCarryingAReference() []domain.Unit {
	kept := []domain.Unit{}

	for _, unit := range maintenanceDeploymentUnits {
		if unit.UnitRef == 0 {
			continue
		}

		kept = append(kept, unit)
	}

	return kept
}

// seedMaintenanceDeployment writes the whole fixture catalog into a deployment the server has already
// brought to its own schema.
func seedMaintenanceDeployment(t *testing.T, db *sql.DB) {
	t.Helper()

	seedMaintenanceUnits(t, db, maintenanceDeploymentUnits...)
}

// seedMaintenanceUnits writes the fixture's system and the given unit rows into a deployment the server
// has already brought to its own schema, so the run is tested against stored rows rather than against
// rows the composition root made up.
func seedMaintenanceUnits(t *testing.T, db *sql.DB, units ...domain.Unit) {
	t.Helper()

	statements := []seedStatement{
		{
			query: `INSERT INTO "systems" ("systemId", "label", "systemRef") VALUES (?, ?, ?)`,
			args:  []any{101, "Grand Metro", 10001},
		},
	}

	for _, unit := range units {
		statements = append(statements, seedStatement{
			query: `INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef", "unitFrom", "unitTo") VALUES (?, ?, ?, ?, ?, ?, ?)`,
			args:  []any{unit.ID, unit.Label, unit.Order, unit.SystemID, unit.UnitRef, unit.UnitFrom, unit.UnitTo},
		})
	}

	seed(t, db, statements...)
}

// createMaintenanceLegacyTable fills one legacy table with rows wide enough that dropping it leaves
// its pages on the file's freelist.
func createMaintenanceLegacyTable(t *testing.T, db *sql.DB) {
	t.Helper()

	const body = "a row the legacy server wrote and this server never reads, repeated until the table is wide enough to matter: " +
		"a row the legacy server wrote and this server never reads, repeated until the table is wide enough to matter."

	if _, err := db.Exec(`CREATE TABLE "` + maintenanceLegacyTable + `" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "body" text NOT NULL)`); err != nil {
		t.Fatalf("creating the legacy table: %v", err)
	}

	for i := range maintenanceLegacyRows {
		if _, err := db.Exec(`INSERT INTO "`+maintenanceLegacyTable+`" ("body") VALUES (?)`, body); err != nil {
			t.Fatalf("inserting legacy row %d: %v", i, err)
		}
	}
}

// seedMaintenanceFreelist deletes a run of rows so the file holds free pages a compaction would
// reclaim: a run that compacts nothing is only proved to have compacted nothing where there was
// something to compact. The rows are deleted as one run rather than alternately, because a backend
// returns a page to the file only when nothing is left on it, and deleting every other row leaves
// every page with something on it.
func seedMaintenanceFreelist(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`CREATE TABLE maintenance_probe ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "body" text NOT NULL)`); err != nil {
		t.Fatalf("creating the probe table: %v", err)
	}

	const probeRows = 400

	body := strings.Repeat("x", 300)

	for i := range probeRows {
		if _, err := db.Exec(`INSERT INTO maintenance_probe ("body") VALUES (?)`, body); err != nil {
			t.Fatalf("inserting probe row %d: %v", i, err)
		}
	}

	if _, err := db.Exec(`DELETE FROM maintenance_probe WHERE "id" BETWEEN 101 AND 300`); err != nil {
		t.Fatalf("deleting the run of probe rows: %v", err)
	}
}

// filePages reads the same three facts the compaction measures, so a test can set what the run's own
// measurements say against what the file held before the run.
func filePages(t *testing.T, db *sql.DB) persistence.VacuumMeasurements {
	t.Helper()

	var pages persistence.VacuumMeasurements

	if err := db.QueryRow(`PRAGMA page_size`).Scan(&pages.PageSize); err != nil {
		t.Fatalf("reading the page size: %v", err)
	}

	if err := db.QueryRow(`PRAGMA page_count`).Scan(&pages.PageCount); err != nil {
		t.Fatalf("reading the page count: %v", err)
	}

	if err := db.QueryRow(`PRAGMA freelist_count`).Scan(&pages.FreelistCount); err != nil {
		t.Fatalf("reading the free pages: %v", err)
	}

	return pages
}

// maintenanceTablePresent answers whether a table is standing in the deployment, which is how a test
// checks a dropped one stayed dropped.
func maintenanceTablePresent(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var standing int

	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&standing); err != nil {
		t.Fatalf("asking whether %s stands: %v", name, err)
	}

	return standing > 0
}

// unitNamed finds one unit in the account a run gives back.
func unitNamed(units []domain.Unit, id uint64) (domain.Unit, bool) {
	for _, unit := range units {
		if unit.ID == id {
			return unit, true
		}
	}

	return domain.Unit{}, false
}

// TestMaintainClearsTheDeploymentBeforeItCompactsIt runs the operator's maintenance pass through the
// composition root and reads the order of its two halves off the file: the clearing drops the legacy
// table and takes out the damaged rows first, so the compaction measures a file already holding the
// pages that drop released, and reclaims them. A run that compacted before it cleared would measure
// an empty freelist and leave those pages in the file behind. The removal counts and the page facts
// come back in one summary, which is what lets the run answer once instead of twice.
func TestMaintainClearsTheDeploymentBeforeItCompactsIt(t *testing.T) {
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

	seedMaintenanceDeployment(t, srv.db)
	createMaintenanceLegacyTable(t, srv.db)

	before := filePages(t, srv.db)

	summary, err := srv.Maintain(ctx)
	if err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	if summary.CompactionSkip != "" {
		t.Errorf("the run says %q about its compaction, want it to have compacted the sqlite file it holds", summary.CompactionSkip)
	}

	if summary.Before.FreelistCount <= before.FreelistCount {
		t.Errorf("the compaction measured %d free pages against the %d the file held before the run, want the pages the clearing released to be on the freelist it measured: the compaction ran before the clearing",
			summary.Before.FreelistCount, before.FreelistCount)
	}

	if summary.After.FreelistCount != 0 {
		t.Errorf("the run leaves %d free pages in the file, want the compaction to have reclaimed the pages the clearing released", summary.After.FreelistCount)
	}

	if summary.After.PageCount >= summary.Before.PageCount {
		t.Errorf("the file holds %d pages after the run and %d before it, want the compaction to have shrunk it", summary.After.PageCount, summary.Before.PageCount)
	}

	if summary.After.PageSize != summary.Before.PageSize || summary.After.PageSize == 0 {
		t.Errorf("the run reports page size %d after and %d before, want the file's own page size on both sides", summary.After.PageSize, summary.Before.PageSize)
	}

	if summary.UnitsRemoved != maintenanceDamagedUnits {
		t.Errorf("the run removed %d unit rows, want the %d labelled with their own radio numbers", summary.UnitsRemoved, maintenanceDamagedUnits)
	}

	if summary.TablesDropped != 1 {
		t.Errorf("the run dropped %d legacy tables, want the one the deployment held", summary.TablesDropped)
	}

	if len(summary.DroppedTables) != 1 {
		t.Fatalf("the run names %v, want the one legacy table it dropped", summary.DroppedTables)
	}

	if summary.DroppedTables[0].Name != maintenanceLegacyTable {
		t.Errorf("the run dropped %s, want %s", summary.DroppedTables[0].Name, maintenanceLegacyTable)
	}

	if summary.DroppedTables[0].Rows != maintenanceLegacyRows {
		t.Errorf("%s went with %d rows, want the %d rows it held", maintenanceLegacyTable, summary.DroppedTables[0].Rows, maintenanceLegacyRows)
	}

	if summary.DroppedArchive || summary.ArchiveRowsRemoved != 0 {
		t.Errorf("the run names %d archived rows and an archive table %v, want nothing said about an archive table: this deployment's migration had nothing to archive",
			summary.ArchiveRowsRemoved, summary.DroppedArchive)
	}

	for _, want := range []domain.Unit{maintenanceDeploymentUnits[2], maintenanceDeploymentUnits[3]} {
		got, found := unitNamed(summary.RemovedUnits, want.ID)

		if !found {
			t.Errorf("the run does not name unit %d among what it removed, want the row it removed named with the values it held", want.ID)

			continue
		}

		if got.Label != want.Label || got.UnitRef != want.UnitRef || got.SystemID != want.SystemID {
			t.Errorf("the run names unit %d as %q of system %d with reference %d, want %q of system %d with reference %d",
				want.ID, got.Label, got.SystemID, got.UnitRef, want.Label, want.SystemID, want.UnitRef)
		}
	}

	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "units"`); got != len(maintenanceDeploymentUnits)-maintenanceDamagedUnits {
		t.Errorf("the catalog holds %d units after the run, want the %d that were not the shape it clears",
			got, len(maintenanceDeploymentUnits)-maintenanceDamagedUnits)
	}

	if kept := countRows(t, srv.db, `SELECT COUNT(*) FROM "units" WHERE "unitId" IN (301, 302, 305)`); kept != 3 {
		t.Errorf("the catalog holds %d of the three units the run must leave alone, want all of them kept exactly as they were", kept)
	}

	if left := countRows(t, srv.db, `SELECT COUNT(*) FROM "units" WHERE "unitId" IN (303, 304)`); left != 0 {
		t.Errorf("%d of the two rows the migration left labelled with their own numbers are still in the catalog, want both removed", left)
	}

	if maintenanceTablePresent(t, srv.db, maintenanceLegacyTable) {
		t.Errorf("%s is still standing, want the run to have dropped it", maintenanceLegacyTable)
	}

	// Every line the run printed is also a stored entry under its own subsystem, at the level a clearing
	// is filed at, and one entry per line rather than one entry per class: the deployment keeps the account
	// the terminal showed.
	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs" WHERE "source" = 'maintenance' AND "level" = 'warn'`); got != len(summary.Lines) {
		t.Errorf("the run filed %d warn entries under the maintenance subsystem and printed %d lines, want one entry for every line it printed", got, len(summary.Lines))
	}

	for _, line := range summary.Lines {
		if got := logSourceOfMessage(t, srv.db, line); got != "maintenance" {
			t.Errorf("the entry %q is filed under %q, want it filed under the maintenance subsystem the run reports under", line, got)
		}
	}
}

// TestAMaintenanceRunOverADeploymentAnotherInstanceHoldsIsRefused keeps the maintenance run to the
// same claim every other mode that opens the database takes: the refusal comes before anything reads
// the file, so the damaged rows a run would have removed are where they were.
func TestAMaintenanceRunOverADeploymentAnotherInstanceHoldsIsRefused(t *testing.T) {
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

	seedMaintenanceDeployment(t, srv.db)
	createMaintenanceLegacyTable(t, srv.db)

	second, err := New(testConfigIn(base, io.Discard))
	if err == nil {
		_ = second.Close()
		t.Fatal("a maintenance run over a deployment another server holds started, want it refused as in use")
	}

	if !errors.Is(err, ErrInUse) {
		t.Errorf("the refusal is %v, want it to say the deployment is in use", err)
	}

	if second != nil {
		t.Error("the refused run hands back a server, want nothing to run a clearing against")
	}

	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "units" WHERE "unitId" IN (303, 304)`); got != maintenanceDamagedUnits {
		t.Errorf("the refusal left %d of the two rows a run would have removed, want both kept: the refusal happens before the file is opened", got)
	}

	if !maintenanceTablePresent(t, srv.db, maintenanceLegacyTable) {
		t.Errorf("%s is gone although the run was refused, want the deployment untouched", maintenanceLegacyTable)
	}
}

// TestMaintainClearsABackendTheServerCannotCompactAndNamesTheSkippedCompaction holds the two halves
// apart where only one of them can run. The clearing runs on every backend; the compaction is not
// attempted at all, which the file itself proves: the free pages it would have reclaimed are still
// there. The summary names the backend the server cannot rewrite and carries no page facts, because
// it has none to carry.
func TestMaintainClearsABackendTheServerCannotCompactAndNamesTheSkippedCompaction(t *testing.T) {
	ctx := context.Background()

	for _, dialect := range []persistence.Dialect{
		persistence.DialectMariadb,
		persistence.DialectMysql,
		persistence.DialectPostgresql,
	} {
		// A sqlite file stands in for the deployment so the compaction half can be watched from the
		// outside. The server is wired to a backend it cannot rewrite, and a run that reached for the
		// file's compaction statement would take its free pages with it.
		db, err := persistence.Open(persistence.Settings{
			Dialect:  persistence.DialectSqlite,
			FilePath: filepath.Join(t.TempDir(), "rdio-scanner.db"),
		})
		if err != nil {
			t.Fatalf("%s: opening the stand-in file: %v", dialect, err)
		}

		seedMaintenanceFreelist(t, db)

		pages := filePages(t, db)
		if pages.FreelistCount == 0 {
			db.Close()
			t.Fatalf("%s: the stand-in file holds no free pages, want pages a compaction would have reclaimed", dialect)
		}

		store := &fakes.Maintenance{
			Report: ports.MigrationDamageReport{
				Units: []domain.Unit{maintenanceDeploymentUnits[2]},
			},
		}

		srv := &Server{
			dialect:     dialect,
			db:          db,
			clearDamage: application.NewClearMigrationDamageHandler(store, &fakes.Logs{}, fakes.NewClock(time.Unix(1763000000, 0))),
		}

		summary, err := srv.Maintain(ctx)
		if err != nil {
			t.Errorf("%s: Maintain: %v", dialect, err)

			db.Close()

			continue
		}

		if store.Cleared != 1 {
			t.Errorf("%s: the run asked for %d clearings, want one: the clearing runs on every backend", dialect, store.Cleared)
		}

		if summary.CompactionSkip == "" {
			t.Errorf("%s: the run says nothing about its compaction, want it to name the backend it did not compact", dialect)
		} else if !strings.Contains(summary.CompactionSkip, string(dialect)) {
			t.Errorf("%s: the run's compaction says %q, want it to name the backend the server is wired to", dialect, summary.CompactionSkip)
		}

		if summary.Before != (persistence.VacuumMeasurements{}) || summary.After != (persistence.VacuumMeasurements{}) {
			t.Errorf("%s: the run carries the page facts %v and %v, want none: nothing was measured because nothing was compacted",
				dialect, summary.Before, summary.After)
		}

		if summary.UnitsRemoved != 1 {
			t.Errorf("%s: the run removed %d unit rows, want the one its clearing named", dialect, summary.UnitsRemoved)
		}

		if after := filePages(t, db); after.FreelistCount != pages.FreelistCount || after.PageCount != pages.PageCount {
			t.Errorf("%s: the file went from %d pages with %d free to %d pages with %d free, want it exactly as it was: the compaction was not attempted",
				dialect, pages.PageCount, pages.FreelistCount, after.PageCount, after.FreelistCount)
		}

		db.Close()
	}
}

// TestMaintainReportOnlyChangesNothingAndStillClaimsTheDeployment is the dry run at the composition
// root: the listing it answers with names what a run would remove, and the deployment keeps every
// row and every page it had, including the free pages a compaction would have reclaimed and the log
// entries a clearing would have filed. It opens the deployment the way a real run does, so another
// instance is refused while it holds it.
func TestMaintainReportOnlyChangesNothingAndStillClaimsTheDeployment(t *testing.T) {
	ctx := context.Background()

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

	seedMaintenanceDeployment(t, srv.db)
	createMaintenanceLegacyTable(t, srv.db)
	seedMaintenanceFreelist(t, srv.db)

	units := countRows(t, srv.db, `SELECT COUNT(*) FROM "units"`)
	legacy := countRows(t, srv.db, `SELECT COUNT(*) FROM "`+maintenanceLegacyTable+`"`)
	entries := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs"`)
	pages := filePages(t, srv.db)

	summary, err := srv.MaintainReportOnly(ctx)
	if err != nil {
		t.Fatalf("MaintainReportOnly: %v", err)
	}

	if summary.UnitsRemoved != maintenanceDamagedUnits {
		t.Errorf("the report names %d unit rows, want the %d a run would remove", summary.UnitsRemoved, maintenanceDamagedUnits)
	}

	if summary.TablesDropped != 1 || summary.DroppedTables[0].Name != maintenanceLegacyTable {
		t.Errorf("the report names %v, want the one legacy table a run would drop", summary.DroppedTables)
	}

	if summary.CompactionSkip != "" {
		t.Errorf("the report says %q about compaction, want it to carry no compaction at all: a dry run compacts nothing", summary.CompactionSkip)
	}

	if summary.Before != (persistence.VacuumMeasurements{}) || summary.After != (persistence.VacuumMeasurements{}) {
		t.Errorf("the report carries the page facts %v and %v, want none: it measured nothing because it compacted nothing", summary.Before, summary.After)
	}

	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "units"`); got != units {
		t.Errorf("the report left %d units in the catalog where it found %d, want it to remove no row", got, units)
	}

	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "`+maintenanceLegacyTable+`"`); got != legacy {
		t.Errorf("the report left %d rows in %s where it found %d, want it to drop no table", got, maintenanceLegacyTable, legacy)
	}

	if got := countRows(t, srv.db, `SELECT COUNT(*) FROM "logs"`); got != entries {
		t.Errorf("the report filed %d log entries where the deployment held %d, want none: a listing nobody acted on is not a change", got, entries)
	}

	if after := filePages(t, srv.db); after != pages {
		t.Errorf("the report left the file at %v where it found it at %v, want the pages and free pages exactly as they were", after, pages)
	}

	second, err := New(testConfigIn(base, io.Discard))
	if err == nil {
		_ = second.Close()
		t.Fatal("a second server started over a deployment a report-only run holds, want it refused as in use")
	}

	if !errors.Is(err, ErrInUse) {
		t.Errorf("the refusal is %v, want it to say the deployment is in use", err)
	}
}

// TestAMaintainedDeploymentStartsAgainWithoutNamingADamagedUnit is what the operator does with a
// deployment the run went through: the maintenance pass takes out the rows the legacy migration invented,
// the server is opened again the way a start opens it, and the start that follows the run has nothing to
// report. The report is the one the startup writes on every start, so a deployment left holding rows of
// that shape would be heard again on the next start.
func TestAMaintainedDeploymentStartsAgainWithoutNamingADamagedUnit(t *testing.T) {
	ctx := context.Background()

	base := t.TempDir()

	srv, err := New(testConfigIn(base, io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	seedMaintenanceUnits(t, srv.db, maintenanceRowsCarryingAReference()...)
	createMaintenanceLegacyTable(t, srv.db)

	summary, err := srv.Maintain(ctx)
	if err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	if summary.UnitsRemoved != maintenanceDamagedUnits {
		t.Errorf("the run removed %d unit rows, want the %d carrying their own number as their label", summary.UnitsRemoved, maintenanceDamagedUnits)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	console := strings.Builder{}

	again, err := New(testConfigIn(base, &console))
	if err != nil {
		t.Fatalf("New over the deployment the run cleared: %v", err)
	}

	defer func() {
		if err := again.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if err := again.Start(ctx); err != nil {
		t.Fatalf("starting the server over the deployment the run cleared: %v", err)
	}

	if out := console.String(); strings.Contains(out, "the migration left") {
		t.Errorf("the start that follows a maintenance run reports damaged units:\n%s", out)
	}

	if got := countRows(t, again.db, `SELECT COUNT(*) FROM "logs" WHERE "message" LIKE 'the migration left%'`); got != 0 {
		t.Errorf("the log view holds %d entries from a damage report, want none: the run left no row a start would report", got)
	}

	if got := countRows(t, again.db, `SELECT COUNT(*) FROM "units"`); got != len(maintenanceRowsCarryingAReference())-maintenanceDamagedUnits {
		t.Errorf("the catalog holds %d units after the run and a start, want the %d the deployment named",
			got, len(maintenanceRowsCarryingAReference())-maintenanceDamagedUnits)
	}

	if got := countRows(t, again.db, `SELECT COUNT(*) FROM "units" WHERE "unitId" IN (303, 304)`); got != 0 {
		t.Errorf("the catalog holds %d of the rows the run removed after a start, want none of them back", got)
	}

	if got := countRows(t, again.db, `SELECT COUNT(*) FROM "units" WHERE "unitId" IN (301, 305)`); got != 2 {
		t.Errorf("the catalog holds %d of the two rows the run left standing after a start, want both", got)
	}

	if maintenanceTablePresent(t, again.db, maintenanceLegacyTable) {
		t.Errorf("%s is standing again after the run dropped it, want it gone", maintenanceLegacyTable)
	}

	assertEveryServerLogEntry(t, again.db)
}

// TestAStartAfterAMaintenanceRunNamesOnlyTheRowsTheRunLeftStanding is the other half of the same start: a
// deployment keeps the row a run may not remove — a unit that holds no radio reference, which is a name an
// operator wrote rather than a number the migration invented — and the start that follows the run reports
// exactly that row and no other.
func TestAStartAfterAMaintenanceRunNamesOnlyTheRowsTheRunLeftStanding(t *testing.T) {
	ctx := context.Background()

	base := t.TempDir()

	srv, err := New(testConfigIn(base, io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	seedMaintenanceDeployment(t, srv.db)
	createMaintenanceLegacyTable(t, srv.db)

	summary, err := srv.Maintain(ctx)
	if err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	if summary.UnitsRemoved != maintenanceDamagedUnits {
		t.Errorf("the run removed %d unit rows, want the %d carrying their own number as their label", summary.UnitsRemoved, maintenanceDamagedUnits)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	console := strings.Builder{}

	again, err := New(testConfigIn(base, &console))
	if err != nil {
		t.Fatalf("New over the deployment the run cleared: %v", err)
	}

	defer func() {
		if err := again.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if err := again.Start(ctx); err != nil {
		t.Fatalf("starting the server over the deployment the run cleared: %v", err)
	}

	out := console.String()

	for _, want := range []string{
		"the migration left 1 unit damaged: it holds no radio reference, the numbers they were migrated from are not in this database",
		`the migration left unit 302 "Car 4" of Grand Metro (10001) damaged`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the start that follows a maintenance run does not say %q:\n%s", want, out)
		}
	}

	for _, gone := range []string{
		"the migration left unit 301 ",
		"the migration left unit 303 ",
		"the migration left unit 304 ",
		"the migration left unit 305 ",
		"they are labelled with their own numbers",
		"it is labelled with its own number",
	} {
		if strings.Contains(out, gone) {
			t.Errorf("the start that follows a maintenance run still reports %q, want it to report only the row the run left standing:\n%s", gone, out)
		}
	}

	if got := strings.Count(out, "the migration left unit "); got != 1 {
		t.Errorf("the start that follows a maintenance run names %d units damaged, want the 1 row it left standing:\n%s", got, out)
	}

	if got := countRows(t, again.db, `SELECT COUNT(*) FROM "logs" WHERE "source" = 'database' AND "message" LIKE 'the migration left%'`); got != 2 {
		t.Errorf("the log view holds %d entries from that report, want the summary and the one damaged unit the start named", got)
	}

	if got := countRows(t, again.db, `SELECT COUNT(*) FROM "units" WHERE "unitId" = 302 AND "unitRef" = 0`); got != 1 {
		t.Errorf("the start left %d rows of the unit the run left standing, want the one row it reported written as it was", got)
	}

	if got := countRows(t, again.db, `SELECT COUNT(*) FROM "units"`); got != len(maintenanceDeploymentUnits)-maintenanceDamagedUnits {
		t.Errorf("the catalog holds %d units after the run and a start, want the %d the run left standing",
			got, len(maintenanceDeploymentUnits)-maintenanceDamagedUnits)
	}

	assertEveryServerLogEntry(t, again.db)
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

// a claim that failed because the folder would not hold the sidecar is not a
// deployment another process holds. The composition root is what turns a claim
// failure into the in-use refusal, and it has to keep the two apart: an operator
// whose folder is owned by someone else is told the folder is the problem, not
// that a server is running when none is.
func TestAFolderThatWillNotHoldTheClaimIsNotReportedAsADeploymentInUse(t *testing.T) {
	base := t.TempDir()

	dbFile := filepath.Join(base, "rdio-scanner.db")

	if err := os.Chmod(base, 0o555); err != nil {
		t.Fatalf("chmod %s: %v", base, err)
	}

	defer func() {
		if err := os.Chmod(base, 0o755); err != nil {
			t.Errorf("chmod %s writable again: %v", base, err)
		}
	}()

	srv, err := New(testConfigIn(base, io.Discard))
	if err == nil {
		_ = srv.Close()
		t.Fatal("a composition root started over a folder that cannot hold its claim, want it refused")
	}

	if srv != nil {
		t.Error("the refused start handed back an assembled server, want nothing assembled")
	}

	if errors.Is(err, ErrInUse) {
		t.Errorf("the refusal is reported as a deployment in use: %v, want the folder's own failure", err)
	}

	if text := err.Error(); strings.Contains(text, "in use") {
		t.Errorf("the refusal says the deployment is in use when nothing holds it: %q", text)
	}

	if !strings.Contains(err.Error(), dbFile) {
		t.Errorf("the refusal does not name the database file the operator has to fix: %q, want it to name %q", err.Error(), dbFile)
	}

	// the reason the folder gave stays in the sentence, because the fix is the
	// folder's ownership and not a server to stop
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("the refusal does not carry the reason the folder gave: %q", err.Error())
	}
}

// a deployment another process holds is said in use once. The claim answers the
// held deployment with the in-use failure of its own, and the composition root
// adds the same words again beside it, so the operator reads one refusal as two.
func TestADeploymentAnotherProcessHoldsIsSaidInUseOnce(t *testing.T) {
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

	second, err := New(testConfigIn(base, io.Discard))
	if err == nil {
		_ = second.Close()
		t.Fatal("a second composition root against a live deployment started, want it refused as in use")
	}

	if !errors.Is(err, ErrInUse) {
		t.Errorf("the refusal is %v, want it to say the deployment is in use", err)
	}

	if got := strings.Count(err.Error(), "the database is in use"); got != 1 {
		t.Errorf("the refusal says the deployment is in use %d times: %q, want it said once", got, err.Error())
	}
}

// a database failure the composition root reports on a file-backed deployment names the file the
// operator named beside the words the failure already carries. The section prefix is what files the
// line under the database subsystem, and the migration it names is what tells the operator which
// half of the pass stopped; both stay exactly as they were.
func TestADatabaseFailureOnAFileBackedDeploymentNamesTheDatabaseFile(t *testing.T) {
	base := t.TempDir()

	srv, err := New(testConfigIn(base, io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dbFile := filepath.Join(base, "rdio-scanner.db")

	if err := os.Chmod(dbFile, 0o444); err != nil {
		t.Fatalf("chmod %s so no account can write it: %v", dbFile, err)
	}

	defer func() {
		if err := os.Chmod(dbFile, 0o644); err != nil {
			t.Errorf("chmod %s writable again: %v", dbFile, err)
		}
	}()

	second, err := New(testConfigIn(base, io.Discard))
	if err == nil {
		_ = second.Close()
		t.Fatal("a composition root started over a database file no account can write, want it refused")
	}

	if second != nil {
		t.Error("the refused start handed back an assembled server, want nothing assembled")
	}

	if errors.Is(err, ErrInUse) {
		t.Errorf("the refusal is reported as a deployment in use: %v, want the migration's own failure", err)
	}

	text := err.Error()

	if !strings.Contains(text, dbFile) {
		t.Errorf("the refusal does not name the database file the operator has to fix: %q, want it to name %q", text, dbFile)
	}

	if !strings.Contains(text, "database.migrate:") {
		t.Errorf("the refusal no longer carries the section prefix that files the line: %q", text)
	}

	if !strings.Contains(text, "migration.") {
		t.Errorf("the refusal no longer names the migration that stopped: %q", text)
	}

	if !strings.Contains(text, "attempt to write a readonly database") {
		t.Errorf("the refusal no longer carries the failure the database gave: %q", text)
	}
}

// the file is named only where there is a file to fix. A deployment on a database server has none:
// its failure already names the host and the database it could not reach, and naming a file there
// would send the operator to a folder that holds nothing of theirs.
func TestADatabaseFailureOnAServerBackedDeploymentNamesNoDatabaseFile(t *testing.T) {
	base := t.TempDir()

	config := testConfigIn(base, io.Discard)
	config.DbType = DbTypeMariadb
	config.DbHost = "database.invalid"
	config.DbPort = 3306
	config.DbName = "rdio_scanner"
	config.DbConnectTimeout = "not a duration"

	srv, err := New(config)
	if err == nil {
		_ = srv.Close()
		t.Fatal("a composition root reached a database whose data source it could not build, want it refused")
	}

	if srv != nil {
		t.Error("the refused start handed back an assembled server, want nothing assembled")
	}

	if errors.Is(err, ErrInUse) {
		t.Errorf("the refusal is reported as a deployment in use: %v, want the data source's own failure", err)
	}

	text := err.Error()

	if !strings.Contains(text, "connect timeout") {
		t.Errorf("the refusal does not carry the reason the data source gave: %q", text)
	}

	if strings.Contains(text, "rdio-scanner.db") {
		t.Errorf("the refusal names a database file beside a deployment on a database server, which has no file to fix: %q", text)
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

// TestNoLockFileRemainsAfterTheServerCloses looks at the deployment
// folder when the server stops: a clean release leaves no lock file
// naming a dead process for an administrator to find.
func TestNoLockFileRemainsAfterTheServerCloses(t *testing.T) {
	base := t.TempDir()

	srv, err := New(testConfigIn(base, io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, "rdio-scanner.lock")); err != nil {
		t.Fatalf("the running server holds no deployment lock where the base directory says: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, "rdio-scanner.lock")); !os.IsNotExist(err) {
		t.Errorf("stat rdio-scanner.lock after Close = %v, want not exists: the stop removes the lock file", err)
	}
}

// TestTheStopReportsAClaimItCouldNotRelease stops a server whose
// deployment directory refuses the lock file's removal: the console
// says the release failed, and the deployment is still acquirable —
// the claim was let go even though the file stayed behind.
func TestTheStopReportsAClaimItCouldNotRelease(t *testing.T) {
	base := t.TempDir()

	defer os.Chmod(base, 0o755)

	var out strings.Builder

	srv, err := New(testConfigIn(base, &out))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := os.Chmod(base, 0o555); err != nil {
		t.Fatalf("chmod the deployment directory read-only: %v", err)
	}

	_ = srv.Close()

	if !strings.Contains(out.String(), "the server could not release its claim on the deployment") {
		t.Errorf("the stop console = %q, want the failed release named", out.String())
	}

	if _, err := os.Stat(filepath.Join(base, "rdio-scanner.lock")); err != nil {
		t.Errorf("stat rdio-scanner.lock after the failed removal = %v, want the file to survive", err)
	}

	if err := os.Chmod(base, 0o755); err != nil {
		t.Fatalf("chmod the deployment directory writable again: %v", err)
	}

	second, err := New(testConfigIn(base, io.Discard))
	if err != nil {
		t.Fatalf("New over the surviving lock file = %v, want success: the claim was let go", err)
	}

	defer func() {
		if err := second.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
}
