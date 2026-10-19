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

// Package upgrade says whether a request is asking to become a websocket connection.
//
// It lives on its own because two endpoints of this server can be either a socket or an ordinary endpoint —
// the administrator's configuration path, and the path the scanner webapp is served from — and a server
// whose two endpoints disagree about what an upgrade request looks like answers the same request two
// different ways depending on which one it reached first.
package upgrade

import (
	"net/http"
	"strings"
)

// Wanted reports whether a request is asking for a websocket upgrade.
//
// It is the legacy server's own test, applied to one header and to nothing else: the Upgrade header, read
// without regard to how it is spelled. The Connection header a proxy may add is not looked at, so a request
// that only claims to be upgradable is answered as the ordinary request it is.
func Wanted(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("upgrade"), "websocket")
}
