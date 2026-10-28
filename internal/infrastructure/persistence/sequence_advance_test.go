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
	"os"
	"regexp"
	"strings"
	"testing"
)

// The sequence advance carries a named list of tables and keys, and a table left out
// of the list fails the upgrade the quiet way: the migrated rows sit on identifiers
// the sequences never moved past, and the first row the server adds for itself on its
// own collides. The list cannot be reviewed by eye against the migration, so this
// check reads both sides of it from the code itself.

// TestTheSequenceAdvanceListCarriesEveryMigratedPrimaryKey reads which tables the v8
// PostgreSQL schema hands its own counter to, reads which of them the migration pass
// inserts identifiers into, and requires the advance list to carry exactly those.
func TestTheSequenceAdvanceListCarriesEveryMigratedPrimaryKey(t *testing.T) {
	// The schema copy declares a bigserial primary key for every table whose counter
	// belongs to the backend, so the schema itself names the tables that could ever
	// need an advance.
	table := regexp.MustCompile(`(?is)CREATE TABLE IF NOT EXISTS "([A-Za-z]+)"\s*\((.*?)\n\s*\);`)
	serialKey := regexp.MustCompile(`"([A-Za-z]+)" bigserial NOT NULL PRIMARY KEY`)

	keys := map[string]string{}

	for _, statement := range postgresSchema {
		declared := table.FindStringSubmatch(statement)
		if declared == nil {
			continue
		}

		if key := serialKey.FindStringSubmatch(declared[2]); key != nil {
			keys[declared[1]] = key[1]
		}
	}

	if len(keys) == 0 {
		t.Fatalf("the PostgreSQL schema copy declares no bigserial primary key, so this check proves nothing")
	}

	// Which of those tables the migration pass inserts identifiers into is what its
	// own insert statements say: a table whose insert names its primary key is one
	// whose counter the pass leaves behind on PostgreSQL. An insert that names only
	// foreign keys — the call detail tables under a call, the talkgroup-groups join —
	// leaves its own counter to the backend and needs nothing.
	source, err := os.ReadFile("migrations.go")
	if err != nil {
		t.Fatalf("reading the migration's own source: %v", err)
	}

	insert := regexp.MustCompile(`INSERT INTO "([A-Za-z]+)" \(([^)]*)\)`)

	explicit := map[string]bool{}

	for _, statement := range insert.FindAllStringSubmatch(string(source), -1) {
		tableName, columns := statement[1], statement[2]

		if key, ok := keys[tableName]; ok && strings.Contains(columns, `"`+key+`"`) {
			explicit[tableName] = true
		}
	}

	if len(explicit) == 0 {
		t.Fatalf("the migration inserts no explicit primary keys, so this check proves nothing")
	}

	carried := map[string]string{}

	for _, sequence := range migratedSequenceTables {
		carried[sequence.table] = sequence.key
	}

	for tableName := range explicit {
		key := keys[tableName]

		switch listKey, ok := carried[tableName]; {
		case !ok:
			t.Errorf("the migration inserts explicit %s values into %s, but the sequence advance list does not carry that table", key, tableName)
		case listKey != key:
			t.Errorf("the sequence advance list carries %s under the key %q, want the schema's %s", tableName, listKey, key)
		}
	}

	// The other direction, so a list entry cannot drift onto a table the schema
	// never gave a bigserial primary key: an advance of a sequence that is not there
	// is a statement the backend would refuse.
	for tableName, key := range carried {
		if keys[tableName] != key {
			t.Errorf("the sequence advance list carries %s under the key %q, but the PostgreSQL schema gives that table no such bigserial primary key", tableName, key)
		}
	}
}

// The advance belongs to PostgreSQL alone: SQLite and MariaDB move their counters
// past an identifier they are handed, and the setval statement only PostgreSQL
// understands. On the other dialects the step has to do nothing at all, and a step
// that forgot its dialect would fail every sqlite migration with a syntax error.
func TestAdvanceMigratedSequencesLeavesSqliteAlone(t *testing.T) {
	db := openLegacyDatabase(t)

	s := newStore(db, DialectSqlite)

	if err := advanceMigratedSequences(context.Background(), s); err != nil {
		t.Fatalf("advanceMigratedSequences on sqlite: %v", err)
	}
}
