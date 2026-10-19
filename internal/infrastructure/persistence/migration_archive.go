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
	"errors"
	"fmt"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The migration archive is the table that holds every v6 row the migration pass read and could not
// carry, so that dropping a v6 table never destroys a row quietly.
//
// It lives in the migration pass and not in the dialect schemas for the reason recorded for
// the log indexes:
// ApplySchema's statements are the legacy server's, and a database the other server wrote has to come
// out of them exactly as it went in — a new table included. The step that owns the archive runs after
// the schema and before any v6 table is read, so every database this server starts on gains it, fresh
// or inherited, and the legacy server simply never sees a table it does not know.
//
// MySQL is the reason the table has three spellings again: it wants a sized column where the index
// reaches (tableName), and a TEXT column in a unique index needs a prefix length it would rather not
// have to state. The sized column is the only difference that matters; the row means the same thing
// on every backend.
const (
	migrationArchiveTableSqlite = `CREATE TABLE IF NOT EXISTS "migrationArchive" (
    "archiveId" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
    "tableName" text NOT NULL,
    "rowId" integer NOT NULL,
    "reason" text NOT NULL,
    "contents" text NOT NULL,
    "archivedAt" integer NOT NULL
  )`

	migrationArchiveTableServer = `CREATE TABLE IF NOT EXISTS "migrationArchive" (
    "archiveId" bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    "tableName" varchar(255) NOT NULL,
    "rowId" bigint NOT NULL,
    "reason" text NOT NULL,
    "contents" text NOT NULL,
    "archivedAt" bigint NOT NULL
  )`

	migrationArchiveTablePostgres = `CREATE TABLE IF NOT EXISTS "migrationArchive" (
    "archiveId" bigserial NOT NULL PRIMARY KEY,
    "tableName" text NOT NULL,
    "rowId" bigint NOT NULL,
    "reason" text NOT NULL,
    "contents" text NOT NULL,
    "archivedAt" bigint NOT NULL
  )`

	// The unique index is what holds each left-behind row exactly once: a v6 table that survived
	// because a row of it could not be read is re-read on the next start, and its already-archived
	// rows archive again as nothing. It is probed rather than asked for, because MySQL has no
	// `CREATE INDEX IF NOT EXISTS` and the same lines have to work on all four backends.
	migrationArchiveIndexSqlite   = `CREATE UNIQUE INDEX "migration_archive_table_row" ON "migrationArchive" ("tableName","rowId")`
	migrationArchiveIndexServer   = `CREATE UNIQUE INDEX "migration_archive_table_row" ON "migrationArchive" ("tableName","rowId")`
	migrationArchiveIndexPostgres = `CREATE UNIQUE INDEX "migration_archive_table_row" ON "migrationArchive" ("tableName","rowId")`
)

// ensureMigrationArchive gives the database the table the pass archives into.
//
// It is idempotent the way every other step of the startup pass is: the table is asked for with
// `IF NOT EXISTS`, and the index is probed first, so a start after a start adds nothing.
func ensureMigrationArchive(ctx context.Context, s store) error {
	table, index := migrationArchiveTableSqlite, migrationArchiveIndexSqlite

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		table, index = migrationArchiveTableServer, migrationArchiveIndexServer

	case DialectPostgresql:
		table, index = migrationArchiveTablePostgres, migrationArchiveIndexPostgres
	}

	if _, err := s.exec(ctx, table); err != nil {
		return statementError("migration", "ensureArchive", err, table)
	}

	has, err := hasMigrationArchiveIndex(ctx, s)
	if err != nil {
		return operationError("migration", "ensureArchive", err)
	}

	if has {
		return nil
	}

	if _, err := s.exec(ctx, index); err != nil {
		return statementError("migration", "ensureArchive", err, index)
	}

	return nil
}

