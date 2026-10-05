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

package domain

import (
	"testing"
	"time"
)

func scopedCall() Call {
	return Call{
		System:    System{ID: 7, SystemRef: 4},
		Talkgroup: Talkgroup{ID: 21, TalkgroupRef: 1514000},
	}
}

func TestWildcardScopeGrantsEverything(t *testing.T) {
	scope, present := ParseAccessScope("*")

	if !present {
		t.Fatal("a wildcard scope must count as present")
	}

	if !scope.Grants(scopedCall()) {
		t.Error("the wildcard scope must grant any system and talkgroup")
	}
}

func TestAbsentScopeIsAbsentAndGrantsNothing(t *testing.T) {
	scope, present := ParseAccessScope(nil)

	if present {
		t.Error("a missing scope must count as absent; the PIN gate depends on telling the two apart")
	}

	if scope.Grants(scopedCall()) {
		t.Error("an absent scope must grant nothing")
	}
}

func TestScopedSystemsMatchByClientFacingReferences(t *testing.T) {
	// The legacy scope compares the system entry id against the system *reference*,
	// and the talkgroup entries against the talkgroup *reference*.
	scope, _ := ParseAccessScope([]any{
		map[string]any{"id": float64(4), "talkgroups": []any{float64(1514000)}},
	})

	if !scope.Grants(scopedCall()) {
		t.Fatal("a scope naming the call's system reference and talkgroup reference must grant it")
	}

	wrongSystem, _ := ParseAccessScope([]any{
		map[string]any{"id": float64(9), "talkgroups": []any{float64(1514000)}},
	})

	if wrongSystem.Grants(scopedCall()) {
		t.Error("a scope naming another system reference must not grant the call")
	}

	wrongTalkgroup, _ := ParseAccessScope([]any{
		map[string]any{"id": float64(4), "talkgroups": []any{float64(1514001)}},
	})

	if wrongTalkgroup.Grants(scopedCall()) {
		t.Error("a scope naming another talkgroup reference must not grant the call")
	}
}

func TestScopedSystemWithWildcardTalkgroupsGrantsAnyTalkgroup(t *testing.T) {
	scope, _ := ParseAccessScope([]any{
		map[string]any{"id": float64(4), "talkgroups": "*"},
	})

	if !scope.Grants(scopedCall()) {
		t.Fatal("a scoped system whose talkgroups are the wildcard must grant any talkgroup on it")
	}

	otherTalkgroup := scopedCall()
	otherTalkgroup.Talkgroup = Talkgroup{ID: 99, TalkgroupRef: 222000}

	if !scope.Grants(otherTalkgroup) {
		t.Error("the talkgroup wildcard must grant a different talkgroup on the same system")
	}

	otherSystem := scopedCall()
	otherSystem.System = System{ID: 8, SystemRef: 5}

	if scope.Grants(otherSystem) {
		t.Error("the talkgroup wildcard must not spill onto another system")
	}
}

func TestScopeIgnoresMalformedEntries(t *testing.T) {
	scope, present := ParseAccessScope([]any{
		"not an object",
		map[string]any{"talkgroups": []any{float64(1514000)}},
		map[string]any{"id": "not a number", "talkgroups": []any{float64(1514000)}},
	})

	if !present {
		t.Fatal("a scope made of unusable entries is still a scope")
	}

	if scope.Grants(scopedCall()) {
		t.Error("malformed entries must not grant access")
	}
}

func TestAccessApplyMap(t *testing.T) {
	access := Access{}.ApplyMap(map[string]any{
		"id":         float64(3),
		"code":       "secret-code",
		"expiration": "2026-12-31T00:00:00Z",
		"ident":      "dispatch",
		"limit":      float64(2),
		"order":      float64(7),
		"systems":    "*",
	})

	if access.ID != 3 || access.Code != "secret-code" || access.Ident != "dispatch" || access.Limit != 2 || access.Order != 7 {
		t.Errorf("ApplyMap = %+v, want the submitted scalar fields", access)
	}

	if want := uint64(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC).Unix()); access.Expiration != want {
		t.Errorf("Expiration = %d, want the submitted RFC3339 instant as %d", access.Expiration, want)
	}

	if !access.Scope.Grants(scopedCall()) {
		t.Error("ApplyMap must carry the submitted scope through")
	}
}

func TestAccessApplyMapIgnoresAnUnparsableExpiration(t *testing.T) {
	access := Access{Expiration: 100}.ApplyMap(map[string]any{"expiration": "not a date"})

	if access.Expiration != 100 {
		t.Errorf("Expiration = %d, want the previous value kept when the submitted text does not parse", access.Expiration)
	}
}

// TestAccessApplyMapClearsAnAbsentScope pins a legacy detail: the systems key is
// assigned unconditionally, so a configuration payload that omits it clears the scope.
func TestAccessApplyMapClearsAnAbsentScope(t *testing.T) {
	scoped := Access{}.ApplyMap(map[string]any{"code": "c", "systems": "*"})

	if !scoped.Scope.Grants(scopedCall()) {
		t.Fatal("the fixture should be unrestricted before the clear")
	}

	cleared := scoped.ApplyMap(map[string]any{"code": "c"})

	if cleared.Scope.Grants(scopedCall()) {
		t.Error("omitting the systems key must clear the scope, as the legacy server does")
	}
}

func TestAccessExpired(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	if (Access{}).Expired(now) {
		t.Error("an access with no expiration must never expire")
	}

	past := Access{Expiration: uint64(now.Add(-time.Hour).Unix())}
	if !past.Expired(now) {
		t.Error("an access whose expiration has passed must report expired")
	}

	future := Access{Expiration: uint64(now.Add(time.Hour).Unix())}
	if future.Expired(now) {
		t.Error("an access expiring in the future must not report expired")
	}
}

