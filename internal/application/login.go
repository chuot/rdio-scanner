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
	"fmt"
	"sync"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

const (
	// loginAttemptsMax is how many login attempts one address is allowed before the server
	// stops looking at what that address submits.
	//
	// The count is what the legacy server keeps, and it is counted before the credential
	// is examined, so the boundary is observable: the attempt at this number is still
	// answered on its merits, and the one past it is refused whatever credential it carries.
	loginAttemptsMax = uint(3)

	// loginAttemptsWindow is the interval an address's attempt count is measured over.
	//
	// The legacy server meant ten minutes here and enforced zero: it built the window with
	// `time.Duration(time.Duration.Minutes(10))`, a method expression that takes the 10 as a
	// Duration — ten nanoseconds — converts it to minutes and back, and truncates to zero. The
	// consequence was that an address's count never aged out, so an address that had spent its
	// budget stayed refused until a successful login or a restart.
	//
	// The window is the one the contract describes, per the owner's decision. `countAttempt`
	// ages a history out over it, which is what makes "three attempts per window" mean what it
	// says.
	loginAttemptsWindow = 10 * time.Minute
)

// AdminLoginOutcome reports what the server decided about one attempt to start an
// administrator session. The REST layer maps it onto the status codes the frozen contract
// fixes; the outcome itself carries no wire detail.
type AdminLoginOutcome string

const (
	// AdminLoginAccepted means the credential matched and a session token was issued. The
	// login endpoint answers with the token and the password-change flag.
	AdminLoginAccepted AdminLoginOutcome = "accepted"

	// AdminLoginInvalidCredential means the credential was looked at and did not match.
	// The server says so in its own log and answers the caller with an empty 401, because
	// the login form shows one generic failure whatever went wrong.
	AdminLoginInvalidCredential AdminLoginOutcome = "invalid-credential"

	// AdminLoginAttemptLimit means the address has spent its attempt budget and the
	// credential was never looked at. This is the silent 401 of the frozen contract: no
	// body, and no hint of how many attempts are left or when the next one is allowed.
	AdminLoginAttemptLimit AdminLoginOutcome = "attempt-limit"

	// AdminLoginFailed means the server could not complete a step the login depends on.
	// The legacy login endpoint answers this with 417.
	AdminLoginFailed AdminLoginOutcome = "failed"
)

// AdminLoginResult is the acknowledgement the login endpoint gets: what happened, the
// session token when there is one, and the flag the login response carries. It carries no
// domain data.
type AdminLoginResult struct {
	Outcome AdminLoginOutcome

	// Token is the newly issued administrator session token, empty when no session was
	// started. It is an opaque credential: the application neither builds nor reads it.
	Token string

	// PasswordNeedChange is reported to the caller so the administrator can be nudged to
	// replace the initial password.
	//
	// The legacy login response reports it as true on every successful login, whatever the
	// stored option says; the option itself is only ever reported by the configuration
	// endpoints. This server reproduces that, so a client that branches on this flag at login
	// keeps branching the same way.
	PasswordNeedChange bool
}

// AdminLoginCommand asks the server to start an administrator session for the credential
// somebody submitted.
type AdminLoginCommand struct {
	// RemoteAddr is the address the attempt is counted against. The REST layer resolves it from the
	// request — the address of the connection, or the forwarding entry when the peer is a proxy the
	// operator named — because it is the only layer that sees the request.
	RemoteAddr string

	// Credential is what the administrator submitted. A payload without a password, or one
	// whose password is not text, arrives here as the empty string: the legacy server still
	// counts that as an attempt and still refuses it.
	//
	// A body that is not readable JSON never reaches this command. The legacy login endpoint
	// answers it with 400 before it counts anything, so a malformed request does not spend
	// the address's budget, and the REST layer must keep that order.
	Credential string
}

