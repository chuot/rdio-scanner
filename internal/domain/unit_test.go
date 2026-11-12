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

// TestUnitDamagedByMigration pins the shape of a row the legacy migration created rather than a deployment
// named: a radio reference, a label that is that reference written out as a decimal number, and no range. The
// rows this rule names are the ones a maintenance run may remove, so each survivor is a case of its own.
func TestUnitDamagedByMigration(t *testing.T) {
	for _, tc := range []struct {
		name string
		unit Unit
		want bool
	}{
		{name: "a unit labelled with the number it was migrated from", unit: Unit{ID: 1, Label: "47003", UnitRef: 47003}, want: true},
		{name: "the same shape at a small reference", unit: Unit{ID: 2, Label: "1", UnitRef: 1}, want: true},
		{name: "a range the operator described", unit: Unit{ID: 3, Label: "Fleet 47", UnitRef: 47003, UnitFrom: 47003, UnitTo: 47999}, want: false},
		{name: "a range labelled with its own reference", unit: Unit{ID: 4, Label: "47003", UnitRef: 47003, UnitFrom: 47003, UnitTo: 47999}, want: false},
		{name: "a unit labelled by hand", unit: Unit{ID: 5, Label: "Car", UnitRef: 47003}, want: false},
		{name: "a unit with no radio reference and no label", unit: Unit{ID: 6}, want: false},
		{name: "a unit with no radio reference and a number for a label", unit: Unit{ID: 7, Label: "0"}, want: false},
		{name: "a unit with no radio reference and a name for a label", unit: Unit{ID: 8, Label: "Dispatch", UnitFrom: 47003, UnitTo: 47999}, want: false},
		{name: "a label with the reference padded", unit: Unit{ID: 9, Label: "047003", UnitRef: 47003}, want: false},
		{name: "a label with the reference and a trailing space", unit: Unit{ID: 10, Label: "47003 ", UnitRef: 47003}, want: false},
		{name: "the unit a real deployment had configured before it was migrated", unit: Unit{ID: 11, Label: "test", UnitRef: 1}, want: false},
		{name: "a damaged row saved with the display order the ingest writes", unit: Unit{ID: 12, Label: "47003", UnitRef: 47003, Order: 0}, want: true},
	} {
		if got := tc.unit.DamagedByMigration(); got != tc.want {
			t.Errorf("%s: unit %+v reports damaged %v, want %v", tc.name, tc.unit, got, tc.want)
		}
	}
}
