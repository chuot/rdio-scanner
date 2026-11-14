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
	"path/filepath"
	"strings"
	"testing"

	"rdio-scanner/cmd/build/buildinfo"
)

// These tests read the plan the tool composes for the bundles: what
// npm is told to do, in what order, with what in the environment,
// and the freshness question the tree answers about a bundle; the
// tree helpers live in build_test.go.

// the whole pipeline is the two applications, then the ffmpeg, then
// the server: the server embeds both bundles and the ffmpeg.
func TestTheWholePipelineIsTheApplicationsThenTheFFmpegThenTheServer(t *testing.T) {
	o, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	steps := plan(o, "/repo", theVersion(t), "npm", planState{})

	if len(steps) != 2 {
		t.Fatalf("the whole pipeline plans %d steps, want the two applications", len(steps))
	}

	if steps[0].label != subScanner || steps[1].label != subAdmin {
		t.Errorf("the applications are planned in the order %s, %s, want the scanner then the administration", steps[0].label, steps[1].label)
	}

	// the ffmpeg and the server are not plan steps: the tool runs the
	// ffmpeg itself and composes the server last, both saying they belong.
	if !o.buildsFFmpeg() || !o.buildsServer() {
		t.Errorf("a whole-pipeline run reports it builds the ffmpeg %v and the server %v, want both", o.buildsFFmpeg(), o.buildsServer())
	}

	if serverStep(o, "/repo", theVersion(t)).label != subServer {
		t.Errorf("the server step is labelled %q, want %s", serverStep(o, "/repo", theVersion(t)).label, subServer)
	}
}

