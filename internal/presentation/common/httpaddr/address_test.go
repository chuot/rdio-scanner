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

// trustedLocalhost names the localhost peer as the trusted proxy, so
// these tests exercise the forwarded path. see the
// trusted-proxy-selection-is-not-a-security-check maintainer note.
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

// TestAddressReportsAForwardedEntryThePatternCannotCutAsItArrived: a
// forward entry with no port to cut is reported whole. see the
// trusted-proxy-selection-is-not-a-security-check maintainer note.
func TestAddressReportsAForwardedEntryThePatternCannotCutAsItArrived(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.Header.Set("X-Forwarded-For", ":8123, 198.51.100.4")
	r.RemoteAddr = "127.0.0.1:43210"

	if got := Address(r, trustedLocalhost(t)); got != ":8123" {
		t.Errorf("address = %q, want the first forwarded entry, which the pattern cannot cut", got)
	}
}

// TestAddressKeepsAForwardedAddressInBracketsWhole: cut at the port colon. see the
// trusted-proxy-selection-is-not-a-security-check maintainer note.
func TestAddressKeepsAForwardedAddressInBracketsWhole(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.Header.Set("X-Forwarded-For", "[2001:db8::1]:1234")
	r.RemoteAddr = "127.0.0.1:43210"

	if got := Address(r, trustedLocalhost(t)); got != "[2001:db8::1]" {
		t.Errorf("address = %q, want the bracketed address without its port", got)
	}
}

// TestAddressIsTheConnectionAddressWithoutItsPortWhenNothingIsForwarded:
// names the connection, port cut, when no forwarding header is present.
func TestAddressIsTheConnectionAddressWithoutItsPortWhenNothingIsForwarded(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.RemoteAddr = "127.0.0.1:43210"

	if got := Address(r, nil); got != "127.0.0.1" {
		t.Errorf("address = %q, want the connection address with its port removed", got)
	}
}

// TestAddressIgnoresAForwardingHeaderThatNamesNothing: an empty header
// names nothing, so the connection is named.
func TestAddressIgnoresAForwardingHeaderThatNamesNothing(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.Header.Set("X-Forwarded-For", "")
	r.RemoteAddr = "127.0.0.1:43210"

	if got := Address(r, nil); got != "127.0.0.1" {
		t.Errorf("address = %q, want the connection address", got)
	}
}

// TestAddressReportsAnAddressWithNoPortAsItArrived: the port-cutting
// pattern never matches, and what arrived is reported.
func TestAddressReportsAnAddressWithNoPortAsItArrived(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/config", nil)
	r.RemoteAddr = "127.0.0.1"

	if got := Address(r, nil); got != "127.0.0.1" {
		t.Errorf("address = %q, want the address as it arrived", got)
	}
}
