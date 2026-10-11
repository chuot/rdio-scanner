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
	"errors"
	"sort"

	"rdio-scanner/internal/domain"
)

// The log source column and the two indexes the log search runs against.
//
// They are not in the dialect schemas' `logs` statement for the indexes, and not only there for the
// column. ApplySchema's statements are the legacy server's, every one of them `IF NOT EXISTS`, and its
// contract is that a database the other server wrote comes out exactly as it went in — which
// TestApplySchemaLeavesALegacyWrittenDatabaseExactlyAsItFoundIt checks object by object, indexes included.
// So everything this feature changes about a database that already exists happens here, in the migration
// pass, after the schema has been applied and before any v6 table is read.
//
// MySQL is the reason the column has two spellings: it refuses a default on a TEXT column, and the default
// is the part that lets the legacy server keep inserting into this table without knowing the column exists.
// It is the reason the index by source has two as well, for the opposite trouble with the same column.
const (
	logSourceColumnSqlite     = `ALTER TABLE "logs" ADD COLUMN "source" text NOT NULL DEFAULT ''`
	logSourceColumnServer     = `ALTER TABLE "logs" ADD COLUMN "source" varchar(255) NOT NULL DEFAULT ''`
	logSourceColumnPostgres   = `ALTER TABLE "logs" ADD COLUMN "source" text NOT NULL DEFAULT ''`
	logSourceIndexBySource    = `CREATE INDEX "logs_source_idx" ON "logs" ("source", "level", "timestamp")`
	logSourceIndexByTimestamp = `CREATE INDEX "logs_time_idx" ON "logs" ("timestamp")`

	// logSourceIndexBySourceServer is the same index spelled for the backend that cannot build the other
	// one. `level` is a TEXT column there, and MySQL only indexes a prefix of a TEXT column: asked for
	// the whole thing it refuses the statement — "Specified key was too long; max key length is 3072
	// bytes" — and a deployment on MariaDB ends its startup in this step. The prefix is the index the log
	// search needs, because a level is one of a handful of short words and never the long part of a row.
	logSourceIndexBySourceServer = `CREATE INDEX "logs_source_idx" ON "logs" ("source", "level"(255), "timestamp")`
)

// ensureLogSourceColumn gives the log table the column that says which part of the server wrote each
// entry, and the indexes the log search is written against.
//
// It is idempotent in the way every other step of the startup pass is: it asks the database what it
// already has and adds only what is missing. A `logs` table that is not there at all ends the step with
// nothing said and nothing done, which is how each v6 migration already answers the same question.
func ensureLogSourceColumn(ctx context.Context, s store) error {
	columns, err := logTableColumns(ctx, s)
	if err != nil {
		return operationError("migration", "ensureLogSource", err)
	}

	// No columns means no table. This server creates the table in ApplySchema, so the case this reaches
	// is a database that was opened before its schema was applied — and a migration step is not the place
	// to decide that a table should exist.
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

	// A missing index is the sign of a pass that died before creating it — and of
	// a backfill that died halfway (CA-012.2). The fill used to belong to the run
	// that added the column and to no other, so the rows that run never reached
	// stayed nameless for good, and the deployment's log view kept showing blank
	// rows no filter could ever find.
	//
	// The fill belongs to the run that adds the column and to the runs that find
	// the indexes missing, and to no other run. A deployment's log is read and
	// filtered for months, and re-deciding on every start what an old entry meant
	// is not something an operator can rely on — least of all when the operator,
	// or a legacy server sharing the database, has already re-filed a row by hand.
	// A row that already names a source keeps it, which is what makes running the
	// fill again over a complete log change nothing.
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

		// MySQL is the dialect that has no `CREATE INDEX IF NOT EXISTS`, which is why the index is probed
		// rather than asked for, and why the same three lines work on all four backends.
		if _, err := s.exec(ctx, index.statement); err != nil {
			return statementError("migration", "ensureLogSource", err, index.statement)
		}
	}

	return nil
}

// logTableColumns reads the columns the log table has, from the database's own description of itself.
//
// A table that is not there answers with no rows on every dialect: SQLite's pragma over a missing table is
// an empty result rather than an error, and the two catalog queries simply match nothing.
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
// that wrote it. It is the inventory of every prefix the server has ever written, and it is only
// ever applied to rows written before the column existed.
//
// Two of its rules are the ones that matter. Longer prefixes are applied before shorter ones that are a
// prefix of them, so downstream.send: is a downstream line and not something else. And a message that maps
// to nothing is filed under legacy rather than left blank: an operator filtering by subsystem on a database
// that predates this feature has to be able to tell "this happened before the log knew" from "nothing
// happened".
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
