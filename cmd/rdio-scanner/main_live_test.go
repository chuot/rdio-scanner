//go:build live

package main

// The live maintenance cases run the one-shot against the databases the migration's live harness
// starts, so that the clearing a run performs is seen on MariaDB and PostgreSQL as it is on a file,
// and the half a run cannot perform there — rewriting a database it is not — is named instead of
// left silent. The cases need a running backend: they stand aside when the harness's settings are
// not in the environment.

import (
	"bytes"
	"database/sql"
	"log"
	"os"
	"strconv"
	"strings"
	"testing"

	"rdio-scanner/internal/infrastructure/persistence"
)

// The live fixture's numbers are well outside those the harness's own deployment writes, so a run
// over a database another case already filled still names these rows on their own.
const (
	liveMaintenanceDamaged    = 9101
	liveMaintenanceNamed      = 9102
	liveMaintenanceUnnumbered = 9103
)

// TestMaintenanceRunClearsOnMariaDBAndPostgreSQLAndNamesTheSkippedCompaction is the run over the
// backends it can clear but cannot compact: the damaged unit is gone, the rows an operator wrote are
// where they were, the deployment records the removal at warn level under the maintenance subsystem,
// the report says the compaction needs a sqlite database and was skipped and names the backend, and
// the run exits 0 without ever reporting a compaction. A second run over the same deployment names
// no removal at all and still names the half it skipped.
func TestMaintenanceRunClearsOnMariaDBAndPostgreSQLAndNamesTheSkippedCompaction(t *testing.T) {
	for _, backend := range []struct {
		dbType  string
		dialect persistence.Dialect
	}{
		{dbType: DbTypeMariadb, dialect: persistence.DialectMariadb},
		{dbType: DbTypePostgresql, dialect: persistence.DialectPostgresql},
	} {
		t.Run(backend.dbType, func(t *testing.T) {
			settings, ok := liveMaintenanceSettings(t, backend.dialect)
			if !ok {
				t.Skipf("RDIO_LIVE_%s_HOST and its pair are not set, so there is no %s deployment to run against",
					strings.ToUpper(backend.dbType), backend.dbType)
			}

			skipLine := "the compaction was skipped: it needs the sqlite database, not " + backend.dbType

			seedLiveMaintenanceDeployment(t, settings)

			args := liveMaintenanceArgs(t, settings)

			out := &bytes.Buffer{}

			logOut := &bytes.Buffer{}

			previous := log.Writer()
			log.SetOutput(logOut)

			defer log.SetOutput(previous)

			code := run(args, out, noBundlesUnbuilt)
			text := out.String()

			if code != 0 {
				t.Fatalf("the maintenance run over %s exited %d, want 0: %s", backend.dbType, code, out.String())
			}

			// what the operator standing at the terminal would have read
			t.Logf("the run over %s reported:\n%s", backend.dbType, text)

			for _, want := range []string{
				`removed unit "9101" carrying the radio number 9101, of system 9101`,
				"removed 1 unit labelled with its own radio number",
				skipLine,
			} {
				if !strings.Contains(text, want) {
					t.Errorf("the run over %s does not report %q:\n%s", backend.dbType, want, text)
				}
			}

			for _, unwanted := range []string{"vacuumed:", "dropped the table", "dropped the migration archive", "would remove", "would drop"} {
				if strings.Contains(text, unwanted) {
					t.Errorf("the run over %s carries %q, want only the removals it performed and the compaction it skipped:\n%s",
						backend.dbType, unwanted, text)
				}
			}

			if strings.Contains(logOut.String(), "removed unit ") {
				t.Errorf("the run over %s said its removals through the logger as well as its report:\n%s",
					backend.dbType, logOut.String())
			}

			db := openLiveMaintenanceDatabase(t, settings)

			defer db.Close()

			if count := countLiveRows(t, db, "units", unitRowCondition(liveMaintenanceDamaged)); count != 0 {
				t.Errorf("the damaged unit %d is still stored %d times on %s, want it removed", liveMaintenanceDamaged, count, backend.dbType)
			}

			for _, unitID := range []int{liveMaintenanceNamed, liveMaintenanceUnnumbered} {
				if count := countLiveRows(t, db, "units", unitRowCondition(unitID)); count != 1 {
					t.Errorf("unit %d is stored %d times on %s, want the row an operator wrote untouched", unitID, count, backend.dbType)
				}
			}

			// the removal reached the deployment's own log at warn level under the
			// maintenance subsystem, as it does on a file
			logged := liveMaintenanceLogMessages(t, db)

			if !strings.Contains(logged, `warn removed unit "9101" carrying the radio number 9101, of system 9101`) {
				t.Errorf("the %s deployment does not record the removal at warn level under the maintenance subsystem:\n%s",
					backend.dbType, logged)
			}

			out = &bytes.Buffer{}

			if code := run(args, out, noBundlesUnbuilt); code != 0 {
				t.Fatalf("the second maintenance run over %s exited %d, want 0: %s", backend.dbType, code, out.String())
			}

			text = out.String()

			for _, unwanted := range []string{"removed ", "dropped "} {
				if strings.Contains(text, unwanted) {
					t.Errorf("the second run over %s names removals (%q) on a deployment the first run already cleared:\n%s",
						backend.dbType, unwanted, text)
				}
			}

			if !strings.Contains(text, skipLine) {
				t.Errorf("the second run over %s does not name the compaction it skipped:\n%s", backend.dbType, text)
			}
		})
	}
}

