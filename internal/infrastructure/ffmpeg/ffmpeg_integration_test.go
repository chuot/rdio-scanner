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

// installedConverter is the converter a deployment with ffmpeg gets. These
// are integration tests over the real binary, so a machine without the tool
// is a failure and not a skip.
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

// TestTheDisabledLevelLeavesTheRecordingUntouched is the level that stores
// exactly what the recording tool produced: nothing is run and nothing is
// changed, not even the name.

// TestTheDisabledLevelLeavesTheRecordingUntouched is the level that stores
// exactly what the recording tool produced: nothing is run and nothing is
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

// TestAMissingConverterIsReportedOncePerServerRun is what a deployment
// without ffmpeg sees: one warning line, and then every recording stored as
// it arrived.

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

// TestARecordingConvertedWithoutTagsCarriesNoTags is the call on a talkgroup
// that has no tag: no tags at all are written for it, not even the title or
// the moment.

// TestARecordingConvertedWithoutTagsCarriesNoTags is the call on a talkgroup
// that has no tag: no tags at all are written for it, not even the title or
// the moment.
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

// sourceTaggedRecording is a recording whose file already carries tags of its
// own, the way a recording tool's output can: every label names the tool that
// wrote it, so a tag that reaches the converted recording names where it came
// from.
func sourceTaggedRecording(t *testing.T) []byte {
	t.Helper()

	path := filepath.Join(t.TempDir(), "recorder.mp3")

	command := exec.Command("ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-metadata", "album=Album From The Recorder",
		"-metadata", "artist=Artist From The Recorder",
		"-metadata", "comment=Comment From The Recorder",
		"-metadata", "genre=Genre From The Recorder",
		"-metadata", "title=Title From The Recorder",
		"-y", path,
	)

	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("building a recording that carries its own tags: %v\n%s", err, out)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the recording that carries its own tags: %v", err)
	}

	return body
}

// TestARecordingConvertedFromATaggedSourceKeepsNoneOfThoseTags is the
// recording that arrives already tagged: the converted recording the server
// stores says what this server knows about the call, and nothing the file it
// came from already said.

// TestARecordingConvertedFromATaggedSourceKeepsNoneOfThoseTags is the
// recording that arrives already tagged: the converted recording the server
// stores says what this server knows about the call, and nothing the file it
// came from already said.
func TestARecordingConvertedFromATaggedSourceKeepsNoneOfThoseTags(t *testing.T) {
	converter := installedConverter(t)

	in := ports.AudioFile{Filename: "recorder.mp3", Mime: "audio/mpeg", Data: sourceTaggedRecording(t)}

	calls := []struct {
		name     string
		metadata *ports.AudioMetadata
	}{
		{
			name:     "a call on a talkgroup with no tag",
			metadata: nil,
		},
		{
			name: "a call on a talkgroup with a tag",
			metadata: &ports.AudioMetadata{
				Album:  "Ground",
				Artist: "Car 4",
				Date:   time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC),
				Genre:  "Dispatch",
				Title:  "Dispatch 1",
			},
		},
	}

	for _, call := range calls {
		converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, call.metadata)
		if err != nil {
			t.Fatalf("converting a recording that carries its own tags (%s): %v", call.name, err)
		}

		path := writeConverted(t, converted)

		for _, key := range []string{"album", "artist", "comment", "genre", "title"} {
			got := probe(t, path, "-show_entries", "format_tags="+key, "-of", "default=nw=1:nk=1")

			if strings.Contains(got, "From The Recorder") {
				t.Errorf("the converted recording's %s tag is %q for %s, want nothing the file it came from already said", key, got, call.name)
			}
		}
	}
}

// TestTheLoudnessLevelsPadTheRecordingOnlyForTheReleaseThatHasTheFilters is
// the observable difference between the two releases: the filters pad the
// recording to three seconds, and a release without them does not.

// TestTheLoudnessLevelsPadTheRecordingOnlyForTheReleaseThatHasTheFilters is
// the observable difference between the two releases: the filters pad the
// recording to three seconds, and a release without them does not.
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

// TestAStoppedCallerKeepsTheOriginalRecording covers the converter's one
// deliberate difference from the legacy: the run belongs to the caller that
// asked for it.

// TestAStoppedCallerKeepsTheOriginalRecording covers the converter's one
// deliberate difference from the legacy: the run belongs to the caller that
// asked for it.
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

// aRecordingBuiltWithTheMachine writes a file with the machine's own ffmpeg, from
// its tone and colour sources: the containers these three tests are about are not
// fixtures in the repository, they are the shape of a recording tool's output.
func aRecordingBuiltWithTheMachine(t *testing.T, name string, args ...string) []byte {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)

	command := exec.Command("ffmpeg", append([]string{"-hide_banner", "-loglevel", "error"}, append(args, append([]string{"-y"}, path)...)...)...)

	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("building %s: %v\n%s", name, err, out)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}

	if len(body) == 0 {
		t.Fatalf("building %s wrote nothing", name)
	}

	return body
}

