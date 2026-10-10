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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/kardianos/service"

	"rdio-scanner/internal/domain"
)

// The composition root is the one place that decides whose copy of the server's log entries an operator
// reads. A server running in a terminal says every entry it files out loud; a server the service manager
// started says them to the manager's log and has nothing to say to a terminal. The hub and the listener
// endpoint have no console of their own — everything they say about a connection is an entry in the server
// log — so the only way to prove the wiring is to drive a real connection through the real server and read
// the console the composition root handed it. A hub handed a log no operator ever reads would pass every
// test in the hub's own package.

// TestAServerAnOperatorStartedSaysItsLogEntriesToTheTerminal is the copy of the log an operator running the
// server in a terminal expects: every entry the server writes to its log view is said out loud as well.
func TestAServerAnOperatorStartedSaysItsLogEntriesToTheTerminal(t *testing.T) {
	var terminal bytes.Buffer

	srv := testServerOn(t, &terminal)

	const message = "the recording could not be stored"

	if err := srv.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Now(),
		Level:    domain.LogLevelError,
		Source:   domain.LogSourceCalls,
		Message:  message,
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	if !bytes.Contains(terminal.Bytes(), []byte("error calls: the recording could not be stored")) {
		t.Errorf("the terminal holds %q, want the entry said with its level and the part of the server that "+
			"wrote it", terminal.String())
	}

	// The stored copy is the sentence on its own. The level and the source belong to the rendering, not to
	// what the log view holds, and an operator filtering the log view by subsystem must not have to know how
	// the terminal prints it.
	if !hasLogMessage(t, srv.db, message) {
		t.Errorf("the log view does not hold the entry the server wrote")
	}
}

// TestAServerTheServiceManagerStartedSaysItsLogEntriesToTheManager is the same server with a service manager
// in front of it instead of a terminal: the entries go to the manager's log, at the level each entry is
// filed under, and the terminal stays silent — which is what the legacy server does when it is running as a
// service, and what an operator reading the service's own log expects to find.
func TestAServerTheServiceManagerStartedSaysItsLogEntriesToTheManager(t *testing.T) {
	var terminal bytes.Buffer

	manager := &recordedServiceLogger{}

	srv := testServerUnderTheServiceManager(t, &terminal, manager)

	const message = "the recording could not be stored"

	if err := srv.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Now(),
		Level:    domain.LogLevelError,
		Source:   domain.LogSourceCalls,
		Message:  message,
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	if !manager.has(domain.LogLevelError, "error calls: the recording could not be stored") {
		t.Errorf("the service manager's log holds %q, want the entry at the level it names, with the part of "+
			"the server that wrote it", manager.String())
	}

	if terminal.Len() != 0 {
		t.Errorf("the terminal holds %q, want a server running as a service to have nothing to say to one", terminal.String())
	}

	// The administrator's copy is the same either way: which operator copy was used changes who reads the
	// line, not whether the line is in the log.
	if !hasLogMessage(t, srv.db, message) {
		t.Errorf("the log view does not hold the entry the server wrote")
	}
}

// TestTheServiceLogTakesEachEntryAtTheLevelItIsFiledUnder is the routing itself, including the entry filed
// under a level the service manager has no word for: the legacy server's switch has no default case, so such
// an entry reaches the log view and no operator's copy of it.
func TestTheServiceLogTakesEachEntryAtTheLevelItIsFiledUnder(t *testing.T) {
	manager := &recordedServiceLogger{}

	echo := NewServiceLog(manager)

	echo.Echo(domain.LogLevelError, domain.LogSourceCalls, "the recording could not be stored")
	echo.Echo(domain.LogLevelWarn, domain.LogSourceDirwatch, "the watched directory is not there")
	echo.Echo(domain.LogLevelInfo, domain.LogSourceServer, "server started")
	echo.Echo("verbose", domain.LogSourceServer, "an entry filed under a level the manager has no word for")

	for _, want := range []struct{ level, message string }{
		{domain.LogLevelError, "error calls: the recording could not be stored"},
		{domain.LogLevelWarn, "warn dirwatch: the watched directory is not there"},
		{domain.LogLevelInfo, "info server: server started"},
	} {
		if !manager.has(want.level, want.message) {
			t.Errorf("the service manager's log holds %q, want %q at the level %s", manager.String(), want.message, want.level)
		}
	}

	if got := manager.count(); got != 3 {
		t.Errorf("the service manager was told about %d entries, want the three whose levels it has a word for", got)
	}
}

