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

	// TLS is the mysql driver's tls parameter (the db_tls ini option): "true",
	// "skip-verify", "false", or the name of a registered tls config.
	// Empty — the default — connects in plaintext, exactly as deployments
	// connect today; the operator opts in.
	TLS string

	// SslMode is the postgres sslmode parameter (the db_sslmode ini option),
	// in the form pgx reads it: "disable", "require", "verify-ca",
	// "verify-full", and the rest of the postgres names. Empty — the
	// default — is what the server sends today.
	SslMode string

	// ConnectTimeout is how long the connection attempt waits (the
	// db_connect_timeout ini option), in Go's duration form ("5s"). Empty —
	// the default — leaves the timing to the driver and the operating system,
	// as it is today.
	ConnectTimeout string
}

// Open connects to the database the server was started with and leaves it ready for the
// repositories built on it.
//
// The connection is not verified here, exactly as the legacy server does not verify it: a bad
// host or an unreadable file is reported when the first query runs, not when the server starts.
//
// Unlike the legacy server, an unusable setting is returned as an error rather than written to
// the process log and followed by a hard exit. Deciding how a failure to open the database ends
// the process belongs to the composition root, which is the only place that knows the
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
		//
		// `_txlock=immediate` is this server's own addition, and it is here for the saves. A
		// reconciling save reads the rows it is about to replace before it replaces them, and a read
		// inside a transaction takes the database's read lock; a transaction that holds the read lock
		// is not allowed to ask for the write lock. So the busy timeout above is a wait such a save
		// could never make: begun deferred, it is refused the moment it writes, however briefly
		// whatever else is holding the database means to keep holding it. Beginning immediate takes
		// the write lock before the read, which turns a busy database into the ten-second wait this
		// connection string promises rather than into a refusal the save was never allowed to avoid.
		//
		// Two things are deliberately outside that. A transaction asked for as read-only is begun
		// without the begin mode, which is how the reads that only read — a listener fetching one
		// recorded call, the stored catalog every save announces through — keep their lock; a server
		// that took the write lock to answer a read would serialize its readers behind its writers,
		// which is the opposite of what a busy timeout is for. And a statement that runs outside a
		// transaction is not affected by a begin mode at all: it waits for a busy database exactly as
		// it always waited.
		// The path is escaped into the URL the driver reads: the
		// legacy install lives under "Rdio Scanner", and a path with a space,
		// a `#`, or a `?` in it is a path, not a query. Escaping is segment by
		// segment so the separators stay separators, and a plain path escapes
		// to itself — byte-for-byte the string the legacy server wrote.
		segments := strings.Split(settings.FilePath, "/")

		for i, segment := range segments {
			segments[i] = url.PathEscape(segment)
		}

		return "sqlite", "file:" + strings.Join(segments, "/") + "?_pragma=foreign_keys=on&_pragma=busy_timeout%3d10000&_txlock=immediate", nil

	case DialectMariadb, DialectMysql:
		// ANSI_QUOTES is what lets the same double-quoted identifiers in the schema work on
		// MariaDB and MySQL as they do on sqlite and PostgreSQL.
		//
		// clientFoundRows is what makes an option update report the row it found rather
		// than the rows it changed: an update that writes the same value changes nothing,
		// and the save reads "nothing changed" as "no such row" and inserts a duplicate
		// for every key.
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
		// The URL form escapes the credential: a password with an
		// `@`, a `/`, or a `%` in it is a password, not a piece of the
		// connection string, and today's plain interpolation hands it to the
		// parser as structure. With nothing opted in, the URL reads byte-for-byte
		// as the string the legacy server wrote.
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
		// The legacy server writes this to the log and exits. Here it is an error, which is
		// also what fixes the reported failure: the message is produced from the settings that were given, and
		// not from a configuration that may not be there.
		return "", "", fmt.Errorf("unknown database type %s", settings.Dialect)
	}
}
