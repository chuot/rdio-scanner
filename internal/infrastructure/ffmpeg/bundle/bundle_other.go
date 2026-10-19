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

//go:build !(linux && amd64) && !(linux && arm64) && !(linux && arm) && !(linux && 386) && !(windows && amd64) && !(darwin && amd64) && !(darwin && arm64) && !(freebsd && amd64)

package bundle

import (
	"io/fs"
)

// A target outside the release matrix carries no ffmpeg at all, whatever is sitting in the bundle
// directories: the architectures this project does not release, and every architecture of a release target
// this build has no way to embed. A target this project does release carries the ffmpeg binaries its own
// directory holds, and a supplied target whose directory holds only the placeholder carries none, which the lookup
// reports.
func target() (fs.FS, string) {
	return nil, ""
}
