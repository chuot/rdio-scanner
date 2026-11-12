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
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The deployment the clearing is tested against is the recorded migrated fixture — the tables and
// rows a legacy server's finished migration pass leaves behind, damaged unit row 303 among them —
// taken through this server's own startup pass over those statements and then given the live tables
// and rows the recorded fixture does not carry, so the clearing is asked to leave every kind of
// stored row alone and not only the kinds the fixture happens to hold. The startup pass is part of
// the fixture because no deployment is ever found in the mid-pass state the recorded statements
// spell: it is the pass that gives the calls table its recording-length column and its index set,
// and it changes none of the recorded rows.
//
// The added unit rows are each a survivor some looser rule would wrongly remove: a described range,
// a number written with padding, a number with a trailing space, and a unit an operator named in a
// system that no longer exists.

func openDamagedDeploymentDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db := buildRecordedDatabase(t, recordedDamagedStatements(t, "migrated"))

	migrateDatabase(t, db)

	for _, statement := range damagedDeploymentRows {
		mustExec(t, db, statement)
	}

	// Unit 308 hangs off a system that is not in the database, which is a shape a migrated
	// deployment really holds and the recorded statements cannot write: the stored foreign key
	// cascades a system's units away with it. The link is made on one connection with the
	// enforcement off, and that connection closes with the enforcement on again, leaving the pool
	// holding it exactly as the server opens it.
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("acquiring a connection to write an unlinked unit: %v", err)
	}

	defer conn.Close()

	for _, statement := range []string{
		`PRAGMA foreign_keys = OFF`,
		`INSERT INTO "units" VALUES(308,'Tug 3',8,999,47999,0,0)`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := conn.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	return db
}

var damagedDeploymentRows = []string{
	// A described range, a padded number, and a number with a trailing space: each reads like the
	// damaged row 303 on a rule that only looks partway at the label, and none is damage.
	`INSERT INTO "units" VALUES(304,'Fleet 47',5,101,47003,47003,47999);`,
	`INSERT INTO "units" VALUES(305,'047003',1,102,47003,0,0);`,
	`INSERT INTO "units" VALUES(306,'47003 ',7,102,47003,0,0);`,

	// A second damaged row, at the display order a deployment sets rather than the 0 the ingest
	// writes, and carrying the kind of number a group reference looks like.
	`INSERT INTO "units" VALUES(307,'1514000',6,101,1514000,0,0);`,

	`INSERT INTO "groups" VALUES(401,'','Dispatch group','',1);`,
	`INSERT INTO "talkgroupGroups" VALUES(501,401,201);`,
	`INSERT INTO "sites" VALUES(901,'Site A',1,901,101);`,
	`INSERT INTO "accesses" VALUES(1101,'code-1',0,'access one',0,1,'[10001]');`,
	`INSERT INTO "apikeys" VALUES(1201,0,'key one','apikey-0123456789abcdef',1,'[10001]');`,
	`INSERT INTO "downstreams" VALUES(1301,'apikey-0123456789abcdef',0,1,'[{"id":10001}]','http://127.0.0.1:3999/api/call-upload');`,
	`INSERT INTO "logs" VALUES(1401,'info','call','recorded call 701',1749988900000);`,
	`INSERT INTO "options" VALUES(1501,'timezone','UTC');`,
	`INSERT INTO "dirwatches" VALUES(1601,0,0,'/var/spool/dsdplus',0,'wav',1514000000,'0644',1,901,101,1514000,'dsdPlus');`,
	`INSERT INTO "callFrequencies" VALUES(1701,701,-80.0,0,1514000000,0.0,0);`,
	`INSERT INTO "callPatches" VALUES(1702,701,202);`,
	`INSERT INTO "delayed" VALUES(1703,701,1749988900000);`,
}

// damagedDeploymentRowCounts is what every table of the deployment holds, and the number each one
// holds after a clearing that touched the damaged unit rows and nothing else.
var damagedDeploymentRowCounts = []struct {
	table string
	rows  int
}{
	{table: "units", rows: 6},
	{table: "systems", rows: 2},
	{table: "tags", rows: 1},
	{table: "talkgroups", rows: 3},
	{table: "groups", rows: 1},
	{table: "talkgroupGroups", rows: 1},
	{table: "sites", rows: 1},
	{table: "accesses", rows: 1},
	{table: "apikeys", rows: 1},
	{table: "downstreams", rows: 1},
	{table: "logs", rows: 1},
	{table: "options", rows: 1},
	{table: "dirwatches", rows: 1},
	{table: "calls", rows: 3},
	{table: "callFrequencies", rows: 1},
	{table: "callPatches", rows: 1},
	{table: "callUnits", rows: 2},
	{table: "delayed", rows: 1},
}

// damagedDeploymentRemovedUnits are the rows the rule names, held apart from the survivors so that
// the file running the same deployment on a live backend can compare against the same set. Each one
// holds a radio reference, a label written as that number in decimal, and no range, and each one is
// held here in the shape the clearing reports it: the identifier, the label, the reference, the range,
// and the system it belongs to, which are the values the clearing reads and the rule uses. Display
// order is not among them because the rule does not read it, and 307 stands in the deployment at order
// 6 all the same; the test asks after the stored row as well, so the order a damaged row carries is
// shown to be no shield.
var damagedDeploymentRemovedUnits = []domain.Unit{
	{ID: 303, Label: "6789", SystemID: 101, UnitRef: 6789},
	{ID: 307, Label: "1514000", SystemID: 101, UnitRef: 1514000},
}

