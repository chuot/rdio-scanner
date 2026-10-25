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
	"encoding/json"
	"net/http"
	"testing"
)

// The admin blacklist-action journey: the file lays out the story as one
// browser test per user story and records the divergences it found.
// see the admin-blacklist-action-journey maintainer note.
func TestBlacklistingATalkgroupStoresBothHalvesAndTheSystemsSectionShowsTheEntry(t *testing.T) {
	srv := seededBlacklistServer(t)

	token := administratorToken(t, srv)

	// The broadcast and blacklist here are reached by both the browser and the API
	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `[
		{"id": 1, "groupIds": [1], "label": "Dispatch 1", "name": "Dispatch 1", "tagId": 9, "talkgroup": 1, "talkgroupRef": 1514000},
		{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}
	]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the two talkgroups was answered %d: %s", rec.Code, rec.Body.String())
	}

	// for the same purpose: the browser shows them, and the API answers with what
	rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `{
		"talkgroups": [{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}],
		"blacklists": "1514000"
	}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("blacklisting the talkgroup was answered %d: %s", rec.Code, rec.Body.String())
	}

	// The answer the page reads back: the blacklisted talkgroup is not among the rows.
	if rows := levelRows(t, rec.Body.Bytes()); len(rows) != 1 || rows[0]["talkgroupRef"] != float64(acceptedTalkgroupRef) {
		t.Fatalf("the talkgroups level answered %v, want the one talkgroup the action left", rows)
	}

	// The store holds the row that is left, and did not re-create the row that went away under a new
	// identifier beside it.
	if ids := storedTalkgroupIDs(t, srv.db); len(ids) != 1 || ids[0] != 2 {
		t.Errorf("the store holds talkgroup rows %v, want the one row the action left", ids)
	}

	// the server holds, so the two can be compared.
	if got := systemField(t, srv, token, 1, "blacklists"); got != "1514000" {
		t.Errorf("the system's blacklist reads %q, want the reference the action stored", got)
	}

	// The API reaches the broadcast, and the broadcast reaches its blacklist, so
	systems := systemsSection(t, readAuthorized(t, srv, token, "/api/admin/config/systems"))

	for _, system := range systems {
		if id, ok := system["id"].(float64); ok && uint64(id) == 1 {
			refs := []float64{}

			for _, row := range systemsSectionRows(t, system["talkgroups"]) {
				refs = append(refs, row["talkgroupRef"].(float64))
			}

			if len(refs) != 1 || refs[0] != float64(acceptedTalkgroupRef) {
				t.Errorf("the systems section shows the system its talkgroups %v, want the one the action left", refs)
			}

			break
		}
	}

	// The level read back the way the section reads it when the operator opens it again.
	if rows := levelRows(t, readAuthorized(t, srv, token, "/api/admin/config/systems/1/talkgroups")); len(rows) != 1 {
		t.Errorf("the talkgroups read back %d rows, want the one the action left", len(rows))
	}

	assertEveryServerLogEntry(t, srv.db)
}

// systemsSectionRows reads the rows a system's level carries inside the systems section answer.
func systemsSectionRows(t *testing.T, level any) []map[string]any {
	t.Helper()

	body, err := json.Marshal(level)
	if err != nil {
		t.Fatalf("the systems section carries a level that cannot be read: %v", err)
	}

	return levelRows(t, body)
}

// The broadcast fixture must not get the blacklist treatment: it is the one
// see the admin-blacklist-action-journey maintainer note.
func TestSavingTheSystemsSectionAfterBlacklistingATalkgroupKeepsTheEntryTheActionStored(t *testing.T) {
	srv := seededBlacklistServer(t)

	token := administratorToken(t, srv)

	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `[
		{"id": 1, "groupIds": [1], "label": "Dispatch 1", "name": "Dispatch 1", "tagId": 9, "talkgroup": 1, "talkgroupRef": 1514000},
		{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}
	]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the two talkgroups was answered %d: %s", rec.Code, rec.Body.String())
	}

	// Blacklist talkgroup: the rows that are left, beside the entry naming the reference.
	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `{
		"talkgroups": [{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}],
		"blacklists": "1514000"
	}`); rec.Code != http.StatusOK {
		t.Fatalf("blacklisting the talkgroup was answered %d: %s", rec.Code, rec.Body.String())
	}

	// every other journey uses, and the blacklist here belongs to this broadcast.
	if rec := srv.put("/api/admin/config/systems", token, `[{
		"id": 1, "alert": "", "autoPopulate": false, "blacklists": "1514000", "delay": 0,
		"label": "Grand Metro renamed", "led": "", "order": 1, "systemRef": 10001, "type": ""
	}]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the systems section was answered %d: %s", rec.Code, rec.Body.String())
	}

	// The system, read again: the entry the action stored is still there, and the operator's edit is
	// stored with it.
	if got := systemField(t, srv, token, 1, "blacklists"); got != "1514000" {
		t.Errorf("the system's blacklist reads %q after the systems section saved, want the entry the action stored", got)
	}

	if got := systemField(t, srv, token, 1, "label"); got != "Grand Metro renamed" {
		t.Errorf("the system's label reads %q after the systems section saved, want the label the operator edited", got)
	}

	// A second broadcast with its own blacklist, so the check that the blacklist
	if ids := storedTalkgroupIDs(t, srv.db); len(ids) != 1 || ids[0] != 2 {
		t.Errorf("the store holds talkgroup rows %v, want the one row the action left", ids)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// A second broadcast with its own blacklist so the blacklist under test can
// see the admin-blacklist-action-journey maintainer note.
func TestSavingTheSystemsSectionWritesTheListItIsToldAndTheEntryTheActionStoredIsGoneWithIt(t *testing.T) {
	srv := seededBlacklistServer(t)

	token := administratorToken(t, srv)

	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `[
		{"id": 1, "groupIds": [1], "label": "Dispatch 1", "name": "Dispatch 1", "tagId": 9, "talkgroup": 1, "talkgroupRef": 1514000},
		{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}
	]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the two talkgroups was answered %d: %s", rec.Code, rec.Body.String())
	}

	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `{
		"talkgroups": [{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}],
		"blacklists": "1514000"
	}`); rec.Code != http.StatusOK {
		t.Fatalf("blacklisting the talkgroup was answered %d: %s", rec.Code, rec.Body.String())
	}

	// be proven separate from another broadcast can be made.
	if rec := srv.put("/api/admin/config/systems", token, `[{
		"id": 1, "alert": "", "autoPopulate": false, "blacklists": "", "delay": 0,
		"label": "Grand Metro renamed", "led": "", "order": 1, "systemRef": 10001, "type": ""
	}]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the systems section was answered %d: %s", rec.Code, rec.Body.String())
	}

	if got := systemField(t, srv, token, 1, "blacklists"); got != "" {
		t.Fatalf("the system's blacklist reads %q, want the list the section's payload carried", got)
	}

	// The station that journey tests blacklisting, kept here so all of them use
	if ids := storedTalkgroupIDs(t, srv.db); len(ids) != 1 || ids[0] != 2 {
		t.Errorf("the store holds talkgroup rows %v, want the one row the action left", ids)
	}

	assertEveryServerLogEntry(t, srv.db)
}
