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
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The assistant's endpoint carries a bound of its own: composites run several
// queries, and a session's stream stays open for the session's life. Two pins
// follow: a capability runs inside its own bound, so the assistant gets an
// answer naming the work that ran out of time, and held streams keep writing.

// newSurfaceWithDeps is the surface with its streaming dependencies named:
// the bound one capability's work gets, and the session keepalive interval.
func newSurfaceWithDeps(t *testing.T, tweak func(*serverBundle, Deps) Deps) *serverBundle {
	t.Helper()

	bundle := newServer(t, true)

	deps := tweak(bundle, bundle.Deps())

	bundle.server = New(deps)
	bundle.handler = bundle.server.Handler()

	return bundle
}

// TestAToolWhoseWorkOutlivesItsOwnDeadlineAnswersAFailureTheAssistantCanRead:
// the endpoint's bound is generous for legitimate composites. A capability
// outliving its own bound is the deployment's problem, and the assistant must
// hear it from the capability in time to ask a narrower question.
func TestAToolWhoseWorkOutlivesItsOwnDeadlineAnswersAFailureTheAssistantCanRead(t *testing.T) {
	const (
		itsOwnDeadline = 50 * time.Millisecond
		theWorkTakes   = 200 * time.Millisecond
	)

	bundle := newSurfaceWithDeps(t, func(b *serverBundle, deps Deps) Deps {
		deps.ToolDeadline = itsOwnDeadline
		deps.Logs = application.NewSearchLogsHandler(slowLogs{Logs: b.logs, after: theWorkTakes}, b.clock)

		return deps
	})

	s := newSessionOn(t, bundle, bundle.server.full, Caller{Tier: TierFull})

	started := time.Now()

	refused := refusedCall(t, s, "search_logs", map[string]any{"level": "error"})

	elapsed := time.Since(started)

	if elapsed >= theWorkTakes {
		t.Fatalf("the capability waited %v for the log view, want it stopped at its own bound of %v: a "+
			"capability that runs out of time stops its work and says so, it does not wait for a "+
			"repository that is not answering", elapsed, itsOwnDeadline)
	}

	if !strings.Contains(refused, "search_logs") {
		t.Fatalf("the refusal says %q, and does not name the capability the assistant asked to run", refused)
	}

	if !strings.Contains(refused, "time") {
		t.Fatalf("the refusal says %q, and does not say the capability ran out of time: the assistant "+
			"needs to know the question was too big, not that the server is broken", refused)
	}
}

// TestAToolWhoseWorkFitsItsOwnDeadlineIsNotRefusedByIt: a capability that
// answers inside its own bound answers as it always did, with no notice of the
// bound at all.
func TestAToolWhoseWorkFitsItsOwnDeadlineIsNotRefusedByIt(t *testing.T) {
	bundle := newSurfaceWithDeps(t, func(b *serverBundle, deps Deps) Deps {
		deps.ToolDeadline = 5 * time.Second

		return deps
	})

	s := newSessionOn(t, bundle, bundle.server.readonly, readScoped())

	answered := call(t, s, "list_systems", map[string]any{})

	var view struct {
		Systems []struct {
			SystemRef uint `json:"systemRef"`
		} `json:"systems"`
	}
	decode(t, answered, &view)

	if len(view.Systems) == 0 {
		t.Fatalf("the systems projection answered %+v, want the fixture's systems", view)
	}
}

// TestTheSurfaceKeepsItsSessionsStreamsAlive: a session's stream is open for the
// session's life and is kept live by the protocol's own ping, which asks the
// client to answer and writes what keeps the stream live; a connection writing
// nothing past the socket's write bound is closed.
func TestTheSurfaceKeepsItsSessionsStreamsAlive(t *testing.T) {
	const interval = 40 * time.Millisecond

	bundle := newSurfaceWithDeps(t, func(b *serverBundle, deps Deps) Deps {
		deps.SessionKeepalive = interval

		return deps
	})

	s, streams := newRecordingSession(t, bundle, readScoped())

	// Two pings, so the test is watching a rhythm and not one message that happened to be sent.
	for range 2 {
		select {
		case said := <-streams.requests:
			if said.method != "ping" {
				t.Fatalf("the surface kept the stream alive with %q, want a ping: the ping is the "+
					"protocol's own keepalive and asks the client for nothing else", said.method)
			}

			if !said.wantsAnAnswer {
				t.Fatalf("the surface's keepalive is a message the client is not asked to answer: a " +
					"ping is a request, and the answer is what tells the server the client is there")
			}

		case <-time.After(5 * time.Second):
			t.Fatal("the surface wrote nothing to its session's stream: a session whose stream falls " +
				"silent is closed by the socket's write bound, and the assistant loses it")
		}
	}

	// A session the server has closed is not kept alive: the surface keeps the
	// sessions it is holding, and a stream nobody holds gets nothing.
	if err := bundle.server.CloseSessions(context.Background()); err != nil {
		t.Fatalf("closing the surface's sessions: %v", err)
	}

	_ = s.Close()

	closed := time.Now()

	select {
	case said := <-streams.requests:
		if said.at.After(closed.Add(interval)) {
			t.Fatalf("the surface pinged a session it had closed: %q at %v, after the session ended at %v",
				said.method, said.at.Format(time.RFC3339Nano), closed.Format(time.RFC3339Nano))
		}

	case <-time.After(20 * interval):
	}
}

