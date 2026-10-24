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
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

func TestBindLeavesTheQuestionMarkPlaceholdersAloneExceptForPostgresql(t *testing.T) {
	const query = `SELECT "callId" FROM "calls" WHERE "systemId" = ? AND "timestamp" BETWEEN ? AND ?`

	for _, dialect := range []Dialect{DialectSqlite, DialectMariadb, DialectMysql} {
		if bound := dialect.bind(query); bound != query {
			t.Errorf("%s bound query = %s, want the query unchanged", dialect, bound)
		}
	}

	const want = `SELECT "callId" FROM "calls" WHERE "systemId" = $1 AND "timestamp" BETWEEN $2 AND $3`

	if bound := DialectPostgresql.bind(query); bound != want {
		t.Errorf("postgresql bound query = %s, want %s", bound, want)
	}
}

// The only `?` the scan in bind rewrites is one standing outside a quoted
// literal.
func TestBindDoesNotNumberAQuestionMarkInsideAQuotedLiteral(t *testing.T) {
	const query = `SELECT 'what?' FROM "calls" WHERE "audioMime" = ?`

	const want = `SELECT 'what?' FROM "calls" WHERE "audioMime" = $1`

	if bound := DialectPostgresql.bind(query); bound != want {
		t.Errorf("bound query = %s, want %s", bound, want)
	}
}

func TestScopeTextIsTheShapeTheLegacyServerStores(t *testing.T) {
	wildcard, _ := domain.ParseAccessScope("*")

	if text := scopeText(wildcard); text != `"*"` {
		t.Errorf("wildcard scope stored as %s, want \"*\"", text)
	}

	scoped, _ := domain.ParseAccessScope([]any{
		map[string]any{"id": float64(10001), "talkgroups": []any{float64(1514000)}},
	})

	if text := scopeText(scoped); text != `[{"id":10001,"talkgroups":[1514000]}]` {
		t.Errorf("scoped scope stored as %s", text)
	}

	empty, _ := domain.ParseAccessScope([]any{})

	if text := scopeText(empty); text != `[]` {
		t.Errorf("empty scope stored as %s, want []", text)
	}

	absent, _ := domain.ParseAccessScope(nil)

	if text := scopeText(absent); text != `null` {
		t.Errorf("absent scope stored as %s, want the null the legacy server writes", text)
	}
}

func TestParseScopeReadsBackEveryShapeTheColumnCarries(t *testing.T) {
	for _, tc := range []struct {
		stored   string
		wantKind string
	}{
		{`"*"`, "wildcard"},
		{`[{"id":10001,"talkgroups":[1514000,1514001]}]`, "scoped"},
		{`[]`, "empty"},
		{`null`, "absent"},
		{"", "absent"},
		{`not json`, "absent"},
	} {
		scope := parseScope(tc.stored)

		switch tc.wantKind {
		case "wildcard":
			if !scope.Present || !scope.Wildcard {
				t.Errorf("%s reads back as %#v, want the wildcard", tc.stored, scope)
			}

		case "scoped":
			if !scope.Present || scope.Wildcard || len(scope.Systems) != 1 {
				t.Errorf("%s reads back as %#v, want one system", tc.stored, scope)

				continue
			}

			if refs := scope.Systems[0].TalkgroupRefs; len(refs) != 2 || refs[0] != 1514000 || refs[1] != 1514001 {
				t.Errorf("%s reads back with talkgroups %v, want [1514000 1514001]", tc.stored, refs)
			}

			if !scope.GrantsReferences(10001, 1514001) {
				t.Errorf("%s does not grant the talkgroup it names", tc.stored)
			}

		case "empty":
			if !scope.Present || scope.Wildcard || len(scope.Systems) != 0 {
				t.Errorf("%s reads back as %#v, want a scope that was supplied and names nothing", tc.stored, scope)
			}

		case "absent":
			if scope.Present {
				t.Errorf("%s reads back as %#v, want no scope at all: this is the state the PIN gate treats as unauthenticated", tc.stored, scope)
			}
		}
	}
}

// The two servers agree on the scope column: the text this server writes is the
// text the legacy server wrote in the fixture, and it reads back as the scope it
// was written from.
func TestScopeTextRoundTripsThroughTheStoredColumn(t *testing.T) {
	stored := `[{"id":10001,"talkgroups":[1514000,1514001]}]`

	scope := parseScope(stored)

	if text := scopeText(scope); text != stored {
		t.Errorf("the scope read from %s is written back as %s", stored, text)
	}

	var legacy any

	if err := json.Unmarshal([]byte(stored), &legacy); err != nil {
		t.Fatalf("the fixture stores JSON the legacy server wrote: %v", err)
	}

	parsed, present := domain.ParseAccessScope(legacy)

	if !present || !parsed.GrantsReferences(10001, 1514001) {
		t.Errorf("the fixture scope reads back as %#v", parsed)
	}
}

func TestStatementErrorKeepsTheLegacyShape(t *testing.T) {
	err := statementError("calls", "save", errors.New("constraint failed"), `INSERT INTO "calls" ("audio") VALUES (?)`)

	const want = `calls.save: constraint failed in INSERT INTO "calls" ("audio") VALUES (?)`

	if err.Error() != want {
		t.Errorf("error = %v, want %s", err, want)
	}
}

func TestOperationErrorNamesTheOperationWithoutInventingAStatement(t *testing.T) {
	err := operationError("calls", "search", errors.New("no rows"))

	const want = `calls.search: no rows`

	if err.Error() != want {
		t.Errorf("error = %v, want %s", err, want)
	}
}

