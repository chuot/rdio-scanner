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
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// These are component tests of what the ffmpeg phase decides. Nothing here compiles anything: the
// whole point is that the build says what it is going to run, and refuses what it cannot honour,
// before it runs any of it.

// aManifestWith is the manifest the repository carries, with the parts a test cares about changed.
func aManifestWith(ffmpeg string, recipe []string) bundle.Manifest {
	manifest := bundle.ReadManifest()

	manifest.FFmpeg = ffmpeg
	manifest.Recipe = recipe

	return manifest
}

// anUncompiledTree is a tree that has nothing cached and has podman to compile with.
func anUncompiledTree() ffmpegState {
	return ffmpegState{podmanIsThere: true}
}

func stepNamed(plan ffmpegPlan, label string) (step, bool) {
	for _, s := range plan.Steps {
		if s.label == label {
			return s, true
		}
	}

	return step{}, false
}

func TestTheBuildCompilesAnFfmpegForTheTargetItIsAskedFor(t *testing.T) {
	o, err := parseArgs([]string{"ffmpeg"})
	if err != nil {
		t.Fatalf("the build refused its own ffmpeg command: %v", err)
	}

	if o.sub != subFFmpeg {
		t.Errorf("the command is %q, want %s", o.sub, subFFmpeg)
	}

	if o.target != "" {
		t.Errorf("with no target named the tool recorded %q, want the target left to be decided", o.target)
	}

	o, err = parseArgs([]string{"ffmpeg", "linux-arm64"})
	if err != nil {
		t.Fatalf("the build refused a target it knows: %v", err)
	}

	if o.target != "linux-arm64" {
		t.Errorf("the build builds for %s, want linux-arm64", o.target)
	}

	// The target an operator did not name is the one this tool is running on, which is what makes a
	// developer's plain build carry an ffmpeg too.
	plan, err := planFFmpeg(options{}, "/repo", bundle.ReadManifest(), anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused the target it is running on: %v", err)
	}

	if plan.Target != runtimeTarget() {
		t.Errorf("with no target named the build compiles for %s, want %s", plan.Target, runtimeTarget())
	}
}

func TestTheBuildRefusesACommandLineThatNamesTwoThings(t *testing.T) {
	if _, err := parseArgs([]string{"ffmpeg", "server"}); err == nil {
		t.Error("the build accepted ffmpeg and server as one command")
	}
}

func TestTheBuildRefusesATargetItCannotName(t *testing.T) {
	if _, err := parseArgs([]string{"ffmpeg", "arm64"}); err == nil {
		t.Error("the build accepted arm64, which is not a target this tool can name")
	}

	if _, err := parseArgs([]string{"ffmpeg", "--pre", "beta1", "linux-arm64"}); err == nil {
		t.Error("the build accepted a target that was not the thing it was asked to build")
	}
}

// A target the manifest has no record for is not a release target this project ships, and asking for it
// is not a failure the build has to fight: it says there is nothing to compile and does nothing.
func TestATargetTheManifestHasNoRecordForProducesNothing(t *testing.T) {
	for _, target := range []string{"netbsd-amd64", "openbsd-amd64"} {
		plan, err := planFFmpeg(options{target: target}, t.TempDir(), bundle.ReadManifest(), anUncompiledTree(), theReleaseHost)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}

		if len(plan.Steps) != 0 {
			t.Errorf("%s has no manifest record and the build still has %d steps for it", target, len(plan.Steps))
		}

		if plan.Says == "" {
			t.Errorf("%s carries no ffmpeg and the build says nothing about it", target)
		}
	}
}

// The container, the toolchain packages and the cross-compile arguments come from the manifest, so
// adding a target is writing a record and not writing Go.
func TestTheFfmpegBuildUsesTheContainerAndToolchainTheManifestRecords(t *testing.T) {
	manifest := bundle.ReadManifest()
	record := manifest.Targets["linux-arm64"]

	plan, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", manifest, anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused linux-arm64: %v", err)
	}

	image, ok := stepNamed(plan, "image")
	if !ok {
		t.Fatal("the build has no step that prepares the build container")
	}

	if !carries(image.args, record.Container) {
		t.Errorf("the build container is not %s", record.Container)
	}

	for _, pkg := range record.Packages {
		if !carries(image.args, pkg) {
			t.Errorf("the build container does not install %s", pkg)
		}
	}

	configure, ok := stepNamed(plan, "configure")
	if !ok {
		t.Fatal("the build has no step that configures the source")
	}

	for _, flag := range record.Cross {
		if !hasArg(configure.args, flag) {
			t.Errorf("configure does not pass %s", flag)
		}
	}

	for _, flag := range manifest.Recipe {
		if !hasArg(configure.args, flag) {
			t.Errorf("configure does not pass %s", flag)
		}
	}

	if _, ok := stepNamed(plan, "make"); !ok {
		t.Error("the build has no step that compiles the source")
	}
}

// A Windows build has no ffmpeg make target: the rule is %$(PROGSSUF)$(EXESUF), and make ffmpeg
// fails with "No rule to make target". Plain make builds both programs.
func TestTheFfmpegBuildRunsPlainMakeForAWindowsTarget(t *testing.T) {
	plan, err := planFFmpeg(options{target: "windows-amd64"}, "/repo", bundle.ReadManifest(), anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused windows-amd64: %v", err)
	}

	make, ok := stepNamed(plan, "make")
	if !ok {
		t.Fatal("the build has no step that compiles the source")
	}

	if hasArg(make.args, "ffmpeg") {
		t.Errorf("the Windows build asks make for the ffmpeg target it does not have: %v", make.args)
	}
}

