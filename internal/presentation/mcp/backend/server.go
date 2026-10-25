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
	"errors"
	"iter"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

// EndpointPath is the path the surface is served from: a route of its
// own, a sibling of the administrator's API, promised to assistants in
// the surface's configuration lines; see the mcp-endpoint-is-its-own-
// path-on-the-server maintainer note.
const EndpointPath = "/mcp"

// toolDeadline bounds one capability's own work, narrower than the
// endpoint's bound, so the assistant hears the capability's own failure
// in time to ask a narrower one; see the mcp-endpoint-is-its-own-path-on-
// the-server maintainer note.
const toolDeadline = 2 * time.Minute

// sessionKeepalive is how often the surface asks held sessions whether
// they are still there. The socket gives a connection thirty seconds
// from its last write, and an assistant with nothing to ask writes
// nothing: the ask keeps the stream live, well inside that bound.
const sessionKeepalive = 10 * time.Second

// serverName is what the surface announces itself as: an operator
// recognises it in the assistant's tool list.
const serverName = "rdio-scanner"

// Deps is what the surface is built from: use case handlers, ports, and
// two deployment facts. The surface is handed no repository and nothing
// it can reach into: a capability asks a use case to do a thing, and the
// use case decides.
type Deps struct {
	// Version is the release the server reports, and the version the surface announces as its own.
	Version string

	// Logger is where the surface's own lines go. Left nil, the SDK keeps its
	// own logger; the composition root hands this surface the server's.
	Logger *slog.Logger

	// Session is the administrator's session check: the gate the administrator's own REST
	// surface uses, so this surface recognises the same credential the page does.
	Session *application.CheckAdminSessionHandler

	// Resolve is what a listener access code entitles its holder to. It is the same use case
	// the call history endpoints gate with, carrying the per-address guess budget, so this
	// surface spends that budget and not a second one.
	Resolve *application.ResolveAccessQueryHandler

	// Trusted is the operator's trusted-proxy list, the same one the rest of the listener
	// reads. It decides which forwarding entry the surface may attribute an attempt to.
	Trusted *httpaddr.Trusted

	// Config reads the configuration the deployment is running.
	Config *application.GetConfigHandler

	// Logs reads the log view.
	Logs *application.SearchLogsHandler

	// Clock is the server's clock, for the windows the diagnostics ask for.
	Clock ports.Clock

	// Call reads one recorded call by its identifier, and Search answers the
	// history search. They are the use cases the call history endpoints answer
	// with, so an assistant and a listener get the same page.
	Call   *application.GetCallQueryHandler
	Search *application.SearchCallsQueryHandler

	// Docker is whether the server thinks it is running in a container. The
	// answer cannot change while the server runs, so the composition root
	// asks once.
	Docker bool

	// ToolDeadline is the bound one capability's own work runs under, and
	// SessionKeepalive is how often the surface asks its sessions whether they
	// are still there. Left at zero, the surface uses its own bounds.
	ToolDeadline     time.Duration
	SessionKeepalive time.Duration

	// The writes: one section of the configuration, one level of one system,
	// the listener access codes, and the administrator credential. They are
	// the use cases the administrator's own page saves with, and the surface
	// adds no rule of its own to the save.
	Section    *application.UpdateConfigurationSectionHandler
	SubSection *application.UpdateSystemSubSectionHandler
	AddUser    *application.AddAccessUserHandler
	RemoveUser *application.RemoveAccessUserHandler
	Password   *application.ChangeAdminPasswordHandler
}

// Server is the surface: the capability sets the tiers read and the HTTP
// handler that carries them. Each set is one server object shared by every
// session of its tier, so a capability can tell a connected assistant the
// list changed; see the credential-gate-decides-the-tier-before-the-protocol maintainer note.
type Server struct {
	readonly *mcp.Server
	full     *mcp.Server
	handler  http.Handler

	session *application.CheckAdminSessionHandler
	resolve *application.ResolveAccessQueryHandler
	trusted *httpaddr.Trusted

	// log is where the surface's own lines go: the caller it would not let in, the caller a
	// capability refused, the capability that changed the deployment.
	log *slog.Logger

	// writeTools names the capabilities that change the deployment, recorded
	// as they are registered, so it cannot name one that is not a write and
	// cannot miss one that is.
	writeTools map[string]bool
}

// New builds the surface from the use cases it answers with, and registers
// its capabilities on the servers of the tiers that reach them.
func New(deps Deps) *Server {
	implementation := &mcp.Implementation{
		Name:    serverName,
		Version: deps.Version,
	}

	// The surface always has somewhere to file its lines. Left no logger, the
	// lines go nowhere and the surface answers as it did before.
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	srv := &Server{
		readonly:   mcp.NewServer(implementation, &mcp.ServerOptions{Logger: logger}),
		full:       mcp.NewServer(implementation, &mcp.ServerOptions{Logger: logger}),
		session:    deps.Session,
		resolve:    deps.Resolve,
		trusted:    deps.Trusted,
		log:        logger,
		writeTools: map[string]bool{},
	}

	srv.registerCapabilities(deps)

	// Every capability runs under a bound of its own, and the stream carrying its answer is
	// kept writing while the work runs. This is the narrower bound that lets a capability
	// that runs out of time answer its own failure.
	srv.readonly.AddReceivingMiddleware(withToolDeadline(deps.toolDeadline(), deps.sessionKeepalive()))
	srv.full.AddReceivingMiddleware(withToolDeadline(deps.toolDeadline(), deps.sessionKeepalive()))

	// The surface files what the operator asks about after the fact, from
	// what the capability answered. It is registered last, so it is the
	// outermost middleware and sees the answer the caller reads.
	srv.readonly.AddReceivingMiddleware(withActivityLog(logger, srv.writeTools))
	srv.full.AddReceivingMiddleware(withActivityLog(logger, srv.writeTools))

	// The sessions the surface is holding are asked whether they are still there, at the
	// interval that keeps a stream writing inside the socket's write bound.
	if keepalive := deps.sessionKeepalive(); keepalive > 0 {
		srv.keepSessionsAlive(keepalive)
	}

	// The handler hands each request the server of the tier the gate decided,
	// from the credential and not from anything the protocol is asked for; a
	// refused caller never reaches the protocol handler. See the credential-
	// gate-decides-the-tier-before-the-protocol maintainer note.
	sdk := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		if who, ok := CallerOf(r.Context()); ok && who.Tier == TierFull {
			return srv.full
		}

		return srv.readonly
	}, &mcp.StreamableHTTPOptions{Logger: deps.Logger})

	srv.handler = srv.Gate()(sdk)

	return srv
}

