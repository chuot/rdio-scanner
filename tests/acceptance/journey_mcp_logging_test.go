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
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// the assistant's work has to show up in the operator's log view under a filterable
// subsystem, and that view must never carry a session credential: it is shared freely.

func TestTheAssistantsWorkIsInTheLogViewTheOperatorFiltersBySubsystem(t *testing.T) {
	srv := startedServerNamingTheAssistant(t)

	token := srv.loginAnswered(t)

	endpoint := httptest.NewServer(srv.server)
	t.Cleanup(endpoint.Close)

	session := theAssistantConnected(t, endpoint.URL, token)

	// an access list makes the server restricted: a caller that proves nothing is a caller
	// the credential gate refuses, the event the operator wants to see.
	if saved := srv.put("/api/admin/config/access", token, `[
	  { "id": 0, "code": "the-journey-access-code", "ident": "Journey desk", "systems": "*" }
	]`); saved.Code != http.StatusOK {
		t.Fatalf("saving the access list was answered %d: %s", saved.Code, saved.Body.String())
	}

	// a caller the credential gate refuses: somebody knocked on the assistant's endpoint
	// with nothing to show, the event the operator wants to see.
	resp, err := http.Post(endpoint.URL+"/mcp", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("knocking on the assistant's endpoint: %v", err)
	}

	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a caller with no credential was answered %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	// The log view, filtered by the subsystem the assistant's work is filed under.
	view := theLogViewTheOperatorSees(t, session, map[string]any{"source": "mcp", "limit": float64(100)})

	if len(view.Entries) == 0 {
		t.Fatal("the log view filtered by the assistant's subsystem answered no entries: the operator's " +
			"question is what the assistant did, and the filter has to answer it")
	}

	for _, entry := range view.Entries {
		if entry.Source != "mcp" {
			t.Fatalf("the log view filtered by %q answered an entry filed under %q: the filter answers "+
				"the subsystem it names", "mcp", entry.Source)
		}
	}

	if !anyOfTheEntriesNames(view.Entries, "127.0.0.1") {
		t.Fatalf("the assistant's subsystem holds %v, and none of it names the address the refused caller "+
			"came from: the operator's question is who kept knocking, and the address is the answer",
			messagesOfTheEntries(view.Entries))
	}

	// no entry in the whole log view carries the administrator's session credential: the
	// view is read out loud, pasted into tickets, and handed to clients.
	everything := theLogViewTheOperatorSees(t, session, map[string]any{"limit": float64(500)})

	for _, entry := range everything.Entries {
		if strings.Contains(entry.Message, token) {
			t.Fatalf("the log view holds %q filed under %s, and the entry carries the session credential "+
				"the login handed out: a log view is read out loud, pasted into a ticket, and handed to "+
				"a client", entry.Message, entry.Source)
		}
	}
}

// theLogViewTheOperatorSees is the administrator's log search asked through the
// assistant's own capability: operator's panel and tool read one view.
func theLogViewTheOperatorSees(t *testing.T, session *mcp.ClientSession, criteria map[string]any) logViewAnswer {
	t.Helper()

	text := askTheAssistant(t, session, "search_logs", criteria)

	var view logViewAnswer
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatalf("decoding the log view: %v\n%s", err, text)
	}

	return view
}

// logViewAnswer is the page of the log view as the capability answers it.
type logViewAnswer struct {
	Count   uint64         `json:"count"`
	Entries []logViewEntry `json:"entries"`
}

// logViewEntry is one entry of the log view.
type logViewEntry struct {
	ID      uint64 `json:"id"`
	Level   string `json:"level"`
	Source  string `json:"source"`
	Message string `json:"message"`
}

// askTheAssistant asks for one capability and hands back what it answered.
func askTheAssistant(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) string {
	t.Helper()

	answered, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}

	text := ""

	for _, content := range answered.Content {
		if line, ok := content.(*mcp.TextContent); ok {
			text += line.Text
		}
	}

	if answered.IsError {
		t.Fatalf("%s answered a failure: %s", tool, text)
	}

	return text
}

func anyOfTheEntriesNames(entries []logViewEntry, name string) bool {
	for _, entry := range entries {
		if strings.Contains(entry.Message, name) {
			return true
		}
	}

	return false
}

func messagesOfTheEntries(entries []logViewEntry) []string {
	messages := []string{}

	for _, entry := range entries {
		messages = append(messages, entry.Message)
	}

	return messages
}
