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
	"fmt"
	"path/filepath"
	"runtime"

	"rdio-scanner/internal/buildinfo"
)

// The subcommands the tool knows. An empty one is the whole pipeline: both applications, then the
// server that carries them.
const (
	subClient  = "client"
	subScanner = "scanner"
	subAdmin   = "admin"
	subServer  = "server"
	subVersion = "version"
)

// clientsDir is where the two applications live and where their dependencies are installed. Each
// application writes into the bundle directory of the Go package that embeds it, so the build has
// nowhere to send an output directory.
const clientsDir = "internal/presentation/clients"

// options is what the command line asked for, once it is known to be a command line this tool
// honours.
type options struct {
	sub    string
	pre    string
	output string

	// installClients is decided by the tool looking at the tree, not by the operator: a first run
	// on a fresh clone has no esbuild to build with.
	installClients bool
}

func (o options) buildsClients() bool {
	return o.sub == "" || o.sub == subClient || o.sub == subScanner || o.sub == subAdmin
}

func (o options) buildsServer() bool {
	return o.sub == "" || o.sub == subServer
}

// defaultOutputPath is where the server binary lands when the operator did not say: the same
// per-platform directory the release matrix writes to.
func defaultOutputPath() string {
	return filepath.Join("dist", runtime.GOOS+"-"+runtime.GOARCH, "rdio-scanner")
}

// parseArgs reads the command line and refuses the ones this tool cannot honour. A prerelease name
// that does not fit the version grammar is refused here rather than halfway through a build, because
// it is an instruction the operator gave and not a fact the tree got wrong.
func parseArgs(args []string) (options, error) {
	o := options{output: defaultOutputPath()}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch arg {
		case "--pre", "-pre":
			if i+1 >= len(args) {
				return options{}, fmt.Errorf("%s needs a prerelease name, like --pre beta1", arg)
			}

			i++

			if err := buildinfo.ValidatePre(args[i]); err != nil {
				return options{}, err
			}

			o.pre = args[i]

		case "-o", "--output":
			if i+1 >= len(args) {
				return options{}, fmt.Errorf("%s needs a path to write the server binary to", arg)
			}

			i++

			o.output = args[i]

		case subClient, subScanner, subAdmin, subServer, subVersion:
			if o.sub != "" {
				return options{}, fmt.Errorf("%s and %s are not one command: name one thing to build", o.sub, arg)
			}

			o.sub = arg

		default:
			return options{}, fmt.Errorf("%s is not something this tool builds or a flag it knows", arg)
		}
	}

	return o, nil
}

// step is one external command the tool runs: a directory, a program, its arguments, and the extra
// environment it needs. It is composed here and executed there, so the shape of a command line is
// something a test can read.
type step struct {
	label string

	// result is what the step leaves behind, said once it ran.
	result string

	dir  string
	name string
	args []string
	env  []string
}

// appBundle is the bundle directory of the Go package that embeds each application. A build has
// nowhere to send an output directory because the destination is a fact about where the application
// lives.
var appBundle = map[string]string{
	subScanner: "internal/presentation/serving/scanner/bundle",
	subAdmin:   "internal/presentation/serving/admin/bundle",
}

// plan composes the commands the options asked for, in the order they have to happen: the scanner
// application, then the administration application, then the server that embeds both.
func plan(o options, repo string, version buildinfo.Version, npmName string) []step {
	steps := []step{}

	clients := filepath.Join(repo, clientsDir)

	if o.buildsClients() {
		if o.installClients {
			steps = append(steps, step{
				label:  "deps",
				result: "installing the client dependencies",
				dir:    clients,
				name:   npmName,
				args:   []string{"ci", "--loglevel=error", "--no-progress"},
			})
		}

		for _, app := range []string{subScanner, subAdmin} {
			if o.sub != "" && o.sub != subClient && o.sub != app {
				continue
			}

			steps = append(steps, step{
				label:  app,
				result: appBundle[app],
				dir:    clients,
				name:   npmName,
				args:   []string{"run", "build:" + app},
				// The one way the version reaches a bundle. No source file is rewritten to carry it.
				env: []string{"RDIO_VERSION=" + version.String()},
			})
		}
	}

	if o.buildsServer() {
		steps = append(steps, step{
			label:  subServer,
			result: o.output,
			dir:    repo,
			name:   "go",
			args: []string{
				"build",
				"-ldflags", "-X main.Version=" + version.String(),
				"-o", o.output,
				"./cmd/rdio-scanner",
			},
		})
	}

	return steps
}
