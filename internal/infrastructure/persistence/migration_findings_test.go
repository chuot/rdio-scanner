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
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

// The migration findings record holds one row per finding this server has announced about a
// deployment, so that a later start knows it already said this one. A finding is remembered by the
// thing it names, never by the sentence about it: a table the pass left standing, a row it left in
// the archive, and a unit it left damaged each take one row.

// A recorded finding is keyed by what it is. A table the pass left where it found it is keyed under
// its own name with 0, which no row of that table can be; a row the pass left in the archive is
// keyed under the same pair the archive holds that row under; and a unit the startup report names is
// keyed under the live units table and the unit's own identity, which no legacy table is named for.
func TestRecordingAFindingHoldsItUnderTheThingItNames(t *testing.T) {
	db := openEmptyDatabase(t)

	migrateDatabase(t, db)

	// The pass creates the record only where a legacy table stands for it to read a finding out of,
	// and this database holds none: the test is about what a finding holds, so it asks for the table
	// the pass would have created for it.
	createMigrationFindings(t, db)

	s := newStore(db, DialectSqlite)
	s.clock = fixedClock{migrationInstant}

	tx, err := s.begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	for _, finding := range []struct {
		table string
		rowId int64
	}{
		{"rdioScannerCalls", 0},
		{"rdioScannerCalls", 705},
		{"units", 303},
	} {
		first, err := recordFinding(context.Background(), tx, finding.table, finding.rowId)
		if err != nil {
			tx.rollback()

			t.Fatalf("recordFinding(%s, %d): %v", finding.table, finding.rowId, err)
		}

		if !first {
			tx.rollback()

			t.Fatalf("recording the finding %s %d answered that this start is not the first to name it", finding.table, finding.rowId)
		}
	}

	if err := tx.commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	rows := readRows(t, db, `SELECT "tableName", "rowId", "saidAt" FROM "migrationFindings" ORDER BY "tableName", "rowId"`)

	saidAt := strconv.FormatInt(migrationInstant.UnixMilli(), 10)

	want := [][]string{
		{"rdioScannerCalls", "0", saidAt},
		{"rdioScannerCalls", "705", saidAt},
		{"units", "303", saidAt},
	}

	if len(rows) != len(want) {
		t.Fatalf("the record holds %d findings, want the 3 the pass named: %v", len(rows), rows)
	}

	for i := range want {
		if !equalStrings(rows[i], want[i]) {
			t.Errorf("the finding the pass named at %d:\n  got  %v\n  want %v", i, rows[i], want[i])
		}
	}
}

// A finding the pass reads again on a later start — the same standing table, the same archived row —
// is the same finding. The record holds it once, and the write answers that this start is not the
// first to name it, which is the answer every sentence in the pass is gated on.
func TestRecordingTheSameFindingTwiceHoldsItOnceAndSaysTheLaterStartIsNotTheFirst(t *testing.T) {
	db := openEmptyDatabase(t)

	migrateDatabase(t, db)

	createMigrationFindings(t, db)

	first, err := recordFindingInto(t, db, "rdioScannerCalls", 705)
	if err != nil {
		t.Fatalf("recording the calls row 705: %v", err)
	}

	again, err := recordFindingInto(t, db, "rdioScannerCalls", 705)
	if err != nil {
		t.Fatalf("recording the calls row 705 a second time: %v", err)
	}

	if !first {
		t.Errorf("recording a finding the record does not hold answered that this start is not the first to name it")
	}

	if again {
		t.Errorf("recording the same finding again answered that this start is the first to name it: a start that meets a finding the record already holds has to be told it is not new")
	}

	if got := countRows(t, db, "migrationFindings"); got != 1 {
		t.Errorf("the record holds %d findings after the same finding was named twice, want it held exactly once", got)
	}
}

// A finding belongs to the thing it names. When the pass drops a v6 table, what the record knows
// about that table goes with it: a database that no longer holds the table has nothing left to say
// about it, and a finding about a table that is gone would be a finding no later start could ever
// meet again.
func TestDroppingATableClearsWhatTheRecordKnowsAboutThatTable(t *testing.T) {
	db := openEmptyDatabase(t)

	migrateDatabase(t, db)

	createMigrationFindings(t, db)

	for _, finding := range []struct {
		table string
		rowId int64
	}{
		{"rdioScannerCalls", 0},
		{"rdioScannerCalls", 705},
		{"units", 303},
	} {
		if _, err := recordFindingInto(t, db, finding.table, finding.rowId); err != nil {
			t.Fatalf("recording the finding %s %d: %v", finding.table, finding.rowId, err)
		}
	}

	forgetFindingsForTableIn(t, db, "rdioScannerCalls")

	want := [][]string{
		{"units", "303"},
	}

	got := recordedFindings(t, db)

	if len(got) != len(want) {
		t.Fatalf("the record holds %d findings after the calls table went: %v", len(got), got)
	}

	for i := range want {
		if !equalStrings(got[i], want[i]) {
			t.Errorf("the finding the record keeps after the calls table went:\n  got  %v\n  want %v", got[i], want[i])
		}
	}
}

