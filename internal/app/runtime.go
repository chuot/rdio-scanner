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

type runtimeState struct {
	hub      ports.Hub
	watches  *dirwatch.Dirwatchers
	held     *delayer.Delayer
	sessions administratorConnections

	converter ports.AudioConverter
	prober    ports.AudioLengthProber

	// dialect and dbPath decide whether this server can state its own database size at all.
	dialect persistence.Dialect
	dbPath  string

	// startedAt is the instant this server's start sequence ran, zero until it has run.
	// see the status-endpoint-reads maintainer note.
	mutex     sync.RWMutex
	startedAt time.Time
}

// administratorConnections is the question the status read asks of the admin socket.
// see the status-endpoint-reads maintainer note.
type administratorConnections interface {
	Count() int
}

var _ ports.RuntimeState = (*runtimeState)(nil)

// markStarted records the instant the start sequence ran; an unstarted server records
// nothing, and the status read says so rather than inventing an uptime.
// see the status-endpoint-reads maintainer note.
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

// AdministratorSessionCount reports how many administrator connections the server holds.
// see the status-endpoint-reads maintainer note.
func (state *runtimeState) AdministratorSessionCount() int {
	return state.sessions.Count()
}

// DirwatchersRunning reports how many directory watchers this server has started and has
// not stopped, which is neither the number the configuration names nor the number enabled.
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

// DatabaseSize reports the size of the file this server writes its database into, known only
// when it owns that file: a backend-dependent number misleads a status page.
// see the status-endpoint-reads maintainer note.
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

// audioProgramIsAvailable is the server's question to the audio programs it runs: did the
// program answer the probe asked at startup. It stays in the composition root so the status
// view does not depend on the ingest port, and a program that cannot be asked is reported
// as not available.
type audioProgramIsAvailable interface {
	Available() bool
}

func programIsAvailable(program any) bool {
	reporter, ok := program.(audioProgramIsAvailable)

	return ok && reporter.Available()
}
