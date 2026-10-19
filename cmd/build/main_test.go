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
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// The tool's whole job is to run the right commands in the right order with the resolved version in
// them. The plan it composes is tested in commands_test.go and the machine it touches in
// build_test.go; what is tested here is the run itself: the order the steps happen in, what the
// process exits with when a step says no, what the version subcommand prints, and what the run says
// about the version it resolved.

// fakeRun is a tool that records what it was told to run instead of running it.

func fakeRun(declared source, failAt string) (*tool, *bytes.Buffer, *[]step) {
	var ran []step

	// The dist run compiles and archives the ready targets at once, one goroutine per target, and
	// every step of such a run lands in this recorder. An append to a shared slice from several
	// workers loses entries, so every recording is taken under one lock.
	var mu sync.Mutex

	record := func(s step) {
		mu.Lock()
		defer mu.Unlock()

		ran = append(ran, s)
	}

	out := &bytes.Buffer{}

	t := &tool{
		repo: "/repo",
		read: func(string) (source, error) {
			return declared, nil
		},
		resolve: func(name string) (string, error) {
			return name, nil
		},
		installed: func(string) bool {
			return true
		},

		// The operator's standard error is where a failure goes. A test that reads the run's own lines
		// is not the operator, and says so by naming the stream it wants the failure written to.
		errout: io.Discard,

		// The container image's build time is a fact the run reads off the clock, so the stand-in says
		// what the clock said: a test that reads the command line the run composed has to know the
		// moment it was composed at.
		now: func() time.Time {
			return time.Date(2026, 10, 13, 12, 30, 45, 0, time.UTC)
		},
		execute: func(s step) error {
			if s.label == failAt {
				return errors.New(s.label + " said no")
			}

			record(s)

			return nil
		},

		// The ffmpeg phase is not one external command, so the stand-in records that it ran rather than
		// what it ran. What it does is tested in ffmpeg_test.go.
		ffmpeg: func(o options, stdout io.Writer) (bool, error) {
			record(step{label: subFFmpeg, result: o.target})

			return false, nil
		},

		// The archive is not an external command. The stand-in records that it was written, and what it
		// does is tested in archive_test.go. The archive reads the program the release wrote into the
		// tree, so the run's order is what is under test here.
		releaseFiles: func(repo string, tr releaseTarget, output string) ([]archiveFile, error) {
			return []archiveFile{{name: "rdio-scanner", body: []byte("the binary")}}, nil
		},

		archive: func(path string, files []archiveFile) error {
			record(step{label: subArchive, result: path})

			return nil
		},

		// A clean run removes files from the tree. The stand-in records that it ran; what it removes is
		// tested in build_test.go, in a tree the test made.
		clean: func(repo string) ([]string, error) {
			record(step{label: subClean, result: repo})

			return []string{"removed"}, nil
		},

		// The state is what the tree says about what has already been built. The stand-in says nothing is
		// built, which is the first run on a fresh clone; what the checks do is tested in build_test.go.
		state: func(repo string, o options) (planState, error) {
			return planState{}, nil
		},
	}

	return t, out, &ran
}

var released = source{core: "8.0.0", declared: true, tag: "v8.0.0", commit: "db79311"}
var development = source{core: "8.0.0", declared: true, commit: "db79311"}

// uncommitted is a tree git says is dirty: work that is not in the commit the version names.
var uncommitted = source{core: "8.0.0", declared: true, commit: "db79311", dirty: true, changed: []string{"cmd/build/build.go"}}

func TestRunningWithNoSubcommandBuildsBothApplicationsAndThenTheServer(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run(nil, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	got := []string{}
	for _, s := range *ran {
		got = append(got, s.label)
	}

	// Each bundle is written beside the Go package that embeds it, so a server built before both
	// applications existed would embed a stale one. The ffmpeg is compiled before the server for the
	// same reason: it is embedded in it too.
	want := []string{"scanner", "admin", "ffmpeg", "server"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("steps ran in the order %v, want %v", got, want)
	}
}

func TestEachSubcommandRunsOnlyTheStepsItNames(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{args: []string{"client"}, want: []string{"scanner", "admin"}},
		{args: []string{"scanner"}, want: []string{"scanner"}},
		{args: []string{"admin"}, want: []string{"admin"}},
		{args: []string{"ffmpeg"}, want: []string{"ffmpeg"}},
		{args: []string{"server"}, want: []string{"server"}},
		{args: []string{"version"}, want: []string{}},
	}

	for _, c := range cases {
		tl, _, ran := fakeRun(released, "")

		if code := tl.run(c.args, io.Discard); code != 0 {
			t.Fatalf("%v: exit code = %d, want 0", c.args, code)
		}

		got := []string{}
		for _, s := range *ran {
			got = append(got, s.label)
		}

		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%v ran %v, want %v", c.args, got, c.want)
		}
	}
}

// The version reaches the server through the linker and the applications through the environment.
// Neither is rewritten in a source file, which is the mechanism that produced the drift this tool
// exists to remove.
func TestAPreReleaseNameThatIsNotAPrereleaseIsRejectedBeforeAnythingIsBuilt(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	errout := &bytes.Buffer{}

	tl.errout = errout

	if code := tl.run([]string{"--pre", "beta 1"}, out); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}

	if len(*ran) != 0 {
		t.Errorf("%d steps ran after a rejected command line", len(*ran))
	}

	if !strings.Contains(errout.String(), "beta 1") {
		t.Errorf("the refusal %q does not name the prerelease it rejected", errout.String())
	}

	if out.Len() != 0 {
		t.Errorf("a rejected command line wrote %q to the writer the run was given, which carries the run's progress",
			out.String())
	}
}

func TestACommandTheToolDoesNotKnowIsRejectedBeforeAnythingRuns(t *testing.T) {
	for _, args := range [][]string{
		{"--not_a_flag"},
		{"not_a_subcommand"},
		{"scanner", "admin"},
		{"-o"},
		{"--pre"},
	} {
		tl, out, ran := fakeRun(released, "")

		if code := tl.run(args, out); code != 2 {
			t.Errorf("%v: exit code = %d, want 2\n%s", args, code, out.String())
		}

		if len(*ran) != 0 {
			t.Errorf("%v: %d steps ran after a rejected command line", args, len(*ran))
		}
	}
}

// A step that fails ends the run, and the steps that would have followed it do not run: a server
// built after a failed application build would embed a bundle that is not there.
func TestAStepThatFailsEndsTheRunAndStopsTheStepsAfterIt(t *testing.T) {
	tl, out, ran := fakeRun(released, "admin")

	errout := &bytes.Buffer{}

	tl.errout = errout

	if code := tl.run(nil, out); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}

	if len(*ran) != 1 {
		t.Errorf("%d steps ran after the administration build failed, want only the scanner build", len(*ran))
	}

	if !strings.Contains(errout.String(), "admin") {
		t.Errorf("the failure %q does not say which step failed", errout.String())
	}
}

