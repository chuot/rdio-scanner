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
