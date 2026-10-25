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
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The catalog is checked against the shape the surface was built to have:
// capabilities named for the operator's questions, not for the transport. An
// endpoint mirror is the same HTTP API with an extra protocol in front.

func TestNoCapabilityIsATransportShape(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	tools, err := s.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list the tools: %v", err)
	}

	for _, tool := range tools.Tools {
		if shape := transportShaped(tool.Name); shape != "" {
			t.Fatalf("%s is a %s: a capability named for the transport is a REST call with a protocol in front of it",
				tool.Name, shape)
		}
	}

	// The matcher has to catch the names told not to be written, or the check
	// passes by looking at nothing.
	for _, name := range []string{
		"getLogs",
		"get_logs",
		"getFeed",
		"updateFeed",
		"restartFeed",
		"post_admin_logs",
		"put_config",
		"search_calls_by_system",
		"search_calls_by_tag",
		"search_logs_by_level",
		"get_call_audio",
		"list_systems_talkgroups",
	} {
		if transportShaped(name) == "" {
			t.Fatalf("%s is the shape this surface was told not to write, and the check does not catch it", name)
		}
	}
}

// transportShaped names how a capability name is a transport shape, or the
// empty string when it is a question. Refused: a contract-named endpoint
// mirror, a transport verb as a name, a per-filter search clone, an endpoint
// path with separators changed. A bare 'get' is not refused.
func transportShaped(name string) string {
	lower := strings.ToLower(name)
	flat := strings.ReplaceAll(lower, "_", "")

	// The endpoint mirrors the contract names and their separator spellings:
	// the shapes a client could assemble into a workflow.
	for _, shape := range []string{
		"getlogs", "getfeed", "updatefeed", "restartfeed",
		"adminlogs", "callssearch", "adminconfig", "adminalerts", "adminlogin", "adminlogout",
		"adminuseradd", "adminuserremove", "callaudio", "callupload",
	} {
		if strings.HasPrefix(flat, shape) || strings.Contains(flat, shape) {
			return "endpoint mirror of " + shape
		}
	}

	// No capability name is a transport verb: post, put, patch, delete are the
	// transport's, and the product never speaks of posting a thing.
	for _, verb := range []string{"post", "put", "patch", "delete"} {
		if strings.HasPrefix(flat, verb) {
			return "transport method " + verb
		}
	}

	// A per-filter clone moves a filter from its arguments into its name: the
	// tell is a search verb carrying a dataset and a filter, or the filter
	// named as a clause of its own.
	if strings.Contains(lower, "_by_") {
		return "filter moved into its name as a clause"
	}

	parts := strings.Split(lower, "_")
	if len(parts) >= 3 {
		for _, filter := range []string{
			"system", "systems", "talkgroup", "talkgroups", "tag", "tags", "group", "groups",
			"date", "level", "source", "sources", "audio", "site", "sites", "unit", "units",
			"config", "logs", "feed",
		} {
			if parts[len(parts)-1] == filter {
				return "filter moved into its name, ending " + filter
			}
		}
	}

	return ""
}

// TestEveryToolDeclaresItsEntryPoints: every capability says what it reaches, so
// nobody can misread its cost, and the description carries the entry points
// the inventory names.
func TestEveryToolDeclaresItsEntryPoints(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	tools, err := s.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list the tools: %v", err)
	}

	for _, tool := range tools.Tools {
		capability, ok := catalogOf(tool.Name)
		if !ok {
			t.Fatalf("%s is registered and not inventoried", tool.Name)
		}

		if len(capability.Endpoints) == 0 {
			t.Fatalf("%s declares no entry point it is built from", tool.Name)
		}

		if tool.Description == "" {
			t.Fatalf("%s carries no description for an assistant to choose from", tool.Name)
		}

		for _, endpoint := range capability.Endpoints {
			path := endpoint[strings.Index(endpoint, " ")+1:]

			if !strings.Contains(tool.Description, path) {
				t.Fatalf("%s is built from %s and says nothing about it: %s", tool.Name, endpoint, tool.Description)
			}
		}

		// A capability composing several entry points says so in its description:
		// an assistant reading one call as one request is surprised by the cost.
		if len(capability.Endpoints) > 1 && !strings.Contains(tool.Description, "and") {
			t.Fatalf("%s composes %d entry points and its description does not say so", tool.Name, len(capability.Endpoints))
		}
	}
}