// The record is created the way every other step of the startup pass is created: asked for again on a
// later start, it leaves the findings it already holds, and the instant each was first said at, exactly
// as they stand.
func TestMigrationFindingsStayStandingWhenTheyAreAskedForAgain(t *testing.T) {
	db := openEmptyDatabase(t)

	migrateDatabase(t, db)

	createMigrationFindings(t, db)

	if _, err := recordFindingInto(t, db, "units", 303); err != nil {
		t.Fatalf("recording the damaged unit 303: %v", err)
	}

	saidAt := readRows(t, db, `SELECT "saidAt" FROM "migrationFindings"`)

	createMigrationFindings(t, db)

	if got := countRows(t, db, "migrationFindings"); got != 1 {
		t.Fatalf("the record holds %d findings after it was asked for a second time, want the one finding it already held", got)
	}

	if again := readRows(t, db, `SELECT "saidAt" FROM "migrationFindings"`); !equalStrings(again[0], saidAt[0]) {
		t.Errorf("the record restamped the instant it first named the finding: first %v, then %v", saidAt[0], again[0])
	}

	indexes := readRows(t, db, `SELECT "name" FROM sqlite_master WHERE "type" = 'index' AND "tbl_name" = 'migrationFindings'`)

	if len(indexes) != 1 {
		t.Errorf("the record carries %d indexes after it was asked for a second time, want the one unique index it was created with: %v", len(indexes), indexes)
	}
}

// A finding this start cannot write down is a failure of the record, and the caller is told: the
// announcement still happens, because a finding nobody recorded is a finding a later start will meet
// again, but pretending the record worked when it did not is how a deployment loses the record of what
// was already said to it.
func TestRecordingAFindingIsRefusedWhenTheRecordIsNotThere(t *testing.T) {
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

	_, err = recordFinding(context.Background(), tx, "rdioScannerCalls", 705)

	if err == nil {
		t.Fatal("recording a finding into a database that has no findings record is a failure the caller has to be told about")
	}

	tx.rollback()
}

// recordedFindings is every finding the record holds, ordered by what it names and its identity.
func recordedFindings(t *testing.T, db *sql.DB) [][]string {
	t.Helper()

	return readRows(t, db, `SELECT "tableName", "rowId" FROM "migrationFindings" ORDER BY "tableName", "rowId"`)
}

// The record is declared for every dialect the server can run on: a finding a deployed backend
// announced has to be remembered by the backend it runs on, not only by the one these tests use.
func TestMigrationFindingsAreDeclaredForEveryDialect(t *testing.T) {
	for _, tc := range []struct {
		dialect Dialect
		table   string
		index   string
	}{
		{DialectSqlite, migrationFindingsTableSqlite, migrationFindingsIndexSqlite},
		{DialectMariadb, migrationFindingsTableServer, migrationFindingsIndexServer},
		{DialectMysql, migrationFindingsTableServer, migrationFindingsIndexServer},
		{DialectPostgresql, migrationFindingsTablePostgres, migrationFindingsIndexPostgres},
	} {
		if !hasCreateTableIfNotExists(tc.table, "migrationFindings") {
			t.Errorf("%s does not create the migration findings record with a statement that can run on every start: %s", tc.dialect, tc.table)
		}

		if !hasUniqueIndexNamed(tc.index, "migration_findings_table_row", "migrationFindings", "tableName", "rowId") {
			t.Errorf("%s does not hold each finding once by what it names and its identity: %s", tc.dialect, tc.index)
		}
	}
}

