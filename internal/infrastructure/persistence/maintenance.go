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
	"fmt"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// v6MigrationTables are the table names a v6 deployment's own schema wrote, listed in the order the
// migration's carry steps read them. Every one of them is read by a carry step only when it is
// present, and a carry step that could not read every row of one leaves that table standing. This is
// the one place those names are spelled: the startup pass asks them of the database to decide whether
// to create the migration archive at all, and the maintenance run asks them to decide which tables to
// drop. Neither one drops a table this list does not name, so no table this server reads and writes
// can be reached by either.
var v6MigrationTables = []string{
	"rdioScannerGroups",
	"rdioScannerTags",
	"rdioScannerSystems",
	"rdioScannerTalkgroups",
	"rdioScannerUnits",
	"rdioScannerConfigs",
	"rdioScannerMeta",
	"rdioScannerLogs",
	"rdioScannerDownstreams",
	v6DirwatchesTable,
	"rdioScannerCalls",
	"rdioScannerApiKeys",
	"rdioScannerAccesses",
}

// migrationArchiveTable is the table the migration pass keeps the rows it could not carry in, and the
// table a clearing drops once those rows have been named.
const migrationArchiveTable = "migrationArchive"

// legacyTablesPresent answers whether the deployment still holds any of the legacy tables the
// migration reads, which is the only state in which a migration pass has rows it might not carry and
// the only state in which a finished deployment has a legacy table left to drop.
func legacyTablesPresent(ctx context.Context, s store) (bool, error) {
	for _, table := range v6MigrationTables {
		present, err := tableExists(ctx, s, table)
		if err != nil {
			return false, err
		}

		if present {
			return true, nil
		}
	}

	return false, nil
}

// clearStatements are the statements the clearing asks one backend to run. They are kept apart from
// the run so that what each backend is asked can be read without starting a backend. Which unit rows
// are damage is decided by the Domain predicate rather than by anything a backend compares, so all
// four dialects are asked the same statements, each written once with the `?` placeholder the store
// rewrites for PostgreSQL. A statement that names a legacy table holds the name itself: the names come
// from this package's own list, not from anything the database answers. The reading asks the three read
// statements of this set and none of the ones that remove or drop, so both halves of the run put the same
// questions to a backend and only one of them changes anything.
type clearStatements struct {
	units           string
	deleteUnit      string
	archiveRows     string
	dropArchive     string
	countLegacyRow  string
	dropLegacyTable string
}

// Maintenance is the adapter for the records a legacy migration left in problem: the unit rows the
// migration created rather than a deployment named, the migration archive table, and the v6 tables the
// migration left standing. It answers the run's two questions from the same three reads and the same
// statements — remove them, or say which ones removing them would name — so what a run says it would do and
// what a run does cannot be read out of different queries. The operator's one-shot maintenance run builds it
// once; no start, scheduled pass, or endpoint reaches it.
type Maintenance struct {
	store store

	statements clearStatements
}

var _ ports.MigrationClearer = (*Maintenance)(nil)
var _ ports.MigrationDamageReader = (*Maintenance)(nil)

// NewMaintenance builds the maintenance adapter for one opened database: both its halves, the clearing and
// the reading, are built the same way and hold the same statements.
func NewMaintenance(db *sql.DB, dialect Dialect) *Maintenance {
	return &Maintenance{
		store: newStore(db, dialect),
		statements: clearStatements{
			units:           `SELECT "unitId", "label", "unitRef", "unitFrom", "unitTo", "systemId" FROM "units" ORDER BY "unitId"`,
			deleteUnit:      `DELETE FROM "units" WHERE "unitId" = ?`,
			archiveRows:     `SELECT "tableName", "rowId", "reason", "archivedAt" FROM "` + migrationArchiveTable + `" ORDER BY "tableName", "rowId"`,
			dropArchive:     `DROP TABLE "` + migrationArchiveTable + `"`,
			countLegacyRow:  `SELECT COUNT(*) FROM "%s"`,
			dropLegacyTable: `DROP TABLE "%s"`,
		},
	}
}

