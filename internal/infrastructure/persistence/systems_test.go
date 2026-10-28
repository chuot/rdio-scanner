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
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"rdio-scanner/internal/application/ports"

	"rdio-scanner/internal/domain"
)

// Every column of the systems table the legacy `Systems.Write` stored reads back into the field
// it belongs to.
func TestAllSystemsReadsTheSystemsTheLegacyServerWrote(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if len(systems) != 2 {
		t.Fatalf("AllSystems returned %d systems, want the 2 the legacy server stored", len(systems))
	}

	for _, tc := range []struct {
		name string
		want domain.System
		got  domain.System
	}{
		{
			name: "the auto-populating system",
			want: domain.System{
				ID: 1, Alert: "", AutoPopulate: true, Blacklists: "40000,40001", Delay: 0,
				Kind: "talkgroup", Label: "Grand Metro", LED: "", Order: 1, SystemRef: 10001,
			},
			got: systems[0],
		},
		{
			name: "the plain system",
			want: domain.System{
				ID: 2, Alert: "", AutoPopulate: false, Blacklists: "", Delay: 0,
				Kind: "talkgroup", Label: "Harbour", LED: "", Order: 2, SystemRef: 10002,
			},
			got: systems[1],
		},
	} {
		got := tc.got

		if got.ID != tc.want.ID || got.Alert != tc.want.Alert || got.AutoPopulate != tc.want.AutoPopulate ||
			got.Blacklists != tc.want.Blacklists || got.Delay != tc.want.Delay || got.Kind != tc.want.Kind ||
			got.Label != tc.want.Label || got.LED != tc.want.LED || got.Order != tc.want.Order ||
			got.SystemRef != tc.want.SystemRef {
			t.Errorf("%s reads back as %#v, want %#v", tc.name, got, tc.want)
		}
	}

	// The legacy Read sorts the catalog by display order; the fixture stores it in that order
	// already, so the sort is what keeps it there when an administrator reorders the list.
	if systems[0].Order > systems[1].Order {
		t.Errorf("the catalog comes back out of display order: %d before %d", systems[0].Order, systems[1].Order)
	}
}

// The boolean columns hold the integers a Go %t written into an integer(1) column produced, and
// the read accepts that shape.
func TestAllSystemsReadsTheBooleanTheLegacyServerStored(t *testing.T) {
	db := openLegacyDatabase(t)

	for _, tc := range []struct {
		systemID   int64
		wantStored int64
		wantLive   bool
	}{
		{systemID: 1, wantStored: 1, wantLive: true},
		{systemID: 2, wantStored: 0, wantLive: false},
	} {
		var (
			storedType string
			stored     int64
		)

		query := `SELECT typeof("autoPopulate"), "autoPopulate" FROM "systems" WHERE "systemId" = ?`

		if err := db.QueryRow(query, tc.systemID).Scan(&storedType, &stored); err != nil {
			t.Fatalf("reading system %d: %v", tc.systemID, err)
		}

		if storedType != "integer" || stored != tc.wantStored {
			t.Errorf("system %d stores autoPopulate as %s %v, want the integer %v the legacy server wrote",
				tc.systemID, storedType, stored, tc.wantStored)
		}
	}

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if !systems[0].AutoPopulate {
		t.Errorf("system 1 reads back with autoPopulate %v, want true", systems[0].AutoPopulate)
	}

	if systems[1].AutoPopulate {
		t.Errorf("system 2 reads back with autoPopulate %v, want false", systems[1].AutoPopulate)
	}
}

// `order` and `talkgroupRef` are read from the columns they are stored in, never from each other:
// the legacy server marshals the display order out under the JSON key "talkgroup".
func TestAllSystemsReadsTalkgroupOrderAndTalkgroupRefFromTheirOwnColumns(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	talkgroups := systems[0].Talkgroups

	if len(talkgroups) != 3 {
		t.Fatalf("system 1 has %d talkgroups, want the 3 the legacy server stored", len(talkgroups))
	}

	for _, tc := range []struct {
		talkgroupID   uint64
		wantOrder     uint
		wantRef       uint
		wantLabel     string
		wantName      string
		wantKind      string
		wantTagID     uint64
		wantFrequency uint
		wantDelay     uint
		wantAlert     string
		wantLED       string
	}{
		{talkgroupID: 1, wantOrder: 1, wantRef: 1514000, wantLabel: "Dispatch 1", wantName: "Dispatch 1", wantKind: "audio", wantTagID: 3, wantFrequency: 1514000000},
		{talkgroupID: 2, wantOrder: 2, wantRef: 1514001, wantLabel: "Interop 9", wantName: "Interop 9", wantKind: "audio", wantTagID: 6},
		{talkgroupID: 3, wantOrder: 3, wantRef: 1514002, wantLabel: "Delayed Talk", wantName: "Delayed Talk", wantKind: "audio", wantTagID: 6, wantDelay: 90},
	} {
		talkgroup, ok := talkgroups.ByID(tc.talkgroupID)
		if !ok {
			t.Fatalf("talkgroup %d was not loaded", tc.talkgroupID)
		}

		if talkgroup.Order != tc.wantOrder {
			t.Errorf("talkgroup %d has order %d, want %d: the order column is not the talkgroupRef column",
				tc.talkgroupID, talkgroup.Order, tc.wantOrder)
		}

		if talkgroup.TalkgroupRef != tc.wantRef {
			t.Errorf("talkgroup %d has talkgroupRef %d, want %d: the talkgroupRef column is not the order column",
				tc.talkgroupID, talkgroup.TalkgroupRef, tc.wantRef)
		}

		if talkgroup.Label != tc.wantLabel || talkgroup.Name != tc.wantName || talkgroup.Kind != tc.wantKind ||
			talkgroup.TagID != tc.wantTagID || talkgroup.Frequency != tc.wantFrequency ||
			talkgroup.Delay != tc.wantDelay || talkgroup.Alert != tc.wantAlert || talkgroup.LED != tc.wantLED {
			t.Errorf("talkgroup %d reads back as %#v, want label %q name %q type %q tagId %d frequency %d delay %d",
				tc.talkgroupID, talkgroup, tc.wantLabel, tc.wantName, tc.wantKind, tc.wantTagID, tc.wantFrequency, tc.wantDelay)
		}
	}

	if talkgroups[0].Order > talkgroups[1].Order || talkgroups[1].Order > talkgroups[2].Order {
		t.Errorf("the talkgroups come back out of display order: %v", talkgroups)
	}
}

