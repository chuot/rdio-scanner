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
// own target carries none: macOS, FreeBSD, and every plain `go build` a developer runs.
const theHostFFmpeg = "ffmpeg"

// converterChoice is which ffmpeg this server converts with, and what the operator has to be told
// about it.
type converterChoice struct {
	// Program is the ffmpeg the server runs: the path of the one it carries inside its own binary, or
	// the name ffmpeg when it runs the one its host installs.
	Program string

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

// theConverter decides which ffmpeg a server converts with from what its own bundle produced.
//
// The ffmpeg the server carries wins over whatever the host happens to have installed, because the
// server was built for the host it will run on and its ffmpeg was compiled for that architecture.
// A server that carries none converts with the host's and has nothing to say about it; a server that
// carried one and could not use it converts with the host's too, and says why.
func theConverter(bundled string, failure string) converterChoice {
	if bundled != "" {
		return converterChoice{Program: bundled}
	}

	return converterChoice{Program: theHostFFmpeg, Notice: failure}
}

// bundledFFmpeg writes the ffmpeg this binary carries into the server's own folder and returns the
// path to run it from, or reports why the server will have to do without.
//
// A server whose target carries no ffmpeg is not a failure and reports no reason: it is the ordinary
// case for the targets this project does not compile an ffmpeg for.
func bundledFFmpeg(config *Config) (string, string) {
	bundled, ok := bundle.Lookup()
	if !ok {
		return "", ""
	}

	path, _, err := bundled.Materialize(config.GetFFmpegDirPath())
	if err != nil {
		return "", err.Error()
	}

	return path, ""
}
