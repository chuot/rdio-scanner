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
	"errors"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Converter is the audio converter that runs ffmpeg.
//
// Whether ffmpeg is installed, and which release it is, is decided once when the server
// starts, which is what the legacy decides: a deployment that installs ffmpeg while the
// server is running converts nothing until it restarts, and a deployment that removes it
// keeps converting until it does.
type Converter struct {
	// program is the ffmpeg this converter runs: the one the server carries inside its own binary,
	// or the name `ffmpeg` when the server's target carries none and the host's is used.
	program string

	// available is whether `ffmpeg -version` ran at all.
	available bool

	// loudnorm is whether that ffmpeg is new enough for the loudness filters, which the
	// legacy records as version43 and applies to the two modes that ask for them.
	loudnorm bool

	// warned records that the missing converter has already been reported. The legacy
	// reports it once per server run and then converts nothing in silence, which is what
	// a deployment without ffmpeg sees in its log: one line, and then nothing.
	warned bool

	// mutex guards warned and the probe results against the concurrent handlers that read
	// them. The legacy reads and writes warned from those handlers without a lock, which is
	// a data race rather than a behavior.
	mutex sync.Mutex
}

// New builds the converter for the ffmpeg this deployment installs, which is the one found on the
// PATH. A server whose target carries no bundle of its own is a server in that situation.
func New() *Converter {
	return NewAt("ffmpeg")
}

// NewAt builds the converter for the ffmpeg at the path the server was given, which is the one it
// carries inside its own binary and writes out to its data directory before running it.
//
// The release that program is gets asked of that program, so a bundled ffmpeg and a host ffmpeg are
// not taken for the same release when they are not.
func NewAt(program string) *Converter {
	converter := &Converter{program: program}

	stdout := bytes.NewBuffer([]byte(nil))

	command := exec.Command(program, "-version")
	command.Stdout = stdout

	if err := command.Run(); err == nil {
		converter.available = true

		if line, err := stdout.ReadString('\n'); err == nil {
			converter.loudnorm = hasLoudnorm(line)
		}
	}

	return converter
}

// hasLoudnorm reads the release out of the first line of `ffmpeg -version` and reports whether
// it is 4.3 or newer, which is the release that has the loudnorm filter the two conversion
// levels ask for.
//
// The version is read as two single digits, so a release numbered 10.0 is not recognized and
// converts without the filters. That is what the legacy reads, and it is worth knowing before a
// deployment moves to a release numbered that way.
func hasLoudnorm(line string) bool {
	version := regexp.MustCompile(`.*ffmpeg version .{0,1}([0-9])\.([0-9])\.[0-9].*`).
		ReplaceAllString(strings.TrimSuffix(line, "\n"), "$1.$2")

	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}

	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])

	if errMajor != nil || errMinor != nil {
		return false
	}

	return major > 4 || (major == 4 && minor >= 3)
}

// Arguments returns the argument list the converter gives ffmpeg for one recording: the recording
// arrives on the pipe and leaves on it, the tags are the ones the talkgroup has, and the filters are
// the ones this release of ffmpeg and this conversion level ask for.
//
// It is the converter's own argument list, and the build's ffmpeg check uses it to check the
// ffmpeg it compiles, so that the arguments the server runs and the arguments the compiled ffmpeg was
// checked against cannot drift apart.
func Arguments(mode uint, metadata *ports.AudioMetadata, loudnorm bool) []string {
	args := []string{"-i", "-"}

	// The tags are written only for a talkgroup that has a tag, which is what the legacy
	// checks: a call on an untagged talkgroup is converted with no tags at all, not even the
	// title or the moment it happened.
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

// Convert turns one recording into the m4a the server stores.
//
// The disabled level returns the recording untouched without running anything. A converter that
// is not installed is reported the first time a call needs it and left unreported afterwards,
// and in both cases the recording is returned as it arrived. A recording ffmpeg refuses is
// reported with what ffmpeg said, and the recording is again returned as it arrived: a call is
// never lost because its audio would not convert.
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
		// The legacy prints what ffmpeg said onto the server's standard output and reports
		// no failure, so a deployment whose recordings fail to convert sees nothing in its
		// log. Here it is reported, and the recording is kept either way.
		return in, fmt.Errorf("ffmpeg: %s", strings.TrimSpace(stderr.String()))
	}

	return ports.AudioFile{
		Filename: fmt.Sprintf("%v.m4a", strings.TrimSuffix(in.Filename, path.Ext(in.Filename))),
		Mime:     "audio/mp4",
		Data:     stdout.Bytes(),
	}, nil
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
