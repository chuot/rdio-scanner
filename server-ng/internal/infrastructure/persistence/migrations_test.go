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
			"regenerate it with server-ng/testdata/legacyv6/generate.sh", err)
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

// TestMigrateOnAFreshDatabaseMigratesNothingAndSaysNothing is the case every server-ng deployment
// is in: there are no v6 tables, so the pass creates the schema and then does and says nothing.
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
func TestMigrateOnALegacyWrittenV7DatabaseMigratesNothing(t *testing.T) {
	db := openLegacyDatabase(t)

	before := readSchemaObjects(t, db)

	if progress := migrateDatabase(t, db); len(progress) != 0 {
		t.Errorf("a v7 database was announced as migrating: %v", progress)
	}

	if after := readSchemaObjects(t, db); !equalStrings(before, after) {
		t.Errorf("the schema of a v7 database changed")
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
// See server/migrations.go, migrateSystems: `system.Blacklists` and `system.Led` are assigned
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
// See server/migrations.go: `talkgroup.Led`, `l.Level`, `dirwatch.Kind`, and `call.AudioMime` are
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

// TestMigrateUnitsStoreEveryMigratedUnitWithAZeroUnitReference is a legacy defect: the legacy
// insert names the identity the unit object never had in the position the unit reference belongs
// in, so every migrated unit loses the v6 reference its row was read for.
//
// See server/migrations.go, migrateUnits: the insert ends with `unit.Id`, and `NewUnit()` never
// gives a unit an id.
func TestMigrateUnitsStoreEveryMigratedUnitWithAZeroUnitReference(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	// The call migration adds a unit of its own later on; this test is about the units the unit
	// migration wrote from the v6 table.
	got := readRows(t, db, `SELECT "unitId", "label", "order", "systemId", "unitRef" FROM "units" WHERE "unitId" IN (301, 302) ORDER BY 1`)
	want := [][]string{
		{"301", "Engine 12", "1", "101", "0"},
		{"302", "Car 4", "2", "101", "0"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the migrated units:\n  got  %v\n  want %v", got, want)
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
// See server/migrations.go, migrateDirwatches: `talkgroupId` is scanned only by the lookup query,
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
	want = [][]string{{"701", "1.5", "12345"}, {"702", "2.5", "99999"}}

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
// See server/migrations.go, migrateCalls and migrateDirwatches: the lookup query has no ON clause.
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
// See server/migrations.go, migrateCalls: `frequencies.String` is only tested for length, while
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

// TestMigrateCallsTreatsAMigratedUnitAsAUnitThatDoesNotExist is what the unit reference defect
// costs: the call asks whether its source unit is already in the units table by the reference the
// unit migration threw away, finds nothing, and creates the unit a second time.
//
// See TestMigrateUnitsStoreEveryMigratedUnitWithAZeroUnitReference.
func TestMigrateCallsTreatsAMigratedUnitAsAUnitThatDoesNotExist(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	duplicates := 0

	for _, row := range rowsOf(t, db, "units", `"unitId", "label", "systemId", "unitRef"`) {
		if row[1] == "6789" && row[2] == "101" && row[3] == "6789" {
			duplicates++
		}

		if row[1] == "Car 4" && row[3] != "0" {
			t.Fatalf("the migrated unit %v lost its reference in the wrong way", row)
		}
	}

	if duplicates != 1 {
		t.Errorf("the source unit of call 706, which the migration had already migrated as unit 302, was created %d times, want 1", duplicates)
	}
}

// TestMigrateCallsReportsNoFailureForTheUnitItCreated is the legacy defect that takes the whole
// upgrade down. The legacy migration reports the unit insert from the branch that only runs when
// the insert succeeded, with the error it just tested for being nil, and its error formatter calls
// Error() on that nil: running the legacy binary over this fixture panics at
// server/migrations.go:435 and the calls migration is rolled back.
//
// server-ng reports the line instead of crashing, and keeps the legacy outcome that matters: the
// call is left without its source unit, because the callUnits row is written only when the unit
// insert failed.
func TestMigrateCallsReportsNoFailureForTheUnitItCreated(t *testing.T) {
	db, progress := migrateLegacyV6Fixture(t)

	found := 0

	for _, line := range progress {
		if !strings.HasPrefix(line, "migration.migrateCalls:") {
			continue
		}

		if !strings.Contains(line, `%!s(<nil>)`) {
			t.Errorf("the failure the legacy migration reported with no error at all came out as %q", line)
		}

		if !strings.Contains(line, `INSERT INTO "units" ("label", "systemId", "unitRef") VALUES (?, ?, ?)`) {
			t.Errorf("the failure the legacy migration reported with no error at all names %q", line)
		}

		found++
	}

	if found != 1 {
		t.Errorf("the unit the call migration created was reported %d times, want 1; progress: %v", found, progress)
	}

	for _, row := range rowsOf(t, db, "callUnits", `"callId"`) {
		if row[0] == "706" {
			t.Errorf("call 706 got its source unit row, which the legacy migration writes only when the unit insert fails")
		}
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
