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

// Integration tier for the converter: runs the ffmpeg this binary
// carries, the programs written out of it, and the converter over them.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// theToneLength is how long the two fixtures in assets are, and the length the padded conversion is
// expected to bring up to.
const theToneLength = 1.0

// thePaddedToneLength is what apad=whole_dur=3s pads a recording to.
const thePaddedToneLength = 3.0

// the bundle is proven end to end: the programs written out of this
// binary read a WAV and an MP3 and convert a WAV into a padded m4a.
func TestTheFfmpegThisServerCarriesReadsAndConvertsTheAudioItIsGiven(t *testing.T) {
	manifest := bundle.ReadManifest()

	bundled, ok := bundle.Lookup()
	if !ok {
		t.Fatal("this build carries no ffmpeg: run `go run ./cmd/build ffmpeg` before this tier")
	}

	dir := t.TempDir()

	ffmpegPath, ffprobePath, err := bundled.Materialize(dir)
	if err != nil {
		t.Fatalf("the server could not write out the ffmpeg it carries: %v", err)
	}

	// the programs run from the folder the server wrote them into, each
	// named for its release, so upgrades never replace a running binary.
	for _, program := range []struct {
		what string
		path string
	}{
		{"ffmpeg", ffmpegPath},
		{"ffprobe", ffprobePath},
	} {
		if filepath.Dir(program.path) != dir {
			t.Errorf("the bundled %s runs from %s, not from %s", program.what, program.path, dir)
			continue
		}

		if want := program.what + "-" + manifest.FFmpeg; !strings.HasPrefix(filepath.Base(program.path), want) {
			t.Errorf("the bundled %s is named %s, not after the release it is: %s",
				program.what, filepath.Base(program.path), want)
		}
	}

	// The recordings the tier reads are the ones a recorder leaves behind: a WAV and an MP3, one second
	// of tone each.
	wav := aRecordingFrom(t, dir, "tone.wav")
	mp3 := aRecordingFrom(t, dir, "tone.mp3")

	if got := aProbedDuration(t, ffprobePath, wav); got < theToneLength || got > theToneLength+0.1 {
		t.Errorf("the bundled ffprobe reads the WAV as %s s, not the second it is", strconv.FormatFloat(got, 'f', 6, 64))
	}

	if got := aProbedDuration(t, ffprobePath, mp3); got < theToneLength || got > theToneLength+0.1 {
		t.Errorf("the bundled ffprobe reads the MP3 as %s s, not the second it is", strconv.FormatFloat(got, 'f', 6, 64))
	}

	// the conversion runs through the server's own converter at the level a
	// talkgroup with a loudness target uses.
	converter := converterChoice{Program: ffmpegPath}.converter()

	converted, err := converter.Convert(
		context.Background(),
		ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: readOrFatal(t, wav)},
		domain.AudioConversionEnabledLoudNorm,
		&ports.AudioMetadata{
			Album:  "Talkgroup 1",
			Artist: "Dispatch",
			Date:   time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
			Genre:  "Broadcast",
			Title:  "Call 1",
		},
	)
	if err != nil {
		t.Fatalf("the bundled ffmpeg refused the recording: %v", err)
	}

	if string(converted.Data[4:8]) != "ftyp" {
		t.Fatalf("the bundled ffmpeg did not produce the m4a the server stores: %s", filepath.Base(wav))
	}

	m4a := writeRecording(t, dir, "converted.m4a", converted.Data)

	if got := aProbedDuration(t, ffprobePath, m4a); got < thePaddedToneLength {
		t.Errorf("the bundled ffprobe reads the converted recording as %s s, shorter than the three seconds its filters pad it to",
			strconv.FormatFloat(got, 'f', 6, 64))
	}
}