// TestTheKeepaliveIntervalKeepsAStreamWritingInsideTheSocketWriteBound:
// the interval is chosen against the socket's thirty second write bound,
// at or beyond which the stream closes before the next ping is due.
func TestTheKeepaliveIntervalKeepsAStreamWritingInsideTheSocketWriteBound(t *testing.T) {
	if sessionKeepalive > 10*time.Second {
		t.Fatalf("the surface pings its sessions every %v: the socket's write bound is 30 seconds, and a "+
			"stream that writes nothing for that long is closed. The interval has to sit well inside it.",
			sessionKeepalive)
	}
}

// TestTheToolDeadlineIsWellInsideTheEndpointBound: the endpoint bound is the
// assistant's whole exchange, generous for composites, and the capability's own
// bound has to arrive early enough that the failure reads as the capability's
// and not as a stream that stopped.
func TestTheToolDeadlineIsWellInsideTheEndpointBound(t *testing.T) {
	if toolDeadline < 30*time.Second {
		t.Fatalf("a capability runs under %v: a composite capability runs several queries over a full "+
			"deployment, and a bound under half a minute refuses work this surface exists to do", toolDeadline)
	}

	if toolDeadline > 5*time.Minute {
		t.Fatalf("a capability runs under %v: a capability that runs out of time is the assistant's "+
			"problem to hear about, and five minutes is a long time to sit on a question that is not "+
			"going to answer", toolDeadline)
	}
}

// slowLogs is a log repository that does not answer for a while and notices the
// cancellation the bound brings: the shape a capability meets on a busy server,
// and the reason a capability carries a bound of its own.
type slowLogs struct {
	*fakes.Logs

	after time.Duration
}

func (logs slowLogs) SearchLogs(ctx context.Context, search domain.LogSearch) (ports.LogSearchPage, error) {
	select {
	case <-time.After(logs.after):

	case <-ctx.Done():
		return ports.LogSearchPage{}, ctx.Err()
	}

	return logs.Logs.SearchLogs(ctx, search)
}

// recordingStream is the client's side of a session, keeping what the surface
// writes: the keepalive is proven by what arrives here, and nothing else in the
// surface writes here.
type recordingStream struct {
	requests chan recordedRequest

	mu     sync.Mutex
	closed bool
}

// recordedRequest is one message the surface sent to the session: the method it asked for, whether
// it asked the client to answer, and when it arrived.
type recordedRequest struct {
	method        string
	wantsAnAnswer bool
	at            time.Time
}

func newRecordingSession(t *testing.T, bundle *serverBundle, who Caller) (*session, *recordingStream) {
	t.Helper()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	streams := &recordingStream{requests: make(chan recordedRequest, 64)}

	serverSession, err := bundle.server.readonly.Connect(
		context.WithValue(context.Background(), callerKey{}, who), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the surface to an in-memory session: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "rdio-scanner-test-harness", Version: "1"}, nil)

	clientSession, err := client.Connect(context.Background(),
		&recordingTransport{Transport: clientTransport, streams: streams}, nil)
	if err != nil {
		t.Fatalf("connect the harness to the surface: %v", err)
	}

	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})

	return &session{ClientSession: clientSession, bundle: bundle}, streams
}

// recordingTransport is a transport that reports every request the client receives to the stream it
// is recording.
type recordingTransport struct {
	mcp.Transport

	streams *recordingStream
}

func (transport *recordingTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := transport.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}

	return &recordingConnection{Connection: conn, streams: transport.streams}, nil
}

// recordingConnection is the connection the client reads its session's stream off.
type recordingConnection struct {
	mcp.Connection

	streams *recordingStream
}

func (conn *recordingConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := conn.Connection.Read(ctx)
	if err != nil {
		return msg, err
	}

	if req, ok := msg.(*jsonrpc.Request); ok {
		select {
		case conn.streams.requests <- recordedRequest{
			method:        req.Method,
			wantsAnAnswer: req.ID.IsValid(),
			at:            time.Now(),
		}:

		default:
		}
	}

	return msg, err
}
