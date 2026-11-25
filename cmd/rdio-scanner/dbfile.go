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
	"path/filepath"
)

// refuseUnwritableDatabaseFile stops a start whose database it cannot write and
// names the path that stands in the way: the file itself, the folder it sits in,
// the folder that is not there, or a path that is a folder. A start that reaches
// the driver or the claim first reports that layer's words and the statement it
// happened to be running, and never the path it could not write, so an operator
// is left to work out which of the deployment's files another account owns.
func refuseUnwritableDatabaseFile(config *Config) error {
	// A server on another host writes no local database file, and asking one
	// whether a file it never opens is writable would refuse a start whose
	// database is a connection to a server that answers.
	if config.DbType != DbTypeSqlite {
		return nil
	}

	dbFile := config.GetDbFilePath()
	dbDir := filepath.Dir(dbFile)

	// A start may create the file it named, but it cannot create the folder that
	// was named above it, and an operator told the folder "does not exist" knows
	// to make one, which is not what a permissions sentence says.
	if _, err := os.Lstat(dbDir); os.IsNotExist(err) {
		return fmt.Errorf("the folder the database file was named in does not exist: %s", dbDir)
	}

	// The file's own bits are not the whole answer: a start writes beside the file
	// as well as into it, so a folder that will not hold those pages makes the file
	// unusable however its mode reads, and a file inside such a folder cannot even
	// be opened for the check below.
	if !isWritableDir(dbDir) {
		return fmt.Errorf("no write permissions in %s, the folder the database file %s sits in", dbDir, dbFile)
	}

	info, err := os.Lstat(dbFile)
	if err != nil {
		// A file that is not there yet belongs to the start that creates it, in a
		// folder this account was just shown it can write.
		return nil
	}

	if info.IsDir() {
		// The driver refuses this shape too, but with the same words it uses for a
		// missing or unreadable file, so the start says what the path is.
		return fmt.Errorf("the database file %s is a folder", dbFile)
	}

	if canWriteFile(dbFile) {
		return nil
	}

	return fmt.Errorf("no write permissions in the database file %s (%s)", dbFile, unwritableFileClause(info))
}

// unwritableFileClause supplies what follows the file a refusal named: its mode,
// the uid that owns it, and the uid the server is running as. All three are read
// after the failed attempt has decided the refusal, so they explain a decision
// rather than make one, and they are the three numbers stat and id report for the
// same file — the mode a chmod changes, the owner a chown changes, and the account
// the deployment's own process reports. A path this system carries no uid for says
// that, and no number is invented for it.
func unwritableFileClause(info os.FileInfo) string {
	mode := info.Mode().Perm()

	owner, named := fileOwnerUID(info)
	if !named {
		return fmt.Sprintf("mode %04o, owned by an account this system does not name", mode)
	}

	return fmt.Sprintf("mode %04o, owned by uid %d; the server runs as uid %d", mode, owner, os.Getuid())
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
