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

package ffmpeg

import (
	"os"
	"path/filepath"
	"testing"
)

// The status view reports whether this deployment measures the length of a recording at all. It is the
// same fact the startup warning states, but the warning is said once per server run and this is read
// again every time the status is asked for.
func TestTheProberReportsWhetherItsProgramRanItsProbe(t *testing.T) {
	if available := NewProberAt(aProberProgramIsGiven(t), t.TempDir()).Available(); !available {
		t.Error("Available() = false for a program that answered the version probe, want true: this deployment measures lengths")
	}

	missing := NewProberAt(filepath.Join(t.TempDir(), "ffprobe-9.0.2"), t.TempDir())

	if available := missing.Available(); available {
		t.Error("Available() = true for a program that is not there, want false: no audio length will be measured")
	}
}

// aProberProgramIsGiven writes a program that answers the version probe and reports its path. The test
// is about whether the probe ran, and not about what the host's ffprobe happens to say.
func aProberProgramIsGiven(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ffprobe")

	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"-version\" ]; then\n" +
		"    echo \"ffprobe version 9.0.2\"\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\n" +
		"exit 0\n"

	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("the test cannot write the program it was given: %v", err)
	}

	return path
}
