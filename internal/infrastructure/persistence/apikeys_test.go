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
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

func TestAllApikeysReadsTheCredentialsTheLegacyServerStored(t *testing.T) {
	apikeys := NewApikeys(openLegacyDatabase(t), DialectSqlite)

	list, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if len(list) != 2 {
		t.Fatalf("AllApikeys returned %d credentials, want the two the legacy server stored", len(list))
	}

	if list[0].Ident != "scanner-key" || list[1].Ident != "scoped-key" {
		t.Errorf("read back %q then %q, want them in the stored display order", list[0].Ident, list[1].Ident)
	}

	if list[0].Key != "fixture-apikey-0123456789" {
		t.Errorf("the first credential is keyed %q, want the key the fixture stores", list[0].Key)
	}

	for _, apikey := range list {
		if apikey.Disabled {
			t.Errorf("%q reads back as disabled, want it enabled", apikey.Ident)
		}
	}

	if !list[0].Scope.Wildcard {
		t.Errorf("the wildcard credential reads back as %+v", list[0].Scope)
	}

	if !list[1].Scope.Present || list[1].Scope.Wildcard {
		t.Fatalf("the scoped credential reads back as %+v, want the explicit scope the fixture stores", list[1].Scope)
	}

	if !list[1].Accepts(callOf(10001, 1514000)) || list[1].Accepts(callOf(10001, 1514001)) || list[1].Accepts(callOf(10002, 1520000)) {
		t.Errorf("the scope read from the fixture accepts the wrong calls: %+v", list[1].Scope)
	}
}

func TestAllApikeysWritesTheScopeBackAsTheTextTheLegacyServerStored(t *testing.T) {
	db := openLegacyDatabase(t)
	apikeys := NewApikeys(db, DialectSqlite)

	list, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	stored := map[uint64]string{}

	for _, apikey := range list {
		stored[apikey.ID] = readApikeyScopeText(t, db, apikey.ID)
	}

	if stored[1] != `"*"` {
		t.Fatalf("the fixture stores %s, want the quoted wildcard the legacy server wrote", stored[1])
	}

	if stored[2] != `[{"id":10001,"talkgroups":[1514000]}]` {
		t.Fatalf("the fixture stores %s, want the explicit scope the legacy server wrote", stored[2])
	}

	if err := apikeys.SaveApikeys(context.Background(), list); err != nil {
		t.Fatalf("SaveApikeys: %v", err)
	}

	for _, apikey := range list {
		written := readApikeyScopeText(t, db, apikey.ID)

		if written != stored[apikey.ID] {
			t.Errorf("the scope read from %s is written back as %s", stored[apikey.ID], written)
		}
	}
}

func TestAllApikeysAnswersFromTheLoadedListRatherThanFromTheDatabase(t *testing.T) {
	db := openLegacyDatabase(t)
	apikeys := NewApikeys(db, DialectSqlite)

	before, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if _, err := db.Exec(`DELETE FROM "apikeys" WHERE "apikeyId" = 2`); err != nil {
		t.Fatalf("DELETE: %v", err)
	}

	after, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if len(after) != len(before) {
		t.Fatalf("the second read returned %d credentials after the stored row was deleted, want the %d the server accepts uploads against", len(after), len(before))
	}

	if _, found := after.ByKey("fixture-apikey-scoped"); !found {
		t.Error("the credential the server is running with disappeared from the read")
	}
}

// TestSaveApikeysStoresADisabledCredentialTheLegacyServerReadsBack proves both directions of the
// `disabled` column.
//
// The legacy server writes the flag with `%t` into an `integer(1)` column, and sqlite reads the
// bare `true` and `false` that produces as the integer literals 1 and 0 — which is what the
// fixture's `downstreams.disabled` actually holds. This server writes the flag as a bound boolean,
// which the same column stores the same way, and both servers read it back with a plain scan into
// a Go bool.
func TestSaveApikeysStoresADisabledCredentialTheLegacyServerReadsBack(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	apikeys := NewApikeys(db, DialectSqlite)

	list := domain.Apikeys{
		{ID: 0, Disabled: true, Ident: "off", Key: "off-key", Order: 1, Scope: wildcardScope(t)},
		{ID: 0, Disabled: false, Ident: "on", Key: "on-key", Order: 2, Scope: wildcardScope(t)},
	}

	if err := apikeys.SaveApikeys(context.Background(), list); err != nil {
		t.Fatalf("SaveApikeys: %v", err)
	}

	for _, tc := range []struct {
		ident string
		want  int64
	}{
		{"off", 1},
		{"on", 0},
	} {
		var (
			value int64
			kind  string
		)

		query := `SELECT "disabled", typeof("disabled") FROM "apikeys" WHERE "ident" = ?`

		if err := db.QueryRow(query, tc.ident).Scan(&value, &kind); err != nil {
			t.Fatalf("%s: %v", query, err)
		}

		if kind != "integer" || value != tc.want {
			t.Errorf("%s stores disabled as %s %d, want the integer %d the legacy server's own read accepts", tc.ident, kind, value, tc.want)
		}
	}

	// A new adapter over the same table is what the other server starting up is.
	reread := NewApikeys(db, DialectSqlite)

	stored, err := reread.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if !stored[0].Disabled || stored[1].Disabled {
		t.Errorf("the stored credentials read back as disabled=%v and disabled=%v, want true then false", stored[0].Disabled, stored[1].Disabled)
	}
}

