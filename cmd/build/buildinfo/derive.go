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

package buildinfo

import (
	"fmt"
	"regexp"
	"strings"
)

// Version is the version one build carries, assembled once by
// Derive and printed through one method, so nothing downstream
// orders the parts by hand; only the core is what a pinned
// contract compares against.
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

// String is the version in the form a client reads: a core, an
// optional prerelease, and optional build metadata naming the
// commit, with no spaces, so it survives a command line unquoted.
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

// ValidatePre reports whether a name is a prerelease this grammar
// accepts; empty is the absence of one, not a bad one, and it is
// exported so the tool can refuse a bad name before a build starts.
func ValidatePre(pre string) error {
	if pre == "" {
		return nil
	}

	if !prereleasePattern.MatchString(pre) {
		return fmt.Errorf("%q is not a prerelease name: use identifiers like beta1 or alpha.3", pre)
	}

	return nil
}

// tagPattern is a whole version as a tag names it: an optional v, a
// core, and an optional prerelease; build metadata is not part of it.
var tagPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)

// Derive decides the version one build carries from the declared
// core, the tag naming the commit if any, its short hash, and a
// prerelease the operator asked for. A tag may add a prerelease and
// says whether the commit appears, never the version.
func Derive(core, tag, commit, pre string) (Version, error) {
	if !corePattern.MatchString(core) {
		return Version{}, fmt.Errorf("%q is not a core version in the form X.Y.Z", core)
	}

	if err := ValidatePre(pre); err != nil {
		return Version{}, err
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

// readTag takes the core and prerelease out of a tag name, and
// refuses a tag that is not a version at all rather than letting it
// suppress the commit hash.
func readTag(tag string) (core string, pre string, err error) {
	matches := tagPattern.FindStringSubmatch(tag)
	if matches == nil {
		return "", "", fmt.Errorf("the tag %s on HEAD is not a version, so it cannot be compared with the version the repository declares", tag)
	}

	core = strings.Join(matches[1:4], ".")
	pre = strings.TrimPrefix(matches[4], "-")

	return core, pre, nil
}
