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

var loginTime = time.Date(2026, 4, 2, 8, 15, 0, 0, time.UTC)

const (
	adminAddress    = "203.0.113.7"
	otherAddress    = "198.51.100.4"
	adminCredential = "correct horse battery staple"
)

type loginFixture struct {
	options   *fakes.Options
	logs      *fakes.Logs
	tokens    *fakes.Tokens
	passwords *fakes.PasswordVerifier
	clock     *fakes.Clock

	login  *AdminLoginHandler
	logout *AdminLogoutHandler
	check  *CheckAdminSessionHandler
}

func newLoginFixture() *loginFixture {
	fixture := &loginFixture{
		// The credential on record is held in the options, which is where the server keeps
		// it. The in-memory verifier below compares the two credentials as written; the
		// real one compares a submitted password against the bcrypt hash stored here, and
		// nothing in the application layer can tell the difference.
		options:   &fakes.Options{Stored: domain.Options{AdminPasswordHash: adminCredential}},
		logs:      &fakes.Logs{},
		tokens:    fakes.NewTokens(),
		passwords: fakes.NewPasswordVerifier(),
		clock:     fakes.NewClock(loginTime),
	}

	fixture.login = NewAdminLoginHandler(fixture.options, fixture.logs, fixture.tokens, fixture.passwords, fixture.clock)
	fixture.logout = NewAdminLogoutHandler(fixture.tokens, fixture.logs, fixture.clock)
	fixture.check = NewCheckAdminSessionHandler(fixture.tokens)

	return fixture
}

// sessionIsOpen asks the question every administrator endpoint asks before it does anything else.

// sessionIsOpen asks the question every administrator endpoint asks before it does anything else.
func (fixture *loginFixture) sessionIsOpen(token string) bool {
	return fixture.check.Handle(context.Background(), CheckAdminSessionQuery{Token: token})
}

// attempt logs in from the address the tests use by default.

// attempt logs in from the address the tests use by default.
func (fixture *loginFixture) attempt(credential string) (AdminLoginResult, error) {
	return fixture.attemptFrom(adminAddress, credential)
}

// attemptFrom logs in from a named address, which is how the tests show that the attempt
// budget belongs to the address that made the attempt.

// attemptFrom logs in from a named address, which is how the tests show that the attempt
// budget belongs to the address that made the attempt.
func (fixture *loginFixture) attemptFrom(remoteAddr, credential string) (AdminLoginResult, error) {
	return fixture.login.Handle(context.Background(), AdminLoginCommand{
		RemoteAddr: remoteAddr,
		Credential: credential,
	})
}

// mustLogin logs in and fails the test unless a session was started, returning the token
// the server issued.

// mustLogin logs in and fails the test unless a session was started, returning the token
// the server issued.
func (fixture *loginFixture) mustLogin(t *testing.T, credential string) string {
	t.Helper()

	result, err := fixture.attempt(credential)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if result.Outcome != AdminLoginAccepted {
		t.Fatalf("login outcome = %q, want an accepted session", result.Outcome)
	}

	return result.Token
}

// warnings returns the entries the login use case reported to the server log, which is what
// a deployment sees in its log view.

// warnings returns the entries the login use case reported to the server log, which is what
// a deployment sees in its log view.
func (fixture *loginFixture) warnings() []domain.LogEntry {
	out := []domain.LogEntry{}

	for _, entry := range fixture.logs.Stored {
		if entry.Level == domain.LogLevelWarn {
			out = append(out, entry)
		}
	}

	return out
}

// entries returns everything the login and logout use cases wrote to the server log. A sign-out is not a
// warning, so the tests that care about what the log view holds read it whole.

// entries returns everything the login and logout use cases wrote to the server log. A sign-out is not a
// warning, so the tests that care about what the log view holds read it whole.
func (fixture *loginFixture) entries() []domain.LogEntry {
	out := []domain.LogEntry{}

	out = append(out, fixture.logs.Stored...)

	return out
}

// signedOut returns the entries that say a session was ended.

// signedOut returns the entries that say a session was ended.
func (fixture *loginFixture) signedOut() []domain.LogEntry {
	out := []domain.LogEntry{}

	for _, entry := range fixture.logs.Stored {
		if entry.Source == domain.LogSourceLogin && entry.Message == "the administrator signed out" {
			out = append(out, entry)
		}
	}

	return out
}

