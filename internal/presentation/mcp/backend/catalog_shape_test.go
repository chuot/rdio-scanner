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
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The catalog is checked against the shape the surface was built to have: capabilities named for
// the questions an operator asks, and not for the transport that carries the answers. A surface that
// mirrors its endpoints one for one is the same HTTP API with an extra protocol on front of it, and
// an assistant reading `get_logs`, `get_logs_by_level`, `get_logs_by_source` has to assemble the
// workflow itself, which is the part an assistant gets wrong.

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

	// The matcher has to catch the names the surface was told not to write, or it is a check that
	// passes because it looks at nothing.
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

// transportShaped names how a capability name is a transport shape, or answers the empty string when
// it is a question.
//
// Four shapes are refused: a name mirroring an endpoint the contract says stays a workflow, a
// transport method used as a capability's name, a per-filter clone of a search, and an endpoint's
// own path with its separators changed.
//
// "get" is not refused on its own: reading one recorded call is what an operator asks for in the
// product's own words, and the contract names that capability. What is refused is the endpoint
// mirror the contract names, and the verbs that exist only in the transport.
func transportShaped(name string) string {
	lower := strings.ToLower(name)
	flat := strings.ReplaceAll(lower, "_", "")

	// The endpoint mirrors the contract names by name, and any spelling of them with the
	// separators changed. These are the shapes a client would have to assemble into a workflow.
	for _, shape := range []string{
		"getlogs", "getfeed", "updatefeed", "restartfeed",
		"adminlogs", "callssearch", "adminconfig", "adminalerts", "adminlogin", "adminlogout",
		"adminuseradd", "adminuserremove", "callaudio", "callupload",
	} {
		if strings.HasPrefix(flat, shape) || strings.Contains(flat, shape) {
			return "endpoint mirror of " + shape
		}
	}

	// A method's name is not a capability's name. Nothing in the product speaks of posting,
	// putting, patching, or deleting a thing; those are the transport's verbs.
	for _, verb := range []string{"post", "put", "patch", "delete"} {
		if strings.HasPrefix(flat, verb) {
			return "transport method " + verb
		}
	}

	// A per-filter clone is a search with one filter moved out of its arguments and into its name:
	// the same question with a name per filter, so the assistant has to work out which sibling to
	// call. The tell is a search verb carrying a dataset and then a filter, or the filter named as
	// a clause of its own.
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

// TestEveryToolDeclaresItsEntryPoints is the catalog's other half: a capability that does not say
// what it reaches is a capability whose cost nobody can read, and the description an assistant
// chooses from has to carry the same entry points the inventory names.
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

		// A tool that composes several entry points says so: an assistant that reads one call as
		// one request is an assistant that will be surprised by the cost of a composite.
		if len(capability.Endpoints) > 1 && !strings.Contains(tool.Description, "and") {
			t.Fatalf("%s composes %d entry points and its description does not say so", tool.Name, len(capability.Endpoints))
		}
	}
}
