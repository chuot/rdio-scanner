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
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

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

// coveringKeyMember is the set's key on the calls table, the one member the copied schema
// statements do not declare. A database that already carries the other nine gains exactly that
// one from the startup step.
func coveringKeyMember() indexSpec {
	for _, member := range callIndexSet {
		if member.table == "calls" {
			return member
		}
	}

	return indexSpec{name: "calls_timestamp_cover", table: "calls"}
}

// createdCoveringKeyLine is the one line a database that already carries the other nine call history
// keys hears from the startup pass.
var createdCoveringKeyLine = createdLine(coveringKeyMember()).message

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
	// carries, and because the pass runs the set's own step the line naming that key is all it says
	// over a database with nothing to migrate. Running the step again afterwards is what a restart
	// does, and it has nothing left to add and nothing to say.
	lines := migrateLines(t, db)

	wantLines := []progressLine{{level: domain.LogLevelInfo, message: createdCoveringKeyLine}}

	if len(lines) != len(wantLines) {
		t.Fatalf("the pass reported %v over a database with nothing to migrate, want only the line naming "+
			"the covering key it adds", lines)
	}

	for i := range wantLines {
		if lines[i] != wantLines[i] {
			t.Errorf("line %d:\n  got  %q under %q\n  want %q under %q",
				i+1, lines[i].message, lines[i].level, wantLines[i].message, wantLines[i].level)
		}
	}

	reported := []progressLine{}

	if err := ensureCallIndexSet(ctx, s, func(level, message string) {
		reported = append(reported, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("ensureCallIndexSet: %v", err)
	}

	if len(reported) != 0 {
		t.Errorf("the set's step over a database the pass already brought up reported: %v, want it to read "+
			"the catalog and add nothing", reported)
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

// The statement inventory kept here is composed from the same pieces the repository's own
// read-path builders compose a real history read from: the joins, the filters every history read
// carries on its own, the projection each read answers with, and the ordering and limit it ends with.
// A test can therefore compare what the inventory records against what those builders answer now,
// rather than against a copy of a statement an earlier run wrote down.

// inventoriedStatement is one statement the inventory carries: the name the measurement gives it, the
// repository method that issues it, whether it reads or writes, and the statement itself.
type inventoriedStatement struct {
	label     string
	probe     string
	kind      string
	statement string
}

// unmeasuredStatement is one statement a measured piece of work issues that no measurement times,
// with what leaves it out of the inventoried list.
type unmeasuredStatement struct {
	probe     string
	statement string
	reason    string
}

// historyReadShape is one read a history search issues: what the measurement calls it beside its
// search's own name, the projection it answers with, and the ordering and limit it ends with.
type historyReadShape struct {
	suffix  string
	columns string
	tail    string
}

// unmeasuredRead is one read a measured search issues beside the reads a measurement times.
type unmeasuredRead struct {
	shape  historyReadShape
	reason string
}

// historySearchWork is one history search the measurements time: the criteria the query handler runs,
// the filter those criteria add to the ones every history read carries, the reads timed for it, and
// the reads it issues beside them.
type historySearchWork struct {
	label    string
	criteria ports.CallSearch
	filters  string
	reads    []historyReadShape
	extra    []unmeasuredRead
}

// historySpan is the dated range the windowed reads are measured against, taken from the calls the
// test fixture holds.
type historySpan struct {
	newest  time.Time
	dayFrom time.Time
}

const (
	// inventoryHistoryJoin names the joins every history read repeats, which the repository states
	// once for the whole history.
	inventoryHistoryJoin = `FROM "calls" AS c ` +
		`LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" ` +
		`LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" ` +
		`LEFT JOIN "delayed" AS d ON d."callId" = c."callId"`

	// inventoryBaseFilters are the filters every history read carries, whatever its caller asks for.
	inventoryBaseFilters = `c."systemId" > 0 AND c."talkgroupId" > 0 AND ` +
		`s."systemRef" IS NOT NULL AND t."talkgroupRef" IS NOT NULL AND d."callId" IS NULL`

	// The three projections a history search answers with, and the four orderings its reads end with.
	inventorySpanColumns  = `SELECT c."timestamp"`
	inventoryCountColumns = `SELECT COUNT(*)`
	inventoryPageColumns  = `SELECT c."callId", c."timestamp", c."audioLength", s."systemRef", t."talkgroupRef"`

	inventorySpanOldestTail      = ` ORDER BY c."timestamp" ASC LIMIT 1`
	inventorySpanNewestTail      = ` ORDER BY c."timestamp" DESC LIMIT 1`
	inventoryPageTail            = ` ORDER BY c."timestamp" DESC LIMIT ? OFFSET ?`
	inventoryOldestFirstPageTail = ` ORDER BY c."timestamp" ASC LIMIT ? OFFSET ?`
)

// The filter each measured search adds to the ones the history carries on its own, one per search.
const (
	inventoryBareHistoryFilter     = ``
	inventoryDayFilter             = `c."timestamp" >= ? AND c."timestamp" <= ?`
	inventorySystemFilter          = `(s."systemRef" = ?)`
	inventorySystemTalkgroupFilter = `(s."systemRef" = ? AND t."talkgroupRef" = ?)`
	inventoryTagPairFilter         = `((s."systemRef" = ? AND t."talkgroupRef" IN (?, ?)) OR ` +
		`(s."systemRef" = ? AND t."talkgroupRef" IN (?)))`
	inventoryGroupPairFilter = `((s."systemRef" = ? AND t."talkgroupRef" IN (?, ?, ?)) OR ` +
		`(s."systemRef" = ? AND t."talkgroupRef" IN (?)))`
	inventoryAccessScopeFilter = `((s."systemRef" = ? AND t."talkgroupRef" IN (?, ?)) OR s."systemRef" = ?)`
	inventoryUnmatchedFilter   = `(1 = 0)`
)

// everyHistoryRead and pageOnlyHistoryRead are the reads a history search issues: the search's oldest
// and newest call, how many calls it holds, and one page of it. A caller that asks for a page and
// nothing else issues that page alone, and the measurement names such a read after the caller rather
// than after the shape of the read, so it carries no suffix of its own.
var (
	everyHistoryRead = []historyReadShape{
		{suffix: " span oldest", columns: inventorySpanColumns, tail: inventorySpanOldestTail},
		{suffix: " span newest", columns: inventorySpanColumns, tail: inventorySpanNewestTail},
		{suffix: " count", columns: inventoryCountColumns, tail: ""},
		{suffix: " page 25", columns: inventoryPageColumns, tail: inventoryPageTail},
	}

	pageOnlyHistoryRead = []historyReadShape{
		{columns: inventoryPageColumns, tail: inventoryPageTail},
	}
)

// The systems, talkgroups, and access scope the measured searches run against, taken from what the
// test fixture stores. Each list is as short as the fixture allows while still standing for more than
// one system and more than one talkgroup on a system, so every filter shape the search builders have
// is measured.
const (
	historySystemRef    uint = 10001
	historyTalkgroupRef uint = 1514000
)

func historyTagScopes() []domain.TalkgroupScope {
	return []domain.TalkgroupScope{
		{SystemRef: historySystemRef, TalkgroupRefs: []uint{1514000, 1514001}},
		{SystemRef: 10002, TalkgroupRefs: []uint{1520000}},
	}
}

func historyGroupScopes() []domain.TalkgroupScope {
	return []domain.TalkgroupScope{
		{SystemRef: historySystemRef, TalkgroupRefs: []uint{1514000, 1514001, 1514002}},
		{SystemRef: 10002, TalkgroupRefs: []uint{1520000}},
	}
}

func historyAccessScope() domain.AccessScope {
	return domain.AccessScope{
		Present: true,
		Systems: []domain.ScopedSystem{
			{SystemRef: historySystemRef, TalkgroupsGiven: true, TalkgroupRefs: []uint{1514000, 1514001}},
			{SystemRef: 10002, TalkgroupsWildcard: true},
		},
	}
}

// historyRead composes one inventoried statement from the pieces above, the way the repository's own
// filter builder joins its filters into the history's WHERE clause.
func historyRead(columns, filters, tail string) string {
	where := inventoryBaseFilters

	if filters != "" {
		where += ` AND ` + filters
	}

	return columns + ` ` + inventoryHistoryJoin + ` WHERE ` + where + tail
}

// historySearchStatement composes one read of a measured search.
func historySearchStatement(work historySearchWork, shape historyReadShape) string {
	return historyRead(shape.columns, work.filters, shape.tail)
}

// callHistoryStatementInventory returns the whole statement inventory: the reads the measured
// history searches issue, and the reads and writes those searches do not stand for.
func callHistoryStatementInventory(span historySpan) []inventoriedStatement {
	out := []inventoriedStatement{}

	for _, work := range historySearchWorks(span) {
		for _, shape := range work.reads {
			out = append(out, inventoriedStatement{
				label:     work.label + shape.suffix,
				probe:     "SearchCalls",
				kind:      "read",
				statement: historySearchStatement(work, shape),
			})
		}
	}

	return append(out, historiedReadsAndWrites()...)
}

// statementsOutsideTheReports returns every statement a measured work issues that no measurement
// times: the reads a search issues beside the reads timed for it, and the statements of the
// repository reads a measurement touches only part of.
func statementsOutsideTheReports(span historySpan) []unmeasuredStatement {
	out := []unmeasuredStatement{}

	for _, work := range historySearchWorks(span) {
		for _, extra := range work.extra {
			out = append(out, unmeasuredStatement{
				probe:     "SearchCalls",
				statement: historySearchStatement(work, extra.shape),
				reason:    extra.reason,
			})
		}
	}

	return append(out, historiedStatementsOutsideTheReports()...)
}

// historySearchWorks is the table of the history searches the reports time: the criteria the
// query handler runs, the filter that criteria adds to the ones every history read carries on
// its own, the reads a report times for it, and the reads it issues beside those.
func historySearchWorks(span historySpan) []historySearchWork {
	return []historySearchWork{
		{
			label:    "all",
			criteria: ports.CallSearch{},
			filters:  inventoryBareHistoryFilter,
			reads:    everyHistoryRead,
		},
		{
			label:    "day",
			criteria: ports.CallSearch{From: span.dayFrom, To: span.newest},
			filters:  inventoryDayFilter,
			reads:    everyHistoryRead,
		},
		{
			label:    "system",
			criteria: ports.CallSearch{SystemRef: historySystemRef},
			filters:  inventorySystemFilter,
			reads:    everyHistoryRead,
		},
		{
			label:    "system talkgroup",
			criteria: ports.CallSearch{SystemRef: historySystemRef, TalkgroupRef: historyTalkgroupRef},
			filters:  inventorySystemTalkgroupFilter,
			reads:    everyHistoryRead,
		},
		{
			label:    "tag scope",
			criteria: ports.CallSearch{TagTalkgroups: historyTagScopes()},
			filters:  inventoryTagPairFilter,
			reads:    everyHistoryRead,
		},
		{
			label:    "group scope",
			criteria: ports.CallSearch{GroupTalkgroups: historyGroupScopes()},
			filters:  inventoryGroupPairFilter,
			reads:    everyHistoryRead,
		},
		{
			label:    "search tag window",
			criteria: ports.CallSearch{From: span.dayFrom, To: span.newest, Limit: 25, TagTalkgroups: historyTagScopes()},
			filters:  inventoryTagPairFilter + " AND " + inventoryDayFilter,
			reads:    everyHistoryRead,
		},
		{
			label:    "search talkgroup window",
			criteria: ports.CallSearch{From: span.dayFrom, To: span.newest, Limit: 25, SystemRef: historySystemRef, TalkgroupRef: historyTalkgroupRef},
			filters:  inventorySystemTalkgroupFilter + " AND " + inventoryDayFilter,
			reads:    everyHistoryRead,
		},
		{
			label:    "all page 25 offset 1000",
			criteria: ports.CallSearch{Limit: 25, Offset: 1000},
			filters:  inventoryBareHistoryFilter,
			reads:    pageOnlyHistoryRead,
		},
		{
			label:    "access scope page",
			criteria: ports.CallSearch{Scope: historyAccessScope(), Limit: 25},
			filters:  inventoryAccessScopeFilter,
			reads:    pageOnlyHistoryRead,
			extra: []unmeasuredRead{
				{shape: historyReadShape{suffix: " span oldest", columns: inventorySpanColumns, tail: inventorySpanOldestTail},
					reason: "the measurement times a caller reading under its own access scope as a page alone; this read takes that caller’s filter and reaches the same keys as the page it times"},
				{shape: historyReadShape{suffix: " span newest", columns: inventorySpanColumns, tail: inventorySpanNewestTail},
					reason: "the measurement times a caller reading under its own access scope as a page alone; this read takes that caller’s filter and reaches the same keys as the page it times"},
				{shape: historyReadShape{suffix: " count", columns: inventoryCountColumns, tail: ""},
					reason: "the measurement times a caller reading under its own access scope as a page alone; this read takes that caller’s filter and reaches the same keys as the page it times"},
			},
		},
		{
			label:    "oldest first page",
			criteria: ports.CallSearch{Limit: 25, Ascending: true},
			filters:  inventoryBareHistoryFilter,
			reads:    nil,
			extra: []unmeasuredRead{
				{shape: historyReadShape{suffix: " page 25 oldest first", columns: inventoryPageColumns, tail: inventoryOldestFirstPageTail},
					reason: "the reports time the history page newest first; the tools that walk the history from its oldest end ask for the same page oldest first, and no report times that page"},
			},
		},
		{
			label:    "empty scope",
			criteria: ports.CallSearch{Scope: domain.AccessScope{Present: true}, Limit: 25},
			filters:  inventoryUnmatchedFilter,
			reads:    nil,
			extra: []unmeasuredRead{
				{shape: historyReadShape{suffix: " span oldest", columns: inventorySpanColumns, tail: inventorySpanOldestTail},
					reason: "the measurement times a caller whose access names nothing as a page alone; this read takes the same filter as the page it times"},
				{shape: historyReadShape{suffix: " span newest", columns: inventorySpanColumns, tail: inventorySpanNewestTail},
					reason: "the measurement times a caller whose access names nothing as a page alone; this read takes the same filter as the page it times"},
				{shape: historyReadShape{suffix: " count", columns: inventoryCountColumns, tail: ""},
					reason: "the measurement times a caller whose access names nothing as a page alone; this read takes the same filter as the page it times"},
				{shape: historyReadShape{suffix: " page 25", columns: inventoryPageColumns, tail: inventoryPageTail},
					reason: "the measurement times a caller whose access names nothing as a page alone; this read takes the same filter as the page it times"},
			},
		},
	}
}

// historiedReadsAndWrites returns the inventoried reads and writes no history search stands
// for: each is a statement the measurement names, issued by the repository method the entry
// names, as that method issues it now.
func historiedReadsAndWrites() []inventoriedStatement {
	return []inventoriedStatement{
		{
			label: "status count 2m",
			probe: "CountCallsSince",
			kind:  "read",
			statement: `SELECT COUNT(*) FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT ` +
				`JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" LEFT JOIN "delayed" AS d ON ` +
				`d."callId" = c."callId" WHERE d."callId" IS NULL AND c."timestamp" >= ?`,
		},
		{
			label: "status hours 24h",
			probe: "CountCallsByHour",
			kind:  "read",
			statement: `SELECT c."timestamp" / 3600000 AS "bucket", COUNT(*) FROM "calls" AS c LEFT JOIN "systems" AS ` +
				`s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = ` +
				`c."talkgroupId" LEFT JOIN "delayed" AS d ON d."callId" = c."callId" WHERE d."callId" IS NULL ` +
				`AND c."timestamp" >= ? GROUP BY "bucket" ORDER BY "bucket" ASC`,
		},
		{
			label: "status tallies 15m",
			probe: "CountCallsByTalkgroup",
			kind:  "read",
			statement: `SELECT s."systemRef", t."talkgroupRef", t."name", t."label", COUNT(*) FROM "calls" AS c LEFT ` +
				`JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON ` +
				`t."talkgroupId" = c."talkgroupId" LEFT JOIN "delayed" AS d ON d."callId" = c."callId" WHERE ` +
				`d."callId" IS NULL AND c."timestamp" >= ? GROUP BY s."systemRef", t."talkgroupRef", t."name", ` +
				`t."label" ORDER BY COUNT(*) DESC, t."talkgroupRef" ASC LIMIT ?`,
		},
		{
			label: "recent 10",
			probe: "RecentCalls",
			kind:  "read",
			statement: `SELECT c."callId", c."timestamp", c."audioLength", s."systemRef", t."talkgroupRef" FROM ` +
				`"calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS ` +
				`t ON t."talkgroupId" = c."talkgroupId" LEFT JOIN "delayed" AS d ON d."callId" = c."callId" ` +
				`WHERE d."callId" IS NULL ORDER BY c."timestamp" DESC, c."callId" DESC LIMIT ?`,
		},
		{
			label: "recent 200",
			probe: "RecentCalls",
			kind:  "read",
			statement: `SELECT c."callId", c."timestamp", c."audioLength", s."systemRef", t."talkgroupRef" FROM ` +
				`"calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS ` +
				`t ON t."talkgroupId" = c."talkgroupId" LEFT JOIN "delayed" AS d ON d."callId" = c."callId" ` +
				`WHERE d."callId" IS NULL ORDER BY c."timestamp" DESC, c."callId" DESC LIMIT ?`,
		},
		{
			label: "call by id",
			probe: "GetCall",
			kind:  "read",
			statement: `SELECT c."audio", c."audioFilename", c."audioMime", c."audioLength", c."siteRef", ` +
				`c."timestamp", c."systemId", c."talkgroupId", s."systemRef", t."talkgroupRef", t."label", ` +
				`t."name", s."delay", t."delay" FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = ` +
				`c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" WHERE c."callId" ` +
				`= ?`,
		},
		{
			label: "patches by call",
			probe: "GetCall",
			kind:  "read",
			statement: `SELECT COALESCE(t."talkgroupRef", 0) FROM "callPatches" AS cp LEFT JOIN "talkgroups" AS t ON ` +
				`t."talkgroupId" = cp."talkgroupId" WHERE cp."callId" = ? ORDER BY cp."callPatchId"`,
		},
		{
			label: "frequencies by call",
			probe: "GetCall",
			kind:  "read",
			statement: `SELECT "callFrequencyId", "dbm", "errors", "frequency", "offset", "spikes" FROM ` +
				`"callFrequencies" WHERE "callId" = ? ORDER BY "callFrequencyId"`,
		},
		{
			label: "units by call",
			probe: "GetCall",
			kind:  "read",
			statement: `SELECT "callUnitId", "offset", "unitRef" FROM "callUnits" WHERE "callId" = ? ORDER BY ` +
				`"callUnitId"`,
		},
		{
			label:     "delayed list",
			probe:     "AllDelayed",
			kind:      "read",
			statement: `SELECT "callId", "timestamp" FROM "delayed" ORDER BY "callId"`,
		},
		{
			label:     "delayed drop",
			probe:     "UnscheduleDelayed",
			kind:      "write",
			statement: `DELETE FROM "delayed" WHERE "callId" = ?`,
		},
		{
			label: "duplicate check",
			probe: "FindDuplicateCandidates",
			kind:  "read",
			statement: `SELECT c."callId", c."timestamp", c."systemId", c."talkgroupId", c."siteRef", s."systemRef", ` +
				`t."talkgroupRef" FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" ` +
				`LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" WHERE c."systemId" = ? AND ` +
				`c."talkgroupId" = ? AND c."siteRef" = ? AND c."timestamp" BETWEEN ? AND ? ORDER BY ` +
				`c."timestamp"`,
		},
		{
			label:     "talkgroup lookup",
			probe:     "SaveCall",
			kind:      "read",
			statement: `SELECT "talkgroupId" FROM "talkgroups" WHERE "systemId" = ? AND "talkgroupRef" = ?`,
		},
		{
			label:     "group members",
			probe:     "SaveSystemTalkgroups",
			kind:      "read",
			statement: `SELECT "groupId" FROM "talkgroupGroups" WHERE "talkgroupId" = ?`,
		},
		{
			label: "talkgroups by system",
			probe: "AllSystems",
			kind:  "read",
			statement: `SELECT t."talkgroupId", t."alert", t."delay", t."frequency", t."label", t."led", t."name", ` +
				`t."order", t."tagId", t."talkgroupRef", t."type", GROUP_CONCAT(COALESCE(tg."groupId", 0) ORDER ` +
				`BY tg."groupId") FROM "talkgroups" AS t LEFT JOIN "talkgroupGroups" AS tg ON tg."talkgroupId" ` +
				`= t."talkgroupId" WHERE t."systemId" = ? GROUP BY t."talkgroupId"`,
		},
		{
			label: "insert call",
			probe: "SaveCall",
			kind:  "write",
			statement: `INSERT INTO "calls" ("audio", "audioFilename", "audioLength", "audioMime", "siteRef", ` +
				`"systemId", "talkgroupId", "timestamp") VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		},
		{
			label: "insert call frequencies",
			probe: "SaveCall",
			kind:  "write",
			statement: `INSERT INTO "callFrequencies" ("callId", "dbm", "errors", "frequency", "offset", "spikes") ` +
				`VALUES (?, ?, ?, ?, ?, ?)`,
		},
		{
			label:     "insert call units",
			probe:     "SaveCall",
			kind:      "write",
			statement: `INSERT INTO "callUnits" ("callId", "offset", "unitRef") VALUES (?, ?, ?)`,
		},
		{
			label:     "insert delayed",
			probe:     "ScheduleDelayed",
			kind:      "write",
			statement: `INSERT INTO "delayed" ("callId", "timestamp") VALUES (?, ?)`,
		},
	}
}

// historiedStatementsOutsideTheReports returns the statements those reads and writes run beside
// the ones a report times, and what leaves each out of the inventoried list.
func historiedStatementsOutsideTheReports() []unmeasuredStatement {
	return []unmeasuredStatement{
		{
			probe: "AllSystems",
			statement: `SELECT "systemId", "alert", "autoPopulate", "blacklists", "delay", "label", "led", "order", ` +
				`"systemRef", "type" FROM "systems"`,
			reason: "the configuration read carries the system rows beside the talkgroups the measurement times, and no report times those rows",
		},
		{
			probe:     "AllSystems",
			statement: `SELECT "siteId", "label", "order", "siteRef" FROM "sites" WHERE "systemId" = ?`,
			reason:    "the configuration read carries each system’s sites, and no report times them",
		},
		{
			probe: "AllSystems",
			statement: `SELECT "unitId", "label", "order", "unitRef", "unitFrom", "unitTo" FROM "units" WHERE ` +
				`"systemId" = ?`,
			reason: "the configuration read carries each system’s units, and no report times them",
		},
		{
			probe:     "SaveCall",
			statement: `INSERT INTO "callPatches" ("callId", "talkgroupId") VALUES (?, ?)`,
			reason:    "the call the measurement saves names a talkgroup patched onto it, so this write runs in the server, and no measurement times it",
		},
		{
			probe:     "SaveSystemTalkgroups",
			statement: `SELECT COUNT(*) FROM "systems" WHERE "systemId" = ?`,
			reason:    "the guard a configuration save runs before it writes a system, timed by no report",
		},
		{
			probe:     "SaveSystemTalkgroups",
			statement: `SELECT "talkgroupId" FROM "talkgroups" WHERE "systemId" = ?`,
			reason:    "the stored talkgroups a configuration save compares its own list against, timed by no report",
		},
		{
			probe:     "SaveSystemTalkgroups",
			statement: `SELECT COUNT(*) FROM "talkgroups" WHERE "talkgroupId" = ?`,
			reason:    "the guard each talkgroup a configuration save keeps is checked against, timed by no report",
		},
		{
			probe: "SaveSystemTalkgroups",
			statement: `UPDATE "talkgroups" SET "alert" = ?, "delay" = ?, "frequency" = ?, "label" = ?, "led" = ?, ` +
				`"name" = ?, "order" = ?, "tagId" = ?, "talkgroupRef" = ?, "type" = ? WHERE "talkgroupId" = ?`,
			reason: "the write a configuration save makes to a talkgroup it keeps, timed by no report",
		},
		{
			probe:     "SaveSystemTalkgroups",
			statement: `SELECT COUNT(*) FROM "talkgroupGroups" WHERE "talkgroupId" = ? AND "groupId" = ?`,
			reason:    "the guard each group link a configuration save keeps is checked against, timed by no report",
		},
	}
}

// historySpanOfTheDatabase reads the dated range the windowed reads are measured against out of the
// calls the fixture holds: its newest call, and the day that ends with it.
func historySpanOfTheDatabase(t *testing.T, db *sql.DB) historySpan {
	t.Helper()

	var newestMillis int64

	if err := db.QueryRow(`SELECT MAX("timestamp") FROM "calls"`).Scan(&newestMillis); err != nil {
		t.Fatalf("reading the newest call the fixture holds: %v", err)
	}

	newest := time.UnixMilli(newestMillis)

	return historySpan{newest: newest, dayFrom: newest.Add(-24 * time.Hour)}
}

// statementsOfTheMeasuredWorks runs each piece of work the measurements time through the same
// repositories the server runs it with, and answers, under each repository method's name, the
// statements that method handed the database, once each, in the order it handed them.
func statementsOfTheMeasuredWorks(t *testing.T, db *sql.DB, counter *statementCounter, span historySpan) map[string][]string {
	t.Helper()

	calls := NewCalls(db, DialectSqlite)
	delayed := NewDelayedCalls(db, DialectSqlite)
	systems := NewSystems(db, DialectSqlite)

	var systemID, talkgroupID, siteRef int64

	err := db.QueryRow(`SELECT "systemId", "talkgroupId", "siteRef" FROM "calls" ORDER BY "callId" LIMIT 1`).
		Scan(&systemID, &talkgroupID, &siteRef)
	if err != nil {
		t.Fatalf("reading the call row the fixture holds: %v", err)
	}

	measured := map[string][]string{}

	measure := func(probe string, run func() error) {
		t.Helper()

		counter.reset()

		if err := run(); err != nil {
			t.Fatalf("%s: %v", probe, err)
		}

		seen := map[string]bool{}

		for _, statement := range counter.statements() {
			if !seen[statement] {
				seen[statement] = true

				measured[probe] = append(measured[probe], statement)
			}
		}
	}

	// Every history search the works table names, run through the read path the server reads through.
	for _, work := range historySearchWorks(span) {
		criteria := work.criteria

		measure("SearchCalls", func() error {
			_, err := calls.SearchCalls(context.Background(), criteria)

			return err
		})
	}

	measure("CountCallsSince", func() error {
		_, err := calls.CountCallsSince(context.Background(), span.newest.Add(-2*time.Minute))

		return err
	})

	measure("CountCallsByHour", func() error {
		_, err := calls.CountCallsByHour(context.Background(), span.dayFrom)

		return err
	})

	measure("CountCallsByTalkgroup", func() error {
		_, err := calls.CountCallsByTalkgroup(context.Background(), span.newest.Add(-15*time.Minute), 10)

		return err
	})

	for _, limit := range []uint{10, 200} {
		measure("RecentCalls", func() error {
			_, err := calls.RecentCalls(context.Background(), limit)

			return err
		})
	}

	measure("GetCall", func() error {
		_, err := calls.GetCall(context.Background(), fixtureCallID)

		return err
	})

	measure("AllDelayed", func() error {
		_, err := delayed.AllDelayed(context.Background())

		return err
	})

	measure("UnscheduleDelayed", func() error {
		return delayed.UnscheduleDelayed(context.Background(), fixtureDelayedCallID)
	})

	measure("ScheduleDelayed", func() error {
		return delayed.ScheduleDelayed(context.Background(), fixtureCallID, span.newest.Add(time.Hour))
	})

	measure("FindDuplicateCandidates", func() error {
		_, err := calls.FindDuplicateCandidates(context.Background(),
			uint64(systemID), uint64(talkgroupID), uint(siteRef), span.dayFrom, span.newest)

		return err
	})

	// The stored talkgroups the configuration save writes back are read through the count driver, so
	// the configuration read's own statements are among the measured ones.
	var stored domain.Systems

	measure("AllSystems", func() error {
		var err error

		stored, err = systems.AllSystems(context.Background())

		return err
	})

	measure("SaveCall", func() error {
		call := domain.NewCall()

		call.Audio = testAudio
		call.AudioFilename = "audio.wav"
		call.AudioMime = "audio/wav"
		call.Timestamp = span.newest
		call.System = domain.System{ID: uint64(systemID), SystemRef: historySystemRef}
		call.Talkgroup = domain.Talkgroup{ID: uint64(talkgroupID), TalkgroupRef: historyTalkgroupRef}
		call.Frequencies = []domain.CallFrequency{{Dbm: -95, Frequency: 851000000}}
		call.Units = []domain.CallUnit{{UnitRef: 12345}}
		call.PatchTalkgroupRefs = []uint{1514001}

		_, err := calls.SaveCall(context.Background(), call)

		return err
	})

	measure("SaveSystemTalkgroups", func() error {
		return systems.SaveSystemTalkgroups(context.Background(), uint64(systemID), stored[0].Talkgroups, nil)
	})

	return measured
}

// everyExtraShape collects the reads a search issues beside the reads a measurement times.
func everyExtraShape(extras []unmeasuredRead) []historyReadShape {
	out := make([]historyReadShape, 0, len(extras))

	for _, extra := range extras {
		out = append(out, extra.shape)
	}

	return out
}

// historyPairClausesOf answers the pair filters the handler builds for one search's criteria: the
// systems and talkgroups its caller's access names, or the ones expanded from a group label or a tag
// label. A search whose criteria names no such list has none.
func historyPairClausesOf(criteria ports.CallSearch) []string {
	switch {
	case criteria.Scope.Present && !criteria.Scope.Wildcard:
		clauses, _ := accessScopeFilters(criteria.Scope)

		return clauses

	case len(criteria.GroupTalkgroups) > 0:
		clauses, _ := callSearchPairFilters(expandedTalkgroups(criteria.GroupTalkgroups))

		return clauses

	case len(criteria.TagTalkgroups) > 0:
		clauses, _ := callSearchPairFilters(expandedTalkgroups(criteria.TagTalkgroups))

		return clauses
	}

	return nil
}

// TestStatementInventoryMatchesTheHandlerBuilders checks the statement inventory against the
// repository's own read-path builders, so a change to how a history read is built fails here instead
// of leaving the inventory holding a statement the server no longer issues.
func TestStatementInventoryMatchesTheHandlerBuilders(t *testing.T) {
	db, counter := openCountedLegacyDatabase(t)

	if err := Migrate(context.Background(), db, DialectSqlite, fixedClock{migrationInstant}, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	span := historySpanOfTheDatabase(t, db)

	works := historySearchWorks(span)
	inventory := callHistoryStatementInventory(span)
	outside := statementsOutsideTheReports(span)
	measured := statementsOfTheMeasuredWorks(t, db, counter, span)

	// The inventory composes its history reads from the joins and the filters the repository names for
	// the whole history.
	if inventoryHistoryJoin != callHistoryFrom {
		t.Errorf("the inventory composes its history reads from %s, not from the joins the repository names, %s",
			inventoryHistoryJoin, callHistoryFrom)
	}

	base, _ := callSearchFilters(ports.CallSearch{})

	if inventoryBaseFilters != base {
		t.Errorf("the inventory carries %s on every history read, but callSearchFilters answers %s for a "+
			"search that filters nothing", inventoryBaseFilters, base)
	}

	// The filter each measured search adds to those is the filter the handler's own builders answer for
	// that search's criteria now, whatever a measurement once recorded for it, and where that filter is
	// made of pairs it is the clauses the pair builders join, in the order they join them.
	for _, work := range works {
		where, _ := callSearchFilters(work.criteria)

		filters := strings.TrimPrefix(strings.TrimPrefix(where, base), ` AND `)

		if work.filters != filters {
			t.Errorf("the inventory filters %s as\n\t%s\nbut callSearchFilters answers\n\t%s\nfor its criteria",
				work.label, work.filters, filters)
		}

		if clauses := historyPairClausesOf(work.criteria); len(clauses) > 0 {
			want := `(` + strings.Join(clauses, " OR ") + `)`

			if !strings.HasPrefix(work.filters, want) {
				t.Errorf("the inventory filters %s as\n\t%s\nbut the handler's own pair filters join\n\t%s",
					work.label, work.filters, want)
			}
		}

		// And each read the search issues is that filter over the history's joins, under the projection
		// and the ordering and limit the measurement timed it with.
		shapes := append([]historyReadShape{}, work.reads...)
		shapes = append(shapes, everyExtraShape(work.extra)...)

		for _, shape := range shapes {
			got := historySearchStatement(work, shape)
			want := shape.columns + ` ` + callHistoryFrom + ` WHERE ` + where + shape.tail

			if got != want {
				t.Errorf("the inventory reads %s as\n\t%s\nbut its search's filter over the history's joins "+
					"composes\n\t%s", work.label+shape.suffix, got, want)
			}
		}
	}

	// Each inventoried statement is carried under one name.
	labels := map[string]int{}

	for _, entry := range inventory {
		labels[entry.label]++
	}

	for label, times := range labels {
		if times > 1 {
			t.Errorf("the inventory carries %s %d times", label, times)
		}
	}

	// Each entry is classed by what its statement does to the history.
	for _, entry := range inventory {
		kind := "write"
		if strings.HasPrefix(entry.statement, "SELECT") {
			kind = "read"
		}

		if entry.kind != kind {
			t.Errorf("the inventory calls %s a %s, but %s is a %s statement",
				entry.label, entry.kind, firstLineOf(entry.statement), kind)
		}
	}

	// The inventoried list and the statements the measured works issue beside it name every statement
	// the measured works issue: nothing a measured work hands the database is left unnamed, and every
	// entry either list names is a statement its own probe really hands the database.
	inventoried := map[string]bool{}
	listed := map[string]bool{}

	for _, entry := range inventory {
		inventoried[entry.statement] = true
	}

	for _, entry := range outside {
		listed[entry.statement] = true
	}

	for probe, statements := range measured {
		for _, statement := range statements {
			if !inventoried[statement] && !listed[statement] {
				t.Errorf("%s issues %s, which the inventory neither carries nor lists outside",
					probe, firstLineOf(statement))
			}
		}
	}

	for _, entry := range inventory {
		if !slices.Contains(measured[entry.probe], entry.statement) {
			t.Errorf("the inventoried statement %s, read by %s as %s, is not a statement %s issues",
				firstLineOf(entry.statement), entry.label, entry.probe, entry.probe)
		}
	}

	for _, entry := range outside {
		if !slices.Contains(measured[entry.probe], entry.statement) {
			t.Errorf("the outside statement %s, attributed to %s, is not a statement %s issues: %s",
				firstLineOf(entry.statement), entry.probe, entry.probe, entry.reason)
		}
	}

	// The inventory is the whole measured set and nothing beside it: the reads of the twelve measured
	// searches, which the reports time as 34 named reads, plus the 19 reads and writes the reports name
	// under their own names.
	if got := len(inventory); got != 53 {
		t.Errorf("the inventory carries %d statements, not the 53 the reports time", got)
	}

	if got := len(inventory) - len(historiedReadsAndWrites()); got != 34 {
		t.Errorf("the measured searches contribute %d inventoried reads, not the 34 the reports time for "+
			"their %d searches", got, len(works))
	}

	for kind, want := range map[string]int{"read": 48, "write": 5} {
		got := 0

		for _, entry := range inventory {
			if entry.kind == kind {
				got++
			}
		}

		if got != want {
			t.Errorf("the inventory carries %d %s statements, not the %d the reports name", got, kind, want)
		}
	}
}

// measuredSetNames lists the ten call history keys in the order the set documents them. The list is written
// out here instead of read back from the set, because each key the measurements turned down leaves a hole in
// it: adding an eleventh key, or handing one of these ten a column list a measurement rejected, has to be
// written here too before it can pass.
func measuredSetNames() []string {
	return []string{
		"callfrequencies_call",
		"callpatches_call",
		"callunits_call",
		"delayed_call",
		"talkgroups_system_ref",
		"talkgroups_tag",
		"talkgroupgroups_talkgroup",
		"talkgroupgroups_group",
		"sites_system",
		"calls_timestamp_cover",
	}
}

// rejectedCallIndexShapes names every column list on the calls table that was measured and turned down: the
// pair key led by the talkgroup, the pair keys led by the system in the two orders that were not chosen, and
// the timestamp-led keys that stop short of the covering form. Each is a key a later reader could be tempted
// to add, and none of them earned a place.
func rejectedCallIndexShapes() []indexSpec {
	return []indexSpec{
		{name: "calls_talkgroup_pair", table: "calls", columns: []string{"talkgroupId", "timestamp"}},
		{name: "calls_system_pair", table: "calls", columns: []string{"systemId", "timestamp"}},
		{name: "calls_system_pair_before_timestamp", table: "calls", columns: []string{"systemId", "timestamp", "talkgroupId"}},
		{name: "calls_timestamp_only", table: "calls", columns: []string{"timestamp"}},
		{name: "calls_timestamp_and_call", table: "calls", columns: []string{"timestamp", "callId"}},
		{name: "calls_timestamp_lead_short_of_the_cover", table: "calls", columns: []string{"timestamp", "callId", "audioLength"}},
	}
}

// shippedPairIndex is the key on the calls table the server shipped with. It is not a member of the set: the
// schema copies declare it, and the step finds it and leaves it alone.
func shippedPairIndex() indexSpec {
	return indexSpec{
		name:    "calls_idx",
		table:   "calls",
		columns: []string{"systemId", "siteRef", "talkgroupId", "timestamp"},
	}
}

// setTableNames lists each table the set names once, in the order the set first names it.
func setTableNames() []string {
	tables := []string{}
	seen := map[string]bool{}

	for _, member := range callIndexSet {
		if !seen[member.table] {
			seen[member.table] = true

			tables = append(tables, member.table)
		}
	}

	return tables
}

// keysTheSetNamesOn lists the keys the set declares over one table, and on the calls table the shipped pair
// key the schema copies declare as well. The list is what the catalogue over that table has to answer, so the
// names are read from the set rather than written out again here.
func keysTheSetNamesOn(table string) []indexSpec {
	keys := []indexSpec{}

	for _, member := range callIndexSet {
		if member.table == table {
			keys = append(keys, member)
		}
	}

	if table == "calls" {
		keys = append(keys, shippedPairIndex())
	}

	return keys
}

// automaticIndexNames names the keys the database made for itself on the given tables. SQLite reports one for
// a primary key or a uniqueness rule the table declares, and the engine gives it a name no one asked for.
func automaticIndexNames(t *testing.T, db *sql.DB, tables []string) []string {
	t.Helper()

	found := []string{}

	for _, table := range tables {
		rows, err := db.Query(`SELECT "name" FROM "sqlite_master" WHERE "type" = 'index' AND "tbl_name" = ? AND "name" LIKE 'sqlite_autoindex_%' ORDER BY "name"`, table)
		if err != nil {
			t.Fatalf("looking for the keys the database made on %s: %v", table, err)
		}

		names := []string{}

		for rows.Next() {
			var name string

			if err := rows.Scan(&name); err != nil {
				rows.Close()

				t.Fatalf("reading the name of a key the database made on %s: %v", table, err)
			}

			names = append(names, name)
		}

		if err := rows.Err(); err != nil {
			rows.Close()

			t.Fatalf("reading the keys the database made on %s: %v", table, err)
		}

		rows.Close()

		found = append(found, names...)
	}

	return found
}

// columnsHeldOnce reports how many of the given keys carry one column list, in index order.
func columnsHeldOnce(indexes []indexSpec, columns []string) int {
	held := 0

	for _, index := range indexes {
		if slices.Equal(index.columns, columns) {
			held++
		}
	}

	return held
}

// duplicatedColumnLists names each column list that more than one of the given keys carries. Two keys over
// the same columns cost the insert path twice and the read path nothing, so an empty list is what a table
// the set has been applied to should show.
func duplicatedColumnLists(indexes []indexSpec) []string {
	counts := map[string]int{}
	order := []string{}

	for _, index := range indexes {
		list := strings.Join(index.columns, ", ")

		if counts[list] == 0 {
			order = append(order, list)
		}

		counts[list]++
	}

	duplicated := []string{}

	for _, list := range order {
		if counts[list] > 1 {
			duplicated = append(duplicated, list)
		}
	}

	return duplicated
}

// The set is exactly the measured set, on a database that has never run ANALYZE: the state a deployment is in
// when it starts, and the state every figure that chose these keys was taken in. The keys the measurements
// turned down are named here so that adding one of them later fails a test that says which measurement said
// no.
func TestCallIndexSetCarriesNoSystemPairIndex(t *testing.T) {
	db := migratedLegacyDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	if got := plannerStatisticsTables(t, db); len(got) != 0 {
		t.Fatalf("the database holds the tables %v that hold planner statistics, want none: every figure behind "+
			"this set was read from a database the planner never measured", got)
	}

	if got := setMemberNames(); !slices.Equal(got, measuredSetNames()) {
		t.Fatalf("the set declares (%s), want the ten measured keys (%s)",
			strings.Join(got, ", "), strings.Join(measuredSetNames(), ", "))
	}

	if missing, err := missingCallIndexes(ctx, s); err != nil {
		t.Fatalf("missingCallIndexes: %v", err)
	} else if got := missingNames(missing); len(got) != 0 {
		t.Fatalf("the probe reports (%s) missing from a database the startup pass has run over, want all ten "+
			"present", strings.Join(got, ", "))
	}

	// None of the ten is a key the database chose on its own: an automatic key covers the columns a table
	// happens to declare unique, which is not the columns the history reads ask for.
	if found := automaticIndexNames(t, db, setTableNames()); len(found) != 0 {
		t.Fatalf("the set's tables hold the keys the database made for itself %v, want every key on them named "+
			"by the schema copies or by the step", found)
	}

	// Every key on the tables the set names, read table by table: exactly the keys the set declares, and on
	// the calls table the shipped pair key beside them. An eleventh key, on any of those tables and under any
	// name, is written here before it can pass.
	carries := []indexSpec{}

	for _, table := range setTableNames() {
		indexes, err := hasCallIndexShape(ctx, s, table)
		if err != nil {
			t.Fatalf("hasCallIndexShape(%s): %v", table, err)
		}

		if got := indexShapes(indexes); !slices.Equal(got, indexShapes(keysTheSetNamesOn(table))) {
			t.Errorf("%s holds %v, want the keys the set names on it and the shipped pair key there: %v",
				table, got, indexShapes(keysTheSetNamesOn(table)))
		}

		carries = append(carries, indexes...)
	}

	calls := []indexSpec{}

	for _, index := range carries {
		if index.table == "calls" {
			calls = append(calls, index)
		}
	}

	// Every rejected form is named, and none of them is a member of the set or a key the calls table ends
	// up carrying.
	for _, rejected := range rejectedCallIndexShapes() {
		for _, member := range callIndexSet {
			if member.table == rejected.table && slices.Equal(member.columns, rejected.columns) {
				t.Errorf("the set carries %s over (%s), which is the rejected form %s",
					member.name, strings.Join(member.columns, ", "), rejected.name)
			}
		}

		for _, index := range calls {
			if slices.Equal(index.columns, rejected.columns) {
				t.Errorf("the calls table holds %s over (%s), which is the rejected form %s: no measurement gave "+
					"that list a place", index.name, strings.Join(index.columns, ", "), rejected.name)
			}
		}
	}

	timestampLed := []string{}
	systemLed := []string{}

	for _, index := range calls {
		if len(index.columns) > 0 && index.columns[0] == "timestamp" {
			timestampLed = append(timestampLed, index.name)
		}

		if len(index.columns) > 0 && index.columns[0] == "systemId" {
			systemLed = append(systemLed, index.name)
		}
	}

	if len(timestampLed) != 1 || timestampLed[0] != coveringKeyMember().name {
		t.Errorf("the calls table leads with the timestamp in %v, want the covering key %s alone",
			timestampLed, coveringKeyMember().name)
	}

	if want := []string{"timestamp", "callId", "audioLength", "systemId", "talkgroupId"}; !slices.Equal(coveringKeyMember().columns, want) {
		t.Errorf("the covering key carries (%s), want every column the history reads report: (%s)",
			strings.Join(coveringKeyMember().columns, ", "), strings.Join(want, ", "))
	}

	if len(systemLed) != 1 || systemLed[0] != shippedPairIndex().name {
		t.Errorf("the calls table leads with the system in %v, want the shipped %s alone",
			systemLed, shippedPairIndex().name)
	}

	// The two keys the calls table ends with, in the words the database holds them in: the shipped pair key
	// with the columns it shipped with, and the covering key the step adds. No third key, and no full-text
	// definition of any kind.
	definitions := indexDefinitions(t, db, "calls")

	if !slices.Equal(definitions, []string{shippedPairDefinition, coveringKeyDefinition}) {
		t.Errorf("the calls table carries %v, want exactly %s and %s",
			definitions, shippedPairIndex().name, coveringKeyMember().name)
	}

	dialects := []Dialect{DialectSqlite, DialectMariadb, DialectMysql, DialectPostgresql}

	for _, member := range callIndexSet {
		for _, dialect := range dialects {
			statement := callIndexCreateStatement(dialect, member)

			upper := strings.ToUpper(statement)

			if strings.Contains(upper, "USING") || strings.Contains(upper, "FULLTEXT") || strings.Contains(upper, " FTS") {
				t.Errorf("%s builds %s for %s, want a plain key over a column list: the talkgroup search path "+
					"stays a scan", dialect, statement, member.name)
			}
		}
	}
}

// No column list on the calls table is held by two keys, whatever those keys are named. The engines that
// build a key for a foreign key on their own each give it a name of their own, so the only comparison that
// holds on every backend is over the columns a finished key carries.
func TestNoColumnListIsIndexedTwice(t *testing.T) {
	db := migratedLegacyDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	carries := []indexSpec{}

	for _, table := range setTableNames() {
		indexes, err := hasCallIndexShape(ctx, s, table)
		if err != nil {
			t.Fatalf("hasCallIndexShape(%s): %v", table, err)
		}

		if got := duplicatedColumnLists(indexes); len(got) != 0 {
			t.Errorf("%s holds %v over the same columns twice, want one key per column list", table, got)
		}

		carries = append(carries, indexes...)
	}

	calls := []indexSpec{}

	for _, index := range carries {
		if index.table == "calls" {
			calls = append(calls, index)
		}
	}

	if got := duplicatedColumnLists(calls); len(got) != 0 {
		t.Errorf("the calls table holds %v over the same columns twice, want the shipped pair list and the "+
			"covering list held once each", got)
	}

	// The shipped pair key keeps its own list, and the set's covering key is held once: the set declares no
	// second key over either of those lists.
	if got := columnsHeldOnce(calls, shippedPairIndex().columns); got != 1 {
		t.Errorf("the calls table holds the shipped list %d times, want once in %s", got, shippedPairIndex().name)
	}

	if got := columnsHeldOnce(calls, coveringKeyMember().columns); got != 1 {
		t.Errorf("the calls table holds the covering list %d times, want once in %s", got, coveringKeyMember().name)
	}

	// A database that already carries the covering list under a name the set does not use is a deployment
	// that added the key by hand. The step finds it by the columns it holds, creates no second key over the
	// same list, and reports the other nine.
	planted := openLegacyDatabase(t)

	p := newStore(planted, DialectSqlite)

	if err := ensureCallAudioLengthColumn(ctx, p); err != nil {
		t.Fatalf("ensureCallAudioLengthColumn: %v", err)
	}

	handBuilt := `CREATE INDEX "calls_history_by_hand" ON "calls" ("timestamp","callId","audioLength","systemId","talkgroupId")`

	if _, err := planted.Exec(handBuilt); err != nil {
		t.Fatalf("%s: %v", firstLineOf(handBuilt), err)
	}

	if missing, err := missingCallIndexes(ctx, p); err != nil {
		t.Fatalf("missingCallIndexes: %v", err)
	} else {
		got := missingNames(missing)

		if columnsHeldOnce(missing, coveringKeyMember().columns) != 0 {
			t.Fatalf("the probe reports (%s) missing, which holds the covering list: the key that already carries "+
				"it is %s", strings.Join(got, ", "), "calls_history_by_hand")
		}

		want := []string{}

		for _, member := range callIndexSet {
			if member.table != "calls" {
				want = append(want, member.name)
			}
		}

		if !slices.Equal(got, want) {
			t.Fatalf("the probe reports (%s) missing, want the nine keys beside the covering one", strings.Join(got, ", "))
		}
	}

	reported := []progressLine{}

	if err := ensureCallIndexSet(ctx, p, func(level, message string) {
		reported = append(reported, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("ensureCallIndexSet: %v", err)
	}

	if len(reported) != len(callIndexSet)-1 {
		t.Errorf("the step reported %v, want one line for each of the nine keys beside the covering one", reported)
	}

	for _, line := range reported {
		if strings.Contains(line.message, coveringKeyMember().name) {
			t.Errorf("the step reported %s, want no line creating the covering key: the hand-built key already "+
				"holds that list", line.message)
		}
	}

	held, err := hasCallIndexShape(ctx, p, "calls")
	if err != nil {
		t.Fatalf("hasCallIndexShape(calls): %v", err)
	}

	if got := indexShapes(held); !slices.Equal(got, indexShapes([]indexSpec{shippedPairIndex(),
		{name: "calls_history_by_hand", table: "calls", columns: coveringKeyMember().columns}})) {
		t.Errorf("the calls table ends with %v, want the shipped pair key and the hand-built key and no key "+
			"the step added over a list either one already holds", got)
	}

	if got := duplicatedColumnLists(held); len(got) != 0 {
		t.Errorf("after the step the calls table holds %v over the same columns twice, want the covering list "+
			"held once by the key that was there first", got)
	}

	// The four tables that hang off a call each take exactly one key over the call they belong to, the key a
	// backend that indexes a foreign key makes for itself too. On a database the legacy server wrote none of
	// them exists before the step, and none of them appears twice after it.
	for _, table := range []string{"callFrequencies", "callPatches", "callUnits", "delayed"} {
		indexes, err := hasCallIndexShape(ctx, p, table)
		if err != nil {
			t.Fatalf("hasCallIndexShape(%s): %v", table, err)
		}

		if got := columnsHeldOnce(indexes, []string{"callId"}); got != 1 {
			t.Errorf("%s holds a key over (callId) %d times after the step, want once", table, got)
		}

		if got := duplicatedColumnLists(indexes); len(got) != 0 {
			t.Errorf("%s holds %v over the same columns twice, want one key per column list", table, got)
		}
	}
}

// The plan assertions below ask the migrated database how it would run each statement the server issues
// for the call history, and read the answer out of the database rather than out of a copy of an earlier
// run. Each placeholder of an inventoried statement is filled with a value the fixture holds for the
// column that placeholder is compared with, so the plan a test reads is the plan the read gets when the
// server runs it with its own values.

const (
	// coveringKeyIndex names the key every history read is written against.
	coveringKeyIndex = "calls_timestamp_cover"

	// shippedPairIndexName names the key the shipped database already carries over the system, site,
	// talkgroup and timestamp of a call.
	shippedPairIndexName = "calls_idx"

	// delayedKeyIndex names the key the delayed table is read, joined and dropped through.
	delayedKeyIndex = "delayed_call"
)

// queryBuiltIndexMarkers name the ways a database answers a query with an index it made for that query
// alone. None of them appears in a plan over a database that carries the set.
var queryBuiltIndexMarkers = []string{
	"AUTOMATIC COVERING INDEX",
	"AUTOMATIC PARTIAL COVERING INDEX",
	"BLOOM FILTER",
}

// historyPlanValues answers, for each column the inventoried statements compare a placeholder with, the
// values the migrated fixture holds for that column, in the order the fixture stores them.
func historyPlanValues(t *testing.T, db *sql.DB, span historySpan) map[string][]any {
	t.Helper()

	values := map[string][]any{
		"LIMIT":     {25},
		"OFFSET":    {0},
		"timestamp": {millisOf(span.dayFrom), millisOf(span.newest)},
	}

	for _, column := range []struct{ table, name string }{
		{table: "systems", name: "systemId"},
		{table: "systems", name: "systemRef"},
		{table: "talkgroups", name: "talkgroupId"},
		{table: "talkgroups", name: "talkgroupRef"},
		{table: "sites", name: "siteRef"},
		{table: "calls", name: "callId"},
		{table: "groups", name: "groupId"},
		{table: "tags", name: "tagId"},
	} {
		query := `SELECT "` + column.name + `" FROM "` + column.table + `"`

		rows, err := db.Query(query)
		if err != nil {
			t.Fatalf("reading the %s values the fixture holds: %v", column.name, err)
		}

		found := []any{}

		for rows.Next() {
			var value int64

			if err := rows.Scan(&value); err != nil {
				rows.Close()

				t.Fatalf("reading a %s value the fixture holds: %v", column.name, err)
			}

			found = append(found, value)
		}

		rows.Close()

		if err := rows.Err(); err != nil {
			t.Fatalf("reading the %s values the fixture holds: %v", column.name, err)
		}

		if len(found) == 0 {
			t.Fatalf("the fixture holds no value of %s.%s to bind", column.table, column.name)
		}

		values[column.name] = found
	}

	return values
}

// planArgumentsFor fills every placeholder of one inventoried statement with a value the fixture holds
// for the column that placeholder is compared with. Where a statement compares one column several times,
// the fixture's own values for it are used in turn, so a filter naming three talkgroups is measured
// against three talkgroups the fixture stores.
func planArgumentsFor(statement string, values map[string][]any) []any {
	args := []any{}

	taken := map[string]int{}

	for {
		next := strings.Index(statement, "?")
		if next < 0 {
			return args
		}

		column := placeholderColumn(statement[:next])

		statement = statement[next+1:]

		cycle := values[column]
		if len(cycle) == 0 {
			cycle = []any{1}
		}

		args = append(args, cycle[taken[column]%len(cycle)])

		taken[column]++
	}
}

// placeholderColumn answers which column the placeholder that follows a statement's prefix is compared
// with, which is the last quoted name that prefix carries. A limit and an offset are the two placeholders
// a statement carries without a column in front of them.
func placeholderColumn(prefix string) string {
	if strings.HasSuffix(prefix, "LIMIT ") {
		return "LIMIT"
	}

	if strings.HasSuffix(prefix, "OFFSET ") {
		return "OFFSET"
	}

	end := strings.LastIndex(prefix, `"`)
	if end < 0 {
		return ""
	}

	start := strings.LastIndex(prefix[:end], `"`)
	if start < 0 {
		return ""
	}

	return prefix[start+1 : end]
}

// planForStatement answers how the database would run one statement, in the shape the measurements record
// a plan: each step of the answer joined into one text.
func planForStatement(t *testing.T, db *sql.DB, statement string, values map[string][]any) string {
	t.Helper()

	return strings.Join(queryPlan(t, db, statement, planArgumentsFor(statement, values)...), " / ")
}

// planForLabel explains the inventoried statement one label names.
func planForLabel(t *testing.T, db *sql.DB, inventory []inventoriedStatement, values map[string][]any, label string) string {
	t.Helper()

	return planForStatement(t, db, inventoriedStatementFor(t, inventory, label).statement, values)
}

// inventoriedStatementFor answers the one inventoried statement a label names.
func inventoriedStatementFor(t *testing.T, inventory []inventoriedStatement, label string) inventoriedStatement {
	t.Helper()

	for _, entry := range inventory {
		if entry.label == label {
			return entry
		}
	}

	t.Fatalf("the statement inventory carries no statement labelled %s", label)

	return inventoriedStatement{}
}

// statementOutsideTheInventory answers the one statement outside the inventoried list that carries the
// given text, which is how a test reads the plan of a guard or a configuration read that no measurement
// times.
func statementOutsideTheInventory(t *testing.T, outside []unmeasuredStatement, fragment string) string {
	t.Helper()

	found := []string{}

	for _, entry := range outside {
		if strings.Contains(entry.statement, fragment) {
			found = append(found, entry.statement)
		}
	}

	if len(found) != 1 {
		t.Fatalf("%d statements outside the inventoried list carry %s, want exactly one: %v",
			len(found), fragment, found)
	}

	return found[0]
}

// planWalksIndex reports whether the plan reaches the named index at all, whichever step of it does so.
func planWalksIndex(plan, index string) bool {
	return strings.Contains(plan, "INDEX "+index)
}

// planSeeksIndexOn reports whether the plan seeks the named index with a constraint on the given column,
// which is what a key does when it answers a filter rather than only covering the rows it walks.
func planSeeksIndexOn(plan, index, column string) bool {
	return strings.Contains(plan, "INDEX "+index+" ("+column)
}

// assertNothingBuiltForTheQuery fails when a plan answers with an index the database made for that query
// instead of reaching a key the set names.
func assertNothingBuiltForTheQuery(t *testing.T, label, plan string) {
	t.Helper()

	for _, marker := range queryBuiltIndexMarkers {
		if strings.Contains(plan, marker) {
			t.Errorf("the %s read would run as %s: %s means the database builds an index for this query "+
				"instead of reaching one the set names", label, plan, marker)
		}
	}
}

// historyReadSortsTheHistory reports whether a read that puts its rows in timestamp order needs a sort of
// its own to do it. A grouped aggregate orders its own groups, which is not the history being sorted.
func historyReadSortsTheHistory(plan, statement string) bool {
	if strings.Contains(statement, "GROUP BY") {
		return false
	}

	return strings.Contains(plan, "USE TEMP B-TREE FOR ORDER BY")
}

// namedCallHistoryKeys lists every key a plan over the call history is allowed to reach: the ten members of
// the set, and the pair key the shipped schema already declares over the calls table.
func namedCallHistoryKeys() []string {
	names := []string{shippedPairIndexName}

	return append(names, setMemberNames()...)
}

// planIndexNames lists the keys a plan reaches, in the order its steps reach them, without repeating one.
func planIndexNames(plan string) []string {
	names := []string{}

	for _, step := range strings.Split(plan, " / ") {
		_, rest, found := strings.Cut(step, " USING COVERING INDEX ")

		if !found {
			_, rest, found = strings.Cut(step, " USING INDEX ")

			if !found {
				continue
			}
		}

		name, _, _ := strings.Cut(rest, " ")

		name, _, _ = strings.Cut(name, "(")

		if name == "" || hasString(names, name) {
			continue
		}

		names = append(names, name)
	}

	return names
}

// planSeeksAPrimaryKey reports whether a plan reaches a table through the key that table is stored in.
func planSeeksAPrimaryKey(plan string) bool {
	return strings.Contains(plan, "USING INTEGER PRIMARY KEY") || strings.Contains(plan, "USING INDEX sqlite_autoindex")
}

// planStepsForTable answers the steps of a plan that read one table or the alias it is read under.
func planStepsForTable(plan, table string) []string {
	steps := []string{}

	for _, step := range strings.Split(plan, " / ") {
		fields := strings.Fields(step)

		if len(fields) < 2 {
			continue
		}

		if fields[0] != "SCAN" && fields[0] != "SEARCH" {
			continue
		}

		if fields[1] != table {
			continue
		}

		steps = append(steps, strings.TrimSpace(step))
	}

	return steps
}

// planWalksTablesWithoutAnIndex answers the steps of a plan that read a table from its rows instead of from a
// key, which is what a table walk looks like in a plan.
func planWalksTablesWithoutAnIndex(plan string) []string {
	walked := []string{}

	for _, step := range strings.Split(plan, " / ") {
		fields := strings.Fields(step)

		if len(fields) < 2 {
			continue
		}

		if fields[0] != "SCAN" && fields[0] != "SEARCH" {
			continue
		}

		if strings.Contains(step, " USING ") {
			continue
		}

		walked = append(walked, strings.TrimSpace(step))
	}

	return walked
}

// planScansAKey reports whether a plan reads a key in its own order, which is what a read that a key's column
// list cannot narrow still gets when the key covers the columns it needs.
func planScansAKey(plan, table string) bool {
	for _, step := range planStepsForTable(plan, table) {
		if strings.HasPrefix(step, "SCAN ") {
			return true
		}
	}

	return false
}

// planConstraintForTable answers the constraints one plan step narrows its key with, which are the parts of a
// read's filters a key answers by seeking instead of by walking.
func planConstraintForTable(plan, table string) string {
	for _, step := range planStepsForTable(plan, table) {
		start := strings.Index(step, " (")
		if start < 0 {
			continue
		}

		return strings.TrimSuffix(strings.TrimPrefix(step[start:], " ("), ")")
	}

	return ""
}

// assertPlanHasStep fails when a plan does not carry one step exactly as the measurements recorded it.
func assertPlanHasStep(t *testing.T, label, plan, want string) {
	t.Helper()

	if !hasString(strings.Split(plan, " / "), want) {
		t.Errorf("the %s read would run as %s, want a step of %s", label, plan, want)
	}
}

// TestEveryHistoryReadReachesItsIndex asks the migrated database how it would run every read the server issues
// for the call history. Each read reaches a key the set names or the key a table is stored in, no read walks a
// table the set gives a key, no read sorts the history with a tree built for that query, and no plan carries a
// key the database made for the query. A calls key led by systemId, which is a shape the measurements turned
// down, is found by the probe, left alone by the step, and changes no read's plan.
func TestEveryHistoryReadReachesItsIndex(t *testing.T) {
	db := migratedLegacyDatabase(t)

	s := newStore(db, DialectSqlite)

	ctx := context.Background()

	span := historySpanOfTheDatabase(t, db)

	values := historyPlanValues(t, db, span)

	inventory := callHistoryStatementInventory(span)

	planned := map[string]string{}

	allowed := namedCallHistoryKeys()

	for _, entry := range inventory {
		if entry.kind != "read" {
			continue
		}

		plan := planForStatement(t, db, entry.statement, values)

		planned[entry.label] = plan

		assertNothingBuiltForTheQuery(t, entry.label, plan)

		if len(planIndexNames(plan)) == 0 && !planSeeksAPrimaryKey(plan) {
			t.Errorf("the %s read would run as %s: it reaches no key the set names and no table's own key", entry.label, plan)
		}

		if walked := planWalksTablesWithoutAnIndex(plan); len(walked) != 0 {
			t.Errorf("the %s read would run as %s: it walks %v where the set names a key", entry.label, plan, walked)
		}

		for _, name := range planIndexNames(plan) {
			if !hasString(allowed, name) {
				t.Errorf("the %s read would run as %s through %s, which is not a key the set names", entry.label, plan, name)
			}
		}

		if historyReadSortsTheHistory(plan, entry.statement) {
			t.Errorf("the %s read would run as %s: it sorts the history with a tree built for that query", entry.label, plan)
		}
	}

	// The unfiltered page, and the oldest and newest call of the history beside it: the covering key answers
	// all three in its own order, which is the order the history is read in.
	for _, label := range []string{"all page 25", "all span oldest", "all span newest"} {
		assertPlanHasStep(t, label, planned[label], "SCAN c USING COVERING INDEX "+coveringKeyIndex)
	}

	// The count of the whole history has no filter a key could narrow, so it walks the pair key the shipped
	// database already carries, whose first column every history read filters on.
	assertPlanHasStep(t, "all count", planned["all count"], "SEARCH c USING COVERING INDEX "+shippedPairIndexName+" (systemId>?)")

	// The pair page and the pair count. A pair key over the system and the talkgroup was measured and turned
	// down, so these two reads reach a key the set names, and either that key answers their order or they sort.
	for _, label := range []string{"system talkgroup page 25", "system talkgroup count", "system page 25", "system count"} {
		plan := planned[label]

		if !planWalksIndex(plan, coveringKeyIndex) && !planWalksIndex(plan, shippedPairIndexName) {
			t.Errorf("the %s read would run as %s: neither the covering key nor the shipped pair key answers it", label, plan)
		}
	}

	// A read bounded by a dated range is narrowed by the covering key on the timestamp it is bounded by.
	for _, label := range []string{
		"day page 25", "day count", "day span oldest", "day span newest",
		"search tag window page 25", "search tag window count", "search tag window span oldest", "search tag window span newest",
		"search talkgroup window page 25", "search talkgroup window count", "search talkgroup window span oldest",
		"search talkgroup window span newest",
	} {
		plan := planned[label]

		if !planSeeksIndexOn(plan, coveringKeyIndex, "timestamp") {
			t.Errorf("the %s read would run as %s: the covering key is not narrowed on the timestamp that bounds it", label, plan)
		}
	}

	// The status counts and the recent window are the reads a caller runs on every refresh, and each one is
	// bounded by a timestamp the covering key is narrowed on.
	for _, label := range []string{"status count 2m", "status hours 24h", "status tallies 15m"} {
		plan := planned[label]

		if !planSeeksIndexOn(plan, coveringKeyIndex, "timestamp") {
			t.Errorf("the %s read would run as %s: the covering key is not narrowed on the timestamp that bounds it", label, plan)
		}
	}

	for _, label := range []string{"recent 10", "recent 200"} {
		plan := planned[label]

		if !planWalksIndex(plan, coveringKeyIndex) {
			t.Errorf("the %s read would run as %s: the covering key does not answer it", label, plan)
		}

		if !planScansAKey(plan, "c") && !planSeeksIndexOn(plan, coveringKeyIndex, "timestamp") {
			t.Errorf("the %s read would run as %s: the key that answers it is neither scanned nor narrowed", label, plan)
		}
	}

	// The reads a measured search issues beside the reads a report times are planned too, so a filter shape
	// only those reads carry cannot reach a table walk unnoticed.
	for _, entry := range statementsOutsideTheReports(span) {
		if !strings.Contains(entry.statement, `FROM "calls" AS c`) {
			continue
		}

		plan := planForStatement(t, db, entry.statement, values)

		assertNothingBuiltForTheQuery(t, entry.probe, plan)

		if !planWalksIndex(plan, coveringKeyIndex) && !planWalksIndex(plan, shippedPairIndexName) {
			t.Errorf("the read outside the inventoried list (%s) would run as %s: neither the covering key nor the shipped "+
				"pair key answers it", entry.reason, plan)
		}

		if walked := planWalksTablesWithoutAnIndex(plan); len(walked) != 0 {
			t.Errorf("the read outside the inventoried list (%s) would run as %s: it walks %v where the set names a key",
				entry.reason, plan, walked)
		}

		if historyReadSortsTheHistory(plan, entry.statement) {
			t.Errorf("the read outside the inventoried list (%s) would run as %s: it sorts the history with a tree built "+
				"for that query", entry.reason, plan)
		}
	}

	// A calls key led by systemId beside the set: the shape the measurements turned down, planted here to show
	// the probe finds it, the step leaves it alone, and it changes nothing about how the by-system reads run.
	bySystem := []string{
		"system page 25", "system count", "system span oldest", "system span newest",
		"system talkgroup page 25", "system talkgroup count", "system talkgroup span oldest", "system talkgroup span newest",
	}

	before := map[string]string{}

	for _, label := range bySystem {
		before[label] = planned[label]
	}

	const plantedKey = "calls_system_ts"

	const plantedDefinition = `CREATE INDEX "calls_system_ts" ON "calls" ("systemId","timestamp")`

	if _, err := db.Exec(`CREATE INDEX "` + plantedKey + `" ON "calls" ("systemId","timestamp")`); err != nil {
		t.Fatalf("planting a calls key led by systemId: %v", err)
	}

	carries, err := hasCallIndexShape(ctx, s, "calls")
	if err != nil {
		t.Fatalf("hasCallIndexShape(calls) beside a planted key: %v", err)
	}

	found := []string{}

	for _, index := range carries {
		found = append(found, index.name)
	}

	if !hasString(found, plantedKey) {
		t.Fatalf("the probe finds %v on the calls table beside a planted key, want it to name %s", found, plantedKey)
	}

	missing, err := missingCallIndexes(ctx, s)
	if err != nil {
		t.Fatalf("missingCallIndexes beside a planted key: %v", err)
	}

	if got := missingNames(missing); len(got) != 0 {
		t.Errorf("the set reports %v missing beside a planted key, want the planted key to be neither wanted nor removed", got)
	}

	reported := []progressLine{}

	if err := ensureCallIndexSet(ctx, s, func(level, message string) {
		reported = append(reported, progressLine{level: level, message: message})
	}); err != nil {
		t.Fatalf("ensureCallIndexSet beside a planted key: %v", err)
	}

	if len(reported) != 0 {
		t.Errorf("the step reports %v beside a planted key, want it to add nothing", reported)
	}

	definitions := indexDefinitions(t, db, "calls")

	if want := []string{shippedPairDefinition, plantedDefinition, coveringKeyDefinition}; !slices.Equal(definitions, want) {
		t.Errorf("the calls table carries %v after the step beside a planted key, want the planted key, the shipped pair "+
			"key and the covering key, each with the statement that made it", definitions)
	}

	for _, label := range bySystem {
		plan := planForLabel(t, db, callHistoryStatementInventory(span), values, label)

		if plan != before[label] {
			t.Errorf("the %s read runs as %s beside a calls key led by systemId, and as %s without it: the planted key "+
				"changes how the read runs, and the set names no key over those columns", label, plan, before[label])
		}

		if !planWalksIndex(plan, coveringKeyIndex) && !planWalksIndex(plan, shippedPairIndexName) {
			t.Errorf("the %s read would run as %s beside a planted key: the covering key no longer answers it", label, plan)
		}
	}
}

// statementsOfTheConfigurationWrites returns the writes a configuration save or a retention prune makes beside
// the statements a report times, each named the way the plan assertions name it.
func statementsOfTheConfigurationWrites() []inventoriedStatement {
	return []inventoriedStatement{
		{
			label:     "prune delete",
			probe:     "DeleteCallsBefore",
			kind:      "write",
			statement: `DELETE FROM "calls" WHERE "timestamp" < ?`,
		},
		{
			label:     "call drop",
			probe:     "DeleteCall",
			kind:      "write",
			statement: `DELETE FROM "calls" WHERE "callId" = ?`,
		},
		{
			label:     "talkgroup drop",
			probe:     "SaveSystemTalkgroups",
			kind:      "write",
			statement: `DELETE FROM "talkgroups" WHERE "talkgroupId" IN (?)`,
		},
		{
			label:     "tag drop",
			probe:     "SaveTags",
			kind:      "write",
			statement: `DELETE FROM "tags" WHERE "tagId" IN (?)`,
		},
		{
			label:     "group drop",
			probe:     "SaveGroups",
			kind:      "write",
			statement: `DELETE FROM "groups" WHERE "groupId" IN (?)`,
		},
		{
			label:     "site drop",
			probe:     "SaveSystemSites",
			kind:      "write",
			statement: `DELETE FROM "sites" WHERE "siteId" IN (?)`,
		},
		{
			label:     "unit drop",
			probe:     "SaveSystemUnits",
			kind:      "write",
			statement: `DELETE FROM "units" WHERE "unitId" IN (?)`,
		},
		{
			label:     "system drop",
			probe:     "SaveSystems",
			kind:      "write",
			statement: `DELETE FROM "systems" WHERE "systemId" IN (?)`,
		},
		{
			label:     "group link drop",
			probe:     "SaveSystemTalkgroups",
			kind:      "write",
			statement: `DELETE FROM "talkgroupGroups" WHERE "talkgroupId" = ? AND "groupId" IN (?)`,
		},
		{
			label:     "group link add",
			probe:     "SaveSystemTalkgroups",
			kind:      "write",
			statement: `INSERT INTO "talkgroupGroups" ("groupId", "talkgroupId") VALUES (?, ?)`,
		},
		{
			label:     "delayed clear",
			probe:     "ClearDelayed",
			kind:      "write",
			statement: `DELETE FROM "delayed"`,
		},
	}
}

// TestChildReadsAndCascadeReachTheirIndexes asks how the database would run the reads of one call's frequencies,
// patches and units, the retention prune with the deletes its cascades make, and the deletes a configuration save
// makes to the tables those calls hang from. Each reaches a key the set names or the key its own table is stored
// in, and none of them builds a key for the delete it is running. Each delete is explained and not run, so the
// fixture the other assertions read is left as it is.
func TestChildReadsAndCascadeReachTheirIndexes(t *testing.T) {
	db := migratedLegacyDatabase(t)

	span := historySpanOfTheDatabase(t, db)

	values := historyPlanValues(t, db, span)

	inventory := callHistoryStatementInventory(span)

	writes := statementsOfTheConfigurationWrites()

	// The rows hanging from one call are read through the key each of those tables carries over the call it
	// belongs to.
	for _, one := range []struct{ label, index string }{
		{label: "frequencies by call", index: "callfrequencies_call"},
		{label: "patches by call", index: "callpatches_call"},
		{label: "units by call", index: "callunits_call"},
	} {
		plan := planForLabel(t, db, inventory, values, one.label)

		assertNothingBuiltForTheQuery(t, one.label, plan)

		if !planSeeksIndexOn(plan, one.index, "callId") {
			t.Errorf("the %s read would run as %s: %s is not narrowed on the call its rows belong to",
				one.label, plan, one.index)
		}
	}

	// The keys each of those tables, and the delay list beside them, carries over the call a row belongs to.
	callChildKeys := []struct{ index, column string }{
		{index: delayedKeyIndex, column: "callId"},
		{index: "callunits_call", column: "callId"},
		{index: "callpatches_call", column: "callId"},
		{index: "callfrequencies_call", column: "callId"},
	}

	// The retention prune deletes the calls older than a timestamp, which is the range the covering key is
	// narrowed on, and the cascades that follow each reach the key of the table they delete from.
	prune := planForLabel(t, db, writes, values, "prune delete")

	assertNothingBuiltForTheQuery(t, "prune delete", prune)

	assertPlanHasStep(t, "prune delete", prune, "SEARCH calls USING COVERING INDEX "+coveringKeyIndex+" (timestamp<?)")

	for _, key := range callChildKeys {
		if !planSeeksIndexOn(prune, key.index, key.column) {
			t.Errorf("the prune delete would run as %s: the cascade to %s does not reach the key over the call its "+
				"rows belong to", prune, key.index)
		}
	}

	// The delete of one call finds that call by the key the calls table is stored in, and the cascades beside it
	// reach the same child keys the prune's do.
	dropOne := planForLabel(t, db, writes, values, "call drop")

	assertNothingBuiltForTheQuery(t, "call drop", dropOne)

	if !planSeeksAPrimaryKey(dropOne) {
		t.Errorf("the delete of one call would run as %s: the call is not found by the key its own table is stored in",
			dropOne)
	}

	if walked := planWalksTablesWithoutAnIndex(dropOne); len(walked) != 0 {
		t.Errorf("the delete of one call would run as %s: it walks %v where the set names a key", dropOne, walked)
	}

	for _, key := range callChildKeys {
		if !planSeeksIndexOn(dropOne, key.index, key.column) {
			t.Errorf("the delete of one call would run as %s: the cascade to %s does not reach the key over the call "+
				"its rows belong to", dropOne, key.index)
		}
	}

	// A delete of a system, a talkgroup, a tag or a group reaches the key of each table its cascades read. Each
	// of these is explained, so the rows the cascade would remove stay where they are.
	for _, one := range []struct{ label, index, column string }{
		{label: "talkgroup drop", index: "talkgroupgroups_talkgroup", column: "talkgroupId"},
		{label: "tag drop", index: "talkgroups_tag", column: "tagId"},
		{label: "group drop", index: "talkgroupgroups_group", column: "groupId"},
		{label: "system drop", index: "talkgroups_system_ref", column: "systemId"},
		{label: "system drop", index: "sites_system", column: "systemId"},
		{label: "group link drop", index: "talkgroupgroups_group", column: "groupId"},
	} {
		plan := planForLabel(t, db, writes, values, one.label)

		assertNothingBuiltForTheQuery(t, one.label, plan)

		if !planSeeksIndexOn(plan, one.index, one.column) {
			t.Errorf("the %s would run as %s: %s is not narrowed on %s", one.label, plan, one.index, one.column)
		}
	}

	// The calls a parent delete cascades into are read through a key the set names: the covering key for the
	// calls of one talkgroup, the shipped pair key for the calls of one system.
	if plan := planForLabel(t, db, writes, values, "talkgroup drop"); !planWalksIndex(plan, coveringKeyIndex) {
		t.Errorf("the talkgroup delete would run as %s: its calls are not read through the covering key", plan)
	}

	if plan := planForLabel(t, db, writes, values, "system drop"); !planWalksIndex(plan, shippedPairIndexName) {
		t.Errorf("the system delete would run as %s: its calls are not read through the shipped pair key", plan)
	}

	// A delete of one row a table is keyed by finds that row by the key the table is stored in, and the tables
	// hanging from it reach their own keys.
	for _, label := range []string{"site drop", "unit drop", "talkgroup drop", "tag drop", "group drop", "system drop"} {
		plan := planForLabel(t, db, writes, values, label)

		if !planSeeksAPrimaryKey(plan) {
			t.Errorf("the %s would run as %s: the row is not found by the key its own table is stored in", label, plan)
		}
	}

	// An insert names no key in its plan: the engine adds the row to the table and to every key over it, and the
	// keys it has to maintain are the ones the catalogue test finds. What an insert must not do is answer with a
	// key it made for the query it is running.
	for _, entry := range append(inventory, writes...) {
		if entry.kind != "write" {
			continue
		}

		plan := planForStatement(t, db, entry.statement, values)

		assertNothingBuiltForTheQuery(t, entry.label, plan)

		if entry.label == "delayed drop" || strings.HasPrefix(entry.statement, "DELETE FROM") {
			continue
		}

		if plan != "" {
			t.Errorf("the %s would run as %s: an insert names no key in its plan, want the plan to name no step",
				entry.label, plan)
		}
	}
}

// TestDelayedJoinUsesTheNamedDelayedIndex covers the join every history read carries, the delay list's own reads
// and deletes, and the duplicate guard a saved call is checked against. The join reaches the key the delay list
// carries over the call it names, and no read or delete in this group builds a key for the query it is running.
func TestDelayedJoinUsesTheNamedDelayedIndex(t *testing.T) {
	db := migratedLegacyDatabase(t)

	span := historySpanOfTheDatabase(t, db)

	values := historyPlanValues(t, db, span)

	inventory := callHistoryStatementInventory(span)

	// Every history read joins the delay list to answer that a call still waiting is not part of the history,
	// and each of those joins reaches the delay list's key by the call it looks for.
	const delayedJoinStep = "SEARCH d USING COVERING INDEX " + delayedKeyIndex + " (callId=?) LEFT-JOIN"

	joined := 0

	for _, entry := range inventory {
		if entry.kind != "read" || !strings.Contains(entry.statement, `LEFT JOIN "delayed" AS d`) {
			continue
		}

		joined++

		plan := planForStatement(t, db, entry.statement, values)

		assertNothingBuiltForTheQuery(t, entry.label, plan)

		if !hasString(strings.Split(plan, " / "), delayedJoinStep) {
			t.Errorf("the %s read would run as %s, want the join to the delay list to run as %s", entry.label, plan,
				delayedJoinStep)
		}
	}

	if joined == 0 {
		t.Fatal("the inventoried statements carry no read that joins the delay list")
	}

	// A call taken off the delay list is found by that key, and the whole delay list is read along it.
	assertPlanHasStep(t, "delayed drop", planForLabel(t, db, inventory, values, "delayed drop"),
		"SEARCH delayed USING COVERING INDEX "+delayedKeyIndex+" (callId=?)")

	assertPlanHasStep(t, "delayed list", planForLabel(t, db, inventory, values, "delayed list"),
		"SCAN delayed USING INDEX "+delayedKeyIndex)

	// The delay list emptied entirely has no filter to narrow a key with, so it reads the table, and it still
	// builds nothing for the delete it runs.
	cleared := planForLabel(t, db, statementsOfTheConfigurationWrites(), values, "delayed clear")

	assertPlanHasStep(t, "delayed clear", cleared, "SCAN delayed")

	assertNothingBuiltForTheQuery(t, "delayed clear", cleared)

	// The duplicate guard reads the calls a saved call could be a duplicate of. Its filters are the columns of
	// the pair key the shipped database carries, and the read carries no join to the delay list, so that key
	// answers the whole read.
	guard := planForLabel(t, db, inventory, values, "duplicate check")

	assertNothingBuiltForTheQuery(t, "duplicate check", guard)

	assertPlanHasStep(t, "duplicate check", guard, "SEARCH c USING COVERING INDEX "+shippedPairIndexName+
		" (systemId=? AND siteRef=? AND talkgroupId=? AND timestamp>? AND timestamp<?)")

	if strings.Contains(inventoriedStatementFor(t, inventory, "duplicate check").statement, `"delayed"`) {
		t.Error("the duplicate guard reads the delay list, which is not the read the measurements name")
	}

	// The state the key was measured for: on a database that lost the delay list's key, the same join builds a
	// key for the query it belongs to, which is what every read above is asserted not to do.
	without := migratedLegacyDatabase(t)

	if _, err := without.Exec(`DROP INDEX "` + delayedKeyIndex + `"`); err != nil {
		t.Fatalf("removing the key the delay list is read through: %v", err)
	}

	spanWithout := historySpanOfTheDatabase(t, without)

	planWithout := planForLabel(t, without, callHistoryStatementInventory(spanWithout),
		historyPlanValues(t, without, spanWithout), "all page 25")

	for _, marker := range []string{"BLOOM FILTER ON d (callId=?)", "SEARCH d USING AUTOMATIC COVERING INDEX (callId=?)"} {
		if !strings.Contains(planWithout, marker) {
			t.Errorf("the whole-history page without the delay list's key would run as %s, want it to answer with %s: "+
				"the assertion above only bites if losing that key makes the join build a key for itself", planWithout,
				marker)
		}
	}
}

// TestLookupAndJoinReadsReachTheirKeys covers the reads a configuration save and a configuration read run beside
// the history: the talkgroup a call names is looked up through the key over the system it belongs to and its
// reference, both sides of the group-membership table are read through their own keys, the sites of a system are
// read through the key over that system, and a guard for one row reaches the key its table is stored in.
func TestLookupAndJoinReadsReachTheirKeys(t *testing.T) {
	db := migratedLegacyDatabase(t)

	span := historySpanOfTheDatabase(t, db)

	values := historyPlanValues(t, db, span)

	inventory := callHistoryStatementInventory(span)

	outside := statementsOutsideTheReports(span)

	// The talkgroup a saved call names is found by the key over the system it belongs to and the reference it
	// carries there.
	lookup := planForLabel(t, db, inventory, values, "talkgroup lookup")

	assertNothingBuiltForTheQuery(t, "talkgroup lookup", lookup)

	if !planSeeksIndexOn(lookup, "talkgroups_system_ref", "systemId") {
		t.Errorf("the talkgroup lookup would run as %s: it is not narrowed on the system the talkgroup belongs to", lookup)
	}

	if !strings.Contains(planConstraintForTable(lookup, "talkgroups"), "talkgroupRef=?") {
		t.Errorf("the talkgroup lookup would run as %s: the key it reaches is not narrowed on the reference the "+
			"talkgroup carries on that system", lookup)
	}

	// Both sides of the group-membership table are read through the key over that side.
	members := planForLabel(t, db, inventory, values, "group members")

	assertNothingBuiltForTheQuery(t, "group members", members)

	if !planSeeksIndexOn(members, "talkgroupgroups_talkgroup", "talkgroupId") {
		t.Errorf("the members of a group would run as %s: the membership table is not narrowed on the talkgroup it "+
			"is read for", members)
	}

	guard := planForStatement(t, db, statementOutsideTheInventory(t, outside,
		`FROM "talkgroupGroups" WHERE "talkgroupId" = ? AND "groupId" = ?`), values)

	assertNothingBuiltForTheQuery(t, "group link guard", guard)

	if !planSeeksIndexOn(guard, "talkgroupgroups_group", "groupId") {
		t.Errorf("the guard a group link is checked against would run as %s: the membership table is not narrowed on "+
			"the group the link names", guard)
	}

	// The stored talkgroups of a system, and the talkgroups of a system a configuration read answers with the
	// groups each of them belongs to, reach the key over the system and the key over the membership side.
	stored := planForStatement(t, db, statementOutsideTheInventory(t, outside,
		`SELECT "talkgroupId" FROM "talkgroups" WHERE "systemId" = ?`), values)

	assertNothingBuiltForTheQuery(t, "stored talkgroups of a system", stored)

	if !planSeeksIndexOn(stored, "talkgroups_system_ref", "systemId") {
		t.Errorf("the stored talkgroups of a system would run as %s: they are not narrowed on the system they belong to",
			stored)
	}

	aggregate := planForLabel(t, db, inventory, values, "talkgroups by system")

	assertNothingBuiltForTheQuery(t, "talkgroups by system", aggregate)

	if !planSeeksIndexOn(aggregate, "talkgroups_system_ref", "systemId") {
		t.Errorf("the talkgroups of a system would run as %s: they are not narrowed on the system they belong to",
			aggregate)
	}

	if !planSeeksIndexOn(aggregate, "talkgroupgroups_talkgroup", "talkgroupId") {
		t.Errorf("the talkgroups of a system would run as %s: the groups named beside each of them are not read "+
			"through the key over the membership side", aggregate)
	}

	// The sites of a system are read through the key over that system.
	sites := planForStatement(t, db, statementOutsideTheInventory(t, outside, `FROM "sites" WHERE "systemId" = ?`), values)

	assertNothingBuiltForTheQuery(t, "stored sites of a system", sites)

	if !planSeeksIndexOn(sites, "sites_system", "systemId") {
		t.Errorf("the sites of a system would run as %s: they are not narrowed on the system they belong to", sites)
	}

	// A guard for one row reaches the key that table is stored in, which is the shortest read that table can get.
	for _, fragment := range []string{
		`SELECT COUNT(*) FROM "systems" WHERE "systemId" = ?`,
		`SELECT COUNT(*) FROM "talkgroups" WHERE "talkgroupId" = ?`,
	} {
		plan := planForStatement(t, db, statementOutsideTheInventory(t, outside, fragment), values)

		assertNothingBuiltForTheQuery(t, fragment, plan)

		if !planSeeksAPrimaryKey(plan) {
			t.Errorf("%s would run as %s: the guard does not reach the key its own table is stored in", fragment, plan)
		}
	}

	// One call read back by its identifier reaches the calls table, its system and its talkgroup by the keys those
	// three tables are stored in.
	one := planForLabel(t, db, inventory, values, "call by id")

	assertNothingBuiltForTheQuery(t, "call by id", one)

	for _, table := range []string{"c", "s", "t"} {
		steps := planStepsForTable(one, table)

		if len(steps) != 1 || !strings.Contains(steps[0], "USING INTEGER PRIMARY KEY") {
			t.Errorf("the call read back by its identifier would run as %s: %s is not read by the key its own table "+
				"is stored in", one, table)
		}
	}
}

// TestTalkgroupSearchReadStaysUnindexed records the two reads the set does not serve: the page of one system and
// one talkgroup, and the windowed read a talkgroup search runs. Neither is given a key of its own, so the key
// that answers each is scanned or narrowed only on the timestamp the read is bounded by, and the pair filter is
// answered while the rows are walked. The shape of the history filters is pinned by the test that reads the set
// itself, which is what keeps these two reads from being handed a key of their own later.
func TestTalkgroupSearchReadStaysUnindexed(t *testing.T) {
	db := migratedLegacyDatabase(t)

	span := historySpanOfTheDatabase(t, db)

	values := historyPlanValues(t, db, span)

	inventory := callHistoryStatementInventory(span)

	unserved := []string{
		"system talkgroup page 25",
		"search talkgroup window page 25",
		"search talkgroup window count",
		"search talkgroup window span oldest",
		"search talkgroup window span newest",
	}

	for _, label := range unserved {
		entry := inventoriedStatementFor(t, inventory, label)

		plan := planForStatement(t, db, entry.statement, values)

		assertNothingBuiltForTheQuery(t, label, plan)

		if !planWalksIndex(plan, coveringKeyIndex) && !planWalksIndex(plan, shippedPairIndexName) {
			t.Errorf("the %s read would run as %s: no key the set names answers it", label, plan)
		}

		if !planScansAKey(plan, "c") && !planSeeksIndexOn(plan, coveringKeyIndex, "timestamp") &&
			!historyReadSortsTheHistory(plan, entry.statement) {
			t.Errorf("the %s read would run as %s: the read neither scans a key, nor is narrowed on the timestamp "+
				"that bounds it, nor sorts, so a key the set does not name appears to answer it", label, plan)
		}

		// The pair filter is not what a key is narrowed on: only a timestamp a read is bounded by is.
		constraint := planConstraintForTable(plan, "c")

		for _, term := range strings.Split(constraint, " AND ") {
			if term != "" && !strings.Contains(term, "timestamp") {
				t.Errorf("the %s read would run as %s: %s narrows a key with a column no key of the set carries in "+
					"that order", label, plan, term)
			}
		}

		for _, name := range planIndexNames(plan) {
			if !hasString(namedCallHistoryKeys(), name) {
				t.Errorf("the %s read would run as %s through %s, which is not a key the set names", label, plan, name)
			}
		}
	}
}

// The history a read budget is judged against is the shape a long-running deployment reaches: ninety days
// of calls across twelve systems, three hundred talkgroups on each, six hundred talkgroup units, and a
// skewed share of the traffic on a handful of talkgroups. One shape at every scale, because the lookup
// tables keep their sizes as the call count grows, which is what makes a smaller fixture the same world
// with fewer calls per talkgroup and a cheaper read to answer.
const (
	historyFixtureDays         = 90
	historyFixtureSystems      = 12
	historyFixtureTalkgroups   = 300
	historyFixtureSites        = 4
	historyFixtureGroups       = 120
	historyFixtureTags         = 40
	historyFixtureUnits        = 600
	historyFixtureAudioBytes   = 4096
	historyFixturePatchPercent = 10

	// historyFixtureDelayedShare is how much of the history the queue holds back: one call in three
	// hundred, which is the share the sixty-thousand-call history carries when two hundred of its calls
	// are held. The held calls are the most recent ones, and keeping the share fixed as the history grows
	// is what leaves a day's worth of calls for a day-bounded read to answer at every scale.
	historyFixtureDelayedShare = 300

	// historyFixtureSeedRows is how many calls one seeding statement carries. One row per statement is the
	// right shape for a forty-row deployment, because a failure names the row; a hundred rows of a call
	// table is a statement of a few hundred kilobytes, which every backend here accepts at its defaults.
	historyFixtureSeedRows = 100
)

// historyFixtureSystem and historyFixtureTalkgroup carry the two identifiers each history filter is
// built from: the row a lookup table stores a system or a talkgroup as, and the reference the caller
// names it by.
type historyFixtureSystem struct {
	id  int64
	ref int
}

type historyFixtureTalkgroup struct {
	id     int64
	ref    int
	system int64
}

// historyFixtureWorld is the configuration half of the history: the systems and their sites, the
// talkgroups with their tags, the groups that hold them, and the units a call names. The calls themselves
// are built beside it, oldest first, so callId order and timestamp order agree the way they do in a live
// archive.
type historyFixtureWorld struct {
	systems    []historyFixtureSystem
	talkgroups []historyFixtureTalkgroup
	sites      []int64
	units      []int64
}

// historyFixtureDatabase returns a database holding the given number of calls in that shape, in the state
// every budget is judged in: the set created over it, and no table holding planner statistics.
//
// The database is built from the shipped schema rather than carried across a migration, which is the state
// a deployment that has finished its startup pass is in. The covering key arrives from the step that
// creates it, after the column it indexes exists, exactly as it does on a migrated deployment.
func historyFixtureDatabase(t *testing.T, calls int) *sql.DB {
	t.Helper()

	db := openSchemaDatabase(t)

	ctx := context.Background()

	rng := rand.New(rand.NewSource(1))

	world := seedHistoryLookups(t, ctx, db, rng)

	seedHistoryCalls(t, ctx, db, world, rng, calls)

	if err := ensureCallIndexSet(ctx, newStore(db, DialectSqlite), func(level, message string) {
		t.Logf("ensureCallIndexSet over the history fixture: %s %s", level, message)
	}); err != nil {
		t.Fatalf("ensureCallIndexSet over the history fixture: %v", err)
	}

	if found := plannerStatisticsTables(t, db); len(found) != 0 {
		t.Fatalf("the history fixture carries %v, so its reads are not measured in the state every budget "+
			"is stated in", found)
	}

	return db
}

// seedHistoryLookups fills the configuration tables the history joins against. Talkgroup references
// follow production's numbering, where the leading digits place a talkgroup on its system, which is what
// makes the pair filters the history view builds name references a caller could really be shown.
func seedHistoryLookups(t *testing.T, ctx context.Context, db *sql.DB, rng *rand.Rand) historyFixtureWorld {
	t.Helper()

	world := historyFixtureWorld{}

	for i := range historyFixtureSystems {
		ref := 1001 + i

		res, err := db.ExecContext(ctx,
			`INSERT INTO "systems" ("label", "delay", "order", "systemRef", "type") VALUES(?, ?, ?, ?, ?)`,
			fmt.Sprintf("System %02d", i+1), 0, i, ref, "type-1")
		if err != nil {
			t.Fatalf("seeding system %d: %v", ref, err)
		}

		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("reading the system %d the fixture stores: %v", ref, err)
		}

		world.systems = append(world.systems, historyFixtureSystem{id: id, ref: ref})

		for s := range historyFixtureSites {
			siteRef := int64((i+1)*100 + s + 1)

			if _, err := db.ExecContext(ctx,
				`INSERT INTO "sites" ("label", "order", "siteRef", "systemId") VALUES(?, ?, ?, ?)`,
				fmt.Sprintf("Site %03d", siteRef), s, siteRef, id); err != nil {
				t.Fatalf("seeding site %d: %v", siteRef, err)
			}

			world.sites = append(world.sites, siteRef)
		}
	}

	for i := range historyFixtureTags {
		if _, err := db.ExecContext(ctx, `INSERT INTO "tags" ("label", "order") VALUES(?, ?)`,
			fmt.Sprintf("tag-%02d", i+1), i); err != nil {
			t.Fatalf("seeding tag %d: %v", i+1, err)
		}
	}

	for i := range historyFixtureGroups {
		if _, err := db.ExecContext(ctx, `INSERT INTO "groups" ("label", "order") VALUES(?, ?)`,
			fmt.Sprintf("group-%03d", i+1), i); err != nil {
			t.Fatalf("seeding group %d: %v", i+1, err)
		}
	}

	tagIDs := columnValues(t, ctx, db, `SELECT "tagId" FROM "tags" ORDER BY "tagId"`, "tags")

	groupIDs := columnValues(t, ctx, db, `SELECT "groupId" FROM "groups" ORDER BY "groupId"`, "groups")

	for _, sys := range world.systems {
		for i := range historyFixtureTalkgroups {
			ref := 10_000 + sys.ref*1000 + i + 1

			res, err := db.ExecContext(ctx,
				`INSERT INTO "talkgroups" ("delay", "frequency", "label", "name", "order", "systemId", `+
					`"tagId", "talkgroupRef", "type") VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				0, 851_000_000+i*12_500, fmt.Sprintf("label-%04d", ref), fmt.Sprintf("Talkgroup %04d", ref),
				i, sys.id, tagIDs[(i*7)%len(tagIDs)], ref, "type-2")
			if err != nil {
				t.Fatalf("seeding talkgroup %d: %v", ref, err)
			}

			id, err := res.LastInsertId()
			if err != nil {
				t.Fatalf("reading the talkgroup %d the fixture stores: %v", ref, err)
			}

			world.talkgroups = append(world.talkgroups, historyFixtureTalkgroup{id: id, ref: ref, system: sys.id})
		}
	}

	// Group membership is the reverse index the group filter reads: four talkgroups per group, taken from
	// the history's own talkgroups so a group named by a caller has calls behind it.
	for i, groupID := range groupIDs {
		start := (i * 5) % len(world.talkgroups)

		for k := range 4 {
			talkgroup := world.talkgroups[(start+k)%len(world.talkgroups)]

			if _, err := db.ExecContext(ctx,
				`INSERT INTO "talkgroupGroups" ("groupId", "talkgroupId") VALUES(?, ?)`,
				groupID, talkgroup.id); err != nil {
				t.Fatalf("seeding group %d's talkgroups: %v", groupID, err)
			}
		}
	}

	for i := range historyFixtureUnits {
		ref := int64(20_000 + i)

		if _, err := db.ExecContext(ctx,
			`INSERT INTO "units" ("label", "order", "systemId", "unitRef") VALUES(?, ?, ?, ?)`,
			fmt.Sprintf("Unit-%05d", ref), i, world.systems[i%len(world.systems)].id, ref); err != nil {
			t.Fatalf("seeding unit %d: %v", ref, err)
		}

		world.units = append(world.units, ref)
	}

	return world
}

// seedHistoryCalls writes the history and the rows that hang off each call. Talkgroup choice is skewed,
// because a handful of talkgroups carry most of a deployment's traffic, which is what a per-talkgroup key
// would be measured against; the calls a search has to anti-join out of its results are the most recent
// ones, which is the state a live queue is in.
func seedHistoryCalls(t *testing.T, ctx context.Context, db *sql.DB, world historyFixtureWorld, rng *rand.Rand, calls int) {
	t.Helper()

	newest := time.Now().UTC().Truncate(time.Second)
	oldest := newest.Add(-historyFixtureDays * 24 * time.Hour)

	span := newest.UnixMilli() - oldest.UnixMilli()

	audio := make([]byte, historyFixtureAudioBytes)
	for i := range audio {
		audio[i] = byte(i)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("beginning the transaction that seeds %d calls: %v", calls, err)
	}

	defer tx.Rollback()

	// Every call is planned before any row is written, so a child row can name a call that is already in
	// the table when that child is inserted, and the identifiers of a batch stay in the order the history
	// is stored in.
	ids := make([]int64, calls)
	talkgroups := make([]historyFixtureTalkgroup, calls)
	sites := make([]int64, calls)
	timestamps := make([]int64, calls)

	for i := range calls {
		talkgroup := world.talkgroups[historyFixtureZipf(len(world.talkgroups), rng)]

		ids[i] = int64(i + 1)
		talkgroups[i] = talkgroup
		sites[i] = world.sites[int(talkgroup.system)%len(world.sites)]
		timestamps[i] = oldest.UnixMilli() + int64(i)*span/int64(calls) + int64(rng.Intn(1000))
	}

	written := newHistoryFixtureBatch(t, tx, "calls",
		"callId", "audio", "audioFilename", "audioLength", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp")

	for i := range calls {
		written.add(ids[i], audio, fmt.Sprintf("rdio-%d.wav", i), 10_000+rng.Intn(30_000), "audio/wav",
			sites[i], talkgroups[i].system, talkgroups[i].id, timestamps[i])
	}

	written.flush()

	children := newHistoryFixtureBatch(t, tx, "callFrequencies",
		"callId", "dbm", "errors", "frequency", "offset", "spikes")

	for i := range calls {
		for range 1 + rng.Intn(2) {
			children.add(ids[i], -100.0, 0, 851_000_000+rng.Intn(1_000_000), 0.0, 0)
		}
	}

	children.flush()

	units := newHistoryFixtureBatch(t, tx, "callUnits", "callId", "offset", "unitRef")

	for i := range calls {
		for range 1 + rng.Intn(3) {
			units.add(ids[i], rng.Float64()*5_000, world.units[rng.Intn(len(world.units))])
		}
	}

	units.flush()

	patches := newHistoryFixtureBatch(t, tx, "callPatches", "callId", "talkgroupId")

	for i := range calls {
		if rng.Intn(100) >= historyFixturePatchPercent {
			continue
		}

		other := world.talkgroups[historyFixtureZipf(len(world.talkgroups), rng)]
		if other.id == talkgroups[i].id {
			continue
		}

		patches.add(ids[i], other.id)
	}

	patches.flush()

	// The held calls are the most recent ones, which is the state a live queue is in when a search page has
	// to anti-join them out of the rows it reads.
	held := calls / historyFixtureDelayedShare
	if held < 1 {
		held = 1
	}

	heldRows := newHistoryFixtureBatch(t, tx, "delayed", "callId", "timestamp")

	for i := calls - held; i < calls; i++ {
		heldRows.add(ids[i], timestamps[i])
	}

	heldRows.flush()

	if err := tx.Commit(); err != nil {
		t.Fatalf("committing %d calls into the history fixture: %v", calls, err)
	}

	if got := countRows(t, db, "calls"); got != calls {
		t.Fatalf("the history fixture holds %d calls, want %d", got, calls)
	}

	for _, table := range []string{"callFrequencies", "callUnits", "callPatches", "delayed"} {
		if got := countRows(t, db, table); got == 0 {
			t.Fatalf("the history fixture holds no %s rows", table)
		}
	}

	t.Logf("the history fixture holds %d calls over %d days, %d of them held back by the queue, against the "+
		"%d systems and %d talkgroups of one deployment", calls, historyFixtureDays, held,
		historyFixtureSystems, len(world.talkgroups))
}

// historyFixtureBatch collects the rows one seeding statement carries and writes them together, because a
// table of a hundred thousand single-row statements measures the harness writing them rather than the
// reads the fixture is built to time. A failure names the table it was writing into.
type historyFixtureBatch struct {
	t       *testing.T
	tx      *sql.Tx
	table   string
	columns []string
	rows    []string
	args    []any
}

func newHistoryFixtureBatch(t *testing.T, tx *sql.Tx, table string, columns ...string) *historyFixtureBatch {
	t.Helper()

	return &historyFixtureBatch{t: t, tx: tx, table: table, columns: columns}
}

// add holds one row, and writes what the batch has collected once it carries a full statement's worth.
func (b *historyFixtureBatch) add(row ...any) {
	if len(row) != len(b.columns) {
		b.t.Fatalf("seeding %s: the row carries %d values against the %d columns it names", b.table, len(row), len(b.columns))
	}

	holders := make([]string, 0, len(row))

	for range row {
		holders = append(holders, "?")
	}

	b.rows = append(b.rows, "("+strings.Join(holders, ", ")+")")
	b.args = append(b.args, row...)

	if len(b.rows) == historyFixtureSeedRows {
		b.flush()
	}
}

// flush writes the rows the batch is holding and empties it, so the next table starts a fresh statement.
func (b *historyFixtureBatch) flush() {
	b.t.Helper()

	if len(b.rows) == 0 {
		return
	}

	names := make([]string, 0, len(b.columns))

	for _, column := range b.columns {
		names = append(names, `"`+column+`"`)
	}

	statement := `INSERT INTO "` + b.table + `" (` + strings.Join(names, ", ") + `) VALUES ` +
		strings.Join(b.rows, ", ")

	if _, err := b.tx.Exec(statement, b.args...); err != nil {
		b.t.Fatalf("seeding %d rows of %s: %v", len(b.rows), b.table, err)
	}

	b.rows = b.rows[:0]
	b.args = b.args[:0]
}

// historyFixtureZipf picks which talkgroup a call belongs to: half of the traffic lands on the busiest
// twentieth of them, four tenths more on the next tenth, and the rest is spread over the whole set, which
// is the share a deployment's traffic actually takes.
func historyFixtureZipf(n int, rng *rand.Rand) int {
	r := rng.Float64()

	if r < 0.5 {
		return rng.Intn(n / 20)
	}

	if r < 0.9 {
		return n/20 + rng.Intn(n/10)
	}

	return rng.Intn(n)
}

// columnValues reads one column of a configuration table in the order the table stores it.
func columnValues(t *testing.T, ctx context.Context, db *sql.DB, statement, table string) []int64 {
	t.Helper()

	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		t.Fatalf("reading the identifiers of %s: %v", table, err)
	}

	defer rows.Close()

	found := []int64{}

	for rows.Next() {
		var value int64

		if err := rows.Scan(&value); err != nil {
			t.Fatalf("reading an identifier of %s: %v", table, err)
		}

		found = append(found, value)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the identifiers of %s: %v", table, err)
	}

	if len(found) == 0 {
		t.Fatalf("the history fixture holds no %s rows to build a history against", table)
	}

	return found
}

// historyBudgetScale and historyUpperBudgetScale are the two scales the specification states its read
// figures at. A fixture smaller than the lower of them is judged against the lower scale's figures, which
// is the stricter of the two ways a count is budgeted.
const (
	historyBudgetScale      = 60_000
	historyUpperBudgetScale = 150_000
)

// historyReadBudget answers the ceiling the specification sets for the read one label names at a given
// number of calls. The figures the budget table states are absolute ceilings rather than speed-up factors,
// and a read the table does not name at a scale is judged against the ceiling that table sets for that
// shape of read at that scale: a page or a span inside five milliseconds, a count inside one hundred
// milliseconds at sixty thousand calls and one hundred and twenty at one hundred and fifty thousand, and a
// count bounded by a day inside fifteen milliseconds wherever it is read. A write carries no ceiling: the
// table sets none, and the cost of the delete a retention pass runs is budgeted by its own test.
func historyReadBudget(label string, calls int) time.Duration {
	if strings.HasSuffix(label, " count") {
		if label == "day count" {
			return 15 * time.Millisecond
		}

		if calls > historyBudgetScale {
			return 120 * time.Millisecond
		}

		return 100 * time.Millisecond
	}

	return 5 * time.Millisecond
}

// historyReadTiming is what one read costs on a fixture: the middle of the samples taken for it, which is
// the shape every figure in the specification is stated in, and the slowest of them.
type historyReadTiming struct {
	median time.Duration
	worst  time.Duration
}

// measureHistoryReads runs every inventoried read a fixed number of times over the fixture and answers
// what each costs. The samples are taken in rounds so a read is measured more than once against the same
// index set, and a budget is judged against the middle sample rather than the slowest one, because a
// history read is measured against a page it walks and the spread between two samples of one read is the
// noise floor every figure here is read against.
func measureHistoryReads(t *testing.T, db *sql.DB, inventory []inventoriedStatement, values map[string][]any) map[string]historyReadTiming {
	t.Helper()

	const rounds = 3
	const samplesPerRound = 5

	samples := map[string][]time.Duration{}

	for round := range rounds {
		for _, entry := range inventory {
			if entry.kind != "read" {
				continue
			}

			args := planArgumentsFor(entry.statement, values)

			started := time.Now()

			rows, err := db.Query(entry.statement, args...)
			if err != nil {
				t.Fatalf("running the %s read: %v", entry.label, err)
			}

			found := 0

			for rows.Next() {
				found++
			}

			rows.Close()

			if err := rows.Err(); err != nil {
				t.Fatalf("reading the rows the %s read answers: %v", entry.label, err)
			}

			taken := time.Since(started)

			samples[entry.label] = append(samples[entry.label], taken)

			t.Logf("round %d: the %s read answers %d rows in %s", round+1, entry.label, found, taken)
		}
	}

	timings := map[string]historyReadTiming{}

	for label, times := range samples {
		slices.Sort(times)

		timings[label] = historyReadTiming{median: times[len(times)/2], worst: times[len(times)-1]}
	}

	return timings
}

// assertHistoryReadsWithinTheirBudgets is the check every scale of the history is judged by: each
// inventoried read reaches a key the set names rather than one the database builds for that read, no read
// sorts the history it walks, and the middle of its samples stays inside the ceiling the specification sets
// for that shape of read at that scale.
func assertHistoryReadsWithinTheirBudgets(t *testing.T, db *sql.DB, inventory []inventoriedStatement,
	values map[string][]any, calls int) {
	t.Helper()

	for _, entry := range inventory {
		if entry.kind != "read" {
			continue
		}

		plan := planForStatement(t, db, entry.statement, values)

		t.Logf("the %s read runs as %s", entry.label, plan)

		assertNothingBuiltForTheQuery(t, entry.label, plan)

		if historyReadSortsTheHistory(plan, entry.statement) {
			t.Errorf("the %s read would run as %s: it sorts the history it reads instead of reading it in "+
				"the order a key of the set stores it", entry.label, plan)
		}
	}

	timings := measureHistoryReads(t, db, inventory, values)

	for _, entry := range inventory {
		if entry.kind != "read" {
			continue
		}

		got := timings[entry.label]
		ceiling := historyReadBudget(entry.label, calls)

		t.Logf("at %d calls the %s read costs %s (slowest sample %s) against the %s ceiling a read of that "+
			"shape carries at that scale", calls, entry.label, got.median, got.worst, ceiling)

		if got.median > ceiling {
			t.Errorf("the %s read costs %s at %d calls, against the %s ceiling a read of that shape carries "+
				"at that scale: its slowest sample cost %s", entry.label, got.median, calls, ceiling, got.worst)
		}
	}
}

// TestHistoryReadsStayWithinTheirBudgetsAtTenThousandCalls holds every inventoried read of a deployment's
// history inside the ceiling its shape carries, on a fixture large enough that walking the history without
// a key is what a read would actually cost. The plan-shape tests read the same statements over a
// forty-call database, where a read is cheap enough that a plan choice could go unnoticed; the budgets are
// what make a read that loses its key fail instead of merely looking worse in a plan.
func TestHistoryReadsStayWithinTheirBudgetsAtTenThousandCalls(t *testing.T) {
	const calls = 10_000

	db := historyFixtureDatabase(t, calls)

	span := historySpanOfTheDatabase(t, db)

	values := historyPlanValues(t, db, span)

	inventory := callHistoryStatementInventory(span)

	assertHistoryReadsWithinTheirBudgets(t, db, inventory, values, calls)
}
