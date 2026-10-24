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
	"strings"
	"testing"
	"time"

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

// Both directions of the `disabled` column: the legacy server wrote `%t` into an
// `integer(1)` column and sqlite read that as 1 and 0; this server writes a bound
// boolean to the same column, and both read it back as a Go bool.
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

// A flag stored as the text `true` rather than the integer 1 still reads as the boolean
// it means.
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

// A save that adds a new credential still removes the stored one the list dropped: the
// legacy `server/apikey.go` `Apikeys.Write` stopped its loop at the first new credential,
// and this server repairs that rule, as the accesses repository does.
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

// A credential whose ident contains a single quote saves: the write is parameterized,
// where the legacy `server/apikey.go` `Apikeys.Write` interpolated `ident` and `key` with
// no escaping at all and an apostrophe failed the whole save.
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

// AllApikeys hands out a copy: a caller that sorts, trims, or edits the slice it was
// given must not change the credential list the server enforces with.
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

// The order column is signed and the list reads it into an unsigned field: a stored -3
// clamps to zero and the clamp is reported.
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

// The credential save asks the table once, in one statement, which of the submitted
// identities are stored, and the row outcomes are unchanged.
func TestSaveApikeysAsksTheTableOnceWhichRowsItIsSaving(t *testing.T) {
	db, counted := openCountedSchemaDatabase(t)

	for i := 1; i <= 20; i++ {
		const query = `INSERT INTO "apikeys" ("disabled", "ident", "key", "order", "systems") VALUES (0, ?, ?, ?, '"*"')`

		if _, err := db.Exec(query, fmt.Sprintf("ident-%02d", i), fmt.Sprintf("key-%02d", i), int64(i)); err != nil {
			t.Fatalf("seeding apikey %d: %v", i, err)
		}
	}

	apikeys := NewApikeys(db, DialectSqlite)

	list, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	list[0].Ident = "renamed-key"

	counted.reset()

	if err := apikeys.SaveApikeys(context.Background(), list); err != nil {
		t.Fatalf("SaveApikeys: %v", err)
	}

	var ident string

	if err := db.QueryRow(`SELECT "ident" FROM "apikeys" WHERE "apikeyId" = 1`).Scan(&ident); err != nil {
		t.Fatalf("reading the renamed credential: %v", err)
	}

	if ident != "renamed-key" {
		t.Errorf("the stored credential reads back as %q, want renamed-key", ident)
	}

	if got := countRows(t, db, "apikeys"); got != 20 {
		t.Errorf("the apikeys table holds %d rows, want the twenty submitted", got)
	}

	if n := counted.count(`SELECT COUNT(*) FROM "apikeys" WHERE "apikeyId" = ?`); n != 0 {
		t.Errorf("the save ran %d per-row COUNT statements, want none: a hundred-row list must not cost a hundred round trips", n)
	}

	if n := counted.count(`SELECT "apikeyId" FROM "apikeys" WHERE "apikeyId" IN (`); n != 1 {
		t.Errorf("the save ran %d membership statements, want exactly one", n)
	}
}

// The credential twin of the access cache: the listener check answers from the cache
// while the save's statements are still running.
func TestAllApikeysAnswersFromTheCacheWhileASaveIsStillWalkingTheDatabase(t *testing.T) {
	db, counted := openCountedSchemaDatabase(t)

	const query = `INSERT INTO "apikeys" ("disabled", "ident", "key", "order", "systems") VALUES (0, 'cached-key', 'cached-secret', 1, '"*"')`

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("seeding the cached credential: %v", err)
	}

	apikeys := NewApikeys(db, DialectSqlite)

	cached, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if len(cached) != 1 {
		t.Fatalf("the cache holds %d credentials, want the one seeded", len(cached))
	}

	saved := domain.Apikeys{}

	for i := 1; i <= 10; i++ {
		saved = append(saved, domain.Apikey{Ident: fmt.Sprintf("saving-%02d", i), Key: fmt.Sprintf("saving-key-%02d", i)})
	}

	counted.setDelay(200 * time.Millisecond)

	finished := make(chan struct{})

	go func() {
		defer close(finished)

		if err := apikeys.SaveApikeys(context.Background(), saved); err != nil {
			t.Errorf("SaveApikeys: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	started := time.Now()

	list, err := apikeys.AllApikeys(context.Background())
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if elapsed > 250*time.Millisecond {
		t.Errorf("the cache answer took %v with a save in flight, want the cached list back without waiting on the save's statements", elapsed)
	}

	// One complete state, as in the accesses twin: the cached list, or the
	// submitted list that takes effect the moment the save begins.
	if len(list) == 1 && list[0].Ident == "cached-key" {
		// The cached list answered.
	} else if len(list) == 10 && list[0].Ident == "saving-01" {
		// The submitted list has taken effect.
	} else {
		t.Errorf("the cache answered %v, want one complete list — the cached one or the submitted one, never a state assembled halfway through the save's statements", list)
	}

	<-finished

	counted.setDelay(0)

	after, err := apikeys.AllApikeys(context.Background())
	if err != nil {
		t.Fatalf("AllApikeys: %v", err)
	}

	if len(after) != 10 {
		t.Errorf("the cache holds %d credentials after the save, want the ten saved", len(after))
	}
}
