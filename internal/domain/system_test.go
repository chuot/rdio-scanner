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

func TestSystemApplyMap(t *testing.T) {
	system := System{}.ApplyMap(map[string]any{
		"id":           float64(2),
		"alert":        "alert1",
		"autoPopulate": true,
		"blacklists":   "100,200",
		"delay":        float64(5),
		"type":         "talkpoint",
		"label":        "County Trunk",
		"led":          "#0000ff",
		"order":        float64(1),
		"systemRef":    float64(9),
		"sites":        []any{map[string]any{"siteRef": float64(3), "label": "North"}},
		"talkgroups":   []any{map[string]any{"talkgroupRef": float64(100), "label": "Fire", "order": float64(4)}},
		"units":        []any{map[string]any{"unitRef": float64(501), "label": "Dispatch"}},
	})

	if system.ID != 2 || system.SystemRef != 9 || system.Label != "County Trunk" || !system.AutoPopulate {
		t.Fatalf("ApplyMap = %+v, want the submitted identifiers", system)
	}

	if system.Blacklists != Blacklists("100,200") || system.Delay != 5 || system.Kind != "talkpoint" || system.Order != 1 {
		t.Errorf("ApplyMap = %+v, want the submitted scalar fields", system)
	}

	if len(system.Sites) != 1 || system.Sites[0].SiteRef != 3 {
		t.Errorf("Sites = %+v, want one site with ref 3", system.Sites)
	}

	if len(system.Talkgroups) != 1 || system.Talkgroups[0].TalkgroupRef != 100 || system.Talkgroups[0].Order != 4 {
		t.Errorf("Talkgroups = %+v, want one talkgroup with ref 100 and order 4", system.Talkgroups)
	}

	if len(system.Units) != 1 || system.Units[0].UnitRef != 501 {
		t.Errorf("Units = %+v, want one unit with ref 501", system.Units)
	}
}

func TestSystemAcceptsTalkgroupAppliesTheBlacklist(t *testing.T) {
	system := System{Blacklists: "100,200"}

	if !system.AcceptsTalkgroup(300) {
		t.Error("talkgroup 300 is not blacklisted, want it accepted")
	}

	if system.AcceptsTalkgroup(100) {
		t.Error("talkgroup 100 is blacklisted, want it refused")
	}
}

func TestSystemsLookupsAndNextRef(t *testing.T) {
	systems := Systems{{ID: 1, SystemRef: 5, Label: "Alpha"}, {ID: 2, SystemRef: 1, Label: "Bravo"}}

	if system, ok := systems.ByID(2); !ok || system.Label != "Bravo" {
		t.Errorf("ByID(2) = %+v, %v, want Bravo", system, ok)
	}

	if system, ok := systems.ByRef(5); !ok || system.ID != 1 {
		t.Errorf("ByRef(5) = %+v, %v, want Alpha", system, ok)
	}

	if system, ok := systems.ByLabel("Bravo"); !ok || system.SystemRef != 1 {
		t.Errorf("ByLabel(Bravo) = %+v, %v, want the system with ref 1", system, ok)
	}

	if _, ok := systems.ByLabel("Ghost"); ok {
		t.Error("ByLabel(Ghost) reported a system, want none")
	}

	if got := systems.NextSystemRef(); got != 2 {
		t.Errorf("NextSystemRef = %d, want the lowest unused reference 2", got)
	}

	if got := (Systems{}).NextSystemRef(); got != 1 {
		t.Errorf("NextSystemRef on an empty catalog = %d, want 1", got)
	}
}

func TestSystemAddTalkgroupAndMergeUnitsAreIdempotent(t *testing.T) {
	system := System{}

	system, added := system.AddTalkgroup(Talkgroup{TalkgroupRef: 100, Label: "Fire"})
	if !added {
		t.Fatal("the first talkgroup must be added")
	}

	if _, added := system.AddTalkgroup(Talkgroup{TalkgroupRef: 100, Label: "Duplicate"}); added {
		t.Error("a talkgroup with an existing reference must not be added twice")
	}

	system, merged := system.MergeUnits(Units{{UnitRef: 10, Label: "A"}, {UnitRef: 11, Label: "B"}})
	if !merged || len(system.Units) != 2 {
		t.Fatalf("MergeUnits produced %+v, want two units", system.Units)
	}

	if _, merged := system.MergeUnits(Units{{UnitRef: 10, Label: "A again"}}); merged {
		t.Error("merging an already known unit must report no change")
	}

	if len(system.Units) != 2 {
		t.Errorf("units = %+v, want the original two unchanged", system.Units)
	}
}

func TestTalkgroupsAndSystemsAreOrdered(t *testing.T) {
	talkgroups := Talkgroups{{TalkgroupRef: 1, Order: 30}, {TalkgroupRef: 2, Order: 10}}

	if got := talkgroups.Ordered(); got[0].TalkgroupRef != 2 {
		t.Errorf("Ordered() = %+v, want the order-10 talkgroup first", got)
	}

	systems := Systems{{SystemRef: 1, Order: 30}, {SystemRef: 2, Order: 10}}

	if got := systems.Ordered(); got[0].SystemRef != 2 {
		t.Errorf("Systems.Ordered() = %+v, want the order-10 system first", got)
	}
}