// TestAConnectionTheServerWouldNotTakeOverIsSaidOutLoud is the line the listener endpoint used to only
// print, now stored as well: an operator reading the terminal and an operator reading the log view are told
// about the same connection, in the shape each of them searches in.
func TestAConnectionTheServerWouldNotTakeOverIsSaidOutLoud(t *testing.T) {
	var terminal bytes.Buffer

	srv := testServerOn(t, &terminal)

	if rec := srv.askForAnUpgrade("/"); rec.Code != http.StatusBadRequest {
		t.Fatalf("a request that asks to become a listener connection and is not one was answered %d, want "+
			"the answer Go's upgrader gives", rec.Code)
	}

	const message = "a listener connection was refused"

	waitForLogMentioning(t, srv.db, message)

	if !bytes.Contains(terminal.Bytes(), []byte("warn livefeed: "+message)) {
		t.Errorf("the terminal holds %q, want the entry said with its level and the part of the server that "+
			"wrote it", terminal.String())
	}
}

// TestWhatTheHubReportsAboutAListenerIsSaidOutLoud is the other half of the live feed's lines. The hub has no
// console of its own: everything it says about a connection — a listener that joined, a frame it could not
// read, a frame it could not write — is an entry in the server log, and the composition root is what says those
// entries out loud. Driving a real listener through the real hub is what proves the wiring, because the hub
// could be handed a log no operator ever reads and every test in the hub's own package would still pass.
func TestWhatTheHubReportsAboutAListenerIsSaidOutLoud(t *testing.T) {
	var terminal bytes.Buffer

	srv := testServerOn(t, &terminal)

	conn := srv.dial(t, "/")

	sendFrame(t, conn, `["CFG"]`)

	const message = "a listener joined from 127.0.0.1"

	waitForLogMentioning(t, srv.db, message)

	if !bytes.Contains(terminal.Bytes(), []byte("info livefeed: "+message)) {
		t.Errorf("the terminal holds %q, want the hub's entry said with its level and the part of the server "+
			"that wrote it", terminal.String())
	}
}

// testServerUnderTheServiceManager is the server the composition root assembles when the service manager is
// the one that started the program: the manager's log is the operator's copy, and the terminal the program
// was started with has nothing to say.
func testServerUnderTheServiceManager(t *testing.T, out io.Writer, manager *recordedServiceLogger) *Server {
	t.Helper()

	config := testConfigWith(t, out)
	config.Echo = NewServiceLog(manager)

	srv, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	return srv
}

// The manager's log has three levels and a formatted form of each, and every one of them reports whether the
// line got through. Nothing here cares: the server's log entries are the unformatted kind, and a line that
// did not get through is not something the server can do anything about.
type recordedServiceLogger struct {
	mutex   sync.Mutex
	levels  []string
	entries []string
}

var _ service.Logger = (*recordedServiceLogger)(nil)

func (manager *recordedServiceLogger) Error(v ...any) error {
	return manager.record(domain.LogLevelError, v)
}
func (manager *recordedServiceLogger) Info(v ...any) error {
	return manager.record(domain.LogLevelInfo, v)
}
func (manager *recordedServiceLogger) Warning(v ...any) error {
	return manager.record(domain.LogLevelWarn, v)
}

func (manager *recordedServiceLogger) Errorf(format string, a ...any) error {
	return manager.record(domain.LogLevelError, []any{fmt.Sprintf(format, a...)})
}

func (manager *recordedServiceLogger) Infof(format string, a ...any) error {
	return manager.record(domain.LogLevelInfo, []any{fmt.Sprintf(format, a...)})
}

func (manager *recordedServiceLogger) Warningf(format string, a ...any) error {
	return manager.record(domain.LogLevelWarn, []any{fmt.Sprintf(format, a...)})
}

func (manager *recordedServiceLogger) record(level string, v []any) error {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	manager.levels = append(manager.levels, level)
	manager.entries = append(manager.entries, fmt.Sprint(v...))

	return nil
}

// has reports whether the manager was told this line at this level.
func (manager *recordedServiceLogger) has(level, message string) bool {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	for i, entry := range manager.entries {
		if entry == message && manager.levels[i] == level {
			return true
		}
	}

	return false
}

// count reports how many lines the manager was given.
func (manager *recordedServiceLogger) count() int {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	return len(manager.entries)
}

// String is what a failing test says the manager was told.
func (manager *recordedServiceLogger) String() string {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	out := ""

	for i, entry := range manager.entries {
		out += fmt.Sprintf("%s: %s; ", manager.levels[i], entry)
	}

	return out
}
