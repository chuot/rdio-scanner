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
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// migrationInstant is the moment the migration tests stamp archived rows with, so that archivedAt is
// asserted rather than approximated, and fixedClock is the server clock the tests hand to Migrate.
var migrationInstant = time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

// The migration archive is the table that holds every v6 row the migration pass read
// and could not carry, so dropping the v6 table never destroys a row; it lives in the
// migration pass, not in the dialect schemas.

// The archive table is created where one of the legacy table names is present for the pass to read a
// row out of, with the unique index that makes re-archiving a surviving v6 table hold each row once. A
// database holding none of them — a fresh one, or one the legacy server left at v7 — has nothing to
// archive and gains no table from a start.
func TestMigrateCreatesTheMigrationArchiveOnlyWhereALegacyTableIsPresent(t *testing.T) {
	fresh := openEmptyDatabase(t)

	migrateDatabase(t, fresh)

	if hasMigrationArchiveTable(t, fresh) {
		t.Errorf("a pass over a database holding none of the legacy table names created the migration archive, want nothing archived for a database with nothing to migrate: %v",
			readTableNames(t, fresh))
	}

	inherited := openLegacyV6Database(t)

	migrateDatabase(t, inherited)

	assertMigrationArchive(t, inherited, "a database the legacy server wrote rows into")

	// A database the legacy server left at v7 holds none of the table names this pass reads, and it is
	// the state a deployment that has been through the earlier server's own migration is in: nothing
	// left to archive, and no table built for it by a start.
	v7 := openLegacyDatabase(t)

	migrateDatabase(t, v7)

	if hasMigrationArchiveTable(t, v7) {
		t.Errorf("a pass over a database the legacy server left at v7 created the migration archive, want it left as the other server wrote it: %v",
			readTableNames(t, v7))
	}
}

