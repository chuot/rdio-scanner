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
	"rdio-scanner/internal/infrastructure/ffmpeg"
	"rdio-scanner/internal/infrastructure/ffmpeg/bundle"
)

// theHostFFmpeg is the ffmpeg a deployment installs for itself, which is what a server runs when its
// own bundle produced nothing: a plain `go build` a developer runs, and a target whose pair was never
// built for it. Every release archive carries its own pair, so a release deployment never takes this
// branch.
const theHostFFmpeg = "ffmpeg"

// theHostFFprobe is the ffprobe that same deployment installs, measured the same way: the server
// probes lengths with the ffprobe of the pair it carries, and with its host's when it carries none.
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

// prober builds the prober this server measures lengths with, working in the folder the server
// writes its programs into: the probe files share that folder with the programs, and the probe
// removes them when the probe ends.
func (choice converterChoice) prober(workDir string) *ffmpeg.Prober {
	if choice.ProbeProgram == theHostFFprobe {
		return ffmpeg.NewProber(workDir)
	}

	return ffmpeg.NewProberAt(choice.ProbeProgram, workDir)
}

// theConverter decides which ffmpeg a server converts with, and which ffprobe measures its
// lengths, from what its own bundle produced.
//
// The ffmpeg the server carries wins over whatever the host happens to have installed, because the
// server was built for the host it will run on and its ffmpeg was compiled for that architecture.
// A server that carries none converts with the host's and has nothing to say about it; a server that
// carried one and could not use it converts with the host's too, and says why. The ffprobe travels
// the same branch: the pair the bundle materializes is one decision, not two programs decided
// apart.
func theConverter(bundled string, bundledProbe string, failure string) converterChoice {
	if bundled != "" && bundledProbe != "" {
		return converterChoice{Program: bundled, ProbeProgram: bundledProbe}
	}

	return converterChoice{Program: theHostFFmpeg, ProbeProgram: theHostFFprobe, Notice: failure}
}

// bundledFFmpeg writes the pair this binary carries into the folder the server was given for it and
// returns the paths to run them from, or reports why the server will have to do without.
//
// A server whose target carries no ffmpeg is not a failure and reports no reason: it is the ordinary
// case for the targets this project does not compile an ffmpeg for.
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
