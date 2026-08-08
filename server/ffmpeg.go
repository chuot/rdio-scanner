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
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var ffmpegCommand = exec.Command

type FFMpeg struct {
	available bool
	version43 bool
	warned    bool
}

func NewFFMpeg() *FFMpeg {
	ffmpeg := &FFMpeg{}

	stdout := bytes.NewBuffer([]byte(nil))

	cmd := exec.Command("ffmpeg", "-version")
	cmd.Stdout = stdout

	if err := cmd.Run(); err == nil {
		ffmpeg.available = true

		if l, err := stdout.ReadString('\n'); err == nil {
			s := regexp.MustCompile(`.*ffmpeg version .{0,1}([0-9])\.([0-9])\.[0-9].*`).ReplaceAllString(strings.TrimSuffix(l, "\n"), "$1.$2")
			v := strings.Split(s, ".")
			if len(v) > 1 {
				if major, err := strconv.Atoi(v[0]); err == nil {
					if minor, err := strconv.Atoi(v[1]); err == nil {
						if major > 4 || (major == 4 && minor >= 3) {
							ffmpeg.version43 = true
						}
					}
				}
			}
		}
	}

	return ffmpeg
}

func (ffmpeg *FFMpeg) Convert(call *Call, systems *Systems, tags *Tags, mode uint) error {
	var (
		err  error
	)

	if mode == AUDIO_CONVERSION_DISABLED {
		return nil
	}

	if !ffmpeg.available {
		if !ffmpeg.warned {
			ffmpeg.warned = true

			return errors.New("ffmpeg is not available, no audio conversion will be performed")
		}
		return nil
	}

	metadata := []string{}
	if tag, ok := tags.GetTagById(call.Talkgroup.TagId); ok {
		metadata = append(metadata,
			"-metadata", fmt.Sprintf("album=%v", call.Talkgroup.Label),
			"-metadata", fmt.Sprintf("artist=%v", call.System.Label),
			"-metadata", fmt.Sprintf("date=%v", call.Timestamp),
			"-metadata", fmt.Sprintf("genre=%v", tag),
			"-metadata", fmt.Sprintf("title=%v", call.Talkgroup.Name),
		)
	}

	args := audioConversionArgs(ffmpeg.version43, mode, metadata)

	cmd := ffmpegCommand("ffmpeg", args...)
	cmd.Stdin = bytes.NewReader(call.Audio)

	stdout := bytes.NewBuffer([]byte(nil))
	cmd.Stdout = stdout

	stderr := bytes.NewBuffer([]byte(nil))
	cmd.Stderr = stderr

	if err = cmd.Run(); err == nil {
		call.Audio = stdout.Bytes()
		call.AudioFilename = fmt.Sprintf("%v.m4a", strings.TrimSuffix(call.AudioFilename, path.Ext((call.AudioFilename))))
		call.AudioMime = "audio/mp4"

	} else {
		status := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			status = exitErr.ExitCode()
		}

		return fmt.Errorf("ffmpeg conversion failed: exit_status=%d input_bytes=%d class=%s", status, len(call.Audio), ffmpegErrorClass(stderr.String()))
	}

	return nil
}

func audioConversionArgs(version43 bool, mode uint, metadata []string) []string {
	args := []string{"-i", "-"}
	args = append(args, metadata...)

	if version43 {
		switch mode {
		case AUDIO_CONVERSION_ENABLED_NORM:
			args = append(args, "-af", "apad=whole_dur=3s,loudnorm")
		case AUDIO_CONVERSION_ENABLED_LOUD_NORM:
			args = append(args, "-af", "apad=whole_dur=3s,loudnorm=I=-16:TP=-1.5:LRA=11")
		}
	}

	return append(args, "-c:a", "aac", "-b:a", "64k", "-movflags", "frag_keyframe+empty_moov", "-f", "ipod", "-")
}

func ffmpegErrorClass(stderr string) string {
	s := strings.ToLower(stderr)

	switch {
	case strings.Contains(s, "does not contain any stream") || strings.Contains(s, "no streams"):
		return "no_stream"
	case strings.Contains(s, "invalid data found when processing input"):
		return "invalid_input"
	case strings.Contains(s, "error opening input"):
		return "input_open"
	case strings.Contains(s, "error opening output") || strings.Contains(s, "invalid argument"):
		return "output_open"
	default:
		return "other"
	}
}
