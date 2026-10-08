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
const (
	logSourceColumnSqlite     = `ALTER TABLE "logs" ADD COLUMN "source" text NOT NULL DEFAULT ''`
	logSourceColumnServer     = `ALTER TABLE "logs" ADD COLUMN "source" varchar(255) NOT NULL DEFAULT ''`
	logSourceColumnPostgres   = `ALTER TABLE "logs" ADD COLUMN "source" text NOT NULL DEFAULT ''`
	logSourceIndexBySource    = `CREATE INDEX "logs_source_idx" ON "logs" ("source", "level", "timestamp")`
	logSourceIndexByTimestamp = `CREATE INDEX "logs_time_idx" ON "logs" ("timestamp")`
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
	}

	for _, index := range []struct {
		name      string
		statement string
	}{
		{"logs_source_idx", logSourceIndexBySource},
		{"logs_time_idx", logSourceIndexByTimestamp},
	} {
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
		if err == sql.ErrNoRows {
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
