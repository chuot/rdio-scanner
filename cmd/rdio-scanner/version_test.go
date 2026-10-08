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

	"rdio-scanner/internal/buildinfo"
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
	// golden was taken in and pinned to the tree the goldens are pinned to. Five of the pinned
	// captures carry that version, because what a server reports is part of what a client is told.
	// Comparing against the contract is what comparing against the legacy source was for, and it
	// does not reach into a directory that is no longer here.
	//
	// The comparison is between cores. A build carries a prerelease and a commit hash to say which
	// build of a release it is, and neither is a different release: a development build satisfies the
	// same contract a tagged release does, and only a different core breaks it.
	resolved, err := buildinfo.CoreOf(Version)
	if err != nil {
		t.Fatalf("Version = %q is not a version a client can compare: %v", Version, err)
	}

	if pinned := pinnedWireCore(t); resolved != pinned {
		t.Errorf("this build reports the core %q, and the pinned wire contract reports %q", resolved, pinned)
	}
}

// TestThePinnedWireContractComparesCoresAndNotWholeVersions is that contract in both directions, so
// that the comparison is not merely observed passing but seen refusing.
func TestThePinnedWireContractComparesCoresAndNotWholeVersions(t *testing.T) {
	pinned := pinnedWireCore(t)

	if pinned != "8.0.0" {
		t.Fatalf("the pinned capture carries the core %q, and the cases below name 8.0.0: they move with the capture", pinned)
	}

	cases := []struct {
		version string
		want    bool
	}{
		{version: "8.0.0", want: true},
		{version: "8.0.0-beta1+gdb79311", want: true},
		{version: "8.0.0+gdb79311", want: true},
		{version: "8.0.0-alpha.3", want: true},

		{version: "9.0.0", want: false},
		{version: "8.1.0-beta1", want: false},
		{version: "7.9.0", want: false},
		{version: "dev", want: false},
	}

	for _, c := range cases {
		core, err := buildinfo.CoreOf(c.version)

		satisfies := err == nil && core == pinned

		if satisfies != c.want {
			t.Errorf("%s satisfies the pinned core %q = %v, want %v (CoreOf: %v)", c.version, pinned, satisfies, c.want, err)
		}
	}
}

// pinnedWireCore reads the core of the version out of the pinned capture of a version response.
func pinnedWireCore(t *testing.T) string {
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

	core, err := buildinfo.CoreOf(payload.Version)
	if err != nil {
		t.Fatalf("%s pins %v", path, err)
	}

	return core
}

func TestUnknownFlagIsRejected(t *testing.T) {
	var out bytes.Buffer

	if code := run([]string{"-not_a_flag"}, &out); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}
