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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/cmd/build/buildinfo"
)

// The subcommands the tool knows. An empty one is the whole pipeline: both applications, then the
// ffmpeg the server carries, then the server that carries both.
const (
	subClient    = "client"
	subScanner   = "scanner"
	subAdmin     = "admin"
	subFFmpeg    = "ffmpeg"
	subServer    = "server"
	subVersion   = "version"
	subDist      = "dist"
	subClean     = "clean"
	subContainer = "container"
	subTest      = "test"
)

// The container image is linux-amd64, because that is the architecture the Containerfile's guard checks
// for, and the name the image is tagged with.
const (
	containerTarget = "linux-amd64"

	// containerImage is the image's name. The tags are what say which release it is: the run tags the
	// image with the version it resolved and with `latest`, and the versioned tag is the one the run
	// reports, because that is the tag that says which release the machine is standing on.
	containerImage     = "rdio-scanner"
	containerLatestTag = "latest"
)

// The container programs this build knows, and the name the operator gives the one they want.
//
// podman is the program the build was developed against and the one the compile guide names first.
// docker is the program a machine with no podman still has. The order here is the order a machine is
// asked in.
const (
	podmanProgram = "podman"
	dockerProgram = "docker"

	// containerProgramEnv names the program the operator wants. It is how a machine that has both
	// programs is told to use the one it does not prefer, which is the only way the docker path is
	// exercised on a machine that has podman.
	containerProgramEnv = "RDIO_CONTAINER_PROGRAM"
)

// resolveContainerProgram asks the machine which container program it has, in the order the build
// prefers, and honours the name the operator gave.
//
// A machine with neither is refused, and the refusal names both programs it looked for: a message that
// names one program tells the operator to install a program they may already have. A name the operator
// gave that is not a container program is refused, and a name the machine does not have is refused: a
// run that quietly fell back to the program the operator said not to use would be a run doing something
// the operator ruled out. A variable set to nothing says nothing.
func resolveContainerProgram(lookup func(string) (string, error), env func(string) (string, bool)) (string, error) {
	named, ok := env(containerProgramEnv)

	if ok && named != "" {
		if named != podmanProgram && named != dockerProgram {
			return "", fmt.Errorf("%s is %q, which is not a container program: name %s or %s",
				containerProgramEnv, named, podmanProgram, dockerProgram)
		}

		if where, err := lookup(named); err != nil || where == "" {
			return "", fmt.Errorf("%s names %s, which this machine does not have: use %s=%s to name the program this machine has",
				containerProgramEnv, named, containerProgramEnv, dockerProgram)
		}

		return named, nil
	}

	for _, name := range []string{podmanProgram, dockerProgram} {
		if where, err := lookup(name); err == nil && where != "" {
			return name, nil
		}
	}

	return "", fmt.Errorf("the build needs a container program: it looked for %s and %s and this machine has neither",
		podmanProgram, dockerProgram)
}

// theContainerProgram is the program a run speaks to, and the way it says "run this one command in a
// container and leave nothing behind".
//
// The files the container writes in the operator's tree belong to the operator who ran the build: the
// ffmpeg phase compiles into a directory under the tree, and the build has to read, overwrite, and throw
// away what the container wrote there. podman is told to keep the host's user namespace, which is what
// makes the artifacts come back as the operator's own. docker has no user namespace to keep — asked for
// one it runs as root and the artifacts come back owned by root — so it is told the operator's own
// identity numbers, which is the same guarantee stated in the vocabulary docker has.
type theContainerProgram struct {
	name string
}