// A covered target is a promise: the build does not quietly produce a server that will have to do
// without an ffmpeg.
func TestTheBuildRefusesToCompileAnFfmpegItHasNoToolchainFor(t *testing.T) {
	_, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", bundle.ReadManifest(), ffmpegState{}, theReleaseHost)
	if err == nil {
		t.Fatal("the build reported no problem compiling linux-arm64 without a container engine")
	}

	if !strings.Contains(err.Error(), "podman") {
		t.Errorf("the build says %q, which does not name what is missing", err)
	}
}

// The pinned digest is the integrity anchor: ffmpeg.org publishes a GPG signature and no checksum,
// so the build checks the digest it recorded when the release was chosen.
func TestTheBuildRefusesASourceTarballThatIsNotTheReleaseItPinned(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "ffmpeg-9.0.2.tar.xz")

	if err := os.WriteFile(path, []byte("something else entirely"), 0644); err != nil {
		t.Fatalf("the test cannot write the source it was given: %v", err)
	}

	err := checkSource(path, bundle.ReadManifest())
	if err == nil {
		t.Fatal("the build accepted a source tarball that is not the release it pinned")
	}

	if !strings.Contains(err.Error(), "8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e") {
		t.Errorf("the build says %q, which does not name the digest it expected", err)
	}
}

func TestTheBuildAcceptsTheSourceTarballItPinned(t *testing.T) {
	manifest := bundle.ReadManifest()

	body := []byte("the release this project pinned")

	manifest.Source.SHA256 = sha256Of(body)

	path := filepath.Join(t.TempDir(), "ffmpeg-9.0.2.tar.xz")

	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatalf("the test cannot write the source it pinned: %v", err)
	}

	if err := checkSource(path, manifest); err != nil {
		t.Errorf("the build refused the source it pinned: %v", err)
	}
}

func TestTheFfmpegCacheKeyChangesWithEverythingThatChangesTheCompile(t *testing.T) {
	manifest := bundle.ReadManifest()

	same := cacheKey(manifest, "linux-arm64")

	if changed := cacheKey(manifest, "linux-arm64"); changed != same {
		t.Errorf("the same manifest and target gave two cache keys, %s and %s", same, changed)
	}

	byTarget := cacheKey(manifest, "linux-arm")
	if byTarget == same {
		t.Error("linux-arm and linux-arm64 share a cache key")
	}

	byVersion := cacheKey(aManifestWith("9.0.3", manifest.Recipe), "linux-arm64")
	if byVersion == same {
		t.Error("a new ffmpeg release reuses the previous release's cache")
	}

	byRecipe := cacheKey(aManifestWith(manifest.FFmpeg, append(append([]string{}, manifest.Recipe...), "--enable-filter=volume")), "linux-arm64")
	if byRecipe == same {
		t.Error("a changed configure recipe reuses the previous recipe's cache")
	}

	// The source is part of what the compiled pair is. A cache that survives the digest being changed
	// is a cache that can be handed a tarball this project never pinned.
	withSource := manifest
	withSource.Source = bundle.Source{URL: manifest.Source.URL, SHA256: strings.Repeat("0", 64)}

	if cacheKey(withSource, "linux-arm64") == same {
		t.Error("a different pinned source digest reuses the previous source's cache")
	}

	record := manifest.Targets["linux-arm64"]
	record.Cross = []string{"--enable-cross-compile", "--arch=aarch64"}

	withCross := manifest
	withCross.Targets = map[string]bundle.Target{"linux-arm64": record}

	if cacheKey(withCross, "linux-arm64") == same {
		t.Error("a changed toolchain reuses the previous toolchain's cache")
	}
}

// A supplied pair is cached by the pair itself: the digest of each program is the only thing that says
// which binaries went into that cache directory, and the way a target's ffmpeg comes to be is part of what
// the directory holds.
//
// The five compiled targets' keys are pinned by value here. A warm cache is the only reason a release run
// takes minutes rather than hours, and a manifest that gains a field must not throw it away: a compiled
// record pins no programs, and its key is the compile's inputs and nothing else. The values below are the
// compile as it reads today, so a recipe that gains a switch moves them — which is the key doing its job,
// and the release run that follows recompiles those five targets once.
func TestTheFfmpegCacheKeyForASuppliedTargetIsThePairItPins(t *testing.T) {
	manifest := bundle.ReadManifest()

	supplied := cacheKey(manifest, "macos-arm64")

	rebuilt := bundle.ReadManifest()
	record := rebuilt.Targets["macos-arm64"]
	record.Programs = map[string]string{"ffmpeg": strings.Repeat("a", 64), "ffprobe": strings.Repeat("b", 64)}
	rebuilt.Targets["macos-arm64"] = record

	if cacheKey(rebuilt, "macos-arm64") == supplied {
		t.Error("a supplied pair rebuilt on its own platform reuses the cache of the pair it replaced")
	}

	compiled := bundle.ReadManifest()
	record = compiled.Targets["macos-arm64"]
	record.Produced = ""
	record.Programs = nil
	compiled.Targets["macos-arm64"] = record

	if cacheKey(compiled, "macos-arm64") == supplied {
		t.Error("a target this build compiles an ffmpeg for keeps the cache entry of the pair it no longer ships")
	}

	want := map[string]string{
		"linux-386":     "0fe270712477f3b22658387941f2c232a793e664bbe5a55970996d701be746b9",
		"linux-amd64":   "fba72d5bd353a0ec01117d488c7475e065c72f7fd4f0d552aa04600d595fbf72",
		"linux-arm":     "11319ad2a5697ebfab755f69fe8fe3d8939b6e95e444b727d5275430e0010367",
		"linux-arm64":   "56eb350e321fcb3a9ce58c41158da28b1b0729f4d945a9f5b61de69b59278aaa",
		"windows-amd64": "d526f96b16f0fb55e4baaf8038ead09d8de9dac0f69640329a43bd1464558f20",
	}

	for _, target := range []string{"linux-386", "linux-amd64", "linux-arm", "linux-arm64", "windows-amd64"} {
		if got := cacheKey(manifest, target); got != want[target] {
			t.Errorf("%s's cache key is %s, want %s: something the compiled compile reads has moved, and the warm entries move with it — when the move is the recipe, record the key it computes now", target, got, want[target])
		}
	}
}

