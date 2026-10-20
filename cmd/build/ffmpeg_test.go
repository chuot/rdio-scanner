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

// anUncompiledTree is a tree that has nothing cached, on a machine that has podman to compile with.
func anUncompiledTree() ffmpegState {
	return ffmpegState{program: podmanProgram}
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

	// The source is part of what the compiled binaries are. A cache that survives the digest being changed
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

// A supplied target's cache entry is named by the manifest's record and the target, exactly as a compiled
// target's is. The binaries themselves are not part of the name of the entry they land in: the cache is
// this machine's own tree, gitignored, and what this machine compiled is what sits in it. An Apple Silicon
// Mac and an Intel Mac keying the Intel target alike is not a collision, because each machine's cache
// holds only what that machine compiled.
//
// The five compiled targets' keys are pinned by value here. A warm cache is the only reason a release run
// takes minutes rather than hours, and a manifest that gains a field must not throw it away: a compiled
// record has no supplied fields at all, and its key is the compile's inputs and nothing else. The values
// below are the compile as it reads today, so a recipe that gains a switch moves them — which is the key
// doing its job, and the release run that follows recompiles those five targets once.
func TestTheFfmpegCacheKeyOfSuppliedBinariesIsTheRecipeAndTheTarget(t *testing.T) {
	manifest := bundle.ReadManifest()

	supplied := cacheKey(manifest, "macos-arm64")

	// The record the manifest carries, copied and changed: the map is shared when the manifest struct is
	// copied, so a test that wants a second record builds a second map.
	withFlags := bundle.ReadManifest()
	targets := map[string]bundle.Target{}
	for name, record := range withFlags.Targets {
		targets[name] = record
	}
	record := targets["macos-arm64"]
	record.LDFlags = append(append([]string{}, record.LDFlags...), "-mmacosx-version-min=12.0")
	targets["macos-arm64"] = record
	withFlags.Targets = targets

	if cacheKey(withFlags, "macos-arm64") == supplied {
		t.Error("a supplied record whose flags changed reuses the cache of the binaries the old flags built")
	}

	withMake := bundle.ReadManifest()
	targets = map[string]bundle.Target{}
	for name, rec := range withMake.Targets {
		targets[name] = rec
	}
	record = targets["macos-arm64"]
	record.Make = "make"
	targets["macos-arm64"] = record
	withMake.Targets = targets

	if cacheKey(withMake, "macos-arm64") == supplied {
		t.Error("a supplied record naming a different make program reuses the cache of the binaries the old one built")
	}

	compiled := bundle.ReadManifest()
	targets = map[string]bundle.Target{}
	for name, rec := range compiled.Targets {
		targets[name] = rec
	}
	record = targets["macos-arm64"]
	record.Produced = ""
	targets["macos-arm64"] = record
	compiled.Targets = targets

	if cacheKey(compiled, "macos-arm64") == supplied {
		t.Error("a target this build compiles an ffmpeg for keeps the cache entry of the binaries it no longer ships")
	}

	// The two macOS targets are compiled by the same host with the same recipe and differ only by the
	// architecture the compile is told to produce, so their keys have to differ.
	if cacheKey(manifest, "macos-amd64") == cacheKey(manifest, "macos-arm64") {
		t.Error("macos-amd64 and macos-arm64 share a cache key, and one Mac compiling both writes one entry")
	}

	want := map[string]string{
		"linux-386":     "e6b0dcd5763abfee3ed4dcbd8888dbc3ec5d9eeeffb38378d2a7b5fa6996ab4f",
		"linux-amd64":   "20793f67a5ed90ec23ac53388b8d15e9491294553ed753f7250feaf682ae1373",
		"linux-arm":     "95e8a1ad5f04e1fef752f679a1f99fdd4585f85ccd9f2f96137fc34f5815ffa4",
		"linux-arm64":   "13243228d40b52d03600f55b1f073c0ce87690e9831f541189d10cdb4420fd98",
		"windows-amd64": "dfe8ad310c25f9487fc8206e3d79bc62ec5b46badd22583bd367eb11577a52a5",
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
		ffmpegState{program: podmanProgram, bundleIsCached: true},
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
		ffmpegState{program: podmanProgram, sourceIsCached: true, bundleIsCached: true},
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
		theContainerProgram{name: podmanProgram},
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

// The licence text says the program is compiled for audio, and the switches that make that true are the
// recipe's. The text is composed from the recipe, so a recipe that drops a switch stops promising the
// build that switch makes: a notice that promises what the recipe does not produce is a notice the
// release cannot stand behind, and the archive carries it into every deployment.
func TestTheLicenceSaysTheFfmpegIsCompiledForAudioOnly(t *testing.T) {
	manifest := bundle.ReadManifest()

	licence := ffmpegLicence(manifest, "linux-arm64")

	for _, want := range []string{
		"compiled for audio",
		"no video codec, video decoder, or video encoder is compiled in",
		"no network protocol is compiled in",
	} {
		if !strings.Contains(licence, want) {
			t.Errorf("the licence text does not say %q:\n%s", want, licence)
		}
	}

	dropped := bundle.ReadManifest()
	dropped.Recipe = aRecipeWithout(dropped.Recipe, "--disable-network")

	if strings.Contains(ffmpegLicence(dropped, "linux-arm64"), "no network protocol is compiled in") {
		t.Error("the licence text promises a build with no network protocol to a recipe that dropped --disable-network")
	}

	dropped = bundle.ReadManifest()
	dropped.Recipe = aRecipeWithout(dropped.Recipe, "--disable-everything")

	if strings.Contains(ffmpegLicence(dropped, "linux-arm64"), "no video codec") {
		t.Error("the licence text promises a build with no video codec to a recipe that dropped --disable-everything")
	}
}

// aRecipeWithout is the recipe with one switch taken out, which is how a test shows a licence text
// promising a build its own recipe does not produce.
func aRecipeWithout(recipe []string, flag string) []string {
	kept := []string{}

	for _, one := range recipe {
		if one == flag {
			continue
		}

		kept = append(kept, one)
	}

	return kept
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

// The supplied targets are the ones this build compiles no ffmpeg for in a container: the ffmpeg binaries
// are compiled on the platform the target runs on, dropped where the build finds it, and run by the run
// that adopts them. These are the decisions the ffmpeg phase makes about binaries it must run to know
// anything about them, and the plan takes them before anything is written.

// aDropOfSuppliedBinaries drops the binaries a maintainer builds for one target and returns the manifest this
// repository carries. Nothing is recorded about them: the run that adopts them is the run that proves them.
func aDropOfSuppliedBinaries(t *testing.T, repo string, target string) bundle.Manifest {
	t.Helper()

	body := aSuppliedProgramBytes(target)

	return aDropOfSuppliedBinariesWith(t, repo, target, body, append(append([]byte{}, body...), byte(len(target))))
}

// aDropOfSuppliedBinariesWith drops binaries whose programs are the bytes a test asks for. The plan reads which
// machine code they hold, so a test that wants the plan to reach the run hands it the target's own shape.
func aDropOfSuppliedBinariesWith(t *testing.T, repo string, target string, ffmpegBytes []byte, ffprobeBytes []byte) bundle.Manifest {
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

	return bundle.ReadManifest()
}

// Binaries that were not dropped are the ordinary state of a release run on a machine that does not have
// the Mac or the FreeBSD box in front of it. It is not a failure, and the build says where to build them
// and where to drop them.
func TestASuppliedTargetWhoseBinariesWereNotDroppedProducesNothing(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64"} {
		plan, err := planFFmpeg(options{target: target}, t.TempDir(), bundle.ReadManifest(), anUncompiledTree(), theReleaseHost)
		if err != nil {
			t.Errorf("%s: binaries that were simply not dropped failed the run: %v", target, err)
			continue
		}

		if len(plan.Steps) != 0 {
			t.Errorf("%s: the build has %d steps for a target with nothing dropped", target, len(plan.Steps))
		}

		if !plan.Skipped {
			t.Errorf("%s: the plan does not mark the target skipped, so the release run builds a server and an archive for nothing", target)
		}

		if !strings.Contains(plan.Says, "internal/infrastructure/ffmpeg/bundle/"+target) {
			t.Errorf("%s: the message %q does not name the directory to drop the binaries in", target, plan.Says)
		}

		if !strings.Contains(plan.Says, "go run ./cmd/build ffmpeg "+target) {
			t.Errorf("%s: the message %q does not name the command that compiles them where they run", target, plan.Says)
		}
	}
}

// The machine code the file holds is the one thing the build reads of binaries it did not compile, and it
// reads it before anything else: an Apple Silicon Mac runs the arm64 binaries and the x86-64 ones alike,
// so the only thing that says the Intel archive carries Intel code is the header. The refusal names the
// architecture in both words, because the operator has to see which one is in front of them.
func TestTheBuildRefusesSuppliedBinariesOfTheOtherArchitecture(t *testing.T) {
	repo := t.TempDir()

	intel := machOBytes(aSystemProgram(theX8664CPU))

	manifest := aDropOfSuppliedBinariesWith(t, repo, "macos-arm64", intel, intel)

	_, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, anUncompiledTree(), theReleaseHost)
	if err == nil {
		t.Fatal("the build adopted x86-64 binaries for macos-arm64, which runs arm64")
	}

	if !strings.Contains(err.Error(), "arm64") || !strings.Contains(err.Error(), "x86-64") {
		t.Errorf("the failure %q does not name both architectures", err.Error())
	}
}

// Binaries dropped for a target the build cannot run in a container are adopted: the plan says so, and the
// run that adopts them runs them first. The plan carries the adoption, because the licence text the archive
// carries is written from that run.
func TestTheBuildAdoptsTheSuppliedBinariesDroppedForItsTarget(t *testing.T) {
	repo := t.TempDir()

	manifest := aDropOfSuppliedBinaries(t, repo, "macos-arm64")

	plan, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, anUncompiledTree(), theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused the binaries that were dropped: %v", err)
	}

	if plan.Skipped {
		t.Error("the build skipped binaries that were dropped for it")
	}

	if !plan.Adopts {
		t.Error("the plan does not carry the adoption, so the run publishes a cache entry it never adopted into")
	}

	if !strings.Contains(plan.Says, "supplied") {
		t.Errorf("the message %q does not say the ffmpeg is supplied", plan.Says)
	}
}

// Binaries that are already in the cache are not adopted again, and the drop is not needed for them: the
// cache is keyed by the manifest's record and the target, so a warm entry is what this machine's own run
// produced.
func TestSuppliedBinariesAlreadyInCacheAreNotAdoptedAgain(t *testing.T) {
	repo := t.TempDir()

	manifest := aDropOfSuppliedBinaries(t, repo, "macos-arm64")

	paths := ffmpegPathsFor(repo, manifest, "macos-arm64")

	if err := adoptSuppliedFFmpeg(paths, "macos-arm64", suppliedFFMpegLicence(manifest, "macos-arm64", aRunOfTheBinaries())); err != nil {
		t.Fatalf("adopting the binaries: %v", err)
	}

	plan, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, ffmpegState{bundleIsCached: true}, theReleaseHost)
	if err != nil {
		t.Fatalf("the build refused cached supplied binaries: %v", err)
	}

	if len(plan.Steps) != 0 {
		t.Errorf("cached supplied binaries are adopted again: %d steps", len(plan.Steps))
	}

	if plan.Skipped {
		t.Error("cached supplied binaries are skipped, and the release run ships no archive for them")
	}

	if plan.Adopts {
		t.Error("the plan adopts binaries the cache already holds, so the run looks for a drop it does not need")
	}

	if plan.Says == "" {
		t.Error("the build says nothing about reusing supplied binaries")
	}
}

