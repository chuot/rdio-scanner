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
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// The tool's four contacts with the machine it runs on: which tree it builds,
// which programs it can run, whether a directory is there, and what one step
// does. The plan itself is composed and tested in commands.go.

// the tool builds the tree git names, and without git it builds
// the directory it was run from
func TestTheRepositoryRootIsTheTreeGitSays(t *testing.T) {
	repo, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}

	for _, part := range []string{"go.mod", filepath.Join("cmd", "build")} {
		if _, err := os.Stat(filepath.Join(repo, part)); err != nil {
			t.Errorf("the tree git names, %q, does not hold %s", repo, part)
		}
	}
}

// a required program is found by the name the machine answers,
// and the failure names the program that was looked for
func TestTheExecutableIsTheOneTheMachineHas(t *testing.T) {
	path, err := resolveExecutable("go")
	if err != nil {
		t.Fatalf("resolveExecutable(go): %v", err)
	}

	if path == "" {
		t.Error("the toolchain running this test is not found by the name it is known by")
	}

	if _, err := resolveExecutable("not-a-program-on-the-path"); err == nil {
		t.Error("a program that is not on the path was accepted, want a failure that names it")
	}
}

// a directory is known by its existence: the tool looks at the tree to see
// whether the client dependencies are already installed
func TestADirectoryIsKnownByItsExistence(t *testing.T) {
	dir := t.TempDir()

	file := filepath.Join(dir, "a-file")

	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	if !directoryExists(dir) {
		t.Errorf("%q is a directory the tool does not see", dir)
	}

	if directoryExists(file) {
		t.Errorf("%q is a file the tool counts as a directory", file)
	}

	if directoryExists(filepath.Join(dir, "not-there")) {
		t.Errorf("a directory that is not there is seen as installed")
	}
}

// a step runs in its own directory, the machine's environment in front of its
// own, and the program's exit is the step's result: a failing step ends the run
func TestAStepRunsInItsOwnDirectoryWithItsOwnExit(t *testing.T) {
	dir := t.TempDir()

	file := filepath.Join(dir, "a-file")

	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	// the same program asked the same question in two directories:
	// only the one holding the file answers yes
	if err := runStep(step{name: "sh", args: []string{"-c", "test -f a-file"}, dir: dir}); err != nil {
		t.Errorf("a step run in %q could not find the file it is standing next to: %v", dir, err)
	}

	if err := runStep(step{name: "sh", args: []string{"-c", "test -f a-file"}, dir: filepath.Dir(dir)}); err == nil {
		t.Error("a step run outside the directory that holds the file answered as if it were inside it")
	}

	// The extra environment reaches the program, and the machine's own environment is still there
	// under it.
	if err := runStep(step{
		name: "sh",
		args: []string{"-c", "test -n \"$STEP_MARK\" -a -n \"$PATH\""},
		dir:  dir,
		env:  []string{"STEP_MARK=here"},
	}); err != nil {
		t.Errorf("the step's own environment did not reach the program with the machine's behind it: %v", err)
	}

	if err := runStep(step{name: "sh", args: []string{"-c", "exit 3"}, dir: dir}); err == nil {
		t.Error("a step whose program says no is reported as having succeeded")
	}
}

