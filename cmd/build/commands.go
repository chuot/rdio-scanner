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
	"strings"

	"rdio-scanner/internal/buildinfo"
)

// The subcommands the tool knows. An empty one is the whole pipeline: both applications, then the
// ffmpeg the server carries, then the server that carries both.
const (
	subClient  = "client"
	subScanner = "scanner"
	subAdmin   = "admin"
	subFFmpeg  = "ffmpeg"
	subServer  = "server"
	subVersion = "version"
)

// presentationDir is where the two applications live and where their dependencies are installed. Each
// application writes into the bundle directory of the Go package that embeds it, so the build has
// nowhere to send an output directory.
const presentationDir = "internal/presentation"

// options is what the command line asked for, once it is known to be a command line this tool
// honours.
type options struct {
	sub    string
	pre    string
	output string

	// target is the platform the ffmpeg phase compiles for, which is how the release matrix cross-builds
	// from one machine. An empty one is the target this tool is running on.
	target string

	// installClients is decided by the tool looking at the tree, not by the operator: a first run
	// on a fresh clone has no esbuild to build with.
	installClients bool
}

func (o options) buildsClients() bool {
	return o.sub == "" || o.sub == subClient || o.sub == subScanner || o.sub == subAdmin
}

func (o options) buildsFFmpeg() bool {
	return o.sub == "" || o.sub == subFFmpeg
}

func (o options) buildsServer() bool {
	return o.sub == "" || o.sub == subServer
}

// defaultOutputPath is where the server binary lands when the operator did not say: the per-platform
// directory the release matrix writes to. A run that named no target is a run for the machine it is
// running on, and a windows target's binary carries the suffix its operating system puts on a program.
func defaultOutputPath(target string) string {
	if target == "" {
		target = runtimeTarget()
	}

	return filepath.Join("dist", target, "rdio-scanner"+programSuffix(target))
}

// parseArgs reads the command line and refuses the ones this tool cannot honour. A prerelease name
// that does not fit the version grammar is refused here rather than halfway through a build, because
// it is an instruction the operator gave and not a fact the tree got wrong.
//
// The default output path is decided after the whole command line is read, because the target that
// decides it can be named before or after the flag: the path a run writes to is a fact about the
// target it is building for, and a target named after `-o` still decides it.
func parseArgs(args []string) (options, error) {
	o := options{}
	namedOutput := false

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
			namedOutput = true

		case subClient, subScanner, subAdmin, subFFmpeg, subServer, subVersion:
			if o.sub != "" {
				return options{}, fmt.Errorf("%s and %s are not one command: name one thing to build", o.sub, arg)
			}

			o.sub = arg

			// The ffmpeg phase is the one thing this tool builds for a platform the operator names,
			// because that is how the release matrix cross-builds from one machine. The platform has to
			// be the word right after it, so that a target left lying at the end of a command line is
			// refused instead of quietly building something else.
			if arg == subFFmpeg && i+1 < len(args) && isTarget(args[i+1]) {
				i++

				o.target = args[i]
			}

		default:
			return options{}, fmt.Errorf("%s is not something this tool builds or a flag it knows", arg)
		}
	}

	if !namedOutput {
		o.output = defaultOutputPath(o.target)
	}

	return o, nil
}

// isTarget reports whether a word names a platform this tool can be asked to build for: a Go operating
// system and a Go architecture, joined the way go names them. A word that does not fit is a mistake in
// the command line, and refusing it here is cheaper than a build that goes the wrong way.
//
// A platform this project compiles no ffmpeg for still fits, and the ffmpeg phase says so.
func isTarget(word string) bool {
	goos, goarch, found := strings.Cut(word, "-")
	if !found {
		return false
	}

	for _, part := range []string{goos, goarch} {
		if part == "" {
			return false
		}

		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				return false
			}
		}
	}

	return true
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
	subScanner: "internal/presentation/scanner/frontend/bundle",
	subAdmin:   "internal/presentation/admin/frontend/bundle",
}

// plan composes the commands the options asked for, in the order they have to happen: the scanner
// application, then the administration application. The ffmpeg and the server that carries both come
// after, because the server embeds the bundles and the ffmpeg the build produced.
func plan(o options, repo string, version buildinfo.Version, npmName string) []step {
	steps := []step{}

	presentation := filepath.Join(repo, presentationDir)

	if o.buildsClients() {
		if o.installClients {
			steps = append(steps, step{
				label:  "deps",
				result: "installing the client dependencies",
				dir:    presentation,
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
				dir:    presentation,
				name:   npmName,
				args:   []string{"run", "build:" + app},
				// The one way the version reaches a bundle. No source file is rewritten to carry it.
				env: []string{"RDIO_VERSION=" + version.String()},
			})
		}
	}

	return steps
}

// serverStep is the server binary for the target the run is building for, composed last because it
// embeds both applications and the ffmpeg compiled for that target. The version reaches it through the
// linker, which is the mechanism that replaced the source file an operator had to edit by hand. The
// target reaches it through the environment, which is how go is told to cross-compile: a server built
// for the machine the tool runs on while its ffmpeg was built for another target is a release that
// ships neither.
func serverStep(o options, repo string, version buildinfo.Version) step {
	target := o.target
	if target == "" {
		target = runtimeTarget()
	}

	goos, goarch, _ := strings.Cut(target, "-")

	return step{
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
		env: []string{"GOOS=" + goos, "GOARCH=" + goarch},
	}
}
