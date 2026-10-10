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

package persistence

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// openLegacyV6Database builds the v6.6.3-shaped database the way the server opens its own database,
// foreign keys enforced.
func openLegacyV6Database(t *testing.T) *sql.DB {
	t.Helper()

	return buildRecordedDatabase(t, append(append([]string{}, legacyV6Schema...), legacyV6Rows...))
}

// openLegacyV6PartialDatabase builds the v6.6.3 database with the rows the pass cannot read.
func openLegacyV6PartialDatabase(t *testing.T) *sql.DB {
	t.Helper()

	return buildRecordedDatabase(t, append(append([]string{}, legacyV6Schema...), legacyV6PartialRows...))
}

// migrateLegacyV6Fixture migrates a private copy of the fixture and returns the database with
// every line the migration reported.
func migrateLegacyV6Fixture(t *testing.T) (*sql.DB, []string) {
	t.Helper()

	db := openLegacyV6Database(t)

	return db, migrateDatabase(t, db)
}

// The damaged fixtures are databases a legacy server has already migrated, in the two states the
// legacy migration pass actually leaves behind: migrated, where the pass finished and left the
// damage in the units and the missing source links, and crashed, where the calls migration was
// rolled back and the v6 calls table is still there. Their statements are recorded in
// migrations_fixtures_test.go.

// openDamagedFixture builds one of the damaged fixtures by name, so that a test which writes to it
// cannot damage the recorded statements.
func openDamagedFixture(t *testing.T, name string) *sql.DB {
	t.Helper()

	return buildRecordedDatabase(t, recordedDamagedStatements(t, name))
}

// damageReport keeps the lines the startup damage report wrote out of a progress report.
func damageReport(progress []string) []string {
	found := []string{}

	for _, line := range progress {
		if strings.HasPrefix(line, "the migration left") {
			found = append(found, line)
		}
	}

	return found
}

// tableSnapshot reads the tables the startup report is asked to leave alone, so that a test can
// prove the report read the database instead of writing it.
func tableSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()

	out := strings.Builder{}

	for _, table := range []string{"systems", "talkgroups", "units", "calls", "callUnits"} {
		for _, row := range rowsOf(t, db, table, "*") {
			fmt.Fprintf(&out, "%s %s\n", table, strings.Join(row, "|"))
		}
	}

	return out.String()
}

// migrateDatabase runs the migration pass and collects what it reports.
func migrateDatabase(t *testing.T, db *sql.DB) []string {
	t.Helper()

	messages := []string{}

	for _, line := range migrateLines(t, db) {
		messages = append(messages, line.message)
	}

	return messages
}

// progressLine is one line the migration reported, with the level it was filed under. The level matters
// on its own: the log view is filtered by it, so a problem filed as a notice is a problem an operator
// never sees, and a notice filed as a problem is one they act on.
type progressLine struct {
	level   string
	message string
}

