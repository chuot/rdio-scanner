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
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

// openDownstreamTestDatabase opens an empty database with the legacy schema applied, for the tests
// that write.
func openDownstreamTestDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db, err := Open(Settings{
		Dialect:  DialectSqlite,
		FilePath: filepath.Join(t.TempDir(), "rdio-scanner.db"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	return db
}

// TestAllDownstreamsReadsWhatTheLegacyServerStored is the the criterion half for this adapter: the
// downstream the legacy server stored through its own configuration endpoint reads back field for
// field, including the scope it stored as JSON text.
func TestAllDownstreamsReadsWhatTheLegacyServerStored(t *testing.T) {
	db := openLegacyDatabase(t)

	if got := countRows(t, db, "downstreams"); got == 0 {
		t.Fatal("the database written by the legacy server stores no downstream")
	}

	downstreams, err := NewDownstreams(db, DialectSqlite).AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	if len(downstreams) != 1 {
		t.Fatalf("AllDownstreams returned %d downstreams, want the one the legacy server stored", len(downstreams))
	}

	downstream := downstreams[0]

	if downstream.ID != 1 {
		t.Errorf("ID = %d, want 1: the identifier the legacy table assigned", downstream.ID)
	}

	if downstream.Apikey != "fixture-apikey-0123456789" {
		t.Errorf("Apikey = %q, want the key the legacy server stored", downstream.Apikey)
	}

	if !downstream.Disabled {
		t.Errorf("Disabled = %v, want true: the legacy server stored the flag as on", downstream.Disabled)
	}

	if downstream.Order != 1 {
		t.Errorf("Order = %d, want 1", downstream.Order)
	}

	if downstream.URL != "http://127.0.0.1:3999/api/call-upload" {
		t.Errorf("URL = %q, want the address the legacy server stored", downstream.URL)
	}

	if !downstream.Scope.Present || downstream.Scope.Wildcard {
		t.Fatalf("Scope = %#v, want the explicit scope the legacy server stored", downstream.Scope)
	}

	if !downstream.Scope.GrantsReferences(10002, 1520000) {
		t.Error("the stored scope does not grant the system and talkgroup it names")
	}

	if downstream.Scope.GrantsReferences(10002, 1520001) || downstream.Scope.GrantsReferences(10001, 1514000) {
		t.Error("the stored scope grants a talkgroup it does not name")
	}
}

// TestSaveDownstreamsWritesTheStoredScopeBackAsTheSameText proves the two servers agree about the
// `systems` column: the scope read out of the legacy-written row is written back as the exact text
// the legacy server wrote.
func TestSaveDownstreamsWritesTheStoredScopeBackAsTheSameText(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewDownstreams(db, DialectSqlite)

	loaded, err := repo.AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	const storedScope = `[{"id":10002,"talkgroups":[1520000]}]`

	var before string

	if err := db.QueryRow(`SELECT "systems" FROM "downstreams"`).Scan(&before); err != nil {
		t.Fatalf("reading the stored scope: %v", err)
	}

	if before != storedScope {
		t.Fatalf("the legacy server stored %q, want %q", before, storedScope)
	}

	if err := repo.SaveDownstreams(context.Background(), loaded); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	var after string

	if err := db.QueryRow(`SELECT "systems" FROM "downstreams"`).Scan(&after); err != nil {
		t.Fatalf("reading the stored scope: %v", err)
	}

	if after != storedScope {
		t.Errorf("the scope read from the legacy row was written back as %s, want the same text: %s", after, storedScope)
	}
}

// TestAllDownstreamsReadsTheFlagsTheLegacyServerStored pins the boolean column in both directions
// of what the legacy server writes: the fixture's row holds the integer 1, and a row written with
// the bare `true`/`false` tokens Go's `%t` produces holds the same.
func TestAllDownstreamsReadsTheFlagsTheLegacyServerStored(t *testing.T) {
	db := openLegacyDatabase(t)

	// The legacy server's own INSERT, with the `%t` slot resolved to the bare `false` token it
	// produces, and the scope it marshalled — the wildcard arrives as the JSON text `"*"`. The
	// statement is a constant reproduced from `server/downstream.go` Downstreams.Write; no Go value
	// is interpolated into it.
	const legacyRow = `INSERT INTO "downstreams" ("apikey", "disabled", "order", "systems", "url") VALUES ('enabled-downstream', false, 9, '"*"', 'http://127.0.0.1:4000/api/call-upload')`

	if _, err := db.Exec(legacyRow); err != nil {
		t.Fatalf("writing a row the way the legacy server writes one: %v", err)
	}

	downstreams, err := NewDownstreams(db, DialectSqlite).AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	if len(downstreams) != 2 {
		t.Fatalf("AllDownstreams returned %d downstreams, want the fixture's and the one this test wrote", len(downstreams))
	}

	var enabled domain.Downstream

	for _, downstream := range downstreams {
		if downstream.Apikey == "enabled-downstream" {
			enabled = downstream
		}
	}

	if enabled.ID == 0 {
		t.Fatal("the row written the way the legacy server writes one was not read back")
	}

	if enabled.Disabled {
		t.Errorf("a legacy-stored `false` reads back as Disabled=%v, want false", enabled.Disabled)
	}

	if !enabled.Scope.Wildcard || !enabled.Scope.GrantsReferences(99999, 99999) {
		t.Errorf("a stored `*` scope reads back as %#v, want the wildcard that grants everything", enabled.Scope)
	}
}

// TestSaveDownstreamsWritesRowsTheLegacyServerReadsBack is the the criterion half for this adapter: the
// row this server writes is read by the legacy server's own read statement, scanning into the field
// types its Downstream has, and the flag lands with the storage class its rows have.
func TestSaveDownstreamsWritesRowsTheLegacyServerReadsBack(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	scope, _ := domain.ParseAccessScope([]any{
		map[string]any{"id": float64(10001), "talkgroups": []any{float64(1514000), float64(1514001)}},
	})

	submitted := domain.Downstreams{{
		Apikey: "round-trip-key",
		Order:  3,
		Scope:  scope,
		URL:    "https://other.example.com/api/call-upload",
	}}

	if err := NewDownstreams(db, DialectSqlite).SaveDownstreams(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	// The legacy server's read statement, verbatim, scanning into the types its Downstream fields
	// have.
	var (
		id       uint64
		apikey   string
		disabled bool
		order    uint
		systems  string
		url      string
	)

	const legacyRead = `SELECT "downstreamId", "apikey", "disabled", "order", "systems", "url" FROM "downstreams"`

	if err := db.QueryRow(legacyRead).Scan(&id, &apikey, &disabled, &order, &systems, &url); err != nil {
		t.Fatalf("the legacy server's own read cannot read the row this server wrote: %v", err)
	}

	want := submitted[0]

	if apikey != want.Apikey || disabled != want.Disabled || order != want.Order || url != want.URL {
		t.Errorf("the legacy read got apikey=%q disabled=%v order=%d url=%q, want %q %v %d %q",
			apikey, disabled, order, url, want.Apikey, want.Disabled, want.Order, want.URL)
	}

	// The legacy server reads the scope by unmarshalling the column when it is not empty.
	var legacy any

	if len(systems) > 0 {
		if err := json.Unmarshal([]byte(systems), &legacy); err != nil {
			t.Fatalf("the legacy server cannot unmarshal the scope this server wrote (%s): %v", systems, err)
		}
	}

	legacyScope, present := domain.ParseAccessScope(legacy)

	if !present || !legacyScope.GrantsReferences(10001, 1514001) {
		t.Errorf("the scope this server wrote reads back in the legacy server as %#v", legacyScope)
	}

	var disabledType string

	if err := db.QueryRow(`SELECT typeof("disabled") FROM "downstreams"`).Scan(&disabledType); err != nil {
		t.Fatalf("typeof: %v", err)
	}

	if disabledType != "integer" {
		t.Errorf("the flag is stored as %s, want integer as the legacy server stores it", disabledType)
	}
}

// TestAllDownstreamsReadsAnEmptyScopeColumnAsNoScopeSupplied pins the state the legacy server leaves
// behind for a downstream whose payload named no systems: an empty column, which forwards nothing.
func TestAllDownstreamsReadsAnEmptyScopeColumnAsNoScopeSupplied(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	// The legacy server writes an empty string when the submitted downstream named no systems at
	// all, because its Write only marshals a scope that is not nil.
	const legacyRow = `INSERT INTO "downstreams" ("apikey", "disabled", "order", "systems", "url") VALUES ('no-scope', false, 1, '', 'http://127.0.0.1:4001/api/call-upload')`

	if _, err := db.Exec(legacyRow); err != nil {
		t.Fatalf("writing a row the way the legacy server writes one: %v", err)
	}

	downstreams, err := NewDownstreams(db, DialectSqlite).AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	if len(downstreams) != 1 {
		t.Fatalf("AllDownstreams returned %d downstreams, want 1", len(downstreams))
	}

	if downstreams[0].Scope.Present {
		t.Errorf("an empty scope column reads back as %#v, want no scope supplied at all", downstreams[0].Scope)
	}

	if downstreams[0].Accepts(domain.Call{System: domain.System{SystemRef: 10001}, Talkgroup: domain.Talkgroup{TalkgroupRef: 1514000}}) {
		t.Error("a downstream with no scope forwards a call, want nothing forwarded")
	}
}

// TestSaveDownstreamsStoresANoScopeDownstreamSoTheLegacyServerReadsItAsNone covers the other
// direction. This server writes the shared scope text, which names a scope that was never supplied as
// `null` rather than the empty string the legacy server writes; the legacy server's own read rule
// turns that into the same thing it stores.
func TestSaveDownstreamsStoresANoScopeDownstreamSoTheLegacyServerReadsItAsNone(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	submitted := domain.Downstreams{{
		Apikey: "no-scope",
		URL:    "http://127.0.0.1:4001/api/call-upload",
	}}

	if err := NewDownstreams(db, DialectSqlite).SaveDownstreams(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	var systems string

	if err := db.QueryRow(`SELECT "systems" FROM "downstreams"`).Scan(&systems); err != nil {
		t.Fatalf("reading the stored scope: %v", err)
	}

	if systems != "null" {
		t.Errorf("a downstream with no scope is stored as %q, want the null the shared scope text produces", systems)
	}

	// The legacy server's read rule: unmarshal the column when it is not empty.
	var legacy any

	if len(systems) > 0 {
		if err := json.Unmarshal([]byte(systems), &legacy); err != nil {
			t.Fatalf("the legacy server cannot read the scope this server wrote (%s): %v", systems, err)
		}
	}

	if _, present := domain.ParseAccessScope(legacy); present {
		t.Errorf("the legacy server reads the scope this server wrote as supplied, want it read as no scope")
	}
}

// TestAllDownstreamsReturnsTheDownstreamsInTheirDisplayOrder pins the ordering the legacy server
// applies to the list it read.
func TestAllDownstreamsReturnsTheDownstreamsInTheirDisplayOrder(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	submitted := domain.Downstreams{
		{Apikey: "third", Order: 30, URL: "http://127.0.0.1:4003/api/call-upload"},
		{Apikey: "first", Order: 10, URL: "http://127.0.0.1:4001/api/call-upload"},
		{Apikey: "second", Order: 20, URL: "http://127.0.0.1:4002/api/call-upload"},
	}

	if err := NewDownstreams(db, DialectSqlite).SaveDownstreams(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	stored, err := NewDownstreams(db, DialectSqlite).AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	if len(stored) != 3 {
		t.Fatalf("the table stores %d downstreams, want 3", len(stored))
	}

	for i, want := range []uint{10, 20, 30} {
		if stored[i].Order != want {
			t.Errorf("downstream %d has order %d, want %d", i, stored[i].Order, want)
		}
	}
}

// TestAllDownstreamsReportsTheConfigurationInEffectAndNotTheStoredRows is the configuration-in-effect rule.
func TestAllDownstreamsReportsTheConfigurationInEffectAndNotTheStoredRows(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	repo := NewDownstreams(db, DialectSqlite)

	submitted := domain.Downstreams{{Apikey: "live", Order: 1, URL: "http://127.0.0.1:4001/api/call-upload"}}

	if err := repo.SaveDownstreams(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	if _, err := db.Exec(`DELETE FROM "downstreams"`); err != nil {
		t.Fatalf("DELETE: %v", err)
	}

	live, err := repo.AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	if len(live) != 1 || live[0].Apikey != "live" {
		t.Errorf("AllDownstreams returned %v, want the downstream the running server is configured with", live)
	}

	restarted, err := NewDownstreams(db, DialectSqlite).AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams after a restart: %v", err)
	}

	if len(restarted) != 0 {
		t.Errorf("a restarted server reads %v, want nothing: the table is empty", restarted)
	}
}

// TestSaveDownstreamsLeavesTheSubmittedDownstreamsInEffectWhenTheSaveFails is a preserved quirk.
func TestSaveDownstreamsLeavesTheSubmittedDownstreamsInEffectWhenTheSaveFails(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	repo := NewDownstreams(db, DialectSqlite)

	stored := domain.Downstreams{{Apikey: "stored", Order: 1, URL: "http://127.0.0.1:4001/api/call-upload"}}

	if err := repo.SaveDownstreams(context.Background(), stored); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	unreachable, cancel := context.WithCancel(context.Background())
	cancel()

	submitted := domain.Downstreams{{Apikey: "submitted", Order: 2, URL: "http://127.0.0.1:4002/api/call-upload"}}

	err := repo.SaveDownstreams(unreachable, submitted)
	if err == nil {
		t.Fatal("a save against a store that cannot be reached is a failure the caller is told about")
	}

	if !strings.HasPrefix(err.Error(), "downstreams.write:") {
		t.Errorf("error = %v, want the legacy downstreams.write shape", err)
	}

	live, err := repo.AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	if len(live) != 1 || live[0].Apikey != "submitted" {
		t.Errorf("the server forwards to %v, want the submitted downstream: the legacy server replaces its list before it writes", live)
	}

	var apikey string

	if err := db.QueryRow(`SELECT "apikey" FROM "downstreams"`).Scan(&apikey); err != nil {
		t.Fatalf("reading the stored apikey: %v", err)
	}

	if apikey != "stored" {
		t.Errorf("the table stores %q, want the downstream that was stored before the failed save", apikey)
	}
}

// TestSaveDownstreamsRemovesTheDownstreamsTheSubmittedListNoLongerNames is the reconciling half of
// the save.
func TestSaveDownstreamsRemovesTheDownstreamsTheSubmittedListNoLongerNames(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	repo := NewDownstreams(db, DialectSqlite)

	if err := repo.SaveDownstreams(context.Background(), domain.Downstreams{
		{Apikey: "keep", Order: 1, URL: "http://127.0.0.1:4001/api/call-upload"},
		{Apikey: "drop", Order: 2, URL: "http://127.0.0.1:4002/api/call-upload"},
	}); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	stored, err := repo.AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	remaining := domain.Downstreams{}

	for _, downstream := range stored {
		if downstream.Apikey == "keep" {
			remaining = append(remaining, downstream)
		}
	}

	if len(remaining) != 1 {
		t.Fatalf("the stored downstreams are %v, want one of them named keep", stored)
	}

	if err := repo.SaveDownstreams(context.Background(), remaining); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	if got := countRows(t, db, "downstreams"); got != 1 {
		t.Fatalf("the table stores %d downstreams after the save, want 1: the dropped one is removed", got)
	}
}

// TestSaveDownstreamsRemovesAStoredDownstreamEvenWhenTheSubmittedListAddsANewOne pins the repaired
// reconcile.
//
// `server/downstream.go` `Downstreams.Write` decides whether a stored downstream is still named by
// scanning the submitted list and stopping at the first entry whose identifier is zero or equal — so
// one new, identifier-less downstream keeps every stored one, and a save that adds a downstream and
// drops another leaves the dropped one stored.
func TestSaveDownstreamsRemovesAStoredDownstreamEvenWhenTheSubmittedListAddsANewOne(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	repo := NewDownstreams(db, DialectSqlite)

	if err := repo.SaveDownstreams(context.Background(), domain.Downstreams{
		{Apikey: "keep", Order: 1, URL: "http://127.0.0.1:4001/api/call-upload"},
		{Apikey: "drop", Order: 2, URL: "http://127.0.0.1:4002/api/call-upload"},
	}); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	stored, err := repo.AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	if len(stored) != 2 {
		t.Fatalf("the table stores %d downstreams, want 2 before the reconciling save", len(stored))
	}

	submitted := domain.Downstreams{
		{ID: stored[0].ID, Apikey: stored[0].Apikey, Order: stored[0].Order, URL: stored[0].URL},
		{Apikey: "new", Order: 3, URL: "http://127.0.0.1:4003/api/call-upload"},
	}

	if err := repo.SaveDownstreams(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	if got := countRows(t, db, "downstreams"); got != 2 {
		t.Fatalf("the table stores %d downstreams, want 2: the downstream the list dropped must go even though the list also adds one", got)
	}

	live, err := repo.AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	if len(live) != 2 {
		t.Errorf("the server forwards to %d downstreams, want 2: the downstream the list dropped is not forwarded to", len(live))
	}
}

// TestSaveDownstreamsUpdatesTheDownstreamThatCarriesTheStoredIdentifier pins the update half of the
// save.
func TestSaveDownstreamsUpdatesTheDownstreamThatCarriesTheStoredIdentifier(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	repo := NewDownstreams(db, DialectSqlite)

	if err := repo.SaveDownstreams(context.Background(), domain.Downstreams{
		{Apikey: "edited", Order: 1, URL: "http://127.0.0.1:4001/api/call-upload"},
	}); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	stored, err := repo.AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	edited := stored[0]
	edited.Disabled = true
	edited.URL = "http://127.0.0.1:4999/api/call-upload"

	if err := repo.SaveDownstreams(context.Background(), domain.Downstreams{edited}); err != nil {
		t.Fatalf("SaveDownstreams: %v", err)
	}

	if got := countRows(t, db, "downstreams"); got != 1 {
		t.Fatalf("the table stores %d downstreams after the edit, want 1: the edit replaced the row", got)
	}

	after, err := NewDownstreams(db, DialectSqlite).AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	if after[0].ID != edited.ID {
		t.Errorf("the edited downstream has identifier %d, want the one it was stored under (%d)", after[0].ID, edited.ID)
	}

	if !after[0].Disabled || after[0].URL != edited.URL {
		t.Errorf("the edited downstream reads back as disabled=%v url=%q, want true %q", after[0].Disabled, after[0].URL, edited.URL)
	}
}

// TestSaveDownstreamsStoresAUrlContainingAQuote is the the requirement half of this adapter: the values go
// in as parameters, so a URL that names a quote is stored exactly as submitted.
func TestSaveDownstreamsStoresAUrlContainingAQuote(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	const url = `http://127.0.0.1:4001/api/call-upload?note=it's`

	submitted := domain.Downstreams{{Apikey: "quoted", Order: 1, URL: url}}

	if err := NewDownstreams(db, DialectSqlite).SaveDownstreams(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDownstreams for a URL containing a quote: %v", err)
	}

	stored, err := NewDownstreams(db, DialectSqlite).AllDownstreams(context.Background())
	if err != nil {
		t.Fatalf("AllDownstreams: %v", err)
	}

	if len(stored) != 1 || stored[0].URL != url {
		t.Errorf("the table stores %v, want the URL exactly as it was submitted", stored)
	}
}

// TestAllDownstreamsReportsAnUnreachableStore keeps the failure the shape the server log has always
// had.
func TestAllDownstreamsReportsAnUnreachableStore(t *testing.T) {
	db := openDownstreamTestDatabase(t)

	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := NewDownstreams(db, DialectSqlite).AllDownstreams(context.Background())
	if err == nil {
		t.Fatal("a store that cannot be reached is a failure the caller is told about")
	}

	if !strings.HasPrefix(err.Error(), "downstreams.read:") {
		t.Errorf("error = %v, want the legacy downstreams.read shape", err)
	}

	if !strings.Contains(err.Error(), `FROM "downstreams"`) {
		t.Errorf("error = %v, want it to name the statement that failed", err)
	}
}
