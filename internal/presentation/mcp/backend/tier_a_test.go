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

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The read-only capabilities an access-code caller reaches: the history search, one call, and the
// discovery step every one of them needs. Each test drives the protocol the way an assistant does,
// over the in-memory transport, with the caller the gate would have decided.

// readScoped is the caller of an access code naming one system.
func readScoped() Caller {
	return Caller{
		Tier:   TierReadOnly,
		Ident:  listenerIdent,
		Scope:  scopedTo(systemOneRef),
		Scoped: true,
	}
}

func TestTheTierAToolsDeclareTheirSchemasAndAnnotations(t *testing.T) {
	s := newSession(t, true)

	listed, err := s.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list the tools: %v", err)
	}

	want := map[string][]string{
		"search_calls": {"system", "talkgroup", "group", "tag", "date", "limit", "offset", "sort"},
		"get_call":     {"callId"},
		"list_systems": {},
	}

	for name, properties := range want {
		// The assertions are made on the JSON the client receives, not on the SDK's Go types:
		// the catalog an assistant reads is the wire shape, and that is what must stay honest.
		var declared struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Annotations struct {
				ReadOnlyHint    bool  `json:"readOnlyHint"`
				DestructiveHint *bool `json:"destructiveHint"`
				IdempotentHint  bool  `json:"idempotentHint"`
			} `json:"annotations"`
			InputSchema struct {
				Type       string                     `json:"type"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"inputSchema"`
			OutputSchema *struct {
				Type string `json:"type"`
			} `json:"outputSchema"`
		}

		tool, ok := find(listed, name)
		if !ok {
			t.Fatalf("the catalog carries no %s tool: %+v", name, listed.Tools)
		}

		body, err := json.Marshal(tool)
		if err != nil {
			t.Fatalf("the %s catalog entry does not encode: %v", name, err)
		}
		if err := json.Unmarshal(body, &declared); err != nil {
			t.Fatalf("the %s catalog entry is not the shape a client reads: %v\n%s", name, err, body)
		}

		if !declared.Annotations.ReadOnlyHint {
			t.Fatalf("%s is not declared read-only: %s", name, body)
		}
		if declared.Annotations.DestructiveHint != nil && *declared.Annotations.DestructiveHint {
			t.Fatalf("%s is declared destructive: %s", name, body)
		}
		if !declared.Annotations.IdempotentHint {
			t.Fatalf("%s is not declared idempotent: a read that answers the same page twice is a read", name)
		}
		if declared.Description == "" {
			t.Fatalf("%s carries no description: %s", name, body)
		}
		if declared.InputSchema.Type != "object" {
			t.Fatalf("%s declares no input schema a client can fill: %s", name, body)
		}
		if declared.OutputSchema == nil {
			t.Fatalf("%s declares no output schema, so an assistant reads its answer as prose", name)
		}

		for _, property := range properties {
			if _, ok := declared.InputSchema.Properties[property]; !ok {
				t.Fatalf("%s takes no %q argument: %s", name, property, body)
			}
		}
	}
}

func TestSearchCallsAnswersAPageForAnAccessCodeCaller(t *testing.T) {
	s := newCallerSession(t, readScoped())

	answered := call(t, s, "search_calls", map[string]any{"limit": 2})

	var page struct {
		Count   uint `json:"count"`
		Results []struct {
			ID     uint64 `json:"id"`
			System uint   `json:"system"`
		} `json:"results"`
	}
	decode(t, answered, &page)

	if page.Count != 2 {
		t.Fatalf("the scoped search answered %d calls, want the two on the system the code names: %+v",
			page.Count, page.Results)
	}
	for _, row := range page.Results {
		if row.System != systemOneRef {
			t.Fatalf("the scoped search returned call %d on system %d, outside the caller's scope",
				row.ID, row.System)
		}
	}
}

