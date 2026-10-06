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
	"os"
	"path/filepath"
	"testing"
)

// legacyFixture is a SQLite database written entirely by the legacy server: its schema, its
// seeds, its configuration save, and two ingested calls, produced by running the legacy binary
// and driving it through its own endpoints. See testdata/legacydb/generate.sh.
//
// It is what lets the persistence tests prove the thing REQ-007 actually claims — that this code
// reads a database the other server wrote — instead of proving that it reads a database it wrote
// itself.
const legacyFixture = "../../../testdata/legacydb/rdio-scanner.db"

// copyLegacyFixture puts the fixture in a temporary directory and returns its path, so that a
// test which writes to it cannot damage the committed one.
func copyLegacyFixture(t *testing.T) string {
	t.Helper()

	b, err := os.ReadFile(legacyFixture)
	if err != nil {
		t.Fatalf("the database written by the legacy server is missing: %v\n"+
			"regenerate it with server-ng/testdata/legacydb/generate.sh", err)
	}

	path := filepath.Join(t.TempDir(), "rdio-scanner.db")

	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("copying the fixture: %v", err)
	}

	return path
}

// openLegacyDatabase opens the fixture the way the server opens its own database.
func openLegacyDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db, err := Open(Settings{Dialect: DialectSqlite, FilePath: copyLegacyFixture(t)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	return db
}

// countRows reads the row count of a table, for asserting that a test is looking at a fixture
// that actually contains the thing it claims to read.
func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var count int

	query := `SELECT COUNT(*) FROM "` + table + `"`
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return count
}

// openEmptyDatabase opens the file a server-ng deployment that has never run before gets: nothing
// in it. Migrate is what puts the tables there, so a test of Migrate must not have applied them
// first.
func openEmptyDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db, err := Open(Settings{Dialect: DialectSqlite, FilePath: filepath.Join(t.TempDir(), "rdio-scanner.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	return db
}

// openSchemaDatabase opens a database that has the legacy tables and nothing in them, which is what
// a repository test writes into.
func openSchemaDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db := openEmptyDatabase(t)

	if err := ApplySchema(context.Background(), db, DialectSqlite); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	return db
}

// openLegacyDatabaseWithout opens the database the legacy server wrote with the named tables
// emptied: the legacy schema, the legacy column types, and no rows to trip over.
func openLegacyDatabaseWithout(t *testing.T, tables ...string) *sql.DB {
	t.Helper()

	db := openLegacyDatabase(t)

	for _, table := range tables {
		query := `DELETE FROM "` + table + `"`

		if _, err := db.Exec(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}

	return db
}
