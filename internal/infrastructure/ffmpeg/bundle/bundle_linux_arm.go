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

//go:build linux && arm

package bundle

import (
	"embed"
	"io/fs"
)

// The generated bundle for linux-arm. On a fresh clone this directory holds only its placeholder, which
// //go:embed all: matches, so a plain go build compiles and the lookup reports no bundle.
//
//go:embed all:linux-arm
var linuxArm embed.FS

func target() (fs.FS, string) {
	return linuxArm, "linux-arm"
}
