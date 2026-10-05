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
	"fmt"
	"sort"
)

// System is a trunked radio system: the aggregate root that owns its talkgroups,
// sites and units and enforces its own blacklist and auto-populate rules.
type System struct {
	ID           uint64
	Alert        string
	AutoPopulate bool
	Blacklists   Blacklists
	Delay        uint
	Kind         string
	Label        string
	LED          string
	Order        uint
	Sites        Sites
	SystemRef    uint
	Talkgroups   Talkgroups
	Units        Units
}

// ApplyMap reads a system out of an administrator configuration payload, keeping the
// current value for keys that are absent or wrongly typed.
func (system System) ApplyMap(m map[string]any) System {
	if v, ok := m["id"].(float64); ok {
		system.ID = uint64(v)
	}

	if v, ok := m["alert"].(string); ok {
		system.Alert = v
	}

	if v, ok := m["autoPopulate"].(bool); ok {
		system.AutoPopulate = v
	}

	if v, ok := m["blacklists"].(string); ok {
		system.Blacklists = Blacklists(v)
	}

	if v, ok := m["delay"].(float64); ok {
		system.Delay = uint(v)
	}

	if v, ok := m["type"].(string); ok {
		system.Kind = v
	}

	if v, ok := m["label"].(string); ok {
		system.Label = v
	}

	if v, ok := m["led"].(string); ok && len(v) > 0 {
		system.LED = v
	}

	if v, ok := m["order"].(float64); ok {
		system.Order = uint(v)
	}

	if v, ok := m["sites"].([]any); ok {
		system.Sites = sitesFrom(v)
	}

	if v, ok := m["systemRef"].(float64); ok {
		system.SystemRef = uint(v)
	}

	if v, ok := m["talkgroups"].([]any); ok {
		system.Talkgroups = talkgroupsFrom(v)
	}

	if v, ok := m["units"].([]any); ok {
		system.Units = unitsFrom(v)
	}

	return system
}

// GeneratedLabel is the placeholder label auto-populate gives a system it has never
// seen described.
func (system System) GeneratedLabel() string {
	return fmt.Sprintf("System %d", system.SystemRef)
}

// AcceptsTalkgroup reports whether the system accepts calls on a talkgroup, applying
// its blacklist.
func (system System) AcceptsTalkgroup(talkgroupRef uint) bool {
	return !system.Blacklists.IsBlacklisted(talkgroupRef)
}

// AddTalkgroup adds a talkgroup to the system, returning the updated system and
// whether anything changed.
func (system System) AddTalkgroup(talkgroup Talkgroup) (System, bool) {
	if _, ok := system.Talkgroups.ByRef(talkgroup.TalkgroupRef); ok {
		return system, false
	}

	system.Talkgroups = append(system.Talkgroups, talkgroup)

	return system, true
}

// MergeUnits adds every unit of another collection that the system does not already
// know, reporting whether anything was added.
func (system System) MergeUnits(units Units) (System, bool) {
	merged := false

	for _, unit := range units {
		if _, exists := system.Units.ByRef(unit.UnitRef); exists {
			continue
		}

		system.Units = append(system.Units, Unit{Label: unit.Label, UnitRef: unit.UnitRef})
		merged = true
	}

	return system, merged
}

// Systems is the server's system catalog.
type Systems []System

// ByID finds a system by its database identifier.
func (systems Systems) ByID(id uint64) (System, bool) {
	for _, system := range systems {
		if system.ID == id {
			return system, true
		}
	}

	return System{}, false
}

// ByRef finds a system by the reference clients identify it by.
func (systems Systems) ByRef(ref uint) (System, bool) {
	for _, system := range systems {
		if system.SystemRef == ref {
			return system, true
		}
	}

	return System{}, false
}

// ByLabel finds a system by its label.
func (systems Systems) ByLabel(label string) (System, bool) {
	for _, system := range systems {
		if system.Label == label {
			return system, true
		}
	}

	return System{}, false
}

// NextSystemRef returns the lowest system reference not in use, the reference
// auto-populate assigns to a newly discovered system.
func (systems Systems) NextSystemRef() uint {
	for candidate := uint(1); candidate > 0; candidate++ {
		if _, taken := systems.ByRef(candidate); !taken {
			return candidate
		}
	}

	return 0
}

// Ordered returns the systems sorted by their display order.
func (systems Systems) Ordered() Systems {
	out := append(Systems{}, systems...)

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}

func sitesFrom(items []any) Sites {
	sites := Sites{}

	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			sites = append(sites, Site{}.ApplyMap(m))
		}
	}

	return sites
}

func talkgroupsFrom(items []any) Talkgroups {
	talkgroups := Talkgroups{}

	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			talkgroups = append(talkgroups, Talkgroup{GroupIDs: []uint64{}}.ApplyMap(m))
		}
	}

	return talkgroups
}

func unitsFrom(items []any) Units {
	units := Units{}

	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			units = append(units, Unit{}.ApplyMap(m))
		}
	}

	return units
}
