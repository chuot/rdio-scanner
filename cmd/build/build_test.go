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
	"os"
	"path/filepath"
	"testing"
)

// The tool's four contacts with the machine it runs on: which tree it builds, which programs it can
// run, whether a directory is there, and what one step does when it is executed. The plan is composed
// in commands.go and tested there; what is tested here is the half that touches the machine.

// The tree the tool builds is the one git says. A tree without git is built where the operator stood,
// which is where `go run` stood too, and the tool is run from inside the tree either way.
func TestTheRepositoryRootIsTheTreeGitSays(t *testing.T) {
	repo, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}

	for _, part := range []string{"go.mod", filepath.Join("cmd", "build")} {
		if _, err := os.Stat(filepath.Join(repo, part)); err != nil {
			t.Errorf("the tree git names, %q, does not hold %s", repo, part)
		}
	}
}

// A program the tool has to run is looked up by the name the machine answers, and the name that was
// looked for is named in the failure: the difference between "npm is not installed" and a confusing
// failure.
func TestTheExecutableIsTheOneTheMachineHas(t *testing.T) {
	path, err := resolveExecutable("go")
	if err != nil {
		t.Fatalf("resolveExecutable(go): %v", err)
	}

	if path == "" {
		t.Error("the toolchain running this test is not found by the name it is known by")
	}

	if _, err := resolveExecutable("not-a-program-on-the-path"); err == nil {
		t.Error("a program that is not on the path was accepted, want a failure that names it")
	}
}

// A directory is known by its existence: the client dependencies are installed once, and the tool
// looks at the tree to find out whether they already are.
func TestADirectoryIsKnownByItsExistence(t *testing.T) {
	dir := t.TempDir()

	file := filepath.Join(dir, "a-file")

	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	if !directoryExists(dir) {
		t.Errorf("%q is a directory the tool does not see", dir)
	}

	if directoryExists(file) {
		t.Errorf("%q is a file the tool counts as a directory", file)
	}

	if directoryExists(filepath.Join(dir, "not-there")) {
		t.Errorf("a directory that is not there is seen as installed")
	}
}

// A step runs in its own directory, with the machine's environment in front of its own, and the
// program's own exit is the step's result: a step that says no ends the run, and the steps after it
// do not happen.
func TestAStepRunsInItsOwnDirectoryWithItsOwnExit(t *testing.T) {
	dir := t.TempDir()

	file := filepath.Join(dir, "a-file")

	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	// The same program, asked the same question, in two directories: the one that holds the file and
	// the one that does not. Only the first can answer yes.
	if err := runStep(step{name: "sh", args: []string{"-c", "test -f a-file"}, dir: dir}); err != nil {
		t.Errorf("a step run in %q could not find the file it is standing next to: %v", dir, err)
	}

	if err := runStep(step{name: "sh", args: []string{"-c", "test -f a-file"}, dir: filepath.Dir(dir)}); err == nil {
		t.Error("a step run outside the directory that holds the file answered as if it were inside it")
	}

	// The extra environment reaches the program, and the machine's own environment is still there
	// under it.
	if err := runStep(step{
		name: "sh",
		args: []string{"-c", "test -n \"$STEP_MARK\" -a -n \"$PATH\""},
		dir:  dir,
		env:  []string{"STEP_MARK=here"},
	}); err != nil {
		t.Errorf("the step's own environment did not reach the program with the machine's behind it: %v", err)
	}

	if err := runStep(step{name: "sh", args: []string{"-c", "exit 3"}, dir: dir}); err == nil {
		t.Error("a step whose program says no is reported as having succeeded")
	}
}

// A clean run removes the release output, the client dependencies, and the contents of both bundles,
// and leaves the `.gitkeep` files that keep a plain go build compiling. The placeholders are the reason
// the bundle contents are removed one by one and the bundle directory itself is left standing.
func TestCleanRemovesTheReleaseOutputAndTheBundleContentsAndLeavesThePlaceholders(t *testing.T) {
	repo := t.TempDir()

	writeTreeFile(t, repo, filepath.Join("dist", "linux-amd64", "rdio-scanner"), "the binary")
	writeTreeFile(t, repo, filepath.Join("dist", "rdio-scanner-linux-amd64-v8.0.0.zip"), "the archive")
	writeTreeFile(t, repo, filepath.Join(presentationDir, "node_modules", "esbuild", "package.json"), "the dependency")

	for _, app := range []string{subScanner, subAdmin} {
		bundle := filepath.Join(presentationDir, app, "frontend", "bundle")

		writeTreeFile(t, repo, filepath.Join(bundle, ".gitkeep"), "")
		writeTreeFile(t, repo, filepath.Join(bundle, "index.html"), "the bundle")
		writeTreeFile(t, repo, filepath.Join(bundle, "assets", "app.js"), "the bundle")
	}

	removed, err := cleanTree(repo)
	if err != nil {
		t.Fatalf("cleanTree: %v", err)
	}

	for _, path := range []string{"dist", filepath.Join(presentationDir, "node_modules")} {
		if directoryExists(filepath.Join(repo, path)) {
			t.Errorf("%s survived a clean run", path)
		}
	}

	for _, app := range []string{subScanner, subAdmin} {
		bundle := filepath.Join(repo, presentationDir, app, "frontend", "bundle")

		if _, err := os.Stat(filepath.Join(bundle, ".gitkeep")); err != nil {
			t.Errorf("%s lost the placeholder that keeps a plain go build compiling", bundle)
		}

		for _, name := range []string{"index.html", "assets"} {
			if _, err := os.Stat(filepath.Join(bundle, name)); err == nil {
				t.Errorf("%s kept %s, which a clean run removes", bundle, name)
			}
		}
	}

	// The release output, the dependencies, and the two bundles' contents.
	if len(removed) != 6 {
		t.Errorf("the clean run named %d removals, want 6: %v", len(removed), removed)
	}
}

// writeTreeFile puts a file into a tree the tool is being asked to clean.
func writeTreeFile(t *testing.T, repo string, path string, contents string) {
	t.Helper()

	full := filepath.Join(repo, path)

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(full), err)
	}

	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
