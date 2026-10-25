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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerCapabilities builds the catalog an assistant is handed from what
// an administrator asks, not from REST routes: each capability lands on
// the servers of the tiers its reach names, writes last, behind a
// confirmation; see the capabilities-are-questions-not-routes maintainer note.
func (s *Server) registerCapabilities(deps Deps) {
	for _, build := range []func(*mcp.Server, Deps){
		registerVersionResource,
		registerCallTools,
		registerReadResources,
		registerFeedDiagnosis,
	} {
		build(s.readonly, deps)
		build(s.full, deps)
	}

	for _, build := range []func(*mcp.Server, Deps){
		registerAdminTools,
		registerHealthReview,
		registerConfigResources,
		registerStandingQuestions,
		func(target *mcp.Server, deps Deps) { registerWriteTools(target, deps, s.writeTools) },
	} {
		build(s.full, deps)
	}
}

// registerVersionResource publishes the release, the container answer, and
// whether the administrator credential still needs replacing.
func registerVersionResource(target *mcp.Server, deps Deps) {
	target.AddResource(&mcp.Resource{
		URI:   "rdio-scanner://version",
		Name:  "Version",
		Title: "The release this server reports",
		Description: "The version the server reports, whether it thinks it is running in a container, and " +
			"whether the administrator credential still needs to be replaced.",
		MIMEType: "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		contents, err := versionContents(ctx, deps)
		if err != nil {
			return nil, err
		}

		return &mcp.ReadResourceResult{Contents: contents}, nil
	})
}
