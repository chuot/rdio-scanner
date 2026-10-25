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
	"rdio-scanner/internal/infrastructure/ffmpeg"
	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// theHostFFmpeg is the ffmpeg a host installs for itself, used when the
// server's own bundle produced nothing.
// see the ffmpeg-bundle-provenance maintainer note.
const theHostFFmpeg = "ffmpeg"

// theHostFFprobe is this host's own ffprobe, used for length probes.
// see the ffmpeg-bundle-provenance maintainer note.
const theHostFFprobe = "ffprobe"

// converterChoice is which ffmpeg this server converts with, which ffprobe it measures lengths
// with, and what the operator has to be told about them.
type converterChoice struct {
	// Program is the ffmpeg the server runs: the path of the one it carries inside its own binary, or
	// the name ffmpeg when it runs the one its host installs.
	Program string

	// ProbeProgram is the ffprobe the server measures lengths with: the path of the one it carries
	// inside its own binary, or the name ffprobe when it runs the one its host installs.
	ProbeProgram string

	// Notice is what the operator has to be told about the converter, and is empty when there is
	// nothing to say.
	Notice string
}

// converter builds the converter this server converts with.
func (choice converterChoice) converter() *ffmpeg.Converter {
	if choice.Program == theHostFFmpeg {
		return ffmpeg.New()
	}

	return ffmpeg.NewAt(choice.Program)
}

// prober builds the prober for this server's ffprobe, working in the
// folder of the server's programs: probe files share it and are removed.
func (choice converterChoice) prober(workDir string) *ffmpeg.Prober {
	if choice.ProbeProgram == theHostFFprobe {
		return ffmpeg.NewProber(workDir)
	}

	return ffmpeg.NewProberAt(choice.ProbeProgram, workDir)
}

// theConverter picks the ffmpeg and ffprobe from what the binary's
// bundle produced: a carried pair wins over the host's install, and a
// pair that could not be used falls back to the host's and says why.
// see the ffmpeg-bundle-provenance maintainer note.
func theConverter(bundled string, bundledProbe string, failure string) converterChoice {
	if bundled != "" && bundledProbe != "" {
		return converterChoice{Program: bundled, ProbeProgram: bundledProbe}
	}

	return converterChoice{Program: theHostFFmpeg, ProbeProgram: theHostFFprobe, Notice: failure}
}

// bundledFFmpeg writes the pair this binary carries into the given
// folder and returns their run paths, or reports why they are absent.
// A target with no bundled ffmpeg is the ordinary case, not a failure.
func bundledFFmpeg(dir string) (string, string, string) {
	bundled, ok := bundle.Lookup()
	if !ok {
		return "", "", ""
	}

	ffmpegPath, ffprobePath, err := bundled.Materialize(dir)
	if err != nil {
		return "", "", err.Error()
	}

	return ffmpegPath, ffprobePath, ""
}
