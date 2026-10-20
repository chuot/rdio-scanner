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
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// runtimeTarget is the target this build is running on, which is the target an operator who named none
// is building for. It is named by the release target the machine belongs to, because the manifest, the
// output path, and the messages an operator reads all name the target by the name the release uses: the
// operator on the Mac that the macOS ffmpeg binaries are built on is asked about macos-arm64, the
// target the manifest
// records, and not about the name go builds a macOS binary for.
func runtimeTarget() string {
	return releaseTargetWordFor(runtime.GOOS, runtime.GOARCH)
}

// ffmpegState is what the tree already has, read before anything is decided.
type ffmpegState struct {
	// program is the container program this run speaks to, and it is empty when the machine has
	// neither: a plan made from an empty program is refused before a source is fetched.
	program        string
	sourceIsCached bool
	bundleIsCached bool

	// nativeTools is the host's own build tools, resolved: the program name mapped to where it was found,
	// and no entry for a program that is not there. Only a compile that runs on the target's own platform
	// uses it, and it is read before the plan is made so that a host missing a compiler is told which one
	// before a minute of work finds it out.
	nativeTools map[string]string
}

// host is the platform the machine running the build is on, named the way the release names a platform:
// the ffmpeg binaries a supplied target carries run on one platform alone, and the question the build asks of it is
// whether the machine standing in front of the build is that platform.
type host struct {
	goos   string
	goarch string
}

// thisMachine is the platform the build is running on.
func thisMachine() host {
	return host{goos: runtime.GOOS, goarch: runtime.GOARCH}
}

// isTheHostOf says whether this machine is the platform one target's ffmpeg binaries run on.
//
// It is the rule that decides what a supplied target means for a run. The ffmpeg binaries are compiled
// platform's own compiler, against the platform's own libraries, and a compile of it written on any other
// machine is a binary that platform refuses to run: the build that is standing on the target's platform
// compiles them, and every other machine adopts what was dropped or skips the target.
func (h host) isTheHostOf(target string) bool {
	tr, found := targetFor(target)

	return found && h.goos == tr.goos && h.goarch == tr.goarch
}

// canCompile says whether this machine can produce one target's ffmpeg binaries: the operating system is the
// target's, and the architecture is the target's, or the other architecture macOS ships.
//
// Apple ships one toolchain that produces both macOS architectures: the Command Line Tools' clang takes
// -arch x86_64 on an Apple Silicon Mac and -arch arm64 on an Intel Mac, the macOS SDK carries the
// libraries of both, and codesign signs a program of either architecture on either machine. A FreeBSD
// ffmpeg binary is statically linked against FreeBSD's own libc, which is the whole point of compiling it
// there, and a host of
// another operating system does not have it.
func (h host) canCompile(target string) bool {
	tr, found := targetFor(target)
	if !found || h.goos != tr.goos {
		return false
	}

	if h.goarch == tr.goarch {
		return true
	}

	return h.goos == "darwin" && aMacOSReleaseTarget(h.goarch) && aMacOSReleaseTarget(tr.goarch)
}

// aMacOSReleaseTarget reports whether the release matrix ships a macOS platform for this architecture,
// which is the list the two architectures a Mac can be are read from.
func aMacOSReleaseTarget(goarch string) bool {
	for _, tr := range releaseTargets {
		if tr.goos == "darwin" && tr.goarch == goarch {
			return true
		}
	}

	return false
}

// theFFmpegArchitectureOf is the architecture of a target, named the way ffmpeg names architectures:
// x86_64 and aarch64.
func theFFmpegArchitectureOf(target string) string {
	tr, found := targetFor(target)

	if found && tr.goarch == "arm64" {
		return "aarch64"
	}

	return "x86_64"
}

// theCompilerArchitectureTheHostMustProduce is the architecture the compiler has to be told to produce,
// named the way the macOS compiler names it, when the host running the compile is not of the target's
// architecture. An empty answer is the compile on the target's own host, where the compiler's own default
// is the target's architecture and naming it would be the build guessing at a toolchain's defaults.
func theCompilerArchitectureTheHostMustProduce(target string, machine host) string {
	tr, found := targetFor(target)
	if !found || machine.goarch == tr.goarch {
		return ""
	}

	if tr.goarch == "arm64" {
		return "arm64"
	}

	return "x86_64"
}

// ffmpegPaths are the places the ffmpeg phase keeps its pieces. Everything under the build directory
// is disposable: deleting it costs a rebuild and nothing else.
type ffmpegPaths struct {
	root    string
	sources string
	source  string
	context string
	scratch string
	cached  string
	bundle  string
}

func (p ffmpegPaths) cachedProgram(name string) string {
	return filepath.Join(p.cached, name)
}

// ffmpegSource is the release the build compiles from, and whether it still has to be fetched.
type ffmpegSource struct {
	Path  string
	URL   string
	Fetch bool
}

// ffmpegPlan is what the ffmpeg phase decided to do, decided completely before any of it runs.
type ffmpegPlan struct {
	Target string

	// Says is what the operator is told about this phase, and is empty when the ordinary thing is
	// happening.
	Says string

	// Steps are the external commands that produce the bundle, and are empty when nothing has to be
	// compiled: an uncovered target, a bundle that is already there, or the ffmpeg binaries adopted from what
	// was dropped for it.
	Steps []step

	// Skipped says this target has nothing to build and produces nothing: the ffmpeg binaries the manifest says are
	// supplied were not dropped. A release run skips it and ships the rest.
	Skipped bool

	// Native says this plan compiles the ffmpeg binaries on the machine running the build, because that
	// machine is the platform they run on. It is the answer to the operator who asked for the ffmpeg of the
	// machine they are standing on, and it is never a skip: the compile is what that run asked for.
	Native bool

	// Adopts says the plan is taking binaries a maintainer dropped in the target's bundle directory,
	// rather than compiling them. The run that adopts them runs them first, through the same gate a
	// compile runs its own binaries through.
	Adopts bool

	Source ffmpegSource
	Paths  ffmpegPaths

	// Image is the container the target is compiled in, and Runner is the container its compiled
	// program is checked in, which is the same one unless the target needs an operating system the
	// build container does not have.
	Image  string
	Runner string
}

// ffmpegPathsFor is where one target's pieces are kept. The cache key is in the path, so a recipe that
// changed cannot be mistaken for the one it replaced.
func ffmpegPathsFor(repo string, manifest bundle.Manifest, target string) ffmpegPaths {
	root := filepath.Join(repo, ".build", "ffmpeg")
	source := sourceName(manifest)

	return ffmpegPaths{
		root:    root,
		sources: filepath.Join(root, "src"),
		source:  filepath.Join(root, "src", source),
		context: filepath.Join(root, "context"),
		scratch: filepath.Join(root, "scratch", target),
		cached:  filepath.Join(root, manifest.FFmpeg, cacheKey(manifest, target), target),
		bundle:  filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", target),
	}
}

