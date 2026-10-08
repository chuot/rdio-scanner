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
	"os"
	"runtime"

	"github.com/kardianos/service"

	"rdio-scanner/internal/domain"
)

// The -service mode is the server running under the operating system's service manager: systemd on a Linux
// deployment, the service control manager on Windows. It has two halves that look alike and are not the same
// thing at all.
//
// When the operator names an action — start, stop, restart, install, uninstall — this process is a client of
// the service manager. It asks for the action and ends with the code that says whether the manager did it,
// and it never becomes a server.
//
// When the service manager is the one that asked for this program, with the action it wrote into the service
// configuration at install time, this process *is* the service, and the server starts underneath it.

// serviceActionRun is the action the service manager runs the service with. It is what the installed service
// configuration says, and not something an operator types.
const serviceActionRun = "run"

// daemon is one deployment's arrangement with the service manager.
type daemon struct {
	iface   *daemonInterface
	service service.Service
	logger  service.Logger
}

// newDaemon builds that arrangement: the name the manager knows the service by, the description an operator
// sees in the manager's own tools, and the command line the manager will run the service with.
func newDaemon() (*daemon, error) {
	// The manager on FreeBSD will not accept a service name with a hyphen in it, which is why the service
	// has two names. https://github.com/kardianos/service/issues/223
	name := "rdio-scanner"
	if runtime.GOOS == "freebsd" {
		name = "rdioscanner"
	}

	// The process a stop signals is this one. A process id is always a process, and on the systems that
	// answer this way the lookup cannot fail, which is why the failure is left as it is.
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

// run hands this process to the service manager and lets it carry on, which is what the legacy server does:
// the manager's own loop runs on a goroutine of its own, and the server starts as it would have anyway.
func (d *daemon) run() {
	go func() {
		if err := d.service.Run(); err != nil {
			d.logger.Error(err)
		}
	}()
}

// asksTheServiceManager reports whether the action the operator named is something to ask the service
// manager to do, rather than the action the manager itself runs the service with. An empty action means the
// operator did not use this mode at all.
func asksTheServiceManager(action string) bool {
	return action != "" && action != serviceActionRun
}

// controlService asks the service manager to do what the operator asked for.
//
// An action the manager does not know is refused, and the legacy server says nothing about it: the exit code
// is the only thing the operator is given. That is reproduced, because a script that ran
// `-service install` on a system it has no business touching is told by the exit code and nothing else.
func controlService(action string) int {
	d, err := newDaemon()
	if err != nil {
		return exitWithError(err)
	}

	if err := service.Control(d.service, action); err != nil {
		return -1
	}

	return 0
}

// daemonInterface is what the service manager is told this service does. Its start is the server's start,
// which the composition root is already doing; its stop is the interrupt this server already answers by
// shutting its listeners down and closing what it had open.
//
// The stop signals this process rather than calling the shutdown directly for the same reason the legacy
// server does it that way: the manager's stop arrives on the manager's own goroutine, and the way this
// server shuts down is the way it shuts down when an operator presses the keys — one path, one owner.
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

// serviceLog is the copy of the server's log entries a service manager keeps: the manager's own log, at the
// level the entry is filed under. A server running as a service has no terminal in front of it, and the
// manager's log is what an operator reads.
type serviceLog struct {
	logger service.Logger
}

var _ logEcho = serviceLog{}

func (log serviceLog) echo(level string, source domain.LogSource, message string) {
	line := renderLogEntry(level, source, message)

	switch level {
	case domain.LogLevelError:
		log.logger.Error(line)

	case domain.LogLevelWarn:
		log.logger.Warning(line)

	case domain.LogLevelInfo:
		log.logger.Info(line)
	}
}
