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

import "testing"

func TestBlacklistsMatchExactlyAsWritten(t *testing.T) {
	blacklists := Blacklists("100,200,300")

	if !blacklists.IsBlacklisted(200) {
		t.Error("200 is listed, want it blacklisted")
	}

	if blacklists.IsBlacklisted(250) {
		t.Error("250 is not listed, want it accepted")
	}

	// A padded entry does not match, matching the legacy comparison.
	if !Blacklists("100, 200").IsBlacklisted(100) || Blacklists("100, 200").IsBlacklisted(200) {
		t.Error("padded entries must not match; the legacy server compares entries verbatim")
	}

	if Blacklists("").IsBlacklisted(100) {
		t.Error("an empty blacklist must accept everything")
	}
}

// TestBlacklistsCountNamesTheReferencesTheyList: the count an operator is shown is the number of
// references the list names, not the number of characters its text takes. A padded entry counts as
// the entry it is, the same entry the gate refuses to match.
func TestBlacklistsCountNamesTheReferencesTheyList(t *testing.T) {
	for _, tc := range []struct {
		blacklists Blacklists
		want       int
	}{
		{blacklists: "", want: 0},
		{blacklists: "   ", want: 0},
		{blacklists: "1514000", want: 1},
		{blacklists: "40000,40001", want: 2},
		{blacklists: "1514000, 1514100", want: 2},
	} {
		if got := tc.blacklists.Count(); got != tc.want {
			t.Errorf("the list %q counts %d references, want %d", string(tc.blacklists), got, tc.want)
		}
	}
}