// liveMaintenanceArgs are the command line an operator would type for the harness's database, on a
// base folder of its own so a run writes nothing beside a real deployment.
func liveMaintenanceArgs(t *testing.T, settings persistence.Settings) []string {
	t.Helper()

	return []string{
		"-base_dir", t.TempDir(),
		"-db_type", dbTypeForDialect(settings.Dialect),
		"-db_host", settings.Host,
		"-db_port", strconv.FormatUint(uint64(settings.Port), 10),
		"-db_user", settings.Username,
		"-db_pass", settings.Password,
		"-db_name", settings.Name,
		"-vacuum",
	}
}

// dbTypeForDialect names the command line's spelling of a backend the store names by dialect.
func dbTypeForDialect(dialect persistence.Dialect) string {
	switch dialect {
	case persistence.DialectMariadb:
		return DbTypeMariadb
	case persistence.DialectMysql:
		return DbTypeMysql
	case persistence.DialectPostgresql:
		return DbTypePostgresql
	default:
		return DbTypeSqlite
	}
}

// liveMaintenanceSettings read the harness's backend settings out of the environment the harness
// exports, so the case names the databases the harness started and no others.
func liveMaintenanceSettings(t *testing.T, dialect persistence.Dialect) (persistence.Settings, bool) {
	t.Helper()

	prefix, port := "RDIO_LIVE_MARIADB_", uint(3306)

	switch dialect {
	case persistence.DialectPostgresql:
		prefix, port = "RDIO_LIVE_POSTGRESQL_", 5432
	case persistence.DialectMysql:
		prefix = "RDIO_LIVE_MYSQL_"
	case persistence.DialectSqlite:
		t.Fatalf("the live maintenance case is asked for the sqlite backend, which needs no running server")
	}

	host := os.Getenv(prefix + "HOST")
	if host == "" {
		return persistence.Settings{}, false
	}

	if named := os.Getenv(prefix + "PORT"); named != "" {
		number, err := strconv.ParseUint(named, 10, 32)
		if err != nil {
			t.Fatalf("%sPORT = %q is not a port number", prefix, named)
		}

		port = uint(number)
	}

	return persistence.Settings{
		Dialect:  dialect,
		Host:     host,
		Port:     port,
		Username: os.Getenv(prefix + "USER"),
		Password: os.Getenv(prefix + "PASSWORD"),
		Name:     os.Getenv(prefix + "NAME"),
	}, true
}