// TestAllApikeysReadsADisabledFlagStoredAsTextAsTheBooleanItMeans covers the other shape the
// column can carry: a row whose flag is the text `true` rather than the integer 1.
func TestAllApikeysReadsADisabledFlagStoredAsTextAsTheBooleanItMeans(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")

	const query = `INSERT INTO "apikeys" ("disabled", "ident", "key", "order", "systems") VALUES ('true', 'text flag', 'text-flag-key', 1, '"*"')`

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	stored, err := NewApikeys(db, DialectSqlite).AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if len(stored) != 1 || !stored[0].Disabled {
		t.Errorf("a credential stored with the text true reads back as %+v, want it disabled", stored)
	}
}

func TestSaveApikeysRemovesTheStoredCredentialTheSubmittedListDrops(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	apikeys := NewApikeys(db, DialectSqlite)

	first := domain.Apikeys{
		{ID: 0, Ident: "keep", Key: "keep-key", Order: 1, Scope: wildcardScope(t)},
		{ID: 0, Ident: "drop", Key: "drop-key", Order: 2, Scope: wildcardScope(t)},
	}

	if err := apikeys.SaveApikeys(context.Background(), first); err != nil {
		t.Fatalf("SaveApikeys: %v", err)
	}

	kept, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if len(kept) != 2 {
		t.Fatalf("the first save stored %d credentials, want 2", len(kept))
	}

	if err := apikeys.SaveApikeys(context.Background(), domain.Apikeys{kept[0]}); err != nil {
		t.Fatalf("SaveApikeys: %v", err)
	}

	if count := countRows(t, db, "apikeys"); count != 1 {
		t.Errorf("the table has %d credentials after a save that names one, want 1", count)
	}

	list, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if len(list) != 1 || list[0].Key != "keep-key" {
		t.Errorf("the server is running with %+v, want only the credential the save named", list)
	}
}

// TestSaveApikeysRemovesAStoredCredentialEvenWhenTheSubmittedListNamesANewOne pins the same repaired
// reconcile as the accesses one, recorded as Q33 in decision-preserved-quirks.md:
// The legacy `server/apikey.go` `Apikeys.Write` stops its "is this stored row still named?" loop at the first
// submitted credential with no identity, so a save that adds one credential deletes none.
func TestSaveApikeysRemovesAStoredCredentialEvenWhenTheSubmittedListNamesANewOne(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	apikeys := NewApikeys(db, DialectSqlite)

	stored := domain.Apikeys{
		{ID: 0, Ident: "keep", Key: "keep-key", Order: 1, Scope: wildcardScope(t)},
		{ID: 0, Ident: "remove me", Key: "remove-me-key", Order: 2, Scope: wildcardScope(t)},
	}

	if err := apikeys.SaveApikeys(context.Background(), stored); err != nil {
		t.Fatalf("SaveApikeys: %v", err)
	}

	kept, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	submitted := domain.Apikeys{kept[0], {ID: 0, Ident: "new", Key: "new-key", Order: 3, Scope: wildcardScope(t)}}

	if err := apikeys.SaveApikeys(context.Background(), submitted); err != nil {
		t.Fatalf("SaveApikeys: %v", err)
	}

	if count := countRows(t, db, "apikeys"); count != 2 {
		t.Fatalf("the table has %d credentials, want 2: the credential the list dropped must go even though the list also names a new one", count)
	}

	list, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if _, found := list.ByKey("remove-me-key"); found {
		t.Errorf("the stored credentials are %#v, want the credential the list dropped removed", list)
	}
}

