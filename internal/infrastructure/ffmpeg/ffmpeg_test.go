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
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
func installedConverter(t *testing.T) *Converter {
	t.Helper()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed on this machine: the conversion tests are skipped")
	}

	converter := New()

	if !converter.available {
		t.Fatal("ffmpeg is on the path but the converter reports it is not installed")
	}

	return converter
}

// writeConverted puts the converted recording in a file, because the container and its tags are
// read back with the same tools the server's audio endpoints hand them to.
func writeConverted(t *testing.T, converted ports.AudioFile) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), converted.Filename)

	if err := os.WriteFile(path, converted.Data, 0o644); err != nil {
		t.Fatalf("writing the converted recording: %v", err)
	}

	return path
}

// probe asks ffprobe one question about a converted recording.
func probe(t *testing.T, path string, args ...string) string {
	t.Helper()

	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed on this machine: the conversion tests are skipped")
	}

	command := exec.Command("ffprobe", append(append([]string{"-v", "error"}, args...), path)...)

	out, err := command.Output()
	if err != nil {
		t.Fatalf("probing %s: %v", path, err)
	}

	return strings.TrimSpace(string(out))
}

// TestTheDisabledLevelLeavesTheRecordingUntouched is the level an administrator chooses when the
// server is to store exactly what the recording tool produced: nothing is run and nothing is
// changed, not even the name.
func TestTheDisabledLevelLeavesTheRecordingUntouched(t *testing.T) {
	converter := installedConverter(t)

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionDisabled, nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if converted.Filename != "call.wav" || converted.Mime != "audio/wav" {
		t.Errorf("the recording is presented as %q %q, want it left exactly as it arrived", converted.Filename, converted.Mime)
	}

	if !bytes.Equal(converted.Data, in.Data) {
		t.Errorf("the recording was changed by a conversion that was disabled")
	}
}

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
func TestTheDisabledLevelIsSilentEvenWithoutAConverter(t *testing.T) {
	converter := &Converter{}

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	if _, err := converter.Convert(context.Background(), in, domain.AudioConversionDisabled, nil); err != nil {
		t.Errorf("Convert = %v, want nothing said about a converter this deployment does not need", err)
	}
}

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
func TestConvertingARecordingProducesTheFormatTheServerStores(t *testing.T) {
	converter := installedConverter(t)

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if converted.Filename != "call.m4a" {
		t.Errorf("the recording is named %q, want the call's name with the container the server stores", converted.Filename)
	}

	if converted.Mime != "audio/mp4" {
		t.Errorf("the recording is presented as %q, want the media type the server hands to clients", converted.Mime)
	}

	if len(converted.Data) == 0 {
		t.Fatal("the converted recording is empty")
	}

	path := writeConverted(t, converted)

	if codec := probe(t, path, "-select_streams", "a:0", "-show_entries", "stream=codec_name", "-of", "default=nw=1:nk=1"); codec != "aac" {
		t.Errorf("the converted recording holds %q, want the codec the server stores calls in", codec)
	}
}

// TestTheCallTagsAreWrittenIntoTheConvertedRecording covers the tags a client reads out of a
// recording it downloads: the group, the radio, the moment, the tag, and the talkgroup.
func TestTheCallTagsAreWrittenIntoTheConvertedRecording(t *testing.T) {
	converter := installedConverter(t)

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, &ports.AudioMetadata{
		Album:  "Ground",
		Artist: "Car 4",
		Date:   time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC),
		Genre:  "Dispatch",
		Title:  "Dispatch 1",
	})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	path := writeConverted(t, converted)

	want := map[string]string{
		"album":  "Ground",
		"artist": "Car 4",
		"genre":  "Dispatch",
		"title":  "Dispatch 1",
	}

	for key, value := range want {
		got := probe(t, path, "-show_entries", "format_tags="+key, "-of", "default=nw=1:nk=1")

		if got != value {
			t.Errorf("the converted recording's %s tag is %q, want %q", key, got, value)
		}
	}

	date := probe(t, path, "-show_entries", "format_tags=date", "-of", "default=nw=1:nk=1")

	if !strings.HasPrefix(date, "2026-10-06 16:00:00") {
		t.Errorf("the converted recording's date tag is %q, want the moment the call happened", date)
	}
}

