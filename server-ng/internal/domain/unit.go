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

// Unit names a radio unit of a system. UnitFrom and UnitTo describe a range of unit
// references the label covers; both are zero for a single unit.
type Unit struct {
	ID       uint64
	Label    string
	Order    uint
	SystemID uint64
	UnitRef  uint
	UnitFrom uint
	UnitTo   uint
}

// ApplyMap reads a unit out of an administrator configuration payload, keeping the
// current value for keys that are absent or wrongly typed.
func (unit Unit) ApplyMap(m map[string]any) Unit {
	if v, ok := m["id"].(float64); ok {
		unit.ID = uint64(v)
	}

	if v, ok := m["label"].(string); ok {
		unit.Label = v
	}

	if v, ok := m["order"].(float64); ok {
		unit.Order = uint(v)
	}

	if v, ok := m["systemId"].(float64); ok {
		unit.SystemID = uint64(v)
	}

	if v, ok := m["unitRef"].(float64); ok {
		unit.UnitRef = uint(v)
	}

	if v, ok := m["unitFrom"].(float64); ok {
		unit.UnitFrom = uint(v)
	}

	if v, ok := m["unitTo"].(float64); ok {
		unit.UnitTo = uint(v)
	}

	return unit
}

// Units is a collection of units belonging to one system.
type Units []Unit

// ByID finds a unit by its database identifier.
func (units Units) ByID(id uint64) (Unit, bool) {
	for _, unit := range units {
		if unit.ID == id {
			return unit, true
		}
	}

	return Unit{}, false
}

// Ordered returns the units sorted by their display order.
func (units Units) Ordered() Units {
	out := append(Units{}, units...)

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}
