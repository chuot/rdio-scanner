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
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// legacyTables are the tables the legacy server's schema creates, which both servers have to agree
// on. sqlite_sequence is not one of them: sqlite maintains it for the AUTOINCREMENT primaries.
var legacyTables = []string{
	"accesses",
	"apikeys",
	"callFrequencies",
	"callPatches",
	"callUnits",
	"calls",
	"delayed",
	"dirwatches",
	"downstreams",
	"groups",
	"logs",
	"options",
	"sites",
	"systems",
	"tags",
	"talkgroupGroups",
	"talkgroups",
	"units",
}

// TestApplySchemaLeavesALegacyWrittenDatabaseExactlyAsItFoundIt is the CA-007.1 half that concerns
// the schema: server-ng starts against a database the legacy server created and leaves it alone,
// so the legacy server can still open it afterwards.
func TestApplySchemaLeavesALegacyWrittenDatabaseExactlyAsItFoundIt(t *testing.T) {
	db := openLegacyDatabase(t)

	before := readSchemaObjects(t, db)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema over a database written by the legacy server: %v", err)
	}

	after := readSchemaObjects(t, db)

	if len(after) != len(before) {
		t.Fatalf("the database has %d schema objects after the apply and had %d before", len(after), len(before))
	}

	for i := range before {
		if before[i] != after[i] {
			t.Errorf("schema object %d changed\nbefore: %s\nafter:  %s", i, before[i], after[i])
		}
	}

	// The fixture is the legacy server's own work, so a schema apply that changed a table would
	// have had to rewrite rows to survive; the counts are the cheap proof that it did not.
	for _, table := range []string{"systems", "talkgroups", "groups", "tags", "accesses", "apikeys", "dirwatches", "downstreams", "logs", "options", "calls"} {
		if count := countRows(t, db, table); count == 0 {
			t.Errorf("%s has no rows: the fixture written by the legacy server is not the one generate.sh produced", table)
		}
	}
}

// TestApplySchemaTwiceIsTheSameAsOnce covers the startup path the legacy server is on: the schema
// is applied on every start, including the start after a start.
func TestApplySchemaTwiceIsTheSameAsOnce(t *testing.T) {
	db := openLegacyDatabase(t)

	before := readSchemaObjects(t, db)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema a second time: %v", err)
	}

	after := readSchemaObjects(t, db)

	if len(before) != len(after) {
		t.Fatalf("applying the schema twice changed the database: %d schema objects before, %d after", len(before), len(after))
	}

	for i := range before {
		if before[i] != after[i] {
			t.Errorf("schema object %d changed\nbefore: %s\nafter:  %s", i, before[i], after[i])
		}
	}
}

// TestApplySchemaCreatesEveryLegacyTableOnAFreshDatabase is the other direction of REQ-007: a
// database server-ng creates is one the legacy server can open, so it has to contain every table
// the legacy server expects, and the index its call history queries are written against.
func TestApplySchemaCreatesEveryLegacyTableOnAFreshDatabase(t *testing.T) {
	db, err := Open(Settings{
		Dialect:  DialectSqlite,
		FilePath: filepath.Join(t.TempDir(), "rdio-scanner.db"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	defer db.Close()

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	created := readTableNames(t, db)

	if len(created) != len(legacyTables) {
		t.Fatalf("the fresh database has tables %v, want %v", created, legacyTables)
	}

	for i := range legacyTables {
		if created[i] != legacyTables[i] {
			t.Fatalf("the fresh database has tables %v, want %v", created, legacyTables)
		}
	}

	var index string

	if err := db.QueryRow(`SELECT "name" FROM "sqlite_master" WHERE "type" = 'index' AND "name" = 'calls_idx'`).Scan(&index); err != nil {
		t.Errorf("calls_idx is missing: the legacy server's call history queries are written against it: %v", err)
	}
}

// TestEveryDialectSchemaCoversTheSameTables guards the one thing a copy can get wrong: a table
// present for sqlite and missing for MySQL or PostgreSQL, which would look like a working server
// until somebody deploys on the backend that lost it.
func TestEveryDialectSchemaCoversTheSameTables(t *testing.T) {
	// Case-insensitive because the legacy server writes the logs table in lowercase, and the copy
	// keeps it that way.
	declared := regexp.MustCompile(`(?i)CREATE TABLE IF NOT EXISTS "([A-Za-z]+)"`)

	for _, tc := range []struct {
		dialect Dialect
		schema  []string
	}{
		{DialectSqlite, sqliteSchema},
		{DialectMariadb, mysqlSchema},
		{DialectMysql, mysqlSchema},
		{DialectPostgresql, postgresSchema},
	} {
		tables := []string{}

		for _, statement := range tc.schema {
			for _, m := range declared.FindAllStringSubmatch(statement, -1) {
				tables = append(tables, m[1])
			}
		}

		sort.Strings(tables)

		if len(tables) != len(legacyTables) {
			t.Errorf("%s creates %v, want %v", tc.dialect, tables, legacyTables)

			continue
		}

		for i := range legacyTables {
			if tables[i] != legacyTables[i] {
				t.Errorf("%s creates %v, want %v", tc.dialect, tables, legacyTables)

				break
			}
		}
	}
}

func TestSchemaForAnUnknownDialectIsTheLegacyFailure(t *testing.T) {
	_, err := schemaFor(Dialect("oracle"))

	if err == nil {
		t.Fatal("a dialect with no schema is a failure the caller is told about")
	}

	if err.Error() != "no database schema" {
		t.Errorf("error = %v, want the legacy wording", err)
	}
}

// readSchemaObjects reads every schema object, definition included, in a stable order, so that two
// databases can be compared for "identical" rather than for "has the same table names".
func readSchemaObjects(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.Query(`SELECT "type", "name", COALESCE("sql", '') FROM "sqlite_master" ORDER BY "type", "name"`)
	if err != nil {
		t.Fatalf("reading the schema: %v", err)
	}

	defer rows.Close()

	objects := []string{}

	for rows.Next() {
		var kind, name, definition string

		if err := rows.Scan(&kind, &name, &definition); err != nil {
			t.Fatalf("reading the schema: %v", err)
		}

		objects = append(objects, kind+" "+name+" "+definition)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the schema: %v", err)
	}

	return objects
}

func readTableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.Query(`SELECT "name" FROM "sqlite_master" WHERE "type" = 'table' AND "name" != 'sqlite_sequence' ORDER BY "name"`)
	if err != nil {
		t.Fatalf("reading the tables: %v", err)
	}

	defer rows.Close()

	names := []string{}

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			t.Fatalf("reading the tables: %v", err)
		}

		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the tables: %v", err)
	}

	return names
}
