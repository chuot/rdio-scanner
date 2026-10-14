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
// This API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

package acceptance

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Blacklisting a talkgroup from the Talkgroups section, on the server a deployment runs.
//
// The operator's action is one intent — this talkgroup is gone and this system refuses it — and the
// defect was that the two halves were saved by two different sections, so saving the section the
// operator was standing in stored one half and left the other in a section nobody was told about.
// What is pinned here is the path the administration page takes: one request to the talkgroups
// level, the answer the page reads back, and the recording a radio sends afterwards.
//
// The rows are laid out the way the conformance fixture lays them out: the first system's first
// talkgroup is row 1 and reference 1514000. The two numbers differ, which is what makes a stored
// entry say which of the two it names.

const (
	// blacklistUploadKey is the credential the journey's recordings are posted with. Its scope
	// covers every system, so that what the journey is about is the blacklist and not the gate in
	// front of the ingest.
	blacklistUploadKey = "a-key-that-refuses-nothing"

	blacklistedTalkgroupRef = 1514000
	acceptedTalkgroupRef    = 1514100

	// uploadSuccess is the sentence an uploader is given when the server took the recording. The
	// server says the same sentence about a recording its ingest then refused, so a journey that
	// wants to know what the server made of a recording reads its own log view, not this line.
	uploadSuccess = "Call imported successfully.\n"
)

// seededBlacklistServer is a started server holding the journeys' configuration. The configuration is
// put in place before the server starts, because the server reads the upload credentials once and
// answers every later upload against that list: a credential seeded after the start is a credential
// the server never learned about.
func seededBlacklistServer(t *testing.T) *journeyServer {
	t.Helper()

	srv := journeyServerIn(t, t.TempDir())

	seedBlacklistConfiguration(t, srv.db)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	return srv
}

