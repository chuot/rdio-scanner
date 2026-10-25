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
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/ffmpeg"
)

// command runs one program, hands it a recording on its input, and returns what it
// wrote. It is how the check reaches a compiled ffmpeg through the build container.
type command func(argv []string, stdin []byte) ([]byte, error)

// checkPrograms are the two compiled programs the check runs, each as the arguments
// that start it: the container, the interpreter, and the program.
type checkPrograms struct {
	FFmpeg  []string
	FFProbe []string

	// Work is where the container sees the recordings the check writes out, the same
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

// theRecordingTags are the tags the server writes into a converted recording, read
// the way the converter writes them, and the ones the check asks back for.
var theRecordingTags = map[string]string{
	"album":  fmt.Sprintf("%v", theRecordingMetadata.Album),
	"artist": fmt.Sprintf("%v", theRecordingMetadata.Artist),
	"date":   fmt.Sprintf("%v", theRecordingMetadata.Date),
	"genre":  fmt.Sprintf("%v", theRecordingMetadata.Genre),
	"title":  fmt.Sprintf("%v", theRecordingMetadata.Title),
}

// conversionLevel is one of the levels a server is configured with, and what it does
// to the length of the recording it converts.
type conversionLevel struct {
	Name   string
	Mode   uint
	Padded bool
}

// theConversionLevels are the levels a compiled ffmpeg is checked against. The
// disabled level is absent: it converts without running ffmpeg at all.
var theConversionLevels = []conversionLevel{
	{Name: "enabled", Mode: domain.AudioConversionEnabled},
	{Name: "norm", Mode: domain.AudioConversionEnabledNorm, Padded: true},
	{Name: "loudnorm", Mode: domain.AudioConversionEnabledLoudNorm, Padded: true},
}

// thePaddedLength is what apad=whole_dur=3s pads a recording to, and the shortest
// converted recording the filters are allowed to produce.
const thePaddedLength = 3.0

// checkInput is one recording the check feeds ffmpeg.
type checkInput struct {
	Name string
	Mime string
	Body []byte
}

//go:embed assets/tone.mp3
var toneMP3 []byte

// checkInputs are the recordings the dirwatch hands the converter: the same
// one-second tone as a WAV and as an MP3, so the trimmed build decodes both.
func checkInputs() []checkInput {
	return []checkInput{
		{Name: "wav", Mime: "audio/wav", Body: aToneWAV()},
		{Name: "mp3", Mime: "audio/mpeg", Body: toneMP3},
	}
}

// checkReport is what the check saw, which the build says to the operator.
type checkReport struct {
	Conversions int
	Shortest    float64
	Longest     float64

	// Codecs is what the program registers: the codecs it can decode and encode, the
	// audio-only promise the check proves at its one gate.
	Codecs codecRegistry

	// Productions is what each conversion produced, in the order the check ran them.
	// The build prints the shortest and the longest; the report keeps every one.
	// see the ffmpeg-check-shipping-gate maintainer note.
	Productions []production
}

// codecRegistry is what a compiled ffmpeg registers: the codecs it can decode and
// encode, named. A program registering a video codec is not this project's program.
type codecRegistry struct {
	Audio []string `json:"audio"`
	Video []string `json:"video"`
}

// production is one converted recording: what went in, which level made it, how
// big it came out, and how long the bundled ffprobe says it is.
type production struct {
	Input    string `json:"input"`
	Mode     string `json:"mode"`
	Bytes    int    `json:"bytes"`
	Duration string `json:"duration"`
}

// ffmpegCheck runs a compiled ffmpeg through the converter's own argument list and
// reads what comes back as the server's audio endpoints read it: an m4a, padded
// to the length the filters ask for, carrying the server's tags.
// see the ffmpeg-check-shipping-gate maintainer note.
func ffmpegCheck(execute command, programs checkPrograms, dir string) (checkReport, error) {
	metadata := &theRecordingMetadata

	report := checkReport{Shortest: math.MaxFloat64}

	for _, in := range checkInputs() {
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

			report.Productions = append(report.Productions, production{
				Input:    in.Name,
				Mode:     level.Name,
				Bytes:    len(converted),
				Duration: format.Duration,
			})

			if duration < report.Shortest {
				report.Shortest = duration
			}

			if duration > report.Longest {
				report.Longest = duration
			}
		}
	}

	// Ask here what the program registers: the conversions prove the binaries do the
	// job, the registry proves what they are.
	// see the ffmpeg-check-shipping-gate maintainer note.
	codecs, err := theCodecsTheProgramRegisters(execute, programs)
	if err != nil {
		return report, err
	}

	if len(codecs.Video) > 0 {
		return report, fmt.Errorf(
			"the compiled ffmpeg registers video codecs: %s. This project ships an ffmpeg compiled for audio, and the recipe that produces it is internal/infrastructure/ffmpeg/bundle/manifest.json, which enables no video component",
			strings.Join(codecs.Video, ", "),
		)
	}

	report.Codecs = codecs

	return report, nil
}

