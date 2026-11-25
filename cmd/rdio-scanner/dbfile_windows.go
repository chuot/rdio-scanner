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

//go:build windows

package main

import (
	"os"
)

// fileOwnerUID reports no uid: a Windows file carries no POSIX stat, so the uid a
// chown would change is not an answer this system has. The mode the refusal names
// is still the mode the file's read-only attribute became, which is the half of
// the line a Windows operator can act on.
func fileOwnerUID(info os.FileInfo) (int, bool) {
	return 0, false
}