// TestClearMigrationDamageRemovesTheDamagedUnitRowsAndNothingElse names the removed set by
// identifier and by the stored values that make each row damage, then asks after every other kind
// of row the deployment holds.
func TestClearMigrationDamageRemovesTheDamagedUnitRowsAndNothingElse(t *testing.T) {
	db := openDamagedDeploymentDatabase(t)

	report, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("ClearMigrationDamage: %v", err)
	}

	wantRemoved := damagedDeploymentRemovedUnits

	if len(report.Units) != len(wantRemoved) {
		t.Fatalf("the clearing removed %d unit rows, want %d: %+v", len(report.Units), len(wantRemoved), report.Units)
	}

	for _, want := range wantRemoved {
		got, ok := reportedUnit(report.Units, want.ID)
		if !ok {
			t.Errorf("the clearing did not report unit %d (%q, unitRef %d), want it removed: %+v",
				want.ID, want.Label, want.UnitRef, report.Units)

			continue
		}

		if got != want {
			t.Errorf("the clearing removed unit %+v, want %+v: the report names each removed row by the stored values that made it damage", got, want)
		}
	}

	// Each removed row is gone from the table as well as reported gone. Unit 307 stands at display
	// order 6, which the clearing neither reads nor reports: the order a damaged row carries is no
	// shield, and a row the run named but left standing would be a row it only described.
	for _, want := range wantRemoved {
		stored, err := readUnitRow(context.Background(), db, want.ID)
		if err != sql.ErrNoRows {
			t.Errorf("unit %d (%q) reads %+v answering %v after the clearing, want no stored row of it left", want.ID, want.Label, stored, err)
		}
	}

	for _, survivor := range damagedDeploymentSurvivingUnits {
		stored, err := readUnitRow(context.Background(), db, survivor.ID)
		if err != nil {
			t.Errorf("unit %d (%q) is gone, want it kept: %v", survivor.ID, survivor.Label, err)

			continue
		}

		if stored != survivor {
			t.Errorf("unit %d reads %+v after the clearing, want it stored exactly as it was: %+v", survivor.ID, stored, survivor)
		}

		if _, ok := reportedUnit(report.Units, survivor.ID); ok {
			t.Errorf("the clearing reported unit %d (%q) as removed, want it kept", survivor.ID, survivor.Label)
		}
	}

	for _, kept := range damagedDeploymentRowCounts {
		if got := countRows(t, db, kept.table); got != kept.rows {
			t.Errorf("%s holds %d rows after the clearing, want the %d it held before it", kept.table, got, kept.rows)
		}
	}
}

// damagedDeploymentSurvivingUnits are the rows the fixture holds that the rule must each refuse:
// the two a deployment named without a radio reference, a described range, a padded number, a
// number with a trailing space, and a named unit of a system that is no longer in the database.
var damagedDeploymentSurvivingUnits = []domain.Unit{
	{ID: 301, Label: "Engine 12", Order: 1, SystemID: 101},
	{ID: 302, Label: "Car 4", Order: 2, SystemID: 101},
	{ID: 304, Label: "Fleet 47", Order: 5, SystemID: 101, UnitRef: 47003, UnitFrom: 47003, UnitTo: 47999},
	{ID: 305, Label: "047003", Order: 1, SystemID: 102, UnitRef: 47003},
	{ID: 306, Label: "47003 ", Order: 7, SystemID: 102, UnitRef: 47003},
	{ID: 308, Label: "Tug 3", Order: 8, SystemID: 999, UnitRef: 47999},
}

// TestClearMigrationDamageLeavesTheSurvivingUnitsInTheirOwnOrder reads the clearing through the
// catalog every client of the server reads: the units that stay are the units the deployment
// configured, in the order the deployment ordered them, the gaps left by the removed rows aside.
func TestClearMigrationDamageLeavesTheSurvivingUnitsInTheirOwnOrder(t *testing.T) {
	db := openDamagedDeploymentDatabase(t)

	if _, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background()); err != nil {
		t.Fatalf("ClearMigrationDamage: %v", err)
	}

	systems, err := NewSystems(db, DialectSqlite).AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems after the clearing: %v", err)
	}

	if len(systems) != 2 {
		t.Fatalf("AllSystems answered %d systems, want the 2 the deployment holds", len(systems))
	}

	for _, want := range []struct {
		systemID uint64
		unitIDs  []uint64
	}{
		{systemID: 101, unitIDs: []uint64{301, 302, 304}},
		{systemID: 102, unitIDs: []uint64{305, 306}},
	} {
		system, ok := systems.ByID(want.systemID)
		if !ok {
			t.Fatalf("AllSystems answered no system %d", want.systemID)
		}

		if len(system.Units) != len(want.unitIDs) {
			t.Fatalf("system %d holds %d units after the clearing, want %d: %+v",
				want.systemID, len(system.Units), len(want.unitIDs), system.Units)
		}

		for i, id := range want.unitIDs {
			if system.Units[i].ID != id {
				t.Errorf("system %d holds unit %d at position %d, want unit %d: %+v",
					want.systemID, system.Units[i].ID, i, id, system.Units)
			}
		}
	}
}

// TestClearMigrationDamageIsRepeatable is the guarantee a mistaken second run rests on: the rule
// reads what the first run left, and what it left is nothing it names.
func TestClearMigrationDamageIsRepeatable(t *testing.T) {
	db := openDamagedDeploymentDatabase(t)

	first, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("the first ClearMigrationDamage: %v", err)
	}

	if len(first.Units) == 0 {
		t.Fatalf("the first clearing removed no unit row, want the damaged ones")
	}

	second, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("the second ClearMigrationDamage: %v", err)
	}

	if len(second.Units) != 0 {
		t.Errorf("the second clearing removed %d unit rows, want none: %+v", len(second.Units), second.Units)
	}

	if got := countRows(t, db, "units"); got != 6 {
		t.Errorf("%d unit rows are stored after the second clearing, want the 6 survivors", got)
	}
}

