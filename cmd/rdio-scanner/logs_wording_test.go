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
	"database/sql"
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

// assertEveryServerLogEntry is the guard over a real server's log view: every row names the part of the
// server that wrote it, and no row begins with the internal name of the code that wrote it.
//
// The application layer's writers are guarded in internal/application/logs_wording_test.go. This is the same
// two rules at the level where the watched directories, the livefeed hub, the upload endpoint and the
// retention pass actually write, read back out of the store every entry is written through — which is also
// the only place a row the *other* server wrote can be told apart from one this server wrote.
//
// It is called at the end of the integration tests that already reach each subsystem, rather than from one
// test that tries to reach all of them, so a failure names the journey that produced the entry.
func assertEveryServerLogEntry(t *testing.T, db *sql.DB) {
	t.Helper()

	const query = `SELECT "source", "message" FROM "logs" ORDER BY "logid"`

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("reading the server's own log view: %v", err)
	}

	defer rows.Close()

	written := 0

	for rows.Next() {
		var source, message string

		if err := rows.Scan(&source, &message); err != nil {
			t.Fatalf("reading an entry from the server's own log view: %v", err)
		}

		written++

		if message == "" {
			t.Errorf("the server wrote an entry with no message")
			continue
		}

		if source == "" {
			t.Errorf("the server wrote %q without naming the part of the server that wrote it", message)
			continue
		}

		if !domain.IsKnownLogSource(domain.LogSource(source)) {
			t.Errorf("the server wrote %q under %q, which is not a part of this server", message, source)
			continue
		}

		if prefix := internalPrefixAt(message); prefix != "" {
			t.Errorf("the server wrote %q, which begins with the internal name %q instead of saying what happened",
				message, prefix)
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the server's own log view: %v", err)
	}

	if written == 0 {
		t.Errorf("the server wrote nothing to its own log view, so this guard proves nothing about what it writes")
	}
}

// internalPrefixAt is the front-of-message shape the wording rule forbids: a token naming a section and an
// operation, or a bare section name, ending in a colon.
//
// Only the front counts. The failure's own text is kept verbatim after the sentence this feature puts in
// front of it, because that text names the statement or the syscall that failed, so `the log search failed:
// logs.search: no such table: logs` is the rule being followed and not broken.
func internalPrefixAt(message string) string {
	first := message

	if space := strings.Index(message, " "); space >= 0 {
		first = message[:space]
	}

	if strings.HasSuffix(first, ":") {
		return first
	}

	return ""
}