// sourceName is the tarball the release is published as, and sourceDir is the directory it extracts
// to. They are the same name with the suffix taken off, which is what ffmpeg's own releases are.
func sourceName(manifest bundle.Manifest) string {
	return "ffmpeg-" + manifest.FFmpeg + ".tar.xz"
}

func sourceDir(manifest bundle.Manifest) string {
	return strings.TrimSuffix(sourceName(manifest), ".tar.xz")
}

// ffmpegScratch is what the ffmpeg phase writes and no later build reads: the configured source trees it
// compiles in, the tarball it extracted them from, the container context it built in, and the working
// copy of the binaries it ran. The cache beside them is what every later build reads, and a clean run
// leaves it standing: the scratch is the bulk that is worth a rebuild and nothing else, and the cache is
// the part that turns a rebuild into a copy.
func ffmpegScratch(repo string) []string {
	root := filepath.Join(repo, ".build", "ffmpeg")

	names := []string{"scratch", "src", "context", "run"}

	paths := make([]string, 0, len(names))

	for _, name := range names {
		paths = append(paths, filepath.Join(root, name))
	}

	return paths
}

// pruneFFMpegCache removes the cache entries the manifest the build reads cannot produce. The cache key
// is a digest of the release, the pinned source, the target, the recipe, the toolchain, and the check
// environment, so an entry whose key no target the manifest records computes is an entry the manifest as
// it is now cannot ask for, and removing it costs nothing a build would have reused. An age rule guesses
// about an entry a build is about to read, and the cost of a wrong guess is a full compile of a target.
//
// The cache is kept under the directory named for the release the manifest pins. A directory under the
// cache root named for another release is the whole cache of a manifest that no longer exists, and goes
// with it.
func pruneFFMpegCache(repo string) ([]string, error) {
	manifest := bundle.ReadManifest()

	root := filepath.Join(repo, ".build", "ffmpeg")

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("the build cannot read %s: %w", root, err)
	}

	// The scratch is the clean run's own removal, named by itself, and is not a cache entry.
	scratch := map[string]bool{}

	for _, path := range ffmpegScratch(repo) {
		scratch[path] = true
	}

	// The keys the manifest computes now, one per target it records.
	computed := map[string]bool{}

	for target := range manifest.Targets {
		computed[manifest.FFmpeg+string(filepath.Separator)+cacheKey(manifest, target)] = true
	}

	var removed []string

	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())

		if scratch[path] {
			continue
		}

		if entry.Name() != manifest.FFmpeg {
			removed = append(removed, path)

			continue
		}

		keys, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("the build cannot read %s: %w", path, err)
		}

		for _, key := range keys {
			if computed[entry.Name()+string(filepath.Separator)+key.Name()] {
				continue
			}

			removed = append(removed, filepath.Join(path, key.Name()))
		}
	}

	for _, path := range removed {
		if err := os.RemoveAll(path); err != nil {
			return nil, fmt.Errorf("the clean run cannot remove %s: %w", path, err)
		}
	}

	return removed, nil
}

// cacheKey is what has to be the same for a compiled ffmpeg to be reused: the release, the source this
// project pinned, the target, the configure recipe, and that target's toolchain and check environment.
//
// The source digest is in here and not only in the run that proves the binaries after compiling, because a
// cache hit skips that run: without it, a manifest whose pinned source was changed would quietly reuse
// what the old source compiled.
//
// A target whose ffmpeg binaries are compiled on its own platform has no container to key on, so its key
// adds the way it is produced, the make program that build names, and the flags the compile is told to
// pass. A compiled record keys on the container's inputs alone, so a manifest gaining these fields does
// not invalidate a warm cache.
func cacheKey(manifest bundle.Manifest, target string) string {
	record := manifest.Targets[target]

	parts := []string{
		manifest.FFmpeg,
		manifest.Source.URL,
		manifest.Source.SHA256,
		target,
		strings.Join(manifest.Recipe, "\n"),
		record.Container,
		strings.Join(record.Packages, " "),
		strings.Join(record.Cross, " "),
		strings.Join(record.CFlags, " "),
		strings.Join(record.LDFlags, " "),
		record.Conformance.Container,
		strings.Join(record.Conformance.Packages, " "),
		strings.Join(record.Conformance.Run, " "),
	}

	// The fields a target compiled on its own platform has are the way it is produced, the make program
	// that build ran, and the flags its compile passes. The binaries themselves are not part of the name
	// of the entry they land in: the cache is this machine's own tree, and what this machine compiled is
	// what sits in it.
	if record.Produced != "" {
		parts = append(parts, record.Produced, record.Make)
	}

	return sha256Of([]byte(strings.Join(parts, "\n")))
}

func sha256Of(body []byte) string {
	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}

// ffmpegTarget is the target the ffmpeg phase compiles for: the one the operator named, or the one this
// machine is.
func ffmpegTarget(o options) string {
	if o.target != "" {
		return o.target
	}

	return runtimeTarget()
}

