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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// legacyV6Fixture is a database shaped like one a v6.6.3 deployment leaves behind: the v6 schema,
// and rows that cover every table the v6 → v7 migration pass reads. See
// testdata/legacyv6/generate.sh.
//
// The expectations below were read off the legacy server's own output: the binary built from
// server/ was run over this fixture and the resulting tables were read back. Where the legacy
// server cannot finish — it panics inside the calls migration on the last call of this fixture —
// the expectation comes from the legacy source instead, and the test that pins it says so.
const legacyV6Fixture = "../../../testdata/legacyv6/rdio-scanner.db"

// copyLegacyV6Fixture puts the fixture in a temporary directory and returns its path, so that a
// test which writes to it cannot damage the committed one.
func copyLegacyV6Fixture(t *testing.T) string {
	t.Helper()

	b, err := os.ReadFile(legacyV6Fixture)
	if err != nil {
		t.Fatalf("the v6-shaped fixture is missing: %v\n"+
			"regenerate it with testdata/legacyv6/generate.sh", err)
	}

	path := filepath.Join(t.TempDir(), "rdio-scanner.db")

	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("copying the fixture: %v", err)
	}

	return path
}

// openLegacyV6Database opens a private copy of the fixture the way the server opens its own
// database, foreign keys enforced.
func openLegacyV6Database(t *testing.T) *sql.DB {
	t.Helper()

	db, err := Open(Settings{Dialect: DialectSqlite, FilePath: copyLegacyV6Fixture(t)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	return db
}

// migrateLegacyV6Fixture migrates a private copy of the fixture and returns the database with
// every line the migration reported.
func migrateLegacyV6Fixture(t *testing.T) (*sql.DB, []string) {
	t.Helper()

	db := openLegacyV6Database(t)

	return db, migrateDatabase(t, db)
}

// The damaged fixtures are databases a legacy server has already migrated, in the two states the
// legacy migration pass actually leaves behind: migrated.db, where the pass finished and left the
// damage in the units and the missing source links, and crashed.db, where the calls migration was
// rolled back and the v6 calls table is still there. See testdata/legacydamaged/generate.sh.
const (
	damagedMigratedFixture = "../../../testdata/legacydamaged/migrated.db"
	damagedCrashedFixture  = "../../../testdata/legacydamaged/crashed.db"
)

// openDamagedFixture opens a private copy of one of the damaged fixtures, so that a test which
// writes to it cannot damage the committed one.
func openDamagedFixture(t *testing.T, fixture string) *sql.DB {
	t.Helper()

	b, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("the damaged fixture %s is missing: %v\n"+
			"regenerate it with testdata/legacydamaged/generate.sh", fixture, err)
	}

	path := filepath.Join(t.TempDir(), "rdio-scanner.db")

	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("copying %s: %v", fixture, err)
	}

	db, err := Open(Settings{Dialect: DialectSqlite, FilePath: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	return db
}

// damageReport keeps the lines the startup damage report wrote out of a progress report.
func damageReport(progress []string) []string {
	found := []string{}

	for _, line := range progress {
		if strings.HasPrefix(line, "migration.damage:") {
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

	progress := []string{}

	if err := Migrate(context.Background(), db, DialectSqlite, func(message string) {
		progress = append(progress, message)
	}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	return progress
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

// TestMigrateMovesEveryV6TableIntoTheV7Schema is the shape of the whole pass: a v6 database comes
// out holding exactly the v7 tables, with none of the v6 tables left behind.
func TestMigrateMovesEveryV6TableIntoTheV7Schema(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	if got, want := readTableNames(t, db), legacyTables; !equalStrings(got, want) {
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

// TestMigrateOnAFreshDatabaseMigratesNothingAndSaysNothing is the case a deployment of this server
// that has never run is in: there are no v6 tables, so the pass creates the schema and then does and
// says nothing.
func TestMigrateOnAFreshDatabaseMigratesNothingAndSaysNothing(t *testing.T) {
	db := openEmptyDatabase(t)

	if progress := migrateDatabase(t, db); len(progress) != 0 {
		t.Errorf("a fresh database was announced as migrating: %v", progress)
	}

	if got, want := readTableNames(t, db), legacyTables; !equalStrings(got, want) {
		t.Errorf("the tables of a fresh database:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateOnALegacyWrittenV7DatabaseMigratesNothing covers the deployment that swaps one server
// binary for the other on a database the legacy server already migrated.
//
// FEAT-010 narrowed what "nothing" means here, and deliberately: the v6 migration pass still moves no
// row and says no word on a database that has no v6 tables, but the startup pass now brings the log table
// up to the shape this server writes — the column that names the part of the server that wrote each entry,
// and the two indexes the log search runs against. That is the one schema change this server makes to a
// database the other server wrote, and the test says so exactly: those three objects and nothing else.
func TestMigrateOnALegacyWrittenV7DatabaseMigratesNothing(t *testing.T) {
	db := openLegacyDatabase(t)

	before := readSchemaObjects(t, db)
	beforeRows := readLogRows(t, db)

	if progress := migrateDatabase(t, db); len(progress) != 0 {
		t.Errorf("a v7 database was announced as migrating: %v", progress)
	}

	after := readSchemaObjects(t, db)

	if len(after) != len(before)+2 {
		t.Fatalf("a v7 database gained %d schema objects, want the logs table's source column and the two log indexes", len(after)-len(before))
	}

	// The logs table is the one object allowed to read differently afterwards, and only because it
	// gained a column. Every other table, index and trigger has to be byte-identical, and the only new
	// objects are the two indexes the log search runs against.
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

	if len(added) != 2 || !isLogIndex(added[0], "logs_source_idx") || !isLogIndex(added[1], "logs_time_idx") {
		t.Errorf("the schema objects a v7 database gained: %v, want logs_source_idx and logs_time_idx", added)
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

	err := Migrate(context.Background(), db, DialectSqlite, nil)
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

// TestMigrateTalkgroupsReportAForeignKeyFailureForATalkgroupInNoGroup is a legacy defect the
// fixture pins: v6 stored 0 for a talkgroup in no group, the migration keeps the talkgroup, and
// then hands that 0 to the group link, which the foreign key rejects — one line per row, and the
// pass carries on.
func TestMigrateTalkgroupsReportAForeignKeyFailureForATalkgroupInNoGroup(t *testing.T) {
	db, progress := migrateLegacyV6Fixture(t)

	found := 0

	for _, line := range progress {
		if strings.HasPrefix(line, "migration.migrateTalkgroups:") && strings.Contains(line, "talkgroupGroups") {
			found++
		}
	}

	if found != 1 {
		t.Errorf("the group link of the talkgroup in no group was reported %d times, want 1; progress: %v", found, progress)
	}

	if got := countRows(t, db, "talkgroups"); got != 3 {
		t.Errorf("the talkgroup whose group link failed was not kept: %d talkgroups, want 3", got)
	}

	if got := rowsOf(t, db, "talkgroupGroups", `"groupId", "talkgroupId"`); len(got) != 2 {
		t.Errorf("the group links:\n  got  %v\n  want the two links whose group exists, the talkgroup in no group having failed", got)
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

// TestMigrateOptionsDropsAKeyTheLegacyUnpackListDoesNotName: a key the blob holds and the fixed
// list does not name is not migrated, and nothing is said about it.
func TestMigrateOptionsDropsAKeyTheLegacyUnpackListDoesNotName(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := readRows(t, db, `SELECT "key", "value" FROM "options" WHERE "key" = 'unknownKey'`)

	if len(got) != 0 {
		t.Errorf("the v6 options blob carried %v into the v7 options, want nothing", got)
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
// a row missing its apikey, its scope, or its url is skipped.
func TestMigrateDownstreamsCopyTheV6Row(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "downstreams", `"downstreamId", "apikey", "disabled", "order", "systems", "url"`)
	want := [][]string{
		{"501", "relay-api-key", "1", "1", "[10001]", "http://127.0.0.1:8080/patch"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated downstreams:\n  got  %v\n  want %v", got, want)
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
		if strings.HasPrefix(line, "migration.migrateDirwatches:") {
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

// TestMigrateCallsSkipACallWhoseSystemDoesNotResolve: a call whose references do not resolve is
// skipped without a word, and is dropped with the v6 table.
func TestMigrateCallsSkipACallWhoseSystemDoesNotResolve(t *testing.T) {
	db, progress := migrateLegacyV6Fixture(t)

	for _, row := range rowsOf(t, db, "calls", `"callId"`) {
		if row[0] == "705" {
			t.Fatalf("the call naming a system nothing migrated was migrated")
		}
	}

	for _, line := range progress {
		if strings.Contains(line, `INSERT INTO "calls"`) {
			t.Errorf("the skipped call was reported: %q", line)
		}
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
// scope, or no key is still stored, with the empty value the legacy migration stored.
func TestMigrateApikeysCopyTheV6Rows(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "apikeys", `"apikeyId", "disabled", "ident", "key", "order", "systems"`)
	want := [][]string{
		{"801", "0", "ops key", "ops-api-key", "1", "[10001]"},
		{"802", "1", "test key", "test-api-key", "2", "[10002]"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated apikeys:\n  got  %v\n  want %v", got, want)
	}
}

// TestMigrateAccessesStoreTheExpirationInSeconds: an access expiration is the one timestamp in this
// schema that is not Unix milliseconds, and an access with no expiration is stored as 0.
func TestMigrateAccessesStoreTheExpirationInSeconds(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	got := rowsOf(t, db, "accesses", `"accessId", "code", "expiration", "ident", "limit", "order", "systems"`)
	want := [][]string{
		{"901", "ABC123", "1749992400", "ops", "10", "1", "[10001]"},
		{"902", "XYZ789", "0", "guest", "0", "2", "[10002]"},
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

	if err := Migrate(context.Background(), db, DialectSqlite, nil); err != nil {
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
	db := openDamagedFixture(t, damagedCrashedFixture)

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
	db := openDamagedFixture(t, damagedMigratedFixture)

	before := tableSnapshot(t, db)

	got := damageReport(migrateDatabase(t, db))
	want := []string{
		"migration.damage: 2 units hold no radio reference, the numbers they were migrated from are not in this database",
		`migration.damage:   unit 301 "Engine 12" of Grand Metro (10001)`,
		`migration.damage:   unit 302 "Car 4" of Grand Metro (10001)`,
		"migration.damage: 1 unit is labelled with its own number, the shape of a unit the migration created rather than a deployment named",
		`migration.damage:   unit 303 "6789" of Grand Metro (10001)`,
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
	db := openDamagedFixture(t, damagedMigratedFixture)

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
	for _, fixture := range []string{damagedMigratedFixture, damagedCrashedFixture} {
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
