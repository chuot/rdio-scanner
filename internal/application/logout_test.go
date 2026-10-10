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
	"testing"

	"rdio-scanner/internal/domain"
)

func TestLogoutHandlerRetiresThePresentedToken(t *testing.T) {
	fixture := newLoginFixture()

	token := fixture.mustLogin(t, adminCredential)

	result, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: token})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}

	if result.Outcome != AdminLogoutAccepted {
		t.Errorf("outcome = %q, want the session ended", result.Outcome)
	}

	if len(fixture.tokens.Window) != 0 {
		t.Errorf("token window = %v, want the ended session's token taken out of the accepted set", fixture.tokens.Window)
	}

	result, err = fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: token})
	if err != nil {
		t.Fatalf("second logout: %v", err)
	}

	if result.Outcome != AdminLogoutRefused {
		t.Errorf("second logout outcome = %q, want a token that is already ended to be refused", result.Outcome)
	}
}

// TestLogoutWritesThatTheAdministratorSignedOut is the entry the log view had nothing for before: ending
// your own session was the one thing this server did in silence, so an operator could not tell when an
// administrator stopped watching.

// TestLogoutWritesThatTheAdministratorSignedOut is the entry the log view had nothing for before: ending
// your own session was the one thing this server did in silence, so an operator could not tell when an
// administrator stopped watching.
func TestLogoutWritesThatTheAdministratorSignedOut(t *testing.T) {
	fixture := newLoginFixture()

	token := fixture.mustLogin(t, adminCredential)

	if _, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: token}); err != nil {
		t.Fatalf("logout: %v", err)
	}

	wrote := fixture.signedOut()

	if len(wrote) != 1 {
		t.Fatalf("server log = %v, want one entry saying the administrator signed out", fixture.entries())
	}

	if wrote[0].Level != domain.LogLevelInfo {
		t.Errorf("the sign-out was written at level %q, want an ordinary entry, not a warning", wrote[0].Level)
	}

	if got := wrote[0].DateTime; !got.Equal(loginTime) {
		t.Errorf("the sign-out was stamped %v, want the instant the session was ended", got)
	}
}

// A logout the server refused did not end a session, so it is not a sign-out and writes no entry.

// A logout the server refused did not end a session, so it is not a sign-out and writes no entry.
func TestRefusedLogoutWritesNothingToTheServerLog(t *testing.T) {
	fixture := newLoginFixture()

	fixture.mustLogin(t, adminCredential)

	result, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: "a token nobody was given"})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}

	if result.Outcome != AdminLogoutRefused {
		t.Fatalf("outcome = %q, want a token the server never issued to be refused", result.Outcome)
	}

	if got := fixture.entries(); len(got) != 0 {
		t.Errorf("server log = %v, want a refused logout to say nothing about itself", got)
	}
}

func TestLogoutHandlerRefusesATokenTheServerDoesNotAccept(t *testing.T) {
	fixture := newLoginFixture()

	fixture.mustLogin(t, adminCredential)

	result, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: "a token nobody was given"})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}

	if result.Outcome != AdminLogoutRefused {
		t.Errorf("outcome = %q, want a token the server never issued to be refused", result.Outcome)
	}

	if len(fixture.tokens.Window) != 1 {
		t.Errorf("token window = %v, want a refused logout to end no session", fixture.tokens.Window)
	}
}

func TestLogoutHandlerLeavesOtherAdministratorSessionsRunning(t *testing.T) {
	fixture := newLoginFixture()

	first := fixture.mustLogin(t, adminCredential)
	second := fixture.mustLogin(t, adminCredential)

	if _, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: first}); err != nil {
		t.Fatalf("logout of the first session: %v", err)
	}

	result, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: second})
	if err != nil {
		t.Fatalf("logout of the second session: %v", err)
	}

	if result.Outcome != AdminLogoutAccepted {
		t.Errorf("outcome = %q, want ending one administrator session to leave the others open", result.Outcome)
	}
}

// TestAdminLoginAndLogoutAreUnchangedWhenTheServerLogCannotBeWritten is CA-007 for the administrator's
// credential: every one of these use cases writes a log entry as a side effect, and a log view that cannot
// be written is not allowed to change what the server decided or what it did. A deployment whose log table
// is broken must still be able to sign in and sign out.
// TestALogoutThatCouldNotEndTheSessionSaysSo is a sign-out the server could not carry out: the token was
// accepted, and retiring it failed. The session stays open, the caller is told, and until now nothing was
// written at all — so an administrator reporting "I cannot log out" left nothing to check.

// TestAdminLoginAndLogoutAreUnchangedWhenTheServerLogCannotBeWritten is CA-007 for the administrator's
// credential: every one of these use cases writes a log entry as a side effect, and a log view that cannot
// be written is not allowed to change what the server decided or what it did. A deployment whose log table
// is broken must still be able to sign in and sign out.
// TestALogoutThatCouldNotEndTheSessionSaysSo is a sign-out the server could not carry out: the token was
// accepted, and retiring it failed. The session stays open, the caller is told, and until now nothing was
// written at all — so an administrator reporting "I cannot log out" left nothing to check.
func TestALogoutThatCouldNotEndTheSessionSaysSo(t *testing.T) {
	fixture := newLoginFixture()

	token := fixture.mustLogin(t, adminCredential)

	fixture.tokens.RevokeErr = errors.New("no such table: tokens")

	result, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: token})

	if !errors.Is(err, fixture.tokens.RevokeErr) {
		t.Fatalf("logout error = %v, want the token store's own failure", err)
	}

	if result.Outcome != AdminLogoutFailed {
		t.Fatalf("outcome = %q, want a logout the server could not carry out", result.Outcome)
	}

	if !fixture.sessionIsOpen(token) {
		t.Error("the session was closed by a logout that failed, want the failure left where it happened")
	}

	assertOneFailureEntry(t, fixture, "the administrator session could not be ended", fixture.tokens.RevokeErr)
}

func TestAdminLoginAndLogoutAreUnchangedWhenTheServerLogCannotBeWritten(t *testing.T) {
	fixture := newLoginFixture()
	fixture.logs.Err = errors.New("no such table: logs")

	accepted, err := fixture.login.Handle(context.Background(), AdminLoginCommand{
		RemoteAddr: adminAddress,
		Credential: adminCredential,
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if accepted.Outcome != AdminLoginAccepted {
		t.Errorf("login outcome = %q, want the credential accepted even though the log could not be written", accepted.Outcome)
	}

	if !fixture.sessionIsOpen(accepted.Token) {
		t.Error("the session the login issued is not open, want the log failure to change nothing about it")
	}

	refused, err := fixture.attempt("not the credential on record")
	if err != nil {
		t.Fatalf("refused login: %v", err)
	}

	if refused.Outcome != AdminLoginInvalidCredential {
		t.Errorf("refused outcome = %q, want the credential still refused, with the warning it earns dropped", refused.Outcome)
	}

	if len(fixture.tokens.Issued) != 1 {
		t.Errorf("tokens issued = %v, want the one session the accepted credential earned", fixture.tokens.Issued)
	}

	result, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: accepted.Token})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}

	if result.Outcome != AdminLogoutAccepted {
		t.Errorf("logout outcome = %q, want the session ended even though the log could not be written", result.Outcome)
	}

	if fixture.sessionIsOpen(accepted.Token) {
		t.Error("the session is still open after a logout, want the log failure to change nothing about it")
	}
}