// A talkgroup's groups come from its `talkgroupGroups` rows, and a talkgroup with none gets an
// empty list rather than the zero the left join aggregates.
func TestAllSystemsReadsATalkgroupsGroupsFromTheJoinTable(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	talkgroups := systems[0].Talkgroups

	for _, tc := range []struct {
		talkgroupID uint64
		want        []uint64
	}{
		{talkgroupID: 1, want: []uint64{1, 5}},
		{talkgroupID: 2, want: []uint64{4}},
		{talkgroupID: 3, want: []uint64{}},
	} {
		talkgroup, ok := talkgroups.ByID(tc.talkgroupID)
		if !ok {
			t.Fatalf("talkgroup %d was not loaded", tc.talkgroupID)
		}

		if talkgroup.GroupIDs == nil {
			t.Errorf("talkgroup %d has a nil group list, want an empty one", tc.talkgroupID)

			continue
		}

		if strings.Join(idsAsText(talkgroup.GroupIDs), ",") != strings.Join(idsAsText(tc.want), ",") {
			t.Errorf("talkgroup %d has groups %v, want %v", tc.talkgroupID, talkgroup.GroupIDs, tc.want)
		}
	}
}

// TestAllSystemsReadsTheStoredBlacklists pins the comma-separated text a system's blacklists column
// carries, including the empty one.
func TestAllSystemsReadsTheStoredBlacklists(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if got := systems[0].Blacklists; got != "40000,40001" {
		t.Errorf("system 1 has blacklists %q, want the stored text", got)
	}

	if got := systems[1].Blacklists; got != "" {
		t.Errorf("system 2 has blacklists %q, want nothing", got)
	}

	if !systems[0].AcceptsTalkgroup(1514000) {
		t.Errorf("system 1 refuses a talkgroup its blacklist does not name")
	}

	if systems[0].AcceptsTalkgroup(40000) {
		t.Errorf("system 1 accepts a talkgroup its blacklist names")
	}
}

// TestAllSystemsReadsEachSystemsSitesAndUnits loads the parts the aggregate owns.
func TestAllSystemsReadsEachSystemsSitesAndUnits(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	sites := systems[0].Sites

	if len(sites) != 1 {
		t.Fatalf("system 1 has %d sites, want the 1 the legacy server stored", len(sites))
	}

	if sites[0] != (domain.Site{ID: 1, Label: "North Site", Order: 1, SiteRef: 77, SystemID: 1}) {
		t.Errorf("system 1 has site %#v, want the stored one", sites[0])
	}

	if len(systems[1].Sites) != 0 {
		t.Errorf("system 2 has %d sites, want none", len(systems[1].Sites))
	}

	units := systems[0].Units

	if len(units) != 1 {
		t.Fatalf("system 1 has %d units, want the 1 the legacy server stored", len(units))
	}

	if units[0] != (domain.Unit{ID: 1, Label: "Engine 12", Order: 1, SystemID: 1, UnitRef: 12345}) {
		t.Errorf("system 1 has unit %#v, want the stored one", units[0])
	}

	if len(systems[1].Units) != 0 {
		t.Errorf("system 2 has %d units, want none", len(systems[1].Units))
	}
}

