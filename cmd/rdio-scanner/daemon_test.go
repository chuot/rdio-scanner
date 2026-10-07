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

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/kardianos/service"

	"rdio-scanner/server-ng/internal/domain"
)

// The -service mode is the server under the operating system's service manager. What these tests can prove
// on a machine with no service manager to talk to is the two decisions the mode is made of: which actions
// are something to ask the manager for, and whose copy of the server's log entries an operator reads when
// there is no terminal in front of the server.

// TestAnActionTheServiceManagerDoesNotKnowEndsWithAFailureAndSaysNothing is the mode's answer to an action it
// was not given to perform: the exit code is the only thing the operator is told, which is what the legacy
// server gives and what a script checking that exit code is relying on.
func TestAnActionTheServiceManagerDoesNotKnowEndsWithAFailureAndSaysNothing(t *testing.T) {
	var out bytes.Buffer

	code := run([]string{"-base_dir", t.TempDir(), "-service", "bogus"}, &out)

	if code != -1 {
		t.Errorf("an action the service manager does not know ended with %d, want the code of a refusal", code)
	}

	if out.Len() != 0 {
		t.Errorf("an action the service manager does not know said %q, want nothing said at all", out.String())
	}
}

// TestTheActionTheServiceManagerRunsWithIsNotAnActionToAskFor is the half of the mode that is not a request:
// the installed service runs the program with `run`, and that is the server starting under the manager rather
// than this process asking the manager for something and ending.
func TestTheActionTheServiceManagerRunsWithIsNotAnActionToAskFor(t *testing.T) {
	if asksTheServiceManager(serviceActionRun) {
		t.Errorf("the action the service manager runs the service with was treated as an action to ask for, want the server to start under the manager")
	}

	if asksTheServiceManager("") {
		t.Errorf("no action at all was treated as an action to ask the service manager for, want the ordinary server start")
	}

	for _, action := range []string{"start", "stop", "restart", "install", "uninstall"} {
		if !asksTheServiceManager(action) {
			t.Errorf("%s was not treated as an action to ask the service manager for, want this process to ask and end", action)
		}
	}
}

// TestAServerAnOperatorStartedSaysItsLogEntriesToTheTerminal is the copy of the log an operator running the
// server in a terminal expects: every entry the server writes to its log view is said out loud as well.
func TestAServerAnOperatorStartedSaysItsLogEntriesToTheTerminal(t *testing.T) {
	var terminal bytes.Buffer

	srv := testServerOn(t, &terminal)

	const message = "the recording could not be stored"

	if err := srv.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Now(),
		Level:    domain.LogLevelError,
		Message:  message,
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	if !bytes.Contains(terminal.Bytes(), []byte(message)) {
		t.Errorf("the terminal holds %q, want the log entry the server wrote", terminal.String())
	}

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

	srv := testServerUnderTheServiceManager(t, &terminal, &daemon{logger: manager})

	const message = "the recording could not be stored"

	if err := srv.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Now(),
		Level:    domain.LogLevelError,
		Message:  message,
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	if !manager.has(domain.LogLevelError, message) {
		t.Errorf("the service manager's log holds %q, want the log entry at the level it names", manager.String())
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

	log := serviceLog{logger: manager}

	log.echo(domain.LogLevelError, "the recording could not be stored")
	log.echo(domain.LogLevelWarn, "the watched directory is not there")
	log.echo(domain.LogLevelInfo, "server started")
	log.echo("verbose", "an entry filed under a level the manager has no word for")

	for _, want := range []struct{ level, message string }{
		{domain.LogLevelError, "the recording could not be stored"},
		{domain.LogLevelWarn, "the watched directory is not there"},
		{domain.LogLevelInfo, "server started"},
	} {
		if !manager.has(want.level, want.message) {
			t.Errorf("the service manager's log holds %q, want %q at the level %s", manager.String(), want.message, want.level)
		}
	}

	if got := manager.count(); got != 3 {
		t.Errorf("the service manager was told about %d entries, want the three whose levels it has a word for", got)
	}
}

// testServerUnderTheServiceManager is the server the composition root assembles when the service manager is
// the one that started the program.
func testServerUnderTheServiceManager(t *testing.T, out io.Writer, as *daemon) *server {
	t.Helper()

	srv, err := newServer(testConfig(t), out, as)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	return srv
}

// recordedServiceLogger is a service manager's log that remembers what it was told, and at which level.
//
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
