//go:build integration

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
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/ports"
)

// installedProber is the prober a deployment with ffprobe gets. These are integration tests
// over the real binary, so a machine that does not have the tool is a failure and not a skip:
// the suite that runs with the `integration` tag is the suite that says the probe works.
func installedProber(t *testing.T) *Prober {
	t.Helper()

	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Fatalf("ffprobe is not on the path: %v", err)
	}

	prober := NewProber(t.TempDir())

	if !prober.available {
		t.Fatal("ffprobe is on the path but the prober reports it is not installed")
	}

	return prober
}

// TestTheProberMeasuresTheRecordingItIsGiven: ffprobe cannot read a length from a stream it
// cannot seek, so the prober writes the audio out, asks about the file, and answers with the
// length of the recording it was handed.
func TestTheProberMeasuresTheRecordingItIsGiven(t *testing.T) {
	prober := installedProber(t)

	length, err := prober.Probe(context.Background(), ports.AudioFile{
		Filename: "call.wav",
		Mime:     "audio/wav",
		Data:     recordedRecording(t),
	})
	if err != nil {
		t.Fatalf("the prober refused the recording: %v", err)
	}

	if length < time.Second || length > 1100*time.Millisecond {
		t.Errorf("the recording measured %v, want the second of tone it is", length)
	}
}

// The probe file is a step on the way to the answer, not a thing the server keeps: the
// folder the probe works in is empty again when the probe ends.
func TestTheProberLeavesNoFileBehind(t *testing.T) {
	dir := t.TempDir()

	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Fatalf("ffprobe is not on the path: %v", err)
	}

	prober := NewProber(dir)

	if _, err := prober.Probe(context.Background(), ports.AudioFile{
		Filename: "call.wav",
		Mime:     "audio/wav",
		Data:     recordedRecording(t),
	}); err != nil {
		t.Fatalf("the prober refused the recording: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the probe folder: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("the probe folder holds %d files after the probe, want none: %v", len(entries), entries)
	}
}

// A recording ffprobe refuses — bytes that are not audio at all — is a probe that cannot
// answer: the prober reports the failure and names no length, and it leaves no file behind
// on this path either.
func TestTheProberReportsARecordingItCannotRead(t *testing.T) {
	dir := t.TempDir()

	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Fatalf("ffprobe is not on the path: %v", err)
	}

	prober := NewProber(dir)

	length, err := prober.Probe(context.Background(), ports.AudioFile{
		Filename: "call.wav",
		Mime:     "audio/wav",
		Data:     []byte("this is not a recording, it is a sentence about a recording"),
	})
	if err == nil {
		t.Fatal("the prober read a length out of bytes that are not audio")
	}

	if length != 0 {
		t.Errorf("the prober named %v for a recording it refused, want no length", length)
	}

	if entries, err := os.ReadDir(dir); err == nil && len(entries) != 0 {
		t.Errorf("the refused recording left %d files in the probe folder, want none", len(entries))
	}
}

// A deployment without ffprobe is a deployment whose calls keep no measured length: the
// reason is said once per server run and the calls keep arriving, exactly as a deployment
// without ffmpeg sees its converter reported.
func TestAMissingProberIsReportedOncePerServerRun(t *testing.T) {
	prober := NewProberAt("/does/not/exist/ffprobe-9.0.2", t.TempDir())

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: recordedRecording(t)}

	_, err := prober.Probe(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "ffprobe is not available") {
		t.Fatalf("the missing prober = %v, want the report a deployment without ffprobe sees", err)
	}

	if _, err := prober.Probe(context.Background(), in); err != nil {
		t.Errorf("the missing prober was reported a second time: %v", err)
	}
}
