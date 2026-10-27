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

	"rdio-scanner/internal/domain"
)

// The trailing space of a stored label is part of the label and reads back as stored.
func TestAllTagsReadsTheTagsTheLegacyServerWrote(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewTags(db, DialectSqlite)

	tags, err := repo.AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	want := domain.Tags{
		{ID: 1, Label: "Air Traffic Control"},
		{ID: 2, Label: "Emergency "},
		{ID: 3, Label: "Fire Dispatch"},
		{ID: 4, Label: "Fire Tac"},
		{ID: 5, Label: "Fire Talk"},
		{ID: 6, Label: "Interop"},
		{ID: 7, Label: "Security"},
		{ID: 8, Label: "Talkgroup 12345"},
	}

	if len(tags) != len(want) {
		t.Fatalf("AllTags returned %d tags, want the %d the legacy server stored", len(tags), len(want))
	}

	for i := range want {
		if tags[i] != want[i] {
			t.Errorf("tag %d reads back as %#v, want %#v", i, tags[i], want[i])
		}
	}
}

// Tags are read from the database once.
func TestAllTagsIsReadFromTheDatabaseOnce(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewTags(db, DialectSqlite)

	first, err := repo.AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO "tags" ("alert", "label", "led", "order") VALUES ('', 'Written Behind The Servers Back', '', 0)`); err != nil {
		t.Fatalf("inserting a tag behind the repository: %v", err)
	}

	second, err := repo.AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	if len(first) != 8 || len(second) != 8 {
		t.Fatalf("AllTags returned %d tags then %d, want the loaded collection both times", len(first), len(second))
	}
}

// A failed tags read keeps the legacy failure shape.
func TestAllTagsReportsTheLegacyFailureShapeWhenTheDatabaseCannotBeRead(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewTags(db, DialectSqlite)

	db.Close()

	_, err := repo.AllTags(context.Background())
	if err == nil {
		t.Fatal("a database that cannot be reached is a failure the caller is told about")
	}

	if !strings.HasPrefix(err.Error(), "tags.read: ") {
		t.Errorf("error = %v, want it to start with tags.read:", err)
	}
}

// The order column is signed and the list reads it into an unsigned field, so a
// stored negative order reads as zero like the accesses list already clamps, and
// never fails the whole tag read: one row with a negative order must not leave
// the server without its tags.
func TestStoredNegativeTagOrderReadsAsZero(t *testing.T) {
	db := openLegacyDatabase(t)

	const query = `INSERT INTO "tags" ("label", "order") VALUES ('legacy-row', -7)`

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	list, err := NewTags(db, DialectSqlite).AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	var legacy domain.Tag

	for _, tag := range list {
		if tag.Label == "legacy-row" {
			legacy = tag
		}
	}

	if legacy.Label != "legacy-row" {
		t.Fatal("the stored legacy-row tag did not read back at all")
	}

	if legacy.Order != 0 {
		t.Errorf("the stored -7 read as order %d, want 0", legacy.Order)
	}
}

// A successful save re-reads and caches the stored row.
func TestSaveTagsStoresANewTagAndCachesTheStoredRow(t *testing.T) {
	db := openSchemaDatabase(t)

	repo := NewTags(db, DialectSqlite)

	if err := repo.SaveTags(context.Background(), domain.Tags{
		{Alert: "alert1", Label: "Fire Dispatch", LED: "red", Order: 2},
		{Label: "Interop", Order: 1},
	}); err != nil {
		t.Fatalf("SaveTags: %v", err)
	}

	stored, err := repo.AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	// The stored collection comes back sorted by display order.
	want := domain.Tags{
		{ID: 2, Label: "Interop", Order: 1},
		{ID: 1, Alert: "alert1", Label: "Fire Dispatch", LED: "red", Order: 2},
	}

	if len(stored) != len(want) {
		t.Fatalf("the catalog has %d tags, want %d", len(stored), len(want))
	}

	for i := range want {
		if stored[i] != want[i] {
			t.Errorf("tag %d reads back as %#v, want %#v", i, stored[i], want[i])
		}
	}
}

// A save updates a stored tag in place.
func TestSaveTagsUpdatesAStoredTagInPlace(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewTags(db, DialectSqlite)

	tags, err := repo.AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	updated := append(domain.Tags{}, tags...)
	updated[2].Label = "Fire Ground"
	updated[2].LED = "blue"

	if err := repo.SaveTags(context.Background(), updated); err != nil {
		t.Fatalf("SaveTags: %v", err)
	}

	if got := countRows(t, db, "tags"); got != 8 {
		t.Fatalf("the tags table has %d rows after the save, want the same 8", got)
	}

	stored, err := repo.AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	renamed, ok := stored.ByLabel("Fire Ground")
	if !ok {
		t.Fatalf("the stored tags are %#v, want the renamed one among them", stored)
	}

	if renamed != (domain.Tag{ID: 3, Label: "Fire Ground", LED: "blue"}) {
		t.Errorf("the renamed tag reads back as %#v, want the submitted values", renamed)
	}
}

// Dropping a tag cascades to the talkgroups that name it and their memberships.
func TestSaveTagsRemovesAStoredTagAndCascadesTheTalkgroupsThatNameIt(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewTags(db, DialectSqlite)

	tags, err := repo.AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	kept := append(domain.Tags{}, tags[:2]...)
	kept = append(kept, tags[3:]...)

	if err := repo.SaveTags(context.Background(), kept); err != nil {
		t.Fatalf("SaveTags: %v", err)
	}

	if got := countRows(t, db, "tags"); got != 7 {
		t.Fatalf("the tags table has %d rows, want 7", got)
	}

	if got := countRows(t, db, "talkgroups"); got != 3 {
		t.Errorf("the talkgroups table has %d rows, want 3: the talkgroup that named the dropped tag cascaded with it", got)
	}

	if got := countRows(t, db, "talkgroupGroups"); got != 1 {
		t.Errorf("talkgroupGroups has %d rows, want 1: the dropped talkgroup's memberships cascaded with it", got)
	}
}

// Repaired reconcile: the legacy write kept every stored tag when a submitted tag had no
// identity, testing `tag.Id == 0 || tag.Id == tagId`; this server deletes what the list removed.
func TestSaveTagsRemovesAStoredTagEvenWhenTheSubmittedListNamesANewOne(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewTags(db, DialectSqlite)

	tags, err := repo.AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	// The administrator drops "Air Traffic Control" and adds "Ramp".
	submitted := append(domain.Tags{}, tags[1:]...)
	submitted = append(submitted, domain.Tag{Label: "Ramp"})

	if err := repo.SaveTags(context.Background(), submitted); err != nil {
		t.Fatalf("SaveTags: %v", err)
	}

	if got := countRows(t, db, "tags"); got != 8 {
		t.Fatalf("the tags table has %d rows, want 8: the seven tags the list kept plus the one it added", got)
	}

	stored, err := repo.AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	if _, found := stored.ByLabel("Air Traffic Control"); found {
		t.Errorf("the stored tags are %#v, want the tag the list dropped removed", stored)
	}
}

// A save that cannot write still leaves the submitted tags in effect.
func TestSaveTagsLeavesTheSubmittedTagsInEffectWhenTheStoreCannotBeWritten(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewTags(db, DialectSqlite)

	if _, err := repo.AllTags(context.Background()); err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	// A store the save cannot write to at all.
	if _, err := db.Exec(`DROP TABLE "tags"`); err != nil {
		t.Fatalf("dropping the tags table: %v", err)
	}

	submitted := domain.Tags{{Label: "Ramp"}}

	if err := repo.SaveTags(context.Background(), submitted); err == nil {
		t.Fatal("a section that cannot be stored is a failure the caller is told about")
	}

	live, err := repo.AllTags(context.Background())
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}

	if len(live) != 1 || live[0].Label != "Ramp" {
		t.Errorf("the server is running with %#v, want the submitted section: a section that could not be stored is still in effect", live)
	}
}

// A quote in a label stores as text: every statement is parameterized, where the legacy
// string-built SQL went wrong.
func TestSaveTagsStoresAQuoteInALabelAsText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		label string
	}{
		{name: "an apostrophe", label: "Fire Dispatch's"},
		{name: "text shaped like the end of a statement", label: `Ramp'); DELETE FROM "tags"; --`},
	} {
		db := openSchemaDatabase(t)

		repo := NewTags(db, DialectSqlite)

		if err := repo.SaveTags(context.Background(), domain.Tags{{Label: tc.label}}); err != nil {
			t.Fatalf("%s: SaveTags: %v", tc.name, err)
		}

		stored, err := repo.AllTags(context.Background())
		if err != nil {
			t.Fatalf("%s: AllTags: %v", tc.name, err)
		}

		if len(stored) != 1 || stored[0].Label != tc.label {
			t.Errorf("%s: the saved tag reads back as %#v, want the label stored as the text it is", tc.name, stored)
		}

		if got := countRows(t, db, "tags"); got != 1 {
			t.Errorf("%s: the tags table has %d rows, want 1: the value was never part of the statement", tc.name, got)
		}
	}
}
