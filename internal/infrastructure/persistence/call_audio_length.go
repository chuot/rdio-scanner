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
)

// The length of a call's recording, as a column of the calls table.
//
// Like the log source column before it, it is not something ApplySchema can do: the
// dialect schemas carry it for a database created here, and every one of ApplySchema's
// statements is `IF NOT EXISTS`, so over a `calls` table the legacy server wrote they
// change nothing — which is their contract. The column arrives in the migration pass
// that brings an existing database up to this shape.
//
// There is nothing to backfill: the default the column arrives with is zero, and zero is
// exactly what a call the legacy server wrote means — the length was never measured. The
// migration writes no length into a row it cannot know.
const (
	callAudioLengthColumnSqlite   = `ALTER TABLE "calls" ADD COLUMN "audioLength" integer NOT NULL DEFAULT 0`
	callAudioLengthColumnServer   = `ALTER TABLE "calls" ADD COLUMN "audioLength" bigint NOT NULL DEFAULT 0`
	callAudioLengthColumnPostgres = `ALTER TABLE "calls" ADD COLUMN "audioLength" bigint NOT NULL DEFAULT 0`
)

// ensureCallAudioLengthColumn gives the calls table the column that says how long each
// call's recording lasts.
//
// It is idempotent in the way every other step of the startup pass is: it asks the
// database what it already has and adds only what is missing. A `calls` table that is not
// there at all ends the step with nothing said and nothing done, which is how every other
// migration step answers the same question.
func ensureCallAudioLengthColumn(ctx context.Context, s store) error {
	columns, err := callTableColumns(ctx, s)
	if err != nil {
		return operationError("migration", "ensureCallAudioLength", err)
	}

	// No columns means no table, and a migration step is not the place to decide that a
	// table should exist.
	if len(columns) == 0 {
		return nil
	}

	if hasColumn(columns, "audioLength") {
		return nil
	}

	statement := callAudioLengthColumnSqlite

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		statement = callAudioLengthColumnServer

	case DialectPostgresql:
		statement = callAudioLengthColumnPostgres
	}

	if _, err := s.exec(ctx, statement); err != nil {
		return statementError("migration", "ensureCallAudioLength", err, statement)
	}

	return nil
}

// callTableColumns reads the columns the calls table has, from the database's own
// description of itself — the same probe the log source step runs, over the other table.
//
// A table that is not there answers with no rows on every dialect.
func callTableColumns(ctx context.Context, s store) ([]string, error) {
	query := `SELECT "name" FROM pragma_table_info("calls")`

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		query = `SELECT "COLUMN_NAME" FROM "information_schema"."COLUMNS" WHERE "TABLE_SCHEMA" = DATABASE() AND "TABLE_NAME" = 'calls'`

	case DialectPostgresql:
		query = `SELECT "column_name" FROM "information_schema"."columns" WHERE "table_name" = 'calls'`
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