// TestClearMigrationDamageLeavesARecordedCallExactlyAsItWas is the promise the whole run rests on:
// a call the deployment recorded keeps its row, its children, its audio bytes, and the payload a
// client reads for it, byte for byte, whatever the clearing removed around it.
func TestClearMigrationDamageLeavesARecordedCallExactlyAsItWas(t *testing.T) {
	db := openDamagedDeploymentDatabase(t)

	const callID = 701

	tables := []string{"calls", "callUnits", "callFrequencies", "callPatches", "delayed"}

	before := map[string][][]string{}

	for _, table := range tables {
		before[table] = rowsOf(t, db, table, "*")
	}

	callBefore, err := NewCalls(db, DialectSqlite).GetCall(context.Background(), callID)
	if err != nil {
		t.Fatalf("GetCall before the clearing: %v", err)
	}

	report, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("ClearMigrationDamage: %v", err)
	}

	for _, table := range tables {
		after := rowsOf(t, db, table, "*")

		if diff := rowDiff(before[table], after); diff != "" {
			t.Errorf("%s changed under the clearing: %s", table, diff)
		}
	}

	callAfter, err := NewCalls(db, DialectSqlite).GetCall(context.Background(), callID)
	if err != nil {
		t.Fatalf("GetCall after the clearing: %v", err)
	}

	if !bytes.Equal(callBefore.Audio, callAfter.Audio) {
		t.Errorf("the call's audio reads %q after the clearing, want the bytes it held before it: %q", callAfter.Audio, callBefore.Audio)
	}

	if callAfter.ID != callBefore.ID ||
		callAfter.AudioFilename != callBefore.AudioFilename ||
		callAfter.AudioMime != callBefore.AudioMime ||
		callAfter.AudioLength != callBefore.AudioLength ||
		callAfter.SiteRef != callBefore.SiteRef ||
		callAfter.Timestamp != callBefore.Timestamp ||
		callAfter.Delayed != callBefore.Delayed {
		t.Errorf("the call payload reads id=%d file=%q mime=%q length=%v site=%d at=%v delayed=%v, want the payload it had before the clearing: id=%d file=%q mime=%q length=%v site=%d at=%v delayed=%v",
			callAfter.ID, callAfter.AudioFilename, callAfter.AudioMime, callAfter.AudioLength, callAfter.SiteRef,
			callAfter.Timestamp, callAfter.Delayed,
			callBefore.ID, callBefore.AudioFilename, callBefore.AudioMime, callBefore.AudioLength, callBefore.SiteRef,
			callBefore.Timestamp, callBefore.Delayed)
	}

	if callAfter.System.ID != callBefore.System.ID || callAfter.System.Label != callBefore.System.Label ||
		callAfter.Talkgroup.ID != callBefore.Talkgroup.ID || callAfter.Talkgroup.Label != callBefore.Talkgroup.Label {
		t.Errorf("the call payload names system %d %q and talkgroup %d %q, want system %d %q and talkgroup %d %q",
			callAfter.System.ID, callAfter.System.Label, callAfter.Talkgroup.ID, callAfter.Talkgroup.Label,
			callBefore.System.ID, callBefore.System.Label, callBefore.Talkgroup.ID, callBefore.Talkgroup.Label)
	}

	if !reflect.DeepEqual(callAfter.Units, callBefore.Units) {
		t.Errorf("the call's units read %+v after the clearing, want what they read before it: %+v", callAfter.Units, callBefore.Units)
	}

	if !reflect.DeepEqual(callAfter.Frequencies, callBefore.Frequencies) {
		t.Errorf("the call's frequencies read %+v after the clearing, want what they read before it: %+v", callAfter.Frequencies, callBefore.Frequencies)
	}

	if !reflect.DeepEqual(callAfter.PatchTalkgroupRefs, callBefore.PatchTalkgroupRefs) {
		t.Errorf("the call's patch references read %v after the clearing, want what they read before it: %v",
			callAfter.PatchTalkgroupRefs, callBefore.PatchTalkgroupRefs)
	}

	// The unit reference a call carries lives in callUnits, not in a unit label: the call heard
	// unit 12345, and no removal of a unit row reaches into the recording.
	if len(callBefore.Units) == 0 {
		t.Fatalf("the fixture's call holds no unit, want the recorded unit reference")
	}

	if callBefore.Units[0].UnitRef != 12345 {
		t.Fatalf("the fixture's call holds unitRef %d, want 12345", callBefore.Units[0].UnitRef)
	}

	for _, unit := range report.Units {
		if unit.UnitRef == callBefore.Units[0].UnitRef {
			t.Errorf("the clearing removed unit %+v, whose reference the recorded call heard: the call's units belong to the call", unit)
		}
	}
}

// rowDiff reports the rows a snapshot lost or gained, and answers empty when the two snapshots hold
// the same rows in the same order.
func rowDiff(before [][]string, after [][]string) string {
	out := strings.Builder{}

	key := func(row []string) string { return strings.Join(row, "|") }

	seen := map[string]int{}

	for _, row := range before {
		seen[key(row)]++
	}

	for _, row := range after {
		seen[key(row)]--
	}

	for _, row := range before {
		if n := seen[key(row)]; n < 0 {
			fmt.Fprintf(&out, "lost %s\n", key(row))

			seen[key(row)] = 0
		}
	}

	for _, row := range after {
		if n := seen[key(row)]; n > 0 {
			fmt.Fprintf(&out, "gained %s\n", key(row))

			seen[key(row)] = 0
		}
	}

	return out.String()
}