// The systems the server answers with are the ones it is running with: the catalog is read from
// the database once.
func TestAllSystemsIsReadFromTheDatabaseOnce(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	first, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO "systems" ("alert", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES ('', 0, '', 0, 'Written Behind The Servers Back', '', 3, 10003, 'talkgroup')`); err != nil {
		t.Fatalf("inserting a system behind the repository: %v", err)
	}

	second, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("AllSystems returned %d systems then %d, want the loaded catalog both times", len(first), len(second))
	}
}

// A database that cannot be read fails with the log lines and shape the legacy server's had.
func TestAllSystemsReportsTheLegacyFailureShapeWhenTheDatabaseCannotBeRead(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	db.Close()

	_, err := repo.AllSystems(context.Background())
	if err == nil {
		t.Fatal("a database that cannot be reached is a failure the caller is told about")
	}

	if !strings.HasPrefix(err.Error(), "systems.read: ") {
		t.Errorf("error = %v, want it to start with systems.read:", err)
	}
}

// The re-read after a write that persisted gives the identities the tables assigned, which the
// next read, the next save, and the configuration broadcast work from.
func TestSaveSystemsStoresANewSystemAndCachesTheStoredRows(t *testing.T) {
	db := openSchemaDatabase(t)

	if _, err := db.Exec(`INSERT INTO "tags" ("tagId", "alert", "label", "led", "order") VALUES (1, '', 'Fire Dispatch', '', 0)`); err != nil {
		t.Fatalf("seeding a tag: %v", err)
	}

	repo := NewSystems(db, DialectSqlite)

	submitted := domain.Systems{{
		AutoPopulate: true,
		Blacklists:   "40000,40001",
		Delay:        30,
		Kind:         "talkgroup",
		Label:        "Grand Metro",
		LED:          "red",
		Order:        1,
		SystemRef:    10001,
		Sites:        domain.Sites{{Label: "North Site", Order: 1, SiteRef: 77}},
		Talkgroups:   domain.Talkgroups{{Label: "Dispatch 1", Name: "Dispatch 1", Order: 1, TagID: 1, TalkgroupRef: 1514000, Kind: "audio"}},
		Units:        domain.Units{{Label: "Engine 12", Order: 1, UnitRef: 12345, UnitFrom: 100, UnitTo: 200}},
	}}

	if err := repo.SaveSystems(context.Background(), submitted); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	stored, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if len(stored) != 1 {
		t.Fatalf("the catalog has %d systems, want 1", len(stored))
	}

	system := stored[0]

	if system.ID == 0 {
		t.Fatal("the saved system has no identifier: the stored tables assign one and the re-read has to carry it")
	}

	if len(system.Talkgroups) != 1 || system.Talkgroups[0].ID == 0 {
		t.Fatalf("the saved system has talkgroups %#v, want one with an assigned identifier", system.Talkgroups)
	}

	if len(system.Sites) != 1 || system.Sites[0].ID == 0 {
		t.Fatalf("the saved system has sites %#v, want one with an assigned identifier", system.Sites)
	}

	if len(system.Units) != 1 || system.Units[0].ID == 0 {
		t.Fatalf("the saved system has units %#v, want one with an assigned identifier", system.Units)
	}

	// The submitted value is not what the server keeps running with after a save that persisted.
	if submitted[0].ID != 0 {
		t.Fatalf("the test assumed the submitted system had no identifier")
	}
}

// Every column the legacy `Systems.Write` writes round-trips through the save and the read.
func TestSaveSystemsRoundTripsEverySystemColumn(t *testing.T) {
	db := openSchemaDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	want := domain.System{
		Alert:        "alert1",
		AutoPopulate: true,
		Blacklists:   "40000,40001",
		Delay:        90,
		Kind:         "talkgroup",
		Label:        "Grand Metro",
		LED:          "red",
		Order:        4,
		SystemRef:    10009,
	}

	if err := repo.SaveSystems(context.Background(), domain.Systems{want}); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	stored, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if len(stored) != 1 {
		t.Fatalf("the catalog has %d systems, want 1", len(stored))
	}

	got := stored[0]

	if got.Alert != want.Alert || got.AutoPopulate != want.AutoPopulate || got.Blacklists != want.Blacklists ||
		got.Delay != want.Delay || got.Kind != want.Kind || got.Label != want.Label || got.LED != want.LED ||
		got.Order != want.Order || got.SystemRef != want.SystemRef {
		t.Errorf("the saved system reads back as %#v, want %#v", got, want)
	}
}

// What this adapter writes for a boolean is the integer the legacy server's own write produced, so
// the legacy server can read the row back.
func TestSaveSystemsWritesABooleanTheLegacyServerStoresTheSameWay(t *testing.T) {
	db := openSchemaDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	err := repo.SaveSystems(context.Background(), domain.Systems{
		{Label: "On", Order: 1, SystemRef: 1, AutoPopulate: true},
		{Label: "Off", Order: 2, SystemRef: 2, AutoPopulate: false},
	})
	if err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	for _, tc := range []struct {
		label string
		want  int64
	}{
		{label: "On", want: 1},
		{label: "Off", want: 0},
	} {
		var (
			storedType string
			stored     int64
		)

		if err := db.QueryRow(`SELECT typeof("autoPopulate"), "autoPopulate" FROM "systems" WHERE "label" = ?`, tc.label).Scan(&storedType, &stored); err != nil {
			t.Fatalf("reading %s: %v", tc.label, err)
		}

		if storedType != "integer" || stored != tc.want {
			t.Errorf("%s stores autoPopulate as %s %v, want the integer %v the legacy server writes",
				tc.label, storedType, stored, tc.want)
		}
	}

	if count := countRows(t, db, "systems"); count != 2 {
		t.Fatalf("the systems table has %d rows, want 2", count)
	}
}

// A stored system keeps its identifier through a save: the row is updated in place, not inserted
// a second time.
func TestSaveSystemsUpdatesAStoredSystemInPlace(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	updated := append(domain.Systems{}, systems...)

	updated[0].Label = "Grand Metro Renamed"
	updated[0].Delay = 45
	updated[0].AutoPopulate = false
	updated[0].Blacklists = ""
	updated[0].LED = "green"

	if err := repo.SaveSystems(context.Background(), updated); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	if count := countRows(t, db, "systems"); count != 2 {
		t.Fatalf("the systems table has %d rows after the save, want the same 2: the save updates the stored row", count)
	}

	stored, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if stored[0].ID != 1 || stored[0].Label != "Grand Metro Renamed" || stored[0].Delay != 45 ||
		stored[0].AutoPopulate || stored[0].Blacklists != "" || stored[0].LED != "green" {
		t.Errorf("system 1 reads back as %#v, want the submitted values", stored[0])
	}
}

// A whole-catalog save that drops a system drops it from the table, and the cascade takes its
// talkgroups, sites, units, and recorded calls with it.
func TestSaveSystemsRemovesAStoredSystemTheSubmittedListOmits(t *testing.T) {
	db := openLegacyDatabase(t)

	if _, err := db.Exec(`INSERT INTO "calls" ("audio", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES (?, ?, ?, ?, ?, ?, ?)`,
		[]byte("audio"), "audio.wav", "", 0, 2, 4, int64(1750000000000)); err != nil {
		t.Fatalf("seeding a call on system 2: %v", err)
	}

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if err := repo.SaveSystems(context.Background(), systems[:1]); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	for _, tc := range []struct {
		table string
		want  int
	}{
		{table: "systems", want: 1},
		{table: "talkgroups", want: 3},
		{table: "talkgroupGroups", want: 3},
		{table: "sites", want: 1},
		{table: "units", want: 1},
		{table: "calls", want: 2},
	} {
		if got := countRows(t, db, tc.table); got != tc.want {
			t.Errorf("%s has %d rows after the save, want %d", tc.table, got, tc.want)
		}
	}
}

// A save that adds a new system still removes the stored system the submitted list dropped: the
// legacy `Systems.Write` tested `system.Id == 0 || system.Id == systemId` for every stored
// identifier, so a save that adds a system silently kept every system the list removed, and this
// server repairs that rule for every configuration section.
func TestSaveSystemsRemovesAStoredSystemEvenWhenTheSubmittedListNamesANewOne(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	// The administrator keeps system 1, drops system 2, and adds a third.
	submitted := append(domain.Systems{}, systems[:1]...)
	submitted = append(submitted, domain.System{Label: "Airport", Order: 3, SystemRef: 10003})

	if err := repo.SaveSystems(context.Background(), submitted); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	if got := countRows(t, db, "systems"); got != 2 {
		t.Fatalf("the systems table has %d rows, want 2: the system the list dropped must go even though the list also names a new one", got)
	}

	// The dropped system's talkgroups go with it, which is the cascade the repair makes reachable.
	if got := countRows(t, db, "talkgroups"); got != 3 {
		t.Errorf("the talkgroups table has %d rows, want 3: system 2 and its talkgroups are removed together", got)
	}

	stored, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if _, found := stored.ByLabel("Harbour"); found {
		t.Errorf("the stored systems are %#v, want the dropped system removed", stored)
	}
}

// A save that removes a system removes the calls recorded against it: the calls table references
// the systems table ON DELETE CASCADE and foreign_keys is on, the risk accepted with the repaired
// keep-rule, which the legacy server's broken scan hid.
func TestSaveSystemsRemovalTakesTheRecordedCallsWithIt(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if got := countRows(t, db, "calls"); got != 2 {
		t.Fatalf("the fixture records %d calls, want 2 before the save", got)
	}

	// The submitted list keeps "Harbour" (system 2) and drops "Grand Metro" (system 1), which is the
	// system the fixture's calls were recorded against.
	submitted := domain.Systems{}

	for _, system := range systems {
		if system.Label == "Harbour" {
			submitted = append(submitted, system)
		}
	}

	if err := repo.SaveSystems(context.Background(), submitted); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	if got := countRows(t, db, "calls"); got != 0 {
		t.Errorf("the calls table has %d rows, want 0: removing a system removes the calls recorded against it", got)
	}
}

// One transaction and the talkgroups foreign key make a rejected talkgroup take the systems
// written before it down with it.
func TestSaveSystemsDiscardsTheWholeSystemsSectionWhenATalkgroupIsRejected(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	submitted := append(domain.Systems{}, systems...)
	submitted = append(submitted, domain.System{
		Label:      "Airport",
		Order:      3,
		SystemRef:  10003,
		Talkgroups: domain.Talkgroups{{Label: "Ramp", Name: "Ramp", Order: 1, TagID: 999, TalkgroupRef: 1530000}},
	})

	err = repo.SaveSystems(context.Background(), submitted)
	if err == nil {
		t.Fatal("a talkgroup the tags table does not name is a failure the caller is told about")
	}

	if !strings.Contains(err.Error(), "talkgroups.writetx") {
		t.Errorf("error = %v, want it to name talkgroups.writetx as the legacy server's does", err)
	}

	for _, tc := range []struct {
		table string
		want  int
	}{
		{table: "systems", want: 2},
		{table: "talkgroups", want: 4},
		{table: "sites", want: 1},
		{table: "units", want: 1},
	} {
		if got := countRows(t, db, tc.table); got != tc.want {
			t.Errorf("%s has %d rows after the rejected save, want the stored %d: the whole systems section was discarded", tc.table, got, tc.want)
		}
	}
}

// A save that could not be stored still leaves the submitted configuration in effect, the quirk
// kept from the legacy server.
func TestSaveSystemsLeavesTheSubmittedSystemsInEffectWhenTheSaveFails(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	if _, err := repo.AllSystems(context.Background()); err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	submitted := domain.Systems{{
		Label:      "Airport",
		Order:      3,
		SystemRef:  10003,
		Talkgroups: domain.Talkgroups{{Label: "Ramp", Name: "Ramp", Order: 1, TagID: 999, TalkgroupRef: 1530000}},
	}}

	if err := repo.SaveSystems(context.Background(), submitted); err == nil {
		t.Fatal("the save was expected to fail")
	}

	live, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if len(live) != 1 || live[0].Label != "Airport" {
		t.Fatalf("the server is running with %#v, want the submitted system: a section that could not be stored is still in effect", live)
	}

	if got := countRows(t, db, "systems"); got != 2 {
		t.Errorf("the systems table has %d rows, want the stored 2", got)
	}
}

// The aggregate's identity, not whatever the submitted part happens to carry, is what the child
// rows are keyed by.
func TestSaveSystemsWritesSitesAndUnitsUnderTheSystemBeingSaved(t *testing.T) {
	db := openSchemaDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	submitted := domain.Systems{{
		ID:        0,
		Label:     "Alpha",
		Order:     1,
		SystemRef: 10001,
		Sites:     domain.Sites{{Label: "North Site", Order: 1, SiteRef: 77, SystemID: 4242}},
		Units:     domain.Units{{Label: "Engine 12", Order: 1, UnitRef: 12345, SystemID: 4242}},
	}}

	if err := repo.SaveSystems(context.Background(), submitted); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	for _, tc := range []struct {
		table string
		want  int64
	}{
		{table: "sites", want: 1},
		{table: "units", want: 1},
	} {
		var systemID int64

		if err := db.QueryRow(`SELECT "systemId" FROM "` + tc.table + `"`).Scan(&systemID); err != nil {
			t.Fatalf("reading %s: %v", tc.table, err)
		}

		if systemID != tc.want {
			t.Errorf("%s stores systemId %d, want %d: the system being saved, not the one the submitted part names", tc.table, systemID, tc.want)
		}
	}
}

// Every site and unit column the legacy `Sites.WriteTx` and `Units.WriteTx` write round-trips
// through the save and the read.
func TestSaveSystemsRoundTripsSiteAndUnitColumns(t *testing.T) {
	db := openSchemaDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	submitted := domain.Systems{{
		Label:     "Alpha",
		Order:     1,
		SystemRef: 10001,
		Sites: domain.Sites{
			{Label: "North Site", Order: 1, SiteRef: 77},
			{Label: "South Site", Order: 2, SiteRef: 78},
		},
		Units: domain.Units{
			{Label: "Engine 12", Order: 1, UnitRef: 12345},
			{Label: "Engines 100 to 199", Order: 2, UnitRef: 100, UnitFrom: 100, UnitTo: 199},
		},
	}}

	if err := repo.SaveSystems(context.Background(), submitted); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	stored, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	wantSites := domain.Sites{
		{ID: 1, Label: "North Site", Order: 1, SiteRef: 77, SystemID: 1},
		{ID: 2, Label: "South Site", Order: 2, SiteRef: 78, SystemID: 1},
	}

	if len(stored[0].Sites) != len(wantSites) {
		t.Fatalf("the saved system has sites %#v, want %#v", stored[0].Sites, wantSites)
	}

	for i := range wantSites {
		if stored[0].Sites[i] != wantSites[i] {
			t.Errorf("site %d reads back as %#v, want %#v", i, stored[0].Sites[i], wantSites[i])
		}
	}

	wantUnits := domain.Units{
		{ID: 1, Label: "Engine 12", Order: 1, SystemID: 1, UnitRef: 12345},
		{ID: 2, Label: "Engines 100 to 199", Order: 2, SystemID: 1, UnitRef: 100, UnitFrom: 100, UnitTo: 199},
	}

	if len(stored[0].Units) != len(wantUnits) {
		t.Fatalf("the saved system has units %#v, want %#v", stored[0].Units, wantUnits)
	}

	for i := range wantUnits {
		if stored[0].Units[i] != wantUnits[i] {
			t.Errorf("unit %d reads back as %#v, want %#v", i, stored[0].Units[i], wantUnits[i])
		}
	}
}

// A site or unit the submitted list omits is removed: the systems reconcile applies to the parts
// the aggregate owns.
func TestSaveSystemsRemovesAStoredSiteOrUnitTheSubmittedListOmits(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if len(systems[0].Sites) != 1 || len(systems[0].Units) != 1 {
		t.Fatalf("the fixture system has %d sites and %d units, want 1 and 1", len(systems[0].Sites), len(systems[0].Units))
	}

	kept := append(domain.Systems{}, systems...)
	kept[0].Sites = domain.Sites{}
	kept[0].Units = domain.Units{}

	if err := repo.SaveSystems(context.Background(), kept); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	if got := countRows(t, db, "sites"); got != 0 {
		t.Errorf("the sites table has %d rows, want 0", got)
	}

	if got := countRows(t, db, "units"); got != 0 {
		t.Errorf("the units table has %d rows, want 0", got)
	}

	// The recorded call that named the site is untouched: the site is not what the call is keyed by.
	if got := countRows(t, db, "calls"); got != 2 {
		t.Errorf("the calls table has %d rows, want the stored 2", got)
	}
}

// A saved talkgroup's `talkgroupGroups` rows are the memberships the submitted list names: the
// ones it no longer names are gone.
func TestSaveTalkgroupsReplacesATalkgroupsGroupsMembership(t *testing.T) {
	db := openSchemaDatabase(t)

	seedGroupsTagsAndSystem(t, db)
	seedMemberships(t, db, 3)

	repo := NewSystems(db, DialectSqlite)

	talkgroups, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if got := talkgroups[0].Talkgroups[0].GroupIDs; len(got) != 1 || got[0] != 3 {
		t.Fatalf("the seeded talkgroup has groups %v, want [3]", got)
	}

	submitted := append(domain.Systems{}, talkgroups...)
	submitted[0].Talkgroups[0].GroupIDs = []uint64{2}

	if err := repo.SaveSystems(context.Background(), submitted); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	stored, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if got := stored[0].Talkgroups[0].GroupIDs; len(got) != 1 || got[0] != 2 {
		t.Errorf("the talkgroup has groups %v after the save, want [2]", got)
	}

	if count := countRows(t, db, "talkgroupGroups"); count != 1 {
		t.Errorf("talkgroupGroups has %d rows, want 1", count)
	}
}

// A group membership is inserted only when it is not already stored: repeating a save does not
// accumulate duplicate memberships.
func TestSaveTalkgroupsDoesNotStoreAGroupMembershipTwice(t *testing.T) {
	db := openSchemaDatabase(t)

	seedGroupsTagsAndSystem(t, db)

	repo := NewSystems(db, DialectSqlite)

	for round := 1; round <= 3; round++ {
		systems, err := repo.AllSystems(context.Background())
		if err != nil {
			t.Fatalf("AllSystems: %v", err)
		}

		submitted := append(domain.Systems{}, systems...)
		submitted[0].Talkgroups[0].GroupIDs = []uint64{2}

		if err := repo.SaveSystems(context.Background(), submitted); err != nil {
			t.Fatalf("SaveSystems round %d: %v", round, err)
		}

		if count := countRows(t, db, "talkgroupGroups"); count != 1 {
			t.Fatalf("talkgroupGroups has %d rows after round %d, want 1", count, round)
		}
	}
}

// A talkgroup in groups 2 and 1 saved as group 2 alone loses the membership that named the dropped
// group: the legacy `Talkgroups.WriteTx` tested the submitted identifiers against the join row's own
// number, and this server compares against `groupId`.
func TestSaveTalkgroupsRemovesAGroupMembershipTheSubmittedListOmits(t *testing.T) {
	db := openSchemaDatabase(t)

	seedGroupsTagsAndSystem(t, db)

	// The talkgroup is in group 2 (join row 1) and group 1 (join row 2).
	seedMemberships(t, db, 2, 1)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	submitted := append(domain.Systems{}, systems...)
	submitted[0].Talkgroups[0].GroupIDs = []uint64{2}

	if err := repo.SaveSystems(context.Background(), submitted); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	stored, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	got := stored[0].Talkgroups[0].GroupIDs

	if containsID(got, 1) {
		t.Errorf("the talkgroup has groups %v after the save, want only group 2: the membership the list dropped is the membership removed", got)
	}

	if !containsID(got, 2) {
		t.Errorf("the talkgroup has groups %v after the save, want group 2 to still be a member", got)
	}

	// The membership that did not change is left where it is rather than deleted and re-inserted.
	if count := countRows(t, db, "talkgroupGroups"); count != 1 {
		t.Errorf("talkgroupGroups has %d rows, want 1", count)
	}
}

// A new talkgroup in a save does not protect the stored talkgroups the list dropped: the legacy
// `Talkgroups.WriteTx` tested the stored identifier against zero, not the submitted one.
func TestSaveTalkgroupsRemovesATalkgroupTheSubmittedListOmitsEvenWhenANewOneIsAdded(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	kept := talkgroupByID(t, systems[0].Talkgroups, 2)

	submitted := append(domain.Systems{}, systems...)
	submitted[0].Talkgroups = domain.Talkgroups{
		kept,
		{Label: "New Dispatch", Name: "New Dispatch", Order: 4, TagID: 3, TalkgroupRef: 1514009, Kind: "audio"},
	}

	if err := repo.SaveSystems(context.Background(), submitted); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	if got := countRows(t, db, "talkgroups"); got != 3 {
		t.Fatalf("the talkgroups table has %d rows, want 3: system 1's two dropped talkgroups went and the new one arrived", got)
	}

	if got := countRows(t, db, "talkgroupGroups"); got != 1 {
		t.Errorf("talkgroupGroups has %d rows, want 1: the memberships of the deleted talkgroups cascaded with them", got)
	}
}

// A talkgroup identifier already stored under another system is updated where it lives rather than
// moved: the legacy update never names `systemId` and its exists-check is not scoped to the system
// being saved, a defect kept rather than fixed.
func TestSaveTalkgroupsUpdatesATalkgroupWhereItIsStoredInsteadOfMovingIt(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewSystems(db, DialectSqlite)

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	// The administrator moves system 1's first talkgroup to system 2, carrying its identifier.
	moved := talkgroupByID(t, systems[0].Talkgroups, 1)
	moved.Order = 1

	submitted := append(domain.Systems{}, systems...)
	submitted[1].Talkgroups = domain.Talkgroups{moved}

	if err := repo.SaveSystems(context.Background(), submitted); err != nil {
		t.Fatalf("SaveSystems: %v", err)
	}

	var systemID uint64

	if err := db.QueryRow(`SELECT "systemId" FROM "talkgroups" WHERE "talkgroupId" = 1`).Scan(&systemID); err != nil {
		t.Fatalf("reading the talkgroup: %v", err)
	}

	if systemID != 1 {
		t.Errorf("talkgroup 1 is stored under system %d, want the legacy outcome: it stayed under system 1", systemID)
	}

	stored, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if len(stored[1].Talkgroups) != 0 {
		t.Errorf("system 2 has talkgroups %#v, want none: the moved talkgroup was updated where it already lives", stored[1].Talkgroups)
	}
}

// Parameterizing every statement stores a quote inside a label or blacklist as the text it is, and
// the statement around it stays one statement.
func TestSaveSystemsStoresAQuoteInAStoredValueAsText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		label string
	}{
		{name: "an apostrophe", label: "Harbour's Reach"},
		{name: "a doubled quote", label: `Grand "Metro"`},
		{name: "text shaped like the end of a statement", label: `Airport'); DELETE FROM "systems"; --`},
	} {
		db := openSchemaDatabase(t)

		repo := NewSystems(db, DialectSqlite)

		if err := repo.SaveSystems(context.Background(), domain.Systems{
			{Label: tc.label, Order: 1, SystemRef: 10001, Blacklists: "40000,40001"},
		}); err != nil {
			t.Fatalf("%s: SaveSystems: %v", tc.name, err)
		}

		stored, err := repo.AllSystems(context.Background())
		if err != nil {
			t.Fatalf("%s: AllSystems: %v", tc.name, err)
		}

		if len(stored) != 1 || stored[0].Label != tc.label {
			t.Errorf("%s: the saved system reads back as %#v, want the label stored as the text it is", tc.name, stored)
		}

		if got := countRows(t, db, "systems"); got != 1 {
			t.Errorf("%s: the systems table has %d rows, want 1: the value was never part of the statement", tc.name, got)
		}
	}
}

// seedGroupsTagsAndSystem puts one system with one talkgroup that has no group memberships, one
// tag, and three groups in a fresh database, so a join-row test starts from stored rows it names.
func seedGroupsTagsAndSystem(t *testing.T, db *sql.DB) {
	t.Helper()

	const seed = `
		INSERT INTO "tags" ("tagId", "alert", "label", "led", "order") VALUES (1, '', 'Fire Dispatch', '', 0);
		INSERT INTO "groups" ("groupId", "alert", "label", "led", "order") VALUES (1, '', 'Air', '', 0), (2, '', 'Fire', '', 0), (3, '', 'Law', '', 0);
		INSERT INTO "systems" ("systemId", "alert", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (1, '', 0, '', 0, 'Alpha', '', 1, 10001, 'talkgroup');
		INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (1, '', 0, 0, 'Dispatch 1', '', 'Dispatch 1', 1, 1, 1, 1514000, 'audio');
	`

	if _, err := db.Exec(seed); err != nil {
		t.Fatalf("seeding a system with one talkgroup: %v", err)
	}
}

// seedMemberships stores group memberships for the talkgroup seedGroupsTagsAndSystem created, in the
// order given, so that a test can say which join row carries which group.
func seedMemberships(t *testing.T, db *sql.DB, groupIDs ...uint64) {
	t.Helper()

	for _, groupID := range groupIDs {
		if _, err := db.Exec(`INSERT INTO "talkgroupGroups" ("groupId", "talkgroupId") VALUES (?, 1)`, groupID); err != nil {
			t.Fatalf("seeding the membership of group %d: %v", groupID, err)
		}
	}
}

// talkgroupByID finds a stored talkgroup, and fails the test when the fixture does not have it.
func talkgroupByID(t *testing.T, talkgroups domain.Talkgroups, id uint64) domain.Talkgroup {
	t.Helper()

	talkgroup, ok := talkgroups.ByID(id)
	if !ok {
		t.Fatalf("talkgroup %d was not loaded: the fixture written by the legacy server is not the one generate.sh produced", id)
	}

	return talkgroup
}

func idsAsText(ids []uint64) []string {
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		out = append(out, strconv.FormatUint(id, 10))
	}

	return out
}

