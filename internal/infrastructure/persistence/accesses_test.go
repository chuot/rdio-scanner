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
	"fmt"
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

// Accesses that share a display order keep the order the table returns them in: the
// legacy `server/access.go` `Read` used the unstable `sort.Slice`, and this server's
// stable sort is the deliberate repair.
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

// A new access stores with no identity written, the expiration as Unix seconds, and the
// scope as the JSON the legacy server stored.
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

// A save that adds a new access still removes the stored access the submitted list
// dropped: the legacy `server/access.go` `Accesses.Write` returned at the first new access
// when checking stored ids, so it deleted nothing, and this server repairs that rule.
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

// An identity the table does not have becomes a new access with a new identity, not an
// edit of a row that is not there, and the stored accesses it does not name are stale.
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

// An empty submitted list makes every stored row stale: the table ends with no accesses,
// which is the state that makes a server unrestricted.
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

// A scope that was never supplied and a scope that names nothing are two different states,
// stored as two different texts.
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

// Expiration zero is not an instant: an access with no expiration never runs out.
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

// The submitted list becomes what the server enforces before anything is written, and a
// write that fails does not take the new configuration back out again.
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

// After a write that succeeded, the read-back gives the administrator the identities the
// table assigned.
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

// AllAccesses hands out a copy: a caller that sorts, trims, or edits the slice it was
// given must not change the list the server enforces with.
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

// The expiration, limit, and order columns are signed and the list reads them into
// unsigned fields: a stored -1 clamps to zero with a warning to the operator, never a
// silent policy change.
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

// The save asks the table once, in one statement, which of the submitted identities are
// stored; one COUNT per submitted row cost a round trip per row inside the save's
// transaction, and the row outcomes are unchanged by the batching.
func TestSaveAccessesAsksTheTableOnceWhichRowsItIsSaving(t *testing.T) {
	db, counted := openCountedSchemaDatabase(t)

	for i := 1; i <= 20; i++ {
		const query = `INSERT INTO "accesses" ("code", "expiration", "ident", "limit", "order", "systems") VALUES (?, ?, ?, 0, ?, '"*"')`

		if _, err := db.Exec(query, fmt.Sprintf("code-%02d", i), int64(1893456000+i), fmt.Sprintf("ident-%02d", i), int64(i)); err != nil {
			t.Fatalf("seeding access %d: %v", i, err)
		}
	}

	accesses := NewAccesses(db, DialectSqlite)

	list, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	list[0].Code = "renamed"

	counted.reset()

	if err := accesses.SaveAccesses(context.Background(), list); err != nil {
		t.Fatalf("SaveAccesses: %v", err)
	}

	// Row outcomes first: the batching changed nothing about what is stored.
	var code string

	if err := db.QueryRow(`SELECT "code" FROM "accesses" WHERE "accessId" = 1`).Scan(&code); err != nil {
		t.Fatalf("reading the renamed access: %v", err)
	}

	if code != "renamed" {
		t.Errorf("the stored access reads back as %q, want renamed", code)
	}

	if got := countRows(t, db, "accesses"); got != 20 {
		t.Errorf("the accesses table holds %d rows, want the twenty submitted", got)
	}

	// The statements: the per-row COUNT is gone, and one membership statement
	// remains.
	if n := counted.count(`SELECT COUNT(*) FROM "accesses" WHERE "accessId" = ?`); n != 0 {
		t.Errorf("the save ran %d per-row COUNT statements, want none: a hundred-row list must not cost a hundred round trips", n)
	}

	if n := counted.count(`SELECT "accessId" FROM "accesses" WHERE "accessId" IN (`); n != 1 {
		t.Errorf("the save ran %d membership statements, want exactly one", n)
	}
}

// The cache answers while a save is in flight: the mutex must not be held across every
// statement of the save, because a slow save would stop the listener checks that read
// the cached list.
func TestAllAccessesAnswersFromTheCacheWhileASaveIsStillWalkingTheDatabase(t *testing.T) {
	db, counted := openCountedSchemaDatabase(t)

	const query = `INSERT INTO "accesses" ("code", "expiration", "ident", "limit", "order", "systems") VALUES ('cached', 1893456000, 'cached-ident', 0, 1, '"*"')`

	if _, err := db.Exec(query); err != nil {
		t.Fatalf("seeding the cached access: %v", err)
	}

	accesses := NewAccesses(db, DialectSqlite)

	cached, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if len(cached) != 1 {
		t.Fatalf("the cache holds %d accesses, want the one seeded", len(cached))
	}

	// The save the administrator is asking for, on a database that takes 200 ms
	// a statement: delete, ten rows, the read back — seconds of statements.
	saved := domain.Accesses{}

	for i := 1; i <= 10; i++ {
		saved = append(saved, domain.Access{Code: fmt.Sprintf("saving-%02d", i), Expiration: 1893456000, Ident: fmt.Sprintf("saving-%02d", i)})
	}

	counted.setDelay(200 * time.Millisecond)

	finished := make(chan struct{})

	go func() {
		defer close(finished)

		if err := accesses.SaveAccesses(context.Background(), saved); err != nil {
			t.Errorf("SaveAccesses: %v", err)
		}
	}()

	// The save is in flight — its first statements are walking the database.
	time.Sleep(100 * time.Millisecond)

	started := time.Now()

	list, err := accesses.AllAccesses(context.Background())
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if elapsed > 250*time.Millisecond {
		t.Errorf("the cache answer took %v with a save in flight, want the cached list back without waiting on the save's statements", elapsed)
	}

	// The answer is one of the two complete states: the cached list, or the
	// submitted list — which takes effect the moment the save begins, the
	// legacy server's order of operations. What it must never be is a list
	// assembled halfway through the save's statements.
	if len(list) == 1 && list[0].Code == "cached" {
		// The cached list answered, as it does when the save has not reached
		// its first assignment yet.
	} else if len(list) == 10 && list[0].Code == "saving-01" {
		// The submitted list has taken effect; the save is still walking the
		// database behind it.
	} else {
		t.Errorf("the cache answered %v, want one complete list — the cached one or the submitted one, never a state assembled halfway through the save's statements", list)
	}

	<-finished

	counted.setDelay(0)

	// The save is done: the stored rows are what the cache now answers, read
	// back as the legacy server reads them back.
	after, err := accesses.AllAccesses(context.Background())
	if err != nil {
		t.Fatalf("AllAccesses: %v", err)
	}

	if len(after) != 10 {
		t.Errorf("the cache holds %d accesses after the save, want the ten saved", len(after))
	}
}