func TestLoginHandlerStartsASessionForTheRightCredential(t *testing.T) {
	fixture := newLoginFixture()

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if result.Outcome != AdminLoginAccepted {
		t.Errorf("outcome = %q, want an accepted session", result.Outcome)
	}

	if result.Token != "token-1" {
		t.Errorf("token = %q, want the token the issuer handed out", result.Token)
	}

	if len(fixture.tokens.Window) != 1 {
		t.Errorf("token window = %v, want the issued token accepted for the new session", fixture.tokens.Window)
	}

	if len(fixture.warnings()) != 0 {
		t.Errorf("server log = %v, want a successful login to say nothing about it", fixture.warnings())
	}
}

func TestLoginHandlerChecksTheCredentialTheServerHasOnRecord(t *testing.T) {
	fixture := newLoginFixture()

	if _, err := fixture.attempt("wrong credential"); err != nil {
		t.Fatalf("login: %v", err)
	}

	if fixture.passwords.AskedFor() != 1 {
		t.Fatalf("credential checks = %d, want the submitted credential checked once", fixture.passwords.AskedFor())
	}

	asked := fixture.passwords.Asked[0]
	if asked.Stored != adminCredential || asked.Submitted != "wrong credential" {
		t.Errorf("credential check = %+v, want the submitted credential compared with the one on record", asked)
	}
}

func TestLoginHandlerRefusesAWrongCredentialAndSaysWhyInItsOwnLog(t *testing.T) {
	fixture := newLoginFixture()

	result, err := fixture.attempt("wrong credential")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if result.Outcome != AdminLoginInvalidCredential {
		t.Errorf("outcome = %q, want a credential that does not match", result.Outcome)
	}

	if result.Token != "" {
		t.Errorf("token = %q, want no session started for a credential that does not match", result.Token)
	}

	if len(fixture.tokens.Issued) != 0 {
		t.Errorf("issued tokens = %v, want none for a refused credential", fixture.tokens.Issued)
	}

	warnings := fixture.warnings()
	if len(warnings) != 1 {
		t.Fatalf("server log = %v, want one warning about the refused attempt", warnings)
	}

	if warnings[0].Source != domain.LogSourceLogin {
		t.Errorf("warning = %q, want it filed with the part of the server that opens administrator sessions", warnings[0].Source)
	}

	if warnings[0].Message != "a login attempt from 203.0.113.7 was refused" {
		t.Errorf("warning = %q, want the refused attempt and the address it came from", warnings[0].Message)
	}
}

// TestLoginHandlerRefusesAnEmptyCredentialEvenWhenNothingIsOnRecord pins the legacy rule
// that an empty submission is never compared against the stored credential: a server whose
// stored credential is empty still refuses a caller that submits nothing.

// TestLoginHandlerRefusesAnEmptyCredentialEvenWhenNothingIsOnRecord pins the legacy rule
// that an empty submission is never compared against the stored credential: a server whose
// stored credential is empty still refuses a caller that submits nothing.
func TestLoginHandlerRefusesAnEmptyCredentialEvenWhenNothingIsOnRecord(t *testing.T) {
	fixture := newLoginFixture()
	fixture.options.Stored.AdminPasswordHash = ""

	result, err := fixture.attempt("")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if result.Outcome != AdminLoginInvalidCredential {
		t.Errorf("outcome = %q, want an empty credential to be refused", result.Outcome)
	}

	if fixture.passwords.AskedFor() != 0 {
		t.Errorf("credential checks = %d, want an empty credential never handed to the verifier", fixture.passwords.AskedFor())
	}

	if len(fixture.tokens.Issued) != 0 {
		t.Errorf("issued tokens = %v, want no session started for an empty credential", fixture.tokens.Issued)
	}
}

func TestLoginHandlerAnswersTheThirdAttemptOnItsMerits(t *testing.T) {
	fixture := newLoginFixture()

	for attempt := 1; attempt <= 2; attempt++ {
		result, err := fixture.attempt("wrong credential")
		if err != nil {
			t.Fatalf("login attempt %d: %v", attempt, err)
		}

		if result.Outcome != AdminLoginInvalidCredential {
			t.Fatalf("attempt %d outcome = %q, want the attempt refused for its credential", attempt, result.Outcome)
		}
	}

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if result.Outcome != AdminLoginAccepted {
		t.Errorf("third attempt outcome = %q, want the third attempt inside the budget to be answered on its merits", result.Outcome)
	}
}

