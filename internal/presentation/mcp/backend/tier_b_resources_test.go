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

// The administrator's configuration resources: the sections an operator inspects often enough to
// read them directly, with no tool call in between. Two of them inventory credentials, and a
// credential inventory read by an assistant must not hand out the credentials it inventories.

func TestTheAdministratorReadsEveryConfigurationResource(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	uris := []string{
		optionsURI,
		configSystemsURI,
		groupsURI,
		tagsURI,
		accessURI,
		apikeysURI,
		dirwatchesURI,
		downstreamsURI,
		alertsURI,
	}

	listed, err := s.ListResources(context.Background(), &mcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("list the resources: %v", err)
	}

	for _, uri := range uris {
		found := false
		for _, resource := range listed.Resources {
			if resource.URI == uri {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s is not in the catalog an assistant reads: %+v", uri, listed.Resources)
		}

		text := readText(t, s, uri)

		if !json.Valid([]byte(text)) {
			t.Fatalf("%s is not JSON a client can parse: %s", uri, text)
		}

		if text == "null" || text == "" {
			t.Fatalf("%s answers %q, and the fixture holds a configuration: %s", uri, text, text)
		}
	}
}

// The credential inventories. The point of reading them is to know which listeners and which
// capture tools exist and what they are entitled to; the secret itself is not part of that
// question, and an assistant that repeats a code into a chat log has leaked it.
func TestTheCredentialInventoriesAreMasked(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	secrets := []string{listenerCode, fixtureApikey}

	for _, uri := range []string{accessURI, apikeysURI, downstreamsURI} {
		text := readText(t, s, uri)

		for _, secret := range secrets {
			if strings.Contains(text, secret) {
				t.Fatalf("%s carries the credential %q, which is what a credential inventory must not hand out: %s",
					uri, secret, text)
			}
		}

		if !strings.Contains(text, maskedCredential) {
			t.Fatalf("%s does not say which fields it masked: %s", uri, text)
		}
	}

	// The inventories still answer the questions they are read for: who the code belongs to,
	// when it stops working, and what it is entitled to.
	access := readText(t, s, accessURI)

	var rows []struct {
		Ident   string          `json:"ident"`
		Systems json.RawMessage `json:"systems"`
	}
	decodeInto(t, access, &rows)

	if len(rows) != 1 {
		t.Fatalf("the access inventory answered %d rows, want the fixture's one: %s", len(rows), access)
	}
	if rows[0].Ident != listenerIdent {
		t.Fatalf("the access inventory names %q, want the code's own identity: %s", rows[0].Ident, access)
	}
	if string(rows[0].Systems) == "null" || len(rows[0].Systems) == 0 {
		t.Fatalf("the access inventory names no scope, and the fixture's code names one: %s", access)
	}
}

// The read-only tier reads no configuration resource: not in its catalog, and not by URI either.
// The routing hands a caller the set its credential opens, so a resource the caller was never
// handed is not there to read, and the answer says nothing about the deployment.
func TestTheReadOnlyTierSeesNoConfigurationResource(t *testing.T) {
	s := newCallerSession(t, readScoped())

	listed, err := s.ListResources(context.Background(), &mcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("list the resources: %v", err)
	}

	for _, resource := range listed.Resources {
		if strings.HasPrefix(resource.URI, "rdio-scanner://config/") {
			t.Fatalf("the read-only tier lists a configuration resource: %s", resource.URI)
		}
	}

	for _, uri := range []string{optionsURI, accessURI, apikeysURI, alertsURI} {
		_, err := s.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
		if err == nil {
			t.Fatalf("%s answered an access-code caller", uri)
		}
	}

	// The read-only tier's own resources are readable: the rule above is a tier rule, not a rule
	// against reading.
	if _, err := s.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: versionURI}); err != nil {
		t.Fatalf("the version resource refused an access-code caller: %v", err)
	}
}

// One level of one system, read by a template a client fills in: the level lists are long enough
// that an assistant asks for one system's talkgroups, not the whole tree, and the whole tree is
// beside it as a resource.
func TestTheSystemLevelTemplatesReadOneLevelOfOneSystem(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	for _, probe := range []struct {
		uri  string
		want int
	}{
		{"rdio-scanner://systems/10001/talkgroups", 2},
		{"rdio-scanner://systems/10001/sites", 1},
		{"rdio-scanner://systems/10001/units", 1},
		{"rdio-scanner://systems/10002/talkgroups", 1},
	} {
		text := readText(t, s, probe.uri)

		var rows []json.RawMessage
		decodeInto(t, text, &rows)

		if len(rows) != probe.want {
			t.Fatalf("%s answered %d rows, want %d: %s", probe.uri, len(rows), probe.want, text)
		}
	}

	// A system this server does not hold answers the empty list, the same answer a system with
	// nothing under it gets.
	text := readText(t, s, "rdio-scanner://systems/99999/talkgroups")

	var rows []json.RawMessage
	decodeInto(t, text, &rows)

	if len(rows) != 0 {
		t.Fatalf("a system this server does not hold answered %d rows: %s", len(rows), text)
	}
}

// A browsable slice of the log: one severity, one day. The search tool takes every filter; this is
// the slice a client reads when it is browsing.
func TestTheLogTemplateReadsOneSeverityOfOneDay(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	day := fixtureTime.Format("2006-01-02")

	failures := readText(t, s, "rdio-scanner://logs/error?date="+day)

	var rows []logEntryShape
	decodeInto(t, failures, &rows)

	if len(rows) != 1 || rows[0].Level != "error" {
		t.Fatalf("the error slice answered %+v, want the one error line the fixture carries", rows)
	}

	warnings := readText(t, s, "rdio-scanner://logs/warn?date="+day)

	var warned []logEntryShape
	decodeInto(t, warnings, &warned)

	if len(warned) != 3 {
		t.Fatalf("the warning slice answered %d lines, want the three the fixture carries: %+v", len(warned), warned)
	}

	for _, row := range warned {
		if row.Level != "warn" {
			t.Fatalf("the warning slice carried a line of severity %q: %+v", row.Level, warned)
		}
	}

	// A severity the server does not file entries under answers the empty slice, and a day with
	// nothing in it answers the empty slice.
	other := readText(t, s, "rdio-scanner://logs/error?date="+fixtureTime.Add(-24*time.Hour).Format("2006-01-02"))

	var empty []logEntryShape
	decodeInto(t, other, &empty)

	if len(empty) != 0 {
		t.Fatalf("a day with nothing in it answered %d lines: %+v", len(empty), empty)
	}
}

// decodeInto reads a resource body into the shape the test asks about: a resource that declares
// one shape and answers another is caught here.
func decodeInto(t *testing.T, text string, into any) {
	t.Helper()

	if err := json.Unmarshal([]byte(text), into); err != nil {
		t.Fatalf("the resource is not the JSON it declares: %v\n%s", err, text)
	}
}
