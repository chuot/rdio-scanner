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

package dblock

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAcquireTakesTheLockBesideTheDatabase(t *testing.T) {
	dir := t.TempDir()

	handle, err := Acquire(filepath.Join(dir, "rdio-scanner.db"))
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	defer handle.Close()

	info, err := os.Stat(filepath.Join(dir, "rdio-scanner.lock"))
	if err != nil {
		t.Fatalf("stat rdio-scanner.lock: %v", err)
	}

	if info.IsDir() {
		t.Fatal("rdio-scanner.lock is a directory, want the lock file beside the database")
	}
}

// The lock takes the database file's name, so a deployment that points
// `-db_file` at its own file keeps its lock beside that file, and two
// databases in one directory keep two locks.
func TestAcquireNamesTheLockAfterTheDatabaseFile(t *testing.T) {
	dir := t.TempDir()

	handle, err := Acquire(filepath.Join(dir, "myscanner.sqlite"))
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	defer handle.Close()

	if _, err := os.Stat(filepath.Join(dir, "myscanner.lock")); err != nil {
		t.Errorf("stat myscanner.lock: %v: the lock takes the database file's name", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "rdio-scanner.lock")); !os.IsNotExist(err) {
		t.Errorf("stat rdio-scanner.lock: %v: a custom database file must not lock under the default name", err)
	}
}

// A database file with no extension still gets one lock file name, not a
// doubled suffix or an empty one.
func TestAcquireOnADatabaseFileWithNoExtension(t *testing.T) {
	dir := t.TempDir()

	handle, err := Acquire(filepath.Join(dir, "scanner-db"))
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	defer handle.Close()

	if _, err := os.Stat(filepath.Join(dir, "scanner-db.lock")); err != nil {
		t.Errorf("stat scanner-db.lock: %v", err)
	}
}

func TestAcquireRecordsTheHoldingProcess(t *testing.T) {
	dir := t.TempDir()

	handle, err := Acquire(filepath.Join(dir, "rdio-scanner.db"))
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	defer handle.Close()

	content, err := os.ReadFile(filepath.Join(dir, "rdio-scanner.lock"))
	if err != nil {
		t.Fatalf("read rdio-scanner.lock: %v", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatalf("parse %q as a process id: %v", content, err)
	}

	if pid != os.Getpid() {
		t.Errorf("holder pid = %d, want %d: the file names the process that holds the deployment", pid, os.Getpid())
	}
}

// The lock is what refuses a second process, so a second attempt against a held
// lock must fail, and fail as ErrInUse: that is the sentinel the server and the
// vacuum task answer with. A second handle in the same process is enough to
// prove it — the advisory lock sits on the open file description, so two
// handles conflict the same way two processes would, and the test needs no
// subprocess.
func TestAcquireFailsWhileAnotherProcessHoldsTheLock(t *testing.T) {
	dir := t.TempDir()

	handle, err := Acquire(filepath.Join(dir, "rdio-scanner.db"))
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	defer handle.Close()

	_, err = Acquire(filepath.Join(dir, "rdio-scanner.db"))
	if err == nil {
		t.Fatal("Acquire while held: want the failure ErrInUse, got nil")
	}

	if !errors.Is(err, ErrInUse) {
		t.Errorf("Acquire while held: want ErrInUse, got %v", err)
	}
}

// Closing is the release: the server's Close, and the vacuum task's defer, both
// end the hold this way, and the next acquirer must get in.
func TestCloseReleasesTheLock(t *testing.T) {
	dir := t.TempDir()

	handle, err := Acquire(filepath.Join(dir, "rdio-scanner.db"))
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := handle.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	released, err := Acquire(filepath.Join(dir, "rdio-scanner.db"))
	if err != nil {
		t.Fatalf("Acquire after Close: %v", err)
	}

	defer released.Close()
}

// A clean release removes the lock file: an administrator listing the
// deployment directory after the server stopped must not find a file naming a
// dead process. A later Acquire creates a fresh file.
func TestCloseRemovesTheLockFile(t *testing.T) {
	dir := t.TempDir()

	handle, err := Acquire(filepath.Join(dir, "rdio-scanner.db"))
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := handle.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "rdio-scanner.lock")); !os.IsNotExist(err) {
		t.Errorf("stat rdio-scanner.lock after Close = %v, want not exists: the release removes the file", err)
	}

	released, err := Acquire(filepath.Join(dir, "rdio-scanner.db"))
	if err != nil {
		t.Fatalf("Acquire after Close: %v", err)
	}

	defer released.Close()

	if _, err := os.Stat(filepath.Join(dir, "rdio-scanner.lock")); err != nil {
		t.Errorf("stat rdio-scanner.lock after a fresh Acquire: %v: a new hold creates a new file", err)
	}
}

// A database path with no directory part — the relative form an ini file's
// plain file name gives — still locks beside the database.
func TestAcquireOnARelativeDatabasePath(t *testing.T) {
	dir := t.TempDir()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	defer os.Chdir(previous)

	handle, err := Acquire("rdio-scanner.db")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	defer handle.Close()

	if _, err := os.Stat(filepath.Join(dir, "rdio-scanner.lock")); err != nil {
		t.Errorf("stat rdio-scanner.lock: %v: the lock sits beside the database, not elsewhere", err)
	}
}

// The lock is the arbiter, and its contents are only diagnostics: a stale file
// left by a crashed holder is still lockable, so a fresh Acquire over one must
// succeed and rewrite the holder.
func TestAcquireSucceedsOverALockFileLeftByACrash(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "rdio-scanner.lock"), []byte("999999999\n"), 0o644); err != nil {
		t.Fatalf("write a stale lock file: %v", err)
	}

	handle, err := Acquire(filepath.Join(dir, "rdio-scanner.db"))
	if err != nil {
		t.Fatalf("Acquire over a stale lock file: %v", err)
	}

	defer handle.Close()

	content, err := os.ReadFile(filepath.Join(dir, "rdio-scanner.lock"))
	if err != nil {
		t.Fatalf("read rdio-scanner.lock: %v", err)
	}

	if strings.TrimSpace(string(content)) != strconv.Itoa(os.Getpid()) {
		t.Errorf("holder pid after re-acquiring = %q, want this process: a stale file's contents are overwritten, never consulted", content)
	}
}
