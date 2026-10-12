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
// operator on the Mac that a macOS pair is built on is asked about macos-arm64, the target the manifest
// records, and not about the name go builds a macOS binary for.
func runtimeTarget() string {
	return releaseTargetWordFor(runtime.GOOS, runtime.GOARCH)
}

// ffmpegState is what the tree already has, read before anything is decided.
type ffmpegState struct {
	podmanIsThere  bool
	sourceIsCached bool
	bundleIsCached bool

	// nativeTools is the host's own build tools, resolved: the program name mapped to where it was found,
	// and no entry for a program that is not there. Only a compile that runs on the target's own platform
	// uses it, and it is read before the plan is made so that a host missing a compiler is told which one
	// before a minute of work finds it out.
	nativeTools map[string]string
}

// host is the platform the machine running the build is on, named the way the release names a platform:
// the pair a supplied target carries runs on one platform alone, and the question the build asks of it is
// whether the machine standing in front of the build is that platform.
type host struct {
	goos   string
	goarch string
}

// thisMachine is the platform the build is running on.
func thisMachine() host {
	return host{goos: runtime.GOOS, goarch: runtime.GOARCH}
}

// isTheHostOf says whether this machine is the platform one target's pair runs on.
//
// It is the rule that decides what a supplied target means for a run. The pair is compiled with the
// platform's own compiler, against the platform's own libraries, and a compile of it written on any other
// machine is a binary that platform refuses to run: the build that is standing on the target's platform
// compiles the pair, and every other machine adopts what was dropped or skips the target.
func (h host) isTheHostOf(target string) bool {
	tr, found := targetFor(target)

	return found && h.goos == tr.goos && h.goarch == tr.goarch
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
	// compiled: an uncovered target, a bundle that is already there, or a pair adopted from what the
	// maintainer dropped.
	Steps []step

	// Skipped says this target has nothing to build and produces nothing: the pair the manifest says is
	// supplied was not dropped. A release run skips it and ships the rest.
	Skipped bool

	// Native says this plan compiles the pair on the machine running the build, because that machine is
	// the platform the pair runs on. It is the answer to the operator who asked for the ffmpeg of the
	// machine they are standing on, and it is never a skip: the compile is what that run asked for.
	Native bool

	// Check is the ffmpeg check the build adopted a supplied pair on, and is empty for a target this
	// build compiles. The licence text the release archive carries names the check that ran.
	Check anFfmpegCheck

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

// cacheKey is what has to be the same for a compiled ffmpeg to be reused: the release, the source this
// project pinned, the target, the configure recipe, and that target's toolchain and check environment.
//
// The digest is in here and not only in the check that runs before compiling, because a cache hit skips
// that check: without it, a manifest whose pinned source was changed would quietly reuse what the old
// source compiled.
//
// A target whose pair is compiled on its own platform has no container to key on, so its key adds the way
// it is produced, the make program that build names, and the digest of each program. A pair rebuilt on that
// platform is a new cache entry, and a compiled record keys on the compile's container inputs alone, so a
// manifest gaining these fields does not invalidate a warm cache.
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
	// that build ran, and the digest of each program it produced. A compiled target keys on the
	// container's inputs alone, so a manifest gaining these fields does not invalidate a warm cache.
	if record.Produced != "" {
		parts = append(parts, record.Produced, record.Make, record.Programs["ffmpeg"], record.Programs["ffprobe"])
	}

	return sha256Of([]byte(strings.Join(parts, "\n")))
}

func sha256Of(body []byte) string {
	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}

