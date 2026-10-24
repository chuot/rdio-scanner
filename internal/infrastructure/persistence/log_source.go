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
	"sort"

	"rdio-scanner/internal/domain"
)

// The log source column and the two indexes the log search runs against. All
// three arrive through the migration pass, not the dialect schemas: the schema
// statements are the legacy server's, and a database the other server wrote
// has to come out of this server exactly as it went in, so everything that
// changes a database that already exists happens here, after the schema has
// been applied and before any v6 table is read. MySQL is why the column and
// the source index each have two spellings, and the migration-pass posture is
// the log-source migration pass maintainer note.
const (
	logSourceColumnSqlite     = `ALTER TABLE "logs" ADD COLUMN "source" text NOT NULL DEFAULT ''`
	logSourceColumnServer     = `ALTER TABLE "logs" ADD COLUMN "source" varchar(255) NOT NULL DEFAULT ''`
	logSourceColumnPostgres   = `ALTER TABLE "logs" ADD COLUMN "source" text NOT NULL DEFAULT ''`
	logSourceIndexBySource    = `CREATE INDEX "logs_source_idx" ON "logs" ("source", "level", "timestamp")`
	logSourceIndexByTimestamp = `CREATE INDEX "logs_time_idx" ON "logs" ("timestamp")`

	// logSourceIndexBySourceServer is the same index spelled for the backend
	// that cannot build the other one: `level` is a TEXT column there, and
	// MySQL indexes only a prefix of a TEXT column, so this spelling names the
	// prefix that keeps the statement buildable. A level is one of a handful
	// of short words, so that prefix is the index the log search needs.
	logSourceIndexBySourceServer = `CREATE INDEX "logs_source_idx" ON "logs" ("source", "level"(255), "timestamp")`
)

// ensureLogSourceColumn gives the log table the column that says which part of the server wrote each
// entry, and the indexes the log search is written against. The step is idempotent like every other
// step of the startup pass: it asks what the database already has and adds only what is missing, and
// a `logs` table that is not there at all ends the step with nothing said and nothing done.
func ensureLogSourceColumn(ctx context.Context, s store) error {
	columns, err := logTableColumns(ctx, s)
	if err != nil {
		return operationError("migration", "ensureLogSource", err)
	}

	// No columns means no table: this server creates the table in ApplySchema,
	// and a migration step is not the place to decide that a table should exist.
	if len(columns) == 0 {
		return nil
	}

	needsBackfill := false

	if !hasColumn(columns, "source") {
		statement := logSourceColumnSqlite

		switch s.dialect {
		case DialectMariadb, DialectMysql:
			statement = logSourceColumnServer

		case DialectPostgresql:
			statement = logSourceColumnPostgres
		}

		if _, err := s.exec(ctx, statement); err != nil {
			return statementError("migration", "ensureLogSource", err, statement)
		}

		needsBackfill = true
	}

	bySource := logSourceIndexBySource

	if s.dialect == DialectMariadb || s.dialect == DialectMysql {
		bySource = logSourceIndexBySourceServer
	}

	indexes := []struct {
		name      string
		statement string
	}{
		{"logs_source_idx", bySource},
		{"logs_time_idx", logSourceIndexByTimestamp},
	}

	// A missing index is the sign of a pass that died before creating it — and
	// of a backfill that died halfway. The fill belongs to the run that adds
	// the column and to the runs that find the indexes missing, and to no
	// other run; it files only rows that still name no source, so a row an
	// operator or a legacy server already re-filed keeps what it was given.
	// Why the fill belongs to no other run is the log-source backfill
	// maintainer note.
	missing := false

	for _, index := range indexes {
		created, err := hasLogIndex(ctx, s, index.name)
		if err != nil {
			return operationError("migration", "ensureLogSource", err)
		}

		if !created {
			missing = true
		}
	}

	if needsBackfill || missing {
		if err := backfillLogSources(ctx, s); err != nil {
			return err
		}
	}

	for _, index := range indexes {
		created, err := hasLogIndex(ctx, s, index.name)
		if err != nil {
			return operationError("migration", "ensureLogSource", err)
		}

		if created {
			continue
		}

		// MySQL has no `CREATE INDEX IF NOT EXISTS`, so the index is probed rather
		// than asked for, which is what lets the same lines work on all backends.
		if _, err := s.exec(ctx, index.statement); err != nil {
			return statementError("migration", "ensureLogSource", err, index.statement)
		}
	}

	return nil
}

