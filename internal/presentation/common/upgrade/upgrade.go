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

// Package upgrade says whether a request asks to become a websocket
// connection. It lives on its own because two endpoints must read an
// upgrade request the same way. see the
// upgrade-required-is-decided-elsewhere maintainer note.
package upgrade

import (
	"net/http"
	"strings"
)

// Wanted reports whether a request asks for a websocket upgrade: the
// legacy server's own test, the Upgrade header read without regard to
// spelling, and nothing else. see the
// upgrade-required-is-decided-elsewhere maintainer note.
func Wanted(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("upgrade"), "websocket")
}
