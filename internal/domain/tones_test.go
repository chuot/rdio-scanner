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
	"fmt"
	"testing"
)

// The alert and keypad tone tables were copied out of the legacy server verbatim, and these two
// tests used to prove it by reading server/alert.go and server/keypad.go. The legacy source is not
// in this repository, so both tests skipped — every day, since the legacy trees were deleted — and
// a test that always skips asserts nothing.
//
// The authority is the recorded tables in tones_fixtures_test.go, taken from git tree
// 02ae33d4651fee4c46a93bdcf574d0aead89e542 and spelled with the same decimal literals the legacy
// source spells, so the float32 values compared here are the float32 values the legacy server built.

func TestAlertTablesMatchTheLegacyServerVerbatim(t *testing.T) {
	legacy := alertTables

	alerts := Alerts()

	if len(alerts) != 9 {
		t.Fatalf("Alerts() defines %d alerts, want the nine built-in ones", len(alerts))
	}

	var ours []Oscillator

	for i := 1; i <= 9; i++ {
		ours = append(ours, alerts[fmt.Sprintf("alert%d", i)]...)
	}

	assertOscillatorsEqual(t, legacy, ours, "alert tables")
}

func TestKeypadBeepTablesMatchTheLegacyServerVerbatim(t *testing.T) {
	legacy := keypadBeepTables

	uniden := KeypadBeepsFor(Options{KeypadBeeps: KeypadBeepsUniden})
	whistler := KeypadBeepsFor(Options{KeypadBeeps: KeypadBeepsWhistler})

	ours := append([]Oscillator{}, uniden.Activate...)
	ours = append(ours, uniden.Deactivate...)
	ours = append(ours, uniden.Denied...)
	ours = append(ours, whistler.Activate...)
	ours = append(ours, whistler.Deactivate...)
	ours = append(ours, whistler.Denied...)

	assertOscillatorsEqual(t, legacy, ours, "keypad beep tables")
}

func TestUnknownAlertNameYieldsAnEmptySequence(t *testing.T) {
	if alert := AlertByName("alert10"); len(alert) != 0 {
		t.Errorf("AlertByName(alert10) = %v, want an empty sequence", alert)
	}
}

func TestUnknownKeypadBeepSelectionYieldsEmptySequences(t *testing.T) {
	beeps := KeypadBeepsFor(Options{KeypadBeeps: "motorbot"})

	if len(beeps.Activate)+len(beeps.Deactivate)+len(beeps.Denied) != 0 {
		t.Errorf("KeypadBeepsFor(motorbot) = %+v, want three empty sequences", beeps)
	}
}

func assertOscillatorsEqual(t *testing.T, want, got []Oscillator, what string) {
	t.Helper()

	if len(want) != len(got) {
		t.Fatalf("%s define %d tones, legacy defines %d", what, len(got), len(want))
	}

	for i := range want {
		if want[i] != got[i] {
			t.Errorf("%s tone %d = %+v, legacy defines %+v", what, i+1, got[i], want[i])
		}
	}
}
