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

package clock

import (
	"testing"
	"time"
)

// TestTheClockReportsTheMomentInTheServersLocalTimeZone pins the one thing about the real clock
// that the rest of the server depends on: the moment it reports is a local one, because the
// directory watch mask reads the date and time a filename names in the clock's own zone.
func TestTheClockReportsTheMomentInTheServersLocalTimeZone(t *testing.T) {
	before := time.Now()

	at := NewClock().Now()

	after := time.Now()

	if at.Before(before) || at.After(after) {
		t.Errorf("the clock reports %v, want the moment between %v and %v", at, before, after)
	}

	if at.Location() != time.Local {
		t.Errorf("the clock reports the moment in %v, want the time zone the server runs in", at.Location())
	}
}
