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

//go:build darwin && arm64

package bundle

import (
	"embed"
	"io/fs"
)

// The bundle for macos-arm64. The build compiles these binaries in a container on the machine the build runs
// on, against the Apple SDK tree its maintainer prepares, and publishes them here, gzipped: nothing on that host
// starts them, so what they are held to is read out of the files the compile produced. A Mac compiling this
// target instead uses its own compiler, linker and signer, and a pair its maintainer built by hand is dropped
// here and adopted on a macOS host, after being run there; either way the pair lands gzipped in this directory.
// On a fresh clone, and on any build that has not compiled or adopted this target's pair, the directory holds only
// its placeholder, which //go:embed all: matches, so a plain go build compiles and the lookup reports no bundle.
//
//go:embed all:macos-arm64
var darwinArm64 embed.FS

func target() (fs.FS, string) {
	return darwinArm64, "macos-arm64"
}
