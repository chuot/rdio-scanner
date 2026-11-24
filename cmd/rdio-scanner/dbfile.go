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
	"fmt"
	"os"
)

// refuseUnwritableDatabaseFile stops a start that named a database file it
// cannot write, and names that file. A start that reaches the driver first
// reports the driver's words and the statement it happened to be running, and
// never the path it could not write, so an operator is left to work out which of
// the deployment's files another account owns.
func refuseUnwritableDatabaseFile(config *Config) error {
	// A server on another host writes no local database file, and asking one
	// whether a file it never opens is writable would refuse a start whose
	// database is a connection to a server that answers.
	if config.DbType != DbTypeSqlite {
		return nil
	}

	dbFile := config.GetDbFilePath()

	info, err := os.Lstat(dbFile)
	if err != nil {
		// A file that is not there yet belongs to the start that creates it.
		return nil
	}

	if info.IsDir() {
		// The start that opens a folder as its database is refused by the driver,
		// and naming that shape is a refusal this guard takes on separately.
		return nil
	}

	if canWriteFile(dbFile) {
		return nil
	}

	return fmt.Errorf("no write permissions in the database file %s", dbFile)
}

// canWriteFile asks the named file whether this process may write it, by opening
// it for writing and closing it again. The file's mode bits are not asked: the
// answer has to hold for the account the server really runs as, which is the one
// thing an operator's fix turns on, and only an attempt by that account gives it.
// The file is opened with no create flag, so a refusal cannot make a file the
// operator did not name.
func canWriteFile(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}

	_ = f.Close()

	return true
}
