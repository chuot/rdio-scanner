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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerCapabilities is where the surface's catalog is built: the tools, the resources, and the
// prompts an assistant is handed.
//
// The catalog is derived from what an administrator asks, not from the REST routes. A capability that
// answers one question with several endpoints says so in its description, so an assistant can tell what
// a call is buying. A filter is an argument of the capability that searches, never a capability of its
// own.
//
// Each capability is registered on the servers of the tiers its reach names. The read-only capabilities
// are on both, because the administrator reads the history and the systems projection too, and reads
// the whole of them because the administrator's credential is not scoped by a listener access list. The
// administrator's capabilities are on the administrator's server alone: an access-code caller never
// sees them in a catalog, and never reaches them by name.
//
// Capabilities are added in the order the surface can prove them: the read-only capabilities an access
// code may use first, then the administrator's read capabilities, then the writes — each write behind a
// confirmation, and each destructive one behind the protocol's own confirmation round-trip. Nothing is
// ever reachable before the credential gate that decides who may call it.
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
	} {
		build(s.full, deps)
	}
}

// registerVersionResource publishes the fact an assistant needs before it asks for anything else: which
// release this deployment is, whether it thinks it is in a container, and whether the administrator
// credential still has to be replaced.
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
