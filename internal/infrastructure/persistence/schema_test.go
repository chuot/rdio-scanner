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
	"slices"
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

// copiedIndexNames are the members of the call-history index set that each dialect's
// schema copy declares, in the order the copies declare them. The set's covering key is
// absent: its column list names a column a database written by the legacy server gains
// only after this copy has been applied, so the startup pass creates that key after the
// stage that adds the column.
var copiedIndexNames = []string{
	"callfrequencies_call",
	"callpatches_call",
	"callunits_call",
	"delayed_call",
	"talkgroups_system_ref",
	"talkgroups_tag",
	"talkgroupgroups_talkgroup",
	"talkgroupgroups_group",
	"sites_system",
}

// copiedIndexSchemaObjects names those same keys the way a list of schema objects names
// them, with the kind each is stored as, in the order such a list reports them.
func copiedIndexSchemaObjects() []string {
	names := make([]string, 0, len(copiedIndexNames))

	for _, name := range copiedIndexNames {
		names = append(names, "index "+name)
	}

	sort.Strings(names)

	return names
}

// This server starts against a legacy-created database and rewrites nothing in it, so the
// legacy server can still open it afterwards. It does add the index keys its own schema
// copy declares.
func TestApplySchemaLeavesEveryObjectOfALegacyWrittenDatabase(t *testing.T) {
	db := openLegacyDatabase(t)

	before := readSchemaObjects(t, db)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema over a database written by the legacy server: %v", err)
	}

	after := readSchemaObjects(t, db)

	if len(after) != len(before)+len(copiedIndexNames) {
		t.Fatalf("the database has %d schema objects after the apply and had %d before, want the %d index keys the schema copy adds",
			len(after), len(before), len(copiedIndexNames))
	}

	// Every object the legacy server created is still there with its own definition, and
	// the only new objects are the index keys the copy declares, each named. The objects
	// come back sorted by kind and name, so the new keys move the existing ones along the
	// list and each is looked for by name.
	for _, object := range before {
		if !hasString(after, object) {
			t.Errorf("the apply changed or removed a schema object of the database written by the legacy server: %s", object)
		}
	}

	if gained := gainedSchemaObjects(before, after); !equalStrings(gained, copiedIndexSchemaObjects()) {
		t.Errorf("the apply over a legacy-written database gained %v, want the %d index keys the schema copy declares: %v",
			gained, len(copiedIndexNames), copiedIndexSchemaObjects())
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

	firstPass := readSchemaObjects(t, db)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema a second time: %v", err)
	}

	after := readSchemaObjects(t, db)

	if len(firstPass) != len(after) {
		t.Fatalf("applying the schema twice changed the database: %d schema objects after one apply, %d after two", len(firstPass), len(after))
	}

	for i := range firstPass {
		if firstPass[i] != after[i] {
			t.Errorf("schema object %d changed\nafter one apply:  %s\nafter two applies: %s", i, firstPass[i], after[i])
		}
	}

	// The nine keys the copy declares are all the apply adds to the database the legacy
	// server wrote, which is what a second apply finds already there.
	if len(after) != len(before)+len(copiedIndexNames) {
		t.Fatalf("applying the schema twice left %d schema objects where the legacy-written database had %d, want the %d index keys the copy adds",
			len(after), len(before), len(copiedIndexNames))
	}

	if gained := gainedSchemaObjects(before, after); !equalStrings(gained, copiedIndexSchemaObjects()) {
		t.Errorf("applying the schema twice gained %v, want the %d index keys gained once: %v",
			gained, len(copiedIndexNames), copiedIndexSchemaObjects())
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

// The copy comparison above reaches table names only. This one reaches the indexes each
// copy creates, because a key carried by one copy and not by the others is how one backend
// answers a history read with a scan while its neighbour answers it from the index. The two
// differences the copies genuinely have are stated instead of smoothed over: the shipped
// pair key keeps the column order the released schema of that backend gave it, so the two
// copies spell it in different orders, and the MySQL copy carries no index statement at all,
// because MySQL has no spelling of a create that a schema applied on every boot can use and
// every member of the call history index set reaches that backend through the startup pass.
func TestEverySchemaCopyCarriesTheSameIndexSet(t *testing.T) {
	carries := map[Dialect][]indexSpec{
		DialectSqlite:     indexesOfSchemaCopy(sqliteSchema),
		DialectPostgresql: indexesOfSchemaCopy(postgresSchema),
		DialectMysql:      indexesOfSchemaCopy(mysqlSchema),
	}

	sqlite, postgres := carries[DialectSqlite], carries[DialectPostgresql]

	if len(sqlite) == 0 {
		t.Fatalf("the sqlite schema copy declares no index, so this comparison holds nothing")
	}

	if len(postgres) != len(sqlite) {
		t.Fatalf("the postgresql schema copy declares %d indexes (%s) and the sqlite copy declares %d (%s): the two copies must create the same indexes",
			len(postgres), indexNames(postgres), len(sqlite), indexNames(sqlite))
	}

	// The pair key the shipped startup statement carries, read out of that statement rather
	// than written out again here.
	shipped := indexesOfSchemaCopy([]string{callsSearchIndexCreate})[0]

	// The order the released PostgreSQL schema gives the same key, which the postgresql copy
	// keeps and the startup statement does not use.
	const shippedPostgresColumns = "systemId,talkgroupId,siteRef,timestamp"

	for i := range sqlite {
		one, other := sqlite[i], postgres[i]

		if one.name != other.name {
			t.Errorf("index %d of the sqlite copy is %s and of the postgresql copy is %s: the two copies must create the same indexes in the same order",
				i, one.name, other.name)

			continue
		}

		if one.table != other.table {
			t.Errorf("%s sits on %s in the sqlite copy and on %s in the postgresql copy", one.name, one.table, other.table)
		}

		sqliteColumns := strings.Join(one.columns, ",")
		postgresColumns := strings.Join(other.columns, ",")

		if one.name == shipped.name {
			if sqliteColumns != strings.Join(shipped.columns, ",") {
				t.Errorf("the sqlite copy declares %s over (%s), want the columns the startup pass creates it over: (%s)",
					one.name, sqliteColumns, strings.Join(shipped.columns, ", "))
			}

			if postgresColumns != shippedPostgresColumns {
				t.Errorf("the postgresql copy declares %s over (%s), want the order that backend's released schema gives it: (%s)",
					one.name, postgresColumns, strings.ReplaceAll(shippedPostgresColumns, ",", ", "))
			}

			continue
		}

		if sqliteColumns != postgresColumns {
			t.Errorf("%s carries (%s) in the sqlite copy and (%s) in the postgresql copy: the two copies must declare the same columns in the same order",
				one.name, sqliteColumns, postgresColumns)
		}
	}

	// Neither copy may declare a member of the set over a column a database the legacy server
	// wrote gains only later, which is the case for the set's covering key: the startup pass
	// creates it, after the stage that adds the column it indexes.
	legacyColumns := columnsOfLegacyWrittenTables(t)

	for _, dialect := range []Dialect{DialectSqlite, DialectPostgresql, DialectMysql} {
		for _, index := range carries[dialect] {
			for _, member := range callIndexSet {
				if member.name != index.name {
					continue
				}

				if missing := columnsAbsentFromLegacyTable(legacyColumns, member); len(missing) > 0 {
					t.Errorf("%s declares %s over a column a database written by the legacy server does not have yet: %s, which is the column the startup pass creates that key after",
						dialect, member.name, strings.Join(missing, ", "))
				}
			}
		}
	}

	if len(carries[DialectMysql]) != 0 {
		t.Errorf("the MySQL schema copy declares %s, want no index statement at all: every member of the call history index set reaches MySQL and MariaDB through the startup pass",
			indexNames(carries[DialectMysql]))
	}
}

// The set is what the startup pass creates, and the schema copies declare the members of
// it they can run: a copied statement reaches a database the legacy server wrote as well
// as a fresh one, and the columns a legacy-written table already carries are fewer than the
// set indexes. An index over a column the table does not have yet is refused outright on
// PostgreSQL, which aborts the whole apply and the start with it, and accepted on SQLite,
// which stores a key that answers that column's own name instead of the row's value for
// every row written before the column stage ran. So each copy declares the members a
// legacy-written table can host, key for key and column for column, in the set's order, and
// the startup pass supplies the rest.
func TestCallIndexSetMatchesEverySchemaCopy(t *testing.T) {
	legacyColumns := columnsOfLegacyWrittenTables(t)

	inSet := map[string]bool{}

	for _, member := range callIndexSet {
		inSet[member.name] = true
	}

	for _, tc := range []struct {
		dialect Dialect
		schema  []string
	}{
		{DialectSqlite, sqliteSchema},
		{DialectPostgresql, postgresSchema},
	} {
		declared := indexesOfSchemaCopy(tc.schema)

		carried := []indexSpec{}
		laterColumns := []string{}

		for _, member := range callIndexSet {
			if missing := columnsAbsentFromLegacyTable(legacyColumns, member); len(missing) > 0 {
				laterColumns = append(laterColumns, strings.Join(missing, ", "))

				continue
			}

			carried = append(carried, member)
		}

		for _, member := range carried {
			hits := []indexSpec{}

			for _, index := range declared {
				if index.name == member.name {
					hits = append(hits, index)
				}
			}

			if len(hits) != 1 {
				t.Errorf("%s declares %s %d times, want it declared exactly once", tc.dialect, member.name, len(hits))

				continue
			}

			if hits[0].table != member.table {
				t.Errorf("%s declares %s on %s, want it on %s", tc.dialect, member.name, hits[0].table, member.table)
			}

			if strings.Join(hits[0].columns, ",") != strings.Join(member.columns, ",") {
				t.Errorf("%s declares %s over (%s), want it over (%s)", tc.dialect, member.name,
					strings.Join(hits[0].columns, ", "), strings.Join(member.columns, ", "))
			}
		}

		order := []string{}

		for _, index := range declared {
			if inSet[index.name] {
				order = append(order, index.name)
			}
		}

		if strings.Join(order, ", ") != indexNames(carried) {
			t.Errorf("%s declares %d of the set in the order %s, want %s", tc.dialect, len(carried),
				strings.Join(order, ", "), indexNames(carried))
		}

		if len(order) != len(carried) {
			t.Errorf("%s carries %d indexes of the set over the columns a database written by the legacy server has, want %d; a later column is %s",
				tc.dialect, len(order), len(carried), strings.Join(laterColumns, "; "))
		}
	}
}

// columnsOfLegacyWrittenTables reads the columns of every table exactly as a database the
// legacy server wrote has them, which is the shape a copied schema statement is applied to
// on every start.
func columnsOfLegacyWrittenTables(t *testing.T) map[string][]string {
	t.Helper()

	db := openLegacyDatabase(t)

	columns := map[string][]string{}

	for _, table := range legacyTables {
		rows, err := db.Query(`SELECT "name" FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}

		names := []string{}

		for rows.Next() {
			var name string

			if err := rows.Scan(&name); err != nil {
				rows.Close()

				t.Fatalf("%s: %v", table, err)
			}

			names = append(names, name)
		}

		rows.Close()

		if err := rows.Err(); err != nil {
			t.Fatalf("%s: %v", table, err)
		}

		if len(names) == 0 {
			t.Fatalf("%s carries no columns in the database written by the legacy server", table)
		}

		columns[table] = names
	}

	return columns
}

// columnsAbsentFromLegacyTable names the columns a member indexes that a legacy-written
// table does not carry, which are the columns a later migration stage adds.
func columnsAbsentFromLegacyTable(legacyColumns map[string][]string, member indexSpec) []string {
	missing := []string{}

	for _, column := range member.columns {
		if !slices.Contains(legacyColumns[member.table], column) {
			missing = append(missing, column)
		}
	}

	return missing
}

// A copy declares an index in one spelling, so one pattern reads every index of a
// copy back as a name, the table it sits on, and its columns in the order the copy
// lists them.
func indexesOfSchemaCopy(copy []string) []indexSpec {
	declared := regexp.MustCompile(`(?i)CREATE INDEX(?: IF NOT EXISTS)? "([A-Za-z0-9_]+)" ON "([A-Za-z0-9_]+)" \(([A-Za-z0-9_ ",]+)\)`)

	indexes := []indexSpec{}

	for _, statement := range copy {
		for _, m := range declared.FindAllStringSubmatch(statement, -1) {
			columns := []string{}

			for _, column := range strings.Split(m[3], ",") {
				column = strings.Trim(strings.TrimSpace(column), `"`)

				if column != "" {
					columns = append(columns, column)
				}
			}

			indexes = append(indexes, indexSpec{name: m[1], table: m[2], columns: columns})
		}
	}

	return indexes
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

// schemaObjectNames names each object readSchemaObjects returns by its kind and its name,
// which are the first two words of the string that object is built from. The order of the
// list it is given is kept, so a list read from the schema and a list expected by hand can
// be compared directly.
func schemaObjectNames(objects []string) []string {
	names := []string{}

	for _, object := range objects {
		kind, rest, _ := strings.Cut(object, " ")
		name, _, _ := strings.Cut(rest, " ")

		names = append(names, kind+" "+name)
	}

	return names
}

// gainedSchemaObjects names, with the kind each is stored as, the schema objects after
// holds that before does not, in the order the schema reports them.
func gainedSchemaObjects(before, after []string) []string {
	held := map[string]bool{}

	for _, object := range before {
		held[object] = true
	}

	gained := []string{}

	for _, object := range after {
		if held[object] {
			continue
		}

		gained = append(gained, object)
	}

	return schemaObjectNames(gained)
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

// MySQL has no `CREATE INDEX IF NOT EXISTS` — that spelling belongs to MariaDB, which took
// it on in 10.1.4 — and the schema is applied on every start, so the first statement MySQL
// refuses stops the server before it has opened its listening sockets. The rest of the
// schema copy already probes index existence instead of asking the backend to skip a
// duplicate, so the copy has to keep probing.
func TestMysqlSchemaCreatesIndexesInSyntaxMysqlAccepts(t *testing.T) {
	provisioned := regexp.MustCompile(`(?i)CREATE\s+(UNIQUE\s+)?INDEX\s+IF\s+NOT\s+EXISTS`)

	for _, statement := range mysqlSchema {
		if refused := provisioned.FindString(statement); refused != "" {
			t.Errorf("the MySQL schema creates an index with %q, a spelling MySQL reports as a syntax error: %s",
				refused, statement)
		}
	}
}
