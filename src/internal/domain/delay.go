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

import "time"

// delayUnit is the unit a delay is counted in. A system's or a talkgroup's delay is stored as a
// whole number of minutes, because the delay exists to hold a call back from the operators who can
// least afford to be shown it early.
const delayUnit = time.Minute

// Delay is how long this call is held back before anybody is told about it.
//
// The talkgroup it was heard on decides first: a talkgroup with a delay of its own is not held back
// further by the system it belongs to. A call whose talkgroup and system both name nothing is held
// back for nothing.
func (call Call) Delay() time.Duration {
	if call.Talkgroup.Delay > 0 {
		return time.Duration(call.Talkgroup.Delay) * delayUnit
	}

	return time.Duration(call.System.Delay) * delayUnit
}

// VisibleAt is the instant this call stops being held back: the moment it happened, plus its delay.
func (call Call) VisibleAt() time.Time {
	return call.Timestamp.Add(call.Delay())
}

// IsWaiting reports whether this call is still being held back at the given instant.
//
// The delay is measured from the moment the call happened, not from the moment this server received
// it. That is what makes a recording that arrived late wait out only what is left of its delay
// rather than a whole delay on top of the lateness — and what makes a recording that arrived after
// its own delay had run out be shown at once.
func (call Call) IsWaiting(at time.Time) bool {
	return call.Delay() > 0 && call.VisibleAt().After(at)
}
