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
	"slices"
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

// A history read touches five columns of the calls table: the timestamp it orders
// and windows on, the row identity and the audio length it reports, and the two
// keys its joins reach a system reference and a talkgroup reference through. The
// set's covering key is its only key on that table, and it carries all five, so an
// unfiltered history page answers from the index without reading a table row.
func TestCoveringKeyCarriesEveryHistoryColumn(t *testing.T) {
	historyColumns := []string{"timestamp", "callId", "audioLength", "systemId", "talkgroupId"}

	var onCalls []indexSpec

	for _, member := range callIndexSet {
		if member.table == "calls" {
			onCalls = append(onCalls, member)
		}
	}

	if len(onCalls) != 1 {
		t.Fatalf("the set carries %d keys on the calls table (%s), want the covering key alone",
			len(onCalls), indexNames(onCalls))
	}

	covering := onCalls[0]

	if covering.name != "calls_timestamp_cover" {
		t.Errorf("the set's only key on the calls table is %s, want the covering key", covering.name)
	}

	if len(covering.columns) == 0 || covering.columns[0] != "timestamp" {
		t.Errorf("%s carries (%s), want a key led by the timestamp: every history read orders on it",
			covering.name, strings.Join(covering.columns, ", "))
	}

	for _, column := range historyColumns {
		if !slices.Contains(covering.columns, column) {
			t.Errorf("%s does not carry %s: the history read needs that column, so it would leave "+
				"the index for the table", covering.name, column)
		}
	}
}