func (p theContainerProgram) run() []string {
	if p.name == dockerProgram {
		return []string{"run", "--rm", "--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())}
	}

	return []string{"run", "--rm", "--userns=host"}
}

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

	// all says the operator asked for the whole release matrix. A release run that does not ask for it
	// builds the platform it is standing on: the archive a developer builds to try what they built is
	// the one their own machine runs, and the eight-target matrix is a release, which is a deliberate act.
	all bool

	// allowDirty is the release run's override: it says the tree is being released with uncommitted
	// work in it, deliberately, and the run says so on every line the override is used for.
	allowDirty bool

	// installClients is decided by the tool looking at the tree, not by the operator: a first run
	// on a fresh clone has no esbuild to build with.
	installClients bool

	// outputNamed says the operator said where the server binary goes. Only the runs that compile one
	// server binary are asked that question, so a release run writes one binary per target and decides
	// the path it writes to again for each of them.
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
// not run, which is the rule the ffmpeg cache follows: the compiled binaries are reused and nothing is
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
// target it is building for, and a target named after `-o` still decides it. The flag itself is
// accepted for the runs that write a server binary, and for those alone, which is a check made after
// the line is read because the command it belongs to arrives on either side of it.
//
// A release run writes archives, and an archive carries the binary the run wrote to, so a release run
// that honoured a named path would write every archive from one binary and read each one from wherever
// that path happened to point.
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

			// A prerelease name is the version a build stamps into what it compiles. A test run compiles
			// nothing, so the name has nothing to stamp, and the suites are the same suites whatever the
			// version says. The command arrives on either side of the flag, so the check looks ahead as
			// well as behind.
			if o.sub == subTest || (i+1 < len(args) && args[i+1] == subTest) {
				return options{}, fmt.Errorf("--pre %s is the version a build stamps into what it compiles, and a test run compiles nothing: go run ./cmd/build test runs this project's suites", args[i])
			}

		case "-o", "--output":
			if i+1 >= len(args) {
				return options{}, fmt.Errorf("%s needs a path to write the server binary to", arg)
			}

			i++

			o.output = args[i]
			o.outputNamed = true

		case "--all":
			// The whole matrix is a release, and a release is a deliberate act, so the flag that asks
			// for it belongs to the release run and to nothing else: a client run, a clean run, and a
			// container image are all one machine's business, and have no matrix to walk.
			if o.sub != subDist {
				// The flag and the command arrive in either order, so the refusal names the command the
				// operator typed and not the order the words happened to arrive in.
				who := o.sub

				if who == "" && i+1 < len(args) && isKnownCommand(args[i+1]) {
					who = args[i+1]
				}

				if who == "" {
					return options{}, fmt.Errorf("--all is the release matrix, and a run that names no command builds this machine's whole tree: go run ./cmd/build dist --all builds every platform this project releases")
				}

				return options{}, fmt.Errorf("--all is the release matrix, and %s is not it: go run ./cmd/build dist --all builds every platform this project releases", who)
			}

			// The flag names every platform and a target names one, and the two are not one request.
			// The check is made looking forward as well as behind, so that the refusal names the
			// command line the operator typed and not the order the words happened to arrive in.
			if o.target != "" {
				return options{}, fmt.Errorf("--all builds every platform this project releases, and %s is one of them: name one target or --all, not both", o.target)
			}

			if i+1 < len(args) && isTarget(args[i+1]) {
				return options{}, fmt.Errorf("--all builds every platform this project releases, and %s is one of them: name one target or --all, not both", args[i+1])
			}

			o.all = true

		case "--allow-dirty":
			// The release is built from a commit, and this flag says the tree is being released with
			// work in it that is not committed. That is a decision about a release, so the flag belongs
			// to the release run: a client build, a container image, and a clean run are one machine's
			// business and release nothing. The command the flag belongs to can arrive on either side of
			// it, so the check looks ahead as well as behind, and the refusal names the command line the
			// operator typed and not the order the words happened to arrive in.
			next := ""
			if i+1 < len(args) {
				next = args[i+1]
			}

			if o.sub == subDist || next == subDist {
				o.allowDirty = true

				continue
			}

			who := o.sub
			if who == "" && isKnownCommand(next) {
				who = next
			}

			if who != "" {
				return options{}, fmt.Errorf("--allow-dirty is a release run saying it releases a tree that is not committed, and %s releases nothing: go run ./cmd/build dist --allow-dirty releases the tree as it stands", who)
			}

			return options{}, fmt.Errorf("--allow-dirty is a release run saying it releases a tree that is not committed, and a run that names no command builds this machine's whole tree: go run ./cmd/build dist --allow-dirty releases the tree as it stands")

		case subClient, subScanner, subAdmin, subFFmpeg, subServer, subVersion, subDist, subClean, subContainer, subTest:
			if o.sub != "" {
				return options{}, fmt.Errorf("%s and %s are not one command: name one thing to build", o.sub, arg)
			}

			o.sub = arg

			// A clean run builds nothing, so a target after it is a word lying at the end of a command
			// line. The container image is one architecture: the Containerfile's guard checks for the
			// ffmpeg compiled for linux-amd64, and a target named after it is a run that would build an
			// ffmpeg the image never embeds. A test run builds nothing at all: it runs the suites, and
			// the suites are the whole tree's, not one platform's.
			if arg == subClean || arg == subContainer || arg == subTest {
				if i+1 < len(args) && isTarget(args[i+1]) {
					if arg == subClean {
						return options{}, fmt.Errorf("a clean run builds nothing, so %s is not a target it is asked for", args[i+1])
					}

					if arg == subTest {
						return options{}, fmt.Errorf("a test run builds nothing, so %s is not a target it is asked for: go run ./cmd/build test runs this project's suites on this machine", args[i+1])
					}

					return options{}, fmt.Errorf("the container image is %s, so %s is not a target a container run builds for", containerTarget, args[i+1])
				}

				if arg == subContainer {
					o.target = containerTarget
				}

				continue
			}

			// The ffmpeg phase and the release matrix are the two things this tool works on for a
			// platform the operator names, because that is how the release matrix cross-builds from one
			// machine. The platform has to be the word right after it, so that a target left lying at the
			// end of a command line is refused instead of quietly building something else.
			if (arg == subFFmpeg || arg == subDist) && i+1 < len(args) && isTarget(args[i+1]) {
				i++

				// The word the operator typed is resolved to the release word here, once, so the manifest
				// lookup, the archive's name, and every message the run prints all use the one name the
				// release is built under.
				o.target = theReleaseWordTheRunBuildsFor(args[i])

				// The ffmpeg phase compiles for any platform the manifest can answer for, and says so
				// when it has no record. The matrix is a product decision: a target outside it is a
				// platform this project does not release, and building one is not what was asked for.
				if arg == subDist {
					if _, found := targetFor(o.target); !found {
						return options{}, fmt.Errorf("%s is not a platform this project releases, so no archive is built for it", o.target)
					}
				}
			}

		default:
			return options{}, fmt.Errorf("%s is not something this tool builds or a flag it knows", arg)
		}
	}

	// `-o` is where a run writes the server binary it compiles. The whole-tree run and the server run
	// compile one, and every other run compiles none, so the path it names is a path nothing is written
	// to. The check is made here, after the line is read, because `-o` and the command it belongs to
	// arrive in either order.
	if o.outputNamed && o.sub != "" && o.sub != subServer {
		return options{}, fmt.Errorf("-o is where a run writes the server binary it compiles, and %s compiles none: go run ./cmd/build -o <path> and go run ./cmd/build server -o <path> are the runs that write one", o.sub)
	}

	if !o.outputNamed {
		o.output = defaultOutputPath(o.target)
	}

	return o, nil
}