// A second build that changes nothing does not compile anything again.
func TestAFfmpegAlreadyCompiledForTheSameRecipeIsNotCompiledAgain(t *testing.T) {
	plan, err := planFFmpeg(
		options{target: "linux-arm64"},
		"/repo",
		bundle.ReadManifest(),
		ffmpegState{podmanIsThere: true, bundleIsCached: true},
		theReleaseHost,
	)
	if err != nil {
		t.Fatalf("the build refused linux-arm64: %v", err)
	}

	if len(plan.Steps) != 0 {
		t.Errorf("a cached ffmpeg is compiled again: %d steps", len(plan.Steps))
	}

	if plan.Says == "" {
		t.Error("the build reuses a compiled ffmpeg and says nothing about it")
	}
}

// The source is fetched once and kept, because a release tarball that is already there does not have
// to be downloaded again.
func TestTheBuildFetchesTheSourceOnlyWhenItDoesNotHaveIt(t *testing.T) {
	plan, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", bundle.ReadManifest(), anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused linux-arm64: %v", err)
	}

	if plan.Source.Path == "" {
		t.Fatal("the build does not know where the ffmpeg source is kept")
	}

	if !plan.Source.Fetch {
		t.Error("a tree with no cached source does not plan to fetch one")
	}

	plan, err = planFFmpeg(
		options{target: "linux-arm64"},
		"/repo",
		bundle.ReadManifest(),
		ffmpegState{podmanIsThere: true, sourceIsCached: true, bundleIsCached: true},
		theReleaseHost,
	)
	if err != nil {
		t.Fatalf("the build refused linux-arm64: %v", err)
	}

	if plan.Source.Fetch {
		t.Error("the build plans to fetch a source it already has")
	}
}

// The ffmpeg check reaches the compiled ffmpeg through the container it was built in, and the
// container has to be named before the program: without it podman reads the program as the image to
// run, and says "invalid reference format".
func TestTheFFMPEGCheckNamesTheContainerItRunsTheCompiledFfmpegIn(t *testing.T) {
	argv := containerRun(
		"podman",
		"rdio-ffmpeg:linux-arm64-0123456789ab",
		"/repo/.build/ffmpeg/scratch/linux-arm64",
		[]string{"qemu-aarch64", "/work/ffmpeg-9.0.2/ffmpeg", "-i", "-"},
	)

	if argv[0] != "podman" || argv[1] != "run" {
		t.Fatalf("the check does not run the compiled ffmpeg in a container: %v", argv)
	}

	for i, arg := range argv {
		if arg != "qemu-aarch64" {
			continue
		}

		if argv[i-1] != "rdio-ffmpeg:linux-arm64-0123456789ab" {
			t.Errorf("the container is not named before the program it runs: %v", argv)
		}

		break
	}

	if !carries(argv, "/repo/.build/ffmpeg/scratch/linux-arm64:/work") {
		t.Error("the check does not mount the directory the compiled ffmpeg was built in")
	}
}

// A release archive has to say what ffmpeg is inside it, under what licence, and compiled from what.
// The text is written from the manifest, so it cannot name a release the build did not compile.
func TestTheBuildSaysWhatTheFfmpegItCompiledIsAndUnderWhatLicence(t *testing.T) {
	manifest := bundle.ReadManifest()

	licence := ffmpegLicence(manifest, "linux-arm64")

	for _, want := range []string{
		"Lesser General Public License",
		manifest.FFmpeg,
		manifest.Source.URL,
		manifest.Source.SHA256,
		"--enable-filter=loudnorm",
		"go run ./cmd/build ffmpeg linux-arm64",
	} {
		if !strings.Contains(licence, want) {
			t.Errorf("the licence text does not name %s", want)
		}
	}

	// The licence posture is a promise this feature makes, and the licence text is only true while the
	// recipe keeps it.
	for _, flag := range manifest.Recipe {
		if strings.HasPrefix(flag, "--enable-gpl") || strings.HasPrefix(flag, "--enable-nonfree") {
			t.Errorf("the recipe carries %s, which is not the LGPL-only posture the licence text states", flag)
		}
	}

	if !strings.Contains(licence, "without --enable-gpl") {
		t.Error("the licence text does not say what was left out")
	}
}

