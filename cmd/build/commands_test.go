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
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"rdio-scanner/cmd/build/buildinfo"
)

// The command line and the server and container steps are the two halves of the tool that can be
// read without running anything: what the operator asked for, and the commands that asking composes.
// The plan the tool composes for the applications is tested in webapps_test.go. These tests refuse
// the command lines the tool cannot honour and read the server binary and the container image the
// run composes. Nothing here runs a build.

// theVersion is the version a plan is composed for: an untagged build of the declared core, which is
// the shape a development build has.
func theVersion(t *testing.T) buildinfo.Version {
	t.Helper()

	version, err := buildinfo.Derive("8.0.0", "", "db79311", "")
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}

	return version
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}

	return false
}

// The version reaches the server through the linker. It reaches the bundles through the environment.
// Neither is rewritten in a source file, which is the mechanism that produced the drift this tool
// exists to remove.
func TestTheServerBuildCarriesTheResolvedVersionInTheLinkerFlags(t *testing.T) {
	o, err := parseArgs([]string{"server"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	s := serverStep(o, "/repo", theVersion(t))

	want := "go build -ldflags -X main.Version=8.0.0+gdb79311 -o " + defaultOutputPath("") + " ./cmd/rdio-scanner"
	got := strings.Join(append([]string{s.name}, s.args...), " ")
	if got != want {
		t.Errorf("the server is built with\n  %s\nwant\n  %s", got, want)
	}

	if s.dir != "/repo" {
		t.Errorf("the server is built in %q, want the repository root", s.dir)
	}
}

// The server binary goes where the operator said, and where the release matrix expects when they
// did not.
func TestTheOutputPathIsTheOneTheOperatorNamed(t *testing.T) {
	o, err := parseArgs([]string{"server", "-o", "./rdio-scanner"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if !contains(serverStep(o, "/repo", theVersion(t)).args, "./rdio-scanner") {
		t.Errorf("the server is built with %v, which does not write to the named path", serverStep(o, "/repo", theVersion(t)).args)
	}

	o, err = parseArgs([]string{"server"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	want := filepath.Join("dist", runtime.GOOS+"-"+runtime.GOARCH, "rdio-scanner")
	if o.output != want {
		t.Errorf("the server is built to %q, want the per-platform directory %q", o.output, want)
	}

	if serverStep(o, "/repo", theVersion(t)).result != want {
		t.Errorf("the server step says it leaves behind %q, want the path it writes to", serverStep(o, "/repo", theVersion(t)).result)
	}
}

// A prerelease is an instruction about the version, so a name that does not fit the grammar is a
// command line the tool cannot honour rather than a build that fails halfway through.
func TestAPreReleaseNameThatIsNotAPrereleaseIsRejected(t *testing.T) {
	_, err := parseArgs([]string{"--pre", "beta 1"})
	if err == nil {
		t.Fatal("a prerelease name with a space in it was accepted")
	}

	if !strings.Contains(err.Error(), "beta 1") {
		t.Errorf("the refusal %q does not name the prerelease it rejected", err.Error())
	}

	o, err := parseArgs([]string{"--pre", "beta1"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.pre != "beta1" {
		t.Errorf("the prerelease is %q, want beta1", o.pre)
	}
}

func TestACommandTheToolDoesNotKnowIsRejected(t *testing.T) {
	for _, args := range [][]string{
		{"--not_a_flag"},
		{"not_a_subcommand"},
		{"scanner", "admin"},
		{"-o"},
		{"--pre"},
	} {
		_, err := parseArgs(args)
		if err == nil {
			t.Errorf("%v was accepted, want a refusal", args)
		}
	}
}

// A server built for a target is built for that target, not for the machine the tool happens to be
// running on. The ffmpeg phase already cross-builds for a named target, and the server embeds it: a
// linux-arm64 ffmpeg inside an amd64 binary is a bundle nothing loads, and a server built for the host
// while its ffmpeg was built for arm64 is a release that ships neither.
func TestTheServerIsBuiltForTheTargetTheRunIsBuildingFor(t *testing.T) {
	o := options{target: "linux-arm64", output: defaultOutputPath("linux-arm64")}

	s := serverStep(o, "/repo", theVersion(t))

	if !contains(s.env, "GOOS=linux") {
		t.Errorf("the server is built with env %v, which does not name the target's operating system", s.env)
	}

	if !contains(s.env, "GOARCH=arm64") {
		t.Errorf("the server is built with env %v, which does not name the target's architecture", s.env)
	}

	want := filepath.Join("dist", "linux-arm64", "rdio-scanner")
	if s.result != want {
		t.Errorf("the server for linux-arm64 is written to %q, want %q", s.result, want)
	}

	// A windows target's program has the suffix its operating system puts on a program it runs, which
	// is the same rule the ffmpeg phase already follows.
	o = options{target: "windows-amd64", output: defaultOutputPath("windows-amd64")}

	want = filepath.Join("dist", "windows-amd64", "rdio-scanner.exe")
	if serverStep(o, "/repo", theVersion(t)).result != want {
		t.Errorf("the server for windows-amd64 is written to %q, want %q", serverStep(o, "/repo", theVersion(t)).result, want)
	}

	// A run that named no target builds for the machine it is running on, which is what it did before
	// the release matrix existed.
	o = options{output: defaultOutputPath("")}

	want = filepath.Join("dist", runtime.GOOS+"-"+runtime.GOARCH, "rdio-scanner")
	if serverStep(o, "/repo", theVersion(t)).result != want {
		t.Errorf("a run that named no target writes to %q, want %q", serverStep(o, "/repo", theVersion(t)).result, want)
	}
}

// The compile is the check: the build runs the binaries it compiled, and a drop is run by the compile
// that adopts it. There is no command that proves binaries the build did not compile, and no file a
// later build reads back as somebody else's proof.
func TestTheFfmpegCheckIsNotACommandThisToolAccepts(t *testing.T) {
	for _, args := range [][]string{
		{"ffmpeg-check"},
		{"ffmpeg-check", "macos-arm64"},
		{"ffmpeg-check", "darwin-amd64"},
		{"ffmpeg-check", "freebsd-amd64"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("%v is a command line this tool honours, and it is not one", args)
		}
	}
}

// The usage line is what an operator reads when they type something the tool does not do. It names the
// commands the tool has, and a command that no longer exists in it is not in that line.
func TestTheUsageLineNamesTheCommandsTheToolHas(t *testing.T) {
	out := &bytes.Buffer{}

	if code := (&tool{}).run([]string{"nonsense"}, out); code == 0 {
		t.Fatal("a run naming no command succeeded")
	}

	line := out.String()

	for _, want := range []string{"client", "scanner", "admin", "ffmpeg", "server", "dist", "container", "clean", "version"} {
		if !strings.Contains(line, want) {
			t.Errorf("the usage line %q does not name %s", line, want)
		}
	}

	if strings.Contains(line, "ffmpeg-check") {
		t.Errorf("the usage line offers a command the tool does not have: %q", line)
	}
}

// A person naming the target their machine is types the two words go names that machine by. The release
// word is what the manifest records and what the archive is named for, so the word typed is resolved to
// the release word here, where the run reads it, and every phase after this one works with one name.
func TestTheTargetACommandNamesIsTheReleaseWordTheRunBuildsFor(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		target string
	}{
		{args: []string{"ffmpeg", "darwin-arm64"}, target: "macos-arm64"},
		{args: []string{"ffmpeg", "darwin-amd64"}, target: "macos-amd64"},
		{args: []string{"dist", "darwin-arm64"}, target: "macos-arm64"},
		{args: []string{"ffmpeg", "macos-arm64"}, target: "macos-arm64"},
		{args: []string{"ffmpeg", "linux-amd64"}, target: "linux-amd64"},
		{args: []string{"ffmpeg", "freebsd-amd64"}, target: "freebsd-amd64"},
	} {
		o, err := parseArgs(tc.args)
		if err != nil {
			t.Errorf("%v: the command was refused: %v", tc.args, err)
			continue
		}

		if o.target != tc.target {
			t.Errorf("%v names the target %q, want the release word %q", tc.args, o.target, tc.target)
		}
	}
}

// A release run builds the platform it is standing on: the archive a developer builds to try the build is
// the one their own machine runs, and the eight-target matrix is a release, which is a deliberate act.
// Asking for the matrix and naming one target at the same time is a command line whose two halves ask for
// different numbers of archives.
func TestDistBuildsThePlatformItIsRunOnUnlessTheWholeMatrixIsAskedFor(t *testing.T) {
	o, err := parseArgs([]string{"dist"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.all {
		t.Error("dist alone asks for the whole matrix, and the machine running it is one platform")
	}

	if o.target != "" {
		t.Errorf("dist alone names %s, and the machine running it decides the target", o.target)
	}

	o, err = parseArgs([]string{"dist", "--all"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if !o.all {
		t.Error("dist --all does not ask for the whole matrix")
	}

	o, err = parseArgs([]string{"dist", "linux-arm64"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.all {
		t.Error("dist linux-arm64 asks for the whole matrix, and one target was named")
	}

	if o.target != "linux-arm64" {
		t.Errorf("dist builds %s, want linux-arm64", o.target)
	}

	for _, args := range [][]string{
		{"dist", "--all", "linux-arm64"},
		{"dist", "linux-arm64", "--all"},
		{"--all"},
		{"ffmpeg", "--all"},
		{"server", "--all"},
		{"client", "--all"},
		{"container", "--all"},
		{"clean", "--all"},
		{"version", "--all"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("%v is a command line this tool honours, and it is not one", args)
		}
	}
}

// The ffmpeg is the one thing this tool builds for a platform the operator names. The platform has to
// be the word right after it, so that a target left lying at the end of a command line is refused
// instead of quietly building something else.
func TestTheFFmpegTargetIsTheWordRightAfterIt(t *testing.T) {
	o, err := parseArgs([]string{"ffmpeg", "linux-arm64"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.target != "linux-arm64" {
		t.Errorf("the ffmpeg is compiled for %q, want linux-arm64", o.target)
	}

	o, err = parseArgs([]string{"ffmpeg"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if o.target != "" {
		t.Errorf("the ffmpeg is compiled for %q, want the platform this tool runs on", o.target)
	}

	for _, word := range []string{"linux-arm64", "windows-386", "linux-arm"} {
		if !isTarget(word) {
			t.Errorf("%q is not a platform, and the tool refuses a build for it", word)
		}
	}

	for _, word := range []string{"linux", "-arm64", "linux-arm64-x", "Linux-arm64", "linux--arm64"} {
		if isTarget(word) {
			t.Errorf("%q is accepted as a platform, want it refused", word)
		}
	}
}
