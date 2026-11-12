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

package persistence

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// The migration pass owns every change to a database that already exists: ApplySchema
// cannot make them — its statements are all `IF NOT EXISTS` — and must not, because its contract
// is that a legacy-written database comes out exactly as it went in.

func TestMigrateAddsTheLogSourceColumnToADatabaseThatAlreadyHasTheTable(t *testing.T) {
	db := openLegacyDatabase(t)

	if columns := logColumns(t, db); hasString(columns, "source") {
		t.Fatalf("the fixture already has a source column (%v): it is no longer the database the legacy server wrote", columns)
	}

	before := countRows(t, db, "logs")

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate over a database written by the legacy server: %v", err)
	}

	after := logColumns(t, db)

	if !hasString(after, "source") {
		t.Fatalf("the logs table has %v after the migration, want a source column among them", after)
	}

	if moved := countRows(t, db, "logs"); moved != before {
		t.Errorf("the logs table had %d rows before the migration and has %d after it", before, moved)
	}

	for _, index := range []string{"logs_source_idx", "logs_time_idx"} {
		if !hasString(logIndexes(t, db), index) {
			t.Errorf("the logs table has indexes %v, want %s among them", logIndexes(t, db), index)
		}
	}
}

// The migration pass runs on every start: the second start must not re-add the column,
// re-run the backfill, or change a row.
func TestMigrateAddingTheLogSourceColumnTwiceIsTheSameAsOnce(t *testing.T) {
	db := openLegacyDatabase(t)

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	once := readSchemaObjects(t, db)
	onceRows := readLogRows(t, db)

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate a second time: %v", err)
	}

	twice := readSchemaObjects(t, db)

	if len(once) != len(twice) {
		t.Fatalf("the second migration changed the schema: %d objects after one run, %d after two", len(once), len(twice))
	}

	for i := range once {
		if once[i] != twice[i] {
			t.Errorf("schema object %d changed on the second migration\nafter one: %s\nafter two: %s", i, once[i], twice[i])
		}
	}

	twiceRows := readLogRows(t, db)

	if len(onceRows) != len(twiceRows) {
		t.Fatalf("the second migration changed the log rows: %d after one run, %d after two", len(onceRows), len(twiceRows))
	}

	for i := range onceRows {
		if onceRows[i] != twiceRows[i] {
			t.Errorf("log row %d changed on the second migration\nafter one: %s\nafter two: %s", i, onceRows[i], twiceRows[i])
		}
	}
}

// A database with no `logs` table at all: the probe says nothing and does nothing, as every
// v6 migration in the file already answers the same question.
func TestEnsureLogSourceColumnOnADatabaseWithNoLogsTable(t *testing.T) {
	db := openLegacyDatabase(t)

	if _, err := db.Exec(`DROP TABLE "logs"`); err != nil {
		t.Fatalf("DROP TABLE logs: %v", err)
	}

	if err := ensureLogSourceColumn(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("the step in front of a database with no logs table: %v", err)
	}

	if hasString(readTableNames(t, db), "logs") {
		t.Error("the step created a logs table it was not asked to create")
	}
}

// The table is there and empty, and the step still gives it the column and the indexes: a
// log table without them is a log view that cannot be filtered.
func TestEnsureLogSourceColumnOnAnEmptyLogsTable(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "logs")

	if err := ensureLogSourceColumn(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("ensureLogSourceColumn: %v", err)
	}

	if columns := logColumns(t, db); !hasString(columns, "source") {
		t.Errorf("the logs table has %v, want a source column among them", columns)
	}

	for _, index := range []string{"logs_source_idx", "logs_time_idx"} {
		if !hasString(logIndexes(t, db), index) {
			t.Errorf("the logs table has indexes %v, want %s among them", logIndexes(t, db), index)
		}
	}
}

// A database that already has the column: the indexes are still ensured, and nothing else
// moves.
func TestEnsureLogSourceColumnEnsuresTheIndexesOnADatabaseThatAlreadyHasTheColumn(t *testing.T) {
	db := openLegacyDatabase(t)

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	before := readSchemaObjects(t, db)

	if err := ensureLogSourceColumn(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("ensureLogSourceColumn a second time: %v", err)
	}

	after := readSchemaObjects(t, db)

	if len(before) != len(after) {
		t.Fatalf("the second run changed the schema: %d objects before, %d after", len(before), len(after))
	}
}

