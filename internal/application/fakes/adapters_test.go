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
	"testing"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

func TestTokensRollingWindowRetiresTheOldest(t *testing.T) {
	tokens := NewTokens()

	issued := []string{}

	for i := 0; i < 6; i++ {
		token, err := tokens.Issue(context.Background())
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}

		issued = append(issued, token)
	}

	if err := tokens.Validate(context.Background(), issued[0]); err == nil {
		t.Error("the first token must be retired once a sixth is issued")
	}

	if err := tokens.Validate(context.Background(), issued[1]); err != nil {
		t.Errorf("the second token must still be accepted: %v", err)
	}

	if err := tokens.Revoke(context.Background(), issued[1]); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if err := tokens.Validate(context.Background(), issued[1]); err == nil {
		t.Error("a revoked token must no longer be accepted")
	}
}

func TestConverterLeavesDisabledAudioAloneAndRenamesConvertedAudio(t *testing.T) {
	converter := NewConverter()

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: []byte("audio")}

	out, err := converter.Convert(context.Background(), in, domain.AudioConversionDisabled, nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if out.Filename != "call.wav" || out.Mime != "audio/wav" {
		t.Errorf("Convert with conversion disabled = %+v, want the input untouched", out)
	}

	out, err = converter.Convert(context.Background(), in, domain.AudioConversionEnabledLoudNorm, &ports.AudioMetadata{Album: "Dispatch"})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if out.Filename != "call.m4a" || out.Mime != "audio/mp4" {
		t.Errorf("Convert = %+v, want the converted name and media type", out)
	}

	if converter.Metadata[1] == nil || converter.Metadata[1].Album != "Dispatch" {
		t.Error("the metadata asked for must reach the converter")
	}

	if converter.Modes[1] != domain.AudioConversionEnabledLoudNorm {
		t.Errorf("Convert recorded mode %d, want the level it was asked for", converter.Modes[1])
	}
}

func TestConverterReportsWhenItIsNotAvailable(t *testing.T) {
	converter := NewConverter()
	converter.Available = false

	_, err := converter.Convert(context.Background(), ports.AudioFile{Filename: "call.wav"}, domain.AudioConversionEnabled, nil)

	if err == nil {
		t.Error("a converter that is not installed must report it, so the use case can keep the original audio")
	}
}