// planFFmpeg decides what the ffmpeg phase does for one target.
//
// A target the manifest has no record for is not a failure: it is a platform this project does not
// compile an ffmpeg for at all, and the phase says so and builds nothing. A target the manifest does
// have a record for is a promise, and a build that cannot keep it says so instead of quietly producing a
// server that will do without.
//
// The machine is the platform the build is running on, and it is part of the decision because two of
// the targets are compiled on the platform their ffmpeg binaries run on: on that platform the build has
// the source, the digest, the recipe, the flags, and the run, and compiling them there is what the
// operator asked for. On every other platform they are something the build is handed.
func planFFmpeg(o options, repo string, manifest bundle.Manifest, state ffmpegState, machine host) (ffmpegPlan, error) {
	target := ffmpegTarget(o)

	record, covered := manifest.Targets[target]
	if !covered {
		return ffmpegPlan{Target: target, Says: "no ffmpeg is compiled for " + target}, nil
	}

	paths := ffmpegPathsFor(repo, manifest, target)

	if record.Produced == suppliedProduction {
		return planSuppliedFFmpeg(repo, manifest, target, record, paths, state, machine)
	}

	source := ffmpegSource{Path: paths.source, URL: manifest.Source.URL}

	if state.bundleIsCached {
		source.Fetch = false

		return ffmpegPlan{
			Target: target,
			Says:   "reusing the ffmpeg compiled for " + target,
			Source: source,
			Paths:  paths,
		}, nil
	}

	if state.program == "" {
		return ffmpegPlan{}, fmt.Errorf("compiling the ffmpeg for %s needs a container program: it looked for %s and %s and this machine has neither",
			target, podmanProgram, dockerProgram)
	}

	// Every step of this plan speaks to the program the run resolved, and enters a container the way
	// that program understands.
	program := theContainerProgram{name: state.program}

	key := cacheKey(manifest, target)
	image := "rdio-ffmpeg:" + target + "-" + key[:12]
	runner := image

	if record.Conformance.Container != "" {
		runner = "rdio-ffmpeg:" + target + "-check-" + key[:12]
	}

	source.Fetch = !state.sourceIsCached

	plan := ffmpegPlan{
		Target: target,
		Source: source,
		Paths:  paths,
		Image:  image,
		Runner: runner,
	}

	packages := record.Packages
	if record.Conformance.Container == "" {
		packages = append(append([]string{}, record.Packages...), record.Conformance.Packages...)
	}

	plan.Steps = append(plan.Steps, step{
		label:  "image",
		result: image,
		dir:    repo,
		name:   program.name,
		args:   buildImageArgs(image, record.Container, packages),
	})

	if runner != image {
		plan.Steps = append(plan.Steps, step{
			label:  "runner",
			result: runner,
			dir:    repo,
			name:   program.name,
			args:   buildImageArgs(runner, record.Conformance.Container, record.Conformance.Packages),
		})
	}

	// Each target starts from a pristine copy of the tarball: a tree that has already been configured
	// for another target fails later with ffbuild/config.mak is missing, which reads like a broken
	// recipe and is not one. The container does the throwing away because it is the container that owns
	// what is left in there.
	plan.Steps = append(plan.Steps, step{
		label:  "clean",
		result: "/work",
		dir:    repo,
		name:   program.name,
		args: append(program.run(),
			"-v", paths.scratch+":/work",
			image,
			"find", "/work", "-mindepth", "1", "-delete",
		),
	})

	plan.Steps = append(plan.Steps, step{
		label:  "unpack",
		result: "/work/" + sourceDir(manifest),
		dir:    repo,
		name:   program.name,
		args: append(program.run(),
			"-v", paths.sources+":/src:ro",
			"-v", paths.scratch+":/work",
			image,
			"tar", "-xJf", "/src/"+sourceName(manifest), "-C", "/work",
		),
	})

	plan.Steps = append(plan.Steps, configureStep(repo, paths, image, manifest, record, program))

	plan.Steps = append(plan.Steps, step{
		label:  "make",
		result: "ffmpeg and ffprobe for " + target,
		dir:    repo,
		name:   program.name,
		args: append(program.run(),
			"-v", paths.scratch+":/work",
			"-w", "/work/"+sourceDir(manifest),
			image,
			"make", "-j", strconv.Itoa(runtime.NumCPU()),
		),
	})

	// The tree the container compiled in belongs to whatever uid the container was given, which is not
	// a uid this build can name: rootless podman maps it into a subuid range, and the ownership ffmpeg's
	// own tarball carries lands somewhere else again. Chasing the uid is a dead end — the numbers differ
	// between rootless and rootful podman, and a wrong one leaves a tree the next build cannot even
	// open. Making the tree readable, writable and traversable by everybody in it is what the build
	// actually needs, and it does not depend on which uid turned out to be which.
	plan.Steps = append(plan.Steps, step{
		label:  "chmod",
		result: paths.scratch,
		dir:    repo,
		name:   program.name,
		args: append(program.run(),
			"-v", paths.scratch+":/work",
			image,
			"chmod", "-R", "a+rwX", "/work",
		),
	})

	return plan, nil
}

// suppliedProduction is the manifest's way of saying that this build compiles the ffmpeg binaries for a
// target where
// the target runs, and not in a container: the machine that is the target compiles it with its own
// toolchain, and every other machine adopts the binaries that host produced — the ones this build
// compiled there, or the ones the maintainer built by hand and dropped for it to adopt.
const suppliedProduction = "supplied"

// planSuppliedFFmpeg decides what the ffmpeg phase does with binaries this build compiles in a container
// nowhere.
//
// They are compiled on the platform they run on, and two machines can produce them. The maintainer's own
// build does, and drops them in the bundle directory for this build to adopt; the build standing on that
// same platform does it itself, with the source, digest, recipe, flags, and recipe the manifest already
// carries. Binaries the maintainer dropped win over the compile every time: the drop is what he built
// and tested, and compiling over it would throw it away.
//
// The plan reads one thing from a drop: which machine code the files hold. The run that adopts them runs
// them, on the machine standing here, through the same gate a compile runs its own binaries through.
//
// A target with nothing dropped for it, on a machine that is not its platform, is not a failure. The
// two supplied targets are built on machines the release machine does not control, and a release run that
// stopped for the one it does not have would ship nothing at all, including the six archives it could have
// shipped. It is reported, and skipped.
func planSuppliedFFmpeg(repo string, manifest bundle.Manifest, target string, record bundle.Target, paths ffmpegPaths, state ffmpegState, machine host) (ffmpegPlan, error) {
	// The cache key is the manifest's record and the target, so a warm cache is the entry this machine's
	// own run published, and the drop is not needed again: it has already been consumed out of this
	// directory.
	if state.bundleIsCached {
		return ffmpegPlan{
			Target: target,
			Says:   "reusing the ffmpeg supplied for " + target,
			Paths:  paths,
		}, nil
	}

	drop := suppliedDropFor(paths, target)

	if !aFileIsThere(drop["ffmpeg"]) && !aFileIsThere(drop["ffprobe"]) {
		// Nothing was dropped, and this machine can produce the binaries: the compile is what only a host of
		// the target's own operating system can do, and it is what the operator asked for when they asked
		// for the ffmpeg of the machine they are on. A macOS host of the other architecture is such a
		// host, because one macOS toolchain produces both architectures.
		if machine.canCompile(target) {
			return planNativeFFmpeg(repo, manifest, target, record, paths, state, machine)
		}

		return ffmpegPlan{
			Target: target,
			Says: "the ffmpeg for " + target + " was not supplied: build them on a " + target +
				" host with go run ./cmd/build ffmpeg " + target + ", or drop ffmpeg and ffprobe in " + paths.bundle,
			Skipped: true,
			Paths:   paths,
		}, nil
	}

	// The binaries a maintainer dropped are run by the run that adopts them, on this machine, through the
	// same gate the compile runs its own binaries through. What the plan reads here is which machine code
	// they hold, because that is the one thing running cannot say on a machine that runs both macOS
	// architectures.
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if !aFileIsThere(drop[name]) {
			return ffmpegPlan{}, fmt.Errorf("the ffmpeg for %s is supplied and %s has no %s: drop ffmpeg and ffprobe in %s, or build them on a %s host with go run ./cmd/build ffmpeg %s",
				target, paths.bundle, name, paths.bundle, target, target)
		}

		if err := checkSuppliedProgram(drop[name], target); err != nil {
			return ffmpegPlan{}, err
		}
	}

	return ffmpegPlan{
		Target: target,
		Says:   "adopting the ffmpeg supplied for " + target,
		Adopts: true,
		Paths:  paths,
	}, nil
}

