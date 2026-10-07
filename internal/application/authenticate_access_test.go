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

package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

var accessTime = time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

type accessFixture struct {
	accesses *fakes.Accesses
	systems  *fakes.Systems
	groups   *fakes.Groups
	tags     *fakes.Tags
	options  *fakes.Options
	logs     *fakes.Logs
	hub      *fakes.Hub
	clock    *fakes.Clock

	handler *AuthenticateAccessHandler
	gate    *AccessGate
}

func newAccessFixture() *accessFixture {
	fixture := &accessFixture{
		accesses: &fakes.Accesses{},
		systems:  &fakes.Systems{},
		groups:   &fakes.Groups{},
		tags:     &fakes.Tags{},
		options:  &fakes.Options{Stored: domain.DefaultOptions()},
		logs:     &fakes.Logs{},
		hub:      fakes.NewHub(),
		clock:    fakes.NewClock(accessTime),
	}

	fixture.handler = NewAuthenticateAccessHandler(
		fixture.accesses,
		fixture.systems,
		fixture.groups,
		fixture.tags,
		fixture.options,
		fixture.logs,
		fixture.hub,
		fixture.clock,
	)

	fixture.gate = NewAccessGate(fixture.accesses, fixture.hub)

	return fixture
}

func (fixture *accessFixture) addAccess(access domain.Access) {
	fixture.accesses.Stored = append(fixture.accesses.Stored, access)
}

func (fixture *accessFixture) presentCode(clientID, code string) (AuthenticateAccessResult, error) {
	return fixture.handler.Handle(context.Background(), AuthenticateAccessCommand{ClientID: clientID, Code: code})
}

// loggedContaining reports whether the server wrote a log line containing the fragment,
// which is how these tests pin the legacy warning wording byte for byte.
func (fixture *accessFixture) loggedContaining(fragment string) bool {
	for _, entry := range fixture.logs.Stored {
		if strings.Contains(entry.Message, fragment) {
			return true
		}
	}

	return false
}

func TestAuthenticateAccessGrantsAValidCodeAndSendsTheConfiguration(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch"})
	fixture.hub.Open("listener", "10.0.0.1")

	result, err := fixture.presentCode("listener", "dispatch-code")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessAuthenticated {
		t.Fatalf("outcome = %q, want %q", result.Outcome, AccessAuthenticated)
	}

	access, ok := fixture.hub.AccessOf("listener")
	if !ok || access.ID != 1 {
		t.Errorf("the session holds %+v, want the access the code names", access)
	}

	if len(fixture.hub.DeliveredConfigs["listener"]) != 1 {
		t.Error("an authenticated session must be sent the configuration its access allows")
	}

	if len(fixture.hub.AccessRequests) != 0 {
		t.Errorf("access-code requests = %v, want none after a successful authentication", fixture.hub.AccessRequests)
	}
}

func TestAuthenticateAccessRefusesAnUnknownCodeAndAsksAgain(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch"})
	fixture.hub.Open("listener", "10.0.0.1")

	result, err := fixture.presentCode("listener", "wrong-code")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessCodeInvalid {
		t.Errorf("outcome = %q, want %q", result.Outcome, AccessCodeInvalid)
	}

	if _, ok := fixture.hub.AccessOf("listener"); ok {
		t.Error("a refused code must not leave an access on the session")
	}

	if len(fixture.hub.AccessRequests) != 1 || fixture.hub.AccessRequests[0] != "listener" {
		t.Errorf("access-code requests = %v, want the session asked for its code again", fixture.hub.AccessRequests)
	}

	if len(fixture.hub.DeliveredConfigs["listener"]) != 0 {
		t.Error("a refused code must not send any configuration")
	}

	if !fixture.loggedContaining("invalid access code wrong-code for ip 10.0.0.1") {
		t.Errorf("logs = %v, want the legacy warning naming the code and the address", fixture.logs.Stored)
	}
}

// TestAuthenticateAccessLocksAValidCodeArrivingOnTheLastAttempt pins the legacy order: the
// attempt counter is examined before the code is looked up, and a code that is valid but
// arrives on the last allowed attempt is refused anyway.
func TestAuthenticateAccessLocksAValidCodeArrivingOnTheLastAttempt(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch"})
	fixture.hub.Open("listener", "10.0.0.1")

	for attempt := 1; attempt <= 4; attempt++ {
		result, err := fixture.presentCode("listener", "wrong-code")
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}

		if result.Outcome != AccessCodeInvalid {
			t.Fatalf("attempt %d outcome = %q, want %q", attempt, result.Outcome, AccessCodeInvalid)
		}
	}

	result, err := fixture.presentCode("listener", "dispatch-code")
	if err != nil {
		t.Fatalf("attempt 5: %v", err)
	}

	if result.Outcome != AccessLocked {
		t.Errorf("outcome = %q, want %q: a valid code on the fifth attempt still locks the session", result.Outcome, AccessLocked)
	}

	if !fixture.loggedContaining("locked access for ident dispatch locked") {
		t.Errorf("logs = %v, want the legacy warning, including its wording", fixture.logs.Stored)
	}

	if len(fixture.hub.DeliveredConfigs["listener"]) != 0 {
		t.Error("a locked session must not be sent its configuration")
	}
}

