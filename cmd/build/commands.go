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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/cmd/build/buildinfo"
)

// the build subcommands, an empty one standing for the whole pipeline:
// applications, then ffmpeg, then the server that carries both.
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

// the container image is built for three architectures: the word after
// `container` names one, --all names all three, and a run naming neither
// builds the machine it stands on. linux-amd64 is the first of the three,
// and the architecture the Containerfile's guard names first.
const (
	containerTarget = "linux-amd64"

	// containerImage is the image's name, and containerLatestTag its
	// moving tag: the run tags the image with the resolved version and
	// with latest, and reports the versioned tag.
	containerImage     = "rdio-scanner"
	containerLatestTag = "latest"
)

// containerTargets are the words a container run accepts, in the order the
// release matrix names them. The image carries the ffmpeg compiled for the
// target it is built for, so a target outside these three has no image.
var containerTargets = []string{containerTarget, "linux-arm", "linux-arm64"}

// the container programs this build knows, podman preferred:
// the order here is the order a machine is asked in.
const (
	podmanProgram = "podman"
	dockerProgram = "docker"

	// containerProgramEnv names the container program the operator
	// wants, which is how a machine with both is made use the other.
	containerProgramEnv = "RDIO_CONTAINER_PROGRAM"
)

// resolveContainerProgram returns the container program to run, the
// operator's named one when given, otherwise podman then docker;
// every refusal names both programs it looked for.
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

// theContainerProgram names the program a run speaks to, how it runs
// one command in a container leaving no files behind in the operator's
// tree, and the questions it asks that program in the words that
// program answers.
type theContainerProgram struct {
	name string
}

