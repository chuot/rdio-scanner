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

// The webapps phase is everything the tool knows about the two web
// applications: where they live, where their bundles are written,
// what a bundle is built from, whether one is current, which
// applications a run builds, and the npm commands that build them.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"rdio-scanner/cmd/build/buildinfo"
)

// presentationDir is where the two applications live and where their
// dependencies are installed; each application writes into the
// bundle directory of the Go package that embeds it.
const presentationDir = "internal/presentation"

// appBundle is the bundle directory of the Go package that embeds
// each application: the destination is a fact about where the
// application lives, so the build never sends an output directory.
var appBundle = map[string]string{
	subScanner: "internal/presentation/scanner/frontend/bundle",
	subAdmin:   "internal/presentation/admin/frontend/bundle",
}

// bundleOutput is the file a built bundle is judged by, written into
// the bundle directory of the Go package that embeds it.
func bundleOutput(repo string, app string) string {
	return filepath.Join(repo, presentationDir, app, "frontend", "bundle", "index.html")
}

// clientSources is every file the bundle of one application is built
// from: the build script and manifests, that application's frontend, the
// shared sources, and for administration alone the OpenAPI contract.
func clientSources(repo string, app string) ([]string, error) {
	var files []string

	root := filepath.Join(repo, presentationDir)

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("the build cannot read %s: %w", root, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		if strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".mjs") {
			files = append(files, filepath.Join(root, name))
		}
	}

	for _, source := range []string{app, "common"} {
		if err := walkFiles(filepath.Join(root, source, "frontend"), func(path string) {
			if isBundleContent(path) {
				return
			}

			files = append(files, path)
		}); err != nil {
			return nil, err
		}
	}

	// the administration bundle carries the OpenAPI contract as a static
	// file its OpenAPI section renders from, so a contract edit must
	// make it stale; the scanner bundle carries no contract to go stale.
	if app == subAdmin {
		files = append(files, filepath.Join(repo, "docs", "openapi.json"))
	}

	return files, nil
}

// isBundleContent reports whether a file is inside a built bundle:
// the bundle and everything under it are output, not sources. A
// rule watching its own output would rebuild on every run.
func isBundleContent(path string) bool {
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == "bundle" {
			return true
		}
	}

	return false
}

// bundleIsCurrent reports whether a built bundle is newer than every
// file it is built from; a bundle that is not there is not current.
func bundleIsCurrent(repo string, app string) (bool, error) {
	return newerThan(repo, bundleOutput(repo, app), func(string) ([]string, error) {
		return clientSources(repo, app)
	})
}

// clientSteps is the pair of applications a run builds: the ones the
// command line named, and the ones whose bundle is not current.
func clientSteps(o options, state planState) []string {
	var apps []string

	for _, app := range []string{subScanner, subAdmin} {
		// A run naming one application builds that one; a run naming the whole
		// pipeline, both applications, the release matrix, or the container
		// image builds both, because the bundles are the same for every target.
		if o.sub != "" && o.sub != subClient && o.sub != subDist && o.sub != subContainer && o.sub != app {
			continue
		}

		if app == subScanner && state.scannerCurrent {
			continue
		}

		if app == subAdmin && state.adminCurrent {
			continue
		}

		apps = append(apps, app)
	}

	return apps
}

// needsNpm is decided by the plan: the dependencies are looked for
// only when a client step is going to run, and a run that builds no
// bundle has no use for npm.
func needsNpm(o options, state planState) bool {
	return len(clientSteps(o, state)) > 0
}

// runWebapps runs the webapps phase: it looks for npm when a bundle
// is going to be built, decides whether the dependencies have to be
// installed, and runs the steps that build the bundles.
func (t tool) runWebapps(o options, version buildinfo.Version, stdout io.Writer, state planState) error {
	npmName := ""

	if needsNpm(o, state) {
		npm, err := t.resolve("npm")
		if err != nil {
			return fmt.Errorf("building the applications needs npm: %w", err)
		}

		npmName = npm

		o.installClients = !t.installed(filepath.Join(t.repo, presentationDir, "node_modules"))
	}

	for _, s := range plan(o, t.repo, version, npmName, state) {
		if err := t.execute(s); err != nil {
			return fmt.Errorf("%s: %w", s.label, err)
		}

		fmt.Fprintf(stdout, "%-7s %s\n", s.label, s.result)
	}

	return nil
}

// plan composes the commands the options asked for, in the order
// they have to happen: the scanner application, then the
// administration application, because the server embeds both bundles.
func plan(o options, repo string, version buildinfo.Version, npmName string, state planState) []step {
	steps := []step{}

	presentation := filepath.Join(repo, presentationDir)

	if o.installClients {
		steps = append(steps, step{
			label:  "deps",
			result: "installing the client dependencies",
			dir:    presentation,
			name:   npmName,
			args:   []string{"ci", "--loglevel=error", "--no-progress"},
		})
	}

	for _, app := range clientSteps(o, state) {
		steps = append(steps, step{
			label:  app,
			result: appBundle[app],
			dir:    presentation,
			name:   npmName,
			args:   []string{"run", "build:" + app},
			// The one way the version reaches a bundle. No source file is rewritten to carry it.
			env: []string{"RDIO_VERSION=" + version.String()},
		})
	}

	return steps
}
