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

package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

// ResolveAccessQueryHandler is the stateless half of the listener access rule: what an access
// credential entitles a caller to, asked by an HTTP request rather than by a socket session. The
// socket's own use case keeps its hub, its prompts, and its config push; this one asks the same
// domain questions and answers nothing about connections.

var resolveAccessTime = time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)

const resolveAccessCode = "753159"

func newResolveAccessFixture(t *testing.T, accesses domain.Accesses) (*ResolveAccessQueryHandler, *fakes.Accesses, *fakes.Logs, *fakes.Clock) {
	t.Helper()

	stored := &fakes.Accesses{Stored: accesses}
	logs := &fakes.Logs{}
	clock := fakes.NewClock(resolveAccessTime)

	return NewResolveAccessQueryHandler(stored, logs, clock), stored, logs, clock
}

func resolveAccessList(scope domain.AccessScope) domain.Accesses {
	return domain.Accesses{{ID: 1, Code: resolveAccessCode, Ident: "dispatch-desk", Scope: scope}}
}

func TestResolveAccessAnswersTheAnonymousAccessWhenTheServerIsNotRestricted(t *testing.T) {
	handler, _, _, _ := newResolveAccessFixture(t, domain.Accesses{})

	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessResolvedAnonymous {
		t.Errorf("outcome = %v, want %v: a server with no access list opens its history to an anonymous "+
			"caller, as the listener socket does", result.Outcome, domain.AccessResolvedAnonymous)
	}

	if result.Access.HasScope() {
		t.Errorf("the anonymous access names a scope, want the whole history: %v", result.Access.Scope)
	}
}

func TestResolveAccessResolvesAListedUnexpiredCode(t *testing.T) {
	handler, _, _, _ := newResolveAccessFixture(t, resolveAccessList(scopedResolveTo(10001)))

	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: resolveAccessCode})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessResolvedCode {
		t.Fatalf("outcome = %v, want %v", result.Outcome, domain.AccessResolvedCode)
	}

	if result.Access.Ident != "dispatch-desk" {
		t.Errorf("the access resolved is %q, want the access that carries the code", result.Access.Ident)
	}

	if !result.Access.Grants(resolveAccessCall(10001, 1514000)) {
		t.Errorf("the resolved access does not grant the system it names: %v", result.Access.Scope)
	}

	if result.Access.Grants(resolveAccessCall(10002, 1515000)) {
		t.Errorf("the resolved access grants a system it does not name: %v", result.Access.Scope)
	}
}

func TestResolveAccessRefusesACodeNoAccessCarries(t *testing.T) {
	handler, _, _, _ := newResolveAccessFixture(t, resolveAccessList(wildcardResolveScope()))

	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: "000000"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessRefusedUnknown {
		t.Errorf("outcome = %v, want %v", result.Outcome, domain.AccessRefusedUnknown)
	}
}

// A caller that presents nothing on a restricted server is refused the same way a wrong code is:
// the server does not explain the difference, and the difference is not something a caller is
// entitled to know.
func TestResolveAccessRefusesACallerThatPresentsNoCode(t *testing.T) {
	handler, _, _, _ := newResolveAccessFixture(t, resolveAccessList(wildcardResolveScope()))

	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessRefusedUnknown {
		t.Errorf("outcome = %v, want %v", result.Outcome, domain.AccessRefusedUnknown)
	}
}

func TestResolveAccessRefusesAnExpiredAccess(t *testing.T) {
	accesses := resolveAccessList(wildcardResolveScope())
	accesses[0].Expiration = uint64(resolveAccessTime.Add(-time.Hour).Unix())

	handler, _, _, _ := newResolveAccessFixture(t, accesses)

	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: resolveAccessCode})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessRefusedExpired {
		t.Errorf("outcome = %v, want %v", result.Outcome, domain.AccessRefusedExpired)
	}

	if result.Access.HasScope() {
		t.Errorf("an expired access still yielded a scope: %v", result.Access.Scope)
	}
}

