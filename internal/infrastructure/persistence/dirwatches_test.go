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
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

package persistence

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"rdio-scanner/server-ng/internal/domain"
)

// openDirwatchTestDatabase opens an empty database with the legacy schema applied, for the tests
// that write. It is named after this adapter rather than given a package-wide name so that the
// other persistence adapters' tests can bring their own.
func openDirwatchTestDatabase(t *testing.T) *sql.DB {
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

// TestAllDirwatchesReadsWhatTheLegacyServerStored is the CA-007.1 half for this adapter: the
// directory watch the legacy server stored through its own configuration endpoint reads back
// field for field.
func TestAllDirwatchesReadsWhatTheLegacyServerStored(t *testing.T) {
	db := openLegacyDatabase(t)

	if got := countRows(t, db, "dirwatches"); got == 0 {
		t.Fatal("the database written by the legacy server stores no directory watch")
	}

	dirwatches, err := NewDirwatches(db, DialectSqlite).AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if len(dirwatches) != 1 {
		t.Fatalf("AllDirwatches returned %d watches, want the one the legacy server stored", len(dirwatches))
	}

	watch := dirwatches[0]

	if watch.ID != 1 {
		t.Errorf("ID = %d, want 1: the identifier the legacy table assigned", watch.ID)
	}

	if watch.Delay != 2000 {
		t.Errorf("Delay = %d, want the 2000 ms the legacy server stored", watch.Delay)
	}

	if !strings.HasSuffix(watch.Directory, "/spool") {
		t.Errorf("Directory = %q, want the watched directory the legacy server was told to watch", watch.Directory)
	}

	if watch.Extension != ".wav" {
		t.Errorf("Extension = %q, want .wav", watch.Extension)
	}

	if watch.Kind != domain.DirwatchTypeDSDPlus {
		t.Errorf("Kind = %q, want %q: the parser the legacy payload named with the key \"type\"", watch.Kind, domain.DirwatchTypeDSDPlus)
	}

	if watch.Order != 1 {
		t.Errorf("Order = %d, want 1", watch.Order)
	}
}

// TestAllDirwatchesReadsTheFlagsTheLegacyServerStored pins the boolean columns. The legacy server
// writes Go's `%t` into `integer(1)` columns, which reaches sqlite as the bare tokens `true` and
// `false` — and sqlite reads those as the integers 1 and 0, which is what the fixture's rows hold.
// The read accepts both that and the text a `true`/`false` written as a string would leave behind.
func TestAllDirwatchesReadsTheFlagsTheLegacyServerStored(t *testing.T) {
	db := openLegacyDatabase(t)

	// The legacy server's own INSERT, with the `%t` slots already resolved to the bare `true`
	// and `false` tokens it produces. Nothing here is a Go value interpolated into SQL: the
	// statement is a constant, reproduced from server/dirwatch.go Dirwatches.Write.
	const legacyRow = `INSERT INTO "dirwatches" ("delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type") VALUES (2000, true, '/spool/legacy-flags', false, '.mp3', 0, '', 9, 0, 0, 0, 'default')`

	if _, err := db.Exec(legacyRow); err != nil {
		t.Fatalf("writing a row the way the legacy server writes one: %v", err)
	}

	dirwatches, err := NewDirwatches(db, DialectSqlite).AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	// The fixture's own watch, stored by the legacy server as 0 and 0.
	if len(dirwatches) < 2 {
		t.Fatalf("AllDirwatches returned %d watches, want the fixture's and the one this test wrote", len(dirwatches))
	}

	var legacy domain.Dirwatch

	for _, watch := range dirwatches {
		if watch.Directory == "/spool/legacy-flags" {
			legacy = watch
		}

		if strings.HasSuffix(watch.Directory, "/spool") && !watch.DeleteAfter && watch.Disabled {
			t.Errorf("the fixture's watch reads back as DeleteAfter=%v Disabled=%v, want both false", watch.DeleteAfter, watch.Disabled)
		}
	}

	if !legacy.DeleteAfter {
		t.Errorf("a legacy-stored `true` reads back as DeleteAfter=%v, want true", legacy.DeleteAfter)
	}

	if legacy.Disabled {
		t.Errorf("a legacy-stored `false` reads back as Disabled=%v, want false", legacy.Disabled)
	}
}

// TestSaveDirwatchesWritesRowsTheLegacyServerReadsBack is the CA-007.2 half for this adapter: the
// row server-ng writes is read by the legacy server's own read statement, scanning into the field
// types its Dirwatch has, and lands with the same storage class its rows have.
func TestSaveDirwatchesWritesRowsTheLegacyServerReadsBack(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	submitted := domain.Dirwatches{{
		Delay:       3000,
		DeleteAfter: true,
		Directory:   "/spool/round-trip",
		Disabled:    false,
		Extension:   "opus",
		Frequency:   1514000000,
		Kind:        domain.DirwatchTypeTrunkRecorder,
		Mask:        "#SYS #TG",
		Order:       2,
		SiteID:      7,
		SystemID:    11,
		TalkgroupID: 13,
	}}

	if err := NewDirwatches(db, DialectSqlite).SaveDirwatches(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	// The legacy server's read statement, verbatim, scanning into the types its Dirwatch fields
	// have: uint64, uint, bool, and string.
	var (
		id          uint64
		delay       uint
		deleteAfter bool
		directory   string
		disabled    bool
		extension   string
		frequency   uint
		mask        string
		order       uint
		siteID      uint64
		systemID    uint64
		talkgroupID uint64
		kind        string
	)

	const legacyRead = `SELECT "dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type" FROM "dirwatches"`

	if err := db.QueryRow(legacyRead).Scan(
		&id, &delay, &deleteAfter, &directory, &disabled, &extension,
		&frequency, &mask, &order, &siteID, &systemID, &talkgroupID, &kind,
	); err != nil {
		t.Fatalf("the legacy server's own read cannot read the row server-ng wrote: %v", err)
	}

	want := submitted[0]

	if delay != want.Delay || deleteAfter != want.DeleteAfter || directory != want.Directory || disabled != want.Disabled {
		t.Errorf("the legacy read got delay=%d deleteAfter=%v directory=%q disabled=%v, want %d %v %q %v",
			delay, deleteAfter, directory, disabled, want.Delay, want.DeleteAfter, want.Directory, want.Disabled)
	}

	if extension != want.Extension || frequency != want.Frequency || mask != want.Mask || order != want.Order {
		t.Errorf("the legacy read got extension=%q frequency=%d mask=%q order=%d, want %q %d %q %d",
			extension, frequency, mask, order, want.Extension, want.Frequency, want.Mask, want.Order)
	}

	if siteID != want.SiteID || systemID != want.SystemID || talkgroupID != want.TalkgroupID || kind != want.Kind {
		t.Errorf("the legacy read got siteId=%d systemId=%d talkgroupId=%d type=%q, want %d %d %d %q",
			siteID, systemID, talkgroupID, kind, want.SiteID, want.SystemID, want.TalkgroupID, want.Kind)
	}

	// The legacy server's own rows hold the two flags as integers, which is what a `true`/`false`
	// token becomes. A row server-ng writes has to land the same way, or the two servers disagree
	// about what the column holds.
	var deleteAfterType, disabledType string

	if err := db.QueryRow(`SELECT typeof("deleteAfter"), typeof("disabled") FROM "dirwatches"`).Scan(&deleteAfterType, &disabledType); err != nil {
		t.Fatalf("typeof: %v", err)
	}

	if deleteAfterType != "integer" || disabledType != "integer" {
		t.Errorf("the flags are stored as %s and %s, want integer as the legacy server stores them", deleteAfterType, disabledType)
	}
}

// TestSaveDirwatchesStoresAWatchForASystemThatDoesNotExist pins that the watched system, talkgroup,
// and site are plain integers on this table with no foreign key: a dirwatch may name a system that
// is not configured, and storing it is not a failure.
func TestSaveDirwatchesStoresAWatchForASystemThatDoesNotExist(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	if got := countRows(t, db, "systems"); got != 0 {
		t.Fatalf("the fresh database has %d systems, want none: the watch has to name a system that does not exist", got)
	}

	watch := domain.Dirwatch{
		Directory:   "/spool/unresolved",
		Kind:        domain.DirwatchTypeDefault,
		Order:       1,
		SiteID:      99991,
		SystemID:    99992,
		TalkgroupID: 99993,
	}

	if err := NewDirwatches(db, DialectSqlite).SaveDirwatches(context.Background(), domain.Dirwatches{watch}); err != nil {
		t.Fatalf("SaveDirwatches for a system that does not exist: %v", err)
	}

	stored, err := NewDirwatches(db, DialectSqlite).AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if len(stored) != 1 {
		t.Fatalf("the table stores %d watches, want 1", len(stored))
	}

	if stored[0].SiteID != 99991 || stored[0].SystemID != 99992 || stored[0].TalkgroupID != 99993 {
		t.Errorf("the watch reads back as siteId=%d systemId=%d talkgroupId=%d, want the references it was configured with",
			stored[0].SiteID, stored[0].SystemID, stored[0].TalkgroupID)
	}
}

// TestAllDirwatchesReturnsTheWatchesInTheirDisplayOrder pins the ordering the legacy server applies
// to the list it read.
func TestAllDirwatchesReturnsTheWatchesInTheirDisplayOrder(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	// Written in an order that is not the display order, so the sort has to be what answers.
	submitted := domain.Dirwatches{
		{Directory: "/spool/third", Kind: domain.DirwatchTypeDefault, Order: 30},
		{Directory: "/spool/first", Kind: domain.DirwatchTypeDefault, Order: 10},
		{Directory: "/spool/second", Kind: domain.DirwatchTypeDefault, Order: 20},
	}

	if err := NewDirwatches(db, DialectSqlite).SaveDirwatches(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	stored, err := NewDirwatches(db, DialectSqlite).AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if len(stored) != 3 {
		t.Fatalf("the table stores %d watches, want 3", len(stored))
	}

	for i, want := range []uint{10, 20, 30} {
		if stored[i].Order != want {
			t.Errorf("watch %d has order %d, want %d", i, stored[i].Order, want)
		}
	}
}

// TestAllDirwatchesReportsTheConfigurationInEffectAndNotTheStoredRows is the §9 rule: the watches
// are read from the database once and then held, because what the administrator is shown and what
// the watchers run on is what the server is running with.
func TestAllDirwatchesReportsTheConfigurationInEffectAndNotTheStoredRows(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	repo := NewDirwatches(db, DialectSqlite)

	submitted := domain.Dirwatches{{Directory: "/spool/live", Kind: domain.DirwatchTypeDefault, Order: 1}}

	if err := repo.SaveDirwatches(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	// Something else empties the table underneath the running server.
	if _, err := db.Exec(`DELETE FROM "dirwatches"`); err != nil {
		t.Fatalf("DELETE: %v", err)
	}

	live, err := repo.AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if len(live) != 1 || live[0].Directory != "/spool/live" {
		t.Errorf("AllDirwatches returned %v, want the watch the running server is configured with", live)
	}

	// A server that starts over reads the table, which is what the running configuration came from.
	restarted, err := NewDirwatches(db, DialectSqlite).AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches after a restart: %v", err)
	}

	if len(restarted) != 0 {
		t.Errorf("a restarted server reads %v, want nothing: the table is empty", restarted)
	}
}

// TestSaveDirwatchesLeavesTheSubmittedWatchesInEffectWhenTheSaveFails is Q13: the submitted
// configuration is in effect before anything is stored, so a section that could not be persisted is
// still what the server runs on.
func TestSaveDirwatchesLeavesTheSubmittedWatchesInEffectWhenTheSaveFails(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	repo := NewDirwatches(db, DialectSqlite)

	stored := domain.Dirwatches{{Directory: "/spool/stored", Kind: domain.DirwatchTypeDefault, Order: 1}}

	if err := repo.SaveDirwatches(context.Background(), stored); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	unreachable, cancel := context.WithCancel(context.Background())
	cancel()

	submitted := domain.Dirwatches{{Directory: "/spool/submitted", Kind: domain.DirwatchTypeDefault, Order: 2}}

	err := repo.SaveDirwatches(unreachable, submitted)
	if err == nil {
		t.Fatal("a save against a store that cannot be reached is a failure the caller is told about")
	}

	if !strings.HasPrefix(err.Error(), "dirwatches.write:") {
		t.Errorf("error = %v, want the legacy dirwatches.write shape", err)
	}

	live, err := repo.AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if len(live) != 1 || live[0].Directory != "/spool/submitted" {
		t.Errorf("the server runs with %v, want the submitted watch: the legacy server replaces its list before it writes", live)
	}

	if got := countRows(t, db, "dirwatches"); got != 1 {
		t.Fatalf("the table stores %d watches, want the one that was stored before the failed save", got)
	}

	var directory string

	if err := db.QueryRow(`SELECT "directory" FROM "dirwatches"`).Scan(&directory); err != nil {
		t.Fatalf("reading the stored directory: %v", err)
	}

	if directory != "/spool/stored" {
		t.Errorf("the table stores %q, want the watch that was stored before the failed save", directory)
	}
}

// TestSaveDirwatchesRemovesTheWatchesTheSubmittedListNoLongerNames is the reconciling half of the
// save: a whole-collection save that drops a watch drops it from the table too.
func TestSaveDirwatchesRemovesTheWatchesTheSubmittedListNoLongerNames(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	repo := NewDirwatches(db, DialectSqlite)

	if err := repo.SaveDirwatches(context.Background(), domain.Dirwatches{
		{Directory: "/spool/keep", Kind: domain.DirwatchTypeDefault, Order: 1},
		{Directory: "/spool/drop", Kind: domain.DirwatchTypeDefault, Order: 2},
	}); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	kept, err := repo.AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if len(kept) != 2 {
		t.Fatalf("the table stores %d watches, want 2 before the reconciling save", len(kept))
	}

	remaining := domain.Dirwatches{}

	for _, watch := range kept {
		if watch.Directory == "/spool/keep" {
			remaining = append(remaining, watch)
		}
	}

	if len(remaining) != 1 {
		t.Fatalf("the stored watches are %v, want one of them named /spool/keep", kept)
	}

	if err := repo.SaveDirwatches(context.Background(), remaining); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	if got := countRows(t, db, "dirwatches"); got != 1 {
		t.Fatalf("the table stores %d watches after the save, want 1: the dropped watch is removed", got)
	}

	stored, err := NewDirwatches(db, DialectSqlite).AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if len(stored) != 1 || stored[0].Directory != "/spool/keep" {
		t.Errorf("the table stores %v, want only the watch the submitted list named", stored)
	}
}

// TestSaveDirwatchesRemovesAStoredWatchEvenWhenTheSubmittedListAddsANewOne pins the repaired
// reconcile recorded as Q33 in decision-preserved-quirks.md.
//
// `server/dirwatch.go` `Dirwatches.Write` decides whether a stored watch is still named by scanning
// the submitted list and stopping at the first entry whose identifier is zero or equal — so one new,
// identifier-less watch keeps every stored watch, and a save that adds a watch and drops another
// leaves the dropped one stored until a later save that adds nothing. The same shape is in
// Systems.Write, Groups.Write, Tags.Write, Accesses.Write, and Apikeys.Write, and server-ng repairs
// all of them.
func TestSaveDirwatchesRemovesAStoredWatchEvenWhenTheSubmittedListAddsANewOne(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	repo := NewDirwatches(db, DialectSqlite)

	if err := repo.SaveDirwatches(context.Background(), domain.Dirwatches{
		{Directory: "/spool/keep", Kind: domain.DirwatchTypeDefault, Order: 1},
		{Directory: "/spool/drop", Kind: domain.DirwatchTypeDefault, Order: 2},
	}); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	stored, err := repo.AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	// The submitted list names the watch it keeps by identifier and adds one the table has never
	// seen. The watch it drops is simply absent.
	submitted := domain.Dirwatches{
		{ID: stored[0].ID, Directory: stored[0].Directory, Kind: stored[0].Kind, Order: stored[0].Order},
		{Directory: "/spool/new", Kind: domain.DirwatchTypeDefault, Order: 3},
	}

	if err := repo.SaveDirwatches(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	if got := countRows(t, db, "dirwatches"); got != 2 {
		t.Fatalf("the table stores %d watches, want 2: the watch the list dropped must go even though the list also adds one", got)
	}

	live, err := repo.AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if len(live) != 2 {
		t.Errorf("the server watches %d directories, want 2: the watch the list dropped is not watched", len(live))
	}
}

// TestSaveDirwatchesUpdatesTheWatchThatCarriesTheStoredIdentifier pins the update half of the save:
// a submitted watch whose identifier is already stored replaces that row rather than adding one.
func TestSaveDirwatchesUpdatesTheWatchThatCarriesTheStoredIdentifier(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	repo := NewDirwatches(db, DialectSqlite)

	if err := repo.SaveDirwatches(context.Background(), domain.Dirwatches{
		{Directory: "/spool/edited", Delay: 2000, Kind: domain.DirwatchTypeDefault, Order: 1},
	}); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	stored, err := repo.AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	edited := stored[0]
	edited.Delay = 9000
	edited.Kind = domain.DirwatchTypeSdrTrunk
	edited.Extension = "mp3"

	if err := repo.SaveDirwatches(context.Background(), domain.Dirwatches{edited}); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	if got := countRows(t, db, "dirwatches"); got != 1 {
		t.Fatalf("the table stores %d watches after the edit, want 1: the edit replaced the row", got)
	}

	after, err := NewDirwatches(db, DialectSqlite).AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if after[0].ID != edited.ID {
		t.Errorf("the edited watch has identifier %d, want the one it was stored under (%d)", after[0].ID, edited.ID)
	}

	if after[0].Delay != 9000 || after[0].Kind != domain.DirwatchTypeSdrTrunk || after[0].Extension != "mp3" {
		t.Errorf("the edited watch reads back as delay=%d type=%q extension=%q, want 9000 %q %q",
			after[0].Delay, after[0].Kind, after[0].Extension, domain.DirwatchTypeSdrTrunk, "mp3")
	}
}

// TestSaveDirwatchesGivesANewWatchTheIdentifierTheTableAssignedIt pins why a saved section is read
// back: the watch the administrator added has no identifier until the table gives it one.
func TestSaveDirwatchesGivesANewWatchTheIdentifierTheTableAssignedIt(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	repo := NewDirwatches(db, DialectSqlite)

	if err := repo.SaveDirwatches(context.Background(), domain.Dirwatches{
		{Directory: "/spool/new", Kind: domain.DirwatchTypeDefault, Order: 1},
	}); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	live, err := repo.AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if live[0].ID == 0 {
		t.Errorf("the saved watch has no identifier, want the one the table assigned: a save that persisted is read back")
	}
}

// TestSaveDirwatchesStoresADirectoryContainingAQuote is the REQ-007 half of this adapter: the legacy
// `Dirwatches.Write` interpolates the directory into the statement with no escaping at all, so a
// directory naming a quote breaks the statement and the whole save fails. The values go in as
// parameters here.
func TestSaveDirwatchesStoresADirectoryContainingAQuote(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	const directory = `/var/spool/calls/o'brien`

	submitted := domain.Dirwatches{{Directory: directory, Kind: domain.DirwatchTypeDefault, Mask: "#SYS #TG", Order: 1}}

	if err := NewDirwatches(db, DialectSqlite).SaveDirwatches(context.Background(), submitted); err != nil {
		t.Fatalf("SaveDirwatches for a directory containing a quote: %v", err)
	}

	stored, err := NewDirwatches(db, DialectSqlite).AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if len(stored) != 1 || stored[0].Directory != directory {
		t.Errorf("the table stores %v, want the directory exactly as it was submitted", stored)
	}
}

// TestAllDirwatchesReportsAnUnreachableStore keeps the failure the shape the server log has always
// had.
func TestAllDirwatchesReportsAnUnreachableStore(t *testing.T) {
	db := openDirwatchTestDatabase(t)

	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := NewDirwatches(db, DialectSqlite).AllDirwatches(context.Background())
	if err == nil {
		t.Fatal("a store that cannot be reached is a failure the caller is told about")
	}

	if !strings.HasPrefix(err.Error(), "dirwatches.read:") {
		t.Errorf("error = %v, want the legacy dirwatches.read shape", err)
	}

	if !strings.Contains(err.Error(), `FROM "dirwatches"`) {
		t.Errorf("error = %v, want it to name the statement that failed", err)
	}
}

// TestSaveDirwatchesOnADatabaseTheLegacyServerWroteKeepsTheStoredWatch is the direction a
// deployment actually takes over: server-ng saves the configuration it just read out of a
// legacy-written database and the legacy-written row is still there afterwards.
func TestSaveDirwatchesOnADatabaseTheLegacyServerWroteKeepsTheStoredWatch(t *testing.T) {
	db := openLegacyDatabase(t)

	repo := NewDirwatches(db, DialectSqlite)

	loaded, err := repo.AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("the legacy-written database stores %d watches, want 1", len(loaded))
	}

	if err := repo.SaveDirwatches(context.Background(), loaded); err != nil {
		t.Fatalf("SaveDirwatches: %v", err)
	}

	if got := countRows(t, db, "dirwatches"); got != 1 {
		t.Fatalf("the table stores %d watches after re-saving what was read from it, want 1", got)
	}

	after, err := NewDirwatches(db, DialectSqlite).AllDirwatches(context.Background())
	if err != nil {
		t.Fatalf("AllDirwatches: %v", err)
	}

	if after[0].ID != loaded[0].ID || after[0].Directory != loaded[0].Directory || after[0].Kind != loaded[0].Kind {
		t.Errorf("the legacy watch is stored as %v, want it unchanged: %v", after[0], loaded[0])
	}
}
