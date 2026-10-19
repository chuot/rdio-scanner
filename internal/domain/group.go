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

import "sort"

// Group is a listener-facing grouping of talkgroups, such as Fire or Law.
type Group struct {
	ID    uint64
	Alert string
	Label string
	LED   string
	Order uint
}

// DefaultGroupLabels are the groups a fresh server is seeded with.
func DefaultGroupLabels() []string {
	return []string{"Air", "EMS", "Fire", "Interop", "Law", "Unknown"}
}

// ApplyMap reads a group out of an administrator configuration payload, keeping the
// current value for keys that are absent or wrongly typed.
func (group Group) ApplyMap(m map[string]any) Group {
	if v, ok := m["id"].(float64); ok {
		group.ID = uint64(v)
	}

	if v, ok := m["alert"].(string); ok {
		group.Alert = v
	}

	if v, ok := m["label"].(string); ok {
		group.Label = v
	}

	if v, ok := m["led"].(string); ok {
		group.LED = v
	}

	if v, ok := m["order"].(float64); ok {
		group.Order = uint(v)
	}

	return group
}

// Groups is a collection of groups with the lookups the ingest pipeline and the
// configuration use cases need.
type Groups []Group

// ByID finds a group by its database identifier.
func (groups Groups) ByID(id uint64) (Group, bool) {
	for _, group := range groups {
		if group.ID == id {
			return group, true
		}
	}

	return Group{}, false
}

// ByLabel finds a group by its label, the identity the ingest pipeline resolves
// talkgroup memberships against.
func (groups Groups) ByLabel(label string) (Group, bool) {
	for _, group := range groups {
		if group.Label == label {
			return group, true
		}
	}

	return Group{}, false
}

// IDsForLabels maps group labels to identifiers, silently dropping labels that do
// not exist, as the legacy GetGroupIds does.
func (groups Groups) IDsForLabels(labels []string) []uint64 {
	ids := []uint64{}

	for _, label := range labels {
		if group, ok := groups.ByLabel(label); ok {
			ids = append(ids, group.ID)
		}
	}

	return ids
}

// Ordered returns the groups sorted by their display order, the order they are read
// from the database in.
func (groups Groups) Ordered() Groups {
	out := append(Groups{}, groups...)

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}
