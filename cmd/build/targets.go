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
	"fmt"
	"strings"
)

// releaseTarget is one platform the release matrix builds for.
// The name is what the archive and its directory are called, and go
// builds this project's macos release for the operating system word
// darwin: the two are kept apart so a macos release is not misnamed.
type releaseTarget struct {
	name   string
	goos   string
	goarch string
}

// targetWord is the name the ffmpeg phase knows this target by,
// which is the name the manifest records its toolchain under.
func (t releaseTarget) targetWord() string {
	return t.name + "-" + t.goarch
}

// releaseTargets is the matrix: the platforms a release archive is built for.
// Which targets carry an ffmpeg, and how those binaries came to be, is
// not in this list: the manifest answers that.
// see the targets-and-platforms maintainer note.
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

// targetFor resolves the word the operator named to the release
// target it names; a word outside the matrix is refused.
func targetFor(word string) (releaseTarget, bool) {
	// the same machine, named two ways: go builds a macOS binary for
	// darwin, the release names it macos, and a person may type either.
	if goos, goarch, found := strings.Cut(word, "-"); found && goos == "darwin" {
		word = "macos-" + goarch
	}

	for _, t := range releaseTargets {
		if t.targetWord() == word {
			return t, true
		}
	}

	return releaseTarget{}, false
}

// theReleaseWordTheRunBuildsFor is the release word a target word
// names: the manifest, the archive name, and the guide all use the
// release word, so every phase downstream works with one name.
// see the targets-and-platforms maintainer note.
func theReleaseWordTheRunBuildsFor(word string) string {
	tr, found := targetFor(word)
	if !found {
		return word
	}

	return tr.targetWord()
}

// releaseTargetWordFor is the release target a machine belongs to,
// named the way the release names it: go builds a macOS binary for
// darwin and the release is macos, so the matrix maps one word.
// see the targets-and-platforms maintainer note.
func releaseTargetWordFor(goos string, goarch string) string {
	for _, tr := range releaseTargets {
		if tr.goos == goos && tr.goarch == goarch {
			return tr.targetWord()
		}
	}

	return goos + "-" + goarch
}

// distTargets is the list of platforms one release run walks, decided
// before any of it runs: the named target, the whole matrix when it
// was asked for, or the release target of the machine running it.
// see the targets-and-platforms maintainer note.
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

	word := releaseTargetWordFor(goos, goarch)

	if tr, found := targetFor(word); found {
		return []releaseTarget{tr}, nil
	}

	return nil, fmt.Errorf("this machine is %s-%s, which is no platform this project releases: build every platform this project releases with go run ./cmd/build dist --all", goos, goarch)
}

// serverGoos is the operating system go builds a target word for; a
// word the matrix has no place for is built for its own name.
func serverGoos(word string) string {
	if t, found := targetFor(word); found {
		return t.goos
	}

	goos, _, _ := strings.Cut(word, "-")

	return goos
}