func (p theContainerProgram) run() []string {
	if p.name == dockerProgram {
		return []string{"run", "--rm", "--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())}
	}

	return []string{"run", "--rm", "--userns=host"}
}

// coresAsk is the question a program answers with the number of cores
// the machine its containers run on has. The form differs per program
// because each templates its own info payload: docker names its count
// NCPU and has no Host, podman names it Host.CPUs and has no NCPU, and
// each rejects the other's field by name rather than giving a count.
func (p theContainerProgram) coresAsk() []string {
	if p.name == dockerProgram {
		return []string{"info", "--format", "{{.NCPU}}"}
	}

	return []string{"info", "--format", "{{.Host.CPUs}}"}
}

// subArchive names the release's one non-command step, so the log
// reports writing the archive as it reports compiling.
const subArchive = "archive"

// options is what the command line asked for, once it is known to be a command line this tool
// honours.
type options struct {
	sub    string
	pre    string
	output string

	// target is the platform the ffmpeg phase cross-compiles for;
	// empty means the platform this tool runs on.
	target string

	// all says the operator asked for the whole release matrix;
	// a release run without it builds only the platform it is
	// standing on, which is what a developer means by a build to try.
	all bool

	// allowDirty is the release run's choice to release a tree with
	// uncommitted work in it, and the run says so whenever it is used.
	allowDirty bool

	// installClients is decided by the tool from the tree, not the
	// operator: a fresh clone has no esbuild to build the clients with.
	installClients bool

	// outputNamed says the operator named a server binary path; only
	// single-server runs are asked, and a release run decides a path
	// again for each target it writes.
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

// buildsServerBinary reports the runs that compile a server binary:
// the whole pipeline, the server subcommand, and the matrix, which
// compiles one binary per target.
func (o options) buildsServerBinary() bool {
	return o.buildsServer() || o.sub == subDist
}

// planState is what the tree says about what is already built; a
// phase with nothing to do does not run, as the ffmpeg cache shows.
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

// parseArgs reads the command line and refuses what this tool
// cannot honour: flags arrive on either side of their command, and
// the default output path waits for the whole line to be read.
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

			// a prerelease is the version a build stamps into what it compiles,
			// and a test run compiles nothing and runs the same suites whatever
			// the version says; the command can arrive on either side of the
			// flag, so the check looks ahead as well as behind.
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
			// --all asks for a whole list, and only two runs have one: the release run's
			// matrix of platforms, and the container run's three architectures. It is a
			// deliberate request, so the flag belongs to those two runs and to nothing else.
			//
			// The flag and the command arrive in either order, so the look-ahead names the
			// command the operator typed. Only a peeked `container` lets the flag through:
			// `--all dist` stays refused although `dist --all` is the release run that asks
			// for the matrix.
			who := o.sub

			if who == "" && i+1 < len(args) && isKnownCommand(args[i+1]) {
				who = args[i+1]
			}

			forContainer := o.sub == subContainer || who == subContainer

			if o.sub != subDist && !forContainer {
				if who == "" {
					return options{}, fmt.Errorf("--all is the release matrix, and a run that names no command builds this machine's whole tree: go run ./cmd/build dist --all builds every platform this project releases")
				}

				return options{}, fmt.Errorf("--all is the release matrix, and %s is not it: go run ./cmd/build dist --all builds every platform this project releases", who)
			}

			// --all names every target of the run's list and a target word names one, and these
			// are not one request; the check looks forward too, so the refusal names the command
			// line the operator typed. A target a container run has no image for is refused for
			// that target, whichever side of the flag it sits on.
			if forContainer {
				word := o.target

				if word == "" && i+1 < len(args) && isTarget(args[i+1]) {
					word = theReleaseWordTheRunBuildsFor(args[i+1])
				}

				if word != "" && !isContainerTarget(word) {
					return options{}, fmt.Errorf("a container image is built for %s, so %s is not a target a container run builds for", strings.Join(containerTargets, ", "), word)
				}

				if word != "" {
					return options{}, fmt.Errorf("--all builds every architecture a container image is built for, and %s is one of them: name one target or --all, not both", word)
				}

				o.all = true

				continue
			}

			if o.target != "" {
				return options{}, fmt.Errorf("--all builds every platform this project releases, and %s is one of them: name one target or --all, not both", o.target)
			}

			if i+1 < len(args) && isTarget(args[i+1]) {
				return options{}, fmt.Errorf("--all builds every platform this project releases, and %s is one of them: name one target or --all, not both", args[i+1])
			}

			o.all = true

		case "--allow-dirty":
			// the release is built from a commit, so releasing an uncommitted
			// tree is a decision about a release: the flag belongs to the release
			// run alone, the command can arrive on either side of the flag, and
			// the refusal names the command line the operator typed.
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

			// a clean run builds nothing, and a test run builds nothing and runs
			// the whole tree's suites, so a target after either is a stray word;
			// a container run builds one of three architectures, the word after it
			// names which one, and a run naming no target builds the machine it
			// stands on.
			if arg == subClean || arg == subContainer || arg == subTest {
				if i+1 < len(args) && isTarget(args[i+1]) {
					if arg == subClean {
						return options{}, fmt.Errorf("a clean run builds nothing, so %s is not a target it is asked for", args[i+1])
					}

					if arg == subTest {
						return options{}, fmt.Errorf("a test run builds nothing, so %s is not a target it is asked for: go run ./cmd/build test runs this project's suites on this machine", args[i+1])
					}

					word := theReleaseWordTheRunBuildsFor(args[i+1])

					if !isContainerTarget(word) {
						return options{}, fmt.Errorf("a container image is built for %s, so %s is not a target a container run builds for", strings.Join(containerTargets, ", "), word)
					}

					// --all has already asked for every architecture the image is built
					// for, so the word beside it is a second request.
					if o.all {
						return options{}, fmt.Errorf("--all builds every architecture a container image is built for, and %s is one of them: name one target or --all, not both", word)
					}

					i++

					o.target = word

					continue
				}

				continue
			}

			// the ffmpeg phase and the release matrix are the runs that build for
			// a named platform, and the platform must be the word right after,
			// so a target left at the line's end is refused, not built quietly.
			if (arg == subFFmpeg || arg == subDist) && i+1 < len(args) && isTarget(args[i+1]) {
				i++

				// the typed word becomes the release word once, here, so the
				// manifest lookup, the archive name, and every message the run
				// prints share the one name.
				o.target = theReleaseWordTheRunBuildsFor(args[i])

				// the manifest answers for any platform, but the matrix is a
				// product decision: a target outside it is a platform this
				// project does not release, and the run refuses it.
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

	// -o names where a run writes the server binary it compiles, and
	// every other run compiles none, so the path is one nothing is
	// written to; this check waits for the whole line because -o and its
	// command arrive in either order.
	if o.outputNamed && o.sub != "" && o.sub != subServer {
		return options{}, fmt.Errorf("-o is where a run writes the server binary it compiles, and %s compiles none: go run ./cmd/build -o <path> and go run ./cmd/build server -o <path> are the runs that write one", o.sub)
	}

	if !o.outputNamed {
		o.output = defaultOutputPath(o.target)
	}

	return o, nil
}

// isKnownCommand reports whether a word names a command, which the
// look-ahead checks need so a refusal names the typed command.
// knownCommands is every subcommand, pinned to the compile guide
var knownCommands = []string{
	subClient,
	subScanner,
	subAdmin,
	subFFmpeg,
	subServer,
	subDist,
	subContainer,
	subClean,
	subTest,
	subVersion,
}

func isKnownCommand(word string) bool {
	for _, name := range knownCommands {
		if word == name {
			return true
		}
	}

	return false
}

// isTarget reports whether a word names a buildable platform: a Go
// operating system and architecture joined as go names them. A word
// outside the release matrix still fits: each phase says what it can
// do for that platform.
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

// isContainerTarget reports whether a release word names one of the three
// architectures a container image is built for.
func isContainerTarget(word string) bool {
	for _, target := range containerTargets {
		if word == target {
			return true
		}
	}

	return false
}

// step is one external command: a directory, a program, its
// arguments, and its extra environment; composed here, executed
// there, so a test can read a command line.
type step struct {
	label string

	// result is what the step leaves behind, said once it ran.
	result string

	dir  string
	name string
	args []string
	env  []string
}

// serverStep composes the server binary for the run's target, last
// because it embeds both applications and that target's ffmpeg;
// the version arrives through the linker, the target through goos,
// and source paths are trimmed to the module they belong to, so the
// binary names no folder of the machine that compiled it.
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
			"-trimpath",
			"-ldflags", "-s -w -X main.Version=" + version.String(),
			"-o", o.output,
			"./cmd/rdio-scanner",
		},
		env: []string{"CGO_ENABLED=0", "GOOS=" + serverGoos(target), "GOARCH=" + goarch},
	}
}

