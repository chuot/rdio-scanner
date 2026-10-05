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

// Talkgroup is a radio channel of a system, identified to clients by TalkgroupRef.
type Talkgroup struct {
	ID           uint64
	Alert        string
	Delay        uint
	Frequency    uint
	GroupIDs     []uint64
	Kind         string
	Label        string
	LED          string
	Name         string
	Order        uint
	TagID        uint64
	TalkgroupRef uint
}

// ApplyMap reads a talkgroup out of an administrator configuration payload. The
// display order is read from the "order" key only, exactly as the legacy server does,
// even though the legacy server emits it under the "talkgroup" key; clients are
// expected to normalize that drift. See
// docs/specs/FEAT-003-server-ng-refactor/decision-preserved-quirks.md (quirk Q4).
func (talkgroup Talkgroup) ApplyMap(m map[string]any) Talkgroup {
	if v, ok := m["id"].(float64); ok {
		talkgroup.ID = uint64(v)
	}

	if v, ok := m["alert"].(string); ok {
		talkgroup.Alert = v
	}

	if v, ok := m["delay"].(float64); ok {
		talkgroup.Delay = uint(v)
	}

	if v, ok := m["frequency"].(float64); ok {
		talkgroup.Frequency = uint(v)
	}

	if v, ok := m["groupIds"].([]any); ok {
		ids := []uint64{}

		for _, item := range v {
			if id, ok := item.(float64); ok {
				ids = append(ids, uint64(id))
			}
		}

		talkgroup.GroupIDs = ids
	}

	if v, ok := m["type"].(string); ok {
		talkgroup.Kind = v
	}

	if v, ok := m["label"].(string); ok {
		talkgroup.Label = v
	}

	if v, ok := m["led"].(string); ok {
		talkgroup.LED = v
	}

	if v, ok := m["name"].(string); ok {
		talkgroup.Name = v
	}

	if v, ok := m["order"].(float64); ok {
		talkgroup.Order = uint(v)
	}

	if v, ok := m["tagId"].(float64); ok {
		talkgroup.TagID = uint64(v)
	}

	if v, ok := m["talkgroupRef"].(float64); ok {
		talkgroup.TalkgroupRef = uint(v)
	}

	return talkgroup
}

// GeneratedLabel is the placeholder label auto-populate gives a talkgroup it has
// never seen described: the numeric reference as text.
func (talkgroup Talkgroup) GeneratedLabel() string {
	return fmt.Sprintf("%d", talkgroup.TalkgroupRef)
}

// GeneratedName is the placeholder name auto-populate gives an undescribed talkgroup.
func (talkgroup Talkgroup) GeneratedName() string {
	return fmt.Sprintf("Talkgroup %d", talkgroup.TalkgroupRef)
}

// HasGeneratedLabel reports whether the label is still the auto-populate placeholder.
func (talkgroup Talkgroup) HasGeneratedLabel() bool {
	return talkgroup.Label == "" || talkgroup.Label == talkgroup.GeneratedLabel()
}

// HasGeneratedName reports whether the name is still the auto-populate placeholder.
func (talkgroup Talkgroup) HasGeneratedName() bool {
	return talkgroup.Name == "" || talkgroup.Name == talkgroup.GeneratedName()
}

// Talkgroups is a collection of talkgroups belonging to one system.
type Talkgroups []Talkgroup

// ByID finds a talkgroup by its database identifier.
func (talkgroups Talkgroups) ByID(id uint64) (Talkgroup, bool) {
	for _, talkgroup := range talkgroups {
		if talkgroup.ID == id {
			return talkgroup, true
		}
	}

	return Talkgroup{}, false
}

// ByRef finds a talkgroup by the reference clients identify it by.
func (talkgroups Talkgroups) ByRef(ref uint) (Talkgroup, bool) {
	for _, talkgroup := range talkgroups {
		if talkgroup.TalkgroupRef == ref {
			return talkgroup, true
		}
	}

	return Talkgroup{}, false
}

// ByLabel finds a talkgroup by its label.
func (talkgroups Talkgroups) ByLabel(label string) (Talkgroup, bool) {
	for _, talkgroup := range talkgroups {
		if talkgroup.Label == label {
			return talkgroup, true
		}
	}

	return Talkgroup{}, false
}

// Ordered returns the talkgroups sorted by their display order.
func (talkgroups Talkgroups) Ordered() Talkgroups {
	out := append(Talkgroups{}, talkgroups...)

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}