func containsID(ids []uint64, want uint64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}

	return false
}

// The per-level sites save renumbers the submitted rows 1-based, updates what is stored, inserts
// what is new, and leaves the other system's sites alone.
func TestSaveSystemSitesReplacesTheSitesOfOneSystem(t *testing.T) {
	db := openLegacyDatabase(t)
	repo := NewSystems(db, DialectSqlite)

	sites := domain.Sites{
		{ID: 1, SiteRef: 77, Label: "Metro East"},
		{SiteRef: 102, Label: "Metro West"},
	}

	if err := repo.SaveSystemSites(context.Background(), 1, sites); err != nil {
		t.Fatalf("SaveSystemSites: %v", err)
	}

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	first, ok := systems.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored")
	}

	if len(first.Sites) != 2 {
		t.Fatalf("system 1 has %d sites, want the two submitted rows", len(first.Sites))
	}

	if first.Sites[0].Label != "Metro East" || first.Sites[0].Order != 1 || first.Sites[0].SiteRef != 77 {
		t.Errorf("the stored row = %+v, want the submitted row updated in place at order 1", first.Sites[0])
	}

	if first.Sites[1].Label != "Metro West" || first.Sites[1].Order != 2 || first.Sites[1].SiteRef != 102 {
		t.Errorf("the inserted row = %+v, want order 2", first.Sites[1])
	}

	second, ok := systems.ByID(2)
	if !ok {
		t.Fatalf("system 2 is not stored")
	}

	for _, site := range second.Sites {
		if site.SystemID != 2 {
			t.Errorf("system 2's site %+v was touched by a save of system 1", site)
		}
	}
}

