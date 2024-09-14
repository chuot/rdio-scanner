// Copyright (C) 2019-2024 Chrystian Huot <chrystian@huot.qc.ca>
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

type KeypadBeeps struct {
	Activate   []OscillatorData `json:"activate"`
	Deactivate []OscillatorData `json:"deactivate"`
	Denied     []OscillatorData `json:"denied"`
}

func GetKeypadBeeps(options *Options) KeypadBeeps {
	switch options.KeypadBeeps {
	case "uniden":
		return KeypadBeeps{
			Activate: []OscillatorData{
				{
					Begin:     0,
					End:       0.05,
					Frequency: 1200,
					Kind:      "square",
				},
			},

			Deactivate: []OscillatorData{
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

			Denied: []OscillatorData{
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

	case "whistler":
		return KeypadBeeps{
			Activate: []OscillatorData{
				{
					Begin:     0,
					End:       0.05,
					Frequency: 2000,
					Kind:      "triangle",
				},
			},

			Deactivate: []OscillatorData{
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

			Denied: []OscillatorData{
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

	default:
		return KeypadBeeps{
			Activate:   []OscillatorData{},
			Deactivate: []OscillatorData{},
			Denied:     []OscillatorData{},
		}
	}
}