// aRecordingCarryingVideo is a one second tone with a 64x64 black video stream beside it, the
// shape a recording tool that writes video and audio into one mp4 produces. The index is written
// at the front of the file, because the server hands every recording to ffmpeg on a pipe with no
// way back to the head of it.
func aRecordingCarryingVideo(t *testing.T) []byte {
	t.Helper()

	return aRecordingBuiltWithTheMachine(t, "camera.mp4",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:duration=1",
		"-map", "0:a", "-map", "1:v",
		"-c:a", "aac", "-c:v", "mpeg4", "-shortest",
		"-movflags", "+faststart",
	)
}

// aRecordingWithNoAudio is the video stream on its own: a file that arrives as a
// recording and carries nothing a call could hear.
func aRecordingWithNoAudio(t *testing.T) []byte {
	t.Helper()

	return aRecordingBuiltWithTheMachine(t, "camera.mp4",
		"-f", "lavfi", "-i", "color=c=black:s=64x64:r=10:duration=1",
		"-c:v", "mpeg4",
		"-movflags", "+faststart",
	)
}

// aRecordingWithTwoAudioStreams is the Matroska file with a mono one second tone as its first
// audio stream and a stereo two second tone as its second, and the default flag on the second:
// the file ffmpeg's own choice of the best stream answers with the second one.
func aRecordingWithTwoAudioStreams(t *testing.T) []byte {
	t.Helper()

	return aRecordingBuiltWithTheMachine(t, "two.mkv",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=660:duration=2",
		"-filter_complex", "[1:a]pan=stereo|c0=c0|c1=c0[stereo]",
		"-map", "0:a", "-map", "[stereo]",
		"-c:a", "aac",
		"-disposition:a:0", "0",
		"-disposition:a:1", "default",
	)
}

// TestARecordingThatCarriesVideoBesideItsAudioIsStoredAsItsAudio is the mp4 a
// recording tool wrote with its picture beside its sound: the call is stored as
// its audio, because this build compiles no video encoder and has no business
// keeping a picture.
func TestARecordingThatCarriesVideoBesideItsAudioIsStoredAsItsAudio(t *testing.T) {
	converter := installedConverter(t)

	in := ports.AudioFile{Filename: "camera.mp4", Mime: "audio/mp4", Data: aRecordingCarryingVideo(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err != nil {
		t.Fatalf("converting a recording that carries video beside its audio: %v", err)
	}

	streams := probe(t, writeConverted(t, converted), "-show_entries", "stream=codec_type", "-of", "csv=p=0")

	if streams != "audio" {
		t.Errorf("the stored recording carries the streams %q, want its audio alone: the file it was given carries a one second tone and a 64x64 video stream, and this build compiles no video encoder", streams)
	}
}

// TestARecordingWithNoAudioStreamIsRefusedAndKeepsWhatItArrivedWith is the file
// that arrived as a recording and carries nothing a call could hear: the failure
// is reported, and the bytes the call was given are kept.
func TestARecordingWithNoAudioStreamIsRefusedAndKeepsWhatItArrivedWith(t *testing.T) {
	converter := installedConverter(t)

	body := aRecordingWithNoAudio(t)

	in := ports.AudioFile{Filename: "camera.mp4", Mime: "audio/mp4", Data: body}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err == nil {
		t.Fatalf("a recording with no audio stream was stored as %q %q in %d bytes, want the conversion refused: a recording with nothing to hear is not a call", converted.Filename, converted.Mime, len(converted.Data))
	}

	if !strings.Contains(err.Error(), "ffmpeg") {
		t.Errorf("Convert = %v, want what the program said about the recording", err)
	}

	if converted.Filename != "camera.mp4" || !bytes.Equal(converted.Data, body) {
		t.Errorf("the recording is presented as %q, want the bytes it arrived with kept when nothing converted them", converted.Filename)
	}
}

// TestARecordingWithTwoAudioStreamsStoresTheFirstOne is the recording whose file
// carries more than one audio stream: the stored call is the server's choice, the
// stream the file lists first, and not ffmpeg's choice of the stream it rates the
// best one.
func TestARecordingWithTwoAudioStreamsStoresTheFirstOne(t *testing.T) {
	converter := installedConverter(t)

	in := ports.AudioFile{Filename: "two.mkv", Mime: "audio/x-matroska", Data: aRecordingWithTwoAudioStreams(t)}

	converted, err := converter.Convert(context.Background(), in, domain.AudioConversionEnabled, nil)
	if err != nil {
		t.Fatalf("converting a recording with two audio streams: %v", err)
	}

	path := writeConverted(t, converted)

	channels := probe(t, path, "-show_entries", "stream=channels", "-of", "csv=p=0")

	raw := probe(t, path, "-show_entries", "format=duration", "-of", "default=nw=1:nk=1")

	duration, err := time.ParseDuration(raw + "s")
	if err != nil {
		t.Fatalf("the stored recording's duration is %q: %v", raw, err)
	}

	const theFirstAudioStream = 1 * time.Second

	if channels != "1" || duration < theFirstAudioStream/2 || duration > 3*theFirstAudioStream/2 {
		t.Errorf("the stored recording has %q channel and lasts %v, want the first audio stream: the file it was given carries a mono one second tone first and a stereo two second tone marked the default stream, which is the stream ffmpeg picks when it is left to choose", channels, duration)
	}
}
