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

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWatchedRecordingsPopulateTheConfigurationTheyName drops
// recordings naming unknown systems, talkgroups, groups, tags and
// radios, checked against the `dirwatch-autopopulate` capture.
func TestWatchedRecordingsPopulateTheConfigurationTheyName(t *testing.T) {
	captured := capturedRun(t, "dirwatch-autopopulate.json")

	srv := journeyServerIn(t, t.TempDir())

	watched := filepath.Join(srv.base, "watch")

	dsd := filepath.Join(watched, "dsd")
	trunk := filepath.Join(watched, "trunk")

	for _, directory := range []string{dsd, filepath.Join(dsd, "20261006"), trunk} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("preparing the watched directory %s: %v", directory, err)
		}
	}

	seedWatches(t, srv.db, dsd, trunk)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// a system named by label and a talkgroup named only by its
	// reference: the reference becomes its label, the system takes
	// the first free reference, and the default group and tag
	// receive the talkgroup
	dropTrunkRecording(t, srv, trunk, "first", firstRecordingSidecar)

	// a labeled upload for the same system and talkgroup replaces
	// the placeholder label and name, creates nothing, and leaves
	// the talkgroup filed where it already was
	dropTrunkRecording(t, srv, trunk, "second", secondRecordingSidecar)

	// unknown group and tag: both are created, the talkgroup is
	// filed under the tag its group names and labeled as that tag
	// names, and the system takes the next free reference
	dropTrunkRecording(t, srv, trunk, "third", thirdRecordingSidecar)

	// a dsd+ recording names its system by reference, so the
	// server keeps that reference and labels the system from it,
	// and creates the radio it names on that system
	dropRecording(t, filepath.Join(dsd, "20261006"), dsdRecordingName, recording(2048))

	waitForLogMentioning(t, srv.db, "success, "+dsdRecordingName)

	for _, directory := range []string{dsd, trunk} {
		left := findFiles(t, directory)

		if len(left) > 0 {
			t.Errorf("the watch on %s left %v behind, want the recording deleted after ingesting it", directory, left)
		}
	}

	for _, name := range []string{"calls", "logs", "groups", "tags", "talkgroups", "units", "systems"} {
		compareCanonical(t, "the "+name+" the server created", captured[name], rowsOf(t, srv.db, srv.base, name))
	}

	assertEveryServerLogEntry(t, srv.db)

	// the log capture reads the configuration-sourced entries only:
	// every change these recordings caused has to be filed there,
	// naming the recording that caused it, the way an operator
	// traces a change
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

// the four recordings and their sidecars, byte for byte the
// bytes the capture wrote
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

// Pins that a systems-section save keeps the invent-the-talkgroup flag
// set and later recordings still name their talkgroup.
func TestASystemsSectionSaveLeavesAutoPopulateInventingTheTalkgroupsRecordingsName(t *testing.T) {
	srv := journeyServerIn(t, t.TempDir())

	seed(t, srv.db,
		seedStatement{
			query: `INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (1, 1, '', 0, 'Grand Metro', '', 1, 10001, '')`,
		},
		seedStatement{
			query: `INSERT INTO "apikeys" ("apikeyId", "disabled", "ident", "key", "order", "systems") VALUES (1, 0, 'auto populate upload', ?, 0, '"*"')`,
			args:  []any{autoPopulateUploadKey},
		},
		seedStatement{
			query: `INSERT INTO "options" ("key", "value") VALUES ('audioConversion', '0')`,
		},
	)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	token := administratorToken(t, srv)

	// the Systems section as the page submits it: the system row's
	// fields, and the label the operator came to change
	if rec := srv.put("/api/admin/config/systems", token, `[{
		"alert": "",
		"autoPopulate": true,
		"blacklists": "",
		"delay": 0,
		"id": 1,
		"label": "Grand Metro Renamed",
		"led": "",
		"order": 1,
		"systemRef": 10001,
		"type": ""
	}]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the Systems section was answered %d: %s", rec.Code, rec.Body.String())
	}

	// a recording naming an unknown talkgroup: the server invents it,
	// files it under the no-name defaults, keeping the saved label
	answered := srv.uploadRecording(t, uploadedRecording{
		key:       autoPopulateUploadKey,
		systemRef: 10001,
		talkgroup: inventedTalkgroupRef,
		filename:  "invented.wav",
		audio:     recording(2048),
		at:        time.Now().Add(-30 * time.Second),
	})

	if answered.Code != http.StatusOK {
		t.Fatalf("the recording naming an unconfigured talkgroup was answered %d: %s",
			answered.Code, answered.Body.String())
	}

	if rows := countRows(t, srv.db, fmt.Sprintf(`SELECT COUNT(*) FROM "talkgroups" WHERE "talkgroupRef" = %d`, inventedTalkgroupRef)); rows != 1 {
		t.Fatalf("the store holds %d talkgroups for the reference the recording named, want the one auto populate invents", rows)
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "calls"`); rows != 1 {
		t.Errorf("the server kept %d calls, want the one it invented a talkgroup for", rows)
	}

	// the saved label is the one the invented talkgroup is filed
	// under: the save wrote the system row, not a copy of it
	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "systems" WHERE "label" = 'Grand Metro Renamed'`); rows != 1 {
		t.Errorf("the store holds %d systems carrying the label the Systems section saved, want the one", rows)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// autoPopulateUploadKey is the credential the auto populate
// reading posts with, and inventedTalkgroupRef an unused reference
const (
	autoPopulateUploadKey = "a-key-that-refuses-nothing-here"

	inventedTalkgroupRef = 1514500
)

// dropTrunkRecording puts a trunk-recorder pair into a watched
// directory and waits for the server to finish taking it in
func dropTrunkRecording(t *testing.T, srv *journeyServer, directory, name, sidecar string) {
	t.Helper()

	// the sidecar goes in before the recording: the watch looks
	// for recordings, and a recording whose sidecar has not
	// arrived is one the server cannot say anything about
	dropRecording(t, directory, name+".json", []byte(sidecar))
	dropRecording(t, directory, name+".wav", recording(2092))

	waitForLogMentioning(t, srv.db, "success, "+name+".wav")
}