// ClearMigrationDamage drops the legacy tables the deployment still holds, then the migration archive
// table with every row in it, then the unit rows a migration made rather than a deployment named. The
// order is the one that ends a cycle rather than starts it: a carry step reads a legacy table and
// archives what it cannot carry, so emptying the archive while a legacy table stands would have it
// full again on the next start. Everything goes in one transaction that commits once at the end, so a
// failure anywhere in it leaves the deployment exactly as it was. The compaction is not part of this:
// it belongs after the commit, and it is the run's caller's own statement.
func (m *Maintenance) ClearMigrationDamage(ctx context.Context) (ports.MigrationDamageReport, error) {
	const section, operation = "maintenance", "clear"

	report := ports.MigrationDamageReport{
		Units:         []domain.Unit{},
		ArchiveRows:   []ports.ArchivedRow{},
		DroppedTables: []ports.DroppedTable{},
	}

	// The presence questions are asked before the transaction opens. A table that is not there is an
	// answer rather than a failure, and a statement that failed for its own absence is one a backend
	// ends the transaction for.
	legacy, err := standingLegacyTables(ctx, m.store)
	if err != nil {
		return report, operationError(section, operation, err)
	}

	archived, err := tableExists(ctx, m.store, migrationArchiveTable)
	if err != nil {
		return report, operationError(section, operation, err)
	}

	tx, err := m.store.begin(ctx)
	if err != nil {
		return report, operationError(section, operation, err)
	}

	// Every standing table is counted before any is dropped, so each one is reported with the rows it held
	// while it stood.
	tables, err := legacyTableRowCounts(ctx, tx, m.statements, legacy, section, operation)
	if err != nil {
		tx.rollback()

		return report, err
	}

	report.DroppedTables = tables

	for _, table := range legacy {
		drop := fmt.Sprintf(m.statements.dropLegacyTable, table)

		if _, err := tx.exec(ctx, drop); err != nil {
			tx.rollback()

			return report, statementError(section, operation, err, drop)
		}
	}

	if archived {
		rows, err := migrationArchiveRows(ctx, tx, m.statements, section, operation)
		if err != nil {
			tx.rollback()

			return report, err
		}

		report.ArchiveRows = rows

		if _, err := tx.exec(ctx, m.statements.dropArchive); err != nil {
			tx.rollback()

			return report, statementError(section, operation, err, m.statements.dropArchive)
		}

		report.DroppedArchive = true
	}

	// Every unit row is read and each one handed to the Domain predicate, through the same read the
	// report-only half of the run asks: the shape the predicate names is a label read as the plain decimal
	// form of its own number, and a backend asked to compare a text label with a number reads a number out
	// of the text its own way. The rows are collected before any is deleted, so the read is finished before
	// the table changes under it.
	damaged, err := damagedUnitRows(ctx, tx, m.statements, section, operation)
	if err != nil {
		tx.rollback()

		return report, err
	}

	for _, unit := range damaged {
		if _, err := tx.exec(ctx, m.statements.deleteUnit, unit.ID); err != nil {
			tx.rollback()

			return report, statementError(section, operation, err, m.statements.deleteUnit)
		}

		report.Units = append(report.Units, unit)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()

		return report, operationError(section, operation, err)
	}

	return report, nil
}

// ReadMigrationDamage answers the same three questions the clearing answers and removes nothing: the unit
// rows a migration made rather than a deployment named, the legacy tables still standing with the rows
// they hold, and the migration archive with every row kept in it. The classes are read in the order a run
// performs them, so the listing an operator reads before deciding is built from the same account the run
// reports afterwards. The read runs in a read-only transaction, which is what lets the deployment be
// exactly what it was when the listing was read while a server of the same deployment is not running.
func (m *Maintenance) ReadMigrationDamage(ctx context.Context) (ports.MigrationDamageReport, error) {
	const section, operation = "maintenance", "read"

	report := ports.MigrationDamageReport{
		Units:         []domain.Unit{},
		ArchiveRows:   []ports.ArchivedRow{},
		DroppedTables: []ports.DroppedTable{},
	}

	standing, err := standingLegacyTables(ctx, m.store)
	if err != nil {
		return report, operationError(section, operation, err)
	}

	archived, err := tableExists(ctx, m.store, migrationArchiveTable)
	if err != nil {
		return report, operationError(section, operation, err)
	}

	tx, err := m.store.beginRead(ctx)
	if err != nil {
		return report, operationError(section, operation, err)
	}

	defer tx.rollback()

	tables, err := legacyTableRowCounts(ctx, tx, m.statements, standing, section, operation)
	if err != nil {
		return report, err
	}

	report.DroppedTables = tables

	if archived {
		rows, err := migrationArchiveRows(ctx, tx, m.statements, section, operation)
		if err != nil {
			return report, err
		}

		report.ArchiveRows = rows

		// The archive table's own presence is what the listing names, because a run drops the table and
		// every row in it: an operator deciding whether to run it has to know the table is there at all,
		// and not only how many rows are in it.
		report.DroppedArchive = true
	}

	units, err := damagedUnitRows(ctx, tx, m.statements, section, operation)
	if err != nil {
		return report, err
	}

	report.Units = units

	return report, nil
}

