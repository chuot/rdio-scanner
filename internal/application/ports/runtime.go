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

package ports

import (
	"time"
)

// RuntimeState answers what the server is running, as opposed to what it has been configured to run.
//
// It is one port rather than a read added to each holder, because the status view needs all of these
// answers in one read and the only thing that holds the hub, the watches, the holds, the converter, the
// prober, and the administrator sessions at the same time is the composition root.
//
// It is read-only, and none of it takes a context: every answer is a count something already in memory
// is holding, the same shape Hub.ListenerCount already has. Nothing in it starts, stops, or changes
// anything, which is what keeps it apart from Dirwatchers — the write-shaped port the configuration
// saves already use.
type RuntimeState interface {
	// ServerStarted is the instant this server's start sequence ran. A server that has not started
	// answers the zero instant.
	ServerStarted() time.Time

	// ListenerCount is how many listener connections the server is holding.
	ListenerCount() int

	// AdministratorSessionCount is how many administrator connections the server is holding, including
	// the one asking.
	AdministratorSessionCount() int

	// DirwatchersRunning is how many directory watchers this server has started and has not stopped:
	// not the number configured, and not the number enabled.
	DirwatchersRunning() int

	// HeldCallCount is how many calls are waiting out a hold right now.
	HeldCallCount() int

	// AudioConversionAvailable is whether the audio converter this server probed at startup works.
	AudioConversionAvailable() bool

	// AudioProbeAvailable is whether the audio-length prober this server probed at startup works.
	AudioProbeAvailable() bool

	// DatabaseSize reports the size of the server's own database file, and whether this server can know
	// it at all. A deployment whose data lives in a database server process this one does not own
	// answers 0 and false rather than a number that means something different on each backend.
	DatabaseSize() (bytes int64, known bool)
}
