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
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// A maintenance run is tested against the account the store hands back after it has cleared one
// deployment: the unit rows it named damage, the rows the migration archive held while it stood, the
// legacy tables it dropped with the rows each one held. Those are the values the operator reads, so
// the lines are asserted as whole sentences and not as fields a caller would have to assemble.
var damageInstant = time.Date(2026, time.November, 12, 9, 0, 0, 0, time.UTC)

func damagedUnit(id uint64, label string, order uint, systemID uint64, unitRef uint) domain.Unit {
	return domain.Unit{
		ID:       id,
		Label:    label,
		Order:    order,
		SystemID: systemID,
		UnitRef:  unitRef,
	}
}

// damagedDeploymentReport is the finished clearing of a deployment in the state a legacy migration
// leaves behind: two unit rows carrying their own radio numbers as their labels, the four kinds of row
// the archive holds, and two of the legacy tables still standing.
func damagedDeploymentReport() ports.MigrationDamageReport {
	return ports.MigrationDamageReport{
		Units: []domain.Unit{
			damagedUnit(303, "6789", 3, 101, 6789),
			damagedUnit(307, "1514000", 6, 101, 1514000),
		},
		ArchiveRows: []ports.ArchivedRow{
			{
				Table:      "rdioScannerMeta",
				RowID:      7,
				Reason:     "this server has no migration bookkeeping to carry it into",
				ArchivedAt: damageInstant,
			},
			{
				Table:      "rdioScannerTalkgroups",
				RowID:      115,
				Reason:     "its system 8081 is not in this database",
				ArchivedAt: damageInstant,
			},
			{
				Table:      "rdioScannerConfigs",
				RowID:      3,
				Reason:     "it holds values this server cannot carry: afsSystems, tagsToggle",
				ArchivedAt: damageInstant,
			},
			{
				Table:      "rdioScannerCalls",
				RowID:      706,
				Reason:     "it holds no audio",
				ArchivedAt: damageInstant,
			},
		},
		DroppedTables: []ports.DroppedTable{
			{Name: "rdioScannerCalls", Rows: 8},
			{Name: "rdioScannerTalkgroups", Rows: 5},
		},
		DroppedArchive: true,
	}
}

type clearMigrationDamageFixture struct {
	store    *fakes.Maintenance
	logs     *fakes.Logs
	clock    *fakes.Clock
	clear    *ClearMigrationDamageHandler
	reading  *GetMigrationDamageReportHandler
	clearErr error
	readErr  error
	clearRun ClearMigrationDamageResult
	readRun  GetMigrationDamageReportResult
}

func newClearMigrationDamageFixture(report ports.MigrationDamageReport) *clearMigrationDamageFixture {
	fixture := &clearMigrationDamageFixture{
		store: &fakes.Maintenance{Report: report},
		logs:  &fakes.Logs{},
		clock: fakes.NewClock(damageInstant),
	}

	fixture.clear = NewClearMigrationDamageHandler(fixture.store, fixture.logs, fixture.clock)
	fixture.reading = NewGetMigrationDamageReportHandler(fixture.store)

	return fixture
}

// clearOnce runs the maintenance run once, which is what the operator's switch does.
func (fixture *clearMigrationDamageFixture) clearOnce() (ClearMigrationDamageResult, error) {
	result, err := fixture.clear.Handle(context.Background(), ClearMigrationDamageCommand{})

	fixture.clearRun = result
	fixture.clearErr = err

	return result, err
}

// readOnce runs the same listing without clearing, which is what the companion switch does.
func (fixture *clearMigrationDamageFixture) readOnce() (GetMigrationDamageReportResult, error) {
	result, err := fixture.reading.Handle(context.Background(), GetMigrationDamageReportQuery{})

	fixture.readRun = result
	fixture.readErr = err

	return result, err
}

// filedRemovals returns the entries the run filed, oldest first.
func (fixture *clearMigrationDamageFixture) filedRemovals() []domain.LogEntry {
	filed := []domain.LogEntry{}

	for _, entry := range fixture.logs.Recorded() {
		if entry.Source == domain.LogSourceMaintenance {
			filed = append(filed, entry)
		}
	}

	return filed
}

