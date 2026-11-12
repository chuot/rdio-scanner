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
	"fmt"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// ClearMigrationDamageOutcome is what the maintenance run did about the records a legacy migration left
// in problem. Only Done and Nothing are a finished run: a run that found nothing says nothing about it,
// the same way the retention pass says nothing when the retention is switched off.
type ClearMigrationDamageOutcome string

const (
	// ClearMigrationDamageDone means the run removed rows, dropped tables, or both.
	ClearMigrationDamageDone ClearMigrationDamageOutcome = "done"

	// ClearMigrationDamageNothing means the deployment held nothing the run would name. It is not a
	// failure: it is the answer a run gets when it is pointed at a deployment it has already cleared.
	ClearMigrationDamageNothing ClearMigrationDamageOutcome = "nothing"

	// ClearMigrationDamageFailed means the clearing could not be completed. Nothing was removed, and
	// nothing is filed as though it had been.
	ClearMigrationDamageFailed ClearMigrationDamageOutcome = "failed"
)

// ClearMigrationDamageCommand asks the server to clear the records a legacy migration left in problem.
// It carries nothing: an operator ran the switch, and which stored rows are a problem is the server's own
// rule rather than something a caller supplies.
type ClearMigrationDamageCommand struct{}

// GetMigrationDamageReportQuery asks what a maintenance run would clear, and asks for nothing else. It is
// the report-only form of the same operator decision, and it changes no stored row.
type GetMigrationDamageReportQuery struct{}

// MigrationDamageListing is what both halves of a maintenance run answer with: the classes the run named,
// the counts they add up to, and the lines that name each one. A clearing's listing names rows that are
// gone; a report-only listing names the same rows as what a run would remove, and only the verb of a line
// says which of the two it is. The classes are listed in the order a run performs them and an operator
// reads them: the unit rows, then the tables, then the archive.
type MigrationDamageListing struct {
	// RemovedUnits are the unit rows the run named as damage, with the stored values that make them so.
	RemovedUnits []domain.Unit

	// DroppedTables are the legacy tables the run dropped, with the rows each one held while it stood.
	DroppedTables []ports.DroppedTable

	// ArchivedRows are the rows the migration archive held while it stood. Each one is named by the table
	// it came from, its identifier there, and the reason the migration stored beside it. What a row holds
	// is not part of it: an archived call carries that call's audio in it, and no report repeats it.
	ArchivedRows []ports.ArchivedRow

	// DroppedArchive says the archive table was there and went with the rows in it. A deployment whose
	// migration had nothing to archive has no such table, and its listing says nothing about one.
	DroppedArchive bool

	UnitsRemoved       int
	TablesDropped      int
	ArchiveRowsRemoved int

	// Lines is the listing an operator reads: one line per removal, and one count line for each class that
	// removed something. A class that found nothing contributes no line.
	Lines []string

	// damage holds the same lines with each one's sentence beside whether that removal is also worth an
	// entry in the server log. It is what a clearing files from, so what a run says and what a run records
	// are written by one builder and cannot drift apart.
	damage []damageLine
}

// damageLine is one sentence a run says about one thing it removed, and whether the deployment also keeps
// a record of it.
type damageLine struct {
	message string
	logged  bool
}

// ClearMigrationDamageResult is the acknowledgement the maintenance run gives back.
type ClearMigrationDamageResult struct {
	Outcome ClearMigrationDamageOutcome

	MigrationDamageListing
}

// GetMigrationDamageReportResult is the listing the report-only run gives back. It carries no outcome
// because nothing it names happened.
type GetMigrationDamageReportResult struct {
	MigrationDamageListing
}

// ClearMigrationDamageHandler clears the records a legacy migration left in problem: the legacy tables a
// migration left standing, the archive table holding what it could not carry, and the unit rows labelled
// with their own radio numbers. The clearing itself belongs to the store, which does all three in one
// transaction and in the order that ends the cycle a kept legacy table sets up. This handler names what
// went in the words an operator reads and files each removal in the server log, so a deployment cleared
// months ago can still be explained.
type ClearMigrationDamageHandler struct {
	clearer ports.MigrationClearer
	logs    ports.LogRepository
	clock   ports.Clock
}

// NewClearMigrationDamageHandler builds the handler from its ports.
func NewClearMigrationDamageHandler(
	clearer ports.MigrationClearer,
	logs ports.LogRepository,
	clock ports.Clock,
) *ClearMigrationDamageHandler {
	return &ClearMigrationDamageHandler{
		clearer: clearer,
		logs:    logs,
		clock:   clock,
	}
}

// Handle clears the deployment once, files one warn entry per removal, and reports what it removed. The
// entries are filed after the clearing and never before it: a run that could not clear the deployment
// leaves no account of rows it did not remove.
func (handler *ClearMigrationDamageHandler) Handle(ctx context.Context, _ ClearMigrationDamageCommand) (ClearMigrationDamageResult, error) {
	report, err := handler.clearer.ClearMigrationDamage(ctx)
	if err != nil {
		return ClearMigrationDamageResult{Outcome: ClearMigrationDamageFailed}, err
	}

	listing := migrationDamageListing(report, false)

	if listing.namesNothing() {
		return ClearMigrationDamageResult{
			Outcome:                ClearMigrationDamageNothing,
			MigrationDamageListing: listing,
		}, nil
	}

	for _, line := range listing.damage {
		if !line.logged {
			continue
		}

		appendServerLog(ctx, handler.logs, handler.clock, domain.LogLevelWarn, domain.LogSourceMaintenance, line.message)
	}

	return ClearMigrationDamageResult{
		Outcome:                ClearMigrationDamageDone,
		MigrationDamageListing: listing,
	}, nil
}