func TestAuthenticateAccessRefusesAValidCodeAfterTheAttemptsAreSpent(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch"})
	fixture.hub.Open("listener", "10.0.0.1")

	for attempt := 1; attempt <= 5; attempt++ {
		if _, err := fixture.presentCode("listener", "wrong-code"); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	result, err := fixture.presentCode("listener", "dispatch-code")
	if err != nil {
		t.Fatalf("attempt 6: %v", err)
	}

	if result.Outcome != AccessAttemptsExhausted {
		t.Errorf("outcome = %q, want %q: the counter is examined before the code is looked up", result.Outcome, AccessAttemptsExhausted)
	}

	if _, ok := fixture.hub.AccessOf("listener"); ok {
		t.Error("a session that has spent its tries must not be granted an access")
	}
}

func TestAuthenticateAccessResetsTheAttemptsAfterASuccessfulCode(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch"})
	fixture.hub.Open("listener", "10.0.0.1")

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := fixture.presentCode("listener", "wrong-code"); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	if got := fixture.hub.AttemptsOf("listener"); got != 3 {
		t.Fatalf("attempts = %d, want the three refusals counted", got)
	}

	result, err := fixture.presentCode("listener", "dispatch-code")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessAuthenticated {
		t.Fatalf("outcome = %q, want %q", result.Outcome, AccessAuthenticated)
	}

	if got := fixture.hub.AttemptsOf("listener"); got != 0 {
		t.Fatalf("attempts = %d, want the accepted code to clear the counter", got)
	}

	// Without the reset this fourth presentation would be the fifth attempt and would lock.
	result, err = fixture.presentCode("listener", "dispatch-code")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessAuthenticated {
		t.Errorf("outcome = %q, want %q: an accepted code starts the tries over", result.Outcome, AccessAuthenticated)
	}
}

func TestAuthenticateAccessKeepsTheAttemptsWhenACodeIsRefused(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{
		ID:         1,
		Code:       "dispatch-code",
		Ident:      "dispatch",
		Expiration: uint64(accessTime.Add(-time.Hour).Unix()),
	})
	fixture.hub.Open("listener", "10.0.0.1")

	if _, err := fixture.presentCode("listener", "dispatch-code"); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if got := fixture.hub.AttemptsOf("listener"); got != 1 {
		t.Errorf("attempts = %d, want a refused code to keep spending tries", got)
	}
}

func TestAuthenticateAccessReportsAnExpiredAccess(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{
		ID:         1,
		Code:       "dispatch-code",
		Ident:      "dispatch",
		Expiration: uint64(accessTime.Add(-time.Hour).Unix()),
	})
	fixture.hub.Open("listener", "10.0.0.1")

	result, err := fixture.presentCode("listener", "dispatch-code")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessExpired {
		t.Errorf("outcome = %q, want %q", result.Outcome, AccessExpired)
	}

	if len(fixture.hub.AccessExpiries) != 1 || fixture.hub.AccessExpiries[0] != "listener" {
		t.Errorf("expiry notices = %v, want the session told its access had run out", fixture.hub.AccessExpiries)
	}

	if !fixture.loggedContaining("expired access for ident dispatch") {
		t.Errorf("logs = %v, want the legacy warning", fixture.logs.Stored)
	}

	// The access stays attached, as in the legacy server, so the session is still known by
	// the identity it presented.
	if access, ok := fixture.hub.AccessOf("listener"); !ok || access.ID != 1 {
		t.Error("a session refused for expiry must still hold the access it presented")
	}
}

func TestAuthenticateAccessNeverExpiresAnAccessWithoutAnExpiration(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch", Expiration: 0})
	fixture.hub.Open("listener", "10.0.0.1")

	result, err := fixture.presentCode("listener", "dispatch-code")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessAuthenticated {
		t.Errorf("outcome = %q, want %q: an access with no expiration never runs out", result.Outcome, AccessAuthenticated)
	}
}

func TestAuthenticateAccessRefusesTheSecondSessionBeyondAnAccessLimit(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch", Limit: 1})

	fixture.hub.Open("first", "10.0.0.1")
	fixture.hub.Open("second", "10.0.0.2")

	result, err := fixture.presentCode("first", "dispatch-code")
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	if result.Outcome != AccessAuthenticated {
		t.Fatalf("first outcome = %q, want %q: one session under a limit of one is within the limit", result.Outcome, AccessAuthenticated)
	}

	result, err = fixture.presentCode("second", "dispatch-code")
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if result.Outcome != AccessLimitReached {
		t.Errorf("second outcome = %q, want %q", result.Outcome, AccessLimitReached)
	}

	if len(fixture.hub.AccessLimits) != 1 || fixture.hub.AccessLimits[0] != "second" {
		t.Errorf("limit notices = %v, want the second session told its identity had too many sessions open", fixture.hub.AccessLimits)
	}

	if !fixture.loggedContaining("too many concurrent connections for ident dispatch, limit is 1") {
		t.Errorf("logs = %v, want the legacy warning naming the limit", fixture.logs.Stored)
	}
}