// Creating the archive is idempotent, and it is the migration pass that creates it: the schema step
// is the legacy server's and gives no database the archive, whether or not that database has a
// legacy table for the pass to read.
func TestApplySchemaAloneDoesNotCreateTheMigrationArchive(t *testing.T) {
	db := openEmptyDatabase(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	for _, name := range readTableNames(t, db) {
		if name == "migrationArchive" {
			t.Fatal("ApplySchema created the migration archive: the schema step is the legacy server's and stays that way")
		}
	}
}

// TestMigrationArchiveIsUnchangedByASecondMigration is the idempotence every step of the startup pass
// is held to: the step that creates the archive creates it once. It is held against a database the
// legacy server wrote rows into, which is the deployment the archive is created for, and against the
// rows the first pass archived as well as the table that holds them.
func TestMigrationArchiveIsUnchangedByASecondMigration(t *testing.T) {
	db := openLegacyV6Database(t)

	migrateDatabase(t, db)

	assertMigrationArchive(t, db, "a database the legacy server wrote after the first pass")

	before := readSchemaObjects(t, db)
	archived := readRows(t, db, `SELECT "tableName","rowId","reason","contents","archivedAt" FROM "migrationArchive" ORDER BY "tableName","rowId"`)

	if len(archived) == 0 {
		t.Fatalf("the first pass archived nothing into the archive it created, want the v6 rows it could not carry")
	}

	migrateDatabase(t, db)

	after := readSchemaObjects(t, db)

	if len(after) != len(before) {
		t.Fatalf("the second migration changed the schema: %d objects after one run, %d after two", len(before), len(after))
	}

	for i := range before {
		if before[i] != after[i] {
			t.Errorf("schema object %d changed on the second migration\nafter one: %s\nafter two: %s", i, before[i], after[i])
		}
	}

	assertMigrationArchive(t, db, "a database the legacy server wrote after the second pass")

	if again := readRows(t, db, `SELECT "tableName","rowId","reason","contents","archivedAt" FROM "migrationArchive" ORDER BY "tableName","rowId"`); !reflect.DeepEqual(archived, again) {
		t.Errorf("the second migration changed the archive's rows:\n  after one: %v\n  after two: %v", archived, again)
	}
}

// An archive table that already stands, with rows in it, stands through a pass that finds nothing to
// create: the gate asks only whether to build the table, and the record an earlier migration left is
// neither replaced nor emptied by a later start.
func TestMigrationLeavesAStandingMigrationArchiveStanding(t *testing.T) {
	db := openEmptyDatabase(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	createMigrationArchive(t, db)

	if err := archiveRowInto(t, db, "rdioScannerTalkgroups", 405, "its system 10009 is not in this database", map[string]any{"_id": 405}); err != nil {
		t.Fatalf("archiving the fixture's row: %v", err)
	}

	migrateDatabase(t, db)

	assertMigrationArchive(t, db, "a deployment whose archive table already stood before the pass")

	if got := countRows(t, db, "migrationArchive"); got != 1 {
		t.Errorf("the pass left %d rows in the archive that already stood, want the 1 row it held: %v",
			got, readRows(t, db, `SELECT "tableName","rowId","reason" FROM "migrationArchive" ORDER BY "tableName","rowId"`))
	}
}

// The archive is declared for every dialect: a table present only for SQLite leaves a
// deployed backend's uncarriable rows with nowhere to go.
func TestMigrationArchiveIsDeclaredForEveryDialect(t *testing.T) {
	for _, tc := range []struct {
		dialect Dialect
		table   string
		index   string
	}{
		{DialectSqlite, migrationArchiveTableSqlite, migrationArchiveIndexSqlite},
		{DialectMariadb, migrationArchiveTableServer, migrationArchiveIndexServer},
		{DialectMysql, migrationArchiveTableServer, migrationArchiveIndexServer},
		{DialectPostgresql, migrationArchiveTablePostgres, migrationArchiveIndexPostgres},
	} {
		if !hasCreateTableIfNotExists(tc.table, "migrationArchive") {
			t.Errorf("%s does not create the migration archive with a statement that can run on every start: %s", tc.dialect, tc.table)
		}

		if !hasUniqueOn(tc.index, "tableName", "rowId") {
			t.Errorf("%s does not hold each archived row once by v6 table and identity: %s", tc.dialect, tc.index)
		}
	}
}

// isMigrationArchiveTable and isMigrationArchiveIndex name the two objects the archive adds to a
// database the migration pass runs on, in the shape readSchemaObjects reports them.
func isMigrationArchiveTable(object string) bool {
	return strings.HasPrefix(object, "table migrationArchive ")
}

func isMigrationArchiveIndex(object string) bool {
	return strings.HasPrefix(object, "index migration_archive_table_row ")
}

// The row the pass read but did not carry is held with its original values, the
// reason in the language of the system, and the moment the server archived it.
// Binary columns come back base64-encoded, which makes an archived call's audio
// recoverable from the contents column alone.
func TestArchiveRowHoldsTheRowThePassCouldNotCarry(t *testing.T) {
	db := openEmptyDatabase(t)

	migrateDatabase(t, db)

	// The pass creates the archive only where a legacy table is present to read from, and this
	// database holds none: the test is about the row the pass could not carry, so it asks for the
	// table the pass would have created for it.
	createMigrationArchive(t, db)

	s := newStore(db, DialectSqlite)
	s.clock = fixedClock{migrationInstant}

	tx, err := s.begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if err := archiveRow(context.Background(), tx, "rdioScannerCalls", 706, "it holds no audio", map[string]any{
		"id":     706,
		"audio":  []byte{0x01, 0x02, 0x03},
		"system": 10001,
	}); err != nil {
		tx.rollback()

		t.Fatalf("archiveRow: %v", err)
	}

	if err := tx.commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	rows := readRows(t, db, `SELECT "tableName", "rowId", "reason", "contents", "archivedAt" FROM "migrationArchive"`)

	if len(rows) != 1 {
		t.Fatalf("the archive holds %d rows, want the 1 row the pass could not carry: %v", len(rows), rows)
	}

	got := rows[0]

	want := []string{
		"rdioScannerCalls",
		"706",
		"it holds no audio",
		`{"audio":"AQID","id":706,"system":10001}`,
		strconv.FormatInt(migrationInstant.UnixMilli(), 10),
	}

	if !equalStrings(got, want) {
		t.Errorf("the archived row:\n  got  %v\n  want %v", got, want)
	}
}

// A v6 table that survived because a row of it could not be read is re-read on every
// later start, and its already-archived rows archive again as nothing: the first
// reason stays on record.
func TestArchivingTheSameRowTwiceHoldsItOnce(t *testing.T) {
	db := openEmptyDatabase(t)

	migrateDatabase(t, db)

	// As above: the archive is present here because the test needs it, not because this database has
	// a legacy table for the pass to read.
	createMigrationArchive(t, db)

	s := newStore(db, DialectSqlite)
	s.clock = fixedClock{migrationInstant}

	for _, reason := range []string{"its system 10009 is not in this database", "its system 10009 is not in this database"} {
		tx, err := s.begin(context.Background())
		if err != nil {
			t.Fatalf("begin: %v", err)
		}

		if err := archiveRow(context.Background(), tx, "rdioScannerTalkgroups", 405, reason, map[string]any{"_id": 405}); err != nil {
			tx.rollback()

			t.Fatalf("archiveRow: %v", err)
		}

		if err := tx.commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	if got := countRows(t, db, "migrationArchive"); got != 1 {
		t.Errorf("the archive holds %d rows after archiving the same v6 row twice, want it held exactly once", got)
	}
}

// An archive write that cannot run is a failure the caller is told about, not a row
// quietly dropped.
func TestArchiveRowIsRefusedWhenTheArchiveIsNotThere(t *testing.T) {
	db := openEmptyDatabase(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	s := newStore(db, DialectSqlite)
	s.clock = fixedClock{migrationInstant}

	tx, err := s.begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	err = archiveRow(context.Background(), tx, "rdioScannerCalls", 706, "it holds no audio", map[string]any{"id": 706})

	if err == nil {
		t.Fatal("archiving into a database that has no archive is a failure the caller has to be told about")
	}

	tx.rollback()
}

func assertMigrationArchive(t *testing.T, db *sql.DB, which string) {
	t.Helper()

	if !hasMigrationArchiveTable(t, db) {
		t.Fatalf("%s has no migrationArchive table after the migration", which)
	}

	var index string

	query := `SELECT "name" FROM "sqlite_master" WHERE "type" = 'index' AND "name" = 'migration_archive_table_row'`

	if err := db.QueryRow(query).Scan(&index); err != nil {
		t.Fatalf("%s has no migration_archive_table_row index: every archived row has to be held exactly once: %v", which, err)
	}
}

// hasMigrationArchiveTable reports whether the deployment holds the archive table at all, which is
// the question both the migration's gate and the clearing ask before either touches it.
func hasMigrationArchiveTable(t *testing.T, db *sql.DB) bool {
	t.Helper()

	for _, name := range readTableNames(t, db) {
		if name == "migrationArchive" {
			return true
		}
	}

	return false
}

// createMigrationArchive runs the migration's own archive step directly, for a test that needs a
// deployment whose archive table already stands without seeding a legacy table to make the pass build
// one. The step is idempotent, so a fixture may ask for it as many times as it likes.
func createMigrationArchive(t *testing.T, db *sql.DB) {
	t.Helper()

	if err := ensureMigrationArchive(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("creating the migration archive: %v", err)
	}
}

// archiveRowInto writes one archived row through the transaction shape the migration pass uses, for a
// test that needs an archive holding a row rather than an archive the pass filled.
func archiveRowInto(t *testing.T, db *sql.DB, table string, rowID int64, reason string, contents map[string]any) error {
	t.Helper()

	s := newStore(db, DialectSqlite)
	s.clock = fixedClock{migrationInstant}

	tx, err := s.begin(context.Background())
	if err != nil {
		return err
	}

	if err := archiveRow(context.Background(), tx, table, rowID, reason, contents); err != nil {
		tx.rollback()

		return err
	}

	return tx.commit()
}

// archivedRows reads what the archive holds for one v6 table, identity first, so a test can name the
// row the pass left behind together with the reason it says for it.
func archivedRows(t *testing.T, db *sql.DB, table string) [][]string {
	t.Helper()

	return readRows(t, db, `SELECT "tableName", "rowId", "reason", "contents", "archivedAt" FROM "migrationArchive" WHERE "tableName" = '`+table+`' ORDER BY "rowId"`)
}

// archivedRow reads the one row the archive holds for one v6 identity, or nil when it
// holds none. A test that arranges one damaged row asserts what the pass said about
// that row, not that nothing else was archived: the whole-archive assertions live in
// the integrity audit, which is where the count belongs.
func archivedRow(t *testing.T, db *sql.DB, table string, rowId int) []string {
	t.Helper()

	rows := readRows(t, db, `SELECT "tableName", "rowId", "reason", "contents", "archivedAt" FROM "migrationArchive" WHERE "tableName" = '`+table+`' AND "rowId" = `+strconv.Itoa(rowId))

	if len(rows) > 1 {
		t.Fatalf("the archive holds %d rows for %s %d, want it to hold that row once", len(rows), table, rowId)
	}

	if len(rows) == 0 {
		return nil
	}

	return rows[0]
}

// assertArchivedRow asserts the archive holds one v6 row with this reason and these contents.
func assertArchivedRow(t *testing.T, db *sql.DB, table string, rowId int, reason, contents string) {
	t.Helper()

	got := archivedRow(t, db, table, rowId)

	want := []string{
		table,
		strconv.Itoa(rowId),
		reason,
		contents,
		strconv.FormatInt(migrationInstant.UnixMilli(), 10),
	}

	if !equalStrings(got, want) {
		t.Errorf("the archive for %s %d:\n  got  %v\n  want %v", table, rowId, got, want)
	}
}

// hasCreateTableIfNotExists reports a statement that can be asked for on every start without
// refusing a database that already has the table.
func hasCreateTableIfNotExists(statement, table string) bool {
	return regexp.MustCompile(`(?i)CREATE TABLE IF NOT EXISTS "` + table + `"`).MatchString(statement)
}

// hasUniqueOn reports an index statement that holds each row once by exactly the named columns, in
// order: the archive's (tableName, rowId) is what makes re-archiving a surviving v6 table a no-op.
func hasUniqueOn(statement string, columns ...string) bool {
	match := regexp.MustCompile(`(?i)CREATE UNIQUE INDEX "migration_archive_table_row" ON "migrationArchive" \(([^)]*)\)`).FindStringSubmatch(statement)

	if match == nil {
		return false
	}

	declared := []string{}

	for _, column := range strings.Split(match[1], ",") {
		declared = append(declared, strings.Trim(strings.TrimSpace(column), `"`))
	}

	if len(declared) != len(columns) {
		return false
	}

	for i := range columns {
		if declared[i] != columns[i] {
			return false
		}
	}

	return true
}