// planNativeFFmpeg decides what the compile of the ffmpeg binaries looks like when the build is standing
// on the platform they run on.
//
// It is the same trimmed build the container targets make, from the same source the manifest pins, with
// the same recipe, and with the record's flags reaching configure the way they do there. What it does not
// have is a container: the host's own compiler, linker, stripper, make program, and signer are the
// toolchain, and the binaries come out of it as the platform's own programs — which is the whole reason
// they are compiled there and not shipped from a cross-compiler.
//
// Stripping comes before signing, and the order is not a style: strip rewrites the binary, and a signature
// computed over the bytes strip removed is a signature macOS refuses to run.
func planNativeFFmpeg(repo string, manifest bundle.Manifest, target string, record bundle.Target, paths ffmpegPaths, state ffmpegState, machine host) (ffmpegPlan, error) {
	if err := checkNativeToolchain(record, target, state); err != nil {
		return ffmpegPlan{}, err
	}

	work := filepath.Join(paths.scratch, sourceDir(manifest))

	source := ffmpegSource{Path: paths.source, URL: manifest.Source.URL, Fetch: !state.sourceIsCached}

	plan := ffmpegPlan{
		Target: target,
		Native: true,
		Source: source,
		Paths:  paths,
	}

	plan.Steps = append(plan.Steps, step{
		label:  "unpack",
		result: work,
		dir:    repo,
		name:   "tar",
		args:   []string{"-xJf", paths.source, "-C", paths.scratch},
	})

	configure := []string{filepath.Join(work, "configure")}

	configure = append(configure, manifest.Recipe...)

	// A compile across the two macOS architectures names the architecture it is building for in three
	// places: ffmpeg's own architecture switch, the switch that keeps configure from running a test
	// program of an architecture this host may not execute, and the compiler's own architecture flag in
	// the compiler and linker flags. On the target's own host the compiler's default is the target's
	// architecture, and none of the three is named.
	compilerArch := theCompilerArchitectureTheHostMustProduce(target, machine)

	cflags := append([]string{}, record.CFlags...)
	ldflags := append([]string{}, record.LDFlags...)

	if compilerArch != "" {
		configure = append(configure, "--arch="+theFFmpegArchitectureOf(target), "--enable-cross-compile")

		cflags = append(cflags, "-arch "+compilerArch)
		ldflags = append(ldflags, "-arch "+compilerArch)
	}

	if len(cflags) > 0 {
		configure = append(configure, "--extra-cflags="+strings.Join(cflags, " "))
	}

	if len(ldflags) > 0 {
		configure = append(configure, "--extra-ldflags="+strings.Join(ldflags, " "))
	}

	// The configure writes its build files into the directory it is run from — the config.mak the
	// Makefile includes on its second line, the config headers, and the directory skeleton of the tree
	// it is configuring — so it is run in the tree the unpack wrote. Run from anywhere else, that tree
	// has no config.mak for its own make to include, and the directory the configure ran from gains the
	// source tree's directories. The container route says the same thing with a workdir of its own.
	plan.Steps = append(plan.Steps, step{
		label:  "configure",
		result: "the trimmed build for " + target,
		dir:    work,
		name:   configure[0],
		args:   configure[1:],
	})

	plan.Steps = append(plan.Steps, step{
		label:  "make",
		result: "ffmpeg and ffprobe for " + target,
		dir:    work,
		name:   makeProgram(record),
		args:   []string{"-j", strconv.Itoa(runtime.NumCPU())},
	})

	plan.Steps = append(plan.Steps, step{
		label:  "strip",
		result: "the symbols taken out of ffmpeg and ffprobe",
		dir:    work,
		name:   "strip",
		args:   []string{"ffmpeg", "ffprobe"},
	})

	if strings.HasPrefix(target, "macos-") {
		// macOS refuses to run an unsigned binary out of a downloaded archive, and the ad-hoc signature
		// is what the binaries carry: they need no developer identity, and it is the signature the programs the
		// server embeds have to have to start.
		plan.Steps = append(plan.Steps, step{
			label:  "sign",
			result: "the ad-hoc signature ffmpeg and ffprobe run under",
			dir:    work,
			name:   "codesign",
			args:   []string{"-s", "-", "ffmpeg", "ffprobe"},
		})
	}

	return plan, nil
}

// makeProgram is the make program a compile of this target runs. A record naming none runs make; the
// FreeBSD record names gmake, because ffmpeg's build asks for GNU make and FreeBSD's base make is not it.
func makeProgram(record bundle.Target) string {
	if record.Make == "" {
		return "make"
	}

	return record.Make
}

// nativeProgramsAre the programs a compile on the target's own platform runs: the archive reader, a C
// compiler, the stripper, and the make program the record names, plus the signer on a macOS target, where
// the binaries have to carry a signature to run.
func nativeProgramsAre(record bundle.Target, target string) []string {
	programs := []string{"tar", "cc", "strip", makeProgram(record)}

	if strings.HasPrefix(target, "macos-") {
		programs = append(programs, "codesign")
	}

	return programs
}

// checkNativeToolchain is the host's own build tools, asked before a step runs.
//
// A machine missing one of them cannot start the compile, and finding that out from configure's output,
// after the source was fetched and unpacked, is a minute of work spent to learn what could have been said
// at once. The refusal names the program and where it comes from on that platform.
func checkNativeToolchain(record bundle.Target, target string, state ffmpegState) error {
	for _, name := range nativeProgramsAre(record, target) {
		if state.nativeTools[name] == "" {
			return fmt.Errorf("compiling the ffmpeg for %s needs %s, which this machine does not have: %s",
				target, name, whereTheToolComesFrom(name, target))
		}
	}

	return nil
}

// whereTheToolComesFrom is what installs one build tool on the platform the target runs on.
func whereTheToolComesFrom(name string, target string) string {
	if strings.HasPrefix(target, "freebsd-") {
		if name == "gmake" {
			return "pkg install gmake installs it, and ffmpeg's build asks for GNU make where FreeBSD's base make is not it"
		}

		return "it is in the FreeBSD base system"
	}

	return "the Xcode Command Line Tools carry it"
}

// suppliedDropFor is where binaries built by hand sit before this build adopts them, and where the
// binaries this build compiled on its own host are put so the adoption can consume them.
func suppliedDropFor(paths ffmpegPaths, target string) map[string]string {
	drop := map[string]string{}

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		drop[name] = filepath.Join(paths.bundle, name+programSuffix(target))
	}

	return drop
}