// A reused compile has to leave the same tree a fresh one does, licence text included: what a release
// archive carries cannot depend on whether the cache happened to be warm.
func TestAReusedFfmpegIsPublishedTheWayAFreshOneIs(t *testing.T) {
	repo := t.TempDir()
	manifest := bundle.ReadManifest()

	paths := ffmpegPathsFor(repo, manifest, "linux-arm64")

	if err := os.MkdirAll(paths.cached, 0755); err != nil {
		t.Fatalf("the test cannot prepare the cache: %v", err)
	}

	for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
		if err := os.WriteFile(filepath.Join(paths.cached, name), []byte("compiled"), 0644); err != nil {
			t.Fatalf("the test cannot fill the cache: %v", err)
		}
	}

	if err := publishFFmpeg(paths, manifest, "linux-arm64", ffmpegLicence(manifest, "linux-arm64")); err != nil {
		t.Fatalf("the build refused to publish a compiled ffmpeg: %v", err)
	}

	for _, name := range []string{"ffmpeg.gz", "ffprobe.gz", "LICENCE.md"} {
		if !aFileIsThere(filepath.Join(paths.bundle, name)) {
			t.Errorf("%s is not in the bundle directory the server embeds", name)
		}
	}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}

	return false
}

// carries reports whether a command line names something anywhere in it, which is how a build argument
// is checked: the container is named inside --build-arg base=....
func carries(args []string, want string) bool {
	for _, arg := range args {
		if strings.Contains(arg, want) {
			return true
		}
	}

	return false
}

// The supplied targets are the ones this build compiles no ffmpeg for: the pair is compiled on the
// platform the target runs on, dropped where the build finds it, pinned by digest, and proved by the
// ffmpeg check on that platform. These are the decisions the ffmpeg phase makes about a pair it cannot
// run, and every one of them is taken before anything is written.

// aManifestPinning is the manifest this repository carries, with the digests of a supplied pair pinned for
// one target.
func aManifestPinning(target string, ffmpegDigest string, ffprobeDigest string) bundle.Manifest {
	manifest := bundle.ReadManifest()

	record := manifest.Targets[target]
	record.Programs = map[string]string{"ffmpeg": ffmpegDigest, "ffprobe": ffprobeDigest}
	manifest.Targets[target] = record

	return manifest
}

// aSuppliedPair drops the pair a maintainer builds for one target, records the proof the ffmpeg check
// recorded for it, and returns the manifest that pins them.
func aSuppliedPair(t *testing.T, repo string, target string) (bundle.Manifest, string, string) {
	t.Helper()

	body := aSuppliedProgramBytes(target)
	other := append(append([]byte{}, body...), byte(len(target)))

	return aSuppliedPairWith(t, repo, target, body, other), sha256Of(body), sha256Of(other)
}

// aSuppliedPairWith drops a pair whose programs are the bytes a test asks for, pins them, and records the
// proof for them. The pair is accepted by digest and by proof, so what the build refuses is the shape.
func aSuppliedPairWith(t *testing.T, repo string, target string, ffmpegBytes []byte, ffprobeBytes []byte) bundle.Manifest {
	t.Helper()

	dir := filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", target)

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("making the bundle directory: %v", err)
	}

	// The directory a clone carries: the placeholder that makes the embed compile, and nothing else.
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0644); err != nil {
		t.Fatalf("writing the placeholder: %v", err)
	}

	for name, body := range map[string][]byte{"ffmpeg": ffmpegBytes, "ffprobe": ffprobeBytes} {
		if err := os.WriteFile(filepath.Join(dir, name+programSuffix(target)), body, 0755); err != nil {
			t.Fatalf("dropping the supplied %s: %v", name, err)
		}
	}

	ffmpegDigest := sha256Of(ffmpegBytes)
	ffprobeDigest := sha256Of(ffprobeBytes)

	if err := writeFfmpegCheck(repo, aCheckFor(target, ffmpegDigest, ffprobeDigest)); err != nil {
		t.Fatalf("recording the ffmpeg check: %v", err)
	}

	return aManifestPinning(target, ffmpegDigest, ffprobeDigest)
}

// A pair that was not dropped is the ordinary state of a release run on a machine that does not have the
// Mac or the FreeBSD box in front of it. It is not a failure, and the build says where to drop the pair
// and what proves it.
func TestASuppliedTargetWhosePairWasNotDroppedProducesNothing(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64"} {
		manifest := aManifestPinning(target, theFFmpegDigest, theFFProbeDigest)

		plan, err := planFFmpeg(options{target: target}, t.TempDir(), manifest, anUncompiledTree(), theReleaseHost)
		if err != nil {
			t.Errorf("%s: a pair that was simply not dropped failed the run: %v", target, err)
			continue
		}

		if len(plan.Steps) != 0 {
			t.Errorf("%s: the build has %d steps for a target with nothing dropped", target, len(plan.Steps))
		}

		if !plan.Skipped {
			t.Errorf("%s: the plan does not mark the target skipped, so the release run builds a server and an archive for nothing", target)
		}

		if !strings.Contains(plan.Says, "internal/infrastructure/ffmpeg/bundle/"+target) {
			t.Errorf("%s: the message %q does not name the directory to drop the pair in", target, plan.Says)
		}

		if !strings.Contains(plan.Says, "ffmpeg-check") {
			t.Errorf("%s: the message %q does not name the command that proves the pair", target, plan.Says)
		}
	}
}

