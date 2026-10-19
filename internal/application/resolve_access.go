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
	"fmt"
	"sync"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

const (
	// accessAttemptsMax is how many access-code attempts one address is allowed before the server
	// stops looking at what that address presents.
	//
	// It is the administrator login's own bound, chosen deliberately: a stateless surface has no
	// session to count attempts against, so the address is the only thing a guess can be attributed
	// to, and the product already says how many guesses per address per window it is willing to
	// answer. Keeping the two bounds the same is what keeps one rule an operator can state.
	accessAttemptsMax = uint(3)

	// accessAttemptsWindow is the interval an address's attempt count is measured over — the same
	// window the administrator login budget uses, for the same reason.
	accessAttemptsWindow = 10 * time.Minute
)

// ResolveAccessQuery asks what an access credential entitles a caller to.
//
// It is hub-free by construction. The listener socket's own use case drives a socket session: it
// records the attempt against a connected client, grants the access to that client, pushes the
// configuration the access allows down the socket, and asks for a code again when it must. A
// stateless request has none of those things, and a use case that half-worked over HTTP would drag
// the hub into a surface that has no use for it.
type ResolveAccessQuery struct {
	// Address is the address the attempt is counted against. The presentation layer resolves it
	// from the request — the address of the connection, or the forwarding entry when the peer is a
	// proxy the operator named — because it is the only layer that sees the request.
	Address string

	// Code is what the caller presented. An empty code is a caller that presented nothing, and is
	// refused the way a wrong code is.
	Code string
}

// ResolveAccessQueryResult is what the credential entitles the caller to.
//
// The access is the anonymous access when the outcome is anonymous, and the listed access when the
// code resolved. A refusal carries no access: an expired access does not yield a scope, and a
// request from an address that spent its budget never looked at a credential at all.
type ResolveAccessQueryResult struct {
	Outcome domain.AccessResolution
	Access  domain.Access
}

// ResolveAccessQueryHandler decides what a caller that presents an access credential may see.
//
// The order of the checks is the listener's order, because the order is observable: the attempt
// count is examined before the credential is looked at, so a correct code arriving after the budget
// is spent is refused, and the refusal says nothing about the credential it refused.
//
// The attempt counts are the server's own state, held here rather than in a repository, exactly as
// the administrator login holds its own: a restart forgets every address's count, and nothing about
// them is ever visible in the database.
type ResolveAccessQueryHandler struct {
	accesses ports.AccessRepository
	logs     ports.LogRepository
	clock    ports.Clock

	// attempts counts the access-code attempts seen from each address, in the shape the login
	// budget keeps them: a count, the instant of the most recent one, and whether this window has
	// already been warned about.
	attempts map[string]*accessAttempt

	mutex sync.Mutex
}

// accessAttempt is one address's standing budget: how many attempts it has spent in the current
// window, when it last tried, and whether the window has already produced its one warning.
type accessAttempt struct {
	Count  uint
	Date   time.Time
	Warned bool
}

// NewResolveAccessQueryHandler builds the handler from its ports.
func NewResolveAccessQueryHandler(
	accesses ports.AccessRepository,
	logs ports.LogRepository,
	clock ports.Clock,
) *ResolveAccessQueryHandler {
	return &ResolveAccessQueryHandler{
		accesses: accesses,
		logs:     logs,
		clock:    clock,
		attempts: map[string]*accessAttempt{},
	}
}