// The drop is consumed: the binaries move out of the directory a server binary embeds, and what stays
// there are the gzipped binaries and their licence. Raw binaries left in that directory are embedded next to the
// gzipped ones, and every server built for that target carries about ten megabytes of uncompressed program.
func TestSuppliedBinariesAreAdoptedIntoTheDirectoryTheServerEmbeds(t *testing.T) {
	repo := t.TempDir()

	manifest := aDropOfSuppliedBinaries(t, repo, "macos-arm64")

	paths := ffmpegPathsFor(repo, manifest, "macos-arm64")

	licence := suppliedFFMpegLicence(manifest, "macos-arm64", aRunOfTheBinaries())

	digests := map[string]string{}

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		body, err := os.ReadFile(suppliedDropFor(paths, "macos-arm64")[name])
		if err != nil {
			t.Fatalf("reading the dropped %s: %v", name, err)
		}

		digests[name] = sha256Of(body)
	}

	if err := adoptSuppliedFFmpeg(paths, "macos-arm64", licence); err != nil {
		t.Fatalf("adopting the binaries: %v", err)
	}

	if err := publishFFmpeg(paths, manifest, "macos-arm64", licence); err != nil {
		t.Fatalf("publishing the binaries: %v", err)
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

	// The licence text travels with the cache entry: a run reusing the entry publishes the words the run
	// that compiled the binaries wrote, and not words it composes about a compile it did not take part in.
	if !aFileIsThere(filepath.Join(paths.cached, "LICENCE.md")) {
		t.Errorf("the cache entry %s carries no licence text, so a reusing run has nothing to put in the archive", paths.cached)
	}

	// The binaries the server writes out of its own binary are the binaries that were dropped: gzip is
	// lossless, and the ad-hoc code signature signed binaries carry is a hash over those exact bytes. The
	// bundle package's own tests are what prove a directory holding them is a directory it materializes.
	for _, program := range []struct {
		name   string
		digest string
	}{
		{"ffmpeg", digests["ffmpeg"]},
		{"ffprobe", digests["ffprobe"]},
	} {
		for _, dir := range []string{paths.cached, paths.bundle} {
			body, err := readGzip(filepath.Join(dir, program.name+".gz"))
			if err != nil {
				t.Fatalf("reading %s in %s: %v", program.name+".gz", dir, err)
			}

			if sha256Of(body) != program.digest {
				t.Errorf("%s in %s is %s, and the binaries dropped were %s", program.name, dir, sha256Of(body), program.digest)
			}
		}
	}
}