// a clean run removes the release output, the client dependencies, and the bundle
// contents, and leaves the .gitkeep placeholders that keep a plain go build compiling
func TestCleanRemovesTheReleaseOutputAndTheBundleContentsAndLeavesThePlaceholders(t *testing.T) {
	repo := t.TempDir()

	writeTreeFile(t, repo, filepath.Join("dist", "linux-amd64", "rdio-scanner"), "the binary")
	writeTreeFile(t, repo, filepath.Join("dist", "rdio-scanner-linux-amd64-v8.0.0.zip"), "the archive")
	writeTreeFile(t, repo, filepath.Join(presentationDir, "node_modules", "esbuild", "package.json"), "the dependency")

	for _, app := range []string{subScanner, subAdmin} {
		bundle := filepath.Join(presentationDir, app, "frontend", "bundle")

		writeTreeFile(t, repo, filepath.Join(bundle, ".gitkeep"), "")
		writeTreeFile(t, repo, filepath.Join(bundle, "index.html"), "the bundle")
		writeTreeFile(t, repo, filepath.Join(bundle, "assets", "app.js"), "the bundle")
	}

	removed, err := cleanTree(repo)
	if err != nil {
		t.Fatalf("cleanTree: %v", err)
	}

	for _, path := range []string{"dist", filepath.Join(presentationDir, "node_modules")} {
		if directoryExists(filepath.Join(repo, path)) {
			t.Errorf("%s survived a clean run", path)
		}
	}

	for _, app := range []string{subScanner, subAdmin} {
		bundle := filepath.Join(repo, presentationDir, app, "frontend", "bundle")

		if _, err := os.Stat(filepath.Join(bundle, ".gitkeep")); err != nil {
			t.Errorf("%s lost the placeholder that keeps a plain go build compiling", bundle)
		}

		for _, name := range []string{"index.html", "assets"} {
			if _, err := os.Stat(filepath.Join(bundle, name)); err == nil {
				t.Errorf("%s kept %s, which a clean run removes", bundle, name)
			}
		}
	}

	// The release output, the dependencies, and the two bundles' contents.
	if len(removed) != 6 {
		t.Errorf("the clean run named %d removals, want 6: %v", len(removed), removed)
	}
}

// writeTreeFile puts a file into a tree the tool is being asked to clean.
func writeTreeFile(t *testing.T, repo string, path string, contents string) {
	t.Helper()

	full := filepath.Join(repo, path)

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(full), err)
	}

	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// the server binary is current when it is newer than every Go file it is built
// from; the embedded ffmpeg is not a Go file and has its own cache check
func TestTheServerBinaryIsCurrentWhenItIsNewerThanEveryGoFileItIsBuiltFrom(t *testing.T) {
	repo := t.TempDir()

	sources := []string{
		filepath.Join("cmd", "rdio-scanner", "main.go"),
		filepath.Join("internal", "presentation", "scanner", "frontend", "bundle", "bundle.go"),
		filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", "bundle.go"),
	}

	for _, path := range sources {
		writeTreeFile(t, repo, path, "the source")
		setMTime(t, repo, path, date(2026, 1, 1))
	}

	output := filepath.Join("dist", "linux-amd64", "rdio-scanner")

	if current, err := serverIsCurrent(repo, output); err != nil || current {
		t.Errorf("a binary that is not there is reported as current: current %v, err %v", current, err)
	}

	writeTreeFile(t, repo, output, "the binary")
	setMTime(t, repo, output, date(2026, 1, 2))

	if current, err := serverIsCurrent(repo, output); err != nil || !current {
		t.Errorf("a binary newer than every Go file it is built from is not reported as current: current %v, err %v", current, err)
	}

	setMTime(t, repo, sources[2], date(2026, 1, 3))

	if current, err := serverIsCurrent(repo, output); err != nil || current {
		t.Errorf("a Go file newer than the binary is not noticed: current %v, err %v", current, err)
	}
}

// the server embeds the ffmpeg pair and the manifest that describes it, so a pair
// newer than the binary means that binary carries an ffmpeg other than the one the
// release archive's licence names
func TestTheServerBinaryIsNotCurrentWhenTheFfmpegItEmbedsIsNewer(t *testing.T) {
	repo := t.TempDir()

	goSources := []string{
		filepath.Join("cmd", "rdio-scanner", "main.go"),
		filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", "bundle.go"),
	}
	for _, path := range goSources {
		writeTreeFile(t, repo, path, "the source")
		setMTime(t, repo, path, date(2026, 1, 1))
	}

	// The ffmpeg phase publishes the pair into the tree it embeds on every run that
	// covers the target, whether it compiled the pair or read it back from its cache.
	pair := []string{
		filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", "linux-amd64", "ffmpeg.gz"),
		filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", "linux-amd64", "ffprobe.gz"),
	}
	for _, path := range pair {
		writeTreeFile(t, repo, path, "the compiled ffmpeg")
		setMTime(t, repo, path, date(2026, 1, 1))
	}

	output := filepath.Join("dist", "linux-amd64", "rdio-scanner")
	writeTreeFile(t, repo, output, "the binary")
	setMTime(t, repo, output, date(2026, 1, 2))

	if current, err := serverIsCurrent(repo, output); err != nil || !current {
		t.Errorf("a binary newer than everything the phase has published is not reported as current: current %v, err %v", current, err)
	}

	for _, path := range pair {
		writeTreeFile(t, repo, path, "the ffmpeg this run published")
		setMTime(t, repo, path, date(2026, 1, 3))
	}

	if current, err := serverIsCurrent(repo, output); err != nil || current {
		t.Errorf("the ffmpeg the server embeds is newer than the binary that embeds it, and the binary is reported as current: current %v, err %v", current, err)
	}
}

