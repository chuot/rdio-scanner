// Copyright (C) 2026 rdio-scanner contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
// LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
// CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
// SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
// CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
// ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
// POSSIBILITY OF SUCH USE OR OTHER DEALINGS IN THE SOFTWARE.

package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// wildcardScope is the scope the legacy server stores as the text `"*"`: every system, every
// talkgroup.
func wildcardScope(t *testing.T) domain.AccessScope {
	t.Helper()

	scope, ok := domain.ParseAccessScope("*")

	if !ok || !scope.Wildcard {
		t.Fatalf("the wildcard scope reads as %+v (%v), want a present wildcard", scope, ok)
	}

	return scope
}

// callOf is the pair of references a scope is checked against.
func callOf(systemRef, talkgroupRef uint) domain.Call {
	return domain.Call{
		System:    domain.System{SystemRef: systemRef},
		Talkgroup: domain.Talkgroup{TalkgroupRef: talkgroupRef},
	}
}

func readAccessRow(t *testing.T, db *sql.DB, id int64) (code string, expiration int64, ident string, limit int64, order int64, systems string) {
	t.Helper()

	query := `SELECT "code", "expiration", "ident", "limit", "order", "systems" FROM "accesses" WHERE "accessId" = ?`

	if err := db.QueryRow(query, id).Scan(&code, &expiration, &ident, &limit, &order, &systems); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return code, expiration, ident, limit, order, systems
}

func readAccessScopeText(t *testing.T, db *sql.DB, id uint64) string {
	t.Helper()

	query := `SELECT "systems" FROM "accesses" WHERE "accessId" = ?`

	var text string
	if err := db.QueryRow(query, int64(id)).Scan(&text); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return text
}

func TestAllAccessesReadsTheAccessesTheLegacyServerStored(t *testing.T) {
	accesses := NewAccesses(openLegacyDatabase(t), DialectSqlite)

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if len(list) != 2 {
		t.Fatalf("AllAccesses returned %d accesses, want the two the legacy server stored", len(list))
	}

	first, second := list[0], list[1]

	if first.ID != 1 || first.Code != "dispatch-code" || first.Ident != "dispatch" {
		t.Errorf("the first access reads back as %+v, want the stored dispatch access", first)
	}

	if first.Expiration != 1893456000 {
		t.Errorf("the stored expiration reads back as %d, want the Unix seconds the legacy server wrote", first.Expiration)
	}

	if first.Limit != 2 || first.Order != 1 {
		t.Errorf("the stored access reads back with limit %d and order %d, want 2 and 1", first.Limit, first.Order)
	}

	if second.ID != 2 || second.Code != "harbour-code" || second.Ident != "harbour" {
		t.Errorf("the second access reads back as %+v, want the stored harbour access", second)
	}

	if second.Expiration != 1893542400 || second.Limit != 0 || second.Order != 2 {
		t.Errorf("the stored harbour access reads back as %d/%d/%d, want 1893542400/0/2", second.Expiration, second.Limit, second.Order)
	}

	if !list[1].Scope.Wildcard || !list[1].Scope.Present {
		t.Errorf("the harbour access reads back with scope %+v, want the wildcard the fixture stores", list[1].Scope)
	}

	if list[0].Scope.Wildcard || !list[0].Scope.Present {
		t.Fatalf("the dispatch access reads back with scope %+v, want the explicit scope the fixture stores", list[0].Scope)
	}

	if len(list[0].Scope.Systems) != 1 || list[0].Scope.Systems[0].SystemRef != 10001 {
		t.Fatalf("the dispatch access scope is %+v, want the one system the fixture names", list[0].Scope.Systems)
	}

	if !list[0].Grants(callOf(10001, 1514000)) || !list[0].Grants(callOf(10001, 1514001)) {
		t.Error("the scope read from the fixture does not grant the talkgroups it names")
	}

	if list[0].Grants(callOf(10001, 1514002)) || list[0].Grants(callOf(10002, 1520000)) {
		t.Error("the scope read from the fixture grants calls outside itself")
	}
}

func TestAllAccessesOrdersTheStoredAccessesByTheirDisplayOrder(t *testing.T) {
	db := openLegacyDatabase(t)

	const query = `INSERT INTO "accesses" ("code", "expiration", "ident", "limit", "order", "systems") VALUES ('early-code', 1893456000, 'early', 0, 0, '"*"')`

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	list, err := NewAccesses(db, DialectSqlite).AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if len(list) != 3 {
		t.Fatalf("AllAccesses returned %d accesses, want 3", len(list))
	}

	if list[0].Ident != "early" || list[1].Ident != "dispatch" || list[2].Ident != "harbour" {
		t.Errorf("the accesses read back as %v, want them in the stored display order", identsOf(list))
	}
}

