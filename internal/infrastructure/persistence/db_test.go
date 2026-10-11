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
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestOpenSqliteEnforcesForeignKeysAndWaitsForABusyDatabase(t *testing.T) {
	db, err := Open(Settings{
		Dialect:  DialectSqlite,
		FilePath: filepath.Join(t.TempDir(), "rdio-scanner.db"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	defer db.Close()

	var foreignKeys int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}

	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want the legacy server's enforced foreign keys: deleting a system cascades to its talkgroups and calls", foreignKeys)
	}

	var busyTimeout int
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}

	if busyTimeout != 10000 {
		t.Errorf("busy_timeout = %d, want the legacy server's ten second wait for a busy database", busyTimeout)
	}
}

// TestOpenSqliteUsesTheLegacyConnectionPool pins the pool the legacy server runs with: at most
// 25 connections, and all of them kept for reuse.
//
// The idle half is worth pinning by behavior rather than by reading the setting back: a pool that
// kept only the default two idle connections would close twenty-three of the twenty-five the
// legacy server holds ready, and the way that shows up is connections being closed for being idle.
func TestOpenSqliteUsesTheLegacyConnectionPool(t *testing.T) {
	db, err := Open(Settings{
		Dialect:  DialectSqlite,
		FilePath: filepath.Join(t.TempDir(), "rdio-scanner.db"),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	defer db.Close()

	if stats := db.Stats(); stats.MaxOpenConnections != 25 {
		t.Errorf("MaxOpenConnections = %d, want the legacy pool of 25", stats.MaxOpenConnections)
	}

	const held = 5

	conns := make([]*sql.Conn, 0, held)

	for range held {
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatalf("Conn: %v", err)
		}

		conns = append(conns, conn)
	}

	if stats := db.Stats(); stats.InUse != held {
		t.Fatalf("InUse = %d, want %d connections held out of a pool of 25", stats.InUse, held)
	}

	for _, conn := range conns {
		conn.Close()
	}

	stats := db.Stats()

	if stats.Idle != 5 {
		t.Errorf("Idle = %d, want every connection kept ready for the next query", stats.Idle)
	}

	if stats.MaxIdleClosed != 0 {
		t.Errorf("MaxIdleClosed = %d, want none: a pool smaller than the legacy 25 closes connections for being idle", stats.MaxIdleClosed)
	}
}

func TestOpenRejectsAnUnknownDatabaseTypeWithoutOpeningAnything(t *testing.T) {
	db, err := Open(Settings{Dialect: Dialect("oracle"), FilePath: filepath.Join(t.TempDir(), "rdio-scanner.db")})

	if err == nil {
		t.Fatal("a database type the server does not know is a failure the caller is told about")
	}

	if db != nil {
		t.Error("nothing was opened")
	}

	if !strings.Contains(err.Error(), "unknown database type oracle") {
		t.Errorf("error = %v, want the legacy wording that names the type the deployment asked for", err)
	}
}

func TestSqliteDataSourceIsTheLegacyOne(t *testing.T) {
	_, dsn, err := dataSourceName(Settings{Dialect: DialectSqlite, FilePath: "/var/lib/rdio-scanner/rdio-scanner.db"})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	const want = "file:/var/lib/rdio-scanner/rdio-scanner.db?_pragma=foreign_keys=on&_pragma=busy_timeout%3d10000"

	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}
}

func TestMysqlAndMariadbDataSourcesAreTheLegacyOne(t *testing.T) {
	driver, dsn, err := dataSourceName(Settings{
		Dialect:  DialectMariadb,
		Host:     "db.internal",
		Port:     3306,
		Username: "rdio",
		Password: "secret",
		Name:     "rdio-scanner",
	})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	if driver != "mysql" {
		t.Errorf("driver = %q, want MariaDB served by the mysql driver, as the legacy server does", driver)
	}

	// The legacy option set, plus clientFoundRows (decision D10), written the way the
	// driver itself writes it (CA-014.1): the driver's FormatDSN orders its parameters,
	// and the set it produces is the legacy set — same user, host, database, sql_mode,
	// found-rows — with nothing added, so a deployment that opts in to nothing connects
	// exactly as it connects today.
	const want = "rdio:secret@tcp(db.internal:3306)/rdio-scanner?clientFoundRows=true&sql_mode=ANSI_QUOTES"

	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}

	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("the data source is not one the mysql driver accepts: %v", err)
	}

	if parsed.Params["sql_mode"] != "ANSI_QUOTES" {
		t.Errorf("sql_mode = %q, want ANSI_QUOTES: the schema quotes identifiers the way ANSI requires, and the three backends have to agree about that", parsed.Params["sql_mode"])
	}
}

func TestPostgresqlDataSourceIsTheLegacyOne(t *testing.T) {
	driver, dsn, err := dataSourceName(Settings{
		Dialect:  DialectPostgresql,
		Host:     "db.internal",
		Port:     5432,
		Username: "rdio",
		Password: "secret",
		Name:     "rdio-scanner",
	})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	if driver != "pgx" {
		t.Errorf("driver = %q, want the pgx driver the legacy server registers", driver)
	}

	const want = "postgresql://rdio:secret@db.internal:5432/rdio-scanner"

	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}
}

