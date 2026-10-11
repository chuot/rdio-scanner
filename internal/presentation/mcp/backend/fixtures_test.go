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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

var fixtureTime = time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)

const (
	testVersion = "8.0.0-test"

	// testAddress is the address a caller reaches the listener from. It is the documentation
	// address for tests, so a budget keyed on it cannot collide with a real host.
	testAddress = "192.0.2.1"

	// adminToken is an open administrator session. The gate recognises the same credential the
	// administrator's own page presents, so the fixture opens one the way the administrator's
	// tests do: by putting the token in the server's accepted window.
	adminToken = "capture-admin-token"

	// listenerCode is a listener access code, and listenerIdent is the identity it is configured
	// under. A restricted server is a server with a non-empty access list, so the restricted
	// fixture seeds exactly this one access.
	listenerCode  = "753159"
	listenerIdent = "dispatch-desk"

	// The two systems the fixture's history is spread over.
	systemOneRef = uint(10001)
	systemTwoRef = uint(10002)

	// accessAttemptsMax is the number of access-code attempts one address is allowed before the
	// server stops looking at what that address presents. It is the administrator login's bound,
	// restated here so the test that protects it names it.
	accessAttemptsMax = 3
)

// serverBundle is the MCP listener surface, built from the in-memory stand-ins the application
// tests use, reached through the real gate.
type serverBundle struct {
	server  *Server
	handler http.Handler

	accesses *fakes.Accesses
	logs     *fakes.Logs
	tokens   *fakes.Tokens
	clock    *fakes.Clock

	// resolve is the access-code resolver the surface gates with. Its guess budget lives in the
	// handler, so a test that wants one budget shared between two surfaces hands them one handler.
	resolve *application.ResolveAccessQueryHandler

	// The recorded history the two surfaces read, and the configuration the search answers from.
	calls   *fakes.Calls
	systems *fakes.Systems
	groups  *fakes.Groups
	tags    *fakes.Tags
}

func newServer(t *testing.T, restricted bool) *serverBundle {
	t.Helper()

	bundle := &serverBundle{
		accesses: &fakes.Accesses{},
		logs:     &fakes.Logs{},
		tokens:   fakes.NewTokens(),
		clock:    fakes.NewClock(fixtureTime),
	}

	bundle.tokens.Window = []string{adminToken}

	if restricted {
		bundle.accesses.Stored = domain.Accesses{
			{ID: 1, Code: listenerCode, Ident: listenerIdent, Scope: domain.AccessScope{Present: true, Wildcard: true}},
		}
	}

	bundle.resolve = application.NewResolveAccessQueryHandler(bundle.accesses, bundle.logs, bundle.clock)

	// The recorded history the capabilities answer from, spread over two systems so a scoped
	// caller and an unscoped caller can be told apart by what they get back, and over two days so
	// a slice narrowed to one day can be told apart from a slice that was not narrowed at all.
	bundle.calls = fakes.NewCalls()
	bundle.calls.Stored = map[uint64]domain.Call{
		1: fixtureCall(1, systemOneRef, fixtureTime.Add(-3*time.Hour)),
		2: fixtureCall(2, systemOneRef, fixtureTime.Add(-2*time.Hour)),
		3: fixtureCall(3, systemTwoRef, fixtureTime.Add(-1*time.Hour)),
		4: fixtureCall(4, systemTwoRef, fixtureTime.Add(-27*time.Hour)),
	}
	bundle.systems = &fakes.Systems{Stored: domain.Systems{
		{ID: 1, Label: "System", SystemRef: systemOneRef},
		{ID: 2, Label: "System", SystemRef: systemTwoRef},
	}}
	bundle.groups = &fakes.Groups{Stored: domain.Groups{{ID: 1, Label: "Fire"}}}
	bundle.tags = &fakes.Tags{Stored: domain.Tags{{ID: 3, Label: "Fire Dispatch"}}}

	bundle.server = New(bundle.Deps())
	bundle.handler = bundle.server.Handler()

	return bundle
}

// fixtureCall is one recorded call on one system.
func fixtureCall(id uint64, systemRef uint, at time.Time) domain.Call {
	call := domain.NewCall()

	call.ID = id
	call.Audio = []byte("audio-bytes")
	call.AudioFilename = "call.wav"
	call.AudioLength = time.Second
	call.AudioMime = "audio/wav"
	call.System = domain.System{ID: uint64(systemRef), Label: "System", SystemRef: systemRef}
	call.Talkgroup = domain.Talkgroup{ID: 1, Label: "Talkgroup", TalkgroupRef: 1514000}
	call.Timestamp = at

	return call
}

// Deps is the wiring the composition root does for this surface, with the use cases built over the
// fixture's in-memory stores.
func (bundle *serverBundle) Deps() Deps {
	return Deps{
		Version: testVersion,
		Call:    application.NewGetCallQueryHandler(bundle.calls),
		Search:  application.NewSearchCallsQueryHandler(bundle.calls, bundle.systems, bundle.groups, bundle.tags),
		Config: application.NewGetConfigHandler(application.ConfigurationRepositories{
			Accesses:    bundle.accesses,
			Apikeys:     &fakes.Apikeys{},
			Dirwatches:  &fakes.Dirwatches{},
			Downstreams: &fakes.Downstreams{},
			Groups:      bundle.groups,
			Options:     &fakes.Options{},
			Systems:     bundle.systems,
			Tags:        bundle.tags,
		}),
		Logs:    application.NewSearchLogsHandler(bundle.logs, bundle.clock),
		Resolve: bundle.resolve,
		Session: application.NewCheckAdminSessionHandler(bundle.tokens),
		Clock:   bundle.clock,
	}
}

// rebuild hangs the surface back up with a proxy configuration, the way the composition root hands
// the listener the operator's trusted-proxy list. The stores are the same ones, so the attempts a
// caller has already spent are still spent.
func (bundle *serverBundle) rebuild(t *testing.T, trusted *httpaddr.Trusted) {
	t.Helper()

	deps := bundle.Deps()
	deps.Trusted = trusted

	bundle.server = New(deps)
	bundle.handler = bundle.server.Handler()
}

// caller runs the gate around a capture handler and returns the context the gate handed inward.
// A refused caller hands back nothing at all.
func (bundle *serverBundle) caller(req *http.Request) context.Context {
	var ctx context.Context

	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() })
	bundle.server.Gate()(inner).ServeHTTP(httptest.NewRecorder(), req)

	return ctx
}

// logText is everything the surface filed in the server's log view, oldest first.
func (bundle *serverBundle) logText() string {
	var out string
	for _, entry := range bundle.logs.Recorded() {
		out += entry.Message + "\n"
	}
	return out
}

// wildcardScope is the scope that grants every system and talkgroup: a listener that names no
// systems of its own.
func wildcardScope() domain.AccessScope {
	return domain.AccessScope{Present: true, Wildcard: true}
}

// scopedTo is the scope naming systems with every talkgroup on them.
func scopedTo(systemRefs ...uint) domain.AccessScope {
	scope := domain.AccessScope{Present: true}

	for _, systemRef := range systemRefs {
		scope.Systems = append(scope.Systems, domain.ScopedSystem{
			SystemRef:          systemRef,
			TalkgroupsGiven:    true,
			TalkgroupsWildcard: true,
		})
	}

	return scope
}
