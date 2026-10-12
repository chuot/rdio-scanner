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

package upgrade

import (
	"net/http"
	"testing"
)

// The server tells a request that wants a websocket connection from one that wants an answer by looking at
// one header, and the legacy server does not care how that header is spelled.
func TestWantedReadsTheUpgradeHeaderWhateverItsCase(t *testing.T) {
	for _, spelling := range []string{"websocket", "WebSocket", "WEBSOCKET", "webSocket"} {
		req := request()
		req.Header.Set("Upgrade", spelling)

		if !Wanted(req) {
			t.Errorf("an Upgrade header of %q is not an upgrade request, want it to be", spelling)
		}
	}
}

// A request that says nothing about an upgrade is answered as an ordinary request. This is the test the
// legacy server applies at the two endpoints that can be either a socket or an endpoint: the
// administrator's configuration path, and the path the scanner webapp is served from.
func TestWantedIsNotAskedForByAnythingElse(t *testing.T) {
	cases := []struct {
		name   string
		header string
		value  string
	}{
		{name: "no upgrade header at all"},
		{name: "an upgrade header naming something else", header: "Upgrade", value: "h2c"},
		{name: "an upgrade header with nothing in it", header: "Upgrade", value: ""},
	}

	for _, testCase := range cases {
		req := request()

		if testCase.header != "" {
			req.Header.Set(testCase.header, testCase.value)
		}

		if Wanted(req) {
			t.Errorf("%s is an upgrade request, want it to be answered as an ordinary one", testCase.name)
		}
	}
}

// A request that carries the connection half of an upgrade and not the protocol half is not an upgrade. The
// legacy server never looks at the Connection header, and a proxy that adds one to an ordinary request must
// not turn an endpoint into a socket.
func TestWantedIgnoresAConnectionHeaderThatClaimsAnUpgrade(t *testing.T) {
	req := request()

	req.Header.Set("Connection", "Upgrade")

	if Wanted(req) {
		t.Errorf("a request carrying only Connection: Upgrade is an upgrade request, want it to be answered as " +
			"an ordinary one")
	}
}

func request() *http.Request {
	return &http.Request{Header: http.Header{}}
}
