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
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The golden captures in testdata/golden are what the legacy server put on the wire, and
// they are what this package's marshaling is compared against. These tests do not compare payloads:
// they prove the capture is complete, so that a payload test which finds no golden for a command
// cannot quietly pass without one.
//
// They are pinned bytes, taken from the legacy server at git tree
// 02ae33d4651fee4c46a93bdcf574d0aead89e542. The harness that took them was retired with the legacy
// source, so these files are the contract now rather than something a build step regenerates, and
// docs/specs/FEAT-003-server-ng-refactor/decision-parity-verification.md is why they are the yardstick
// rather than a reading of the legacy source.

const goldenDir = "../../../testdata/golden"

// TestEveryGoldenCaptureIsThere is the completeness half: the files the payload tests read are
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
		"payload-cal-detailed.json",
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

// goldenManifest is what testdata/golden/manifest.json declares about the captures: the legacy tree
// they were taken at, the zone they were taken in, what each redaction stands for, and the one field
// a release is allowed to move.
type goldenManifest struct {
	LegacyServerTree string            `json:"legacyServerTree"`
	Timezone         string            `json:"timezone"`
	Placeholders     map[string]string `json:"placeholders"`

	// ShapeSuperseded names the captures that are no longer the bytes the legacy server wrote, because a
	// feature gave this server something to report that the legacy server never had. The manifest's default
	// is that every capture is the legacy tree's bytes; a capture that is not has to say so here, with the
	// contract that superseded it and the decision that authorized that.
	ShapeSuperseded map[string]struct {
		Contract    string   `json:"contract"`
		DecidedBy   string   `json:"decidedBy"`
		Carries     []string `json:"carries"`
		WhatChanged string   `json:"whatChanged"`
		Unchanged   string   `json:"unchanged"`
	} `json:"shapeSuperseded"`

	VersionField struct {
		Pinned    string   `json:"pinned"`
		MovedFrom string   `json:"movedFrom"`
		MovedBy   string   `json:"movedBy"`
		CarriesIt []string `json:"carriesIt"`
	} `json:"versionField"`
}

func readGoldenManifest(t *testing.T) goldenManifest {
	t.Helper()

	var manifest goldenManifest

	b, err := os.ReadFile(filepath.Join(goldenDir, "manifest.json"))
	if err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}

	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}

	return manifest
}

// TestGoldenCapturesSayWhatEachPlaceholderStandsFor keeps the two redactions honest: a capture that
// silently stopped redacting the session token would produce goldens that differ on every machine,
// and a capture that redacted something else would hide it from the tests that read them.
func TestGoldenCapturesSayWhatEachPlaceholderStandsFor(t *testing.T) {
	manifest := readGoldenManifest(t)

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

// TestGoldenCapturesRecordTheOneFieldAReleaseMoves keeps the version move honest in both directions.
//
// The manifest names which captures carry a version and which version they carry; this checks each
// named capture does carry it, that no capture the manifest does not name carries one, and that the
// version a release moved off is nowhere left behind. A capture that gained a version field nobody
// declared, or a capture that kept an old one, would make a release look like it had not changed what
// it tells clients.
func TestGoldenCapturesRecordTheOneFieldAReleaseMoves(t *testing.T) {
	manifest := readGoldenManifest(t)

	if manifest.VersionField.Pinned == "" {
		t.Fatal("the manifest pins no version, so a reader of a capture cannot tell which release it describes")
	}

	if manifest.VersionField.MovedFrom == "" {
		t.Error("the manifest does not record the version the captures were moved off")
	}

	if manifest.VersionField.MovedBy == "" {
		t.Error("the manifest does not name the decision that moved the version")
	}

	if len(manifest.VersionField.CarriesIt) == 0 {
		t.Fatal("the manifest names no capture that carries a version")
	}

	named := map[string]bool{}

	for _, name := range manifest.VersionField.CarriesIt {
		named[name] = true

		b := readGoldenFile(t, name)

		if !carriesVersion(b, manifest.VersionField.Pinned) {
			t.Errorf("%s is named as carrying a version and does not carry %s", name, manifest.VersionField.Pinned)
		}

		if carriesVersion(b, manifest.VersionField.MovedFrom) {
			t.Errorf("%s still carries %s, the version the manifest says a release moved off", name, manifest.VersionField.MovedFrom)
		}
	}

	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatalf("reading %s: %v", goldenDir, err)
	}

	for _, entry := range entries {
		name := entry.Name()

		if entry.IsDir() || name == "manifest.json" || named[name] {
			continue
		}

		b := readGoldenFile(t, name)

		if bytes.Contains(b, []byte(`"version"`)) {
			t.Errorf("%s carries a version field the manifest does not name", name)
		}

		if carriesVersion(b, manifest.VersionField.MovedFrom) {
			t.Errorf("%s still carries %s, the version the manifest says a release moved off", name, manifest.VersionField.MovedFrom)
		}
	}
}

// TestGoldenCapturesNameTheOnesWhoseShapeThisServerSuperseded keeps the manifest's declaration honest in the
// direction a test can reach: a capture that says it was superseded still exists, the contract and the
// decision that superseded it are still in the repository, and the capture still carries the shape it was
// superseded to. A capture quietly reverted to something else, or a specification moved out from under a
// capture that cites it, is what this catches.
//
// The other direction — a capture drifting away from the legacy server without being declared here — is not
// something a Go test can see, because the legacy server is not running. That is what the parity harness is
// for: it runs both servers over the same scenario and compares what they answer. See testdata/parity.
func TestGoldenCapturesNameTheOnesWhoseShapeThisServerSuperseded(t *testing.T) {
	manifest := readGoldenManifest(t)

	for name, superseded := range manifest.ShapeSuperseded {
		capture := readGoldenFile(t, name)

		if superseded.Contract == "" {
			t.Errorf("%s is declared superseded and names no contract", name)
		}

		if superseded.DecidedBy == "" {
			t.Errorf("%s is declared superseded and names no decision", name)
		}

		if superseded.WhatChanged == "" {
			t.Errorf("%s is declared superseded and does not say what changed", name)
		}

		if superseded.Unchanged == "" {
			t.Errorf("%s is declared superseded and does not say what did not change", name)
		}

		// A capture citing a specification that is no longer there is a capture whose shape nobody owns.
		for _, document := range []string{superseded.Contract, superseded.DecidedBy} {
			if _, err := os.Stat(filepath.Join("..", "..", "..", document)); err != nil {
				t.Errorf("%s says %s superseded it, and that file is not in the repository: %v", name, document, err)
			}
		}

		if len(superseded.Carries) == 0 {
			t.Fatalf("%s is declared superseded and names nothing it has to carry", name)
		}

		for _, fragment := range superseded.Carries {
			if !bytes.Contains(capture, []byte(fragment)) {
				t.Errorf("%s is declared as carrying %s and does not:\n%s", name, fragment, capture)
			}
		}
	}
}

// carriesVersion reports whether a capture names this version as the version it reports, in either of
// the two forms a capture writes it in: a JSON field of its own, or a JSON field inside a captured
// frame, which is a JSON string and therefore escaped.
func carriesVersion(capture []byte, version string) bool {
	for _, form := range []string{`"version":"` + version + `"`, `\"version\":\"` + version + `\"`} {
		if bytes.Contains(capture, []byte(form)) {
			return true
		}
	}

	return false
}

func readGoldenFile(t *testing.T, name string) []byte {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatalf("reading the golden capture %s: %v", name, err)
	}

	return b
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
