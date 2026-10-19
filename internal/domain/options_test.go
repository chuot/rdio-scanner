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

package domain

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultOptionsMatchTheLegacyServer(t *testing.T) {
	o := DefaultOptions()

	checks := map[string]struct{ got, want any }{
		"AudioConversion":           {o.AudioConversion, AudioConversionEnabled},
		"AutoPopulate":              {o.AutoPopulate, true},
		"DisableDuplicateDetection": {o.DisableDuplicateDetection, false},
		"KeypadBeeps":               {o.KeypadBeeps, "uniden"},
		"MaxClients":                {o.MaxClients, uint(200)},
		"PruneDays":                 {o.PruneDays, uint(7)},
		"ShowListenersCount":        {o.ShowListenersCount, false},
		"SortTalkgroups":            {o.SortTalkgroups, false},
		"AdminPasswordNeedChange":   {o.AdminPasswordNeedChange, true},
	}

	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
}

func TestApplyMapReadsEveryEditableOption(t *testing.T) {
	o := DefaultOptions().ApplyMap(map[string]any{
		"audioConversion":           float64(3),
		"autoPopulate":              false,
		"branding":                  "City of Test",
		"disableDuplicateDetection": true,
		"email":                     "ops@example.com",
		"keypadBeeps":               "whistler",
		"maxClients":                float64(25),
		"pruneDays":                 float64(30),
		"showListenersCount":        true,
		"sortTalkgroups":            true,
	})

	checks := map[string]struct{ got, want any }{
		"AudioConversion":           {o.AudioConversion, uint(3)},
		"AutoPopulate":              {o.AutoPopulate, false},
		"Branding":                  {o.Branding, "City of Test"},
		"DisableDuplicateDetection": {o.DisableDuplicateDetection, true},
		"Email":                     {o.Email, "ops@example.com"},
		"KeypadBeeps":               {o.KeypadBeeps, "whistler"},
		"MaxClients":                {o.MaxClients, uint(25)},
		"PruneDays":                 {o.PruneDays, uint(30)},
		"ShowListenersCount":        {o.ShowListenersCount, true},
		"SortTalkgroups":            {o.SortTalkgroups, true},
	}

	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
}

func TestApplyMapKeepsCurrentValuesForAbsentStrings(t *testing.T) {
	o := DefaultOptions()
	o.Branding = "Keep me"
	o.Email = "keep@example.com"

	o = o.ApplyMap(map[string]any{"pruneDays": float64(3)})

	if o.Branding != "Keep me" {
		t.Errorf("Branding = %q, want the previous value preserved", o.Branding)
	}

	if o.Email != "keep@example.com" {
		t.Errorf("Email = %q, want the previous value preserved", o.Email)
	}
}

func TestApplyMapFallsBackToDefaultsForAbsentOrWronglyTypedValues(t *testing.T) {
	o := DefaultOptions()
	o.PruneDays = 99
	o.AutoPopulate = false
	o.KeypadBeeps = "whistler"

	o = o.ApplyMap(map[string]any{
		"pruneDays":    "not a number",
		"autoPopulate": "not a bool",
		"keypadBeeps":  float64(7),
	})

	if o.PruneDays != 7 {
		t.Errorf("PruneDays = %d, want the default 7", o.PruneDays)
	}

	if !o.AutoPopulate {
		t.Error("AutoPopulate = false, want the default true")
	}

	if o.KeypadBeeps != "uniden" {
		t.Errorf("KeypadBeeps = %q, want the default uniden", o.KeypadBeeps)
	}
}

// TestApplyMapLegacyAudioConversionDefaultQuirk pins a legacy defect and its masking:
// when the submitted configuration has no audioConversion key, the legacy server
// assigns the audio-conversion default to MaxClients — but its own later maxClients
// branch then overwrites that with the maxClients default, so the defect is only
// observable when a maxClients value is actually submitted.
func TestApplyMapLegacyAudioConversionDefaultQuirk(t *testing.T) {
	o := DefaultOptions().ApplyMap(map[string]any{"maxClients": float64(25)})

	if o.MaxClients != 25 {
		t.Errorf("MaxClients = %d, want the submitted 25 to survive the audioConversion default branch", o.MaxClients)
	}

	o = DefaultOptions().ApplyMap(map[string]any{})

	if o.MaxClients != 200 {
		t.Errorf("MaxClients = %d, want the maxClients default 200 to mask the audioConversion default branch", o.MaxClients)
	}
}

