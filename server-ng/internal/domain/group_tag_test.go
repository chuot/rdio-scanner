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
	"reflect"
	"testing"
)

func TestDefaultGroupLabelsMatchTheLegacySeeds(t *testing.T) {
	want := []string{"Air", "EMS", "Fire", "Interop", "Law", "Unknown"}

	if got := DefaultGroupLabels(); !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultGroupLabels() = %v, want %v", got, want)
	}
}

func TestDefaultTagLabelsMatchTheLegacySeeds(t *testing.T) {
	want := []string{
		"Air Traffic Control",
		"Emergency ",
		"Fire Dispatch",
		"Fire Tac",
		"Fire Talk",
		"Interop",
		"Security",
		"Service",
		"Untagged",
	}

	if got := DefaultTagLabels(); !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultTagLabels() = %v, want %v", got, want)
	}
}

func TestGroupApplyMap(t *testing.T) {
	group := Group{}.ApplyMap(map[string]any{
		"id":    float64(7),
		"alert": "alert1",
		"label": "Law",
		"led":   "#ff0000",
		"order": float64(3),
	})

	want := Group{ID: 7, Alert: "alert1", Label: "Law", LED: "#ff0000", Order: 3}

	if group != want {
		t.Errorf("ApplyMap = %+v, want %+v", group, want)
	}
}

func TestGroupApplyMapIgnoresWronglyTypedKeys(t *testing.T) {
	group := Group{ID: 7, Label: "Law"}.ApplyMap(map[string]any{
		"id":    "not a number",
		"label": float64(12),
		"order": "not a number",
	})

	if group.ID != 7 {
		t.Errorf("ID = %d, want the previous 7 preserved", group.ID)
	}

	if group.Label != "Law" {
		t.Errorf("Label = %q, want the previous Law preserved", group.Label)
	}

	if group.Order != 0 {
		t.Errorf("Order = %d, want 0", group.Order)
	}
}

func TestTagApplyMap(t *testing.T) {
	tag := Tag{}.ApplyMap(map[string]any{
		"id":    float64(4),
		"alert": "alert2",
		"label": "Fire Dispatch",
		"led":   "#00ff00",
		"order": float64(2),
	})

	want := Tag{ID: 4, Alert: "alert2", Label: "Fire Dispatch", LED: "#00ff00", Order: 2}

	if tag != want {
		t.Errorf("ApplyMap = %+v, want %+v", tag, want)
	}
}

func TestGroupsLookups(t *testing.T) {
	groups := Groups{{ID: 1, Label: "Fire"}, {ID: 2, Label: "Law"}}

	if group, ok := groups.ByID(2); !ok || group.Label != "Law" {
		t.Errorf("ByID(2) = %+v, %v, want the Law group", group, ok)
	}

	if _, ok := groups.ByID(99); ok {
		t.Error("ByID(99) reported a group, want none")
	}

	if group, ok := groups.ByLabel("Fire"); !ok || group.ID != 1 {
		t.Errorf("ByLabel(Fire) = %+v, %v, want the Fire group", group, ok)
	}

	if _, ok := groups.ByLabel("EMS"); ok {
		t.Error("ByLabel(EMS) reported a group, want none")
	}
}

func TestGroupsIDsForLabelsDropsUnknownLabels(t *testing.T) {
	groups := Groups{{ID: 1, Label: "Fire"}, {ID: 2, Label: "Law"}}

	got := groups.IDsForLabels([]string{"Law", "Ghost", "Fire"})

	if want := []uint64{2, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("IDsForLabels = %v, want %v in submission order with unknown labels dropped", got, want)
	}

	if got := (Groups{}).IDsForLabels([]string{"Fire"}); len(got) != 0 {
		t.Errorf("IDsForLabels on an empty collection = %v, want an empty slice", got)
	}
}

func TestGroupsAndTagsAreOrderedByDisplayOrder(t *testing.T) {
	groups := Groups{{ID: 1, Label: "C", Order: 30}, {ID: 2, Label: "A", Order: 10}, {ID: 3, Label: "B", Order: 20}}

	got := groups.Ordered()

	if got[0].Label != "A" || got[1].Label != "B" || got[2].Label != "C" {
		t.Errorf("Ordered() = %v, want A, B, C", got)
	}

	if groups[0].Label != "C" {
		t.Error("Ordered() mutated the receiver; it must return a sorted copy")
	}

	tags := Tags{{ID: 1, Label: "Z", Order: 5}, {ID: 2, Label: "Y", Order: 1}}

	if got := tags.Ordered(); got[0].Label != "Y" {
		t.Errorf("Tags.Ordered() = %v, want Y first", got)
	}
}