// Nothing is adopted for a target whose pair has not been pinned: the pair is not reproducible from this
// repository, so the digest in the manifest is the thing a release is made of, and a build that adopted
// whatever it found would be shipping a binary nobody recorded.
func TestTheBuildRefusesASuppliedPairTheManifestDoesNotPin(t *testing.T) {
	repo := t.TempDir()

	aSuppliedPairWith(t, repo, "macos-arm64", aSuppliedProgramBytes("macos-arm64"), aSuppliedProgramBytes("macos-arm64"))

	_, err := planFFmpeg(options{target: "macos-arm64"}, repo, bundle.ReadManifest(), anUncompiledTree(), theReleaseHost)
	if err == nil {
		t.Fatal("the build adopted a pair the manifest does not pin")
	}

	if !strings.Contains(err.Error(), "manifest.json") || !strings.Contains(err.Error(), "programs") {
		t.Errorf("the failure %q does not name the manifest field to fill in", err.Error())
	}
}

// The pinned digest is the pair. A pair that is not the pinned one is a rebuild nobody recorded, and the
// failure names both digests, because the operator has to be able to tell which one is in front of them.
func TestTheBuildRefusesASuppliedPairThatIsNotThePinnedOne(t *testing.T) {
	repo := t.TempDir()

	_, ffmpegDigest, ffprobeDigest := aSuppliedPair(t, repo, "macos-arm64")

	pinned := aManifestPinning("macos-arm64", strings.Repeat("7", 64), strings.Repeat("8", 64))

	_, err := planFFmpeg(options{target: "macos-arm64"}, repo, pinned, anUncompiledTree(), theReleaseHost)
	if err == nil {
		t.Fatal("the build adopted a pair that is not the pair the manifest pins")
	}

	if !strings.Contains(err.Error(), ffmpegDigest) || !strings.Contains(err.Error(), strings.Repeat("7", 64)) {
		t.Errorf("the failure %q names neither the digest found nor the digest pinned", err.Error())
	}

	if !strings.Contains(err.Error(), ffprobeDigest) {
		t.Errorf("the failure %q does not name the ffprobe it also refused", err.Error())
	}
}

// A pair accepted by digest and by proof is refused on its shape, with the rule named: the build cannot
// run it, and the headers are what it can read.
func TestTheBuildRefusesASuppliedPairThatIsNotTheProgramItsTargetRuns(t *testing.T) {
	repo := t.TempDir()

	// The same recipe on an Apple Silicon Mac produces a signed program. An unsigned build is what a
	// maintainer gets when he forgets codesign, and the proof is recorded for it, because the check ran
	// on a machine that let it start.
	unsigned := aSystemProgram(theArm64CPU)
	unsigned.Signature = false

	manifest := aSuppliedPairWith(t, repo, "macos-arm64", machOBytes(unsigned), aSuppliedProgramBytes("macos-arm64"))

	_, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, anUncompiledTree(), theReleaseHost)
	if err == nil {
		t.Fatal("the build adopted an unsigned program for macos-arm64")
	}

	if !strings.Contains(err.Error(), "signature") {
		t.Errorf("the failure %q does not name the code signature rule", err.Error())
	}
}

// The proof is the only evidence a supplied pair converts audio. A pair the check never proved is not
// adopted, and the failure names the command that produces a proof and the machine it runs on.
func TestTheBuildRefusesASuppliedPairNobodyProvedOnItsOwnPlatform(t *testing.T) {
	repo := t.TempDir()

	manifest := aSuppliedPairWith(t, repo, "macos-arm64", aSuppliedProgramBytes("macos-arm64"), aSuppliedProgramBytes("macos-arm64"))

	if err := os.Remove(ffmpegCheckFile(repo, "macos-arm64")); err != nil {
		t.Fatalf("removing the proof: %v", err)
	}

	_, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, anUncompiledTree(), theReleaseHost)
	if err == nil {
		t.Fatal("the build adopted a pair with no ffmpeg check recorded for it")
	}

	if !strings.Contains(err.Error(), "ffmpeg-check") {
		t.Errorf("the failure %q does not name the command that records the proof", err.Error())
	}
}

// A pair that is pinned, shaped, and proved is adopted: the plan carries the proof it verified, because
// the licence text the release archive carries names the check that ran.
func TestTheBuildAdoptsTheSuppliedPairItPinsAndItsCheckProved(t *testing.T) {
	repo := t.TempDir()

	manifest, ffmpegDigest, ffprobeDigest := aSuppliedPair(t, repo, "macos-arm64")

	plan, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused the pair it pins and its check proved: %v", err)
	}

	if plan.Skipped {
		t.Error("the build skipped a pair that was dropped, pinned, shaped, and proved")
	}

	if plan.Check.Target != "macos-arm64" {
		t.Errorf("the plan carries no proof of the pair it adopted: %+v", plan.Check)
	}

	if plan.Check.FFmpeg != ffmpegDigest || plan.Check.FFProbe != ffprobeDigest {
		t.Errorf("the plan carries the proof of the pair %s / %s, not the pair it adopted", plan.Check.FFmpeg, plan.Check.FFProbe)
	}

	if !strings.Contains(plan.Says, "supplied") {
		t.Errorf("the message %q does not say the ffmpeg is supplied", plan.Says)
	}
}

// A pair that is already in the cache is not adopted again, and the drop is not needed for it: the cache
// key is the pinned digests, so a warm cache is the pair the manifest names.
func TestASuppliedPairAlreadyInCacheIsNotAdoptedAgain(t *testing.T) {
	repo := t.TempDir()

	manifest, _, _ := aSuppliedPair(t, repo, "macos-arm64")

	paths := ffmpegPathsFor(repo, manifest, "macos-arm64")

	if err := adoptSuppliedFFmpeg(paths, "macos-arm64"); err != nil {
		t.Fatalf("adopting the pair: %v", err)
	}

	plan, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, ffmpegState{bundleIsCached: true}, theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused a cached supplied pair: %v", err)
	}

	if len(plan.Steps) != 0 {
		t.Errorf("a cached supplied pair is adopted again: %d steps", len(plan.Steps))
	}

	if plan.Skipped {
		t.Error("a cached supplied pair is skipped, and the release run ships no archive for it")
	}

	if plan.Says == "" {
		t.Error("the build says nothing about reusing a supplied pair")
	}
}

