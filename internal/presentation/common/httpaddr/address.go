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

// Trusted is the list of proxy addresses whose forwarding header the
// server honors, built once from the trusted_proxies option, and the
// header is the peer's statement, not a fact. see the
// trusted-proxy-selection-is-not-a-security-check maintainer note.
type Trusted struct {
	prefixes []netip.Prefix
}

// ParseTrusted reads the operator's list: comma-separated CIDR prefixes
// and bare addresses, a bare address becoming its own single-address
// prefix. Names are refused, so no trust decision depends on DNS.
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

// contains reports whether the peer matches a named prefix; a nil list
// trusts nobody. see the trusted-proxy-selection-is-not-a-security-check maintainer note.
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

// peerAddress reads the connection address. The transport names it with
// a port; a portless address is accepted too, as a hand-built request
// carries one.
func peerAddress(remoteAddr string) (netip.Addr, bool) {
	if ap, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return ap.Addr(), true
	}

	if addr, err := netip.ParseAddr(remoteAddr); err == nil {
		return addr, true
	}

	return netip.Addr{}, false
}

// Address names the machine a request came from: when the peer is a
// named proxy, the first nonempty forwarding entry minus its port,
// otherwise the connection address. see the
// trusted-proxy-selection-is-not-a-security-check maintainer note.
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
