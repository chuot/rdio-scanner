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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

var fixtureTime = time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)

const (
	// adminToken is an open administrator session. The call history endpoints accept the same
	// credential the administrator's own page presents, so the fixture opens one the same way the
	// administrator's tests do: by putting the token in the server's accepted window.
	adminToken = "capture-admin-token"

	// listenerCode is a listener access code, and listenerIdent is the identity it is configured
	// under. The restricted-server tests seed one access, and the scoped tests name the systems it
	// grants.
	listenerCode = "753159"
	listenerIdent = "dispatch-desk"
)

// The two systems the fixture's history is spread over, named by the references clients use.
const (
	systemOneRef   = uint(10001)
	systemTwoRef   = uint(10002)
	talkgroupOneRef = uint(1514000)
	talkgroupTwoRef = uint(1515000)
)

// callsFixture is a server whose call history, configuration, access list, sessions, and log are the
// in-memory stand-ins the application tests use, reached through the real endpoints.
type callsFixture struct {
	calls    *fakes.Calls
	systems  *fakes.Systems
	groups   *fakes.Groups
	tags     *fakes.Tags
	accesses *fakes.Accesses
	logs     *fakes.Logs
	tokens   *fakes.Tokens
	clock    *fakes.Clock

	routes http.Handler
}

func newCallsFixture(t *testing.T) *callsFixture {
	t.Helper()

	fixture := &callsFixture{
		calls:    fakes.NewCalls(),
		systems:  &fakes.Systems{Stored: domain.Systems{fixtureSystem(systemOneRef), fixtureSystem(systemTwoRef)}},
		groups:   &fakes.Groups{Stored: domain.Groups{{ID: 1, Label: "Fire"}}},
		tags:     &fakes.Tags{Stored: domain.Tags{{ID: 3, Label: "Fire Dispatch"}}},
		accesses: &fakes.Accesses{},
		logs:     &fakes.Logs{},
		tokens:   fakes.NewTokens(),
		clock:    fakes.NewClock(fixtureTime),
	}

	// The administrator is already logged in: the call history endpoints accept an open
	// administrator session, and minting one is the login endpoint's own proof, not this surface's.
	fixture.tokens.Window = []string{adminToken}

	// The history: three calls, two of them on the first system and one on the second, so a
	// scoped caller and an unscoped caller can be told apart by what they get back.
	fixture.calls.Stored = map[uint64]domain.Call{
		1: fixtureCall(1, systemOneRef, talkgroupOneRef, fixtureTime.Add(-3*time.Hour), "fire-1.mp3", "audio/mpeg", 4200*time.Millisecond),
		2: fixtureCall(2, systemOneRef, talkgroupOneRef, fixtureTime.Add(-2*time.Hour), "fire-2.mp3", "audio/mpeg", 1800*time.Millisecond),
		3: fixtureCall(3, systemTwoRef, talkgroupTwoRef, fixtureTime.Add(-1*time.Hour), "marine-3.wav", "audio/wav", 900*time.Millisecond),
	}

	fixture.routes = fixture.buildRoutes()

	return fixture
}

// buildRoutes is the routing the composition root installs for this surface.
func (fixture *callsFixture) buildRoutes() http.Handler {
	mux := http.NewServeMux()

	NewCalls(CallsDeps{
		Call:     application.NewGetCallQueryHandler(fixture.calls),
		Clock:    fixture.clock,
		Logs:     fixture.logs,
		Resolve:  application.NewResolveAccessQueryHandler(fixture.accesses, fixture.logs, fixture.clock),
		Search:   application.NewSearchCallsQueryHandler(fixture.calls, fixture.systems, fixture.groups, fixture.tags),
		Session:  application.NewCheckAdminSessionHandler(fixture.tokens),
		Trusted:  trustedLocalhostProxy(),
	}).Register(mux)

	return mux
}

// seedAccessList makes the server restricted: a non-empty access list is what makes a server demand
// an access code, the same rule the listener socket applies.
func (fixture *callsFixture) seedAccessList(t *testing.T, scopes ...domain.AccessScope) {
	t.Helper()

	next := domain.Accesses{}

	for i, scope := range scopes {
		next = append(next, domain.Access{
			ID:    uint64(i + 1),
			Code:  listenerCode,
			Ident: listenerIdent,
			Scope: scope,
		})
	}

	fixture.accesses.Stored = next
}

// seedExpiredAccess makes the server restricted by an access whose date has passed.
func (fixture *callsFixture) seedExpiredAccess(t *testing.T, scope domain.AccessScope) {
	t.Helper()

	fixture.accesses.Stored = domain.Accesses{{
		ID:         1,
		Code:       listenerCode,
		Ident:      listenerIdent,
		Expiration: uint64(fixture.clock.Now().Add(-time.Hour).Unix()),
		Scope:      scope,
	}}
}

// wildcardScope is the scope that grants every system and talkgroup.
func wildcardScope() domain.AccessScope {
	return domain.AccessScope{Present: true, Wildcard: true}
}

// scopedTo is the scope naming one system with every talkgroup on it.
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

func fixtureSystem(systemRef uint) domain.System {
	system := domain.System{ID: uint64(systemRef), Label: "System", SystemRef: systemRef}

	return system
}

func fixtureCall(id uint64, systemRef, talkgroupRef uint, at time.Time, name, mime string, length time.Duration) domain.Call {
	call := domain.NewCall()

	call.ID = id
	call.Audio = []byte("audio-bytes-of-" + name)
	call.AudioFilename = name
	call.AudioLength = length
	call.AudioMime = mime
	call.System = domain.System{ID: uint64(systemRef), Label: "System", SystemRef: systemRef}
	call.Talkgroup = domain.Talkgroup{ID: uint64(talkgroupRef), Label: "Talkgroup", TalkgroupRef: talkgroupRef}
	call.Timestamp = at

	return call
}

// callsRequest is one request to one call history endpoint. The credential is left out of the struct
// when the test is about a request that carries none, because these endpoints answer differently
// depending on whether the caller proved anything.
type callsRequest struct {
	body   string
	code   string
	method string
	path   string
	token  string

	// Range is the range header, when the test is about a partial recording.
	Range string

	// Forwarded is the forwarding header, when the test is about which machine the server thinks a
	// request came from.
	Forwarded string
}

func (fixture *callsFixture) serve(request callsRequest) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()

	fixture.routes.ServeHTTP(rec, fixture.newRequest(request))

	return rec
}

func (fixture *callsFixture) newRequest(request callsRequest) *http.Request {
	req := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))

	if request.token != "" {
		req.Header.Set("Authorization", request.token)
	}

	if request.code != "" {
		req.Header.Set("X-Access-Code", request.code)
	}

	if request.Range != "" {
		req.Header.Set("Range", request.Range)
	}

	if request.Forwarded != "" {
		req.Header.Set("X-Forwarded-For", request.Forwarded)
	}

	return req
}

// trustedLocalhostProxy names the peers these tests connect from as proxies the operator named, the
// way the administrator's tests do: the address the endpoints name is the address the budget is
// keyed on, and the forwarding header has to be honored for a test to be able to change it.
func trustedLocalhostProxy() *httpaddr.Trusted {
	trusted, err := httpaddr.ParseTrusted("127.0.0.0/8, 192.0.2.0/24")
	if err != nil {
		panic("the test's own proxy list does not parse: " + err.Error())
	}

	return trusted
}