// The drop is consumed: the pair moves out of the directory a server binary embeds, and what stays there
// is the gzipped pair and its licence. A raw pair left in that directory is embedded next to the gzipped
// one, and every server built for that target carries about ten megabytes of uncompressed program.
func TestASuppliedPairIsAdoptedIntoTheDirectoryTheServerEmbeds(t *testing.T) {
	repo := t.TempDir()

	manifest, ffmpegDigest, ffprobeDigest := aSuppliedPair(t, repo, "macos-arm64")

	paths := ffmpegPathsFor(repo, manifest, "macos-arm64")

	check, err := readFfmpegCheck(repo, "macos-arm64", ffmpegDigest, ffprobeDigest)
	if err != nil {
		t.Fatalf("reading the proof: %v", err)
	}

	if err := adoptSuppliedFFmpeg(paths, "macos-arm64"); err != nil {
		t.Fatalf("adopting the pair: %v", err)
	}

	if err := publishFFmpeg(paths, manifest, "macos-arm64", suppliedFFMpegLicence(manifest, "macos-arm64", check)); err != nil {
		t.Fatalf("publishing the pair: %v", err)
	}

	names := []string{}

	entries, err := os.ReadDir(paths.bundle)
	if err != nil {
		t.Fatalf("reading the bundle directory: %v", err)
	}

	for _, entry := range entries {
		names = append(names, entry.Name())

		if entry.Name() == "ffmpeg" || entry.Name() == "ffprobe" {
			t.Errorf("%s is left in the directory the server embeds: //go:embed all: embeds every file in it", entry.Name())
		}
	}

	for _, want := range []string{".gitkeep", "ffmpeg.gz", "ffprobe.gz", "LICENCE.md"} {
		if !theNamesCarry(names, want) {
			t.Errorf("the bundle directory does not carry %s: %v", want, names)
		}
	}

	// The pair the server writes out of its own binary is the pair that was dropped: gzip is lossless,
	// and the ad-hoc code signature a signed pair carries is a hash over those exact bytes. The bundle
	// package's own tests are what prove a directory holding this pair is a directory it materializes.
	for _, pair := range []struct {
		name   string
		digest string
	}{
		{"ffmpeg", ffmpegDigest},
		{"ffprobe", ffprobeDigest},
	} {
		for _, dir := range []string{paths.cached, paths.bundle} {
			body, err := readGzip(filepath.Join(dir, pair.name+".gz"))
			if err != nil {
				t.Fatalf("reading %s in %s: %v", pair.name+".gz", dir, err)
			}

			if sha256Of(body) != pair.digest {
				t.Errorf("%s in %s is %s, and the pair dropped was %s", pair.name, dir, sha256Of(body), pair.digest)
			}
		}
	}
}

// The licence a supplied archive carries says what that ffmpeg is, and says where it was compiled: on the
// host the target runs on, which is the one place a pair of that shape is compiled, and adopted by the
// release build on the digest the manifest pins and the proof the check left there.
func TestTheBuildSaysWhatASuppliedFfmpegIsAndUnderWhatLicence(t *testing.T) {
	manifest := aManifestPinning("macos-arm64", theFFmpegDigest, theFFProbeDigest)

	check := aCheckFor("macos-arm64", theFFmpegDigest, theFFProbeDigest)

	text := suppliedFFMpegLicence(manifest, "macos-arm64", check)

	for _, want := range []string{
		"Lesser General Public License",
		manifest.FFmpeg,
		manifest.Source.URL,
		manifest.Source.SHA256,
		manifest.Recipe[0],
		theFFmpegDigest,
		theFFProbeDigest,
		"compiled on",
		"adopted",
		"macos-arm64",
		check.Host,
		check.Checked,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the licence text for a supplied pair names no %q", want)
		}
	}

	for _, not := range []string{
		"go run ./cmd/build ffmpeg macos-arm64",
		"compiled from source for macos-arm64. It converts",
	} {
		if strings.Contains(text, not) {
			t.Errorf("the licence text says %q, which is not true of a pair this build did not compile", not)
		}
	}
}

func theNamesCarry(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}

	return false
}