func logColumns(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.Query(`SELECT "name" FROM pragma_table_info("logs")`)
	if err != nil {
		t.Fatalf("reading the columns of the logs table: %v", err)
	}

	defer rows.Close()

	names := []string{}

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			t.Fatalf("reading the columns of the logs table: %v", err)
		}

		names = append(names, name)
	}

	return names
}

func logIndexes(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.Query(`SELECT "name" FROM "sqlite_master" WHERE "type" = 'index' AND "tbl_name" = 'logs'`)
	if err != nil {
		t.Fatalf("reading the indexes of the logs table: %v", err)
	}

	defer rows.Close()

	names := []string{}

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			t.Fatalf("reading the indexes of the logs table: %v", err)
		}

		names = append(names, name)
	}

	return names
}

// readLogRows reads the whole log table in order, so a migration that quietly rewrote or
// lost a row is a failing test rather than a difference nobody looked at.
func readLogRows(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.Query(`SELECT "logid", "level", "message", "timestamp" FROM "logs" ORDER BY "logid"`)
	if err != nil {
		t.Fatalf("reading the log rows: %v", err)
	}

	defer rows.Close()

	lines := []string{}

	for rows.Next() {
		var (
			id        int64
			level     string
			message   string
			timestamp int64
		)

		if err := rows.Scan(&id, &level, &message, &timestamp); err != nil {
			t.Fatalf("reading the log rows: %v", err)
		}

		lines = append(lines, level+"|"+message+"|"+itoa(timestamp))
	}

	return lines
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}

	digits := ""

	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}

	return digits
}

// logIndexDefinition reads back what the database says one of its own indexes is, so a test
// can name the object it expects without pasting the statement the migration ran.
func logIndexDefinition(t *testing.T, db *sql.DB, name string) string {
	t.Helper()

	return logSchemaDefinition(t, db, "index", name)
}

func logSchemaDefinition(t *testing.T, db *sql.DB, kind, name string) string {
	t.Helper()

	var definition string

	query := `SELECT COALESCE("sql", '') FROM "sqlite_master" WHERE "type" = ? AND "name" = ?`

	if err := db.QueryRow(query, kind, name).Scan(&definition); err != nil {
		t.Fatalf("reading the definition of the %s %s: %v", kind, name, err)
	}

	return definition
}

// isLogsTable and isLogIndex name a schema object by its kind and its name, the first two
// words of the string readSchemaObjects builds.
func isLogsTable(object string) bool {
	return strings.HasPrefix(object, "table logs ")
}

// isCallsTable names the calls table the same way: the startup pass gives it the
// audioLength column, so it is the other object allowed to read differently after the pass.
func isCallsTable(object string) bool {
	return strings.HasPrefix(object, "table calls ")
}

func isLogIndex(object string, name string) bool {
	return strings.HasPrefix(object, "index "+name+" ")
}