// Handle resolves one access credential.
func (handler *ResolveAccessQueryHandler) Handle(ctx context.Context, query ResolveAccessQuery) (ResolveAccessQueryResult, error) {
	accesses, err := handler.accesses.AllAccesses(ctx)
	if err != nil {
		return ResolveAccessQueryResult{}, err
	}

	// A server with no access list is not restricted, and the listener socket answers that
	// configuration by handing the caller the anonymous access and never asking for a code. The
	// stateless surface does the same thing, so one configuration means one thing.
	if !accesses.Restricted() {
		return ResolveAccessQueryResult{Outcome: domain.AccessResolvedAnonymous, Access: domain.NewAccess()}, nil
	}

	attempt := handler.historyFor(query.Address)

	// Presenting nothing is not guessing. The listener socket's own prompt costs a session nothing,
	// and each code it presents costs one attempt; a stateless caller that arrives without a
	// credential and then presents its real one is not locked out by its own arrival.
	if query.Code == "" {
		handler.warnOnce(ctx, attempt, query.Address, "a caller from %s presented no access code")

		return ResolveAccessQueryResult{Outcome: domain.AccessRefusedUnknown}, nil
	}

	attempt.Count++
	attempt.Date = handler.clock.Now()

	if attempt.Count > accessAttemptsMax {
		return ResolveAccessQueryResult{Outcome: domain.AccessRefusedBudget}, nil
	}

	access, ok := accesses.ByCode(query.Code)
	if !ok {
		// A code is a credential, and a wrong guess at a credential is not something to keep in a
		// log an operator reads. The warning names the address, once per window: a guessing tool
		// must not fill the log view, and must not learn from the log how far each guess got.
		handler.warnOnce(ctx, attempt, query.Address, "a caller from %s presented an access code that is not valid")

		return ResolveAccessQueryResult{Outcome: domain.AccessRefusedUnknown}, nil
	}

	if access.Expired(handler.clock.Now()) {
		handler.log(ctx, domain.LogLevelWarn, fmt.Sprintf("the access code for ident %s has expired", access.Ident))

		return ResolveAccessQueryResult{Outcome: domain.AccessRefusedExpired}, nil
	}

	// A code that resolved gives its address its budget back. The budget is a limit on trying, not a
	// penalty that outlives the success, and it belongs to the address that spent it: no other
	// address gets anything back.
	handler.forgetAttempt(query.Address)

	return ResolveAccessQueryResult{Outcome: domain.AccessResolvedCode, Access: access}, nil
}

// historyFor is the address's standing budget, aged to the current window: the record is created
// when the address has no history, its count is cleared when its window has passed, and the
// histories of addresses that have been quiet for longer than the window are dropped.
//
// It spends nothing. The caller spends an attempt by incrementing the count it gets back, which is
// what keeps a request that presents no credential from charging the address that made it.
//
// The count is examined before the credential is looked at, which is what makes the bound
// observable in the way the listener socket's bound is: the attempt at the bound is answered on its
// merits, and the one past it is refused whatever it carries.
func (handler *ResolveAccessQueryHandler) historyFor(address string) *accessAttempt {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()

	attempt, ok := handler.attempts[address]
	if !ok {
		attempt = &accessAttempt{}
		handler.attempts[address] = attempt
	}

	if handler.clock.Now().Sub(attempt.Date) > accessAttemptsWindow {
		attempt.Count = 0
		attempt.Warned = false
	}

	// Histories whose window has passed are dropped, as the login budget drops them: a budget
	// measured over a window has nothing to say about an address that has been quiet for longer
	// than the window, and a map that only ever grows is a memory a brute-force tool fills with
	// spoofed keys.
	for key, history := range handler.attempts {
		if key != address && handler.clock.Now().Sub(history.Date) > accessAttemptsWindow {
			delete(handler.attempts, key)
		}
	}

	return attempt
}

// forgetAttempt clears the budget of the address whose credential just resolved.
func (handler *ResolveAccessQueryHandler) forgetAttempt(address string) {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()

	delete(handler.attempts, address)
}

// warnOnce files one warning about an address that is presenting codes the server does not carry,
// and files it only for the first such attempt of its window.
func (handler *ResolveAccessQueryHandler) warnOnce(ctx context.Context, attempt *accessAttempt, address string, message string) {
	handler.mutex.Lock()

	if attempt.Warned {
		handler.mutex.Unlock()

		return
	}

	attempt.Warned = true

	handler.mutex.Unlock()

	handler.log(ctx, domain.LogLevelWarn, fmt.Sprintf(message, address))
}

// log files one line under the access subsystem, the way the listener's access use case files its
// own lines.
func (handler *ResolveAccessQueryHandler) log(ctx context.Context, level string, message string) {
	if handler.logs == nil {
		return
	}

	_ = handler.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: handler.clock.Now(),
		Level:    level,
		Source:   domain.LogSourceAccess,
		Message:  message,
	})
}