// TestClearMigrationDamageCarriesWhatTheRunRemoved asserts the run's answer is the clearing the store
// performed: the removed unit rows by their stored values, the archive it dropped with the rows that
// were in it, the legacy tables it dropped with the rows each one held, and the counts the operator
// is told.
func TestClearMigrationDamageCarriesWhatTheRunRemoved(t *testing.T) {
	fixture := newClearMigrationDamageFixture(damagedDeploymentReport())

	result, err := fixture.clearOnce()
	if err != nil {
		t.Fatalf("the maintenance run failed: %v", err)
	}

	if result.Outcome != ClearMigrationDamageDone {
		t.Errorf("the run answers %q, want it to say it cleared the deployment", result.Outcome)
	}

	if len(result.RemovedUnits) != 2 {
		t.Fatalf("the run names %d removed units, want the 2 the store removed: %+v", len(result.RemovedUnits), result.RemovedUnits)
	}

	for _, want := range damagedDeploymentReport().Units {
		got, ok := findUnit(result.RemovedUnits, want.ID)
		if !ok {
			t.Errorf("the run does not name unit %d (%q), want it named with its stored values", want.ID, want.Label)

			continue
		}

		if got != want {
			t.Errorf("the run names unit %d as %+v, want %+v", want.ID, got, want)
		}
	}

	if !result.DroppedArchive {
		t.Errorf("the run says the migration archive stands, want it named as dropped")
	}

	if len(result.ArchivedRows) != 4 {
		t.Errorf("the run names %d archived rows, want one for each of the 4 rows the table held: %+v", len(result.ArchivedRows), result.ArchivedRows)
	}

	if len(result.DroppedTables) != 2 {
		t.Errorf("the run names %d dropped tables, want the 2 the store dropped: %+v", len(result.DroppedTables), result.DroppedTables)
	}

	if result.UnitsRemoved != 2 {
		t.Errorf("the run counts %d removed units, want 2", result.UnitsRemoved)
	}

	if result.TablesDropped != 2 {
		t.Errorf("the run counts %d dropped tables, want 2", result.TablesDropped)
	}

	if result.ArchiveRowsRemoved != 4 {
		t.Errorf("the run counts %d archived rows gone with the table, want 4", result.ArchiveRowsRemoved)
	}
}

// TestClearMigrationDamageNamesEachRemovedUnitByLabelNumberAndSystem asserts the line for a removed
// unit carries the three things an operator recognises the row by: the label stored on it, the radio
// number it carries, and the system it belonged to.
func TestClearMigrationDamageNamesEachRemovedUnitByLabelNumberAndSystem(t *testing.T) {
	fixture := newClearMigrationDamageFixture(damagedDeploymentReport())

	if _, err := fixture.clearOnce(); err != nil {
		t.Fatalf("the maintenance run failed: %v", err)
	}

	wants := []string{
		`removed unit "6789" carrying the radio number 6789, of system 101`,
		`removed unit "1514000" carrying the radio number 1514000, of system 101`,
	}

	for _, want := range wants {
		if !hasLine(fixture.clearRun.Lines, want) {
			t.Errorf("the run does not print %q, want the removed unit named by its label, its number, and its system, in: %q", want, fixture.clearRun.Lines)
		}
	}

	count := `removed 2 units labelled with their own radio numbers`

	if !hasLine(fixture.clearRun.Lines, count) {
		t.Errorf("the run does not print %q, want one count line for the class, in: %q", count, fixture.clearRun.Lines)
	}
}

// TestClearMigrationDamageNamesEachDroppedTableAndTheArchive asserts the line for a dropped table
// carries the table's name and the rows it held, and the archive is named once with its row count and
// once more for each row's table, identifier, and stored reason.
func TestClearMigrationDamageNamesEachDroppedTableAndTheArchive(t *testing.T) {
	fixture := newClearMigrationDamageFixture(damagedDeploymentReport())

	if _, err := fixture.clearOnce(); err != nil {
		t.Fatalf("the maintenance run failed: %v", err)
	}

	wants := []string{
		"dropped the table rdioScannerCalls, which held 8 rows",
		"dropped the table rdioScannerTalkgroups, which held 5 rows",
		"dropped the migration archive, which held 4 rows the migration could not carry",
		"removed the archived row rdioScannerMeta 7: this server has no migration bookkeeping to carry it into",
		"removed the archived row rdioScannerTalkgroups 115: its system 8081 is not in this database",
		"removed the archived row rdioScannerConfigs 3: it holds values this server cannot carry: afsSystems, tagsToggle",
		"removed the archived row rdioScannerCalls 706: it holds no audio",
	}

	for _, want := range wants {
		if !hasLine(fixture.clearRun.Lines, want) {
			t.Errorf("the run does not print %q, want every dropped table and every archived row named, in: %q", want, fixture.clearRun.Lines)
		}
	}
}