// podmanRun is how every step and every check command enters a container.
//
// --userns=host is what makes the artifacts the container writes belong to the operator who ran the
// build. Without it, rootless podman maps the container's root to a subuid: the tree it compiled in
// comes back owned by somebody the build cannot read, overwrite, or throw away, and the next build
// cannot start.
func podmanRun() []string {
	return []string{"run", "--rm", "--userns=host"}
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
// The machine is the platform the build is running on, and it is part of the decision because three of
// the targets are compiled on the platform their pair runs on: on that platform the build has the source,
// the digest, the recipe, the flags, and the check, and compiling the pair there is what the operator
// asked for. On every other platform the pair is something the build is handed.
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

	if !state.podmanIsThere {
		return ffmpegPlan{}, fmt.Errorf("compiling the ffmpeg for %s needs podman, which this machine does not have", target)
	}

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
		name:   "podman",
		args:   buildImageArgs(image, record.Container, packages),
	})

	if runner != image {
		plan.Steps = append(plan.Steps, step{
			label:  "runner",
			result: runner,
			dir:    repo,
			name:   "podman",
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
		name:   "podman",
		args: append(podmanRun(),
			"-v", paths.scratch+":/work",
			image,
			"find", "/work", "-mindepth", "1", "-delete",
		),
	})

	plan.Steps = append(plan.Steps, step{
		label:  "unpack",
		result: "/work/" + sourceDir(manifest),
		dir:    repo,
		name:   "podman",
		args: append(podmanRun(),
			"-v", paths.sources+":/src:ro",
			"-v", paths.scratch+":/work",
			image,
			"tar", "-xJf", "/src/"+sourceName(manifest), "-C", "/work",
		),
	})

	plan.Steps = append(plan.Steps, configureStep(repo, paths, image, manifest, record))

	plan.Steps = append(plan.Steps, step{
		label:  "make",
		result: "ffmpeg and ffprobe for " + target,
		dir:    repo,
		name:   "podman",
		args: append(podmanRun(),
			"-v", paths.scratch+":/work",
			"-w", "/work/"+sourceDir(manifest),
			image,
			"make", "-j", strconv.Itoa(runtime.NumCPU()),
		),
	})

	// The tree the container compiled in belongs to whatever uid podman gave the container, which is
	// not a uid this build can name: rootless podman maps it into a subuid range, and the ownership
	// ffmpeg's own tarball carries lands somewhere else again. Chasing the uid is a dead end — the
	// numbers differ between rootless and rootful podman, and a wrong one leaves a tree the next build
	// cannot even open. Making the tree readable, writable and traversable by everybody in it is what
	// the build actually needs, and it does not depend on which uid turned out to be which.
	plan.Steps = append(plan.Steps, step{
		label:  "chmod",
		result: paths.scratch,
		dir:    repo,
		name:   "podman",
		args: append(podmanRun(),
			"-v", paths.scratch+":/work",
			image,
			"chmod", "-R", "a+rwX", "/work",
		),
	})

	return plan, nil
}

// suppliedProduction is the manifest's way of saying that this build compiles no ffmpeg for a target: the
// pair is compiled on the platform the target runs on, by the project's own maintainer, and dropped in the
// bundle directory for this build to adopt.
const suppliedProduction = "supplied"

