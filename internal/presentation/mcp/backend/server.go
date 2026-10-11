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
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

// endpointPath is the path the MCP surface is served from. It is a sibling of the administrator's API,
// not a child of it: the surface answers the same listener, the same TLS, and the same proxy rules the
// rest of the server answers on, and it is a route of its own.
const endpointPath = "/mcp"

// serverName is what the surface announces itself as. An assistant reads it, and an operator
// recognises it in the assistant's tool list beside the other things it was told to reach.
const serverName = "rdio-scanner"

// Deps is what the MCP surface is built from: the use cases its capabilities call, the credential checks
// it gates requests with, and the two facts about the deployment it reports.
//
// Every field is a use case handler or a port. The surface is handed no repository, no persistence type,
// and nothing it can reach into: a capability asks a use case to do a thing, and the use case decides.
type Deps struct {
	// Version is the release the server reports, and the version the surface announces as its own.
	Version string

	// Logger is where the surface's own lines go. Left nil, the SDK keeps its own logger; the
	// composition root hands this surface the server's, so the assistant's work lands in the log view
	// the operator already reads.
	Logger *slog.Logger

	// Session is the administrator's session check: the gate the administrator's own REST surface
	// uses, handed here so the surface recognises the same credential the administrator's page does.
	Session *application.CheckAdminSessionHandler

	// Resolve is what a listener access code entitles its holder to. It is the same use case the
	// call history endpoints gate with, and it carries the per-address guess budget with it, so
	// this surface spends the same budget the REST surface spends and not a second one beside it.
	Resolve *application.ResolveAccessQueryHandler

	// Trusted is the operator's trusted-proxy list, the same one the rest of the listener reads.
	// It decides which forwarding entry the surface may attribute an attempt to.
	Trusted *httpaddr.Trusted

	// Config reads the configuration the deployment is running.
	Config *application.GetConfigHandler

	// Logs reads the log view.
	Logs *application.SearchLogsHandler

	// Clock is the server's clock, for the windows the diagnostics ask for.
	Clock ports.Clock

	// Call reads one recorded call by its identifier, and Search answers the history search. They
	// are the same use cases the call history endpoints answer with, so a page an assistant gets
	// and a page a listener gets come from one rule.
	Call   *application.GetCallQueryHandler
	Search *application.SearchCallsQueryHandler

	// Docker is whether the server thinks it is running in a container. The composition root asks the
	// environment once, because the answer cannot change while the server is running, and hands the
	// answer here — the same way it hands the administrator's surface.
	Docker bool
}

// Server is the MCP surface: the two capability sets the two tiers read, and the HTTP handler that
// carries them.
//
// The surface answers one address, and the gate in front of it decides who is asking before the
// protocol speaks. The tier decides which set of capabilities the caller is handed: an assistant
// choosing from a catalog half of which refuses it is an assistant that has to learn its own reach by
// being refused, so the read-only set carries only what a listener's code opens.
//
// Each set is one server object shared by every session of its tier, which is what lets a capability
// tell a connected assistant that the capability list changed.
type Server struct {
	readonly *mcp.Server
	full     *mcp.Server
	handler  http.Handler

	session *application.CheckAdminSessionHandler
	resolve *application.ResolveAccessQueryHandler
	trusted *httpaddr.Trusted
}

// New builds the surface from the use cases it answers with, and registers its capabilities on the
// servers of the tiers that reach them.
func New(deps Deps) *Server {
	implementation := &mcp.Implementation{
		Name:    serverName,
		Version: deps.Version,
	}

	srv := &Server{
		readonly: mcp.NewServer(implementation, nil),
		full:     mcp.NewServer(implementation, nil),
		session:  deps.Session,
		resolve:  deps.Resolve,
		trusted:  deps.Trusted,
	}

	srv.registerCapabilities(deps)

	// The handler hands each request the server of the tier the gate decided for it. The gate runs
	// in front of this handler and leaves the caller in the request's context, so the tier is decided
	// by the credential and not by anything the protocol is asked for.
	//
	// The gate sits outside it. The protocol handler is the thing that speaks the protocol, and a
	// caller the server does not accept never reaches it: a refusal is answered before the surface
	// says anything about itself.
	sdk := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		if who, ok := CallerOf(r.Context()); ok && who.Tier == TierFull {
			return srv.full
		}

		return srv.readonly
	}, &mcp.StreamableHTTPOptions{Logger: deps.Logger})

	srv.handler = srv.Gate()(sdk)

	return srv
}

// Register hangs the surface on the mux the composition root is building its routes on.
//
// The composition root calls this only when the configuration names the surface, so a deployment that
// never asked for it never learns the path exists.
func (s *Server) Register(mux *http.ServeMux) {
	mux.Handle(endpointPath, s.handler)
}

// Handler is the surface's HTTP handler, for a composition root that serves it outside the middleware
// the rest of the routes carry.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// CloseSessions says goodbye to every assistant holding a session, the way the stopping server says
// goodbye to its other connections.
//
// The SDK closes a session's own transport when the session is closed, so the surface does not track
// connections of its own: it asks each server object which sessions it is holding and closes each one.
func (s *Server) CloseSessions(ctx context.Context) error {
	var first error

	for _, target := range []*mcp.Server{s.readonly, s.full} {
		for session := range target.Sessions() {
			if err := session.Close(); err != nil && first == nil {
				first = err
			}
		}
	}

	_ = ctx

	return first
}