// Register hangs the surface on the mux the composition root is building its
// routes on. The root calls this only when the configuration names the
// surface.
func (s *Server) Register(mux *http.ServeMux) {
	mux.Handle(EndpointPath, s.handler)
}

// Handler is the surface's HTTP handler, for a composition root that serves
// it outside the middleware the rest of the routes carry.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// toolDeadline is the bound one capability's work runs under: the surface's own, when the composition
// root named none.
func (deps Deps) toolDeadline() time.Duration {
	if deps.ToolDeadline > 0 {
		return deps.ToolDeadline
	}

	return toolDeadline
}

// sessionKeepalive is how often the surface asks its sessions whether they
// are still there: the surface's own, when the composition root named none.
func (deps Deps) sessionKeepalive() time.Duration {
	if deps.SessionKeepalive > 0 {
		return deps.SessionKeepalive
	}

	return sessionKeepalive
}

// methodCallTool is the one method a capability's work runs under a bound of
// its own. Every other method answers in the round trip it is asked in.
const methodCallTool = "tools/call"

// withToolDeadline bounds one capability's work and keeps the stream
// writing while it runs, so the failure is the capability's own, not a
// silent stream; see the mcp-endpoint-is-its-own-path-on-the-server
// maintainer note.
func withToolDeadline(at, keepalive time.Duration) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != methodCallTool {
				return next(ctx, method, req)
			}

			tool, session := "the capability", (*mcp.ServerSession)(nil)

			if call, ok := req.(*mcp.CallToolRequest); ok {
				session = call.Session

				if call.Params != nil && call.Params.Name != "" {
					tool = call.Params.Name
				}
			}

			working, cancel := context.WithTimeout(ctx, at)
			defer cancel()

			defer keepStreamAlive(session, keepalive)()

			answered, err := next(working, method, req)

			// Only a capability whose own bound expired gets the surface's refusal,
			// and only while the caller is still listening. An answer already given
			// keeps the shape it was given.
			if !ranOutOfItsBound(working, ctx) || !theWorkFailed(answered, err) {
				return answered, err
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{
					Text: tool + " ran out of time on this server, and the server stopped its work. " +
						"Ask a narrower question: a shorter window, one subsystem, a smaller page.",
				}},
				IsError: true,
			}, nil
		}
	}
}

// ranOutOfItsBound reports whether the capability's own bound expired while
// the caller is still listening.
func ranOutOfItsBound(working, request context.Context) bool {
	return working.Err() != nil && request.Err() == nil
}

// theWorkFailed reports whether the capability did not answer: a tool-level failure the
// caller read as a refusal, or a work failure that reached the protocol. A refusal the
// capability answered on its own is its answer and not the bound's.
func theWorkFailed(answered mcp.Result, err error) bool {
	if err != nil {
		return errors.Is(err, context.DeadlineExceeded)
	}

	result, ok := answered.(*mcp.CallToolResult)

	return ok && result != nil && result.IsError
}

// keepStreamAlive asks the session carrying a capability's answer whether it
// is still there, for as long as the capability works. It answers with the
// function that stops the asking, which the caller defers.
func keepStreamAlive(session *mcp.ServerSession, interval time.Duration) func() {
	if session == nil || interval <= 0 {
		return func() {}
	}

	asked := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-asked:
				return

			case <-ticker.C:
				// The asking is the keepalive: the write is what the socket's write bound is
				// measured against. A client that does not answer is a client whose stream is
				// not being kept alive by this surface, and saying so is all there is to say.
				ctx, cancel := context.WithTimeout(context.Background(), interval)

				_ = session.Ping(ctx, nil)

				cancel()
			}
		}
	}()

	return func() { close(asked) }
}

// keepSessionsAlive asks every session the surface is holding whether it is
// still there, at the interval that keeps a stream writing inside the
// socket's write bound. A closed session is off the list it walks.
func (s *Server) keepSessionsAlive(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			for _, target := range []*mcp.Server{s.readonly, s.full} {
				for session := range target.Sessions() {
					ctx, cancel := context.WithTimeout(context.Background(), interval)

					_ = session.Ping(ctx, nil)

					cancel()
				}
			}
		}
	}()
}

// Sessions are the assistant sessions the surface is holding right now. The
// stopping server asks for them after it says it closed them: the stop's
// promise is that no session outlives it.
func (s *Server) Sessions() iter.Seq[*mcp.ServerSession] {
	return func(yield func(*mcp.ServerSession) bool) {
		for _, target := range []*mcp.Server{s.readonly, s.full} {
			for session := range target.Sessions() {
				if !yield(session) {
					return
				}
			}
		}
	}
}

// CloseSessions says goodbye to every held session, the way the stopping
// server does to its other connections: the SDK closes a session's own
// transport, so the surface tracks none of its own; see the credential-
// gate-decides-the-tier-before-the-protocol maintainer note.
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