// planSuppliedFFmpeg decides what the ffmpeg phase does with a pair this build compiles in a container
// nowhere.
//
// The pair is compiled on the platform it runs on, and two machines can produce it. The maintainer's own
// build does, and drops the pair in the bundle directory for this build to adopt; the build standing on
// that same platform does it itself, with the source, digest, recipe, flags, and check the manifest
// already carries. A pair the maintainer dropped wins over the compile every time: the drop is the pair
// its proof was recorded for, and compiling over it would throw away the evidence the release adopts on.
//
// The build never runs a supplied pair it was handed. The evidence it can gather is the digest the
// manifest pins, the shape the target's own linker writes, and the proof the ffmpeg check recorded on
// that platform, and all three have to agree with the pair in front of the build before any of it is
// written.
//
// A target with nothing dropped for it, on a machine that is not its platform, is not a failure. The
// three pairs are built on machines the release machine does not control, and a release run that stopped
// for the one it does not have would ship nothing at all, including the five archives it could have
// shipped. It is reported, and skipped.
func planSuppliedFFmpeg(repo string, manifest bundle.Manifest, target string, record bundle.Target, paths ffmpegPaths, state ffmpegState, machine host) (ffmpegPlan, error) {
	// The cache key is the pinned pair, so a warm cache is the cache of the pair the manifest names, and
	// the drop is not needed again: it has already been consumed out of this directory.
	if state.bundleIsCached {
		return ffmpegPlan{
			Target: target,
			Says:   "reusing the ffmpeg supplied for " + target,
			Paths:  paths,
		}, nil
	}

	drop := suppliedDropFor(paths, target)

	if !aFileIsThere(drop["ffmpeg"]) && !aFileIsThere(drop["ffprobe"]) {
		// Nothing was dropped, and this machine is the platform the pair runs on: the compile is what
		// only it can do, and it is what the operator asked for when they asked for the ffmpeg of the
		// machine they are on.
		if machine.isTheHostOf(target) {
			return planNativeFFmpeg(repo, manifest, target, record, paths, state)
		}

		return ffmpegPlan{
			Target: target,
			Says: "the ffmpeg for " + target + " was not supplied: drop ffmpeg and ffprobe in " + paths.bundle +
				", prove them with go run ./cmd/build ffmpeg-check " + target + " on a " + target + " host, and pin what it prints in manifest.json",
			Skipped: true,
			Paths:   paths,
		}, nil
	}

	ffmpegDigest, ffprobeDigest := record.Programs["ffmpeg"], record.Programs["ffprobe"]

	if ffmpegDigest == "" || ffprobeDigest == "" {
		return ffmpegPlan{}, fmt.Errorf("the ffmpeg for %s is supplied and the manifest pins no digest for it: fill programs.ffmpeg and programs.ffprobe in %s with the digests go run ./cmd/build ffmpeg-check %s prints",
			target, filepath.Join(paths.bundle, "manifest.json"), target)
	}

	digests := map[string]string{}

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		body, err := os.ReadFile(drop[name])
		if err != nil {
			return ffmpegPlan{}, fmt.Errorf("the supplied %s for %s cannot be read: %w", name, target, err)
		}

		digests[name] = sha256Of(body)
	}

	if digests["ffmpeg"] != ffmpegDigest || digests["ffprobe"] != ffprobeDigest {
		return ffmpegPlan{}, fmt.Errorf("the supplied pair for %s is %s / %s and the manifest pins %s / %s: rebuild it on a %s host, run go run ./cmd/build ffmpeg-check %s, and pin the digests it prints",
			target, digests["ffmpeg"], digests["ffprobe"], ffmpegDigest, ffprobeDigest, target, target)
	}

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if err := checkSuppliedProgram(drop[name], target); err != nil {
			return ffmpegPlan{}, err
		}
	}

	check, err := readFfmpegCheck(repo, target, digests["ffmpeg"], digests["ffprobe"])
	if err != nil {
		return ffmpegPlan{}, err
	}

	return ffmpegPlan{
		Target: target,
		Says:   "adopting the ffmpeg supplied for " + target,
		Paths:  paths,
		Check:  check,
	}, nil
}

