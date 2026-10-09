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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWatchedRecordingsPopulateTheConfigurationTheyName is the same path as the end-to-end run with the
// configuration taken away: the server is given two watches and nothing else, and the recordings it reads
// name systems, talkgroups, groups, tags and radios it has never heard of. What the test compares is the
// configuration the server ended up creating, row by row and column by column, against what the legacy
// server created from the same recordings in the same order —
// testdata/golden/dirwatch-autopopulate.json.
//
// The recordings are dropped one at a time, and the next is not dropped until the previous one has become a
// call, because the order is what each recording is testing: the first creates a talkgroup whose label is
// its own reference written out as a number, and the second is the labeled upload that repairs that
// placeholder. Dropping them together would test whatever order two directory watchers happened to notice
// them in.
func TestWatchedRecordingsPopulateTheConfigurationTheyName(t *testing.T) {
	golden := readGoldenRun(t, "dirwatch-autopopulate.json")

	srv := testServer(t)

	watched := filepath.Join(srv.config.BaseDir, "watch")

	dsd := filepath.Join(watched, "dsd")
	trunk := filepath.Join(watched, "trunk")

	for _, directory := range []string{dsd, filepath.Join(dsd, "20261006"), trunk} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("preparing the watched directory %s: %v", directory, err)
		}
	}

	seedWatches(t, srv.db, dsd, trunk)

	if err := srv.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// A system named by label, and a talkgroup named only by its reference: the server has no label to
	// give the talkgroup, so it writes the reference out as a number, and it has no group or tag to file
	// it under, so it uses the ones a fresh deployment starts with. The system gets the first reference
	// nothing else is using.
	dropTrunkRecording(t, srv, trunk, "first", firstRecordingSidecar)

	// The same system and the same talkgroup, and this time the sidecar says what they are called. Nothing
	// is created: the placeholder label and name are replaced with the ones the sidecar names, and the
	// group and tag the sidecar names are left alone because the talkgroup was already filed somewhere.
	dropTrunkRecording(t, srv, trunk, "second", secondRecordingSidecar)

	// A system, a group and a tag the server has never seen. The group and the tag are created, the
	// talkgroup is filed under the tag its group names, and the label it is given is the one its own tag
	// names. This system also gets the next reference nothing else is using.
	dropTrunkRecording(t, srv, trunk, "third", thirdRecordingSidecar)

	// A dsd+ recording names its system by reference rather than by label, so the server keeps the
	// reference the recording carries and gives the system a label made out of it. It also names a radio
	// the server has never heard of, which is created on the system the recording's system became.
	dropRecording(t, filepath.Join(dsd, "20261006"), dsdRecordingName, recording(2048))

	waitForLogMentioning(t, srv.db, "success, "+dsdRecordingName)

	for _, directory := range []string{dsd, trunk} {
		left := findFiles(t, directory)

		if len(left) > 0 {
			t.Errorf("the watch on %s left %v behind, want the recording deleted after ingesting it", directory, left)
		}
	}

	for _, name := range []string{"calls", "logs", "groups", "tags", "talkgroups", "units", "systems"} {
		compareCanonical(t, "the "+name+" the server created", golden[name], endToEndRows(t, srv.db, name))
	}

	assertEveryServerLogEntry(t, srv.db)

	// The capture reports the two columns the legacy capture reported, so the filing is proven here. An
	// operator looking for something that appeared in a configuration nobody edited filters to the
	// configuration, so every change these recordings caused has to be filed there — and has to name the
	// recording that caused it, which is the thing an operator traces.
	const query = `SELECT "message" FROM "logs" WHERE "source" = 'config' ORDER BY "logid"`

	rows, err := srv.db.Query(query)
	if err != nil {
		t.Fatalf("reading the configuration entries the run left behind: %v", err)
	}

	defer rows.Close()

	created := []string{}

	for rows.Next() {
		var message string

		if err := rows.Scan(&message); err != nil {
			t.Fatalf("reading a configuration entry the run left behind: %v", err)
		}

		created = append(created, message)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("reading the configuration entries the run left behind: %v", err)
	}

	// Three systems, three talkgroups, one group, one tag, one unit, and one placeholder label replaced.
	if len(created) != 10 {
		t.Fatalf("the log view files %d entries under the configuration, want the 10 things these recordings "+
			"caused the server to create: %v", len(created), created)
	}

	for _, message := range created {
		if !strings.HasPrefix(message, "the recording ") {
			t.Errorf("the configuration entry %q does not name the recording that caused the change", message)
		}
	}
}

// The four recordings, and the sidecars that say what they are. They are the recordings the capture wrote,
// byte for byte, which is why the golden can pin how long they are.
const (
	firstRecordingSidecar = `{
  "short_name": "Grand Metro",
  "talkgroup": 1514000,
  "timestamp": 1791288000000
}
`

	secondRecordingSidecar = `{
  "short_name": "Grand Metro",
  "talkgroup": 1514000,
  "talkgroup_description": "Dispatch 1",
  "talkgroup_group": "Ground",
  "talkgroup_group_tag": "Dispatch",
  "talkgroup_tag": "Main",
  "timestamp": 1791288060000
}
`

	thirdRecordingSidecar = `{
  "short_name": "Harbour Bridge",
  "talkgroup": 3001,
  "talkgroup_description": "Bridge Toll",
  "talkgroup_group": "Marine",
  "talkgroup_group_tag": "Toll",
  "talkgroup_tag": "Harbour Toll",
  "timestamp": 1791288120000
}
`
)

// dropTrunkRecording puts a trunk-recorder pair into a watched directory and waits for the server to have
// finished taking it in.
func dropTrunkRecording(t *testing.T, srv *server, directory, name, sidecar string) {
	t.Helper()

	// The sidecar goes in before the recording it describes: the recording is what the watch is looking
	// for, and a recording whose sidecar has not arrived yet is a recording the server cannot say
	// anything about.
	dropRecording(t, directory, name+".json", []byte(sidecar))
	dropRecording(t, directory, name+".wav", recording(2092))

	waitForLogMentioning(t, srv.db, "success, "+name+".wav")
}
