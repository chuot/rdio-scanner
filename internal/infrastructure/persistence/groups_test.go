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

// Every column the legacy `Groups.Write` stored reads back into the field it belongs to, in
// the display order the legacy `Groups.Read` sorts into.
func TestAllGroupsReadsTheGroupsTheLegacyServerWrote(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewGroups(db, DialectSqlite)

	groups, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	want := domain.Groups{
		{ID: 1, Label: "Air"},
		{ID: 2, Label: "EMS"},
		{ID: 3, Alert: "alert1", Label: "Fire", LED: "red"},
		{ID: 4, Label: "Interop"},
		{ID: 5, Label: "Law"},
		{ID: 6, Label: "Unknown"},
	}

	if len(groups) != len(want) {
		t.Fatalf("AllGroups returned %d groups, want the %d the legacy server stored", len(groups), len(want))
	}

	for i := range want {
		if groups[i] != want[i] {
			t.Errorf("group %d reads back as %#v, want %#v", i, groups[i], want[i])
		}
	}
}

// The groups the server answers with are the ones it is running with: the collection is read
// from the database once.
func TestAllGroupsIsReadFromTheDatabaseOnce(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewGroups(db, DialectSqlite)

	first, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO "groups" ("alert", "label", "led", "order") VALUES ('', 'Written Behind The Servers Back', '', 0)`); err != nil {
		t.Fatalf("inserting a group behind the repository: %v", err)
	}

	second, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	if len(first) != 6 || len(second) != 6 {
		t.Fatalf("AllGroups returned %d groups then %d, want the loaded collection both times", len(first), len(second))
	}
}

// TestAllGroupsReportsTheLegacyFailureShapeWhenTheDatabaseCannotBeRead keeps the log lines the shape
// the legacy server's have.
func TestAllGroupsReportsTheLegacyFailureShapeWhenTheDatabaseCannotBeRead(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewGroups(db, DialectSqlite)

	db.Close()

	_, err := repo.AllGroups(context.Background())
	if err == nil {
		t.Fatal("a database that cannot be reached is a failure the caller is told about")
	}

	if !strings.HasPrefix(err.Error(), "groups.read: ") {
		t.Errorf("error = %v, want it to start with groups.read:", err)
	}
}

// The read-back after a successful write gives the identities the table assigned, which the
// next save and the configuration broadcast work from.
func TestSaveGroupsStoresANewGroupAndCachesTheStoredRow(t *testing.T) {
	db := openSchemaDatabase(t)

	repo := NewGroups(db, DialectSqlite)

	if err := repo.SaveGroups(context.Background(), domain.Groups{
		{Alert: "alert1", Label: "Fire", LED: "red", Order: 2},
		{Label: "Law", Order: 3},
	}); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	stored, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	want := domain.Groups{
		{ID: 1, Alert: "alert1", Label: "Fire", LED: "red", Order: 2},
		{ID: 2, Label: "Law", Order: 3},
	}

	if len(stored) != len(want) {
		t.Fatalf("the catalog has %d groups, want %d", len(stored), len(want))
	}

	for i := range want {
		if stored[i] != want[i] {
			t.Errorf("group %d reads back as %#v, want %#v", i, stored[i], want[i])
		}
	}
}

// A submitted group whose identifier is already stored replaces that row in place.
func TestSaveGroupsUpdatesAStoredGroupInPlace(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewGroups(db, DialectSqlite)

	groups, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	updated := append(domain.Groups{}, groups...)
	updated[2].Label = "Fire Ground"
	updated[2].Alert = ""
	updated[2].LED = "blue"
	updated[2].Order = 1

	if err := repo.SaveGroups(context.Background(), updated); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	if got := countRows(t, db, "groups"); got != 6 {
		t.Fatalf("the groups table has %d rows after the save, want the same 6", got)
	}

	stored, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	renamed, ok := stored.ByLabel("Fire Ground")
	if !ok {
		t.Fatalf("the stored groups are %#v, want the renamed one among them", stored)
	}

	if renamed != (domain.Group{ID: 3, Label: "Fire Ground", LED: "blue", Order: 1}) {
		t.Errorf("the renamed group reads back as %#v, want the submitted values", renamed)
	}
}

// A whole-collection save that drops a group drops it from the table too, and
// `talkgroupGroups` references groups `ON DELETE CASCADE`, so the memberships go with it.
func TestSaveGroupsRemovesAStoredGroupTheSubmittedListOmits(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewGroups(db, DialectSqlite)

	groups, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	kept := append(domain.Groups{}, groups[1:]...)

	if err := repo.SaveGroups(context.Background(), kept); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	if got := countRows(t, db, "groups"); got != 5 {
		t.Fatalf("the groups table has %d rows, want 5", got)
	}

	if got := countRows(t, db, "talkgroupGroups"); got != 2 {
		t.Errorf("talkgroupGroups has %d rows, want 2: the membership that named the dropped group cascaded with it", got)
	}

	if got := countRows(t, db, "talkgroups"); got != 4 {
		t.Errorf("the talkgroups table has %d rows, want the stored 4: a group is not what a talkgroup is keyed by", got)
	}
}

// A save that adds a new group still removes the stored group the submitted list dropped: the
// legacy `server/group.go` `Groups.Write` tested `group.Id == 0 || group.Id == groupId`, so one
// group with no identity yet kept every stored group, and this server repairs that rule.
func TestSaveGroupsRemovesAStoredGroupEvenWhenTheSubmittedListNamesANewOne(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewGroups(db, DialectSqlite)

	groups, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	// The administrator drops "Air" and adds "Ground".
	submitted := append(domain.Groups{}, groups[1:]...)
	submitted = append(submitted, domain.Group{Label: "Ground", Order: 7})

	if err := repo.SaveGroups(context.Background(), submitted); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	if got := countRows(t, db, "groups"); got != 6 {
		t.Fatalf("the groups table has %d rows, want 6: the six groups the list kept plus the one it added", got)
	}

	stored, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	if _, found := stored.ByLabel("Air"); found {
		t.Errorf("the stored groups are %#v, want the group the list dropped removed", stored)
	}
}

// The submitted section is what the server runs with even when it could not be stored.
func TestSaveGroupsLeavesTheSubmittedGroupsInEffectWhenTheStoreCannotBeWritten(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewGroups(db, DialectSqlite)

	if _, err := repo.AllGroups(context.Background()); err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	// A store the save cannot write to at all.
	if _, err := db.Exec(`DROP TABLE "groups"`); err != nil {
		t.Fatalf("dropping the groups table: %v", err)
	}

	submitted := domain.Groups{{Label: "Ground", Order: 1}}

	if err := repo.SaveGroups(context.Background(), submitted); err == nil {
		t.Fatal("a section that cannot be stored is a failure the caller is told about")
	}

	live, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	if len(live) != 1 || live[0].Label != "Ground" {
		t.Errorf("the server is running with %#v, want the submitted section: a section that could not be stored is still in effect", live)
	}
}

// Every statement is parameterized, so a quote in a label is stored as text: the legacy
// `Groups.Write` reached for that by doubling the quote inside an interpolated statement.
func TestSaveGroupsStoresAQuoteInALabelAsText(t *testing.T) {
	db := openSchemaDatabase(t)

	repo := NewGroups(db, DialectSqlite)

	label := `Law'); DELETE FROM "groups"; --`

	if err := repo.SaveGroups(context.Background(), domain.Groups{{Label: label}}); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}

	stored, err := repo.AllGroups(context.Background())
	if err != nil {
		t.Fatalf("AllGroups: %v", err)
	}

	if len(stored) != 1 || stored[0].Label != label {
		t.Errorf("the saved group reads back as %#v, want the label stored as the text it is", stored)
	}

	if got := countRows(t, db, "groups"); got != 1 {
		t.Errorf("the groups table has %d rows, want 1: the value was never part of the statement", got)
	}
}
