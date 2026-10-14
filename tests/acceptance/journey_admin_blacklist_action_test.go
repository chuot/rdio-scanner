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
	"encoding/json"
	"net/http"
	"testing"
)

// Blacklisting a talkgroup and then opening the system, on the server a deployment runs.
//
// The operator's report is the empty field: the action removed the talkgroup row, the operator went to
// the Systems section and reloaded it, and the Blacklists field held nothing. What the journey protects
// is the one request the action sends and the reading the operator makes after it — the system's own
// section, read the way the page reads it when the section is opened. No save of any section follows
// the action, so the request the click makes is the whole of what is stored.
//
// The rows are laid out the way the conformance fixture lays them out: the first system's first
// talkgroup is row 1 and reference 1514000, and the two numbers differ, which is what makes a stored
// entry say which of the two it names.
func TestBlacklistingATalkgroupStoresBothHalvesAndTheSystemsSectionShowsTheEntry(t *testing.T) {
	srv := seededBlacklistServer(t)

	token := administratorToken(t, srv)

	// The two talkgroups the operator starts from, saved the way the section saves them. The row the
	// operator is standing on is row 1, reference 1514000.
	if rec := srv.put("/api/admin/config/systems/1/talkgroups", token, `[
		{"id": 1, "groupIds": [1], "label": "Dispatch 1", "name": "Dispatch 1", "tagId": 9, "talkgroup": 1, "talkgroupRef": 1514000},
		{"id": 2, "groupIds": [1], "label": "Dispatch 2", "name": "Dispatch 2", "tagId": 9, "talkgroup": 2, "talkgroupRef": 1514100}
	]`); rec.Code != http.StatusOK {
		t.Fatalf("saving the two talkgroups was answered %d: %s", rec.Code, rec.Body.String())
	}

	// The one request the Blacklist action sends: the rows that are left, beside the entry naming the
	// reference of the row that is going away.
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

	// The Systems section, read the way the operator opening it after the action reads it: the field
	// carries the entry the action stored, naming the reference and not the row identifier.
	if got := systemField(t, srv, token, 1, "blacklists"); got != "1514000" {
		t.Errorf("the system's blacklist reads %q, want the reference the action stored", got)
	}

	// The same reading carries the talkgroups the action stored: the blacklisted talkgroup is gone
	// from the system the operator opens, which is what the operator sees when the section is reloaded.
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