// the bundle embeds the manifest that names the ffmpeg release, so a manifest newer
// than the binary means the binary describes a different ffmpeg than the release
// archive's licence does
func TestTheServerBinaryIsNotCurrentWhenTheManifestOfTheFfmpegItEmbedsIsNewer(t *testing.T) {
	repo := t.TempDir()

	goSources := []string{
		filepath.Join("cmd", "rdio-scanner", "main.go"),
		filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", "bundle.go"),
	}
	for _, path := range goSources {
		writeTreeFile(t, repo, path, "the source")
		setMTime(t, repo, path, date(2026, 1, 1))
	}

	embedded := filepath.Join("internal", "infrastructure", "ffmpeg", "bundle", "manifest.json")
	writeTreeFile(t, repo, embedded, `{"release": "ffmpeg-7.1.1"}`)
	setMTime(t, repo, embedded, date(2026, 1, 1))

	output := filepath.Join("dist", "linux-amd64", "rdio-scanner")
	writeTreeFile(t, repo, output, "the binary")
	setMTime(t, repo, output, date(2026, 1, 2))

	if current, err := serverIsCurrent(repo, output); err != nil || !current {
		t.Errorf("a binary newer than the manifest it embeds is not reported as current: current %v, err %v", current, err)
	}

	writeTreeFile(t, repo, embedded, `{"release": "ffmpeg-8.0.1"}`)
	setMTime(t, repo, embedded, date(2026, 1, 3))

	if current, err := serverIsCurrent(repo, output); err != nil || current {
		t.Errorf("the manifest the server embeds is newer than the binary that embeds it, and the binary is reported as current: current %v, err %v", current, err)
	}
}

// the ffmpeg phase writes disposable scratch no later build reads and a cache every
// later build reads; a clean run removes the first and leaves the second standing
func TestACleanRunRemovesTheFfmpegScratchAndLeavesTheCacheStanding(t *testing.T) {
	repo := t.TempDir()

	manifest := bundle.ReadManifest()

	scratch := filepath.Join(".build", "ffmpeg", "scratch", "linux-amd64", "ffmpeg-9.0.2", "config.h")
	tarball := filepath.Join(".build", "ffmpeg", "src", sourceName(manifest))
	context := filepath.Join(".build", "ffmpeg", "context", "Dockerfile")
	working := filepath.Join(".build", "ffmpeg", "run", "macos-arm64", "ffmpeg")

	cache := filepath.Join(".build", "ffmpeg", manifest.FFmpeg, cacheKey(manifest, "linux-amd64"), "linux-amd64", "ffmpeg")

	for _, path := range []string{scratch, tarball, context, working, cache} {
		writeTreeFile(t, repo, path, "the file a build wrote")
	}

	removed, err := cleanTree(repo)
	if err != nil {
		t.Fatalf("cleanTree: %v", err)
	}

	for _, path := range []string{scratch, tarball, context, working} {
		if _, err := os.Stat(filepath.Join(repo, path)); err == nil {
			t.Errorf("%s survived a clean run, and it is what no later build reads", path)
		}
	}

	if _, err := os.Stat(filepath.Join(repo, cache)); err != nil {
		t.Errorf("the clean run removed the compiled ffmpeg a later build reads: %v", err)
	}

	for _, name := range []string{"scratch", "src", "context", "run"} {
		if !carriesAPath(removed, filepath.Join(".build", "ffmpeg", name)) {
			t.Errorf("the clean run named %v and did not name %s", removed, name)
		}
	}
}