func readGzip(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// The target an operator who named none is building for is the release target this machine belongs to,
// named the way the release names it. On a Mac that is macos-arm64, the target the manifest records, and
// not the darwin-arm64 name go builds for: a phase that named the machine by go's word asked the manifest
// about a target this project has no record for, and answered with the uncovered-target message on the
// very machine the supplied pair is built on.
func TestTheFFmpegTargetOfARunThatNamedNoTargetIsTheMachinesReleaseTarget(t *testing.T) {
	want := releaseTargetWordFor(runtime.GOOS, runtime.GOARCH)

	if got := ffmpegTarget(options{}); got != want {
		t.Errorf("with no target named the ffmpeg phase builds for %q, want the release target %q of this %s-%s machine", got, want, runtime.GOOS, runtime.GOARCH)
	}
}

// The machine a supplied pair is compiled on is the machine that runs it, and the build can be standing on
// it: the operator on the Apple Silicon Mac asked for the ffmpeg of the machine they are on, and the
// repository in front of them names the source, its digest, the recipe, the flags that floor sets, the make
// program that build needs, and the check that proves the result. The compile is what that run asked for,
// and it is what only that machine can do.
func TestTheSuppliedTargetOnItsOwnHostCompilesItsPair(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64"} {
		manifest := bundle.ReadManifest()

		plan, err := planFFmpeg(options{target: target}, t.TempDir(), manifest, aNativeTree(target), aMachineForTarget(target))
		if err != nil {
			t.Errorf("%s: the build refused to compile the pair on the host that is the target: %v", target, err)
			continue
		}

		if !plan.Native {
			t.Errorf("%s: the plan does not compile the pair on the host that is the target", target)
			continue
		}

		if plan.Skipped {
			t.Errorf("%s: the plan skips the target whose pair it can compile, and the operator on that host is told to drop a pair they are standing on", target)
		}

		want := []string{"unpack", "configure", "make", "strip"}
		if strings.HasPrefix(target, "macos-") {
			// Stripping rewrites the binary, so the signature goes on after it: a signature over the
			// bytes strip removed is a signature macOS refuses to run.
			want = append(want, "sign")
		}

		if got := stepLabels(plan.Steps); !sameWords(got, want) {
			t.Errorf("%s: the compile runs %v, want %v", target, got, want)
		}

		for _, s := range plan.Steps {
			if s.name == "podman" {
				t.Errorf("%s: the compile of a pair built on this host runs a container: %v", target, s.args)
			}
		}

		if !plan.Source.Fetch {
			t.Errorf("%s: the compile plans no source to fetch", target)
		}

		if plan.Source.URL != manifest.Source.URL {
			t.Errorf("%s: the compile fetches %s, want the source the manifest pins, %s", target, plan.Source.URL, manifest.Source.URL)
		}

		configure := stepFor(plan.Steps, "configure")

		if configure.name == "" {
			t.Fatalf("%s: the compile plans no configure", target)
		}

		for _, flag := range manifest.Recipe {
			if !carries(configure.args, flag) {
				t.Errorf("%s: the configure does not carry the manifest's %s", target, flag)
			}
		}

		record := manifest.Targets[target]

		if len(record.CFlags) > 0 && !carries(configure.args, "--extra-cflags="+strings.Join(record.CFlags, " ")) {
			t.Errorf("%s: the configure does not add the record's cflags %v to the source: %v", target, record.CFlags, configure.args)
		}

		if len(record.LDFlags) > 0 && !carries(configure.args, "--extra-ldflags="+strings.Join(record.LDFlags, " ")) {
			t.Errorf("%s: the configure does not add the record's ldflags %v to the source: %v", target, record.LDFlags, configure.args)
		}

		if carries(configure.args, "--enable-cross-compile") {
			t.Errorf("%s: the compile cross-compiles on the machine the pair runs on", target)
		}

		make := stepFor(plan.Steps, "make")

		if want := aMakeProgramFor(target); make.name != want {
			t.Errorf("%s: the compile runs %s, want %s: the record names the make program its host needs", target, make.name, want)
		}

		strip := stepFor(plan.Steps, "strip")

		if !carries(strip.args, "ffmpeg") || !carries(strip.args, "ffprobe") {
			t.Errorf("%s: the compile strips %v", target, strip.args)
		}

		if sign := stepFor(plan.Steps, "sign"); sign.name != "" {
			if !carries(sign.args, "codesign") && sign.name != "codesign" {
				t.Errorf("%s: the sign step is %s %v", target, sign.name, sign.args)
			}

			if stepIndex(plan.Steps, "strip") > stepIndex(plan.Steps, "sign") {
				t.Errorf("%s: the pair is signed before it is stripped, and the signature is over bytes strip rewrote", target)
			}
		}
	}
}

// The audio-only switches are not a note kept in the manifest: they are what the source is told. Both
// routes are shown passing them to configure — the compile this machine runs in a container, and the
// compile a target's own host runs on the machine that is the target — because a switch that reaches
// only one route is one recipe with two meanings, and the supplied pairs are compiled by the other one.
func TestTheAudioOnlySwitchesReachConfigureOnBothRoutes(t *testing.T) {
	manifest := bundle.ReadManifest()

	want := []string{"--disable-everything", "--disable-network"}

	container, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", manifest, anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused linux-arm64: %v", err)
	}

	native, err := planFFmpeg(options{target: "macos-arm64"}, t.TempDir(), manifest, aNativeTree("macos-arm64"), aMachineForTarget("macos-arm64"))
	if err != nil {
		t.Fatalf("the build refused to compile macos-arm64 on its own host: %v", err)
	}

	for _, route := range []struct {
		target string
		plan   ffmpegPlan
	}{
		{target: "linux-arm64", plan: container},
		{target: "macos-arm64", plan: native},
	} {
		configure := stepFor(route.plan.Steps, "configure")

		if configure.name == "" {
			t.Fatalf("%s: the compile plans no configure", route.target)
		}

		for _, flag := range want {
			if !carries(configure.args, flag) {
				t.Errorf("%s: the configure does not tell the source %s: %v", route.target, flag, configure.args)
			}
		}
	}
}

