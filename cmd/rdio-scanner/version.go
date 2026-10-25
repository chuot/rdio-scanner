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

// command rdio-scanner is the server's composition root: it loads
// configuration, wires infrastructure adapters into the use cases, and serves
// the HTTP and WebSocket interfaces
// see the legacy-drop-in-contract maintainer note.
package main

// Version is the release version the build injects with -ldflags, resolved
// from VERSION.txt and git, and a binary built any other way reports the dev
// fallback on purpose
// see the buildinfo-version-derivation maintainer note.
var Version = "dev"
