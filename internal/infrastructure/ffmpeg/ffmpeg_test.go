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
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// recordedRecording is one second of a 440 Hz tone, in the 8 kHz 16-bit mono wav the recording
// tools write. A tone rather than silence, because the loudness filters measure the recording
// they are handed and have nothing to measure in silence.
func recordedRecording(t *testing.T) []byte {
	t.Helper()

	const (
		samples   = 8000
		rate      = 8000
		amplitude = 12000
	)

	body := make([]byte, samples*2)

	for i := 0; i < samples; i++ {
		sample := int16(amplitude * math.Sin(2*math.Pi*440*float64(i)/float64(rate)))
		binary.LittleEndian.PutUint16(body[i*2:], uint16(sample))
	}

	wav := bytes.NewBuffer([]byte(nil))

	wav.WriteString("RIFF")
	writeUint32(wav, uint32(36+len(body)))
	wav.WriteString("WAVE")

	wav.WriteString("fmt ")
	writeUint32(wav, 16)
	writeUint16(wav, 1) // PCM
	writeUint16(wav, 1) // one channel
	writeUint32(wav, rate)
	writeUint32(wav, rate*2)
	writeUint16(wav, 2) // one sample frame
	writeUint16(wav, 16)

	wav.WriteString("data")
	writeUint32(wav, uint32(len(body)))
	wav.Write(body)

	return wav.Bytes()
}

func writeUint32(out *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	out.Write(b[:])
}

func writeUint16(out *bytes.Buffer, v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	out.Write(b[:])
}

// installedConverter is the converter a deployment with ffmpeg gets. The conversion tests are
// integration tests over the real binary, and they are skipped — and reported — when the
// deployment does not have it.

// TestAMissingConverterIsReportedOncePerServerRun is what a deployment without ffmpeg sees: one
// warning line in the administrator's log, and then a server that stores every recording as it
// arrived and says nothing more about it.
func TestAMissingConverterIsReportedOncePerServerRun(t *testing.T) {
	converter := &Converter{}

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)

	if err == nil || !strings.Contains(err.Error(), "ffmpeg is not available") {
		t.Fatalf("Convert = %v, want the reason no conversion happened", err)
	}

	if !bytes.Equal(converted.Data, in.Data) || converted.Filename != "call.wav" {
		t.Errorf("the recording is presented as %q, want the original kept when nothing converted it", converted.Filename)
	}

	converted, err = converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)

	if err != nil {
		t.Errorf("the second Convert = %v, want the silence the legacy keeps after the one warning it writes", err)
	}

	if !bytes.Equal(converted.Data, in.Data) {
		t.Errorf("the recording was changed by a converter that is not installed")
	}
}

// TestTheDisabledLevelIsSilentEvenWithoutAConverter pins the order the legacy checks in: a
// deployment that has turned conversion off never hears about the missing converter at all.

// TestTheDisabledLevelIsSilentEvenWithoutAConverter pins the order the legacy checks in: a
// deployment that has turned conversion off never hears about the missing converter at all.
func TestTheDisabledLevelIsSilentEvenWithoutAConverter(t *testing.T) {
	converter := &Converter{}

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	if _, err := converter.Convert(context.Background(), in, domain.AudioConversionDisabled, nil); err != nil {
		t.Errorf("Convert = %v, want nothing said about a converter this deployment does not need", err)
	}
}

// TestTheVersionProbeRecognisesTheReleasesTheFiltersNeed covers the release read out of the first
// line of `ffmpeg -version`, which decides whether the two loudness levels get their filters.

// TestTheVersionProbeRecognisesTheReleasesTheFiltersNeed covers the release read out of the first
// line of `ffmpeg -version`, which decides whether the two loudness levels get their filters.
func TestTheVersionProbeRecognisesTheReleasesTheFiltersNeed(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "the release this machine has",
			line: "ffmpeg version 8.0.1-3ubuntu2 Copyright (c) 2000-2025 the FFmpeg developers",
			want: true,
		},
		{name: "the release that introduced the filter", line: "ffmpeg version 4.3.1 Copyright (c) 2000-2021", want: true},
		{name: "a later release", line: "ffmpeg version 6.1.1-1 Copyright (c) 2000-2023", want: true},
		{name: "a tagged build", line: "ffmpeg version n7.0.1 Copyright (c) 2000-2024", want: true},
		{name: "the release before it", line: "ffmpeg version 4.2.7 Copyright (c) 2000-2020", want: false},
		{
			// The release the legacy reads as having no filters: a version line without a
			// patch number is not a version line the pattern recognizes.
			name: "a release that names no patch number",
			line: "ffmpeg version 4.3 Copyright (c) 2000-2021",
			want: false,
		},
		{
			// And the release a deployment eventually moves to: the version is read as two
			// single digits, so a two-digit major is read as its last digit.
			name: "a release with a two-digit major version",
			line: "ffmpeg version 10.0.1 Copyright (c) 2000-2027",
			want: false,
		},
		{name: "a build that names no version at all", line: "ffmpeg version git-2026-01-01-abcd123", want: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasLoudnorm(c.line); got != c.want {
				t.Errorf("hasLoudnorm(%q) = %v, want %v", c.line, got, c.want)
			}
		})
	}
}

// TestConvertingARecordingProducesTheFormatTheServerStores is the plain level: the wav a
// recording tool wrote becomes the m4a the server serves, named for the recording it came from.

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
