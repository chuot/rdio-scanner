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

package domain

import (
	"testing"
)

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

// TestApikeysValidateRejectsAnEmptyKey: a row stored with no key matches every
// keyless upload, which turns an empty credential field in the administrator's page into an open
// door. The list is refused at apply time, before it can be saved.
func TestApikeysValidateRejectsAnEmptyKey(t *testing.T) {
	apikeys := Apikeys{
		{ID: 1, Ident: "dispatch", Key: "dispatch-key"},
		{ID: 2, Ident: "nameless"},
	}

	if err := apikeys.Validate(); err == nil {
		t.Error("Validate = nil, want the keyless row refused: a row with no key matches every keyless upload")
	}
}

// The rule must not cost the server its endpoint: a list of real keys is accepted.
func TestApikeysValidateAcceptsAListOfRealKeys(t *testing.T) {
	apikeys := Apikeys{
		{ID: 1, Ident: "dispatch", Key: "dispatch-key"},
		{ID: 2, Ident: "harbour", Key: "harbour-key"},
	}

	if err := apikeys.Validate(); err != nil {
		t.Errorf("Validate = %v, want a list of real keys accepted", err)
	}
}

// TestApikeysByKeyRefusesAKeylessPresentation: even a list that already holds a keyless row —
// saved by an older server — must not answer a keyless upload. The lookup itself refuses the
// empty presentation.
func TestApikeysByKeyRefusesAKeylessPresentation(t *testing.T) {
	apikeys := Apikeys{
		{ID: 1, Ident: "nameless"},
		{ID: 2, Ident: "dispatch", Key: "dispatch-key"},
	}

	if apikey, ok := apikeys.ByKey(""); ok {
		t.Errorf("ByKey of nothing = %+v, want no credential: an empty key must match nothing", apikey)
	}
}
