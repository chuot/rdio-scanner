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
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Dialect names the database the server was started with. The four names are exactly the four
// values the `-db_type` flag and the `db_type` ini key accept, so a deployment's configuration
// means the same thing to both servers.
type Dialect string

const (
	DialectSqlite     Dialect = "sqlite"
	DialectMariadb    Dialect = "mariadb"
	DialectMysql      Dialect = "mysql"
	DialectPostgresql Dialect = "postgresql"
)

// Settings are the database choices the server was started with. They are gathered here rather
// than read from the command-line configuration so that opening a database does not depend on
// how the server was told what to open.
//
// FilePath is only used by the sqlite dialect; Host, Port, Username, Password, and Name are only
// used by the three server-backed dialects. That is how the legacy server behaves too: a
// deployment that switches from sqlite to mysql keeps an unused db_file in its ini file.
type Settings struct {
	Dialect Dialect

	FilePath string

	Host     string
	Port     uint
	Username string
	Password string
	Name     string
}

// Open connects to the database the server was started with and leaves it ready for the
// repositories built on it.
//
// The connection is not verified here, exactly as the legacy server does not verify it: a bad
// host or an unreadable file is reported when the first query runs, not when the server starts.
//
// Unlike the legacy server, an unusable setting is returned as an error rather than written to
// the process log and followed by a hard exit. Deciding how a failure to open the database ends
// the process belongs to the composition root (T042), which is the only place that knows the
// server is starting and not merely being tested.
func Open(settings Settings) (*sql.DB, error) {
	driver, dsn, err := dataSourceName(settings)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("database.open: %w", err)
	}

	// The same pool the legacy server runs with. A deployment that swaps one binary for the
	// other gets the same number of concurrent connections, which is what the sqlite
	// busy_timeout above is tuned for.
	db.SetConnMaxLifetime(time.Minute)
	db.SetMaxIdleConns(25)
	db.SetMaxOpenConns(25)

	return db, nil
}

// dataSourceName picks the driver and the connection string for a dialect.
//
// The strings are the ones the legacy server builds, character for character, because a
// deployment's ini file is not rewritten when it swaps servers and the two must reach the same
// database the same way. They are connection strings rather than queries, so the rule that every
// query is parameterized does not apply to them — but it does apply to everything that runs
// through the connection they produce.
func dataSourceName(settings Settings) (driver string, dsn string, err error) {
	switch settings.Dialect {
	case DialectSqlite:
		// The two pragmas are what make the legacy server's sqlite behavior what it is:
		// foreign keys enforced, so deleting a system cascades to its talkgroups and calls,
		// and a busy database waited for rather than failed on.
		//
		// The odd `%3d` in the second pragma is what the legacy server writes: an encoded `=`
		// inside an already-built query string. The driver decodes it, so the pragma arrives as
		// `busy_timeout=10000`. It is kept because it works, and because a connection string
		// that differs from the one the other server writes is a difference someone would have
		// to explain.
		return "sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys=on&_pragma=busy_timeout%%3d10000", settings.FilePath), nil

	case DialectMariadb, DialectMysql:
		// ANSI_QUOTES is what lets the same double-quoted identifiers in the schema work on
		// MariaDB and MySQL as they do on sqlite and PostgreSQL.
		return "mysql", fmt.Sprintf(
			"%s:%s@tcp(%s:%d)/%s?sql_mode=ANSI_QUOTES",
			settings.Username, settings.Password, settings.Host, settings.Port, settings.Name,
		), nil

	case DialectPostgresql:
		return "pgx", fmt.Sprintf(
			"postgresql://%s:%s@%s:%d/%s",
			settings.Username, settings.Password, settings.Host, settings.Port, settings.Name,
		), nil

	default:
		// The legacy server writes this to the log and exits. Here it is an error, which is
		// also what fixes Q7: the message is produced from the settings that were given, and
		// not from a configuration that may not be there.
		return "", "", fmt.Errorf("unknown database type %s", settings.Dialect)
	}
}