// migrateLines runs the migration pass and keeps the level of every line it reported.
func migrateLines(t *testing.T, db *sql.DB) []progressLine {
	t.Helper()

	lines := []progressLine{}

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, func(level, message string) {
		lines = append(lines, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	return lines
}

// announcements keeps the "migrating …..." lines out of a progress report, for tests that care
// about which tables the pass found and in what order, not about the failures it printed.
func announcements(progress []string) []string {
	found := []string{}

	for _, line := range progress {
		if strings.HasSuffix(line, "...") {
			found = append(found, line)
		}
	}

	return found
}

// readRows runs a query and returns every value as a string, so a whole table can be compared at
// once. A NULL comes back as "NULL" and a blob as its bytes.
func readRows(t *testing.T, db *sql.DB, query string) [][]string {
	t.Helper()

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("reading the columns of %s: %v", query, err)
	}

	out := [][]string{}

	for rows.Next() {
		values := make([]any, len(columns))
		holders := make([]any, len(columns))

		for i := range values {
			holders[i] = &values[i]
		}

		if err := rows.Scan(holders...); err != nil {
			t.Fatalf("reading a row of %s: %v", query, err)
		}

		row := make([]string, len(columns))

		for i, value := range values {
			switch v := value.(type) {
			case nil:
				row[i] = "NULL"
			case []byte:
				row[i] = string(v)
			case int64:
				row[i] = strconv.FormatInt(v, 10)
			case float64:
				row[i] = strconv.FormatFloat(v, 'f', -1, 64)
			case string:
				row[i] = v
			default:
				row[i] = fmt.Sprintf("%v", v)
			}
		}

		out = append(out, row)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading %s: %v", query, err)
	}

	return out
}

func rowsOf(t *testing.T, db *sql.DB, table string, columns string) [][]string {
	t.Helper()

	return readRows(t, db, `SELECT `+columns+` FROM "`+table+`" ORDER BY 1`)
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// holdsIdentity reports whether a v8 table holds a row with that identity, which is how a test says
// the pass did not carry the row.
func holdsIdentity(t *testing.T, db *sql.DB, table, column string, identity int64) bool {
	t.Helper()

	var count int

	query := `SELECT COUNT(*) FROM "` + table + `" WHERE "` + column + `" = ?`

	if err := db.QueryRow(query, identity).Scan(&count); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return count > 0
}

// hasV6Table reports whether a v6 table is still there. Since FEAT-011 that is not a success or a
// failure on its own: it means the pass could not read every row of it and chose to leave it rather
// than destroy it.
func hasV6Table(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()

	for _, name := range readTableNames(t, db) {
		if name == table {
			return true
		}
	}

	return false
}

// TestMigrateMovesEveryV6TableIntoTheV7Schema is the shape of the whole pass: a v6 database comes
// out holding exactly the v7 tables, with none of the v6 tables left behind.
// archivedTables are the tables a database holds after this server's migration pass: the legacy
// tables, plus the migration archive this server owns. FEAT-011 added the archive to the migration
// pass, not to ApplySchema — the reason is in
// docs/specs/FEAT-011-v663-migration-integrity/decision-archive-then-drop.md.
var archivedTables = func() []string {
	tables := append(append([]string{}, legacyTables...), "migrationArchive")

	sort.Strings(tables)

	return tables
}()

func TestMigrateMovesEveryV6TableIntoTheV7Schema(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	if got, want := readTableNames(t, db), archivedTables; !equalStrings(got, want) {
		t.Errorf("the tables after migrating a v6 database:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateAnnouncesEachV6TableItFoundInLegacyOrder pins the order of the pass, which is not a
// detail: the talkgroup, unit, dirwatch, and call migrations resolve their v6 references against
// tables the earlier ones have already filled.
func TestMigrateAnnouncesEachV6TableItFoundInLegacyOrder(t *testing.T) {
	_, progress := migrateLegacyV6Fixture(t)

	want := []string{
		"migrating groups...",
		"migrating tags...",
		"migrating systems...",
		"migrating talkgroups...",
		"migrating units...",
		"migrating options...",
		"migrating meta...",
		"migrating logs...",
		"migrating downstreams...",
		"migrating dirwatches...",
		"migrating calls...",
		"migrating apikeys...",
		"migrating accesses...",
	}

	if got := announcements(progress); !equalStrings(got, want) {
		t.Errorf("the tables the migration announced:\n  got  %v\n  want %v", got, want)
	}
}

// TestTheMigrationSaysItsAnnouncementsAsNoticesAndItsFailuresAsProblems is the level half of the migration
// report. The source already separates the migration's entries from everything else in the log view; the
// level is what lets an operator narrow further, to the rows the pass could not carry and the damage it
// could not repair, without reading thirteen lines about which tables it looked at.
//
// The damage report matters most here: on a deployment the legacy server left damaged it is written on
// every start, and an entry buried at the same level as every ingested call is an entry nobody finds.
func TestTheMigrationSaysItsAnnouncementsAsNoticesAndItsFailuresAsProblems(t *testing.T) {
	// The clean fixture and the one with the rows the pass cannot read: the second is where the lines
	// FEAT-011 adds are said, and a line the pass says has to be classifiable as one of the two things
	// the pass says.
	for _, db := range []*sql.DB{openLegacyV6Database(t), openLegacyV6PartialDatabase(t)} {
		lines := migrateLines(t, db)

		problems := 0

		for _, line := range lines {
			switch {
			case strings.HasSuffix(line.message, "..."):
				if line.level != domain.LogLevelInfo {
					t.Errorf("%q is filed under %q, want a notice: the pass found that table and carried it",
						line.message, line.level)
				}

			case strings.Contains(line.message, "could not be finished"),
				strings.HasPrefix(line.message, "the migration could not carry"),
				strings.HasPrefix(line.message, "the migration carried"),
				strings.HasPrefix(line.message, "the migration left"):
				if line.level != domain.LogLevelWarn {
					t.Errorf("%q is filed under %q, want a problem: this is a row or a unit the upgrade could not carry",
						line.message, line.level)
				}

				problems++

			default:
				t.Errorf("%q is filed under %q, which is neither an announcement nor a problem: the pass says nothing else, so a line it says has to read as one of those two",
					line.message, line.level)
			}
		}

		if problems == 0 {
			t.Fatalf("the fixture produced no migration failure, want the rows it is built to leave behind: %v", lines)
		}
	}
}

// TestMigrateSaysEveryRowItCouldNotCarryAndEveryTableItLeftBehind is CA-003 at the level the pass reports
// at: the wording of each line FEAT-011 adds, in the order the pass says it, and the level it is filed
// under. The summary comes first because an operator reading the log needs the count before the list, the
// same shape the startup damage report uses.
func TestMigrateSaysEveryRowItCouldNotCarryAndEveryTableItLeftBehind(t *testing.T) {
	db := openLegacyV6PartialDatabase(t)

	got := []progressLine{}

	for _, line := range migrateLines(t, db) {
		if strings.HasPrefix(line.message, "the migration could not carry") ||
			strings.HasPrefix(line.message, "the migration left the") {
			got = append(got, line)
		}
	}

	want := []progressLine{
		{level: domain.LogLevelWarn, message: `the migration could not carry 1 talkgroup: it is kept in the migrationArchive table`},
		{level: domain.LogLevelWarn, message: `the migration could not carry talkgroup 207 "Orphaned": its system 10009 is not in this database`},
		{level: domain.LogLevelWarn, message: `the migration left the talkgroups table where it found it: a row of it could not be read`},
		{level: domain.LogLevelWarn, message: `the migration could not carry 1 unit: it is kept in the migrationArchive table`},
		{level: domain.LogLevelWarn, message: `the migration could not carry unit 304 "Orphan Engine": its system 10009 is not in this database`},
		{level: domain.LogLevelWarn, message: `the migration could not carry 1 config row: it is kept in the migrationArchive table`},
		{level: domain.LogLevelWarn, message: `the migration could not carry the config row "options": it holds values this server cannot carry: "autoDetectGroups" (v8 has no option of that name), "unknownKey" (v8 has no option of that name)`},
		{level: domain.LogLevelWarn, message: `the migration could not carry 1 log: it is kept in the migrationArchive table`},
		{level: domain.LogLevelWarn, message: `the migration could not carry log 405: it holds no message`},
		{level: domain.LogLevelWarn, message: `the migration could not carry 1 downstream: it is kept in the migrationArchive table`},
		{level: domain.LogLevelWarn, message: `the migration could not carry downstream 503: it holds no url`},
		{level: domain.LogLevelWarn, message: `the migration could not carry 2 calls: they are kept in the migrationArchive table`},
		{level: domain.LogLevelWarn, message: `the migration could not carry call 705: its system 10003 is not in this database`},
		{level: domain.LogLevelWarn, message: `the migration could not carry call 710: it holds no audio`},
		{level: domain.LogLevelWarn, message: `the migration left the calls table where it found it: a row of it could not be read`},
		{level: domain.LogLevelWarn, message: `the migration could not carry 1 access: it is kept in the migrationArchive table`},
		{level: domain.LogLevelWarn, message: `the migration could not carry access 904: it holds no code`},
	}

	if len(got) != len(want) {
		t.Fatalf("the pass said %d of the lines FEAT-011 is for:\n  got  %v\n  want %v", len(got), got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n  got  %q under %q\n  want %q under %q", i+1, got[i].message, got[i].level,
				want[i].message, want[i].level)
		}
	}
}

// TestMigrateOnAFreshDatabaseMigratesNothingAndSaysNothing is the case a deployment of this server
// that has never run is in: there are no v6 tables, so the pass creates the schema and then does and
// says nothing.
func TestMigrateOnAFreshDatabaseMigratesNothingAndSaysNothing(t *testing.T) {
	db := openEmptyDatabase(t)

	if progress := migrateDatabase(t, db); len(progress) != 0 {
		t.Errorf("a fresh database was announced as migrating: %v", progress)
	}

	if got, want := readTableNames(t, db), archivedTables; !equalStrings(got, want) {
		t.Errorf("the tables of a fresh database:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateOnALegacyWrittenV7DatabaseMigratesNothing covers the deployment that swaps one server
// binary for the other on a database the legacy server already migrated.
//
// FEAT-010 narrowed what "nothing" means here, and deliberately: the v6 migration pass still moves no
// row and says no word on a database that has no v6 tables, but the startup pass now brings the log table
// up to the shape this server writes — the column that names the part of the server that wrote each entry,
// and the two indexes the log search runs against. FEAT-011 added one more object the pass owns: the
// migration archive and its unique index, the table that holds every v6 row a pass could not carry so
// that no upgrade destroys one quietly. That is the whole list of what this server changes on a database
// the other server wrote, and the test says so exactly: those four objects and nothing else.
func TestMigrateOnALegacyWrittenV7DatabaseMigratesNothing(t *testing.T) {
	db := openLegacyDatabase(t)

	before := readSchemaObjects(t, db)
	beforeRows := readLogRows(t, db)

	if progress := migrateDatabase(t, db); len(progress) != 0 {
		t.Errorf("a v7 database was announced as migrating: %v", progress)
	}

	after := readSchemaObjects(t, db)

	if len(after) != len(before)+4 {
		t.Fatalf("a v7 database gained %d schema objects, want the two log indexes and the migration archive with its index", len(after)-len(before))
	}

	// The logs table is the one object allowed to read differently afterwards, and only because it
	// gained a column. Every other table, index and trigger has to be byte-identical, and the only new
	// objects are the two indexes the log search runs against and the archive the migration writes into.
	for _, object := range before {
		if isLogsTable(object) {
			continue
		}

		if !hasString(after, object) {
			t.Errorf("a v7 database lost or changed a schema object it already had: %s", object)
		}
	}

	added := []string{}

	for _, object := range after {
		if hasString(before, object) || isLogsTable(object) {
			continue
		}

		added = append(added, object)
	}

	if len(added) != 4 ||
		!isLogIndex(added[0], "logs_source_idx") ||
		!isLogIndex(added[1], "logs_time_idx") ||
		!isMigrationArchiveIndex(added[2]) ||
		!isMigrationArchiveTable(added[3]) {
		t.Errorf("the schema objects a v7 database gained: %v, want logs_source_idx, logs_time_idx, the migrationArchive table, and its migration_archive_table_row index", added)
	}

	if !hasString(after, `index logs_source_idx `+logIndexDefinition(t, db, "logs_source_idx")) {
		t.Error("logs_source_idx is not the index the migration says it created")
	}

	afterRows := readLogRows(t, db)

	if len(beforeRows) != len(afterRows) {
		t.Fatalf("a v7 database had %d log rows before the migration and %d after it", len(beforeRows), len(afterRows))
	}

	for i := range beforeRows {
		if beforeRows[i] != afterRows[i] {
			t.Errorf("log row %d changed on a database that had nothing to migrate:\nbefore: %s\nafter:  %s", i, beforeRows[i], afterRows[i])
		}
	}
}

// TestMigrateTwiceIsTheSameAsOnce is what a restart does: the first pass dropped every v6 table,
// so the second one finds nothing to say and changes nothing.
func TestMigrateTwiceIsTheSameAsOnce(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	before := readSchemaObjects(t, db)
	beforeCalls := countRows(t, db, "calls")

	if progress := migrateDatabase(t, db); len(progress) != 0 {
		t.Errorf("the second pass announced: %v", progress)
	}

	if after := readSchemaObjects(t, db); !equalStrings(before, after) {
		t.Errorf("the second pass changed the schema")
	}

	if after := countRows(t, db, "calls"); after != beforeCalls {
		t.Errorf("the second pass changed the calls: %d rows, was %d", after, beforeCalls)
	}
}

// TestMigrateTwiceOverAV6TableItLeftBehindCarriesNothingTwice is the case FEAT-011 creates, where
// TestMigrateTwiceIsTheSameAsOnce above cannot reach it: a v6 table the pass could not read whole
// survives to the next start, so the next start reads the rows it already carried a second time. They
// must not be written twice, reported as constraint failures, or archived as if they had been lost.
func TestMigrateTwiceOverAV6TableItLeftBehindCarriesNothingTwice(t *testing.T) {
	once := openLegacyV6PartialDatabase(t)

	migrateDatabase(t, once)

	twice := openLegacyV6PartialDatabase(t)

	migrateDatabase(t, twice)

	progress := migrateDatabase(t, twice)

	if before, after := readSchemaObjects(t, once), readSchemaObjects(t, twice); !equalStrings(before, after) {
		t.Errorf("two passes left a different schema than one:\n  once  %v\n  twice %v", before, after)
	}

	tables := []string{
		"groups", "tags", "systems", "talkgroups", "talkgroupGroups", "units", "options", "logs",
		"downstreams", "dirwatches", "calls", "callFrequencies", "callUnits", "apikeys", "accesses",
		"migrationArchive",
	}

	for _, table := range tables {
		got, want := rowsOf(t, twice, table, "*"), rowsOf(t, once, table, "*")

		if !equalStrings(flat(got), flat(want)) {
			t.Errorf("the second pass changed %s:\n  once  %v\n  twice %v", table, want, got)
		}
	}

	for _, line := range progress {
		if strings.Contains(line, "could not be finished") {
			t.Errorf("the second pass reported a failure the first pass did not: %v", progress)
		}
	}

	// The tables the first pass left behind are still there, and still left behind.
	for _, table := range []string{"rdioScannerTalkgroups", "rdioScannerCalls"} {
		if !hasV6Table(t, twice, table) {
			t.Errorf("%s was dropped by a second pass that still could not read every row of it", table)
		}
	}
}

// TestMigrateTwiceOverACrashedLegacyDatabaseChangesNothing is the same guarantee over the database a
// legacy server actually leaves when its calls migration rolls back: the v8 tables are already written,
// the v6 calls table is still there, and this server's first pass carries the calls and drops it. The
// start after that must find nothing to do.
func TestMigrateTwiceOverACrashedLegacyDatabaseChangesNothing(t *testing.T) {
	db := openDamagedFixture(t, "crashed")

	migrateDatabase(t, db)

	objects := readSchemaObjects(t, db)
	contents := map[string]string{}

	for _, table := range []string{"calls", "callFrequencies", "callUnits", "units", "migrationArchive"} {
		contents[table] = strings.Join(flat(rowsOf(t, db, table, "*")), "\n")
	}

	progress := migrateDatabase(t, db)

	// The startup damage report says its lines again, as it does at every start. What the second pass
	// must not do is read the v6 tables a second time or report a failure for doing so.
	for _, line := range progress {
		if strings.HasPrefix(line, "migrating ") || strings.Contains(line, "could not be finished") {
			t.Errorf("the second pass over a crashed legacy database migrated or failed: %v", progress)
		}
	}

	if after := readSchemaObjects(t, db); !equalStrings(objects, after) {
		t.Errorf("the second pass over a crashed legacy database changed the schema")
	}

	for table, want := range contents {
		if got := strings.Join(flat(rowsOf(t, db, table, "*")), "\n"); got != want {
			t.Errorf("the second pass over a crashed legacy database changed %s:\n  once  %s\n  twice %s", table, want, got)
		}
	}
}

// TestMigrateAnnouncesAndDropsAV6TableThatHoldsNoRows pins the probe: it asks whether the v6 table
// can be queried at all and throws the answer away, so an empty v6 table is announced, opened a
// transaction for, and dropped, exactly like a full one.
func TestMigrateAnnouncesAndDropsAV6TableThatHoldsNoRows(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `DELETE FROM "rdioScannerApiKeys"`)

	progress := migrateDatabase(t, db)

	if !strings.Contains(strings.Join(progress, "\n"), "migrating apikeys...") {
		t.Errorf("an empty v6 table was not announced: %v", progress)
	}

	for _, name := range readTableNames(t, db) {
		if name == "rdioScannerApiKeys" {
			t.Errorf("the empty v6 table was left behind")
		}
	}

	if count := countRows(t, db, "apikeys"); count != 0 {
		t.Errorf("an empty v6 table migrated %d apikeys", count)
	}
}

// TestMigrateSkipsAV6TableThatIsNotThereWithoutSayingSo is the other half of the probe: a v6 table
// that is not there ends that migration with nothing said and nothing done.
func TestMigrateSkipsAV6TableThatIsNotThereWithoutSayingSo(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `DROP TABLE "rdioScannerTags"`)

	progress := migrateDatabase(t, db)

	if strings.Contains(strings.Join(progress, "\n"), "migrating tags...") {
		t.Errorf("a missing v6 table was announced: %v", progress)
	}

	if count := countRows(t, db, "tags"); count != 0 {
		t.Errorf("a missing v6 table migrated %d tags", count)
	}
}

// TestMigrateLeavesAV6TableItCouldNotReadWhole is REQ-002: the pass may not destroy what it never
// read. sqlite lets a v6 deployment keep text in a column it declared `datetime` or `integer`, and
// the pass reads a table row by row, so such a row is lost at the scan — before the pass has any
// values to archive. The legacy server dropped the table at the end of the pass whatever happened,
// which is how a deployment lost a row it could not even be told about. Here the table stays, with
// its unread row in it, and the pass says why.
func TestMigrateLeavesAV6TableItCouldNotReadWhole(t *testing.T) {
	db := openLegacyV6PartialDatabase(t)

	progress := migrateDatabase(t, db)

	for _, table := range []string{"rdioScannerTalkgroups", "rdioScannerCalls"} {
		if !hasV6Table(t, db, table) {
			t.Errorf("%s was dropped by a pass that could not read every row of it", table)
		}
	}

	// The unread rows are still there, unchanged: the pass did not read them, so it has no right to
	// rewrite or remove them.
	got := readRows(t, db, `SELECT "_id", "frequency", "name" FROM "rdioScannerTalkgroups" WHERE "_id" = 206`)
	want := [][]string{{"206", "not a frequency", "Unreadable"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the talkgroup the pass could not read:\n  got  %v\n  want %v", got, want)
	}

	got = readRows(t, db, `SELECT "id", "dateTime", "audioName" FROM "rdioScannerCalls" WHERE "id" = 709`)
	want = [][]string{{"709", "not a timestamp", "709.wav"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the call the pass could not read:\n  got  %v\n  want %v", got, want)
	}

	// The rows of those same tables the pass could read were carried or archived as they are over the
	// clean fixture: keeping a table is not the same as not migrating it.
	carried := rowsOf(t, db, "talkgroups", `"talkgroupId"`)

	if !equalStrings(flat(carried), flat([][]string{{"201"}, {"202"}, {"203"}})) {
		t.Errorf("the carried talkgroups: %v, want the three the pass could read", carried)
	}

	carried = rowsOf(t, db, "calls", `"callId"`)

	if !equalStrings(flat(carried), flat([][]string{{"701"}, {"702"}, {"703"}, {"704"}, {"706"}})) {
		t.Errorf("the carried calls: %v, want the five the pass could read", carried)
	}

	if archived := archivedRows(t, db, "rdioScannerCalls"); len(archived) != 2 || archived[0][1] != "705" || archived[1][1] != "710" {
		t.Errorf("the archived calls: %v, want the two calls the pass read and could not carry", archived)
	}

	// The tables it read whole are dropped, and the migrations after them ran.
	for _, table := range []string{"rdioScannerGroups", "rdioScannerUnits", "rdioScannerLogs", "rdioScannerMeta"} {
		if hasV6Table(t, db, table) {
			t.Errorf("%s survived a pass that read every row of it", table)
		}
	}

	if count := countRows(t, db, "units"); count != 2 {
		t.Errorf("the units after a pass that met an unread row elsewhere: %d, want the two the fixture holds", count)
	}

	for _, want := range []string{
		"the migration left the talkgroups table where it found it: a row of it could not be read",
		"the migration left the calls table where it found it: a row of it could not be read",
	} {
		if !hasString(progress, want) {
			t.Errorf("the migration said nothing about the table it left behind: %v", progress)
		}
	}
}

// TestMigrateReturnsAFailureTheLegacyMigrationReturnedAndStopsThePass covers the failures the
// legacy server returned rather than printed. The meta migration is the clearest one: it drops the
// v6 bookkeeping table outside any transaction and returns when the drop fails, which ends the
// whole upgrade with the v6 tables the later migrations would have consumed still in place.
//
// The arrange makes the drop fail by leaving a view where the table was: the probe reads a view as
// easily as a table, and a table drop refuses a view.
func TestMigrateReturnsAFailureTheLegacyMigrationReturnedAndStopsThePass(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `DROP TABLE "rdioScannerMeta"`)
	mustExec(t, db, `CREATE VIEW "rdioScannerMeta" AS SELECT 'x' AS name`)

	err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil)
	if err == nil {
		t.Fatalf("Migrate: expected the failure the legacy migration returned, got none")
	}

	for _, want := range []string{
		"database.migrate:",
		"migration.migrateMeta:",
		`in DROP TABLE "rdioScannerMeta"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Migrate returned %q, expected it to contain %q", err, want)
		}
	}

	if count := countRows(t, db, "rdioScannerLogs"); count == 0 {
		t.Errorf("the failure stopped the pass before the logs were migrated, but the v6 logs are gone")
	}

	if count := countRows(t, db, "logs"); count != 0 {
		t.Errorf("the failure stopped the pass, yet %d logs were migrated after it", count)
	}
}

// TestMigrateGroupsKeepTheirIdentityAndAreOrderedByLabel: a migrated group keeps the identity its
// talkgroups refer to, and only its display order is decided by sorting the labels.
func TestMigrateGroupsKeepTheirIdentityAndAreOrderedByLabel(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "groups", `"groupId", "label", "order"`)
	want := [][]string{
		{"1", "Unknown", "6"},
		{"2", "Fire", "3"},
		{"3", "Law", "5"},
		{"5", "EMS", "2"},
		{"7", "Air", "1"},
		{"9", "Interop", "4"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated groups:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateTagsKeepTheirIdentityAndAreOrderedByLabel is the same rule for tags, with a label the
// legacy server had to quote-escape and a sort that lands out of id order.
func TestMigrateTagsKeepTheirIdentityAndAreOrderedByLabel(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "tags", `"tagId", "label", "order"`)
	want := [][]string{
		{"1", "Air Traffic Control", "1"},
		{"2", "Interop", "3"},
		{"4", "Fire Dispatch", "2"},
		{"6", "Untagged", "5"},
		{"8", "Marshal's Office", "4"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated tags:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateSystemsStoresTheBlacklistWithoutItsBrackets pins the one transformation the system
// migration performs on a value: the v6 blacklist is a JSON array, and the legacy migration stores
// it with the brackets cut out.
func TestMigrateSystemsStoresTheBlacklistWithoutItsBrackets(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "systems", `"systemId", "autoPopulate", "blacklists", "label", "led", "order", "systemRef"`)
	want := [][]string{
		{"101", "1", "40000, 40001", "Grand Metro", "red", "1", "10001"},
		{"102", "0", "", "Harbour", "", "2", "10002"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated systems:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateSystemsStoresAQuoteTheLegacyStatementCouldNotHold is a deliberate deviation, and the
// cost of binding values instead of writing them into the statement: the legacy migration escapes
// the system label but not the blacklist or the color, so a single quote in either one ends the
// string early and the INSERT is a syntax error. The legacy logs the failure and drops the system,
// which then takes its talkgroups, units, calls, and dirwatches with it because nothing resolves to
// it any more. Here the value is a parameter, the row lands, and the rows that hang off it resolve.
//
// See the legacy `server/migrations.go`, migrateSystems: `system.Blacklists` and `system.Led` are assigned
// without escapeQuotes and then interpolated into the INSERT.
func TestMigrateSystemsStoresAQuoteTheLegacyStatementCouldNotHold(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `UPDATE "rdioScannerSystems" SET "blacklists" = '[400''x]', "led" = 'red''x' WHERE "_id" = 101`)

	progress := migrateDatabase(t, db)

	got := rowsOf(t, db, "systems", `"systemId", "blacklists", "label", "led"`)
	want := [][]string{
		{"101", "400'x", "Grand Metro", "red'x"},
		{"102", "", "Harbour", ""},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated systems:\n  got  %v\n  want %v", got, want)
	}

	for _, line := range progress {
		if strings.HasPrefix(line, "migration.migrateSystems:") {
			t.Errorf("the system holding a quote in its blacklist was reported as a failure: %q", line)
		}
	}

	if got, want := countRows(t, db, "talkgroups"), 3; got != want {
		t.Errorf("the talkgroups of that system migrated %d rows, want %d; progress: %v", got, want, progress)
	}
}

// TestMigrateStoresTextTheLegacyInterpolatedWithoutEscaping covers the rest of the columns the
// legacy migration writes into its statement without escaping them: the talkgroup color, the log
// level, the dirwatch type, and the call audio type. Each one is stored as the v6 row held it.
//
// See the legacy `server/migrations.go`: `talkgroup.Led`, `l.Level`, `dirwatch.Kind`, and `call.AudioMime` are
// assigned raw and interpolated into an INSERT that quotes them.
func TestMigrateStoresTextTheLegacyInterpolatedWithoutEscaping(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `UPDATE "rdioScannerTalkgroups" SET "led" = 'amber''x' WHERE "_id" = 202`)
	mustExec(t, db, `UPDATE "rdioScannerLogs" SET "level" = 'warn''x' WHERE "_id" = 402`)
	mustExec(t, db, `UPDATE "rdioScannerDirWatches" SET "type" = 'dsdPlus''x' WHERE "_id" = 601`)
	mustExec(t, db, `UPDATE "rdioScannerCalls" SET "audioType" = 'audio/wav''x' WHERE "id" = 701`)

	progress := migrateDatabase(t, db)

	for _, failure := range []struct {
		table   string
		columns string
		id      string
		want    []string
	}{
		{"talkgroups", `"talkgroupId", "led"`, "202", []string{"202", "amber'x"}},
		{"logs", `"logId", "level"`, "402", []string{"402", "warn'x"}},
		{"dirwatches", `"dirwatchId", "type"`, "601", []string{"601", "dsdPlus'x"}},
		{"calls", `"callId", "audioMime"`, "701", []string{"701", "audio/wav'x"}},
	} {
		for _, row := range rowsOf(t, db, failure.table, failure.columns) {
			if row[0] == failure.id && !equalStrings(row, failure.want) {
				t.Errorf("the %s row holding a quote landed as %v, want %v", failure.table, row, failure.want)
			}
		}

		if got := countRows(t, db, failure.table); got == 0 {
			t.Errorf("no %s rows survived the migration; progress: %v", failure.table, progress)
		}
	}
}

// TestMigrateTalkgroupsResolveTheirSystemThroughTheSystemReference: a v6 talkgroup names its system
// by the v6 system's `id`, not its row identity, and the migrated talkgroup lands on the identity
// the systems migration gave that system.
func TestMigrateTalkgroupsResolveTheirSystemThroughTheSystemReference(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "talkgroups", `"talkgroupId", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef"`)
	want := [][]string{
		{"201", "1514000000", "Dispatch 1", "", "Dispatch 1", "1", "101", "1", "1514000"},
		{"202", "1514001000", "Interop 9", "blue", "Interop 9", "2", "101", "4", "1514001"},
		{"203", "1514002000", "Un grouped", "", "Un grouped", "3", "102", "6", "1514002"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated talkgroups:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateTalkgroupsSayWhichTalkgroupLostItsGroupLink is the repaired half of a legacy defect the
// fixture pins. v6 stored 0 for a talkgroup in no group, and the migration handed that 0 to the group
// link, whose foreign key rejects it: the refusal came back worded differently on every backend, and
// PostgreSQL treats a refused statement as the end of the transaction rather than of the row. The pass
// now asks whether the group is in this database before it writes the link. The talkgroup is carried
// either way, no link is written for a group that does not exist, and the group the row named is said.
func TestMigrateTalkgroupsSayWhichTalkgroupLostItsGroupLink(t *testing.T) {
	db, progress := migrateLegacyV6Fixture(t)

	if got := countRows(t, db, "talkgroups"); got != 3 {
		t.Errorf("the talkgroup in no group was not carried: %d talkgroups, want 3", got)
	}

	want := [][]string{{"3", "201"}, {"7", "202"}}

	if got := rowsOf(t, db, "talkgroupGroups", `"groupId", "talkgroupId"`); !equalStrings(flat(got), flat(want)) {
		t.Errorf("the group links:\n  got  %v\n  want %v, the talkgroup in no group having none", got, want)
	}

	line := `the migration carried talkgroup 203 "Un grouped" without its group: group 0 is not in this database`

	if !hasString(progress, line) {
		t.Errorf("the talkgroup that lost its group link was not named: want %q in %v", line, progress)
	}

	for _, line := range progress {
		if strings.Contains(line, "talkgroupGroups") {
			t.Errorf("a group link is reported as a statement the database refused instead of as a fact about "+
				"the row: %q", line)
		}
	}
}

// TestMigrateUnitsKeepTheRadioReferenceEachV6UnitHeld is the repaired half of Q22.
//
// The legacy insert named `unit.Id` — the identity `NewUnit()` never gave the unit — in the position the
// unit reference belongs in, so every migrated unit landed with `unitRef` 0 and the v6 reference its row
// was read for was thrown away. The v6 `id` column is the radio's own number, and it is what
// `callUnits.unitRef` and the call migration's unit lookup go looking for, so losing it is what made a
// migrated unit unfindable at the very next step of the same migration. See
// docs/specs/FEAT-004-legacy-defect-repairs/decision-migration-unit-references.md.
func TestMigrateUnitsKeepTheRadioReferenceEachV6UnitHeld(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	// The call migration adds a unit of its own later on; this test is about the units the unit
	// migration wrote from the v6 table.
	got := readRows(t, db, `SELECT "unitId", "label", "order", "systemId", "unitRef" FROM "units" WHERE "unitId" IN (301, 302) ORDER BY 1`)
	want := [][]string{
		{"301", "Engine 12", "1", "101", "12345"},
		{"302", "Car 4", "2", "101", "6789"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated units:\n  got  %v\n  want %v", got, want)
	}

	for _, row := range rowsOf(t, db, "units", `"unitId", "label", "unitRef"`) {
		if row[2] == "0" {
			t.Errorf("the migrated unit %v holds no radio reference, want the v6 reference its row was read for", row)
		}
	}
}

// TestMigrateTalkgroupsArchivesATalkgroupWhoseSystemIsNotInThisDatabase is REQ-001 on the shape the
// legacy pass was worst at: a talkgroup in a radio system this deployment never had. The legacy
// migration resolved its system, found nothing, said nothing, and the row went out of the database
// with the v6 table. Here the row is kept and the reason is said.
func TestMigrateTalkgroupsArchivesATalkgroupWhoseSystemIsNotInThisDatabase(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `INSERT INTO "rdioScannerTalkgroups" ("_id", "frequency", "groupId", "id", "label", "led", "name", "order", "systemId", "tagId") VALUES (204, 1514003000, 3, 1514003, 'Orphan', NULL, 'Orphan', 4, 10009, 6)`)

	progress := migrateDatabase(t, db)

	if holdsIdentity(t, db, "talkgroups", "talkgroupId", 204) {
		t.Fatal("the migration carried a talkgroup whose system is not in this database: the row would name a system that does not exist")
	}

	assertArchivedRow(t, db, "rdioScannerTalkgroups", 204, "its system 10009 is not in this database", `{"_id":204,"frequency":1514003000,"groupId":3,"id":1514003,"label":"Orphan","led":null,"name":"Orphan","order":4,"systemId":10009,"tagId":6}`)

	if !hasString(progress, `the migration could not carry talkgroup 204 "Orphan": its system 10009 is not in this database`) {
		t.Errorf("the migration said nothing about the talkgroup it could not carry: %v", progress)
	}

	if hasV6Table(t, db, "rdioScannerTalkgroups") {
		t.Error("the v6 talkgroups table survived a pass that read every row of it: only a row the pass could not read keeps its table alive")
	}
}

// TestMigrateTalkgroupsArchivesATalkgroupTheDatabaseRefused is the other way a row is lost: the
// migration had a system for it, but the row names a tag this database does not have, so the insert
// into the v8 table is refused. The failure line the pass already says is kept, and the row itself is
// archived rather than dropped with the v6 table.
func TestMigrateTalkgroupsArchivesATalkgroupTheDatabaseRefused(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `INSERT INTO "rdioScannerTalkgroups" ("_id", "frequency", "groupId", "id", "label", "led", "name", "order", "systemId", "tagId") VALUES (205, 1514004000, 3, 1514004, 'No tag', NULL, 'No tag', 5, 10001, 999)`)

	progress := migrateDatabase(t, db)

	if holdsIdentity(t, db, "talkgroups", "talkgroupId", 205) {
		t.Fatal("the migration carried a talkgroup whose tag is not in this database, which the v8 table will not hold")
	}

	assertArchivedRow(t, db, "rdioScannerTalkgroups", 205, "the database refused it", `{"_id":205,"frequency":1514004000,"groupId":3,"id":1514004,"label":"No tag","led":null,"name":"No tag","order":5,"systemId":10001,"tagId":999}`)

	if !hasString(progress, `the migration could not carry talkgroup 205 "No tag": the database refused it`) {
		t.Errorf("the migration said nothing about the talkgroup it could not carry: %v", progress)
	}

	failed := false

	for _, line := range progress {
		if strings.HasPrefix(line, "the migration of talkgroups could not be finished") {
			failed = true

			break
		}
	}

	if !failed {
		t.Errorf("the pass no longer says the insert failure it has always reported: %v", progress)
	}

	if hasV6Table(t, db, "rdioScannerTalkgroups") {
		t.Error("the v6 talkgroups table survived a pass that read every row of it")
	}
}

// TestMigrateUnitsArchivesAUnitWhoseSystemIsNotInThisDatabase is REQ-001 on the units table, which
// resolves its system the same way and skips the same way. Unit 304 of the fixture is that unit: the v6
// units table has a unique (systemId, id), so the fixture holds the one row of a system this database
// does not have rather than every test adding its own.
func TestMigrateUnitsArchivesAUnitWhoseSystemIsNotInThisDatabase(t *testing.T) {
	db := openLegacyV6Database(t)

	progress := migrateDatabase(t, db)

	if holdsIdentity(t, db, "units", "unitId", 304) {
		t.Fatal("the migration carried a unit whose system is not in this database")
	}

	assertArchivedRow(t, db, "rdioScannerUnits", 304, "its system 10009 is not in this database", `{"_id":304,"id":55555,"label":"Orphan Engine","order":3,"systemId":10009}`)

	if !hasString(progress, `the migration could not carry unit 304 "Orphan Engine": its system 10009 is not in this database`) {
		t.Errorf("the migration said nothing about the unit it could not carry: %v", progress)
	}

	if hasV6Table(t, db, "rdioScannerUnits") {
		t.Error("the v6 units table survived a pass that read every row of it")
	}
}

// TestMigrateUnitsCarryNothingTwiceForAUnitTheV8TableAlreadyHolds is the state a v6 table that
// survived a completed pass leaves behind: the next start reads the same rows again, into identities
// the v8 table already holds. Those units are not lost — they are in the v8 table under the identity
// they were written under — so they are not archived, nothing is said about them, and the pass does not
// report a constraint failure for a row it has already carried.
func TestMigrateUnitsCarryNothingTwiceForAUnitTheV8TableAlreadyHolds(t *testing.T) {
	db := openLegacyV6Database(t)

	migrateDatabase(t, db)

	mustExec(t, db, `CREATE TABLE "rdioScannerUnits" ("_id" integer primary key autoincrement, "id" integer, "label" text, "order" integer, "systemId" integer)`)
	mustExec(t, db, `INSERT INTO "rdioScannerUnits" ("_id", "id", "label", "order", "systemId") VALUES (301, 12345, 'Engine 12', 1, 10001), (302, 6789, 'Car 4', 2, 10001)`)

	progress := migrateDatabase(t, db)

	if got := archivedRow(t, db, "rdioScannerUnits", 301); got != nil {
		t.Errorf("the archive holds %v, want nothing: a unit the v8 table already holds has not been lost", got)
	}

	if got := archivedRow(t, db, "rdioScannerUnits", 302); got != nil {
		t.Errorf("the archive holds %v, want nothing: a unit the v8 table already holds has not been lost", got)
	}

	// The unit the first pass archived stays archived exactly once, rather than again.
	assertArchivedRow(t, db, "rdioScannerUnits", 304, "its system 10009 is not in this database", `{"_id":304,"id":55555,"label":"Orphan Engine","order":3,"systemId":10009}`)

	for _, line := range progress {
		if strings.Contains(line, "unit 301") || strings.Contains(line, "unit 302") || strings.Contains(line, "the migration of units could not be finished") {
			t.Errorf("the second pass said something about a unit it had already carried: %v", progress)
		}
	}

	got := readRows(t, db, `SELECT "unitId", "label", "unitRef" FROM "units" WHERE "unitId" IN (301, 302) ORDER BY 1`)
	want := [][]string{{"301", "Engine 12", "12345"}, {"302", "Car 4", "6789"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the units after a second pass:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateOptionsUnpacksTheV6OptionsBlobAndCopiesTheOtherRows: the v6 options blob is unpacked
// into the fixed list of names the legacy migration names, in that order, each stored as the JSON
// text of its value; every other row of the v6 configuration table is copied across as it stands.
func TestMigrateOptionsUnpacksTheV6OptionsBlobAndCopiesTheOtherRows(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "options", `"optionId", "key", "value"`)
	want := [][]string{
		{"1", "audioConversion", "true"},
		{"2", "autoPopulate", "false"},
		{"3", "branding", `"Rdio Legacy"`},
		{"4", "dimmerDelay", "30000"},
		{"5", "disableDuplicateDetection", "false"},
		{"6", "duplicateDetectionTimeFrame", "300"},
		{"7", "email", `"ops@example.com"`},
		{"8", "keypadBeeps", `"off"`},
		{"9", "maxClients", "5"},
		{"10", "playbackGoesLive", "true"},
		{"11", "pruneDays", "30"},
		{"12", "showListenersCount", "false"},
		{"13", "sortTalkgroups", "true"},
		{"14", "time12hFormat", "false"},
		{"15", "secret", "legacy secret value"},
		{"16", "extra", `{"nested":true}`},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated options:\n  got  %v\n  want %v", got, want)
	}
}

// openLegacyV6DatabaseWithV8Schema is the state a start that carried rows but could not drop the v6
// tables they came from leaves behind: the v6 rows are still there, and so is the v8 table they were
// already written into. It is the state in which the next start finds the database refusing an
// identity it already holds.
func openLegacyV6DatabaseWithV8Schema(t *testing.T) *sql.DB {
	t.Helper()

	db := openLegacyV6Database(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	return db
}

// TestMigrateGroupsCarryNothingTwiceForAGroupTheV8TableAlreadyHolds is REQ-004 on the groups table,
// in the state that produces it: the v8 table already holds the identity the v6 row would be written
// under.
func TestMigrateGroupsCarryNothingTwiceForAGroupTheV8TableAlreadyHolds(t *testing.T) {
	db := openLegacyV6DatabaseWithV8Schema(t)

	mustExec(t, db, `INSERT INTO "groups" ("groupId", "label", "order") VALUES (2, 'Already here', 1)`)

	progress := migrateDatabase(t, db)

	if got := archivedRows(t, db, "rdioScannerGroups"); len(got) != 0 {
		t.Errorf("the archive holds %v, want nothing: a group the v8 table already holds has not been lost", got)
	}

	for _, line := range progress {
		if strings.Contains(line, "group 2 ") || strings.Contains(line, "the migration of groups could not be finished") {
			t.Errorf("the pass said something about a group it had already carried: %v", progress)
		}
	}

	got := readRows(t, db, `SELECT "groupId", "label" FROM "groups" WHERE "groupId" = 2`)
	want := [][]string{{"2", "Already here"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the group the v8 table already held:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateTagsCarryNothingTwiceForATagTheV8TableAlreadyHolds is the tags half.
func TestMigrateTagsCarryNothingTwiceForATagTheV8TableAlreadyHolds(t *testing.T) {
	db := openLegacyV6DatabaseWithV8Schema(t)

	mustExec(t, db, `INSERT INTO "tags" ("tagId", "label", "order") VALUES (4, 'Already here', 1)`)

	progress := migrateDatabase(t, db)

	if got := archivedRows(t, db, "rdioScannerTags"); len(got) != 0 {
		t.Errorf("the archive holds %v, want nothing: a tag the v8 table already holds has not been lost", got)
	}

	for _, line := range progress {
		if strings.Contains(line, "tag 4") || strings.Contains(line, "the migration of tags could not be finished") {
			t.Errorf("the pass said something about a tag it had already carried: %v", progress)
		}
	}

	got := readRows(t, db, `SELECT "tagId", "label" FROM "tags" WHERE "tagId" = 4`)
	want := [][]string{{"4", "Already here"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the tag the v8 table already held:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateSystemsCarryNothingTwiceForASystemTheV8TableAlreadyHolds is the systems half, and the
// one whose loss would reach furthest: every talkgroup, unit, and call of that system resolves
// through it.
func TestMigrateSystemsCarryNothingTwiceForASystemTheV8TableAlreadyHolds(t *testing.T) {
	db := openLegacyV6DatabaseWithV8Schema(t)

	mustExec(t, db, `INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "label", "led", "order", "systemRef") VALUES (101, 1, '40000, 40001', 'Already here', 'red', 1, 10001)`)

	progress := migrateDatabase(t, db)

	if got := archivedRows(t, db, "rdioScannerSystems"); len(got) != 0 {
		t.Errorf("the archive holds %v, want nothing: a system the v8 table already holds has not been lost", got)
	}

	for _, line := range progress {
		if strings.Contains(line, "system 101") || strings.Contains(line, "the migration of systems could not be finished") {
			t.Errorf("the pass said something about a system it had already carried: %v", progress)
		}
	}

	got := readRows(t, db, `SELECT "systemId", "label" FROM "systems" WHERE "systemId" = 101`)
	want := [][]string{{"101", "Already here"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the system the v8 table already held:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateApikeysCarryNothingTwiceForAnApikeyTheV8TableAlreadyHolds is the apikeys half.
func TestMigrateApikeysCarryNothingTwiceForAnApikeyTheV8TableAlreadyHolds(t *testing.T) {
	db := openLegacyV6DatabaseWithV8Schema(t)

	mustExec(t, db, `INSERT INTO "apikeys" ("apikeyId", "disabled", "ident", "key", "order", "systems") VALUES (801, 0, 'Already here', 'already-here', 1, '[10001]')`)

	progress := migrateDatabase(t, db)

	if got := archivedRows(t, db, "rdioScannerApiKeys"); len(got) != 0 {
		t.Errorf("the archive holds %v, want nothing: an api key the v8 table already holds has not been lost", got)
	}

	for _, line := range progress {
		if strings.Contains(line, "apikey 801") || strings.Contains(line, "the migration of apikeys could not be finished") {
			t.Errorf("the pass said something about an api key it had already carried: %v", progress)
		}
	}

	got := readRows(t, db, `SELECT "apikeyId", "key" FROM "apikeys" WHERE "apikeyId" = 801`)
	want := [][]string{{"801", "already-here"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the api key the v8 table already held:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateDirwatchesArchivesADirwatchThatHoldsNoDirectory is the dirwatch shape the v6 table can
// actually hold: the directory is the whole point of a dirwatch, and an empty one watches nothing.
func TestMigrateDirwatchesArchivesADirwatchThatHoldsNoDirectory(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `INSERT INTO "rdioScannerDirWatches" ("_id", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "systemId", "talkgroupId", "type", "usePolling") VALUES (604, 0, 0, '', 0, 'wav', 1514000000, '0644', 4, 10001, 1514000, 'dsdPlus', 0)`)

	progress := migrateDatabase(t, db)

	if holdsIdentity(t, db, "dirwatches", "dirwatchId", 604) {
		t.Fatal("the migration carried a dirwatch with no directory")
	}

	assertArchivedRow(t, db, "rdioScannerDirWatches", 604, "it holds no directory", `{"_id":604,"delay":0,"deleteAfter":false,"directory":"","disabled":false,"extension":"wav","frequency":1514000000,"mask":"0644","order":4,"systemId":10001,"talkgroupId":1514000,"type":"dsdPlus"}`)

	if !hasString(progress, `the migration could not carry dirwatch 604: it holds no directory`) {
		t.Errorf("the migration said nothing about the dirwatch it could not carry: %v", progress)
	}
}

// TestMigrateDirwatchesNameTheTableTheV6DatabaseCreated pins the one spelling the dirwatch migration's
// probe, read, archive, and drop use.
//
// No sqlite run can show the three spellings this migration used to disagree on: sqlite matches a table
// name whatever its case, which is exactly why the defect survived — on the backend every development test
// runs on, it has no effect at all. What sqlite can show is the name a v6 database carries for that table,
// the name the pass records in the archive, and the name of the table the pass leaves behind. They are one
// string, and the statements are built from it. The backend where the disagreement was real — a probe that
// fails, and a table neither migrated nor dropped, on that start and every later one — is proved live, in
// the MariaDB run recorded in
// docs/specs/FEAT-011-v663-migration-integrity/validation.md.
func TestMigrateDirwatchesNameTheTableTheV6DatabaseCreated(t *testing.T) {
	db := openLegacyV6Database(t)

	// The name the v6 database carries for its own dirwatch table, read out of its schema: it is the name
	// the statements have to use, because it is the name the deployment's database has.
	got := readRows(t, db, `SELECT "name" FROM "sqlite_master" WHERE "type" = 'table' AND "name" = 'rdioScannerDirWatches'`)

	if len(got) != 1 {
		t.Fatalf("the v6 fixture carries its dirwatch table as %v, want %q", got, v6DirwatchesTable)
	}

	// One dirwatch the pass can read and cannot carry, and one it cannot read at all: the table survives,
	// and the row left in the archive names it.
	mustExec(t, db, `UPDATE "rdioScannerDirWatches" SET "directory" = '' WHERE "_id" = 603`)
	mustExec(t, db, `UPDATE "rdioScannerDirWatches" SET "delay" = 'not a delay' WHERE "_id" = 602`)

	progress := migrateDatabase(t, db)

	archived := archivedRows(t, db, v6DirwatchesTable)

	if len(archived) != 1 || archived[0][1] != "603" {
		t.Fatalf("the dirwatch archive: %v, want the one dirwatch the pass read and could not carry", archived)
	}

	if archived[0][0] != v6DirwatchesTable {
		t.Errorf("the archive names the table %q, want %q: the archive and the read have to name one table",
			archived[0][0], v6DirwatchesTable)
	}

	left := readRows(t, db, `SELECT "name" FROM "sqlite_master" WHERE "type" = 'table' AND lower("name") = 'rdioscannerdirwatches'`)

	if !equalStrings(flat(left), flat([][]string{{v6DirwatchesTable}})) {
		t.Errorf("the dirwatch table the pass left behind is named %v, want %q", left, v6DirwatchesTable)
	}

	if !hasString(progress, "the migration left the dirwatches table where it found it: a row of it could not be read") {
		t.Errorf("the migration said nothing about the dirwatch table it left behind: %v", progress)
	}
}

// is not migrated, and since FEAT-011 the config row that held it is archived with the key named, so
// the value is not simply gone.
func TestMigrateOptionsArchivesAKeyV8HasNoOptionOf(t *testing.T) {
	db, progress := migrateLegacyV6Fixture(t)

	got := readRows(t, db, `SELECT "key", "value" FROM "options" WHERE "key" = 'unknownKey'`)

	if len(got) != 0 {
		t.Errorf("the v6 options blob carried %v into the v7 options, want nothing", got)
	}

	archived := archivedRows(t, db, "rdioScannerConfigs")

	if len(archived) != 1 {
		t.Fatalf("the archive holds %d config rows, want the one options row the pass could not carry whole: %v", len(archived), archived)
	}

	if archived[0][1] != "1" {
		t.Errorf("the archived config row is %s, want the options row, identity 1", archived[0][1])
	}

	want := `it holds values this server cannot carry: "autoDetectGroups" (v8 has no option of that name), "unknownKey" (v8 has no option of that name)`

	if archived[0][2] != want {
		t.Errorf("the archived config row says %q, want %q", archived[0][2], want)
	}

	// The whole blob is archived, not just the key: what the pass could not carry is recoverable from
	// the row it came from.
	if !strings.Contains(archived[0][3], `\"unknownKey\":\"dropped\"`) {
		t.Errorf("the archived config row does not hold the blob it came from: %s", archived[0][3])
	}

	if !hasString(progress, `the migration could not carry the config row "options": `+want) {
		t.Errorf("the migration said nothing about the option it could not carry: %v", progress)
	}
}

// TestMigrateOptionsArchivesAValueWithTheWrongShape is the other half of the blob: a name the unpack
// list knows, holding a value of a type the legacy source does not accept. The other values of the same
// blob are still carried.
func TestMigrateOptionsArchivesAValueWithTheWrongShape(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `UPDATE "rdioScannerConfigs" SET "val" = '{"branding":"Rdio Legacy","pruneDays":"thirty days","unknownKey":"dropped"}' WHERE "key" = 'options'`)

	progress := migrateDatabase(t, db)

	carried := rowsOf(t, db, "options", `"key", "value"`)

	for _, row := range carried {
		if row[0] == "pruneDays" || row[0] == "unknownKey" {
			t.Errorf("the options hold %v, want neither of the values the pass could not carry", row)
		}
	}

	if !holdsColumn(carried, "branding") {
		t.Errorf("the options lost the value it could carry: %v", carried)
	}

	archived := archivedRows(t, db, "rdioScannerConfigs")

	if len(archived) != 1 {
		t.Fatalf("the archive holds %d config rows, want one: %v", len(archived), archived)
	}

	want := `it holds values this server cannot carry: "pruneDays" (the value has the wrong shape), "unknownKey" (v8 has no option of that name)`

	if archived[0][2] != want {
		t.Errorf("the archived config row says %q, want %q", archived[0][2], want)
	}

	if !hasString(progress, `the migration could not carry the config row "options": `+want) {
		t.Errorf("the migration said nothing about the options it could not carry: %v", progress)
	}
}

// TestMigrateOptionsArchivesABlobThatCouldNotBeRead is the whole row lost at once: the options blob is
// not JSON this server can read, so nothing of it can be unpacked. The row is archived as the v6 table
// holds it, and the pass says so.
func TestMigrateOptionsArchivesABlobThatCouldNotBeRead(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `UPDATE "rdioScannerConfigs" SET "val" = 'branding = Rdio Legacy' WHERE "key" = 'options'`)

	progress := migrateDatabase(t, db)

	archived := archivedRows(t, db, "rdioScannerConfigs")

	if len(archived) != 1 {
		t.Fatalf("the archive holds %d config rows, want the options row the pass could not read: %v", len(archived), archived)
	}

	if archived[0][2] != "it could not be read" {
		t.Errorf("the archived config row says %q, want it could not be read", archived[0][2])
	}

	if !strings.Contains(archived[0][3], `branding = Rdio Legacy`) {
		t.Errorf("the archived config row does not hold the text it could not read: %s", archived[0][3])
	}

	if !hasString(progress, `the migration could not carry the config row "options": it could not be read`) {
		t.Errorf("the migration said nothing about the options it could not read: %v", progress)
	}

	// The rows that do not name the options blob are still copied as they stand.
	carried := rowsOf(t, db, "options", `"key", "value"`)

	if !holdsColumn(carried, "secret") || !holdsColumn(carried, "extra") {
		t.Errorf("the options lost the rows the pass could carry: %v", carried)
	}
}

// TestMigrateMetaDropsTheV6BookkeepingTable: the v6 migration list has no v7 counterpart, and the
// meta migration is the one migration whose failure the legacy server returned rather than printed.
func TestMigrateMetaDropsTheV6BookkeepingTable(t *testing.T) {
	db := openLegacyV6Database(t)

	if got := countRows(t, db, "rdioScannerMeta"); got == 0 {
		t.Fatalf("the fixture has no v6 bookkeeping rows to drop")
	}

	migrateDatabase(t, db)

	for _, name := range readTableNames(t, db) {
		if name == "rdioScannerMeta" {
			t.Errorf("the v6 bookkeeping table was left behind")
		}
	}
}

// TestMigrateLogsStoreTheTimestampInMilliseconds: the v6 log timestamp is a datetime and the v7 one
// is Unix milliseconds, which is what every timestamp in this schema is.
func TestMigrateLogsStoreTheTimestampInMilliseconds(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "logs", `"logid", "level", "message", "timestamp"`)
	want := [][]string{
		{"401", "info", "server started", "1749988800000"},
		{"402", "warn", "unit O'Brien keyed", "1749989100000"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated logs:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateDownstreamsCopyTheV6Row: a downstream is copied across with its v6 identity, and only
// a row missing its apikey, its scope, or its url is skipped. Its scope is the JSON the v6
// administrator's system picker wrote, and it is carried word for word because that JSON is the shape
// this server's scope reads (v6.6.3:server/downstream.go:86) — a scope that meant "every talkgroup on
// system 10001" before the upgrade means the same thing after it.
func TestMigrateDownstreamsCopyTheV6Row(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "downstreams", `"downstreamId", "apikey", "disabled", "order", "systems", "url"`)
	want := [][]string{
		{"501", "relay-api-key", "1", "1", `[{"id":10001,"talkgroups":"*"}]`, "http://127.0.0.1:8080/patch"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated downstreams:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateLogsArchivesALogThatHoldsNoMessage is REQ-001 on the deployment's own history: a log row
// with nothing in the message column is a row the log view cannot show, and the legacy pass dropped it
// with the v6 table without a word.
func TestMigrateLogsArchivesALogThatHoldsNoMessage(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `INSERT INTO "rdioScannerLogs" ("_id", "dateTime", "level", "message") VALUES (403, '2025-06-15 13:00:00.000 +0000 UTC', 'info', '')`)

	progress := migrateDatabase(t, db)

	if holdsIdentity(t, db, "logs", "logId", 403) {
		t.Fatal("the migration carried a log entry with no message, which the log view has nothing to show for it")
	}

	assertArchivedRow(t, db, "rdioScannerLogs", 403, "it holds no message", `{"_id":403,"dateTime":"2025-06-15T13:00:00Z","level":"info","message":""}`)

	if !hasString(progress, `the migration could not carry log 403: it holds no message`) {
		t.Errorf("the migration said nothing about the log entry it could not carry: %v", progress)
	}
}

// TestMigrateLogsArchivesALogThatHoldsNoLevel is the same row with its other required column missing:
// the level is what the log view files the entry under, and an entry with none has no place in it.
func TestMigrateLogsArchivesALogThatHoldsNoLevel(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `INSERT INTO "rdioScannerLogs" ("_id", "dateTime", "level", "message") VALUES (404, '2025-06-15 13:05:00.000 +0000 UTC', '', "unit keyed")`)

	progress := migrateDatabase(t, db)

	assertArchivedRow(t, db, "rdioScannerLogs", 404, "it holds no level", `{"_id":404,"dateTime":"2025-06-15T13:05:00Z","level":"","message":"unit keyed"}`)

	if !hasString(progress, `the migration could not carry log 404: it holds no level`) {
		t.Errorf("the migration said nothing about the log entry it could not carry: %v", progress)
	}
}

// TestMigrateDownstreamsArchivesADownstreamThatHoldsNoUrl is REQ-001 on the configuration sections:
// the url is the whole point of a downstream, and a row without one used to leave the database with
// the v6 table.
func TestMigrateDownstreamsArchivesADownstreamThatHoldsNoUrl(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `INSERT INTO "rdioScannerDownstreams" ("_id", "apiKey", "disabled", "order", "systems", "url") VALUES (502, 'relay-api-key-2', 0, 2, '[10002]', '')`)

	progress := migrateDatabase(t, db)

	if holdsIdentity(t, db, "downstreams", "downstreamId", 502) {
		t.Fatal("the migration carried a downstream with no url")
	}

	assertArchivedRow(t, db, "rdioScannerDownstreams", 502, "it holds no url", `{"_id":502,"apiKey":"relay-api-key-2","disabled":false,"order":2,"systems":"[10002]","url":""}`)

	if !hasString(progress, `the migration could not carry downstream 502: it holds no url`) {
		t.Errorf("the migration said nothing about the downstream it could not carry: %v", progress)
	}
}

// TestMigrateAccessesArchivesAnAccessThatHoldsNoCode is the accesses half: the code is what an operator
// types, and an access without one cannot be used or even named. Access 904 of the fixture is that
// access — `code` is `not null unique`, so the fixture holds the one row with no code rather than every
// test adding its own.
func TestMigrateAccessesArchivesAnAccessThatHoldsNoCode(t *testing.T) {
	db := openLegacyV6Database(t)

	progress := migrateDatabase(t, db)

	if holdsIdentity(t, db, "accesses", "accessId", 904) {
		t.Fatal("the migration carried an access with no code")
	}

	assertArchivedRow(t, db, "rdioScannerAccesses", 904, "it holds no code", `{"_id":904,"code":"","expiration":"2025-06-15T13:10:00Z","ident":"no code","limit":0,"order":3,"systems":"[{\"id\":10001,\"talkgroups\":\"*\"}]"}`)

	if !hasString(progress, `the migration could not carry access 904: it holds no code`) {
		t.Errorf("the migration said nothing about the access it could not carry: %v", progress)
	}
}

// TestMigrateDirwatchesResolveSystemAndTalkgroupReferences: a v6 dirwatch names both of its
// references as v6 references, and the lookup the dirwatch migration runs resolves them to v7
// identities.
func TestMigrateDirwatchesResolveSystemAndTalkgroupReferences(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "dirwatches", `"dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "systemId", "talkgroupId", "type"`)
	want := [][]string{
		{"601", "0", "0", "/var/spool/dsdplus/1514000", "0", "wav", "1514000000", "0644", "1", "101", "201", "dsdPlus"},
		{"602", "5", "1", "/var/spool/default/1514002", "1", "mp3", "1514002000", "0755", "2", "102", "203", "default"},
	}

	if len(got) < 2 {
		t.Fatalf("the migrated dirwatches:\n  got  %v\n  want at least the two resolvable dirwatches", got)
	}

	if !equalStrings(flat(got[:2]), flat(want)) {
		t.Errorf("the migrated dirwatches:\n  got  %v\n  want %v", got[:2], want)
	}
}

// TestMigrateDirwatchesStoreZeroForATalkgroupThatDoesNotResolve: an unresolved talkgroup becomes 0,
// which the column accepts, so the dirwatch is kept pointing at nothing and nothing is said.
func TestMigrateDirwatchesStoreZeroForATalkgroupThatDoesNotResolve(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "dirwatches", `"dirwatchId", "systemId", "talkgroupId"`)

	for _, row := range got {
		if row[0] == "603" {
			if row[1] != "101" || row[2] != "0" {
				t.Errorf("the dirwatch naming a talkgroup nothing migrated landed as %v, want system 101 and talkgroup 0", row)
			}

			return
		}
	}

	t.Errorf("the dirwatch naming a talkgroup nothing migrated was not kept: %v", got)
}

// TestMigrateDirwatchesResolveNothingWhenNoTalkgroupWasMigrated is the stale-variable defect, and
// the reason it is worth pinning: the dirwatch rows are scanned into two of the four variables the
// lookup query used, so whether a dirwatch's talkgroup resolves at all depends on a variable the
// dirwatch rows never scan into. When the lookup finds no talkgroup, that variable is still NULL
// from the lookup, the reference is handed on as a NULL, and the row is rejected and lost.
//
// See the legacy `server/migrations.go`, migrateDirwatches: `talkgroupId` is scanned only by the lookup query,
// and the insert is built from it.
func TestMigrateDirwatchesResolveNothingWhenNoTalkgroupWasMigrated(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `DELETE FROM "rdioScannerTalkgroups"`)

	progress := migrateDatabase(t, db)

	if got := countRows(t, db, "dirwatches"); got != 0 {
		t.Errorf("with no talkgroup migrated, %d dirwatches were kept, want 0", got)
	}

	found := 0

	for _, line := range progress {
		if strings.Contains(line, "migration.migrateDirwatches:") {
			found++
		}
	}

	if found != 3 {
		t.Errorf("the lost dirwatches were reported %d times, want one per dirwatch; progress: %v", found, progress)
	}
}

// TestMigrateCallsStoreTheCallWithItsFrequenciesPatchesAndUnits: a call that resolves is stored
// with its v6 identity, its audio, its site reference of 0, and its frequencies, patches, and source
// units unpacked out of the JSON the v6 row holds.
func TestMigrateCallsStoreTheCallWithItsFrequenciesPatchesAndUnits(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "calls", `"callId", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp"`)
	want := [][]string{
		{"701", "701.wav", "audio/wav", "0", "101", "201", "1749988800000"},
		{"702", "702.mp3", "audio/mp3", "0", "101", "202", "1749989400000"},
		{"703", "703.wav", "audio/wav", "0", "102", "203", "1749990000000"},
		{"704", "704.wav", "audio/wav", "0", "101", "203", "1749990600000"},
		{"706", "706.wav", "audio/wav", "0", "101", "201", "1749991800000"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated calls:\n  got  %v\n  want %v", got, want)
	}

	got = rowsOf(t, db, "callFrequencies", `"callId", "errors", "frequency", "offset", "spikes"`)
	want = [][]string{
		{"701", "0", "1514000000", "0", "0"},
		{"703", "0", "1514002000", "0", "0"},
		{"704", "3", "1514002000", "1.25", "4"},
		{"706", "0", "1514000000", "0", "0"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated call frequencies:\n  got  %v\n  want %v", got, want)
	}

	got = rowsOf(t, db, "callPatches", `"callId", "talkgroupId"`)
	want = [][]string{{"701", "202"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated call patches:\n  got  %v\n  want %v", got, want)
	}

	got = rowsOf(t, db, "callUnits", `"callId", "offset", "unitRef"`)
	want = [][]string{{"701", "1.5", "12345"}, {"702", "2.5", "99999"}, {"706", "0", "6789"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated call units:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateCallsKeepTheAudioBytesTheV6RowHeld: the v6 audio blob is stored as it was, which is
// the only copy of a call's audio the v7 schema keeps.
func TestMigrateCallsKeepTheAudioBytesTheV6RowHeld(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := readRows(t, db, `SELECT "callId", "audio" FROM "calls" ORDER BY 1`)

	if len(got) != 5 {
		t.Fatalf("the migrated calls: %d rows, want 5", len(got))
	}

	if got[0][1] != "\x00\x01\x02audio for call 701" {
		t.Errorf("the migrated audio of call 701 is %q", got[0][1])
	}
}

// TestMigrateCallsArchivesACallWhoseSystemDoesNotResolve: a call whose references do not resolve is
// not carried, and since FEAT-011 it is not lost either — the row is archived with its audio, and the
// reason is said.
func TestMigrateCallsArchivesACallWhoseSystemDoesNotResolve(t *testing.T) {
	db, progress := migrateLegacyV6Fixture(t)

	if holdsIdentity(t, db, "calls", "callId", 705) {
		t.Fatal("the call naming a system nothing migrated was migrated")
	}

	assertArchivedRow(t, db, "rdioScannerCalls", 705, "its system 10003 is not in this database", `{"audio":"AAECYXVkaW8gZm9yIGNhbGwgNzA1","audioName":"705.wav","audioType":"audio/wav","dateTime":"2025-06-15T12:40:00Z","frequencies":"[]","frequency":1514003000,"id":705,"patches":"[]","source":0,"sources":"[]","system":10003,"talkgroup":1514003}`)

	if !hasString(progress, "the migration could not carry call 705: its system 10003 is not in this database") {
		t.Errorf("the migration said nothing about the call it could not carry: %v", progress)
	}
}

// TestMigrateCallsArchivesACallThatHoldsNoAudio is the shape the v6 table can actually hold: the audio
// column is `not null`, but a zero-length upload leaves it empty, and a call without audio is a call
// this server has nothing to play. Its frequencies, patches, and source units are archived with it,
// which is what makes the row worth reading afterwards.
func TestMigrateCallsArchivesACallThatHoldsNoAudio(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `INSERT INTO "rdioScannerCalls" ("id", "audio", "audioName", "audioType", "dateTime", "frequencies", "frequency", "patches", "source", "sources", "system", "talkgroup") VALUES (707, x'', '707.wav', 'audio/wav', '2025-06-15 12:55:00.000 +0000 UTC', '[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]', 1514000000, '[1514001]', 12345, '[{"pos":1.5,"src":12345}]', 10001, 1514000)`)

	progress := migrateDatabase(t, db)

	if holdsIdentity(t, db, "calls", "callId", 707) {
		t.Fatal("the migration carried a call with no audio, which the v8 table stores as a blob the player cannot play")
	}

	assertArchivedRow(t, db, "rdioScannerCalls", 707, "it holds no audio", `{"audio":"","audioName":"707.wav","audioType":"audio/wav","dateTime":"2025-06-15T12:55:00Z","frequencies":"[{\"errorCount\":0,\"freq\":1514000000,\"pos\":0,\"spikeCount\":0}]","frequency":1514000000,"id":707,"patches":"[1514001]","source":12345,"sources":"[{\"pos\":1.5,\"src\":12345}]","system":10001,"talkgroup":1514000}`)

	if !hasString(progress, "the migration could not carry call 707: it holds no audio") {
		t.Errorf("the migration said nothing about the call it could not carry: %v", progress)
	}
}

// TestMigrateCallsArchivesACallWhoseTalkgroupIsNotInThisDatabase is the other reference: the call
// names a talkgroup no migrated talkgroup holds, so the v8 row would name a talkgroup that does not
// exist.
func TestMigrateCallsArchivesACallWhoseTalkgroupIsNotInThisDatabase(t *testing.T) {
	db := openLegacyV6Database(t)

	mustExec(t, db, `INSERT INTO "rdioScannerCalls" ("id", "audio", "audioName", "audioType", "dateTime", "frequencies", "frequency", "patches", "source", "sources", "system", "talkgroup") VALUES (708, x'000102617564696f20666f722063616c6c20373038', '708.wav', 'audio/wav', '2025-06-15 12:56:00.000 +0000 UTC', '[]', 1514999000, '[]', 0, '[]', 10001, 1514999)`)

	progress := migrateDatabase(t, db)

	if holdsIdentity(t, db, "calls", "callId", 708) {
		t.Fatal("the migration carried a call whose talkgroup is not in this database")
	}

	assertArchivedRow(t, db, "rdioScannerCalls", 708, "its talkgroup 1514999 is not in this database", `{"audio":"AAECYXVkaW8gZm9yIGNhbGwgNzA4","audioName":"708.wav","audioType":"audio/wav","dateTime":"2025-06-15T12:56:00Z","frequencies":"[]","frequency":1514999000,"id":708,"patches":"[]","source":0,"sources":"[]","system":10001,"talkgroup":1514999}`)

	if !hasString(progress, "the migration could not carry call 708: its talkgroup 1514999 is not in this database") {
		t.Errorf("the migration said nothing about the call it could not carry: %v", progress)
	}
}

// TestMigrateCallsCarryNothingTwiceForACallTheV8TableAlreadyHolds is the calls half of the same
// state, and the one where writing a row twice would also have written its frequencies and its source
// units twice.
func TestMigrateCallsCarryNothingTwiceForACallTheV8TableAlreadyHolds(t *testing.T) {
	db := openLegacyV6Database(t)

	migrateDatabase(t, db)

	mustExec(t, db, `CREATE TABLE "rdioScannerCalls" ("id" integer primary key autoincrement, "audio" longblob not null, "audioName" varchar(255), "audioType" varchar(255), "dateTime" datetime not null, "frequencies" text not null, "frequency" integer, "patches" text not null, "source" integer, "sources" text not null, "system" integer not null, "talkgroup" integer not null)`)
	mustExec(t, db, `INSERT INTO "rdioScannerCalls" ("id", "audio", "audioName", "audioType", "dateTime", "frequencies", "frequency", "patches", "source", "sources", "system", "talkgroup") VALUES (701, x'000102617564696f20666f722063616c6c20373031', '701.wav', 'audio/wav', '2025-06-15 12:00:00.000 +0000 UTC', '[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]', 1514000000, '[1514001]', 12345, '[{"pos":1.5,"src":12345}]', 10001, 1514000)`)

	progress := migrateDatabase(t, db)

	if got := archivedRow(t, db, "rdioScannerCalls", 701); got != nil {
		t.Errorf("the archive holds %v, want nothing: a call the v8 table already holds has not been lost", got)
	}

	if got := archivedRows(t, db, "rdioScannerCalls"); len(got) != 2 {
		t.Fatalf("the archive after a second pass over a call the v8 table already holds: %v, want the two calls the first pass archived, once each", got)
	}

	for _, line := range progress {
		if strings.Contains(line, "call 701") || strings.Contains(line, "the migration of calls could not be finished") {
			t.Errorf("the second pass said something about a call it had already carried: %v", progress)
		}
	}

	if count := countRows(t, db, "callFrequencies"); count != 4 {
		t.Errorf("the second pass wrote the call frequencies again: %d rows, want the four the first pass wrote", count)
	}
}

// TestMigrateCallsResolvesATalkgroupOfAnotherSystemBecauseTheLookupJoinHasNoOnClause is a legacy
// defect: the lookup that fills the reference maps is a LEFT JOIN with no ON clause, which every
// backend this server supports reads as a cross product, so every talkgroup is known to every
// system.
//
// See the legacy `server/migrations.go`, migrateCalls and migrateDirwatches: the lookup query has no ON clause.
func TestMigrateCallsResolvesATalkgroupOfAnotherSystemBecauseTheLookupJoinHasNoOnClause(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "calls", `"callId", "systemId", "talkgroupId"`)

	for _, row := range got {
		if row[0] == "704" {
			if row[1] != "101" || row[2] != "203" {
				t.Errorf("the call on system 10001 naming a talkgroup of system 10002 landed as %v, want system 101 and talkgroup 203", row)
			}

			return
		}
	}

	t.Errorf("the call on system 10001 naming a talkgroup of system 10002 was not migrated: %v", got)
}

// TestMigrateCallsStoreNoFrequencyWhenTheListIsTheEmptyArray is a legacy defect: the frequency list
// is tested for a non-empty string rather than for a non-empty list, so a call whose list is the
// empty array takes the list branch and stores no frequency at all, even though its single
// frequency column names one. The source list is tested for the empty array as well, which is why
// the same call still gets its source unit.
//
// See the legacy `server/migrations.go`, migrateCalls: `frequencies.String` is only tested for length, while
// `sources.String` is also tested against `[]`.
func TestMigrateCallsStoreNoFrequencyWhenTheListIsTheEmptyArray(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	for _, row := range rowsOf(t, db, "callFrequencies", `"callId", "frequency"`) {
		if row[0] == "702" {
			t.Errorf("the call whose frequency list is the empty array stored a frequency anyway: %v", row)
		}
	}

	for _, row := range rowsOf(t, db, "callUnits", `"callId", "unitRef"`) {
		if row[0] == "702" && row[1] == "99999" {
			return
		}
	}

	t.Errorf("the call whose source list is the empty array did not fall through to its single source")
}

// TestMigrateCallsStoreTheSingleFrequencyWhenTheListIsAnEmptyString is the branch the defect above
// never reaches: an empty string, as opposed to the empty array, does fall through to the single
// frequency column.
func TestMigrateCallsStoreTheSingleFrequencyWhenTheListIsAnEmptyString(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "callFrequencies", `"callId", "errors", "frequency", "offset", "spikes"`)

	for _, row := range got {
		if row[0] == "703" {
			want := []string{"703", "0", "1514002000", "0", "0"}
			if !equalStrings(row, want) {
				t.Errorf("the call whose frequency list is an empty string landed as %v, want %v", row, want)
			}

			return
		}
	}

	t.Errorf("the call whose frequency list is an empty string stored no frequency: %v", got)
}

// TestMigrateCallsUseTheUnitTheUnitMigrationAlreadyWrote is the repaired half of Q23.
//
// The call migration asks whether the unit a call came from is already in the units table, by the number
// the call names and the system the call belongs to. With the unit reference kept, the unit the unit
// migration wrote is found, no second unit is created for it, and the call gets its `callUnits` row —
// which the legacy migration wrote only on the branch where the unit insert had failed, leaving every
// call whose unit had to be created without its source. See
// docs/specs/FEAT-004-legacy-defect-repairs/decision-migration-unit-references.md.
func TestMigrateCallsUseTheUnitTheUnitMigrationAlreadyWrote(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	for _, row := range rowsOf(t, db, "units", `"unitId", "label", "systemId", "unitRef"`) {
		if row[1] == "6789" {
			t.Errorf("the call migration created a unit labelled with the number %v, want it to use the unit "+
				"the unit migration already wrote: %v", row[1], row)
		}
	}

	got := readRows(t, db, `SELECT "callId", "offset", "unitRef" FROM "callUnits" WHERE "callId" = 706`)
	want := [][]string{{"706", "0", "6789"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("call 706's source units:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateCallsReportNoFailureForWorkThatSucceeded is the repaired half of Q24.
//
// The legacy migration reported the unit insert from the branch that only runs when the insert succeeded,
// with the error it had just tested for being nil, and its error formatter called `Error()` on that nil and
// took the whole server down. A migration that had nothing to complain about now says nothing.
func TestMigrateCallsReportNoFailureForWorkThatSucceeded(t *testing.T) {
	_, progress := migrateLegacyV6Fixture(t)

	for _, line := range progress {
		if strings.HasPrefix(line, "migration.migrateCalls:") {
			t.Errorf("the calls migration reported a failure it did not have: %q", line)
		}
	}
}

// TestMigrateCallsCreateOneUnitPerUnitNumberTheyHaveNeverSeen keeps the unit the call migration creates
// a unit: one row per number per system, found by the next call that names the same number instead of
// created again, and created for the system the call belongs to rather than wherever a matching number
// happens to live.
func TestMigrateCallsCreateOneUnitPerUnitNumberTheyHaveNeverSeen(t *testing.T) {
	db := openLegacyV6Database(t)

	const insert = `INSERT INTO "rdioScannerCalls" ("id", "audio", "audioName", "audioType", "dateTime", "frequencies", "frequency", "patches", "source", "sources", "system", "talkgroup") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	// Two calls on Grand Metro naming a unit number the migration has never seen, and one call on Harbour
	// naming a number Grand Metro already has a unit for.
	for _, call := range []struct {
		id        int
		system    int
		talkgroup int
		source    int
	}{
		{707, 10001, 1514000, 55555},
		{708, 10001, 1514000, 55555},
		{709, 10002, 1514002, 12345},
	} {
		const audio = "audio for a call the fixture did not hold"

		_, err := db.Exec(insert, call.id, []byte(audio), fmt.Sprintf("%d.wav", call.id), "audio/wav",
			"2025-06-15 13:00:00.000 +0000 UTC", "[]", 1514000000, "[]", call.source, "[]",
			call.system, call.talkgroup)

		if err != nil {
			t.Fatalf("%s: %v", insert, err)
		}
	}

	migrateDatabase(t, db)

	created := 0

	for _, row := range rowsOf(t, db, "units", `"unitId", "label", "systemId", "unitRef"`) {
		if row[3] == "55555" {
			created++

			if row[2] != "101" {
				t.Errorf("the unit created for the number 55555 belongs to system %s, want the system the calls "+
					"that named it belong to: %v", row[2], row)
			}

			if row[1] != "55555" {
				t.Errorf("the unit created for the number 55555 is labelled %q, want the number as the text the "+
					"legacy statement put in that column: %v", row[1], row)
			}
		}
	}

	if created != 1 {
		t.Errorf("the number 55555, named by two calls of one system, produced %d units, want one; the calls "+
			"that name a unit number the migration has never seen create it once and share it", created)
	}

	got := readRows(t, db, `SELECT "callId", "unitRef" FROM "callUnits" WHERE "unitRef" = 55555 ORDER BY "callId"`)
	want := [][]string{{"707", "55555"}, {"708", "55555"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the calls naming the new unit:\n  got  %v\n  want %v", got, want)
	}

	// The number 12345 is already a unit of Grand Metro. A Harbour call naming it belongs to Harbour, and
	// gets a unit of Harbour; the Grand Metro unit is left where it is.
	harbour := readRows(t, db, `SELECT "label", "systemId", "unitRef" FROM "units" WHERE "unitRef" = 12345 AND "systemId" = 102`)

	if len(harbour) != 1 {
		t.Errorf("the Harbour call naming unit 12345 left %v, want one unit of Harbour: %v", harbour,
			readRows(t, db, `SELECT "unitId", "label", "systemId", "unitRef" FROM "units" WHERE "unitRef" = 12345`))
	}

	grandMetro := readRows(t, db, `SELECT "unitId", "label", "systemId", "unitRef" FROM "units" WHERE "unitId" = 301`)

	if len(grandMetro) != 1 || grandMetro[0][2] != "101" {
		t.Errorf("the Grand Metro unit whose number a Harbour call also named came back as %v, want it left alone", grandMetro)
	}
}

// TestMigrateApikeysCopyTheV6Rows: only a missing identity skips an apikey. One with no ident, no
// scope, or no key is still stored, with the empty value the legacy migration stored. The two scopes
// here are the two shapes a v6 deployment holds — a list of systems with their talkgroups, and the
// `*` the administrator's form starts a new access or apikey with — carried across unchanged.
func TestMigrateApikeysCopyTheV6Rows(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "apikeys", `"apikeyId", "disabled", "ident", "key", "order", "systems"`)
	want := [][]string{
		{"801", "0", "ops key", "ops-api-key", "1", `[{"id":10001,"talkgroups":"*"}]`},
		{"802", "1", "test key", "test-api-key", "2", `"*"`},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated apikeys:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateAccessesStoreTheExpirationInSeconds: an access expiration is the one timestamp in this
// schema that is not Unix milliseconds, an access with no expiration is stored as 0, and the scope is
// the v6 JSON, carried word for word.
func TestMigrateAccessesStoreTheExpirationInSeconds(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "accesses", `"accessId", "code", "expiration", "ident", "limit", "order", "systems"`)
	want := [][]string{
		{"901", "ABC123", "1749992400", "ops", "10", "1", `[{"id":10001,"talkgroups":[1514000,1514001]}]`},
		{"902", "XYZ789", "0", "guest", "0", "2", `"*"`},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated accesses:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateKeepsGoingAfterAPerRowFailure is the reason a per-row failure is not an error here: a
// deployment's upgrade either finishes or does not, and a row the database rejected in one table
// has never stopped the tables after it.
func TestMigrateKeepsGoingAfterAPerRowFailure(t *testing.T) {
	db, progress := migrateLegacyV6Fixture(t)

	if countRows(t, db, "talkgroupGroups") != 2 {
		t.Fatalf("the fixture no longer has a row the database rejects")
	}

	if got := countRows(t, db, "accesses"); got != 2 {
		t.Errorf("the migrations after the failure ran for %d accesses, want 2; progress: %v", got, progress)
	}

	if got := countRows(t, db, "calls"); got != 5 {
		t.Errorf("the migrations after the failure ran for %d calls, want 5", got)
	}
}

// TestMigrateAcceptsANilProgressCallback keeps the pass usable from a composition root that has
// nowhere to send the lines.
func TestMigrateAcceptsANilProgressCallback(t *testing.T) {
	db := openLegacyV6Database(t)

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if got := countRows(t, db, "groups"); got != 6 {
		t.Errorf("migrating with a nil callback stored %d groups, want 6", got)
	}
}

// TestMigrateRecoversTheCallsASurvivingV6TableStillHolds is the recoverable half of REQ-010: the
// legacy server committed the units migration with its damage, crashed in the calls migration, and
// rolled the call set back, leaving the v6 calls table in place. The repaired pass finishes the job
// the legacy server could not — the calls come back with their unit links, and the v6 table is
// dropped the way the legacy pass drops it.
func TestMigrateRecoversTheCallsASurvivingV6TableStillHolds(t *testing.T) {
	db := openDamagedFixture(t, "crashed")

	progress := migrateDatabase(t, db)

	if got := announcements(progress); !equalStrings(got, []string{"migrating calls..."}) {
		t.Errorf("the pass found %v, want the calls table and nothing else: the other v6 tables were "+
			"migrated and dropped before the crash", got)
	}

	got := readRows(t, db, `SELECT "callId", "systemId", "talkgroupId" FROM "calls" ORDER BY "callId"`)
	want := [][]string{{"701", "101", "201"}, {"702", "101", "202"}, {"706", "101", "201"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the recovered calls:\n  got  %v\n  want %v", got, want)
	}

	got = readRows(t, db, `SELECT "callId", "offset", "unitRef" FROM "callUnits" ORDER BY "callId"`)
	want = [][]string{{"701", "1.5", "12345"}, {"702", "2.5", "99999"}, {"706", "0", "6789"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the recovered source units:\n  got  %v\n  want %v", got, want)
	}

	// Call 706 names a number no unit holds any more — the unit that held it is unit 302, left with
	// reference 0 — so the call migration makes a unit for it, as it does for any number it has never
	// seen. The reference unit 302 lost is not recovered, and the startup report names it.
	if got := readRows(t, db, `SELECT "label", "systemId", "unitRef" FROM "units" WHERE "unitRef" = 6789`); len(got) != 1 {
		t.Errorf("the unit for the number call 706 names:\n  got  %v\n  want one unit of Grand Metro at 6789", got)
	}

	if _, err := db.Exec(`SELECT COUNT(*) FROM "rdioScannerCalls"`); err == nil {
		t.Errorf("the v6 calls table is still there after the calls it holds were migrated, want it dropped " +
			"the way the legacy pass drops it")
	}
}

// TestMigrateReportsTheDamageItCannotRepair is the unrecoverable half of REQ-010: a database the
// legacy server finished migrating holds units whose radio reference was thrown away, and the numbers
// they were migrated from are nowhere in it. The startup report names each one, and invents nothing.
func TestMigrateReportsTheDamageItCannotRepair(t *testing.T) {
	db := openDamagedFixture(t, "migrated")

	before := tableSnapshot(t, db)

	got := damageReport(migrateDatabase(t, db))
	want := []string{
		"the migration left 2 units damaged: they hold no radio reference, the numbers they were migrated from are not in this database",
		`the migration left unit 301 "Engine 12" of Grand Metro (10001) damaged`,
		`the migration left unit 302 "Car 4" of Grand Metro (10001) damaged`,
		"the migration left 1 unit damaged: it is labelled with its own number, the shape of a unit the migration created rather than a deployment named",
		`the migration left unit 303 "6789" of Grand Metro (10001) damaged`,
	}

	if !equalStrings(got, want) {
		t.Errorf("the startup report:\n  got  %v\n  want %v", got, want)
	}

	for _, row := range rowsOf(t, db, "units", `"unitId", "label", "unitRef"`) {
		if row[0] != "301" && row[0] != "302" {
			continue
		}

		if row[2] != "0" {
			t.Errorf("the report left unit %s %q holding reference %s, want it left with none: the number it "+
				"was migrated from is not in this database and none is invented", row[0], row[1], row[2])
		}
	}

	if after := tableSnapshot(t, db); after != before {
		t.Errorf("the startup report changed the database:\n  got  %s\n  want %s", after, before)
	}
}

// TestMigrateReportsTheSameDamageAtEveryStart is the idempotence rule: the report is a reading, not a
// pass that leaves the database in a state the next start reads differently, so a second start on the
// same damaged database says the same thing about the same rows.
func TestMigrateReportsTheSameDamageAtEveryStart(t *testing.T) {
	db := openDamagedFixture(t, "migrated")

	first := damageReport(migrateDatabase(t, db))
	before := tableSnapshot(t, db)

	second := damageReport(migrateDatabase(t, db))

	if !equalStrings(second, first) {
		t.Errorf("the second start reported:\n  got  %v\n  want %v", second, first)
	}

	if after := tableSnapshot(t, db); after != before {
		t.Errorf("the second start changed the database:\n  got  %s\n  want %s", after, before)
	}
}

// TestMigrateDeletesNoRowItIsAskedToRepair is CA-010.3: the repair recovers rows and reports rows, and
// removes nothing. The v6 tables it drops are the ones whose rows it has just migrated, which is what
// the legacy pass does too.
func TestMigrateDeletesNoRowItIsAskedToRepair(t *testing.T) {
	for _, fixture := range []string{"migrated", "crashed"} {
		db := openDamagedFixture(t, fixture)

		before := map[string]int{}

		for _, table := range []string{"units", "calls", "callUnits"} {
			before[table] = countRows(t, db, table)
		}

		migrateDatabase(t, db)

		for _, table := range []string{"units", "calls", "callUnits"} {
			if got := countRows(t, db, table); got < before[table] {
				t.Errorf("%s holds %d rows after the startup pass, want at least the %d it held before: %s",
					fixture, got, before[table], table)
			}
		}
	}
}

// TestMigrateReportsNothingForADatabaseWithNoDamage keeps the report off a database that has nothing
// to report, which is every database that has never been through the legacy migration pass.
func TestMigrateReportsNothingForADatabaseWithNoDamage(t *testing.T) {
	db := openLegacyV6Database(t)

	if got := damageReport(migrateDatabase(t, db)); len(got) != 0 {
		t.Errorf("the startup report on a database whose migration this server ran itself: %v, want nothing said", got)
	}
}

func flat(rows [][]string) []string {
	out := []string{}

	for _, row := range rows {
		out = append(out, strings.Join(row, "|"))
	}

	return out
}

// holdsColumn reports whether any of the rows names that value in any column, which is how a test
// asks whether a table holds a key without pinning the whole table.
func holdsColumn(rows [][]string, value string) bool {
	for _, row := range rows {
		for _, column := range row {
			if column == value {
				return true
			}
		}
	}

	return false
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}

// auditTable names one v6 table, the column that identifies its rows, and the v8 table and column those
// identities arrive in.
type auditTable struct {
	v6, v6Identity, v8, v8Identity string
}

// auditTables is every v6 table whose rows are deployment data. `rdioScannerConfigs` is not in it,
// because its rows do not arrive one for one — the options row unpacks into up to fourteen — and it is
// audited on its own. `rdioScannerMeta` is not in it either: it is v6 bookkeeping with no v8 counterpart,
// and `TestTheMigrationDropsWhatTheV8SchemaHasNoRoomFor` is where that declared exception is proved.
var auditTables = []auditTable{
	{v6: "rdioScannerGroups", v6Identity: "_id", v8: "groups", v8Identity: "groupId"},
	{v6: "rdioScannerTags", v6Identity: "_id", v8: "tags", v8Identity: "tagId"},
	{v6: "rdioScannerSystems", v6Identity: "_id", v8: "systems", v8Identity: "systemId"},
	{v6: "rdioScannerTalkgroups", v6Identity: "_id", v8: "talkgroups", v8Identity: "talkgroupId"},
	{v6: "rdioScannerUnits", v6Identity: "_id", v8: "units", v8Identity: "unitId"},
	{v6: "rdioScannerLogs", v6Identity: "_id", v8: "logs", v8Identity: "logId"},
	{v6: "rdioScannerDownstreams", v6Identity: "_id", v8: "downstreams", v8Identity: "downstreamId"},
	{v6: "rdioScannerDirWatches", v6Identity: "_id", v8: "dirwatches", v8Identity: "dirwatchId"},
	{v6: "rdioScannerCalls", v6Identity: "id", v8: "calls", v8Identity: "callId"},
	{v6: "rdioScannerApiKeys", v6Identity: "_id", v8: "apikeys", v8Identity: "apikeyId"},
	{v6: "rdioScannerAccesses", v6Identity: "_id", v8: "accesses", v8Identity: "accessId"},
}

// auditShadow copies every v6 table before the pass runs. The pass drops the v6 tables it reads, and the
// audit has to compare the deployment's database with what the same database holds afterwards, so it
// keeps its own copy of the rows the pass is about to remove. Only this test's database ever sees them.
func auditShadow(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, table := range auditTables {
		mustExec(t, db, `CREATE TABLE "audit`+table.v6+`" AS SELECT * FROM "`+table.v6+`"`)
	}

	mustExec(t, db, `CREATE TABLE "auditrdioScannerConfigs" AS SELECT * FROM "rdioScannerConfigs"`)
	mustExec(t, db, `CREATE TABLE "auditrdioScannerMeta" AS SELECT * FROM "rdioScannerMeta"`)
}

// auditIdentities reads the identities one table holds, in identity order.
func auditIdentities(t *testing.T, db *sql.DB, table, column string) []string {
	t.Helper()

	return auditIdentitiesWhere(t, db, table, column, "")
}

// auditIdentitiesWhere reads the identities one table holds under a condition, in identity order.
//
// Every one of them has to be a number. sqlite reads a quoted name that matches no column as the word
// itself, so a column name this audit got wrong would come back as the same word on both sides of every
// comparison and read as a pass. A value that is not an identity is that mistake, and it is fatal here
// rather than a mismatch further down.
func auditIdentitiesWhere(t *testing.T, db *sql.DB, table, column, where string) []string {
	t.Helper()

	identities := []string{}

	for _, value := range auditColumnValues(t, db, table, column, where) {
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			t.Fatalf("the audit read %s.%s and got %q, which is not an identity: sqlite reads a quoted name that "+
				"matches no column as the word itself, so this is the audit naming a column the table does not have",
				table, column, value)
		}

		identities = append(identities, value)
	}

	return identities
}

// auditColumnValues reads one column of one table, in column order. The options keys are compared by name
// and not by number, so they are read here and not through the identity rule above.
func auditColumnValues(t *testing.T, db *sql.DB, table, column, where string) []string {
	t.Helper()

	query := `SELECT "` + column + `" FROM "` + table + `"`

	if where != "" {
		query += " WHERE " + where
	}

	values := []string{}

	for _, row := range readRows(t, db, query+` ORDER BY "`+column+`"`) {
		values = append(values, row[0])
	}

	return values
}

// TestTheMigrationCarriesOrArchivesEveryRowOfTheLegacyDatabase is the conservation invariant: for every
// row of every v6 table that holds deployment data, the pass either carried it into the v8 table its rule
// names or archived it with a reason. A row in neither is the defect this feature exists to end. A row in
// both is a pass that cannot say what it did.
func TestTheMigrationCarriesOrArchivesEveryRowOfTheLegacyDatabase(t *testing.T) {
	db := openLegacyV6Database(t)

	auditShadow(t, db)

	migrateDatabase(t, db)

	auditConservation(t, db)
}

// auditConservation is the conservation invariant on a database that already holds its shadow copies and
// has already been migrated. The sqlite test and the live-backend tests are one audit run on three
// backends, which is what keeps a backend from being proven by a different set of rules than the others.
func auditConservation(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, table := range auditTables {
		read := auditIdentities(t, db, "audit"+table.v6, table.v6Identity)
		carried := auditIdentities(t, db, table.v8, table.v8Identity)
		archived := auditIdentitiesWhere(t, db, "migrationArchive", "rowId", `"tableName" = '`+table.v6+`'`)

		if len(read) == 0 {
			t.Fatalf("the fixture has no %s rows, so this table proves nothing", table.v6)
		}

		for _, identity := range read {
			inV8, inArchive := hasString(carried, identity), hasString(archived, identity)

			switch {
			case inV8 && inArchive:
				t.Errorf("%s row %s is in the v8 %s table and in the migrationArchive table: the pass counted the same row twice, "+
					"so it cannot say which one an operator should trust", table.v6, identity, table.v8)

			case !inV8 && !inArchive:
				t.Errorf("%s row %s is in neither the v8 %s table nor the migrationArchive table: the migration lost it",
					table.v6, identity, table.v8)
			}
		}

		// The archive is a record of this deployment's rows, not a place where rows appear.
		for _, identity := range archived {
			if !hasString(read, identity) {
				t.Errorf("the migrationArchive table holds %s row %s, which the v6 database does not have", table.v6, identity)
			}
		}
	}
}

// TestTheMigrationCarriesOrArchivesEveryConfigRowOfTheLegacyDatabase is the conservation invariant on the
// one v6 table whose rows do not arrive one for one.
//
// A config row that is not the options row arrives as one options row of the same key. The options row
// unpacks into up to fourteen options rows, so it is carried and archived at the same time when some of
// its keys have no v8 counterpart: the archive holds the v6 row whole, which is what makes the keys that
// were dropped recoverable. The audit therefore counts conservation per key, which is what the v8 table
// actually holds.
func TestTheMigrationCarriesOrArchivesEveryConfigRowOfTheLegacyDatabase(t *testing.T) {
	db := openLegacyV6Database(t)

	auditShadow(t, db)

	migrateDatabase(t, db)

	auditConfigConservation(t, db)
}

// auditConfigConservation is the same invariant on the configs table, which is the one table whose rows do
// not arrive one for one.
func auditConfigConservation(t *testing.T, db *sql.DB) {
	t.Helper()

	archived := auditIdentitiesWhere(t, db, "migrationArchive", "rowId", `"tableName" = 'rdioScannerConfigs'`)

	for _, row := range readRows(t, db, `SELECT "_id", "key" FROM "auditrdioScannerConfigs" ORDER BY "_id"`) {
		rowId, key := row[0], row[1]

		if key != "options" {
			if carried := auditColumnValues(t, db, "options", "key", `"key" = '`+key+`'`); !hasString(carried, key) {
				if !hasString(archived, rowId) {
					t.Errorf("rdioScannerConfigs row %s, key %q: the migration carried it into neither the options table nor the archive",
						rowId, key)
				}
			}

			continue
		}

		// Every key the v6 blob holds is either an options row or named in the archived blob.
		var blob map[string]any

		if err := json.Unmarshal([]byte(readRows(t, db, `SELECT "val" FROM "auditrdioScannerConfigs" WHERE "key" = 'options'`)[0][0]), &blob); err != nil {
			t.Fatalf("the fixture's options blob: %v", err)
		}

		carried := auditColumnValues(t, db, "options", "key", "")

		// The archived copy of the options row holds the blob as the v6 table held it, which is what makes
		// a key the pass could not carry recoverable out of the archive.
		archived := []map[string]any{}

		for _, row := range readRows(t, db, `SELECT "contents" FROM "migrationArchive" WHERE "tableName" = 'rdioScannerConfigs'`) {
			var contents struct {
				Val string `json:"val"`
			}

			if err := json.Unmarshal([]byte(row[0]), &contents); err != nil {
				t.Fatalf("an archived config row holds contents that are not the row as v6 wrote it: %v", err)
			}

			var unpacked map[string]any

			if err := json.Unmarshal([]byte(contents.Val), &unpacked); err != nil {
				continue
			}

			archived = append(archived, unpacked)
		}

		for key := range blob {
			if hasString(carried, key) {
				continue
			}

			found := false

			for _, unpacked := range archived {
				if _, ok := unpacked[key]; ok {
					found = true

					break
				}
			}

			if !found {
				t.Errorf("the options blob's key %q is in neither the options table nor an archived config row: the migration lost it",
					key)
			}
		}
	}
}

// pair writes one column of a value comparison: the name the v6 table gives it and the name the v8 table
// gives it. The queries below read the two side by side so that a failure can name both.
func pair(v6, v8 string) string {
	return `COALESCE(v."` + v6 + `", ''), t."` + v8 + `"`
}

// number is pair for a column the v6 table holds as a number, where the empty value is 0.
func number(v6, v8 string) string {
	return `COALESCE(v."` + v6 + `", 0), COALESCE(t."` + v8 + `", 0)`
}

// flag is a v6 flag column and its v8 counterpart. The fallback is `false` and not 0 because PostgreSQL
// stores these as a boolean and COALESCE will not mix a boolean with an integer; MariaDB and sqlite read
// the same word as the 0 and 1 their columns hold.
func flag(v6, v8 string) string {
	return `COALESCE(v."` + v6 + `", false), COALESCE(t."` + v8 + `", false)`
}

// assertCarriedValues compares each named column's v6 value with the v8 value it arrived in.
//
// The query has to read the row's identity first and then the pairs in the order `columns` names them,
// which is what lets a failure name the table, the row, and the column.
func assertCarriedValues(t *testing.T, db *sql.DB, table string, query string, columns ...string) {
	t.Helper()

	rows := readRows(t, db, query)

	if len(rows) == 0 {
		t.Fatalf("the %s comparison read no rows, so it proves nothing: %s", table, query)
	}

	for _, row := range rows {
		for i, column := range columns {
			if row[2*i+1] != row[2*i+2] {
				t.Errorf("%s row %s: %q is %q in the v6 table and %q in the v8 table",
					table, row[0], column, row[2*i+1], row[2*i+2])
			}
		}
	}
}

// TestTheMigrationCarriesEveryValueIntoTheV8ColumnItsRuleNames is the value-fidelity half of REQ-006:
// every rule of the data model's conversion table, read out of the database the pass left.
func TestTheMigrationCarriesEveryValueIntoTheV8ColumnItsRuleNames(t *testing.T) {
	db := openLegacyV6Database(t)

	auditShadow(t, db)

	migrateDatabase(t, db)

	auditValues(t, db)
}

// auditValues is the value-fidelity half of REQ-006 on a migrated database that already holds its shadow
// copies: every rule of the data model's conversion table, read out of the database the pass left.
func auditValues(t *testing.T, db *sql.DB) {
	t.Helper()

	// Groups and tags keep their identity and their label, and are ordered by label from 1.
	assertCarriedValues(t, db, "rdioScannerGroups",
		`SELECT v."_id", `+pair("label", "label")+` FROM "auditrdioScannerGroups" v JOIN "groups" t ON t."groupId" = v."_id" ORDER BY v."_id"`,
		"label")

	assertCarriedValues(t, db, "rdioScannerTags",
		`SELECT v."_id", `+pair("label", "label")+` FROM "auditrdioScannerTags" v JOIN "tags" t ON t."tagId" = v."_id" ORDER BY v."_id"`,
		"label")

	assertOrderedByLabel(t, db, "auditrdioScannerGroups", "_id", "groups", "groupId", "label")
	assertOrderedByLabel(t, db, "auditrdioScannerTags", "_id", "tags", "tagId", "label")

	// Systems keep their identity, their radio-system reference, and their settings; the blacklist JSON
	// arrives stripped of the brackets v6 stored it in.
	assertCarriedValues(t, db, "rdioScannerSystems",
		`SELECT v."_id", `+number("id", "systemRef")+`, `+pair("label", "label")+`, `+pair("led", "led")+`, `+
			number("order", "order")+`, `+flag("autoPopulate", "autoPopulate")+
			`, COALESCE(REPLACE(REPLACE(v."blacklists", '[', ''), ']', ''), ''), t."blacklists" `+
			`FROM "auditrdioScannerSystems" v JOIN "systems" t ON t."systemId" = v."_id" ORDER BY v."_id"`,
		"id", "label", "led", "order", "autoPopulate", "blacklists")

	// Talkgroups keep their identity, their name, and their references; the system reference is resolved
	// to the v8 system it names.
	assertCarriedValues(t, db, "rdioScannerTalkgroups",
		`SELECT v."_id", `+number("frequency", "frequency")+`, `+pair("label", "label")+`, `+pair("led", "led")+`, `+
			pair("name", "name")+`, `+number("order", "order")+`, `+number("id", "talkgroupRef")+`, `+number("tagId", "tagId")+
			`, COALESCE(v."systemId", 0), COALESCE(s."systemRef", 0) `+
			`FROM "auditrdioScannerTalkgroups" v JOIN "talkgroups" t ON t."talkgroupId" = v."_id" `+
			`JOIN "systems" s ON s."systemId" = t."systemId" ORDER BY v."_id"`,
		"frequency", "label", "led", "name", "order", "id", "tagId", "systemId")

	// A talkgroup's group arrives as a row of talkgroupGroups, except the group of 0 that v6 used for
	// "no group" and that has no v8 row to arrive as.
	assertCarriedValues(t, db, "rdioScannerTalkgroups",
		`SELECT v."_id", v."groupId", COALESCE(g."groupId", 0) FROM "auditrdioScannerTalkgroups" v `+
			`JOIN "talkgroups" t ON t."talkgroupId" = v."_id" LEFT JOIN "talkgroupGroups" g ON g."talkgroupId" = t."talkgroupId" `+
			`WHERE v."groupId" != 0 ORDER BY v."_id"`,
		"groupId")

	// Units keep their identity, their unit number — the FEAT-004 repair — and their system.
	assertCarriedValues(t, db, "rdioScannerUnits",
		`SELECT v."_id", `+number("id", "unitRef")+`, `+pair("label", "label")+`, `+number("order", "order")+
			`, COALESCE(v."systemId", 0), COALESCE(s."systemRef", 0) `+
			`FROM "auditrdioScannerUnits" v JOIN "units" t ON t."unitId" = v."_id" `+
			`JOIN "systems" s ON s."systemId" = t."systemId" WHERE v."systemId" != 10009 ORDER BY v."_id"`,
		"id", "label", "order", "systemId")

	// Downstreams, api keys, and accesses are copied, identity first.
	assertCarriedValues(t, db, "rdioScannerDownstreams",
		`SELECT v."_id", `+pair("apiKey", "apikey")+`, `+flag("disabled", "disabled")+`, `+number("order", "order")+
			`, `+pair("systems", "systems")+`, `+pair("url", "url")+` FROM "auditrdioScannerDownstreams" v `+
			`JOIN "downstreams" t ON t."downstreamId" = v."_id" ORDER BY v."_id"`,
		"apiKey", "disabled", "order", "systems", "url")

	assertCarriedValues(t, db, "rdioScannerApiKeys",
		`SELECT v."_id", `+flag("disabled", "disabled")+`, `+pair("ident", "ident")+`, `+pair("key", "key")+`, `+
			number("order", "order")+`, `+pair("systems", "systems")+` FROM "auditrdioScannerApiKeys" v `+
			`JOIN "apikeys" t ON t."apikeyId" = v."_id" ORDER BY v."_id"`,
		"disabled", "ident", "key", "order", "systems")

	assertCarriedValues(t, db, "rdioScannerAccesses",
		`SELECT v."_id", `+pair("code", "code")+`, `+pair("ident", "ident")+`, `+number("limit", "limit")+`, `+
			number("order", "order")+`, `+pair("systems", "systems")+` FROM "auditrdioScannerAccesses" v `+
			`JOIN "accesses" t ON t."accessId" = v."_id" ORDER BY v."_id"`,
		"code", "ident", "limit", "order", "systems")

	// Dirwatches keep every column v8 has room for. Their references are the rule below, because a
	// reference the legacy lookup had no row for arrives as nothing.
	assertCarriedValues(t, db, "rdioScannerDirWatches",
		`SELECT v."_id", `+number("delay", "delay")+`, `+flag("deleteAfter", "deleteAfter")+`, `+
			pair("directory", "directory")+`, `+flag("disabled", "disabled")+`, `+pair("extension", "extension")+`, `+
			number("frequency", "frequency")+`, `+pair("mask", "mask")+`, `+number("order", "order")+`, `+
			pair("type", "type")+` FROM "auditrdioScannerDirWatches" v JOIN "dirwatches" t ON t."dirwatchId" = v."_id" `+
			`ORDER BY v."_id"`,
		"delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "type")

	assertDirwatchReferences(t, db)

	// Calls keep their identity, their audio byte for byte, and their names; the system and talkgroup
	// references are resolved, and every call of a v6 deployment that had no site arrives with siteRef 0.
	assertCarriedValues(t, db, "rdioScannerCalls",
		`SELECT v."id", v."audio", t."audio", `+pair("audioName", "audioFilename")+`, `+pair("audioType", "audioMime")+
			`, COALESCE(v."system", 0), COALESCE(s."systemRef", 0), COALESCE(v."talkgroup", 0), COALESCE(g."talkgroupRef", 0), 0, t."siteRef" `+
			`FROM "auditrdioScannerCalls" v JOIN "calls" t ON t."callId" = v."id" `+
			`JOIN "systems" s ON s."systemId" = t."systemId" JOIN "talkgroups" g ON g."talkgroupId" = t."talkgroupId" `+
			`ORDER BY v."id"`,
		"audio", "audioName", "audioType", "system", "talkgroup", "siteRef")

	// The datetimes: v6 wrote them as text, v8 counts them, and no dialect converts one to the other the
	// same way, so the audit converts them itself and names the unit it compared.
	assertCarriedTimes(t, db, "rdioScannerLogs", "dateTime", "timestamp", "milliseconds", time.Millisecond,
		`SELECT v."_id", v."dateTime", t."timestamp" FROM "auditrdioScannerLogs" v JOIN "logs" t ON t."logId" = v."_id" `+
			`WHERE v."message" != '' ORDER BY v."_id"`)

	assertCarriedTimes(t, db, "rdioScannerCalls", "dateTime", "timestamp", "milliseconds", time.Millisecond,
		`SELECT v."id", v."dateTime", t."timestamp" FROM "auditrdioScannerCalls" v JOIN "calls" t ON t."callId" = v."id" ORDER BY v."id"`)

	assertCarriedTimes(t, db, "rdioScannerAccesses", "expiration", "expiration", "seconds", time.Second,
		`SELECT v."_id", v."expiration", t."expiration" FROM "auditrdioScannerAccesses" v JOIN "accesses" t ON t."accessId" = v."_id" `+
			`WHERE v."expiration" IS NOT NULL ORDER BY v."_id"`)

	// The JSON lists of a call arrive as the three child tables, and the archived copy of a call holds the
	// same lists for the rows the pass could not carry.
	assertCarriedJSONLists(t, db)
}

// assertOrderedByLabel is the groups and tags rule: the v8 `order` is 1..n in label order, which is what
// the legacy migration computed and what the administrator's list shows.
func assertOrderedByLabel(t *testing.T, db *sql.DB, v6Table, v6Identity, v8Table, v8Identity, column string) {
	t.Helper()

	rows := readRows(t, db, `SELECT "`+v8Identity+`", "`+column+`", "order" FROM "`+v8Table+`" ORDER BY "order"`)

	if len(rows) == 0 {
		t.Fatalf("the %s table is empty, so the order rule proves nothing", v8Table)
	}

	last := ""

	for i, row := range rows {
		if want := strconv.Itoa(i + 1); row[2] != want {
			t.Errorf("%s row %s has order %q, want %q: the pass orders these by label, from 1", v8Table, row[0], row[2], want)
		}

		if i > 0 && row[1] < last {
			t.Errorf("%s row %s is ordered after %q, which is after it in the label order the pass uses", v8Table, row[1], last)
		}

		last = row[1]
	}

	if got, want := len(rows), len(readRows(t, db, `SELECT "`+v6Identity+`" FROM "`+v6Table+`"`)); got != want {
		t.Errorf("%s holds %d rows, want the %d the v6 table has", v8Table, got, want)
	}
}

// assertCarriedTimes compares the datetime text a v6 row holds with the integer the v8 table holds for it,
// in the unit the data model's rule names. v6 wrote these as text in the layout Go's time package prints;
// no dialect converts that text to an epoch count the same way, so the audit converts it here.
func assertCarriedTimes(t *testing.T, db *sql.DB, table, v6Column, v8Column, unitName string, unit time.Duration, query string) {
	t.Helper()

	rows := readRows(t, db, query)

	if len(rows) == 0 {
		t.Fatalf("the %s %s comparison read no rows, so it proves nothing: %s", table, v6Column, query)
	}

	for _, row := range rows {
		when, err := parseV6DateTime(row[1])
		if err != nil {
			t.Fatalf("%s row %s: %q is %q, which is not a datetime a v6 database writes: %v",
				table, row[0], v6Column, row[1], err)
		}

		want := strconv.FormatInt(when.UnixNano()/int64(unit), 10)

		if row[2] != want {
			t.Errorf("%s row %s: %q is %q in the v6 table and %q in the v8 table, want %q counted in %s",
				table, row[0], v6Column, row[1], row[2], want, unitName)
		}
	}
}

// assertCarriedJSONLists is the calls rule the child tables hold: the frequencies, patches, and source
// units a v6 call kept in three JSON columns arrive as rows of callFrequencies, callPatches, and
// callUnits, and the counts match what the JSON held.
func assertCarriedJSONLists(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, row := range readRows(t, db, `SELECT "id", "system", "frequencies", "frequency", "patches", "source", "sources" FROM "auditrdioScannerCalls" ORDER BY "id"`) {
		callId, system, frequencies, frequency, patches, source, sources := row[0], row[1], row[2], row[3], row[4], row[5], row[6]

		if !hasString(auditIdentitiesWhere(t, db, "calls", "callId", `"callId" = `+callId), callId) {
			continue
		}

		// The frequencies list a call wrote arrives as one callFrequencies row for each entry of it — an
		// empty list arrives as none. A call that wrote no list at all arrives with the one row for the
		// frequency the call itself names, which is what the legacy migration wrote for the calls that
		// only had a frequency.
		want := len(jsonNames(t, frequencies))

		if frequencies == "" && isNumber(frequency) && frequency != "0" {
			want = 1
		}

		if got := len(auditIdentitiesWhere(t, db, "callFrequencies", "callId", `"callId" = `+callId)); got != want {
			t.Errorf("call %s holds %q and frequency %q, which the pass reads as %d frequencies, and callFrequencies holds %d rows for it",
				callId, frequencies, frequency, want, got)
		}

		// A patch names a talkgroup of the system the call belongs to. A number that system has no row
		// for is dropped, which is what the legacy lookup left and what FEAT-003 pinned.
		want = 0

		for _, ref := range jsonNames(t, patches) {
			if resolves(t, db, system, ref) {
				want++
			}
		}

		if got := len(auditIdentitiesWhere(t, db, "callPatches", "callId", `"callId" = `+callId)); got != want {
			t.Errorf("call %s holds %q, of which %d name a talkgroup of system %s, and callPatches holds %d rows for it",
				callId, patches, want, system, got)
		}

		// The source units a call wrote arrive as one callUnits row each. A call that wrote no list
		// arrives with the one row for the unit number the call itself names — and with a unit created
		// for that number when no unit holds it, which is the FEAT-004 repair.
		want = len(sourceUnits(t, sources))

		if (sources == "" || sources == "[]") && isNumber(source) && source != "0" {
			want = 1
		}

		if got := len(auditIdentitiesWhere(t, db, "callUnits", "callId", `"callId" = `+callId)); got != want {
			t.Errorf("call %s holds %q and source %q, which the pass reads as %d source units, and callUnits holds %d rows for it",
				callId, sources, source, want, got)
		}
	}
}

// isNumber reports whether a value read out of the database is a number the pass can use, rather than the
// NULL a v6 row left in that column.
func isNumber(text string) bool {
	_, err := strconv.ParseInt(text, 10, 64)

	return err == nil
}

// sourceUnits counts the entries of a call's source list that name a unit number. An entry with no number
// is skipped, which is what the legacy migration did.
func sourceUnits(t *testing.T, raw string) []string {
	t.Helper()

	if raw == "" || raw == "[]" {
		return nil
	}

	var entries []struct {
		Src float64 `json:"src"`
	}

	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("%q is not the source list a v6 call holds: %v", raw, err)
	}

	units := []string{}

	for _, entry := range entries {
		if entry.Src > 0 {
			units = append(units, strconv.FormatFloat(entry.Src, 'f', -1, 64))
		}
	}

	return units
}

// assertDirwatchReferences is the dirwatch reference rule: the system and the talkgroup a v6 dirwatch
// names arrive as the v8 rows they name, and a number the legacy lookup had no row for arrives as no
// reference at all.
//
// That last branch is what the legacy pass wrote and what FEAT-003 pinned: the v6 number it replaced is
// not kept anywhere, which is recorded as a residual risk in
// docs/specs/FEAT-011-v663-migration-integrity/spec.md. The audit proves the branch only ever happens for
// a reference the deployment really had no row for.
func assertDirwatchReferences(t *testing.T, db *sql.DB) {
	t.Helper()

	rows := readRows(t, db, `SELECT v."_id", COALESCE(v."systemId", 0), COALESCE(s."systemRef", 0), `+
		`COALESCE(v."talkgroupId", 0), COALESCE(g."talkgroupRef", 0) `+
		`FROM "auditrdioScannerDirWatches" v JOIN "dirwatches" t ON t."dirwatchId" = v."_id" `+
		`LEFT JOIN "systems" s ON s."systemId" = t."systemId" `+
		`LEFT JOIN "talkgroups" g ON g."talkgroupId" = t."talkgroupId" ORDER BY v."_id"`)

	if len(rows) == 0 {
		t.Fatal("the dirwatch reference comparison read no rows, so it proves nothing")
	}

	for _, row := range rows {
		dirwatchId, system, arrivedSystem, talkgroup, arrivedTalkgroup := row[0], row[1], row[2], row[3], row[4]

		if isNumber(system) && system != "0" && arrivedSystem != "0" && system != arrivedSystem {
			t.Errorf("rdioScannerDirWatches row %s names system %s, and the v8 dirwatch row arrived naming system %s",
				dirwatchId, system, arrivedSystem)
		}

		if isNumber(talkgroup) && talkgroup != "0" && arrivedTalkgroup != "0" && talkgroup != arrivedTalkgroup {
			t.Errorf("rdioScannerDirWatches row %s names talkgroup %s, and the v8 dirwatch row arrived naming talkgroup %s",
				dirwatchId, talkgroup, arrivedTalkgroup)
		}

		if arrivedTalkgroup == "0" && isNumber(talkgroup) && talkgroup != "0" && resolves(t, db, system, talkgroup) {
			t.Errorf("rdioScannerDirWatches row %s names talkgroup %s of system %s, which the migrated database has, "+
				"and the v8 dirwatch row arrived naming no talkgroup at all", dirwatchId, talkgroup, system)
		}
	}
}

// resolves reports whether a v6 system number and talkgroup number name a talkgroup of that system in the
// migrated database, which is the lookup the dirwatch and call migrations make.
func resolves(t *testing.T, db *sql.DB, systemRef, talkgroupRef string) bool {
	t.Helper()

	query := `SELECT COUNT(*) FROM "talkgroups" t JOIN "systems" s ON s."systemId" = t."systemId" ` +
		`WHERE s."systemRef" = ` + systemRef + ` AND t."talkgroupRef" = ` + talkgroupRef

	return readRows(t, db, query)[0][0] != "0"
}

// jsonNames counts the entries of one of a call's JSON list columns. An empty string is a v6 deployment
// that wrote nothing there, and counts as no entries.
func jsonNames(t *testing.T, raw string) []string {
	t.Helper()

	if raw == "" {
		return nil
	}

	var entries []json.RawMessage

	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("%q is not the JSON list a v6 call holds: %v", raw, err)
	}

	names := []string{}

	for _, entry := range entries {
		names = append(names, string(entry))
	}

	return names
}

// TestTheMigrationArchivesTheAudioOfTheCallsItCouldNotCarry is the recoverable half of the archive rule:
// an archived call holds its audio base64-encoded, because a call the pass could not carry is still a call
// somebody recorded, and the audio is the only part of it that cannot be typed back in.
func TestTheMigrationArchivesTheAudioOfTheCallsItCouldNotCarry(t *testing.T) {
	db := openLegacyV6Database(t)

	auditShadow(t, db)

	migrateDatabase(t, db)

	auditArchivedCallAudio(t, db)
}

// auditArchivedCallAudio is the recoverable half of the archive rule on a migrated database that already
// holds its shadow copies.
func auditArchivedCallAudio(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, row := range readRows(t, db, `SELECT "id", "audio" FROM "auditrdioScannerCalls" ORDER BY "id"`) {
		callId, audio := row[0], row[1]

		archived := archivedRow(t, db, "rdioScannerCalls", mustAtoi(t, callId))

		if archived == nil {
			continue
		}

		var contents struct {
			Audio string `json:"audio"`
		}

		if err := json.Unmarshal([]byte(archived[3]), &contents); err != nil {
			t.Fatalf("call %s is archived with contents that are not JSON: %v", callId, err)
		}

		got, err := base64.StdEncoding.DecodeString(contents.Audio)
		if err != nil {
			t.Fatalf("call %s is archived with audio that is not base64: %v", callId, err)
		}

		if string(got) != audio {
			t.Errorf("call %s is archived with %d bytes of audio, want the %d bytes the v6 table holds",
				callId, len(got), len(audio))
		}
	}
}

// TestTheMigrationLeavesNoV8RowPointingAtNothing is the reference-integrity half of REQ-006: a row that
// arrives pointing at a system, talkgroup, tag, group, or call that is not there is a row the operator
// cannot open, and a migration that produced one would look like a success.
func TestTheMigrationLeavesNoV8RowPointingAtNothing(t *testing.T) {
	db := openLegacyV6Database(t)

	migrateDatabase(t, db)

	auditReferences(t, db)
}

// auditReferences is the reference-integrity half of REQ-006 on a migrated database. It reads the migrated
// tables only, so it is the one half of the audit that needs no shadow copies.
func auditReferences(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, check := range []struct {
		says, query string
	}{
		{"a talkgroup names a system that is not there",
			`SELECT "talkgroupId", "systemId" FROM "talkgroups" WHERE "systemId" NOT IN (SELECT "systemId" FROM "systems")`},

		{"a talkgroup names a tag that is not there",
			`SELECT "talkgroupId", "tagId" FROM "talkgroups" WHERE "tagId" NOT IN (SELECT "tagId" FROM "tags")`},

		{"a unit names a system that is not there",
			`SELECT "unitId", "systemId" FROM "units" WHERE "systemId" NOT IN (SELECT "systemId" FROM "systems")`},

		{"a call names a system that is not there",
			`SELECT "callId", "systemId" FROM "calls" WHERE "systemId" NOT IN (SELECT "systemId" FROM "systems")`},

		{"a call names a talkgroup that is not there",
			`SELECT "callId", "talkgroupId" FROM "calls" WHERE "talkgroupId" NOT IN (SELECT "talkgroupId" FROM "talkgroups")`},

		{"a call's frequency row names a call that is not there",
			`SELECT "callFrequencyId", "callId" FROM "callFrequencies" WHERE "callId" NOT IN (SELECT "callId" FROM "calls")`},

		{"a call's patch row names a call that is not there",
			`SELECT "callPatchId", "callId" FROM "callPatches" WHERE "callId" NOT IN (SELECT "callId" FROM "calls")`},

		{"a call's patch row names a talkgroup that is not there",
			`SELECT "callPatchId", "talkgroupId" FROM "callPatches" WHERE "talkgroupId" NOT IN (SELECT "talkgroupId" FROM "talkgroups")`},

		{"a call's unit row names a call that is not there",
			`SELECT "callUnitId", "callId" FROM "callUnits" WHERE "callId" NOT IN (SELECT "callId" FROM "calls")`},

		{"a talkgroup group link names a group that is not there",
			`SELECT "talkgroupGroupId", "groupId" FROM "talkgroupGroups" WHERE "groupId" NOT IN (SELECT "groupId" FROM "groups")`},

		{"a talkgroup group link names a talkgroup that is not there",
			`SELECT "talkgroupGroupId", "talkgroupId" FROM "talkgroupGroups" WHERE "talkgroupId" NOT IN (SELECT "talkgroupId" FROM "talkgroups")`},
	} {
		if rows := readRows(t, db, check.query); len(rows) > 0 {
			t.Errorf("%s: %v", check.says, rows)
		}
	}
}

// TestTheMigrationDropsWhatTheV8SchemaHasNoRoomFor is the declared-exception half of REQ-006: the two
// things the pass does remove, and does not archive, because the v8 schema has no place for them and they
// are not deployment data.
//
// `rdioScannerMeta` is v6's own migration bookkeeping. `usePolling` is a v6 dirwatch column v7 removed
// with the polling feature: the dirwatch row itself is carried, and the one value of it v8 cannot hold is
// said to be dropped here rather than left to be discovered.
func TestTheMigrationDropsWhatTheV8SchemaHasNoRoomFor(t *testing.T) {
	db := openLegacyV6Database(t)

	auditShadow(t, db)

	migrateDatabase(t, db)

	auditDeclaredDrops(t, db, DialectSqlite)
}

// hasTable reports whether a database holds a table, read out of the catalog that backend itself offers.
// sqlite, MariaDB, and PostgreSQL each list their tables out of a view of their own, and an audit that
// asks one backend in the way another one answers reports a table missing because of the question.
func hasTable(t *testing.T, db *sql.DB, dialect Dialect, table string) bool {
	t.Helper()

	return hasString(tableNames(t, db, dialect), table)
}

// tableNames lists the tables a database holds, in name order.
func tableNames(t *testing.T, db *sql.DB, dialect Dialect) []string {
	t.Helper()

	switch dialect {
	case DialectSqlite:
		return readTableNames(t, db)

	case DialectMariadb, DialectMysql:
		return flat(readRows(t, db, "SHOW TABLES"))

	case DialectPostgresql:
		return flat(readRows(t, db, `SELECT "tablename" FROM "pg_tables" WHERE "schemaname" = current_schema() ORDER BY 1`))
	}

	t.Fatalf("tableNames: %s is not a backend this audit can read its tables from", dialect)

	return nil
}

// auditDeclaredDrops is the declared-exception half of REQ-006 on a migrated database that already holds
// its shadow copies.
func auditDeclaredDrops(t *testing.T, db *sql.DB, dialect Dialect) {
	t.Helper()

	if hasTable(t, db, dialect, "rdioScannerMeta") {
		t.Error("the v6 rdioScannerMeta table survived the pass, which is v6 bookkeeping with no v8 counterpart")
	}

	if rows := readRows(t, db, `SELECT "rowId" FROM "migrationArchive" WHERE "tableName" = 'rdioScannerMeta'`); len(rows) > 0 {
		t.Errorf("the archive holds %v for rdioScannerMeta, want nothing: its rows are not deployment data", rows)
	}

	if got := len(readRows(t, db, `SELECT "name" FROM "auditrdioScannerMeta"`)); got == 0 {
		t.Fatal("the fixture has no rdioScannerMeta rows, so dropping it proves nothing")
	}

	// The dirwatch rows are carried, and the polling flag is the one value of them v8 has no column for.
	if got, want := len(auditIdentities(t, db, "dirwatches", "dirwatchId")), len(auditIdentities(t, db, "auditrdioScannerDirWatches", "_id")); got != want {
		t.Errorf("the v8 dirwatches table holds %d rows, want the %d the v6 table has: usePolling is dropped, the row is not",
			got, want)
	}

	if rows := readRows(t, db, `SELECT "rowId", "contents" FROM "migrationArchive" WHERE "tableName" = 'rdioScannerDirWatches' AND "contents" LIKE '%usePolling%'`); len(rows) > 0 {
		t.Errorf("the archive holds %v for dirwatches with a usePolling value in it, want nothing: v8 has no column to put it back in",
			rows)
	}
}

// mustAtoi reads an identity the audit got out of the database back into the number the archive looks a
// row up by.
func mustAtoi(t *testing.T, text string) int {
	t.Helper()

	n, err := strconv.Atoi(text)
	if err != nil {
		t.Fatalf("%q is not the identity the archive holds rows by: %v", text, err)
	}

	return n
}

// largeAudioBytes is one call's worth of audio here. The number is chosen for the page cache, not for
// realism: twenty kilobytes across five hundred calls is more than sqlite keeps dirty in memory, which is
// the moment the pass used to stop at.
const largeAudioBytes = 20 * 1024

func TestMigrateCallsCarriesATableSqliteCannotReadAndWriteAtOnce(t *testing.T) {
	db := openLegacyV6Database(t)

	// The arranged calls resolve through the systems, talkgroups and units the fixture already holds. A
	// call naming something the database does not hold would be archived here rather than carried, and
	// what this test is about is carrying a hundred times more of it than the fixture holds.
	for n := range 500 {
		id := 720 + n

		if _, err := db.Exec(
			`INSERT INTO "rdioScannerCalls" ("id", "audio", "audioName", "audioType", "dateTime", "frequencies", "frequency", "patches", "source", "sources", "system", "talkgroup") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id,
			make([]byte, largeAudioBytes),
			fmt.Sprintf("%d.wav", id),
			"audio/wav",
			"2025-06-15 09:00:00.000 +0000 UTC",
			`[{"errorCount":0,"freq":1514000000,"pos":0,"spikeCount":0}]`,
			1514000000,
			"[]",
			12345,
			"[]",
			10001,
			1514000,
		); err != nil {
			t.Fatalf("arranging call %d: %v", id, err)
		}
	}

	// A pass that is waiting on itself never comes back, so the deadline is what makes this test fail
	// rather than freeze the suite. It is not a bar on speed: the pass carries this table in a fraction
	// of a second, and the deadline only ever speaks for the case where it stopped.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := Migrate(ctx, db, DialectSqlite, fixedClock{migrationInstant}, func(level, message string) {}); err != nil {
		t.Fatalf("Migrate over a calls table of %d KiB per row: %v", largeAudioBytes/1024, err)
	}

	carried := countArrangedCalls(t, db)

	if carried != 500 {
		t.Errorf("the pass carried %d of the 500 calls it was given, and stopped at %d", carried, largeAudioBytes/1024)
	}

	if hasV6Table(t, db, "rdioScannerCalls") {
		t.Errorf("the pass left rdioScannerCalls where it found it, so it did not read the whole table")
	}
}

// countArrangedCalls counts the calls this test put into the v6 table that came out in the v8 one. The
// fixture's own calls are not counted: they are the subject of every other test in this package.
func countArrangedCalls(t *testing.T, db *sql.DB) int {
	t.Helper()

	var carried int

	if err := db.QueryRow(
		`SELECT COUNT(*) FROM "calls" WHERE "callId" >= 720 AND "callId" < 1220`,
	).Scan(&carried); err != nil {
		t.Fatalf("counting the carried calls: %v", err)
	}

	return carried
}
