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

// Package backend is the listener application's own surface: the WebSocket connection the web
// app and the native Rdio Scanner applications hold open, the JSON-array frame they exchange,
// the command constants of that protocol, and the hub that keeps the server's listener
// connections.
//
// The wire format is frozen: it is what the native applications speak, and it must not change.
// The commands the protocol has are the ones the protocol-completeness guard in message_test.go
// enumerates, and each one is compared against a capture taken from the legacy server.
//
// The administrator configuration socket is not here. It has no commands, no live feed, no
// history, and no access codes; it lives in admin/backend.
package backend