func reportedUnit(units []domain.Unit, id uint64) (domain.Unit, bool) {
	for _, unit := range units {
		if unit.ID == id {
			return unit, true
		}
	}

	return domain.Unit{}, false
}

// readUnitRow reads one unit row straight out of the database, so that a survivor is compared with
// what is stored rather than with what a repository chose to answer.
func readUnitRow(ctx context.Context, db *sql.DB, id uint64) (domain.Unit, error) {
	var (
		label    string
		order    int64
		systemID int64
		unitRef  int64
		unitFrom int64
		unitTo   int64
	)

	query := `SELECT "label", "order", "systemId", "unitRef", "unitFrom", "unitTo" FROM "units" WHERE "unitId" = ?`

	if err := db.QueryRowContext(ctx, query, id).
		Scan(&label, &order, &systemID, &unitRef, &unitFrom, &unitTo); err != nil {
		return domain.Unit{}, err
	}

	return domain.Unit{
		ID:       id,
		Label:    label,
		Order:    uint(order),
		SystemID: uint64(systemID),
		UnitRef:  uint(unitRef),
		UnitFrom: uint(unitFrom),
		UnitTo:   uint(unitTo),
	}, nil
}

// archiveFixtureRow is one row a deployment's migration archive holds, with the values the clearing
// is asked to report — the table it came from, its identifier there, and the stored reason — beside
// the contents column the report must never carry.
type archiveFixtureRow struct {
	Table    string
	RowID    uint64
	Reason   string
	Contents string
}

// damagedDeploymentArchiveRows are the kinds of row the measured deployment's archive holds, taken
// from it: the migration's own bookkeeping, a row of a system the deployment does not have, and a
// configuration row the migration could not carry. Beside them sits a row whose stored values hold a
// call's encoded audio, which is the one kind the clearing has to name without repeating.
var damagedDeploymentArchiveRows = []archiveFixtureRow{
	{
		Table:    "rdioScannerMeta",
		RowID:    7,
		Reason:   "this server has no migration bookkeeping to carry it into",
		Contents: `{"name":"migrated-units"}`,
	},
	{
		Table:    "rdioScannerTalkgroups",
		RowID:    115,
		Reason:   "its system 8081 is not in this database",
		Contents: `{"_id":115,"id":115,"label":"Marine","systemId":8081}`,
	},
	{
		Table:    "rdioScannerConfigs",
		RowID:    3,
		Reason:   `it holds values this server cannot carry: "afsSystems" (v8 has no option of that name), "tagsToggle" (v8 has no option of that name)`,
		Contents: `{"_id":3,"key":"tagsToggle","val":"true"}`,
	},
	{
		Table:    "rdioScannerCalls",
		RowID:    706,
		Reason:   "it holds no audio",
		Contents: `{"audio":"AQIDBAUFBgc=","id":706,"system":10001}`,
	},
}

// openDamagedDeploymentDatabaseWithArchive is the same deployment with the archive the migration
// filled still standing in it, so the clearing is asked about a table the deployment really holds
// rather than one the fixture invented beside an empty database.
func openDamagedDeploymentDatabaseWithArchive(t *testing.T) *sql.DB {
	t.Helper()

	db := openDamagedDeploymentDatabase(t)

	createMigrationArchive(t, db)

	for _, row := range damagedDeploymentArchiveRows {
		statement := fmt.Sprintf(
			`INSERT INTO "migrationArchive" ("tableName","rowId","reason","contents","archivedAt") VALUES('%s',%d,'%s','%s',%d);`,
			row.Table, row.RowID, row.Reason, row.Contents, migrationInstant.UnixMilli(),
		)

		mustExec(t, db, statement)
	}

	return db
}

// TestClearMigrationDropsTheMigrationArchiveAndNamesEveryRowInIt is the destructive half read
// against the archive's own contents: the table goes with every row in it, whatever kind of row it
// is, and each one is named by where it came from, its identifier there, and the reason stored with
// it. The values that only the contents column hold never ride into the report.
func TestClearMigrationDropsTheMigrationArchiveAndNamesEveryRowInIt(t *testing.T) {
	db := openDamagedDeploymentDatabaseWithArchive(t)

	before := readRows(t, db, `SELECT "tableName","rowId","reason","contents","archivedAt" FROM "migrationArchive" ORDER BY "tableName","rowId"`)

	if len(before) != len(damagedDeploymentArchiveRows) {
		t.Fatalf("the fixture's archive holds %d rows, want the %d it was given: %v", len(before), len(damagedDeploymentArchiveRows), before)
	}

	report, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("ClearMigrationDamage: %v", err)
	}

	for _, name := range readTableNames(t, db) {
		if name == "migrationArchive" {
			t.Fatalf("the migration archive is still among the deployment's tables after the clearing: %v", readTableNames(t, db))
		}
	}

	if !report.DroppedArchive {
		t.Errorf("the clearing did not report dropping the migration archive it dropped")
	}

	if len(report.ArchiveRows) != len(damagedDeploymentArchiveRows) {
		t.Fatalf("the clearing reported %d archive rows, want one for each of the %d rows in the table: %+v",
			len(report.ArchiveRows), len(damagedDeploymentArchiveRows), report.ArchiveRows)
	}

	for _, want := range damagedDeploymentArchiveRows {
		got, ok := reportedArchiveRow(report.ArchiveRows, want.Table, want.RowID)
		if !ok {
			t.Errorf("the clearing did not report the archive row %s %d, want it named before the table went: %+v",
				want.Table, want.RowID, report.ArchiveRows)

			continue
		}

		if got.Table != want.Table || got.RowID != want.RowID || got.Reason != want.Reason {
			t.Errorf("the clearing reported the archive row %+v, want %s %d held for %q",
				got, want.Table, want.RowID, want.Reason)
		}

		if !got.ArchivedAt.UTC().Equal(migrationInstant) {
			t.Errorf("the clearing reported the archive row %s %d archived at %v, want the stored moment %v",
				want.Table, want.RowID, got.ArchivedAt, migrationInstant)
		}
	}

	// The rendering is what the operator and the log entry are built from: an encoded call recording
	// reaching it means the report carries the archive's contents column, which is the one column
	// this run is asked to name a row without.
	rendered := fmt.Sprintf("%+v", report.ArchiveRows)

	for _, want := range damagedDeploymentArchiveRows {
		if strings.Contains(rendered, want.Contents) {
			t.Errorf("the reported archive rows carry the contents of the row %s %d (%s): %s",
				want.Table, want.RowID, want.Contents, rendered)
		}
	}

	if strings.Contains(rendered, "contents") {
		t.Errorf("the reported archive rows name a contents field, want the row named by its table, identifier, and stored reason: %s", rendered)
	}
}

