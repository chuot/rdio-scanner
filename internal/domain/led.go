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

// knownLEDColors are the colour names every LED consumer — the administration
// selects, the scanner webapp resolver, and the configuration payload — can
// render.
var knownLEDColors = map[string]bool{
	"blue":    true,
	"cyan":    true,
	"green":   true,
	"magenta": true,
	"orange":  true,
	"red":     true,
	"white":   true,
	"yellow":  true,
}

// ValidLEDColor reports whether name is a colour the LED machinery can render.
func ValidLEDColor(name string) bool {
	return knownLEDColors[name]
}

// DefaultLEDColor is the colour a consumer falls back to when no level names
// a known one.
const DefaultLEDColor = "green"

// EffectiveLEDColor returns the first candidate that is a known colour, in
// the order given, and DefaultLEDColor when none is. Candidates are stored
// level colours listed in priority order.
func EffectiveLEDColor(candidates ...string) string {
	for _, candidate := range candidates {
		if knownLEDColors[candidate] {
			return candidate
		}
	}

	return DefaultLEDColor
}
