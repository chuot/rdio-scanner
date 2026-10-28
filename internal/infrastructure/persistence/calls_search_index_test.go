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
	"testing"
)

// The calls search index arrives through a step of the startup pass, not through the
// copied schema statements: the legacy copy spells the creation `IF NOT EXISTS`, which
// MySQL reports as a syntax error, and the copy is applied on every start.

func searchIndexDefinitions(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.Query(`SELECT "sql" FROM "sqlite_master" WHERE "type" = 'index' AND "tbl_name" = 'calls' ORDER BY "name"`)
	if err != nil {
		t.Fatalf("reading the indexes of the calls table: %v", err)
	}

	defer rows.Close()

	definitions := []string{}

	for rows.Next() {
		var definition string
		if err := rows.Scan(&definition); err != nil {
			t.Fatalf("scanning an index definition: %v", err)
		}

		definitions = append(definitions, definition)
	}

	return definitions
}

// A calls table that does not carry the search index gets it from the startup pass,
// over the column order the history filter's statements are written against.
func TestMigrateGivesACallsTableWithoutTheSearchIndexOne(t *testing.T) {
	db := openLegacyDatabase(t)

	if _, err := db.Exec(`DROP INDEX "calls_idx"`); err != nil {
		t.Fatalf("DROP INDEX calls_idx: %v", err)
	}

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate over a database whose calls table has no search index: %v", err)
	}

	definitions := searchIndexDefinitions(t, db)

	if len(definitions) != 1 || definitions[0] != callsSearchIndexCreate {
		t.Errorf("the migrated calls table carries %v, want exactly %s", definitions, callsSearchIndexCreate)
	}
}

// The startup pass runs on every start: the second start must not re-create the index.
func TestMigrateAddingTheCallsSearchIndexTwiceIsTheSameAsOnce(t *testing.T) {
	db := openLegacyDatabase(t)

	if _, err := db.Exec(`DROP INDEX "calls_idx"`); err != nil {
		t.Fatalf("DROP INDEX calls_idx: %v", err)
	}

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

// A calls table that already carries the index keeps the one it has: the step asks the
// database what it already has and adds only what is missing.
func TestMigrateLeavesACallsTableThatAlreadyHasTheSearchIndexAlone(t *testing.T) {
	db := openLegacyDatabase(t)

	before := searchIndexDefinitions(t, db)

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	after := searchIndexDefinitions(t, db)

	if len(before) != len(after) {
		t.Fatalf("the migration changed the indexes of the calls table: %d before, %d after", len(before), len(after))
	}

	for i := range before {
		if before[i] != after[i] {
			t.Errorf("index %d changed\nbefore: %s\nafter: %s", i, before[i], after[i])
		}
	}
}

// A `calls` table that is not there at all ends the step doing nothing.
func TestEnsureCallsSearchIndexOnADatabaseWithNoCallsTable(t *testing.T) {
	db := openLegacyDatabase(t)

	if _, err := db.Exec(`DROP TABLE "calls"`); err != nil {
		t.Fatalf("DROP TABLE calls: %v", err)
	}

	s := newStore(db, DialectSqlite)

	if err := ensureCallsSearchIndex(context.Background(), s); err != nil {
		t.Fatalf("ensureCallsSearchIndex on a database with no calls table: %v", err)
	}
}
