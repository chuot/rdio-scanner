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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"rdio-scanner/cmd/build/buildinfo"
)

// source is what the repository says about the build being asked
// for: the core declared in VERSION.txt, if any, and git's account of the commit.
type source struct {
	core     string
	declared bool
	tag      string
	commit   string
	dirty    bool

	// changed is the work git says is not in the commit: the paths a
	// release run names when it refuses an uncommitted tree.
	changed []string
}

// readSource asks the repository those questions; every git call is
// an argument array, so a version string never meets a shell.
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

	// a commit no tag names is a development build, and git says so by
	// failing; a repository that cannot answer at all is already an error.
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

// changedPaths reads the paths out of git's porcelain status: the work
// not in the commit the release is named for. git quotes a renamed
// path by the file it became, and status characters are skipped left.
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

// git runs one git command in one directory and returns its trimmed
// output, keeping git's own complaint: an error that says only exit
// status 128 sends the operator to run git to learn what the tool knew.
func git(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo

	var errout bytes.Buffer

	cmd.Stderr = &errout

	out, err := cmd.Output()
	if err != nil {
		why := strings.TrimSpace(errout.String())
		if why == "" {
			why = err.Error()
		}

		return "", fmt.Errorf("git %s in %s: %s", strings.Join(args, " "), repo, why)
	}

	return strings.TrimSpace(string(out)), nil
}

// explain says where the resolved version came from, because a
// version nobody can explain is the drift this tool replaced.
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
