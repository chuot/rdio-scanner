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
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

// This file is the integration tier: it runs the ffmpeg this binary actually carries, the programs the
// server writes out of it, and the converter that runs them. Nothing below it can reach that seam, and
// it is compiled only when asked for:
//
//	go test -tags integration./cmd/rdio-scanner/
//
// It fails when the bundle is absent instead of skipping, which is why it is a tier and not a test.

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

// TestTheFfmpegThisServerCarriesReadsAndConvertsTheAudioItIsGiven is the acceptance proof for the
// bundle: the programs written out of the server's own binary read a WAV and an MP3, and convert a WAV
// into the fragmented m4a the server stores, with the length its filters ask for.
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

	// The two programs run from the folder the server wrote them into, and each is named for the
	// release it is, which is what lets a server upgrade without replacing a binary another running
	// server is using.
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

	// And the conversion is the one the server runs, through the converter the server builds, at the
	// level a talkgroup with a loudness target uses.
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

// aRecordingFrom copies one of the tier's fixtures into the directory the tier works in, so that
// ffprobe is given a file and not a pipe — the form the duration feature will have to use.
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