// TestClearMigrationDropsAMigrationArchiveThatHoldsNothing covers the archive a deployment can hold
// empty: the table is the migration's record-keeping and goes, and a row the table does not have is
// a row no report can name.
func TestClearMigrationDropsAMigrationArchiveThatHoldsNothing(t *testing.T) {
	db := openDamagedDeploymentDatabase(t)

	createMigrationArchive(t, db)

	if got := countRows(t, db, "migrationArchive"); got != 0 {
		t.Fatalf("the fixture's archive holds %d rows, want an empty table", got)
	}

	report, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("ClearMigrationDamage: %v", err)
	}

	for _, name := range readTableNames(t, db) {
		if name == "migrationArchive" {
			t.Fatalf("an empty migration archive is still among the deployment's tables after the clearing: %v", readTableNames(t, db))
		}
	}

	if !report.DroppedArchive {
		t.Errorf("the clearing did not report dropping the migration archive it dropped")
	}

	if len(report.ArchiveRows) != 0 {
		t.Errorf("the clearing reported %d archive rows from a table that held none: %+v", len(report.ArchiveRows), report.ArchiveRows)
	}
}

// TestClearMigrationSaysNothingAboutAnAbsentMigrationArchive is the half a deployment whose migration
// had nothing to archive rests on: a table that is not there is not a failure, and a run that says
// something about it says something that did not happen.
func TestClearMigrationSaysNothingAboutAnAbsentMigrationArchive(t *testing.T) {
	db := openDamagedDeploymentDatabase(t)

	mustExec(t, db, `DROP TABLE IF EXISTS "migrationArchive"`)

	if hasMigrationArchiveTable(t, db) {
		t.Fatal("the fixture holds a migration archive it was written to drop")
	}

	report, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("ClearMigrationDamage over a deployment with no migration archive: %v", err)
	}

	if report.DroppedArchive {
		t.Errorf("the clearing reported dropping a migration archive that was not in the deployment")
	}

	if len(report.ArchiveRows) != 0 {
		t.Errorf("the clearing reported %d archive rows from a deployment that has no archive table: %+v",
			len(report.ArchiveRows), report.ArchiveRows)
	}
}

// TestMigrationAfterAClearingCreatesNoMigrationArchiveWithoutALegacyTable is what makes dropping the
// archive final rather than a table that comes back on the next start: the pass creates the table for
// rows it might not carry, and a deployment holding none of the legacy table names has none.
func TestMigrationAfterAClearingCreatesNoMigrationArchiveWithoutALegacyTable(t *testing.T) {
	db := openDamagedDeploymentDatabaseWithArchive(t)

	if _, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background()); err != nil {
		t.Fatalf("ClearMigrationDamage: %v", err)
	}

	if hasMigrationArchiveTable(t, db) {
		t.Fatal("the clearing left the migration archive among the deployment's tables")
	}

	for _, line := range migrateDatabase(t, db) {
		if strings.Contains(line, "migrationArchive") {
			t.Errorf("the migration pass run after the clearing says something about the archive it did not create: %q", line)
		}
	}

	if hasMigrationArchiveTable(t, db) {
		t.Errorf("the migration pass run after a clearing created a migration archive for a deployment holding none of the legacy tables: %v",
			readTableNames(t, db))
	}
}

func reportedArchiveRow(rows []ports.ArchivedRow, table string, rowID uint64) (ports.ArchivedRow, bool) {
	for _, row := range rows {
		if row.Table == table && row.RowID == rowID {
			return row, true
		}
	}

	return ports.ArchivedRow{}, false
}

// The legacy tables the clearing is asked to drop are the ones the migration's own statements name.
// A deployment holds one when the pass could not read every row out of it and chose to leave the
// table rather than destroy a row, which is the state that makes the migration re-read and re-archive
// the same rows on every later start.

// damagedDeploymentLegacyTables are two of those tables, in the shape the recorded v6 fixture gives
// them: the calls table holding a row whose timestamp is not a datetime, and the talkgroups table
// holding a row whose frequency is not a number.
var damagedDeploymentLegacyTables = []string{"rdioScannerCalls", "rdioScannerTalkgroups"}

// legacyTableName reports whether a table is one the legacy server wrote rather than one this server
// writes, which is the difference the clearing must never get wrong.
func legacyTableName(name string) bool {
	return strings.HasPrefix(name, "rdioScanner")
}