// planNativeFFmpeg decides what the compile of a supplied pair looks like when the build is standing on
// the platform that pair runs on.
//
// It is the same trimmed build the container targets make, from the same source the manifest pins, with
// the same recipe, and with the record's flags reaching configure the way they do there. What it does not
// have is a container: the host's own compiler, linker, stripper, make program, and signer are the
// toolchain, and the pair comes out of it as the platform's own binary — which is the whole reason the
// pair is compiled there and not shipped from a cross-compiler.
//
// Stripping comes before signing, and the order is not a style: strip rewrites the binary, and a signature
// computed over the bytes strip removed is a signature macOS refuses to run.
func planNativeFFmpeg(repo string, manifest bundle.Manifest, target string, record bundle.Target, paths ffmpegPaths, state ffmpegState) (ffmpegPlan, error) {
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

	if len(record.CFlags) > 0 {
		configure = append(configure, "--extra-cflags="+strings.Join(record.CFlags, " "))
	}

	if len(record.LDFlags) > 0 {
		configure = append(configure, "--extra-ldflags="+strings.Join(record.LDFlags, " "))
	}

	plan.Steps = append(plan.Steps, step{
		label:  "configure",
		result: "the trimmed build for " + target,
		dir:    repo,
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
		// is what the pair carries: it needs no developer identity, and it is the signature the pair the
		// server embeds has to have to start.
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
// the pair has to carry a signature to run.
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

// suppliedDropFor is where the maintainer's pair sits before this build adopts it.
func suppliedDropFor(paths ffmpegPaths, target string) map[string]string {
	drop := map[string]string{}

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		drop[name] = filepath.Join(paths.bundle, name+programSuffix(target))
	}

	return drop
}

// adoptSuppliedFFmpeg consumes the drop: the pair moves out of the directory a server binary embeds and
// into this build's own cache, gzipped.
//
// The pair has to leave that directory. //go:embed all: takes every file in it, so a raw pair left sitting
// next to the gzipped one is embedded as well, and every server built for that target carries about ten
// megabytes of program it will never use. The gzipped pair in the cache is the same pair: gzip is
// lossless, and the ad-hoc code signature a signed macOS pair carries is a hash over these bytes.
func adoptSuppliedFFmpeg(paths ffmpegPaths, target string) error {
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

	return nil
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
func configureStep(repo string, paths ffmpegPaths, image string, manifest bundle.Manifest, record bundle.Target) step {
	args := append(podmanRun(),
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
		name:   "podman",
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
// program the operator has to install, so that podman is the only new prerequisite this build adds.
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
// It reports the targets it has nothing to produce for: the three whose pair is compiled on its own
// platform. A target of that kind with nothing dropped for it is skipped, and the caller decides what a
// skipped target means for the run — the release run ships the archives it can ship, and a run that
// compiles one binary compiles none.
func (t tool) runFFmpeg(o options, stdout io.Writer) (bool, error) {
	manifest := bundle.ReadManifest()

	podman, err := t.resolve("podman")

	target := ffmpegTarget(o)

	paths := ffmpegPathsFor(t.repo, manifest, target)

	record, covered := manifest.Targets[target]

	// The tools of a compile that runs on the target's own platform are resolved the way podman is, and
	// for the same reason: the plan is made from what this machine has, and a host missing one of them is
	// told which one before the source is fetched.
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
		podmanIsThere:  err == nil && podman != "",
		sourceIsCached: aFileIsThere(paths.source),
		bundleIsCached: covered && aFileIsThere(paths.cachedProgram("ffmpeg.gz")) && aFileIsThere(paths.cachedProgram("ffprobe.gz")),
		nativeTools:    nativeTools,
	}

	plan, err := planFFmpeg(o, t.repo, manifest, state, thisMachine())
	if err != nil {
		return false, err
	}

	if plan.Says != "" {
		fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Says)
	}

	if plan.Native {
		return false, fmt.Errorf("the ffmpeg for %s is compiled on this machine, and this build does not run that compile yet", target)
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

			if err := fetchSource(plan.Source); err != nil {
				return false, err
			}
		}

		if err := checkSource(plan.Source.Path, manifest); err != nil {
			return false, err
		}

		for _, s := range plan.Steps {
			if err := t.execute(s); err != nil {
				return false, fmt.Errorf("%s: %w", s.label, err)
			}

			fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)
		}

		report, err := t.runFFMPEGCheck(podman, plan, manifest, record, target)
		if err != nil {
			return false, err
		}

		fmt.Fprintf(stdout, "%-7s %d conversions, %s s to %s s\n", "check", report.Conversions,
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

// runSuppliedFFmpeg does the work of a target this build compiles no ffmpeg for: nothing to fetch, nothing
// to compile, nothing to run, and no container to build.
//
// A target whose pair was not dropped is reported as skipped, and that is the whole of it: the message the
// plan printed says where to drop the pair, what proves it, and what pins it.
func (t tool) runSuppliedFFmpeg(plan ffmpegPlan, manifest bundle.Manifest, target string, stdout io.Writer) (bool, error) {
	if plan.Skipped {
		return true, nil
	}

	// A warm cache is the pinned pair, and the drop has already been consumed out of the directory:
	// there is nothing to adopt, and publishing from the cache is what makes the archive.
	if plan.Check.Target == target {
		if err := adoptSuppliedFFmpeg(plan.Paths, target); err != nil {
			return false, err
		}

		fmt.Fprintf(stdout, "%-7s %s\n", "adopt", plan.Paths.cached)
	}

	if err := publishFFmpeg(plan.Paths, manifest, target, suppliedFFMpegLicence(manifest, target, plan.Check)); err != nil {
		return false, err
	}

	fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Paths.bundle)

	return false, nil
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
func (t tool) runFFMPEGCheck(podman string, plan ffmpegPlan, manifest bundle.Manifest, record bundle.Target, target string) (checkReport, error) {
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
		inContainer(podman, image, plan.Paths.scratch),
		checkPrograms{FFmpeg: ffmpeg, FFProbe: ffprobe, Work: "/work"},
		plan.Paths.scratch,
	)
}

// containerRun is how the build reaches a binary it cannot run itself: the compiled ffmpeg lives in the
// mounted scratch directory, inside the container that compiled it. The image has to be named before the
// program, or podman reads the program as the image to run.
func containerRun(podman string, image string, scratch string, argv []string) []string {
	args := append([]string{podman}, podmanRun()...)
	args = append(args, "-i", "-v", scratch+":/work", image)

	return append(args, argv...)
}

// inContainer runs one program in that container, hands it a recording on its input, and hands back
// what it wrote.
func inContainer(podman string, image string, scratch string) command {
	return func(argv []string, stdin []byte) ([]byte, error) {
		args := containerRun(podman, image, scratch, argv)

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

// publishFFmpeg puts the pair, and what a release archive has to say about it, into the bundle directory
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

	text.WriteString(fmt.Sprintf(
		"\nThe same recipe, the same digest and the same toolchain are recorded in this repository at\ninternal/infrastructure/ffmpeg/bundle/manifest.json, and the command that produced this binary\nis `go run ./cmd/build ffmpeg %s`.\n",
		target,
	))

	return text.String()
}

// suppliedFFMpegLicence is what a release archive carries about a pair this build did not compile.
//
// A compiled pair is reproducible from this repository, so its notice points at the manifest and the
// command. A supplied pair is not: it was built by the project's maintainer, on the platform the target
// runs on, and the only record of it this repository holds is the digest the manifest pins and the proof
// the ffmpeg check left on that platform. The notice names those, and says plainly that the release build
// adopted the pair rather than produced it.
func suppliedFFMpegLicence(manifest bundle.Manifest, target string, check anFfmpegCheck) string {
	var text strings.Builder

	text.WriteString("# The ffmpeg this server carries\n\n")
	text.WriteString(fmt.Sprintf(
		"This build of rdio-scanner carries ffmpeg %s and ffprobe %s inside its own binary, compiled for %s\nby the project's maintainer on %s, the platform that target runs on. It converts audio with\nnothing installed on the host.\n\n",
		manifest.FFmpeg, manifest.FFmpeg, target, check.Host,
	))

	text.WriteString("ffmpeg is free software, copyright the FFmpeg developers. This binary is used under the GNU\nLesser General Public License version 2.1 or later, and was configured without --enable-gpl\nand without --enable-nonfree, so it carries no GPL or otherwise restricted component. The\nlicence text is at https://www.gnu.org/licenses/old-licenses/lgpl-2.1.html\n\n")

	text.WriteString("The source it was compiled from:\n\n")
	text.WriteString(fmt.Sprintf("    %s\n    sha256 %s\n\n", manifest.Source.URL, manifest.Source.SHA256))

	text.WriteString("The configure recipe it was built with:\n\n")

	for _, flag := range manifest.Recipe {
		text.WriteString("    " + flag + "\n")
	}

	text.WriteString("The programs this archive carries, by digest:\n\n")

	record := manifest.Targets[target]

	text.WriteString(fmt.Sprintf("    ffmpeg  sha256 %s\n    ffprobe sha256 %s\n\n", record.Programs["ffmpeg"], record.Programs["ffprobe"]))

	if check.Target == target {
		text.WriteString(fmt.Sprintf(
			"\nThis pair was not compiled by the release build: the machine that produces the releases cannot run a\n%s program. It adopted the pair the maintainer built, after checking it against the digest recorded\nin internal/infrastructure/ffmpeg/bundle/manifest.json, and on the evidence of the build's ffmpeg check,\nwhich ran for it on %s on %s and converted %d recordings.\n",
			target, check.Host, check.Checked, len(check.Conversions),
		))

		return text.String()
	}

	text.WriteString(fmt.Sprintf(
		"\nThis pair was not compiled by the release build: the machine that produces the releases cannot run a\n%s program. It adopted the pair the maintainer built, on the evidence of the build's ffmpeg check\nrecorded for it on its own platform, and the digest above is that evidence.\n",
		target,
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
