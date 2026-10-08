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
	"context"
	"os"
	"path/filepath"
	"testing"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// aProgramIsGiven writes a program the test controls and returns its path. It answers the version
// probe and converts by saying who it is, so a test can tell which program actually ran without
// needing the real ffmpeg on this machine.
func aProgramIsGiven(t *testing.T, version string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ffmpeg")

	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"-version\" ]; then\n" +
		"    echo \"" + version + "\"\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\n" +
		"cat > /dev/null\n" +
		"printf 'converted by the program the server was given'\n"

	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("the test cannot write the program it was given: %v", err)
	}

	return path
}

func TestTheConverterRunsTheProgramItWasGiven(t *testing.T) {
	converter := NewAt(aProgramIsGiven(t, "ffmpeg version 9.0.2 Copyright (c) 2000-2026 the FFmpeg developers"))

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err != nil {
		t.Fatalf("the converter refused a recording: %v", err)
	}

	if string(converted.Data) != "converted by the program the server was given" {
		t.Errorf("the recording was converted by something other than the program the converter was given: %q", string(converted.Data))
	}
}

// A server whose bundled program could not be written out, or whose data directory will not hold an
// executable, is a server with no converter: the recording is kept and the reason is said once.
func TestTheConverterReportsAProgramItWasGivenThatIsNotThere(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "ffmpeg-9.0.2")

	converter := NewAt(missing)

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err == nil {
		t.Fatal("a converter given a program that is not there reported no failure")
	}

	if string(converted.Data) != string(in.Data) {
		t.Error("the recording was changed even though there was no program to convert it")
	}

	if _, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil); err != nil {
		t.Errorf("the missing converter was reported a second time: %v", err)
	}
}

// The release the given program reports is what decides whether the loudness filters are used, so a
// bundled ffmpeg and a host ffmpeg are not interchangeable when they are different releases.
func TestTheConverterAsksTheProgramItWasGivenWhichReleaseItIs(t *testing.T) {
	old := NewAt(aProgramIsGiven(t, "ffmpeg version 4.2.1 Copyright (c) 2000-2026 the FFmpeg developers"))

	if old.loudnorm {
		t.Error("a 4.2 release was taken for one that has the loudness filters")
	}

	new := NewAt(aProgramIsGiven(t, "ffmpeg version 9.0.2 Copyright (c) 2000-2026 the FFmpeg developers"))

	if !new.loudnorm {
		t.Error("a 9.0 release was taken for one that does not have the loudness filters")
	}
}
