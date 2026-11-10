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
	"fmt"
	"strings"

	"rdio-scanner/internal/domain"
)

// indexSpec names one secondary index: the name it is created under, the table it
// sits on, and the columns it carries in the order it carries them. A key is
// identified by its table and column list rather than by its name, because the
// schema's foreign keys make an index of their own over several of these column
// lists under names of the engine's choosing.
type indexSpec struct {
	name    string
	table   string
	columns []string
}

// callIndexSet is the set of secondary indexes the call history reads are written
// against, in the order the set is documented. It is the one list the schema copies,
// the startup pass and the tests all read, so a key cannot exist in one of them and be
// missing from another.
//
// The keys over a foreign key's own column serve the child lookups a call detail read
// performs, the joins that scope a history page to a system, a tag or a group, and the
// duplicate guard and drop that the delayed table takes part in.
//
// The covering key leads with the timestamp every history read orders on and carries the
// columns those reads report, so an unfiltered page answers from the index without reading
// a table row. It is the one member the copied schema does not declare: it indexes the
// recording length column, which a database the legacy server wrote gains only after that
// copy has been applied, and an index created over a column the table does not have yet is
// refused outright on PostgreSQL and stored wrong on SQLite. The startup pass creates it
// after that column exists, on every backend.
//
// The talkgroup search path is not indexed by this set: its reads look for a reference or
// a label inside a talkgroup, which is a scan a key over a fixed column list would not
// serve.
var callIndexSet = []indexSpec{
	{name: "callfrequencies_call", table: "callFrequencies", columns: []string{"callId"}},
	{name: "callpatches_call", table: "callPatches", columns: []string{"callId"}},
	{name: "callunits_call", table: "callUnits", columns: []string{"callId"}},
	{name: "delayed_call", table: "delayed", columns: []string{"callId"}},
	{name: "talkgroups_system_ref", table: "talkgroups", columns: []string{"systemId", "talkgroupRef"}},
	{name: "talkgroups_tag", table: "talkgroups", columns: []string{"tagId"}},
	{name: "talkgroupgroups_talkgroup", table: "talkgroupGroups", columns: []string{"talkgroupId"}},
	{name: "talkgroupgroups_group", table: "talkgroupGroups", columns: []string{"groupId"}},
	{name: "sites_system", table: "sites", columns: []string{"systemId"}},
	{name: "calls_timestamp_cover", table: "calls", columns: []string{"timestamp", "callId", "audioLength", "systemId", "talkgroupId"}},
}

