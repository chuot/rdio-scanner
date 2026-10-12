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
	"strings"
)

// releaseTarget is one platform the release matrix builds for.
//
// The name is what the archive and its directory are called, and is not always the operating system go
// builds for: this project ships a macos release, and go builds a macos binary for darwin. The two
// words are kept apart so that a release named macos is not built for a target go has never heard of.
type releaseTarget struct {
	name   string
	goos   string
	goarch string
}

// targetWord is the name the ffmpeg phase knows this target by, which is the name the manifest records
// its toolchain under.
func (t releaseTarget) targetWord() string {
	return t.name + "-" + t.goarch
}

// releaseTargets is the matrix: the platforms a release archive is built for.
//
// Which of them carry an ffmpeg is not in this list. The manifest answers that, and the three this
// project ships with the ffmpeg their host installs have no record to answer from. The matrix is a
// product decision about what gets released; the manifest is a build decision about what gets
// compiled, and they are not the same list.
var releaseTargets = []releaseTarget{
	{name: "freebsd", goos: "freebsd", goarch: "amd64"},
	{name: "linux", goos: "linux", goarch: "386"},
	{name: "linux", goos: "linux", goarch: "amd64"},
	{name: "linux", goos: "linux", goarch: "arm"},
	{name: "linux", goos: "linux", goarch: "arm64"},
	{name: "macos", goos: "darwin", goarch: "amd64"},
	{name: "macos", goos: "darwin", goarch: "arm64"},
	{name: "windows", goos: "windows", goarch: "amd64"},
}

// targetFor resolves the word the operator named to the release target it names. A word outside the
// matrix is not a target this project releases, and a release run asked for one is refused.
func targetFor(word string) (releaseTarget, bool) {
	for _, t := range releaseTargets {
		if t.targetWord() == word {
			return t, true
		}
	}

	return releaseTarget{}, false
}

// distTargets is the list of platforms one release run walks, decided before any of it runs: the target
// the operator named, every platform the matrix releases when the whole matrix was asked for, and the
// release target of the machine running the run when neither was.
//
// The machine is asked by the operating system go builds for and the architecture it builds for, which
// are not the words the release is named by: a machine running darwin is a macos release target, and the
// archive it builds carries the macos name.
//
// A machine the project releases nothing for is refused naming the flag that asks for the matrix. A
// release run that built nothing at all is not what a release run is for, and an operator standing on a
// machine outside the matrix is building a release, not trying a build.
func distTargets(o options, goos string, goarch string) ([]releaseTarget, error) {
	if o.target != "" {
		tr, found := targetFor(o.target)

		if !found {
			return nil, fmt.Errorf("%s is not a platform this project releases, so no archive is built for it", o.target)
		}

		return []releaseTarget{tr}, nil
	}

	if o.all {
		return releaseTargets, nil
	}

	for _, tr := range releaseTargets {
		if tr.goos == goos && tr.goarch == goarch {
			return []releaseTarget{tr}, nil
		}
	}

	return nil, fmt.Errorf("this machine is %s-%s, which is no platform this project releases: build every platform this project releases with go run ./cmd/build dist --all", goos, goarch)
}

// serverGoos is the operating system go builds a target word for. A word the matrix has no place for is
// built for the operating system its own name names, which is what a hand-named target asks for.
func serverGoos(word string) string {
	if t, found := targetFor(word); found {
		return t.goos
	}

	goos, _, _ := strings.Cut(word, "-")

	return goos
}
