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

// These tests exercise the build run itself: the order its steps happen in, its exit status
// when a step says no, what the version subcommand prints, and what it says about the resolved
// version.

// fakeRun is a tool that records what it was told to run instead of running it.

func fakeRun(declared source, failAt string) (*tool, *bytes.Buffer, *[]step) {
	var ran []step

	// The recorder takes its appends under this lock because a dist run records from one
	// goroutine per target.
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

		// The stand-in answers the CPU-count question the ffmpeg phase asks the container engine before it
		// plans a compile in that engine's machine, and names the machine it asked by the engine's own name.
		theRun: func(argv []string, stdin []byte) ([]byte, error) {
			return []byte(theContainerMachineHasTheseCores + "\n"), nil
		},

		// A test that reads the run's own lines names the stream it wants failures
		// written to, because it is not the operator.
		errout: io.Discard,

		// The stand-in fixes the clock so a test reading the composed command lines
		// knows the moment they were composed at.
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

		// The ffmpeg stand-in records that the phase ran rather than what command it
		// ran; the phase itself is tested in ffmpeg_test.go.
		ffmpeg: func(o options, stdout io.Writer) (bool, error) {
			record(step{label: subFFmpeg, result: o.target})

			return false, nil
		},

		// The archive stand-in records that it was written; what it does is tested in
		// archive_test.go, and the pairing of the run's steps is what is under test
		// here.
		releaseFiles: func(repo string, tr releaseTarget, output string) ([]archiveFile, error) {
			return []archiveFile{{name: "rdio-scanner", body: []byte("the binary")}}, nil
		},

		archive: func(path string, files []archiveFile) error {
			record(step{label: subArchive, result: path})

			return nil
		},

		// The clean stand-in records the removal; what is removed is tested in
		// build_test.go, in a tree the test builds.
		clean: func(repo string) ([]string, error) {
			record(step{label: subClean, result: repo})

			return []string{"removed"}, nil
		},

		// The state stand-in reports nothing built yet, the state of a first run on a
		// fresh clone; the checks themselves live in build_test.go.
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

	// The server build comes after the two application bundles and the ffmpeg because the
	// server embeds all three.
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

// The version reaches the server through the linker and the applications through the
// environment, never rewritten into a source file.
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

// A failed step ends the run and the steps after it do not run, because a server built after a
// failed bundle build would embed a bundle that is not there.
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

// The run logs the version it resolved and where it came from, because a resolved version has
// to be explainable.
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

// A prerelease name that kept its commit because the tree has uncommitted work
// says so on the run's own log; a prerelease name that kept nothing says nothing.
func TestTheRunExplainsAPrereleaseNameThatKeptItsCommit(t *testing.T) {
	tl, out, _ := fakeRun(uncommitted, "")

	if code := tl.run([]string{"client", "--pre", "beta1"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	named := []string{}

	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.Contains(line, "beta1") && strings.Contains(line, "+g") && strings.Contains(line, "uncommitted") {
			named = append(named, line)
		}
	}

	if len(named) != 1 {
		t.Fatalf("the run's log\n  %s\nholds %d lines naming the prerelease name, the commit part it kept, and the uncommitted tree; want exactly one",
			out.String(), len(named))
	}

	tl, out, _ = fakeRun(development, "")

	if code := tl.run([]string{"client", "--pre", "beta1"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.Contains(line, "beta1") && strings.Contains(line, "+g") {
			t.Errorf("the log of a prerelease built from a committed tree says %q, and it kept no commit part to say anything about", line)
		}
	}
}

// The version subcommand answers the version alone even when the prerelease name
// keeps its commit: one line, and no note beside it.
func TestTheRunExplainsAPrereleaseNameInTheVersionSubcommandAlone(t *testing.T) {
	for _, c := range []struct {
		src  source
		want string
	}{
		{src: development, want: "8.0.0-beta1"},
		{src: uncommitted, want: "8.0.0-beta1+gdb79311"},
	} {
		tl, out, ran := fakeRun(c.src, "")

		if code := tl.run([]string{"version", "--pre", "beta1"}, out); code != 0 {
			t.Fatalf("commit %s dirty %v: exit code = %d, want 0", c.src.commit, c.src.dirty, code)
		}

		if len(*ran) != 0 {
			t.Errorf("commit %s dirty %v: the version subcommand ran %d steps, want none", c.src.commit, c.src.dirty, len(*ran))
		}

		lines := strings.Split(strings.TrimSpace(out.String()), "\n")

		if len(lines) != 1 {
			t.Fatalf("commit %s dirty %v: the version subcommand printed %d lines:\n  %s\nwant the version and nothing else",
				c.src.commit, c.src.dirty, len(lines), out.String())
		}

		if lines[0] != c.want {
			t.Errorf("commit %s dirty %v: the version subcommand printed %q, want %q", c.src.commit, c.src.dirty, lines[0], c.want)
		}
	}
}

// A dist run builds the applications once, then each target's ffmpeg serially, then every
// target's compile and archive at once; the ordering rules live.
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

	// The remaining recorded steps are exactly the matrix's compiles and archives, each
	// archive following its own target's compile.
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

// theStepsCounted counts recorded steps so a test can name which steps ran without naming the
// order the machine finished them in.
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

// A dist run reads each target's archive from the path that target's server step wrote to,
// never a path it chose for itself.
func TestADistRunReadsTheArchiveFromThePathTheServerStepWritesTo(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	var read []string

	// The archive reads are recorded from per-target workers, so this recorder takes its
	// appends under a lock of its own.
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

// The archive of a release run carries the binary that same run compiled, replaced before
// anything reads it.
func TestADistRunArchivesTheBinaryItCompiled(t *testing.T) {
	repo := t.TempDir()

	target := "linux-arm64"

	writeArchiveSource(t, repo, target, "rdio-scanner", "the binary from an earlier build")
	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeArchiveLicence(t, repo, target, "the licence")

	tl, _, _ := fakeRun(released, "")

	tl.repo = repo
	tl.releaseFiles = archiveFiles

	// The tool runs with the repository as its working directory, so the archive path a run names is a
	// path inside it.
	tl.archive = func(path string, files []archiveFile) error {
		return writeArchive(filepath.Join(repo, path), files)
	}

	// The stand-in compile writes its binary where the step says it writes and nowhere
	// else.
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

// A binary that is newer than every file it is built from is a binary that reports the version the
// release ships. The release names its archive for the commit it is built from, so after two runs
// with a moved commit between them, the second run's archive carries a program that reports the
// version that archive is named for.
func TestTheSecondOfTwoReleaseRunsArchivesAProgramThatReportsItsOwnVersion(t *testing.T) {
	repo := scratchRepository(t, "")

	target := "linux-amd64"

	writeScratchVersionFile(t, repo, "8.0.0")

	// The build writes binaries and archives under dist; a tree the release is built from
	// stays clean only while that output is ignored.
	writeRepoFile(t, repo, ".gitignore", "/dist\n")

	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")
	writeArchiveLicence(t, repo, target, "the licence")

	// A release is built from a commit, so everything these runs read is committed before
	// the first one runs.
	for _, args := range [][]string{
		{"add", "-A"},
		{"commit", "-q", "-m", "the tree the first release is built from"},
	} {
		if out := scratchGit(t, repo, args...); out != "" {
			t.Fatalf("git %s in the scratch repository printed %q", strings.Join(args, " "), out)
		}
	}

	tl, _, _ := fakeRun(released, "")

	// This run asks its own scratch repository what version it is, builds from what the
	// tree says has already been built, and writes the archive it names.
	tl.repo = repo
	tl.read = func(string) (source, error) {
		return readSource(repo)
	}
	tl.state = buildState
	tl.releaseFiles = archiveFiles
	tl.archive = func(path string, files []archiveFile) error {
		return writeArchive(filepath.Join(repo, path), files)
	}

	// The stand-in compile writes, where the step says it writes, a program whose body is
	// the version that step stamped through the linker: what a real binary reports.
	tl.execute = func(s step) error {
		stamp := ""

		for _, arg := range s.args {
			if rest, found := strings.CutPrefix(arg, "-s -w -X main.Version="); found {
				stamp = strings.Fields(rest)[0]
			}
		}

		// A step that stamps no version compiles no server binary.
		if stamp == "" {
			return nil
		}

		path := filepath.Join(repo, theOutputPathOf(s))

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(path), err)
		}

		if err := os.WriteFile(path, []byte(stamp), 0o755); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}

		return nil
	}

	if code := tl.run([]string{"dist", target}, io.Discard); code != 0 {
		t.Fatalf("the first release run's exit code = %d, want 0", code)
	}

	// A commit that touches no file the server is built from still changes the version the
	// next release is named for.
	if out := scratchGit(t, repo, "commit", "-q", "--allow-empty", "-m", "a commit the release names itself for"); out != "" {
		t.Fatalf("git commit in the scratch repository printed %q", out)
	}

	if code := tl.run([]string{"dist", target}, io.Discard); code != 0 {
		t.Fatalf("the second release run's exit code = %d, want 0", code)
	}

	commit := scratchGit(t, repo, "rev-parse", "--short", "HEAD")

	// The program the second run's archive carries is read out of it the way a listener's
	// download would read it.
	body, err := os.ReadFile(filepath.Join(repo, archivePath(target, "8.0.0+g"+commit)))
	if err != nil {
		t.Fatalf("the second release run wrote no archive: %v", err)
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

	if program != "8.0.0+g"+commit {
		t.Errorf("the second run's archive carries a program reporting %q, want the version %s it is named for",
			program, "8.0.0+g"+commit)
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

// theTargetOf reads a step's target out of its environment by the GOOS and GOARCH values the
// matrix sets for each target.
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

// A release run refuses a tree with uncommitted changes before building anything and names the
// paths and the override.
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

// A prerelease name does not soften the release refusal: a release run asked to
// package an uncommitted tree stops before it builds anything.
func TestADistRunAskedForAPrereleaseStillRefusesAnUncommittedTree(t *testing.T) {
	tl, out, ran := fakeRun(uncommitted, "")

	errout := &bytes.Buffer{}

	tl.errout = errout

	if code := tl.run([]string{"dist", "--pre", "beta1"}, out); code != 1 {
		t.Fatalf("exit code = %d, want 1: a release run asked to package a tree that is not committed", code)
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

// A run that used the override says on its own log that the release is built from an
// uncommitted tree.
func TestADistRunBuiltFromAnUncommittedTreeSaysSo(t *testing.T) {
	tl, out, _ := fakeRun(uncommitted, "")

	if code := tl.run([]string{"dist", "--allow-dirty"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if !strings.Contains(out.String(), "uncommitted") {
		t.Errorf("the run's log %q does not say the release is built from an uncommitted tree", out.String())
	}
}

// A release run reports the archives it wrote and the targets it wrote none for, and its exit
// status says whether the release is complete.
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

// A dist run asked for a target that wrote no archive ends non-zero so scripts can tell an
// incomplete machine from a broken run.
func TestADistRunExitsNonZeroWhenATargetItWasAskedForWroteNoArchive(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	tl.ffmpeg = wroteNoFfmpegFor(ran, "linux-386", "windows-amd64")

	if code := tl.run([]string{"dist", "--all"}, out); code != 3 {
		t.Fatalf("exit code = %d, want 3: two of the eight targets the run was asked for wrote no archive", code)
	}

	for _, want := range []string{"6 archives written", "2 targets wrote none: linux-386, windows-amd64"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the run's log %q does not say %q", out.String(), want)
		}
	}
}

// A run asked for one target whose ffmpeg phase produced nothing exits non-zero instead of
// shipping a server that embeds nothing.
func TestADistRunAskedForOneTargetThatWroteNoFfmpegExitsNonZero(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	tl.ffmpeg = wroteNoFfmpegFor(ran, "linux-386")

	if code := tl.run([]string{"dist", "linux-386"}, out); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}

	if !strings.Contains(out.String(), "0 archives written, 1 target wrote none: linux-386") {
		t.Errorf("the run's log %q does not say which target wrote none", out.String())
	}

	for _, s := range *ran {
		if s.label == subArchive || s.label == subServer {
			t.Errorf("%s ran for %s, a target that wrote no ffmpeg to embed", s.label, s.result)
		}
	}
}

// A release run reports a macOS target whose prepared SDK tree is absent as one it wrote no
// archive for.
func TestAReleaseRunReportsAMacOSTargetWhoseSysrootIsAbsentAsWritingNoArchive(t *testing.T) {
	manifest := bundle.ReadManifest()

	record := manifest.Targets["macos-amd64"]

	home := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv(darwinSysrootVariable, "")

	lookedIn := filepath.Join(home, ".cache", "rdio-scanner", "ffmpeg-darwin-sdk", record.Sysroot.SDK,
		"MacOSX"+record.Sysroot.SDK+".sdk")

	tl, out, ran := fakeRun(released, "")

	tl.repo = t.TempDir()
	tl.machine = func() host { return theReleaseHost }

	tl.ffmpeg = tl.runFFmpeg

	if code := tl.run([]string{"dist", "macos-amd64"}, out); code != 3 {
		t.Fatalf("exit code = %d, want 3: the one archive this run was asked for is compiled against a tree this machine has not prepared", code)
	}

	for _, want := range []string{"0 archives written, 1 target wrote none: macos-amd64", lookedIn, record.Sysroot.SDK, darwinSysrootVariable} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the run's log %q does not say %q", out.String(), want)
		}
	}

	// A target whose compile cannot start gets no container, no compile, and no archive,
	// and no server either: it would embed no ffmpeg.
	for _, s := range *ran {
		if s.label == subArchive || s.label == subServer || s.label == "image" || s.label == "configure" {
			t.Errorf("%s ran for %s, a target whose compile this machine cannot start", s.label, s.result)
		}
	}
}

// The ffmpeg phase refuses a target whose prepared tree records another tree's digest, and
// fetches and builds nothing for it.
func TestARunAskedForTheFfmpegOfATargetWhoseSysrootRecordsAnotherTreeIsRefused(t *testing.T) {
	manifest := bundle.ReadManifest()

	record := manifest.Targets["macos-amd64"]

	const theDigestOfSomeOtherTree = "0000000000000000000000000000000000000000000000000000000000000000"

	home := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv(darwinSysrootVariable, "")

	tree := filepath.Join(home, ".cache", "rdio-scanner", "ffmpeg-darwin-sdk", record.Sysroot.SDK,
		"MacOSX"+record.Sysroot.SDK+".sdk")

	if err := writeASysrootTree(tree); err != nil {
		t.Fatalf("the test cannot write the sysroot it was shown: %v", err)
	}

	if err := writeTheSysrootDigestRecordedFor(record, tree); err != nil {
		t.Fatalf("the test cannot write the digest the preparation records beside its tree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(filepath.Dir(tree), "sha256"), []byte(theDigestOfSomeOtherTree+"\n"), 0644); err != nil {
		t.Fatalf("the test cannot relabel the tree with another tree's digest: %v", err)
	}

	tl, out, ran := fakeRun(released, "")

	errout := &bytes.Buffer{}

	tl.errout = errout
	tl.repo = t.TempDir()
	tl.machine = func() host { return theReleaseHost }

	tl.ffmpeg = tl.runFFmpeg

	if code := tl.run([]string{"ffmpeg", "macos-amd64"}, out); code != 1 {
		t.Fatalf("exit code = %d, want 1: the one ffmpeg this run was asked for is compiled against a tree whose record does not match its own", code)
	}

	for _, want := range []string{"macos-amd64", record.Sysroot.SDK, tree, theDigestOfSomeOtherTree, record.Sysroot.SHA256, darwinSysrootVariable} {
		if !strings.Contains(errout.String(), want) {
			t.Errorf("the refusal on the operator's stream %q does not say %q", errout.String(), want)
		}
	}

	if strings.Contains(errout.String(), "no prepared sysroot") {
		t.Errorf("a tree that is there was refused as a tree that is not: %q", errout.String())
	}

	if strings.Contains(out.String(), record.Sysroot.SHA256) {
		t.Errorf("the refusal of the tree reached %q, and a refusal belongs to the operator, not to the run's progress", out.String())
	}

	for _, s := range *ran {
		if s.label == "image" || s.label == "unpack" || s.label == "configure" || s.label == "make" || s.label == subServer {
			t.Errorf("%s ran for %s, a target refused before its compile could start", s.label, s.result)
		}
	}
}

// A dist run removes release archives of versions it did not release, so an upload cannot grab
// the wrong file sitting beside the right one.
func TestADistRunRemovesTheArchivesOfTheVersionsItDidNotRelease(t *testing.T) {
	repo := t.TempDir()

	// The release folder fixtures: an older version's leftover folder, an archive for a
	// target this run does not build, a double-v prerelease name, and a non-archive file.
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

// A release run asked for a prerelease name writes the archive that name names, and
// prunes the release folder by the same name it wrote.
func TestADistRunAskedForAPrereleaseNamesItsArchivesAndPrunesByThatName(t *testing.T) {
	repo := t.TempDir()

	// The release folder fixtures: the plain version's archive for the target this run
	// builds, and this prerelease's archive for a target it does not build.
	writeRepoFile(t, repo, "dist/rdio-scanner-linux-amd64-v8.0.0.zip", "the archive of the release this prerelease stands beside")
	writeRepoFile(t, repo, "dist/rdio-scanner-linux-arm64-v8.0.0-beta1.zip", "the archive of this prerelease, for a target this run is not building")

	tl, out, _ := fakeRun(development, "")

	tl.repo = repo
	tl.archive = func(path string, files []archiveFile) error {
		return writeArchive(filepath.Join(repo, path), files)
	}

	if code := tl.run([]string{"dist", "linux-amd64", "--pre", "beta1"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	for _, path := range []string{
		"dist/rdio-scanner-linux-amd64-v8.0.0-beta1.zip",
		"dist/rdio-scanner-linux-arm64-v8.0.0-beta1.zip",
	} {
		if _, err := os.Stat(filepath.Join(repo, path)); err != nil {
			t.Errorf("the release folder holds no %s, and it is an archive of the version the run released: %v\nthe run wrote\n  %s",
				path, err, out.String())
		}
	}

	if _, err := os.Stat(filepath.Join(repo, "dist/rdio-scanner-linux-amd64-v8.0.0-beta1+gdb79311.zip")); err == nil {
		t.Errorf("the release run named an archive for the prerelease of a committed tree by a commit part that version does not carry:\n  %s",
			out.String())
	}

	path := "dist/rdio-scanner-linux-amd64-v8.0.0.zip"

	if _, err := os.Stat(filepath.Join(repo, path)); err == nil {
		t.Errorf("%s survived a run that released 8.0.0-beta1, and it is an archive of a version the run did not release", path)
	}

	if !strings.Contains(out.String(), path) {
		t.Errorf("the run's log %q does not name the removal of %s", out.String(), path)
	}
}

// The version part of an archive name is split after the known target, not at the last hyphen,
// because prerelease names carry hyphens.
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

// Every per-target line of a release run's log names the target the line is about.
func TestAReleaseRunNamesTheTargetEveryLineOfItsLogIsAbout(t *testing.T) {
	tl, out, _ := fakeRun(released, "")

	// Every target the manifest records is compiled by this host, so the matrix run writes all eight archives
	// and exits 0.
	if code := tl.run([]string{"dist", "--all", "--allow-dirty"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}

		word := strings.Fields(line)[0]

		// The run's own untagged lines are the version line, the notes, the two shared
		// application builds, the summary, and the removals.
		if word == "version" || word == "note" || word == subDist || word == subScanner || word == subAdmin {
			continue
		}

		if !isTarget(word) {
			t.Errorf("the log line %q does not name the target its work is about", line)
		}
	}
}

// The ffmpeg phase stays serial and finishes before any server compile starts, so the log
// records one target's block of lines at a time.
func TestAReleaseRunFinishesTheFfmpegPhaseBeforeTheServerCompilesStart(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"dist", "--all", "--allow-dirty"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
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

// The archives a release run writes must not depend on the order its targets ran in, and each
// archive reads only its own target's binary.
func TestAReleaseRunWritesTheSameArchivesWhicheverWayTheTargetsRan(t *testing.T) {
	repo := t.TempDir()

	// The fixture tree holds each target's binary, the two guides, and each ready target's
	// ffmpeg licence, with every file time fixed so both runs archive one identical tree.
	for _, tr := range releaseTargets {
		target := tr.targetWord()

		writeRepoFile(t, repo, defaultOutputPath(target), "the binary of "+target)
		writeArchiveLicence(t, repo, target, "the licence of "+target)
	}

	writeRepoFile(t, repo, "docs/admin-guide.html", "the guide")
	writeRepoFile(t, repo, "docs/user-guide.html", "the guide for the listener")

	fixEveryFileTime(t, repo, time.Date(2026, 1, 2, 3, 4, 6, 0, time.Local))

	tl, _, _ := fakeRun(released, "")

	tl.repo = repo
	tl.ffmpeg = func(options, io.Writer) (bool, error) { return false, nil }

	// The binaries are the ones this tree's sources produce, so the runs copy them into
	// their archives and compile nothing.
	tl.state = func(string, options) (planState, error) {
		return planState{serverCurrent: true}, nil
	}

	tl.releaseFiles = archiveFiles

	// The run composes archive paths relative to the tree it builds, and this harness
	// points that tree at the temporary one.
	tl.archive = func(path string, files []archiveFile) error {
		return writeArchive(filepath.Join(repo, path), files)
	}

	t.Setenv(buildJobsEnv, "1")

	if code := tl.run([]string{"dist", "--all", "--allow-dirty"}, io.Discard); code != 0 {
		t.Fatalf("the serial release run: exit code = %d, want 0", code)
	}

	serial := theArchivesOfTheRelease(t, repo)

	t.Setenv(buildJobsEnv, "4")

	if code := tl.run([]string{"dist", "--all", "--allow-dirty"}, io.Discard); code != 0 {
		t.Fatalf("the parallel release run: exit code = %d, want 0", code)
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

// wroteNoFfmpegFor is the ffmpeg phase of a stand-in run that produced no ffmpeg for the
// targets it names, exercising the summary line and exit status for a partly unfillable
// matrix.
func wroteNoFfmpegFor(ran *[]step, targets ...string) func(options, io.Writer) (bool, error) {
	return func(o options, stdout io.Writer) (bool, error) {
		for _, target := range targets {
			if o.target == target {
				fmt.Fprintf(stdout, "%-7s the build wrote no ffmpeg for %s\n", subFFmpeg, o.target)

				return true, nil
			}
		}

		if ran != nil {
			*ran = append(*ran, step{label: subFFmpeg, result: o.target})
		}

		return false, nil
	}
}

// fixEveryFileTime gives every file in a tree the same time, so the archives carry the tree's
// times and not the moment the test ran.
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

// The verification run runs the two test suites and builds nothing, and it is the command a
// release is gated on.
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

// A failing suite ends the verification run non-zero and names the suite that failed.
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

// The target whose ffmpeg fails ends the dist run, which names that target and leaves nothing
// compiled: the ffmpeg phase finishes before any compile starts.
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

// A dist run that names no target builds the single archive for the platform it is standing
// on, not the whole cross matrix.
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

// A container run builds the two application bundles and the image architecture's ffmpeg, then
// asks podman to build the image, which compiles the server inside itself.
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

// The container step passes the resolved version to podman as a build argument and tags the
// image with it beside latest, naming the Containerfile explicitly.
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

// A container run is for linux-amd64, the architecture the Containerfile's own guard checks
// for.
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

// A clean run removes the release output, the dependencies, and both bundles' contents, and
// builds nothing.
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

// A second release run over current sources runs no client step and no server step, and still
// writes the ffmpeg and the archive.
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

// Rebuilding a bundle makes the embedding server binary out of date, so a run that rebuilds a
// bundle rebuilds the server too.
func TestARunThatRebuildsABundleRebuildsTheServer(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	tl.state = func(repo string, o options) (planState, error) {
		// Before the run the bundles are absent and the binary is newer than every Go
		// file; after it the bundles are newer than the binary.
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

// The native run is the build standing on the host that compiles the binaries, with the
// external commands and the network source stood in.

// whatRan is what the native run harness recorded: the order the steps ran in and the mode the
// produced binaries arrived with.
type whatRan struct {
	ran      []string
	dropMode map[string]os.FileMode
}

// aNativeRun is the build standing on the machine that is the target.
func aNativeRun(t *testing.T, target string, failAt string) (*tool, *bytes.Buffer, *whatRan, string) {
	t.Helper()

	return aNativeRunOn(t, target, failAt, aMachineForTarget(target))
}

// aNativeRunOn builds the harness on a host of a chosen architecture; a cross-architecture
// macOS compile borrows it with the other host.
func aNativeRunOn(t *testing.T, target string, failAt string, theHost host) (*tool, *bytes.Buffer, *whatRan, string) {
	t.Helper()

	repo := t.TempDir()

	theManifestFile(t, repo)

	// The bundle directory receives the compiled binaries and is the directory the server
	// embeds, kept empty in the repository for every matrix target.
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

		// The host the build stands on is the one fact that makes the plan use the
		// host's own toolchain instead of a container.
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

			// The step's working directory is what the unpack command would have
			// created, and the fake creates it the same way.
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

		prove: func(target string, record bundle.Target, paths ffmpegPaths, stdout io.Writer) (theRunOfTheBinaries, error) {
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

			return aRunOfTheBinaries(), nil
		},
	}

	return tl, out, seen, repo
}

// The native run compiles its host's targets, runs the binaries, adopts them into the cache
// entry the manifest names, and publishes; the manifest is never rewritten.
func TestTheNativeRunCompilesRunsTheBinariesAndPublishes(t *testing.T) {
	for _, target := range []string{"macos-arm64"} {
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

		// The sign step is the host's own codesign, so the program that host signed is
		// the program that host's loader accepts.
		want := []string{"source", "unpack", "configure", "make", "strip", "sign", "prove"}

		if !sameWords(seen.ran, want) {
			t.Errorf("%s: the run did %v, want %v", target, seen.ran, want)
		}

		// The binaries land in the embedded bundle directory as executables, because
		// the server materializes and runs them from there.
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

		// The binaries are adopted into the cache entry the manifest's record names,
		// because that is the entry the next run looks in.
		paths := ffmpegPathsFor(repo, bundle.ReadManifest(), target)

		for _, name := range []string{"ffmpeg.gz", "ffprobe.gz"} {
			if !aFileIsThere(paths.cachedProgram(name)) {
				t.Errorf("%s: the binaries are not in the cache entry the manifest's record names (%s)", target, paths.cached)
			}

			if !aFileIsThere(filepath.Join(paths.bundle, name)) {
				t.Errorf("%s: the bundle the server embeds has no %s", target, name)
			}
		}

		if !aFileIsThere(filepath.Join(paths.bundle, "FFMPEG.txt")) {
			t.Errorf("%s: the bundle carries no licence text, and the archive quotes the one it ships", target)
		}

		// The licence text travels inside the cache entry, so a run reusing these
		// binaries publishes this run's licence text.
		if !aFileIsThere(filepath.Join(paths.cached, "FFMPEG.txt")) {
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

// Binaries whose proving run failed are not cached and not published, the manifest stays
// exactly as found, and the binaries wait in the bundle directory for the host that can run
// them.
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

		if aFileIsThere(filepath.Join(paths.bundle, "FFMPEG.txt")) {
			t.Errorf("%s: the licence text names binaries the run refused", tc.name)
		}

		// A run that did not finish writing the entry leaves no proof in it: the licence text a
		// compile writes goes in beside the pair it gzipped, and this run gzipped nothing.
		if aFileIsThere(filepath.Join(paths.cached, "FFMPEG.txt")) {
			t.Errorf("%s: the cache entry holds the licence text of binaries the run refused: %s", tc.name, paths.cached)
		}

		// The refused run leaves its binaries in the bundle directory of the host that
		// can run them, which is what the refusal names.
		for _, name := range []string{"ffmpeg", "ffprobe"} {
			if !aFileIsThere(suppliedDropFor(paths, "macos-arm64")[name]) {
				t.Errorf("%s: the %s the compile produced is not in %s for the other Mac to run", tc.name, name, paths.bundle)
			}
		}
	}
}

// A failure reaches the operator's standard error, and the run's own writer carries only the
// run's own lines.
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

// The run's own lines reach the writer it was given, and a successful run leaves the
// operator's standard error empty.
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
