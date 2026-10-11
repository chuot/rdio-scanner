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

package domain

import (
	"crypto/rand"
	"math/big"
	"time"
)

// Audio conversion modes, shared with the administrator configuration payload.
const (
	AudioConversionDisabled        uint = 0
	AudioConversionEnabled         uint = 1
	AudioConversionEnabledNorm     uint = 2
	AudioConversionEnabledLoudNorm uint = 3
)

// DefaultAdminPassword is the initial administrator password; a server still using it
// reports that the password needs changing.
const DefaultAdminPassword = "rdio-scanner"

// DuplicateDetectionWindowMs is how close two calls on the same system, talkgroup and
// site must be to count as duplicates. The option that used to tune it is retired; the
// window is a domain constant.
const DuplicateDetectionWindowMs = 500

// SecretLength is the length of the signing secret persisted in the options table.
const SecretLength = 256

const secretAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&"

// Options holds the server-wide business settings and their defaults.
type Options struct {
	AudioConversion           uint
	AutoPopulate              bool
	Branding                  string
	DisableDuplicateDetection bool
	Email                     string
	KeypadBeeps               string
	MaxClients                uint
	PruneDays                 uint
	ShowListenersCount        bool
	SortTalkgroups            bool

	// AdminPasswordHash is the bcrypt hash produced outside the domain; the domain
	// only stores and reports it.
	AdminPasswordHash       string
	AdminPasswordNeedChange bool

	// Secret signs administrator session tokens.
	Secret string
}

// DefaultOptions returns the option values a fresh server starts with.
func DefaultOptions() Options {
	return Options{
		AudioConversion:           AudioConversionEnabled,
		AutoPopulate:              true,
		DisableDuplicateDetection: false,
		KeypadBeeps:               "uniden",
		MaxClients:                200,
		PruneDays:                 7,
		ShowListenersCount:        false,
		SortTalkgroups:            false,

		AdminPasswordNeedChange: true,
	}
}

// AcceptSecret takes the signing secret a stored configuration holds.
//
// A secret is a secret only at the length the server signs with: a value of any other length — the
// short string a legacy database actually holds, or the empty string one a server that has never saved
// its configuration holds — cannot sign a token, so it is not taken and the options keep what they had.
func (options Options) AcceptSecret(value string) Options {
	if len(value) != SecretLength {
		return options
	}

	options.Secret = value

	return options
}

// WithSigningSecret gives the options a secret they can sign sessions with, making a fresh one when
// they hold nothing usable.
//
// It reports whether a secret was made, which is what tells the caller the new one has to be put on
// record: a secret that only exists in this process is gone at the next start, and every token issued
// with it dies with it.
func (options Options) WithSigningSecret() (Options, bool) {
	if len(options.Secret) == SecretLength {
		return options, false
	}

	options.Secret = GenerateSecret(SecretLength)

	return options, true
}

// ApplyMap merges an administrator configuration payload into the options. Absent or
// wrongly typed keys fall back to the defaults, except the free-text branding and
// email which keep their current value, matching the legacy server key by key.
func (options Options) ApplyMap(m map[string]any) Options {
	if v, ok := m["audioConversion"].(float64); ok {
		options.AudioConversion = uint(v)
	} else {
		// Legacy quirk: the default branch assigns the audio-conversion default to
		// MaxClients. Reproduced for configuration round-trip parity.
		options.MaxClients = uint(AudioConversionEnabled)
	}

	if v, ok := m["autoPopulate"].(bool); ok {
		options.AutoPopulate = v
	} else {
		options.AutoPopulate = true
	}

	if v, ok := m["branding"].(string); ok {
		options.Branding = v
	}

	// Retired legacy option: the old boolean flag maps onto the conversion modes.
	if v, ok := m["disableAudioConversion"].(bool); ok {
		if v {
			options.AudioConversion = AudioConversionEnabledNorm
		} else {
			options.AudioConversion = AudioConversionDisabled
		}
	}

	if v, ok := m["disableDuplicateDetection"].(bool); ok {
		options.DisableDuplicateDetection = v
	} else {
		options.DisableDuplicateDetection = false
	}

	if v, ok := m["email"].(string); ok {
		options.Email = v
	}

	if v, ok := m["keypadBeeps"].(string); ok {
		options.KeypadBeeps = v
	} else {
		options.KeypadBeeps = "uniden"
	}

	if v, ok := m["maxClients"].(float64); ok {
		options.MaxClients = uint(v)
	} else {
		options.MaxClients = 200
	}

	if v, ok := m["pruneDays"].(float64); ok {
		options.PruneDays = uint(v)
	} else {
		options.PruneDays = 7
	}

	if v, ok := m["showListenersCount"].(bool); ok {
		options.ShowListenersCount = v
	} else {
		options.ShowListenersCount = false
	}

	if v, ok := m["sortTalkgroups"].(bool); ok {
		options.SortTalkgroups = v
	} else {
		options.SortTalkgroups = false
	}

	return options
}

// DuplicateDetectionWindow is how close two calls must be to count as duplicates; a
// zero window means detection is disabled. The window is the domain constant.
func (options Options) DuplicateDetectionWindow() time.Duration {
	if options.DisableDuplicateDetection {
		return 0
	}

	return time.Duration(DuplicateDetectionWindowMs) * time.Millisecond
}

// PruneBefore is the instant before which calls and logs are old enough to be pruned.
func (options Options) PruneBefore(now time.Time) time.Time {
	return now.Add(-time.Duration(options.PruneDays) * 24 * time.Hour)
}

// GenerateSecret builds a new signing secret from the legacy alphabet.
//
// The picks come from crypto/rand: math/rand is seeded by the clock, so
// an attacker who knows roughly when the server first started can reproduce the
// secret that start wrote, and a signing secret is exactly the value an attacker
// wants — it opens every session. The alphabet and the length are the legacy
// server's, unchanged.
//
// A read that fails is the operating system's entropy pool failing. The standard
// library's own crypto/rand.Read answers that by panicking, and this follows it:
// a server that cannot make a signing secret must not fall back to a weak one, and
// it must not do so in silence.
func GenerateSecret(n int) string {
	secret := make([]rune, n)
	alphabet := []rune(secretAlphabet)

	span := big.NewInt(int64(len(alphabet)))

	for i := range secret {
		pick, err := rand.Int(rand.Reader, span)
		if err != nil {
			panic("domain: generating the signing secret: " + err.Error())
		}

		secret[i] = alphabet[pick.Int64()]
	}

	return string(secret)
}
