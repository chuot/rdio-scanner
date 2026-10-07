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

// Oscillator is one tone of a synthesized alert or keypad beep: a frequency held
// between two offsets, in seconds, from a waveform.
type Oscillator struct {
	Begin     float32
	End       float32
	Frequency uint
	Kind      string
}

// Alert is a named tone sequence a talkgroup, group or system can be assigned.
type Alert []Oscillator

// keypadBeeps names the two supported beep sets.
const (
	KeypadBeepsUniden   = "uniden"
	KeypadBeepsWhistler = "whistler"
)

// KeypadBeeps are the tones a listener's keypad actions synthesize.
type KeypadBeeps struct {
	Activate   []Oscillator
	Deactivate []Oscillator
	Denied     []Oscillator
}

// Alerts returns the nine built-in alert sequences. The tables are copied verbatim
// from the legacy server: they are audible product behavior, consumed by the web and
// native clients, and a test compares them against the legacy source.
func Alerts() map[string]Alert {
	return map[string]Alert{
		"alert1": {
			{Begin: 0, End: 0.05, Frequency: 3000, Kind: "square"},
			{Begin: 0.075, End: 0.125, Frequency: 3000, Kind: "square"},
			{Begin: 0.15, End: 0.2, Frequency: 3000, Kind: "square"},
		},

		"alert2": {
			{Begin: 0, End: 0.05, Frequency: 800, Kind: "square"},
			{Begin: 0.075, End: 0.125, Frequency: 1000, Kind: "square"},
			{Begin: 0.15, End: 0.2, Frequency: 800, Kind: "square"},
			{Begin: 0.225, End: 0.275, Frequency: 1000, Kind: "square"},
		},

		"alert3": {
			{Begin: 0, End: 0.05, Frequency: 800, Kind: "square"},
			{Begin: 0.075, End: 0.125, Frequency: 1000, Kind: "square"},
			{Begin: 0.15, End: 0.175, Frequency: 3000, Kind: "square"},
			{Begin: 0.225, End: 0.275, Frequency: 3000, Kind: "square"},
		},

		"alert4": {
			{Begin: 0, End: 0.125, Frequency: 800, Kind: "square"},
			{Begin: 0.125, End: 0.25, Frequency: 1000, Kind: "square"},
			{Begin: 0.375, End: 0.5, Frequency: 800, Kind: "square"},
			{Begin: 0.5, End: 0.625, Frequency: 1000, Kind: "square"},
			{Begin: 0.75, End: 0.875, Frequency: 800, Kind: "square"},
			{Begin: 0.875, End: 1, Frequency: 1000, Kind: "square"},
			{Begin: 1.125, End: 1.25, Frequency: 800, Kind: "square"},
			{Begin: 1.250, End: 1.325, Frequency: 1000, Kind: "square"},
		},

		"alert5": {
			{Begin: 0, End: 1.5, Frequency: 1200, Kind: "square"},
		},

		"alert6": {
			{Begin: 0, End: 0.175, Frequency: 1200, Kind: "square"},
			{Begin: 0.225, End: 0.3, Frequency: 1200, Kind: "square"},
		},

		"alert7": {
			{Begin: 0, End: 0.2, Frequency: 2000, Kind: "square"},
			{Begin: 0.2, End: 0.4, Frequency: 800, Kind: "square"},
			{Begin: 0.4, End: 0.6, Frequency: 2000, Kind: "square"},
			{Begin: 0.6, End: 0.8, Frequency: 800, Kind: "square"},
			{Begin: 0.8, End: 1, Frequency: 2000, Kind: "square"},
			{Begin: 1, End: 1.2, Frequency: 800, Kind: "square"},
		},

		"alert8": {
			{Begin: 0, End: 0.03, Frequency: 500, Kind: "square"},
			{Begin: 0.04, End: 0.07, Frequency: 500, Kind: "square"},
			{Begin: 0.08, End: 0.11, Frequency: 500, Kind: "square"},
		},

		"alert9": {
			{Begin: 0, End: 0.07, Frequency: 2400, Kind: "square"},
			{Begin: 0.09, End: 0.16, Frequency: 3000, Kind: "square"},
			{Begin: 0.27, End: 0.34, Frequency: 2400, Kind: "square"},
			{Begin: 0.37, End: 0.44, Frequency: 3000, Kind: "square"},
		},
	}
}

// AlertByName returns the named alert, or an empty sequence when the name is unknown.
func AlertByName(name string) Alert {
	alerts := Alerts()

	if alert, ok := alerts[name]; ok {
		return alert
	}

	return Alert{}
}

// KeypadBeepsFor returns the beep set selected by the server option, with empty
// sequences for any other value.
func KeypadBeepsFor(options Options) KeypadBeeps {
	switch options.KeypadBeeps {
	case KeypadBeepsUniden:
		return KeypadBeeps{
			Activate: []Oscillator{
				{Begin: 0, End: 0.05, Frequency: 1200, Kind: "square"},
			},

			Deactivate: []Oscillator{
				{Begin: 0, End: 0.1, Frequency: 1200, Kind: "square"},
				{Begin: 0.1, End: 0.2, Frequency: 925, Kind: "square"},
			},

			Denied: []Oscillator{
				{Begin: 0, End: 0.05, Frequency: 925, Kind: "square"},
				{Begin: 0.1, End: 0.15, Frequency: 925, Kind: "square"},
			},
		}

	case KeypadBeepsWhistler:
		return KeypadBeeps{
			Activate: []Oscillator{
				{Begin: 0, End: 0.05, Frequency: 2000, Kind: "triangle"},
			},

			Deactivate: []Oscillator{
				{Begin: 0, End: 0.04, Frequency: 1500, Kind: "triangle"},
				{Begin: 0.04, End: 0.08, Frequency: 1400, Kind: "triangle"},
			},

			Denied: []Oscillator{
				{Begin: 0, End: 0.04, Frequency: 1400, Kind: "triangle"},
				{Begin: 0.05, End: 0.09, Frequency: 1400, Kind: "triangle"},
				{Begin: 0.1, End: 0.14, Frequency: 1400, Kind: "triangle"},
			},
		}

	default:
		return KeypadBeeps{
			Activate:   []Oscillator{},
			Deactivate: []Oscillator{},
			Denied:     []Oscillator{},
		}
	}
}
