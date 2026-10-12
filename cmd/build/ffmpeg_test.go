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
	"os"
	"path/filepath"
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
	plan, err := planFFmpeg(options{}, "/repo", bundle.ReadManifest(), anUncompiledTree())
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
		plan, err := planFFmpeg(options{target: target}, t.TempDir(), bundle.ReadManifest(), anUncompiledTree())
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

	plan, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", manifest, anUncompiledTree())
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
	plan, err := planFFmpeg(options{target: "windows-amd64"}, "/repo", bundle.ReadManifest(), anUncompiledTree())
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
	_, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", bundle.ReadManifest(), ffmpegState{})
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

// A second build that changes nothing does not compile anything again.
func TestAFfmpegAlreadyCompiledForTheSameRecipeIsNotCompiledAgain(t *testing.T) {
	plan, err := planFFmpeg(
		options{target: "linux-arm64"},
		"/repo",
		bundle.ReadManifest(),
		ffmpegState{podmanIsThere: true, bundleIsCached: true},
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
	plan, err := planFFmpeg(options{target: "linux-arm64"}, "/repo", bundle.ReadManifest(), anUncompiledTree())
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

	if err := publishFFmpeg(paths, manifest, "linux-arm64"); err != nil {
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
