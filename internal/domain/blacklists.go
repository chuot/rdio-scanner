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
	"strconv"
	"strings"
)

// Blacklists is a comma-separated list of talkgroup references a system refuses to
// accept. Entries are compared exactly as written: a padded entry does not match,
// which is how the legacy server behaves.
type Blacklists string

// Count answers how many talkgroup references the list names. An empty list names none;
// a list of entries names one per entry, the way the field lays them out.
func (blacklists Blacklists) Count() int {
	text := string(blacklists)

	if strings.TrimSpace(text) == "" {
		return 0
	}

	return len(strings.Split(text, ","))
}

// IsBlacklisted reports whether a talkgroup reference is refused by the system.
func (blacklists Blacklists) IsBlacklisted(ref uint) bool {
	target := strconv.FormatUint(uint64(ref), 10)

	for _, entry := range strings.Split(string(blacklists), ",") {
		if entry == target {
			return true
		}
	}

	return false
}
