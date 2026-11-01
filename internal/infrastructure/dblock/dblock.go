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

// Package dblock holds one deployment's database for one process at a time.
//
// A deployment is a directory that carries a database file, and two processes
// on it is a data-loss surface: the server writes the database through a
// connection pool for its whole life, and a one-shot task that rewrites the
// file — the sqlite vacuum — replaces it wholesale. The database engine's own
// locking does not cover that: sqlite's busy timeout waits for statements, not
// for a process that intends to rebuild the file, and the other dialects are
// server-backed and out of reach of a file lock at all. So the arbiter sits
// outside the database, on a file both halves of the deployment open before
// either touches it.
//
// The lock is an advisory operating-system lock on a sidecar file beside the
// database, carrying the database file's name with a .lock extension. It is
// held for the whole life of the process that takes it, and it is taken before
// the database is opened, so every mode that reaches the database passes
// through it. A crashed holder costs nothing: the operating system drops an
// advisory lock when its file descriptor goes away with the process, so the
// file left behind is lockable again on its own.
//
// What the file's contents say is never consulted. It names the holding
// process, which is what an operator inspects when a deployment refuses to
// start, but the lock itself — not the pid inside it — decides who is in. A pid
// can be recycled, and checking it and then acting on it is a window, so no
// decision reads it.
//
// The lock is local to one machine. A deployment whose database sits on a
// shared network filesystem and is opened by more than one host is outside
// what an advisory lock arbitrates, and that shape is out of scope.
package dblock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// lockSuffix is what the deployment's lock file carries in place of the
// database file's own extension. The lock takes the database file's name so a
// deployment that points `-db_file` at myscanner.sqlite keeps its lock as
// myscanner.lock next to it, and the default deployment gets
// rdio-scanner.lock next to rdio-scanner.db. One lock per database file is the
// claim that matters: the vacuum rewrites that one file.
const lockSuffix = ".lock"

// lockPathFor returns the lock file that stands beside the database file at
// dbPath: the same directory, the same base name, the lock suffix in place of
// the database's own extension.
func lockPathFor(dbPath string) string {
	dir, name := filepath.Split(dbPath)
	if ext := filepath.Ext(name); ext != "" {
		name = name[:len(name)-len(ext)]
	}

	return filepath.Join(dir, name+lockSuffix)
}

// ErrInUse reports that another process holds the deployment. Callers answer
// it with their own refusal; it carries no message of its own so that each one
// says what it was about to do.
var ErrInUse = errors.New("the database is in use")

// Handle is a held deployment lock. Its lifetime is the process's: the holder
// keeps it while it works with the database and releases it when it stops.
type Handle struct {
	file *os.File
}

// Acquire takes the deployment lock for the database at dbPath — the same path
// the server was configured with — and records this process in the lock file.
// A deployment another process holds fails as ErrInUse; anything else that
// went wrong, an unreadable directory, is a plain error.
func Acquire(dbPath string) (*Handle, error) {
	lockPath := lockPathFor(dbPath)

	// The file is opened read-write and never truncated on open: the lock, not
	// the file's contents, is what decides, and truncating before holding it
	// would rewrite a file another process is standing on.
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("dblock.acquire: %w", err)
	}

	if err := tryLock(file); err != nil {
		file.Close()

		return nil, err
	}

	if err := writeHolder(file); err != nil {
		unlock(file)
		file.Close()

		return nil, fmt.Errorf("dblock.acquire: %w", err)
	}

	return &Handle{file: file}, nil
}

// Close releases the lock: it removes the lock file by name first, then
// releases the OS lock, then closes the descriptor. The removal comes before
// the unlock so that an acquirer racing the release can never lock this file
// and pass the identity check under a name the holder still owns — an
// acquirer that ends up holding the lock holds the file the path names. A
// failed removal does not stop the release: a stale file is cosmetic, a lock
// that outlives the process is not, so the error is reported after the lock
// is gone.
func (h *Handle) Close() error {
	if h.file == nil {
		return nil
	}

	removeErr := os.Remove(h.file.Name())
	if removeErr != nil && !os.IsNotExist(removeErr) {
		removeErr = fmt.Errorf("dblock.close: %w", removeErr)
	} else {
		removeErr = nil
	}

	unlockErr := unlock(h.file)
	closeErr := h.file.Close()

	h.file = nil

	if unlockErr != nil {
		return fmt.Errorf("dblock.close: %w", unlockErr)
	}

	if closeErr != nil {
		return fmt.Errorf("dblock.close: %w", closeErr)
	}

	return removeErr
}

// writeHolder records the holding process for an operator to read. It starts
// from empty so a file left by an earlier holder cannot keep its pid.
func writeHolder(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return err
	}

	if _, err := file.Seek(0, 0); err != nil {
		return err
	}

	_, err := file.WriteString(strconv.Itoa(os.Getpid()) + "\n")

	return err
}
