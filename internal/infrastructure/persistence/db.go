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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Dialect names the database the server was started with, named exactly as the
// `-db_type` flag and the `db_type` ini key name it, so a deployment's
// configuration means the same thing to both servers.
type Dialect string

const (
	DialectSqlite     Dialect = "sqlite"
	DialectMariadb    Dialect = "mariadb"
	DialectMysql      Dialect = "mysql"
	DialectPostgresql Dialect = "postgresql"
)

// Settings are the database choices the server was started with, gathered here
// so that opening a database does not depend on how the server was configured.
// FilePath serves only the sqlite dialect; Host, Port, Username, Password, and
// Name only the three server-backed dialects, as in the legacy server.
type Settings struct {
	Dialect Dialect

	FilePath string

	Host     string
	Port     uint
	Username string
	Password string
	Name     string

	// TLS is the mysql driver's tls parameter (the db_tls ini option): "true",
	// "skip-verify", "false", or the name of a registered tls config. Empty —
	// the default — connects in plaintext; the operator opts in.
	TLS string

	// SslMode is the postgres sslmode parameter (the db_sslmode ini option),
	// in the form pgx reads it: "disable", "require", "verify-ca",
	// "verify-full", and the rest of the postgres names. Empty — the
	// default — is what the server sends today.
	SslMode string

	// ConnectTimeout is how long the connection attempt waits (the
	// db_connect_timeout ini option), in Go's duration form. Empty — the
	// default — leaves the timing to the driver and the operating system.
	ConnectTimeout string
}

// Open connects to the database the server was started with and leaves it ready
// for the repositories built on it. The connection is not verified here, as in
// the legacy server: a bad host or unreadable file is reported when the first
// query runs. An unusable setting is returned as an error rather than logged
// and exited from; how a failure ends the process belongs to the composition
// root.
func Open(settings Settings) (*sql.DB, error) {
	driver, dsn, err := dataSourceName(settings)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("database.open: %w", err)
	}

	// The same pool the legacy server runs with, which the sqlite busy_timeout
	// is tuned for.
	db.SetConnMaxLifetime(time.Minute)
	db.SetMaxIdleConns(25)
	db.SetMaxOpenConns(25)

	return db, nil
}

// VacuumMeasurements are the sqlite file's page facts read around the VACUUM
// statement: how wide a page is, how many pages the file holds, and how many
// of them sit on the freelist waiting to be reclaimed. The pair of readings is
// what the operator's report is worked out from.
type VacuumMeasurements struct {
	PageSize      int
	PageCount     int
	FreelistCount int
}

// Vacuum runs sqlite's VACUUM statement — the file is rewritten in a compacted
// form under the same name — and measures the freelist before and after so the
// caller can report what was reclaimed. It belongs to the sqlite dialect; the
// caller decides which databases it is asked to run against. A database another
// writer holds fails once the DSN's busy timeout expires, carrying sqlite's own
// "database is locked" wording.
func Vacuum(ctx context.Context, db *sql.DB) (before, after VacuumMeasurements, err error) {
	if before, err = measureSqlitePages(ctx, db); err != nil {
		return before, after, err
	}

	if _, err = db.ExecContext(ctx, `VACUUM`); err != nil {
		return before, after, fmt.Errorf("database.vacuum: %w", err)
	}

	if after, err = measureSqlitePages(ctx, db); err != nil {
		return before, after, err
	}

	return before, after, nil
}

// measureSqlitePages reads the three pragmas the report needs. Pragma reads are
// statement-level, so they need no transaction.
func measureSqlitePages(ctx context.Context, db *sql.DB) (m VacuumMeasurements, err error) {
	if err = db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&m.PageSize); err != nil {
		return m, fmt.Errorf("database.vacuum.page_size: %w", err)
	}

	if err = db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&m.PageCount); err != nil {
		return m, fmt.Errorf("database.vacuum.page_count: %w", err)
	}

	if err = db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&m.FreelistCount); err != nil {
		return m, fmt.Errorf("database.vacuum.freelist_count: %w", err)
	}

	return m, nil
}