// hasMigrationArchiveIndex asks the database whether it already has the archive's unique index, the
// same question hasLogIndex asks of the log table.
func hasMigrationArchiveIndex(ctx context.Context, s store) (bool, error) {
	query := `SELECT COUNT(*) FROM "sqlite_master" WHERE "type" = 'index' AND "tbl_name" = 'migrationArchive' AND "name" = 'migration_archive_table_row'`

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		query = `SELECT COUNT(DISTINCT "INDEX_NAME") FROM "information_schema"."STATISTICS" WHERE "TABLE_SCHEMA" = DATABASE() AND "TABLE_NAME" = 'migrationArchive' AND "INDEX_NAME" = 'migration_archive_table_row'`

	case DialectPostgresql:
		query = `SELECT COUNT(*) FROM "pg_indexes" WHERE "tablename" = 'migrationArchive' AND "indexname" = 'migration_archive_table_row'`
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

// archiveRow writes one row the pass read and could not carry into the archive, with the reason in
// the language of the system and the original values as the pass read them.
//
// The insert is conflict-tolerant against the (tableName, rowId) index on purpose: a v6 table that
// survived because a row of it could not be read is re-read on every later start, and its
// already-archived rows archive again as nothing rather than colliding. A failure the archive itself
// refuses — a missing table, a value that will not bind — is returned, not swallowed: quietly losing
// the record of a lost row is the exact defect this feature exists to end.
func archiveRow(ctx context.Context, tx transaction, tableName string, rowId int64, reason string, contents any) error {
	statement := archiveInsertStatement(tx.store.dialect)

	if _, err := tx.exec(ctx, statement, tableName, rowId, reason, archiveContents(contents), tx.store.clock.Now().UnixMilli()); err != nil {
		return statementError("migration", "archiveRow", err, statement)
	}

	return nil
}

// archiveInsertStatement is the same insert in the three dialects' own tolerance for a row that is
// already there.
func archiveInsertStatement(dialect Dialect) string {
	columns := `INSERT INTO "migrationArchive" ("tableName", "rowId", "reason", "contents", "archivedAt") VALUES (?, ?, ?, ?, ?)`

	switch dialect {
	case DialectMariadb, DialectMysql:
		return `INSERT IGNORE INTO "migrationArchive" ("tableName", "rowId", "reason", "contents", "archivedAt") VALUES (?, ?, ?, ?, ?)`

	case DialectPostgresql:
		return columns + ` ON CONFLICT ("tableName", "rowId") DO NOTHING`

	default:
		return `INSERT OR IGNORE INTO "migrationArchive" ("tableName", "rowId", "reason", "contents", "archivedAt") VALUES (?, ?, ?, ?, ?)`
	}
}

// archiveContents is the original row as JSON, exactly as the pass read it. Binary columns — the v6
// call's audio is the one that matters — arrive base64-encoded, which is what makes an archived call
// recoverable from the contents column alone.
func archiveContents(row any) string {
	b, err := json.Marshal(row)
	if err != nil {
		// A value this package cannot marshal is a value the pass read as something no JSON writer
		// can name. Saying what it looked like beats losing it.
		return fmt.Sprintf("%q", fmt.Sprintf("%v", row))
	}

	return string(b)
}

// migrationArchiver is one migration's record of the rows it read and could not carry.
//
// Every row goes into the archive table inside the transaction the migration is already in, so that the
// record of a lost row commits or rolls back together with the rows the migration carried — which is the
// reason the archive is written by the pass rather than after it.
//
// The lines are held until the table's rows have all been read, because the summary names a count the
// migration only knows at the end, and an operator reading the log needs the count before the list: the
// shape the startup damage report uses.
type migrationArchiver struct {
	ctx      context.Context
	tx       transaction
	progress Progress
	table    string
	one      string
	many     string
	behind   []string
}

// archiving opens the archive for one migration's v6 table. `one` and `many` are what this server calls
// the rows of that table, singular and plural, because the summary line says how many there are.
func archiving(ctx context.Context, tx transaction, progress Progress, table, one, many string) *migrationArchiver {
	return &migrationArchiver{ctx: ctx, tx: tx, progress: progress, table: table, one: one, many: many}
}

// skip keeps the record of one v6 row the migration read and could not carry: the row goes into the
// archive with the reason the migration has for it, and the same reason is held for the operator at warn
// level so that the log view names it.
//
// `named` is the row in the language of the system — `talkgroup 204 "Orphan"`, `call 705`,
// `the config row "options"` — and `reason` is the tail of that sentence, which is also what the
// archive's reason column holds.
//
// An archive the database itself refuses is returned rather than reported and carried on. The
// alternative is the defect this pass exists to end: the v6 table dropped with the row in it and no
// record left behind. Returning rolls the table's transaction back, so the v6 table survives intact,
// the pass stops, and the server says why it will not start.
func (a *migrationArchiver) skip(rowId int64, named, reason string, contents any) error {
	if err := archiveRow(a.ctx, a.tx, a.table, rowId, reason, contents); err != nil {
		return err
	}

	a.behind = append(a.behind, fmt.Sprintf("the migration could not carry %s: %s", named, reason))

	return nil
}

// report says what the migration left in the archive: one line naming the count and where the rows are,
// then one line per row an operator can go and look at. It says nothing at all when there is nothing to
// say.
func (a *migrationArchiver) report() {
	if len(a.behind) == 0 {
		return
	}

	if len(a.behind) == 1 {
		a.progress(domain.LogLevelWarn, fmt.Sprintf("the migration could not carry 1 %s: it is kept in the migrationArchive table", a.one))
	} else {
		a.progress(domain.LogLevelWarn, fmt.Sprintf("the migration could not carry %d %s: they are kept in the migrationArchive table", len(a.behind), a.many))
	}

	for _, line := range a.behind {
		a.progress(domain.LogLevelWarn, line)
	}
}

// namedRow is a row in the language of the system: what kind of thing it is, the identity its v6 row
// was read for, and the name it was given when it has one. It is the subject of every sentence the
// pass says about a row it could not carry.
func namedRow(kind string, id int64, name sql.NullString) string {
	if !name.Valid || name.String == "" {
		return fmt.Sprintf("%s %d", kind, id)
	}

	return fmt.Sprintf("%s %d %q", kind, id, name.String)
}

// v6Int, v6Int32, v6Text and v6Time turn the nullable columns the pass scanned back into the JSON of
// the row as it was read: a column the v6 row left NULL comes back null, so an archived row says what
// the deployment's row actually held rather than what this server would have written for it.
func v6Int(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}

	return v.Int64
}

func v6Int32(v sql.NullInt32) any {
	if !v.Valid {
		return nil
	}

	return int64(v.Int32)
}

func v6Text(v sql.NullString) any {
	if !v.Valid {
		return nil
	}

	return v.String
}

func v6Time(v sql.NullTime) any {
	if !v.Valid {
		return nil
	}

	return v.Time
}

// v6Bool is a v6 flag column as the pass read it. The v6 tables store these as tinyint and the
// drivers hand back a bool, so an archived row says whether the row was disabled, not which digit it
// used.
func v6Bool(v sql.NullBool) any {
	if !v.Valid {
		return nil
	}

	return v.Bool
}

// v6Audio is a call's audio as the archive holds it: the bytes themselves, which JSON writes
// base64-encoded, so that a call this server could not carry can be played again from the contents
// column alone.
func v6Audio(v sql.NullString) any {
	if !v.Valid {
		return nil
	}

	return []byte(v.String)
}

// wallClock is what the migration falls back to when its caller hands it no clock at all. The server
// always hands it its own; the fallback exists so that a caller that neither injects a clock nor
// archives anything — every test of the pass that predates the archive — keeps working.
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

var _ ports.Clock = wallClock{}
