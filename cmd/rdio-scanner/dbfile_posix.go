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

//go:build linux || android || darwin || freebsd || netbsd || openbsd || dragonfly

package main

import (
	"os"
	"syscall"
)

// fileOwnerUID reports the uid that owns the path, read from the stat the
// operating system already answered the check with: no second system call, and
// the same number stat and ls -l print for that file. The owner is the file's
// own, which is the half a chown changes, and it is a different account from the
// server's whenever the deployment was laid out by somebody else.
func fileOwnerUID(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}

	return int(stat.Uid), true
}
