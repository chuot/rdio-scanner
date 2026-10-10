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
	"strings"
	"testing"
)

// The webapps phase is the two applications and the npm commands that build their bundles. These
// tests read the plan the tool composes for the bundles — what npm is told to do, in what order,
// with what in the environment — and the freshness question the tree answers about a bundle. The
// tree helpers writeTreeFile, setMTime, and date live in build_test.go.

// The whole pipeline is the two applications, then the ffmpeg, then the server. The order is the
// point: the server embeds both bundles and the ffmpeg, so a server built first ships without them.
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

// The application builds are given the version in the environment, and each writes into the bundle
// directory of the Go package that embeds it.
func TestTheApplicationBuildsAreGivenTheVersionInTheEnvironment(t *testing.T) {
	o, err := parseArgs([]string{"client"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	steps := plan(o, "/repo", theVersion(t), "npm", planState{})

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

// A bundle that is newer than every file it is built from is not rebuilt: a phase with nothing to do
// does not run. The plan runs the one application whose bundle is not current.
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

// A release run whose bundles are current runs no client step. The bundles are the same for every
// target, so a second matrix run over the same sources compiles nothing.
func TestAPlanRunsNoClientStepWhenBothBundlesAreCurrent(t *testing.T) {
	o, err := parseArgs([]string{"dist"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if steps := plan(o, "/repo", theVersion(t), "npm", planState{scannerCurrent: true, adminCurrent: true}); len(steps) != 0 {
		t.Errorf("the plan runs %d steps, want none", len(steps))
	}
}

// The dependencies are looked for only when a client step is going to run. A run that builds no bundle
// has no use for npm, and installing esbuild into a tree that will not build is work nobody asked for.
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

// A built bundle is current when it is newer than every file it is built from. The built bundle is not
// one of its own sources: a rule that watched its own output would rebuild on every run.
func TestABundleIsCurrentWhenItIsNewerThanEverySourceItIsBuiltFrom(t *testing.T) {
	repo := t.TempDir()

	sources := []string{
		filepath.Join(presentationDir, "package.json"),
		filepath.Join(presentationDir, "build.mjs"),
		filepath.Join(presentationDir, "scanner", "frontend", "main.js"),
		filepath.Join(presentationDir, "common", "frontend", "application", "app.js"),
		filepath.Join(presentationDir, "admin", "frontend", "main.js"),
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

// The built bundle is the output of the client build, not a source. Its nested contents are part of the
// output: a bundle that watched them would rebuild on every run, because the bundle built last is newer
// than the bundle built first, and the first is then never current.
func TestTheBuiltBundleIsNotASourceOfEitherBundle(t *testing.T) {
	repo := t.TempDir()

	sources := []string{
		filepath.Join(presentationDir, "build.mjs"),
		filepath.Join(presentationDir, "package.json"),
		filepath.Join(presentationDir, "scanner", "frontend", "main.js"),
		filepath.Join(presentationDir, "admin", "frontend", "main.js"),
		filepath.Join(presentationDir, "common", "frontend", "application", "app.js"),
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

	files, err := clientSources(repo)
	if err != nil {
		t.Fatalf("clientSources: %v", err)
	}

	bundle := string(filepath.Separator) + "bundle" + string(filepath.Separator)

	for _, path := range files {
		if strings.Contains(path, bundle) {
			t.Errorf("the built bundle's %s is counted as a source", path)
		}
	}

	if len(files) != len(sources) {
		t.Errorf("the client build has %d sources, want the %d files it is built from", len(files), len(sources))
	}
}
