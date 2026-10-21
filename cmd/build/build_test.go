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

// The tool's four contacts with the machine it runs on: which tree it builds, which programs it can
// run, whether a directory is there, and what one step does when it is executed. The plan is composed
// in commands.go and tested there; what is tested here is the half that touches the machine.

// The tree the tool builds is the one git says. A tree without git is built where the operator stood,
// which is where `go run` stood too, and the tool is run from inside the tree either way.
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

// A program the tool has to run is looked up by the name the machine answers, and the name that was
// looked for is named in the failure: the difference between "npm is not installed" and a confusing
// failure.
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

// A directory is known by its existence: the client dependencies are installed once, and the tool
// looks at the tree to find out whether they already are.
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

// A step runs in its own directory, with the machine's environment in front of its own, and the
// program's own exit is the step's result: a step that says no ends the run, and the steps after it
// do not happen.
func TestAStepRunsInItsOwnDirectoryWithItsOwnExit(t *testing.T) {
	dir := t.TempDir()

	file := filepath.Join(dir, "a-file")

	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	// The same program, asked the same question, in two directories: the one that holds the file and
	// the one that does not. Only the first can answer yes.
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

// A clean run removes the release output, the client dependencies, and the contents of both bundles,
// and leaves the `.gitkeep` files that keep a plain go build compiling. The placeholders are the reason
// the bundle contents are removed one by one and the bundle directory itself is left standing.
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

// The server binary is current when it is newer than every Go file the server is built from. The
// ffmpeg the server embeds is not a Go file, and the ffmpeg phase has its own cache check.
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

// The ffmpeg phase writes two kinds of thing: the configured source trees it compiles in, the tarball it
// extracted them from, the container context it built in, and the working copy of the binaries it ran —
// all of it read by no later build — and the compiled programs, which every later build reads. A clean
// run that threw away the reusable 41 MB to keep the disposable 1.1 GB has the split backwards.
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

// The cache key is a digest of the release, the pinned source, the target, the recipe, the toolchain, and
// the check environment. An entry whose key no target the manifest records computes is an entry the
// manifest as it is now cannot ask for, and deleting it costs nothing a build would have reused. An
// entry kept because it is old, or deleted because it is old, is a rule that guesses.
func TestACleanRunPrunesTheCacheEntriesTheManifestCannotProduce(t *testing.T) {
	repo := t.TempDir()

	manifest := bundle.ReadManifest()

	live := filepath.Join(".build", "ffmpeg", manifest.FFmpeg, cacheKey(manifest, "linux-amd64"), "linux-amd64", "ffmpeg")

	// The entry a manifest edit replaced: the same release, a key nothing computes now.
	staleKey := filepath.Join(".build", "ffmpeg", manifest.FFmpeg, "a-key-the-manifest-replaced")

	// The whole cache of a release the manifest no longer pins.
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

// carriesAPath says whether a list of removed paths names a path, repo-relative names included: the
// clean run reports what it removed, and a removal nobody is told about is a tree that changed in
// silence.
func carriesAPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want || strings.HasSuffix(path, string(filepath.Separator)+want) {
			return true
		}
	}

	return false
}

// The server compiles of a release run are independent, and the bound on them is about the machine's
// memory and the log's readability, not about correctness. The operator can bound it lower: a release
// run on a small machine compiling five toolchains at once is a run that trades the release for a
// machine that stops responding. A value that is not a count of work is refused, because a run that
// quietly ignored it would be a run doing more than the operator said.
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

	// Fewer ready targets than cores: the bound is the targets, because a worker with nothing to do is
	// a worker that does nothing.
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

// noEnvironment and anEnvironment answer the question a run asks of the machine's environment, so the
// bound is a fact a test can state without changing the environment the tests run in.
func noEnvironment(string) (string, bool) { return "", false }

func anEnvironment(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]

		return value, ok
	}
}

// The version a server binary reports is stamped from the file the release declares its core version
// in, and the binary is compiled from the module files its dependencies are resolved from. A binary
// that is not rebuilt when one of those files changes reports a version the release does not ship,
// under an archive named for the version the release does ship.
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

// A tree the test made is not a tree git has ever seen, and the tool asks git for the repository root.
// The dates the tests set are the dates the files have, so the suite does not depend on the clock.
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

// The release matrix builds a server binary for each target, so a release run asks the same question a
// server run asks: is the binary newer than every file it is built from. A matrix that never asks it
// compiles every target on every run.
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

// The build command runs with the tool newTool makes, and every part the phases ask for has to be wired
// into it. A part left unwired is a nil function a phase calls on the machine the operator is standing
// on, and the phases that compile the ffmpeg binaries on their own platform run on a Mac and a FreeBSD
// box this machine cannot
// reach: the panic is in the operator's terminal, and no test here runs that code.
//
// The order the wiring is written in is part of what this pins. Taking a method value of the tool
// captures the tool as it is at that moment, so a phase bound before the fields it asks for are set
// carries a copy that does not have them.
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

// The phases bound in newTool are method values, and a method value carries the tool as it was when it
// was taken: the copy the phase runs on is not the tool the command holds. Checking the tool's own
// fields says nothing about the copies, so the wiring is measured by asking a bound phase to do the
// work it does on the operator's machine.
//
// The run that adopts a drop is the phase that runs programs here, and a drop of the other macOS
// architecture is the case that reaches it on any machine: the machine-code rule lets it through on a
// Linux host, and the Linux host cannot execute it. The answer is the refusal naming the host that runs
// those binaries — not a crash in the operator's terminal.
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

// A step's own output is the program's words and not the tool's: they reach the operator's streams, so
// the compiler's explanation and the container's log arrive as they were written. A step whose output
// the tool folded into its own log would put the tool's summary in front of the explanation the
// operator needs, and a step whose complaint the tool swallowed would leave the operator reading a
// failure with no reason in it.
//
// git is a prerequisite of this build, and it writes to both streams: its version to standard output,
// and its complaint about a tree that is not a repository to standard error.
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

// theOperatorsStreams stands in for the streams the process was started with, and gives back the way
// to put them back. The tool writes its own lines to the writer it is given, so a test that asks where
// a program's output lands has to be the process's standard output and standard error.
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
