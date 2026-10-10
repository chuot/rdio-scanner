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
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// repositoryRoot is the directory the tool builds. git knows it, and a tree without git is built
// where the operator stood, which is where go run stood too.
func repositoryRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}

	return os.Getwd()
}

// resolveExecutable finds a program the tool has to run. npm is npm.cmd on Windows, and naming the
// name that was looked for is the difference between "npm is not installed" and a confusing failure.
func resolveExecutable(name string) (string, error) {
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath(name + ".cmd"); err == nil {
			return path, nil
		}
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}

	return path, nil
}

// directoryExists is how the tool knows the client dependencies are already installed.
func directoryExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

// writeFile puts a file the release produces into the tree, making the directory it goes in. The
// document is written into the same directory as the binary it travels with, which a run for a target
// that has never been built before has to make.
func writeFile(path string, body []byte) error {
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("the release cannot prepare %s: %w", dir, err)
	}

	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("the release cannot write %s: %w", path, err)
	}

	return nil
}

// cleanTree removes what a clean run removes: the release output, the client dependencies, and the
// contents of both bundles. The `.gitkeep` file in each bundle is left standing, because it is the
// placeholder that keeps a plain go build compiling: the bundle directory has to exist for the Go
// package that embeds it, and the contents are what a build puts there.
//
// The run names what it removed, so a tree that was already clean says so by naming nothing.
func cleanTree(repo string) ([]string, error) {
	targets := []string{filepath.Join(repo, "dist"), filepath.Join(repo, presentationDir, "node_modules")}

	for _, app := range []string{subScanner, subAdmin} {
		bundle := filepath.Join(repo, presentationDir, app, "frontend", "bundle")

		entries, err := os.ReadDir(bundle)
		if err != nil {
			// A tree that has never been built has no bundle contents to remove.
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("the clean run cannot read %s: %w", bundle, err)
		}

		for _, entry := range entries {
			if entry.Name() == ".gitkeep" {
				continue
			}

			targets = append(targets, filepath.Join(bundle, entry.Name()))
		}
	}

	var removed []string

	for _, path := range targets {
		if _, err := os.Lstat(path); err != nil {
			continue
		}

		if err := os.RemoveAll(path); err != nil {
			return nil, fmt.Errorf("the clean run cannot remove %s: %w", path, err)
		}

		removed = append(removed, path)
	}

	return removed, nil
}

// bundleOutput is the file a built bundle is judged by. The build writes it into the bundle directory of
// the Go package that embeds it.
func bundleOutput(repo string, app string) string {
	return filepath.Join(repo, presentationDir, app, "frontend", "bundle", "index.html")
}

// clientSources is every file a bundle is built from: the build script and its lock file, which build
// both bundles, and the frontend sources of the application the bundle belongs to and the sources the two
// applications share. The built bundle is not one of its own sources: a rule that watched its own output
// would rebuild on every run.
func clientSources(repo string) ([]string, error) {
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

	for _, app := range []string{subScanner, subAdmin, "common"} {
		if err := walkFiles(filepath.Join(root, app, "frontend"), func(path string) {
			if isBundleContent(path) {
				return
			}

			files = append(files, path)
		}); err != nil {
			return nil, err
		}
	}

	return files, nil
}

// isBundleContent reports whether a file is inside a built bundle. The bundle and everything a build
// writes under it are the client build's output, not its sources: a rule that watched its own output
// would rebuild on every run, and a bundle that watched only the files sitting directly in the bundle
// directory would watch the bundle built last as a source of the bundle built first.
func isBundleContent(path string) bool {
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == "bundle" {
			return true
		}
	}

	return false
}

// serverSources is every file the server binary is built from: the Go files, and the contents of both
// bundles, which the server embeds. A binary that is newer than every Go file and older than the bundle
// it embeds is a binary that ships the bundle it was built before.
func serverSources(repo string) ([]string, error) {
	var files []string

	for _, root := range []string{"cmd", "internal"} {
		if err := walkFiles(filepath.Join(repo, root), func(path string) {
			if strings.HasSuffix(path, ".go") {
				files = append(files, path)
			}
		}); err != nil {
			return nil, err
		}
	}

	for _, app := range []string{subScanner, subAdmin} {
		bundle := filepath.Join(repo, appBundle[app])

		if err := walkFiles(bundle, func(path string) {
			if filepath.Base(path) != ".gitkeep" {
				files = append(files, path)
			}
		}); err != nil {
			return nil, err
		}
	}

	return files, nil
}

// walkFiles calls visit for every file under a directory. A directory that is not there has no files,
// which is what a tree that has never been built is.
func walkFiles(root string, visit func(path string)) error {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}

			return fmt.Errorf("the build cannot read %s: %w", path, err)
		}

		if !entry.IsDir() {
			visit(path)
		}

		return nil
	})

	return err
}

// bundleIsCurrent reports whether a built bundle is newer than every file it is built from. A bundle
// that is not there is not current.
func bundleIsCurrent(repo string, app string) (bool, error) {
	return newerThan(repo, bundleOutput(repo, app), clientSources)
}

// serverIsCurrent reports whether the server binary is newer than every file it is built from. A binary
// that is not there is not current.
func serverIsCurrent(repo string, output string) (bool, error) {
	return newerThan(repo, filepath.Join(repo, output), serverSources)
}

// newerThan is the one question both checks ask: is the thing that was built newer than every file it
// was built from.
func newerThan(repo string, built string, sources func(string) ([]string, error)) (bool, error) {
	builtInfo, err := os.Stat(built)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("the build cannot read %s: %w", built, err)
	}

	paths, err := sources(repo)
	if err != nil {
		return false, err
	}

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return false, fmt.Errorf("the build cannot read %s: %w", path, err)
		}

		if info.ModTime().After(builtInfo.ModTime()) {
			return false, nil
		}
	}

	return true, nil
}

// buildState is what the tree says about what has already been built. The checks are made only for the
// phases the run is going to run: a run asked to build one application is not asked what it thinks about
// the server binary.
func buildState(repo string, o options) (planState, error) {
	state := planState{}

	if o.buildsClients() {
		current, err := bundleIsCurrent(repo, subScanner)
		if err != nil {
			return state, err
		}

		state.scannerCurrent = current

		if current, err := bundleIsCurrent(repo, subAdmin); err != nil {
			return state, err
		} else {
			state.adminCurrent = current
		}
	}

	if o.buildsServerBinary() {
		current, err := serverIsCurrent(repo, o.output)
		if err != nil {
			return state, err
		}

		state.serverCurrent = current
	}

	return state, nil
}

// runStep runs one step, with its own directory and its extra environment, and lets whatever the
// program says reach the operator.
func runStep(s step) error {
	cmd := exec.Command(s.name, s.args...)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(), s.env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
