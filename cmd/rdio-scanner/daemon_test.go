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

package main

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/kardianos/service"

	"rdio-scanner/internal/domain"
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

	code := run([]string{"-base_dir", t.TempDir(), "-service", "bogus"}, &out, noBundlesUnbuilt)

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