// aRunOfTheBinaries is the run of a working build: the host that is the target, the codecs the audio-only
// recipe registers, and the six recordings of the server's own audio.
func aRunOfTheBinaries() theRunOfTheBinaries {
	return theRunOfTheBinaries{
		Host:        "darwin-arm64",
		Version:     "ffmpeg version 9.0.2 Copyright (c) 2000-2025 the FFmpeg developers",
		Codecs:      codecRegistry{Audio: theAudioCodecNames},
		Conversions: 6,
		Productions: []production{
			{Input: "wav", Mode: "plain", Bytes: 7675, Duration: "2.128000"},
			{Input: "wav", Mode: "norm", Bytes: 12251, Duration: "3.010667"},
			{Input: "wav", Mode: "loudnorm", Bytes: 12131, Duration: "3.010667"},
			{Input: "mp3", Mode: "plain", Bytes: 7756, Duration: "2.128000"},
			{Input: "mp3", Mode: "norm", Bytes: 12251, Duration: "3.010667"},
			{Input: "mp3", Mode: "loudnorm", Bytes: 12131, Duration: "3.010667"},
		},
	}
}

// The licence a supplied archive carries says what those ffmpeg binaries are and where they came from: the
// host that compiled them, the source and recipe they were built from, the command that built them, and the
// codecs the run recorded. It names no digest of a program, because the build checks nothing about these
// binaries beyond that they run and which machine code they hold.
func TestTheBuildSaysWhatSuppliedBinariesAreAndUnderWhatLicence(t *testing.T) {
	manifest := bundle.ReadManifest()

	run := aRunOfTheBinaries()

	text := suppliedFFMpegLicence(manifest, "macos-arm64", run)

	for _, want := range []string{
		"Lesser General Public License",
		manifest.FFmpeg,
		manifest.Source.URL,
		manifest.Source.SHA256,
		manifest.Recipe[0],
		"compiled for audio",
		"go run ./cmd/build ffmpeg macos-arm64",
		"compiled on",
		"macos-arm64",
		run.Host,
		theAudioCodecNames[0],
		"no video codec",
		"6 recordings",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the licence text for supplied binaries names no %q:\n%s", want, text)
		}
	}

	for _, not := range []string{
		"programs",
		"ffmpeg  sha256",
		"compiled from source for macos-arm64. It converts",
	} {
		if strings.Contains(text, not) {
			t.Errorf("the licence text says %q, which is not true of binaries this build did not compile in a container:\n%s", not, text)
		}
	}
}

