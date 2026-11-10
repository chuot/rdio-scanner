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
	"fmt"
	"slices"
	"strings"
	"testing"

	"rdio-scanner/internal/application/ports"
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
		t.Errorf("the step did not run %s: it ran %v", want_create, ran.statements())
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

	for _, statement := range ran.statements() {
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
			"key alone: %v", got, ran.statements())
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

// The statements the calls table is expected to carry once the set has been applied: the pair key the
// server shipped with, which keeps the column list it shipped with, and the covering key the set adds.
const shippedPairDefinition = `CREATE INDEX "calls_idx" ON "calls" ("systemId","siteRef","talkgroupId","timestamp")`

const coveringKeyDefinition = `CREATE INDEX "calls_timestamp_cover" ON "calls" ("timestamp","callId","audioLength","systemId","talkgroupId")`

// The state an upgrade is in: a database the legacy server wrote, carrying its own pair index and the
// rows that index was built for, and none of the set. That fixture declares foreign keys but creates
// one index, and a foreign key on SQLite makes no index of its own, so every member has to be created
// there, and the shipped pair index has to come out of the step holding the columns it went in with.
func TestCallIndexSetStepRunsOverTheLegacySchema(t *testing.T) {
	db, ran := openCountedLegacyDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	// The recording-length column stage runs first: the calls table the legacy server wrote does not
	// carry the column the covering key indexes, and the key has to be built after it.
	if err := ensureCallAudioLengthColumn(ctx, s); err != nil {
		t.Fatalf("ensureCallAudioLengthColumn: %v", err)
	}

	if missing, err := missingCallIndexes(ctx, s); err != nil {
		t.Fatalf("missingCallIndexes: %v", err)
	} else if got := missingNames(missing); !slices.Equal(got, setMemberNames()) {
		t.Fatalf("before the step the probe reports (%s) missing, want the whole set in the order it is "+
			"documented", strings.Join(got, ", "))
	}

	reported := []progressLine{}

	ran.reset()

	if err := ensureCallIndexSet(ctx, s, func(level, message string) {
		reported = append(reported, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("ensureCallIndexSet over the legacy-written database: %v", err)
	}

	want := []progressLine{}

	for _, member := range callIndexSet {
		want = append(want, createdLine(member))
	}

	if !slices.Equal(reported, want) {
		t.Errorf("the step over the legacy-written database reported %v, want one line for each of the ten "+
			"members in the order the set names them", reported)
	}

	if got := ran.count("CREATE INDEX"); got != len(callIndexSet) {
		t.Errorf("the step ran %d create statements over the legacy-written database, want one for each of the %d members: %v",
			got, len(callIndexSet), ran.statements())
	}

	if found := statementsLike(ran, "DROP"); len(found) != 0 {
		t.Errorf("the step ran %v, want no statement that drops anything", found)
	}

	if missing, err := missingCallIndexes(ctx, s); err != nil {
		t.Fatalf("missingCallIndexes after the step: %v", err)
	} else if got := missingNames(missing); len(got) != 0 {
		t.Errorf("after the step the probe still reports (%s) missing, want the whole set present",
			strings.Join(got, ", "))
	}

	// The pass runs on every start: the next one reads the catalog, finds the set, and says and does
	// nothing.
	again := []progressLine{}

	ran.reset()

	if err := ensureCallIndexSet(ctx, s, func(level, message string) {
		again = append(again, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("ensureCallIndexSet a second time: %v", err)
	}

	if len(again) != 0 {
		t.Errorf("the second run reported %v, want nothing", again)
	}

	if got := ran.count("CREATE INDEX"); got != 0 {
		t.Errorf("the second run created %d indexes, want none: %v", got, ran.statements())
	}

	// The pair index the server shipped with keeps the column list it shipped with, and the covering
	// key is the only key added to that table.
	if got := indexDefinitions(t, db, "calls"); !slices.Equal(got, []string{shippedPairDefinition, coveringKeyDefinition}) {
		t.Errorf("after the step the calls table carries (%s), want the shipped pair key unchanged beside "+
			"the covering key", strings.Join(got, ", "))
	}
}

// The pair index every database this server upgrades carries is left alone by name: the probe reports
// it with the columns it holds them in, the step creates nothing beside a key it found, and the only
// key that reaches that table is the covering key the set names.
func TestShippedPairIndexIsFoundByItsNameAndLeftAlone(t *testing.T) {
	db, ran := openCountedLegacyDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	// The shipped name holding a column list the set does not name, which is the shape an earlier
	// release's key leaves behind.
	planted := `CREATE INDEX "calls_idx" ON "calls" ("systemId","siteRef")`

	for _, statement := range []string{`DROP INDEX "calls_idx"`, planted} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	if err := ensureCallAudioLengthColumn(ctx, s); err != nil {
		t.Fatalf("ensureCallAudioLengthColumn: %v", err)
	}

	ran.reset()

	if err := ensureCallIndexSet(ctx, s, func(string, string) {}); err != nil {
		t.Fatalf("ensureCallIndexSet: %v", err)
	}

	carries, err := hasCallIndexShape(ctx, s, "calls")
	if err != nil {
		t.Fatalf("hasCallIndexShape(calls): %v", err)
	}

	// The key the step found is reported with its own name and its own columns, and the covering key is
	// the member that was not served by it.
	if got := indexShapes(carries); !slices.Equal(got, []string{
		"calls_idx(systemId, siteRef)",
		"calls_timestamp_cover(timestamp, callId, audioLength, systemId, talkgroupId)",
	}) {
		t.Errorf("after the step the probe reports calls carrying (%s), want the planted calls_idx(systemId, "+
			"siteRef) beside the covering key", strings.Join(got, ", "))
	}

	if got := indexDefinitions(t, db, "calls"); !slices.Equal(got, []string{planted, coveringKeyDefinition}) {
		t.Errorf("after the step the calls table carries (%s), want the planted key byte for byte beside the "+
			"covering key", strings.Join(got, ", "))
	}

	if got := ran.count("CREATE INDEX"); got != len(callIndexSet) {
		t.Errorf("the step ran %d create statements, want the covering key beside the nine keys the schema copies "+
			"declare, none of them the shipped key", got)
	}

	// Nothing the step built carries the shipped key's name: the key the probe found is left alone, not
	// rebuilt beside itself.
	for _, statement := range ran.statements() {
		if strings.HasPrefix(statement, "CREATE INDEX") && strings.Contains(statement, `"calls_idx"`) {
			t.Errorf("the step ran %s, want no statement that builds the shipped key again beside the one the "+
				"database already carries", firstLineOf(statement))
		}
	}

	if found := statementsLike(ran, "DROP"); len(found) != 0 {
		t.Errorf("the step ran %v, want no statement that drops the key it found", found)
	}

	// Nothing in the list of statements the step builds drops a key of its own, in any dialect.
	for _, dialect := range []Dialect{DialectSqlite, DialectMariadb, DialectMysql, DialectPostgresql} {
		for _, member := range callIndexSet {
			if statement := callIndexCreateStatement(dialect, member); strings.Contains(strings.ToUpper(statement), "DROP") {
				t.Errorf("%s builds %s for %s, want a create that drops nothing", dialect, statement, member.name)
			}
		}
	}
}

// The history page read the repository issues over a database whose calls table carries the covering
// key walks that key, and the migrated rows answer their recording length as the value the column
// stage gave them. A key built over a table that does not carry its column yet is created without
// complaint, reports the right name and the right column list afterwards, and answers that column's
// own name for every row written before the column was added, so the assertion that carries the
// weight is the one on the value a row answers.
func TestMigratedCallsTableReadsItsRecordingLengthThroughTheCoveringKey(t *testing.T) {
	db := openLegacyDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	// The startup pass adds the keys its copied statements declare and the column the covering key
	// carries; the set's step then adds that key. That order is the one the pass runs them in, and it
	// is what keeps a row an earlier server wrote readable.
	if lines := migrateLines(t, db); len(lines) != 0 {
		t.Errorf("the pass reported %v over a database with nothing to migrate", lines)
	}

	if err := ensureCallIndexSet(ctx, s, func(string, string) {}); err != nil {
		t.Fatalf("ensureCallIndexSet: %v", err)
	}

	if got := indexDefinitions(t, db, "calls"); !slices.Contains(got, coveringKeyDefinition) {
		t.Fatalf("after the pass and the step the calls table carries (%s), want the covering key beside "+
			"the pair key it shipped with", strings.Join(got, ", "))
	}

	page, err := NewCalls(db, DialectSqlite).SearchCalls(ctx, ports.CallSearch{Limit: 200})
	if err != nil {
		t.Fatalf("the history page over migrated rows: %v: a page that cannot read the recording length is "+
			"what a covering key built before that column existed answers", err)
	}

	// The fixture holds two recorded calls, one of them still waiting out a delay, so the history
	// holds the other one.
	if page.Count != 1 || len(page.Results) != 1 {
		t.Fatalf("the history page over the migrated rows reports %d of %d calls, want the one call that is "+
			"not waiting out a delay", len(page.Results), page.Count)
	}

	for _, one := range page.Results {
		if one.AudioLength != 0 {
			t.Errorf("the migrated call %d answers its recording length as %v, want the value the column "+
				"stage gave every row the earlier server wrote", one.ID, one.AudioLength)
		}
	}

	statement, args := historyPageRead()

	if details := queryPlan(t, db, statement, args...); !hasString(
		details, "SCAN c USING COVERING INDEX calls_timestamp_cover") {
		t.Errorf("the history page read plans as %v, want it to walk the covering key instead of the "+
			"table", details)
	}
}

// A covering key built over a table that does not carry its column yet is the shape the startup pass
// must never reach: the create is accepted, the key afterwards carries the set's own name and, once
// the column stage has added its column, the set's own column list, and its plan is the same plan the
// correct key gets, while every row written before the column was added answers that column's name
// instead of a length.
func TestCoveringKeyBuiltBeforeItsColumnIsAddedAnswersTheColumnNameForEarlierRows(t *testing.T) {
	db := openLegacyDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	var covering indexSpec

	for _, member := range callIndexSet {
		if member.table == "calls" {
			covering = member
		}
	}

	if _, err := db.Exec(callIndexCreateStatement(DialectSqlite, covering)); err != nil {
		t.Fatalf("the covering key over a table without its column: %v", err)
	}

	// The create is accepted, and the key the database holds afterwards carries the set's own name and
	// the set's own columns.
	if got := indexDefinitions(t, db, "calls"); !slices.Contains(got, coveringKeyDefinition) {
		t.Fatalf("the calls table after a create over a table without its column carries (%s), want the "+
			"covering key the set names", strings.Join(got, ", "))
	}

	// The probe does not report that key as the member it names: the column the key indexes is not a
	// column of the table yet, so the stored key does not carry the set's column list, and the whole set
	// reads as missing.
	missing, err := missingCallIndexes(ctx, s)
	if err != nil {
		t.Fatalf("missingCallIndexes: %v", err)
	}

	if got := missingNames(missing); !slices.Equal(got, setMemberNames()) {
		t.Fatalf("the probe reports (%s) missing over a key built too early, want the whole set: the key the "+
			"create made is not the member the set names until that member's column is a column of the table",
			strings.Join(got, ", "))
	}

	if err := ensureCallAudioLengthColumn(ctx, s); err != nil {
		t.Fatalf("ensureCallAudioLengthColumn: %v", err)
	}

	// Once the column exists the stored key does carry the set's column list, and the probe recognises
	// it: a step that ran after the column stage would find it present and create nothing beside it. So
	// the catalogue is not what tells this key from the right one, and neither is a read of the table.
	if missing, err := missingCallIndexes(ctx, s); err != nil {
		t.Fatalf("missingCallIndexes after the column stage: %v", err)
	} else {
		want := []string{}

		for _, name := range setMemberNames() {
			if name != covering.name {
				want = append(want, name)
			}
		}

		if got := missingNames(missing); !slices.Equal(got, want) {
			t.Fatalf("the probe reports (%s) missing after the column stage, want every member but the covering "+
				"key: the key is recognised by its name and its column list", strings.Join(got, ", "))
		}
	}

	// The rows themselves read the default the column stage gave them, so a test that reads the table
	// cannot tell this key from the right one either.
	if got := rowsOf(t, db, "calls", "callId, audioLength"); !equalStrings(flat(got), []string{"1|0", "2|0"}) {
		t.Fatalf("the calls rows answer (%v), want the default the column stage gave them", flat(got))
	}

	if _, err := NewCalls(db, DialectSqlite).SearchCalls(ctx, ports.CallSearch{Limit: 200}); err == nil {
		t.Errorf("the history page over rows written before the column stage answers a recording length, want " +
			"the read to fail: every one of those rows carries the column's own name in the key the page walks")
	} else if !strings.Contains(err.Error(), "audioLength") {
		t.Errorf("the history page over those rows fails with %v, want the failure to name the recording-length "+
			"column", err)
	}

	statement, args := historyPageRead()

	if details := queryPlan(t, db, statement, args...); !hasString(
		details, "SCAN c USING COVERING INDEX calls_timestamp_cover") {
		t.Errorf("the read over the key built too early plans as %v: the plan is the same one the correct key "+
			"gets, which is why the assertion that matters is on the value a row answers", details)
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

// setMemberNames names every member of the set, in the order the set documents them: the order a step
// that fills in a database an earlier server wrote reports the keys it creates in.
func setMemberNames() []string {
	names := []string{}

	for _, member := range callIndexSet {
		names = append(names, member.name)
	}

	return names
}

// createdLine writes the line the step reports for one member it created.
func createdLine(member indexSpec) progressLine {
	return progressLine{
		level: domain.LogLevelInfo,
		message: fmt.Sprintf("created the call history index %s on %s over (%s)",
			member.name, member.table, strings.Join(member.columns, ", ")),
	}
}

// historyPageRead builds the history page statement the repository issues, with the arguments an
// unfiltered page hands it, so a test can ask the database how it would run that read.
func historyPageRead() (string, []any) {
	where, args := callSearchFilters(ports.CallSearch{Limit: 200})

	from := callHistoryFrom + ` WHERE ` + where

	statement := `SELECT c."callId", c."timestamp", c."audioLength", s."systemRef", t."talkgroupRef" ` +
		from + ` ORDER BY c."timestamp" DESC LIMIT ? OFFSET ?`

	return statement, append(append([]any{}, args...), 200, 0)
}

// queryPlan asks the database how it would run one statement and returns each step of the answer, so a
// test can name the index a read walks.
func queryPlan(t *testing.T, db *sql.DB, statement string, args ...any) []string {
	t.Helper()

	rows, err := db.Query(`EXPLAIN QUERY PLAN `+statement, args...)
	if err != nil {
		t.Fatalf("explaining %s: %v", firstLineOf(statement), err)
	}

	defer rows.Close()

	details := []string{}

	for rows.Next() {
		var (
			id, parent, detailUnused int
			detail                   string
		)

		if err := rows.Scan(&id, &parent, &detailUnused, &detail); err != nil {
			t.Fatalf("reading a step of the plan of %s: %v", firstLineOf(statement), err)
		}

		details = append(details, detail)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the plan of %s: %v", firstLineOf(statement), err)
	}

	return details
}
