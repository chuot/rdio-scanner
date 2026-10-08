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
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/ffmpeg"
)

// command runs one program with its arguments, hands it a recording on its input, and hands back what
// it wrote. It is how the conformance check reaches a binary it cannot run itself: the compiled ffmpeg
// lives inside the build container, and the check reaches it through podman.
type command func(argv []string, stdin []byte) ([]byte, error)

// conformancePrograms are the two compiled programs the check runs, each as the arguments that start
// it: the container, the interpreter when that container cannot run the architecture on its own, and
// the program.
type conformancePrograms struct {
	FFmpeg  []string
	FFProbe []string

	// Work is where the container sees the recordings the check writes out, which is the same
	// directory the build writes them into on the host.
	Work string
}

// theRecordingMetadata is the talkgroup the check converts a recording for.
var theRecordingMetadata = ports.AudioMetadata{
	Album:  "Talkgroup",
	Artist: "Scanner",
	Date:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	Genre:  "Broadcast",
	Title:  "Call",
}

// theRecordingTags are the tags the server writes into a converted recording, read the way the
// converter writes them, and the ones the check asks the converted recording back for.
var theRecordingTags = map[string]string{
	"album":  fmt.Sprintf("%v", theRecordingMetadata.Album),
	"artist": fmt.Sprintf("%v", theRecordingMetadata.Artist),
	"date":   fmt.Sprintf("%v", theRecordingMetadata.Date),
	"genre":  fmt.Sprintf("%v", theRecordingMetadata.Genre),
	"title":  fmt.Sprintf("%v", theRecordingMetadata.Title),
}

// conversionLevel is one of the levels a server is configured with, and what that level does to the
// length of the recording it converts.
type conversionLevel struct {
	Name   string
	Mode   uint
	Padded bool
}

// theConversionLevels are the levels a compiled ffmpeg is checked against. The disabled level is not
// among them: it converts without running ffmpeg at all, so there is nothing in a compiled ffmpeg for
// it to check.
var theConversionLevels = []conversionLevel{
	{Name: "enabled", Mode: domain.AudioConversionEnabled},
	{Name: "norm", Mode: domain.AudioConversionEnabledNorm, Padded: true},
	{Name: "loudnorm", Mode: domain.AudioConversionEnabledLoudNorm, Padded: true},
}

// thePaddedLength is what apad=whole_dur=3s pads a recording to, and the shortest converted recording
// the filters are allowed to produce.
const thePaddedLength = 3.0

// conformanceInput is one recording the check feeds ffmpeg.
type conformanceInput struct {
	Name string
	Mime string
	Body []byte
}

//go:embed testdata/tone.mp3
var toneMP3 []byte

// conformanceInputs are the recordings the dirwatch hands the converter: the same one-second tone, as
// a WAV and as an MP3, because the trimmed build has to demux and decode both.
func conformanceInputs() []conformanceInput {
	return []conformanceInput{
		{Name: "wav", Mime: "audio/wav", Body: aToneWAV()},
		{Name: "mp3", Mime: "audio/mpeg", Body: toneMP3},
	}
}

// conformanceReport is what the check saw, which the build says to the operator.
type conformanceReport struct {
	Conversions int
	Shortest    float64
	Longest     float64
}

// conformanceCheck runs a compiled ffmpeg through the argument list the converter actually uses, and
// reads what comes back the way the server's audio endpoints read it: an m4a, of the length the
// filters ask for, carrying the tags the server writes.
//
// It is the proof that a trimmed build left nothing the converter needs out. Every way it fails stops
// the build, because a bundle that cannot do the job is worse than no bundle: it is a deployment that
// quietly stores the wrong audio.
func conformanceCheck(execute command, programs conformancePrograms, dir string) (conformanceReport, error) {
	metadata := &theRecordingMetadata

	report := conformanceReport{Shortest: math.MaxFloat64}

	for _, in := range conformanceInputs() {
		for _, level := range theConversionLevels {
			converted, err := execute(starts(programs.FFmpeg, ffmpeg.Arguments(level.Mode, metadata, true)...), in.Body)
			if err != nil {
				return report, fmt.Errorf("the compiled ffmpeg refused the %s level from a %s: %w", level.Name, in.Name, err)
			}

			if !isM4A(converted) {
				return report, fmt.Errorf(
					"the compiled ffmpeg did not produce the recording the server stores for the %s level from a %s",
					level.Name, in.Name,
				)
			}

			name := in.Name + "-" + level.Name + ".m4a"

			if err := os.WriteFile(filepath.Join(dir, name), converted, 0644); err != nil {
				return report, fmt.Errorf("the build cannot keep the recording it converted: %w", err)
			}

			format, err := probe(execute, programs, filepath.Join(programs.Work, name))
			if err != nil {
				return report, err
			}

			duration, err := theCheckConfirms(format, level, in, name)
			if err != nil {
				return report, err
			}

			report.Conversions++

			if duration < report.Shortest {
				report.Shortest = duration
			}

			if duration > report.Longest {
				report.Longest = duration
			}
		}
	}

	return report, nil
}