// The invariant that makes the scan in bind safe, pinned in its sharpest case:
// SQL writes a quote inside a literal as a doubled quote — 'it”s' — and the
// scan reads that pair as one quoted span ending and the next starting, copying
// the literal verbatim and numbering only the placeholders outside literals.
func TestBindNumbersPlaceholdersAroundAnEscapedQuoteLiteral(t *testing.T) {
	const query = `SELECT "value" FROM "options" WHERE "key" = ? AND "value" LIKE 'it''s?x' AND "other" = ?`

	const want = `SELECT "value" FROM "options" WHERE "key" = $1 AND "value" LIKE 'it''s?x' AND "other" = $2`

	if bound := DialectPostgresql.bind(query); bound != want {
		t.Errorf("bound query = %s, want %s: the ? inside the literal is not a placeholder, and the numbering must not count it", bound, want)
	}
}

// holdTheWriteLock borrows a connection from the pool the product opened, begins a
// transaction on it, writes one log row, and leaves it open: the state a save meets
// when a second connection has written and not committed. The row goes into `logs`,
// which neither the access save nor either read here touches, so the only thing they
// contend for is the database's write lock; the holder rolls back when the test ends.
func holdTheWriteLock(t *testing.T, db *sql.DB) {
	t.Helper()

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}

	holder, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		conn.Close()

		t.Fatalf("BeginTx: %v", err)
	}

	if _, err := holder.ExecContext(context.Background(),
		`INSERT INTO "logs" ("level", "source", "message", "timestamp") VALUES (?, ?, ?, ?)`,
		string(domain.LogLevelInfo),
		string(domain.LogSourceServer),
		"a transaction that holds the write lock",
		callHistoryTime().UnixMilli(),
	); err != nil {
		holder.Rollback()
		conn.Close()

		t.Fatalf("the holder could not write: %v", err)
	}

	t.Cleanup(func() {
		if err := holder.Rollback(); err != nil {
			t.Errorf("the holder rolls back as %v, want the write lock released when the test ends", err)
		}

		conn.Close()
	})
}

// Where a writing transaction takes the write lock decides whether a busy database
// is a wait the server honours: a reconciling save reads the identities it holds
// before it replaces them, a read inside a transaction holds the read lock, and a
// transaction holding the read lock may not wait for the write lock. So the write
// lock is taken at the begin, and a busy database refuses the save there, naming the
// transaction, never a statement.
//
// The arrangement is the one a contended whole-suite run produces: a second
// connection has written and not committed, and the submitted access list is not
// the list the store holds, so the first thing the save writes is the delete of the
// row the submission dropped.
func TestAReconcilingSaveIsRefusedAtItsBeginWhenTheWriteLockIsHeld(t *testing.T) {
	db := openSchemaDatabase(t)

	accesses := NewAccesses(db, DialectSqlite)

	stored := domain.Accesses{{
		Code:  "stored-access",
		Ident: "the access the store holds",
		Limit: 1,
		Order: 1,
		Scope: wildcardScope(t),
	}}

	if err := accesses.SaveAccesses(context.Background(), stored); err != nil {
		t.Fatalf("SaveAccesses of the access the store is to hold: %v", err)
	}

	holdTheWriteLock(t, db)

	err := accesses.SaveAccesses(context.Background(), domain.Accesses{{
		Code:  "submitted-instead",
		Ident: "the access the submission names",
		Limit: 2,
		Order: 2,
		Scope: wildcardScope(t),
	}})

	if err == nil {
		t.Fatal("SaveAccesses = nil while another connection holds the write lock, want the save refused")
	}

	message := err.Error()

	if !strings.Contains(message, "accesses.write") {
		t.Errorf("the save reported %v, want it to name the access store's write", message)
	}

	if !strings.Contains(message, "database is locked") {
		t.Errorf("the save reported %v, want the busy database named as the cause", message)
	}

	// The refusal is the transaction's: a save whose begin was refused never reached a statement, and a
	// report that names one is a save that read before it asked for the write lock.
	for _, statement := range []string{`DELETE FROM "accesses"`, `UPDATE "accesses"`, `INSERT INTO "accesses"`, `SELECT "accessId"`} {
		if strings.Contains(message, statement) {
			t.Errorf("the save reported %v, want the failure of its transaction and not of the statement %q it never reached", message, statement)
		}
	}

	if count := countRows(t, db, "accesses"); count != 1 {
		t.Errorf("the table holds %d accesses after a save that was refused, want the stored one left exactly as it was", count)
	}
}

// The half of the same rule that keeps the server serving: the write lock belongs
// to the transactions that write. Both reads here run inside a transaction so
// their statements agree with each other — a listener never sees a call and then
// finds its frequencies gone — but a read that asked for the write lock at its
// begin would put every listener behind whichever writer is running.
func TestAReadTransactionIsNotHeldUpByAConnectionHoldingTheWriteLock(t *testing.T) {
	fixture := newCallsFixture(t)

	recorded := fixture.storeCall(t, 10001, 1514001, callHistoryTime())

	holdTheWriteLock(t, fixture.db)

	catalog, err := NewSystems(fixture.db, DialectSqlite).AllSystems(context.Background())
	if err != nil {
		t.Fatalf("AllSystems: %v", err)
	}

	if len(catalog) != 1 || catalog[0].SystemRef != 10001 {
		t.Fatalf("AllSystems returned %+v, want the one system the fixture stored, read while another connection holds the write lock", catalog)
	}

	found, err := fixture.calls.GetCall(context.Background(), recorded.ID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if found.ID != recorded.ID || found.Talkgroup.TalkgroupRef != 1514001 {
		t.Errorf("GetCall returned %+v, want the recorded call and its talkgroup, read while another connection holds the write lock", found)
	}
}
