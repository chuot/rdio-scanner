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
	"os"
	"runtime"

	"github.com/kardianos/service"
)

// the -service mode runs the server under the operating system's service
// manager, and it has two halves that look alike and are not the same:
// naming an action makes this process a client that asks the manager and
// ends, and the manager's own run action makes it the service itself.

// serviceActionRun is the action the installed service configuration runs
// the service with, not something an operator types.
const serviceActionRun = "run"

// daemon is one deployment's arrangement with the service manager.
type daemon struct {
	iface   *daemonInterface
	service service.Service
	logger  service.Logger
}

// newDaemon builds that arrangement: the name the manager knows the service
// by, the description an operator sees, and the command line to run it with.
func newDaemon() (*daemon, error) {
	// the FreeBSD manager rejects a hyphen in a service name, so the service is
	// registered under two names.
	name := "rdio-scanner"
	if runtime.GOOS == "freebsd" {
		name = "rdioscanner"
	}

	// a process id is always a process, so this lookup cannot fail on the
	// systems that answer this way, and its error is left as it is.
	process, _ := os.FindProcess(os.Getpid())

	d := &daemon{iface: &daemonInterface{process: process}}

	svc, err := service.New(d.iface, &service.Config{
		Name:        name,
		DisplayName: "Rdio Scanner",
		Description: "The perfect software-defined radio companion",
		Arguments:   []string{"-service", serviceActionRun},
	})
	if err != nil {
		return nil, err
	}

	logger, err := svc.Logger(nil)
	if err != nil {
		return nil, err
	}

	d.service = svc
	d.logger = logger

	return d, nil
}

// run hands the process to the manager, whose own loop starts the
// server.
func (d *daemon) run() {
	go func() {
		if err := d.service.Run(); err != nil {
			d.logger.Error(err)
		}
	}()
}

// asksTheServiceManager reports whether the named action is one to ask the
// manager to do rather than the action the manager runs the service with;
func asksTheServiceManager(action string) bool {
	return action != "" && action != serviceActionRun
}

// controlService asks the manager to act, and a refusal the manager answers
// with is said to the operator who asked, through the same failure path this
// mode already uses for its other failures and ending with the same code;
// what the manager said is passed on unchanged.
func controlService(action string) int {
	d, err := newDaemon()
	if err != nil {
		return exitWithError(err)
	}

	if err := service.Control(d.service, action); err != nil {
		return exitWithError(err)
	}

	return 0
}

// daemonInterface is what the manager is told the service does: its start is
// the server's start, and its stop signals this process so shutdown happens
// on the one path the server already answers.
type daemonInterface struct {
	process *os.Process
}

var _ service.Interface = (*daemonInterface)(nil)

func (iface *daemonInterface) Start(s service.Service) error {
	return nil
}

func (iface *daemonInterface) Stop(s service.Service) error {
	if iface.process != nil {
		iface.process.Signal(os.Interrupt)
	}

	return nil
}