func TestLoginHandlerRefusesTheFourthAttemptWithoutLookingAtTheCredential(t *testing.T) {
	fixture := newLoginFixture()

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := fixture.attempt("wrong credential"); err != nil {
			t.Fatalf("login attempt %d: %v", attempt, err)
		}
	}

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if result.Outcome != AdminLoginAttemptLimit {
		t.Errorf("fourth attempt outcome = %q, want the fourth attempt refused whatever credential it carries", result.Outcome)
	}

	if result.Token != "" {
		t.Errorf("token = %q, want no session started for a refused attempt", result.Token)
	}

	if fixture.passwords.AskedFor() != 3 {
		t.Errorf("credential checks = %d, want the refused attempt never examined the credential", fixture.passwords.AskedFor())
	}

	if len(fixture.tokens.Issued) != 0 {
		t.Errorf("issued tokens = %v, want a refused attempt to start no session", fixture.tokens.Issued)
	}
}

func TestLoginHandlerWarnsAboutTooManyAttemptsOncePerLockout(t *testing.T) {
	fixture := newLoginFixture()

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := fixture.attempt("wrong credential"); err != nil {
			t.Fatalf("login attempt %d: %v", attempt, err)
		}
	}

	if _, err := fixture.attempt(adminCredential); err != nil {
		t.Fatalf("login attempt 4: %v", err)
	}

	warnings := fixture.warnings()
	if len(warnings) != 4 {
		t.Fatalf("server log = %v, want the three refused credentials and one lockout warning", warnings)
	}

	want := "too many login attempts from 203.0.113.7"

	if warnings[3].Source != domain.LogSourceLogin {
		t.Errorf("lockout warning = %q, want it filed with the part of the server that opens administrator sessions", warnings[3].Source)
	}

	if warnings[3].Message != want {
		t.Errorf("lockout warning = %q, want %q", warnings[3].Message, want)
	}

	if _, err := fixture.attempt(adminCredential); err != nil {
		t.Fatalf("login attempt 5: %v", err)
	}

	if got := fixture.warnings(); len(got) != 4 {
		t.Errorf("server log = %v, want the lockout warning written once and never repeated", got)
	}
}

func TestLoginHandlerCountsAttemptsSeparatelyForEachAddress(t *testing.T) {
	fixture := newLoginFixture()

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := fixture.attempt("wrong credential"); err != nil {
			t.Fatalf("login attempt %d: %v", attempt, err)
		}
	}

	result, err := fixture.attemptFrom(otherAddress, adminCredential)
	if err != nil {
		t.Fatalf("login from another address: %v", err)
	}

	if result.Outcome != AdminLoginAccepted {
		t.Errorf("outcome = %q, want an address's budget to belong to that address alone", result.Outcome)
	}
}

// TestLoginHandlerRefusesAttemptsMadeInsideTheAttemptWindow is the repaired half of Q10: the window the
// contract describes is the window the server enforces.
//
// The legacy server built its window with `time.Duration(time.Duration.Minutes(10))`, a method
// expression that converts ten nanoseconds to minutes and back and truncates to zero, so an address's
// attempt count never aged out. Here the window is the ten minutes that was meant, and an address that
// has spent its budget is refused while that window is still open.
// See docs/specs/FEAT-004-legacy-defect-repairs/decision-login-attempt-window.md.

// TestLoginHandlerRefusesAttemptsMadeInsideTheAttemptWindow is the repaired half of Q10: the window the
// contract describes is the window the server enforces.
//
// The legacy server built its window with `time.Duration(time.Duration.Minutes(10))`, a method
// expression that converts ten nanoseconds to minutes and back and truncates to zero, so an address's
// attempt count never aged out. Here the window is the ten minutes that was meant, and an address that
// has spent its budget is refused while that window is still open.
// See docs/specs/FEAT-004-legacy-defect-repairs/decision-login-attempt-window.md.
func TestLoginHandlerRefusesAttemptsMadeInsideTheAttemptWindow(t *testing.T) {
	if loginAttemptsWindow != 10*time.Minute {
		t.Fatalf("attempt window = %v, want the ten minutes the contract describes", loginAttemptsWindow)
	}

	fixture := newLoginFixture()

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := fixture.attempt("wrong credential"); err != nil {
			t.Fatalf("login attempt %d: %v", attempt, err)
		}
	}

	fixture.clock.At = loginTime.Add(9 * time.Minute)

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login inside the window: %v", err)
	}

	if result.Outcome != AdminLoginAttemptLimit {
		t.Errorf("outcome inside the window = %q, want the address still refused", result.Outcome)
	}

	if fixture.passwords.AskedFor() != 3 {
		t.Errorf("credential checks = %d, want a refused attempt to leave the credential unexamined", fixture.passwords.AskedFor())
	}
}

// TestLoginHandlerRestoresTheAttemptBudgetAfterTheWindow is the other half of the repair: the budget is
// measured over a window, so an address that waits out the window is answered on its merits again.

