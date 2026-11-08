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

// runtimeTarget is the release target of the machine running this build,
// the target an operator who named none is building for. It is named the
// way the release names it, not the way go names that machine.
func runtimeTarget() string {
	return releaseTargetWordFor(runtime.GOOS, runtime.GOARCH)
}

// ffmpegState is what the tree already has, read before anything is decided.
type ffmpegState struct {
	// program is the container program this run speaks to, and it is empty when the machine has
	// neither: a plan made from an empty program is refused before a source is fetched.
	program        string
	sourceIsCached bool

	// bundleIsCached is a cache entry this run may reuse: the gzipped pair its recipe keys, and beside
	// that pair the licence text the run that compiled them wrote. The text is what says how those
	// binaries were proved, so an entry holding the pair without it is not reusable. pairIsCached says
	// separately that such a pair is there: the run that leaves it alone names what it found.
	bundleIsCached bool
	pairIsCached   bool

	// nativeTools is the host's own build tools, resolved: the program name mapped to where it was found,
	// and no entry for a program that is not there. Only a Mac compiling a macOS pair uses it, and it is read
	// before the plan is made so that a host missing a compiler is told which one before a minute of work
	// finds it out.
	nativeTools map[string]string

	// sysrootTree is the directory of the SDK tree a record pins, when this
	// machine has that tree prepared, and empty when it does not.
	// sysrootLookedIn is where the run looked either way: the directory named
	// with RDIO_FFMPEG_DARWIN_SDK, or the cache directory the SDK version names.
	sysrootTree     string
	sysrootLookedIn string

	// sysrootDigest is the digest the preparation of that tree recorded beside it, and it is empty when nothing is
	// recorded. It is read only for a tree that was found, and a tree whose recorded digest is not the digest the
	// record pins is a tree this build does not compile against.
	sysrootDigest string

	// compileCores is how many jobs the compile this plan describes may put to work at once, and compileCoreHost
	// names the machine that count is the count of: the container machine for a compile that runs in a container,
	// this machine for one that runs on the host's own toolchain. The run asks once, before the plan is made, so
	// that the plan's compile and the machine it runs on cannot disagree about how many cores there are.
	//
	// An empty count beside a named machine is a machine that reports nothing, and a plan for it is refused rather
	// than the tool guessing. Both empty is a plan made without asking, which is a compile on this machine, and it
	// uses this machine's own count.
	compileCores    string
	compileCoreHost string
}

// theCompileCanCheckOutHowManyCores is the job count for the compile this plan describes: the count named with
// the machine, when the run named one; this machine's own count otherwise. An empty answer is the case with no
// count to use, and the plan that asks for it refuses.
func (s ffmpegState) theCompileCanCheckOutHowManyCores() string {
	if s.compileCoreHost != "" || s.compileCores != "" {
		return s.compileCores
	}

	return strconv.Itoa(runtime.NumCPU())
}

// refusesToPlanACompileOnAMachineThatNamesNoCores is that refusal: a run that named the machine the compile runs
// on, and got no count from it, has no job count to plan with. The sentence names the program and the form it was
// asked with, because "no count" is what an operator sees whether the machine has no cores to name or the tool
// asked that program in a vocabulary it does not answer.
func (s ffmpegState) refusesToPlanACompileOnAMachineThatNamesNoCores() (string, bool) {
	if s.compileCoreHost != "" && s.compileCores == "" {
		refusal := "the " + s.compileCoreHost + " reports no CPU count to compile with"

		if s.program != "" && s.compileCoreHost == theContainerMachineNamedBy(s.program) {
			ask := append([]string{s.program}, theContainerProgram{name: s.program}.coresAsk()...)

			refusal += ", after being asked with " + strings.Join(ask, " ")
		}

		return refusal, true
	}

	return "", false
}

// host is the platform the machine running the build is on, named the way the release names a platform: the
// ffmpeg of a macOS target runs on one platform alone, and the question the build asks of this one is whether
// it is that platform, because only there the host's own compiler, linker, and signer are the toolchain.
type host struct {
	goos   string
	goarch string

	// cpus is how many cores this machine has to compile on, as it reports them. A machine that reports no
	// count leaves it 0, and a compile planned for that machine is told so rather than the tool choosing a
	// job count of its own.
	cpus int
}

// thisMachine is the platform the build is running on, with the core count of the machine this process runs on.
func thisMachine() host {
	return host{goos: runtime.GOOS, goarch: runtime.GOARCH, cpus: runtime.NumCPU()}
}

// howManyCoresCanACompileOnMeCheckOut is the number of jobs a compile that runs on this machine's own toolchain
// may put to work at once, as this machine reports them. A description of a machine that names no count is this
// process's own machine, and its count is the count a compile on it can use.
func (h host) howManyCoresCanACompileOnMeCheckOut() string {
	if h.cpus <= 0 {
		return strconv.Itoa(runtime.NumCPU())
	}

	return strconv.Itoa(h.cpus)
}

// thisBuildsOwnMachine is how the run names the machine the tool process itself runs on, when it says which
// machine a job count is the count of.
const thisBuildsOwnMachine = "this build's own machine"

// theContainerMachineNamedBy is how the run names the machine the container a compile runs in runs on: the
// machine the container program talks to, which is a different machine from this one whenever the container
// engine runs in a VM.
func theContainerMachineNamedBy(program string) string {
	return program + " machine"
}

// howManyCoresCanTheMachineCompilingHereCheckOut is how many cores the machine that compiles this target's ffmpeg
// has to put to work, and which machine that count is the count of. A compile in a container is counted by the
// container machine, asked once through the engine that runs it; a compile on the host's own toolchain is counted
// by this machine, which is the machine that runs that toolchain.
//
// Asking the engine, and not this process, is the point: the container machine is a different machine from this
// one, and a job count taken from this one oversubscribes it. An engine that reports nothing leaves the count
// empty for the plan to refuse; there is no fallback to this machine's count.
func (t tool) howManyCoresCanTheMachineCompilingHereCheckOut(target string, program string, machine host) (string, string) {
	if machine.canCompile(target) {
		return machine.howManyCoresCanACompileOnMeCheckOut(), thisBuildsOwnMachine
	}

	named := theContainerMachineNamedBy(program)

	if program == "" {
		return "", named
	}

	report, err := t.theRun(append([]string{program}, theContainerProgram{name: program}.coresAsk()...), nil)
	if err != nil {
		return "", named
	}

	cpus, err := strconv.Atoi(strings.TrimSpace(string(report)))
	if err != nil || cpus <= 0 {
		return "", named
	}

	return strconv.Itoa(cpus), named
}

