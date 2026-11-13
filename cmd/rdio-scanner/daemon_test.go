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
	"io"
	"log"
	"strings"
	"sync"
	"testing"

	"github.com/kardianos/service"

	"rdio-scanner/internal/domain"
)

// with no service manager to talk to, these tests pin what a refused control
// ends with and says, and which actions are asked for at all.

// A control the manager refuses is said to the operator who asked for it,
// through the house failure path: the failure names the action that was asked
// for and the reason the manager gave, and the process ends with the code the
// other one-shot failures end with.
func TestARefusedServiceControlEndsWithTheHouseFailureCodeAndSaysWhy(t *testing.T) {
	// the failure is said through the standard logger, so the test watches
	// where that logger writes for the duration of the run
	logOut := &bytes.Buffer{}

	previous := log.Writer()
	log.SetOutput(logOut)

	defer log.SetOutput(previous)

	code := run([]string{"-base_dir", t.TempDir(), "-service", "bogus"}, io.Discard, noBundlesUnbuilt)

	said := logOut.String()

	if code != 1 {
		t.Errorf("an action the service manager refused ended with %d, want the code a failure ends with", code)
	}

	if !strings.Contains(said, "bogus") {
		t.Errorf("the failure does not name the action that was asked for:\n%s", said)
	}

	// the reason is the manager's own words for the action it does not
	// know, named here rather than the whole sentence it composes.
	if !strings.Contains(said, "Unknown action") {
		t.Errorf("the failure does not carry the reason the service manager gave:\n%s", said)
	}
}

// the run action the manager starts the installed service with is not a request
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

// recordedServiceLogger is a service manager's log that remembers what it was
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