// adoptSuppliedFFmpeg consumes the drop: the binaries move out of the directory a server binary embeds and
// into this build's own cache, gzipped, with the licence text the run wrote beside them.
//
// The binaries have to leave that directory. //go:embed all: takes every file in it, so a raw program left
// sitting next to the gzipped one is embedded as well, and every server built for that target carries
// about ten megabytes of program it will never use. The gzipped binaries in the cache are the same
// programs: gzip is lossless, and the ad-hoc code signature signed macOS programs carry is a hash over
// these bytes.
//
// The licence text goes into the cache entry for the same reason the binaries do: the entry is what a
// reusing run reads, and the words a run wrote about the binaries it ran belong with them.
func adoptSuppliedFFmpeg(paths ffmpegPaths, target string, licence string) error {
	if err := os.MkdirAll(paths.cached, 0755); err != nil {
		return fmt.Errorf("the build cannot prepare %s: %w", paths.cached, err)
	}

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		drop := filepath.Join(paths.bundle, name+programSuffix(target))

		body, err := os.ReadFile(drop)
		if err != nil {
			return fmt.Errorf("the supplied %s for %s cannot be read: %w", name, target, err)
		}

		if err := writeGzip(paths.cachedProgram(name+".gz"), body); err != nil {
			return err
		}

		if err := os.Remove(drop); err != nil {
			return fmt.Errorf("the build cannot take the supplied %s out of %s: %w", name, paths.bundle, err)
		}
	}

	if err := os.WriteFile(filepath.Join(paths.cached, "LICENCE.md"), []byte(licence), 0644); err != nil {
		return fmt.Errorf("the build cannot write the licence text for %s: %w", target, err)
	}

	return nil
}

// theLicenceInTheCache is the licence text the run that produced one cache entry wrote. An entry older
// than that habit says only what the manifest says, and names this machine's cache as where the binaries
// came from.
func theLicenceInTheCache(paths ffmpegPaths, manifest bundle.Manifest, target string) (string, error) {
	body, err := os.ReadFile(filepath.Join(paths.cached, "LICENCE.md"))
	if err == nil {
		return string(body), nil
	}

	if !os.IsNotExist(err) {
		return "", fmt.Errorf("the licence text in %s cannot be read: %w", paths.cached, err)
	}

	return suppliedFFMpegLicence(manifest, target, theRunOfTheBinaries{}), nil
}

// buildImageArgs prepares the container one target is compiled or checked in. The build context is an
// empty directory on purpose: the image is a toolchain, and nothing of this repository belongs in it.
func buildImageArgs(image string, base string, packages []string) []string {
	args := []string{
		"build",
		"-t", image,
		"-f", "Containerfile.ffmpeg",
		"--build-arg", "base=" + base,
		"--build-arg", "packages=" + strings.Join(packages, " "),
		filepath.Join(".build", "ffmpeg", "context"),
	}

	return args
}

// configureStep is the trimmed build asked of the source: the recipe, then what makes the compile cross
// to this target, then the flags that link it statically for the target's word size.
//
// --enable-cross-compile and ffmpeg's own win32 and win64 operating-system names are what the manifest
// records, because both mistakes surface as a Makefile error rather than as a cross-compile error.
func configureStep(repo string, paths ffmpegPaths, image string, manifest bundle.Manifest, record bundle.Target, program theContainerProgram) step {
	args := append(program.run(),
		"-v", paths.scratch+":/work",
		"-w", "/work/"+sourceDir(manifest),
		image,
		"./configure",
	)

	args = append(args, manifest.Recipe...)
	args = append(args, record.Cross...)

	if len(record.CFlags) > 0 {
		args = append(args, "--extra-cflags="+strings.Join(record.CFlags, " "))
	}

	if len(record.LDFlags) > 0 {
		args = append(args, "--extra-ldflags="+strings.Join(record.LDFlags, " "))
	}

	return step{
		label:  "configure",
		result: "the trimmed build for " + record.Container,
		dir:    repo,
		name:   program.name,
		args:   args,
	}
}

// programSuffix is what a target's operating system puts on the end of a program it runs.
func programSuffix(target string) string {
	if strings.HasPrefix(target, "windows-") {
		return ".exe"
	}

	return ""
}

// checkSource is the integrity anchor of this feature. ffmpeg.org signs its releases with GPG and
// publishes no checksum, so the digest recorded in the manifest when the release was chosen is what
// every fetch is checked against, and a mismatch stops the build.
func checkSource(path string, manifest bundle.Manifest) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("the ffmpeg source is not there to be checked: %w", err)
	}

	defer file.Close()

	hash := sha256.New()

	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("the ffmpeg source cannot be read to be checked: %w", err)
	}

	got := hex.EncodeToString(hash.Sum(nil))

	if got != manifest.Source.SHA256 {
		return fmt.Errorf(
			"%s is %s, and this build only compiles the ffmpeg release whose sha256 is %s",
			filepath.Base(path), got, manifest.Source.SHA256,
		)
	}

	return nil
}