// TestAllAccessesKeepsAccessesThatShareAnOrderInTheOrderTheTableReturnsThem records the one place
// where this code is deliberately better than the legacy server rather than equal to it.
//
// The legacy `server/access.go` `Read` sorts with `sort.Slice`, which is not stable: two accesses stored under
// the same display order came back in whatever order the sort happened to produce, so the access
// list the administrator saw changed between restarts. domain.Accesses.Ordered sorts stably, and
// the order the table returns rows in is what survives.
func TestAllAccessesKeepsAccessesThatShareAnOrderInTheOrderTheTableReturnsThem(t *testing.T) {
	db := openLegacyDatabase(t)

	for _, ident := range []string{"tied first", "tied second"} {
		const query = `INSERT INTO "accesses" ("code", "expiration", "ident", "limit", "order", "systems") VALUES (?, 1893456000, ?, 0, 1, '"*"')`

		if _, err := db.Exec(query, "tied-"+ident, ident); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}

	list, err := NewAccesses(db, DialectSqlite).AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if len(list) != 4 {
		t.Fatalf("AllAccesses returned %d accesses, want 4", len(list))
	}

	want := []string{"dispatch", "tied first", "tied second", "harbour"}
	if got := identsOf(list); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the accesses read back as %v, want %v", got, want)
	}
}

func TestAllAccessesAnswersFromTheLoadedListRatherThanFromTheDatabase(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	before, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if _, err := db.Exec(`DELETE FROM "accesses" WHERE "accessId" = 2`); err != nil {
		t.Fatalf("DELETE: %v", err)
	}

	after, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if len(after) != len(before) {
		t.Fatalf("the second read returned %d accesses after the stored row was deleted, want the %d the server is enforcing", len(after), len(before))
	}

	if _, found := after.ByCode("harbour-code"); !found {
		t.Error("the access the server is running with disappeared from the read")
	}
}

