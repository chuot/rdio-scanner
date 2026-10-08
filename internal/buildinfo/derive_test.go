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
	"strings"
	"testing"
)

// Derive is where the version a build carries is decided, from the core the repository declared and
// what git says about the commit being built. There are exactly four shapes it can produce, and a
// build that produced a fifth would be a version no client can compare against another.

func TestDeriveProducesTheFourShapesAndNoOther(t *testing.T) {
	cases := []struct {
		name   string
		core   string
		tag    string
		commit string
		pre    string
		want   string
	}{
		{
			name:   "a tagged release",
			core:   "8.0.0",
			tag:    "v8.0.0",
			commit: "db79311",
			want:   "8.0.0",
		},
		{
			name:   "a tagged release the operator asked to name a prerelease",
			core:   "8.0.0",
			tag:    "v8.0.0",
			commit: "db79311",
			pre:    "beta1",
			want:   "8.0.0-beta1",
		},
		{
			name:   "a commit tagged as a prerelease",
			core:   "8.0.0",
			tag:    "v8.0.0-beta1",
			commit: "db79311",
			want:   "8.0.0-beta1",
		},
		{
			name:   "an untagged commit",
			core:   "8.0.0",
			commit: "db79311",
			want:   "8.0.0+gdb79311",
		},
		{
			name:   "an untagged commit the operator asked to name a prerelease",
			core:   "8.0.0",
			commit: "db79311",
			pre:    "beta1",
			want:   "8.0.0-beta1+gdb79311",
		},
		{
			name:   "a repository that never declared a version",
			core:   "0.0.0",
			commit: "db79311",
			want:   "0.0.0+gdb79311",
		},
		{
			name:   "a tagged prerelease the operator overrode",
			core:   "8.0.0",
			tag:    "v8.0.0-beta1",
			commit: "db79311",
			pre:    "rc2",
			want:   "8.0.0-rc2",
		},
		{
			name:   "a prerelease with more than one identifier",
			core:   "8.0.0",
			tag:    "v8.0.0-alpha.3",
			commit: "db79311",
			want:   "8.0.0-alpha.3",
		},
		{
			name:   "a tag that does not use the v prefix",
			core:   "8.0.0",
			tag:    "8.0.0",
			commit: "db79311",
			want:   "8.0.0",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			version, err := Derive(c.core, c.tag, c.commit, c.pre)
			if err != nil {
				t.Fatalf("Derive(%q, %q, %q, %q): %v", c.core, c.tag, c.commit, c.pre, err)
			}

			if got := version.String(); got != c.want {
				t.Errorf("version = %q, want %q", got, c.want)
			}

			// Whatever a prerelease or a commit adds, the core stays the part a pinned wire
			// contract can pin.
			if version.Core() != c.core {
				t.Errorf("core = %q, want the declared core %q", version.Core(), c.core)
			}
		})
	}
}

// A tag that names a different core than VERSION.txt is two sources saying two different things about
// the same commit, and the only honest answer is to stop and show both.
func TestDeriveRefusesATagThatDisagreesWithTheDeclaredCore(t *testing.T) {
	_, err := Derive("8.0.0", "v7.9.0", "db79311", "")
	if err == nil {
		t.Fatal("VERSION.txt saying 8.0.0 and HEAD being tagged v7.9.0 was accepted, and must stop the build")
	}

	for _, want := range []string{"8.0.0", "v7.9.0", "7.9.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q", err, want)
		}
	}
}

// A tag that is not a version at all cannot be compared with the declared core, so it is refused
// rather than treated as "some tag", which is what would silently suppress the commit hash.
func TestDeriveRefusesATagThatIsNotAVersion(t *testing.T) {
	for _, tag := range []string{"release-2026", "v8", "v8.0.0+gdb79311", "v8.0.0-beta..1"} {
		_, err := Derive("8.0.0", tag, "db79311", "")
		if err == nil {
			t.Errorf("the tag %q was accepted, and must be refused", tag)
			continue
		}

		if !strings.Contains(err.Error(), tag) {
			t.Errorf("the refusal %q does not name the tag %q", err, tag)
		}
	}
}

// A prerelease is a slot with rules, and a name that does not fit them is a name that would produce
// a version string a client cannot parse.
func TestDeriveRefusesAPreReleaseNameThatIsNotASemverPrerelease(t *testing.T) {
	for _, pre := range []string{"beta 1", "beta..1", "beta.", "beta+", "beta1+gdb79311", "01"} {
		_, err := Derive("8.0.0", "", "db79311", pre)
		if err == nil {
			t.Errorf("the prerelease %q was accepted, and must be refused", pre)
			continue
		}

		if !strings.Contains(err.Error(), pre) {
			t.Errorf("the refusal %q does not name the prerelease %q", err, pre)
		}
	}
}

// An untagged build is identified by the commit it was built from. Without one, the version would
// claim to be a release while saying nothing about which build it was.
func TestDeriveRefusesAnUntaggedBuildThatDoesNotKnowItsCommit(t *testing.T) {
	_, err := Derive("8.0.0", "", "", "")
	if err == nil {
		t.Fatal("an untagged build with no commit was accepted, and must be refused")
	}

	if !strings.Contains(err.Error(), "commit") {
		t.Errorf("the refusal %q does not say which fact is missing", err)
	}
}

// The core a build carries is what the pinned wire contract compares against, so it has to be
// readable on its own and not only by taking the version string apart.
func TestTheVersionCarriesItsPartsApartFromItsString(t *testing.T) {
	version, err := Derive("8.0.0", "", "db79311", "beta1")
	if err != nil {
		t.Fatalf("deriving a version: %v", err)
	}

	if version.Core() != "8.0.0" {
		t.Errorf("core = %q, want 8.0.0", version.Core())
	}

	if version.Pre() != "beta1" {
		t.Errorf("prerelease = %q, want beta1", version.Pre())
	}

	if version.Commit() != "db79311" {
		t.Errorf("commit = %q, want db79311", version.Commit())
	}

	if version.String() != "8.0.0-beta1+gdb79311" {
		t.Errorf("version = %q, want 8.0.0-beta1+gdb79311", version.String())
	}
}