// fetchSource puts the release tarball in the build cache. It is fetched by the tool rather than by a
// program the operator has to install, so that a container program is the only new prerequisite this
// build adds.
func fetchSource(src ffmpegSource) error {
	client := &http.Client{Timeout: 10 * time.Minute}

	resp, err := client.Get(src.URL)
	if err != nil {
		return fmt.Errorf("the ffmpeg source cannot be fetched from %s: %w", src.URL, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s is not the ffmpeg release this build wants: %s", src.URL, resp.Status)
	}

	temporary, err := os.CreateTemp(filepath.Dir(src.Path), filepath.Base(src.Path)+".*")
	if err != nil {
		return fmt.Errorf("the build cannot prepare a place for the ffmpeg source: %w", err)
	}

	defer os.Remove(temporary.Name())

	if _, err := io.Copy(temporary, resp.Body); err != nil {
		temporary.Close()

		return fmt.Errorf("the ffmpeg source did not arrive whole: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("the ffmpeg source did not arrive whole: %w", err)
	}

	if err := os.Rename(temporary.Name(), src.Path); err != nil {
		return fmt.Errorf("the build cannot put the ffmpeg source where it looks for it: %w", err)
	}

	return nil
}

// runFFmpeg produces the ffmpeg a server binary built on this machine is going to carry.
//
// It reports the targets it has nothing to produce for: the two whose ffmpeg binaries are compiled on
// their own platform, on a machine this build is not standing on, with nothing dropped for it. Such a
// target is skipped, and the caller decides what a skipped target means for the run — the release run
// ships the archives it can ship, and a run that compiles one binary compiles none. On the machine that is
// the target, the same phase compiles them, and that is not a skip.
func (t tool) runFFmpeg(o options, stdout io.Writer) (bool, error) {
	manifest := bundle.ReadManifest()

	// The program the run speaks to is a fact of the machine, asked once, and every step of the plan
	// carries it. A machine with neither program is told here, before a source is fetched.
	program, err := resolveContainerProgram(t.resolve, os.LookupEnv)
	if err != nil {
		return false, err
	}

	target := ffmpegTarget(o)

	machine := t.theMachine()

	paths := ffmpegPathsFor(t.repo, manifest, target)

	record, covered := manifest.Targets[target]

	// The tools of a compile that runs on the target's own platform are resolved the way the container
	// program is, and for the same reason: the plan is made from what this machine has, and a host
	// missing one of them is told which one before the source is fetched.
	nativeTools := map[string]string{}

	if covered && record.Produced == suppliedProduction {
		for _, name := range nativeProgramsAre(record, target) {
			where, err := t.resolve(name)

			if err == nil && where != "" {
				nativeTools[name] = where
			}
		}
	}

	state := ffmpegState{
		program:        program,
		sourceIsCached: aFileIsThere(paths.source),
		bundleIsCached: covered && aFileIsThere(paths.cachedProgram("ffmpeg.gz")) && aFileIsThere(paths.cachedProgram("ffprobe.gz")),
		nativeTools:    nativeTools,
	}

	plan, err := planFFmpeg(o, t.repo, manifest, state, machine)
	if err != nil {
		return false, err
	}

	if plan.Says != "" {
		fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Says)
	}

	if plan.Native {
		return t.runNativeFFmpeg(plan, manifest, target, stdout)
	}

	if record.Produced == suppliedProduction {
		return t.runSuppliedFFmpeg(plan, manifest, target, stdout)
	}

	if len(plan.Steps) > 0 {
		if err := prepareFFmpeg(plan.Paths); err != nil {
			return false, err
		}

		if plan.Source.Fetch {
			fmt.Fprintf(stdout, "%-7s %s\n", "fetch", plan.Source.URL)
		}

		if err := t.source(plan.Source, manifest); err != nil {
			return false, err
		}

		for _, s := range plan.Steps {
			if err := t.execute(s); err != nil {
				return false, fmt.Errorf("%s: %w", s.label, err)
			}

			fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)
		}

		report, err := t.runFFMPEGCheck(theContainerProgram{name: state.program}, plan, manifest, record, target)
		if err != nil {
			return false, err
		}

		fmt.Fprintf(stdout, "%-7s %d conversions, %d codecs, %s s to %s s\n", "check", report.Conversions,
			len(report.Codecs.Audio),
			strconv.FormatFloat(report.Shortest, 'f', 6, 64),
			strconv.FormatFloat(report.Longest, 'f', 6, 64),
		)

		if err := gzipCompiled(plan.Paths, manifest, target); err != nil {
			return false, err
		}
	}

	// A compiled ffmpeg and a reused one end the same way, so that what the server embeds and what the
	// release archive carries do not depend on whether the cache was warm.
	if covered {
		if err := publishFFmpeg(plan.Paths, manifest, target, ffmpegLicence(manifest, target)); err != nil {
			return false, err
		}

		fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Paths.bundle)
	}

	return false, nil
}

// runSuppliedFFmpeg does the work of a target whose ffmpeg binaries are compiled on the platform they run
// on: the binaries a maintainer dropped, the binaries this machine compiled, or the binaries already in
// this machine's cache.
//
// The dropped binaries are run before they are adopted, on the machine standing here, through the same gate
// the compile runs its own binaries through. What converts is gzipped into the cache entry, and the licence
// text the archive carries is written from that run: it names the machine that ran them.
//
// A target with nothing dropped for it, on a machine that is not its platform, is reported as skipped, and
// that is the whole of it: the message the plan printed says where to drop the binaries and where to build
// them.
func (t tool) runSuppliedFFmpeg(plan ffmpegPlan, manifest bundle.Manifest, target string, stdout io.Writer) (bool, error) {
	if plan.Skipped {
		return true, nil
	}

	if plan.Adopts {
		run, err := t.prove(target, plan.Paths, stdout)
		if err != nil {
			return false, err
		}

		licence := suppliedFFMpegLicence(manifest, target, run)

		if err := adoptSuppliedFFmpeg(plan.Paths, target, licence); err != nil {
			return false, err
		}

		fmt.Fprintf(stdout, "%-7s %s\n", "adopt", plan.Paths.cached)

		if err := publishFFmpeg(plan.Paths, manifest, target, licence); err != nil {
			return false, err
		}

		fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Paths.bundle)

		return false, nil
	}

	// The cache entry holds the binaries this machine's own run produced, and the licence text that run
	// wrote is in the entry beside them: the archive carries the words the run said, not words this run
	// composes about a compile it did not take part in.
	licence, err := theLicenceInTheCache(plan.Paths, manifest, target)
	if err != nil {
		return false, err
	}

	if err := publishFFmpeg(plan.Paths, manifest, target, licence); err != nil {
		return false, err
	}

	fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Paths.bundle)

	return false, nil
}

// runNativeFFmpeg does the work of the ffmpeg binaries compiled on the machine that runs them.
//
// The binaries are dropped where a maintainer's binaries are dropped, and from there the run is the
// adoption path: the same run of the binaries, the same gzip into the cache entry, the same licence text,
// and the same removal of the raw programs from the directory the server embeds. A compile that published
// its own binaries by its own route would be a second adoption path, and the two would drift apart.
//
// The order is the order the evidence is needed in. The binaries are run before anything is adopted,
// because a cache entry holding binaries that convert nothing is an archive that ships a server that
// converts nothing.
func (t tool) runNativeFFmpeg(plan ffmpegPlan, manifest bundle.Manifest, target string, stdout io.Writer) (bool, error) {
	if err := prepareFFmpeg(plan.Paths); err != nil {
		return false, err
	}

	// Each compile starts from a pristine copy of the tarball. A tree left by the compile before it,
	// configured for whatever the host compiled last, fails later in a way that reads like a broken
	// recipe. The container targets throw their own tree away in a step of their plan; this tree is the
	// build's own to throw away.
	if err := resetScratch(plan.Paths.scratch); err != nil {
		return false, err
	}

	if plan.Source.Fetch {
		fmt.Fprintf(stdout, "%-7s %s\n", "fetch", plan.Source.URL)
	}

	if err := t.source(plan.Source, manifest); err != nil {
		return false, err
	}

	for _, s := range plan.Steps {
		if err := t.execute(s); err != nil {
			return false, fmt.Errorf("%s: %w", s.label, err)
		}

		fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)
	}

	if err := nativeDrop(t, plan, manifest, target); err != nil {
		return false, err
	}

	fmt.Fprintf(stdout, "%-7s %s\n", "drop", plan.Paths.bundle)

	run, err := t.prove(target, plan.Paths, stdout)
	if err != nil {
		return false, err
	}

	licence := suppliedFFMpegLicence(manifest, target, run)

	if err := adoptSuppliedFFmpeg(plan.Paths, target, licence); err != nil {
		return false, err
	}

	fmt.Fprintf(stdout, "%-7s %s\n", "adopt", plan.Paths.cached)

	if err := publishFFmpeg(plan.Paths, manifest, target, licence); err != nil {
		return false, err
	}

	fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Paths.bundle)

	return false, nil
}