// logTableColumns reads the columns the log table has, from the database's own description of
// itself. A table that is not there answers with no rows on every dialect.
func logTableColumns(ctx context.Context, s store) ([]string, error) {
	query := `SELECT "name" FROM pragma_table_info("logs")`

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		query = `SELECT "COLUMN_NAME" FROM "information_schema"."COLUMNS" WHERE "TABLE_SCHEMA" = DATABASE() AND "TABLE_NAME" = 'logs'`

	case DialectPostgresql:
		query = `SELECT "column_name" FROM "information_schema"."columns" WHERE "table_name" = 'logs'`
	}

	rows, err := s.query(ctx, query)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	names := []string{}

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			return nil, err
		}

		names = append(names, name)
	}

	return names, rows.Err()
}

// hasLogIndex asks the database whether it already has one of the log indexes.
func hasLogIndex(ctx context.Context, s store, name string) (bool, error) {
	query := `SELECT COUNT(*) FROM "sqlite_master" WHERE "type" = 'index' AND "tbl_name" = 'logs' AND "name" = ?`

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		query = `SELECT COUNT(DISTINCT "INDEX_NAME") FROM "information_schema"."STATISTICS" WHERE "TABLE_SCHEMA" = DATABASE() AND "TABLE_NAME" = 'logs' AND "INDEX_NAME" = ?`

	case DialectPostgresql:
		query = `SELECT COUNT(*) FROM "pg_indexes" WHERE "tablename" = 'logs' AND "indexname" = ?`
	}

	var count int

	if err := s.queryRow(ctx, query, name).Scan(&count); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, err
	}

	return count > 0, nil
}

func hasColumn(columns []string, want string) bool {
	for _, column := range columns {
		if column == want {
			return true
		}
	}

	return false
}

