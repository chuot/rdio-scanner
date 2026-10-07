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

// Command sqlite is what capture-dirwatch.sh uses to hand the legacy server a configuration and to read
// back the rows its run left behind. It is not part of the server: it lives under testdata, so the go
// tool does not build it with the rest of the module, and it is built on demand by the capture script.
//
//	usage: sqlite exec <database> <statement>
//	       sqlite rows <database> <query>
//
// exec runs one statement and says nothing. rows prints the result as a JSON array, one object per row,
// which is the shape the capture writes into a golden.
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: sqlite <exec|rows> <database> <sql>")

		os.Exit(2)
	}

	mode, path, query := os.Args[1], os.Args[2], os.Args[3]

	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys=on&_pragma=busy_timeout=10000")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}

	defer db.Close()

	if mode == "exec" {
		if _, err := db.Exec(query); err != nil {
			fmt.Fprintln(os.Stderr, err)

			os.Exit(1)
		}

		return
	}

	if mode != "rows" {
		fmt.Fprintf(os.Stderr, "unknown mode %s\n", mode)

		os.Exit(2)
	}

	rows, err := db.Query(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}

	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}

	found := []map[string]any{}

	for rows.Next() {
		values := make([]any, len(columns))
		holders := make([]any, len(columns))

		for i := range values {
			holders[i] = &values[i]
		}

		if err := rows.Scan(holders...); err != nil {
			fmt.Fprintln(os.Stderr, err)

			os.Exit(1)
		}

		row := map[string]any{}

		for i, column := range columns {
			// Text that the driver hands back as bytes is the same text as far as a golden is concerned.
			if text, ok := values[i].([]byte); ok {
				row[column] = string(text)

				continue
			}

			row[column] = values[i]
		}

		found = append(found, row)
	}

	if err := rows.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	if err := enc.Encode(found); err != nil {
		fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
}