// TestSaveApikeysStoresACredentialWhoseIdentContainsASingleQuote pins the parameterized write.
//
// The legacy `server/apikey.go` `Apikeys.Write` interpolates `ident` and `key` with `fmt.Sprintf` and no
// escaping at all — unlike `Accesses.Write`, which calls `escapeQuotes` — so a credential named
// with an apostrophe has always failed the whole save, transaction and all.
func TestSaveApikeysStoresACredentialWhoseIdentContainsASingleQuote(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	apikeys := NewApikeys(db, DialectSqlite)

	list := domain.Apikeys{{ID: 0, Ident: "Harbour's key", Key: "harbour's-key", Order: 1, Scope: wildcardScope(t)}}

	if err := apikeys.SaveApikeys(context.Background(), list); err != nil {
		t.Fatalf("SaveApikeys: %v", err)
	}

	stored, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if len(stored) != 1 || stored[0].Ident != "Harbour's key" || stored[0].Key != "harbour's-key" {
		t.Errorf("the credential reads back as %+v, want the submitted ident and key with the quote intact", stored)
	}
}

func TestSaveApikeysKeepsTheSubmittedCredentialsInEffectWhenTheStoreRejectsTheWrite(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	apikeys := NewApikeys(db, DialectSqlite)

	if _, err := apikeys.AllApikeys(context.Background()); err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if _, err := db.Exec(`DROP TABLE "apikeys"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	submitted := domain.Apikeys{{ID: 0, Ident: "unstored", Key: "unstored-key", Order: 1, Scope: wildcardScope(t)}}

	if err := apikeys.SaveApikeys(context.Background(), submitted); err == nil {
		t.Fatal("a save against a database that cannot be written is a failure the caller is told about")
	}

	live, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if len(live) != 1 || live[0].Key != "unstored-key" {
		t.Errorf("the server is running with %+v, want the submitted credential", live)
	}
}

func TestSaveApikeysReportsTheLegacyFailureShape(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	apikeys := NewApikeys(db, DialectSqlite)

	if _, err := db.Exec(`DROP TABLE "apikeys"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	err := apikeys.SaveApikeys(context.Background(), domain.Apikeys{})

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "apikeys.write: ") {
		t.Errorf("error = %v, want the legacy apikeys.write shape", err)
	}
}

func TestAllApikeysReportsTheLegacyFailureShape(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")

	if _, err := db.Exec(`DROP TABLE "apikeys"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	_, err := NewApikeys(db, DialectSqlite).AllApikeys(context.Background())

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "apikeys.read: ") {
		t.Errorf("error = %v, want the legacy apikeys.read shape", err)
	}
}

func readApikeyScopeText(t *testing.T, db *sql.DB, id uint64) string {
	t.Helper()

	query := `SELECT "systems" FROM "apikeys" WHERE "apikeyId" = ?`

	var text string
	if err := db.QueryRow(query, int64(id)).Scan(&text); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return text
}

// TestMutatingTheApikeysListTheCacheHandsOutDoesNotChangeTheEnforcedPolicy (REQ-017,
// CA-017.1): the credential list the server enforces with is the slice AllApikeys
// hands out, not a copy of it. A caller that edits what it was handed — a sort, a
// trim, a field — edits the enforced list, and the next AllApikeys answers from it.
// The cache must hand out a copy.
func TestMutatingTheApikeysListTheCacheHandsOutDoesNotChangeTheEnforcedPolicy(t *testing.T) {
	apikeys := NewApikeys(openLegacyDatabase(t), DialectSqlite)

	first, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	first[0].Key = "scribbled-over"
	first[0].Disabled = true

	second, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if second[0].Key != "fixture-apikey-0123456789" || second[0].Disabled {
		t.Errorf("the enforced credential reads back as %+v, want the stored scanner key enabled: the cache handed its own slice to the caller", second[0])
	}
}

// TestStoredNegativeApikeyOrderReadsAsZeroWithAWarning (REQ-017, CA-017.2): the
// order column is signed and the list reads it into an unsigned field; a stored
// -3 becomes the largest display order there is, and the credential moves to the
// end of every list the operator sees. The read clamps it to zero and says so.
func TestStoredNegativeApikeyOrderReadsAsZeroWithAWarning(t *testing.T) {
	db := openLegacyDatabase(t)

	defer db.Close()

	const query = `INSERT INTO "apikeys" ("disabled", "ident", "key", "order", "systems") VALUES (0, 'old-key', 'legacy-negative-order', -3, '"*"')`

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	apikeys := NewApikeys(db, DialectSqlite)

	warnings := []string{}

	apikeys.OnWarn(func(message string) {
		warnings = append(warnings, message)
	})

	list, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	var legacy domain.Apikey

	for _, apikey := range list {
		if apikey.Key == "legacy-negative-order" {
			legacy = apikey
		}
	}

	if legacy.Order != 0 {
		t.Errorf("the stored -3 order reads as %d, want 0: a negative order must not become the last credential in every list", legacy.Order)
	}

	if len(warnings) == 0 {
		t.Fatal("the clamp was said to nobody, want a warning row naming the credential and the value it held")
	}

	if !strings.Contains(warnings[0], "old-key") {
		t.Errorf("the warning says %q, want it to name the credential it clamped", warnings[0])
	}
}