// theCodecsTheProgramRegisters asks the program what it registers: `ffmpeg -codecs`
// prints a legend, a separator, then six flag columns and a codec name per line.
// A line with neither the decoding nor the encoding column set is a codec descriptor,
// format metadata that decodes and encodes nothing, not a registered codec.
func theCodecsTheProgramRegisters(execute command, programs checkPrograms) (codecRegistry, error) {
	body, err := execute(starts(programs.FFmpeg, "-hide_banner", "-codecs"), nil)
	if err != nil {
		return codecRegistry{}, fmt.Errorf("the compiled ffmpeg would not say what it registers: %w", err)
	}

	codecs := codecRegistry{}
	named := map[string]bool{}
	listed := false

	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "-------" {
			listed = true
			continue
		}

		if !listed {
			continue
		}

		flags, name, ok := aCodecLine(line)
		if !ok {
			continue
		}

		// A codec the program can neither decode nor encode is a descriptor, and registers nothing.
		if flags[0] != 'D' && flags[1] != 'E' {
			continue
		}

		if named[name] {
			continue
		}

		named[name] = true

		switch flags[2] {
		case 'V':
			codecs.Video = append(codecs.Video, name)
		case 'A':
			codecs.Audio = append(codecs.Audio, name)
		}
	}

	if !listed {
		return codecRegistry{}, fmt.Errorf(
			"the compiled ffmpeg answered its codec probe with something that is not the codec list: %s",
			firstLineOf(body),
		)
	}

	if len(codecs.Audio) == 0 && len(codecs.Video) == 0 {
		return codecRegistry{}, fmt.Errorf(
			"the compiled ffmpeg answered its codec probe naming no codec it can decode or encode",
		)
	}

	return codecs, nil
}

// codecFlagColumns counts the columns ffmpeg prints before every codec: decoding,
// encoding, the type, intra-only, lossy, lossless; codecFlagAlphabet is their alphabet.
const (
	codecFlagColumns  = 6
	codecFlagAlphabet = "D.EVASTIL"
)

// aCodecLine reads one codec line: six flag columns and a name. A legend line names
// a flag column and carries an `=` where a name would be, and a line whose flags
// are not flags is not a codec line either.
func aCodecLine(line string) (flags string, name string, ok bool) {
	fields := strings.Fields(line)

	if len(fields) < 2 || len(fields[0]) != codecFlagColumns {
		return "", "", false
	}

	for _, flag := range fields[0] {
		if !strings.Contains(codecFlagAlphabet, string(flag)) {
			return "", "", false
		}
	}

	if fields[1] == "=" {
		return "", "", false
	}

	return fields[0], fields[1], true
}

// firstLineOf is the first line of what a program printed, which a refusal quotes
// when the answer is not the answer the check asked for.
func firstLineOf(body []byte) string {
	line, _, _ := strings.Cut(string(body), "\n")

	if trimmed := strings.TrimSpace(line); trimmed != "" {
		return trimmed
	}

	return "(nothing)"
}

// theCheckConfirms the converted recording is the one the server would have stored:
// the tags are all there, and the length is the one this level produces.
func theCheckConfirms(format probedFormat, level conversionLevel, in checkInput, name string) (float64, error) {
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

// probedRecording is what the bundled ffprobe says about a converted recording, and
// the recipe enables the json muxer for exactly this.
type probedRecording struct {
	Format probedFormat `json:"format"`
}

type probedFormat struct {
	Duration string            `json:"duration"`
	Tags     map[string]string `json:"tags"`
}

// probe asks the bundled ffprobe what it knows about one converted recording. It
// gets a file, not a pipe: ffprobe gets no duration from an unseekable stream.
func probe(execute command, programs checkPrograms, path string) (probedFormat, error) {
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

// starts is one program's arguments followed by what it is asked to do, without
// touching the program's own arguments.
func starts(program []string, args ...string) []string {
	argv := make([]string, 0, len(program)+len(args))

	argv = append(argv, program...)

	return append(argv, args...)
}

// isM4A reports whether what ffmpeg wrote is the recording format the server stores,
// the tag the ipod muxer puts in the first box.
func isM4A(body []byte) bool {
	if len(body) < 12 {
		return false
	}

	return string(body[4:8]) == "ftyp"
}

// aToneWAV is one second of the tone the recordings are made of: 8 kHz, one channel,
// 16-bit PCM, the shape the server's own conversion tests write.
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