// A key is recognised by the columns it carries and not by the name it carries them under.
// The schema's foreign keys make an index of their own over several of these column lists,
// under names of the engine's choosing, so a step that recognised names would give the
// table a second index over a list it already serves — and a second key costs every write
// to that table for nothing. The probe therefore reports what a table carries as each
// index's own name with its columns in index order, and reports as missing only a member
// no index carries over its whole list: a key that shares a prefix with one, a key that
// carries its columns in another order, and the shipped pair key on the same table as the
// covering key are three different lists.
func TestForeignKeyIndexUnderAnyNameIsNotDuplicated(t *testing.T) {
	db := openLegacyDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	// The legacy-written database carries one secondary index, `calls_idx`, and none of the
	// set's members. Four indexes are planted in the shape a foreign key or an earlier
	// release leaves behind: one of the set's own lists under another name, one sharing a
	// prefix with another member's list, one carrying a member's columns in the other order,
	// and one carrying a column a member has alone as its second column.
	planted := []string{
		`CREATE INDEX "callunits_lookup" ON "callUnits" ("callId")`,
		`CREATE INDEX "talkgroups_pair" ON "talkgroups" ("systemId","tagId")`,
		`CREATE INDEX "talkgroups_system_only" ON "talkgroups" ("systemId")`,
		`CREATE INDEX "talkgroups_ref_first" ON "talkgroups" ("talkgroupRef","systemId")`,
	}

	for _, statement := range planted {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	// The probe reports the table's indexes under the names they carry, with the columns in
	// the order the index carries them.
	for _, one := range []struct {
		table string
		want  []string
	}{
		{
			table: "calls",
			want:  []string{"calls_idx(systemId, siteRef, talkgroupId, timestamp)"},
		},
		{
			table: "talkgroups",
			want: []string{
				"talkgroups_pair(systemId, tagId)",
				"talkgroups_ref_first(talkgroupRef, systemId)",
				"talkgroups_system_only(systemId)",
			},
		},
	} {
		carries, err := hasCallIndexShape(ctx, s, one.table)
		if err != nil {
			t.Fatalf("hasCallIndexShape(%s): %v", one.table, err)
		}

		if got := indexShapes(carries); !slices.Equal(got, one.want) {
			t.Errorf("hasCallIndexShape(%s) reports (%s), want (%s)",
				one.table, strings.Join(got, ", "), strings.Join(one.want, ", "))
		}
	}

	// The only member the planted indexes serve is the one whose whole column list one of
	// them carries. Every other member is still missing, in the order the set names them.
	missing, err := missingCallIndexes(ctx, s)
	if err != nil {
		t.Fatalf("missingCallIndexes: %v", err)
	}

	want := []string{}

	for _, member := range callIndexSet {
		if member.name != "callunits_call" {
			want = append(want, member.name)
		}
	}

	got := missingNames(missing)

	if !slices.Equal(got, want) {
		t.Errorf("missingCallIndexes reports (%s), want every member of the set except the one "+
			"a planted index already carries over its whole list (%s)", strings.Join(got, ", "), strings.Join(want, ", "))
	}

	// A member's name is not what the probe matches on: `callunits_call` is absent from the
	// report because a key under a name the set does not own already carries its list, and
	// no report line is left for that table.
	for _, member := range missing {
		if member.table == "callUnits" {
			t.Errorf("missingCallIndexes reports %s over (%s) on callUnits, a list the planted "+
				"callunits_lookup already carries under another name",
				member.name, strings.Join(member.columns, ", "))
		}
	}
}

// The covering key is the one member the copied schema statements do not declare, so on a
// database those statements have brought to shape it is the one member left for the step to
// build, and the start after that one has nothing left to build.
func TestCallIndexSetStepCreatesWhatTheCatalogueLacks(t *testing.T) {
	db, ran := openCountedSchemaDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	// The column the covering key indexes is added by the stage that runs before the step,
	// so the key is created over a table that already has it.
	if err := ensureCallAudioLengthColumn(ctx, s); err != nil {
		t.Fatalf("ensureCallAudioLengthColumn: %v", err)
	}

	missing, err := missingCallIndexes(ctx, s)
	if err != nil {
		t.Fatalf("missingCallIndexes: %v", err)
	}

	if got := missingNames(missing); !slices.Equal(got, []string{"calls_timestamp_cover"}) {
		t.Fatalf("a database carrying the copied statements leaves (%s) for the step, want the covering key alone",
			strings.Join(got, ", "))
	}

	reported := []progressLine{}

	ran.reset()

	if err := ensureCallIndexSet(ctx, s, func(level, message string) {
		reported = append(reported, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("ensureCallIndexSet: %v", err)
	}

	if got := ran.count("CREATE INDEX"); got != 1 {
		t.Errorf("the step ran %d create statements over a database missing one member, want one", got)
	}

	want_create := `CREATE INDEX IF NOT EXISTS "calls_timestamp_cover" ON "calls" ("timestamp","callId","audioLength","systemId","talkgroupId")`

	if got := ran.count(want_create); got != 1 {
		t.Errorf("the step did not run %s: it ran %v", want_create, ran.queries)
	}

	// Every member of the set is carried afterwards, each under its own name over its own
	// column list.
	for _, member := range callIndexSet {
		carries, err := hasCallIndexShape(ctx, s, member.table)
		if err != nil {
			t.Fatalf("hasCallIndexShape(%s): %v", member.table, err)
		}

		shape := member.name + "(" + strings.Join(member.columns, ", ") + ")"

		if !slices.Contains(indexShapes(carries), shape) {
			t.Errorf("after the step %s carries %s, want it to carry %s",
				member.table, strings.Join(indexShapes(carries), ", "), shape)
		}
	}

	if missing, err := missingCallIndexes(ctx, s); err != nil {
		t.Fatalf("missingCallIndexes: %v", err)
	} else if len(missing) != 0 {
		t.Errorf("after the step the probe still reports (%s) missing, want nothing", strings.Join(missingNames(missing), ", "))
	}

	want_report := []progressLine{
		{level: domain.LogLevelInfo, message: `created the call history index calls_timestamp_cover on calls over (timestamp, callId, audioLength, systemId, talkgroupId)`},
	}

	if !slices.Equal(reported, want_report) {
		t.Errorf("the step reported %v, want %v", reported, want_report)
	}

	// The startup pass runs on every start: the next one finds the set and changes nothing.
	again := []progressLine{}

	ran.reset()

	if err := ensureCallIndexSet(ctx, s, func(level, message string) {
		again = append(again, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("ensureCallIndexSet a second time: %v", err)
	}

	if len(again) != 0 {
		t.Errorf("the second run reported %v, want it to say nothing", again)
	}

	if got := ran.count("CREATE INDEX"); got != 0 {
		t.Errorf("the second run ran %d create statements, want none", got)
	}
}

// An index the step did not make stays exactly as it found it, whether it carries a column
// list the set does not name or one of the set's own names under another list, and nothing
// the step runs destroys an index.
func TestCallIndexSetStepKeepsEveryIndexItFinds(t *testing.T) {
	db, ran := openCountedSchemaDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	if err := ensureCallAudioLengthColumn(ctx, s); err != nil {
		t.Fatalf("ensureCallAudioLengthColumn: %v", err)
	}

	planted := []struct {
		table     string
		statement string
	}{
		// A key over a column list the set does not name, sharing only a prefix with the
		// covering key's list.
		{table: "calls", statement: `CREATE INDEX "calls_order_pair" ON "calls" ("timestamp","systemId")`},
		// One of the set's own names, held by a key over another column list. The copied
		// statement that made that name is replaced with a key of another shape first, which is
		// the state an earlier release or a hand-edited database leaves behind.
		{table: "sites", statement: `CREATE INDEX "sites_system" ON "sites" ("siteRef")`},
	}

	if _, err := db.Exec(`DROP INDEX "sites_system"`); err != nil {
		t.Fatalf("DROP INDEX sites_system: %v", err)
	}

	for _, one := range planted {
		if _, err := db.Exec(one.statement); err != nil {
			t.Fatalf("%s: %v", one.statement, err)
		}
	}

	reported := []progressLine{}

	ran.reset()

	if err := ensureCallIndexSet(ctx, s, func(level, message string) {
		reported = append(reported, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("ensureCallIndexSet: %v", err)
	}

	for _, statement := range ran.queries {
		if strings.Contains(strings.ToUpper(statement), "DROP") {
			t.Errorf("the step ran %s, and no statement of the step destroys an index", statement)
		}
	}

	for _, one := range planted {
		got := indexDefinitions(t, db, one.table)

		if !slices.Contains(got, one.statement) {
			t.Errorf("after the step %s carries (%s), want it to still carry %s",
				one.table, strings.Join(got, ", "), one.statement)
		}
	}

	// A key that shares only the first column of the covering key's list does not carry that
	// list, so the covering key is created beside it and the planted key keeps its own shape.
	// The database holds the key it made under the plain spelling, without the guard the step
	// asked with.
	if got := indexDefinitions(t, db, "calls"); !slices.Contains(got,
		`CREATE INDEX "calls_timestamp_cover" ON "calls" ("timestamp","callId","audioLength","systemId","talkgroupId")`) {
		t.Errorf("after the step calls carries (%s), want the covering key beside the planted calls_order_pair",
			strings.Join(got, ", "))
	}

	// The name is taken, so the member is not created beside it: the table carries no second
	// key, and the member is still the one the probe reports as missing.
	carries, err := hasCallIndexShape(ctx, s, "sites")
	if err != nil {
		t.Fatalf("hasCallIndexShape(sites): %v", err)
	}

	if got := indexShapes(carries); !slices.Equal(got, []string{"sites_system(siteRef)"}) {
		t.Errorf("after the step sites carries (%s), want only the planted sites_system(siteRef)",
			strings.Join(got, ", "))
	}

	if missing, err := missingCallIndexes(ctx, s); err != nil {
		t.Fatalf("missingCallIndexes: %v", err)
	} else if got := missingNames(missing); !slices.Equal(got, []string{"sites_system"}) {
		t.Errorf("after the step the probe reports (%s) missing, want sites_system alone: its name is "+
			"already held by another key, and the step creates nothing over an occupied name", strings.Join(got, ", "))
	}

	warns := []progressLine{}

	for _, line := range reported {
		if line.level == domain.LogLevelWarn {
			warns = append(warns, line)
		}
	}

	want := []progressLine{
		{level: domain.LogLevelWarn, message: `the name sites_system already belongs to an index on sites over (siteRef): the call history key over (systemId) is not created`},
	}

	if !slices.Equal(warns, want) {
		t.Errorf("the step reported %v for the occupied name, want %v", warns, want)
	}
}

// A table the set names that is not in the database at all ends that member's part of the step
// doing nothing, the way the search index step leaves an absent calls table alone: a migration
// step is not the place to decide that a table should exist. What the rest of the set still lacks
// is created as usual.
func TestEnsureCallIndexSetOnADatabaseMissingOneOfTheSetTables(t *testing.T) {
	db, ran := openCountedSchemaDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	if _, err := db.Exec(`DROP TABLE "sites"`); err != nil {
		t.Fatalf("DROP TABLE sites: %v", err)
	}

	if err := ensureCallAudioLengthColumn(ctx, s); err != nil {
		t.Fatalf("ensureCallAudioLengthColumn: %v", err)
	}

	reported := []progressLine{}

	ran.reset()

	if err := ensureCallIndexSet(ctx, s, func(level, message string) {
		reported = append(reported, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("ensureCallIndexSet on a database with no sites table: %v", err)
	}

	if got := ran.count("CREATE INDEX"); got != 1 {
		t.Fatalf("the step ran %d create statements over a database with no sites table, want the covering "+
			"key alone: %v", got, ran.queries)
	}

	if len(reported) != 1 || reported[0].level != domain.LogLevelInfo ||
		!strings.HasPrefix(reported[0].message, "created the call history index calls_timestamp_cover") {
		t.Errorf("the step reported %v, want one line naming the covering key and nothing about the "+
			"table that is not there", reported)
	}

	// The member that belongs to the absent table is still the one the probe reports as missing,
	// and it is reported as missing by the next start too: the step neither made the table nor
	// complained about it.
	if missing, err := missingCallIndexes(ctx, s); err != nil {
		t.Fatalf("missingCallIndexes: %v", err)
	} else if got := missingNames(missing); !slices.Equal(got, []string{"sites_system"}) {
		t.Errorf("after the step the probe reports (%s) missing, want sites_system alone", strings.Join(got, ", "))
	}
}

// The statement the step builds for one member carries the quoting every copy uses and the
// `IF NOT EXISTS` spelling every backend but MySQL accepts: that one engine has no such spelling,
// which is why the key it is handed is asked for plainly, the way the options key index is.
func TestCallIndexSetCreateStatementUsesTheSpellingTheDialectAccepts(t *testing.T) {
	member := indexSpec{name: "talkgroups_system_ref", table: "talkgroups", columns: []string{"systemId", "talkgroupRef"}}

	quoted := `CREATE INDEX IF NOT EXISTS "talkgroups_system_ref" ON "talkgroups" ("systemId","talkgroupRef")`

	for _, one := range []struct {
		dialect Dialect
		want    string
	}{
		{dialect: DialectSqlite, want: quoted},
		{dialect: DialectMariadb, want: quoted},
		{dialect: DialectPostgresql, want: quoted},
		{dialect: DialectMysql, want: `CREATE INDEX "talkgroups_system_ref" ON "talkgroups" ("systemId","talkgroupRef")`},
	} {
		if got := callIndexCreateStatement(one.dialect, member); got != one.want {
			t.Errorf("%s builds %s, want %s", one.dialect, got, one.want)
		}
	}
}

// indexDefinitions reads the statements that made the indexes of one table, in the words the
// database holds them in, so a test can say an index was left exactly as it was.
func indexDefinitions(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()

	rows, err := db.Query(`SELECT "sql" FROM "sqlite_master" WHERE "type" = 'index' AND "tbl_name" = ? AND "sql" IS NOT NULL ORDER BY "name"`, table)
	if err != nil {
		t.Fatalf("reading the indexes of %s: %v", table, err)
	}

	defer rows.Close()

	definitions := []string{}

	for rows.Next() {
		var definition string

		if err := rows.Scan(&definition); err != nil {
			t.Fatalf("scanning an index definition of %s: %v", table, err)
		}

		definitions = append(definitions, definition)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the indexes of %s: %v", table, err)
	}

	return definitions
}

func missingNames(indexes []indexSpec) []string {
	names := []string{}

	for _, index := range indexes {
		names = append(names, index.name)
	}

	return names
}

// indexShapes writes each index the probe reported as the name it carries with the columns
// it carries in index order, sorted, so that two catalogs can be compared as two lists.
func indexShapes(indexes []indexSpec) []string {
	shapes := []string{}

	for _, index := range indexes {
		shapes = append(shapes, index.name+"("+strings.Join(index.columns, ", ")+")")
	}

	slices.Sort(shapes)

	return shapes
}

// indexNames reads a list of index definitions aloud, which is how a failure that
// compares two lists has to be written to be readable.
func indexNames(indexes []indexSpec) string {
	names := []string{}

	for _, index := range indexes {
		names = append(names, index.name)
	}

	return strings.Join(names, ", ")
}