// The per-level sites save removes a site the submitted list does not name.
func TestSaveSystemSitesRemovesTheRowsNotSubmitted(t *testing.T) {
	db := openLegacyDatabase(t)
	repo := NewSystems(db, DialectSqlite)

	sites := domain.Sites{{SiteRef: 200, Label: "Only Site"}}

	if err := repo.SaveSystemSites(context.Background(), 1, sites); err != nil {
		t.Fatalf("SaveSystemSites: %v", err)
	}

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	first, ok := systems.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored")
	}

	if len(first.Sites) != 1 || first.Sites[0].Label != "Only Site" || first.Sites[0].Order != 1 {
		t.Errorf("system 1 has sites %+v, want only the submitted row at order 1", first.Sites)
	}
}

// Saving sites for a system the store does not have reports ErrNotFound and writes nothing.
func TestSaveSystemSitesReportsAnUnknownSystem(t *testing.T) {
	db := openLegacyDatabase(t)
	repo := NewSystems(db, DialectSqlite)

	err := repo.SaveSystemSites(context.Background(), 99, domain.Sites{{SiteRef: 1, Label: "Nowhere"}})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("SaveSystemSites error = %v, want ports.ErrNotFound", err)
	}

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	first, ok := systems.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored")
	}

	if len(first.Sites) != 1 || first.Sites[0].Label != "North Site" {
		t.Errorf("system 1 has sites %+v, want the stored site untouched by a save of an unknown system", first.Sites)
	}
}