// An access whose date has not passed is not refused for having a date at all.
func TestResolveAccessAcceptsAnAccessWhoseDateHasNotPassed(t *testing.T) {
	accesses := resolveAccessList(wildcardResolveScope())
	accesses[0].Expiration = uint64(resolveAccessTime.Add(24 * time.Hour).Unix())

	handler, _, _, _ := newResolveAccessFixture(t, accesses)

	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: resolveAccessCode})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessResolvedCode {
		t.Errorf("outcome = %v, want %v", result.Outcome, domain.AccessResolvedCode)
	}
}

// The budget bounds guessing, so it is spent by the codes a caller presents, not by the requests
// that present nothing. A listener that arrives with no credential and then presents its real code
// is not locked out by its own arrival, which is what the listener socket's own prompt does not do
// either: the prompt is free, and each code presented costs an attempt.
func TestResolveAccessDoesNotChargeAnAddressForPresentingNoCode(t *testing.T) {
	handler, _, _, _ := newResolveAccessFixture(t, resolveAccessList(wildcardResolveScope()))

	for i := uint(0); i < accessAttemptsMax+1; i++ {
		result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7"})
		if err != nil {
			t.Fatalf("Handle: %v", err)
		}

		if result.Outcome != domain.AccessRefusedUnknown {
			t.Fatalf("a caller presenting nothing = %v, want %v", result.Outcome, domain.AccessRefusedUnknown)
		}
	}

	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: resolveAccessCode})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessResolvedCode {
		t.Errorf("outcome = %v, want %v: presenting nothing is not guessing, and a caller is not "+
			"locked out by its own arrival", result.Outcome, domain.AccessResolvedCode)
	}
}

// Guessing an access code over a stateless surface is bounded per address, on the model of the
// administrator login budget: the attempt at the bound is answered on its merits, and the one past
// it is refused whatever it carries — the credential is never looked at.
func TestResolveAccessRefusesWhatArrivesAfterTheAddressSpentItsBudget(t *testing.T) {
	handler, _, _, _ := newResolveAccessFixture(t, resolveAccessList(wildcardResolveScope()))

	for i := uint(0); i < accessAttemptsMax; i++ {
		result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: "000000"})
		if err != nil {
			t.Fatalf("Handle: %v", err)
		}

		if result.Outcome != domain.AccessRefusedUnknown {
			t.Fatalf("attempt %d = %v, want %v: the attempts up to the bound are answered on their merits",
				i+1, result.Outcome, domain.AccessRefusedUnknown)
		}
	}

	// The correct code, arriving after the budget is spent, is refused.
	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: resolveAccessCode})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessRefusedBudget {
		t.Errorf("outcome = %v, want %v: a correct code arriving after the budget is spent is refused, "+
			"and the credential was never looked at", result.Outcome, domain.AccessRefusedBudget)
	}
}

// The budget belongs to the address that spent it. A second address is not refused for what the
// first one did.
func TestResolveAccessKeysTheBudgetToTheAddressThatSpentIt(t *testing.T) {
	handler, _, _, _ := newResolveAccessFixture(t, resolveAccessList(wildcardResolveScope()))

	for i := uint(0); i <= accessAttemptsMax; i++ {
		if _, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: "000000"}); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}

	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "198.51.100.4", Code: resolveAccessCode})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessResolvedCode {
		t.Errorf("outcome = %v, want %v: a quiet address is not refused for what another address spent",
			result.Outcome, domain.AccessResolvedCode)
	}
}

