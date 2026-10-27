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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// legacyTables are the tables the legacy server's schema creates, which both
// servers have to agree on. sqlite_sequence is not one of them: sqlite maintains
// it for the AUTOINCREMENT primaries.
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

// This server starts against a legacy-created database and leaves it alone, so the
// legacy server can still open it afterwards.
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

	// The fixture is the legacy server's own work, so a schema apply that changed a table
	// would have had to rewrite rows to survive; the counts are the cheap proof it did not.
	for _, table := range []string{"systems", "talkgroups", "groups", "tags", "accesses", "apikeys", "dirwatches", "downstreams", "logs", "options", "calls"} {
		if count := countRows(t, db, table); count == 0 {
			t.Errorf("%s has no rows: the fixture written by the legacy server is not the one generate.sh produced", table)
		}
	}
}

// The schema is applied on every start, including the start after a start.
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

// A database this server creates is one the legacy server can open, so it contains
// every table the legacy server expects, and the index its call history queries
// are written against.
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

// Guards the one thing a copy can get wrong: a table present for sqlite and
// missing for MySQL or PostgreSQL, which looks like a working server until
// somebody deploys on the backend that lost it.
func TestEveryDialectSchemaCoversTheSameTables(t *testing.T) {
	// Case-insensitive because the legacy server writes the logs table in lowercase,
	// and the copy keeps it that way.
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

// schemaClassOf folds away only the spellings one backend uses for the same
// storage: sqlite has no boolean type and spells it integer(1), PostgreSQL
// spells binary as bytea and fractional as float, and a type width suffix
// changes nothing about what the column holds. What it keeps apart is the
// class a bound Go value has to land in: a text column where every other
// backend has a numeric one is not a spelling, it is a column that cannot
// hold the value the Go side binds to it.
func schemaClassOf(declaredType string) string {
	declaredType = strings.ToLower(declaredType)

	if declaredType == "integer(1)" {
		return "boolean"
	}

	if i := strings.IndexByte(declaredType, '('); i >= 0 {
		declaredType = declaredType[:i]
	}

	switch declaredType {
	case "boolean", "bool":
		return "boolean"

	case "integer", "int", "bigint", "smallint", "mediumint", "bigserial", "serial":
		return "numeric"

	case "real", "float", "double", "numeric", "decimal":
		return "fractional"

	case "text", "varchar", "char", "character":
		return "text"

	case "blob", "bytea", "longblob", "mediumblob", "tinyblob", "varbinary":
		return "binary"

	case "datetime", "timestamp", "date":
		return "temporal"

	default:
		return "? " + declaredType
	}
}

// Guards the divergence the table-coverage check cannot see: the same column
// declared in a different type class for one backend. The server works on the
// backends that spell the column right and fails at bind time on the one that
// does not, because the domain binds one Go type to the column and every
// backend has to be able to hold it: a unit range is a pair of numbers bound
// as uint values, and a text column refuses them.
func TestEveryDialectSchemaStoresEverySharedColumnInOneTypeClass(t *testing.T) {
	// Column names are compared case-insensitively because the legacy server
	// spelled some of them inconsistently and every backend resolves column
	// names without regard to case.
	table := regexp.MustCompile(`(?is)CREATE TABLE IF NOT EXISTS "([A-Za-z]+)"\s*\((.*?)\n\s*\);`)
	column := regexp.MustCompile(`(?is)^\s*"([A-Za-z]+)"\s+([A-Za-z]+(?:\([0-9]+\))?)`)
	constraint := regexp.MustCompile(`(?i)^\s*(CONSTRAINT|PRIMARY KEY|UNIQUE|FOREIGN KEY|CHECK)\b`)

	classesOf := func(schema []string) map[string]string {
		classes := map[string]string{}

		for _, statement := range schema {
			if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "CREATE TABLE") {
				continue
			}

			match := table.FindStringSubmatch(statement)
			if match == nil {
				t.Fatalf("a CREATE TABLE statement this check cannot read: %.80s", statement)
			}

			for _, line := range strings.Split(match[2], "\n") {
				line = strings.TrimSpace(line)

				if line == "" || constraint.MatchString(line) {
					continue
				}

				declared := column.FindStringSubmatch(line)
				if declared == nil {
					t.Fatalf("a column definition this check cannot read in %s: %q", match[1], line)
				}

				classes[strings.ToLower(match[1])+"."+strings.ToLower(declared[1])] = schemaClassOf(declared[2])
			}
		}

		return classes
	}

	sqliteClasses := classesOf(sqliteSchema)

	compareTo := map[Dialect]map[string]string{
		DialectMariadb:    classesOf(mysqlSchema),
		DialectMysql:      classesOf(mysqlSchema),
		DialectPostgresql: classesOf(postgresSchema),
	}

	names := make([]string, 0, len(sqliteClasses))
	for name := range sqliteClasses {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		for _, dialect := range []Dialect{DialectMariadb, DialectMysql, DialectPostgresql} {
			other, shared := compareTo[dialect][name]
			if !shared {
				t.Errorf("%s declares no column %s that the sqlite schema declares", dialect, name)

				continue
			}

			if other != sqliteClasses[name] {
				t.Errorf("%s stores column %s as %s while sqlite stores it as %s: the domain binds one Go type to it and every backend must hold it",
					dialect, name, other, sqliteClasses[name])
			}
		}
	}
}

// readSchemaObjects reads every schema object, definition included, in a stable order,
// so two databases can be compared for "identical" rather than for "same table names".
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

// A log entry names the part of the server that wrote it, and a deployment on the
// backend whose copy lost the column has a log view that cannot say where its
// entries came from.
//
// The two log indexes are deliberately not asserted here: they belong to the
// migration step that brings an existing database up to shape, not to the copied
// schema statements.
func TestEveryDialectSchemaDeclaresTheLogSourceColumn(t *testing.T) {
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
			if regexp.MustCompile(`(?i)CREATE TABLE IF NOT EXISTS "logs"`).MatchString(statement) {
				declared = statement

				break
			}
		}

		if declared == "" {
			t.Fatalf("%s has no logs table to declare a source on", tc.dialect)
		}

		// MySQL refuses a default on a TEXT column, so the shared MySQL/MariaDB copy declares
		// the source as a varchar; PostgreSQL and SQLite keep it as text. What every dialect
		// shares is the NOT NULL and the default — the default is what lets the legacy server
		// keep inserting into this table without knowing the column exists.
		if !regexp.MustCompile(`(?i)"source"\s+(text|varchar\(\d+\))\s+not\s+null\s+default\s+''`).MatchString(declared) {
			t.Errorf("%s declares the logs table without a source column carrying a default:\n%s", tc.dialect, declared)
		}
	}
}
