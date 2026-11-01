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

package dblock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// openLockFile opens the sidecar for the lock cycle: read-write, created if
// missing, never truncated — the lock, not the file's contents, is what
// decides, and truncating before holding it would rewrite a file another
// process is standing on. The open goes through CreateFile rather than the
// runtime's own because of one share flag: FILE_SHARE_DELETE is what lets the
// holder remove the file's name while this very handle stays open, and the
// release depends on that removal. Without it Windows refuses the removal of
// a file any handle stands on, and the lock file would outlive every clean
// stop.
func openLockFile(path string) (*os.File, error) {
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}

	// A raw handle carries no name of its own the way a unix descriptor can
	// recover one, so the handle keeps the path it opened: the release removes
	// the file by that name.
	return os.NewFile(uintptr(handle), path), nil
}

// tryLock takes the exclusive lock without waiting. Windows locks a byte range
// rather than the whole file, and one byte at the start is enough: the file is
// a sidecar nobody reads or writes for any other reason, so whoever holds that
// byte holds the deployment. An already-locked range answers immediately, and
// either violation code means the deployment is in use: ERROR_LOCK_VIOLATION
// for a range another handle holds, ERROR_FILE_EXISTS — the code the platform
// headers also name ERROR_FILE_ALREADY_EXISTS — for the exclusive request that
// found one. The lock belongs to the file handle, which the operating system
// closes when the process dies, so a crashed holder's leftover file is lockable
// again without anyone inspecting it.
func tryLock(file *os.File) error {
	var overlapped windows.Overlapped

	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)

	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &overlapped)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return ErrInUse
		}

		return err
	}

	return nil
}

// unlock gives the range back. Closing the handle would release it as well;
// saying so explicitly is what makes the release visible at the point the
// holder stops working with the database.
func unlock(file *os.File) error {
	var overlapped windows.Overlapped

	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}

// sameFile reports whether the path names the very file the handle stands on.
// The comparison is the operating system's own: on Windows it answers with the
// volume serial number and the file index, which name the file on that volume
// and no other. A handle opened before the holder removed the file stands on a
// file the path no longer names, and a file created later under the same name
// carries a different index, so the comparison tells the stale handle from the
// current one.
func sameFile(file *os.File, path string) bool {
	fi, err := file.Stat()
	if err != nil {
		return false
	}

	st, err := os.Stat(path)
	if err != nil {
		return false
	}

	return os.SameFile(fi, st)
}