// a clean run prunes cache entries whose key no target the manifest records computes;
// age decides nothing, only whether the manifest as it is now could ask for the entry.
// see the ffmpeg-cache-pruning maintainer note.
func TestACleanRunPrunesTheCacheEntriesTheManifestCannotProduce(t *testing.T) {
	repo := t.TempDir()

	manifest := bundle.ReadManifest()

	live := filepath.Join(".build", "ffmpeg", manifest.FFmpeg, cacheKey(manifest, "linux-amd64"), "linux-amd64", "ffmpeg")

	// The entry a manifest edit replaced: the same release, a key nothing computes now.
	staleKey := filepath.Join(".build", "ffmpeg", manifest.FFmpeg, "a-key-the-manifest-replaced")

	// the whole cache of a release the manifest no longer pins
	older := filepath.Join(".build", "ffmpeg", "8.9.0")

	writeTreeFile(t, repo, live, "the ffmpeg the manifest asks for")
	writeTreeFile(t, repo, filepath.Join(staleKey, "linux-amd64", "ffmpeg"), "the ffmpeg an older manifest produced")
	writeTreeFile(t, repo, filepath.Join(older, "a-key-of-that-release", "linux-amd64", "ffmpeg"), "the ffmpeg an older release compiled")

	removed, err := cleanTree(repo)
	if err != nil {
		t.Fatalf("cleanTree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(repo, live)); err != nil {
		t.Errorf("the clean run removed the cache entry the manifest asks for: %v", err)
	}

	for _, path := range []string{staleKey, older} {
		if _, err := os.Stat(filepath.Join(repo, path)); err == nil {
			t.Errorf("%s survived a clean run, and the manifest cannot produce it", path)
		}

		if !carriesAPath(removed, path) {
			t.Errorf("the clean run named %v and did not name %s", removed, path)
		}
	}
}

// carriesAPath says whether a list of removed paths names a path,
// repo-relative names included: a removal nobody is told about is a silent tree change.
func carriesAPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want || strings.HasSuffix(path, string(filepath.Separator)+want) {
			return true
		}
	}

	return false
}

// the bound on a release run's independent server compiles is about the machine's
// memory and the log's readability, not correctness: too many at once stalls a
// small machine, and a value that is not a count of work is refused.
func TestTheReleaseRunBoundsItsServerCompiles(t *testing.T) {
	cpus := runtime.NumCPU()

	// More ready targets than cores: the bound is the cores.
	workers, err := distWorkers(cpus+3, noEnvironment)
	if err != nil {
		t.Fatalf("distWorkers: %v", err)
	}

	if workers != cpus {
		t.Errorf("%d workers for %d ready targets on %d cores, want the cores", workers, cpus+3, cpus)
	}

	// fewer ready targets than cores: the bound is the targets,
	// a worker with nothing to do does nothing
	workers, err = distWorkers(2, noEnvironment)
	if err != nil {
		t.Fatalf("distWorkers: %v", err)
	}

	if workers != 2 {
		t.Errorf("%d workers for 2 ready targets, want 2", workers)
	}

	// The operator's bound, when it is lower than the cores, is the bound.
	workers, err = distWorkers(8, anEnvironment(map[string]string{buildJobsEnv: "2"}))
	if err != nil {
		t.Fatalf("distWorkers: %v", err)
	}

	if workers != 2 {
		t.Errorf("%d workers with %s=2, want 2", workers, buildJobsEnv)
	}

	// The operator's bound, when it is higher than the cores, is the cores.
	workers, err = distWorkers(cpus+3, anEnvironment(map[string]string{buildJobsEnv: "9999"}))
	if err != nil {
		t.Fatalf("distWorkers: %v", err)
	}

	if workers != cpus {
		t.Errorf("%d workers with %s=9999 on %d cores, want the cores", workers, buildJobsEnv, cpus)
	}

	// A variable set to nothing says nothing: it is the machine's own bound.
	workers, err = distWorkers(2, anEnvironment(map[string]string{buildJobsEnv: ""}))
	if err != nil {
		t.Fatalf("distWorkers: %v", err)
	}

	if workers != 2 {
		t.Errorf("%d workers with %s set to nothing, want the targets' own bound", workers, buildJobsEnv)
	}

	// A value that is not a count of work is refused.
	for _, value := range []string{"four", "0", "-2", "2.5"} {
		if _, err := distWorkers(2, anEnvironment(map[string]string{buildJobsEnv: value})); err == nil {
			t.Errorf("%s=%s was accepted, and it is not a count of work", buildJobsEnv, value)
		}
	}

	// Nothing ready, nothing to bound: the run that writes no archives reads no count of work.
	workers, err = distWorkers(0, anEnvironment(map[string]string{buildJobsEnv: "four"}))
	if err != nil {
		t.Fatalf("distWorkers: %v", err)
	}

	if workers != 0 {
		t.Errorf("%d workers for no ready targets, want none", workers)
	}
}