// AdminLoginHandler decides whether an administrator may start a session.
//
// The attempt counts are the server's own session-state, held here rather than in a
// repository: the legacy server holds them in process memory too, so a restart forgets
// every address's count, and nothing about them is ever visible in the database.
type AdminLoginHandler struct {
	options   ports.OptionsRepository
	logs      ports.LogRepository
	tokens    ports.TokenIssuer
	passwords ports.PasswordVerifier
	hashes    ports.PasswordHasher
	upgrader  ports.PasswordUpgrader
	clock     ports.Clock

	// attempts counts the login attempts seen from each address, in the shape the legacy
	// server keeps them: a count and the instant of the most recent one.
	attempts map[string]*loginAttempt

	// mutex guards attempts, because the HTTP layer drives this handler from more than one
	// connection at a time.
	mutex sync.Mutex
}

// loginAttempt is one address's login history: how many attempts have been counted and
// when the most recent one was made.
type loginAttempt struct {
	Count uint
	Date  time.Time
}

// NewAdminLoginHandler builds the handler from its ports.
//
// The credential check and the token are both ports: the hashing and the signing belong to
// the adapters, and what the application owns is the decision — how many attempts an
// address gets, and whether the credential on offer is the one on record.
func NewAdminLoginHandler(
	options ports.OptionsRepository,
	logs ports.LogRepository,
	tokens ports.TokenIssuer,
	passwords ports.PasswordVerifier,
	hashes ports.PasswordHasher,
	upgrader ports.PasswordUpgrader,
	clock ports.Clock,
) *AdminLoginHandler {
	return &AdminLoginHandler{
		options:   options,
		logs:      logs,
		tokens:    tokens,
		passwords: passwords,
		hashes:    hashes,
		upgrader:  upgrader,
		clock:     clock,
		attempts:  map[string]*loginAttempt{},
	}
}

// Handle counts the attempt, refuses it if the address has spent its budget, and otherwise
// starts a session for the right credential.
//
// The order is the order the legacy server performs the steps in, and it is observable:
// the attempt is counted before anything is checked, so a refused credential and a malformed
// one both consume the budget; an address that has spent its budget is refused before its
// credential is examined, which is what makes the refusal silent; and the counts are only
// cleared once a session has actually been started.
func (handler *AdminLoginHandler) Handle(ctx context.Context, cmd AdminLoginCommand) (AdminLoginResult, error) {
	attempt := handler.countAttempt(cmd.RemoteAddr)

	if attempt.Count > loginAttemptsMax {
		// The warning is written once, on the attempt that crosses the limit, and never
		// again for the same address: the legacy server counts it the same way, so a
		// log view shows one entry per lockout rather than one per refused request. A
		// lockout in a new window is a new lockout, and gets its own warning.
		if attempt.Count == loginAttemptsMax+1 {
			handler.log(ctx, domain.LogLevelWarn, fmt.Sprintf("too many login attempts from %v", cmd.RemoteAddr))
		}

		return AdminLoginResult{Outcome: AdminLoginAttemptLimit}, nil
	}

	options, err := handler.options.ReadOptions(ctx)
	if err != nil {
		handler.reportFailure(ctx, "the login attempt could not be checked", err)

		return AdminLoginResult{Outcome: AdminLoginFailed}, err
	}

	// An empty credential is refused here rather than handed to the verifier: the legacy
	// server never compares an empty submission against the stored credential, which
	// matters on a server whose stored credential is itself empty.
	if cmd.Credential == "" || !handler.passwords.Matches(ctx, options.AdminPasswordHash, cmd.Credential) {
		handler.log(ctx, domain.LogLevelWarn, fmt.Sprintf("a login attempt from %v was refused", cmd.RemoteAddr))

		return AdminLoginResult{Outcome: AdminLoginInvalidCredential}, nil
	}

	// The credential is proven, so the stored hash is renewed at the cost the
	// deployment is configured for: a deployment that raised its
	// bcrypt cost renews a stored hash the next time the credential is proven,
	// and the stored form stops being the cheap one. The renewal is not the
	// login — a login whose renewal failed is still a login, and the failure is
	// said in the server log.
	if handler.upgrader.NeedsUpgrade(options.AdminPasswordHash) {
		hash, err := handler.hashes.Hash(ctx, cmd.Credential)
		if err == nil {
			options.AdminPasswordHash = hash

			err = handler.options.WriteOptions(ctx, options)
		}
		if err != nil {
			handler.log(ctx, domain.LogLevelWarn, "the administrator credential could not be re-hashed at the configured cost")
		}
	}

	token, err := handler.tokens.Issue(ctx)
	if err != nil {
		handler.reportFailure(ctx, "the administrator session could not be started", err)

		return AdminLoginResult{Outcome: AdminLoginFailed}, err
	}

	handler.forgetAttempt(cmd.RemoteAddr)

	return AdminLoginResult{
		Outcome: AdminLoginAccepted,
		Token:   token,

		// Always true, as the legacy login response has it. See AdminLoginResult.
		PasswordNeedChange: true,
	}, nil
}