// The per-level talkgroups save replaces one system's talkgroups, including the group memberships
// the submitted rows carry.
func TestSaveSystemTalkgroupsReplacesTheTalkgroupsOfOneSystem(t *testing.T) {
	db := openLegacyDatabase(t)
	repo := NewSystems(db, DialectSqlite)

	talkgroups := domain.Talkgroups{
		{ID: 1, TalkgroupRef: 1514000, Label: "Ground", Name: "Dispatch 1", TagID: 3, GroupIDs: []uint64{1}},
		{TalkgroupRef: 1514100, Label: "Fire", Name: "Dispatch 2", TagID: 6, GroupIDs: []uint64{3}},
	}

	if err := repo.SaveSystemTalkgroups(context.Background(), 1, talkgroups, nil); err != nil {
		t.Fatalf("SaveSystemTalkgroups: %v", err)
	}

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	first, ok := systems.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored")
	}

	if len(first.Talkgroups) != 2 {
		t.Fatalf("system 1 has %d talkgroups, want the two submitted rows: the stored talkgroup 3 is removed", len(first.Talkgroups))
	}

	if first.Talkgroups[0].TalkgroupRef != 1514000 || first.Talkgroups[0].Order != 1 {
		t.Errorf("the stored row = %+v, want the submitted row updated in place at order 1", first.Talkgroups[0])
	}

	if first.Talkgroups[1].TalkgroupRef != 1514100 || first.Talkgroups[1].Order != 2 {
		t.Errorf("the inserted row = %+v, want order 2", first.Talkgroups[1])
	}

	if strings.Join(idsAsText(first.Talkgroups[0].GroupIDs), ",") != "1" {
		t.Errorf("the stored talkgroup has groups %v, want the submitted membership", first.Talkgroups[0].GroupIDs)
	}

	if strings.Join(idsAsText(first.Talkgroups[1].GroupIDs), ",") != "3" {
		t.Errorf("the inserted talkgroup has groups %v, want the submitted membership", first.Talkgroups[1].GroupIDs)
	}
}

