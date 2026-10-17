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

package app

import (
	"os"
	"sync"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/infrastructure/delayer"
	"rdio-scanner/internal/infrastructure/dirwatch"
	"rdio-scanner/internal/infrastructure/persistence"
)

// runtimeState answers what this server is running, which is the one question the status read asks that
// no single part of the server can answer on its own: the hub holds the listener connections, the
// watchers hold the directories, the delayer holds the calls, the socket holds the administrator
// connections, and the composition root is the only thing holding all of them.
//
// It is an adapter of the composition root rather than a read added to each holder, and it is read-only:
// nothing here starts, stops, or changes anything, which is what keeps it apart from the write-shaped port
// the configuration saves already use.
type runtimeState struct {
	hub      ports.Hub
	watches  *dirwatch.Dirwatchers
	held     *delayer.Delayer
	sessions administratorConnections

	converter ports.AudioConverter
	prober    ports.AudioLengthProber

	// dialect and dbPath are what decides whether this server can state the size of its own database at
	// all: a deployment writing into a file it owns can, and a deployment whose data lives inside a
	// database server process it does not own cannot.
	dialect persistence.Dialect
	dbPath  string

	// startedAt is the instant this server's start sequence ran, and the zero instant until it has run.
	// It is guarded because the status read is answered on a request goroutine while the start sequence
	// runs on another.
	mutex     sync.RWMutex
	startedAt time.Time
}

// administratorConnections is the one question the status read asks of the administrator socket. It is
// asked through this small interface rather than added to the port the configuration saves notify,
// because counting the connections an operator is holding is not the same job as telling them the
// configuration changed.
type administratorConnections interface {
	Count() int
}

var _ ports.RuntimeState = (*runtimeState)(nil)

// markStarted records the instant the start sequence ran. A server that has not started has not recorded
// anything, and the status read says so rather than inventing an uptime.
func (state *runtimeState) markStarted(at time.Time) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.startedAt = at
}

// ServerStarted reports the instant this server's start sequence ran.
func (state *runtimeState) ServerStarted() time.Time {
	state.mutex.RLock()
	defer state.mutex.RUnlock()

	return state.startedAt
}

// ListenerCount reports how many listener connections the hub is holding.
func (state *runtimeState) ListenerCount() int {
	return state.hub.ListenerCount()
}

// AdministratorSessionCount reports how many administrator connections the server is holding, the page
// asking about them among them.
func (state *runtimeState) AdministratorSessionCount() int {
	return state.sessions.Count()
}

// DirwatchersRunning reports how many directory watchers are watching right now, which is not the number
// the configuration names and not the number that is enabled.
func (state *runtimeState) DirwatchersRunning() int {
	return state.watches.Running()
}

// HeldCallCount reports how many calls are waiting out a hold right now.
func (state *runtimeState) HeldCallCount() int {
	return state.held.Holding()
}

// AudioConversionAvailable reports whether this deployment converts audio.
func (state *runtimeState) AudioConversionAvailable() bool {
	return programIsAvailable(state.converter)
}

// AudioProbeAvailable reports whether this deployment measures the length of a recording.
func (state *runtimeState) AudioProbeAvailable() bool {
	return programIsAvailable(state.prober)
}

// DatabaseSize reports the size of the file this server writes its database into.
//
// Only a deployment writing into a file it owns can answer. A number read out of a database server
// process this server does not own means something different on each backend — the size of a data
// directory, of a tablespace, of nothing at all — and a number whose meaning depends on the backend is
// worse on a status page than no number.
func (state *runtimeState) DatabaseSize() (bytes int64, known bool) {
	if state.dialect != persistence.DialectSqlite || state.dbPath == "" {
		return 0, false
	}

	info, err := os.Stat(state.dbPath)
	if err != nil {
		return 0, false
	}

	return info.Size(), true
}

// audioProgramIsAvailable is the question this server asks of the two audio programs it runs: whether the
// program answered the probe it was asked when the server started.
//
// It is asked here, in the composition root that built the program, rather than added to the port the
// ingest path converts through: converting is what the pipeline needs and reporting what this deployment
// has is what the status view needs, and neither should depend on the other's question. A program that
// cannot be asked is a program this server cannot count on, and is reported as not available.
type audioProgramIsAvailable interface {
	Available() bool
}

func programIsAvailable(program any) bool {
	reporter, ok := program.(audioProgramIsAvailable)

	return ok && reporter.Available()
}