// A cache entry older than the licence text travelling with it says only what the manifest says, and
// names this machine's cache as where the binaries came from. It invents no host and no run.
func TestTheLicenceOfBinariesReusedFromCacheNamesNoRunItDidNotSee(t *testing.T) {
	manifest := bundle.ReadManifest()

	text := suppliedFFMpegLicence(manifest, "macos-arm64", theRunOfTheBinaries{})

	for _, want := range []string{
		"the machine that compiled them",
		manifest.Source.SHA256,
		"macos-arm64",
		"reuses what that run published in its own cache",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the licence text for cached binaries names no %q:\n%s", want, text)
		}
	}

	for _, not := range []string{
		"darwin-arm64",
		"recordings",
		"The run that adopted them asked",
	} {
		if strings.Contains(text, not) {
			t.Errorf("the licence text says %q, which is a run this build did not witness:\n%s", not, text)
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
// very machine the supplied ffmpeg binaries are built on.
func TestTheFFmpegTargetOfARunThatNamedNoTargetIsTheMachinesReleaseTarget(t *testing.T) {
	want := releaseTargetWordFor(runtime.GOOS, runtime.GOARCH)

	if got := ffmpegTarget(options{}); got != want {
		t.Errorf("with no target named the ffmpeg phase builds for %q, want the release target %q of this %s-%s machine", got, want, runtime.GOOS, runtime.GOARCH)
	}
}

// The machine supplied ffmpeg binaries are compiled on is the machine that runs them, and the build can be
// standing on
// it: the operator on the Apple Silicon Mac asked for the ffmpeg of the machine they are on, and the
// repository in front of them names the source, its digest, the recipe, the flags that floor sets, the make
// program that build needs, and the check that proves the result. The compile is what that run asked for,
// and it is what only that machine can do.
func TestTheSuppliedTargetOnItsOwnHostCompilesItsOwnBinaries(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64"} {
		manifest := bundle.ReadManifest()

		plan, err := planFFmpeg(options{target: target}, t.TempDir(), manifest, aNativeTree(target), aMachineForTarget(target))
		if err != nil {
			t.Errorf("%s: the build refused to compile the binaries on the host that is the target: %v", target, err)
			continue
		}

		if !plan.Native {
			t.Errorf("%s: the plan does not compile the binaries on the host that is the target", target)
			continue
		}

		if plan.Skipped {
			t.Errorf("%s: the plan skips the target whose binaries it can compile, and the operator on that host is told to build binaries they are standing on", target)
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
				t.Errorf("%s: the compile of binaries built on this host runs a container: %v", target, s.args)
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
			t.Errorf("%s: the compile cross-compiles on the machine the binaries run on", target)
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
				t.Errorf("%s: the binaries are signed before they are stripped, and the signature is over bytes strip rewrote", target)
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

// ffmpeg's configure writes its build files into the directory it is run from: the config.mak its
// Makefile includes on its second line, the config headers, and the directory skeleton of the tree it is
// configuring. Run it from anywhere else and two things happen: the tree it configured has no
// config.mak, so the make that follows fails reading its own Makefile, and the directory it was run from
// gains the source tree's directories. The compile's own steps therefore run in the tree the compile
// unpacked, and the repository is where the tar command is run from, not where the source is configured
// or built.
func TestTheNativeCompileRunsItsStepsInTheTreeItUnpacked(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64"} {
		manifest := bundle.ReadManifest()

		plan, err := planFFmpeg(options{target: target}, t.TempDir(), manifest, aNativeTree(target), aMachineForTarget(target))
		if err != nil {
			t.Errorf("%s: the build refused to compile the binaries on the host that is the target: %v", target, err)
			continue
		}

		work := filepath.Join(plan.Paths.scratch, sourceDir(manifest))

		unpack := stepFor(plan.Steps, "unpack")

		if got := argAfter(unpack.args, "-C"); got != plan.Paths.scratch {
			t.Errorf("%s: the unpack extracts into %s, want %s: the tree the rest of the compile reads is the one the unpack writes", target, got, plan.Paths.scratch)
		}

		for _, label := range []string{"configure", "make", "strip", "sign"} {
			s := stepFor(plan.Steps, label)

			if s.name == "" {
				continue
			}

			if s.dir != work {
				t.Errorf("%s: the %s step runs in %s, want the tree the unpack wrote (%s)", target, label, s.dir, work)
			}
		}

		if name := stepFor(plan.Steps, "configure").name; name != filepath.Join(work, "configure") {
			t.Errorf("%s: the configure is %s, want the configure of the tree the unpack wrote (%s)", target, name, work)
		}
	}
}

// The two macOS architectures ship one toolchain: the Command Line Tools' clang produces x86_64 on an
// Apple Silicon Mac and arm64 on an Intel Mac, the macOS SDK carries the libraries of both, and codesign
// signs a program of either architecture on either machine. A FreeBSD ffmpeg binary is statically linked
// against FreeBSD's own libc, which is the whole point of compiling it there, so a host of another
// operating system has nothing to link it against and compiles nothing.
func TestAMacOSHostCompilesTheOtherArchitecturesBinaries(t *testing.T) {
	for _, tc := range []struct {
		target  string
		machine host
	}{
		{target: "macos-amd64", machine: host{goos: "darwin", goarch: "arm64"}},
		{target: "macos-arm64", machine: host{goos: "darwin", goarch: "amd64"}},
	} {
		plan, err := planFFmpeg(options{target: tc.target}, t.TempDir(), bundle.ReadManifest(), aNativeTree(tc.target), tc.machine)
		if err != nil {
			t.Errorf("%s on a %s host: the build refused to compile the binaries: %v", tc.target, tc.machine.goarch, err)
			continue
		}

		if !plan.Native {
			t.Errorf("%s on a %s host: the build compiled nothing, and one macOS toolchain produces both architectures", tc.target, tc.machine.goarch)
		}
	}

	for _, machine := range []host{
		{goos: "darwin", goarch: "arm64"},
		{goos: "linux", goarch: "amd64"},
		{goos: "windows", goarch: "amd64"},
	} {
		plan, err := planFFmpeg(options{target: "freebsd-amd64"}, t.TempDir(), bundle.ReadManifest(), aNativeTree("freebsd-amd64"), machine)
		if err != nil {
			t.Errorf("freebsd-amd64 on a %s host: %v", machine.goarch, err)
			continue
		}

		if plan.Native {
			t.Errorf("freebsd-amd64 on a %s host: the build compiled a FreeBSD ffmpeg on a machine that is not FreeBSD, and it is linked against FreeBSD's own libc", machine.goarch)
		}

		if !plan.Skipped {
			t.Errorf("freebsd-amd64 on a %s host: the run promises an ffmpeg for a target it has no binaries for", machine.goarch)
		}
	}
}

// The cache entry of a supplied target is named by the manifest's record and the target, and not by the
// machine computing it: the Mac that compiled the binaries and a run reusing them look for the same entry.
// Each machine's cache is its own tree and gitignored, so an Apple Silicon Mac and an Intel Mac compiling
// the Intel target key the same entry in their own caches, and each cache holds what that machine compiled.
// The two macOS targets key differently, which is what lets one Mac compile both architectures into two
// entries.
func TestTheCacheEntryOfSuppliedBinariesIsTheSameEntryOnEveryHost(t *testing.T) {
	manifest := bundle.ReadManifest()

	fromTheIntelHost := cacheKey(manifest, "macos-amd64")
	fromTheAppleSiliconHost := cacheKey(manifest, "macos-amd64")

	if fromTheIntelHost != fromTheAppleSiliconHost {
		t.Errorf("the same manifest and target give two cache keys, %s and %s", fromTheIntelHost, fromTheAppleSiliconHost)
	}

	if cacheKey(manifest, "macos-amd64") == cacheKey(manifest, "macos-arm64") {
		t.Error("macos-amd64 and macos-arm64 share a cache entry, and one Mac compiling both writes one entry")
	}

	repo := t.TempDir()

	warm := ffmpegState{bundleIsCached: true}

	onIntel, err := planFFmpeg(options{target: "macos-amd64"}, repo, manifest, warm, aMachineForTarget("macos-amd64"))
	if err != nil {
		t.Fatalf("the Intel host refused to reuse the cached binaries: %v", err)
	}

	onAppleSilicon, err := planFFmpeg(options{target: "macos-amd64"}, repo, manifest, warm, host{goos: "darwin", goarch: "arm64"})
	if err != nil {
		t.Fatalf("the Apple Silicon host refused to reuse the cached binaries: %v", err)
	}

	if onIntel.Paths.cached != onAppleSilicon.Paths.cached {
		t.Errorf("the two hosts look for the binaries in different entries: %s and %s", onIntel.Paths.cached, onAppleSilicon.Paths.cached)
	}
}

// A compile across the two macOS architectures names the architecture it is building for: ffmpeg's own
// architecture switch, the switch that tells configure not to run a test program of an architecture the
// host may not execute, and clang's -arch in the compiler and linker flags, beside the flags the manifest
// record names. A compile on the target's own host names none of them, because there the host's own
// architecture is the target's and the compiler's default is the right answer.
func TestACompileAcrossTheTwoArchitecturesNamesTheTargetArchitecture(t *testing.T) {
	for _, tc := range []struct {
		target     string
		machine    host
		ffmpegArch string
		clangArch  string
	}{
		{target: "macos-amd64", machine: host{goos: "darwin", goarch: "arm64"}, ffmpegArch: "x86_64", clangArch: "x86_64"},
		{target: "macos-arm64", machine: host{goos: "darwin", goarch: "amd64"}, ffmpegArch: "aarch64", clangArch: "arm64"},
	} {
		plan, err := planFFmpeg(options{target: tc.target}, t.TempDir(), bundle.ReadManifest(), aNativeTree(tc.target), tc.machine)
		if err != nil {
			t.Errorf("%s on a %s host: %v", tc.target, tc.machine.goarch, err)
			continue
		}

		configure := stepFor(plan.Steps, "configure")

		for _, want := range []string{"--arch=" + tc.ffmpegArch, "--enable-cross-compile"} {
			if !carries(configure.args, want) {
				t.Errorf("%s compiled on a %s host: configure does not name %s", tc.target, tc.machine.goarch, want)
			}
		}

		for _, prefix := range []string{"--extra-cflags=", "--extra-ldflags="} {
			flags := theFlagNamed(configure.args, prefix)

			if !strings.Contains(flags, "-arch "+tc.clangArch) {
				t.Errorf("%s compiled on a %s host: %s does not name -arch %s: the compiler's default is the host's architecture", tc.target, tc.machine.goarch, prefix, tc.clangArch)
			}

			if !strings.Contains(flags, "-mmacosx-version-min=11.0") {
				t.Errorf("%s compiled on a %s host: %s drops the flag the manifest record names: %q", tc.target, tc.machine.goarch, prefix, flags)
			}
		}
	}

	plan, err := planFFmpeg(options{target: "macos-arm64"}, t.TempDir(), bundle.ReadManifest(), aNativeTree("macos-arm64"), aMachineForTarget("macos-arm64"))
	if err != nil {
		t.Fatalf("the build refused to compile the ffmpeg binaries of the machine it is standing on: %v", err)
	}

	own := stepFor(plan.Steps, "configure")

	if carries(own.args, "--enable-cross-compile") || carries(own.args, "--arch=") {
		t.Errorf("a compile on the target's own host names a cross-compile: %v", own.args)
	}

	if carries(own.args, "-arch ") {
		t.Errorf("a compile on the target's own host names -arch, asking the compiler for the architecture it is standing on: %v", own.args)
	}
}

// theFlagNamed is the value of one flag-bearing argument, which is how a test reads what a step was told
// to pass.
func theFlagNamed(args []string, prefix string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return arg
		}
	}

	return ""
}

// A run asked for the machine it is standing on, in the words go names that machine by, compiles the
// ffmpeg binaries of the release target that machine is: the alias is a spelling, and the phase that compiles sees the
// target, not the spelling.
func TestTheFFmpegPhaseAskedForTheWordGoNamesTheMachineByPlansTheReleaseTarget(t *testing.T) {
	o, err := parseArgs([]string{"ffmpeg", "darwin-arm64"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	plan, err := planFFmpeg(o, t.TempDir(), bundle.ReadManifest(), aNativeTree("macos-arm64"), aMachineForTarget("macos-arm64"))
	if err != nil {
		t.Fatalf("the build refused to compile the ffmpeg binaries of the machine it is standing on: %v", err)
	}

	if plan.Target != "macos-arm64" {
		t.Errorf("the plan is for %s, want macos-arm64", plan.Target)
	}

	if !plan.Native {
		t.Errorf("the plan for %s compiles nothing on the machine that is that target", plan.Target)
	}
}

// argAfter is the argument a command names after a flag, which is how a test reads where a step is
// pointed at.
func argAfter(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}

// The compile is what the target's own host does. A machine outside it compiles nothing, and says the
// binaries were not supplied, the host that compiles them, the command that compiles them there, and the
// directory to drop them in.
func TestTheSuppliedTargetOnAnotherHostCompilesNothing(t *testing.T) {
	for _, target := range []string{"macos-amd64", "macos-arm64", "freebsd-amd64"} {
		plan, err := planFFmpeg(options{target: target}, t.TempDir(), bundle.ReadManifest(), aNativeTree(target), theReleaseHost)
		if err != nil {
			t.Errorf("%s: a machine that is not the target's host failed the run: %v", target, err)
			continue
		}

		if plan.Native {
			t.Errorf("%s: the build compiled %s binaries on a machine that cannot run them", target, target)
		}

		if !plan.Skipped {
			t.Errorf("%s: the build does not skip a target whose binaries it cannot produce", target)
		}

		if len(plan.Steps) != 0 {
			t.Errorf("%s: the build has %d steps for a target it is not standing on", target, len(plan.Steps))
		}
	}
}

// Binaries the maintainer dropped are adopted on their own host exactly as they are adopted anywhere
// else: the compile is what the build does when there is nothing to adopt, not a replacement for the drop.
func TestSuppliedBinariesDroppedOnTheirOwnHostAreAdoptedAndNotCompiled(t *testing.T) {
	repo := t.TempDir()

	manifest := aDropOfSuppliedBinaries(t, repo, "macos-arm64")

	plan, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, aNativeTree("macos-arm64"), aMachineForTarget("macos-arm64"))
	if err != nil {
		t.Fatalf("the build refused the binaries dropped for it: %v", err)
	}

	if plan.Native {
		t.Error("the build compiled binaries when binaries are sitting in the directory it embeds")
	}

	if !plan.Adopts {
		t.Error("the plan adopts nothing: the host standing on the target is the host that can run them")
	}
}

// Binaries the cache holds are reused on their own host, and nothing is compiled over them: the cache key
// is the manifest's record and the target, so a warm entry is what this machine's own run published.
func TestSuppliedBinariesInCacheAreReusedOnTheirOwnHost(t *testing.T) {
	repo := t.TempDir()

	manifest := aDropOfSuppliedBinaries(t, repo, "macos-arm64")

	state := aNativeTree("macos-arm64")
	state.bundleIsCached = true

	plan, err := planFFmpeg(options{target: "macos-arm64"}, repo, manifest, state, aMachineForTarget("macos-arm64"))
	if err != nil {
		t.Fatalf("the build refused cached binaries: %v", err)
	}

	if plan.Native {
		t.Error("the build compiles binaries the cache already holds")
	}

	if plan.Adopts {
		t.Error("the build adopts binaries the cache already holds, and looks for a drop it does not need")
	}

	if len(plan.Steps) != 0 {
		t.Errorf("the build has %d steps for binaries it already has", len(plan.Steps))
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

// theReleaseHost is the machine that produces the releases: Linux, and no host supplied ffmpeg binaries
// run on.
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

// Every step that speaks to a container names the program the run resolved. A step that names a program
// the machine does not have is a step that fails on a machine that can build the image, and a plan that
// names the program in its image builds and composes it into its runs is a plan whose steps disagree
// about who they are talking to.
func TestEveryContainerStepOfTheFfmpegPlanNamesTheProgramTheRunResolved(t *testing.T) {
	manifest := bundle.ReadManifest()

	for _, program := range []string{podmanProgram, dockerProgram} {
		plan, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", manifest, ffmpegState{program: program}, theReleaseHost)
		if err != nil {
			t.Fatalf("planFFmpeg for %s: %v", program, err)
		}

		if len(plan.Steps) == 0 {
			t.Fatalf("the plan for %s composed no steps", program)
		}

		for _, s := range plan.Steps {
			if s.name != program {
				t.Errorf("the %s step speaks to %q, want the program the run resolved: %q", s.label, s.name, program)
			}
		}

		// The steps that enter a container say it in the vocabulary that program understands, and every
		// image build names the file it builds from: docker looks for Dockerfile and does not find
		// Containerfile.
		for _, s := range plan.Steps {
			if len(s.args) == 0 {
				t.Fatalf("the %s step carries no arguments", s.label)
			}

			switch s.args[0] {
			case "run":
				begins := theContainerProgram{name: program}.run()

				if len(s.args) < len(begins) {
					t.Errorf("the %s step runs %v, which is shorter than the arguments %s needs", s.label, s.args, program)

					continue
				}

				if !sameArgs(s.args[:len(begins)], begins) {
					t.Errorf("the %s step runs %v, want the arguments %s understands: %v", s.label, s.args, program, begins)
				}
			case "build":
				if file := theFlagValue(s.args, "-f"); file == "" {
					t.Errorf("the %s image build does not name the file it builds from: docker looks for Dockerfile and does not find Containerfile",
						s.label)
				}
			}
		}
	}
}