// The compile is what the target's own host does. A machine outside it compiles nothing, and says what
// FEAT-033 says: the pair is not supplied, where to drop it, what proves it, and what pins it.
func TestTheSuppliedTargetOnAnotherHostCompilesNothing(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64"} {
		plan, err := planFFmpeg(options{target: target}, t.TempDir(), bundle.ReadManifest(), aNativeTree(target), theReleaseHost)
		if err != nil {
			t.Errorf("%s: a machine that is not the target's host failed the run: %v", target, err)
			continue
		}

		if plan.Native {
			t.Errorf("%s: the build compiled a %s pair on a machine that cannot run it", target, target)
		}

		if !plan.Skipped {
			t.Errorf("%s: the build does not skip a target whose pair it cannot produce", target)
		}

		if len(plan.Steps) != 0 {
			t.Errorf("%s: the build has %d steps for a target it is not standing on", target, len(plan.Steps))
		}
	}
}

// A pair the maintainer dropped is adopted on its own host exactly as it is adopted anywhere else: the
// compile is what the build does when there is nothing to adopt, not a replacement for the drop.
func TestASuppliedPairDroppedOnItsOwnHostIsAdoptedAndNotCompiled(t *testing.T) {
	repo := t.TempDir()

	manifest, _, _ := aSuppliedPair(t, repo, "macos-arm64")

	plan, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, aNativeTree("macos-arm64"), aMachineForTarget("macos-arm64"))
	if err != nil {
		t.Fatalf("the build refused the pair dropped for it: %v", err)
	}

	if plan.Native {
		t.Error("the build compiled a pair when a pair is sitting in the directory it embeds")
	}

	if plan.Check.Target != "macos-arm64" {
		t.Errorf("the plan adopts nothing: %+v", plan.Check)
	}
}

// A pair the cache holds is reused on its own host, and nothing is compiled over it: the cache key is the
// pinned pair, so a warm cache is the pair the manifest names.
func TestASuppliedPairInCacheIsReusedOnItsOwnHost(t *testing.T) {
	repo := t.TempDir()

	manifest, _, _ := aSuppliedPair(t, repo, "macos-arm64")

	state := aNativeTree("macos-arm64")
	state.bundleIsCached = true

	plan, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, state, aMachineForTarget("macos-arm64"))
	if err != nil {
		t.Fatalf("the build refused a cached pair: %v", err)
	}

	if plan.Native {
		t.Error("the build compiles a pair the cache already holds")
	}

	if len(plan.Steps) != 0 {
		t.Errorf("the build has %d steps for a pair it already has", len(plan.Steps))
	}
}

// The host's own build tools are what a native compile runs, and a program that is not there is a build
// that cannot start: the operator is told which program, and what installs it, before a minute of work is
// spent finding out.
func TestTheNativeCompileRefusesAHostWithNoToolchain(t *testing.T) {
	for _, missing := range []string{"tar", "cc", "strip", "make", "codesign"} {
		repo := t.TempDir()

		state := aNativeTree("macos-arm64")
		delete(state.nativeTools, missing)

		_, err := planFFmpeg(options{target: "macos-arm64"}, repo, bundle.ReadManifest(), state, aMachineForTarget("macos-arm64"))
		if err == nil {
			t.Errorf("the build planned a compile on a host with no %s", missing)
			continue
		}

		if !strings.Contains(err.Error(), missing) {
			t.Errorf("the refusal %q does not name the %s that is missing", err.Error(), missing)
		}
	}

	// FreeBSD's record names gmake, and that is the program its host has to have: a host with make and no
	// gmake cannot build ffmpeg, because BSD make will not build it.
	state := aNativeTree("freebsd-amd64")
	delete(state.nativeTools, "gmake")

	_, err := planFFmpeg(options{target: "freebsd-amd64"}, t.TempDir(), bundle.ReadManifest(), state, aMachineForTarget("freebsd-amd64"))
	if err == nil {
		t.Fatal("the build planned a FreeBSD compile on a host with no gmake")
	}

	if !strings.Contains(err.Error(), "gmake") {
		t.Errorf("the refusal %q does not name the gmake that is missing", err.Error())
	}
}

func aMachineForTarget(target string) host {
	tr, ok := targetFor(target)
	if !ok {
		panic("no release target " + target)
	}

	return host{goos: tr.goos, goarch: tr.goarch}
}

// theReleaseHost is the machine that produces the releases: Linux, and no host a supplied pair runs on.
var theReleaseHost = host{goos: "linux", goarch: "amd64"}

// aNativeTree is a tree with nothing compiled in it, and the host's own build tools in front of it.
func aNativeTree(target string) ffmpegState {
	state := anUncompiledTree()
	state.nativeTools = aToolchainFor(target)

	return state
}

func aToolchainFor(target string) map[string]string {
	programs := []string{"tar", "cc", "strip", aMakeProgramFor(target)}

	if strings.HasPrefix(target, "macos-") {
		programs = append(programs, "codesign")
	}

	tools := map[string]string{}

	for _, name := range programs {
		tools[name] = "/usr/bin/" + name
	}

	return tools
}

func aMakeProgramFor(target string) string {
	record, ok := bundle.ReadManifest().Targets[target]
	if !ok || record.Make == "" {
		return "make"
	}

	return record.Make
}

func stepFor(steps []step, label string) step {
	for _, s := range steps {
		if s.label == label {
			return s
		}
	}

	return step{}
}

func stepIndex(steps []step, label string) int {
	for i, s := range steps {
		if s.label == label {
			return i
		}
	}

	return -1
}

func stepLabels(steps []step) []string {
	labels := []string{}

	for _, s := range steps {
		labels = append(labels, s.label)
	}

	return labels
}

func sameWords(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
