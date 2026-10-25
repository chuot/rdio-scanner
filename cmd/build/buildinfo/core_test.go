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

// CoreOf is the other half of the rule Derive enforces: only the
// core of a version is what a pinned wire contract can pin.

func TestCoreOfTakesTheReleaseOutOfEveryShapeADerivedVersionTakes(t *testing.T) {
	for _, version := range []string{
		"8.0.0",
		"8.0.0-beta1",
		"8.0.0+gdb79311",
		"8.0.0-beta1+gdb79311",
		"8.0.0-alpha.3",
		"0.0.0+gdb79311",
	} {
		core, err := CoreOf(version)
		if err != nil {
			t.Fatalf("CoreOf(%q): %v", version, err)
		}

		if core != "8.0.0" && core != "0.0.0" {
			t.Errorf("CoreOf(%q) = %q, want the core it starts with", version, core)
		}
	}
}

// the core of a prerelease built from a commit is the release it is a prerelease of.
func TestCoreOfOfAPrereleaseBuiltFromACommitIsTheReleaseItIsAPrereleaseOf(t *testing.T) {
	core, err := CoreOf("8.0.0-beta1+gdb79311")
	if err != nil {
		t.Fatalf("CoreOf: %v", err)
	}

	if core != "8.0.0" {
		t.Errorf("core = %q, want 8.0.0", core)
	}
}

// CoreOf refuses anything without a core at its start.
func TestCoreOfRefusesAnythingWithoutACoreAtItsStart(t *testing.T) {
	for _, version := range []string{"", "dev", "8.0", "v8.0.0", "beta1", "8.0.0.1", "+gdb79311"} {
		_, err := CoreOf(version)
		if err == nil {
			t.Errorf("CoreOf(%q) was accepted, and must be refused", version)
			continue
		}

		if !strings.Contains(err.Error(), version) {
			t.Errorf("the refusal %q does not name the value it refused", err)
		}
	}
}