// A row written before the column existed is filed by reading the prefix its message
// carries, and a row whose prefix maps to nothing is stored as legacy rather than left blank or
// guessed at. The messages are the ones the server actually wrote, including the persistence
// prefixes that reach the log because a caller stored err.Error with nothing added.
func TestBackfillLogSourcesFilesEveryExistingRowByItsPrefix(t *testing.T) {
	cases := []struct {
		message string
		want    domain.LogSource
	}{
		{"server started", domain.LogSourceServer},
		{"configuration changed", domain.LogSourceConfig},
		{"admin.confighandler.put: systems.write: constraint failed", domain.LogSourceConfig},
		{"admin.sectionhandler.put: dirwatches.write: no such table", domain.LogSourceConfig},
		{"apikeys.write: UNIQUE constraint failed", domain.LogSourceConfig},
		{"groups.read: no such column", domain.LogSourceConfig},
		{"options.write: database is locked", domain.LogSourceConfig},
		{"systems.read: no such table", domain.LogSourceConfig},
		{"tags.write: FOREIGN KEY constraint failed", domain.LogSourceConfig},
		{"accesses.read: no such table", domain.LogSourceAccess},
		{"accesses.write: UNIQUE constraint failed", domain.LogSourceAccess},
		{"api: the access code is not valid", domain.LogSourceUpload},
		{"calls.getcall: sql: no rows in result set", domain.LogSourceCalls},
		{"calls.writecall: FOREIGN KEY constraint failed", domain.LogSourceCalls},
		{"calls.search: no such column", domain.LogSourceCalls},
		{"client.message.fromjson: unexpected end of JSON input", domain.LogSourceLivefeed},
		{"client.message.tojson: json: unsupported type", domain.LogSourceLivefeed},
		{"client.processmessage: unknown message", domain.LogSourceLivefeed},
		{"controller.processmessage.commandpin: no such call", domain.LogSourceLivefeed},
		{"controller.processmessage.commandlistcall: no such system", domain.LogSourceLivefeed},
		{"controller.ingestcall: the call is not complete", domain.LogSourceIngest},
		{"newcall: system=10001 talkgroup=1514000 file=audio.wav success", domain.LogSourceIngest},
		{"database.migrate: migration.migrateGroups: no such table", domain.LogSourceDatabase},
		{"database.open: unable to open database file", domain.LogSourceDatabase},
		{"database.seed: UNIQUE constraint failed", domain.LogSourceDatabase},
		{"migration.damage: 1 unit, its system is gone", domain.LogSourceDatabase},
		{"migrating groups...", domain.LogSourceDatabase},
		{"schema.apply: no such module", domain.LogSourceDatabase},
		{"delayer.pop: no such table: delayed", domain.LogSourceDelayer},
		{"delayer.push: database is locked", domain.LogSourceDelayer},
		{"delayer.delay: the hold could not be written", domain.LogSourceDelayer},
		{"delayer.restore: no such row", domain.LogSourceDelayer},
		{"dirwatch.watcher: no such file or directory", domain.LogSourceDirwatch},
		{"dirwatch.walkdir: permission denied", domain.LogSourceDirwatch},
		{"dirwatch.ingest: no such file, /data/incoming/audio.wav", domain.LogSourceDirwatch},
		{"dirwatches.start: no such table", domain.LogSourceDirwatch},
		{"dirwatches.read: no such table", domain.LogSourceDirwatch},
		{"dirwatches.write: FOREIGN KEY constraint failed", domain.LogSourceDirwatch},
		{"downstream: system=10001 talkgroup=1514000 file=audio.wav to http://other:5002 success", domain.LogSourceDownstream},
		{"downstream.send: Post \"http://other:5002\": dial tcp", domain.LogSourceDownstream},
		{"ffmpeg: Conversion failed!", domain.LogSourceFFmpeg},
		{"logs.logevent: no such table", domain.LogSourceLogs},
		{"logs.search: no such column", domain.LogSourceLogs},
		{"logs.prune: database is locked", domain.LogSourceLogs},
		{"passwords.hash: bcrypt cost too high", domain.LogSourceAdmin},
		{"tokens.issue: no such table", domain.LogSourceLogin},
		{"tokens.validate: token is not valid", domain.LogSourceLogin},
		{"websocket.upgrade: connection upgrade failed", domain.LogSourceLivefeed},
		{"too many login attempts for ip=\"10.0.0.7\"", domain.LogSourceLogin},
		{"invalid login attempt for ip 10.0.0.7", domain.LogSourceLogin},
		{"invalid access code 4321 for ip 10.0.0.7", domain.LogSourceAccess},
		{"locked access for ident 1234 locked", domain.LogSourceAccess},
		{"expired access for ident 1234", domain.LogSourceAccess},
		{"too many concurrent connections for ident 1234, limit is 2", domain.LogSourceAccess},
		{"database pruning", domain.LogSourcePrune},
		{"new listener from ip 10.0.0.7 with ident 1234", domain.LogSourceLivefeed},
		{"listener disconnected from ip 10.0.0.7", domain.LogSourceLivefeed},
		{"listeners count is 3", domain.LogSourceLivefeed},
		{"admin password changed.", domain.LogSourceAdmin},
		{"admin.passwordhandler.post: unable to change admin password", domain.LogSourceAdmin},
		{"admin.useraddhandler.post: no such table", domain.LogSourceAdmin},
		{"admin.userremovehandler.post: no such row", domain.LogSourceAdmin},
		{"a line that names nothing this server recognises", domain.LogSourceLegacy},
		{"", domain.LogSourceLegacy},
	}

	db := openLegacyDatabaseWithout(t, "logs")

	for _, c := range cases {
		if _, err := db.Exec(`INSERT INTO "logs" ("level", "message", "timestamp") VALUES (?, ?, ?)`, "info", c.message, int64(1760000000000)); err != nil {
			t.Fatalf("storing %q: %v", c.message, err)
		}
	}

	if err := ensureLogSourceColumn(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("ensureLogSourceColumn: %v", err)
	}

	for _, c := range cases {
		var got string

		query := `SELECT "source" FROM "logs" WHERE "message" = ?`

		if err := db.QueryRow(query, c.message).Scan(&got); err != nil {
			t.Fatalf("reading back %q: %v", c.message, err)
		}

		if got != string(c.want) {
			t.Errorf("%q was filed under %q, want %q", c.message, got, c.want)
		}
	}
}

