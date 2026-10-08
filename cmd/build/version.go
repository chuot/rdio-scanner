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

	"rdio-scanner/internal/buildinfo"
)

// source is what the repository says about the build being asked for: the core it declared in
// VERSION.txt and whether it declared one at all, and what git says about the commit being built.
type source struct {
	core     string
	declared bool
	tag      string
	commit   string
	dirty    bool
}

// readSource asks the repository those questions. Every git call is an argument array, so a version
// string never meets a shell that could read it as an option.
func readSource(repo string) (source, error) {
	path := filepath.Join(repo, "VERSION.txt")

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
	}, nil
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
