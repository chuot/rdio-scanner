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
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"rdio-scanner/internal/buildinfo"
)

// The command line and the plan are the two halves of the tool that can be read without running
// anything: what the operator asked for, and the commands that asking composes. These tests read the
// plan the tool composes — what npm and go are told to do, in what order, with what in the
// environment — and refuse the command lines the tool cannot honour. Nothing here runs a build.

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

// The whole pipeline is the two applications, then the ffmpeg, then the server. The order is the
// point: the server embeds both bundles and the ffmpeg, so a server built first ships without them.
func TestTheWholePipelineIsTheApplicationsThenTheFFmpegThenTheServer(t *testing.T) {
	o, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	steps := plan(o, "/repo", theVersion(t), "npm")

	if len(steps) != 2 {
		t.Fatalf("the whole pipeline plans %d steps, want the two applications", len(steps))
	}

	if steps[0].label != subScanner || steps[1].label != subAdmin {
		t.Errorf("the applications are planned in the order %s, %s, want the scanner then the administration", steps[0].label, steps[1].label)
	}

	// The ffmpeg and the server are not plan steps: the tool runs the ffmpeg itself and composes the
	// server last. Both say they belong to a whole-pipeline run, which is what puts them in the run.
	if !o.buildsFFmpeg() || !o.buildsServer() {
		t.Errorf("a whole-pipeline run reports it builds the ffmpeg %v and the server %v, want both", o.buildsFFmpeg(), o.buildsServer())
	}

	if serverStep(o, "/repo", theVersion(t)).label != subServer {
		t.Errorf("the server step is labelled %q, want %s", serverStep(o, "/repo", theVersion(t)).label, subServer)
	}
}

// Each subcommand plans only the steps it names. The version subcommand plans nothing, because it
// answers one question and builds nothing.
func TestEachSubcommandPlansOnlyTheStepsItNames(t *testing.T) {
	for _, c := range []struct {
		args []string
		want []string
	}{
		{args: []string{"client"}, want: []string{"scanner", "admin"}},
		{args: []string{"scanner"}, want: []string{"scanner"}},
		{args: []string{"admin"}, want: []string{"admin"}},
		{args: []string{"ffmpeg"}, want: []string{}},
		{args: []string{"server"}, want: []string{}},
		{args: []string{"version"}, want: []string{}},
	} {
		o, err := parseArgs(c.args)
		if err != nil {
			t.Fatalf("%v: parseArgs: %v", c.args, err)
		}

		got := []string{}
		for _, s := range plan(o, "/repo", theVersion(t), "npm") {
			got = append(got, s.label)
		}

		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%v plans %v, want %v", c.args, got, c.want)
		}

		if o.buildsFFmpeg() != (len(c.args) > 0 && c.args[0] == "ffmpeg") {
			t.Errorf("%v builds the ffmpeg %v, want %v", c.args, o.buildsFFmpeg(), c.args[0] == "ffmpeg")
		}

		if o.buildsServer() != (len(c.args) > 0 && c.args[0] == "server") {
			t.Errorf("%v builds the server %v, want %v", c.args, o.buildsServer(), c.args[0] == "server")
		}
	}
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

	want := "go build -ldflags -X main.Version=8.0.0+gdb79311 -o " + defaultOutputPath() + " ./cmd/rdio-scanner"
	got := strings.Join(append([]string{s.name}, s.args...), " ")
	if got != want {
		t.Errorf("the server is built with\n  %s\nwant\n  %s", got, want)
	}

	if s.dir != "/repo" {
		t.Errorf("the server is built in %q, want the repository root", s.dir)
	}
}

func TestTheApplicationBuildsAreGivenTheVersionInTheEnvironment(t *testing.T) {
	o, err := parseArgs([]string{"client"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	steps := plan(o, "/repo", theVersion(t), "npm")

	for _, app := range []string{subScanner, subAdmin} {
		s := steps[0]
		if app == subAdmin {
			s = steps[1]
		}

		if s.name != "npm" {
			t.Errorf("%s is built with %q, want npm", app, s.name)
		}

		if strings.Join(s.args, " ") != "run build:"+app {
			t.Errorf("%s is built with %v, want npm run build:%s", app, s.args, app)
		}

		if s.dir != filepath.Join("/repo", presentationDir) {
			t.Errorf("%s is built in %q, want the presentation directory", app, s.dir)
		}

		if !contains(s.env, "RDIO_VERSION=8.0.0+gdb79311") {
			t.Errorf("%s is built with env %v, which does not carry the resolved version", app, s.env)
		}

		// Each application writes into the bundle directory of the Go package that embeds it, and the
		// step says that destination as the result it leaves behind.
		if s.result != appBundle[app] {
			t.Errorf("%s leaves behind %q, want the bundle of the package that embeds it", app, s.result)
		}
	}
}

// Client dependencies are installed once, and only when the tool finds they are not already there.
func TestClientDependenciesAreInstalledOnlyWhenTheyAreNotAlreadyThere(t *testing.T) {
	o, err := parseArgs([]string{"client"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	o.installClients = true

	steps := plan(o, "/repo", theVersion(t), "npm")

	if len(steps) != 3 {
		t.Fatalf("%d steps are planned with the dependencies missing, want the install and the two applications", len(steps))
	}

	if strings.Join(steps[0].args, " ") != "ci --loglevel=error --no-progress" {
		t.Errorf("the install step is %v, want npm ci --loglevel=error --no-progress", steps[0].args)
	}

	if steps[0].dir != filepath.Join("/repo", presentationDir) {
		t.Errorf("the install runs in %q, want the presentation directory", steps[0].dir)
	}

	o.installClients = false

	if len(plan(o, "/repo", theVersion(t), "npm")) != 2 {
		t.Errorf("%d steps are planned with the dependencies already installed, want the two applications", len(plan(o, "/repo", theVersion(t), "npm")))
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
