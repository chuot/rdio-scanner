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
	"reflect"
	"testing"
)

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
