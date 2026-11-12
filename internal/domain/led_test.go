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

func TestValidLEDColorAcceptsOnlyTheEightNamedColours(t *testing.T) {
	for _, name := range []string{"blue", "cyan", "green", "magenta", "orange", "red", "white", "yellow"} {
		if !ValidLEDColor(name) {
			t.Errorf("ValidLEDColor(%q) = false, want true for a named colour", name)
		}
	}

	for _, name := range []string{"", "blurple", "Green", "green ", "grey"} {
		if ValidLEDColor(name) {
			t.Errorf("ValidLEDColor(%q) = true, want false for a value outside the named colours", name)
		}
	}
}

func TestEffectiveLEDColorTakesTheFirstKnownCandidateOrTheGreenDefault(t *testing.T) {
	cases := []struct {
		candidates []string
		want       string
	}{
		{[]string{"magenta", "red"}, "magenta"},
		{[]string{"", "red"}, "red"},
		{[]string{"blurple", "red"}, "red"},
		{[]string{"green", "red"}, "green"},
		{[]string{"", "blurple"}, "green"},
		{nil, "green"},
	}

	for _, c := range cases {
		if got := EffectiveLEDColor(c.candidates...); got != c.want {
			t.Errorf("EffectiveLEDColor(%q) = %q, want %q", c.candidates, got, c.want)
		}
	}

	if DefaultLEDColor != "green" {
		t.Errorf("DefaultLEDColor = %q, want green", DefaultLEDColor)
	}
}
