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
)

// The index the history filter's predicates are written against, and the statement that
// gives it to a backend that does not have it. The SQLite and PostgreSQL schema copies
// create it with `IF NOT EXISTS`, a spelling those two accept; MySQL has no such spelling
// — it belongs to MariaDB, which took it on in 10.1.4 — and the schema copy is applied on
// every start, so the copied statement is out of the MySQL copy and this step carries the
// index for every backend instead, the way the log indexes and the options key index are
// carried. The column order is the one the search filter's statements are written
// against, per the search filter index shape maintainer note.
const callsSearchIndexCreate = `CREATE INDEX "calls_idx" ON "calls" ("systemId","siteRef","talkgroupId","timestamp")`

// ensureCallsSearchIndex gives the calls table the search index, idempotent the way every
// other step of the startup pass is: it asks the database's own catalog whether the index
// is there and creates it only when it is not, and a `calls` table that is not there ends
// the step doing nothing — a migration step is not the place to decide that a table
// should exist.
func ensureCallsSearchIndex(ctx context.Context, s store) error {
	const section, operation = "migration", "ensureCallsSearchIndex"

	exists, err := tableExists(ctx, s, "calls")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	carries, err := hasCallsSearchIndex(ctx, s)
	if err != nil {
		return operationError(section, operation, err)
	}

	if carries {
		return nil
	}

	if _, err := s.exec(ctx, callsSearchIndexCreate); err != nil {
		return statementError(section, operation, err, callsSearchIndexCreate)
	}

	return nil
}

// hasCallsSearchIndex asks the database's own catalog whether the calls table already
// carries the search index — the same probe the log-source and options-key steps run,
// over the calls table.
func hasCallsSearchIndex(ctx context.Context, s store) (bool, error) {
	query := `SELECT COUNT(*) FROM "sqlite_master" WHERE "type" = 'index' AND "tbl_name" = 'calls' AND "name" = ?`

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		query = `SELECT COUNT(DISTINCT "INDEX_NAME") FROM "information_schema"."STATISTICS" WHERE "TABLE_SCHEMA" = DATABASE() AND "TABLE_NAME" = 'calls' AND "INDEX_NAME" = ?`

	case DialectPostgresql:
		query = `SELECT COUNT(*) FROM "pg_indexes" WHERE "tablename" = 'calls' AND "indexname" = ?`
	}

	var count int

	if err := s.queryRow(ctx, query, "calls_idx").Scan(&count); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, err
	}

	return count > 0, nil
}