func TestTheVersionSubcommandPrintsTheResolvedVersionAndBuildsNothing(t *testing.T) {
	tl, out, ran := fakeRun(development, "")

	if code := tl.run([]string{"version"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if len(*ran) != 0 {
		t.Errorf("%d steps ran for the version subcommand, want none", len(*ran))
	}

	if got := strings.TrimSpace(out.String()); got != "8.0.0+gdb79311" {
		t.Errorf("the version printed %q, want 8.0.0+gdb79311", got)
	}
}

// The log says what was resolved and why, because a version nobody can explain is the drift this
// tool replaced with a file.
func TestTheRunSaysWhatVersionItResolvedAndWhy(t *testing.T) {
	tl, out, _ := fakeRun(development, "")

	if code := tl.run([]string{"client"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	line := strings.SplitN(out.String(), "\n", 2)[0]

	for _, want := range []string{"8.0.0+gdb79311", "8.0.0", "not tagged"} {
		if !strings.Contains(line, want) {
			t.Errorf("the version line %q does not say %q", line, want)
		}
	}
}

// A release run asked for the whole matrix builds the applications once, because the bundles are the same
// for every target, then every target's ffmpeg, serially and in the matrix's order, and then the server
// compiles and the archive writes of all those targets at once. The order within the phases is what the
// release's correctness rests on: a server built before the ffmpeg its target carries exists is a server
// that ships without one, and a target's archive is read from the path its own compile wrote to. Which
// target finished its compile first is the machine's business, and the run's log says which target every
// line is about.
func TestADistRunBuildsTheApplicationsOnceThenEveryFfmpegThenTheTargetsAtOnce(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"dist", "--all"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	got := []string{}
	for _, s := range *ran {
		got = append(got, s.label+" "+s.result)
	}

	want := []string{"scanner " + appBundle[subScanner], "admin " + appBundle[subAdmin]}
	for _, tr := range releaseTargets {
		want = append(want, "ffmpeg "+tr.targetWord())
	}

	// The applications come first, and the ffmpeg phase is one target's block of lines at a time, in
	// the matrix's order.
	for i, word := range []string{want[0], want[1]} {
		if got[i] != word {
			t.Fatalf("the release run ran %q at %d, want %q: the applications are built once, first", got[i], i, word)
		}
	}

	for i, tr := range releaseTargets {
		word := tr.targetWord()

		if step := "ffmpeg " + word; got[2+i] != step {
			t.Fatalf("the release run ran %q at %d, want %q: the ffmpeg phase is serial and runs in the matrix's order",
				got[2+i], 2+i, step)
		}
	}

	// The compiles and the writes are the rest, and their order is the machine's: the steps that ran
	// are exactly the steps the matrix asks for, and each target's archive follows its own compile.
	rest := theStepsCounted(got[2+len(releaseTargets):])

	want2 := theStepsCounted(want[2+len(releaseTargets):])

	for step, count := range want2 {
		if rest[step] != count {
			t.Fatalf("the release run ran\n  %v\nwant\n  %v", got, want)
		}
	}

	for _, tr := range releaseTargets {
		word := tr.targetWord()

		compile, write := -1, -1

		for i, step := range got {
			if step == "server "+defaultOutputPath(word) {
				compile = i
			}

			if step == "archive "+archivePath(word, "8.0.0") {
				write = i
			}
		}

		if compile < 0 || write < 0 {
			t.Fatalf("the release run ran no server compile or no archive for %s:\n  %v", word, got)
		}

		if compile > write {
			t.Errorf("the archive of %s was written at %d, before its server compile at %d: an archive is read from the path its own compile wrote to",
				word, write, compile)
		}
	}
}

// theStepsCounted counts the steps a run recorded, so a test can say which steps ran without saying
// which order the machine finished them in.
func theStepsCounted(steps []string) map[string]int {
	counts := map[string]int{}

	for _, step := range steps {
		counts[step]++
	}

	return counts
}

// A release run asked for one target builds that target and nothing else.
func TestADistRunForOneTargetBuildsThatTarget(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"dist", "linux-arm64"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	got := []string{}
	for _, s := range *ran {
		got = append(got, s.label+" "+s.result)
	}

	want := []string{
		"scanner " + appBundle[subScanner],
		"admin " + appBundle[subAdmin],
		"ffmpeg linux-arm64",
		"server " + defaultOutputPath("linux-arm64"),
		"archive " + archivePath("linux-arm64", "8.0.0"),
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the release run ran\n  %v\nwant\n  %v", got, want)
	}
}

// The archive is read from the path the server step writes to. A release run that read the program from
// a path it chose for itself shipped whatever was standing at that path. The compiles of a matrix run
// finish in whatever order the machine scheduled them, so the pairing is stated per target: the archive
// of a target reads the path that target's server step wrote to.
func TestADistRunReadsTheArchiveFromThePathTheServerStepWritesTo(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	var read []string

	// The archive reads are recorded from the matrix run's workers, one goroutine per target, so
	// this recorder takes its appends under a lock of its own.
	var readMu sync.Mutex

	tl.releaseFiles = func(repo string, tr releaseTarget, output string) ([]archiveFile, error) {
		readMu.Lock()
		read = append(read, tr.targetWord()+" "+output)
		readMu.Unlock()

		return []archiveFile{{name: "rdio-scanner", body: []byte("the binary")}}, nil
	}

	if code := tl.run([]string{"dist", "--all"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	var built []string

	for _, s := range *ran {
		if s.label == subServer {
			built = append(built, theTargetOf(s)+" "+theOutputPathOf(s))
		}
	}

	sort.Strings(read)
	sort.Strings(built)

	if strings.Join(read, " ") != strings.Join(built, " ") {
		t.Errorf("the release read its programs from\n  %v\nand the server steps wrote them to\n  %v", read, built)
	}
}

// A release run's archive carries the bytes of the binary that run compiled, written by the real archive
// writer into a real tree: the binary standing at the path from an earlier build is replaced by the one
// this run compiled before anything reads it.
func TestADistRunArchivesTheBinaryItCompiled(t *testing.T) {
	repo := t.TempDir()

	target := "linux-arm64"

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary from an earlier build")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeArchiveLicence(t, repo, target, "the licence")

	tl, _, _ := fakeRun(released, "")

	tl.repo = repo
	tl.releaseFiles = archiveFiles

	// The tool runs with the repository as its working directory, so the archive path a run names is a
	// path inside it.
	tl.archive = func(path string, files []archiveFile) error {
		return writeArchive(filepath.Join(repo, path), files)
	}

	// The stand-in compiles the way the compiler does: it writes the binary where the step says it
	// writes it, and nowhere else.
	tl.execute = func(s step) error {
		if s.label != subServer {
			return nil
		}

		path := filepath.Join(repo, theOutputPathOf(s))

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(path), err)
		}

		if err := os.WriteFile(path, []byte("the binary this run compiled"), 0o755); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}

		return nil
	}

	if code := tl.run([]string{"dist", target}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	body, err := os.ReadFile(filepath.Join(repo, archivePath(target, "8.0.0")))
	if err != nil {
		t.Fatalf("the release archive was not written: %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("a standard zip reader cannot open the archive: %v", err)
	}

	var program string

	for _, f := range reader.File {
		if f.Name != "rdio-scanner" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			t.Fatalf("cannot open %s in the archive: %v", f.Name, err)
		}

		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("reading %s in the archive: %v", f.Name, err)
		}

		program = string(got)
	}

	if program != "the binary this run compiled" {
		t.Errorf("the archive carries %q, want the binary this run compiled", program)
	}
}

// theOutputPathOf is the path a step was told to write to: the word after `-o` on its command line.
func theOutputPathOf(s step) string {
	for i, arg := range s.args {
		if arg == "-o" && i+1 < len(s.args) {
			return s.args[i+1]
		}
	}

	return ""
}

// theTargetOf reads the target a step was composed for out of the environment the step runs with, in
// the name the release matrix calls a target by: the matrix sets GOOS and GOARCH for each target, and a
// test that pairs a compile with the archive that read it needs the target, because the compiles finish
// in whatever order the machine scheduled them.
func theTargetOf(s step) string {
	name, goarch := "", ""

	for _, env := range s.env {
		if value, ok := strings.CutPrefix(env, "GOOS="); ok {
			name = theTargetNames[value]
		}

		if value, ok := strings.CutPrefix(env, "GOARCH="); ok {
			goarch = value
		}
	}

	if name == "" || goarch == "" {
		return ""
	}

	return name + "-" + goarch
}

// theTargetNames is the name the release matrix calls each operating system by.
var theTargetNames = map[string]string{
	"darwin":  "macos",
	"freebsd": "freebsd",
	"linux":   "linux",
	"windows": "windows",
}

// A release run refuses a tree with uncommitted changes. The archive is named for the commit it was
// built from, and a tree with work in it is not that commit: the person who downloads the archive has
// no way to know which of the two the binary came from. The refusal comes before anything is built, and
// names the paths and the override.
func TestADistRunRefusesATreeWithUncommittedChanges(t *testing.T) {
	tl, out, ran := fakeRun(uncommitted, "")

	errout := &bytes.Buffer{}

	tl.errout = errout

	if code := tl.run([]string{"dist"}, out); code != 1 {
		t.Fatalf("exit code = %d, want 1: a release run asked to build a tree that is not committed", code)
	}

	for _, want := range []string{"cmd/build/build.go", "--allow-dirty"} {
		if !strings.Contains(errout.String(), want) {
			t.Errorf("the refusal %q does not name %s, which is what the operator needs to fix it", errout.String(), want)
		}
	}

	if len(*ran) != 0 {
		t.Errorf("the run built %d steps after refusing the tree: %v", len(*ran), *ran)
	}
}

// The override is a maintainer building a pre-release from work in progress, and it is only worth
// having because the log says, on the run that used it, that the release is built from an uncommitted
// tree.
func TestADistRunBuiltFromAnUncommittedTreeSaysSo(t *testing.T) {
	tl, out, _ := fakeRun(uncommitted, "")

	if code := tl.run([]string{"dist", "--allow-dirty"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if !strings.Contains(out.String(), "uncommitted") {
		t.Errorf("the run's log %q does not say the release is built from an uncommitted tree", out.String())
	}
}

// A release run says what it wrote and what it wrote none for, and its exit status says whether the
// release it was asked for is complete. A matrix that ships five of eight archives is doing what this
// machine can do, and that is a different fact from "something broke".
func TestADistRunReportsTheArchivesItWroteAndSaysTheReleaseIsComplete(t *testing.T) {
	tl, out, _ := fakeRun(released, "")

	if code := tl.run([]string{"dist", "--all"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0: every archive the run was asked for was written", code)
	}

	if !strings.Contains(out.String(), "8 archives written") {
		t.Errorf("the run's log %q does not say 8 archives written", out.String())
	}

	tl, out, _ = fakeRun(released, "")

	if code := tl.run([]string{"dist", "linux-arm64"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if !strings.Contains(out.String(), "1 archive written") {
		t.Errorf("the run's log %q does not say 1 archive written", out.String())
	}
}

// An incomplete matrix is not a release, and the run that wrote it says so with its exit status, so a
// script can tell "the five archives are the whole release this machine can make" from "the release is
// missing archives".
func TestADistRunExitsNonZeroWhenATargetItWasAskedForWroteNoArchive(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	skipsSuppliedTargets(tl, ran)

	if code := tl.run([]string{"dist", "--all"}, out); code != 3 {
		t.Fatalf("exit code = %d, want 3: three of the eight targets the run was asked for wrote no archive", code)
	}

	for _, want := range []string{"5 archives written", "3 targets wrote none", "freebsd-amd64", "macos-amd64", "macos-arm64"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the run's log %q does not say %q", out.String(), want)
		}
	}
}

// A release run asked for one target it cannot build wrote none of the one archive it was asked for.
func TestADistRunAskedForOneTargetItCannotBuildExitsNonZero(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	skipsSuppliedTargets(tl, ran)

	if code := tl.run([]string{"dist", "macos-arm64"}, out); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}

	for _, s := range *ran {
		if s.label == subArchive || s.label == subServer {
			t.Errorf("%s ran for %s, a target whose binaries were never supplied", s.label, s.result)
		}
	}
}

// The release folder is where the archives sit side by side until they are uploaded by hand. The
// archives of the version a run released, sitting next to the archives of the versions it did not
// release, is how the wrong file gets uploaded.
func TestADistRunRemovesTheArchivesOfTheVersionsItDidNotRelease(t *testing.T) {
	repo := t.TempDir()

	// The release folder a run of an older version left behind, an archive of the version this run
	// releases for a target it is not building, an archive whose prerelease name carries a second `-v`,
	// and a file that is not an archive.
	writeRepoFile(t, repo, "dist/rdio-scanner-linux-amd64-v7.9.0.zip", "the archive of a release that happened before")
	writeRepoFile(t, repo, "dist/rdio-scanner-linux-arm64-v8.0.0.zip", "the archive of this version, for a target this run is not building")
	writeRepoFile(t, repo, "dist/rdio-scanner-windows-amd64-v8.0.0-v1+g1.zip", "a prerelease archive whose name carries a second -v")
	writeRepoFile(t, repo, "dist/notes.txt", "a file the release folder holds")

	tl, out, _ := fakeRun(released, "")

	tl.repo = repo
	tl.releaseFiles = func(repo string, tr releaseTarget, output string) ([]archiveFile, error) {
		return []archiveFile{{name: "rdio-scanner", body: []byte("the binary")}}, nil
	}
	tl.archive = func(path string, files []archiveFile) error {
		return writeArchive(filepath.Join(repo, path), files)
	}

	if code := tl.run([]string{"dist", "linux-amd64"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	for _, path := range []string{"dist/rdio-scanner-linux-amd64-v7.9.0.zip", "dist/rdio-scanner-windows-amd64-v8.0.0-v1+g1.zip"} {
		if _, err := os.Stat(filepath.Join(repo, path)); err == nil {
			t.Errorf("%s survived the release run, and it is not an archive of the version the run released", path)
		}

		if !strings.Contains(out.String(), path) {
			t.Errorf("the run's log %q does not name the removal of %s", out.String(), path)
		}
	}

	for _, path := range []string{
		"dist/rdio-scanner-linux-arm64-v8.0.0.zip",
		"dist/rdio-scanner-linux-amd64-v8.0.0.zip",
		"dist/notes.txt",
	} {
		if _, err := os.Stat(filepath.Join(repo, path)); err != nil {
			t.Errorf("the release run removed %s, and it is not an archive of a version it did not release: %v", path, err)
		}
	}
}

// The version in an archive's name is what the release folder's pruning compares. The name is
// `rdio-scanner-<target>-v<version>.zip`, and the target is the part whose shape the tool knows, so the
// separator between them is found by the target's shape and not by the last hyphen a prerelease name
// happens to carry.
func TestTheVersionInAnArchiveNameIsReadByTheTargetTheNameNames(t *testing.T) {
	for name, want := range map[string]string{
		"rdio-scanner-linux-amd64-v8.0.0.zip":              "8.0.0",
		"rdio-scanner-windows-amd64-v8.0.0+g1885b7e2.zip":  "8.0.0+g1885b7e2",
		"rdio-scanner-linux-arm64-v8.0.0-v1+g1885b7e2.zip": "8.0.0-v1+g1885b7e2",
	} {
		got, found := archiveVersionOf(name)
		if !found || got != want {
			t.Errorf("%s reads version %q, found %v, want %q", name, got, found, want)
		}
	}

	if _, found := archiveVersionOf("notes.txt"); found {
		t.Error("notes.txt was read as a release archive name, and it is not one")
	}

	if _, found := archiveVersionOf("rdio-scanner-nonsense-v8.0.0.zip"); found {
		t.Error("an archive named for no target this tool builds was read as one it knows the version of")
	}
}

// A release run compiles the server binaries of the targets it can at once, and a log written by
// several targets at once is a log that has to say which target each line is about: an operator reading
// a line that says `server  dist/linux-arm64/rdio-scanner` in a log of five targets is reading a line
// whose owner the line itself has to name.
func TestAReleaseRunNamesTheTargetEveryLineOfItsLogIsAbout(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	tl.ffmpeg = skipTheSuppliedTargets(ran)

	// The three targets whose ffmpeg binaries are compiled on their own platform wrote none, which is
	// the release this machine can produce: the run says so and exits 3.
	if code := tl.run([]string{"dist", "--all", "--allow-dirty"}, out); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}

	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}

		word := strings.Fields(line)[0]

		// The version line, the notes, the two applications the whole run builds once for every
		// target, the summary, and the removals are the run's own lines: they are about the run and not
		// about one target's work.
		if word == "version" || word == "note" || word == subDist || word == subScanner || word == subAdmin {
			continue
		}

		if !isTarget(word) {
			t.Errorf("the log line %q does not name the target its work is about", line)
		}
	}
}

// The ffmpeg phase builds container images, runs qemu and wine, and saturates the machine inside one
// target. It stays serial, and it finishes before the server compiles start, so the log that records
// what was adopted is one target's block of lines at a time.
func TestAReleaseRunFinishesTheFfmpegPhaseBeforeTheServerCompilesStart(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	tl.ffmpeg = skipTheSuppliedTargets(ran)

	if code := tl.run([]string{"dist", "--all", "--allow-dirty"}, io.Discard); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}

	lastFfmpeg, firstServer := -1, -1

	for i, s := range *ran {
		if s.label == subFFmpeg {
			lastFfmpeg = i
		}

		if s.label == subServer && firstServer < 0 {
			firstServer = i
		}
	}

	if firstServer < 0 {
		t.Fatal("the release run compiled no server binary")
	}

	if lastFfmpeg > firstServer {
		t.Errorf("a server compile ran at %d, before the ffmpeg phase finished at %d: the ffmpeg phase is serial and comes first", firstServer, lastFfmpeg)
	}
}

// The archives are the release's product, and their bytes must not depend on the order the targets ran
// in. A run that read a target's binary after another target's compile had written over it, or that
// wrote one target's archive from the bytes of the target beside it, would write a release that is not
// the release the serial run writes.
func TestAReleaseRunWritesTheSameArchivesWhicheverWayTheTargetsRan(t *testing.T) {
	repo := t.TempDir()

	// The tree a release run writes its archives from: each target's binary, the guide, and the licence
	// of the ffmpeg each ready target carries. The times are fixed, because an archive carries the time
	// of the file it carries, and both runs are reading the same tree.
	for _, tr := range releaseTargets {
		target := tr.targetWord()

		if theSuppliedTargets[target] {
			continue
		}

		writeRepoFile(t, repo, defaultOutputPath(target), "the binary of "+target)
		writeArchiveLicence(t, repo, target, "the licence of "+target)
	}

	writeRepoFile(t, repo, guideSource, "the guide")

	fixEveryFileTime(t, repo, time.Date(2026, 1, 2, 3, 4, 6, 0, time.Local))

	tl, _, _ := fakeRun(released, "")

	tl.repo = repo
	tl.ffmpeg = skipTheSuppliedTargets(nil)

	// The binaries are the ones this tree's sources produce, so the run copies them into its archives
	// and compiles nothing: the bytes the two runs compare are the bytes of one tree.
	tl.state = func(string, options) (planState, error) {
		return planState{serverCurrent: true}, nil
	}

	tl.releaseFiles = archiveFiles

	// The archive paths the run composes are relative to the tree it builds, and this tree is the
	// temporary one: the real writer writes there.
	tl.archive = func(path string, files []archiveFile) error {
		return writeArchive(filepath.Join(repo, path), files)
	}

	t.Setenv(buildJobsEnv, "1")

	if code := tl.run([]string{"dist", "--all", "--allow-dirty"}, io.Discard); code != 3 {
		t.Fatalf("the serial release run: exit code = %d, want 3", code)
	}

	serial := theArchivesOfTheRelease(t, repo)

	t.Setenv(buildJobsEnv, "4")

	if code := tl.run([]string{"dist", "--all", "--allow-dirty"}, io.Discard); code != 3 {
		t.Fatalf("the parallel release run: exit code = %d, want 3", code)
	}

	parallel := theArchivesOfTheRelease(t, repo)

	if len(serial) != len(parallel) {
		t.Fatalf("the serial run wrote %d archives and the parallel run wrote %d", len(serial), len(parallel))
	}

	for name, digest := range serial {
		if parallel[name] != digest {
			t.Errorf("%s: the parallel run wrote %s and the serial run wrote %s", name, parallel[name], digest)
		}
	}
}

// theSuppliedTargets are the targets whose ffmpeg binaries are compiled on the platform they run on:
// on a Linux host they write none, and the five the host compiles are the ready targets.
var theSuppliedTargets = map[string]bool{
	"macos-amd64":   true,
	"macos-arm64":   true,
	"freebsd-amd64": true,
}

// skipTheSuppliedTargets is the ffmpeg phase of a machine that is not the platform the supplied
// targets' binaries run on. The targets it produces for are recorded, when the caller is counting the
// steps that ran.
func skipTheSuppliedTargets(ran *[]step) func(options, io.Writer) (bool, error) {
	return func(o options, stdout io.Writer) (bool, error) {
		if theSuppliedTargets[o.target] {
			return true, nil
		}

		if ran != nil {
			*ran = append(*ran, step{label: subFFmpeg, result: o.target})
		}

		return false, nil
	}
}

// fixEveryFileTime gives every file in a tree the same time, so the times the archives carry are the
// tree's and not the moment the test ran.
func fixEveryFileTime(t *testing.T, repo string, when time.Time) {
	t.Helper()

	err := filepath.Walk(repo, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		return os.Chtimes(path, when, when)
	})

	if err != nil {
		t.Fatalf("fixing the tree's times: %v", err)
	}
}

// theArchivesOfTheRelease reads the bytes of every archive the release folder holds, by name.
func theArchivesOfTheRelease(t *testing.T, repo string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(repo, "dist"))
	if err != nil {
		t.Fatalf("reading the release folder: %v", err)
	}

	archives := map[string]string{}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}

		body, err := os.ReadFile(filepath.Join(repo, "dist", entry.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}

		archives[entry.Name()] = fmt.Sprintf("%x", sha256.Sum256(body))
	}

	return archives
}

// The verification run runs the two suites and builds nothing: the same command a developer runs before
// pushing is the command a release is gated on, and a run that built something first reported the
// build's result and not the suites'.
func TestATestRunRunsTheTwoSuitesAndBuildsNothing(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"test"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	var got []string

	for _, s := range *ran {
		got = append(got, s.label+" "+strings.Join(s.args, " "))
	}

	want := []string{
		subTest + " test ./...",
		subTest + " test",
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the test run ran\n  %v\nwant\n  %v", got, want)
	}
}

// A suite that fails ends the run, and the run names the suite that failed. The release is gated on
// this command, so a run that carried on past a failing suite would report success to whatever gates on
// it.
func TestATestRunEndsNonZeroWhenASuiteFails(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	tl.execute = func(s step) error {
		if s.name == "npm" {
			return errors.New("the client suite said no")
		}

		*ran = append(*ran, s)

		return nil
	}

	errout := &bytes.Buffer{}

	tl.errout = errout

	if code := tl.run([]string{"test"}, out); code != 1 {
		t.Fatalf("exit code = %d, want 1: a suite failed", code)
	}

	if !strings.Contains(errout.String(), "npm") {
		t.Errorf("the failure %q does not name the suite that failed", errout.String())
	}

	if len(*ran) != 1 {
		t.Errorf("%d steps ran after the client suite failed, want only the Go suite", len(*ran))
	}
}

// A target whose ffmpeg fails ends the run, and the run names the target it stopped at. The targets
// after it are not built, and no server is compiled for the targets before it either: the ffmpeg phase
// finishes before any compile starts, so a run that stopped in that phase stopped with nothing compiled
// at all. A half matrix is not a release, and a run that carried on would leave an archive directory
// with a server in it and no ffmpeg to embed. This is the promise the compiled targets keep, and it is
// not the way supplied binaries that were never dropped are treated.
func TestADistRunStopsAtTheTargetWhoseFfmpegFails(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	tl.ffmpeg = func(o options, stdout io.Writer) (bool, error) {
		if o.target == "linux-arm" {
			return false, errors.New("the ffmpeg for linux-arm said no")
		}

		*ran = append(*ran, step{label: subFFmpeg, result: o.target})

		return false, nil
	}

	errout := &bytes.Buffer{}

	tl.errout = errout

	if code := tl.run([]string{"dist", "--all"}, out); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}

	if !strings.Contains(errout.String(), "linux-arm") {
		t.Errorf("the failure %q does not name the target it stopped at", errout.String())
	}

	got := []string{}
	for _, s := range *ran {
		got = append(got, s.label+" "+s.result)
	}

	want := []string{
		"scanner " + appBundle[subScanner],
		"admin " + appBundle[subAdmin],
		"ffmpeg freebsd-amd64",
		"ffmpeg linux-386",
		"ffmpeg linux-amd64",
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the run ran\n  %v\nwant\n  %v", got, want)
	}
}

