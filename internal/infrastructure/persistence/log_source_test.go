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
	"strings"
	"testing"
)

// T005: the migration step that owns every change this feature makes to a database that already exists.
//
// ApplySchema cannot do any of it — every one of its statements is `IF NOT EXISTS`, so on a `logs` table
// that is already there they change nothing — and it must not try: its contract is that a database the
// legacy server wrote comes out exactly as it went in.

func TestMigrateAddsTheLogSourceColumnToADatabaseThatAlreadyHasTheTable(t *testing.T) {
	db := openLegacyDatabase(t)

	if columns := logColumns(t, db); hasString(columns, "source") {
		t.Fatalf("the fixture already has a source column (%v): it is no longer the database the legacy server wrote", columns)
	}

	before := countRows(t, db, "logs")

	if err := Migrate(context.Background(), db, DialectSqlite, nil); err != nil {
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

// TestMigrateBringsTheLogSourceColumnUpTwiceIsTheSameAsOnce is the startup an existing deployment is on:
// the migration pass runs on every start, and the second start must not re-add the column, re-run the
// backfill, or change a row.
func TestMigrateAddingTheLogSourceColumnTwiceIsTheSameAsOnce(t *testing.T) {
	db := openLegacyDatabase(t)

	if err := Migrate(context.Background(), db, DialectSqlite, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	once := readSchemaObjects(t, db)
	onceRows := readLogRows(t, db)

	if err := Migrate(context.Background(), db, DialectSqlite, nil); err != nil {
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

// TestEnsureLogSourceColumnOnADatabaseWithNoLogsTable says what the probe does when the table the step is
// here to change is not there: it says nothing and does nothing, which is how every v6 migration in this
// file already answers the same question.
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

// TestEnsureLogSourceColumnOnAnEmptyLogsTable covers the database this server itself created and then
// emptied: the table is there, it holds nothing, and the step still gives it the column and the indexes,
// because a log table without them is a log view that cannot be filtered.
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

// TestEnsureLogSourceColumnAddsTheIndexesWithoutAColumnToAdd covers the start after the start, on a
// database that already has the column: the indexes are still ensured, and nothing else moves.
func TestEnsureLogSourceColumnEnsuresTheIndexesOnADatabaseThatAlreadyHasTheColumn(t *testing.T) {
	db := openLegacyDatabase(t)

	if err := Migrate(context.Background(), db, DialectSqlite, nil); err != nil {
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

// readLogRows reads the whole log table in order, so a migration that quietly rewrote or lost a row is a
// failing test rather than a difference nobody looked at.
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

// logIndexDefinition reads back what the database says one of its own indexes is, so a test can name the
// object it expects without pasting the statement the migration ran.
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

// isLogsTable and isLogIndex name a schema object by its kind and its name, which is the first two words
// of the string readSchemaObjects builds.
func isLogsTable(object string) bool {
	return strings.HasPrefix(object, "table logs ")
}

func isLogIndex(object string, name string) bool {
	return strings.HasPrefix(object, "index "+name+" ")
}
