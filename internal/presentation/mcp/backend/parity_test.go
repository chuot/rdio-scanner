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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
	callsbackend "rdio-scanner/internal/presentation/calls/backend"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

// The parity suite is the proof that the two stateless call surfaces are one rule. The call history
// endpoints and the Model Context Protocol listener answer the same restricted mode, the same
// expiries, the same scopes, and the same guess budget, because they read the same use case over
// the same stores — and an operator who changed one of them would find this suite naming the half
// that moved.

const (
	paritySystemOneRef = uint(10001)
	paritySystemTwoRef = uint(10002)

	parityCallOnSystemOne = uint64(1)
	parityCallOnSystemTwo = uint64(2)
)

// parityFixture is one server, one access list, one history, and one session window, with both
// stateless call surfaces mounted on the same mux.
type parityFixture struct {
	*serverBundle

	routes http.Handler
}

func newParity(t *testing.T, scope domain.AccessScope) *parityFixture {
	t.Helper()

	bundle := newServer(t, true)
	bundle.accesses.Stored = domain.Accesses{
		{ID: 1, Code: listenerCode, Ident: listenerIdent, Scope: scope},
	}
	bundle.tokens.Window = []string{adminToken}

	bundle.calls = fakes.NewCalls()
	bundle.calls.Stored = map[uint64]domain.Call{
		parityCallOnSystemOne: parityCall(parityCallOnSystemOne, paritySystemOneRef, fixtureTime.Add(-2*time.Hour)),
		parityCallOnSystemTwo: parityCall(parityCallOnSystemTwo, paritySystemTwoRef, fixtureTime.Add(-time.Hour)),
	}
	bundle.systems = &fakes.Systems{Stored: domain.Systems{
		{ID: 1, Label: "System", SystemRef: paritySystemOneRef},
		{ID: 2, Label: "System", SystemRef: paritySystemTwoRef},
	}}

	bundle.resolve = application.NewResolveAccessQueryHandler(bundle.accesses, bundle.logs, bundle.clock)

	// One resolver, both surfaces. The guess budget lives in the resolver, so this is the wiring
	// that makes it one budget per address, and not two budgets side by side that an address
	// spends twice over.
	bundle.server = New(bundle.Deps())
	bundle.handler = bundle.server.Handler()

	fixture := &parityFixture{serverBundle: bundle}
	fixture.routes = fixture.buildRoutes(t)

	return fixture
}

// buildRoutes is the routing the composition root installs for these two surfaces: the call history
// registered unconditionally, and the Model Context Protocol listener beside it.
func (fixture *parityFixture) buildRoutes(t *testing.T) http.Handler {
	mux := http.NewServeMux()

	callsbackend.NewCalls(callsbackend.CallsDeps{
		Call:    application.NewGetCallQueryHandler(fixture.calls),
		Clock:   fixture.clock,
		Logs:    fixture.logs,
		Resolve: fixture.resolve,
		Search:  application.NewSearchCallsQueryHandler(fixture.calls, fixture.systems, fixture.groups, fixture.tags),
		Session: application.NewCheckAdminSessionHandler(fixture.tokens),
		Trusted: parityProxy(t),
	}).Register(mux)

	fixture.server.Register(mux)

	return mux
}

