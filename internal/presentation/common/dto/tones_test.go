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

package dto

import (
	"testing"
)

// The tone tables are the payload: they reach the wire through the
// configuration payload and the alert endpoint. This file proves the
// tables themselves: the nine names, the bytes each marshals to, and the
// three keypad states for each brand the server knows.

// TestTheNineAlertTablesAreTheCapturedAlertEndpoint proves each alert
// table marshals to the bytes the legacy server answered, name by name,
// tone by tone, in order.
func TestTheNineAlertTablesAreTheCapturedAlertEndpoint(t *testing.T) {
	if got := jsonOf(t, alertTones); got != capturedAnswer(t, "rest-alerts.json") {
		t.Errorf("the alert tables marshal to %s, want the captured alert endpoint", got)
	}

	if len(alertTones) != 9 {
		t.Errorf("the alert table carries %d entries, want the nine the server carries", len(alertTones))
	}
}

// TestTheKeypadBeepTablesAreTheThreeStatesForEachBrand proves the brand
// the server was told picks the table, and an unknown brand gets three
// empty lists.
func TestTheKeypadBeepTablesAreTheThreeStatesForEachBrand(t *testing.T) {
	for _, brand := range []struct{ option, want string }{
		{
			option: "uniden",
			want:   `{"activate":[{"begin":0,"end":0.05,"frequency":1200,"type":"square"}],"deactivate":[{"begin":0,"end":0.1,"frequency":1200,"type":"square"},{"begin":0.1,"end":0.2,"frequency":925,"type":"square"}],"denied":[{"begin":0,"end":0.05,"frequency":925,"type":"square"},{"begin":0.1,"end":0.15,"frequency":925,"type":"square"}]}`,
		},
		{
			option: "whistler",
			want:   `{"activate":[{"begin":0,"end":0.05,"frequency":2000,"type":"triangle"}],"deactivate":[{"begin":0,"end":0.04,"frequency":1500,"type":"triangle"},{"begin":0.04,"end":0.08,"frequency":1400,"type":"triangle"}],"denied":[{"begin":0,"end":0.04,"frequency":1400,"type":"triangle"},{"begin":0.05,"end":0.09,"frequency":1400,"type":"triangle"},{"begin":0.1,"end":0.14,"frequency":1400,"type":"triangle"}]}`,
		},
		{
			option: "nobody",
			want:   `{"activate":[],"deactivate":[],"denied":[]}`,
		},
	} {
		if got := jsonOf(t, keypadBeeps(brand.option)); got != brand.want {
			t.Errorf("the keypad beeps for %s marshal to %s, want %s", brand.option, got, brand.want)
		}
	}
}

// TestTheToneKeysAreTheFourTheClientsRead proves one tone carries the four
// keys, in the order the clients that read them expect, because any other
// shape is a payload the clients cannot parse.
func TestTheToneKeysAreTheFourTheClientsRead(t *testing.T) {
	tone := Tone{Begin: 0.05, End: 0.1, Frequency: 1200, Kind: "square"}

	if got := jsonOf(t, tone); got != `{"begin":0.05,"end":0.1,"frequency":1200,"type":"square"}` {
		t.Errorf("a tone marshals to %s, want the four keys in the order the clients read them", got)
	}
}
