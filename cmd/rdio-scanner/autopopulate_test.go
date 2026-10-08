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
