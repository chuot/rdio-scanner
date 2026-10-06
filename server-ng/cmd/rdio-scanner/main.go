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

	"rdio-scanner/server-ng/internal/domain"
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

	srv, err := newServer(config, os.Stderr)
	if err != nil {
		return exitWithError(err)
	}

	if err := srv.start(context.Background()); err != nil {
		srv.stop(context.Background())

		return exitWithError(err)
	}

	srv.waitForInterrupt()

	err = srv.listen()

	srv.stop(context.Background())

	if err != nil {
		return exitWithError(err)
	}

	return 0
}

// changeAdminPassword is the -admin_password mode: store the credential the administrator chose, say so in
// the log view, and stop without starting a server.
//
// It is the legacy server's startup branch rather than a use case, because it is a way of running the
// program and not a thing the server does while it is running. It writes the whole configuration, as the
// legacy server does, which is also what stores the signing secret the options read generated — the one
// thing this mode does that a startup does not.
func changeAdminPassword(config *Config) int {
	ctx := context.Background()

	srv, err := newServer(config, os.Stderr)
	if err != nil {
		return exitWithError(err)
	}

	defer func() {
		if err := srv.db.Close(); err != nil {
			exitWithError(err)
		}
	}()

	hash, err := srv.hashes.Hash(ctx, config.NewAdminPassword)
	if err != nil {
		return exitWithError(err)
	}

	options, err := srv.options.ReadOptions(ctx)
	if err != nil {
		return exitWithError(err)
	}

	options.AdminPasswordHash = hash

	// A credential that is still the one the server ships with is one the administrator has to replace,
	// which is what the web application insists on until they do.
	options.AdminPasswordNeedChange = config.NewAdminPassword == domain.DefaultAdminPassword

	if err := srv.options.WriteOptions(ctx, options); err != nil {
		return exitWithError(err)
	}

	if err := srv.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: srv.clock.Now(),
		Level:    domain.LogLevelInfo,
		Message:  "admin password changed.",
	}); err != nil {
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
