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

package fakes

import (
	"time"

	"rdio-scanner/internal/application/ports"
)

// Runtime is the fake the status read's live block is tested against: a server whose connections,
// holds, and audio programs are set to whatever the test is asking about.
type Runtime struct {
	StartedAt             time.Time
	Listeners             int
	AdministratorSessions int
	RunningWatchers       int
	HeldCalls             int
	AudioConversion       bool
	AudioProbe            bool

	DatabaseBytes int64
	DatabaseKnown bool
}

var _ ports.RuntimeState = (*Runtime)(nil)

// ServerStarted reports the instant this fake started.
func (runtime *Runtime) ServerStarted() time.Time {
	return runtime.StartedAt
}

// ListenerCount reports how many listener connections this fake holds.
func (runtime *Runtime) ListenerCount() int {
	return runtime.Listeners
}

// AdministratorSessionCount reports how many administrator connections this fake holds.
func (runtime *Runtime) AdministratorSessionCount() int {
	return runtime.AdministratorSessions
}

// DirwatchersRunning reports how many directory watchers this fake is running.
func (runtime *Runtime) DirwatchersRunning() int {
	return runtime.RunningWatchers
}

// HeldCallCount reports how many calls this fake is holding back.
func (runtime *Runtime) HeldCallCount() int {
	return runtime.HeldCalls
}

// AudioConversionAvailable reports whether this fake converts audio.
func (runtime *Runtime) AudioConversionAvailable() bool {
	return runtime.AudioConversion
}

// AudioProbeAvailable reports whether this fake measures audio lengths.
func (runtime *Runtime) AudioProbeAvailable() bool {
	return runtime.AudioProbe
}

// DatabaseSize reports this fake's database file size and whether it can know it.
func (runtime *Runtime) DatabaseSize() (bytes int64, known bool) {
	return runtime.DatabaseBytes, runtime.DatabaseKnown
}