// hasCallIndexShape reports every index the backend carries on one table: the name it
// carries it under, and the columns it carries in index order. The names are the
// backend's own, so a key a foreign key clause made and a key the schema declared are
// reported the same way, and nothing here reads or compares the text of the statement
// that created an index — only the columns the finished index holds.
//
// A primary key is reported with the rest of them: it is an index the backend carries
// over a column list, and a member whose list happens to be that list is served by it.
// A key part that is not a plain column — an expression, or a column stored as a
// prefix — is reported as an empty column name in its place, so that the list keeps its
// length and the position of every part while being unable to match a member's list.
func hasCallIndexShape(ctx context.Context, s store, table string) ([]indexSpec, error) {
	// SQLite reads its own compile-time catalog tables as table-valued functions, with the
	// table name bound as an argument: `index_list` answers the indexes on that table and
	// `index_xinfo` answers the columns of one named index, in index order. The `key` flag
	// selects the parts that carry a column or an expression and drops the rowid pointer a
	// covering index also carries.
	query := `SELECT "i"."name", COALESCE("x"."name", '') AS "column_name"
		FROM "pragma_index_list"(?) AS "i"
		JOIN "pragma_index_xinfo"("i"."name") AS "x"
		WHERE "x"."key" = 1
		ORDER BY "i"."name", "x"."seqno"`

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		// One row per column of an index, so the rows are grouped in the order they arrive:
		// the engine already reports each index under its own name with its columns in
		// `SEQ_IN_INDEX` order. A prefix of a column is reported as an unnamed part.
		query = `SELECT "INDEX_NAME", CASE WHEN "SUB_PART" IS NULL THEN "COLUMN_NAME" ELSE '' END AS "column_name"
			FROM "information_schema"."STATISTICS"
			WHERE "TABLE_SCHEMA" = DATABASE() AND "TABLE_NAME" = ?
			ORDER BY "INDEX_NAME", "SEQ_IN_INDEX"`

	case DialectPostgresql:
		// `indkey` is the vector of column numbers the index leads with, in index order, and
		// `unnest` with `ORDINALITY` keeps that order while the join resolves each number to
		// the column's name; the lateral join is what lets that vector read the index row it
		// belongs to. A number that names no column is an expression part. The namespace is
		// the one the session's own search path resolves to, which is where the schema copy
		// created these tables.
		query = `SELECT "ci"."relname", COALESCE("a"."attname", '') AS "column_name"
			FROM "pg_index" AS "i"
			JOIN "pg_class" AS "ci" ON "ci"."oid" = "i"."indexrelid"
			JOIN "pg_class" AS "ct" ON "ct"."oid" = "i"."indrelid"
			JOIN "pg_namespace" AS "ns" ON "ns"."oid" = "ct"."relnamespace" AND "ns"."nspname" = current_schema()
			CROSS JOIN LATERAL "unnest"("i"."indkey") WITH ORDINALITY AS "o"("attnum", "ord")
			LEFT JOIN "pg_attribute" AS "a" ON "a"."attrelid" = "ct"."oid" AND "a"."attnum" = "o"."attnum"
			WHERE "ct"."relname" = ?
			ORDER BY "ci"."relname", "o"."ord"`
	}

	rows, err := s.query(ctx, query, table)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	indexes := []indexSpec{}

	for rows.Next() {
		var name, column string

		if err := rows.Scan(&name, &column); err != nil {
			return nil, err
		}

		if len(indexes) == 0 || indexes[len(indexes)-1].name != name {
			indexes = append(indexes, indexSpec{name: name, table: table})
		}

		indexes[len(indexes)-1].columns = append(indexes[len(indexes)-1].columns, column)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return indexes, nil
}

// callIndexCarrying is one reading of the catalog over the whole set: the indexes each table the
// set names carries, filed under that table's name, and the members no index on their own table
// carries over their whole column list. The probe and the startup step read the same one reading,
// so a start that creates keys and a test that reports what is lacking ask the database the same
// questions the same number of times.
type callIndexCarrying struct {
	carried map[string][]indexSpec
	missing []indexSpec

	// folded says whether this backend's catalog answers identifiers in a spelling that can
	// differ from the one they were written with, which is the case on MySQL and MariaDB: on those
	// two every comparison here ignores case, and on the other two it does not.
	folded bool
}

// nameHeld reports the index on a member's table that already carries the member's own name,
// together with the columns that index holds. A member reaches this question only when no key on
// its table carries its list, so a name found here belongs to a key of another shape: one an
// earlier release made, or one someone made by hand.
func (c callIndexCarrying) nameHeld(member indexSpec) (indexSpec, bool) {
	for _, index := range c.carried[member.table] {
		if c.folded {
			if strings.EqualFold(index.name, member.name) {
				return index, true
			}

			continue
		}

		if index.name == member.name {
			return index, true
		}
	}

	return indexSpec{}, false
}

// carryCallIndexes reads every table the set names once and answers which members their tables do
// not carry, in the order the set names them. Names play no part in that answer: the comparison is
// the ordered column list, so a key a foreign key clause made counts as serving a member while a
// key over a longer list, a key that shares only a prefix with one, and a key that carries the same
// columns in another order serve nothing.
func carryCallIndexes(ctx context.Context, s store) (callIndexCarrying, error) {
	// On MySQL and MariaDB an index's columns answer in the spelling the table declares them
	// with, and a schema copy can declare one column another way than the clause beside it does;
	// the shipped delayed table is that case. The other two backends compare names exactly,
	// because what they answer is what was written.
	carrying := callIndexCarrying{
		carried: map[string][]indexSpec{},
		folded:  s.dialect == DialectMariadb || s.dialect == DialectMysql,
	}

	for _, member := range callIndexSet {
		indexes, ok := carrying.carried[member.table]

		if !ok {
			var err error

			indexes, err = hasCallIndexShape(ctx, s, member.table)
			if err != nil {
				return callIndexCarrying{}, err
			}

			carrying.carried[member.table] = indexes
		}

		if !carriesColumnList(indexes, member.columns, carrying.folded) {
			carrying.missing = append(carrying.missing, member)
		}
	}

	return carrying, nil
}