// theCheckConfirms the converted recording is the recording the server would have stored: the tags are
// all there, and the length is the one this level of conversion produces.
func theCheckConfirms(format probedFormat, level conversionLevel, in conformanceInput, name string) (float64, error) {
	duration, err := strconv.ParseFloat(format.Duration, 64)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("the compiled ffmpeg produced %s, whose length the bundled ffprobe cannot read", name)
	}

	if level.Padded && duration < thePaddedLength {
		return 0, fmt.Errorf(
			"%s is %s seconds, and the %s level pads a recording to %v seconds",
			name, format.Duration, level.Name, thePaddedLength,
		)
	}

	// A plain recording that came back padded is a build that ran filters nobody asked for.
	if !level.Padded && duration >= thePaddedLength {
		return 0, fmt.Errorf("%s is %s seconds, and nothing asked the %s level to pad it", name, format.Duration, level.Name)
	}

	for tag, want := range theRecordingTags {
		if got := format.Tags[tag]; got != want {
			return 0, fmt.Errorf("%s carries %s=%q, and the server writes %s=%q", name, tag, got, tag, want)
		}
	}

	return duration, nil
}

// probedRecording is what the bundled ffprobe says about a converted recording. The recipe enables the
// json muxer for exactly this.
type probedRecording struct {
	Format probedFormat `json:"format"`
}

type probedFormat struct {
	Duration string            `json:"duration"`
	Tags     map[string]string `json:"tags"`
}

// probe asks the bundled ffprobe what it knows about one converted recording. It is given a file and
// not the pipe: ffprobe cannot get a duration from a stream it cannot seek, which is the thing the
// recording-duration feature has to know.
func probe(execute command, programs conformancePrograms, path string) (probedFormat, error) {
	argv := starts(programs.FFProbe,
		"-v", "error",
		"-show_entries", "format=duration:format_tags=title,artist,album,date,genre",
		"-of", "json",
		path,
	)

	body, err := execute(argv, nil)
	if err != nil {
		return probedFormat{}, fmt.Errorf("the bundled ffprobe could not read %s: %w", path, err)
	}

	var probed probedRecording
	if err := json.Unmarshal(body, &probed); err != nil {
		return probedFormat{}, fmt.Errorf("the bundled ffprobe said something unreadable about %s: %w", path, err)
	}

	return probed.Format, nil
}

// starts is one program's arguments followed by what it is being asked to do, without touching the
// program's own arguments.
func starts(program []string, args ...string) []string {
	argv := make([]string, 0, len(program)+len(args))

	argv = append(argv, program...)

	return append(argv, args...)
}

// isM4A reports whether what ffmpeg wrote is the recording format the server stores, which is what the
// ipod muxer puts in the first box.
func isM4A(body []byte) bool {
	if len(body) < 12 {
		return false
	}

	return string(body[4:8]) == "ftyp"
}

// aToneWAV is one second of the tone the recordings are made of, written the way the server's own
// conversion tests write it: 8 kHz, one channel, 16-bit PCM.
func aToneWAV() []byte {
	const (
		samples   = 8000
		rate      = 8000
		amplitude = 12000
	)

	body := make([]byte, samples*2)

	for i := 0; i < samples; i++ {
		sample := int16(amplitude * math.Sin(2*math.Pi*440*float64(i)/float64(rate)))

		body[i*2] = byte(sample)
		body[i*2+1] = byte(sample >> 8)
	}

	wav := make([]byte, 0, 44+len(body))

	wav = append(wav, "RIFF"...)
	wav = appendUint32(wav, uint32(36+len(body)))
	wav = append(wav, "WAVE"...)
	wav = append(wav, "fmt "...)
	wav = appendUint32(wav, 16)
	wav = appendUint16(wav, 1) // PCM
	wav = appendUint16(wav, 1) // one channel
	wav = appendUint32(wav, rate)
	wav = appendUint32(wav, rate*2)
	wav = appendUint16(wav, 2) // one sample frame
	wav = appendUint16(wav, 16)
	wav = append(wav, "data"...)
	wav = appendUint32(wav, uint32(len(body)))

	return append(wav, body...)
}

func appendUint32(out []byte, v uint32) []byte {
	return append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendUint16(out []byte, v uint16) []byte {
	return append(out, byte(v), byte(v>>8))
}
