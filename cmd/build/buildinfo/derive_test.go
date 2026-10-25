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
	"strings"
	"testing"
)

// Derive decides the version a build carries from the declared core
// and what git says about the commit, and produces four shapes.

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

			// whatever a prerelease or a commit adds, the core stays the part
			// a pinned wire contract can pin.
			if version.Core() != c.core {
				t.Errorf("core = %q, want the declared core %q", version.Core(), c.core)
			}
		})
	}
}

// Derive refuses a tag naming a different core than the declared one.
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

// Derive refuses a tag that is not a version instead of treating it as some tag.
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

// Derive refuses a prerelease name that is not a semver prerelease.
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

// Derive refuses an untagged build that does not know its commit.
func TestDeriveRefusesAnUntaggedBuildThatDoesNotKnowItsCommit(t *testing.T) {
	_, err := Derive("8.0.0", "", "", "")
	if err == nil {
		t.Fatal("an untagged build with no commit was accepted, and must be refused")
	}

	if !strings.Contains(err.Error(), "commit") {
		t.Errorf("the refusal %q does not say which fact is missing", err)
	}
}

// the version exposes its core apart from its string, for the pinned wire contract.
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