// missingCallIndexes reports the members of the set no index on their table already carries over
// their whole column list, in the order the set names them.
func missingCallIndexes(ctx context.Context, s store) ([]indexSpec, error) {
	carrying, err := carryCallIndexes(ctx, s)
	if err != nil {
		return nil, err
	}

	return carrying.missing, nil
}

// carriesColumnList reports whether any of a table's indexes carries exactly this list:
// same length, same columns, same order.
func carriesColumnList(indexes []indexSpec, columns []string, folded bool) bool {
	for _, index := range indexes {
		if len(index.columns) != len(columns) {
			continue
		}

		same := true

		for i, column := range columns {
			if folded {
				if !strings.EqualFold(index.columns[i], column) {
					same = false
					break
				}

				continue
			}

			if index.columns[i] != column {
				same = false
				break
			}
		}

		if same {
			return true
		}
	}

	return false
}

// ensureCallIndexSet creates whatever members of the call index set a backend does not carry yet,
// and creates nothing else. A column list the catalog already carries under any name is left alone,
// because a key a foreign key clause made over the same columns serves the same reads and a second
// one over them costs writes and space for nothing. Nothing here drops, renames or recreates an
// index: a name the probe finds is left alone whatever its columns turn out to carry, so the pair
// index the server shipped with keeps the shape it shipped with. The statements run one at a time,
// outside any transaction, so a database another part of the server is already using is not held
// under a write lock for longer than one key build takes. There is nothing to undo afterwards, and
// no planner statistics are refreshed on the way through.
func ensureCallIndexSet(ctx context.Context, s store, progress Progress) error {
	const section, operation = "migration", "ensureCallIndexSet"

	carrying, err := carryCallIndexes(ctx, s)
	if err != nil {
		return operationError(section, operation, err)
	}

	// A start that finds the set already in place reads the catalog and stops there: no statement,
	// no log line, no failure.
	if len(carrying.missing) == 0 {
		return nil
	}

	present := map[string]bool{}

	for _, member := range carrying.missing {
		// A table that is not there ends that member's part of the step doing nothing: whether a
		// database has a table is decided by the schema statements, not by an index step.
		exists, ok := present[member.table]

		if !ok {
			exists, err = tableExists(ctx, s, member.table)
			if err != nil {
				return operationError(section, operation, err)
			}

			present[member.table] = exists
		}

		if !exists {
			continue
		}

		// The name this key would be created under belongs to a key of another shape. The database
		// is left exactly as it is and the operator is told: the member cannot be created without
		// taking that other key's name, and a second key beside it would hold columns the set does
		// not name.
		if held, taken := carrying.nameHeld(member); taken {
			progress(domain.LogLevelWarn, fmt.Sprintf(
				"the name %s already belongs to an index on %s over (%s): the call history key over (%s) is not created",
				member.name, member.table, strings.Join(held.columns, ", "), strings.Join(member.columns, ", "),
			))

			continue
		}

		statement := callIndexCreateStatement(s.dialect, member)

		if _, err := s.exec(ctx, statement); err != nil {
			return statementError(section, operation, err, statement)
		}

		progress(domain.LogLevelInfo, fmt.Sprintf("created the call history index %s on %s over (%s)",
			member.name, member.table, strings.Join(member.columns, ", ")))
	}

	return nil
}

// callIndexCreateStatement builds the one statement that gives a backend one member of the set. The
// identifiers carry the double quoting the schema copies use, which the MySQL and MariaDB data
// sources put in ANSI mode. `IF NOT EXISTS` is the spelling SQLite, MariaDB and PostgreSQL accept;
// MySQL has no such spelling, and there the key is asked for plainly the way the options key index
// is, because this step only runs a create the catalog probe has already said is needed.
func callIndexCreateStatement(dialect Dialect, member indexSpec) string {
	columns := make([]string, 0, len(member.columns))

	for _, column := range member.columns {
		columns = append(columns, `"`+column+`"`)
	}

	if dialect == DialectMysql {
		return fmt.Sprintf(`CREATE INDEX "%s" ON "%s" (%s)`, member.name, member.table, strings.Join(columns, ","))
	}

	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s" ON "%s" (%s)`, member.name, member.table, strings.Join(columns, ","))
}
