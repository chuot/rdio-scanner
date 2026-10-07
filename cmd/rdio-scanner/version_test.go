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
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestVersionModePrintsVersionAndExitsWithoutError(t *testing.T) {
	var out bytes.Buffer

	code := run([]string{"-version"}, &out)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if got, want := out.String(), Version+"\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestVersionMatchesLegacyServerVersion(t *testing.T) {
	// Drop-in parity (REQ-006): the -version output must match the legacy binary.
	// The legacy value is read from server/version.go so this test fails if the two drift.
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "server", "version.go"))
	if err != nil {
		t.Skipf("legacy server/version.go not available: %v", err)
	}

	match := regexp.MustCompile(`const Version = "([^"]+)"`).FindStringSubmatch(string(src))
	if match == nil {
		t.Fatal("could not read the Version constant from the legacy server")
	}

	if Version != match[1] {
		t.Errorf("Version = %q, legacy server reports %q", Version, match[1])
	}
}

func TestUnknownFlagIsRejected(t *testing.T) {
	var out bytes.Buffer

	if code := run([]string{"-not_a_flag"}, &out); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}
