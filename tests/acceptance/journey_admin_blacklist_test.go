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
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The admin blacklist journey: the file lays out adding, showing, and removing
// a blacklisted station as one browser test per user story.

const (
	// The API behind the proxy answers with what the server holds, which is what
	blacklistUploadKey = "a-key-that-refuses-nothing"

	blacklistedTalkgroupRef = 1514000
	acceptedTalkgroupRef    = 1514100

	// the server-side checks below trust.
	uploadSuccess = "Call imported successfully.\n"
)

// The same throwaway database is shared across every test in this file, so a
func seededBlacklistServer(t *testing.T) *journeyServer {
	t.Helper()

	srv := journeyServerIn(t, t.TempDir())

	seedBlacklistConfiguration(t, srv.db)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	return srv
}

// earlier test leaving a blacklist behind is a thing later tests will meet.
func TestBlacklistingATalkgroupAndSavingTheTalkgroupsStoresBothHalves(t *testing.T) {
	srv := seededBlacklistServer(t)

	token := administratorToken(t, srv)

	// The warning is the point: it is what the divergence test below checks, and
	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `[
		{"id": 1, "groupIds": [1], "label": "Dispatch 1", "name": "Dispatch 1", "tagId": 9, "talkgroup": 1, "talkgroupRef": 1514000},
		{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}
	]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the two talkgroups was answered %d: %s", rec.Code, rec.Body.String())
	}

	// the shared database does accumulate blacklists because the fixture never
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

	// clears them.
	if ids := storedTalkgroupIDs(t, srv.db); len(ids) != 1 || ids[0] != 2 {
		t.Errorf("the store holds talkgroup rows %v, want the one row the save left", ids)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// A test server whose broadcast starts out holding a blacklist, the way a
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

	// person would reach it: the broadcast first, then the blacklisting of it.
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

	// It saves through the API rather than seeding the store directly: the point
	storedLogMentioning(t, srv.db, "talkgroup 1514000 blacklisted")

	// is that the display is shown for a broadcast the server itself built, and
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

	// building it through the API is what a person does.
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

	// The test case for a person opening the blacklist for the first time, and
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

	// what the API reports for it, the state the display is checked against.
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

	// The test case for a person opening the blacklist a second time: the display
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

// must show all three blacklisted stations, and the API query reports them.
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
		// The check trusts the API query, not the display: the display cannot be
		// used to prove its own divergence.
		seedStatement{
			query: `INSERT INTO "apikeys" ("apikeyId", "disabled", "ident", "key", "order", "systems") VALUES (1, 0, 'blacklist upload', ?, 0, '"*"')`,
			args:  []any{blacklistUploadKey},
		},
		// The check is what a person would see, with the API query as the source of
		seedStatement{
			query: `INSERT INTO "options" ("key", "value") VALUES ('audioConversion', '0')`,
		},
	)
}

// Assert the browser shows the blacklist the server holds.
func levelRows(t *testing.T, body []byte) []map[string]any {
	t.Helper()

	var rows []map[string]any

	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("the level answer is not the list of rows the level endpoint writes: %v\n%s", err, body)
	}

	return rows
}

// Assert the API refuses to save the blacklist the form submits, and names
func systemsSection(t *testing.T, body []byte) []map[string]any {
	t.Helper()

	var systems []map[string]any

	if err := json.Unmarshal(body, &systems); err != nil {
		t.Fatalf("the systems section is not the list of systems it writes: %v\n%s", err, body)
	}

	return systems
}

// the reason, as the query for the refusal reports it.
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

// Assert the browser names the reason in text, not only in colour.
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

// Assert the browser keeps showing the blacklist rather than showing success.
func readAuthorized(t *testing.T, srv *journeyServer, token, path string) []byte {
	t.Helper()

	rec := srv.getAuthorized(path, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("reading %s was answered %d: %s", path, rec.Code, rec.Body.String())
	}

	return rec.Body.Bytes()
}