func parityCall(id uint64, systemRef uint, at time.Time) domain.Call {
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

// parityProxy names the address these tests connect from as a peer the operator named, so a test
// can name the address a guess is attributed to.
func parityProxy(t *testing.T) *httpaddr.Trusted {
	t.Helper()

	trusted, err := httpaddr.ParseTrusted("192.0.2.0/24")
	if err != nil {
		t.Fatalf("the parity suite's own proxy list does not parse: %v", err)
	}

	return trusted
}

// rest asks the call history surface, over the real routes.
func (fixture *parityFixture) rest(t *testing.T, method, path, code, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = testAddress

	if code != "" {
		req.Header.Set(accessCodeHeader, code)
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	fixture.routes.ServeHTTP(rec, req)

	return rec
}

// mcp asks the Model Context Protocol surface, over the same routes the composition root mounts.
func (fixture *parityFixture) mcp(t *testing.T, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(""))
	req.RemoteAddr = testAddress
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	fixture.routes.ServeHTTP(rec, req)

	return rec
}

// The refusal is the same answer on both surfaces: same status, same empty body.
func TestTheTwoSurfacesRefuseACallerCarryingNoCredentialTheSameWay(t *testing.T) {
	fixture := newParity(t, wildcardScope())

	rest := fixture.rest(t, http.MethodPost, "/api/calls/search", "", "", `{}`)
	mcp := fixture.mcp(t, nil)

	if rest.Code != http.StatusUnauthorized {
		t.Fatalf("the call history answered %d for a caller carrying no credential, want 401: %s",
			rest.Code, rest.Body.String())
	}
	if mcp.Code != http.StatusUnauthorized {
		t.Fatalf("the Model Context Protocol listener answered %d for the same caller, want 401: %s",
			mcp.Code, mcp.Body.String())
	}
	if mcp.Body.String() != rest.Body.String() {
		t.Fatalf("the two surfaces refused the same caller differently: history %q, listener %q",
			rest.Body.String(), mcp.Body.String())
	}
}

func TestTheTwoSurfacesRefuseAnExpiredAccessCodeTheSameWay(t *testing.T) {
	fixture := newParity(t, wildcardScope())
	fixture.accesses.Stored = domain.Accesses{
		{
			ID:         1,
			Code:       listenerCode,
			Ident:      listenerIdent,
			Expiration: uint64(fixture.clock.Now().Add(-time.Minute).Unix()),
			Scope:      wildcardScope(),
		},
	}

	rest := fixture.rest(t, http.MethodPost, "/api/calls/search", listenerCode, "", `{}`)
	mcp := fixture.mcp(t, map[string]string{accessCodeHeader: listenerCode})

	if rest.Code != http.StatusUnauthorized {
		t.Fatalf("the call history answered %d for an expired access code, want 401", rest.Code)
	}
	if mcp.Code != http.StatusUnauthorized {
		t.Fatalf("the Model Context Protocol listener answered %d for the same expired access code, want 401",
			mcp.Code)
	}
}

// A code that resolves is accepted by both surfaces, and the scope it carries is the same scope on
// both: the history search is restricted by it, and the capabilities are handed the identical value.
func TestTheTwoSurfacesAcceptOneAccessCodeAndCarryOneScope(t *testing.T) {
	scope := scopedTo(paritySystemOneRef)
	fixture := newParity(t, scope)

	rest := fixture.rest(t, http.MethodPost, "/api/calls/search", listenerCode, "", `{}`)
	if rest.Code != http.StatusOK {
		t.Fatalf("the call history answered %d for a valid access code, want the page: %s",
			rest.Code, rest.Body.String())
	}

	var answered struct {
		Count   uint `json:"count"`
		Results []struct {
			ID uint64 `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rest.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the search answer is not a page: %v", err)
	}
	if answered.Count != 1 {
		t.Fatalf("the scoped history search returned %d calls, want the one call on the system the code names",
			answered.Count)
	}

	mcp := fixture.mcp(t, map[string]string{accessCodeHeader: listenerCode})
	if mcp.Code == http.StatusUnauthorized {
		t.Fatalf("the Model Context Protocol listener refused the code the call history accepted: %s",
			mcp.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = testAddress
	req.Header.Set(accessCodeHeader, listenerCode)

	who, ok := CallerOf(fixture.caller(req))
	if !ok {
		t.Fatal("the Model Context Protocol listener carried no caller to its capabilities")
	}
	if !reflect.DeepEqual(who.Scope, scope) {
		t.Fatalf("the listener carries scope %+v, want the scope the history search restricted itself with %+v",
			who.Scope, scope)
	}
	if who.Tier != TierReadOnly {
		t.Fatalf("an access code reached the listener as tier %v, want the read-only tier", who.Tier)
	}
}

// A call the caller may not see is answered as absent on both surfaces, by the same scope.
func TestTheTwoSurfacesAnswerAnOutOfScopeCallAsAbsent(t *testing.T) {
	fixture := newParity(t, scopedTo(paritySystemOneRef))

	rest := fixture.rest(t, http.MethodGet, "/api/calls/2/metadata", listenerCode, "", "")
	if rest.Code != http.StatusNotFound {
		t.Fatalf("the call history answered %d for a call outside the code's scope, want 404: %s",
			rest.Code, rest.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = testAddress
	req.Header.Set(accessCodeHeader, listenerCode)

	who, ok := CallerOf(fixture.caller(req))
	if !ok {
		t.Fatal("the Model Context Protocol listener carried no caller to its capabilities")
	}

	// The capability checks the call the same way the endpoint does, with the domain's own rule,
	// so the refusal the endpoint gives is the refusal the capability gives.
	if who.Scope.Grants(parityCall(parityCallOnSystemTwo, paritySystemTwoRef, fixtureTime)) {
		t.Fatal("the scope the listener carries grants a call the call history answered as absent")
	}
	if !who.Scope.Grants(parityCall(parityCallOnSystemOne, paritySystemOneRef, fixtureTime)) {
		t.Fatal("the scope the listener carries refuses a call the call history handed over")
	}
}

// The budget belongs to the address, so it is one budget across both surfaces: guessing at one
// closes the other, and the order the guesses arrive in does not matter.
func TestTheTwoSurfacesShareOneGuessBudgetPerAddress(t *testing.T) {
	fixture := newParity(t, wildcardScope())

	for i := 0; i < accessAttemptsMax; i++ {
		fixture.rest(t, http.MethodPost, "/api/calls/search", "not-a-listed-code", "", `{}`)
	}

	mcp := fixture.mcp(t, map[string]string{accessCodeHeader: listenerCode})
	if mcp.Code != http.StatusUnauthorized {
		t.Fatalf("the listener accepted a valid code after the call history spent the address's budget, "+
			"want 401: %s", mcp.Body.String())
	}

	// And the other way round, on a server whose budget the listener spent.
	fixture = newParity(t, wildcardScope())

	for i := 0; i < accessAttemptsMax; i++ {
		fixture.mcp(t, map[string]string{accessCodeHeader: "not-a-listed-code"})
	}

	rest := fixture.rest(t, http.MethodPost, "/api/calls/search", listenerCode, "", `{}`)
	if rest.Code != http.StatusUnauthorized {
		t.Fatalf("the call history accepted a valid code after the listener spent the address's budget, "+
			"want 401: %s", rest.Body.String())
	}
}

// The administrator's credential opens both surfaces, and opens the listener's write tier.
func TestTheTwoSurfacesAcceptOneAdministratorSession(t *testing.T) {
	fixture := newParity(t, wildcardScope())

	rest := fixture.rest(t, http.MethodPost, "/api/calls/search", "", adminToken, `{}`)
	if rest.Code != http.StatusOK {
		t.Fatalf("the call history answered %d for an administrator session, want the page: %s",
			rest.Code, rest.Body.String())
	}

	mcp := fixture.mcp(t, map[string]string{"Authorization": adminToken})
	if mcp.Code == http.StatusUnauthorized {
		t.Fatalf("the listener refused the session the call history accepted: %s", mcp.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = testAddress
	req.Header.Set("Authorization", adminToken)

	who, ok := CallerOf(fixture.caller(req))
	if !ok {
		t.Fatal("the listener carried no caller to its capabilities")
	}
	if who.Tier != TierFull {
		t.Fatalf("an administrator session reached the listener as tier %v, want the write tier", who.Tier)
	}
}

// A server with no access list lets both surfaces read, and lets neither write.
func TestTheTwoSurfacesTakeAnAnonymousCallerOnAnUnrestrictedServer(t *testing.T) {
	fixture := newParity(t, wildcardScope())
	fixture.accesses.Stored = domain.Accesses{}

	rest := fixture.rest(t, http.MethodPost, "/api/calls/search", "", "", `{}`)
	if rest.Code != http.StatusOK {
		t.Fatalf("an unrestricted call history answered %d for an anonymous caller, want the page: %s",
			rest.Code, rest.Body.String())
	}

	mcp := fixture.mcp(t, nil)
	if mcp.Code == http.StatusUnauthorized {
		t.Fatalf("an unrestricted listener refused the anonymous caller the call history accepted: %s",
			mcp.Body.String())
	}

	who, ok := CallerOf(fixture.caller(httptest.NewRequest(http.MethodPost, "/mcp", nil)))
	if !ok {
		t.Fatal("an unrestricted listener carried no caller to its capabilities")
	}
	if who.Tier != TierReadOnly {
		t.Fatalf("an anonymous caller on an unrestricted server holds tier %v, want the read-only tier: "+
			"reading is what a listener does, and the write tier is gated by the administrator's credential",
			who.Tier)
	}
}