// damageReader is the half of the store the clearing and the reading both ask their read statements of:
// the clearing asks them inside the transaction it commits at the end, the reading inside a read-only one.
// None of the three read statements binds a value, so neither a table name nor a unit's shape is left to a
// backend to decide.
type damageReader interface {
	query(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	queryRow(ctx context.Context, query string, args ...any) *sql.Row
}

// standingLegacyTables asks which of the legacy table names the deployment still holds, in the order the
// migration's carry steps read them. A run drops exactly these tables, so this is the one question both
// halves of the run ask first: a table that is not there is an answer, not a failure.
func standingLegacyTables(ctx context.Context, s store) ([]string, error) {
	standing := []string{}

	for _, table := range v6MigrationTables {
		present, err := tableExists(ctx, s, table)
		if err != nil {
			return nil, err
		}

		if present {
			standing = append(standing, table)
		}
	}

	return standing, nil
}

// legacyTableRowCounts counts the rows each standing legacy table holds, before any of them is dropped, so
// the account names a table with the rows it held while it stood.
func legacyTableRowCounts(ctx context.Context, r damageReader, stmts clearStatements, tables []string, section, operation string) ([]ports.DroppedTable, error) {
	counted := []ports.DroppedTable{}

	for _, table := range tables {
		count := fmt.Sprintf(stmts.countLegacyRow, table)

		var rows int64

		if err := r.queryRow(ctx, count).Scan(&rows); err != nil {
			return nil, statementError(section, operation, err, count)
		}

		counted = append(counted, ports.DroppedTable{Name: table, Rows: int(rows)})
	}

	return counted, nil
}

// migrationArchiveRows names every row the migration archive holds by where it came from, its identifier
// there, and the reason the migration stored beside it. What a row holds is never read: the contents column
// carries a call's encoded audio, and no account a person reads repeats it.
func migrationArchiveRows(ctx context.Context, r damageReader, stmts clearStatements, section, operation string) ([]ports.ArchivedRow, error) {
	rows, err := r.query(ctx, stmts.archiveRows)
	if err != nil {
		return nil, statementError(section, operation, err, stmts.archiveRows)
	}

	found := []ports.ArchivedRow{}

	for rows.Next() {
		var (
			tableName  string
			rowID      int64
			reason     string
			archivedAt int64
		)

		if err := rows.Scan(&tableName, &rowID, &reason, &archivedAt); err != nil {
			rows.Close()

			return nil, operationError(section, operation, err)
		}

		found = append(found, ports.ArchivedRow{
			Table:      tableName,
			RowID:      uint64(rowID),
			Reason:     reason,
			ArchivedAt: instantOf(archivedAt),
		})
	}

	failed := rows.Err()

	rows.Close()

	if failed != nil {
		return nil, operationError(section, operation, failed)
	}

	return found, nil
}

// damagedUnitRows reads every unit row and hands each one to the Domain predicate, rather than asking a
// backend to pick the damaged ones: the shape is a label read as the plain decimal form of its own number,
// and a backend asked to compare a text label with a number reads a number out of the text its own way. The
// rows are collected before any is removed, so the read is finished before the table changes under it.
func damagedUnitRows(ctx context.Context, r damageReader, stmts clearStatements, section, operation string) ([]domain.Unit, error) {
	rows, err := r.query(ctx, stmts.units)
	if err != nil {
		return nil, statementError(section, operation, err, stmts.units)
	}

	found := []domain.Unit{}

	for rows.Next() {
		var (
			unitID   sql.NullInt64
			label    sql.NullString
			unitRef  sql.NullInt64
			unitFrom sql.NullInt64
			unitTo   sql.NullInt64
			systemID sql.NullInt64
		)

		if err := rows.Scan(&unitID, &label, &unitRef, &unitFrom, &unitTo, &systemID); err != nil {
			rows.Close()

			return nil, operationError(section, operation, err)
		}

		// A row without an identifier is a row no delete can name, and a value that is not there is
		// not a reference a label was written as.
		if !unitID.Valid {
			continue
		}

		unit := domain.Unit{
			ID:       uint64(unitID.Int64),
			Label:    label.String,
			SystemID: uint64(systemID.Int64),
			UnitRef:  uint(unitRef.Int64),
			UnitFrom: uint(unitFrom.Int64),
			UnitTo:   uint(unitTo.Int64),
		}

		if unit.DamagedByMigration() {
			found = append(found, unit)
		}
	}

	failed := rows.Err()

	rows.Close()

	if failed != nil {
		return nil, operationError(section, operation, failed)
	}

	return found, nil
}
