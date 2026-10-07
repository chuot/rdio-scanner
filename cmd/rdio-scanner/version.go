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

// Command rdio-scanner is this server's composition root: it loads configuration,
// wires the concrete infrastructure adapters into the application use cases, and
// serves the HTTP and WebSocket interfaces.
//
// It is a drop-in for the legacy server: the same configuration files, the same
// database a legacy deployment left behind, and the same interfaces clients already
// speak to — including the version it reports, which is pinned in testdata/golden/.
package main

// Version is the release version this binary reports. `make sed` stamps it from the
// Makefile's `ver :=`; the value committed here is the one the pinned wire contract
// carries, and TestVersionMatchesThePinnedWireContract holds the two together.
const Version = "8.0.0"
