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

	"rdio-scanner/cmd/build/buildinfo"
)

// The subcommands the tool knows. An empty one is the whole pipeline: both applications, then the
// ffmpeg the server carries, then the server that carries both.
const (
	subClient      = "client"
	subScanner     = "scanner"
	subAdmin       = "admin"
	subFFmpeg      = "ffmpeg"
	subFFmpegCheck = "ffmpeg-check"
	subServer      = "server"
	subVersion     = "version"
	subDist        = "dist"
	subClean       = "clean"
	subContainer   = "container"
)

// The container image is linux-amd64, because that is the architecture the Containerfile's guard checks
// for, and the name the image is tagged with.
const (
	containerTarget = "linux-amd64"
	containerImage  = "rdio-scanner:latest"
)

// The one thing a release writes that is not an external command: the archive. It is named so the log
// says what a release run did, the same way it says what it compiled.
const subArchive = "archive"

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

	// outputNamed says the operator said where the server binary goes. A release run writes one binary
	// per target, so the default path is decided again for each target, and a path the operator named
	// is not overwritten by the matrix.
	outputNamed bool
}

func (o options) buildsClients() bool {
	return o.sub == "" || o.sub == subClient || o.sub == subScanner || o.sub == subAdmin || o.sub == subDist || o.sub == subContainer
}

func (o options) buildsFFmpeg() bool {
	return o.sub == "" || o.sub == subFFmpeg || o.sub == subContainer
}

func (o options) buildsServer() bool {
	return o.sub == "" || o.sub == subServer
}

// buildsServerBinary is the run that compiles a server binary: the whole pipeline, the server
// subcommand, and the release matrix, which compiles one binary per target. The matrix asks the same
// question a server run asks, and a matrix that never asks it compiles every target on every run.
func (o options) buildsServerBinary() bool {
	return o.buildsServer() || o.sub == subDist
}

// planState is what the tree says about what has already been built. A phase that has nothing to do does
// not run, which is the rule the ffmpeg cache follows: the compiled pair is reused and nothing is
// compiled.
type planState struct {
	scannerCurrent bool
	adminCurrent   bool
	serverCurrent  bool
}

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
			o.outputNamed = true

		case subClient, subScanner, subAdmin, subFFmpeg, subFFmpegCheck, subServer, subVersion, subDist, subClean, subContainer:
			if o.sub != "" {
				return options{}, fmt.Errorf("%s and %s are not one command: name one thing to build", o.sub, arg)
			}

			o.sub = arg

			// A clean run builds nothing, so a target after it is a word lying at the end of a command
			// line. The container image is one architecture: the Containerfile's guard checks for the
			// ffmpeg compiled for linux-amd64, and a target named after it is a run that would build an
			// ffmpeg the image never embeds.
			if arg == subClean || arg == subContainer {
				if i+1 < len(args) && isTarget(args[i+1]) {
					if arg == subClean {
						return options{}, fmt.Errorf("a clean run builds nothing, so %s is not a target it is asked for", args[i+1])
					}

					return options{}, fmt.Errorf("the container image is %s, so %s is not a target a container run builds for", containerTarget, args[i+1])
				}

				if arg == subContainer {
					o.target = containerTarget
				}

				continue
			}

			// The ffmpeg phase, the release matrix, and the ffmpeg check are the three things this tool
			// works on for a platform the operator names, because that is how the release matrix
			// cross-builds from one machine. The platform has to be the word right after it, so that a
			// target left lying at the end of a command line is refused instead of quietly building
			// something else.
			if (arg == subFFmpeg || arg == subDist || arg == subFFmpegCheck) && i+1 < len(args) && isTarget(args[i+1]) {
				i++

				o.target = args[i]

				// The ffmpeg phase compiles for any platform the manifest can answer for, and says so
				// when it has no record. The matrix is a product decision: a target outside it is a
				// platform this project does not release, and building one is not what was asked for.
				// The check proves the pair a release carries, so it proves a released platform.
				if arg == subDist || arg == subFFmpegCheck {
					if _, found := targetFor(o.target); !found {
						if arg == subFFmpegCheck {
							return options{}, fmt.Errorf("%s is not a platform this project releases, so no pair is checked for it", o.target)
						}

						return options{}, fmt.Errorf("%s is not a platform this project releases, so no archive is built for it", o.target)
					}
				}
			}

		default:
			return options{}, fmt.Errorf("%s is not something this tool builds or a flag it knows", arg)
		}
	}

	// The check proves the pair of one target, on the platform that target runs on. It has no default:
	// a check that fell back to the machine it happens to stand on would prove a pair the release does
	// not ship, and record it as though it had.
	if o.sub == subFFmpegCheck && o.target == "" {
		return options{}, fmt.Errorf("the ffmpeg check proves the pair of one target: go run ./cmd/build %s <goos-goarch>", subFFmpegCheck)
	}

	if !o.outputNamed {
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

	_, goarch, _ := strings.Cut(target, "-")

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
		env: []string{"GOOS=" + serverGoos(target), "GOARCH=" + goarch},
	}
}

// containerStep asks podman to build the image. The version reaches it through the build argument the
// Containerfile reads into the linker flag: an image built without it reports the development fallback,
// which is the sign that nobody said what the image is. The server binary is compiled inside the image,
// so the run that prepares it builds no binary of its own.
func containerStep(o options, repo string, version buildinfo.Version) step {
	return step{
		label:  subContainer,
		result: containerImage,
		dir:    repo,
		name:   "podman",
		args: []string{
			"build",
			"--pull",
			"--build-arg", "VERSION=" + version.String(),
			"-t", containerImage,
			".",
		},
	}
}
