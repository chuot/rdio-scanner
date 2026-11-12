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
	"time"

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
// from this package's own list, not from anything the database answers.
type clearStatements struct {
	units           string
	deleteUnit      string
	archiveRows     string
	dropArchive     string
	countLegacyRow  string
	dropLegacyTable string
}

// Maintenance is the adapter that clears the records a legacy migration left in problem: the unit rows
// the migration created rather than a deployment named, the migration archive table, and the v6 tables
// the migration left standing. The operator's one-shot maintenance switch builds and runs it once; no
// start, scheduled pass, or endpoint reaches it.
type Maintenance struct {
	store store

	statements clearStatements
}

// NewMaintenance builds the clearing for one opened database.
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
	legacy := []string{}

	for _, table := range v6MigrationTables {
		present, err := tableExists(ctx, m.store, table)
		if err != nil {
			return report, operationError(section, operation, err)
		}

		if present {
			legacy = append(legacy, table)
		}
	}

	archived, err := tableExists(ctx, m.store, migrationArchiveTable)
	if err != nil {
		return report, operationError(section, operation, err)
	}

	tx, err := m.store.begin(ctx)
	if err != nil {
		return report, operationError(section, operation, err)
	}

	for _, table := range legacy {
		count := fmt.Sprintf(m.statements.countLegacyRow, table)

		var rows int64

		if err := tx.queryRow(ctx, count).Scan(&rows); err != nil {
			tx.rollback()

			return report, statementError(section, operation, err, count)
		}

		drop := fmt.Sprintf(m.statements.dropLegacyTable, table)

		if _, err := tx.exec(ctx, drop); err != nil {
			tx.rollback()

			return report, statementError(section, operation, err, drop)
		}

		report.DroppedTables = append(report.DroppedTables, ports.DroppedTable{Name: table, Rows: int(rows)})
	}

	if archived {
		rows, err := tx.query(ctx, m.statements.archiveRows)
		if err != nil {
			tx.rollback()

			return report, statementError(section, operation, err, m.statements.archiveRows)
		}

		for rows.Next() {
			var (
				tableName  string
				rowID      int64
				reason     string
				archivedAt int64
			)

			if err := rows.Scan(&tableName, &rowID, &reason, &archivedAt); err != nil {
				rows.Close()
				tx.rollback()

				return report, operationError(section, operation, err)
			}

			report.ArchiveRows = append(report.ArchiveRows, ports.ArchivedRow{
				Table:      tableName,
				RowID:      uint64(rowID),
				Reason:     reason,
				ArchivedAt: time.UnixMilli(archivedAt),
			})
		}

		failed := rows.Err()

		rows.Close()

		if failed != nil {
			tx.rollback()

			return report, operationError(section, operation, failed)
		}

		if _, err := tx.exec(ctx, m.statements.dropArchive); err != nil {
			tx.rollback()

			return report, statementError(section, operation, err, m.statements.dropArchive)
		}

		report.DroppedArchive = true
	}

	// Every unit row is read and each one handed to the predicate, rather than a selection the
	// database makes: the shape the predicate names is a label read as the plain decimal form of its
	// own number, and a backend asked to compare a text label with a number reads a number out of the
	// text its own way. The rows are collected before any is deleted, so the read is finished before
	// the table changes under it.
	units, err := tx.query(ctx, m.statements.units)
	if err != nil {
		tx.rollback()

		return report, statementError(section, operation, err, m.statements.units)
	}

	damaged := []domain.Unit{}

	for units.Next() {
		var (
			unitID   sql.NullInt64
			label    sql.NullString
			unitRef  sql.NullInt64
			unitFrom sql.NullInt64
			unitTo   sql.NullInt64
			systemID sql.NullInt64
		)

		if err := units.Scan(&unitID, &label, &unitRef, &unitFrom, &unitTo, &systemID); err != nil {
			units.Close()
			tx.rollback()

			return report, operationError(section, operation, err)
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
			damaged = append(damaged, unit)
		}
	}

	failed := units.Err()

	units.Close()

	if failed != nil {
		tx.rollback()

		return report, operationError(section, operation, failed)
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
