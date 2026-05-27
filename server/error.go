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
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
)

// errorFormatter returns errors that are safe to surface in the admin
// Logs view. The full SQL `query` string passed by callers is written
// only to the process's stderr at startup-debug level (env RDIO_DEBUG_SQL=1)
// — it must never end up in the logs table, because failing INSERTs/UPDATEs
// frequently embed parameter values that include bcrypt hashes, API keys,
// or access codes.
func errorFormatter(section string, label string) func(err error, query string) error {
	debugSQL := os.Getenv("RDIO_DEBUG_SQL") == "1"
	return func(err error, query string) error {
		safe := fmt.Sprintf("%s.%s: %s", section, label, err.Error())
		if debugSQL && len(query) > 0 {
			log.Printf("[sql] %s :: %s", safe, query)
		}
		return errors.New(safe)
	}
}
