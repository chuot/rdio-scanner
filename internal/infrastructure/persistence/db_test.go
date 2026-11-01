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

// The pool matches the legacy server's: twenty-five connections held ready,
// none closed for being idle.
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

// The sqlite connection string carries the legacy pragmas character for
// character, down to the encoded `=`, plus this server's own `_txlock=immediate`
// save posture.
func TestSqliteDataSourceCarriesTheLegacyPragmasAndTheWritePosture(t *testing.T) {
	_, dsn, err := dataSourceName(Settings{Dialect: DialectSqlite, FilePath: "/var/lib/rdio-scanner/rdio-scanner.db"})
	if err != nil {
		t.Fatalf("dataSourceName: %v", err)
	}

	const want = "file:/var/lib/rdio-scanner/rdio-scanner.db?_pragma=foreign_keys=on&_pragma=busy_timeout%3d10000&_txlock=immediate"

	if dsn != want {
		t.Errorf("dsn = %q, want %q: the legacy pair of pragmas and the write posture this server adds", dsn, want)
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

	// The DSN is the legacy option set plus clientFoundRows, written in the
	// driver's own FormatDSN form.
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

// The mysql driver reports changed rows, not found rows, so an option save that
// writes the same value would insert a duplicate row; the data source asks for
// found rows.
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

// Transport options the operator names — TLS and a connect timeout — reach the
// mysql data source in the form the driver reads; naming nothing carries neither.
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

// The postgres twin of the same transport opt-in, in the form pgx reads.
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

// The connection string is a URL, so the database path the operator names —
// the legacy install lives under "Rdio Scanner" — is escaped, and the driver
// reads back the path as named.
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

	if parsed.RawQuery != "_pragma=foreign_keys=on&_pragma=busy_timeout%3d10000&_txlock=immediate" {
		t.Errorf("the data source carries %q, want the pragmas and the write posture, and that escaping the path changed nothing in them", parsed.RawQuery)
	}
}

// A password with an `@`, a `/`, or a `%` in it is escaped in the URL, and the
// driver reads back the credential the operator named.
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

// openTestSqlite opens a fresh sqlite file for a persistence test.
func openTestSqlite(t *testing.T) *sql.DB {
	t.Helper()

	return openTestSqliteAt(t, filepath.Join(t.TempDir(), "rdio-scanner.db"))
}

// openTestSqliteAt opens a fresh sqlite file at a named path, for the tests
// that need the path itself — to hold it open from a second connection, or to
// show the vacuum leaves it where it found it.
func openTestSqliteAt(t *testing.T, filePath string) *sql.DB {
	t.Helper()

	db, err := Open(Settings{
		Dialect:  DialectSqlite,
		FilePath: filePath,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	return db
}

// Vacuum compacts the sqlite file in place: the free pages measured before the
// statement are gone after it, the file keeps its path, and the rows that were
// not deleted read back unchanged.
func TestVacuumCompactsTheSqliteFileInPlace(t *testing.T) {
	db := openTestSqlite(t)

	if _, err := db.Exec(`CREATE TABLE calls (id INTEGER PRIMARY KEY, body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	const rows = 500

	for i := range rows {
		body := strings.Repeat("x", 200)
		if _, err := db.Exec(`INSERT INTO calls (body) VALUES (?)`, body); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	// Deleting half the rows leaves pages on the freelist: the file keeps its
	// size, and that is exactly what the task reclaims.
	if _, err := db.Exec(`DELETE FROM calls WHERE id % 2 = 0`); err != nil {
		t.Fatalf("delete: %v", err)
	}

	before, after, err := Vacuum(context.Background(), db)
	if err != nil {
		t.Fatalf("Vacuum: %v", err)
	}

	if before.FreelistCount == 0 {
		t.Fatalf("before.FreelistCount = 0, want free pages: the deleted rows should have left the freelist the task reclaims")
	}

	if before.PageSize == 0 {
		t.Errorf("before.PageSize = 0, want the file's page size: the report works out reclaimed bytes from it")
	}

	if after.FreelistCount != 0 {
		t.Errorf("after.FreelistCount = %d, want 0: the vacuum rewrites the file without the freelist", after.FreelistCount)
	}

	if after.PageCount >= before.PageCount {
		t.Errorf("after.PageCount = %d, want fewer than the %d pages before the vacuum: the reclaimed pages leave the file", after.PageCount, before.PageCount)
	}

	if after.PageSize != before.PageSize {
		t.Errorf("after.PageSize = %d, want the same %d: the compacted file keeps the page size it had", after.PageSize, before.PageSize)
	}

	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM calls`).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}

	if remaining != rows/2 {
		t.Errorf("remaining rows = %d, want %d: the vacuum must not touch the rows that were left", remaining, rows/2)
	}

	var body string
	if err := db.QueryRow(`SELECT body FROM calls WHERE id = 1`).Scan(&body); err != nil {
		t.Fatalf("read a surviving row: %v", err)
	}

	if body != strings.Repeat("x", 200) {
		t.Errorf("the surviving row reads back %q, want its 200 x characters unchanged", body)
	}
}

// A database with no free pages reports zero before and after: the operator
// sees the task ran and reclaimed nothing, not a run that failed.
func TestVacuumReportsZeroForADatabaseWithNoFreePages(t *testing.T) {
	db := openTestSqlite(t)

	if _, err := db.Exec(`CREATE TABLE calls (id INTEGER PRIMARY KEY, body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	for i := range 20 {
		if _, err := db.Exec(`INSERT INTO calls (body) VALUES (?)`, "call"); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	if _, _, err := Vacuum(context.Background(), db); err != nil {
		t.Fatalf("first Vacuum: %v", err)
	}

	before, after, err := Vacuum(context.Background(), db)
	if err != nil {
		t.Fatalf("second Vacuum: %v", err)
	}

	if before.FreelistCount != 0 {
		t.Errorf("before.FreelistCount = %d, want 0: the first vacuum left no freelist to reclaim", before.FreelistCount)
	}

	if after.FreelistCount != 0 {
		t.Errorf("after.FreelistCount = %d, want 0", after.FreelistCount)
	}

	if before.PageSize == 0 {
		t.Errorf("before.PageSize = 0, want the file's page size even when nothing is reclaimed")
	}
}

// A writer outside the deployment's own lock — an operator's sqlite3 session,
// for one — still cannot have the file rewritten under it: VACUUM waits for the
// write lock and fails with sqlite's own lock error once the busy timeout
// expires, and the failure reaches the caller as an error.
func TestVacuumFailsWithSqlitesLockErrorAgainstABusyDatabase(t *testing.T) {
	dir := t.TempDir()

	// Two connections with a short busy timeout each stand in for the task's
	// own pair: the server's DSN waits ten seconds, a test waits half a
	// second, and both fail the same way when the wait runs out.
	dsn := "file:" + filepath.Join(dir, "rdio-scanner.db") + "?_pragma=foreign_keys=on&_pragma=busy_timeout=500&_txlock=immediate"

	holder, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open the holding connection: %v", err)
	}

	defer holder.Close()

	if _, err := holder.Exec(`CREATE TABLE calls (id INTEGER PRIMARY KEY, body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	// One connection, so the write transaction sits on a connection that
	// carries the short timeout, and the vacuum's own connection cannot be
	// the one holding the transaction.
	holder.SetMaxOpenConns(1)

	vacuuming, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open the vacuuming connection: %v", err)
	}

	defer vacuuming.Close()

	tx, err := holder.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err := tx.Exec(`INSERT INTO calls (body) VALUES ('in flight')`); err != nil {
		tx.Rollback()
		t.Fatalf("insert inside the transaction: %v", err)
	}

	start := time.Now()

	_, _, err = Vacuum(context.Background(), vacuuming)

	elapsed := time.Since(start)

	tx.Rollback()

	if err == nil {
		t.Fatal("Vacuum against a held write lock: want a failure, got none")
	}

	if !strings.Contains(err.Error(), "database is locked") {
		t.Errorf("Vacuum error = %q, want it to name sqlite's lock: the operator's message is worked out from this failure", err)
	}

	if elapsed < 400*time.Millisecond {
		t.Errorf("Vacuum failed after %v, want the busy timeout waited out before the failure: a held database is waited for, not skipped", elapsed)
	}
}
