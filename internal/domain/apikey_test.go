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
