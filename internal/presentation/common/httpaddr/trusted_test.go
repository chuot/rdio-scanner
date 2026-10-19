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

package httpaddr

import (
	"net/http/httptest"
	"testing"
)

// The forwarding header is a statement the peer makes, not a fact the server knows. It is honored
// only when the connection came from a proxy the operator named: a server that trusts the header
// from anyone lets a client pick the address its rate limits and log lines are keyed on.

func TestAddressHonorsTheForwardedEntryFromATrustedProxy(t *testing.T) {
	trusted, err := ParseTrusted("127.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseTrusted: %v", err)
	}

	r := httptest.NewRequest("GET", "/api/admin/login", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7:8123, 198.51.100.4")
	r.RemoteAddr = "127.0.0.1:43210"

	if got := Address(r, trusted); got != "203.0.113.7" {
		t.Errorf("address = %q, want the forwarded entry because the peer is a trusted proxy", got)
	}
}

func TestAddressIgnoresTheForwardedEntryFromAnUntrustedPeer(t *testing.T) {
	trusted, err := ParseTrusted("127.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseTrusted: %v", err)
	}

	r := httptest.NewRequest("GET", "/api/admin/login", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.RemoteAddr = "198.51.100.5:43210"

	if got := Address(r, trusted); got != "198.51.100.5" {
		t.Errorf("address = %q, want the connection address because the peer is not a trusted proxy", got)
	}
}

func TestAddressIgnoresTheForwardedEntryWhenNoProxyIsNamed(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/login", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.RemoteAddr = "198.51.100.5:43210"

	if got := Address(r, nil); got != "198.51.100.5" {
		t.Errorf("address = %q, want the connection address when no trusted proxy is configured", got)
	}
}

func TestAddressHonorsTheForwardedEntryFromAnExactIPv6Proxy(t *testing.T) {
	trusted, err := ParseTrusted("2001:db8::1")
	if err != nil {
		t.Fatalf("ParseTrusted: %v", err)
	}

	r := httptest.NewRequest("GET", "/api/admin/login", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.RemoteAddr = "[2001:db8::1]:43210"

	if got := Address(r, trusted); got != "203.0.113.7" {
		t.Errorf("address = %q, want the forwarded entry because the peer matches the named address", got)
	}
}

// A connection address with no port is still the connection's address, and a named proxy still
// covers it.
func TestAddressTrustsAPeerNamedWithoutItsPort(t *testing.T) {
	trusted, err := ParseTrusted("127.0.0.1")
	if err != nil {
		t.Fatalf("ParseTrusted: %v", err)
	}

	r := httptest.NewRequest("GET", "/api/admin/login", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.RemoteAddr = "127.0.0.1"

	if got := Address(r, trusted); got != "203.0.113.7" {
		t.Errorf("address = %q, want the forwarded entry for a peer named without its port", got)
	}
}

func TestParseTrustedReadsCIDRsAndBareAddresses(t *testing.T) {
	trusted, err := ParseTrusted("127.0.0.1, ::1, 10.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseTrusted: %v", err)
	}

	for _, peer := range []string{"127.0.0.1:1", "10.9.8.7:1", "[::1]:1"} {
		r := httptest.NewRequest("GET", "/api/admin/login", nil)
		r.Header.Set("X-Forwarded-For", "203.0.113.7")
		r.RemoteAddr = peer

		if got := Address(r, trusted); got != "203.0.113.7" {
			t.Errorf("peer %s: address = %q, want the forwarded entry", peer, got)
		}
	}
}

func TestParseTrustedRejectsANameThatIsNotAnAddress(t *testing.T) {
	if _, err := ParseTrusted("proxy.example.com"); err == nil {
		t.Error("ParseTrusted accepted a host name, want a rejection: a name invites a DNS-dependent trust decision")
	}
}

func TestParseTrustedAcceptsAnEmptyList(t *testing.T) {
	trusted, err := ParseTrusted("")
	if err != nil {
		t.Fatalf("ParseTrusted: %v", err)
	}

	r := httptest.NewRequest("GET", "/api/admin/login", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.RemoteAddr = "198.51.100.5:43210"

	if got := Address(r, trusted); got != "198.51.100.5" {
		t.Errorf("address = %q, want the connection address for an empty list", got)
	}
}
