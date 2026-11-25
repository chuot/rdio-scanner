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
	"strings"
	"testing"
)

// A Windows file names its owner by a security identifier rather than a numeric
// uid, and this start does not read the file's access control list. The clause
// keeps the half a Windows file reports — the mode its attributes become — and
// names the other half as not named. The runtime reports no uid for a Windows
// process, so the line must not carry one, least of all the -1 the runtime
// answers when asked.
func TestUnwritableFileClauseSaysTheOwnerIsNotNamedForAWindowsFile(t *testing.T) {
	clause := unwritableFileClause(statInfo{name: "rdio-scanner.db", mode: 0o600})

	if want := "mode 0600, owned by an account this system does not name"; clause != want {
		t.Errorf("unwritableFileClause = %q, want %q", clause, want)
	}

	if strings.Contains(clause, "uid") {
		t.Errorf("unwritableFileClause = %q, want no uid in it: this system reports none for the file or for the process", clause)
	}
}
