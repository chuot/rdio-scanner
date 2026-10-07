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

import "testing"

func TestUnitApplyMap(t *testing.T) {
	unit := Unit{}.ApplyMap(map[string]any{
		"id":       float64(11),
		"label":    "Dispatch 1",
		"order":    float64(2),
		"systemId": float64(5),
		"unitRef":  float64(12345),
		"unitFrom": float64(100),
		"unitTo":   float64(200),
	})

	want := Unit{ID: 11, Label: "Dispatch 1", Order: 2, SystemID: 5, UnitRef: 12345, UnitFrom: 100, UnitTo: 200}

	if unit != want {
		t.Errorf("ApplyMap = %+v, want %+v", unit, want)
	}
}

func TestUnitApplyMapKeepsAbsentKeys(t *testing.T) {
	unit := Unit{ID: 11, UnitRef: 12345}.ApplyMap(map[string]any{"label": "Dispatch 1"})

	if unit.ID != 11 || unit.UnitRef != 12345 {
		t.Errorf("ApplyMap = %+v, want ID and UnitRef preserved", unit)
	}
}

func TestUnitsByIDAndOrder(t *testing.T) {
	units := Units{{ID: 1, Label: "B", Order: 20}, {ID: 2, Label: "A", Order: 10}}

	if unit, ok := units.ByID(2); !ok || unit.Label != "A" {
		t.Errorf("ByID(2) = %+v, %v, want unit A", unit, ok)
	}

	if _, ok := units.ByID(77); ok {
		t.Error("ByID(77) reported a unit, want none")
	}

	got := units.Ordered()

	if got[0].Label != "A" || got[1].Label != "B" {
		t.Errorf("Ordered() = %v, want A then B", got)
	}
}

func TestSiteApplyMap(t *testing.T) {
	site := Site{}.ApplyMap(map[string]any{
		"id":       float64(3),
		"label":    "North",
		"order":    float64(1),
		"siteRef":  float64(7),
		"systemId": float64(5),
	})

	want := Site{ID: 3, Label: "North", Order: 1, SiteRef: 7, SystemID: 5}

	if site != want {
		t.Errorf("ApplyMap = %+v, want %+v", site, want)
	}
}

func TestSitesByIDAndRef(t *testing.T) {
	sites := Sites{{ID: 1, SiteRef: 10, Label: "North"}, {ID: 2, SiteRef: 20, Label: "South"}}

	if site, ok := sites.ByID(2); !ok || site.SiteRef != 20 {
		t.Errorf("ByID(2) = %+v, %v, want the South site", site, ok)
	}

	if site, ok := sites.ByRef(10); !ok || site.Label != "North" {
		t.Errorf("ByRef(10) = %+v, %v, want the North site", site, ok)
	}

	if _, ok := sites.ByRef(99); ok {
		t.Error("ByRef(99) reported a site, want none")
	}
}
