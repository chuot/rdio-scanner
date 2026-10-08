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
// them. These tests assert the plan it composes rather than running a build: what npm and go are
// asked to do, in what order, with what in the environment, and what the process exits with when a
// step says no.

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
	// applications existed would embed a stale one.
	want := []string{"scanner", "admin", "server"}
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
func TestTheServerBuildCarriesTheResolvedVersionInTheLinkerFlags(t *testing.T) {
	tl, _, ran := fakeRun(development, "")

	if code := tl.run([]string{"server"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	server := (*ran)[0]

	want := "go build -ldflags -X main.Version=8.0.0+gdb79311 -o " + defaultOutputPath() + " ./cmd/rdio-scanner"
	got := strings.Join(append([]string{server.name}, server.args...), " ")
	if got != want {
		t.Errorf("the server is built with\n  %s\nwant\n  %s", got, want)
	}

	if server.dir != "/repo" {
		t.Errorf("the server is built in %q, want the repository root", server.dir)
	}
}

func TestTheApplicationBuildsAreGivenTheVersionInTheEnvironment(t *testing.T) {
	tl, _, ran := fakeRun(development, "")

	if code := tl.run([]string{"client"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	for _, app := range []string{"scanner", "admin"} {
		step := (*ran)[0]
		if app == "admin" {
			step = (*ran)[1]
		}

		if step.name != "npm" {
			t.Errorf("%s is built with %q, want npm", app, step.name)
		}

		if strings.Join(step.args, " ") != "run build:"+app {
			t.Errorf("%s is built with %v, want npm run build:%s", app, step.args, app)
		}

		if step.dir != "/repo/internal/presentation/clients" {
			t.Errorf("%s is built in %q, want the client directory", app, step.dir)
		}

		if !contains(step.env, "RDIO_VERSION=8.0.0+gdb79311") {
			t.Errorf("%s is built with env %v, which does not carry the resolved version", app, step.env)
		}
	}
}

// Client dependencies are installed once, and only when they are not already there.
func TestClientDependenciesAreInstalledOnlyWhenTheyAreNotAlreadyThere(t *testing.T) {
	tl, _, ran := fakeRun(released, "")
	tl.installed = func(string) bool { return false }

	if code := tl.run([]string{"client"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	install := (*ran)[0]
	if strings.Join(install.args, " ") != "ci --loglevel=error --no-progress" {
		t.Errorf("the install step is %v, want npm ci --loglevel=error --no-progress", install.args)
	}

	if len(*ran) != 3 {
		t.Errorf("%d steps ran, want the install and the two applications", len(*ran))
	}
}

func TestTheOutputPathIsTheOneTheOperatorNamed(t *testing.T) {
	tl, _, ran := fakeRun(released, "")

	if code := tl.run([]string{"server", "-o", "./rdio-scanner"}, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if !contains((*ran)[0].args, "./rdio-scanner") {
		t.Errorf("the server is built with %v, which does not write to the named path", (*ran)[0].args)
	}
}

// A prerelease is an instruction about the version, so a name that does not fit the grammar is a
// command line the tool cannot honour rather than a build that fails halfway through.
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

func TestACommandTheToolDoesNotKnowIsRejected(t *testing.T) {
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

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}

	return false
}