// dataSourceName picks the driver and the connection string for a dialect. The
// strings are the ones the legacy server builds, character for character: a
// deployment's ini file is not rewritten when it swaps servers, and the two
// must reach the same database the same way. They are connection strings, not
// queries, so the rule that every query is parameterized does not apply to
// them — but it does apply to everything that runs through the connections
// they produce.
func dataSourceName(settings Settings) (driver string, dsn string, err error) {
	switch settings.Dialect {
	case DialectSqlite:
		// The pragmas carry the legacy server's sqlite behavior: foreign keys
		// enforced, so deleting a system cascades to its talkgroups and calls,
		// and a busy database waited for rather than failed on. The odd `%3d`
		// in the second pragma is the legacy server's own encoded `=`: the
		// driver decodes it, and the string is kept because it must read
		// exactly as the other server writes it.
		//
		// `_txlock=immediate` is this server's own addition. A transaction
		// asked for as read-only is begun without the begin mode, so reads
		// that only read keep their read lock, and a statement outside a
		// transaction is not affected by a begin mode at all.
		//
		// The path is escaped into the URL the driver reads: a path with a
		// space, a `#`, or a `?` in it is a path, not a query. Escaping is
		// segment by segment so the separators stay separators, and a plain
		// path escapes to itself — byte-for-byte the string the legacy server
		// wrote.
		segments := strings.Split(settings.FilePath, "/")

		for i, segment := range segments {
			segments[i] = url.PathEscape(segment)
		}

		return "sqlite", "file:" + strings.Join(segments, "/") + "?_pragma=foreign_keys=on&_pragma=busy_timeout%3d10000&_txlock=immediate", nil

	case DialectMariadb, DialectMysql:
		// ANSI_QUOTES is what lets the same double-quoted identifiers in the schema work on
		// MariaDB and MySQL as they do on sqlite and PostgreSQL. Why clientFoundRows,
		// carried on the field below.
		source := &mysql.Config{
			User:   settings.Username,
			Passwd: settings.Password,
			Net:    "tcp",
			Addr:   fmt.Sprintf("%s:%d", settings.Host, settings.Port),
			DBName: settings.Name,

			Params: map[string]string{"sql_mode": "ANSI_QUOTES"},

			// The driver's own defaults, named so FormatDSN says nothing about
			// them: the data source carries the legacy option set and nothing
			// else.
			AllowNativePasswords: true,
			CheckConnLiveness:    true,
			MaxAllowedPacket:     64 << 20,

			// clientFoundRows is what makes an option update report the row it
			// found; the driver's own field is what carries it.
			ClientFoundRows: true,
		}

		if settings.TLS != "" {
			source.Params["tls"] = settings.TLS
		}

		if settings.ConnectTimeout != "" {
			timeout, err := time.ParseDuration(settings.ConnectTimeout)
			if err != nil {
				return "", "", fmt.Errorf("database connect timeout %s is not a duration: %w", settings.ConnectTimeout, err)
			}

			source.Timeout = timeout
		}

		return "mysql", source.FormatDSN(), nil

	case DialectPostgresql:
		// The URL form escapes the credential: a password with an `@`, a `/` or a
		// `%` in it stays a password, and with nothing opted in the URL reads as
		// the string the legacy server wrote.
		source := &url.URL{
			Scheme: "postgresql",
			User:   url.UserPassword(settings.Username, settings.Password),
			Host:   fmt.Sprintf("%s:%d", settings.Host, settings.Port),
			Path:   "/" + settings.Name,
		}

		query := source.Query()

		if settings.SslMode != "" {
			query.Set("sslmode", settings.SslMode)
		}

		if settings.ConnectTimeout != "" {
			timeout, err := time.ParseDuration(settings.ConnectTimeout)
			if err != nil {
				return "", "", fmt.Errorf("database connect timeout %s is not a duration: %w", settings.ConnectTimeout, err)
			}

			// pgx reads connect_timeout in seconds.
			query.Set("connect_timeout", strconv.Itoa(int(timeout.Seconds())))
		}

		source.RawQuery = query.Encode()

		return "pgx", source.String(), nil

	default:
		// The legacy server logs this and exits; here it is an error reported from
		// the settings that were given.
		return "", "", fmt.Errorf("unknown database type %s", settings.Dialect)
	}
}
