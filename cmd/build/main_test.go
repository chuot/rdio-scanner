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

		// The archive is not an external command. The stand-in records that it was written, and what it
		// does is tested in archive_test.go. The archive reads what the release wrote into the tree, so
		// the run's order is what is under test here.
		releaseFiles: func(repo string, tr releaseTarget) ([]archiveFile, error) {
			return []archiveFile{{name: "rdio-scanner", body: []byte("the binary")}}, nil
		},

		archive: func(path string, files []archiveFile) error {
			ran = append(ran, step{label: subArchive, result: path})

			return nil
		},

		// A clean run removes files from the tree. The stand-in records that it ran; what it removes is
		// tested in build_test.go, in a tree the test made.
		clean: func(repo string) ([]string, error) {
			ran = append(ran, step{label: subClean, result: repo})

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
		"archive " + archivePath("freebsd-amd64", "8.0.0"),
		"ffmpeg linux-386",
		"server " + defaultOutputPath("linux-386"),
		"archive " + archivePath("linux-386", "8.0.0"),
		"ffmpeg linux-amd64",
		"server " + defaultOutputPath("linux-amd64"),
		"archive " + archivePath("linux-amd64", "8.0.0"),
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the run ran\n  %v\nwant\n  %v", got, want)
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
		"container rdio-scanner:latest",
	}

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the container run ran\n  %v\nwant\n  %v", got, want)
	}
}

// The version the image reports is the one the run resolved, through the build argument the
// Containerfile reads. An image built without it reports the development fallback, which is the sign
// that nobody said what the image is.
func TestTheContainerStepAsksPodmanToBuildTheImageWithTheResolvedVersion(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"container"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	last := (*ran)[len(*ran)-1]

	if last.name != "podman" {
		t.Fatalf("the container run ended with %s, want podman", last.name)
	}

	want := []string{"build", "--pull", "--build-arg", "VERSION=8.0.0", "-t", "rdio-scanner:latest", "."}

	if strings.Join(last.args, " ") != strings.Join(want, " ") {
		t.Errorf("podman ran with %v, want %v", last.args, want)
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