// A release run that named no target and asked for no matrix builds the archive for the platform it is
// standing on: the run a developer does to try what they just built is one archive, not eight cross-builds.
func TestADistRunBuildsTheArchiveForThePlatformItIsRunOn(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"dist"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	got := []string{}

	for _, s := range *ran {
		got = append(got, s.label+" "+s.result)
	}

	want := []string{
		"scanner " + appBundle[subScanner],
		"admin " + appBundle[subAdmin],
		"ffmpeg " + runtimeTarget(),
		"server " + defaultOutputPath(runtimeTarget()),
		"archive " + archivePath(runtimeTarget(), "8.0.0"),
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the release run ran\n  %v\nwant\n  %v", got, want)
	}
}

// skipsSuppliedTargets makes the stand-in answer the way the ffmpeg phase answers for a target whose
// binaries the maintainer never dropped: the message names the target, the host that compiles them, the
// command that compiles them there, and the directory to drop them in, and the target is reported as
// skipped.
func skipsSuppliedTargets(tl *tool, ran *[]step) {
	tl.ffmpeg = func(o options, stdout io.Writer) (bool, error) {
		record, found := bundle.ReadManifest().Targets[o.target]

		if found && record.Produced == suppliedProduction {
			fmt.Fprintf(stdout, "%-7s the ffmpeg for %s was not supplied: build them on a %s host with go run ./cmd/build ffmpeg %s, or drop ffmpeg and ffprobe in internal/infrastructure/ffmpeg/bundle/%s\n",
				subFFmpeg, o.target, o.target, o.target, o.target)

			return true, nil
		}

		*ran = append(*ran, step{label: subFFmpeg, result: o.target})

		return false, nil
	}
}