// the converter's reported length and the bundled ffprobe's measurement
// of the same recording agree, at every conversion level.
func TestTheConversionReportsTheLengthTheConvertedRecordingIs(t *testing.T) {
	bundled, ok := bundle.Lookup()
	if !ok {
		t.Fatal("this build carries no ffmpeg: run `go run ./cmd/build ffmpeg` before this tier")
	}

	dir := t.TempDir()

	ffmpegPath, ffprobePath, err := bundled.Materialize(dir)
	if err != nil {
		t.Fatalf("the server could not write out the ffmpeg it carries: %v", err)
	}

	wav := aRecordingFrom(t, dir, "tone.wav")

	for _, level := range []uint{
		domain.AudioConversionEnabled,
		domain.AudioConversionEnabledNorm,
		domain.AudioConversionEnabledLoudNorm,
	} {
		converter := converterChoice{Program: ffmpegPath}.converter()

		converted, err := converter.Convert(context.Background(), ports.AudioFile{
			Filename: "call.wav",
			Mime:     "audio/wav",
			Data:     readOrFatal(t, wav),
		}, level, nil)
		if err != nil {
			t.Fatalf("level %d: the bundled ffmpeg refused the recording: %v", level, err)
		}

		if converted.Duration == 0 {
			t.Errorf("level %d: the conversion stated no running time, want the length of the file it wrote", level)

			continue
		}

		m4a := writeRecording(t, dir, fmt.Sprintf("converted-%d.m4a", level), converted.Data)

		want := aProbedDuration(t, ffprobePath, m4a)
		got := converted.Duration.Seconds()

		// the file ffmpeg writes is the stated running time plus the AAC
		// encoder's priming: the report is never longer than the file, and the
		// file is never more than the encoder's padding longer than the report.
		if got > want {
			t.Errorf("level %d: the conversion stated %s s, longer than the %s s recording it wrote",
				level, strconv.FormatFloat(got, 'f', 6, 64), strconv.FormatFloat(want, 'f', 6, 64))

			continue
		}

		if want-got > 0.2 {
			t.Errorf("level %d: the conversion stated %s s, the converted recording is %s s — further apart than the AAC encoder's priming can explain",
				level, strconv.FormatFloat(got, 'f', 6, 64), strconv.FormatFloat(want, 'f', 6, 64))
		}
	}
}

