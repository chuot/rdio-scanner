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
	"fmt"
	"math"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Converter runs audio conversions with ffmpeg. Whether ffmpeg is installed,
// which release it is, is decided once at startup.
type Converter struct {
	// program is the ffmpeg this converter runs: the one the server carries inside its own binary,
	// or the name `ffmpeg` when the server's target carries none and the host's is used.
	program string

	// available is whether the filter probe — `ffmpeg -hide_banner -filters` — ran at all.
	available bool

	// loudnorm is whether that program's filter list names the loudnorm filter,
	// which is what decides whether the two loudness modes get their filters.
	loudnorm bool

	// warned records that the missing converter has already been reported, once
	// per server run.
	warned bool

	// mutex guards warned and the probe results against the concurrent handlers
	// that read them.
	mutex sync.Mutex
}

// New builds the converter for the ffmpeg this deployment installs, which is the one found on the
// PATH. A server whose target carries no bundle of its own is a server in that situation.
func New() *Converter {
	return NewAt("ffmpeg")
}

// NewAt builds the converter for the ffmpeg at the path the server was given,
// the one it carries inside its binary and writes out to its data directory.
// The filters that program has get asked of that program.
func NewAt(program string) *Converter {
	converter := &Converter{program: program}

	stdout := bytes.NewBuffer([]byte(nil))

	command := exec.Command(program, "-hide_banner", "-filters")
	command.Stdout = stdout

	if err := command.Run(); err == nil {
		converter.available = true

		converter.loudnorm = hasLoudnorm(stdout.String())
	}

	return converter
}

// loudnormFilterLine matches the loudnorm line `ffmpeg -filters` prints:
// the flag columns, then the filter name.
var loudnormFilterLine = regexp.MustCompile(`(?m)^\s*[TSC.]{2,3}\s+loudnorm\s`)

// ffmpegProgressTime matches the running time ffmpeg states in the progress
// lines it writes while it converts: `time=00: 00: 03.01`.
var ffmpegProgressTime = regexp.MustCompile(`time=(\d+):(\d{2}):(\d{2})(?:\.(\d{1,6}))?`)

// statedRunningTime is the length of the recording a program was writing
// while it printed these progress lines: the last running time it stated is
// the end of the file it produced.
func statedRunningTime(progress string) time.Duration {
	matches := ffmpegProgressTime.FindAllStringSubmatch(progress, -1)
	if len(matches) == 0 {
		return 0
	}

	match := matches[len(matches)-1]

	hours, _ := strconv.Atoi(match[1])
	minutes, _ := strconv.Atoi(match[2])
	seconds, _ := strconv.Atoi(match[3])

	length := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second

	if match[4] != "" {
		// The fraction is a fixed-width count of a fraction of a second: ".01" is a
		// hundredth and ".010667" is a millionth of a second times 10667.
		fraction, _ := strconv.Atoi(match[4])
		length += time.Duration(fraction) * time.Duration(float64(time.Second)/math.Pow(10, float64(len(match[4]))))
	}

	return length
}

// hasLoudnorm reports whether the filter list a program printed names the
// loudnorm filter, which is the ground truth.
func hasLoudnorm(filters string) bool {
	return loudnormFilterLine.MatchString(filters)
}

// Arguments returns the argument list the converter gives ffmpeg for one
// recording: the recording arrives on the pipe and leaves on it, its first audio
// stream is the one stored and its video stream is left out, and the tags and
// filters are the ones this recording and this ffmpeg ask for. The build's ffmpeg
// check runs against this same list.
func Arguments(mode uint, metadata *ports.AudioMetadata, loudnorm bool) []string {
	// The recording's audio is what this server stores, and it is named rather than
	// left to ffmpeg: a container that carries video beside its audio is refused by a
	// build that compiles no video encoder unless the video stream is set aside first,
	// and a file with more than one audio stream otherwise gets whichever stream ffmpeg
	// rates the best one instead of the one this server picked.
	args := []string{"-i", "-", "-vn", "-map", "0:a:0"}

	// The tags are written only for a talkgroup that has a tag.
	if metadata != nil {
		args = append(args,
			"-metadata", fmt.Sprintf("album=%v", metadata.Album),
			"-metadata", fmt.Sprintf("artist=%v", metadata.Artist),
			"-metadata", fmt.Sprintf("date=%v", metadata.Date),
			"-metadata", fmt.Sprintf("genre=%v", metadata.Genre),
			"-metadata", fmt.Sprintf("title=%v", metadata.Title),
		)
	}

	if loudnorm {
		switch mode {
		case domain.AudioConversionEnabledNorm:
			args = append(args, "-af", "apad=whole_dur=3s,loudnorm")

		case domain.AudioConversionEnabledLoudNorm:
			args = append(args, "-af", "apad=whole_dur=3s,loudnorm=I=-16:TP=-1.5:LRA=11")
		}
	}

	return append(args, "-c:a", "aac", "-b:a", "32k", "-movflags", "frag_keyframe+empty_moov", "-f", "ipod", "-")
}

// Convert turns one recording into the m4a the server stores. The disabled
// level returns the recording untouched without running anything, and a call
// is never lost because its audio would not convert: a converter that is not
// installed, and a recording ffmpeg refuses, both leave the recording as it
// arrived.
func (converter *Converter) Convert(ctx context.Context, in ports.AudioFile, mode uint, metadata *ports.AudioMetadata) (ports.AudioFile, error) {
	if mode == domain.AudioConversionDisabled {
		return in, nil
	}

	if missing, err := converter.notInstalled(); missing {
		return in, err
	}

	args := Arguments(mode, metadata, converter.loudnorm)

	command := exec.CommandContext(ctx, converter.program, args...)

	command.Stdin = bytes.NewReader(in.Data)

	stdout := bytes.NewBuffer([]byte(nil))
	command.Stdout = stdout

	stderr := bytes.NewBuffer([]byte(nil))
	command.Stderr = stderr

	if err := command.Run(); err != nil {
		// A conversion that failed is reported with what ffmpeg said, and the
		// recording is kept either way; and why.
		report := stderr.String()

		if len(report) > 512 {
			report = report[:512]
		}

		return in, fmt.Errorf("ffmpeg: %s", strings.TrimSpace(domain.SanitizeText(report, 0)))
	}

	return ports.AudioFile{
		Filename: fmt.Sprintf("%v.m4a", strings.TrimSuffix(in.Filename, path.Ext(in.Filename))),
		Mime:     "audio/mp4",
		Duration: statedRunningTime(stderr.String()),
		Data:     stdout.Bytes(),
	}, nil
}

// Available reports whether the program this converter was given ran its
// probe, which is what the status view shows.
func (converter *Converter) Available() bool {
	converter.mutex.Lock()
	defer converter.mutex.Unlock()

	return converter.available
}

// notInstalled reports whether this deployment has no converter, and the reason to say so — which
// is said once per server run and left unsaid afterwards.
func (converter *Converter) notInstalled() (bool, error) {
	converter.mutex.Lock()
	defer converter.mutex.Unlock()

	if converter.available {
		return false, nil
	}

	if converter.warned {
		return true, nil
	}

	converter.warned = true

	return true, errors.New("ffmpeg is not available, no audio conversion will be performed")
}
