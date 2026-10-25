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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// the tests pin how VERSION.txt is read: what it accepts, what is
// assumed when the file is absent, and what is refused.

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

// ReadCore assumes 0.0.0 when the file is not there.
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

// ReadCore refuses cores that are not bare, whether tagged,
// partial, or already a whole derived version.
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
