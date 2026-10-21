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

// The bundle for freebsd-amd64. The build compiles these binaries for that target in a container on the release
// host, against the FreeBSD project's own toolchain image as its sysroot, and drops them here, gzipped: nothing on
// that host runs them, so what they are held to is read out of the files the compile produced. On a fresh clone,
// and on any build that has not compiled or adopted this target's pair, the directory holds only its placeholder,
// which //go:embed all: matches, so a plain go build compiles and the lookup reports no bundle.
//
//go:embed all:freebsd-amd64
var freebsdAmd64 embed.FS

func target() (fs.FS, string) {
	return freebsdAmd64, "freebsd-amd64"
}
