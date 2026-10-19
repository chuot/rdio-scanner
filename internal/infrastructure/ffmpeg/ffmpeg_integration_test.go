//go:build integration

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

package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// installedConverter is the converter a deployment with ffmpeg gets. These are integration tests
// over the real binary, so a machine that does not have the tool is a failure and not a skip: the
// suite that runs with the `integration` tag is the suite that says the conversion works, and a run
// that quietly skipped everything would say nothing.
func installedConverter(t *testing.T) *Converter {
	t.Helper()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Fatalf("ffmpeg is not on the path: %v", err)
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
		t.Fatalf("ffprobe is not on the path: %v", err)
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
