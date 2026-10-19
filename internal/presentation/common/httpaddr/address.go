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
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
)

// withoutPort is the pattern the server uses when it names an address: everything before the first colon.
var withoutPort = regexp.MustCompile(`(.+):.*$`)

// Trusted is the list of proxy addresses whose forwarding header the server honors. It is built once
// from the operator's `trusted_proxies` option and shared by every endpoint that names an address.
//
// The forwarding header is a statement the peer makes, not a fact the server knows. A server that
// honors it from anyone lets the client pick the address the login budget and the access warnings are
// keyed on, which is how a brute-force tool rotates its identity. Honoring it only for peers the
// operator named keeps the header's use (a deployment behind a proxy sees the real client) and removes
// the abuse (a direct connection sees its own socket address, which is what the limiter must count).
type Trusted struct {
	prefixes []netip.Prefix
}

// ParseTrusted reads the operator's list: comma-separated CIDR prefixes and bare addresses. A bare
// address becomes the single-address prefix of its own family. A name is refused: a host name would
// make a trust decision depend on DNS, which the server has no business consulting to decide who to
// rate-limit.
func ParseTrusted(spec string) (*Trusted, error) {
	trusted := &Trusted{}

	for _, name := range strings.Split(spec, ",") {
		name = strings.TrimSpace(name)
		if len(name) == 0 {
			continue
		}

		if prefix, err := netip.ParsePrefix(name); err == nil {
			trusted.prefixes = append(trusted.prefixes, prefix)
			continue
		}

		if addr, err := netip.ParseAddr(name); err == nil {
			trusted.prefixes = append(trusted.prefixes, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}

		return nil, fmt.Errorf("trusted proxy %q is not an address or a CIDR prefix", name)
	}

	return trusted, nil
}

// contains reports whether the connection came from one of the named prefixes. A nil list trusts
// nobody, which is the default: no proxy configured, socket address only.
func (trusted *Trusted) contains(peer netip.Addr) bool {
	if trusted == nil {
		return false
	}

	for _, prefix := range trusted.prefixes {
		if prefix.Contains(peer) {
			return true
		}
	}

	return false
}

// peerAddress reads the address of the connection. The transport names it with a port
// ("127.0.0.1:43210", "[2001:db8::1]:43210"); an address with no port is accepted too, because that
// is what a hand-built request carries.
func peerAddress(remoteAddr string) (netip.Addr, bool) {
	if ap, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return ap.Addr(), true
	}

	if addr, err := netip.ParseAddr(remoteAddr); err == nil {
		return addr, true
	}

	return netip.Addr{}, false
}

// Address is the address the server names for a request: the first entry of the forwarding header that
// has something in it, with its port removed, when the connection came from a proxy the operator named,
// and the address of the connection otherwise.
//
// Both halves of the server name addresses this way — the listener that joined, the warning about a
// guessed access code, the login attempts counted against a machine — so that a line about one and a line
// about the other can be read as being about the same machine, and so that what the limiter counts is
// what the operator reads.
//
// The naming itself is the legacy server's GetRemoteAddr, with its quirks intact: a forwarding header is
// split on every comma and the first entry with anything before its colon wins, so an entry that is only
// a port is skipped rather than reported; an address with nothing before its colon is reported exactly as
// it arrived; and only the first colon is treated as the boundary, which leaves an IPv6 address cut at its
// first colon.
func Address(r *http.Request, trusted *Trusted) string {
	if peer, ok := peerAddress(r.RemoteAddr); ok && trusted.contains(peer) {
		for _, address := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
			if host := withoutPort.ReplaceAllString(address, "$1"); len(host) > 0 {
				return host
			}
		}
	}

	if host := withoutPort.ReplaceAllString(r.RemoteAddr, "$1"); len(host) > 0 {
		return host
	}

	return r.RemoteAddr
}
