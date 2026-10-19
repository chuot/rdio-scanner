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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path"
	"sync"
	"time"

	"rdio-scanner/internal/application/ports"
)

// Prober is the audio length prober that runs ffprobe.
//
// It is the fallback for the calls the converter could not measure: conversion switched off,
// no ffmpeg installed, a conversion that failed, a conversion that stated no running time.
// ffprobe cannot read a length from a stream it cannot seek, so the prober writes the audio
// out to a temporary file in the folder it works in, asks about that file, and removes it
// again — the probe file is a step on the way to the answer and never a thing the server
// keeps.
//
// Whether ffprobe is installed is decided once when the server starts, the same rule the
// converter runs by: a deployment that installs ffprobe while the server is running measures
// nothing until it restarts.
type Prober struct {
	// program is the ffprobe this prober runs: the one the server carries inside its own
	// binary, or the name `ffprobe` when the server's target carries none and the host's is
	// used.
	program string

	// workDir is the folder the probe files are written into — the same folder the server
	// writes its programs into — created on first use and emptied by every probe.
	workDir string

	// available is whether the version probe — `ffprobe -hide_banner -version` — ran at all.
	available bool

	// warned records that the missing prober has already been reported: one line per server
	// run, and silence after it, which is what a deployment without ffprobe sees.
	warned bool

	// mutex guards warned and the probe result against the concurrent handlers that read
	// them, the same guard the converter holds.
	mutex sync.Mutex
}

// NewProber builds the prober for the ffprobe this deployment installs, which is the one
// found on the PATH, working in the folder the server was given.
func NewProber(workDir string) *Prober {
	return NewProberAt("ffprobe", workDir)
}

// NewProberAt builds the prober for the ffprobe at the path the server was given, which is
// the one it carries inside its own binary and writes out to its folder before running it.
func NewProberAt(program string, workDir string) *Prober {
	prober := &Prober{program: program, workDir: workDir}

	command := exec.Command(program, "-hide_banner", "-version")

	if err := command.Run(); err == nil {
		prober.available = true
	}

	return prober
}

// probedRecordingFormat is the part of the ffprobe answer this prober asked for. The recipe
// enables the json muxer for exactly this, and it is the recipe the build's ffmpeg check
// probes converted recordings with, so the runtime probe and the build gate read recordings
// the same way.
type probedRecordingFormat struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Probe reports how long one recording lasts.
//
// The audio is written to a temporary file, ffprobe is asked for the duration of that file,
// and the file is removed on every way this ends — the answer, the refusal, and the failure.
// A recording ffprobe cannot read, a format whose duration ffprobe cannot name, and a
// deployment without ffprobe each report an error and name no length: the call is stored
// with the length unknown, never with a guessed one.
func (prober *Prober) Probe(ctx context.Context, in ports.AudioFile) (time.Duration, error) {
	if missing, err := prober.notInstalled(); missing {
		return 0, err
	}

	if err := os.MkdirAll(prober.workDir, 0755); err != nil {
		return 0, fmt.Errorf("the server cannot prepare %s for a probe: %w", prober.workDir, err)
	}

	temporary, err := os.CreateTemp(prober.workDir, "probe-*"+path.Ext(in.Filename))
	if err != nil {
		return 0, fmt.Errorf("the server cannot prepare a probe file in %s: %w", prober.workDir, err)
	}

	defer os.Remove(temporary.Name())

	if _, err := temporary.Write(in.Data); err != nil {
		temporary.Close()

		return 0, fmt.Errorf("the server cannot write the recording it is probing: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return 0, fmt.Errorf("the server cannot finish writing the recording it is probing: %w", err)
	}

	command := exec.CommandContext(ctx, prober.program,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "json",
		temporary.Name(),
	)

	out, err := command.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}

	var probed probedRecordingFormat
	if err := json.Unmarshal(out, &probed); err != nil {
		return 0, fmt.Errorf("ffprobe: it said something unreadable about the recording: %w", err)
	}

	if probed.Format.Duration == "" {
		return 0, errors.New("ffprobe: the recording names no duration")
	}

	var seconds float64
	if _, err := fmt.Sscanf(probed.Format.Duration, "%g", &seconds); err != nil {
		return 0, fmt.Errorf("ffprobe: the duration %q is not a count of seconds", probed.Format.Duration)
	}

	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, fmt.Errorf("ffprobe: the duration %q is not a length", probed.Format.Duration)
	}

	// ffprobe counts seconds to the microsecond; the server keeps the same count in the
	// duration it carries.
	return time.Duration(math.Round(seconds*1_000_000)) * time.Microsecond, nil
}

// Available reports whether the program this prober was given ran its probe.
//
// It is what the status view shows: whether this deployment measures the length of a recording at all.
// A prober that is not there is the same fact the startup warning states, and that warning is said once
// per server run while this is read again every time the status is asked for.
func (prober *Prober) Available() bool {
	prober.mutex.Lock()
	defer prober.mutex.Unlock()

	return prober.available
}

// notInstalled reports whether this deployment has no prober, and the reason to say so —
// which is said once per server run and left unsaid afterwards.
func (prober *Prober) notInstalled() (bool, error) {
	prober.mutex.Lock()
	defer prober.mutex.Unlock()

	if prober.available {
		return false, nil
	}

	if prober.warned {
		return true, nil
	}

	prober.warned = true

	return true, errors.New("ffprobe is not available, no audio length will be measured")
}
