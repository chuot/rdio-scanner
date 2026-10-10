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
	"rdio-scanner/internal/app"
	"context"
	"os"
	"path/filepath"
	"testing"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// aProgramTheServerCarries is a bundled ffmpeg the test controls: it answers the version probe and
// converts by saying who it is, so a test can tell which ffmpeg actually converted the recording.
func aProgramTheServerCarries(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ffmpeg-9.0.2")

	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"-version\" ]; then\n" +
		"    echo \"ffmpeg version 9.0.2 Copyright (c) 2000-2026 the FFmpeg developers\"\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\n" +
		"cat > /dev/null\n" +
		"printf 'converted by the ffmpeg the server carries'\n"

	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("the test cannot write the ffmpeg the server carries: %v", err)
	}

	return path
}

func aRecording() ports.AudioFile {
	return ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: []byte("audio")}
}

// The whole point of carrying an ffmpeg is that the server converts with the one it carries, and not
// with whatever the host happens to have installed.
func TestTheServerConvertsWithTheFfmpegItCarriesInsideItsOwnBinary(t *testing.T) {
	bundled := aProgramTheServerCarries(t)

	choice := theConverter(bundled, "")

	if choice.Program != bundled {
		t.Errorf("the server chose %s, want the ffmpeg it carries at %s", choice.Program, bundled)
	}

	if choice.Notice != "" {
		t.Errorf("the server has something to say about a converter it chose: %q", choice.Notice)
	}

	converted, err := choice.converter().Convert(context.Background(), aRecording(), domain.AudioConversionEnabled, nil)
	if err != nil {
		t.Fatalf("the server refused to convert a recording: %v", err)
	}

	if string(converted.Data) != "converted by the ffmpeg the server carries" {
		t.Errorf("the recording was converted by %q, not by the ffmpeg the server carries", string(converted.Data))
	}
}

// macOS, FreeBSD, and every plain go build a developer runs carry no ffmpeg of their own, and those
// deployments convert with the ffmpeg their host installs, which is what they have always done.
func TestAServerWhoseTargetCarriesNoFfmpegConvertsWithTheOneItsHostInstalls(t *testing.T) {
	choice := theConverter("", "")

	if choice.Program != theHostFFmpeg {
		t.Errorf("the server chose %s, want the ffmpeg its host installs", choice.Program)
	}

	if choice.Notice != "" {
		t.Errorf("a server that never had an ffmpeg of its own has something to say: %q", choice.Notice)
	}
}

// A server that carries an ffmpeg and cannot write it out is a broken deployment, and the operator
// has to be told why: it says the reason once and converts with the host's ffmpeg rather than
// refusing to record.
func TestAServerThatCannotWriteOutItsFfmpegSaysWhyAndConvertsWithTheHosts(t *testing.T) {
	failure := "the server cannot prepare /data/ffmpeg for its ffmpeg"

	choice := theConverter("", failure)

	if choice.Program != theHostFFmpeg {
		t.Errorf("the server chose %s, want the ffmpeg its host installs after its own failed", choice.Program)
	}

	if choice.Notice != failure {
		t.Errorf("the server says %q about its ffmpeg, want %q", choice.Notice, failure)
	}
}

// The data directory is where the server puts the ffmpeg it carries, in the same way it puts the
// autocert cache and the database.
func TestTheServerWritesTheFfmpegItCarriesIntoItsOwnFolder(t *testing.T) {
	config := &Config{Config: app.Config{BaseDir: t.TempDir()}}

	if got := config.GetFFmpegDirPath(); got != filepath.Join(config.BaseDir, "ffmpeg") {
		t.Errorf("the server's ffmpeg folder is %s, want %s", got, filepath.Join(config.BaseDir, "ffmpeg"))
	}
}