// Binaries that were not dropped are not a failure. The three supplied targets are built on three machines
// the release machine does not control, and a run that stopped for the one it does not have would ship
// nothing at all — not the five archives it could have shipped, and not the two it was told to build. The
// run says which target it skipped and what would have made it buildable, ships the rest, and ends with
// the summary that says the release is incomplete: five of the eight archives it was asked for.
func TestADistRunSkipsASuppliedTargetWhoseBinariesWereNotDropped(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	skipsSuppliedTargets(tl, ran)

	if code := tl.run([]string{"dist", "--all"}, out); code != 3 {
		t.Fatalf("exit code = %d, want 3: a release run missing three supplied targets' binaries ships the five it can and says the release is incomplete", code)
	}

	for _, target := range []string{"freebsd-amd64", "macos-amd64", "macos-arm64"} {
		for _, want := range []string{
			"the ffmpeg for " + target + " was not supplied",
			"internal/infrastructure/ffmpeg/bundle/" + target,
			"go run ./cmd/build ffmpeg " + target,
		} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("the run's output does not say %q, which is what tells the operator how to make %s buildable", want, target)
			}
		}
	}

	archives := 0

	for _, s := range *ran {
		if s.label == subArchive {
			archives++
		}

		for _, target := range []string{"freebsd-amd64", "macos-amd64", "macos-arm64"} {
			if strings.Contains(s.result, target) {
				t.Errorf("%s ran for %s, a target the release run skipped", s.label, s.result)
			}
		}
	}

	if archives != 5 {
		t.Errorf("the run wrote %d archives, want the five of the targets this build compiles an ffmpeg for", archives)
	}
}