// TestBlacklistingATalkgroupAndSavingTheTalkgroupsStoresBothHalves is the operator's own path: the
// talkgroups level saves its rows together with the system's blacklist in one request, the
// blacklisted talkgroup is gone from the level as the page reads it back, and the system's blacklist
// names the reference — the number a recording names its channel by, not the identifier the store
// gave the row.
func TestBlacklistingATalkgroupAndSavingTheTalkgroupsStoresBothHalves(t *testing.T) {
	srv := seededBlacklistServer(t)

	token := administratorToken(t, srv)

	// The two talkgroups the operator starts from, saved the way the level has always taken them.
	// The row the operator is standing on is row 1, reference 1514000.
	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `[
		{"id": 1, "groupIds": [1], "label": "Dispatch 1", "name": "Dispatch 1", "tagId": 9, "talkgroup": 1, "talkgroupRef": 1514000},
		{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}
	]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the two talkgroups was answered %d: %s", rec.Code, rec.Body.String())
	}

	// Blacklist talkgroup, then Save in the Talkgroups section: one request carrying the rows that
	// are left and the entry that refuses the row that is going away.
	rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `{
		"talkgroups": [{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}],
		"blacklists": "1514000"
	}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("saving the talkgroups with the blacklist was answered %d: %s", rec.Code, rec.Body.String())
	}

	// The level answers the rows it stored: the blacklisted talkgroup is not among them.
	if rows := levelRows(t, rec.Body.Bytes()); len(rows) != 1 || rows[0]["talkgroupRef"] != float64(acceptedTalkgroupRef) {
		t.Fatalf("the talkgroups level answered %v, want the one talkgroup the save left", rows)
	}

	// Reading the page again: the level holds the row that is left.
	if rows := levelRows(t, readAuthorized(t, srv, token, "/api/admin/config/systems/1/talkgroups")); len(rows) != 1 {
		t.Errorf("the talkgroups read back %d rows, want the one the save left", len(rows))
	}

	// And the system's blacklist names the reference. The row identifier 1 is nowhere in it.
	if got := systemField(t, srv, token, 1, "blacklists"); got != "1514000" {
		t.Errorf("the system's blacklist reads %q, want the reference the server compares a recording against", got)
	}

	// The rows the store holds: the blacklisted talkgroup is gone, and no row was re-created under
	// a new identifier beside it.
	if ids := storedTalkgroupIDs(t, srv.db); len(ids) != 1 || ids[0] != 2 {
		t.Errorf("the store holds talkgroup rows %v, want the one row the save left", ids)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// TestARecordingForABlacklistedTalkgroupIsRefusedAndTheSameTalkgroupIsAcceptedWithoutTheEntry
// is what the entry means: the server refuses a call on a talkgroup whose reference its blacklist
// names, and the refusal follows the text, so taking the entry out accepts the call again. The
// system has auto populate off, which is the case the published guide got wrong: the gate is on
// every call whose talkgroup resolves, not only where unconfigured talkgroups are invented.
func TestARecordingForABlacklistedTalkgroupIsRefusedAndTheSameTalkgroupIsAcceptedWithoutTheEntry(t *testing.T) {
	srv := seededBlacklistServer(t)

	token := administratorToken(t, srv)

	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `{
		"talkgroups": [
			{"id": 1, "groupIds": [1], "label": "Dispatch 1", "name": "Dispatch 1", "tagId": 9, "talkgroup": 1, "talkgroupRef": 1514000},
			{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}
		],
		"blacklists": "1514000"
	}`); rec.Code != http.StatusOK {
		t.Fatalf("saving the talkgroups with the blacklist was answered %d: %s", rec.Code, rec.Body.String())
	}

	// The recording on the blacklisted reference. The uploader is told the server took it, which is
	// the answer the server has always given whatever its ingest then made of the call; what the
	// refusal costs is visible in the calls the server keeps and in the line it writes for itself.
	answered := srv.uploadRecording(t, uploadedRecording{
		key:       blacklistUploadKey,
		systemRef: 10001,
		talkgroup: blacklistedTalkgroupRef,
		filename:  "blacklisted.wav",
		audio:     recording(2048),
		at:        time.Now().Add(-90 * time.Second),
	})

	if answered.Code != http.StatusOK || answered.Body.String() != uploadSuccess {
		t.Fatalf("the recording on the blacklisted reference was answered %d %q, want the answer an accepted upload is given",
			answered.Code, answered.Body.String())
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "calls"`); rows != 0 {
		t.Errorf("the server kept %d calls for the blacklisted reference, want none", rows)
	}

	// The refusal is written for the operator to find in the server's own log view: the uploader is
	// told the server took the recording, whatever the ingest makes of it.
	storedLogMentioning(t, srv.db, "talkgroup 1514000 blacklisted")

	// The talkgroup the entry does not name is accepted, so the refusal is the entry and not the
	// system, the credential, or the recording.
	answered = srv.uploadRecording(t, uploadedRecording{
		key:       blacklistUploadKey,
		systemRef: 10001,
		talkgroup: acceptedTalkgroupRef,
		filename:  "accepted.wav",
		audio:     recording(2048),
		at:        time.Now().Add(-80 * time.Second),
	})

	if answered.Code != http.StatusOK || answered.Body.String() != uploadSuccess {
		t.Fatalf("the recording on the talkgroup the entry does not name was answered %d %q, want it accepted",
			answered.Code, answered.Body.String())
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "calls"`); rows != 1 {
		t.Fatalf("the server kept %d calls, want the one it accepted", rows)
	}

	// Take the entry out, the way the entry list's remove control does: the field is saved with the
	// entries that remain, and the same recording is accepted.
	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `{
		"talkgroups": [
			{"id": 1, "groupIds": [1], "label": "Dispatch 1", "name": "Dispatch 1", "tagId": 9, "talkgroup": 1, "talkgroupRef": 1514000},
			{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}
		],
		"blacklists": ""
	}`); rec.Code != http.StatusOK {
		t.Fatalf("saving the talkgroups with the entry removed was answered %d: %s", rec.Code, rec.Body.String())
	}

	answered = srv.uploadRecording(t, uploadedRecording{
		key:       blacklistUploadKey,
		systemRef: 10001,
		talkgroup: blacklistedTalkgroupRef,
		filename:  "released.wav",
		audio:     recording(2048),
		at:        time.Now().Add(-70 * time.Second),
	})

	if answered.Code != http.StatusOK || answered.Body.String() != uploadSuccess {
		t.Fatalf("after the entry was removed, the recording on that reference was answered %d %q, want it accepted",
			answered.Code, answered.Body.String())
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "calls"`); rows != 2 {
		t.Errorf("the server kept %d calls after the entry was removed, want the two it accepted", rows)
	}

	if got := systemField(t, srv, token, 1, "blacklists"); got != "" {
		t.Errorf("the system's blacklist reads %q after the entry was removed, want nothing", got)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// TestSavingTheSystemsSectionAfterBlacklistingLeavesTheBlacklistAndTheRemovedTalkgroupAlone is the
// order the defect ended in: blacklist, save the Talkgroups section, then save the Systems section.
// The Systems section submits the fields of the system row, so it has nothing to write the
// talkgroups with — the row the talkgroups section deleted stays deleted, no new row appears, and
// the entry the operator saved is the text the system keeps.
func TestSavingTheSystemsSectionAfterBlacklistingLeavesTheBlacklistAndTheRemovedTalkgroupAlone(t *testing.T) {
	srv := seededBlacklistServer(t)

	token := administratorToken(t, srv)

	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `{
		"talkgroups": [{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}],
		"blacklists": "1514000"
	}`); rec.Code != http.StatusOK {
		t.Fatalf("saving the talkgroups with the blacklist was answered %d: %s", rec.Code, rec.Body.String())
	}

	// The Systems section, as the page submits it: the fields of the system row, and no copy of the
	// sites, talkgroups, and units the level sections save. The label is what the operator came to
	// change, which is what makes the section ask to be saved.
	rec := srv.put("/api/admin/config/systems", token, `[{
		"alert": "",
		"autoPopulate": false,
		"blacklists": "1514000",
		"delay": 0,
		"id": 1,
		"label": "Grand Metro Renamed",
		"led": "",
		"order": 1,
		"systemRef": 10001,
		"type": ""
	}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("saving the Systems section was answered %d: %s", rec.Code, rec.Body.String())
	}

	// The answer carries the levels as the store holds them, which is the talkgroups section's
	// doing and not the Systems section's.
	systems := systemsSection(t, rec.Body.Bytes())

	if rows, ok := systems[0]["talkgroups"].([]any); !ok || len(rows) != 1 {
		t.Errorf("the systems save answered %v talkgroups, want the one the talkgroups section stored", systems[0]["talkgroups"])
	}

	if got := systemField(t, srv, token, 1, "label"); got != "Grand Metro Renamed" {
		t.Errorf("the system reads %q, want the label the Systems section saved", got)
	}

	if got := systemField(t, srv, token, 1, "blacklists"); got != "1514000" {
		t.Errorf("the system's blacklist reads %q after the Systems save, want the entry the talkgroups save stored", got)
	}

	// No row came back, and no row was created beside it: the talkgroups section is the only owner
	// of the talkgroups.
	if ids := storedTalkgroupIDs(t, srv.db); len(ids) != 1 || ids[0] != 2 {
		t.Errorf("the store holds talkgroup rows %v, want the one row the talkgroups save left", ids)
	}

	if sites := countRows(t, srv.db, `SELECT COUNT(*) FROM "sites"`); sites != 1 {
		t.Errorf("the store holds %d sites after the Systems save, want the one the sites section stored", sites)
	}

	if units := countRows(t, srv.db, `SELECT COUNT(*) FROM "units"`); units != 1 {
		t.Errorf("the store holds %d units after the Systems save, want the one the units section stored", units)
	}

	// The same section, submitted by a client that still sends the levels nested inside the system
	// row — the shape the whole-configuration save uses, and the shape the page had before this
	// feature. The Systems section owns the fields of the system row, so a body that carries a copy
	// of the talkgroups cannot bring the deleted row back: the level sections are the only owners of
	// the levels, whatever a systems-section body names in them.
	rec = srv.put("/api/admin/config/systems", token, `[{
		"alert": "",
		"autoPopulate": false,
		"blacklists": "1514000",
		"delay": 0,
		"id": 1,
		"label": "Grand Metro Renamed",
		"led": "",
		"order": 1,
		"sites": [{"id": 1, "label": "Metro East", "order": 1, "siteRef": 101}],
		"systemRef": 10001,
		"talkgroups": [
			{"id": 1, "groupIds": [1], "label": "Dispatch 1", "name": "Dispatch 1", "tagId": 9, "talkgroup": 1, "talkgroupRef": 1514000},
			{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}
		],
		"type": "",
		"units": [{"id": 1, "label": "Car 1", "order": 1, "unitRef": 1001}]
	}]`)

	if rec.Code != http.StatusOK {
		t.Fatalf("saving the Systems section with the levels nested in the system row was answered %d: %s",
			rec.Code, rec.Body.String())
	}

	// The answer carries the levels as the store holds them: one talkgroup, not the two the body
	// named.
	if rows, ok := systemsSection(t, rec.Body.Bytes())[0]["talkgroups"].([]any); !ok || len(rows) != 1 {
		t.Errorf("the systems save answered %v talkgroups, want the one the talkgroups section stored",
			systemsSection(t, rec.Body.Bytes())[0]["talkgroups"])
	}

	if ids := storedTalkgroupIDs(t, srv.db); len(ids) != 1 || ids[0] != 2 {
		t.Errorf("the store holds talkgroup rows %v after a Systems body naming the deleted row, want the one row the talkgroups save left", ids)
	}

	if sites := countRows(t, srv.db, `SELECT COUNT(*) FROM "sites"`); sites != 1 {
		t.Errorf("the store holds %d sites after that save, want the one the sites section stored", sites)
	}

	if units := countRows(t, srv.db, `SELECT COUNT(*) FROM "units"`); units != 1 {
		t.Errorf("the store holds %d units after that save, want the one the units section stored", units)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// seedBlacklistConfiguration is the configuration the journeys start from, laid out the way the
// conformance fixture lays a system out: one system with auto populate off, two talkgroups whose
// row identifiers differ from their references, one site and one unit so a Systems save has levels
// to leave alone, and the credential the recordings are posted with.
func seedBlacklistConfiguration(t *testing.T, db *sql.DB) {
	t.Helper()

	seed(t, db,
		seedStatement{
			query: `INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (1, 0, '', 0, 'Grand Metro', '', 1, 10001, '')`,
		},
		seedStatement{
			query: `INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (1, '', 0, 0, 'Dispatch 1', '', 'Dispatch 1', 1, 1, 9, 1514000, '')`,
		},
		seedStatement{
			query: `INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (2, '', 0, 0, 'Dispatch 2', '', 'Dispatch 2', 2, 1, 9, 1514100, '')`,
		},
		seedStatement{
			query: `INSERT INTO "sites" ("siteId", "label", "order", "systemId", "siteRef") VALUES (1, 'Metro East', 1, 1, 101)`,
		},
		seedStatement{
			query: `INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitFrom", "unitRef", "unitTo") VALUES (1, 'Car 1', 1, 1, 0, 1001, 0)`,
		},
		// The groups and tags a talkgroup names are the ones a fresh deployment seeds, so the
		// journey's rows name identifiers the server already has.
		seedStatement{
			query: `INSERT INTO "apikeys" ("apikeyId", "disabled", "ident", "key", "order", "systems") VALUES (1, 0, 'blacklist upload', ?, 0, '"*"')`,
			args:  []any{blacklistUploadKey},
		},
		// The recordings are kept as they were posted: the journeys are about which calls the
		// server refuses, not about what ffmpeg makes of them.
		seedStatement{
			query: `INSERT INTO "options" ("key", "value") VALUES ('audioConversion', '0')`,
		},
	)
}

// levelRows reads the rows a level save answers with, which is the list the level endpoint writes
// whether the body it read was the list of the level's rows or the object carrying them beside the
// system's blacklist.
func levelRows(t *testing.T, body []byte) []map[string]any {
	t.Helper()

	var rows []map[string]any

	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("the level answer is not the list of rows the level endpoint writes: %v\n%s", err, body)
	}

	return rows
}

// systemsSection reads the systems the Systems section answers with, which is one entry per system
// carrying that system's levels as the store holds them.
func systemsSection(t *testing.T, body []byte) []map[string]any {
	t.Helper()

	var systems []map[string]any

	if err := json.Unmarshal(body, &systems); err != nil {
		t.Fatalf("the systems section is not the list of systems it writes: %v\n%s", err, body)
	}

	return systems
}

// systemField reads one field of one system out of the systems section, as the administration page
// reads it. A system that stores no blacklist is answered without the key at all, which reads back
// as the empty string.
func systemField(t *testing.T, srv *journeyServer, token string, systemID uint64, field string) string {
	t.Helper()

	systems := systemsSection(t, readAuthorized(t, srv, token, "/api/admin/config/systems"))

	for _, system := range systems {
		if id, ok := system["id"].(float64); ok && uint64(id) == systemID {
			value, _ := system[field].(string)

			return value
		}
	}

	t.Fatalf("the systems section answers no system %d: %v", systemID, systems)

	return ""
}

// storedTalkgroupIDs reads the talkgroup rows the store holds for the journeys' system, in the
// order the store gives them.
func storedTalkgroupIDs(t *testing.T, db *sql.DB) []uint64 {
	t.Helper()

	const query = `SELECT "talkgroupId" FROM "talkgroups" WHERE "systemId" = 1 ORDER BY "talkgroupId"`

	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	defer rows.Close()

	ids := []uint64{}

	for rows.Next() {
		var id uint64

		if err := rows.Scan(&id); err != nil {
			t.Fatalf("reading a talkgroup row the store holds: %v", err)
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return ids
}

// readAuthorized reaches an administration path with the session a login handed out, and hands
// back the answer's body, stopping when the answer is not the one the journey asked for.
func readAuthorized(t *testing.T, srv *journeyServer, token, path string) []byte {
	t.Helper()

	rec := srv.getAuthorized(path, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("reading %s was answered %d: %s", path, rec.Code, rec.Body.String())
	}

	return rec.Body.Bytes()
}
