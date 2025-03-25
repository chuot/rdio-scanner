// Copyright (C) 2019-2022 Chrystian Huot <chrystian.huot@saubeo.solutions>
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
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
)

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

func (ffmpeg *FFMpeg) Convert(call *Call, systems *Systems, tags *Tags, mode uint, compression uint) error {
	var (
		args = []string{"-i", "-"}
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

	if system, ok := systems.GetSystem(call.System); ok {
		if talkgroup, ok := system.Talkgroups.GetTalkgroup(call.Talkgroup); ok {
			if tag, ok := tags.GetTag(talkgroup.TagId); ok {
				args = append(args,
					"-metadata", fmt.Sprintf("album=%v", talkgroup.Label),
					"-metadata", fmt.Sprintf("artist=%v", system.Label),
					"-metadata", fmt.Sprintf("date=%v", call.DateTime),
					"-metadata", fmt.Sprintf("genre=%v", tag),
					"-metadata", fmt.Sprintf("title=%v", talkgroup.Name),
				)
			}
		}
	}

	if ffmpeg.version43 {
		if mode == AUDIO_CONVERSION_ENABLED_NORM {
			args = append(args, "-af", "apad=whole_dur=3s,loudnorm")
		} else if mode == AUDIO_CONVERSION_ENABLED_LOUD_NORM {
			args = append(args, "-af", "apad=whole_dur=3s,loudnorm=I=-16:TP=-1.5:LRA=11")
		}
	}

	if compression == AUDIO_COMPRESSION_LOW {
		args = append(args, "-ar", "32k", "-c:a", "libfdk_aac", "-b:a", "32k")
	} else if compression == AUDIO_COMPRESSION_MEDIUM {
		args = append(args, "-ar", "24k", "-c:a", "libfdk_aac", "-b:a", "24k")
	} else if compression == AUDIO_COMPRESSION_HIGH {
		args = append(args, "-ar", "16k", "-c:a", "libfdk_aac", "-b:a", "16k")
	} else if compression == AUDIO_COMPRESSION_ULTRA {
		args = append(args, "-ar", "32k", "-c:a", "libfdk_aac", "-profile:a", "aac_he", "-b:a", "12k")
	} else if compression == AUDIO_COMPRESSION_EXTREME {
		args = append(args, "-ar", "24k", "-c:a", "libfdk_aac", "-profile:a", "aac_he", "-b:a", "8k")
	} else {
		args = append(args, "-ar", "24k", "-c:a", "libfdk_aac", "-b:a", "24k")
	}

	args = append(args, "-movflags", "frag_keyframe+empty_moov", "-f", "ipod", "-")

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdin = bytes.NewReader(call.Audio)

	stdout := bytes.NewBuffer([]byte(nil))
	cmd.Stdout = stdout

	stderr := bytes.NewBuffer([]byte(nil))
	cmd.Stderr = stderr

	if err = cmd.Run(); err == nil {
		call.Audio = stdout.Bytes()
		call.AudioType = "audio/mp4"

		switch v := call.AudioName.(type) {
		case string:
			call.AudioName = fmt.Sprintf("%v.m4a", strings.TrimSuffix(v, path.Ext((v))))
		}

	} else {
		fmt.Println(stderr.String())
	}

	return nil
}
