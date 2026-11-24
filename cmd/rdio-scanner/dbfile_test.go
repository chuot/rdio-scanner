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

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// canWriteFile is the whole decision the guard rests on, and its two answers are
// the shapes the refusals divide on: a file this process can write, and one it
// cannot. The mode bits are not asked, because a file the account running the
// server may not write is unwritable whatever its bits claim.
func TestCanWriteFileAnswersByTheAttemptNotTheMode(t *testing.T) {
	dir := t.TempDir()

	writable := filepath.Join(dir, "writable.db")

	if err := os.WriteFile(writable, []byte("page"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", writable, err)
	}

	if !canWriteFile(writable) {
		t.Errorf("canWriteFile(%s) is false, want it to answer for a file at mode 0644 this account owns", writable)
	}

	gated := filepath.Join(dir, "gated.db")

	if err := os.WriteFile(gated, []byte("page"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", gated, err)
	}

	if err := os.Chmod(gated, 0o444); err != nil {
		t.Fatalf("chmod %s: %v", gated, err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(gated, 0o644)
	})

	if canWriteFile(gated) {
		t.Errorf("canWriteFile(%s) is true, want it false for a file at mode 0444: a start may read it and still not write it", gated)
	}
}

// the guard is about the file the server writes, so it has nothing to say about
// a deployment whose database is a server on another host.
func TestRefuseUnwritableDatabaseFileSaysNothingAboutAServerOnAnotherHost(t *testing.T) {
	cfg, _ := newTestConfig(t, "-db_type", DbTypePostgresql, "-db_host", "db.internal", "-db_name", "rdio")

	if err := refuseUnwritableDatabaseFile(cfg); err != nil {
		t.Errorf("the guard answered a %s deployment: %v", DbTypePostgresql, err)
	}

	cfg, _ = newTestConfig(t, "-db_type", DbTypeMysql, "-db_host", "db.internal", "-db_name", "rdio")

	if err := refuseUnwritableDatabaseFile(cfg); err != nil {
		t.Errorf("the guard answered a %s deployment: %v", DbTypeMysql, err)
	}
}