// isKnownCommand reports whether a word names one of the tool's commands. The checks that look ahead
// in the command line need to know which words name a command, so that a refusal names the command the
// operator typed and not the order the words happened to arrive in.
func isKnownCommand(word string) bool {
	switch word {
	case subClient, subScanner, subAdmin, subFFmpeg, subServer, subVersion, subDist, subClean, subContainer, subTest:
		return true
	}

	return false
}

// isTarget reports whether a word names a platform this tool can be asked to build for: a Go operating
// system and a Go architecture, joined the way go names them. A word that does not fit is a mistake in
// the command line, and refusing it here is cheaper than a build that goes the wrong way.
//
// A word the release matrix has no place for still fits this grammar, and the phase asked to build for
// it says what it can do for that platform: the ffmpeg phase names the target it compiles nothing for,
// and the release run refuses it as a platform this project does not release.
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
//
// cgo is turned off, so every target is compiled the same way and no target is linked against the C
// library of the machine that ran the release. Every dependency of the server is pure Go, so nothing
// is lost by turning it off, and the binaries the server carries are static for the same reason the
// recipe that compiles them is written static.
//
// The debug information is stripped: it is bytes for a debugger, a quarter of the program's size, and
// the ffmpeg binaries the same tool produces are stripped by the compile that makes them.
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
			"-ldflags", "-s -w -X main.Version=" + version.String(),
			"-o", o.output,
			"./cmd/rdio-scanner",
		},
		env: []string{"CGO_ENABLED=0", "GOOS=" + serverGoos(target), "GOARCH=" + goarch},
	}
}

// testSteps are the two suites this project's automated verification is made of: the Go suite over the
// whole repository, and the client suite the two applications' sources are tested by. They are composed
// here, like every other command line the tool runs, so what each suite is told to run, and in what
// directory, is something a test can read.
//
// The Go suite comes first: it is the suite the release is gated on, and a run whose Go suite fails has
// nothing to gain from spending the client suite's time on top of it.
func testSteps(o options, repo string) []step {
	return []step{
		{
			label:  subTest,
			result: "go test ./...",
			dir:    repo,
			name:   "go",
			args:   []string{"test", "./..."},
		},
		{
			label:  subTest,
			result: "npm test",
			dir:    filepath.Join(repo, presentationDir),
			name:   "npm",
			args:   []string{"test"},
		},
	}
}

// imageTagFor is the version spelled the way an image tag's grammar allows: a tag carries letters,
// digits, dots, dashes and underscores, and has no `+`, so the sign that introduces the git metadata is
// written as a dash. The image's own version label carries the version itself; the tag is the same
// release spelled for the grammar a registry checks, and a reader holding the version can spell it.
func imageTagFor(version string) string {
	return strings.Replace(version, "+", "-", 1)
}

// containerStep asks the container program to build the image. The version reaches it through the build
// argument the Containerfile reads into the linker flag and into the image's own labels: an image built
// without it reports the development fallback, which is the sign that nobody said what the image is. The
// server binary is compiled inside the image, so the run that prepares it builds no binary of its own.
//
// The image is tagged with the version the run resolved and with `latest`. The versioned tag is the one
// the run reports, because a machine that says which machine it is on has said less than the machine
// that says which release it is standing on.
//
// The moment the image was made is a fact the run knows and the Containerfile cannot: it arrives as the
// build argument the created label reads.
//
// The file the image is built from is named, because docker looks for Dockerfile and does not find
// Containerfile: a build that names its file has one shape for both programs.
func containerStep(o options, repo string, version buildinfo.Version, created time.Time, program string) step {
	tag := containerImage + ":" + imageTagFor(version.String())

	return step{
		label:  subContainer,
		result: tag,
		dir:    repo,
		name:   program,
		args: []string{
			"build",
			"--pull",
			"-f", "Containerfile",
			"--build-arg", "VERSION=" + version.String(),
			"--build-arg", "BUILD_TIME=" + created.UTC().Format(time.RFC3339),
			"-t", tag,
			"-t", containerImage + ":" + containerLatestTag,
			".",
		},
	}
}