// recordedLegacyTableStatements returns the recorded statements that declare and fill one legacy
// table, so the fixture holds the same legacy shape the migration's own statements name rather than a
// table this file invented beside it.
func recordedLegacyTableStatements(t *testing.T, table string) []string {
	t.Helper()

	statements := []string{}

	for _, statement := range legacyV6Schema {
		if strings.Contains(statement, "CREATE TABLE `"+table+"`") {
			statements = append(statements, statement)
		}
	}

	declared := len(statements)

	for _, statement := range legacyV6PartialRows {
		if strings.Contains(statement, `INSERT INTO "`+table+`"`) {
			statements = append(statements, statement)
		}
	}

	if declared == 0 || len(statements) == declared {
		t.Fatalf("the recorded v6 fixture does not give the %s table both its table and its rows", table)
	}

	return statements
}

// openDamagedDeploymentDatabaseWithLegacyTables is the deployment left holding legacy tables its
// migration could not finish reading, which is the state the clearing exists to end.
func openDamagedDeploymentDatabaseWithLegacyTables(t *testing.T) *sql.DB {
	t.Helper()

	db := openDamagedDeploymentDatabase(t)

	for _, table := range damagedDeploymentLegacyTables {
		for _, statement := range recordedLegacyTableStatements(t, table) {
			mustExec(t, db, statement)
		}
	}

	return db
}

// TestClearMigrationDropsTheLegacyTablesTheDeploymentHoldsAndNamesEachOne is the table half: every
// legacy table in the deployment goes, and each one is named by what it was and how many rows it held
// while it stood.
func TestClearMigrationDropsTheLegacyTablesTheDeploymentHoldsAndNamesEachOne(t *testing.T) {
	db := openDamagedDeploymentDatabaseWithLegacyTables(t)

	before := map[string]int{}

	for _, table := range damagedDeploymentLegacyTables {
		rows := countRows(t, db, table)

		if rows == 0 {
			t.Fatalf("the fixture's %s table holds no rows, want the rows the migration could not read out of it", table)
		}

		before[table] = rows
	}

	report, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("ClearMigrationDamage: %v", err)
	}

	for _, name := range readTableNames(t, db) {
		if legacyTableName(name) {
			t.Errorf("the clearing left the legacy table %s in the deployment: %v", name, readTableNames(t, db))
		}
	}

	if len(report.DroppedTables) != len(damagedDeploymentLegacyTables) {
		t.Fatalf("the clearing reported %d dropped tables, want one for each of the %d the deployment held: %+v",
			len(report.DroppedTables), len(damagedDeploymentLegacyTables), report.DroppedTables)
	}

	for table, rows := range before {
		got, ok := reportedDroppedTable(report.DroppedTables, table)
		if !ok {
			t.Errorf("the clearing did not report dropping the legacy table %s, which held %d rows: %+v", table, rows, report.DroppedTables)

			continue
		}

		if got.Rows != rows {
			t.Errorf("the clearing reported the %s table holding %d rows, want the %d rows it held", table, got.Rows, rows)
		}
	}
}

// TestClearMigrationNamesNoLegacyTableForADeploymentThatHoldsNone is the migrated deployment the
// archive's own gate leaves behind: no legacy table, and a run that reports no table it did not drop.
func TestClearMigrationNamesNoLegacyTableForADeploymentThatHoldsNone(t *testing.T) {
	db := openDamagedDeploymentDatabase(t)

	for _, name := range readTableNames(t, db) {
		if legacyTableName(name) {
			t.Fatalf("the fixture holds a legacy table it was written to hold none of: %s", name)
		}
	}

	report, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("ClearMigrationDamage: %v", err)
	}

	if len(report.DroppedTables) != 0 {
		t.Errorf("the clearing reported %d dropped tables for a deployment holding none of the legacy names: %+v",
			len(report.DroppedTables), report.DroppedTables)
	}
}

// TestClearMigrationLeavesEveryTableTheLiveServerWrites is the boundary the whole run is held to: the
// names it drops come from the legacy server, and every table this server reads and writes stands
// afterwards with the rows in it.
func TestClearMigrationLeavesEveryTableTheLiveServerWrites(t *testing.T) {
	db := openDamagedDeploymentDatabaseWithLegacyTables(t)

	report, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("ClearMigrationDamage: %v", err)
	}

	tables := readTableNames(t, db)

	for _, live := range legacyTables {
		found := false

		for _, name := range tables {
			if name == live {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("the clearing dropped the table %s, which this server writes and the legacy table names do not name: %v", live, tables)
		}
	}

	for _, dropped := range report.DroppedTables {
		for _, live := range legacyTables {
			if dropped.Name == live {
				t.Errorf("the clearing reported dropping %s, a table this server writes: %+v", dropped.Name, report.DroppedTables)
			}
		}
	}

	for _, held := range damagedDeploymentRowCounts {
		if got := countRows(t, db, held.table); got != held.rows {
			t.Errorf("after the clearing the %s table holds %d rows, want the %d it held before", held.table, got, held.rows)
		}
	}
}

