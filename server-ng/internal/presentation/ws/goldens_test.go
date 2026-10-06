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

package ws

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The golden captures in server-ng/testdata/golden are what the legacy server put on the wire, and
// they are what this package's marshaling is compared against. These tests do not compare payloads:
// they prove the capture is complete, so that a payload test which finds no golden for a command
// cannot quietly pass without one.
//
// testdata/capture/README.md is how they are taken, and
// docs/specs/FEAT-003-server-ng-refactor/decision-parity-verification.md is why they are the
// yardstick rather than a reading of the legacy source.

const goldenDir = "../../../testdata/golden"

// TestEveryGoldenCaptureIsThere is the completeness half: the files the differential tests read are
// named here, so a capture that stopped early fails here rather than leaving a payload test with
// nothing to compare against.
func TestEveryGoldenCaptureIsThere(t *testing.T) {
	want := []string{
		"manifest.json",

		// The listener WebSocket, on a server that asks for no access code.
		"payload-ver.json",
		"payload-cfg.json",
		"payload-cal-live.json",
		"payload-cal-fetch.json",
		"payload-lcl.json",
		"ws-frames-open.json",

		// The same WebSocket on a server that does ask for one.
		"payload-cfg-scoped.json",
		"ws-frames-restricted.json",
		"ws-frames-locked.json",
		"ws-frames-missing-call.json",

		// The administrator REST API.
		"rest-login.json",
		"rest-logout-status.txt",
		"rest-config.json",
		"rest-config-unauthorized-status.txt",
		"rest-alerts.json",
		"rest-logs.json",
		"rest-password.json",
		"rest-section-access.json",
		"rest-section-access-restricted.json",
		"rest-section-apikeys.json",
		"rest-section-dirwatch.json",
		"rest-section-downstreams.json",
		"rest-section-groups.json",
		"rest-section-options.json",
		"rest-section-options-put.json",
		"rest-section-systems.json",
		"rest-section-tags.json",

		// The inbound upload endpoints.
		"rest-upload.txt",
		"rest-upload-denied.txt",
		"rest-trunk-recorder-upload.txt",

		// The administrator's config WebSocket.
		"adminws-config.json",
		"adminws-bad-token.json",
	}

	for _, name := range want {
		if _, err := os.Stat(filepath.Join(goldenDir, name)); err != nil {
			t.Errorf("golden capture %s: %v", name, err)
		}
	}
}

// TestGoldenCapturesSayWhatEachPlaceholderStandsFor keeps the two redactions honest: a capture that
// silently stopped redacting the session token would produce goldens that differ on every machine,
// and a capture that redacted something else would hide it from the tests that read them.
func TestGoldenCapturesSayWhatEachPlaceholderStandsFor(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(goldenDir, "manifest.json"))
	if err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}

	var manifest struct {
		LegacyServerTree string            `json:"legacyServerTree"`
		Timezone         string            `json:"timezone"`
		Placeholders     map[string]string `json:"placeholders"`
	}

	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}

	if manifest.LegacyServerTree == "" {
		t.Error("the manifest names no legacy server tree, so the captures are not pinned to a legacy source")
	}

	if manifest.Timezone != "UTC" {
		t.Errorf("the manifest records the timezone %q, want UTC: the legacy renders an instant in the zone it runs in, so captures taken in another zone are not comparable", manifest.Timezone)
	}

	for name, placeholder := range map[string]string{
		"loginToken": "<token>",
		"logTime":    "<datetime>",
	} {
		if got := manifest.Placeholders[name]; got != placeholder {
			t.Errorf("placeholder %s = %q, want %q", name, got, placeholder)
		}
	}
}

// TestGoldenCapturesCoverEveryCommandTheProtocolExchanges is the coverage half of CA-011.1: every
// command docs/specs/FEAT-002-vanilla-client-ng/contract-server-protocol.md §2 lists as something
// the server says must appear in some captured session.
//
// IOS, PID, and SRV are the exception the contract itself makes — native-app only, never handled by
// the web client — and they are asserted absent so that the gap is a recorded one rather than an
// accident of which sessions this capture happened to open.
func TestGoldenCapturesCoverEveryCommandTheProtocolExchanges(t *testing.T) {
	sessions := []string{
		"ws-frames-open.json",
		"ws-frames-restricted.json",
		"ws-frames-locked.json",
		"ws-frames-missing-call.json",
	}

	seen := map[string]int{}

	for _, name := range sessions {
		b, err := os.ReadFile(filepath.Join(goldenDir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}

		// Only the wrapper is parsed. Each frame is a string, and the string is what the server
		// wrote, so the command is read out of the frame rather than out of a decoded message.
		var captured map[string]any

		if err := json.Unmarshal(b, &captured); err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}

		for label, frames := range captured {
			for _, frame := range framesUnder(label, frames) {
				command := commandOf(frame)

				if command == "" {
					t.Errorf("%s/%s: frame %s does not start with a command", name, label, frame)

					continue
				}

				seen[command]++
			}
		}
	}

	for _, command := range []string{"CAL", "CFG", "LCL", "LSC", "LFM", "MAX", "PIN", "VER", "XPR"} {
		if seen[command] == 0 {
			t.Errorf("no captured session contains a %s frame, so nothing here proves what the server says with one", command)
		}
	}

	for _, command := range []string{"IOS", "PID", "SRV"} {
		if seen[command] != 0 {
			t.Errorf("a captured session contains a %s frame, which the contract lists as native-app only and this capture does not drive", command)
		}
	}
}

// framesUnder walks whatever a capture file groups its frames into — one session, several sessions,
// or several sessions each with a list of their own — and returns the frames, which are recorded as
// the strings the server wrote.
func framesUnder(label string, value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}

	case []any:
		out := []string{}

		for _, item := range v {
			out = append(out, framesUnder(label, item)...)
		}

		return out

	default:
		return nil
	}
}

// commandOf reads the command out of a frame exactly as the frame was written: a JSON array whose
// first element is the command.
func commandOf(frame string) string {
	const open = `["`

	if len(frame) < len(open)+1 || frame[:len(open)] != open {
		return ""
	}

	end := len(open)

	for end < len(frame) && frame[end] != '"' {
		end++
	}

	if end == len(open) || end == len(frame) {
		return ""
	}

	return frame[len(open):end]
}
