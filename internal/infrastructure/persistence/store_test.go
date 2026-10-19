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
	"encoding/json"
	"errors"
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

// TestBindDoesNotNumberAQuestionMarkInsideAQuotedLiteral is what makes the scan in bind safe to
// use: the only `?` it rewrites is one it is standing in for.
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

// TestScopeTextRoundTripsThroughTheStoredColumn proves the two servers agree on the column: the
// text this server writes is the text the legacy server wrote in the fixture, and the text the
// fixture carries reads back as the scope it was written from.
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

// TestBindNumbersPlaceholdersAroundAnEscapedQuoteLiteral: the
// invariant that makes the scan in bind safe, pinned in its sharpest case. SQL
// writes a quote inside a literal as a doubled quote — 'it”s' — and the scan
// reads that doubled pair as the end of one quoted span and the start of the
// next: the literal is copied verbatim, its `?` is the literal's own character
// and not a placeholder, and the numbering counts only the placeholders outside.
func TestBindNumbersPlaceholdersAroundAnEscapedQuoteLiteral(t *testing.T) {
	const query = `SELECT "value" FROM "options" WHERE "key" = ? AND "value" LIKE 'it''s?x' AND "other" = ?`

	const want = `SELECT "value" FROM "options" WHERE "key" = $1 AND "value" LIKE 'it''s?x' AND "other" = $2`

	if bound := DialectPostgresql.bind(query); bound != want {
		t.Errorf("bound query = %s, want %s: the ? inside the literal is not a placeholder, and the numbering must not count it", bound, want)
	}
}