// testSteps are the two suites this project verifies with: the Go
// suite first, because the release is gated on it, then the client
// suite; both are composed as data, so a test can read what each
// runs and in what directory.
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

// imageTagFor spells the version for an image tag's grammar, which
// has letters, digits, dots, dashes and underscores but no plus, so
// the sign introducing git metadata is written as a dash.
func imageTagFor(version string) string {
	return strings.Replace(version, "+", "-", 1)
}

// containerPlatformFor spells a container target as the platform the
// container program builds for: a release word joins a Go operating
// system and architecture with a dash, and a platform names the same
// pair with a slash. linux-arm is ARMv7 with hard-float floating point,
// the variant the ffmpeg an arm image embeds is compiled for, so the
// platform named for it carries that variant.
func containerPlatformFor(target string) string {
	platform := strings.ReplaceAll(target, "-", "/")

	if target == "linux-arm" {
		return platform + "/v7"
	}

	return platform
}

// manifestListSuffix marks the manifest list a container run assembles as its own work: no
// published tag and no architecture suffix carries it, so the list can never be mistaken for
// an image a host pulls or runs.
const manifestListSuffix = "-index"

// containerManifestListFor spells the one manifest list a run assembling the three
// architectures builds its images into. It is named from the same tag word the images use, so
// the list and the images it holds are recognisably one run's output, and it carries no
// architecture because it names all three.
func containerManifestListFor(version buildinfo.Version) string {
	return containerImage + ":" + imageTagFor(version.String()) + manifestListSuffix
}

