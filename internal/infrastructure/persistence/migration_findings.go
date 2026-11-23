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
	"fmt"

	"rdio-scanner/internal/domain"
)

// The migration findings record holds one row per finding this server has announced about a
// deployment, so that a later start knows it already said this one. Why the record lives in the
// migration pass, beside the archive, and not in the dialect schemas.
const (
	migrationFindingsTableSqlite = `CREATE TABLE IF NOT EXISTS "migrationFindings" (
    "findingId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "tableName" text NOT NULL,
    "rowId" integer NOT NULL,
    "saidAt" integer NOT NULL
  )`

	migrationFindingsTableServer = `CREATE TABLE IF NOT EXISTS "migrationFindings" (
    "findingId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "tableName" varchar(255) NOT NULL,
    "rowId" bigint NOT NULL,
    "saidAt" bigint NOT NULL
  )`

	migrationFindingsTablePostgres = `CREATE TABLE IF NOT EXISTS "migrationFindings" (
    "findingId" bigserial NOT NULL PRIMARY KEY,
    "tableName" text NOT NULL,
    "rowId" bigint NOT NULL,
    "saidAt" bigint NOT NULL
  )`

	// The unique index holds each finding exactly once across the later starts that re-read the same
	// deployment, and is probed rather than asked for because MySQL has no `CREATE INDEX IF NOT EXISTS`.
	migrationFindingsIndexSqlite   = `CREATE UNIQUE INDEX "migration_findings_table_row" ON "migrationFindings" ("tableName","rowId")`
	migrationFindingsIndexServer   = `CREATE UNIQUE INDEX "migration_findings_table_row" ON "migrationFindings" ("tableName","rowId")`
	migrationFindingsIndexPostgres = `CREATE UNIQUE INDEX "migration_findings_table_row" ON "migrationFindings" ("tableName","rowId")`
)

// ensureMigrationFindings gives the database the findings record and the index that holds each finding
// once, idempotent the way every other step of the startup pass is.
func ensureMigrationFindings(ctx context.Context, s store) error {
	table, index := migrationFindingsTableSqlite, migrationFindingsIndexSqlite

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		table, index = migrationFindingsTableServer, migrationFindingsIndexServer

	case DialectPostgresql:
		table, index = migrationFindingsTablePostgres, migrationFindingsIndexPostgres
	}

	if _, err := s.exec(ctx, table); err != nil {
		return statementError("migration", "ensureFindings", err, table)
	}

	has, err := hasMigrationFindingsIndex(ctx, s)
	if err != nil {
		return operationError("migration", "ensureFindings", err)
	}

	if has {
		return nil
	}

	if _, err := s.exec(ctx, index); err != nil {
		return statementError("migration", "ensureFindings", err, index)
	}

	return nil
}

// hasMigrationFindingsIndex asks the database whether it already has the record's unique index, the
// same question hasMigrationArchiveIndex asks of the archive table.
func hasMigrationFindingsIndex(ctx context.Context, s store) (bool, error) {
	query := `SELECT COUNT(*) FROM "sqlite_master" WHERE "type" = 'index' AND "tbl_name" = 'migrationFindings' AND "name" = 'migration_findings_table_row'`

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		query = `SELECT COUNT(DISTINCT "INDEX_NAME") FROM "information_schema"."STATISTICS" WHERE "TABLE_SCHEMA" = DATABASE() AND "TABLE_NAME" = 'migrationFindings' AND "INDEX_NAME" = 'migration_findings_table_row'`

	case DialectPostgresql:
		query = `SELECT COUNT(*) FROM "pg_indexes" WHERE "tablename" = 'migrationFindings' AND "indexname" = 'migration_findings_table_row'`
	}

	var count int

	if err := s.queryRow(ctx, query).Scan(&count); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, err
	}

	return count > 0, nil
}

// recordFinding writes one finding this start is about to announce and answers whether this start is
// the first to name it. The finding is keyed by what it names: the v6 table and 0 for the table itself,
// the v6 table and the row's own identity for a row left in the archive, and the live units table and
// the unit's identity for a unit left damaged. The write tolerates the pair already standing, which is
// how a later start meets a finding it announced before without disturbing the instant it was first
// said at.
func recordFinding(ctx context.Context, tx transaction, tableName string, rowId int64) (bool, error) {
	statement := findingsInsertStatement(tx.store.dialect)

	result, err := tx.exec(ctx, statement, tableName, rowId, tx.store.clock.Now().UnixMilli())
	if err != nil {
		return false, statementError("migration", "recordFinding", err, statement)
	}

	written, err := result.RowsAffected()
	if err != nil {
		return false, operationError("migration", "recordFinding", err)
	}

	return written > 0, nil
}

// findingsInsertStatement is the same write of one finding in the three dialects' own tolerance for a
// finding that is already on record.
func findingsInsertStatement(dialect Dialect) string {
	switch dialect {
	case DialectMariadb, DialectMysql:
		return `INSERT IGNORE INTO "migrationFindings" ("tableName", "rowId", "saidAt") VALUES (?, ?, ?)`

	case DialectPostgresql:
		return `INSERT INTO "migrationFindings" ("tableName", "rowId", "saidAt") VALUES (?, ?, ?) ON CONFLICT ("tableName", "rowId") DO NOTHING`

	default:
		return `INSERT OR IGNORE INTO "migrationFindings" ("tableName", "rowId", "saidAt") VALUES (?, ?, ?)`
	}
}

// forgetFindingsForTable clears everything the record knows about one table, the way dropping that
// table leaves nothing for a later start to meet. A finding about a table that is gone would stand
// forever as one no start can re-read.
func forgetFindingsForTable(ctx context.Context, tx transaction, table string) error {
	query := `DELETE FROM "migrationFindings" WHERE "tableName" = ?`

	if _, err := tx.exec(ctx, query, table); err != nil {
		return statementError("migration", "forgetFindings", err, query)
	}

	return nil
}

// sayFinding writes one finding the pass is about to announce and says the sentence only for a finding
// the record did not already hold, so a start that meets the same standing thing it reported before
// says nothing. The write runs in the stage's own transaction, which is what makes the announcement and
// the record commit together: a pass that printed a sentence and never committed leaves nothing on
// record, and its successor says the finding again.
//
// A record that cannot be written says the finding anyway, with its own failure named. A finding is
// never hidden by the record's failure.
func sayFinding(ctx context.Context, tx transaction, progress Progress, level, table string, rowId int64, sentence string) {
	first, err := recordFinding(ctx, tx, table, rowId)
	if err != nil {
		progress(domain.LogLevelWarn, fmt.Sprintf("the migration could not record what it found: %s", err.Error()))
		progress(level, sentence)

		return
	}

	if first {
		progress(level, sentence)
	}
}
