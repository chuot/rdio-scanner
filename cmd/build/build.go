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
