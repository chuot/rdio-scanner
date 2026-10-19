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

//go:build freebsd && amd64

package bundle

import (
	"embed"
	"io/fs"
)

// The bundle for freebsd-amd64. This build compiles the ffmpeg binaries for that target on the machine that is the
// target, and adopts them from there: they are compiled where they run, dropped in this directory, and
// gzipped here. On a fresh clone, and on a release run on a machine that is not that platform with nothing
// dropped for it, the directory holds only its placeholder, which
// //go:embed all: matches, so a plain go build compiles and the lookup reports no bundle.
//
//go:embed all:freebsd-amd64
var freebsdAmd64 embed.FS

func target() (fs.FS, string) {
	return freebsdAmd64, "freebsd-amd64"
}
