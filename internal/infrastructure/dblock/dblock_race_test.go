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

package dblock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The removal on release opens the classic lockfile window: a process that
// opened the lock file before the holder removed it can take the OS lock on
// the unlinked file after the holder lets go, while a later process creates a
// fresh file of the same name — two holders on one deployment. The identity
// check closes the window. This test reproduces the interleaving with raw
// descriptors around the public API and shows the stale handle is refused and
// the fresh acquirer of the replacement file is the only holder.
func TestAcquireRefusesAStaleHandleOnAReplacedLockFile(t *testing.T) {
	dir := t.TempDir()

	dbPath := filepath.Join(dir, "rdio-scanner.db")
	lockPath := filepath.Join(dir, "rdio-scanner.lock")

	holder, err := Acquire(dbPath)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// The waiter's open, before the removal: it ends up standing on the file
	// the holder is about to remove.
	stale, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open the lock file: %v", err)
	}

	defer stale.Close()

	if err := holder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The OS lock on the unlinked file is free: whoever locked it would hold a
	// file nothing names any more.
	if err := tryLock(stale); err != nil {
		t.Fatalf("tryLock on the unlinked file: %v: the unlinked file must still be lockable for the race to exist", err)
	}

	// The check that rejects this handle: the locked descriptor no longer
	// matches what the path names.
	if sameFile(stale, lockPath) {
		t.Errorf("sameFile(stale, lock path) = true, want false: the path no longer names the locked file")
	}

	// The fresh acquirer takes the replacement file and holds the deployment
	// alone: a third process is refused.
	fresh, err := Acquire(dbPath)
	if err != nil {
		t.Fatalf("Acquire after the replacement: %v", err)
	}

	if _, err := Acquire(dbPath); !errors.Is(err, ErrInUse) {
		t.Errorf("Acquire while the replacement is held = %v, want ErrInUse: the fresh acquirer must be the only holder", err)
	}

	if err := fresh.Close(); err != nil {
		t.Fatalf("Close the fresh handle: %v", err)
	}
}

// A fresh Acquire over a file nobody replaced still matches: the identity
// check must not refuse an ordinary hold.
func TestSameFileAcceptsTheFileThePathNames(t *testing.T) {
	dir := t.TempDir()

	handle, err := Acquire(filepath.Join(dir, "rdio-scanner.db"))
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	defer handle.Close()

	if !sameFile(handle.file, handle.file.Name()) {
		t.Errorf("sameFile(handle, its own path) = false, want true: an ordinary hold matches")
	}
}