// The write of one finding tolerates the pair already standing on every backend: a record that
// refuses a finding a later start re-reads would stop the pass before it carries what it can carry.
func TestRecordingAFindingToleratesWhatTheRecordAlreadyHoldsOnEveryDialect(t *testing.T) {
	for _, tc := range []struct {
		dialect   Dialect
		tolerates string
	}{
		{DialectSqlite, `INSERT OR IGNORE INTO`},
		{DialectMariadb, `INSERT IGNORE INTO`},
		{DialectMysql, `INSERT IGNORE INTO`},
		{DialectPostgresql, `ON CONFLICT ("tableName", "rowId") DO NOTHING`},
	} {
		statement := findingsInsertStatement(tc.dialect)

		if !strings.Contains(statement, tc.tolerates) {
			t.Errorf("%s records a finding with a write that refuses one the record already holds: %s", tc.dialect, statement)
		}
	}
}

// A record this start cannot write does not get to hide what the start found: the sentence is said, and
// the failure that stopped the record is said beside it. A finding is never hidden by the record's own
// failure.
func TestSayFindingSaysTheFindingWhenTheRecordCannotBeWritten(t *testing.T) {
	db := openEmptyDatabase(t)

	s := newStore(db, DialectSqlite)
	s.clock = fixedClock{migrationInstant}

	tx, err := s.begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	defer tx.rollback()

	lines := []progressLine{}

	sayFinding(context.Background(), tx, func(level, message string) {
		lines = append(lines, progressLine{level: level, message: message})
	}, domain.LogLevelWarn, "rdioScannerCalls", 0,
		"the migration left the calls table where it found it: a row of it could not be read")

	messages, levels := []string{}, map[string]string{}

	for _, line := range lines {
		messages = append(messages, line.message)
		levels[line.message] = line.level
	}

	finding := "the migration left the calls table where it found it: a row of it could not be read"

	if !hasString(messages, finding) {
		t.Errorf("a start that could not write its findings record said nothing about the table it left: %v", messages)
	}

	if levels[finding] != domain.LogLevelWarn {
		t.Errorf("a finding the record could not hold was filed under %q, want %q", levels[finding], domain.LogLevelWarn)
	}

	failure := ""

	for _, message := range messages {
		if strings.HasPrefix(message, "the migration could not record what it found: ") {
			failure = message
		}
	}

	if failure == "" {
		t.Errorf("the pass hid the failure that stopped its record: %v", messages)
	}
}

// hasUniqueIndexNamed reports an index statement that holds each row once by exactly the named
// columns, in order, under the named index on the named table. The pair is what makes a later start
// that re-reads the same deployment meet a finding it already announced.
func hasUniqueIndexNamed(statement, index, table string, columns ...string) bool {
	match := regexp.MustCompile(`(?i)CREATE UNIQUE INDEX "` + index + `" ON "` + table + `" \(([^)]*)\)`).FindStringSubmatch(statement)

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

// hasMigrationFindingsTable reports whether the deployment holds the findings record at all, which is
// the question both the migration's gate and the maintenance run ask before either touches it.
func hasMigrationFindingsTable(t *testing.T, db *sql.DB) bool {
	t.Helper()

	for _, name := range readTableNames(t, db) {
		if name == "migrationFindings" {
			return true
		}
	}

	return false
}

// createMigrationFindings runs the record's own creation step directly, for a test that needs a
// deployment whose record already stands without seeding a legacy table to make the pass build one.
// The step is idempotent, so a fixture may ask for it as many times as it likes.
func createMigrationFindings(t *testing.T, db *sql.DB) {
	t.Helper()

	if err := ensureMigrationFindings(context.Background(), newStore(db, DialectSqlite)); err != nil {
		t.Fatalf("creating the migration findings record: %v", err)
	}
}

// recordFindingInto writes one finding through the transaction shape the migration pass uses, and
// hands back the answer that write gives about whether this start is the first to name it.
func recordFindingInto(t *testing.T, db *sql.DB, table string, rowId int64) (bool, error) {
	t.Helper()

	s := newStore(db, DialectSqlite)
	s.clock = fixedClock{migrationInstant}

	tx, err := s.begin(context.Background())
	if err != nil {
		return false, err
	}

	named, err := recordFinding(context.Background(), tx, table, rowId)
	if err != nil {
		tx.rollback()

		return false, err
	}

	if err := tx.commit(); err != nil {
		return false, err
	}

	return named, nil
}

// forgetFindingsForTableIn runs the record's own delete for one table, through the transaction shape
// the migration pass drops a v6 table in.
func forgetFindingsForTableIn(t *testing.T, db *sql.DB, table string) {
	t.Helper()

	s := newStore(db, DialectSqlite)

	tx, err := s.begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if err := forgetFindingsForTable(context.Background(), tx, table); err != nil {
		tx.rollback()

		t.Fatalf("forgetting what the record knows about %s: %v", table, err)
	}

	if err := tx.commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}