func TestAllAccessesReportsTheLegacyFailureShape(t *testing.T) {
	db := openLegacyDatabase(t)

	if _, err := db.Exec(`DROP TABLE "accesses"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	_, err := NewAccesses(db, DialectSqlite).AllAccesses(context.Background())

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "accesses.read: ") {
		t.Errorf("error = %v, want the legacy accesses.read shape", err)
	}
}

// TestSaveAccessesStoresANewAccessInTheShapeTheLegacyServerReadsBack pins the insert: the identity
// is never written, the expiration is stored as Unix seconds, and the scope is stored as the JSON
// the legacy server stored.
func TestSaveAccessesStoresANewAccessInTheShapeTheLegacyServerReadsBack(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	accesses := NewAccesses(db, DialectSqlite)

	var value any
	if err := json.Unmarshal([]byte(`[{"id":10002,"talkgroups":[1520000]}]`), &value); err != nil {
		t.Fatalf("the submitted scope is not the shape a configuration payload has: %v", err)
	}

	scope, ok := domain.ParseAccessScope(value)
	if !ok {
		t.Fatal("the submitted scope is not one the domain reads")
	}

	next := domain.Accesses{{
		ID:         0,
		Code:       "new-code",
		Expiration: 1893628800,
		Ident:      "new access",
		Limit:      3,
		Order:      3,
		Scope:      scope,
	}}

	if err := accesses.SaveAccesses(context.Background(), next); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	if count := countRows(t, db, "accesses"); count != 1 {
		t.Fatalf("the table has %d accesses after a save that adds one, want 1", count)
	}

	var storedID int64
	if err := db.QueryRow(`SELECT "accessId" FROM "accesses" WHERE "code" = 'new-code'`).Scan(&storedID); err != nil {
		t.Fatalf("the stored access is missing: %v", err)
	}

	code, expiration, ident, limit, order, systems := readAccessRow(t, db, storedID)

	if code != "new-code" || ident != "new access" {
		t.Errorf("the stored access is %q / %q, want new-code / new access", code, ident)
	}

	if expiration != 1893628800 {
		t.Errorf("the stored expiration is %d, want the Unix seconds the access carries", expiration)
	}

	if limit != 3 || order != 3 {
		t.Errorf("the stored access has limit %d and order %d, want 3 and 3", limit, order)
	}

	if systems != `[{"id":10002,"talkgroups":[1520000]}]` {
		t.Errorf("the stored scope is %s, want the JSON the legacy server stores", systems)
	}

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if len(list) != 1 || list[0].ID == 0 {
		t.Fatalf("the server is running with %+v, want the stored access with the identity the table gave it", list)
	}
}

func TestSaveAccessesUpdatesTheStoredAccess(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	list[0].Code = "changed-code"
	list[0].Expiration = 1893715200
	list[0].Ident = "changed"
	list[0].Limit = 9
	list[0].Order = 7
	list[0].Scope = wildcardScope(t)

	if err := accesses.SaveAccesses(context.Background(), list); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	if count := countRows(t, db, "accesses"); count != 2 {
		t.Errorf("the table has %d accesses after a save that changed one, want 2", count)
	}

	code, expiration, ident, limit, order, systems := readAccessRow(t, db, 1)

	if code != "changed-code" || expiration != 1893715200 || ident != "changed" || limit != 9 || order != 7 || systems != `"*"` {
		t.Errorf("the stored access is %q/%d/%q/%d/%d/%s, want the submitted values", code, expiration, ident, limit, order, systems)
	}
}

func TestSaveAccessesRemovesTheStoredAccessTheSubmittedListDrops(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if err := accesses.SaveAccesses(context.Background(), domain.Accesses{list[0]}); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	if count := countRows(t, db, "accesses"); count != 1 {
		t.Errorf("the table has %d accesses after a save that names one, want 1", count)
	}

	kept, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if len(kept) != 1 || kept[0].Code != "dispatch-code" {
		t.Errorf("the server is running with %+v, want only the access the save named", kept)
	}
}

// TestSaveAccessesRemovesAStoredAccessEvenWhenTheSubmittedListNamesANewOne pins the repaired
// reconcile recorded as Q33 in decision-preserved-quirks.md.
//
// The legacy `server/access.go` `Accesses.Write` decides whether a stored row is still wanted by walking the
// submitted list and returning at the first access with no identity — the loop that answers "is this
// stored id in the new list?" treats a brand-new access as a match. A save that adds one user and
// removes another therefore deletes nothing, and the read-back that follows a successful write puts
// the removed user back in the configuration the administrator is shown. This server repairs the rule.
func TestSaveAccessesRemovesAStoredAccessEvenWhenTheSubmittedListNamesANewOne(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	// The administrator keeps the dispatch user, removes the harbour user, and adds a third.
	submitted := domain.Accesses{list[0], {
		ID:         0,
		Code:       "brand-new",
		Expiration: 1893456000,
		Ident:      "brand new",
		Order:      3,
		Scope:      wildcardScope(t),
	}}

	if err := accesses.SaveAccesses(context.Background(), submitted); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	if count := countRows(t, db, "accesses"); count != 2 {
		t.Fatalf("the table has %d accesses, want 2: the access the list dropped must go even though the list also names a new one", count)
	}

	kept, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if _, found := kept.ByCode("harbour-code"); found {
		t.Errorf("the stored accesses are %#v, want the access the list dropped removed", kept)
	}
}

// TestSaveAccessesStoresAnAccessWhoseIdentityNamesNoStoredRow pins the other half of the legacy
// write: the row is chosen by counting the stored rows with that identity, so an access submitted
// with an identity the table does not have becomes a new access with a new identity rather than an
// edit of a row that is not there — and the stored accesses it does not name are stale.
func TestSaveAccessesStoresAnAccessWhoseIdentityNamesNoStoredRow(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	next := domain.Accesses{{
		ID:         99,
		Code:       "gone-code",
		Expiration: 1893456000,
		Ident:      "gone",
		Order:      1,
		Scope:      wildcardScope(t),
	}}

	if err := accesses.SaveAccesses(context.Background(), next); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	if count := countRows(t, db, "accesses"); count != 1 {
		t.Errorf("the table has %d accesses, want 1: the submitted identity names no stored row, and the stored accesses are not named", count)
	}

	var storedID int64
	if err := db.QueryRow(`SELECT "accessId" FROM "accesses" WHERE "code" = 'gone-code'`).Scan(&storedID); err != nil {
		t.Fatalf("the submitted access was not stored: %v", err)
	}

	if storedID == 99 {
		t.Error("the table stored the access under the identity it was submitted with, which the legacy write never writes")
	}
}

// TestSaveAccessesStoresNoAccessesForAnEmptySubmittedList pins what an empty access list does to the
// table, because it is the state that makes a server unrestricted: with nothing submitted, every
// stored row is stale.
func TestSaveAccessesStoresNoAccessesForAnEmptySubmittedList(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	if _, err := accesses.AllAccesses(context.Background()); err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if err := accesses.SaveAccesses(context.Background(), domain.Accesses{}); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	if count := countRows(t, db, "accesses"); count != 0 {
		t.Errorf("the table has %d accesses after a save that names none, want 0", count)
	}

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if list.Restricted() {
		t.Error("the server still reports itself restricted after a save that stored no accesses")
	}
}

func TestSaveAccessesWritesTheScopeBackAsTheTextTheLegacyServerStored(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	stored := map[uint64]string{}

	for _, access := range list {
		stored[access.ID] = readAccessScopeText(t, db, access.ID)
	}

	if stored[1] != `[{"id":10001,"talkgroups":[1514000,1514001]}]` {
		t.Fatalf("the fixture stores %s, want the explicit scope the legacy server wrote", stored[1])
	}

	if stored[2] != `"*"` {
		t.Fatalf("the fixture stores %s, want the quoted wildcard the legacy server wrote", stored[2])
	}

	if err := accesses.SaveAccesses(context.Background(), list); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	for _, access := range list {
		written := readAccessScopeText(t, db, access.ID)

		if written != stored[access.ID] {
			t.Errorf("the scope read from %s is written back as %s", stored[access.ID], written)
		}
	}
}

// TestSaveAccessesKeepsTheDifferenceBetweenNoScopeAndAnEmptyOne pins Q9: a scope that was never
// supplied and a scope that names nothing are two different states, and the table stores them as
// two different texts.
func TestSaveAccessesKeepsTheDifferenceBetweenNoScopeAndAnEmptyOne(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	accesses := NewAccesses(db, DialectSqlite)

	empty, ok := domain.ParseAccessScope([]any{})
	if !ok || empty.Wildcard || len(empty.Systems) != 0 {
		t.Fatalf("the empty scope reads as %+v (%v), want a present scope that names nothing", empty, ok)
	}

	next := domain.Accesses{
		{ID: 0, Code: "no-scope", Expiration: 1893456000, Ident: "no scope", Order: 1},
		{ID: 0, Code: "empty-scope", Expiration: 1893456000, Ident: "empty scope", Order: 2, Scope: empty},
	}

	if err := accesses.SaveAccesses(context.Background(), next); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	var none, named string

	if err := db.QueryRow(`SELECT "systems" FROM "accesses" WHERE "code" = 'no-scope'`).Scan(&none); err != nil {
		t.Fatalf("the access with no scope is missing: %v", err)
	}

	if err := db.QueryRow(`SELECT "systems" FROM "accesses" WHERE "code" = 'empty-scope'`).Scan(&named); err != nil {
		t.Fatalf("the access with an empty scope is missing: %v", err)
	}

	if none != "null" {
		t.Errorf("an access with no scope is stored as %s, want the null the legacy server stores", none)
	}

	if named != "[]" {
		t.Errorf("an access whose scope names nothing is stored as %s, want the empty list", named)
	}

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if list[0].Scope.Present {
		t.Errorf("the access with no scope reads back as %+v, want a scope that was not supplied", list[0].Scope)
	}

	if list[0].HasScope() {
		t.Error("the access with no scope reports a scope, which is what the PIN gate reads as an authenticated client")
	}

	if !list[1].Scope.Present || list[1].Scope.Wildcard {
		t.Errorf("the access with an empty scope reads back as %+v, want a supplied scope that names nothing", list[1].Scope)
	}

	if list[1].Grants(callOf(10001, 1514000)) {
		t.Error("a scope that names nothing grants a call, which it does not in the legacy server")
	}
}

// TestSaveAccessesStoresAnAccessThatNeverExpires pins the expiration rule: zero is not an instant,
// and an access with no expiration never runs out.
func TestSaveAccessesStoresAnAccessThatNeverExpires(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	accesses := NewAccesses(db, DialectSqlite)

	next := domain.Accesses{{ID: 0, Code: "never", Expiration: 0, Ident: "never expires", Order: 1, Scope: wildcardScope(t)}}

	if err := accesses.SaveAccesses(context.Background(), next); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	var storedID int64
	if err := db.QueryRow(`SELECT "accessId" FROM "accesses" WHERE "code" = 'never'`).Scan(&storedID); err != nil {
		t.Fatalf("the stored access is missing: %v", err)
	}

	_, expiration, _, _, _, _ := readAccessRow(t, db, storedID)

	if expiration != 0 {
		t.Errorf("the stored expiration is %d, want 0", expiration)
	}

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if list[0].Expired(time.Now()) {
		t.Error("an access with no expiration reads back as expired")
	}
}

func TestSaveAccessesReportsTheFailureToStoreANewAccess(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	accesses := NewAccesses(db, DialectSqlite)

	const trigger = `CREATE TRIGGER "block_access_inserts" BEFORE INSERT ON "accesses"
		BEGIN SELECT RAISE(ABORT, 'the access list is protected'); END`

	if _, err := db.Exec(trigger); err != nil {
		t.Fatalf("CREATE TRIGGER: %v", err)
	}

	err := accesses.SaveAccesses(context.Background(), domain.Accesses{{ID: 0, Code: "blocked", Ident: "blocked", Order: 1, Scope: wildcardScope(t)}})

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "accesses.write: ") {
		t.Errorf("error = %v, want the legacy accesses.write shape", err)
	}

	if !strings.Contains(err.Error(), "protected") {
		t.Errorf("error = %v, want the reason the database refused the access", err)
	}

	if count := countRows(t, db, "accesses"); count != 0 {
		t.Errorf("the table has %d accesses after a refused save, want none: the whole save is one transaction", count)
	}
}

func TestSaveAccessesReportsTheFailureToStoreAChangedAccess(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	const trigger = `CREATE TRIGGER "block_access_updates" BEFORE UPDATE ON "accesses"
		BEGIN SELECT RAISE(ABORT, 'the access list is protected'); END`

	if _, err := db.Exec(trigger); err != nil {
		t.Fatalf("CREATE TRIGGER: %v", err)
	}

	list[0].Ident = "changed"

	err = accesses.SaveAccesses(context.Background(), list)

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "accesses.write: ") {
		t.Errorf("error = %v, want the legacy accesses.write shape", err)
	}

	if code, _, ident, _, _, _ := readAccessRow(t, db, 1); code != "dispatch-code" || ident != "dispatch" {
		t.Errorf("the stored access is %q / %q after a refused save, want the values it had", code, ident)
	}
}

func TestSaveAccessesReportsTheFailureToRemoveAStoredAccess(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	const trigger = `CREATE TRIGGER "block_access_removals" BEFORE DELETE ON "accesses"
		BEGIN SELECT RAISE(ABORT, 'the access list is protected'); END`

	if _, err := db.Exec(trigger); err != nil {
		t.Fatalf("CREATE TRIGGER: %v", err)
	}

	err = accesses.SaveAccesses(context.Background(), domain.Accesses{list[0]})

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "accesses.write: ") {
		t.Errorf("error = %v, want the legacy accesses.write shape", err)
	}

	if !strings.Contains(err.Error(), "protected") {
		t.Errorf("error = %v, want the reason the database refused the removal", err)
	}

	if count := countRows(t, db, "accesses"); count != 2 {
		t.Errorf("the table has %d accesses after a refused save, want the 2 it had: the whole save is one transaction", count)
	}
}

func TestSaveAccessesReportsTheLegacyFailureShapeOfAWriteThatCannotStart(t *testing.T) {
	db := openLegacyDatabaseWithout(t, "accesses", "apikeys", "logs", "options")
	accesses := NewAccesses(db, DialectSqlite)

	if _, err := db.Exec(`DROP TABLE "accesses"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	err := accesses.SaveAccesses(context.Background(), domain.Accesses{{ID: 0, Code: "unstored", Ident: "unstored", Order: 1, Scope: wildcardScope(t)}})

	if err == nil {
		t.Fatal("the failure is reported")
	}

	if !strings.HasPrefix(err.Error(), "accesses.write: ") {
		t.Errorf("error = %v, want the legacy accesses.write shape", err)
	}
}

// TestSaveAccessesKeepsTheSubmittedAccessesInEffectWhenTheStoreRejectsTheWrite pins §9 of
// decision-internal-ports.md, which is the legacy server's own order of operations: the submitted
// list becomes what the server enforces before anything is written, and a write that fails does not
// take the new configuration back out again.
func TestSaveAccessesKeepsTheSubmittedAccessesInEffectWhenTheStoreRejectsTheWrite(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	if _, err := accesses.AllAccesses(context.Background()); err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if _, err := db.Exec(`DROP TABLE "accesses"`); err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	submitted := domain.Accesses{{ID: 0, Code: "unstored", Expiration: 1893456000, Ident: "unstored", Order: 1, Scope: wildcardScope(t)}}

	if err := accesses.SaveAccesses(context.Background(), submitted); err == nil {
		t.Fatal("a save against a database that cannot be written is a failure the caller is told about")
	}

	live, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if len(live) != 1 || live[0].Code != "unstored" {
		t.Errorf("the server is running with %+v, want the submitted access", live)
	}
}

// TestSaveAccessesReadsBackTheStoredAccessesAfterAWriteThatSucceeded pins the read the legacy
// server's own configuration handler runs after a successful write: the identities the table
// assigned are what the administrator is shown next.
func TestSaveAccessesReadsBackTheStoredAccessesAfterAWriteThatSucceeded(t *testing.T) {
	db := openLegacyDatabase(t)
	accesses := NewAccesses(db, DialectSqlite)

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	submitted := append(list, domain.Access{ID: 0, Code: "third-code", Expiration: 1893456000, Ident: "third", Order: 3, Scope: wildcardScope(t)})

	if err := accesses.SaveAccesses(context.Background(), submitted); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	after, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if len(after) != 3 {
		t.Fatalf("the server is running with %d accesses after the save, want the 3 the table now stores", len(after))
	}

	if after[2].ID == 0 || after[2].Code != "third-code" {
		t.Errorf("the access the save added reads back as %+v, want it with the identity the table gave it", after[2])
	}
}

func identsOf(list domain.Accesses) []string {
	idents := make([]string, 0, len(list))

	for _, access := range list {
		idents = append(idents, access.Ident)
	}

	return idents
}

// TestMutatingTheAccessesListTheCacheHandsOutDoesNotChangeTheEnforcedPolicy (REQ-017,
// CA-017.1): AllAccesses answers from the loaded list, and it hands that list
// straight to the caller. A caller that sorts, trims, or edits the slice it was
// handed is editing the list the server enforces with — the next AllAccesses
// answers from the very same slice, and the server goes on enforcing what some
// caller scribbled. The cache must hand out a copy.
func TestMutatingTheAccessesListTheCacheHandsOutDoesNotChangeTheEnforcedPolicy(t *testing.T) {
	accesses := NewAccesses(openLegacyDatabase(t), DialectSqlite)

	first, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	first[0].Code = "scribbled over"
	first[0].Expiration = 1
	first[0].Limit = 9

	second, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if second[0].Code != "dispatch-code" || second[0].Expiration != 1893456000 || second[0].Limit != 2 {
		t.Errorf("the enforced access list reads back as %+v, want the stored dispatch access: the cache handed its own slice to the caller", second[0])
	}
}

// TestStoredNegativeAccessValuesReadAsZeroWithAWarning (REQ-017, CA-017.2): the
// expiration, limit, and order columns are signed, and the list reads them into
// unsigned fields. A stored -1 — the value a legacy row carries for "no
// expiration" — becomes the largest number of seconds there is: an access that
// never expires, read as a success. The read clamps negatives to zero (an
// expiration of 0 expires immediately, the safe direction) and says the clamp to
// the operator rather than letting a deployment's policy change in silence.
func TestStoredNegativeAccessValuesReadAsZeroWithAWarning(t *testing.T) {
	db := openLegacyDatabase(t)

	defer db.Close()

	const query = `INSERT INTO "accesses" ("code", "expiration", "ident", "limit", "order", "systems") VALUES ('legacy-row', -1, 'old', -5, -3, '"*"')`

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	accesses := NewAccesses(db, DialectSqlite)

	warnings := []string{}

	accesses.OnWarn(func(message string) {
		warnings = append(warnings, message)
	})

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	var legacy domain.Access

	for _, access := range list {
		if access.Code == "legacy-row" {
			legacy = access
		}
	}

	if legacy.Expiration != 0 || legacy.Limit != 0 || legacy.Order != 0 {
		t.Errorf("the stored -1, -5, -3 read as expiration %d, limit %d, order %d, want 0, 0, 0: a negative expiration must not become an access that never expires", legacy.Expiration, legacy.Limit, legacy.Order)
	}

	if len(warnings) == 0 {
		t.Fatal("the clamp was said to nobody, want a warning row naming the access and the values it held")
	}

	if !strings.Contains(warnings[0], "legacy-row") {
		t.Errorf("the warning says %q, want it to name the access it clamped", warnings[0])
	}
}
