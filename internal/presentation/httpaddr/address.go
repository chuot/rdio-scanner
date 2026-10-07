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

package httpaddr

import (
	"net/http"
	"regexp"
	"strings"
)

// withoutPort is the pattern the server uses when it names an address: everything before the first colon.
var withoutPort = regexp.MustCompile(`(.+):.*$`)

// Address is the address the server names for a request: the first entry of the forwarding header that
// has something in it, with its port removed, and the address of the connection when the header names
// nothing.
//
// Both halves of the server name addresses this way — the listener that joined, the warning about a
// guessed access code, the login attempts counted against a machine — so that a line about one and a line
// about the other can be read as being about the same machine.
//
// This is the legacy server's GetRemoteAddr, with its quirks intact: a forwarding header is split on
// every comma and the first entry with anything before its colon wins, so an entry that is only a port is
// skipped rather than reported; an address with nothing before its colon is reported exactly as it
// arrived; and only the first colon is treated as the boundary, which leaves an IPv6 address cut at its
// first colon.
func Address(r *http.Request) string {
	for _, address := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
		if host := withoutPort.ReplaceAllString(address, "$1"); len(host) > 0 {
			return host
		}
	}

	if host := withoutPort.ReplaceAllString(r.RemoteAddr, "$1"); len(host) > 0 {
		return host
	}

	return r.RemoteAddr
}
