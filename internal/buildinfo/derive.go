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

package buildinfo

import (
	"fmt"
	"regexp"
	"strings"
)

// Version is the version one build carries. It is built once, by Derive, and it prints itself, so
// nothing downstream assembles a version out of parts and gets the order of them wrong.
//
// The core is the release. The prerelease says the release has not happened yet. The commit says
// which build this was when no tag said it. Only the core is what a pinned wire contract compares
// against, which is why Core is readable on its own instead of being taken back out of a string.
type Version struct {
	core   string
	pre    string
	commit string
}

// Core is the release this version is, without the parts that describe a build of it.
func (v Version) Core() string { return v.core }

// Pre is the prerelease name this build carries, or empty when it carries none.
func (v Version) Pre() string { return v.pre }

// Commit is the short hash of the commit this build was made from, or empty when a tag already said
// which release it is.
func (v Version) Commit() string { return v.commit }

// String is the version in the form a client reads and a build line passes along: a core, an
// optional prerelease, and optional build metadata naming the commit. No part of it holds a space,
// so it survives a command line without quoting.
func (v Version) String() string {
	out := strings.Builder{}
	out.WriteString(v.core)

	if v.pre != "" {
		out.WriteString("-")
		out.WriteString(v.pre)
	}

	if v.commit != "" {
		out.WriteString("+g")
		out.WriteString(v.commit)
	}

	return out.String()
}

// prereleasePattern is one semantic-versioning prerelease: dot-separated identifiers, each a number
// without a leading zero or a run that carries at least one letter or hyphen.
var prereleasePattern = regexp.MustCompile(`^(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*$`)

// tagPattern is a whole version as a tag names it: an optional v, a core, and an optional
// prerelease. Build metadata is not part of it, because a tag names a release and a release does not
// have a build.
var tagPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)

// Derive decides the version one build carries.
//
// core is what the repository declared. tag is the tag naming the commit being built, or empty when
// no tag names it. commit is that commit's short hash. pre is a prerelease the operator asked for.
//
// A tag decides whether the commit appears and may carry a prerelease; it never decides the version.
// A tag that names a different core than the declared one is two sources disagreeing about the same
// commit, and the build stops rather than picking a side.
func Derive(core, tag, commit, pre string) (Version, error) {
	if !corePattern.MatchString(core) {
		return Version{}, fmt.Errorf("%q is not a core version in the form X.Y.Z", core)
	}

	if pre != "" && !prereleasePattern.MatchString(pre) {
		return Version{}, fmt.Errorf("%q is not a prerelease name: use identifiers like beta1 or alpha.3", pre)
	}

	if tag != "" {
		tagCore, tagPre, err := readTag(tag)
		if err != nil {
			return Version{}, err
		}

		if tagCore != core {
			return Version{}, fmt.Errorf("VERSION.txt says %s and the tag %s on HEAD says %s: one of the two has to change", core, tag, tagCore)
		}

		// The operator asking outranks the name a tag happens to carry, and the build log says which
		// one won.
		if pre == "" {
			pre = tagPre
		}

		return Version{core: core, pre: pre}, nil
	}

	if commit == "" {
		return Version{}, fmt.Errorf("HEAD is not tagged, so this build needs its commit to say which build it is")
	}

	return Version{core: core, pre: pre, commit: commit}, nil
}

// readTag takes the core and the prerelease out of a tag name, and refuses a tag that is not a
// version at all: treating "release-2026" as "some tag" would suppress the commit hash for a reason
// nobody chose.
func readTag(tag string) (core string, pre string, err error) {
	matches := tagPattern.FindStringSubmatch(tag)
	if matches == nil {
		return "", "", fmt.Errorf("the tag %s on HEAD is not a version, so it cannot be compared with the version the repository declares", tag)
	}

	core = strings.Join(matches[1:4], ".")
	pre = strings.TrimPrefix(matches[4], "-")

	return core, pre, nil
}
