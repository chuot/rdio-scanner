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

	"golang.org/x/sys/unix"
)

// tryLock takes the exclusive lock without waiting: an flock held by another
// open file description answers EWOULDBLOCK, which is the deployment being in
// use. The lock belongs to the open file description rather than to the
// process, so two handles in one process conflict the same way two processes
// do, and the operating system drops it when the descriptor closes or the
// process dies — which is what makes a crashed holder's leftover file lockable
// again without anyone inspecting it.
func tryLock(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return ErrInUse
		}

		return err
	}

	return nil
}

// unlock gives the lock back. Closing the descriptor would release it as well;
// saying so explicitly is what makes the release visible at the point the
// holder stops working with the database.
func unlock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

// sameFile reports whether the path names the very file the descriptor stands
// on, comparing device and inode numbers. An acquirer that opened the lock
// file before the holder removed it ends up standing on an unlinked inode; the
// path names a different file by then, and the comparison says so. A live
// inode is never reused while a descriptor still stands on it, so a match on
// an ordinary hold is trustworthy.
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
