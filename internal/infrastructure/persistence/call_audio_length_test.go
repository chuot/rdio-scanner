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
	"regexp"
	"testing"
)

// The calls table the legacy server wrote has no length column, and ApplySchema cannot add
// one: its statements are the legacy server's, every one `IF NOT EXISTS`, and their contract
// is that a database the other server wrote comes out exactly as it went in. The column
// arrives, like the log source column before it, in the migration pass that brings an
// existing database up to this shape.

func callColumns(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.Query(`SELECT "name" FROM pragma_table_info("calls")`)
	if err != nil {
		t.Fatalf("reading the columns of the calls table: %v", err)
	}

	defer rows.Close()

	names := []string{}

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning a column name: %v", err)
		}

		names = append(names, name)
	}

	return names
}

func TestMigrateAddsTheAudioLengthColumnToADatabaseThatAlreadyHasTheCallsTable(t *testing.T) {
	db := openLegacyDatabase(t)

	if columns := callColumns(t, db); hasString(columns, "audioLength") {
		t.Fatalf("the fixture already has an audioLength column (%v): it is no longer the database the legacy server wrote", columns)
	}

	before := countRows(t, db, "calls")

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate over a database written by the legacy server: %v", err)
	}

	after := callColumns(t, db)

	if !hasString(after, "audioLength") {
		t.Fatalf("the calls table has %v after the migration, want an audioLength column among them", after)
	}

	if moved := countRows(t, db, "calls"); moved != before {
		t.Errorf("the calls table had %d rows before the migration and has %d after it", before, moved)
	}
}

// The startup pass runs on every start; the second start must not re-add the column and
// must not change a stored row. A deployment that stored calls with lengths keeps exactly
// those lengths across restarts.
func TestMigrateAddingTheAudioLengthColumnTwiceIsTheSameAsOnce(t *testing.T) {
	db := openLegacyDatabase(t)

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	once := readSchemaObjects(t, db)

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
}

// A calls table that is not there at all ends the step with nothing said and nothing done,
// which is how every other migration step answers the same question.
func TestEnsureCallAudioLengthColumnOnADatabaseWithNoCallsTable(t *testing.T) {
	db := openLegacyDatabase(t)

	if _, err := db.Exec(`DROP TABLE "calls"`); err != nil {
		t.Fatalf("DROP TABLE calls: %v", err)
	}

	s := newStore(db, DialectSqlite)

	if err := ensureCallAudioLengthColumn(context.Background(), s); err != nil {
		t.Fatalf("ensureCallAudioLengthColumn on a database with no calls table: %v", err)
	}
}

// A call the legacy server wrote reads back with no length: the default the column arrives
// with is the server's own "never measured", and the migration writes no length into a row
// it cannot know.
func TestMigrateLeavesLegacyWrittenCallsWithNoLength(t *testing.T) {
	db := openLegacyDatabase(t)

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	calls := NewCalls(db, DialectSqlite)

	found, err := calls.GetCall(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if found.AudioLength != 0 {
		t.Errorf("the legacy-written call reads length %v, want no length", found.AudioLength)
	}
}

// Every dialect's copy of the calls table declares the column with the default, for the
// same reason the log source column does: the default is what lets a write that does not
// name the column — the legacy server's own insert, and this server's writes of calls with
// no measured length — land without naming what they do not know.
func TestEveryDialectSchemaDeclaresTheAudioLengthColumn(t *testing.T) {
	for _, tc := range []struct {
		dialect Dialect
		schema  []string
	}{
		{DialectSqlite, sqliteSchema},
		{DialectMariadb, mysqlSchema},
		{DialectMysql, mysqlSchema},
		{DialectPostgresql, postgresSchema},
	} {
		declared := ""

		for _, statement := range tc.schema {
			if regexp.MustCompile(`(?i)CREATE TABLE IF NOT EXISTS "calls"`).MatchString(statement) {
				declared = statement

				break
			}
		}

		if declared == "" {
			t.Fatalf("%s has no calls table to declare a length on", tc.dialect)
		}

		if !regexp.MustCompile(`(?i)"audioLength"\s+(integer|bigint)\s+not\s+null\s+default\s+0`).MatchString(declared) {
			t.Errorf("%s declares the calls table without an audioLength column carrying a default:\n%s", tc.dialect, declared)
		}
	}
}
