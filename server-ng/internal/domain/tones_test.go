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
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// oscillatorPattern matches one oscillator literal in the legacy tone tables.
var oscillatorPattern = regexp.MustCompile(`(?s)\{\s*Begin:\s*([0-9.]+),\s*End:\s*([0-9.]+),\s*Frequency:\s*(\d+),\s*Kind:\s*"(\w+)",\s*\}`)

func TestAlertTablesMatchTheLegacyServerVerbatim(t *testing.T) {
	legacy := readLegacyOscillators(t, "alert.go")

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
	legacy := readLegacyOscillators(t, "keypad.go")

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

func readLegacyOscillators(t *testing.T, name string) []Oscillator {
	t.Helper()

	path := filepath.Join("..", "..", "..", "server", name)

	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("legacy %s not available: %v", name, err)
	}

	var out []Oscillator

	for _, m := range oscillatorPattern.FindAllStringSubmatch(string(src), -1) {
		begin, err := strconv.ParseFloat(m[1], 32)
		if err != nil {
			t.Fatalf("parsing Begin %q in %s: %v", m[1], name, err)
		}

		end, err := strconv.ParseFloat(m[2], 32)
		if err != nil {
			t.Fatalf("parsing End %q in %s: %v", m[2], name, err)
		}

		frequency, err := strconv.ParseUint(m[3], 10, 32)
		if err != nil {
			t.Fatalf("parsing Frequency %q in %s: %v", m[3], name, err)
		}

		out = append(out, Oscillator{
			Begin:     float32(begin),
			End:       float32(end),
			Frequency: uint(frequency),
			Kind:      m[4],
		})
	}

	if len(out) == 0 {
		t.Fatalf("no oscillator found in legacy %s; the pattern no longer matches the legacy source", name)
	}

	return out
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