// TestClearMigrationEndsTheCycleAKeptLegacyTableStarts is the reason the order is tables before
// archive. A legacy table the migration left behind re-reads and re-archives its rows on every start,
// so a run that emptied the archive without dropping that table would find the archive full again the
// next time the server opens the database. Clearing it once has to end the cycle.
func TestClearMigrationEndsTheCycleAKeptLegacyTableStarts(t *testing.T) {
	db := openLegacyV6PartialDatabase(t)

	migrateDatabase(t, db)

	kept := keptLegacyTables(t, db)

	if len(kept) == 0 {
		t.Fatal("the fixture's migration left no legacy table behind, want the tables holding rows it could not read")
	}

	archived := countRows(t, db, "migrationArchive")

	if archived == 0 {
		t.Fatal("the fixture's migration archived nothing, want the rows the pass could not carry")
	}

	// A second pass reads the same tables and reports them again, which is the state the owner's
	// deployment has been left in: the archive is refilled and the tables stand, start after start.
	for _, line := range migrateDatabase(t, db) {
		if !strings.Contains(line, "table where it found it") {
			continue
		}

		t.Logf("the second pass repeats the kept table line: %q", line)
	}

	if got := countRows(t, db, "migrationArchive"); got != archived {
		t.Fatalf("the second pass left %d archived rows, want the same %d the first pass left", got, archived)
	}

	report, err := NewMaintenance(db, DialectSqlite).ClearMigrationDamage(context.Background())
	if err != nil {
		t.Fatalf("ClearMigrationDamage: %v", err)
	}

	if got := keptLegacyTables(t, db); len(got) != 0 {
		t.Fatalf("the clearing left the legacy tables %v standing, want none for a deployment to re-read on the next start", got)
	}

	if !report.DroppedArchive {
		t.Errorf("the clearing did not report dropping the migration archive it dropped")
	}

	if hasMigrationArchiveTable(t, db) {
		t.Fatalf("the clearing left the migration archive standing: %v", readTableNames(t, db))
	}

	lines := migrateDatabase(t, db)

	// The archive's absence is the whole answer for a deployment with nothing left to archive: a
	// table that is not there holds no rows, and asking for their number would ask a backend for a
	// table it no longer has.
	if hasMigrationArchiveTable(t, db) {
		t.Errorf("the pass run after the clearing created a migration archive for a deployment holding none of the legacy names, with %d archived rows: %v",
			countRows(t, db, "migrationArchive"), readTableNames(t, db))
	}

	for _, line := range lines {
		if strings.Contains(line, "table where it found it") {
			t.Errorf("the pass run after the clearing says it left a table where it found it, after the clearing dropped every legacy table: %q", line)
		}
	}
}

// keptLegacyTables names the legacy tables still standing in a deployment, which is what the
// migration's carry steps read on every start.
func keptLegacyTables(t *testing.T, db *sql.DB) []string {
	t.Helper()

	found := []string{}

	for _, name := range readTableNames(t, db) {
		if legacyTableName(name) {
			found = append(found, name)
		}
	}

	return found
}

func reportedDroppedTable(tables []ports.DroppedTable, name string) (ports.DroppedTable, bool) {
	for _, table := range tables {
		if table.Name == name {
			return table, true
		}
	}

	return ports.DroppedTable{}, false
}

// The clearing's statements are pinned here rather than only run, because MySQL is held to its
// statement even though no MySQL backend is started, and because a statement that let a backend
// decide which unit rows are damage would decide it differently on each one.

// pinnedClearStatements are the statements each of the four backends is asked to run. The four cases
// below hold these same statements on purpose: the clearing reads every unit row and names the damage
// itself, and it drops tables by the names the migration's own statements wrote, so no backend is
// asked to read a number out of a text label or to choose which legacy tables to remove.
var pinnedClearStatements = clearStatements{
	units:           `SELECT "unitId", "label", "unitRef", "unitFrom", "unitTo", "systemId" FROM "units" ORDER BY "unitId"`,
	deleteUnit:      `DELETE FROM "units" WHERE "unitId" = ?`,
	archiveRows:     `SELECT "tableName", "rowId", "reason", "archivedAt" FROM "migrationArchive" ORDER BY "tableName", "rowId"`,
	dropArchive:     `DROP TABLE "migrationArchive"`,
	countLegacyRow:  `SELECT COUNT(*) FROM "%s"`,
	dropLegacyTable: `DROP TABLE "%s"`,
}

func TestClearMigrationBuildsThePinnedStatementForEachDialect(t *testing.T) {
	for _, tc := range []struct {
		dialect Dialect
		want    clearStatements
	}{
		{DialectSqlite, pinnedClearStatements},
		{DialectMariadb, pinnedClearStatements},
		{DialectMysql, pinnedClearStatements},
		{DialectPostgresql, pinnedClearStatements},
	} {
		m := NewMaintenance(openEmptyDatabase(t), tc.dialect)

		got := m.statements

		for _, statement := range []struct {
			what  string
			got   string
			want  string
			table string
		}{
			{"the unit rows it reads", got.units, tc.want.units, "units"},
			{"the unit row it removes", got.deleteUnit, tc.want.deleteUnit, "units"},
			{"the archive rows it reads", got.archiveRows, tc.want.archiveRows, "migrationArchive"},
			{"the archive table it drops", got.dropArchive, tc.want.dropArchive, "migrationArchive"},
			{"the rows a legacy table held", fmt.Sprintf(got.countLegacyRow, "rdioScannerUnits"), fmt.Sprintf(tc.want.countLegacyRow, "rdioScannerUnits"), "rdioScannerUnits"},
			{"the legacy table it drops", fmt.Sprintf(got.dropLegacyTable, "rdioScannerUnits"), fmt.Sprintf(tc.want.dropLegacyTable, "rdioScannerUnits"), "rdioScannerUnits"},
		} {
			if statement.got != statement.want {
				t.Errorf("%s builds %q for %s, want %q", tc.dialect, statement.got, statement.what, statement.want)
			}

			if !dropsTableNamed(statement.got, statement.table) && !readsRowsFrom(statement.got, statement.table) && !removesRowsFrom(statement.got, statement.table) {
				t.Errorf("%s runs %q, which neither reads the %s table, drops it, nor removes a row of it", tc.dialect, statement.got, statement.table)
			}

			if match := labelComparisonIn.FindString(statement.got); match != "" {
				t.Errorf("%s runs %q, which compares a unit label to a value with %q: the clearing names damage in the Domain, not in a statement",
					tc.dialect, statement.got, match)
			}

			if strings.Contains(statement.got, `"$1"`) || strings.Contains(statement.got, `$1`) {
				t.Errorf("%s builds %q with a numbered parameter, where the store rewrites the ? the migration's statements use", tc.dialect, statement.got)
			}
		}

		if !readsColumnsFrom(got.units, "units", "unitId", "label", "unitRef", "unitFrom", "unitTo", "systemId") {
			t.Errorf("%s reads the units table with %q, want the identifier, the label, the radio reference, the range, and the system it belongs to",
				tc.dialect, got.units)
		}

		if strings.Contains(strings.ToUpper(got.units), "WHERE") {
			t.Errorf("%s selects the unit rows with %q, want every row read and the damaged ones named by the Domain", tc.dialect, got.units)
		}

		if !deletesRowByKey(got.deleteUnit, "units", "unitId") {
			t.Errorf("%s removes a unit row with %q, want a delete naming the units table and its identifier", tc.dialect, got.deleteUnit)
		}

		if !readsColumnsFrom(got.archiveRows, "migrationArchive", "tableName", "rowId", "reason", "archivedAt") {
			t.Errorf("%s reads the archive with %q, want the table each row came from, its identifier there, the stored reason, and the moment it was archived",
				tc.dialect, got.archiveRows)
		}

		if strings.Contains(got.archiveRows, `"contents"`) {
			t.Errorf("%s reads the archive with %q, which carries the contents column the run reports a row without", tc.dialect, got.archiveRows)
		}

		for _, name := range v6MigrationTables {
			statement := fmt.Sprintf(got.dropLegacyTable, name)

			if !dropsTableNamed(statement, name) {
				t.Errorf("%s drops the legacy table with %q, want a drop naming %s", tc.dialect, statement, name)
			}

			if strings.Contains(statement, "?") {
				t.Errorf("%s drops %s with %q: the names come from the migration's own statements, so the statement holds the name and not a value",
					tc.dialect, name, statement)
			}
		}
	}
}