// TestClearMigrationDamageListsTheClassesInTheOrderTheOperatorReads them holds the order the listing
// is printed in: the unit rows, then the tables, then the archive, with the counts beside what they
// count.
func TestClearMigrationDamageListsTheClassesInTheOrderTheOperatorReadsThem(t *testing.T) {
	fixture := newClearMigrationDamageFixture(damagedDeploymentReport())

	if _, err := fixture.clearOnce(); err != nil {
		t.Fatalf("the maintenance run failed: %v", err)
	}

	lines := fixture.clearRun.Lines

	firstUnit := lineIndex(lines, `removed unit "6789"`)
	firstTable := lineIndex(lines, "dropped the table rdioScannerCalls")
	firstArchive := lineIndex(lines, "dropped the migration archive")

	if firstUnit < 0 || firstTable < 0 || firstArchive < 0 {
		t.Fatalf("the listing %q is missing a class the run performed", lines)
	}

	if !(firstUnit < firstTable && firstTable < firstArchive) {
		t.Errorf("the listing prints units at %d, tables at %d, and the archive at %d, want units first, then the tables, then the archive: %q", firstUnit, firstTable, firstArchive, lines)
	}

	unitCount := lineIndex(lines, "removed 2 units labelled with their own radio numbers")

	if unitCount < 0 || unitCount < firstUnit {
		t.Errorf("the count line for the unit class sits at %d, want it after the unit rows it counts: %q", unitCount, lines)
	}

	if unitCount > firstTable {
		t.Errorf("the count line for the unit class sits past the table lines, want it closed before the next class begins: %q", lines)
	}
}

// TestClearMigrationDamageReportsNothingAboutAClassThatFoundNothing is the deployment a run has
// already cleared: every class is empty, and an empty class is said nothing about rather than
// reported as zero.
func TestClearMigrationDamageReportsNothingAboutAClassThatFoundNothing(t *testing.T) {
	fixture := newClearMigrationDamageFixture(ports.MigrationDamageReport{
		Units:         []domain.Unit{},
		ArchiveRows:   []ports.ArchivedRow{},
		DroppedTables: []ports.DroppedTable{},
	})

	result, err := fixture.clearOnce()
	if err != nil {
		t.Fatalf("the maintenance run failed: %v", err)
	}

	if result.Outcome != ClearMigrationDamageNothing {
		t.Errorf("a run that found nothing answers %q, want it to say it found nothing", result.Outcome)
	}

	if len(result.Lines) != 0 {
		t.Errorf("a run that found nothing prints %q, want it to print nothing about the clearing", result.Lines)
	}

	if filed := fixture.filedRemovals(); len(filed) != 0 {
		t.Errorf("a run that found nothing files %d entries, want it to file nothing: %+v", len(filed), filed)
	}

	if result.UnitsRemoved != 0 || result.TablesDropped != 0 || result.ArchiveRowsRemoved != 0 {
		t.Errorf("a run that found nothing counts %d units, %d tables, %d archived rows, want none of each",
			result.UnitsRemoved, result.TablesDropped, result.ArchiveRowsRemoved)
	}
}

// TestClearMigrationDamageSaysNothingAboutAnArchiveThatWasNotThere is the deployment whose migration
// had nothing to archive: there is no archive table, and the run does not mention one.
func TestClearMigrationDamageSaysNothingAboutAnArchiveThatWasNotThere(t *testing.T) {
	report := damagedDeploymentReport()
	report.ArchiveRows = []ports.ArchivedRow{}
	report.DroppedArchive = false

	fixture := newClearMigrationDamageFixture(report)

	result, err := fixture.clearOnce()
	if err != nil {
		t.Fatalf("the maintenance run failed: %v", err)
	}

	for _, line := range result.Lines {
		if strings.Contains(line, "migration archive") {
			t.Errorf("the run prints %q for a deployment that has no migration archive, want silence about it: %q", line, result.Lines)
		}
	}

	if result.ArchiveRowsRemoved != 0 {
		t.Errorf("the run counts %d archived rows removed, want none", result.ArchiveRowsRemoved)
	}
}