func TestAccessHasScope(t *testing.T) {
	if (Access{}).HasScope() {
		t.Error("an access with no scope must report none; the PIN gate treats it as unauthenticated")
	}

	scoped := Access{}.ApplyMap(map[string]any{"systems": "*"})
	if !scoped.HasScope() {
		t.Error("an access carrying the wildcard must report a scope")
	}

	list := Access{}.ApplyMap(map[string]any{"systems": []any{}})
	if !list.HasScope() {
		t.Error("an access carrying an empty list must still report a scope")
	}
}

func TestApikeyDefaultsToUnrestricted(t *testing.T) {
	apikey := NewApikey()

	if !apikey.Scope.Grants(scopedCall()) {
		t.Error("a freshly created upload apikey must be unrestricted, as the legacy NewApikey is")
	}
}

func TestApikeyApplyMap(t *testing.T) {
	apikey := Apikey{}.ApplyMap(map[string]any{
		"id":       float64(5),
		"disabled": true,
		"ident":    "trunk-recorder",
		"key":      "upload-key",
		"order":    float64(2),
		"systems":  []any{map[string]any{"id": float64(4), "talkgroups": "*"}},
	})

	if apikey.ID != 5 || !apikey.Disabled || apikey.Ident != "trunk-recorder" || apikey.Key != "upload-key" || apikey.Order != 2 {
		t.Errorf("ApplyMap = %+v, want the submitted fields", apikey)
	}

	if !apikey.Scope.Grants(scopedCall()) {
		t.Error("ApplyMap must carry the submitted scope through")
	}
}

func TestApikeyScopeGrantsTheSameWayAsAccess(t *testing.T) {
	scoped := Apikey{}.ApplyMap(map[string]any{
		"systems": []any{map[string]any{"id": float64(9), "talkgroups": []any{float64(1514000)}}},
	})

	if scoped.Scope.Grants(scopedCall()) {
		t.Error("an apikey scoped to another system must not grant the call")
	}

	if !(Apikey{}).ApplyMap(map[string]any{"systems": "*"}).Scope.Grants(scopedCall()) {
		t.Error("an unrestricted apikey must grant the call")
	}
}

func TestMaxAccessAuthAttemptsIsTheLegacyLimit(t *testing.T) {
	if MaxAccessAuthAttempts != 5 {
		t.Errorf("MaxAccessAuthAttempts = %d, want the legacy limit of 5", MaxAccessAuthAttempts)
	}
}

func TestAccessesUpsertByCodeAddsAnUnseenAccess(t *testing.T) {
	accesses := Accesses{{ID: 1, Code: "dispatch-code", Ident: "dispatch"}}

	next, added := accesses.UpsertByCode(Access{Code: "harbour-code", Ident: "harbour"})

	if !added {
		t.Error("an access under a code that is not on the list is a new access")
	}

	if len(next) != 2 {
		t.Errorf("accesses = %v, want the new access appended", next)
	}

	if len(accesses) != 1 {
		t.Errorf("original = %v, want the original list left alone", accesses)
	}
}

func TestAccessesUpsertByCodeUpdatesEverythingButTheOrder(t *testing.T) {
	accesses := Accesses{{ID: 1, Code: "dispatch-code", Ident: "dispatch", Limit: 1, Order: 5}}

	next, added := accesses.UpsertByCode(Access{
		Code:       "dispatch-code",
		Ident:      "dispatch-renamed",
		Expiration: 1_800_000_000,
		Limit:      3,
		Order:      9,
		Scope:      AccessScope{Present: true, Wildcard: true},
	})

	if added {
		t.Error("an access under a code already on the list is updated, not added")
	}

	if len(next) != 1 {
		t.Fatalf("accesses = %v, want one access", next)
	}

	updated := next[0]

	if updated.Ident != "dispatch-renamed" || updated.Limit != 3 || updated.Expiration != 1_800_000_000 {
		t.Errorf("updated = %+v, want the identity, limit, and expiry replaced", updated)
	}

	if !updated.Scope.Present || !updated.Scope.Wildcard {
		t.Errorf("updated scope = %+v, want the scope replaced", updated.Scope)
	}

	if updated.Order != 5 {
		t.Errorf("order = %d, want the order the access already had kept: the legacy update does not touch it", updated.Order)
	}

	if updated.ID != 1 {
		t.Errorf("id = %d, want the stored access keep its own identifier", updated.ID)
	}
}

func TestAccessesRemoveAllByIdentRemovesEveryAccessUnderTheIdentity(t *testing.T) {
	accesses := Accesses{
		{ID: 1, Code: "a", Ident: "dispatch"},
		{ID: 2, Code: "b", Ident: "dispatch"},
		{ID: 3, Code: "c", Ident: "harbour"},
	}

	next, removed := accesses.RemoveAllByIdent("dispatch")

	if !removed {
		t.Error("an identity on the list is removed")
	}

	if len(next) != 1 || next[0].Ident != "harbour" {
		t.Errorf("accesses = %v, want every access carrying the identity gone", next)
	}
}

func TestAccessesRemoveAllByIdentReportsAnUnmatchedIdentity(t *testing.T) {
	accesses := Accesses{{ID: 1, Code: "a", Ident: "dispatch"}}

	next, removed := accesses.RemoveAllByIdent("nobody")

	if removed {
		t.Error("an identity that is not on the list removed nothing")
	}

	if len(next) != 1 {
		t.Errorf("accesses = %v, want the list left as it was", next)
	}
}
