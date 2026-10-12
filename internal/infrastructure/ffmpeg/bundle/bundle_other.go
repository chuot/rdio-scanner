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

//go:build !(linux && amd64) && !(linux && arm64) && !(linux && arm) && !(linux && 386) && !(windows && amd64) && !(darwin && amd64) && !(darwin && arm64) && !(freebsd && amd64)

package bundle

import (
	"io/fs"
)

// A target this project ships no ffmpeg for carries none, whatever is sitting in the bundle directories:
// the platforms outside the release matrix, and every architecture of a release target whose pair this
// build carries no way to embed.
func target() (fs.FS, string) {
	return nil, ""
}