// The five entries the legacy server wrote into the fixture are filed like any other rows.
func TestBackfillLogSourcesFilesTheLegacyFixtureOwnRows(t *testing.T) {
	db := openLegacyDatabase(t)

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	want := map[string]domain.LogSource{
		"server started":        domain.LogSourceServer,
		"configuration changed": domain.LogSourceConfig,
		"ffmpeg is not available, no audio conversion will be performed": domain.LogSourceFFmpeg,
	}

	rows, err := db.Query(`SELECT "message", "source" FROM "logs" ORDER BY "logid"`)
	if err != nil {
		t.Fatalf("reading the log rows: %v", err)
	}

	defer rows.Close()

	filed := 0

	for rows.Next() {
		var (
			message string
			source  string
		)

		if err := rows.Scan(&message, &source); err != nil {
			t.Fatalf("reading the log rows: %v", err)
		}

		filed++

		if expected, named := want[message]; named && source != string(expected) {
			t.Errorf("%q was filed under %q, want %q", message, source, expected)
		}

		if !domain.IsKnownLogSource(domain.LogSource(source)) {
			t.Errorf("%q was filed under %q, which is not a source this server knows", message, source)
		}
	}

	if filed != 5 {
		t.Errorf("the fixture's log table holds %d rows after the migration, want the 5 it was written with", filed)
	}
}

// The backfill is tied to the run that added the column: a deployment's log is read and
// filtered for months, and re-deciding what an old entry meant on every start is not something
// an operator can rely on.
func TestBackfillLogSourcesRunsOnce(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "logs")

	if _, err := db.Exec(`INSERT INTO "logs" ("level", "message", "timestamp") VALUES (?, ?, ?)`, "info", "dirwatch.walkdir: permission denied", int64(1760000000000)); err != nil {
		t.Fatalf("storing the entry: %v", err)
	}

	if err := ensureLogSourceColumn(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("ensureLogSourceColumn: %v", err)
	}

	// An operator, or another server sharing the database, re-files the entry. The next
	// start must leave that decision alone.
	if _, err := db.Exec(`UPDATE "logs" SET "source" = 'legacy'`); err != nil {
		t.Fatalf("re-filing the entry: %v", err)
	}

	if err := ensureLogSourceColumn(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("ensureLogSourceColumn a second time: %v", err)
	}

	var source string

	if err := db.QueryRow(`SELECT "source" FROM "logs" WHERE "message" = 'dirwatch.walkdir: permission denied'`).Scan(&source); err != nil {
		t.Fatalf("reading the entry back: %v", err)
	}

	if source != string(domain.LogSourceLegacy) {
		t.Errorf("the second run re-filed the entry as %q, want the legacy it was filed under left alone", source)
	}
}

// The mapping and the vocabulary hold together in both directions: a prefix that files a
// row under a name the log view does not offer is a row an operator cannot find, and a name the
// vocabulary records as one only the backfill reaches must be one the backfill can produce.
func TestEverySourceTheBackfillReachesIsOneTheServerHas(t *testing.T) {
	reaches := map[domain.LogSource]bool{}

	for _, mapping := range logSourceBackfill {
		reaches[mapping.source] = true
	}

	// A message that maps to nothing is filed under legacy rather than left blank.
	reaches[domain.LogSourceLegacy] = true

	for source := range reaches {
		if !domain.IsKnownLogSource(source) {
			t.Errorf("the backfill files a row under %q, and the log view does not offer that name", source)
		}
	}

	for _, source := range domain.KnownLogSources() {
		if domain.LogSourceIsWritten(source) {
			continue
		}

		if !reaches[source] {
			t.Errorf("%q is offered by the log view and recorded as a name only the backfill reaches, and the "+
				"backfill produces nothing under it: an operator filtering for it is always told nothing "+
				"happened", source)
		}
	}
}