// aDeploymentRunsThePairItsOwnBinaryCarried, read through the seam the server's startup uses rather
// than through a path the test already holds, and with no ffmpeg reachable on the machine: a fallback to
// the host's install could not run, and a notice about one would be the operator's only clue.
func TestTheServerConvertsWithThePairItWroteOutOfItsOwnBinary(t *testing.T) {
	manifest := bundle.ReadManifest()

	// Nothing but the pair written out of this binary can answer: the host's ffmpeg and ffprobe are
	// unreachable by name, so a choice that fell back to them names programs this run cannot start.
	t.Setenv("PATH", "")

	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if found, err := exec.LookPath(name); err == nil {
			t.Fatalf("the tier cannot put this machine's own %s out of reach: %s still resolves to %s", name, name, found)
		}
	}

	dir := t.TempDir()

	ffmpegPath, ffprobePath, failure := bundledFFmpeg(dir)

	if failure != "" {
		t.Fatalf("the server's own startup could not write out the ffmpeg it carries: %s", failure)
	}

	if ffmpegPath == "" || ffprobePath == "" {
		t.Fatal("the server's own startup found no ffmpeg to run: run `go run ./cmd/build ffmpeg` before this tier")
	}

	// the two programs are the ones this binary carried, named after the release they are and run from
	// the folder the server wrote them into.
	for _, program := range []struct {
		what string
		path string
	}{
		{"ffmpeg", ffmpegPath},
		{"ffprobe", ffprobePath},
	} {
		if filepath.Dir(program.path) != dir {
			t.Errorf("the server runs the %s it carries from %s, not from %s", program.what, program.path, dir)
			continue
		}

		if want := program.what + "-" + manifest.FFmpeg; !strings.HasPrefix(filepath.Base(program.path), want) {
			t.Errorf("the server runs the %s it carries as %s, not after the release it is: %s",
				program.what, filepath.Base(program.path), want)
		}
	}

	choice := theConverter(ffmpegPath, ffprobePath, failure)

	if choice.Program != ffmpegPath {
		t.Errorf("the server converts with %s, not the ffmpeg it wrote out of its own binary at %s", choice.Program, ffmpegPath)
	}

	if choice.ProbeProgram != ffprobePath {
		t.Errorf("the server measures lengths with %s, not the ffprobe it wrote out of its own binary at %s", choice.ProbeProgram, ffprobePath)
	}

	if choice.Notice != "" {
		t.Errorf("the server has something to tell the operator about a converter it did not fall back on: %s", choice.Notice)
	}

	wav := aRecordingFrom(t, dir, "tone.wav")

	prober := choice.prober(dir)

	// the carried ffprobe is what the server's own prober runs, because it is the only ffprobe this
	// deployment can reach at all.
	wavLength, err := prober.Probe(context.Background(), ports.AudioFile{
		Filename: filepath.Base(wav),
		Mime:     "audio/wav",
		Data:     readOrFatal(t, wav),
	})
	if err != nil {
		t.Fatalf("the server's own prober could not read the recording it was given: %v", err)
	}

	if seconds := wavLength.Seconds(); seconds < theToneLength || seconds > theToneLength+0.1 {
		t.Errorf("the server's own prober reads the WAV as %s s, not the second it is", strconv.FormatFloat(seconds, 'f', 6, 64))
	}

	converted, err := choice.converter().Convert(context.Background(), ports.AudioFile{
		Filename: "call.wav",
		Mime:     "audio/wav",
		Data:     readOrFatal(t, wav),
	}, domain.AudioConversionEnabledLoudNorm, nil)
	if err != nil {
		t.Fatalf("the ffmpeg the server chose refused the recording: %v", err)
	}

	if string(converted.Data[4:8]) != "ftyp" {
		t.Fatalf("the ffmpeg the server chose did not produce the m4a the server stores: %s", filepath.Base(wav))
	}

	m4a := writeRecording(t, dir, "converted.m4a", converted.Data)

	m4aLength, err := prober.Probe(context.Background(), ports.AudioFile{
		Filename: filepath.Base(m4a),
		Mime:     "audio/mp4",
		Data:     readOrFatal(t, m4a),
	})
	if err != nil {
		t.Fatalf("the server's own prober could not read the recording it converted: %v", err)
	}

	if seconds := m4aLength.Seconds(); seconds < thePaddedToneLength {
		t.Errorf("the server's own prober reads the converted recording as %s s, shorter than the three seconds its filters pad it to",
			strconv.FormatFloat(seconds, 'f', 6, 64))
	}
}

// aRecordingFrom copies a tier fixture into the working directory, so
// ffprobe receives a file rather than a pipe.
func aRecordingFrom(t *testing.T, dir string, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("assets", name))
	if err != nil {
		t.Fatalf("the tier has no %s to read: %v", name, err)
	}

	return writeRecording(t, dir, name, body)
}

func writeRecording(t *testing.T, dir string, name string, body []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatalf("the tier cannot write %s: %v", name, err)
	}

	return path
}

func readOrFatal(t *testing.T, path string) []byte {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the tier cannot read %s: %v", path, err)
	}

	return body
}

// aProbedDuration asks the materialised ffprobe how long a recording is, the way the server will have
// to ask it.
func aProbedDuration(t *testing.T, ffprobe string, path string) float64 {
	t.Helper()

	out, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "json", path).Output()
	if err != nil {
		t.Fatalf("the bundled ffprobe could not read %s: %v", path, err)
	}

	var probed struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}

	if err := json.Unmarshal(out, &probed); err != nil {
		t.Fatalf("the bundled ffprobe said something unreadable about %s: %v", path, err)
	}

	duration, err := strconv.ParseFloat(probed.Format.Duration, 64)
	if err != nil {
		t.Fatalf("the bundled ffprobe gave no duration for %s: %s", path, fmt.Sprintf("%q", probed.Format.Duration))
	}

	return duration
}