// countAttempt records that an address made one more attempt and returns its history.
//
// The instant is refreshed on every attempt, including the refused ones, which is what the legacy
// server does. What the legacy server never did was let a history age: its window was zero, so a
// count that had been spent stayed spent until a successful login or a restart. Here a history whose
// most recent attempt is older than the window starts over, because a budget measured over a window
// is a budget the window renews.
func (handler *AdminLoginHandler) countAttempt(remoteAddr string) *loginAttempt {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()

	attempt, ok := handler.attempts[remoteAddr]
	if !ok {
		attempt = &loginAttempt{}
		handler.attempts[remoteAddr] = attempt
	}

	if handler.clock.Now().Sub(attempt.Date) > loginAttemptsWindow {
		attempt.Count = 0
	}

	attempt.Count++
	attempt.Date = handler.clock.Now()

	// Histories whose window has passed are dropped. A budget measured over a window has nothing to
	// say about an address that has been quiet for longer than the window, and a map that only ever
	// grows is a memory an attacker fills with the spoofed keys of a brute-force tool: the entries
	// that can no longer spend a budget must not be kept just to be remembered.
	for address, history := range handler.attempts {
		if address != remoteAddr && handler.clock.Now().Sub(history.Date) > loginAttemptsWindow {
			delete(handler.attempts, address)
		}
	}

	return attempt
}

// forgetAttempt clears the attempt history of the address that just signed in.
//
// The legacy server cleared every address's history on any success, which let one successful login —
// including one an attacker paid for — unlock brute-forcing from every address at once. The decision
// repairs that: the budget belongs to the address that spent it, and only that address gets it back.
//
// It runs only after a session has been started: a login that failed on the way out leaves the
// counts where they were.
func (handler *AdminLoginHandler) forgetAttempt(remoteAddr string) {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()

	delete(handler.attempts, remoteAddr)
}

// reportFailure records a step of a sign-in the server could not carry out, and the failure's own text.
//
// The login decision is already taken by the time this runs: the caller gets the same outcome and the same
// error it always got. What was missing was any trace that the server had been asked — a deployment whose
// options table is broken refuses every sign-in and, until this, left no evidence of ever having been
// approached.
func (handler *AdminLoginHandler) reportFailure(ctx context.Context, what string, reason error) {
	appendServerLog(ctx, handler.logs, handler.clock, domain.LogLevelError, domain.LogSourceLogin,
		fmt.Sprintf("%s: %v", what, reason))
}

func (handler *AdminLoginHandler) log(ctx context.Context, level, message string) {
	if err := handler.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: handler.clock.Now(),
		Level:    level,
		Source:   domain.LogSourceLogin,
		Message:  message,
	}); err != nil {
		// A log entry that cannot be written must not change the login decision that has
		// already been taken.
		return
	}
}
