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

package ports

import (
	"context"
	"time"

	"rdio-scanner/internal/domain"
)

// AudioFile is a chunk of audio together with the name and media type it is presented
// under. Duration is how long the audio lasts — the length the adapter that produced the
// file measured; zero means that adapter cannot say, and the use case asks the prober.
type AudioFile struct {
	Filename string
	Mime     string
	Duration time.Duration
	Data     []byte
}

// AudioMetadata are the tags written into a converted audio file. A nil metadata means
// the file is converted without tags, which is what happens for a talkgroup that has no
// tag.
type AudioMetadata struct {
	Album  string
	Artist string
	Date   time.Time
	Genre  string
	Title  string
}

// AudioConverter turns the audio a call was recorded in into the format the server
// stores and serves.
//
// The mode is one of the domain's audio conversion levels; the disabled level means the
// audio is returned untouched.
//
// An adapter that cannot convert — because the converter is not installed, or because it
// failed on this particular audio — reports an error, and the use case keeps the
// original audio. The legacy server does the same thing silently; this server makes the
// failure visible instead of swallowing it.
type AudioConverter interface {
	Convert(ctx context.Context, in AudioFile, mode uint, metadata *AudioMetadata) (AudioFile, error)
}

// AudioLengthProber reports how long one recording lasts, for the calls the converter
// could not measure: conversion switched off, no converter installed, a conversion that
// failed, or a conversion that stated no running time. The audio it is given is the audio
// the server will store, and an adapter that cannot say — no program installed, a
// recording the program refuses — reports an error and the call keeps no length.
type AudioLengthProber interface {
	Probe(ctx context.Context, in AudioFile) (time.Duration, error)
}

// DownstreamTarget is one downstream server to re-post a call to.
type DownstreamTarget struct {
	URL    string
	Apikey string
}

// DownstreamForwarder re-posts a recorded call to another Rdio Scanner server.
type DownstreamForwarder interface {
	Forward(ctx context.Context, target DownstreamTarget, call domain.Call) error
}

// TokenIssuer issues and checks the administrator session tokens.
//
// A token is a signed, non-expiring credential, and only the tokens this process has
// issued in its current rolling window are accepted: issuing a sixth token retires the
// first, and revoking a token removes it from the window.
type TokenIssuer interface {
	Issue(ctx context.Context) (string, error)
	Validate(ctx context.Context, token string) error
	Revoke(ctx context.Context, token string) error
}

// Clock is the current time, injected so that rules about time can be tested without
// waiting for it.
type Clock interface {
	Now() time.Time
}
