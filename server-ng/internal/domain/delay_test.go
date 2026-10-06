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
	"testing"
	"time"
)

// The instant the delay tests are built around, and the delay lengths they name.
var (
	delayedAt = time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC)

	talkgroupDelay = Talkgroup{Delay: 3}
	systemDelay    = System{Delay: 5}
)

// A talkgroup's delay is the one that applies, and a system's delay only applies when the talkgroup
// it was heard on names none.
func TestACallIsHeldBackForItsTalkgroupsDelayBeforeItsSystems(t *testing.T) {
	call := NewCall()
	call.Timestamp = delayedAt
	call.Talkgroup = talkgroupDelay
	call.System = systemDelay

	if got := call.Delay(); got != 3*time.Minute {
		t.Errorf("a call on a talkgroup delayed 3 minutes inside a system delayed 5 minutes waits %v, want 3 minutes", got)
	}

	call.Talkgroup = Talkgroup{Delay: 0}

	if got := call.Delay(); got != 5*time.Minute {
		t.Errorf("a call on a talkgroup with no delay inside a system delayed 5 minutes waits %v, want 5 minutes", got)
	}

	call.System = System{Delay: 0}

	if got := call.Delay(); got != 0 {
		t.Errorf("a call whose talkgroup and system name no delay waits %v, want nothing", got)
	}
}

// A call is held back from the moment it happened, not from the moment the server received it, so a
// recording that arrived late waits out only what is left.
func TestACallIsHeldBackFromTheMomentItHappened(t *testing.T) {
	call := NewCall()
	call.Timestamp = delayedAt
	call.Talkgroup = talkgroupDelay

	if got := call.VisibleAt(); !got.Equal(delayedAt.Add(3 * time.Minute)) {
		t.Errorf("a call that happened at %s and waits 3 minutes becomes visible at %s", delayedAt, got)
	}

	// Halfway through its delay it is still being held back, and at the instant its delay runs out
	// it is not: the delay is a length of time, and the end of it is not part of it.
	if !call.IsWaiting(delayedAt.Add(90 * time.Second)) {
		t.Errorf("a call 90 seconds into a 3 minute delay is not waiting, want it still waiting")
	}

	if call.IsWaiting(delayedAt.Add(3 * time.Minute)) {
		t.Errorf("a call exactly 3 minutes after it happened is still waiting, want its delay to have run out")
	}

	if call.IsWaiting(delayedAt.Add(4 * time.Minute)) {
		t.Errorf("a call 4 minutes after it happened is still waiting, want it shown")
	}
}

// A recording that arrived after its own delay had run out is shown at once, and a call with no
// delay is never waiting however it is measured.
func TestACallWhoseDelayHasAlreadyRunOutIsNotWaiting(t *testing.T) {
	late := NewCall()
	late.Timestamp = delayedAt.Add(-10 * time.Minute)
	late.Talkgroup = talkgroupDelay

	if late.IsWaiting(delayedAt) {
		t.Errorf("a call that happened 10 minutes ago with a 3 minute delay is waiting, want it shown at once")
	}

	plain := NewCall()
	plain.Timestamp = delayedAt.Add(1 * time.Hour)

	if plain.IsWaiting(delayedAt) {
		t.Errorf("a call with no delay is waiting, want nothing to be held back")
	}
}