// nativeDrop puts the compiled binaries where a maintainer's drop is put, executable.
//
// The proof reads it from there, the adoption consumes it from there, and the directory it is dropped in
// is the directory the server embeds, which is why the adoption takes it back out again.
func nativeDrop(t tool, plan ffmpegPlan, manifest bundle.Manifest, target string) error {
	work := filepath.Join(plan.Paths.scratch, sourceDir(manifest))

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		body, err := os.ReadFile(filepath.Join(work, name))
		if err != nil {
			return fmt.Errorf("the compile produced no %s for %s: %w", name, target, err)
		}

		drop := filepath.Join(plan.Paths.bundle, name+programSuffix(target))

		file, err := os.OpenFile(drop, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			return fmt.Errorf("the build cannot drop the compiled %s in %s: %w", name, plan.Paths.bundle, err)
		}

		if _, err := file.Write(body); err != nil {
			file.Close()

			return fmt.Errorf("the build cannot drop the compiled %s in %s: %w", name, plan.Paths.bundle, err)
		}

		if err := file.Close(); err != nil {
			return fmt.Errorf("the build cannot drop the compiled %s in %s: %w", name, plan.Paths.bundle, err)
		}

		// The mode is set, not asked for: a file that was already there keeps the mode it had, and a
		// binary the server cannot start is a binary dropped unreadable.
		if err := os.Chmod(drop, 0755); err != nil {
			return fmt.Errorf("the build cannot make the compiled %s runnable: %w", name, err)
		}
	}

	return nil
}

// resetScratch empties the directory one target's compile works in.
func resetScratch(scratch string) error {
	if err := os.RemoveAll(scratch); err != nil {
		return fmt.Errorf("the build cannot empty %s: %w", scratch, err)
	}

	if err := os.MkdirAll(scratch, 0755); err != nil {
		return fmt.Errorf("the build cannot prepare %s: %w", scratch, err)
	}

	return nil
}

// fetchAndCheckSource puts the pinned release in the build's cache and recognises it as that release.
func fetchAndCheckSource(src ffmpegSource, manifest bundle.Manifest) error {
	if src.Fetch {
		if err := fetchSource(src); err != nil {
			return err
		}
	}

	return checkSource(src.Path, manifest)
}

// prepareFFmpeg makes the directories the phase writes into. The scratch directory is emptied by the
// container that owns what is left in it, in the plan, rather than here.
func prepareFFmpeg(paths ffmpegPaths) error {
	for _, dir := range []string{paths.sources, paths.context, paths.scratch, paths.cached} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("the build cannot prepare %s: %w", dir, err)
		}
	}

	return nil
}

// runFFMPEGCheck runs the compiled programs through the converter's own argument list, inside the
// container that can run them.
func (t tool) runFFMPEGCheck(program theContainerProgram, plan ffmpegPlan, manifest bundle.Manifest, record bundle.Target, target string) (checkReport, error) {
	image := plan.Image
	if record.Conformance.Container != "" {
		image = plan.Runner
	}

	work := "/work/" + sourceDir(manifest)

	ffmpeg := append(append([]string{}, record.Conformance.Run...), work+"/ffmpeg"+programSuffix(target))
	ffprobe := append(append([]string{}, record.Conformance.Run...), work+"/ffprobe"+programSuffix(target))

	// The recordings the check writes out land in the scratch directory itself, which the container
	// sees at /work, and not in the source tree the compiled programs live in.
	return ffmpegCheck(
		inContainer(program, image, plan.Paths.scratch),
		checkPrograms{FFmpeg: ffmpeg, FFProbe: ffprobe, Work: "/work"},
		plan.Paths.scratch,
	)
}

// containerRun is how the build reaches a binary it cannot run itself: the compiled ffmpeg lives in the
// mounted scratch directory, inside the container that compiled it. The image has to be named before the
// program, or the program reads it as the image to run.
func containerRun(program theContainerProgram, image string, scratch string, argv []string) []string {
	args := append([]string{program.name}, program.run()...)
	args = append(args, "-i", "-v", scratch+":/work", image)

	return append(args, argv...)
}

// inContainer runs one program in that container, hands it a recording on its input, and hands back
// what it wrote.
func inContainer(program theContainerProgram, image string, scratch string) command {
	return func(argv []string, stdin []byte) ([]byte, error) {
		args := containerRun(program, image, scratch, argv)

		out := bytes.NewBuffer([]byte(nil))
		errout := bytes.NewBuffer([]byte(nil))

		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = bytes.NewReader(stdin)
		cmd.Stdout = out
		cmd.Stderr = errout

		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("%s", strings.TrimSpace(errout.String()))
		}

		return out.Bytes(), nil
	}
}

// gzipCompiled puts the two programs the container produced into the cache, which is what makes a second
// build with an unchanged recipe compile nothing at all.
func gzipCompiled(paths ffmpegPaths, manifest bundle.Manifest, target string) error {
	suffix := programSuffix(target)
	source := filepath.Join(paths.scratch, sourceDir(manifest))

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		body, err := os.ReadFile(filepath.Join(source, name+suffix))
		if err != nil {
			return fmt.Errorf("the build did not produce %s for %s: %w", name, target, err)
		}

		if err := writeGzip(paths.cachedProgram(name+".gz"), body); err != nil {
			return err
		}
	}

	return nil
}