// each subcommand plans only the steps it names, and version plans
// nothing because it answers one question and builds nothing.
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
		for _, s := range plan(o, "/repo", theVersion(t), "npm", planState{}) {
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

// the application builds are given the version in the environment,
// and each writes into the bundle directory of its embedding package.
func TestTheApplicationBuildsAreGivenTheVersionInTheEnvironment(t *testing.T) {
	o, err := parseArgs([]string{"client"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	for _, c := range []struct {
		name    string
		version func(*testing.T) buildinfo.Version
		env     string
	}{
		{
			name:    "a development build",
			version: theVersion,
			env:     "RDIO_VERSION=8.0.0+gdb79311",
		},
		{
			name:    "a prerelease of a committed tree",
			version: thePrerelease,
			env:     "RDIO_VERSION=8.0.0-beta1",
		},
	} {
		steps := plan(o, "/repo", c.version(t), "npm", planState{})

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

			if !contains(s.env, c.env) {
				t.Errorf("%s: %s is built with env %v, which does not carry %s", c.name, app, s.env, c.env)
			}

			// each application writes into the bundle directory of the Go package that
			// embeds it, and the step records that destination as its result.
			if s.result != appBundle[app] {
				t.Errorf("%s leaves behind %q, want the bundle of the package that embeds it", app, s.result)
			}
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

	steps := plan(o, "/repo", theVersion(t), "npm", planState{})

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

	if len(plan(o, "/repo", theVersion(t), "npm", planState{})) != 2 {
		t.Errorf("%d steps are planned with the dependencies already installed, want the two applications", len(plan(o, "/repo", theVersion(t), "npm", planState{})))
	}
}

// the plan skips a bundle that is newer than every file it is built
// from: a phase with nothing to do does not run.
func TestAPlanSkipsABundleThatIsAlreadyBuilt(t *testing.T) {
	o, err := parseArgs([]string{"client"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	steps := plan(o, "/repo", theVersion(t), "npm", planState{scannerCurrent: true})

	if len(steps) != 1 {
		t.Fatalf("the plan runs %d steps, want the one application whose bundle is not current", len(steps))
	}

	if steps[0].label != subAdmin {
		t.Errorf("the plan runs %s, want the application whose bundle is not current", steps[0].label)
	}
}

// the plan runs no client step when both bundles are current: the
// bundles are the same for every target, so a second matrix run compiles nothing.
func TestAPlanRunsNoClientStepWhenBothBundlesAreCurrent(t *testing.T) {
	o, err := parseArgs([]string{"dist"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if steps := plan(o, "/repo", theVersion(t), "npm", planState{scannerCurrent: true, adminCurrent: true}); len(steps) != 0 {
		t.Errorf("the plan runs %d steps, want none", len(steps))
	}
}

// the dependencies are looked for only when a client step will run:
// a run that builds no bundle has no use for npm.
func TestARunThatBuildsNoBundleNeedsNoNpm(t *testing.T) {
	o, err := parseArgs([]string{"client"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if needsNpm(o, planState{scannerCurrent: true, adminCurrent: true}) {
		t.Error("a run that builds no bundle asks for npm")
	}

	if !needsNpm(o, planState{}) {
		t.Error("a run that builds both bundles does not ask for npm")
	}
}

// a bundle is current when it is newer than every file it is built
// from, and the built bundle is not one of its own sources.
func TestABundleIsCurrentWhenItIsNewerThanEverySourceItIsBuiltFrom(t *testing.T) {
	repo := t.TempDir()

	sources := []string{
		filepath.Join(presentationDir, "package.json"),
		filepath.Join(presentationDir, "build.mjs"),
		filepath.Join(presentationDir, "scanner", "frontend", "main.js"),
		filepath.Join(presentationDir, "common", "frontend", "application", "app.js"),
		filepath.Join(presentationDir, "admin", "frontend", "main.js"),
		filepath.Join("docs", "openapi.json"),
	}

	for _, path := range sources {
		writeTreeFile(t, repo, path, "the source")
		setMTime(t, repo, path, date(2026, 1, 1))
	}

	// A bundle that is not there is not current.
	if current, err := bundleIsCurrent(repo, subScanner); err != nil || current {
		t.Errorf("a bundle that is not there is reported as current: current %v, err %v", current, err)
	}

	// The bundle is built from the sources, so it is newer than all of them.
	bundle := filepath.Join(presentationDir, "scanner", "frontend", "bundle", "index.html")

	writeTreeFile(t, repo, bundle, "the bundle")
	setMTime(t, repo, bundle, date(2026, 1, 2))

	if current, err := bundleIsCurrent(repo, subScanner); err != nil || !current {
		t.Errorf("a bundle newer than every source it is built from is not reported as current: current %v, err %v", current, err)
	}

	// A source touched after the bundle was built makes it not current.
	setMTime(t, repo, sources[3], date(2026, 1, 3))

	if current, err := bundleIsCurrent(repo, subScanner); err != nil || current {
		t.Errorf("a source newer than the bundle is not noticed: current %v, err %v", current, err)
	}
}

// each application is judged by its own sources and the sources the two
// share, and the administration contract belongs to its bundle alone.
func TestEachApplicationIsJudgedByItsOwnSourcesAndTheSourcesTheTwoShare(t *testing.T) {
	repo := t.TempDir()

	scannerSource := filepath.Join(presentationDir, "scanner", "frontend", "main.js")
	adminSource := filepath.Join(presentationDir, "admin", "frontend", "main.js")
	sharedSource := filepath.Join(presentationDir, "common", "frontend", "application", "app.js")
	contract := filepath.Join("docs", "openapi.json")
	script := filepath.Join(presentationDir, "build.mjs")

	for _, path := range []string{scannerSource, adminSource, sharedSource, contract, script} {
		writeTreeFile(t, repo, path, "the source")
		setMTime(t, repo, path, date(2026, 1, 1))
	}

	for _, app := range []string{subScanner, subAdmin} {
		bundle := filepath.Join(presentationDir, app, "frontend", "bundle", "index.html")

		writeTreeFile(t, repo, bundle, "the bundle")
		setMTime(t, repo, bundle, date(2026, 1, 2))
	}

	// One application's own source, touched after both bundles were built.
	setMTime(t, repo, scannerSource, date(2026, 1, 3))

	if current, err := bundleIsCurrent(repo, subScanner); err != nil || current {
		t.Errorf("a scanner source newer than the scanner bundle is not noticed: current %v, err %v", current, err)
	}

	if current, err := bundleIsCurrent(repo, subAdmin); err != nil || !current {
		t.Errorf("a scanner source made the administration bundle stale, and the administration bundle is not built from it: current %v, err %v", current, err)
	}

	// A source the two applications share, touched after both bundles were built.
	setMTime(t, repo, scannerSource, date(2026, 1, 2))
	setMTime(t, repo, sharedSource, date(2026, 1, 3))

	for _, app := range []string{subScanner, subAdmin} {
		if current, err := bundleIsCurrent(repo, app); err != nil || current {
			t.Errorf("a shared source newer than the %s bundle is not noticed: current %v, err %v", app, current, err)
		}
	}

	// The OpenAPI contract is carried by the administration bundle, and by nothing the scanner builds.
	setMTime(t, repo, sharedSource, date(2026, 1, 2))
	setMTime(t, repo, contract, date(2026, 1, 3))

	if current, err := bundleIsCurrent(repo, subAdmin); err != nil || current {
		t.Errorf("the OpenAPI contract newer than the administration bundle is not noticed: current %v, err %v", current, err)
	}

	if current, err := bundleIsCurrent(repo, subScanner); err != nil || !current {
		t.Errorf("the OpenAPI contract made the scanner bundle stale, and the scanner bundle carries no contract: current %v, err %v", current, err)
	}

	// The build script builds both bundles, so it is a source of both.
	setMTime(t, repo, contract, date(2026, 1, 2))
	setMTime(t, repo, script, date(2026, 1, 3))

	for _, app := range []string{subScanner, subAdmin} {
		if current, err := bundleIsCurrent(repo, app); err != nil || current {
			t.Errorf("the build script newer than the %s bundle is not noticed: current %v, err %v", app, current, err)
		}
	}
}

// the built bundle is output, not a source of either bundle: a bundle
// watching its own nested contents would never be current, because
// the bundle built last is always newer than the bundle built first.
func TestTheBuiltBundleIsNotASourceOfEitherBundle(t *testing.T) {
	repo := t.TempDir()

	sources := []string{
		filepath.Join(presentationDir, "build.mjs"),
		filepath.Join(presentationDir, "package.json"),
		filepath.Join(presentationDir, "scanner", "frontend", "main.js"),
		filepath.Join(presentationDir, "admin", "frontend", "main.js"),
		filepath.Join(presentationDir, "common", "frontend", "application", "app.js"),
		filepath.Join("docs", "openapi.json"),
	}

	for _, path := range sources {
		writeTreeFile(t, repo, path, "the source")
	}

	// The built bundles, with the nested contents a build writes.
	for _, app := range []string{subScanner, subAdmin} {
		writeTreeFile(t, repo, filepath.Join(presentationDir, app, "frontend", "bundle", "index.html"), "the bundle")
		writeTreeFile(t, repo, filepath.Join(presentationDir, app, "frontend", "bundle", "assets", "fonts", "roboto.woff2"), "the bundle")
		writeTreeFile(t, repo, filepath.Join(presentationDir, app, "frontend", "bundle", ".gitkeep"), "")
	}

	bundle := string(filepath.Separator) + "bundle" + string(filepath.Separator)

	// Each application's sources: its own frontend, the shared frontend,
	// the script and manifests that build both bundles, and for the
	// administration application alone the OpenAPI contract it carries.
	want := map[string][]string{
		subScanner: {
			filepath.Join(presentationDir, "build.mjs"),
			filepath.Join(presentationDir, "package.json"),
			filepath.Join(presentationDir, "scanner", "frontend", "main.js"),
			filepath.Join(presentationDir, "common", "frontend", "application", "app.js"),
		},
		subAdmin: {
			filepath.Join(presentationDir, "build.mjs"),
			filepath.Join(presentationDir, "package.json"),
			filepath.Join(presentationDir, "admin", "frontend", "main.js"),
			filepath.Join(presentationDir, "common", "frontend", "application", "app.js"),
			filepath.Join("docs", "openapi.json"),
		},
	}

	for _, app := range []string{subScanner, subAdmin} {
		files, err := clientSources(repo, app)
		if err != nil {
			t.Fatalf("clientSources for %s: %v", app, err)
		}

		got := map[string]bool{}

		for _, path := range files {
			rel, err := filepath.Rel(repo, path)
			if err != nil {
				t.Fatalf("%s is not inside the tree the test made: %v", path, err)
			}

			got[rel] = true

			if strings.Contains(path, bundle) {
				t.Errorf("the %s bundle's own output %s is counted as a source", app, rel)
			}
		}

		if len(got) != len(want[app]) {
			t.Errorf("the %s bundle is built from %v, want the %d files it is built from", app, got, len(want[app]))
		}

		for _, path := range want[app] {
			if !got[path] {
				t.Errorf("the %s bundle does not count %s as a source", app, path)
			}
		}
	}
}