// openLiveMaintenanceDatabase opens the harness's deployment through the store's own settings, so the
// case does not spell a connection string the store already knows how to build. The connection is
// closed before a run works on the same deployment.
func openLiveMaintenanceDatabase(t *testing.T, settings persistence.Settings) *sql.DB {
	t.Helper()

	db, err := persistence.Open(settings)
	if err != nil {
		t.Fatalf("%s: Open: %v", settings.Dialect, err)
	}

	return db
}

// seedLiveMaintenanceDeployment writes the rows the run is asked to tell apart — one unit labelled
// with its own radio number, one unit an operator named, one unit carrying no number at all — and
// takes them out again when the case ends, so the harness's database holds what it held before.
func seedLiveMaintenanceDeployment(t *testing.T, settings persistence.Settings) {
	t.Helper()

	removals := []string{
		`DELETE FROM "units" WHERE "unitId" IN (9101, 9102, 9103)`,
		`DELETE FROM "systems" WHERE "systemId" = 9101`,
	}

	db := openLiveMaintenanceDatabase(t, settings)

	defer db.Close()

	for _, stmt := range removals {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: clearing the fixture's numbers before seeding: %v", settings.Dialect, err)
		}
	}

	stmts := []string{
		`INSERT INTO "systems" ("systemId", "label", "systemRef") VALUES (9101, 'Live Maintenance System', 9101)`,
		`INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef") VALUES (9101, '9101', 1, 9101, 9101)`,
		`INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef") VALUES (9102, 'Engine 91', 2, 9101, 9102)`,
		`INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef") VALUES (9103, 'Mobile 91', 3, 9101, 0)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %s: %v", settings.Dialect, stmt, err)
		}
	}

	t.Cleanup(func() {
		db, err := persistence.Open(settings)
		if err != nil {
			t.Errorf("%s: opening the deployment to take the seeded rows out: %v", settings.Dialect, err)

			return
		}

		defer db.Close()

		for _, stmt := range removals {
			if _, err := db.Exec(stmt); err != nil {
				t.Errorf("%s: taking the seeded rows out (%s): %v", settings.Dialect, stmt, err)
			}
		}
	})
}

// unitRowCondition names one unit row by its key, spelled into a condition this package can hand
// straight to the database.
func unitRowCondition(unitID int) string {
	return `"unitId" = ` + strconv.Itoa(unitID)
}

// countLiveRows counts the rows one table answers a condition with. The condition spells its
// numbers into the statement: the store's placeholder numbering is not available to this package,
// and PostgreSQL reads a `?` it was not given.
func countLiveRows(t *testing.T, db *sql.DB, table string, where string) int {
	t.Helper()

	var count int

	query := `SELECT COUNT(*) FROM "` + table + `" WHERE ` + where

	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("counting the rows of %s: %v", table, err)
	}

	return count
}

// liveMaintenanceLogMessages reads back what the deployment recorded under the maintenance
// subsystem, one line per entry, each line naming the level it was filed at. The log key the file
// spells logid is the one the live backends spell logId, and only the ordering depends on it.
func liveMaintenanceLogMessages(t *testing.T, db *sql.DB) string {
	t.Helper()

	rows, err := db.Query(`SELECT "level", "message" FROM "logs" WHERE "source" = 'maintenance' ORDER BY "logId"`)
	if err != nil {
		t.Fatalf("reading the maintenance log entries: %v", err)
	}

	defer rows.Close()

	var messages []string

	for rows.Next() {
		var level, message string

		if err := rows.Scan(&level, &message); err != nil {
			t.Fatalf("reading a maintenance log entry: %v", err)
		}

		messages = append(messages, level+" "+message)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the maintenance log entries: %v", err)
	}

	return strings.Join(messages, "\n")
}