// canCompile says whether this machine can build a target's ffmpeg with its
// own toolchain: only a Mac building a macOS target, either architecture
// from either Mac. Every other target is compiled in a container.
func (h host) canCompile(target string) bool {
	if h.goos != "darwin" {
		return false
	}

	tr, found := targetFor(target)

	return found && tr.goos == "darwin"
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

	// Host says the binaries this plan produces come from the machine running the build: the pair it compiled
	// with its own toolchain, the pair a maintainer dropped for it, or the cache entry its own run published.
	// A plan that is not Host compiles the pair in a container, on whatever machine runs the build.
	Host bool

	// Native says this plan compiles the ffmpeg binaries with the toolchain of the machine running the build,
	// because that machine is a host of the operating system they run on. It is the answer to the operator who
	// asked for the ffmpeg of the machine they are standing on, and it is never a plan that produces nothing:
	// the compile is what that run asked for.
	Native bool

	// Adopts says the plan is taking binaries a maintainer dropped in the target's bundle directory,
	// rather than compiling them. The run that adopts them runs them first, through the same gate a
	// compile runs its own binaries through.
	Adopts bool

	// Reuses says the plan is taking the gzipped pair a previous run left in this manifest's cache
	// entry for the target. Nothing in such a plan compiles or runs anything, so what the release says
	// about those binaries is the text the run that compiled them left in that entry.
	Reuses bool

	// WritesNone says this plan produces no ffmpeg binaries for its target:
	// an input the record pins is not there to read. Says carries the sentence
	// naming what is missing and where to prepare it. A release run reports
	// such a target as one it wrote no archive for and ships the rest.
	WritesNone bool

	Source ffmpegSource
	Paths  ffmpegPaths

	// Image is the container the target is compiled in, and Runner is the container its compiled
	// program is checked in, which is the same one unless the target needs an operating system the
	// build container does not have.
	Image  string
	Runner string

	// Reads are the things this target's record says to read out of the pair it compiled, and is empty for
	// every target whose proof is a run. A plan that names reads builds no runner container, and the run
	// spends the step the others spend running on reading instead.
	Reads []string
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

// ffmpegScratch is what the ffmpeg phase writes and no later build reads:
// its configured trees, extracted tarball, container context, and working
// binaries. The cache beside them is what later builds read, and a clean
// run leaves it standing.
func ffmpegScratch(repo string) []string {
	root := filepath.Join(repo, ".build", "ffmpeg")

	names := []string{"scratch", "src", "context", "run"}

	paths := make([]string, 0, len(names))

	for _, name := range names {
		paths = append(paths, filepath.Join(root, name))
	}

	return paths
}

// pruneFFMPEGCache removes the cache entries the manifest as it is now
// cannot ask for, keyed off release, source, target, recipe, toolchain and
// check environment, plus whole cache directories of old releases.
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

// cacheKey is what has to be the same for a compiled ffmpeg to be reused:
// the release, the pinned source, the target, the configure recipe, and
// that target's toolchain and check environment as its record names them,
// including the machine that toolchain is a set of packages for.
func cacheKey(manifest bundle.Manifest, target string) string {
	record := manifest.Targets[target]

	parts := []string{
		manifest.FFmpeg,
		manifest.Source.URL,
		manifest.Source.SHA256,
		target,
		strings.Join(manifest.Recipe, "\n"),
		record.Container,
		// The machine the container's toolchain runs on: a distribution's cross compilers are packages of one
		// machine, so the same image name and package list installed on another machine is another toolchain.
		record.CompilePlatform,
		strings.Join(record.Packages, " "),
		strings.Join(record.Cross, " "),
		strings.Join(record.CFlags, " "),
		strings.Join(record.LDFlags, " "),
		record.Conformance.Container,
		strings.Join(record.Conformance.Packages, " "),
		strings.Join(record.Conformance.Run, " "),
	}

	// The digest a record pins an image at belongs in the key beside the name:
	// another digest is another toolchain, and an entry warmed under the old
	// one holds binaries built with compilers nobody named. A record naming no
	// digest keys exactly as it keyed before.
	if record.ContainerDigest != "" {
		parts = append(parts, record.ContainerDigest)
	}

	// The container a pair is proved in is part of what an entry describes, so its digest moves the entry too:
	// binaries proved against another image were proved against another environment.
	if record.Conformance.Digest != "" {
		parts = append(parts, record.Conformance.Digest)
	}

	// The three inputs a compile that reads another operating system's tree adds to that list. A record
	// naming none of them keys exactly as it keyed before, so the targets compiled with the build container's
	// own headers and libraries keep the warm entries they already have, and the one that does name them moves
	// whenever the toolchain, the sysroot, or the compiler moves.
	if record.Containerfile != "" {
		parts = append(parts, record.Containerfile)
	}

	// The image a sysroot is copied out of and the digest it is pinned at are one input read two ways: the name
	// says which image, the digest says which content of it. A record naming no sysroot names no digest either,
	// and keys as it keyed before either field existed.
	if record.SysrootImage != "" {
		parts = append(parts, record.SysrootImage)

		if record.SysrootDigest != "" {
			parts = append(parts, record.SysrootDigest)
		}
	}

	if record.CC != "" {
		parts = append(parts, record.CC)
	}

	// The archiver and the indexer are part of the name for the same reason the compiler is: a pair whose
	// libraries were archived and indexed by another tool is another pair of binaries. A record naming
	// neither keys exactly as it keyed before, so the compiles that use the archiver their own container
	// ships keep the warm entries they already have.
	if record.AR != "" {
		parts = append(parts, record.AR)
	}

	if record.Ranlib != "" {
		parts = append(parts, record.Ranlib)
	}

	// A pair that never went through a strip step is another pair of binaries than one that did, so the switch
	// that kept the strip out is part of the name those binaries are cached under. A record that names no such
	// switch keys exactly as it keyed before, which is what keeps every cached entry of the other six targets
	// warm.
	if record.NoStripping {
		parts = append(parts, "--disable-stripping")
	}

	// A sysroot somebody prepared outside both repositories is a tree the compile read: the SDK version, the
	// digest of the tree, and the tool that prepared it are part of the name of the binaries built from it.
	// The mount path is not, because the record names it in its cc, cflags, and ldflags, and a record whose
	// mount disagreed with its own flags is a record the build cannot honour.
	if record.Sysroot != (bundle.Sysroot{}) {
		parts = append(parts, record.Sysroot.SDK, record.Sysroot.SHA256, record.Sysroot.Generator)
	}

	// What a host compile runs is part of the name of the entry its binaries land in, too. The binaries
	// themselves are not: the cache is this machine's own tree, and what this machine compiled is what sits
	// in it.
	if len(record.Host.CFlags) > 0 || len(record.Host.LDFlags) > 0 || record.Host.Make != "" {
		parts = append(parts, strings.Join(record.Host.CFlags, " "), strings.Join(record.Host.LDFlags, " "), record.Host.Make)
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

// theCacheEntryNamingNoProofOfItsPair is what the run says when the cache holds one target's pair and
// nothing that says how those binaries were proved. Such an entry is left where it is and the pair is
// compiled again: a release that carries binaries cannot stay silent about nobody having proved them.
func theCacheEntryNamingNoProofOfItsPair(target string, state ffmpegState) string {
	if state.pairIsCached && !state.bundleIsCached {
		return "the cache entry for " + target + " names no proof of the pair it holds, so that pair is compiled again"
	}

	return ""
}

// planFFmpeg decides what the ffmpeg phase does for one target: a target
// with no record is not a failure, and one with a record is a promise a
// build that cannot keep it says so about.
func planFFmpeg(o options, repo string, manifest bundle.Manifest, state ffmpegState, machine host) (ffmpegPlan, error) {
	target := ffmpegTarget(o)

	record, covered := manifest.Targets[target]
	if !covered {
		return ffmpegPlan{Target: target, Says: "no ffmpeg is compiled for " + target}, nil
	}

	// A record names one proof for the pair its target compiles: the container that runs it through the
	// converter's own argument list, or the things to read out of the files it produced. Naming both would
	// leave this build to choose which proof the release ships, and the cheaper one is the choice a build
	// under pressure makes.
	if len(record.Conformance.Reads) > 0 && (len(record.Conformance.Run) > 0 || record.Conformance.Container != "") {
		return ffmpegPlan{}, fmt.Errorf("the %s record names two proofs at once: its conformance run %q, its conformance container %q, and its conformance reads %q. A record names one proof, and this build will not guess which one %s means",
			target, strings.Join(record.Conformance.Run, " "), record.Conformance.Container, strings.Join(record.Conformance.Reads, " "), target)
	}

	// A record's packages are the container distribution's own builds, which means they were built to run on
	// one machine: Debian's arm64 archive carries no gcc-multilib or libc6-dev-i386, and its binutils cannot
	// read the object files an amd64-hosted cross compiler writes. Without a platform the compile runs on the
	// container host's own machine and installs whatever toolchain that one carries, and the build finds that
	// out only after the compile has spent its time.
	if record.CompilePlatform == "" {
		return ffmpegPlan{}, fmt.Errorf("the %s record names no compile_platform: the packages it installs are the container distribution's builds for one machine, and this build will not compile %s with whatever toolchain the container host happens to carry",
			target, target)
	}

	paths := ffmpegPathsFor(repo, manifest, target)

	// The one host that compiles a target's ffmpeg with its own toolchain takes the host route, and the record
	// is not what decides: a macOS target is compiled in a container on the machine that produces the releases,
	// and with a Mac's own compiler, linker, and signer on a Mac.
	if machine.canCompile(target) {
		plan, err := planHostFFmpeg(repo, manifest, target, record, paths, state, machine)

		if err == nil && plan.Says == "" {
			plan.Says = theCacheEntryNamingNoProofOfItsPair(target, state)
		}

		return plan, err
	}

	source := ffmpegSource{Path: paths.source, URL: manifest.Source.URL}

	if state.bundleIsCached {
		source.Fetch = false

		return ffmpegPlan{
			Target: target,
			Says:   "reusing the ffmpeg compiled for " + target,
			Reuses: true,
			Source: source,
			Paths:  paths,
		}, nil
	}

	if state.program == "" {
		return ffmpegPlan{}, fmt.Errorf("compiling the ffmpeg for %s needs a container program: it looked for %s and %s and this machine has neither",
			target, podmanProgram, dockerProgram)
	}

	// The compile puts as many jobs to work as the machine it runs on has cores, and the run asked that machine
	// before planning. A machine that answers with no count is a machine this build cannot size its compile to,
	// and counting the cores of the machine running the tool instead is how the container ends up with more jobs
	// than it has cores.
	if refusal, refuses := state.refusesToPlanACompileOnAMachineThatNamesNoCores(); refuses {
		return ffmpegPlan{}, fmt.Errorf("compiling the ffmpeg for %s needs the CPU count of the machine its container runs on: %s", target, refusal)
	}

	// A record that pins an SDK names a tree this machine has to have prepared, and the plan is where that is
	// found out. A run that carried on reached configure with nothing mounted at the path its own flags name, and
	// what it reported was clang's complaint about being unable to create an executable file — a compiler's own
	// words for a missing tree, with nothing in them naming the tree the operator was meant to prepare.
	if record.Sysroot != (bundle.Sysroot{}) && state.sysrootTree == "" {
		return theRefusalAnswered(o, target, theRefusalOfAnUnpreparedSysroot(target, record, state.sysrootLookedIn))
	}

	// A tree standing where the pinned tree is kept is the pinned tree only
	// when the digest its preparation recorded beside it is the digest the
	// record names: a swapped or hand-assembled tree would let a release carry
	// a provenance that is a story. The refusal matches the absent tree's.
	if record.Sysroot != (bundle.Sysroot{}) && state.sysrootTree != "" && state.sysrootDigest != record.Sysroot.SHA256 {
		return theRefusalAnswered(o, target, theRefusalOfASysrootThatIsNotThePinnedTree(target, record, state.sysrootTree, state.sysrootDigest))
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
		Says:   theCacheEntryNamingNoProofOfItsPair(target, state),
		Source: source,
		Paths:  paths,
		Image:  image,
		Runner: runner,
		Reads:  record.Conformance.Reads,
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
		args:   buildImageArgs(image, theContainerfileFor(record), theImageTheRecordNames(record.Container, record.ContainerDigest), record.CompilePlatform, theImageTheRecordNames(record.SysrootImage, record.SysrootDigest), packages),
	})

	if runner != image {
		// A check container has no record of its own: it is the shared toolchain file with the packages the
		// record's conformance names, and it has no sysroot to copy because it runs the program rather than
		// compiling it. The image it is built from is pinned by the digest that same record names, because a
		// pair proved against another image is a different proof.
		plan.Steps = append(plan.Steps, step{
			label:  "runner",
			result: runner,
			dir:    repo,
			name:   program.name,
			args:   buildImageArgs(runner, sharedContainerfile, theImageTheRecordNames(record.Conformance.Container, record.Conformance.Digest), record.CompilePlatform, "", record.Conformance.Packages),
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

	plan.Steps = append(plan.Steps, configureStep(repo, paths, image, manifest, record, program, state.sysrootTree))

	// The compile itself reads the same tree configure read: the headers it includes, the libraries it links
	// against and the startup files the link names all come out of it, and the mount stays read-only because the
	// tree is the operator's, prepared once and read by every compile.
	makeArgs := append(program.run(), "-v", paths.scratch+":/work")

	makeArgs = append(makeArgs, sysrootMount(record, state.sysrootTree)...)

	makeArgs = append(makeArgs,
		"-w", "/work/"+sourceDir(manifest),
		image,
		// The job count is the container machine's core count, which the run asked the engine for: this
		// process's own core count belongs to a different machine whenever the engine runs in a VM.
		"make", "-j", state.theCompileCanCheckOutHowManyCores(),
	)

	plan.Steps = append(plan.Steps, step{
		label:  "make",
		result: whatTheCompileProduced(record, target),
		dir:    repo,
		name:   program.name,
		args:   makeArgs,
	})

	// The tree the container compiled in belongs to a uid this build cannot
	// name, so it is made readable, writable and traversable by everybody in
	// it rather than chasing which uid turned out to be which.
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

// planHostFFmpeg decides the ffmpeg of a target whose host is this
// machine. Dropped binaries win over a fresh compile every time, and a
// drop is held to the same reads, gate and run as a compiled pair.
func planHostFFmpeg(repo string, manifest bundle.Manifest, target string, record bundle.Target, paths ffmpegPaths, state ffmpegState, machine host) (ffmpegPlan, error) {
	// The cache key is the manifest's record and the target, so a warm cache is the entry this machine's
	// own run published, and the drop is not needed again: it has already been consumed out of this
	// directory.
	if state.bundleIsCached {
		return ffmpegPlan{
			Target: target,
			Says:   "reusing the ffmpeg built for " + target,
			Host:   true,
			Reuses: true,
			Paths:  paths,
		}, nil
	}

	drop := suppliedDropFor(paths, target)

	if !aFileIsThere(drop["ffmpeg"]) && !aFileIsThere(drop["ffprobe"]) {
		// Nothing was dropped, and this machine is a host of the operating system the binaries run on: the
		// compile is what only such a host can do, and it is what the operator asked for when they asked for
		// the ffmpeg of the machine they are on. A Mac of the other architecture is such a host, because one
		// macOS toolchain produces both architectures.
		return planNativeFFmpeg(repo, manifest, target, record, paths, state, machine)
	}

	// The binaries a maintainer dropped are read here, and run by the run that adopts them, on this machine,
	// through the same gate the compile runs its own binaries through. What this reads is the header and the load
	// commands that target's own linker writes, because a run cannot say those things across the two macOS
	// architectures: an Apple Silicon Mac starts arm64 and x86-64 programs alike.
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if !aFileIsThere(drop[name]) {
			return ffmpegPlan{}, fmt.Errorf("the ffmpeg for %s is supplied and %s has no %s: drop ffmpeg and ffprobe in %s, or build them on a %s host with go run ./cmd/build ffmpeg %s",
				target, paths.bundle, name, paths.bundle, target, target)
		}

		if err := checkSuppliedProgram(drop[name], target, record); err != nil {
			return ffmpegPlan{}, err
		}
	}

	return ffmpegPlan{
		Target: target,
		Says:   "adopting the ffmpeg supplied for " + target,
		Adopts: true,
		Host:   true,
		Paths:  paths,
	}, nil
}

// planNativeFFmpeg decides the compile of the ffmpeg binaries when the
// build stands on a host of the operating system they run on: the same
// trimmed build, with the host's own tools as the toolchain. Stripping
// comes before signing: a signature over bytes strip removed is refused.
func planNativeFFmpeg(repo string, manifest bundle.Manifest, target string, record bundle.Target, paths ffmpegPaths, state ffmpegState, machine host) (ffmpegPlan, error) {
	if err := checkNativeToolchain(record, target, state); err != nil {
		return ffmpegPlan{}, err
	}

	work := filepath.Join(paths.scratch, sourceDir(manifest))

	source := ffmpegSource{Path: paths.source, URL: manifest.Source.URL, Fetch: !state.sourceIsCached}

	plan := ffmpegPlan{
		Target: target,
		Native: true,
		Host:   true,
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
	// the compiler and linker flags. On a Mac compiling a macOS target with the architecture it stands on
	// the compiler's default is that architecture, and none of the three is named.
	compilerArch := theCompilerArchitectureTheHostMustProduce(target, machine)

	cflags := append([]string{}, record.Host.CFlags...)
	ldflags := append([]string{}, record.Host.LDFlags...)

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
		// This compile runs on the machine planning it, so its own core count is the right one to put to work.
		args: []string{"-j", machine.howManyCoresCanACompileOnMeCheckOut()},
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

// makeProgram is the make program the compile a Mac runs for a macOS target. A record naming none runs make, which
// is what the Command Line Tools carry; a container's compile is not asked, because the image installs the GNU
// make ffmpeg's build asks for.
func makeProgram(record bundle.Target) string {
	if record.Host.Make == "" {
		return "make"
	}

	return record.Host.Make
}

// nativeProgramsAre the programs a Mac compiling a macOS pair runs with its own toolchain: the archive reader,
// a C compiler, the stripper, and the make program the record names, plus codesign, because a macOS program has
// to carry a signature to start.
func nativeProgramsAre(record bundle.Target, target string) []string {
	programs := []string{"tar", "cc", "strip", makeProgram(record)}

	if strings.HasPrefix(target, "macos-") {
		programs = append(programs, "codesign")
	}

	return programs
}

// checkNativeToolchain asks for the host's own build tools before a step
// runs and names the missing program and where it comes from, so the
// build never spends a minute finding out what it could say at once.
func checkNativeToolchain(record bundle.Target, target string, state ffmpegState) error {
	for _, name := range nativeProgramsAre(record, target) {
		if state.nativeTools[name] == "" {
			return fmt.Errorf("compiling the ffmpeg for %s needs %s, which this machine does not have: the Xcode Command Line Tools carry it",
				target, name)
		}
	}

	return nil
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

// adoptSuppliedFFmpeg consumes the drop: the binaries move out of the
// directory a server binary embeds and into this build's cache. They have
// to leave because go:embed takes every file: a raw program left there is
// embedded too.
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

// theLicenceInTheCache is the licence text the run that compiled one cache entry's pair wrote beside
// those binaries. It is the only thing in the entry that says how they were proved, so an entry
// without it is named and refused: the run that reuses binaries cannot compose a statement about a
// compile it did not see.
func theLicenceInTheCache(paths ffmpegPaths, target string) (string, error) {
	body, err := os.ReadFile(filepath.Join(paths.cached, "LICENCE.md"))
	if err == nil {
		return string(body), nil
	}

	if !os.IsNotExist(err) {
		return "", fmt.Errorf("the licence text in %s cannot be read: %w", paths.cached, err)
	}

	return "", fmt.Errorf("the cache entry %s holds the ffmpeg pair for %s with no licence text beside it: nothing there says how those binaries were proved, and this run did not compile them", paths.cached, target)
}

// buildImageArgs prepares the container one target is compiled or checked
// in, from an empty context: the image is a toolchain, and nothing of this
// repository belongs in it.
func buildImageArgs(image string, file string, base string, platform string, sysrootImage string, packages []string) []string {
	args := []string{
		"build",
		"-t", image,
		"-f", file,
		// The base is a multi-architecture image, and a stage that names no machine is resolved by the
		// container host to its own. A cross toolchain is a package of one machine, so the machine is named.
		"--build-arg", "compile_platform=" + platform,
		"--build-arg", "base=" + base,
	}

	if sysrootImage != "" {
		args = append(args, "--build-arg", "sysroot_image="+sysrootImage)
	}

	args = append(args,
		"--build-arg", "packages="+strings.Join(packages, " "),
		filepath.Join(".build", "ffmpeg", "context"),
	)

	return args
}

// sharedContainerfile is the file every target that compiles without another operating system's sysroot is
// built from, and the answer for a record that names no file of its own.
const sharedContainerfile = "Containerfile.ffmpeg"

// theContainerfileFor is the file one target's toolchain image is built from, which its record names because
// the file is what makes the compile that target's record describes possible.
func theContainerfileFor(record bundle.Target) string {
	if record.Containerfile != "" {
		return record.Containerfile
	}

	return sharedContainerfile
}

// theImageTheRecordNames is the reference the build hands the container
// program: the name the record writes, at the digest it pins. The tag
// alone can point somewhere else tomorrow, and a compile asked of wherever
// it points is another toolchain wearing this target's name.
func theImageTheRecordNames(name string, digest string) string {
	if digest == "" {
		return name
	}

	return name + "@" + digest
}

// darwinSysrootVariable names the directory an operator keeps a prepared sysroot in when they keep it somewhere
// other than the directory this build looks in. It names the SDK tree itself, because the tree is what the
// container mounts.
const darwinSysrootVariable = "RDIO_FFMPEG_DARWIN_SDK"

// thePreparedSysrootFor answers where the sysroot tree a record pins is
// on this machine: the cache directory for the pinned SDK version, or the
// operator named one. It is reported only when the directory carries.
func thePreparedSysrootFor(record bundle.Target, look func(string) (string, bool), homeDir string) (tree string, lookedIn string) {
	lookedIn, named := look(darwinSysrootVariable)

	if !named || lookedIn == "" {
		lookedIn = filepath.Join(homeDir, ".cache", "rdio-scanner", "ffmpeg-darwin-sdk",
			record.Sysroot.SDK, "MacOSX"+record.Sysroot.SDK+".sdk")
	}

	if !aFileIsThere(filepath.Join(lookedIn, "usr", "lib", "libSystem.tbd")) {
		return "", lookedIn
	}

	return lookedIn, lookedIn
}

// theSysrootDigestFile is the name the preparation of a sysroot tree
// records its digest under, beside the tree itself.
const theSysrootDigestFile = "sha256"

// theSysrootDigestRecordedBeside answers what the preparation of a sysroot
// tree recorded for it. Nothing inside the tree names it, and a tree with
// nothing recorded beside it has no measured digest: empty, not invented.
func theSysrootDigestRecordedBeside(tree string) string {
	if tree == "" {
		return ""
	}

	body, err := os.ReadFile(filepath.Join(filepath.Dir(tree), theSysrootDigestFile))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(body))
}

// theRefusalOfAnUnpreparedSysroot is what the build says instead of
// compiling a pair against a tree it does not have. It names the target,
// the SDK version the record pins, the directory it looked in, and how to
// prepare that tree.
func theRefusalOfAnUnpreparedSysroot(target string, record bundle.Target, lookedIn string) error {
	where := "the cache directory of the machine running the build"

	if lookedIn != "" {
		where = lookedIn
	}

	return fmt.Errorf("the ffmpeg for %s is compiled against the macOS %s SDK its record pins, and this machine has no prepared sysroot at %s: prepare that tree, or name the directory you keep it in with %s",
		target, record.Sysroot.SDK, where, darwinSysrootVariable)
}

// theRefusalOfASysrootThatIsNotThePinnedTree is what the build says
// instead of compiling against a tree standing where the pinned tree is
// kept and not that tree: both a wrong digest and no digest would let
// binaries ship under a digest nothing read, so both are refused.
func theRefusalOfASysrootThatIsNotThePinnedTree(target string, record bundle.Target, tree, recorded string) error {
	if recorded == "" {
		return fmt.Errorf("the ffmpeg for %s is compiled against the macOS %s SDK its record pins, and the tree at %s has no digest recorded beside it, where its preparation records one in a file named %s, and the digest its record names for that tree is %s: re-prepare that tree, or name the directory you keep it in with %s",
			target, record.Sysroot.SDK, tree, theSysrootDigestFile, record.Sysroot.SHA256, darwinSysrootVariable)
	}

	return fmt.Errorf("the ffmpeg for %s is compiled against the macOS %s SDK its record pins, and the digest the tree at %s records beside it is %s, not the %s its record names for that tree: re-prepare that tree, or name the directory you keep it in with %s",
		target, record.Sysroot.SDK, tree, recorded, record.Sysroot.SHA256, darwinSysrootVariable)
}

// theRefusalAnswered hands a refusal of a compile to the run that has to
// answer it, in the shape that run can. A release run reports this target
// as one it wrote no archive for and carries on to the rest; a run asked
// for this one target's ffmpeg is refused outright, not exit 0.
func theRefusalAnswered(o options, target string, refusal error) (ffmpegPlan, error) {
	if o.sub == subDist {
		return ffmpegPlan{Target: target, Says: refusal.Error(), WritesNone: true}, nil
	}

	return ffmpegPlan{}, refusal
}

// sysrootMount is how a prepared sysroot reaches the container that
// compiles against it: mounted where the record says the compiler looks,
// read-only. The compile writes nothing to it, and a mount keeps the tree
// one copy on the machine instead of a layer of every toolchain image.
func sysrootMount(record bundle.Target, tree string) []string {
	if tree == "" || record.Sysroot == (bundle.Sysroot{}) {
		return nil
	}

	return []string{"-v", tree + ":" + record.Sysroot.Mount + ":ro"}
}

// whatTheCompileProduced is what the compile step tells the operator it made. A record that proves its pair
// by reading it has a pair nothing on this machine ran, and the step that compiled it says so; a record that
// proves its pair by running it spends that claim on the run that follows.
func whatTheCompileProduced(record bundle.Target, target string) string {
	if len(record.Conformance.Reads) > 0 {
		return "ffmpeg and ffprobe for " + target + ", compiled and not run"
	}

	return "ffmpeg and ffprobe for " + target
}

// configureStep is the trimmed build asked of the source: the recipe, then
// what makes the compile cross to this target, then the flags that link it
// statically.
func configureStep(repo string, paths ffmpegPaths, image string, manifest bundle.Manifest, record bundle.Target, program theContainerProgram, sysroot string) step {
	args := append(program.run(), "-v", paths.scratch+":/work")

	args = append(args, sysrootMount(record, sysroot)...)

	args = append(args,
		"-w", "/work/"+sourceDir(manifest),
		image,
		"./configure",
	)

	args = append(args, manifest.Recipe...)
	args = append(args, record.Cross...)

	// A compile reading another operating system's headers, libraries and startup files names the compiler
	// that can do it, target triple and sysroot included. The invocation stays one configure argument: the
	// container is asked for the program directly, with no shell between them to split it into words, and
	// configure is the one that hands the whole thing to the compile.
	if record.CC != "" {
		args = append(args, "--cc="+record.CC)
	}

	if len(record.CFlags) > 0 {
		args = append(args, "--extra-cflags="+strings.Join(record.CFlags, " "))
	}

	if len(record.LDFlags) > 0 {
		args = append(args, "--extra-ldflags="+strings.Join(record.LDFlags, " "))
	}

	// The archiver a record names reaches the compile through configure's own options, which is where a
	// toolchain the record names belongs: configure writes both into the Makefile it produces, that Makefile
	// archives every library with the named tool and asks the named indexer for the archive's symbol table.
	if record.AR != "" {
		args = append(args, "--ar="+record.AR)
	}

	if record.Ranlib != "" {
		args = append(args, "--ranlib="+record.Ranlib)
	}

	// And the switch that keeps the programs their link wrote: configure answers --disable-stripping by leaving
	// its own strip step to a program that prints what it skipped, so the shipped program is the one the link
	// wrote, signature and all.
	if record.NoStripping {
		args = append(args, "--disable-stripping")
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

// checkSource is the integrity anchor of this feature: the digest
// the manifest recorded is what every fetch checks against.
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

// fetchSource puts the release tarball in the build cache, fetched by
// the tool itself, so a container program stays the only prerequisite.
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

// runFFmpeg produces the ffmpeg a server binary built on this machine
// carries. It reports whether it produced nothing for the target, which
// is the answer the release run asks: a target that wrote no ffmpeg
// writes no archive, and the plan says what input was missing.
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

	// The pair and the proof of the pair are looked for together, and the entry is reusable only when
	// the run that compiled them left its own licence text beside them.
	pairIsCached := covered && aFileIsThere(paths.cachedProgram("ffmpeg.gz")) && aFileIsThere(paths.cachedProgram("ffprobe.gz"))

	licenceIsCached := pairIsCached && aFileIsThere(filepath.Join(paths.cached, "LICENCE.md"))

	bundleIsCached := pairIsCached && licenceIsCached

	// The tools of the compile a Mac runs for a macOS pair are resolved the way the container program is, and
	// for the same reason: the plan is made from what this machine has, and a host missing one of them is told
	// which one before the source is fetched.
	nativeTools := map[string]string{}

	if covered && machine.canCompile(target) {
		for _, name := range nativeProgramsAre(record, target) {
			where, err := t.resolve(name)

			if err == nil && where != "" {
				nativeTools[name] = where
			}
		}
	}

	// A compile that reads another operating system's headers, libraries and startup files reads them out of a tree
	// the operator prepared, and whether this machine has that tree is asked here for the same reason the container
	// program and the host's own tools are asked here: the plan is made from what this machine has, and a machine
	// that has not prepared the tree is told which SDK is missing before it builds a container. A Mac compiling its
	// own ffmpeg reads the SDK its own toolchain carries, and is asked for nothing. Nothing is asked of a machine
	// whose cache already holds the pair, because that compile does not happen again.
	sysrootTree, sysrootLookedIn := "", ""

	if covered && !bundleIsCached && record.Sysroot != (bundle.Sysroot{}) && !machine.canCompile(target) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return false, fmt.Errorf("the ffmpeg for %s is compiled against the macOS %s SDK its record pins, and this machine has no home directory a prepared copy of it is kept in: name the directory you keep it in with %s",
				target, record.Sysroot.SDK, darwinSysrootVariable)
		}

		sysrootTree, sysrootLookedIn = thePreparedSysrootFor(record, os.LookupEnv, homeDir)
	}

	// The machine the compile runs on is asked how many cores it has, once, here: the plan it is planning is made
	// from that answer, and asking after the compile has started is a question answered too late to plan with. A
	// target whose pair is already cached compiles nothing, and is asked for nothing.
	compileCores, compileCoreHost := "", ""

	if covered && !bundleIsCached {
		compileCores, compileCoreHost = t.howManyCoresCanTheMachineCompilingHereCheckOut(target, program, machine)
	}

	state := ffmpegState{
		program:         program,
		sourceIsCached:  aFileIsThere(paths.source),
		bundleIsCached:  bundleIsCached,
		pairIsCached:    pairIsCached,
		nativeTools:     nativeTools,
		sysrootTree:     sysrootTree,
		sysrootLookedIn: sysrootLookedIn,

		// The job count the compile plans with is the count of the machine the compile runs on, asked once here so
		// the plan and that machine cannot disagree about how many cores there are: the container machine for a
		// compile in a container, this machine for one on the host's own toolchain. A target whose pair is already
		// cached compiles nothing, and is asked for nothing.
		compileCores:    compileCores,
		compileCoreHost: compileCoreHost,

		// A tree that was found is asked what its preparation recorded for it, in the same breath: the directory
		// carrying the library a Darwin link reads is half of being the pinned tree, and the digest recorded beside
		// it is the other half. Nothing is read out of the tree itself, and an empty answer is the honest one for a
		// tree nobody measured.
		sysrootDigest: theSysrootDigestRecordedBeside(sysrootTree),
	}

	plan, err := planFFmpeg(o, t.repo, manifest, state, machine)
	if err != nil {
		return false, err
	}

	if plan.Says != "" {
		fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Says)
	}

	if plan.WritesNone {
		// The compile this target's record describes cannot start here, so nothing is fetched, built, read,
		// gzipped, cached, licensed, or adopted for it, and no server embeds an ffmpeg it does not have. The
		// sentence naming the input that is missing is on the log above; what a run does with a target that
		// wrote none is the run's own answer.
		return true, nil
	}

	if plan.Native {
		return t.runNativeFFmpeg(plan, manifest, target, stdout)
	}

	if plan.Host {
		return t.runFFmpegOnHost(plan, manifest, target, stdout)
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

		if len(plan.Reads) > 0 {
			// The record names what proves the pair because nothing on this machine can start it. The build
			// reads those things out of the files the container left behind, and the pair is not run here or
			// anywhere: nothing is compressed, cached, or published until the pair says every one of them.
			if err := readTheCompiledPair(plan.Paths, manifest, record, target); err != nil {
				return false, err
			}

			fmt.Fprintf(stdout, "%-7s %s\n", "read", strings.Join(plan.Reads, " "))
		} else {
			report, err := t.runFFMPEGCheck(theContainerProgram{name: state.program}, plan, manifest, record, target)
			if err != nil {
				return false, err
			}

			fmt.Fprintf(stdout, "%-7s %d conversions, %d codecs, %s s to %s s\n", "check", report.Conversions,
				len(report.Codecs.Audio),
				strconv.FormatFloat(report.Shortest, 'f', 6, 64),
				strconv.FormatFloat(report.Longest, 'f', 6, 64),
			)
		}

		if err := gzipCompiled(plan.Paths, manifest, target); err != nil {
			return false, err
		}
	}

	// A compiled ffmpeg and a reused one end the same way, so the server
	// embed and the release archive do not depend on a warm cache. What the
	// archive says about the pair comes from whoever compiled them: this
	// run's own words when it compiled them, and the words it left in the
	// cache entry beside them when it did not.
	if covered {
		licence := ffmpegLicence(manifest, target)

		if plan.Reuses {
			licence, err = theLicenceInTheCache(plan.Paths, target)
			if err != nil {
				return false, err
			}
		}

		if err := publishFFmpeg(plan.Paths, manifest, target, licence); err != nil {
			return false, err
		}

		fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Paths.bundle)
	}

	return false, nil
}

// readTheCompiledPair reads out of the container's pair everything
// their record says proves them, answering with the first refusal.
// The target's operating system picks the reader, never the record.
func readTheCompiledPair(paths ffmpegPaths, manifest bundle.Manifest, record bundle.Target, target string) error {
	produced := filepath.Join(paths.scratch, sourceDir(manifest))

	for _, program := range []string{"ffmpeg", "ffprobe"} {
		path := filepath.Join(produced, program+programSuffix(target))

		if theTargetRunsMachOPrograms(target) {
			if err := readAMachOProgram(path, program, target, record.Conformance.Reads); err != nil {
				return err
			}
		} else if err := readAnELFProgram(path, program, target, record.Conformance.Reads, record); err != nil {
			return err
		}
	}

	return nil
}

// aRecordNamingAReadWithNoRule is what this build says when a target's
// record names a proof it does not know how to make: the record's failure
// and not the file's, and both of the build's gates say it the same way.
func aRecordNamingAReadWithNoRule(target string, read string) error {
	return fmt.Errorf("the %s record names %s in its conformance reads, and this build has no rule for that read: it proves what it compiled by reading it, and runs no %s program, so it cannot promise a read it does not have",
		target, read, target)
}

// runFFmpegOnHost does the work of the ffmpeg that comes from the
// machine running the build: a maintainer's dropped pair, or the
// pair this machine's own run cached, both run before adoption.
func (t tool) runFFmpegOnHost(plan ffmpegPlan, manifest bundle.Manifest, target string, stdout io.Writer) (bool, error) {
	if plan.Adopts {
		run, err := t.prove(target, manifest.Targets[target], plan.Paths, stdout)
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

	// The cache entry holds the binaries this machine's own run produced, and
	// the licence text that run wrote is in the entry beside them: the archive
	// carries the words the run said, not words this run composes.
	licence, err := theLicenceInTheCache(plan.Paths, target)
	if err != nil {
		return false, err
	}

	if err := publishFFmpeg(plan.Paths, manifest, target, licence); err != nil {
		return false, err
	}

	fmt.Fprintf(stdout, "%-7s %s\n", subFFmpeg, plan.Paths.bundle)

	return false, nil
}

// runNativeFFmpeg does the work of the ffmpeg binaries compiled on
// the machine that runs them, through the same adoption path a
// maintainer's drop takes, run before anything is adopted.
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

	run, err := t.prove(target, manifest.Targets[target], plan.Paths, stdout)
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

// nativeDrop puts the compiled binaries where a maintainer's drop
// is put, executable. The proof reads and the adoption consumes
// from there, and it is the directory the server embeds: that is
// why the adoption takes the binaries back out again.
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

// containerRun is how the build reaches a binary it cannot run itself:
// the compiled ffmpeg lives in the mounted scratch directory inside the
// container that compiled it. The image must be named before the program.
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

// publishFFmpeg puts the binaries, and what a release archive has to say
// about them, into the bundle directory the server embeds; the go build
// placeholder stays. The licence text is passed in: the two ways a
// target's ffmpeg comes to be say different and true things about it.
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

// ffmpegLicence is what a release archive carries about the ffmpeg inside
// it: the licence, the release it was compiled from, and the exact
// recipe, written from the manifest rather than a document somebody
// updates, beside the toolchain, its digests, and the proof it describes.
func ffmpegLicence(manifest bundle.Manifest, target string) string {
	var text strings.Builder

	record, pinned := manifest.Targets[target]

	text.WriteString("# The ffmpeg this server carries\n\n")
	text.WriteString(fmt.Sprintf(
		"This build of rdio-scanner carries ffmpeg %s and ffprobe %s inside its own binary, compiled\nfrom source for %s. It converts audio with nothing installed on the host.\n\n",
		manifest.FFmpeg, manifest.FFmpeg, target,
	))

	// A target whose record names reads instead of a run is a target this build never started, and the archive
	// is the only place a reader of a release learns that. The claim is made of the proof the record names, so
	// it appears for exactly the targets the build proves by reading and never for one it proved by running its
	// own binaries in a container.
	if pinned && len(record.Conformance.Reads) > 0 {
		text.WriteString(fmt.Sprintf(
			"These two programs were compiled and not run. The machine that compiled them runs no %s program, and\nno machine this build reached runs one either: nothing was started, and the build published only what it could\nread out of the two programs — their own headers, and the records those headers point at. What a program's headers\nshow is how it was linked and what it was linked for, not what it does, and this build does not pretend\notherwise.\n\n",
			target,
		))
	}

	text.WriteString("ffmpeg is free software, copyright the FFmpeg developers. This binary is used under the GNU\nLesser General Public License version 2.1 or later, and was configured without --enable-gpl\nand without --enable-nonfree, so it carries no GPL or otherwise restricted component. The\nlicence text is at https://www.gnu.org/licenses/old-licenses/lgpl-2.1.html\n\n")

	text.WriteString("The source it was compiled from:\n\n")
	text.WriteString(fmt.Sprintf("    %s\n    sha256 %s\n\n", manifest.Source.URL, manifest.Source.SHA256))

	text.WriteString("The configure recipe it was built with:\n\n")

	for _, flag := range manifest.Recipe {
		text.WriteString("    " + flag + "\n")
	}

	text.WriteString(theAudioOnlyPosture(manifest.Recipe))

	if pinned {
		text.WriteString(theCodecsTheRecipeSelects(manifest.Recipe, len(record.Conformance.Reads) > 0))
		text.WriteString(theToolchainThatCompiledIt(record))
		text.WriteString(theProofTheRecordDescribes(record))
	}

	text.WriteString(fmt.Sprintf(
		"The same recipe, the same digest and the same toolchain are recorded in this repository at\ninternal/infrastructure/ffmpeg/bundle/manifest.json, and the command that produced this binary\nis `go run ./cmd/build ffmpeg %s`.\n",
		target,
	))

	return text.String()
}

// theCodecsTheRecipeSelects names the codecs the compiled program was
// asked to carry, composed out of the recipe the notice already quotes:
// a notice naming codecs the recipe no longer selects is one the release
// cannot stand behind, and a cache hit starts no run to report.
func theCodecsTheRecipeSelects(recipe []string, provedByReading bool) string {
	var decoders, encoders []string

	for _, flag := range recipe {
		if value := strings.TrimPrefix(flag, "--enable-decoder="); value != flag {
			decoders = append(decoders, value)
		} else if value := strings.TrimPrefix(flag, "--enable-encoder="); value != flag {
			encoders = append(encoders, value)
		}
	}

	if len(decoders) == 0 && len(encoders) == 0 {
		return ""
	}

	text := "The codecs its recipe selects:\n\n"

	if len(decoders) > 0 {
		text += "    decodes  " + strings.Join(decoders, ", ") + "\n"
	}

	if len(encoders) > 0 {
		text += "    encodes  " + strings.Join(encoders, ", ") + "\n"
	}

	text += "\n"

	if theRecipeCarries(recipe, "--disable-everything") {
		text += "The recipe begins by disabling every component, so nothing else is in it.\n"
	}

	if provedByReading {
		return text + "No run recorded what the two programs register, because nothing was run: what is named\nhere is what the recipe asked the compile for, and not what a build measured them doing.\n\n"
	}

	return text + "The two programs were compiled by a build that started them, read back the codecs they register,\nand refused them if either one registered a video codec.\n\n"
}

// theToolchainThatCompiledIt names the container the pair was compiled
// in at its record's pinned digest, the file that image was built from,
// and the tree its link read: the toolchain as the build actually
// resolved it, and a pinned reference is never shortened.
func theToolchainThatCompiledIt(record bundle.Target) string {
	text := "The toolchain it was compiled in:\n\n    " +
		theImageTheRecordNames(record.Container, record.ContainerDigest) +
		"\n    built from " + theContainerfileFor(record) + "\n\n"

	if record.SysrootImage != "" {
		text += fmt.Sprintf(
			`Its link read a FreeBSD tree of headers, libraries and startup files, copied out of this image
into the container that compiled them. Nothing FreeBSD was run, and the whole tree is copied
rather than a selection of it, which is what keeps a cross-compile from ending up half
FreeBSD and half Debian without anyone deciding that. The image the copy was made from:

    %s

`,
			theImageTheRecordNames(record.SysrootImage, record.SysrootDigest),
		)
	}

	if record.Sysroot != (bundle.Sysroot{}) {
		text += fmt.Sprintf(
			`Its link read Apple's MacOSX %s SDK: a tree prepared outside this repository, by the tool its
record names, and recorded by that preparation as what it measures:

    %s
    sha256 %s

The compile reaches it through a mount that exists only while the container that compiled
them runs, and that image carries no copy of it.

`,
			record.Sysroot.SDK, record.Sysroot.Generator, record.Sysroot.SHA256,
		)
	}

	return text
}

// theProofTheRecordDescribes says how this build came to believe the
// two programs: started in its compile image, started under a runner
// there or in a named second container at a digest, or never started
// and only read. An emulated run is not a run on the machine.
func theProofTheRecordDescribes(record bundle.Target) string {
	if len(record.Conformance.Reads) > 0 {
		return `What stands in place of a run is named by this target's record, and the build holds both
programs to every one of them, in the order it reads them:

    ` + strings.Join(record.Conformance.Reads, "  ") + `

`
	}

	if record.Conformance.Container != "" {
		return "These two programs were proved by starting them" + theRunnerTheRecordNames(record) + ` in a second
container the build made from ` + sharedContainerfile + ` with the packages that record names for its proof:

    ` + theImageTheRecordNames(record.Conformance.Container, record.Conformance.Digest) + `

`
	}

	return "These two programs were proved by starting them in the container that compiled them" +
		theRunnerTheRecordNames(record) + ".\n\n"
}

// theRunnerTheRecordNames is the clause naming the program a proof runs the pair through, when its record
// names one, and says nothing when the pair was started directly.
func theRunnerTheRecordNames(record bundle.Target) string {
	if len(record.Conformance.Run) == 0 {
		return ""
	}

	return " under " + strings.Join(record.Conformance.Run, " and ")
}

// theAudioOnlyPosture is what the recipe excludes, in the words the
// licence texts use, composed from the recipe so a notice cannot name
// a switch the recipe dropped: that notice travels inside every archive.
// It covers codecs and the network, the things the recipe decides.
func theAudioOnlyPosture(recipe []string) string {
	var posture []string

	if theRecipeCarries(recipe, "--disable-everything") {
		posture = append(posture, "the recipe disables every component and enables only the audio\ndemuxers, decoders, encoder, muxers and filters the server converts with, and the pipe, fd and file\nprotocols the recording arrives and leaves on")
		posture = append(posture, "no video codec, video decoder, or video encoder is compiled in")
	}

	if theRecipeCarries(recipe, "--disable-network") {
		posture = append(posture, "no network protocol is compiled in")
	}

	if len(posture) == 0 {
		return ""
	}

	return "\nThis program is compiled for audio: " + strings.Join(posture, ";\n") + ".\n\n"
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

// suppliedFFMpegLicence is what a release archive carries about a pair
// this build did not compile in a container: the pair a Mac compiled
// with its own toolchain, or a maintainer's hand-built drop.
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