// TestTheClearingsLegacyNamesAreTheLegacyTablesTheMigrationNames holds the one declaration both the
// gate and the clearing read to the two sides it has to satisfy: every name is one the legacy server
// wrote, and no name is one this server writes.
func TestTheClearingsLegacyNamesAreTheLegacyTablesTheMigrationNames(t *testing.T) {
	if len(v6MigrationTables) != 13 {
		t.Fatalf("the clearing names %d legacy tables, want the 13 tables the migration's own statements read: %v",
			len(v6MigrationTables), v6MigrationTables)
	}

	for _, name := range v6MigrationTables {
		declared := false

		for _, statement := range legacyV6Schema {
			if strings.Contains(statement, "CREATE TABLE `"+name+"`") {
				declared = true

				break
			}
		}

		if !declared {
			t.Errorf("the clearing names %s, which the recorded v6 schema does not declare as a table the legacy server wrote", name)
		}

		for _, live := range legacyTables {
			if name == live {
				t.Errorf("the clearing names %s, which is a table this server writes: %v", name, v6MigrationTables)
			}
		}
	}

	for _, statement := range legacyV6Schema {
		name := legacyV6SchemaTable(statement)

		if name == "" {
			continue
		}

		found := false

		for _, known := range v6MigrationTables {
			if known == name {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("the legacy server wrote the %s table, which the clearing's names do not cover: %v", name, v6MigrationTables)
		}
	}
}

// legacyV6SchemaTable answers the table one recorded v6 schema statement creates, and empty for the
// statements in that fixture that create an index rather than a table.
func legacyV6SchemaTable(statement string) string {
	match := regexp.MustCompile("CREATE TABLE `([A-Za-z0-9_]+)`").FindStringSubmatch(statement)

	if len(match) != 2 {
		return ""
	}

	return match[1]
}

// labelComparisonIn names every place a statement compares the unit label to a value, which is the
// comparison the clearing is not allowed to make: it reads the label as text and decides the shape in
// the Domain, so that MariaDB, MySQL, and PostgreSQL cannot each read a number out of it their own way.
var labelComparisonIn = regexp.MustCompile(`"label"\s*(=|<>|!=|<|>|<=|>=|\bLIKE\b|\bIN\b)`)

func readsColumnsFrom(statement, table string, columns ...string) bool {
	upper := strings.ToUpper(statement)

	if !strings.HasPrefix(upper, "SELECT ") {
		return false
	}

	if !strings.Contains(statement, `FROM "`+table+`"`) {
		return false
	}

	selected := statement[:strings.Index(upper, "FROM ")]

	for _, column := range columns {
		if !strings.Contains(selected, `"`+column+`"`) {
			return false
		}
	}

	return true
}

func readsRowsFrom(statement, table string) bool {
	return strings.HasPrefix(strings.ToUpper(statement), "SELECT ") && strings.Contains(statement, `FROM "`+table+`"`)
}

func deletesRowByKey(statement, table, key string) bool {
	upper := strings.ToUpper(statement)

	if !strings.HasPrefix(upper, `DELETE FROM "`+strings.ToUpper(table)+`"`) {
		return false
	}

	return strings.Contains(upper, `WHERE "`+strings.ToUpper(key)+`"`)
}

func removesRowsFrom(statement, table string) bool {
	upper := strings.ToUpper(statement)

	return strings.HasPrefix(upper, "DELETE FROM ") && strings.Contains(upper, `FROM "`+strings.ToUpper(table)+`"`)
}

func dropsTableNamed(statement, table string) bool {
	return strings.HasPrefix(strings.ToUpper(statement), "DROP TABLE ") && strings.Contains(statement, `"`+table+`"`)
}
