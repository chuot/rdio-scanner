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
	"errors"
	"io"
	"strings"
	"testing"
)

// The tool's whole job is to run the right commands in the right order with the resolved version in
// them. The plan it composes is tested in commands_test.go and the machine it touches in
// build_test.go; what is tested here is the run itself: the order the steps happen in, what the
// process exits with when a step says no, what the version subcommand prints, and what the run says
// about the version it resolved.

// fakeRun is a tool that records what it was told to run instead of running it.

func fakeRun(declared source, failAt string) (*tool, *bytes.Buffer, *[]step) {
	var ran []step

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
		execute: func(s step) error {
			if s.label == failAt {
				return errors.New(s.label + " said no")
			}

			ran = append(ran, s)

			return nil
		},

		// The ffmpeg phase is not one external command, so the stand-in records that it ran rather than
		// what it ran. What it does is tested in ffmpeg_test.go.
		ffmpeg: func(o options, stdout io.Writer) error {
			ran = append(ran, step{label: subFFmpeg, result: o.target})

			return nil
		},

		// The document and the archive are not external commands either. The stand-in records that they
		// were written, and what they do is tested in docs_test.go and archive_test.go.
		// The document is rendered from the tree, and the archive reads what the release wrote into the
		// tree. Both are seams the stand-in answers, so the run's order is what is under test here.
		document: func(repo string, tr releaseTarget, version string, date string) (string, error) {
			return "<html>" + tr.targetWord() + "</html>", nil
		},

		releaseFiles: func(repo string, tr releaseTarget) ([]archiveFile, error) {
			return []archiveFile{{name: "rdio-scanner", body: []byte("the binary")}}, nil
		},

		write: func(path string, body []byte) error {
			ran = append(ran, step{label: subDocument, result: path})

			return nil
		},

		archive: func(path string, files []archiveFile) error {
			ran = append(ran, step{label: subArchive, result: path})

			return nil
		},
	}

	return t, out, &ran
}

var released = source{core: "8.0.0", declared: true, tag: "v8.0.0", commit: "db79311"}
var development = source{core: "8.0.0", declared: true, commit: "db79311"}

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

	if code := tl.run([]string{"--pre", "beta 1"}, out); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}

	if len(*ran) != 0 {
		t.Errorf("%d steps ran after a rejected command line", len(*ran))
	}

	if !strings.Contains(out.String(), "beta 1") {
		t.Errorf("the refusal %q does not name the prerelease it rejected", out.String())
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

	if code := tl.run(nil, out); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}

	if len(*ran) != 1 {
		t.Errorf("%d steps ran after the administration build failed, want only the scanner build", len(*ran))
	}

	if !strings.Contains(out.String(), "admin") {
		t.Errorf("the failure %q does not say which step failed", out.String())
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

// A release run builds the applications once, because the bundles are the same for every target, and
// then each target's ffmpeg before the server that embeds it. The order is the same reason a single run
// has it: a server built before the ffmpeg its target carries exists is a server that ships without one.
func TestADistRunBuildsTheApplicationsOnceAndThenEachTarget(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"dist"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	got := []string{}
	for _, s := range *ran {
		got = append(got, s.label+" "+s.result)
	}

	want := []string{"scanner " + appBundle[subScanner], "admin " + appBundle[subAdmin]}
	for _, tr := range releaseTargets {
		want = append(want,
			"ffmpeg "+tr.targetWord(),
			"server "+defaultOutputPath(tr.targetWord()),
			"document "+documentPath(tr.targetWord()),
			"archive "+archivePath(tr.targetWord(), "8.0.0"),
		)
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the release run ran\n  %v\nwant\n  %v", got, want)
	}
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
		"document " + documentPath("linux-arm64"),
		"archive " + archivePath("linux-arm64", "8.0.0"),
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the release run ran\n  %v\nwant\n  %v", got, want)
	}
}

// A target whose ffmpeg fails ends the run, and the run names the target it stopped at. The targets
// after it are not built: a half matrix is not a release, and a run that carried on would leave an
// archive directory with a server in it and no ffmpeg to embed.
func TestADistRunStopsAtTheTargetWhoseFfmpegFails(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	tl.ffmpeg = func(o options, stdout io.Writer) error {
		if o.target == "linux-arm" {
			return errors.New("the ffmpeg for linux-arm said no")
		}

		*ran = append(*ran, step{label: subFFmpeg, result: o.target})

		return nil
	}

	if code := tl.run([]string{"dist"}, out); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}

	if !strings.Contains(out.String(), "linux-arm") {
		t.Errorf("the run's output %q does not name the target it stopped at", out.String())
	}

	got := []string{}
	for _, s := range *ran {
		got = append(got, s.label+" "+s.result)
	}

	want := []string{
		"scanner " + appBundle[subScanner],
		"admin " + appBundle[subAdmin],
		"ffmpeg freebsd-amd64",
		"server " + defaultOutputPath("freebsd-amd64"),
		"document " + documentPath("freebsd-amd64"),
		"archive " + archivePath("freebsd-amd64", "8.0.0"),
		"ffmpeg linux-386",
		"server " + defaultOutputPath("linux-386"),
		"document " + documentPath("linux-386"),
		"archive " + archivePath("linux-386", "8.0.0"),
		"ffmpeg linux-amd64",
		"server " + defaultOutputPath("linux-amd64"),
		"document " + documentPath("linux-amd64"),
		"archive " + archivePath("linux-amd64", "8.0.0"),
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the run ran\n  %v\nwant\n  %v", got, want)
	}
}
