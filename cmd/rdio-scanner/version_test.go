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
	"encoding/json"
	"os"
	"path/filepath"
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

func TestVersionMatchesThePinnedWireContract(t *testing.T) {
	// This comparison used to read the version out of the legacy server's own version.go. That file
	// is not in this repository, so the test skipped — every day, since the legacy trees were
	// deleted — and a test that always skips asserts nothing.
	//
	// What the legacy server reported is not lost. testdata/golden/payload-ver.json is the payload
	// the legacy server put on the wire for a version request, captured in the configuration every
	// golden was taken in and pinned to the tree the goldens are pinned to. Seven of the pinned
	// captures carry that version, because what a server reports is part of what a client is told.
	// Comparing against the contract is what comparing against the legacy source was for, and it
	// does not reach into a directory that is no longer here.
	//
	// A release moves this test deliberately. The version is the one value in the goldens a release
	// changes, and a release that changes what the server tells clients should be seen doing it.
	if got, want := Version, pinnedWireVersion(t); got != want {
		t.Errorf("Version = %q, the pinned wire contract reports %q", got, want)
	}
}

// pinnedWireVersion reads the version out of the pinned capture of a version response.
func pinnedWireVersion(t *testing.T) string {
	t.Helper()

	path := filepath.Join("..", "..", "testdata", "golden", "payload-ver.json")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is missing: %v\n"+
			"the goldens are pinned bytes and nothing regenerates them; see testdata/golden/manifest.json", path, err)
	}

	var payload struct {
		Version string `json:"version"`
	}

	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatalf("%s does not parse: %v", path, err)
	}

	if payload.Version == "" {
		t.Fatalf("%s names no version", path)
	}

	return payload.Version
}

func TestUnknownFlagIsRejected(t *testing.T) {
	var out bytes.Buffer

	if code := run([]string{"-not_a_flag"}, &out); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}
