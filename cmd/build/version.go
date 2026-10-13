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
	"os/exec"
	"path/filepath"
	"strings"

	"rdio-scanner/cmd/build/buildinfo"
)

// source is what the repository says about the build being asked for: the core it declared in
// VERSION.txt and whether it declared one at all, and what git says about the commit being built.
type source struct {
	core     string
	declared bool
	tag      string
	commit   string
	dirty    bool

	// changed is the work git says is not in the commit: the paths a release run names when it refuses
	// to release a tree that is not committed.
	changed []string
}

// readSource asks the repository those questions. Every git call is an argument array, so a version
// string never meets a shell that could read it as an option.
func readSource(repo string) (source, error) {
	path := filepath.Join(repo, versionFile)

	core, err := buildinfo.ReadCore(path)
	if err != nil {
		return source{}, err
	}

	commit, err := git(repo, "rev-parse", "--short", "HEAD")
	if err != nil {
		return source{}, err
	}

	// A commit no tag names is a development build, and git says so by failing. A repository that
	// cannot answer at all has already been returned as an error.
	tag, _ := git(repo, "describe", "--tags", "--exact-match")

	status, err := git(repo, "status", "--porcelain")
	if err != nil {
		return source{}, err
	}

	_, statErr := os.Stat(path)

	return source{
		core:     core,
		declared: statErr == nil,
		tag:      tag,
		commit:   commit,
		dirty:    status != "",
		changed:  changedPaths(status),
	}, nil
}

// changedPaths reads the paths out of git's porcelain status: the work that is not in the commit the
// release is named for. A rename names the file it became, and git quotes a path that needs quoting.
// The status characters are the first two characters of the line after the leading space git pads them
// with, and the tool reads git's output trimmed, which takes that space off the first line — so the
// status is skipped from the left, not counted from the start.
func changedPaths(status string) []string {
	var paths []string

	for _, line := range strings.Split(status, "\n") {
		rest := strings.TrimLeft(line, " ")
		if len(rest) <= 2 {
			continue
		}

		path := strings.TrimSpace(rest[2:])

		if _, renamed, found := strings.Cut(path, " -> "); found {
			path = renamed
		}

		paths = append(paths, strings.Trim(path, "\""))
	}

	return paths
}

// git runs one git command in one directory and returns its trimmed output.
func git(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}

// explain says where the resolved version came from, because a version nobody can explain is the
// drift this tool replaced with a file.
func explain(src source, pre string) string {
	parts := []string{}

	if src.declared {
		parts = append(parts, "VERSION.txt "+src.core)
	} else {
		parts = append(parts, "VERSION.txt is missing, assuming "+src.core)
	}

	if src.tag != "" {
		parts = append(parts, "HEAD tagged "+src.tag)
	} else {
		parts = append(parts, "HEAD "+src.commit+" is not tagged")
	}

	if pre != "" {
		parts = append(parts, "prerelease requested by --pre")
	}

	return strings.Join(parts, ", ")
}