// A maintenance run's entries are the account of a deployment cleared months ago, so they have to
// survive the next start with their subsystem intact: the row is stored under the name it was filed
// under, and the startup pass that names nameless rows leaves it alone.
func TestAMaintenanceEntryIsStoredUnderItsSubsystemAndASubsequentStartKeepsIt(t *testing.T) {
	db := openMigratedLegacyDatabase(t, "logs")

	defer db.Close()

	store := []domain.LogEntry{
		{
			DateTime: time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC),
			Level:    domain.LogLevelWarn,
			Source:   domain.LogSourceMaintenance,
			Message:  `removed unit "6789" carrying the radio number 6789, of system 101`,
		},
		{
			DateTime: time.Date(2026, 3, 14, 9, 1, 0, 0, time.UTC),
			Level:    domain.LogLevelWarn,
			Source:   domain.LogSourceMaintenance,
			Message:  "dropped the table rdioScannerCalls, which held 8 rows",
		},
	}

	logs := NewLogs(db, DialectSqlite)

	for _, entry := range store {
		if err := logs.AppendLog(context.Background(), entry); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	if err := ensureLogSourceColumn(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("the start after the clearing: %v", err)
	}

	for _, entry := range store {
		var source string

		// The message is what identifies the row: the startup pass itself files entries in the same
		// table, and only the run's own lines are what this check is about.
		if err := db.QueryRow(`SELECT "source" FROM "logs" WHERE "message" = ?`, entry.Message).Scan(&source); err != nil {
			t.Fatalf("reading the entry the run filed under %s: %v", entry.Source, err)
		}

		if domain.LogSource(source) != domain.LogSourceMaintenance {
			t.Errorf("the entry %q is stored under %q, want %s: the startup pass re-files a row that already "+
				"names its subsystem", entry.Message, source, domain.LogSourceMaintenance)
		}
	}
}

// openMigratedLegacyDatabase opens the legacy-written database and brings it through the
// same startup pass this server runs: the schema, the log source column and its indexes, and the
// v6 migrations. The log repository is only ever reached after that pass, so a test that reaches
// it without the pass is testing a state the server cannot be in.
func openMigratedLegacyDatabase(t *testing.T, tables ...string) *sql.DB {
	t.Helper()

	db := openLegacyDatabaseWithout(t, tables...)

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	return db
}

// A crashed pass leaves the source column there, the indexes it was building not, and the
// rows the crashed run never reached sit nameless forever. The missing index is the sign; the
// fill runs again, and the rows that already name a source keep it.
func TestBackfillLogSourcesRunsWhenTheIndexIsMissing(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "logs")

	defer db.Close()

	if _, err := db.Exec(`INSERT INTO "logs" ("level", "message", "timestamp") VALUES (?, ?, ?)`, "info", "database.open: unable to open database file", int64(1760000000000)); err != nil {
		t.Fatalf("storing the first entry: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO "logs" ("level", "message", "timestamp") VALUES (?, ?, ?)`, "info", "dirwatch.walkdir: permission denied", int64(1760000000001)); err != nil {
		t.Fatalf("storing the second entry: %v", err)
	}

	// The crashed run: the column is added, the fill starts, and the pass dies before
	// its indexes — one row left nameless, no indexes.
	if err := ensureLogSourceColumn(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("the crashed run: %v", err)
	}

	if _, err := db.Exec(`UPDATE "logs" SET "source" = '' WHERE "message" = 'dirwatch.walkdir: permission denied'`); err != nil {
		t.Fatalf("leaving the second entry nameless: %v", err)
	}

	for _, index := range []string{"logs_source_idx", "logs_time_idx"} {
		if _, err := db.Exec(`DROP INDEX "` + index + `"`); err != nil {
			t.Fatalf("dropping the index the crashed run never finished: %v", err)
		}
	}

	if err := ensureLogSourceColumn(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("the start after the crash: %v", err)
	}

	var source string

	if err := db.QueryRow(`SELECT "source" FROM "logs" WHERE "message" = 'dirwatch.walkdir: permission denied'`).Scan(&source); err != nil {
		t.Fatalf("reading the nameless entry: %v", err)
	}

	if source != string(domain.LogSourceDirwatch) {
		t.Errorf("the row the crashed pass never filled is filed under %q, want dirwatch: the missing index is the sign the fill runs again", source)
	}

	if err := db.QueryRow(`SELECT "source" FROM "logs" WHERE "message" = 'database.open: unable to open database file'`).Scan(&source); err != nil {
		t.Fatalf("reading the filled entry: %v", err)
	}

	if source != string(domain.LogSourceDatabase) {
		t.Errorf("the row that already named its source was re-filed as %q, want database left alone", source)
	}

	for _, index := range []string{"logs_source_idx", "logs_time_idx"} {
		if !hasString(logIndexes(t, db), index) {
			t.Errorf("the logs table has indexes %v, want %s among them", logIndexes(t, db), index)
		}
	}
}