// GetMigrationDamageReportHandler reads what a maintenance run would clear and clears nothing. It is a
// query beside the command rather than the command with a flag on it: a listing that removes nothing has
// no outcome to name, no failure to report, and nothing to file. It is given no log repository and no
// clock, because a listing a person read is not a change that happened, and a log view full of would-be
// removals is a record of nothing.
type GetMigrationDamageReportHandler struct {
	reader ports.MigrationDamageReader
}

// NewGetMigrationDamageReportHandler builds the handler from its port.
func NewGetMigrationDamageReportHandler(reader ports.MigrationDamageReader) *GetMigrationDamageReportHandler {
	return &GetMigrationDamageReportHandler{reader: reader}
}

// Handle reads the three classes a clearing names and lists them in the same sentences a real run prints,
// each one marked as what the run would do.
func (handler *GetMigrationDamageReportHandler) Handle(ctx context.Context, _ GetMigrationDamageReportQuery) (GetMigrationDamageReportResult, error) {
	report, err := handler.reader.ReadMigrationDamage(ctx)
	if err != nil {
		return GetMigrationDamageReportResult{}, err
	}

	return GetMigrationDamageReportResult{
		MigrationDamageListing: migrationDamageListing(report, true),
	}, nil
}

// migrationDamageListing turns what the store removed, or would have removed, into the listing both halves
// answer with. The same builder writes both, which is what makes a report-only listing the listing a real
// run prints with each line marked as what it would do.
func migrationDamageListing(report ports.MigrationDamageReport, would bool) MigrationDamageListing {
	lines := damageLines(report, would)

	listing := MigrationDamageListing{
		RemovedUnits:       report.Units,
		DroppedTables:      report.DroppedTables,
		ArchivedRows:       report.ArchiveRows,
		DroppedArchive:     report.DroppedArchive,
		UnitsRemoved:       len(report.Units),
		TablesDropped:      len(report.DroppedTables),
		ArchiveRowsRemoved: len(report.ArchiveRows),
		Lines:              make([]string, 0, len(lines)),
		damage:             lines,
	}

	for _, line := range lines {
		listing.Lines = append(listing.Lines, line.message)
	}

	return listing
}

// damageLines words every removal the run made, or would have made, as the sentence an operator reads, in
// the order a run performs them: the unit rows and the count for that class, then the tables, then the
// archive and the rows that went with it. A class with nothing to name contributes no line, so a run that
// found nothing is silent rather than reporting zeros.
func damageLines(report ports.MigrationDamageReport, would bool) []damageLine {
	removed := damageVerb(would, "removed", "would remove")
	dropped := damageVerb(would, "dropped", "would drop")

	lines := []damageLine{}

	for _, unit := range report.Units {
		lines = append(lines, damageLine{
			message: fmt.Sprintf(
				"%s unit %q carrying the radio number %d, of system %d",
				removed,
				unit.Label,
				unit.UnitRef,
				unit.SystemID,
			),
			logged: true,
		})
	}

	if count := len(report.Units); count > 0 {
		own, noun := damagedUnitOwns(count)

		lines = append(lines, damageLine{
			message: fmt.Sprintf(
				"%s %s labelled with %s own %s",
				removed,
				counted(count, "unit", "units"),
				own,
				noun,
			),
			logged: true,
		})
	}

	for _, table := range report.DroppedTables {
		lines = append(lines, damageLine{
			message: fmt.Sprintf(
				"%s the table %s, which held %s",
				dropped,
				table.Name,
				counted(table.Rows, "row", "rows"),
			),
			logged: true,
		})
	}

	if report.DroppedArchive {
		held := "no archived rows"

		if count := len(report.ArchiveRows); count > 0 {
			held = fmt.Sprintf("%s the migration could not carry", counted(count, "row", "rows"))
		}

		lines = append(lines, damageLine{
			message: fmt.Sprintf("%s the migration archive, which held %s", dropped, held),
			logged:  true,
		})
	}

	// The archived rows are named for the operator and not filed one by one: the entry for the archive
	// table gives the count, and the table went with every row in it.
	for _, row := range report.ArchiveRows {
		lines = append(lines, damageLine{
			message: fmt.Sprintf(
				"%s the archived row %s %d: %s",
				removed,
				row.Table,
				row.RowID,
				row.Reason,
			),
		})
	}

	return lines
}

// damageVerb picks the tense a listing line is written in. Both halves name the same rows, and only the
// verb of a line tells a run that removed them from a run that reports it would.
func damageVerb(would bool, did, wouldDo string) string {
	if would {
		return wouldDo
	}

	return did
}

// damagedUnitOwns words the class's count line: one unit carries its own radio number as its label, and
// several carry their own.
func damagedUnitOwns(count int) (string, string) {
	if count == 1 {
		return "its", "radio number"
	}

	return "their", "radio numbers"
}

// namesNothing reports a listing that names no row and no table, which is a deployment the run has nothing
// to say about.
func (listing MigrationDamageListing) namesNothing() bool {
	return listing.UnitsRemoved == 0 &&
		listing.TablesDropped == 0 &&
		listing.ArchiveRowsRemoved == 0 &&
		!listing.DroppedArchive
}