// A code that resolves gives its address its budget back: the budget is a limit on trying, not a
// punishment that outlives the success.
//
// The resolving attempt is the last one the budget answers on its merits. The bound is examined
// before the credential is looked at, so an attempt past the bound is refused whatever it carries —
// which is what the budget's own bound test asserts.
func TestResolveAccessClearsTheBudgetOfTheAddressThatResolved(t *testing.T) {
	handler, _, _, _ := newResolveAccessFixture(t, resolveAccessList(wildcardResolveScope()))

	for i := uint(0); i < accessAttemptsMax-1; i++ {
		if _, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: "000000"}); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}

	if _, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: resolveAccessCode}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// The budget is spent by the attempts that came before it, so the address is back at the start
	// of its window: this attempt is answered on its merits, and not refused for the past.
	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: "000000"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessRefusedUnknown {
		t.Errorf("outcome = %v, want %v: a resolved code clears the attempts that came before it",
			result.Outcome, domain.AccessRefusedUnknown)
	}
}

// The budget is measured over a window. An address that has been quiet for longer than the window
// has its budget back, which is what makes "three attempts per window" mean what it says.
func TestResolveAccessAgesTheBudgetOutOverItsWindow(t *testing.T) {
	handler, _, _, clock := newResolveAccessFixture(t, resolveAccessList(wildcardResolveScope()))

	for i := uint(0); i <= accessAttemptsMax; i++ {
		if _, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: "000000"}); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}

	clock.At = clock.At.Add(accessAttemptsWindow + time.Second)

	result, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: resolveAccessCode})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != domain.AccessResolvedCode {
		t.Errorf("outcome = %v, want %v: the window passed, so the address has its budget back",
			result.Outcome, domain.AccessResolvedCode)
	}
}

// A code is a credential, and a wrong guess at a credential is not something to keep in a log an
// operator reads. The refusals name the address, once per window, and never the code.
func TestResolveAccessNeverFilesTheCodeItWasGiven(t *testing.T) {
	handler, _, logs, _ := newResolveAccessFixture(t, resolveAccessList(wildcardResolveScope()))

	for _, code := range []string{"000000", "000001", resolveAccessCode + "x", resolveAccessCode} {
		if _, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: code}); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}

	for _, entry := range logs.Recorded() {
		if strings.Contains(entry.Message, resolveAccessCode) {
			t.Errorf("the log files a credential: %q", entry.Message)
		}
	}
}

func TestResolveAccessWarnsOncePerWindowAboutAnAddressThatIsGuessing(t *testing.T) {
	handler, _, logs, _ := newResolveAccessFixture(t, resolveAccessList(wildcardResolveScope()))

	for i := uint(0); i < accessAttemptsMax; i++ {
		if _, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: "000000"}); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}

	warnings := 0

	for _, entry := range logs.Recorded() {
		if entry.Level == domain.LogLevelWarn {
			warnings++
		}
	}

	if warnings != 1 {
		t.Errorf("the log carries %d warnings about a guessing address, want exactly one per window: a "+
			"guessing tool must not fill the log view", warnings)
	}
}

// The server's own step failed: the access list could not be read. That is a failure, not a
// refusal, and it is not dressed up as one.
func TestResolveAccessReportsAFailureWhenTheAccessListCannotBeRead(t *testing.T) {
	stored := &fakes.Accesses{Err: errResolveAccess}
	clock := fakes.NewClock(resolveAccessTime)

	handler := NewResolveAccessQueryHandler(stored, &fakes.Logs{}, clock)

	if _, err := handler.Handle(context.Background(), ResolveAccessQuery{Address: "203.0.113.7", Code: resolveAccessCode}); err == nil {
		t.Errorf("Handle reported no failure, want the failure the server could not complete")
	}
}

func wildcardResolveScope() domain.AccessScope {
	return domain.AccessScope{Present: true, Wildcard: true}
}

func scopedResolveTo(systemRefs ...uint) domain.AccessScope {
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

func resolveAccessCall(systemRef, talkgroupRef uint) domain.Call {
	call := domain.NewCall()

	call.System = domain.System{SystemRef: systemRef}
	call.Talkgroup = domain.Talkgroup{TalkgroupRef: talkgroupRef}

	return call
}

var errResolveAccess = resolveAccessError{}

type resolveAccessError struct{}

func (resolveAccessError) Error() string { return "the access list cannot be read" }