// logSourceBackfill is the mapping from the prefix an existing message carries to the part of the server
// that wrote it, applied only to rows written before the column existed. Longer prefixes are applied
// before shorter ones that are a prefix of them, and a message that maps to nothing is filed under
// legacy rather than left blank: an operator filtering an old database has to be able to tell "this
// happened before the log knew" from "nothing happened".
var logSourceBackfill = []struct {
	prefix string
	source domain.LogSource
}{
	{"admin.confighandler.put:", domain.LogSourceConfig},
	{"admin.sectionhandler.put:", domain.LogSourceConfig},
	{"admin.passwordhandler.post:", domain.LogSourceAdmin},
	{"admin.useraddhandler.post:", domain.LogSourceAdmin},
	{"admin.userremovehandler.post:", domain.LogSourceAdmin},
	{"controller.processmessage.commandlistcall:", domain.LogSourceLivefeed},
	{"controller.processmessage.commandpin:", domain.LogSourceLivefeed},
	{"controller.ingestcall:", domain.LogSourceIngest},
	{"client.message.fromjson:", domain.LogSourceLivefeed},
	{"client.message.tojson:", domain.LogSourceLivefeed},
	{"client.processmessage:", domain.LogSourceLivefeed},
	{"websocket.upgrade:", domain.LogSourceLivefeed},
	{"listeners count is ", domain.LogSourceLivefeed},
	{"new listener from ", domain.LogSourceLivefeed},
	{"listener disconnected from ", domain.LogSourceLivefeed},
	{"too many concurrent connections for ident ", domain.LogSourceAccess},
	{"too many login attempts", domain.LogSourceLogin},
	{"invalid login attempt", domain.LogSourceLogin},
	{"invalid access code ", domain.LogSourceAccess},
	{"locked access for ident ", domain.LogSourceAccess},
	{"expired access for ident ", domain.LogSourceAccess},
	{"tokens.issue:", domain.LogSourceLogin},
	{"tokens.validate:", domain.LogSourceLogin},
	{"passwords.hash:", domain.LogSourceAdmin},
	{"accesses.read:", domain.LogSourceAccess},
	{"accesses.write:", domain.LogSourceAccess},
	{"apikeys.read:", domain.LogSourceConfig},
	{"apikeys.write:", domain.LogSourceConfig},
	{"api:", domain.LogSourceUpload},
	{"calls.checkduplicate:", domain.LogSourceCalls},
	{"calls.getcall:", domain.LogSourceCalls},
	{"calls.prune:", domain.LogSourceCalls},
	{"calls.search:", domain.LogSourceCalls},
	{"calls.writecall:", domain.LogSourceCalls},
	{"database.migrate:", domain.LogSourceDatabase},
	{"database.open:", domain.LogSourceDatabase},
	{"database.seed:", domain.LogSourceDatabase},
	{"migration.damage:", domain.LogSourceDatabase},
	{"migration.", domain.LogSourceDatabase},
	{"migrating ", domain.LogSourceDatabase},
	{"schema.apply:", domain.LogSourceDatabase},
	{"delayer.delay:", domain.LogSourceDelayer},
	{"delayer.pop:", domain.LogSourceDelayer},
	{"delayer.push:", domain.LogSourceDelayer},
	{"delayer.restore:", domain.LogSourceDelayer},
	{"dirwatch.ingest:", domain.LogSourceDirwatch},
	{"dirwatch.watcher:", domain.LogSourceDirwatch},
	{"dirwatch.walkdir:", domain.LogSourceDirwatch},
	{"dirwatches.read:", domain.LogSourceDirwatch},
	{"dirwatches.start:", domain.LogSourceDirwatch},
	{"dirwatches.write:", domain.LogSourceDirwatch},
	{"downstream.send:", domain.LogSourceDownstream},
	{"downstream:", domain.LogSourceDownstream},
	{"groups.read:", domain.LogSourceConfig},
	{"groups.write:", domain.LogSourceConfig},
	{"logs.logevent:", domain.LogSourceLogs},
	{"logs.prune:", domain.LogSourceLogs},
	{"logs.search:", domain.LogSourceLogs},
	{"newcall:", domain.LogSourceIngest},
	{"options.read:", domain.LogSourceConfig},
	{"options.write:", domain.LogSourceConfig},
	{"systems.read:", domain.LogSourceConfig},
	{"systems.write:", domain.LogSourceConfig},
	{"tags.read:", domain.LogSourceConfig},
	{"tags.write:", domain.LogSourceConfig},
	{"talkgroups.read:", domain.LogSourceConfig},
	{"talkgroups.writetx:", domain.LogSourceConfig},
	{"units.read:", domain.LogSourceConfig},
	{"units.writetx:", domain.LogSourceConfig},
	{"sites.read:", domain.LogSourceConfig},
	{"sites.writetx:", domain.LogSourceConfig},
	{"configuration changed", domain.LogSourceConfig},
	{"database pruning", domain.LogSourcePrune},
	{"server started", domain.LogSourceServer},
	{"admin password changed.", domain.LogSourceAdmin},
	{"ffmpeg:", domain.LogSourceFFmpeg},
	{"ffmpeg is not available", domain.LogSourceFFmpeg},
}

// backfillLogSources files every entry the log table already holds under the part of the server that wrote
// it, reading the prefix its message carries.
//
// Each mapping is one statement, and the comparison is substr(message, 1, length(prefix)) = prefix rather
// than LIKE: the same exact match, with nothing in a message that happens to hold a % or an _ needing to be
// escaped first.
func backfillLogSources(ctx context.Context, s store) error {
	mapping := make([]struct {
		prefix string
		source domain.LogSource
	}, len(logSourceBackfill))

	copy(mapping, logSourceBackfill)

	sort.SliceStable(mapping, func(i, j int) bool {
		return len(mapping[i].prefix) > len(mapping[j].prefix)
	})

	query := `UPDATE "logs" SET "source" = ? WHERE "source" = '' AND substr("message", 1, length(?)) = ?`

	for _, entry := range mapping {
		if _, err := s.exec(ctx, query, string(entry.source), entry.prefix, entry.prefix); err != nil {
			return statementError("migration", "backfillLogSources", err, query)
		}
	}

	// Whatever is still unfiled is old, and its prefix meant nothing recognisable — or it was written by a
	// server that wrote no prefix at all.
	legacy := `UPDATE "logs" SET "source" = ? WHERE "source" = ''`

	if _, err := s.exec(ctx, legacy, string(domain.LogSourceLegacy)); err != nil {
		return statementError("migration", "backfillLogSources", err, legacy)
	}

	return nil
}