// A release run asked for one supplied target with nothing dropped for it warns, skips, and writes no
// archive: an archive whose server carries no ffmpeg is the thing the bundle exists to prevent, and the
// operator is told what would have made it buildable. The run was asked for one archive and wrote none,
// which is the incomplete release the exit status names.
func TestADistRunForASuppliedTargetWithNothingDroppedProducesNoArchive(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	skipsSuppliedTargets(tl, ran)

	if code := tl.run([]string{"dist", "macos-arm64"}, out); code != 3 {
		t.Fatalf("exit code = %d, want 3: the one archive the run was asked for was not written", code)
	}

	for _, s := range *ran {
		if s.label == subArchive || s.label == subServer {
			t.Errorf("%s ran for %s, a target whose binaries were never supplied", s.label, s.result)
		}
	}

	if !strings.Contains(out.String(), "macos-arm64") {
		t.Errorf("the run's output %q does not name the target it skipped", out.String())
	}
}

// The container image builds the server inside itself, so the run that prepares it builds the
// applications and the ffmpeg the image's architecture embeds, and then asks podman to build the image.
// Building a server binary here would be a binary the image throws away.
func TestAContainerRunBuildsTheApplicationsAndTheFfmpegThenTheImage(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"container"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	got := []string{}
	for _, s := range *ran {
		got = append(got, s.label+" "+s.result)
	}

	want := []string{
		"scanner " + appBundle[subScanner],
		"admin " + appBundle[subAdmin],
		"ffmpeg linux-amd64",
		"container rdio-scanner:8.0.0",
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the container run ran\n  %v\nwant\n  %v", got, want)
	}
}