// A filter naming a system the caller may not see answers the empty page, the same answer a
// history with nothing in it gets. Naming the filter refused would say what the caller is missing.
func TestSearchCallsAnswersNoRowsForASystemOutsideTheCallersScope(t *testing.T) {
	s := newCallerSession(t, readScoped())

	answered := call(t, s, "search_calls", map[string]any{"system": systemTwoRef})

	var page struct {
		Count uint `json:"count"`
	}
	decode(t, answered, &page)

	if page.Count != 0 {
		t.Fatalf("a search outside the caller's scope answered %d rows, want none", page.Count)
	}
}

func TestGetCallAnswersOneCallAndNamesWhereItsRecordingIs(t *testing.T) {
	s := newCallerSession(t, readScoped())

	answered := call(t, s, "get_call", map[string]any{"callId": 1})

	var detail struct {
		ID       uint64 `json:"id"`
		AudioURL string `json:"audioUrl"`
	}
	decode(t, answered, &detail)

	if detail.ID != 1 {
		t.Fatalf("get_call answered call %d, want call 1", detail.ID)
	}
	if detail.AudioURL != "/api/calls/1/audio" {
		t.Fatalf("get_call names the recording as %q, want the entry point the REST surface serves",
			detail.AudioURL)
	}
}

// A call the caller may not see is answered exactly as a call that is not there.
func TestGetCallAnswersACallOutsideTheCallersScopeAsAbsent(t *testing.T) {
	s := newCallerSession(t, readScoped())

	outside := refusedCall(t, s, "get_call", map[string]any{"callId": 3})
	missing := refusedCall(t, s, "get_call", map[string]any{"callId": 999})

	if outside == missing {
		return
	}
	t.Fatalf("the two refusals differ: %q vs %q, and the difference names the call the caller may not see",
		outside, missing)
}

// refusedCall is the answer to a call that must be refused, read as the text an assistant reads.
func refusedCall(t *testing.T, s *session, tool string, args map[string]any) string {
	t.Helper()

	answered, err := callTool(s, tool, args)
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	if !answered.IsError {
		t.Fatalf("%s answered %+v, want a refusal", tool, answered.Content)
	}

	var lines []string
	for _, content := range answered.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			lines = append(lines, text.Text)
		}
	}

	return fmt.Sprint(lines)
}

func TestListSystemsProjectsOnlyTheSystemsTheCallerMaySee(t *testing.T) {
	s := newCallerSession(t, readScoped())

	answered := call(t, s, "list_systems", map[string]any{})

	var view struct {
		Systems []struct {
			SystemRef uint `json:"systemRef"`
		} `json:"systems"`
	}
	decode(t, answered, &view)

	if len(view.Systems) != 1 || view.Systems[0].SystemRef != systemOneRef {
		t.Fatalf("the scoped projection answered %+v, want only the system the code names", view.Systems)
	}

	full := newCallerSession(t, Caller{Tier: TierFull})

	answered = call(t, full, "list_systems", map[string]any{})
	decode(t, answered, &view)

	if len(view.Systems) != 2 {
		t.Fatalf("the administrator's projection answered %d systems, want both", len(view.Systems))
	}
}

// find is a tool in the catalog by name.
func find(listed *mcp.ListToolsResult, name string) (*mcp.Tool, bool) {
	for _, tool := range listed.Tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return nil, false
}

// callTool is one tools/call round trip, with the failure left to the test to read.
func callTool(s *session, tool string, args map[string]any) (*mcp.CallToolResult, error) {
	return s.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
}

// call is one tools/call round trip that must answer.
func call(t *testing.T, s *session, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	answered, err := callTool(s, tool, args)
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	if answered.IsError {
		t.Fatalf("%s answered a failure: %+v", tool, answered.Content)
	}
	return answered
}

// decode reads a tool's structured answer into the shape the test asks about.
func decode(t *testing.T, answered *mcp.CallToolResult, into any) {
	t.Helper()

	if answered.StructuredContent == nil {
		t.Fatal("the tool carried no structured answer")
	}

	body, err := json.Marshal(answered.StructuredContent)
	if err != nil {
		t.Fatalf("the structured answer does not re-encode: %v", err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("the structured answer is not the shape it declares: %v\n%s", err, body)
	}
}
