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

package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Database struct {
	Config *Config
	Sql    *sql.DB
}

func NewDatabase(config *Config) *Database {
	var err error

	database := &Database{Config: config}

	switch config.DbType {
	case DbTypeSqlite:
		dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys=on&_pragma=busy_timeout%%3d10000", config.GetDbFilePath())

		if database.Sql, err = sql.Open("sqlite", dsn); err != nil {
			log.Fatal(err)
		}

	case DbTypeMariadb, DbTypeMysql:
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?sql_mode=ANSI_QUOTES", config.DbUsername, config.DbPassword, config.DbHost, config.DbPort, config.DbName)

		if database.Sql, err = sql.Open("mysql", dsn); err != nil {
			log.Fatal(err)
		}

	case DbTypePostgresql:
		dsn := fmt.Sprintf("postgresql://%s:%s@%s:%d/%s", config.DbUsername, config.DbPassword, config.DbHost, config.DbPort, config.DbName)

		if database.Sql, err = sql.Open("pgx", dsn); err != nil {
			log.Fatal(err)
		}

	default:
		log.Fatalf("unknown database type %s\n", config.DbType)
	}

	database.Sql.SetConnMaxLifetime(time.Minute)
	database.Sql.SetMaxIdleConns(25)
	database.Sql.SetMaxOpenConns(25)

	if err = database.migrate(); err != nil {
		log.Fatal(err)
	}

	if err = database.seed(); err != nil {
		log.Fatal(err)
	}

	return database
}

func (db *Database) migrate() error {
	var schema []string

	formatError := errorFormatter("database", "migrate")

	switch db.Config.DbType {
	case DbTypeMariadb, DbTypeMysql:
		schema = MysqlSchema
	case DbTypePostgresql:
		schema = PostgresqlSchema
	case DbTypeSqlite:
		schema = SqliteSchema
	default:
		return errors.New("no database schema")
	}

	if tx, err := db.Sql.Begin(); err == nil {

		for _, query := range schema {
			if _, err = tx.Exec(query); err != nil {
				tx.Rollback()
				return formatError(err, query)
			}
		}

		if err = tx.Commit(); err != nil {
			tx.Rollback()
			return formatError(err, "")
		}
	}

	if err := migrateGroups(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateTags(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateSystems(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateTalkgroups(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateUnits(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateOptions(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateMeta(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateLogs(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateDownstreams(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateDirwatches(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateCalls(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateApikeys(db); err != nil {
		return formatError(err, "")
	}

	if err := migrateAccesses(db); err != nil {
		return formatError(err, "")
	}

	return nil
}

func (db *Database) seed() error {
	formatError := func(err error) error {
		return fmt.Errorf("database.seed: %v", err)
	}
	if err := seedGroups(db); err != nil {
		return formatError(err)
	}

	if err := seedTags(db); err != nil {
		return formatError(err)
	}

	return nil
}

func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
