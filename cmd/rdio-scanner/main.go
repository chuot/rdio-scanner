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
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"rdio-scanner/internal/app"
	"rdio-scanner/internal/application/ports"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run dispatches the modes the command line asks for. It returns the process exit code instead of calling
// os.Exit directly, so the modes stay testable.
func run(args []string, stdout io.Writer) int {
	exePath, err := os.Executable()
	if err != nil {
		exePath = ""
	}

	config, mode, err := newConfig(args, exePath, stdout)
	if err != nil {
		var usage usageError
		if errors.As(err, &usage) {
			return 2
		}

		fmt.Fprintf(stdout, "error: %v\n", err)
		return 1
	}

	if mode == ModeExit {
		return 0
	}

	// The -cmd mode drives a server that is already running and then stops. It outranks the administrator
	// password mode because the legacy server reaches it first, and it needs neither this deployment's
	// database nor its configuration — only the address of a server that is answering.
	if mode == ModeCommand {
		return runCommand(config, exePath, stdout)
	}

	// The -service mode. An action the operator named is something the service manager does to the
	// deployment's service, and this process is only the one that asked for it, so it ends when the manager
	// has done it. The action the manager runs the service with is this process becoming the service, and the
	// server starts underneath it. Both come before the administrator password mode, as they do in the legacy
	// server, which reaches them while it is still reading the command line.
	if asksTheServiceManager(config.ServiceAction) {
		return controlService(config.ServiceAction)
	}

	// The administrator password mode is a one-shot that needs the database and nothing else: it is the
	// way a deployment sets its credential from a script, or the way an operator recovers a server whose
	// credential was lost.
	if config.NewAdminPassword != "" {
		return changeAdminPassword(config)
	}

	return serve(config, stdout)
}

// serve starts the server and answers with it until the operator stops it.
func serve(config *Config, stdout io.Writer) int {
	// The banner is said before the database is opened, which is the order the legacy server says it in:
	// what the operator ran is visible before what went wrong.
	fmt.Fprintf(stdout, "\nRdio Scanner v%s\n", Version)
	fmt.Fprintf(stdout, "----------------------------------\n")

	// A server the service manager started is the manager's service first and the server second, which is
	// the order the legacy server has them in: the manager's own loop is handed a goroutine before the
	// database is opened, so a server that cannot start is a service the manager can see failing.
	var echo app.LogEcho

	if config.ServiceAction == serviceActionRun {
		d, err := newDaemon()
		if err != nil {
			return exitWithError(err)
		}

		d.run()

		echo = app.NewServiceLog(d.logger)
	}

	srv, err := app.New(serverConfig(config, os.Stderr, echo))
	if err != nil {
		return exitWithError(err)
	}

	if err := srv.Start(context.Background()); err != nil {
		srv.Close()

		return exitWithError(err)
	}

	err = srv.Serve(context.Background())

	srv.Close()

	if err != nil {
		return exitWithError(err)
	}

	return 0
}

// serverConfig is the configuration the composition root builds a server from: the effective configuration
// the command line and the ini file produced, with the three things this program decides on its own way of
// running — where the server says what it says out loud, whose copy of the log entries an operator reads,
// and which ffmpeg the server converts with.
func serverConfig(config *Config, out io.Writer, echo app.LogEcho) app.Config {
	return app.Config{
		BaseDir:     config.BaseDir,
		DbType:      config.DbType,
		DbFile:      config.DbFile,
		DbHost:      config.DbHost,
		DbPort:      config.DbPort,
		DbName:      config.DbName,
		DbUsername:  config.DbUsername,
		DbPassword:  config.DbPassword,
		Listen:      config.Listen,
		SslAutoCert: config.SslAutoCert,
		SslCertFile: config.SslCertFile,
		SslKeyFile:  config.SslKeyFile,
		SslListen:   config.SslListen,
		Version:     Version,
		Out:         out,
		Echo:        echo,
		Converter: func(dir string) (ports.AudioConverter, string) {
			choice := theConverter(bundledFFmpeg(dir))

			return choice.converter(), choice.Notice
		},
	}
}

// changeAdminPassword is the -admin_password mode: store the credential the administrator chose, say so in
// the log view, and stop without starting a server.
func changeAdminPassword(config *Config) int {
	ctx := context.Background()

	srv, err := app.New(serverConfig(config, os.Stderr, nil))
	if err != nil {
		return exitWithError(err)
	}

	if err := srv.ChangeAdminPassword(ctx, config.NewAdminPassword); err != nil {
		srv.Close()

		return exitWithError(err)
	}

	if err := srv.Close(); err != nil {
		return exitWithError(err)
	}

	return 0
}

// exitWithError reports a failure the way the legacy server reports one — the line, on the standard error,
// with the moment it was said — and returns the exit code the process ends with.
func exitWithError(err error) int {
	log.Println(err)

	return 1
}

// usageError marks a command-line parsing failure, which maps to exit code 2.
type usageError struct {
	err error
}

func (e usageError) Error() string {
	return e.err.Error()
}

func (e usageError) Unwrap() error {
	return e.err
}

var _ error = usageError{}
