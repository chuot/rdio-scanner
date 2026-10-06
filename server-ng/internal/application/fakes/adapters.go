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

package fakes

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

// Converter is an in-memory audio converter. It renames and re-types the audio the way
// the real converter does, without producing real audio.
type Converter struct {
	// Converted records each conversion, in the order they were asked for.
	Converted []ports.AudioFile

	// Metadata records the tags each conversion was asked to write, nil where none were
	// asked for.
	Metadata []*ports.AudioMetadata

	// Modes records the conversion level each conversion was asked for.
	Modes []uint

	// Err, when set, is returned by every conversion.
	Err error

	// Available, when false, makes the converter report that it is not installed.
	Available bool
}

var _ ports.AudioConverter = (*Converter)(nil)

// NewConverter returns a converter that works.
func NewConverter() *Converter {
	return &Converter{Available: true}
}

// Convert records the conversion and returns the audio under the converted name and
// media type, which is what the real converter ends up storing.
func (converter *Converter) Convert(ctx context.Context, in ports.AudioFile, mode uint, metadata *ports.AudioMetadata) (ports.AudioFile, error) {
	converter.Converted = append(converter.Converted, in)
	converter.Metadata = append(converter.Metadata, metadata)
	converter.Modes = append(converter.Modes, mode)

	if converter.Err != nil {
		return ports.AudioFile{}, converter.Err
	}

	if !converter.Available {
		return ports.AudioFile{}, errors.New("audio conversion is not available")
	}

	if mode == domain.AudioConversionDisabled {
		return in, nil
	}

	out := in
	out.Filename = strings.TrimSuffix(in.Filename, extensionOf(in.Filename)) + ".m4a"
	out.Mime = "audio/mp4"

	return out, nil
}

func extensionOf(name string) string {
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return ""
	}

	return name[dot:]
}

// Forwarder is an in-memory downstream forwarder.
//
// It can be made to behave like a downstream server that refuses a call (Err) and like one
// that takes its time (Started and Release): a test closes Release when it wants the re-post
// to finish, so that how long the use case waits for it is something the test decides rather
// than something it hopes about.
type Forwarder struct {
	// Forwarded records each re-post, with the target it went to.
	Forwarded []ForwardedCall

	// Err, when set, is what every re-post reports.
	Err error

	// Started, when set, is closed the first time a re-post is entered.
	Started chan struct{}

	// Finished, when set, is closed the first time a re-post returns.
	Finished chan struct{}

	// Release, when set, is waited for before a re-post finishes. Closing it lets every
	// waiting re-post finish.
	Release chan struct{}

	entered  sync.Once
	finished sync.Once

	mutex sync.Mutex
}

// ForwardedCall is one recorded re-post.
type ForwardedCall struct {
	Target ports.DownstreamTarget
	Call   domain.Call
}

var _ ports.DownstreamForwarder = (*Forwarder)(nil)

// Forward records a re-post.
func (forwarder *Forwarder) Forward(ctx context.Context, target ports.DownstreamTarget, call domain.Call) error {
	forwarder.mutex.Lock()
	forwarder.Forwarded = append(forwarder.Forwarded, ForwardedCall{Target: target, Call: call})
	forwarder.mutex.Unlock()

	if forwarder.Started != nil {
		forwarder.entered.Do(func() { close(forwarder.Started) })
	}

	if forwarder.Release != nil {
		<-forwarder.Release
	}

	if forwarder.Finished != nil {
		forwarder.finished.Do(func() { close(forwarder.Finished) })
	}

	return forwarder.Err
}

// Reposts returns the re-posts recorded so far.
func (forwarder *Forwarder) Reposts() []ForwardedCall {
	forwarder.mutex.Lock()
	defer forwarder.mutex.Unlock()

	return append([]ForwardedCall{}, forwarder.Forwarded...)
}

// Tokens is an in-memory token issuer with the same rolling window the real one has:
// only the five most recently issued tokens are accepted, and issuing a sixth retires
// the first.
type Tokens struct {
	// Issued records every token that was issued, including the retired ones.
	Issued []string

	// Window is the set of tokens that are currently accepted, oldest first.
	Window []string

	// Err, when set, is returned by Issue instead of a token.
	Err error
}

var _ ports.TokenIssuer = (*Tokens)(nil)

// NewTokens returns an issuer with an empty window.
func NewTokens() *Tokens {
	return &Tokens{}
}

// Issue mints the next token and puts it at the end of the rolling window.
func (tokens *Tokens) Issue(ctx context.Context) (string, error) {
	if tokens.Err != nil {
		return "", tokens.Err
	}

	token := "token-" + strconv.Itoa(len(tokens.Issued)+1)

	tokens.Issued = append(tokens.Issued, token)

	if len(tokens.Window) >= 5 {
		tokens.Window = append(tokens.Window[1:], token)
	} else {
		tokens.Window = append(tokens.Window, token)
	}

	return token, nil
}

// Validate accepts a token only while it is still inside the rolling window.
func (tokens *Tokens) Validate(ctx context.Context, token string) error {
	for _, held := range tokens.Window {
		if held == token {
			return nil
		}
	}

	return errors.New("token is not accepted")
}

// Revoke removes a token from the rolling window.
func (tokens *Tokens) Revoke(ctx context.Context, token string) error {
	kept := []string{}

	for _, held := range tokens.Window {
		if held != token {
			kept = append(kept, held)
		}
	}

	tokens.Window = kept

	return nil
}

// Clock is an injectable current time.
type Clock struct {
	// At is the time the clock reports. Tests move it forward to make time pass.
	At time.Time
}

var _ ports.Clock = (*Clock)(nil)

// NewClock returns a clock fixed at the given instant.
func NewClock(at time.Time) *Clock {
	return &Clock{At: at}
}

// Now reports the fixed instant.
func (clock *Clock) Now() time.Time {
	return clock.At
}
