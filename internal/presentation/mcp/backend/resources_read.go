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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The read-only tier's resources. A resource is read directly, with no tool
// call and no arguments, so the caller's reach is decided inside the read:
// the same scope the gate decided for the tools.

const (
	// systemsURI is the caller's own view of the systems, talkgroups, sites, and units.
	systemsURI = "rdio-scanner://systems"

	// callURITemplate is a browsable slice of the recorded history: one
	// talkgroup on one system, optionally narrowed to one day. The history is
	// large and paged, which is why it is a template.
	callURITemplate = "rdio-scanner://calls/{systemRef}/{talkgroupRef}{?date}"
)

// registerReadResources adds the two resources the read-only tier reaches. The administrator
// reads the same two, unscoped, and gains the configuration resources beside them.
func registerReadResources(target *mcp.Server, deps Deps) {
	target.AddResource(&mcp.Resource{
		URI:      systemsURI,
		Name:     "systems",
		Title:    "The systems, talkgroups, sites, and units this caller can name",
		MIMEType: "application/json",
		Description: "The references every other capability names its filters with, projected through " +
			"what this caller is allowed to see: an access code that names two systems sees those two " +
			"and nothing else, an administrator sees the whole configuration. Read this before " +
			"searching: a reference an assistant invented answers as an empty page.",
	}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, errNoCaller
		}

		configuration, err := deps.Config.Handle(ctx, application.GetConfigQuery{})
		if err != nil {
			return nil, err
		}

		// The projection is the one the listener's own configuration payload
		// uses, so an assistant and a listener at a desk name the same set.
		view := configuration.Systems.Scoped(scopeOf(who), configuration.Tags)

		return jsonResource(systemsURI, systemsAnswer(view))
	})

	target.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: callURITemplate,
		Name:        "call-history",
		Title:       "A slice of the recorded call history",
		MIMEType:    "application/json",
		Description: "One talkgroup on one system, narrowed to one day when the date argument is " +
			"given, newest first. Fill the arguments from the systems projection. A slice naming a " +
			"system outside what this caller may see answers the empty slice, the same answer a " +
			"talkgroup with nothing recorded gets. Use the search tool when the slice is not enough: " +
			"it takes every filter the history accepts.",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, errNoCaller
		}

		criteria, readable := parseHistorySlice(req.Params.URI)
		if !readable {
			// A window that names nothing real looks onto nothing: the empty
			// slice, the same answer a talkgroup with nothing recorded gets.
			return jsonResource(req.Params.URI, pageAnswer(ports.CallSearchPage{}, defaultHistorySlice()))
		}

		page, err := deps.Search.Handle(ctx, application.SearchCallsQuery{
			Criteria: criteria,
			Scope:    scopeOf(who),
		})
		if err != nil {
			return nil, err
		}

		return jsonResource(req.Params.URI, pageAnswer(page, criteria))
	})
}

// parseHistorySlice reads the system, the talkgroup, and the optional day out of a slice URI.
// The second answer says whether the URI named two real numbers at all.
func parseHistorySlice(uri string) (domain.CallSearch, bool) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return domain.CallSearch{}, false
	}

	fields := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(fields) != 2 {
		return domain.CallSearch{}, false
	}

	systemRef, err := strconv.ParseUint(fields[0], 10, 32)
	if err != nil {
		return domain.CallSearch{}, false
	}

	talkgroupRef, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil {
		return domain.CallSearch{}, false
	}

	return domain.ParseCallSearch(map[string]any{
		"system":    float64(systemRef),
		"talkgroup": float64(talkgroupRef),
		"date":      dayOf(parsed.Query().Get("date")),
		"sort":      float64(1),
	}), true
}

// defaultHistorySlice is the page an unreadable slice echoes: the page the
// server would have applied to a window naming real numbers.
func defaultHistorySlice() domain.CallSearch {
	return domain.CallSearch{Limit: domain.DefaultCallSearchLimit, Sort: 1}
}

// dayOf is the day argument as the history reads it: the history's
// criterion is an instant inside the day, so a bare day becomes the instant
// that opens it.
func dayOf(argument string) string {
	if argument == "" {
		return ""
	}

	if at, err := time.Parse(time.RFC3339, argument); err == nil {
		return at.Format(time.RFC3339)
	}

	day, err := time.Parse("2006-01-02", argument)
	if err != nil {
		return ""
	}

	return day.Format(time.RFC3339)
}

// jsonResource is a resource read as one JSON text, which is what every
// resource on this surface answers.
func jsonResource(uri string, payload any) (*mcp.ReadResourceResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI:      uri,
		MIMEType: "application/json",
		Text:     string(body),
	}}}, nil
}