// publishFFmpeg puts the binaries, and what a release archive has to say about them, into the bundle directory
// the server embeds. The placeholder that keeps a plain go build compiling is left where it is.
//
// The licence text is passed in, because the two ways a target's ffmpeg comes to be say different and
// true things about it, and this function is not the place to decide which of them the archive carries.
func publishFFmpeg(paths ffmpegPaths, manifest bundle.Manifest, target string, licence string) error {
	if err := os.MkdirAll(paths.bundle, 0755); err != nil {
		return fmt.Errorf("the build cannot prepare %s: %w", paths.bundle, err)
	}

	for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
		body, err := os.ReadFile(paths.cachedProgram(name))
		if err != nil {
			return fmt.Errorf("the build has no %s for %s to embed: %w", strings.TrimSuffix(name, ".gz"), target, err)
		}

		if err := os.WriteFile(filepath.Join(paths.bundle, name), body, 0644); err != nil {
			return fmt.Errorf("the build cannot write %s into the bundle: %w", name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(paths.bundle, "LICENCE.md"), []byte(licence), 0644); err != nil {
		return fmt.Errorf("the build cannot write the licence text for %s: %w", target, err)
	}

	return nil
}

// ffmpegLicence is what a release archive carries about the ffmpeg inside it: the licence the binary is
// under, the release it was compiled from, and the exact recipe it was compiled with.
//
// It is written from the manifest rather than kept as a document somebody has to remember to update,
// because a licence notice that names the wrong release is worse than none.
func ffmpegLicence(manifest bundle.Manifest, target string) string {
	var text strings.Builder

	text.WriteString("# The ffmpeg this server carries\n\n")
	text.WriteString(fmt.Sprintf(
		"This build of rdio-scanner carries ffmpeg %s and ffprobe %s inside its own binary, compiled\nfrom source for %s. It converts audio with nothing installed on the host.\n\n",
		manifest.FFmpeg, manifest.FFmpeg, target,
	))

	text.WriteString("ffmpeg is free software, copyright the FFmpeg developers. This binary is used under the GNU\nLesser General Public License version 2.1 or later, and was configured without --enable-gpl\nand without --enable-nonfree, so it carries no GPL or otherwise restricted component. The\nlicence text is at https://www.gnu.org/licenses/old-licenses/lgpl-2.1.html\n\n")

	text.WriteString("The source it was compiled from:\n\n")
	text.WriteString(fmt.Sprintf("    %s\n    sha256 %s\n\n", manifest.Source.URL, manifest.Source.SHA256))

	text.WriteString("The configure recipe it was built with:\n\n")

	for _, flag := range manifest.Recipe {
		text.WriteString("    " + flag + "\n")
	}

	text.WriteString(theAudioOnlyPosture(manifest.Recipe))

	text.WriteString(fmt.Sprintf(
		"\nThe same recipe, the same digest and the same toolchain are recorded in this repository at\ninternal/infrastructure/ffmpeg/bundle/manifest.json, and the command that produced this binary\nis `go run ./cmd/build ffmpeg %s`.\n",
		target,
	))

	return text.String()
}

// theAudioOnlyPosture is what the recipe says is not compiled in, in the words the licence texts use.
// It is composed from the recipe, because a notice that names a switch the recipe dropped is a notice
// promising a build the recipe does not produce, and that notice travels inside every archive.
//
// The video filters the compiled program carries are the ones ffmpeg's own program selects for its
// output handling, and no configure switch takes them out; the posture is about codecs, decoders,
// encoders and the network, which is what the recipe decides and the proof reads back.
func theAudioOnlyPosture(recipe []string) string {
	var posture []string

	if theRecipeCarries(recipe, "--disable-everything") {
		posture = append(posture, "the recipe disables every component and enables only the audio demuxers, decoders, encoder, muxers and filters the server converts with, and the pipe, fd and file protocols the recording arrives and leaves on")
		posture = append(posture, "no video codec, video decoder, or video encoder is compiled in")
	}

	if theRecipeCarries(recipe, "--disable-network") {
		posture = append(posture, "no network protocol is compiled in")
	}

	if len(posture) == 0 {
		return ""
	}

	return "\nThis program is compiled for audio: " + strings.Join(posture, "; ") + ".\n\n"
}

// theRecipeCarries reports whether the recipe tells the build one switch.
func theRecipeCarries(recipe []string, flag string) bool {
	for _, one := range recipe {
		if one == flag {
			return true
		}
	}

	return false
}

// suppliedFFMpegLicence is what a release archive carries about the ffmpeg binaries compiled on the host
// the target runs on.
//
// A compiled target's binaries are reproducible from this repository, so its notice points at the manifest
// and the command. These are not reproducible on the machine that produces the releases: they are compiled
// where the target runs, by a build standing on that machine, and the build ran them there. The notice
// names that machine, the source and recipe they were built from, the command that built them, and the
// codecs the run recorded. It says plainly that a deployment inherits the libraries of the machine that
// compiled them, because that is the one thing about these binaries the build does not check.
func suppliedFFMpegLicence(manifest bundle.Manifest, target string, run theRunOfTheBinaries) string {
	var text strings.Builder

	host := run.Host
	if host == "" {
		host = "the machine that compiled them"
	}

	text.WriteString("# The ffmpeg this server carries\n\n")
	text.WriteString(fmt.Sprintf(
		"This build of rdio-scanner carries ffmpeg %s and ffprobe %s inside its own binary, compiled for %s\non the machine that runs that machine code. It converts audio with nothing installed on the host.\n\n",
		manifest.FFmpeg, manifest.FFmpeg, target,
	))

	text.WriteString("ffmpeg is free software, copyright the FFmpeg developers. This binary is used under the GNU\nLesser General Public License version 2.1 or later, and was configured without --enable-gpl\nand without --enable-nonfree, so it carries no GPL or otherwise restricted component. The\nlicence text is at https://www.gnu.org/licenses/old-licenses/lgpl-2.1.html\n\n")

	text.WriteString("The source it was compiled from:\n\n")
	text.WriteString(fmt.Sprintf("    %s\n    sha256 %s\n\n", manifest.Source.URL, manifest.Source.SHA256))

	text.WriteString("The configure recipe it was built with:\n\n")

	for _, flag := range manifest.Recipe {
		text.WriteString("    " + flag + "\n")
	}

	text.WriteString(theAudioOnlyPosture(manifest.Recipe))

	text.WriteString(fmt.Sprintf(
		"\nThe command that produced them, on %s:\n\n    go run ./cmd/build ffmpeg %s\n\n",
		host, target,
	))

	if len(run.Codecs.Audio) > 0 {
		text.WriteString(fmt.Sprintf(
			"The run that adopted them asked them what they register: %s, and names no video codec, video\ndecoder, or video encoder. It converted %d recordings of the server's own audio.\n\n",
			strings.Join(run.Codecs.Audio, ", "), run.Conversions,
		))
	}

	if run.Host != "" {
		text.WriteString(fmt.Sprintf(
			"These binaries were compiled on %s, the machine that is the %s target: the machine that produces\nthe releases runs no %s program. The build ran them there and adopted what converted, and it\nchecked one thing about them beyond running them: that the file holds the machine code %s runs.\nWhat libraries they need is what that machine has, which is the one thing this build does not\ncheck.\n",
			run.Host, target, target, target,
		))

		return text.String()
	}

	text.WriteString(fmt.Sprintf(
		"These binaries were compiled on the machine that is the %s target: the machine that produces the\nreleases runs no %s program. The run that compiled them ran them there and adopted what converted,\nand this release reuses what that run published in its own cache. Beyond running them, that run\nchecked only that the file holds the machine code %s runs. What libraries they need is what that\nmachine has, which is the one thing this build does not check.\n",
		target, target, target,
	))

	return text.String()
}

func writeGzip(path string, body []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("the build cannot write %s: %w", path, err)
	}

	defer file.Close()

	writer := gzip.NewWriter(file)

	if _, err := writer.Write(body); err != nil {
		writer.Close()

		return fmt.Errorf("the build cannot compress %s: %w", path, err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("the build cannot finish compressing %s: %w", path, err)
	}

	return nil
}

func aFileIsThere(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}