// TestClearMigrationDamageFilesEveryRemovalAsOneWarnEntryUnderTheMaintenanceSource is the record the
// deployment keeps: one entry per removed row, one per dropped table, one for the dropped archive,
// each at the warn level under its own source, so the log view can answer which rows this server
// removed and when.
func TestClearMigrationDamageFilesEveryRemovalAsOneWarnEntryUnderTheMaintenanceSource(t *testing.T) {
	fixture := newClearMigrationDamageFixture(damagedDeploymentReport())

	if _, err := fixture.clearOnce(); err != nil {
		t.Fatalf("the maintenance run failed: %v", err)
	}

	filed := fixture.filedRemovals()

	wants := []string{
		`removed unit "6789" carrying the radio number 6789, of system 101`,
		`removed unit "1514000" carrying the radio number 1514000, of system 101`,
		"removed 2 units labelled with their own radio numbers",
		"dropped the table rdioScannerCalls, which held 8 rows",
		"dropped the table rdioScannerTalkgroups, which held 5 rows",
		"dropped the migration archive, which held 4 rows the migration could not carry",
	}

	messages := []string{}

	for _, entry := range filed {
		if entry.Level != domain.LogLevelWarn {
			t.Errorf("the entry %q is filed at %q, want it at the warn level", entry.Message, entry.Level)
		}

		if entry.Source != domain.LogSourceMaintenance {
			t.Errorf("the entry %q is filed under %q, want it filed under %q", entry.Message, entry.Source, domain.LogSourceMaintenance)
		}

		if !entry.DateTime.Equal(damageInstant) {
			t.Errorf("the entry %q carries %s, want the instant the run filed it", entry.Message, entry.DateTime)
		}

		messages = append(messages, entry.Message)
	}

	if len(filed) != len(wants) {
		t.Fatalf("the run files %d entries %q, want one for each of the %d removals and their counts", len(filed), messages, len(wants))
	}

	for _, want := range wants {
		if !hasLine(messages, want) {
			t.Errorf("the run files no entry %q, want the removal named there too: %q", want, messages)
		}
	}
}

// TestGetMigrationDamageReportListsWhatARunWouldRemoveAndRemovesNothing is the companion switch: the same
// listing, each line marked as what the run would do, no clearing asked of the store, and no entry filed
// because nothing happened. The two runs are separate fixtures over the same deployment, so the listing a
// report-only run prints is compared against the listing a real run of the same deployment prints.
func TestGetMigrationDamageReportListsWhatARunWouldRemoveAndRemovesNothing(t *testing.T) {
	report := damagedDeploymentReport()

	reading := newClearMigrationDamageFixture(report)
	clearing := newClearMigrationDamageFixture(report)

	result, err := reading.readOnce()
	if err != nil {
		t.Fatalf("the report-only run failed: %v", err)
	}

	if reading.store.Cleared != 0 {
		t.Errorf("the report-only run asked the store to clear %d times, want it to ask for no clearing at all", reading.store.Cleared)
	}

	if reading.store.Reads != 1 {
		t.Errorf("the report-only run asked the store to read the deployment %d times, want exactly one read", reading.store.Reads)
	}

	if filed := reading.filedRemovals(); len(filed) != 0 {
		t.Errorf("the report-only run files %d entries, want it to file nothing because nothing happened: %+v", len(filed), filed)
	}

	if _, err := clearing.clearOnce(); err != nil {
		t.Fatalf("the maintenance run failed: %v", err)
	}

	if len(result.Lines) == 0 {
		t.Fatalf("the report-only listing prints nothing, want the same lines a real run prints: %q", clearing.clearRun.Lines)
	}

	for _, line := range result.Lines {
		if !strings.HasPrefix(line, "would ") {
			t.Errorf("the report-only listing prints %q, want every line marked as what the run would do: %q", line, result.Lines)
		}
	}

	if len(result.Lines) != len(clearing.clearRun.Lines) {
		t.Fatalf("the report-only listing names %d lines %q, want the same %d lines a real run prints %q",
			len(result.Lines), result.Lines, len(clearing.clearRun.Lines), clearing.clearRun.Lines)
	}

	for i, line := range result.Lines {
		ran := clearing.clearRun.Lines[i]

		if strings.HasPrefix(ran, "would ") {
			t.Errorf("the maintenance run prints %q, want a real run to name what it did and not what it would do", ran)
		}

		if listingBody(line) != listingBody(ran) {
			t.Errorf("the report-only listing prints %q where a real run prints %q, want the same listing marked as what the run would do", line, ran)
		}
	}

	if result.UnitsRemoved != 2 || result.TablesDropped != 2 || result.ArchiveRowsRemoved != 4 {
		t.Errorf("the report-only listing counts %d units, %d tables, %d archived rows, want the same counts a real run reports",
			result.UnitsRemoved, result.TablesDropped, result.ArchiveRowsRemoved)
	}
}