func TestAuthenticateAccessImposesNoLimitOnAnAccessWithoutOne(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch", Limit: 0})

	for _, clientID := range []string{"one", "two", "three"} {
		fixture.hub.Open(clientID, "10.0.0.1")

		result, err := fixture.presentCode(clientID, "dispatch-code")
		if err != nil {
			t.Fatalf("%s: %v", clientID, err)
		}

		if result.Outcome != AccessAuthenticated {
			t.Errorf("%s outcome = %q, want %q: an access with no limit is unlimited", clientID, result.Outcome, AccessAuthenticated)
		}
	}
}

func TestAuthenticateAccessGrantsTheAnonymousAccessOnAnUnrestrictedServer(t *testing.T) {
	fixture := newAccessFixture()
	fixture.hub.Open("listener", "10.0.0.1")

	result, err := fixture.presentCode("listener", "")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessNotRequired {
		t.Fatalf("outcome = %q, want %q", result.Outcome, AccessNotRequired)
	}

	access, ok := fixture.hub.AccessOf("listener")
	if !ok {
		t.Fatal("an unrestricted server must still give the session an access")
	}

	if access.HasScope() {
		t.Errorf("the anonymous access has a scope %+v, want none", access.Scope)
	}

	if len(fixture.hub.DeliveredConfigs["listener"]) != 1 {
		t.Error("the session must be sent the configuration it is allowed to see")
	}
}

func TestAuthenticateAccessReportsAFailingRepository(t *testing.T) {
	fixture := newAccessFixture()
	fixture.accesses.Err = errors.New("database is locked")
	fixture.hub.Open("listener", "10.0.0.1")

	result, err := fixture.presentCode("listener", "dispatch-code")

	if result.Outcome != AccessFailed {
		t.Errorf("outcome = %q, want %q", result.Outcome, AccessFailed)
	}

	if err == nil {
		t.Error("a repository failure must reach the caller")
	}
}

func TestAccessGateRefusesAnUnauthenticatedSessionOnARestrictedServer(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch"})
	fixture.hub.Open("listener", "10.0.0.1")

	required, err := fixture.gate.RequiresAccessCode(context.Background(), "listener")
	if err != nil {
		t.Fatalf("RequiresAccessCode: %v", err)
	}

	if !required {
		t.Error("RequiresAccessCode = false, want true: a restricted server asks an unauthenticated session for its code")
	}
}

func TestAccessGateLetsAnAuthenticatedSessionThrough(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{
		ID:    1,
		Code:  "dispatch-code",
		Ident: "dispatch",
		Scope: domain.AccessScope{Present: true, Wildcard: true},
	})
	fixture.hub.Open("listener", "10.0.0.1")

	if _, err := fixture.presentCode("listener", "dispatch-code"); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	required, err := fixture.gate.RequiresAccessCode(context.Background(), "listener")
	if err != nil {
		t.Fatalf("RequiresAccessCode: %v", err)
	}

	if required {
		t.Error("RequiresAccessCode = true, want false once the session holds a real access")
	}
}

func TestAccessGateDoesNotGateAnUnrestrictedServer(t *testing.T) {
	fixture := newAccessFixture()
	fixture.hub.Open("listener", "10.0.0.1")

	required, err := fixture.gate.RequiresAccessCode(context.Background(), "listener")
	if err != nil {
		t.Fatalf("RequiresAccessCode: %v", err)
	}

	if required {
		t.Error("RequiresAccessCode = true, want false: a server with no access list asks nothing of its listeners")
	}
}

// TestAccessGateKeepsGatingAnAccessThatNamesNoSystems pins a preserved legacy behavior: the
// gate recognises an unauthenticated session by the absence of a scope, so an access
// configured without systems stays gated for every command except the access code even
// after it authenticates. Recorded in decision-preserved-quirks.md.
func TestAccessGateKeepsGatingAnAccessThatNamesNoSystems(t *testing.T) {
	fixture := newAccessFixture()
	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch"})
	fixture.hub.Open("listener", "10.0.0.1")

	result, err := fixture.presentCode("listener", "dispatch-code")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessAuthenticated {
		t.Fatalf("outcome = %q, want %q: the code is real, so the access is granted", result.Outcome, AccessAuthenticated)
	}

	required, err := fixture.gate.RequiresAccessCode(context.Background(), "listener")
	if err != nil {
		t.Fatalf("RequiresAccessCode: %v", err)
	}

	if !required {
		t.Error("RequiresAccessCode = false, want the legacy behavior preserved: an access naming no systems is indistinguishable from an anonymous session")
	}
}
