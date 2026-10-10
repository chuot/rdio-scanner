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
// speak to — including the version it reports, which is pinned in the captures recorded beside the
// tests that prove them.
package main

// Version is the release version this binary reports. cmd/build replaces it at build time with
// -ldflags -X main.Version=<version>, where the tool resolves the version from VERSION.txt and git.
//
// A binary built by any other route reports this fallback, and reports it in its banner: `dev` is not
// a version any release ever had, which is the point. It is not a version a pinned wire contract can
// pin either, which is why TestVersionMatchesThePinnedWireContract compares the core the build system
// resolves rather than the value compiled into the binary it happens to be running.
var Version = "dev"
