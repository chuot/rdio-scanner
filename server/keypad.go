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

package main

type KeypadBeep struct {
	Begin     float32 `json:"begin"`
	End       float32 `json:"end"`
	Frequency uint    `json:"frequency"`
	Kind      string  `json:"type"`
}

type KeypadBeeps struct {
	Activate   []KeypadBeep `json:"activate"`
	Deactivate []KeypadBeep `json:"deactivate"`
	Denied     []KeypadBeep `json:"denied"`
}

func GetKeypadBeeps(options *Options) KeypadBeeps {
	var keypadBeeps KeypadBeeps

	switch options.KeypadBeeps {
	case "uniden":
		keypadBeeps = KeypadBeepsUniden
	case "whistler":
		keypadBeeps = KeypadBeepsWhistler
	}

	return keypadBeeps
}

var KeypadBeepsUniden = KeypadBeeps{
	Activate: []KeypadBeep{
		{
			Begin:     0,
			End:       0.05,
			Frequency: 1200,
			Kind:      "square",
		},
	},
	Deactivate: []KeypadBeep{
		{
			Begin:     0,
			End:       0.1,
			Frequency: 1200,
			Kind:      "square",
		},
		{
			Begin:     0.1,
			End:       0.2,
			Frequency: 925,
			Kind:      "square",
		},
	},
	Denied: []KeypadBeep{
		{
			Begin:     0,
			End:       0.05,
			Frequency: 925,
			Kind:      "square",
		},
		{
			Begin:     0.1,
			End:       0.15,
			Frequency: 925,
			Kind:      "square",
		},
	},
}

var KeypadBeepsWhistler = KeypadBeeps{
	Activate: []KeypadBeep{
		{
			Begin:     0,
			End:       0.05,
			Frequency: 2000,
			Kind:      "triangle",
		},
	},
	Deactivate: []KeypadBeep{
		{
			Begin:     0,
			End:       0.04,
			Frequency: 1500,
			Kind:      "triangle",
		},
		{
			Begin:     0.04,
			End:       0.08,
			Frequency: 1400,
			Kind:      "triangle",
		},
	},
	Denied: []KeypadBeep{
		{
			Begin:     0,
			End:       0.04,
			Frequency: 1400,
			Kind:      "triangle",
		},
		{
			Begin:     0.05,
			End:       0.09,
			Frequency: 1400,
			Kind:      "triangle",
		},
		{
			Begin:     0.1,
			End:       0.14,
			Frequency: 1400,
			Kind:      "triangle",
		},
	},
}