// buildDir is the directory every build output the operator is meant to delete lives in, and
// indexFileSuffix names the image index file as what it is: an OCI layout archive, which no
// container program runs as an image.
const (
	buildDir        = ".build"
	indexFileSuffix = ".oci"
)

// containerIndexFileFor spells that file. It is build output, so it lands under .build/ beside
// the ffmpeg the run compiled, and it is named from the version the run resolved so an operator
// publishing a release knows which run wrote it.
func containerIndexFileFor(version buildinfo.Version) string {
	return filepath.Join(buildDir, containerImage+"-"+imageTagFor(version.String())+indexFileSuffix)
}

// containerManifestClearStep drops the run's own manifest list name from the container program's
// store. The program refuses a list name an image or an earlier list already holds, and the list
// is working state for one run: it is cleared before the three builds name it, so a leftover from
// an interrupted run cannot make this one fail, and again once its file is written, so the machine
// keeps the images and not the list.
func containerManifestClearStep(repo string, version buildinfo.Version, program string) step {
	return step{
		label:  subContainer,
		result: "",
		dir:    repo,
		name:   program,
		args: []string{
			"manifest",
			"rm",
			containerManifestListFor(version),
		},
	}
}

// containerManifestExportStep asks podman to write the manifest list the three image builds filled
// into that file: the layout is OCI so a registry or a copy command can read it without this
// machine's container program, every architecture in the list travels with it, and the destination
// is a file rather than a registry reference.
func containerManifestExportStep(repo string, version buildinfo.Version, program string) step {
	file := containerIndexFileFor(version)

	return step{
		label:  subContainer,
		result: file,
		dir:    repo,
		name:   program,
		args: []string{
			"manifest",
			"push",
			"--format=oci",
			"--all",
			containerManifestListFor(version),
			"oci-archive:" + file,
		},
	}
}

// containerStep asks the container program to build the image for one
// architecture: the architecture is named once, by the platform the
// program builds for, and the version and the build time arrive as build
// arguments. The image build compiles the server itself, for the
// architecture the run resolves. The image is tagged with the version and
// with latest, each named for that architecture, and the linux-amd64 run
// also writes the unarch'd pair the published image has always carried.
// A run that builds all three architectures hands each image to the one
// manifest list it assembles, which is how podman carries the three
// architectures under one name.
func containerStep(o options, repo string, version buildinfo.Version, created time.Time, program string) step {
	// the run's target list is already resolved to real architecture words,
	// so the step builds for the target it is given and guesses nothing.
	target := o.target

	versionedTag := containerImage + ":" + imageTagFor(version.String())
	tags := []string{
		versionedTag + "-" + target,
		containerImage + ":" + containerLatestTag + "-" + target,
	}

	if target == containerTarget {
		tags = append(tags, versionedTag, containerImage+":"+containerLatestTag)
	}

	args := []string{
		"build",
		"--pull",
		"--platform=" + containerPlatformFor(target),
		"-f", "Containerfile",
		"--build-arg", "VERSION=" + version.String(),
		"--build-arg", "BUILD_TIME=" + created.UTC().Format(time.RFC3339),
	}

	for _, tag := range tags {
		args = append(args, "-t", tag)
	}

	// podman assembles the multi-architecture image from a manifest list it fills as it builds:
	// every leg of an all-architectures run adds its image to the same list, and the run reads
	// that list back into the index file it writes. A single-target run assembles nothing.
	if o.all && program == podmanProgram {
		args = append(args, "--manifest="+containerManifestListFor(version))
	}

	args = append(args, ".")

	return step{
		label:  subContainer,
		result: tags[0],
		dir:    repo,
		name:   program,
		args:   args,
	}
}
