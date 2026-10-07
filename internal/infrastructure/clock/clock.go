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
	"time"

	"rdio-scanner/internal/application/ports"
)

// Clock is the server's clock: the moment, in the time zone the server runs in.
//
// The time zone is part of what the clock reports rather than an accident of it. The directory
// watch mask reads the date and time a filename names in the clock's own zone, so a server running
// in a fixed-UTC zone and a server running in a local zone read the same filename as two different
// moments — which is what the legacy does, and what an administrator comparing a mask against a
// recording tool's clock has to account for.
type Clock struct{}

var _ ports.Clock = (*Clock)(nil)

// NewClock returns the clock that reports the moment it is asked.
func NewClock() *Clock {
	return &Clock{}
}

// Now reports the moment, in the server's local time zone.
func (clock *Clock) Now() time.Time {
	return time.Now()
}
