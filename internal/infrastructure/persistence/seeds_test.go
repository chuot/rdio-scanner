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
	"strings"
	"testing"
)

// A fresh deployment gets the groups and tags the domain lists, written in the order
// it lists them, each keeping the identity the database gave it.
func TestSeedWritesTheDefaultGroupsAndTagsInLegacyOrder(t *testing.T) {
	db := openEmptyDatabase(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	if err := Seed(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	got := rowsOf(t, db, "groups", `"groupId", "label"`)
	want := [][]string{
		{"1", "Air"},
		{"2", "EMS"},
		{"3", "Fire"},
		{"4", "Interop"},
		{"5", "Law"},
		{"6", "Unknown"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the seeded groups:\n  got  %v\n  want %v", got, want)
	}

	got = rowsOf(t, db, "tags", `"tagId", "label"`)
	want = [][]string{
		{"1", "Air Traffic Control"},
		{"2", "Emergency "},
		{"3", "Fire Dispatch"},
		{"4", "Fire Tac"},
		{"5", "Fire Talk"},
		{"6", "Interop"},
		{"7", "Security"},
		{"8", "Service"},
		{"9", "Untagged"},
	}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the seeded tags:\n  got  %v\n  want %v", got, want)
	}
}

// The legacy server seeded a tag whose label ends in a space, and a deployment
// swapping one server for the other keeps finding it under the same name.
func TestSeedKeepsTheTrailingSpaceInTheEmergencyTag(t *testing.T) {
	db := openEmptyDatabase(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	if err := Seed(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	for _, row := range rowsOf(t, db, "tags", `"label"`) {
		if strings.HasPrefix(row[0], "Emergency") {
			if row[0] != "Emergency " {
				t.Errorf("the seeded tag is %q, want %q with its trailing space", row[0], "Emergency ")
			}

			return
		}
	}

	t.Errorf("the seeded tags hold no Emergency tag: %v", rowsOf(t, db, "tags", `"label"`))
}

// A difference a deployment sees on screen: the migration numbers the groups it
// moves 1..n, the seed leaves its groups at the display order column's default
// of zero, so a fresh server's groups are ordered by identity.
func TestSeedGroupsLeaveTheirDisplayOrderAtZero(t *testing.T) {
	db := openEmptyDatabase(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	if err := Seed(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	for _, table := range []string{"groups", "tags"} {
		for _, row := range rowsOf(t, db, table, `"label", "order"`) {
			if row[1] != "0" {
				t.Errorf("the seeded %s %q was given display order %s, want 0", table, row[0], row[1])
			}
		}
	}
}

// A database that already has groups but no tags: each half decides on its own,
// and neither touches the other's rows.
func TestSeedLeavesATableThatAlreadyHasRowsAloneAndStillSeedsTheOther(t *testing.T) {
	db := openEmptyDatabase(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	mustExec(t, db, `INSERT INTO "groups" ("groupId", "label", "order") VALUES (7, "Talkgroups", 1)`)

	if err := Seed(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	got := rowsOf(t, db, "groups", `"groupId", "label", "order"`)
	want := [][]string{{"7", "Talkgroups", "1"}}

	if !equalStrings(flat(got), flat(want)) {
		t.Errorf("the groups of a deployment that already had one:\n  got  %v\n  want %v", got, want)
	}

	if count := countRows(t, db, "tags"); count != 9 {
		t.Errorf("the tags of a deployment that had none: %d, want the 9 defaults", count)
	}
}

// The second seed over the same database finds rows and writes none.
func TestSeedTwiceWritesTheDefaultsOnce(t *testing.T) {
	db := openEmptyDatabase(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if err := Seed(context.Background(), db, DialectSqlite); err != nil {
			t.Fatalf("Seed, attempt %d: %v", attempt, err)
		}
	}

	if got := countRows(t, db, "groups"); got != 6 {
		t.Errorf("seeding twice wrote %d groups, want 6", got)
	}

	if got := countRows(t, db, "tags"); got != 9 {
		t.Errorf("seeding twice wrote %d tags, want 9", got)
	}
}

// The seed runs after the migration for this: the migration has already moved the
// database's own groups and tags, so the defaults never reach a migrated database.
func TestSeedAfterMigratingAV6DatabaseWritesNothing(t *testing.T) {
	db, _ := migrateLegacyV6Fixture(t)

	if err := Seed(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	if got := countRows(t, db, "groups"); got != 6 {
		t.Errorf("the seeded deployment has %d groups, want the 6 it migrated", got)
	}

	if got := countRows(t, db, "tags"); got != 5 {
		t.Errorf("the seeded deployment has %d tags, want the 5 it migrated", got)
	}

	for _, row := range rowsOf(t, db, "groups", `"label", "order"`) {
		if row[1] == "0" {
			t.Errorf("the seed wrote its default group %q into a migrated deployment", row[0])
		}
	}

	for _, row := range rowsOf(t, db, "tags", `"label", "order"`) {
		if row[1] == "0" {
			t.Errorf("the seed wrote its default tag %q into a migrated deployment", row[0])
		}
	}
}

// Unlike a migration, which shrugs off a table it cannot query, a seed cannot decide
// without its count and returns that failure, wrapped.
func TestSeedReportsAFailureOfTheCountThatDecidesWhetherToSeed(t *testing.T) {
	db := openEmptyDatabase(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	mustExec(t, db, `DROP TABLE "groups"`)

	err := Seed(context.Background(), db, DialectSqlite)
	if err == nil {
		t.Fatalf("Seed: expected the failure the legacy seed returned, got none")
	}

	for _, want := range []string{
		"database.seed:",
		"seeds.seedgroups:",
		`in SELECT COUNT(*) FROM "groups"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Seed returned %q, expected it to contain %q", err, want)
		}
	}
}