// TestARecordingConvertedWithoutTagsCarriesNoTags is the call on a talkgroup that has no tag:
// the legacy writes no tags at all for it, not even the title or the moment, and a client that
// shows a recording's tags shows nothing for those calls.
func TestARecordingConvertedWithoutTagsCarriesNoTags(t *testing.T) {
	converter := installedConverter(t)

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	path := writeConverted(t, converted)

	for _, key := range []string{"album", "artist", "genre", "title"} {
		if got := probe(t, path, "-show_entries", "format_tags="+key, "-of", "default=nw=1:nk=1"); got != "" {
			t.Errorf("the converted recording's %s tag is %q, want no tags for a call whose talkgroup has none", key, got)
		}
	}
}

// TestTheLoudnessLevelsPadTheRecordingOnlyForTheReleaseThatHasTheFilters is the observable
// difference between the two releases: the filters begin by padding the recording to three
// seconds, so a server on an ffmpeg that predates them stores the short recording.
func TestTheLoudnessLevelsPadTheRecordingOnlyForTheReleaseThatHasTheFilters(t *testing.T) {
	converter := installedConverter(t)

	if !converter.loudnorm {
		t.Skip("this machine's ffmpeg is older than the release the loudness filters need")
	}

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	for _, mode := range []uint{domain.AudioConversionEnabledNorm, domain.AudioConversionEnabledLoudNorm} {
		converted, err := converter.Convert(context.Background(), in, mode, nil)
		if err != nil {
			t.Fatalf("Convert mode %d: %v", mode, err)
		}

		path := writeConverted(t, converted)

		raw := probe(t, path, "-show_entries", "format=duration", "-of", "default=nw=1:nk=1")

		duration, err := time.ParseDuration(raw + "s")
		if err != nil {
			t.Fatalf("the converted recording's duration is %q: %v", raw, err)
		}

		if duration < 2*time.Second {
			t.Errorf("mode %d converted a recording of %v, want the three seconds the loudness filters pad it to", mode, duration)
		}
	}
}

// TestARecordingTheConverterRefusesKeepsItsOriginalAudio is the failure that must never cost a
// call: audio ffmpeg cannot read is stored as it arrived.
func TestARecordingTheConverterRefusesKeepsItsOriginalAudio(t *testing.T) {
	converter := installedConverter(t)

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: []byte("this is not audio at all")}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)

	if err == nil {
		t.Fatal("recording audio ffmpeg cannot read was converted, want the failure reported")
	}

	if !strings.Contains(err.Error(), "ffmpeg") {
		t.Errorf("Convert = %v, want what ffmpeg said about the recording", err)
	}

	if converted.Filename != "call.wav" || !bytes.Equal(converted.Data, in.Data) {
		t.Errorf("the recording is presented as %q, want the original kept when nothing converted it", converted.Filename)
	}
}

// TestAStoppedCallerKeepsTheOriginalRecording covers the converter's one deliberate difference
// from the legacy: the run belongs to the caller that asked for it, and a caller that stops gets
// an error rather than a hung server.
func TestAStoppedCallerKeepsTheOriginalRecording(t *testing.T) {
	converter := installedConverter(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	converted, err := converter.Convert(ctx, in, domain.AudioConversionEnabled, nil)

	if err == nil {
		t.Fatal("a conversion its caller stopped reported no failure")
	}

	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "ffmpeg") {
		t.Errorf("Convert = %v, want the stopped caller or what ffmpeg said", err)
	}

	if !bytes.Equal(converted.Data, in.Data) {
		t.Errorf("the recording was changed by a conversion its caller stopped")
	}
}
