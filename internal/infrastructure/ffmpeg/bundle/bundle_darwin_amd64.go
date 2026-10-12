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

//go:build darwin && amd64

package bundle

import (
	"embed"
	"io/fs"
)

// The bundle for macos-amd64. This build compiles nothing for that target: the pair is compiled on the
// platform it runs on and dropped in this directory, and the build gzips it here. On a fresh clone, and
// on a release run where the pair was never dropped, the directory holds only its placeholder, which
// //go:embed all: matches, so a plain go build compiles and the lookup reports no bundle.
//
//go:embed all:macos-amd64
var darwinAmd64 embed.FS

func target() (fs.FS, string) {
	return darwinAmd64, "macos-amd64"
}