// TestApplyMapLegacyDisableAudioConversion maps the retired boolean option onto the
// audio-conversion modes exactly as the legacy server did.
func TestApplyMapLegacyDisableAudioConversion(t *testing.T) {
	if o := DefaultOptions().ApplyMap(map[string]any{"disableAudioConversion": true}); o.AudioConversion != AudioConversionEnabledNorm {
		t.Errorf("AudioConversion = %d, want %d", o.AudioConversion, AudioConversionEnabledNorm)
	}

	if o := DefaultOptions().ApplyMap(map[string]any{"disableAudioConversion": false}); o.AudioConversion != AudioConversionDisabled {
		t.Errorf("AudioConversion = %d, want %d", o.AudioConversion, AudioConversionDisabled)
	}
}

// TestApplyMapIgnoresTheRetiredOptions pins the retirement of the 12-hour format,
// dimmer delay, duplicate detection time frame and playback-goes-live options: a
// submitted body that still carries them (a legacy admin page, a migrated config)
// changes nothing, and the duplicate window stays the domain constant.
func TestApplyMapIgnoresTheRetiredOptions(t *testing.T) {
	o := DefaultOptions().ApplyMap(map[string]any{
		"dimmerDelay":                 float64(9000),
		"duplicateDetectionTimeFrame": float64(1500),
		"playbackGoesLive":            true,
		"time12hFormat":               true,
	})

	if got, want := o.DuplicateDetectionWindow(), 500*time.Millisecond; got != want {
		t.Errorf("DuplicateDetectionWindow = %v, want the 500 ms constant with the submitted time frame ignored", got)
	}
}

func TestDuplicateDetectionWindow(t *testing.T) {
	o := DefaultOptions()

	if got, want := o.DuplicateDetectionWindow(), 500*time.Millisecond; got != want {
		t.Errorf("DuplicateDetectionWindow = %v, want %v", got, want)
	}

	o.DisableDuplicateDetection = true
	if got := o.DuplicateDetectionWindow(); got != 0 {
		t.Errorf("DuplicateDetectionWindow = %v, want 0 when detection is disabled", got)
	}
}

func TestSecretIsGeneratedOnceAndStaysStable(t *testing.T) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&"

	secret := GenerateSecret(256)

	if len([]rune(secret)) != 256 {
		t.Fatalf("secret length = %d, want 256 runes", len([]rune(secret)))
	}

	for _, r := range secret {
		if !containsRune(alphabet, r) {
			t.Errorf("secret contains %q which is outside the allowed alphabet", r)
		}
	}

	if GenerateSecret(256) == secret {
		t.Error("two generated secrets are identical; the secret must be random")
	}
}

// TestOptionsAcceptOnlyASecretTheyCanSignWith puts the rule about a stored signing secret where it
// belongs: on the options. A secret is a secret only at the length the server signs with, and a value
// that cannot sign is not taken.
func TestOptionsAcceptOnlyASecretTheyCanSignWith(t *testing.T) {
	usable := strings.Repeat("a", SecretLength)

	options := DefaultOptions()
	if options.Secret != "" {
		t.Fatalf("a fresh server's secret = %q, want nothing on record until one is made", options.Secret)
	}

	if got := options.AcceptSecret(usable); got.Secret != usable {
		t.Errorf("accepting a %d-character secret left %q, want the stored secret taken", SecretLength, got.Secret)
	}

	for _, unusable := range []string{"", "short", strings.Repeat("a", SecretLength-1), strings.Repeat("a", SecretLength+1)} {
		if got := options.AcceptSecret(unusable); got.Secret != "" {
			t.Errorf("accepting a %d-character value left %q, want the options left as they were", len(unusable), got.Secret)
		}
	}
}

// TestOptionsWithSigningSecretGeneratesOneOnlyWhenTheyCannotSign is the half the legacy server did
// inside its options read: it made a secret whenever the stored one was unusable, and never wrote it
// down. Here the options decide whether they can sign, and say whether they had to make a secret, so
// the caller knows the new one has to be put on record.
func TestOptionsWithSigningSecretGeneratesOneOnlyWhenTheyCannotSign(t *testing.T) {
	options := DefaultOptions()

	withSecret, generated := options.WithSigningSecret()

	if !generated {
		t.Fatal("the options reported no secret generated, want one made for a server that cannot sign")
	}

	if len(withSecret.Secret) != SecretLength {
		t.Errorf("the generated secret is %d characters, want %d", len(withSecret.Secret), SecretLength)
	}

	kept, generated := withSecret.WithSigningSecret()

	if generated {
		t.Error("the options generated a second secret, want the one they already hold")
	}

	if kept.Secret != withSecret.Secret {
		t.Errorf("the secret changed under a call that should have left it alone: %q against %q", kept.Secret, withSecret.Secret)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}

	return false
}
