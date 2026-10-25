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

// Package backend is the scanner webapp's own surface: the WebSocket connection the
// web app and the native applications hold open, the JSON-array frame they exchange,
// the command constants of that protocol, and the hub that keeps the listener
// connections. The wire format is frozen: the commands and their frames are captured
// from the legacy server and compared by the completeness guard in message_test.go;
// see the scanner-wire-format-and-completeness maintainer note. The administrator
// configuration socket is not here; it lives in admin/backend.
package backend
