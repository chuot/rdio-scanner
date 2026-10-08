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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// VERSION.txt is the one place a core version is written, and a build that guessed one is a build
// that told its clients something the repository never said. These tests are about the reading of
// that file: what it accepts, what it assumes when the file is not there, and what it refuses
// instead of making the best of it.

func writeVersionFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "VERSION.txt")

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	return path
}

func TestReadCoreTakesTheCoreFromTheFileAtTheRepositoryRoot(t *testing.T) {
	core, err := ReadCore(writeVersionFile(t, "8.0.0\n"))
	if err != nil {
		t.Fatalf("reading a valid VERSION.txt: %v", err)
	}

	if core != "8.0.0" {
		t.Errorf("core = %q, want 8.0.0", core)
	}
}

func TestReadCoreIgnoresTheWhitespaceAroundTheCore(t *testing.T) {
	for _, contents := range []string{"8.0.0", "8.0.0\n", "  8.0.0  \n", "\ufeff8.0.0\n"} {
		core, err := ReadCore(writeVersionFile(t, contents))
		if err != nil {
			t.Fatalf("VERSION.txt holding %q: %v", contents, err)
		}

		if core != "8.0.0" {
			t.Errorf("VERSION.txt holding %q reads %q, want 8.0.0", contents, core)
		}
	}
}

// A repository with no VERSION.txt is a repository that has not said what version it is, and the
// owner's rule for that is 0.0.0: a version no release ever had, which is loud enough that nobody
// mistakes the binary for one.
func TestReadCoreAssumesZeroWhenTheFileIsNotThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "VERSION.txt")

	core, err := ReadCore(path)
	if err != nil {
		t.Fatalf("a missing VERSION.txt must resolve to 0.0.0, and returned: %v", err)
	}

	if core != "0.0.0" {
		t.Errorf("core = %q, want 0.0.0", core)
	}
}

// Everything here is a version somebody meant well, and each one is refused rather than repaired.
// A core with a v is a tag name, a core with two parts is an incomplete one, and a core with a
// prerelease or a commit in it is a whole derived version written into the file that only the core
// belongs in.
func TestReadCoreRefusesAnythingThatIsNotABareCore(t *testing.T) {
	for _, contents := range []string{
		"v8.0.0",
		"8.0",
		"8",
		"8.0.0.1",
		"8.0.0-beta1",
		"8.0.0+gdb79311",
		"8.0.0 9.0.0",
		"07.0.0",
		"",
		"\n",
		"release-2026",
	} {
		_, err := ReadCore(writeVersionFile(t, contents))
		if err == nil {
			t.Errorf("VERSION.txt holding %q was accepted, and must be refused", contents)
			continue
		}

		if !strings.Contains(err.Error(), "VERSION.txt") {
			t.Errorf("VERSION.txt holding %q is refused with %q, which does not name the file", contents, err)
		}

		if !strings.Contains(err.Error(), "X.Y.Z") {
			t.Errorf("VERSION.txt holding %q is refused with %q, which does not say what shape was expected", contents, err)
		}
	}
}
