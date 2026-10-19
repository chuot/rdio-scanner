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

package backend

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The read-only tier's resources: a browsable slice of the recorded history, and the projection of
// the references it names its filters with. Resources are read directly, with no tool call, so what
// a caller is allowed to see has to be decided by the read itself.

func TestTheReadOnlyTierListsItsResourcesAndNoConfiguration(t *testing.T) {
	s := newCallerSession(t, readScoped())

	listed, err := s.ListResources(context.Background(), &mcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("list the resources: %v", err)
	}

	found := false
	for _, resource := range listed.Resources {
		if strings.HasPrefix(resource.URI, "rdio-scanner://config/") {
			t.Fatalf("the read-only tier lists a configuration resource: %s", resource.URI)
		}
		if resource.URI == systemsURI {
			found = true
		}
	}
	if !found {
		t.Fatalf("the systems projection is not in the catalog: %+v", listed.Resources)
	}

	templates, err := s.ListResourceTemplates(context.Background(), &mcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("list the resource templates: %v", err)
	}

	var call *mcp.ResourceTemplate
	for _, template := range templates.ResourceTemplates {
		if template.URITemplate == callURITemplate {
			call = template
		}
	}
	if call == nil {
		t.Fatalf("the history template is not in the catalog: %+v", templates.ResourceTemplates)
	}

	// The template has to name its variables, or a client cannot build a URI to read.
	for _, variable := range []string{"{systemRef}", "{talkgroupRef}", "{?date}"} {
		if !strings.Contains(call.URITemplate, variable) {
			t.Fatalf("the history template %q does not name %s", call.URITemplate, variable)
		}
	}
	if call.MIMEType != "application/json" {
		t.Fatalf("the history template declares %q, want the type a client parses", call.MIMEType)
	}
	if call.Description == "" {
		t.Fatalf("the history template carries no description")
	}
}

func TestTheHistoryTemplateReadsASliceForTheCallersScope(t *testing.T) {
	s := newCallerSession(t, readScoped())

	own := read(t, s, "rdio-scanner://calls/10001/1514000")
	if own.Count != 2 {
		t.Fatalf("the slice for the system the code names answered %d calls, want its two: %+v",
			own.Count, own.Results)
	}

	// A slice naming a system outside the scope is answered as an empty slice, the same answer a
	// talkgroup with nothing recorded gets. Refusing it by name would say what the caller is missing.
	outside := read(t, s, "rdio-scanner://calls/10002/1514000")
	if outside.Count != 0 || len(outside.Results) != 0 {
		t.Fatalf("a slice outside the caller's scope answered %+v, want the empty slice", outside)
	}
}

func TestTheHistoryTemplateSlicesByDay(t *testing.T) {
	s := newCallerSession(t, readScoped())

	today := fixtureTime.Format("2006-01-02")
	yesterday := fixtureTime.Add(-24 * time.Hour).Format("2006-01-02")

	// The same window, narrowed to each of the two days the history is spread over. A date argument
	// the server ignored would answer both the same way.
	sliced := read(t, s, "rdio-scanner://calls/10001/1514000?date="+today)
	if sliced.Count != 2 {
		t.Fatalf("the slice narrowed to %s answered %d calls, want the two recorded on it: %+v",
			today, sliced.Count, sliced.Results)
	}
	for _, row := range sliced.Results {
		if !strings.HasPrefix(row.DateTime, today) {
			t.Fatalf("the slice narrowed to %s carried a call dated %s: %+v",
				today, row.DateTime, sliced.Results)
		}
	}

	other := read(t, s, "rdio-scanner://calls/10001/1514000?date="+yesterday)
	if other.Count != 0 {
		t.Fatalf("the slice narrowed to %s answered %d calls, want none: %+v",
			yesterday, other.Count, other.Results)
	}
}

func TestTheSystemsProjectionIsTheCallersOwnView(t *testing.T) {
	s := newCallerSession(t, readScoped())

	scoped := readSystems(t, s)
	if len(scoped.Systems) != 1 || scoped.Systems[0].SystemRef != systemOneRef {
		t.Fatalf("the scoped projection answered %+v, want only the system the code names", scoped.Systems)
	}

	full := newCallerSession(t, Caller{Tier: TierFull})

	view := readSystems(t, full)
	if len(view.Systems) != 2 {
		t.Fatalf("the administrator's projection answered %+v, want both systems", view.Systems)
	}
}

// read is one resources/read round trip that must answer, read as the history slice it declares.
func read(t *testing.T, s *session, uri string) historySlice {
	t.Helper()

	text := readText(t, s, uri)

	var slice historySlice
	if err := json.Unmarshal([]byte(text), &slice); err != nil {
		t.Fatalf("the slice at %s is not the JSON it declares: %v\n%s", uri, err, text)
	}

	return slice
}

// readSystems is the systems projection, read as the view it declares.
func readSystems(t *testing.T, s *session) systemsView {
	t.Helper()

	text := readText(t, s, systemsURI)

	var view systemsView
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatalf("the systems projection is not the JSON it declares: %v\n%s", err, text)
	}

	return view
}

// readText is one resources/read round trip that must answer with one JSON text.
func readText(t *testing.T, s *session, uri string) string {
	t.Helper()

	read, err := s.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("read %s: %v", uri, err)
	}
	if len(read.Contents) != 1 {
		t.Fatalf("%s carried %d contents, want one", uri, len(read.Contents))
	}
	if read.Contents[0].MIMEType != "application/json" {
		t.Fatalf("%s is served as %q, want the type a client parses", uri, read.Contents[0].MIMEType)
	}

	return read.Contents[0].Text
}

// historySlice is the shape the history slice declares.
type historySlice struct {
	Count   uint `json:"count"`
	Results []struct {
		ID       uint64 `json:"id"`
		System   uint   `json:"system"`
		DateTime string `json:"dateTime"`
	} `json:"results"`
}
