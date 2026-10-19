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

// EndpointPath is the path the MCP surface is served from. It is a sibling of the administrator's API,
// not a child of it: the surface answers the same listener, the same TLS, and the same proxy rules the
// rest of the server answers on, and it is a route of its own.
//
// The composition root mounts it, so the name is the surface's own to give: the path is part of what
// the surface promises an assistant in its configuration lines, and not something the routes happen to
// pick.
const EndpointPath = "/mcp"

// toolDeadline is the bound on one capability's own work. The endpoint's bound is the assistant's
// whole exchange and is deliberately generous, because a composite capability runs several queries
// over a deployment with a full history. This is the narrower bound beside it: a capability that runs
// out of time stops its own work and answers its own failure, so the assistant hears "that question
// was too big" from the capability, in time to ask a narrower one, instead of sitting on a stream that
// stops in the middle of an answer.
const toolDeadline = 2 * time.Minute

// sessionKeepalive is how often the surface asks the sessions it is holding whether they are still
// there.
//
// A session's stream is open for the life of the session, and a stream that opens and then writes
// nothing is a connection the socket's write bound closes: the socket gives a connection thirty
// seconds from its last write, and an assistant with nothing to ask writes nothing. Asking is the
// protocol's own keepalive — a request the client answers — and the asking is what keeps the stream
// live. The interval sits well inside the socket's write bound, so the next ask is due long before
// the connection is given up.
const sessionKeepalive = 10 * time.Second

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

	// ToolDeadline is the bound one capability's own work runs under, and SessionKeepalive is how
	// often the surface asks its sessions whether they are still there. Left at zero, the surface
	// uses its own bounds; a test names shorter ones to watch both of them fire in the time a test
	// has.
	ToolDeadline     time.Duration
	SessionKeepalive time.Duration

	// The writes: one section of the configuration, one level of one system, the listener access
	// codes, and the credential an administrator signs in with. They are the same use cases the
	// administrator's own page saves with, so an assistant's change and an operator's change go
	// through one rule, and the surface adds no rule of its own to the save.
	Section    *application.UpdateConfigurationSectionHandler
	SubSection *application.UpdateSystemSubSectionHandler
	AddUser    *application.AddAccessUserHandler
	RemoveUser *application.RemoveAccessUserHandler
	Password   *application.ChangeAdminPasswordHandler
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

	// log is where the surface's own lines go: the caller it would not let in, the caller a
	// capability refused, the capability that changed the deployment.
	log *slog.Logger

	// writeTools names the capabilities that change the deployment, recorded as they are registered.
	// The surface files a line when one of them writes, and the list is built by the code that
	// registers them, so it cannot name a capability that is not one and cannot miss one that is.
	writeTools map[string]bool
}

// New builds the surface from the use cases it answers with, and registers its capabilities on the
// servers of the tiers that reach them.
func New(deps Deps) *Server {
	implementation := &mcp.Implementation{
		Name:    serverName,
		Version: deps.Version,
	}

	// The surface always has somewhere to file its lines. Left no logger, the lines go nowhere and
	// the surface answers exactly as it did before it had a log view of its own; the composition
	// root hands it the server's.
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

	// Every capability runs under a bound of its own, and the stream carrying its answer is kept
	// writing while the work runs. The endpoint's bound is the assistant's whole exchange; this is
	// the narrower one that lets a capability that runs out of time answer its own failure.
	srv.readonly.AddReceivingMiddleware(withToolDeadline(deps.toolDeadline(), deps.sessionKeepalive()))
	srv.full.AddReceivingMiddleware(withToolDeadline(deps.toolDeadline(), deps.sessionKeepalive()))

	// The surface files what the operator asks about after the fact, and it files from what the
	// capability answered. It is registered last, so it is the outermost middleware and sees the
	// answer the caller reads — including the refusal a capability's own bound produced.
	srv.readonly.AddReceivingMiddleware(withActivityLog(logger, srv.writeTools))
	srv.full.AddReceivingMiddleware(withActivityLog(logger, srv.writeTools))

	// The sessions the surface is holding are asked whether they are still there, at the interval
	// that keeps a stream writing inside the socket's write bound.
	if keepalive := deps.sessionKeepalive(); keepalive > 0 {
		srv.keepSessionsAlive(keepalive)
	}

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
	mux.Handle(EndpointPath, s.handler)
}

// Handler is the surface's HTTP handler, for a composition root that serves it outside the middleware
// the rest of the routes carry.
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

// sessionKeepalive is how often the surface asks its sessions whether they are still there: the
// surface's own, when the composition root named none.
func (deps Deps) sessionKeepalive() time.Duration {
	if deps.SessionKeepalive > 0 {
		return deps.SessionKeepalive
	}

	return sessionKeepalive
}

// methodCallTool is the one method a capability's work runs under a bound of its own. Every other
// method of the protocol answers in the round trip it is asked in, and is left exactly as the
// protocol answers it.
const methodCallTool = "tools/call"

// withToolDeadline bounds one capability's work, and keeps the stream carrying its answer writing
// while the work runs.
//
// The bound is the capability's own, narrower than the endpoint's, so the failure the assistant gets
// is the capability saying it ran out of time — a failure it can answer by asking a narrower question
// — and not a stream that stopped. The asking is what keeps the stream alive: a capability that works
// for minutes on a composite question is a stream that writes nothing for minutes, and the socket's
// write bound closes a connection that goes silent.
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

			// Only a capability whose own bound expired gets the surface's refusal, and only while
			// the caller is still listening. A capability that answered, a capability that answered a
			// failure of its own before its bound expired, and a caller that asked for a capability
			// this surface does not have all keep the answer they were given.
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

// ranOutOfItsBound reports whether the capability's own bound expired while the caller is still
// listening. A caller that hung up has nothing left to be answered.
func ranOutOfItsBound(working, request context.Context) bool {
	return working.Err() != nil && request.Err() == nil
}

// theWorkFailed reports whether the capability did not answer: a tool-level failure the caller read
// as a refusal, or a work failure that reached the protocol. A refusal the capability answered on its
// own is its answer and not the bound's, and the bound does not overwrite it.
func theWorkFailed(answered mcp.Result, err error) bool {
	if err != nil {
		return errors.Is(err, context.DeadlineExceeded)
	}

	result, ok := answered.(*mcp.CallToolResult)

	return ok && result != nil && result.IsError
}

// keepStreamAlive asks the session carrying a capability's answer whether it is still there, at the
// keepalive interval, for as long as the capability works. It answers with the function that stops
// the asking, which the caller defers: a capability that has answered has nothing left to keep alive.
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

// keepSessionsAlive asks every session the surface is holding whether it is still there, at the
// interval that keeps a stream writing inside the socket's write bound. A session the surface has
// closed is off the list it walks, so an assistant that has gone is not asked again.
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

// Sessions are the assistant sessions the surface is holding right now. The stopping server asks for
// them after it says it closed them: the stop's promise is that no session outlives it, and the only
// way to hold the stop to that promise is to look at the surface the stop was asked to close.
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
