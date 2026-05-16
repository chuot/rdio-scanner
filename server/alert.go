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

package main

type Alert []OscillatorData

var Alerts = map[string]Alert{
	"alert1": {
		{
			Begin:     0,
			End:       0.05,
			Frequency: 3000,
			Kind:      "square",
		},
		{
			Begin:     0.075,
			End:       0.125,
			Frequency: 3000,
			Kind:      "square",
		},
		{
			Begin:     0.15,
			End:       0.2,
			Frequency: 3000,
			Kind:      "square",
		},
	},

	"alert2": {
		{
			Begin:     0,
			End:       0.05,
			Frequency: 800,
			Kind:      "square",
		},
		{
			Begin:     0.075,
			End:       0.125,
			Frequency: 1000,
			Kind:      "square",
		},
		{
			Begin:     0.15,
			End:       0.2,
			Frequency: 800,
			Kind:      "square",
		},
		{
			Begin:     0.225,
			End:       0.275,
			Frequency: 1000,
			Kind:      "square",
		},
	},

	"alert3": {
		{
			Begin:     0,
			End:       0.05,
			Frequency: 800,
			Kind:      "square",
		},
		{
			Begin:     0.075,
			End:       0.125,
			Frequency: 1000,
			Kind:      "square",
		},
		{
			Begin:     0.15,
			End:       0.175,
			Frequency: 3000,
			Kind:      "square",
		},
		{
			Begin:     0.225,
			End:       0.275,
			Frequency: 3000,
			Kind:      "square",
		},
	},

	"alert4": {
		{
			Begin:     0,
			End:       0.125,
			Frequency: 800,
			Kind:      "square",
		},
		{
			Begin:     0.125,
			End:       0.25,
			Frequency: 1000,
			Kind:      "square",
		},
		{
			Begin:     0.375,
			End:       0.5,
			Frequency: 800,
			Kind:      "square",
		},
		{
			Begin:     0.5,
			End:       0.625,
			Frequency: 1000,
			Kind:      "square",
		},
		{
			Begin:     0.75,
			End:       0.875,
			Frequency: 800,
			Kind:      "square",
		},
		{
			Begin:     0.875,
			End:       1,
			Frequency: 1000,
			Kind:      "square",
		},
		{
			Begin:     1.125,
			End:       1.25,
			Frequency: 800,
			Kind:      "square",
		},
		{
			Begin:     1.250,
			End:       1.325,
			Frequency: 1000,
			Kind:      "square",
		},
	},

	"alert5": {
		{
			Begin:     0,
			End:       1.5,
			Frequency: 1200,
			Kind:      "square",
		},
	},

	"alert6": {
		{
			Begin:     0,
			End:       0.175,
			Frequency: 1200,
			Kind:      "square",
		},
		{
			Begin:     0.225,
			End:       0.3,
			Frequency: 1200,
			Kind:      "square",
		},
	},

	"alert7": {
		{
			Begin:     0,
			End:       0.2,
			Frequency: 2000,
			Kind:      "square",
		},
		{
			Begin:     0.2,
			End:       0.4,
			Frequency: 800,
			Kind:      "square",
		},
		{
			Begin:     0.4,
			End:       0.6,
			Frequency: 2000,
			Kind:      "square",
		},
		{
			Begin:     0.6,
			End:       0.8,
			Frequency: 800,
			Kind:      "square",
		},
		{
			Begin:     0.8,
			End:       1,
			Frequency: 2000,
			Kind:      "square",
		},
		{
			Begin:     1,
			End:       1.2,
			Frequency: 800,
			Kind:      "square",
		},
	},

	"alert8": {
		{
			Begin:     0,
			End:       0.03,
			Frequency: 500,
			Kind:      "square",
		},
		{
			Begin:     0.04,
			End:       0.07,
			Frequency: 500,
			Kind:      "square",
		},
		{
			Begin:     0.08,
			End:       0.11,
			Frequency: 500,
			Kind:      "square",
		},
	},

	"alert9": {
		{
			Begin:     0,
			End:       0.07,
			Frequency: 2400,
			Kind:      "square",
		},
		{
			Begin:     0.09,
			End:       0.16,
			Frequency: 3000,
			Kind:      "square",
		},
		{
			Begin:     0.27,
			End:       0.34,
			Frequency: 2400,
			Kind:      "square",
		},
		{
			Begin:     0.37,
			End:       0.44,
			Frequency: 3000,
			Kind:      "square",
		},
	},
}

func GetAlert(name string) Alert {
	for n := range Alerts {
		if n == name {
			return Alerts[name]
		}
	}

	return []OscillatorData{}
}
