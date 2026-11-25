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
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The two uid numbers in the clause answer different questions: the first says
// who owns the file, the second says who the server is. The case the refusal
// exists for is a file another account owns, so the owner has to come from the
// path and never from the process. Both stay numbers, which is what an operator's
// chown and a container image's user account are written as; neither is looked up
// as a name.
func TestUnwritableFileClauseNamesTheModeTheFilesOwnerAndTheRunningUID(t *testing.T) {
	owner := os.Getuid() + 1

	info := statInfo{
		name: "rdio-scanner.db",
		mode: 0o600,
		sys:  &syscall.Stat_t{Uid: uint32(owner)},
	}

	clause := unwritableFileClause(info)

	want := fmt.Sprintf("mode 0600, owned by uid %d; the server runs as uid %d", owner, os.Getuid())

	if clause != want {
		t.Errorf("unwritableFileClause = %q, want %q: the file's owner and the server's own uid are two different answers", clause, want)
	}
}

// the whole line, as the guide quotes it: one line naming the path and the three
// numbers, from the guard a start actually runs.
func TestTheFileRefusalNamesTheModeTheOwnerAndTheRunningUID(t *testing.T) {
	cfg, _ := newTestConfig(t)
	dbFile := filepath.Join(cfg.BaseDir, "rdio-scanner.db")

	seedDatabaseFile(t, dbFile, 0o444)

	err := refuseUnwritableDatabaseFile(cfg)
	if err == nil {
		t.Fatalf("refuseUnwritableDatabaseFile said nothing about a database file at mode 0444: want the start refused")
	}

	refusal := err.Error()

	info, statErr := os.Lstat(dbFile)
	if statErr != nil {
		t.Fatalf("os.Lstat(%s): %v", dbFile, statErr)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("os.Lstat(%s).Sys() is %T, want a stat this system reports a uid from", dbFile, info.Sys())
	}

	want := fmt.Sprintf("no write permissions in the database file %s (mode 0444, owned by uid %d; the server runs as uid %d)", dbFile, stat.Uid, os.Getuid())

	if refusal != want {
		t.Errorf("error = %q, want %q: the refusal is quoted by the guide, so it is the line the code emits and no other", refusal, want)
	}
}