// noEnvironment and anEnvironment answer the questions a run asks of the
// machine's environment, so a test states the bound without changing the environment.
func noEnvironment(string) (string, bool) { return "", false }

func anEnvironment(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]

		return value, ok
	}
}

// the version file and the module files are sources of the server binary:
// a binary not rebuilt when they change reports a version the release does not ship
func TestTheVersionFileAndTheModuleFilesAreSourcesOfTheServerBinary(t *testing.T) {
	repo := t.TempDir()

	writeTreeFile(t, repo, filepath.Join("cmd", "rdio-scanner", "main.go"), "the source")
	setMTime(t, repo, filepath.Join("cmd", "rdio-scanner", "main.go"), date(2026, 1, 1))

	output := filepath.Join("dist", "linux-amd64", "rdio-scanner")

	writeTreeFile(t, repo, output, "the binary")
	setMTime(t, repo, output, date(2026, 1, 2))

	paths := []string{"VERSION.txt", "go.mod", "go.sum"}

	for _, path := range paths {
		writeTreeFile(t, repo, path, "the declared version")
		setMTime(t, repo, path, date(2026, 1, 1))
	}

	if current, err := serverIsCurrent(repo, output); err != nil || !current {
		t.Fatalf("a binary newer than every file it is built from is reported as not current: current %v, err %v", current, err)
	}

	for _, path := range paths {
		// The release moved, and the binary standing here was compiled before it moved.
		setMTime(t, repo, path, date(2026, 1, 3))

		if current, err := serverIsCurrent(repo, output); err != nil || current {
			t.Errorf("%s newer than the binary is not noticed: current %v, err %v", path, current, err)
		}

		setMTime(t, repo, path, date(2026, 1, 1))
	}
}

// setMTime gives a test-made tree the file dates the suite relies on.
// see the build-test-harness maintainer note.
func setMTime(t *testing.T, repo string, path string, when time.Time) {
	t.Helper()

	full := filepath.Join(repo, path)

	if err := os.Chtimes(full, when, when); err != nil {
		t.Fatalf("setting %s's date: %v", path, err)
	}
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
}

