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

// The forwarding header is what the server names when the connection came from a proxy the operator
// named, because a server behind a proxy is otherwise described by the proxy's address and every warning
// it writes is about the wrong machine. These tests name the localhost peer as the trusted proxy.
func trustedLocalhost(t *testing.T) *Trusted {
	t.Helper()

	trusted, err := ParseTrusted("127.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseTrusted: %v", err)
	}

	return trusted
}

func TestAddressIsTheFirstForwardedEntryWithoutItsPort(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7:8123, 198.51.100.4")
	r.RemoteAddr = "127.0.0.1:43210"

	if got := Address(r, trustedLocalhost(t)); got != "203.0.113.7" {
		t.Errorf("address = %q, want the first forwarded entry with its port removed", got)
	}
}

// An entry with nothing before its colon is not something the pattern can cut, and the server reports it
// as it arrived rather than moving on to the next entry: the pattern that removes a port needs at least
// one character before the colon it removes, and a forwarding entry that has nothing to keep is kept.
func TestAddressReportsAForwardedEntryThePatternCannotCutAsItArrived(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.Header.Set("X-Forwarded-For", ":8123, 198.51.100.4")
	r.RemoteAddr = "127.0.0.1:43210"

	if got := Address(r, trustedLocalhost(t)); got != ":8123" {
		t.Errorf("address = %q, want the first forwarded entry, which the pattern cannot cut", got)
	}
}

// An address in brackets with its port is cut at the last colon, which is the one that separates the
// port, and so the address a proxy forwards in IPv6 form survives.
func TestAddressKeepsAForwardedAddressInBracketsWhole(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.Header.Set("X-Forwarded-For", "[2001:db8::1]:1234")
	r.RemoteAddr = "127.0.0.1:43210"

	if got := Address(r, trustedLocalhost(t)); got != "[2001:db8::1]" {
		t.Errorf("address = %q, want the bracketed address without its port", got)
	}
}

// With no forwarding header the connection is all the server has, and its port is not part of the address
// the server names.
func TestAddressIsTheConnectionAddressWithoutItsPortWhenNothingIsForwarded(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.RemoteAddr = "127.0.0.1:43210"

	if got := Address(r, nil); got != "127.0.0.1" {
		t.Errorf("address = %q, want the connection address with its port removed", got)
	}
}

// An empty forwarding header is not a forwarded address: the server does not name an empty string, it
// names the connection.
func TestAddressIgnoresAForwardingHeaderThatNamesNothing(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.Header.Set("X-Forwarded-For", "")
	r.RemoteAddr = "127.0.0.1:43210"

	if got := Address(r, nil); got != "127.0.0.1" {
		t.Errorf("address = %q, want the connection address", got)
	}
}

// An address with no colon at all is the address. The pattern that removes a port does not match it, and
// the server reports what it was given rather than nothing.
func TestAddressReportsAnAddressWithNoPortAsItArrived(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.RemoteAddr = "127.0.0.1"

	if got := Address(r, nil); got != "127.0.0.1" {
		t.Errorf("address = %q, want the address as it arrived", got)
	}
}
