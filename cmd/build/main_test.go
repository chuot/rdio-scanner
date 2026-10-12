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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		ffmpeg: func(o options, stdout io.Writer) (bool, error) {
			ran = append(ran, step{label: subFFmpeg, result: o.target})

			return false, nil
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

// A release run asked for the whole matrix builds the applications once, because the bundles are the same
// for every target, and then each target's ffmpeg before the server that embeds it. The order is the same
// reason a single run has it: a server built before the ffmpeg its target carries exists is a server that
// ships without one.
func TestADistRunBuildsTheApplicationsOnceAndThenEachTarget(t *testing.T) {
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
// archive directory with a server in it and no ffmpeg to embed. This is the promise the compiled targets
// keep, and it is not the way a supplied pair that was never dropped is treated.
func TestADistRunStopsAtTheTargetWhoseFfmpegFails(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	tl.ffmpeg = func(o options, stdout io.Writer) (bool, error) {
		if o.target == "linux-arm" {
			return false, errors.New("the ffmpeg for linux-arm said no")
		}

		*ran = append(*ran, step{label: subFFmpeg, result: o.target})

		return false, nil
	}

	if code := tl.run([]string{"dist", "--all"}, out); code != 1 {
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

// skipsSuppliedPairs makes the stand-in answer the way the ffmpeg phase answers for a target whose
// binaries the maintainer never dropped: the message names the target, the host that compiles them, the
// command that compiles them there, and the directory to drop them in, and the target is reported as
// skipped.
func skipsSuppliedPairs(tl *tool, ran *[]step) {
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
// run says which target it skipped and what would have made it buildable, ships the rest, and ends
// successfully.
func TestADistRunSkipsASuppliedTargetWhoseBinariesWereNotDropped(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	skipsSuppliedPairs(tl, ran)

	if code := tl.run([]string{"dist", "--all"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0: a release run missing one supplied target's binaries ships the rest", code)
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
// operator is told what would have made it buildable.
func TestADistRunForASuppliedTargetWithNothingDroppedProducesNoArchive(t *testing.T) {
	tl, out, ran := fakeRun(released, "")

	skipsSuppliedPairs(tl, ran)

	if code := tl.run([]string{"dist", "macos-arm64"}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
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

// The native run is the build standing on the host a supplied pair is compiled on. The two boundaries it
// crosses are the external commands and the source arriving from the network, and both are stood in for
// here. The tree is a real tree, because what the run writes is a manifest pin, a cache entry, and a bundle
// directory, and those are the files the release reads.

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

			// The compile's own output: the pair the source tree ends with.
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
