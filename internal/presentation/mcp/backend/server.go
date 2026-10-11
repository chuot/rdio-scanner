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

// Server is the MCP surface: one server object, one set of capabilities, and the HTTP handler that
// carries them.
//
// The server object is shared by every session the handler takes on, which is what lets a capability
// tell a connected assistant that the capability list changed.
type Server struct {
	mcp     *mcp.Server
	handler http.Handler

	session *application.CheckAdminSessionHandler
	resolve *application.ResolveAccessQueryHandler
	trusted *httpaddr.Trusted
}

// New builds the surface from the use cases it answers with, and registers its capabilities on it.
func New(deps Deps) *Server {
	srv := &Server{
		mcp: mcp.NewServer(&mcp.Implementation{
			Name:    serverName,
			Version: deps.Version,
		}, nil),
		session: deps.Session,
		resolve: deps.Resolve,
		trusted: deps.Trusted,
	}

	srv.registerCapabilities(deps)

	// The handler hands every request the same server object. The surface is one set of
	// capabilities for the whole deployment, not a server per connection.
	//
	// The gate sits outside it. The protocol handler is the thing that speaks the protocol, and a
	// caller the server does not accept never reaches it: a refusal is answered before the surface
	// says anything about itself.
	sdk := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv.mcp },
		&mcp.StreamableHTTPOptions{Logger: deps.Logger})

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
// connections of its own: it asks the server object which sessions it is holding and closes each one.
func (s *Server) CloseSessions(ctx context.Context) error {
	var first error

	for session := range s.mcp.Sessions() {
		if err := session.Close(); err != nil && first == nil {
			first = err
		}
	}

	_ = ctx

	return first
}
