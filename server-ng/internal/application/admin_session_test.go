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
	"testing"
)

// Every administrator endpoint asks this question before it does anything else, and asks it of the
// credential the request carried. It is asked as a query because answering it changes nothing: a session
// that is refused is not ended, and a session that is accepted is not extended.
func TestCheckAdminSessionAcceptsTheTokenAnAdministratorWasJustGiven(t *testing.T) {
	fixture := newLoginFixture()

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if result.Outcome != AdminLoginAccepted {
		t.Fatalf("login outcome = %q, want an accepted login before its token is checked", result.Outcome)
	}

	if !fixture.sessionIsOpen(result.Token) {
		t.Error("the token this server just issued is not accepted by this server")
	}
}

// A credential the server never issued, a credential that is nothing at all, and a credential that looks
// like one are all refused. The administrator's own endpoints answer every one of them the same way, and
// this query has to be able to say so without inventing a distinction the frozen contract does not have.
func TestCheckAdminSessionRefusesACredentialTheServerDidNotIssue(t *testing.T) {
	fixture := newLoginFixture()

	for _, credential := range []string{"", "not-a-token", "token-1"} {
		if fixture.sessionIsOpen(credential) {
			t.Errorf("the credential %q is accepted, want it refused: this server never issued it", credential)
		}
	}
}

// Logging out retires the credential that was presented, and the session it stood for is over from that
// moment — including for the very next request that presents it.
func TestCheckAdminSessionRefusesACredentialAfterItHasBeenLoggedOut(t *testing.T) {
	fixture := newLoginFixture()

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if _, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: result.Token}); err != nil {
		t.Fatalf("logout: %v", err)
	}

	if fixture.sessionIsOpen(result.Token) {
		t.Error("the credential that was logged out is still accepted")
	}
}

// The server holds the last five credentials it issued and no more, so a session that is five logins old
// ends on its own, without anybody logging out. This is the same window the administrator's own browser
// runs into when it keeps a page open across five logins elsewhere.
func TestCheckAdminSessionRefusesTheOldestCredentialOnceTheServerHasIssuedSix(t *testing.T) {
	fixture := newLoginFixture()

	oldest := ""

	for i := 0; i < 6; i++ {
		result, err := fixture.attempt(adminCredential)
		if err != nil {
			t.Fatalf("login %d: %v", i+1, err)
		}

		if result.Outcome != AdminLoginAccepted {
			t.Fatalf("login %d outcome = %q, want an accepted login", i+1, result.Outcome)
		}

		if oldest == "" {
			oldest = result.Token
		}
	}

	if !fixture.sessionIsOpen(fixture.tokens.Window[len(fixture.tokens.Window)-1]) {
		t.Error("the newest credential is not accepted, want the window to hold the five most recent logins")
	}

	if fixture.sessionIsOpen(oldest) {
		t.Errorf("the credential issued six logins ago is still accepted, want it retired: the server holds %v", fixture.tokens.Window)
	}
}
