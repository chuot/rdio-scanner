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

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"rdio-scanner/cmd/build/buildinfo"
)

// captureVersion is the version the server that produced the recorded
const captureVersion = "8.0.0"

func TestMain(m *testing.M) {
	Version = captureVersion

	os.Exit(m.Run())
}

func TestVersionModePrintsVersionAndExitsWithoutError(t *testing.T) {
	var out bytes.Buffer

	code := run([]string{"-version"}, &out, noBundlesUnbuilt)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if got, want := out.String(), Version+"\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestVersionMatchesThePinnedWireContract(t *testing.T) {
	// the pinned wire contract compares the core the build system resolves from
	// VERSION.txt against the version inside the captured payload: a fallback is
	// not a release a contract can pin, and moving VERSION.txt without moving the
	// see the version-pinned-wire-contract maintainer note.
	resolved, err := buildinfo.ReadCore(filepath.Join("..", "..", "VERSION.txt"))
	if err != nil {
		t.Fatalf("VERSION.txt at the repository root: %v", err)
	}

	if pinned := pinnedWireCore(t); resolved != pinned {
		t.Errorf("VERSION.txt resolves the core %q, and the pinned wire contract reports %q: a release moves both, or neither", resolved, pinned)
	}
}

// the pinned wire contract test proves the comparison in both directions, so
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

// capturedVersionAnswer is the answer a legacy server put on the wire for a
// version request, recorded byte for byte inside the test that compares it
// see the legacy-captured-version-answer maintainer note.
const capturedVersionAnswer = `{"branding":"Capture Dispatch","email":"dispatch@example.com","version":"8.0.0"}`

// pinnedWireCore reads the core of the version out of the recorded answer.
func pinnedWireCore(t *testing.T) string {
	t.Helper()

	var payload struct {
		Version string `json:"version"`
	}

	if err := json.Unmarshal([]byte(capturedVersionAnswer), &payload); err != nil {
		t.Fatalf("the recorded version answer does not parse: %v", err)
	}

	if payload.Version == "" {
		t.Fatalf("the recorded version answer names no version")
	}

	core, err := buildinfo.CoreOf(payload.Version)
	if err != nil {
		t.Fatalf("the recorded version answer pins %v", err)
	}

	return core
}

func TestUnknownFlagIsRejected(t *testing.T) {
	var out bytes.Buffer

	if code := run([]string{"-not_a_flag"}, &out, noBundlesUnbuilt); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}