// the release matrix asks each target's binary the same currentness question
// a single server run asks, instead of compiling every target on every run
func TestAReleaseRunAsksWhetherTheServerBinaryIsCurrent(t *testing.T) {
	repo := t.TempDir()

	writeTreeFile(t, repo, filepath.Join("cmd", "rdio-scanner", "main.go"), "the source")
	setMTime(t, repo, filepath.Join("cmd", "rdio-scanner", "main.go"), date(2026, 1, 1))

	output := filepath.Join("dist", "linux-amd64", "rdio-scanner")

	writeTreeFile(t, repo, output, "the binary")
	setMTime(t, repo, output, date(2026, 1, 2))

	o, err := parseArgs([]string{"dist", "linux-amd64"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	state, err := buildState(repo, o)
	if err != nil {
		t.Fatalf("buildState: %v", err)
	}

	if !state.serverCurrent {
		t.Errorf("a release run does not ask whether the binary it is building is current: %v", state)
	}
}

// every part the phases ask for is wired into the tool newTool makes,
// and the order the wiring is written in is part of what this pins.
// see the tool-wiring-order maintainer note.
func TestTheBuildToolCarriesEveryPartThePhasesAskFor(t *testing.T) {
	tl := newTool("/repo")

	if tl.repo != "/repo" {
		t.Errorf("the tool is told to build %q, want the tree it was given", tl.repo)
	}

	value := reflect.ValueOf(tl)
	kind := value.Type()

	for i := 0; i < kind.NumField(); i++ {
		field := kind.Field(i)

		if field.Type.Kind() != reflect.Func {
			continue
		}

		if value.Field(i).IsNil() {
			t.Errorf("the tool the build command runs with has no %s, and the phase that asks for it panics on the machine the operator is standing on", field.Name)
		}
	}
}

// bound phases carry copies of the tool, so the wiring is measured by asking a
// bound phase to run a drop's binaries here: a cross-architecture drop is refused
// by naming its host, and not a crash in the operator's terminal.
// see the tool-wiring-order maintainer note.
func TestTheToolTheCommandRunsRunsTheBinariesOfADropOnThisMachine(t *testing.T) {
	repo := t.TempDir()

	manifest := bundle.ReadManifest()

	paths := ffmpegPathsFor(repo, manifest, "macos-arm64")

	if err := os.MkdirAll(paths.bundle, 0755); err != nil {
		t.Fatalf("the bundle directory cannot be prepared: %v", err)
	}

	fixture := machOBytes(aSystemProgram(theArm64CPU))

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if err := os.WriteFile(filepath.Join(paths.bundle, name), fixture, 0755); err != nil {
			t.Fatalf("the drop cannot be written: %v", err)
		}
	}

	out := &bytes.Buffer{}

	report, err := newTool(repo).prove("macos-arm64", manifest.Targets["macos-arm64"], paths, out)

	if err == nil {
		t.Fatal("a Linux host ran and adopted the arm64 macOS binaries it cannot execute")
	}

	if report.Host != "" || report.Conversions != 0 || len(report.Codecs.Audio) != 0 {
		t.Errorf("the refused run left a report behind, and the licence text would name it: %+v", report)
	}

	for _, want := range []string{
		"the macos-arm64 ffmpeg binaries do not run on this machine",
		"run go run ./cmd/build ffmpeg macos-arm64 on a macos-arm64 host",
		paths.bundle,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal says %q, and does not name %q", err, want)
		}
	}

	if strings.Contains(out.String(), "check") {
		t.Errorf("the run printed %q, which is what a run that converted the recordings says", out.String())
	}
}

// a step's own output reaches the operator's streams as the program wrote it,
// and git writes to both streams as a prerequisite of this build.
// see the operator-streams maintainer note.
func TestAStepsOwnOutputReachesTheOperator(t *testing.T) {
	dir := scratchWithoutARepository(t)

	out, errout, restore := theOperatorsStreams(t)

	defer restore()

	if err := runStep(step{label: "probe", dir: dir, name: "git", args: []string{"--version"}}); err != nil {
		t.Fatalf("runStep: %v", err)
	}

	if !strings.Contains(theContents(t, out), "git version") {
		t.Errorf("git's own words did not reach the operator's standard output: %q", theContents(t, out))
	}

	// The complaint is what is being looked for; the exit status is git's business.
	_ = runStep(step{label: "probe", dir: dir, name: "git", args: []string{"rev-parse", "--show-toplevel"}})

	if !strings.Contains(theContents(t, errout), "not a git repository") {
		t.Errorf("git's complaint did not reach the operator's standard error: %q", theContents(t, errout))
	}
}

// theOperatorsStreams stands in for the process's own standard output and
// standard error, and gives back the way to put them back.
func theOperatorsStreams(t *testing.T) (*os.File, *os.File, func()) {
	t.Helper()

	out, err := os.CreateTemp(t.TempDir(), "standard-output")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}

	errout, err := os.CreateTemp(t.TempDir(), "standard-error")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}

	oldOut, oldErr := os.Stdout, os.Stderr

	os.Stdout, os.Stderr = out, errout

	return out, errout, func() {
		os.Stdout, os.Stderr = oldOut, oldErr

		out.Close()
		errout.Close()
	}
}

// theContents reads what a stream the test is holding has been given.
func theContents(t *testing.T, f *os.File) string {
	t.Helper()

	body, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	return string(body)
}
