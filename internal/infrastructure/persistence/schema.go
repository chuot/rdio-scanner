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
)

// schemaFor returns the statements the legacy server applies for a dialect.
func schemaFor(dialect Dialect) ([]string, error) {
	switch dialect {
	case DialectSqlite:
		return sqliteSchema, nil

	case DialectMariadb, DialectMysql:
		return mysqlSchema, nil

	case DialectPostgresql:
		return postgresSchema, nil

	default:
		// The legacy server reaches this from a database type its own switch already rejected,
		// and answers "no database schema". The wording is kept because it is what a deployment
		// would have seen.
		return nil, fmt.Errorf("no database schema")
	}
}

// ApplySchema creates the tables the server works with, on a database that may already have them.
//
// It is safe to run on every start, which is what lets this server open a database the legacy
// server created — and lets the legacy server open one this server wrote — without either of them
// changing the other's schema. Every statement is `IF NOT EXISTS`, so a database that already has
// the tables is left exactly as it was, down to the definitions themselves.
//
// The statements are applied in one transaction, as the legacy server does. On MySQL and MariaDB
// that is a formality rather than a guarantee: a data-definition statement commits the transaction
// it runs in, so a failure partway through leaves the earlier tables created on those two backends
// just as it does in the legacy server.
func ApplySchema(ctx context.Context, db *sql.DB, dialect Dialect) error {
	schema, err := schemaFor(dialect)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		// The legacy server skips the whole schema here and goes on to fail at its first
		// migration with a message about a table it was never told to create. A database that
		// cannot even start a transaction is reported as that, instead.
		return fmt.Errorf("schema.apply: %w", err)
	}

	for _, statement := range schema {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			tx.Rollback()
			return fmt.Errorf("schema.apply: %s in %s", err, statement)
		}
	}

	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return fmt.Errorf("schema.apply: %w", err)
	}

	return nil
}