// TestSaveSystemUnitsReplacesTheUnitsOfOneSystem pins the per-level unit save.
func TestSaveSystemUnitsReplacesTheUnitsOfOneSystem(t *testing.T) {
	db := openLegacyDatabase(t)
	repo := NewSystems(db, DialectSqlite)

	units := domain.Units{
		{ID: 1, UnitRef: 12345, Label: "Engine 12"},
		{UnitRef: 678, Label: "Car 3"},
	}

	if err := repo.SaveSystemUnits(context.Background(), 1, units); err != nil {
		t.Fatalf("SaveSystemUnits: %v", err)
	}

	systems, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	first, ok := systems.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored")
	}

	if len(first.Units) != 2 {
		t.Fatalf("system 1 has %d units, want the two submitted rows", len(first.Units))
	}

	if first.Units[0].Label != "Engine 12" || first.Units[0].Order != 1 {
		t.Errorf("the stored row = %+v, want the submitted row updated in place at order 1", first.Units[0])
	}

	if first.Units[1].Label != "Car 3" || first.Units[1].Order != 2 {
		t.Errorf("the inserted row = %+v, want order 2", first.Units[1])
	}
}

// The per-system saves renumber the submitted rows' display order and stamp the system identity
// into a copy, never the caller's own slice.
func TestSaveSystemLevelSavesLeaveTheCallersSliceUntouched(t *testing.T) {
	db := openLegacyDatabase(t)

	defer db.Close()

	repo := NewSystems(db, DialectSqlite)

	sites := domain.Sites{
		{ID: 1, SiteRef: 77, Label: "Metro East"},
		{SiteRef: 102, Label: "Metro West"},
	}

	sitesWant := append(domain.Sites{}, sites...)

	talkgroups := domain.Talkgroups{
		{ID: 1, TalkgroupRef: 1514000, Label: "Ground", Name: "Dispatch 1", TagID: 3, GroupIDs: []uint64{1}},
		{TalkgroupRef: 1514100, Label: "Fire", Name: "Dispatch 2", TagID: 6, GroupIDs: []uint64{3}},
	}

	talkgroupsWant := append(domain.Talkgroups{}, talkgroups...)

	units := domain.Units{
		{ID: 1, UnitRef: 12345, Label: "Engine 12"},
		{UnitRef: 678, Label: "Car 3"},
	}

	unitsWant := append(domain.Units{}, units...)

	if err := repo.SaveSystemSites(context.Background(), 1, sites); err != nil {
		t.Fatalf("SaveSystemSites: %v", err)
	}

	if err := repo.SaveSystemTalkgroups(context.Background(), 1, talkgroups, nil); err != nil {
		t.Fatalf("SaveSystemTalkgroups: %v", err)
	}

	if err := repo.SaveSystemUnits(context.Background(), 1, units); err != nil {
		t.Fatalf("SaveSystemUnits: %v", err)
	}

	if !reflect.DeepEqual(sites, sitesWant) {
		t.Errorf("SaveSystemSites renumbered the caller's slice:\n  got  %v\n  want %v", sites, sitesWant)
	}

	if !reflect.DeepEqual(talkgroups, talkgroupsWant) {
		t.Errorf("SaveSystemTalkgroups renumbered the caller's slice:\n  got  %v\n  want %v", talkgroups, talkgroupsWant)
	}

	if !reflect.DeepEqual(units, unitsWant) {
		t.Errorf("SaveSystemUnits renumbered the caller's slice:\n  got  %v\n  want %v", units, unitsWant)
	}
}

// The Systems-section save writes the system row's own columns and leaves every level under the
// system as the store holds it, including a row the level deleted after the page loaded its copy.
func TestSaveSystemSettingsWritesTheSystemRowAndLeavesTheLevels(t *testing.T) {
	db := openLegacyDatabase(t)
	repo := NewSystems(db, DialectSqlite)

	ctx := context.Background()

	// The copy the Systems section is editing: the systems as the page loaded them.
	loaded, err := repo.AllSystems(ctx)
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	page := append(domain.Systems{}, loaded...)

	first, ok := page.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored")
	}

	sitesWant := append(domain.Sites{}, first.Sites...)
	unitsWant := append(domain.Units{}, first.Units...)

	// The operator deletes a talkgroup in the Talkgroups section and saves that level.
	remaining := domain.Talkgroups{}

	for _, talkgroup := range first.Talkgroups {
		if talkgroup.ID != 1 {
			remaining = append(remaining, talkgroup)
		}
	}

	if err := repo.SaveSystemTalkgroups(ctx, 1, remaining, nil); err != nil {
		t.Fatalf("SaveSystemTalkgroups: %v", err)
	}

	// The operator renames the system and saves the Systems section, from the copy the page loaded.
	page[0].Label = "Grand Metro Renamed"

	if err := repo.SaveSystemSettings(ctx, page); err != nil {
		t.Fatalf("SaveSystemSettings: %v", err)
	}

	stored, err := repo.AllSystems(ctx)
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	first, ok = stored.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored")
	}

	if first.Label != "Grand Metro Renamed" {
		t.Errorf("the stored system is labelled %q, want the label the Systems section saved", first.Label)
	}

	if len(first.Talkgroups) != len(remaining) {
		t.Fatalf("the system has %d talkgroups after the systems-section save, want the %d the talkgroups save left", len(first.Talkgroups), len(remaining))
	}

	if _, found := first.Talkgroups.ByID(1); found {
		t.Errorf("talkgroup 1, which the talkgroups save deleted, is stored again as %+v", first.Talkgroups)
	}

	if !reflect.DeepEqual(first.Sites, sitesWant) {
		t.Errorf("the systems-section save rewrote the sites:\n  got  %v\n  want %v", first.Sites, sitesWant)
	}

	if !reflect.DeepEqual(first.Units, unitsWant) {
		t.Errorf("the systems-section save rewrote the units:\n  got  %v\n  want %v", first.Units, unitsWant)
	}
}

// The Talkgroups section saves its rows and the system's blacklist together: the server compares a
// recording's talkgroup reference against that text, so the save that drops a row stores the entry
// that refuses it.
func TestSaveSystemTalkgroupsStoresTheBlacklistWithTheRows(t *testing.T) {
	db := openLegacyDatabase(t)
	repo := NewSystems(db, DialectSqlite)

	ctx := context.Background()

	// The blacklisted row is gone from the list, and its reference is the entry the list carries.
	talkgroups := domain.Talkgroups{
		{ID: 2, TalkgroupRef: 1514001, Label: "Interop 9", Name: "Interop 9", TagID: 6, GroupIDs: []uint64{1}},
		{ID: 3, TalkgroupRef: 1514002, Label: "Delayed Talk", Name: "Delayed Talk", TagID: 6, GroupIDs: []uint64{1}},
	}

	blacklists := domain.Blacklists("1514000")

	if err := repo.SaveSystemTalkgroups(ctx, 1, talkgroups, &blacklists); err != nil {
		t.Fatalf("SaveSystemTalkgroups: %v", err)
	}

	stored, err := repo.AllSystems(ctx)
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	first, ok := stored.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored")
	}

	if first.Blacklists != domain.Blacklists("1514000") {
		t.Errorf("the stored blacklist is %q, want the entry the talkgroups save carried with the rows", first.Blacklists)
	}

	if len(first.Talkgroups) != 2 {
		t.Fatalf("the system has %d talkgroups, want the 2 submitted rows", len(first.Talkgroups))
	}

	if _, found := first.Talkgroups.ByID(1); found {
		t.Errorf("talkgroup 1 is stored, want the row the submitted list dropped to be removed")
	}
}