// TestGetMigrationDamageReportOfAClearedDeploymentSaysNothing runs the listing over a deployment that
// holds nothing: the same silence a real run keeps, and no clearing asked of the store.
func TestGetMigrationDamageReportOfAClearedDeploymentSaysNothing(t *testing.T) {
	fixture := newClearMigrationDamageFixture(ports.MigrationDamageReport{
		Units:         []domain.Unit{},
		ArchiveRows:   []ports.ArchivedRow{},
		DroppedTables: []ports.DroppedTable{},
	})

	report, err := fixture.readOnce()
	if err != nil {
		t.Fatalf("the report-only run failed: %v", err)
	}

	if len(report.Lines) != 0 {
		t.Errorf("the report-only listing prints %q, want it to print nothing about a deployment that holds nothing", report.Lines)
	}

	if fixture.store.Cleared != 0 {
		t.Errorf("the report-only run asked the store to clear %d times, want no clearing at all", fixture.store.Cleared)
	}
}

// TestClearMigrationDamageFilesNoRemovalEntryWhenTheStoreCannotClear is the failure a run has on a
// deployment it cannot clear: the outcome is a failure, nothing is filed as removed, and no partial
// account is left in the log.
func TestClearMigrationDamageFilesNoRemovalEntryWhenTheStoreCannotClear(t *testing.T) {
	fixture := newClearMigrationDamageFixture(damagedDeploymentReport())
	fixture.store.Failure = errors.New("the database is held by another process")

	result, err := fixture.clearOnce()
	if err == nil {
		t.Fatalf("the maintenance run answers no error, want the store's failure: %+v", result)
	}

	if result.Outcome != ClearMigrationDamageFailed {
		t.Errorf("a failed run answers %q, want it to answer a failure", result.Outcome)
	}

	if len(result.Lines) != 0 {
		t.Errorf("a failed run prints %q, want it to print nothing it did not do", result.Lines)
	}

	if filed := fixture.filedRemovals(); len(filed) != 0 {
		t.Errorf("a failed run files %d entries, want no account of removals that did not happen: %+v", len(filed), filed)
	}
}

// TestGetMigrationDamageReportRefusesADeploymentItCannotRead is the companion switch's failure: the
// listing has no deployment to read, and it says so instead of printing an empty listing.
func TestGetMigrationDamageReportRefusesADeploymentItCannotRead(t *testing.T) {
	fixture := newClearMigrationDamageFixture(damagedDeploymentReport())
	fixture.store.Failure = errors.New("the database is held by another process")

	if _, err := fixture.readOnce(); err == nil {
		t.Fatal("the report-only run answers no error, want the store's failure")
	}

	if filed := fixture.filedRemovals(); len(filed) != 0 {
		t.Errorf("the report-only run files %d entries, want nothing filed: %+v", len(filed), filed)
	}
}

// TestClearMigrationDamageAsksTheStoreOnce is the run's own single purpose: it clears the deployment
// once, and it is the operator who asked. No server start, scheduled pass, or read reaches it.
func TestClearMigrationDamageAsksTheStoreOnce(t *testing.T) {
	fixture := newClearMigrationDamageFixture(damagedDeploymentReport())

	if _, err := fixture.clearOnce(); err != nil {
		t.Fatalf("the maintenance run failed: %v", err)
	}

	if fixture.store.Cleared != 1 {
		t.Errorf("the run asked the store to clear %d times, want exactly one clearing", fixture.store.Cleared)
	}

	if fixture.store.Reads != 0 {
		t.Errorf("the run asked the store to read the deployment %d times, want it to clear it and not read it aside", fixture.store.Reads)
	}
}

func findUnit(units []domain.Unit, id uint64) (domain.Unit, bool) {
	for _, unit := range units {
		if unit.ID == id {
			return unit, true
		}
	}

	return domain.Unit{}, false
}

func hasLine(lines []string, want string) bool {
	return lineIndex(lines, want) >= 0
}

func lineIndex(lines []string, prefix string) int {
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return i
		}
	}

	return -1
}

// listingBody drops the verb a listing line opens with, which is the one thing that tells a removal that
// happened from one a report-only run names. Comparing bodies holds the two listings identical in every
// other word.
func listingBody(line string) string {
	for _, verb := range []string{"would remove ", "would drop ", "removed ", "dropped "} {
		if strings.HasPrefix(line, verb) {
			return strings.TrimPrefix(line, verb)
		}
	}

	return line
}
