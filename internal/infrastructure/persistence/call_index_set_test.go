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
	"slices"
	"strings"
	"testing"
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

func missingNames(indexes []indexSpec) []string {
	names := []string{}

	for _, index := range indexes {
		names = append(names, index.name)
	}

	return names
}

// indexShapes writes each index the probe reported as the name it carries with the columns
// it carries in index order, sorted, so that two catalogues can be compared as two lists.
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