// TestLoginHandlerRestoresTheAttemptBudgetAfterTheWindow is the other half of the repair: the budget is
// measured over a window, so an address that waits out the window is answered on its merits again.
func TestLoginHandlerRestoresTheAttemptBudgetAfterTheWindow(t *testing.T) {
	fixture := newLoginFixture()

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := fixture.attempt("wrong credential"); err != nil {
			t.Fatalf("login attempt %d: %v", attempt, err)
		}
	}

	fixture.clock.At = loginTime.Add(10*time.Minute + time.Millisecond)

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login past the window: %v", err)
	}

	if result.Outcome != AdminLoginAccepted {
		t.Errorf("outcome past the window = %q, want the budget restored once the window it was measured over had passed", result.Outcome)
	}

	if result.Token == "" {
		t.Errorf("token = %q, want the session the restored budget pays for", result.Token)
	}
}

// TestLoginHandlerCountsAFreshWindowAsAFreshBudget keeps the restored budget a budget: the address gets
// three attempts in the new window, not an unlimited run at the server.

// TestLoginHandlerCountsAFreshWindowAsAFreshBudget keeps the restored budget a budget: the address gets
// three attempts in the new window, not an unlimited run at the server.
func TestLoginHandlerCountsAFreshWindowAsAFreshBudget(t *testing.T) {
	fixture := newLoginFixture()

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := fixture.attempt("wrong credential"); err != nil {
			t.Fatalf("login attempt %d: %v", attempt, err)
		}
	}

	fixture.clock.At = loginTime.Add(11 * time.Minute)

	for attempt := 1; attempt <= 3; attempt++ {
		result, err := fixture.attempt("wrong credential")
		if err != nil {
			t.Fatalf("login attempt %d of the new window: %v", attempt, err)
		}

		if result.Outcome != AdminLoginInvalidCredential {
			t.Fatalf("outcome = %q, want attempt %d of the new window answered on its merits", result.Outcome, attempt)
		}
	}

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login past the new window's budget: %v", err)
	}

	if result.Outcome != AdminLoginAttemptLimit {
		t.Errorf("outcome = %q, want the fourth attempt of the new window refused", result.Outcome)
	}
}

func TestSuccessfulLoginClearsEveryAddressesAttemptCount(t *testing.T) {
	fixture := newLoginFixture()

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := fixture.attempt("wrong credential"); err != nil {
			t.Fatalf("login attempt %d: %v", attempt, err)
		}
	}

	if _, err := fixture.attemptFrom(otherAddress, adminCredential); err != nil {
		t.Fatalf("login from another address: %v", err)
	}

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login after another address signed in: %v", err)
	}

	if result.Outcome != AdminLoginAccepted {
		t.Errorf("outcome = %q, want a successful login to clear the attempt counts, as the legacy server does", result.Outcome)
	}
}

// TestLoginHandlerLegacyPasswordNeedChangeQuirk pins the legacy login response, which
// reports that the password needs changing on every successful login whatever the stored
// option says. The option itself is only reported by the configuration endpoints.

// TestLoginHandlerLegacyPasswordNeedChangeQuirk pins the legacy login response, which
// reports that the password needs changing on every successful login whatever the stored
// option says. The option itself is only reported by the configuration endpoints.
func TestLoginHandlerLegacyPasswordNeedChangeQuirk(t *testing.T) {
	fixture := newLoginFixture()
	fixture.options.Stored.AdminPasswordNeedChange = false

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if !result.PasswordNeedChange {
		t.Errorf("password need change = %v, want the login response to report it as the legacy server does", result.PasswordNeedChange)
	}
}

func TestLoginHandlerReportsAFailureWhenNoSessionTokenCanBeIssued(t *testing.T) {
	fixture := newLoginFixture()
	fixture.tokens.Err = errors.New("signing secret is missing")

	for attempt := 1; attempt <= 3; attempt++ {
		result, err := fixture.attempt(adminCredential)

		if !errors.Is(err, fixture.tokens.Err) {
			t.Fatalf("login attempt %d error = %v, want the issuer's failure", attempt, err)
		}

		if result.Outcome != AdminLoginFailed {
			t.Fatalf("login attempt %d outcome = %q, want a failed login", attempt, result.Outcome)
		}
	}

	result, err := fixture.attempt(adminCredential)
	if err != nil {
		t.Fatalf("login attempt 4: %v", err)
	}

	if result.Outcome != AdminLoginAttemptLimit {
		t.Errorf("fourth attempt outcome = %q, want a failed login to count as an attempt and to clear nothing", result.Outcome)
	}
}

