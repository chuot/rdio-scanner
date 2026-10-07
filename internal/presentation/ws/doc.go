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

// Package ws implements the listener WebSocket interface: the catch-all upgrade on
// the page URL, the JSON-array frame [command, payload?, flag?], the command
// constants exchanged with the web and native clients, the client hub, and the
// per-client livefeed matrix.
//
// The wire format is frozen by docs/specs/FEAT-002-vanilla-client-ng/contract-server-protocol.md
// and is consumed by the native Rdio Scanner applications; it must not change.
package ws
