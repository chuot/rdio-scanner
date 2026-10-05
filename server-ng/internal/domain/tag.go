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

import "sort"

// Tag labels a talkgroup for search and display, such as Fire Dispatch.
type Tag struct {
	ID    uint64
	Alert string
	Label string
	LED   string
	Order uint
}

// DefaultTagLabels are the tags a fresh server is seeded with. The trailing space in
// "Emergency " is part of the seeded label and is preserved verbatim.
func DefaultTagLabels() []string {
	return []string{
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
}

// ApplyMap reads a tag out of an administrator configuration payload, keeping the
// current value for keys that are absent or wrongly typed.
func (tag Tag) ApplyMap(m map[string]any) Tag {
	if v, ok := m["id"].(float64); ok {
		tag.ID = uint64(v)
	}

	if v, ok := m["alert"].(string); ok {
		tag.Alert = v
	}

	if v, ok := m["label"].(string); ok {
		tag.Label = v
	}

	if v, ok := m["led"].(string); ok {
		tag.LED = v
	}

	if v, ok := m["order"].(float64); ok {
		tag.Order = uint(v)
	}

	return tag
}

// Tags is a collection of tags with the lookups the ingest pipeline and the
// configuration use cases need.
type Tags []Tag

// ByID finds a tag by its database identifier.
func (tags Tags) ByID(id uint64) (Tag, bool) {
	for _, tag := range tags {
		if tag.ID == id {
			return tag, true
		}
	}

	return Tag{}, false
}

// ByLabel finds a tag by its label.
func (tags Tags) ByLabel(label string) (Tag, bool) {
	for _, tag := range tags {
		if tag.Label == label {
			return tag, true
		}
	}

	return Tag{}, false
}

// Ordered returns the tags sorted by their display order.
func (tags Tags) Ordered() Tags {
	out := append(Tags{}, tags...)

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}