// The version the image reports is the one the run resolved, through the build argument the
// Containerfile reads, and the image is tagged with that version beside `latest` so a machine reading
// the tags knows which release it is standing on. An image built without it reports the development
// fallback, which is the sign that nobody said what the image is. The run asks the container program the
// machine has, and names the file the image is built from, because docker looks for Dockerfile and does
// not find Containerfile.
func TestTheContainerStepAsksPodmanToBuildTheImageWithTheResolvedVersion(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"container"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	last := (*ran)[len(*ran)-1]

	if last.name != podmanProgram {
		t.Fatalf("the container run ended with %s, want %s", last.name, podmanProgram)
	}

	want := []string{
		"build",
		"--pull",
		"-f", "Containerfile",
		"--build-arg", "VERSION=8.0.0",
		"--build-arg", "BUILD_TIME=2026-10-13T12:30:45Z",
		"-t", "rdio-scanner:8.0.0",
		"-t", "rdio-scanner:latest",
		".",
	}

	if strings.Join(last.args, " ") != strings.Join(want, " ") {
		t.Errorf("%s ran with %v, want %v", last.name, last.args, want)
	}
}

// A development build's image says it is one, the same way the binary built from the same commit does.
func TestTheContainerImageOfADevelopmentBuildCarriesTheDevelopmentVersion(t *testing.T) {
	tl, _, ran := fakeRun(development, "")

	if code := tl.run([]string{"container"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	last := (*ran)[len(*ran)-1]

	if !strings.Contains(strings.Join(last.args, " "), "VERSION=8.0.0+gdb79311") {
		t.Errorf("the image was built with %v, which does not carry the development version", last.args)
	}
}

// The image is linux-amd64, because that is the architecture the Containerfile's guard checks for. A
// container run asked for another target is a run that would build an ffmpeg the image never embeds.
func TestAContainerRunIsForTheArchitectureTheContainerfileChecksFor(t *testing.T) {
	o, err := parseArgs([]string{"container"})
	if err != nil {
		t.Fatalf("a container run was refused: %v", err)
	}

	if o.target != "linux-amd64" {
		t.Errorf("a container run builds for %q, want the architecture the Containerfile checks for", o.target)
	}

	if _, err := parseArgs([]string{"container", "linux-arm64"}); err == nil {
		t.Error("a container run for linux-arm64 was accepted, and the image builds for linux-amd64")
	}
}

// A clean run removes what a release and a client build leave behind, and builds nothing: the
// dependencies, the release output, and the contents of both bundles. A run that cleaned and then
// rebuilt is a run that answered a question nobody asked.
func TestACleanRunRemovesTheReleaseOutputTheDependenciesAndTheBundleContents(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"clean"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if len(*ran) != 1 {
		t.Fatalf("the clean run ran %d steps, want the clean step alone", len(*ran))
	}

	if (*ran)[0].label != subClean {
		t.Errorf("the clean run ran %s, want the clean step", (*ran)[0].label)
	}
}

// A clean run is a command the tool honours, and it names no target: there is nothing to build for.
func TestACleanRunIsACommandTheToolHonours(t *testing.T) {
	o, err := parseArgs([]string{"clean"})
	if err != nil {
		t.Fatalf("a clean run was refused: %v", err)
	}

	if o.sub != subClean {
		t.Errorf("the run's command is %q, want %s", o.sub, subClean)
	}

	if o.target != "" {
		t.Errorf("a clean run builds for %q, and a clean run builds nothing", o.target)
	}
}

// A second release run over the same sources compiles nothing: the bundles are current, the server
// binary is current, and the phases that have nothing to do do not run. The ffmpeg and the archive are
// what the run still does, because the archive is the thing a release run is for.
func TestASecondRunForTheSameTargetRunsNoClientStepAndNoServerStep(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	tl.state = func(repo string, o options) (planState, error) {
		return planState{scannerCurrent: true, adminCurrent: true, serverCurrent: true}, nil
	}

	if code := tl.run([]string{"dist", "linux-arm64"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	got := []string{}
	for _, s := range *ran {
		got = append(got, s.label)
	}

	want := []string{"ffmpeg", "archive"}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the second run ran %v, want %v", got, want)
	}
}

// A run asked to build a server whose binary is newer than every Go file it is built from compiles
// nothing.
func TestARunSkipsTheServerStepWhenTheBinaryIsAlreadyBuilt(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	tl.state = func(repo string, o options) (planState, error) {
		return planState{scannerCurrent: true, adminCurrent: true, serverCurrent: true}, nil
	}

	if code := tl.run([]string{"server"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if len(*ran) != 0 {
		t.Errorf("the run ran %d steps, want none", len(*ran))
	}
}

// The server embeds both bundles, so a bundle built in this run makes the binary that embeds it out of
// date. The tree says the binary is newer than every Go file it is built from, and the bundles are not
// built: a run that rebuilt a bundle and left the binary standing ships the bundles the binary was built
// before.
func TestARunThatRebuildsABundleRebuildsTheServer(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	tl.state = func(repo string, o options) (planState, error) {
		// Before the run builds them, the bundles are not there and the binary is newer than every Go
		// file. After the run builds them, the bundles are newer than the binary, which is what the tree
		// says when a bundle is rebuilt.
		for _, s := range *ran {
			if s.label == subScanner || s.label == subAdmin {
				return planState{scannerCurrent: true, adminCurrent: true}, nil
			}
		}

		return planState{serverCurrent: true}, nil
	}

	if code := tl.run(nil, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	got := []string{}
	for _, s := range *ran {
		got = append(got, s.label)
	}

	want := []string{"scanner", "admin", "ffmpeg", "server"}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the run ran %v, want %v", got, want)
	}
}

// A first run, where nothing is built, runs everything: both applications, the ffmpeg, and the server.
func TestAFirstRunBuildsTheApplicationsTheFfmpegAndTheServer(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	tl.state = func(repo string, o options) (planState, error) {
		return planState{}, nil
	}

	if code := tl.run(nil, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	got := []string{}
	for _, s := range *ran {
		got = append(got, s.label)
	}

	want := []string{"scanner", "admin", "ffmpeg", "server"}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the run ran %v, want %v", got, want)
	}
}

// The native run is the build standing on the host the ffmpeg binaries are compiled on. The two
// boundaries it crosses are the external commands and the source arriving from the network, and both are
// stood in for here. The tree is a real tree, because what the run writes is a cache entry and a bundle
// directory, and those are the directories the release reads.

// whatRan is what the native run touched, gathered by the fakes: the order it ran in, and the mode the
// binaries the compile produced arrived with in the directory the build embeds from.
type whatRan struct {
	ran      []string
	dropMode map[string]os.FileMode
}

// aNativeRun is the build standing on the machine that is the target.
func aNativeRun(t *testing.T, target string, failAt string) (*tool, *bytes.Buffer, *whatRan, string) {
	t.Helper()

	return aNativeRunOn(t, target, failAt, aMachineForTarget(target))
}

// aNativeRunOn is the build standing on a host of a chosen architecture. The machine that is the target is
// the one that compiles its own binaries; a compile across the two macOS architectures stands the host of
// the other architecture in front of the same harness.
func aNativeRunOn(t *testing.T, target string, failAt string, theHost host) (*tool, *bytes.Buffer, *whatRan, string) {
	t.Helper()

	repo := t.TempDir()

	theManifestFile(t, repo)

	// The directory the binaries are dropped into is the directory the server embeds, and the repository
	// carries it empty for each supplied target.
	if err := os.MkdirAll(filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", target), 0755); err != nil {
		t.Fatalf("the tree has no bundle directory for %s: %v", target, err)
	}

	seen := &whatRan{dropMode: map[string]os.FileMode{}}

	out := &bytes.Buffer{}

	tl := &tool{
		repo:      repo,
		resolve:   func(name string) (string, error) { return "/usr/bin/" + name, nil },
		installed: func(string) bool { return true },
		errout:    io.Discard,

		// The harness is the build standing on the host the binaries are compiled on, which is the one
		// fact that makes the plan compile them instead of skipping the target.
		machine: func() host { return theHost },

		source: func(src ffmpegSource, manifest bundle.Manifest) error {
			seen.ran = append(seen.ran, "source")

			if failAt == "source" {
				return errors.New("the source the manifest pins is not there to fetch")
			}

			if err := os.MkdirAll(filepath.Dir(src.Path), 0755); err != nil {
				return err
			}

			return os.WriteFile(src.Path, []byte("the source the manifest pins"), 0644)
		},

		execute: func(s step) error {
			if s.label == failAt {
				return errors.New(s.label + " said no")
			}

			seen.ran = append(seen.ran, s.label)

			// The directory the step works in is what the step before it made: the unpack is the
			// command that creates the source tree, and a harness that runs no command makes the
			// directory the way the command that would have run makes it.
			if err := os.MkdirAll(s.dir, 0755); err != nil {
				return err
			}

			// The compile's own output: the programs the source tree ends with.
			if s.label == "make" {
				for _, name := range []string{"ffmpeg", "ffprobe"} {
					if err := os.WriteFile(filepath.Join(s.dir, name), []byte("the compiled "+name), 0755); err != nil {
						return err
					}
				}
			}

			return nil
		},

		prove: func(target string, paths ffmpegPaths, stdout io.Writer) (theRunOfTheBinaries, error) {
			seen.ran = append(seen.ran, "prove")

			drop := suppliedDropFor(paths, target)

			for _, name := range []string{"ffmpeg", "ffprobe"} {
				info, err := os.Stat(drop[name])
				if err != nil {
					return theRunOfTheBinaries{}, fmt.Errorf("the compile left no %s to run: %w", name, err)
				}

				seen.dropMode[name] = info.Mode().Perm()
			}

			if failAt == "prove" {
				return theRunOfTheBinaries{}, errors.New("the run of the binaries said no")
			}

			// The run's own gate: the binaries the compile produced register a video codec, which is the
			// program this project does not ship, said by the host that compiled them.
			if failAt == "codecs" {
				return theRunOfTheBinaries{}, errors.New(
					"the compiled ffmpeg registers video codecs: h264, mpeg4. This project ships an ffmpeg compiled for audio, and the recipe that produces it is internal/infrastructure/ffmpeg/bundle/manifest.json, which enables no video component",
				)
			}

			return aRunOfTheBinaries(), nil
		},
	}

	return tl, out, seen, repo
}

// The binaries a host compiles are run by that host, adopted into the cache entry the manifest's record
// names, and published. The run is not a skip: the operator asked for the ffmpeg of the machine they are
// on, and the server and the archive that carry it are what that run was for.
//
// The manifest is the file the release reads, and the run leaves it byte for byte as it found it: nothing
// the build learns about binaries it ran is written into a tracked file, because running them is the whole
// of what the build needs to know about them.
func TestTheNativeRunCompilesRunsTheBinariesAndPublishes(t *testing.T) {
	for _, target := range []string{"macos-arm64", "freebsd-amd64"} {
		tl, out, seen, repo := aNativeRun(t, target, "")

		original := theManifestFile(t, repo)

		skipped, err := tl.runFFmpeg(options{target: target}, out)
		if err != nil {
			t.Errorf("%s: the run on the host that is the target failed: %v", target, err)
			continue
		}

		if skipped {
			t.Errorf("%s: the run reports the binaries it compiled as binaries it has nothing to produce for, and the release ships no archive for them", target)
			continue
		}

		want := []string{"source", "unpack", "configure", "make", "strip"}

		if strings.HasPrefix(target, "macos-") {
			want = append(want, "sign")
		}

		want = append(want, "prove")

		if !sameWords(seen.ran, want) {
			t.Errorf("%s: the run did %v, want %v", target, seen.ran, want)
		}

		// The binaries are dropped where the build embeds from, and executable: the server materializes
		// them from the cache and runs them, and binaries dropped unreadable are binaries the server
		// cannot start.
		for _, name := range []string{"ffmpeg", "ffprobe"} {
			if seen.dropMode[name] != 0755 {
				t.Errorf("%s: the %s was run as mode %o, want 0755", target, name, seen.dropMode[name])
			}
		}

		body, err := os.ReadFile(filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", "manifest.json"))
		if err != nil {
			t.Fatalf("%s: the manifest the run started with is not there to read: %v", target, err)
		}

		if !bytes.Equal(body, original) {
			t.Errorf("%s: the run wrote into the manifest, the file the release reads and the operator commits", target)
		}

		// The cache entry is the one the manifest's record names, because that is the entry the next run
		// looks for: binaries gzipped into an entry named some other way are binaries the next run
		// compiles again.
		paths := ffmpegPathsFor(repo, bundle.ReadManifest(), target)

		for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
			if !aFileIsThere(paths.cachedProgram(name)) {
				t.Errorf("%s: the binaries are not in the cache entry the manifest's record names (%s)", target, paths.cached)
			}

			if !aFileIsThere(filepath.Join(paths.bundle, name)) {
				t.Errorf("%s: the bundle the server embeds has no %s", target, name)
			}
		}

		if !aFileIsThere(filepath.Join(paths.bundle, "LICENCE.md")) {
			t.Errorf("%s: the bundle carries no licence text, and the archive quotes the one it ships", target)
		}

		// The licence text travels with the cache entry, so the run that reuses these binaries publishes
		// the words this run wrote about them.
		if !aFileIsThere(filepath.Join(paths.cached, "LICENCE.md")) {
			t.Errorf("%s: the cache entry carries no licence text, so a reusing run has nothing to put in the archive", target)
		}

		for _, name := range suppliedDropFor(paths, target) {
			if aFileIsThere(name) {
				t.Errorf("%s: the raw %s is left in the directory the server embeds, next to the gzipped binaries", target, filepath.Base(name))
			}
		}

		for _, want := range []string{target, paths.bundle} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s: the run prints %q nowhere, and the operator needs to see what this run built and where", target, want)
			}
		}
	}
}

// Binaries the run refused are not cached and not published, and the manifest is left exactly as it was:
// the build writes nothing into the file the release reads, before, during, or after a run of the binaries
// it compiled.
//
// The same is true of the binaries a host compiled for the other macOS architecture: the compile runs on
// that host, the run is the binaries' own execution, and when the host cannot execute what it compiled, the
// run stops there with the binaries sitting in the bundle directory for the other Mac to run.
func TestTheNativeRunPublishesNothingWhenTheRunOfTheBinariesFails(t *testing.T) {
	for _, tc := range []struct {
		name    string
		machine host
	}{
		{name: "the target's own host", machine: host{goos: "darwin", goarch: "arm64"}},
		{name: "the host of the other macOS architecture", machine: host{goos: "darwin", goarch: "amd64"}},
	} {
		tl, out, seen, repo := aNativeRunOn(t, "macos-arm64", "prove", tc.machine)

		original := theManifestFile(t, repo)

		_, err := tl.runFFmpeg(options{target: "macos-arm64"}, out)
		if err == nil {
			t.Fatalf("%s: the run published binaries its own run refused", tc.name)
		}

		body, err := os.ReadFile(filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", "manifest.json"))
		if err != nil {
			t.Fatalf("%s: the manifest is not there to read: %v", tc.name, err)
		}

		if !bytes.Equal(body, original) {
			t.Errorf("%s: the run wrote into the manifest for binaries its run refused", tc.name)
		}

		if !sameWords(seen.ran, []string{"source", "unpack", "configure", "make", "strip", "sign", "prove"}) {
			t.Errorf("%s: the run did %v, and the run of the binaries is what refused them", tc.name, seen.ran)
		}

		paths := ffmpegPathsFor(repo, bundle.ReadManifest(), "macos-arm64")

		for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
			if aFileIsThere(paths.cachedProgram(name)) {
				t.Errorf("%s: the binaries the run refused are in the cache as %s", tc.name, name)
			}
		}

		if aFileIsThere(filepath.Join(paths.bundle, "LICENCE.md")) {
			t.Errorf("%s: the licence text names binaries the run refused", tc.name)
		}

		// The binaries the refused run left behind are where the host that can run them looks for them,
		// which is what the refusal names.
		for _, name := range []string{"ffmpeg", "ffprobe"} {
			if !aFileIsThere(suppliedDropFor(paths, "macos-arm64")[name]) {
				t.Errorf("%s: the %s the compile produced is not in %s for the other Mac to run", tc.name, name, paths.bundle)
			}
		}
	}
}

// The run of the binaries is the gate every set of binaries passes through, and the compile on the target's
// own host runs the binaries it just compiled with the same code that runs a drop: two gates would be two
// standards. Binaries that register a video codec are refused on the host that compiled them, and the
// release adopts nothing from them — no cache entry, no licence text, no archive.
func TestTheNativeRunRefusesBinariesThatRegisterVideoCodecs(t *testing.T) {
	tl, out, seen, repo := aNativeRun(t, "macos-arm64", "codecs")

	original := theManifestFile(t, repo)

	_, err := tl.runFFmpeg(options{target: "macos-arm64"}, out)
	if err == nil {
		t.Fatal("the run adopted binaries that register video codecs")
	}

	for _, want := range []string{"h264", "mpeg4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure %q does not name the video codec %s the binaries register", err.Error(), want)
		}
	}

	body, err := os.ReadFile(filepath.Join(repo, "internal", "infrastructure", "ffmpeg", "bundle", "manifest.json"))
	if err != nil {
		t.Fatalf("the manifest is not there to read: %v", err)
	}

	if !bytes.Equal(body, original) {
		t.Error("the run wrote into the manifest for binaries that register video codecs")
	}

	if !sameWords(seen.ran, []string{"source", "unpack", "configure", "make", "strip", "sign", "prove"}) {
		t.Errorf("the run did %v, and the codec probe is what refused the binaries", seen.ran)
	}

	paths := ffmpegPathsFor(repo, bundle.ReadManifest(), "macos-arm64")

	for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
		if aFileIsThere(paths.cachedProgram(name)) {
			t.Errorf("the binaries that register video codecs are in the cache as %s", name)
		}
	}

	if aFileIsThere(filepath.Join(paths.bundle, "LICENCE.md")) {
		t.Error("the licence text names binaries that register video codecs")
	}
}