// TestALoginTheServerCouldNotCheckSaysSo is the sign-in a broken configuration makes impossible. The
// credential is never examined, because the credential on record is in the options the server could not read,
// and every sign-in on the deployment fails. The outcome and the error the caller receives are what they were:
// what was missing was any trace that the server had been asked at all.

// TestALoginTheServerCouldNotCheckSaysSo is the sign-in a broken configuration makes impossible. The
// credential is never examined, because the credential on record is in the options the server could not read,
// and every sign-in on the deployment fails. The outcome and the error the caller receives are what they were:
// what was missing was any trace that the server had been asked at all.
func TestALoginTheServerCouldNotCheckSaysSo(t *testing.T) {
	fixture := newLoginFixture()
	fixture.options.Err = errors.New("no such table: options")

	result, err := fixture.attempt(adminCredential)

	if !errors.Is(err, fixture.options.Err) {
		t.Fatalf("login error = %v, want the configuration's own failure", err)
	}

	if result.Outcome != AdminLoginFailed {
		t.Fatalf("outcome = %q, want a login the server could not carry out", result.Outcome)
	}

	assertOneFailureEntry(t, fixture, "the login attempt could not be checked", fixture.options.Err)
}

// TestALoginThatCouldNotStartASessionSaysSo is the other step a sign-in depends on: the credential was right,
// and the server could not mint the token that a session is made of. An operator looking at a log view that
// says nothing cannot tell this apart from a server nobody tried to sign in to.

// TestALoginThatCouldNotStartASessionSaysSo is the other step a sign-in depends on: the credential was right,
// and the server could not mint the token that a session is made of. An operator looking at a log view that
// says nothing cannot tell this apart from a server nobody tried to sign in to.
func TestALoginThatCouldNotStartASessionSaysSo(t *testing.T) {
	fixture := newLoginFixture()
	fixture.tokens.Err = errors.New("signing secret is missing")

	result, err := fixture.attempt(adminCredential)

	if !errors.Is(err, fixture.tokens.Err) {
		t.Fatalf("login error = %v, want the token issuer's own failure", err)
	}

	if result.Outcome != AdminLoginFailed {
		t.Fatalf("outcome = %q, want a login the server could not carry out", result.Outcome)
	}

	assertOneFailureEntry(t, fixture, "the administrator session could not be started", fixture.tokens.Err)
}

// assertOneFailureEntry is what the two failures above have in common: one entry, filed under the logins, at
// the level of a server that cannot let anybody in, naming the failure's own text.

// assertOneFailureEntry is what the two failures above have in common: one entry, filed under the logins, at
// the level of a server that cannot let anybody in, naming the failure's own text.
func assertOneFailureEntry(t *testing.T, fixture *loginFixture, beginsWith string, failure error) {
	t.Helper()

	wrote := fixture.entries()

	if len(wrote) != 1 {
		t.Fatalf("server log = %v, want one entry saying what the server was trying to do", wrote)
	}

	entry := wrote[0]

	if entry.Source != domain.LogSourceLogin {
		t.Errorf("entry source = %q, want %q", entry.Source, domain.LogSourceLogin)
	}

	if entry.Level != domain.LogLevelError {
		t.Errorf("entry level = %q, want an error: every sign-in through this step is failing", entry.Level)
	}

	if !strings.HasPrefix(entry.Message, beginsWith+": ") {
		t.Errorf("entry message = %q, want it to begin %q", entry.Message, beginsWith)
	}

	if !strings.HasSuffix(entry.Message, failure.Error()) {
		t.Errorf("entry message = %q, want it to carry the failure's own text after the sentence", entry.Message)
	}

	if got := entry.DateTime; !got.Equal(loginTime) {
		t.Errorf("entry stamped %v, want the instant the server was asked", got)
	}
}

func TestLoginHandlerSixthSessionRetiresTheFirstToken(t *testing.T) {
	fixture := newLoginFixture()

	first := ""

	for session := 1; session <= 6; session++ {
		token := fixture.mustLogin(t, adminCredential)

		if session == 1 {
			first = token
		}
	}

	if len(fixture.tokens.Window) != 5 {
		t.Fatalf("token window = %v, want the rolling window of the five most recent sessions", fixture.tokens.Window)
	}

	result, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: first})
	if err != nil {
		t.Fatalf("logout of the first session: %v", err)
	}

	if result.Outcome != AdminLogoutRefused {
		t.Errorf("outcome = %q, want the token the rolling window retired to be refused", result.Outcome)
	}
}