// TestMysqlAndMariadbDataSourcesCarryClientFoundRows (REQ-010, CA-010.3, decision D10):
// the mysql driver reports changed rows, not found rows, so an option update that
// writes the same value reports nothing and the save inserts a duplicate row for every
// key. The data source has to ask for found rows.
func TestMysqlAndMariadbDataSourcesCarryClientFoundRows(t *testing.T) {
	_, dsn, err := dataSourceName(Settings{
		Dialect:  DialectMysql,
		Host:     "db.internal",
		Port:     3306,
		Username: "rdio",
		Password: "secret",
		Name:     "rdio-scanner",
	})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("the data source is not one the mysql driver accepts: %v", err)
	}

	if !parsed.ClientFoundRows {
		t.Error("clientFoundRows is not set on the data source: an option update that changes nothing must report the row it found, not nothing, or the save duplicates every key")
	}
}

// TestMysqlDataSourceCarriesTheTransportOptionsTheOperatorNames (REQ-014, CA-014.1,
// D12): the operator opts in to TLS and a connect timeout by name; the data source
// carries them in the form the mysql driver reads them, and a deployment that names
// nothing carries neither.
func TestMysqlDataSourceCarriesTheTransportOptionsTheOperatorNames(t *testing.T) {
	_, dsn, err := dataSourceName(Settings{
		Dialect:        DialectMysql,
		Host:           "db.internal",
		Port:           3306,
		Username:       "rdio",
		Password:       "secret",
		Name:           "rdio-scanner",
		TLS:            "true",
		ConnectTimeout: "5s",
	})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("the data source is not one the mysql driver accepts: %v", err)
	}

	if parsed.TLS == nil {
		t.Error("the data source carries no tls config, want the driver's TLS for tls=true: the operator opted in to TLS, and the driver is what enforces it")
	}

	if parsed.Timeout != 5*time.Second {
		t.Errorf("connect timeout = %v, want 5s: a database that does not answer must not hold the server", parsed.Timeout)
	}

	_, plain, err := dataSourceName(Settings{
		Dialect:  DialectMysql,
		Host:     "db.internal",
		Port:     3306,
		Username: "rdio",
		Password: "secret",
		Name:     "rdio-scanner",
	})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	untouched, err := mysql.ParseDSN(plain)
	if err != nil {
		t.Fatalf("the data source is not one the mysql driver accepts: %v", err)
	}

	if untouched.TLS != nil || untouched.Timeout != 0 {
		t.Errorf("the default data source carries tls %v and timeout %v, want neither: the defaults connect in plaintext as deployments do today", untouched.TLS, untouched.Timeout)
	}
}

// TestPostgresqlDataSourceCarriesTheTransportOptionsTheOperatorNames (REQ-014,
// CA-014.1, D12): the postgres twin of the same opt-in, in the form pgx reads.
func TestPostgresqlDataSourceCarriesTheTransportOptionsTheOperatorNames(t *testing.T) {
	_, dsn, err := dataSourceName(Settings{
		Dialect:        DialectPostgresql,
		Host:           "db.internal",
		Port:           5432,
		Username:       "rdio",
		Password:       "secret",
		Name:           "rdio-scanner",
		SslMode:        "require",
		ConnectTimeout: "5s",
	})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	const want = "postgresql://rdio:secret@db.internal:5432/rdio-scanner?connect_timeout=5&sslmode=require"

	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}

	_, plain, err := dataSourceName(Settings{
		Dialect:  DialectPostgresql,
		Host:     "db.internal",
		Port:     5432,
		Username: "rdio",
		Password: "secret",
		Name:     "rdio-scanner",
	})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	if plain != "postgresql://rdio:secret@db.internal:5432/rdio-scanner" {
		t.Errorf("the default data source is %q, want the legacy string untouched: the defaults change nothing about how a deployment connects today", plain)
	}
}

// TestSqliteDataSourceEscapesThePathTheOperatorNames (REQ-014, CA-014.1): the
// connection string is a URL, and a database path with a space, a `#`, or a `?` in
// it — the legacy install lives under "Rdio Scanner" — is a path, not a query. The
// path is escaped, and the driver reads back the path the operator named.
func TestSqliteDataSourceEscapesThePathTheOperatorNames(t *testing.T) {
	const path = "/var/lib/Rdio Scanner/rdio#scanner.db"

	_, dsn, err := dataSourceName(Settings{Dialect: DialectSqlite, FilePath: path})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("the data source %q is not one the sqlite driver can read as a URL: %v", dsn, err)
	}

	if parsed.Path != path {
		t.Errorf("the driver reads the database path %q, want %q: a path with a space or a # is a path, not a query", parsed.Path, path)
	}

	if parsed.RawQuery != "_pragma=foreign_keys=on&_pragma=busy_timeout%3d10000" {
		t.Errorf("the pragmas arrived as %q, want the legacy pair", parsed.RawQuery)
	}
}

// TestPostgresqlDataSourceEscapesCredentialsTheOperatorNames (REQ-014, CA-014.1):
// a password with an `@`, a `/`, or a `%` in it is a password, not a piece of the
// connection string. The URL form escapes it, and the driver reads back the
// credential the operator named.
func TestPostgresqlDataSourceEscapesCredentialsTheOperatorNames(t *testing.T) {
	_, dsn, err := dataSourceName(Settings{
		Dialect:  DialectPostgresql,
		Host:     "db.internal",
		Port:     5432,
		Username: "rdio",
		Password: "p@ss/word%1",
		Name:     "rdio-scanner",
	})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("the data source %q is not a URL: %v", dsn, err)
	}

	got, ok := parsed.User.Password()
	if !ok || got != "p@ss/word%1" {
		t.Errorf("the driver reads the password %q, want p@ss/word%%1: a password with an @ is a password, not the end of the userinfo", got)
	}

	if parsed.Host != "db.internal:5432" {
		t.Errorf("the driver reads the host %q, want db.internal:5432", parsed.Host)
	}
}