// A failure is the one line the operator has to see on a machine whose output is being captured, and
// the one line a log of the run should not carry as if it were progress. It reaches the operator's
// standard error; the writer the run was given carries the run's own lines.
func TestAFailureReachesTheOperatorsStandardErrorAndNotTheRunsWriter(t *testing.T) {
	tl, out, _ := fakeRun(released, "server")

	errout := &bytes.Buffer{}

	tl.errout = errout

	if code := tl.run([]string{"server"}, out); code != 1 {
		t.Fatalf("exit code = %d, want 1: a step said no", code)
	}

	if !strings.Contains(errout.String(), "error  server:") {
		t.Errorf("the operator's standard error carries %q, want the error line", errout.String())
	}

	if strings.Contains(out.String(), "error  server:") {
		t.Errorf("the failure reached the writer the run was given, which carries the run's own lines: %q",
			out.String())
	}
}

// The run's own lines — the version it resolved, the notes, and every label and result — reach the
// writer the run was given, and a run that succeeded leaves the operator's standard error empty.
func TestTheRunsOwnLinesReachTheWriterItWasGiven(t *testing.T) {
	tl, out, _ := fakeRun(released, "")

	errout := &bytes.Buffer{}

	tl.errout = errout

	if code := tl.run([]string{"server"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	for _, want := range []string{"version 8.0.0", "server  "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the writer the run was given carries %q, which does not name %s", out.String(), want)
		}
	}

	if errout.Len() != 0 {
		t.Errorf("a run that succeeded wrote %q to the operator's standard error, want nothing", errout.String())
	}
}