// A level save that carries rows only leaves the system's stored blacklist where it is.
func TestSaveSystemTalkgroupsLeavesTheStoredBlacklistWhenTheSaveNamesNone(t *testing.T) {
	db := openLegacyDatabase(t)
	repo := NewSystems(db, DialectSqlite)

	ctx := context.Background()

	talkgroups := domain.Talkgroups{
		{ID: 1, TalkgroupRef: 1514000, Label: "Dispatch 1", Name: "Dispatch 1", TagID: 3, GroupIDs: []uint64{1}},
	}

	if err := repo.SaveSystemTalkgroups(ctx, 1, talkgroups, nil); err != nil {
		t.Fatalf("SaveSystemTalkgroups: %v", err)
	}

	stored, err := repo.AllSystems(ctx)
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	first, ok := stored.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored")
	}

	if first.Blacklists != domain.Blacklists("40000,40001") {
		t.Errorf("the stored blacklist is %q, want the text the store held: a level save that names no blacklist does not clear it", first.Blacklists)
	}
}

// A systems-section save that says nothing about the blacklist keeps the text the store holds, and
// the next start over the same database reads it back.
func TestSaveSystemSettingsKeepsTheStoredBlacklistTextAcrossAStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rdio-scanner.db")

	ctx := context.Background()

	db := openDatabaseAt(t, path, append(append([]string{}, legacySchema...), legacyRows...))

	repo := NewSystems(db, DialectSqlite)

	loaded, err := repo.AllSystems(ctx)
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	next := append(domain.Systems{}, loaded...)

	first, ok := next.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored")
	}

	if first.Blacklists != domain.Blacklists("40000,40001") {
		t.Fatalf("the fixture stores blacklist %q, want the text the legacy server wrote", first.Blacklists)
	}

	for i := range next {
		if next[i].ID == 1 {
			next[i].Label = "Grand Metro Renamed"
		}
	}

	if err := repo.SaveSystemSettings(ctx, next); err != nil {
		t.Fatalf("SaveSystemSettings: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("closing the database: %v", err)
	}

	restarted := NewSystems(openDatabaseAt(t, path, nil), DialectSqlite)

	stored, err := restarted.AllSystems(ctx)
	if err != nil {
		t.Fatalf("AllSystems after the start: %v", err)
	}

	first, ok = stored.ByID(1)
	if !ok {
		t.Fatalf("system 1 is not stored after the start")
	}

	if first.Blacklists != domain.Blacklists("40000,40001") {
		t.Errorf("the system reads back blacklist %q after the start, want the text that was stored", first.Blacklists)
	}

	if first.Label != "Grand Metro Renamed" {
		t.Errorf("the system reads back label %q after the start, want the label the systems-section save stored", first.Label)
	}
}

// openDatabaseAt opens a database at a path the caller names and runs the statements given, so a
// test can close the file, open it again the way the next start does, and read what is in it
// without laying the fixture down a second time.
func openDatabaseAt(t *testing.T, path string, statements []string) *sql.DB {
	t.Helper()

	db, err := Open(Settings{Dialect: DialectSqlite, FilePath: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("the recorded statement %s does not run: %v", firstLineOf(statement), err)
		}
	}

	return db
}

// The systems columns delay, order, and systemRef are signed and the read scans them
// straight into unsigned fields, so a stored negative must read as zero with a warning
// naming the system, not as a wrapped huge number, and never fail the whole catalog read.
func TestStoredNegativeSystemValuesReadAsZeroWithAWarning(t *testing.T) {
	db := openLegacyDatabase(t)

	const query = `INSERT INTO "systems" ("label", "delay", "order", "systemRef", "type") VALUES ('legacy-row', -5, -7, -9, 'talkgroup')`

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	repo := NewSystems(db, DialectSqlite)

	warnings := []string{}

	repo.OnWarn(func(message string) {
		warnings = append(warnings, message)
	})

	list, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	var legacy domain.System

	for _, system := range list {
		if system.Label == "legacy-row" {
			legacy = system
		}
	}

	if legacy.Label != "legacy-row" {
		t.Fatal("the stored legacy-row system did not read back at all")
	}

	if legacy.Delay != 0 || legacy.Order != 0 || legacy.SystemRef != 0 {
		t.Errorf("the stored -5, -7, -9 read as delay %d, order %d, systemRef %d, want all 0",
			legacy.Delay, legacy.Order, legacy.SystemRef)
	}

	if len(warnings) == 0 {
		t.Fatal("the clamp was said to nobody, want a warning naming the system it clamped")
	}

	if !strings.Contains(warnings[0], "legacy-row") {
		t.Errorf("the warning says %q, want it to name the system it clamped", warnings[0])
	}
}

// The units columns order, unitRef, unitFrom, and unitTo are signed and the read scans
// them straight into unsigned fields, so a stored negative must read as zero with a
// warning naming the unit, and the clamps found under a system must reach the same
// listener the systems read reports through.
func TestStoredNegativeUnitValuesReadAsZeroWithAWarning(t *testing.T) {
	db := openLegacyDatabase(t)

	const query = `INSERT INTO "units" ("label", "order", "unitRef", "unitFrom", "unitTo", "systemId") VALUES ('legacy-row', -7, -11, -13, -17, 1)`

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	repo := NewSystems(db, DialectSqlite)

	warnings := []string{}

	repo.OnWarn(func(message string) {
		warnings = append(warnings, message)
	})

	list, err := repo.AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	var legacy domain.Unit

	for _, unit := range list[0].Units {
		if unit.Label == "legacy-row" {
			legacy = unit
		}
	}

	if legacy.Label != "legacy-row" {
		t.Fatal("the stored legacy-row unit did not read back at all")
	}

	if legacy.Order != 0 || legacy.UnitRef != 0 || legacy.UnitFrom != 0 || legacy.UnitTo != 0 {
		t.Errorf("the stored -7, -11, -13, -17 read as order %d, unitRef %d, unitFrom %d, unitTo %d, want all 0",
			legacy.Order, legacy.UnitRef, legacy.UnitFrom, legacy.UnitTo)
	}

	if len(warnings) == 0 {
		t.Fatal("the clamp was said to nobody, want a warning naming the unit it clamped")
	}

	if !strings.Contains(warnings[0], "legacy-row") {
		t.Errorf("the warning says %q, want it to name the unit it clamped", warnings[0])
	}
}
