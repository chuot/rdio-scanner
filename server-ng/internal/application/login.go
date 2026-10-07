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

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
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
	// The window is the one the contract describes, per the owner's decision: see
	// docs/specs/FEAT-004-legacy-defect-repairs/decision-login-attempt-window.md. `countAttempt`
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
	// endpoints. server-ng reproduces that, so a client that branches on this flag at login
	// keeps branching the same way.
	PasswordNeedChange bool
}

// AdminLoginCommand asks the server to start an administrator session for the credential
// somebody submitted.
type AdminLoginCommand struct {
	// RemoteAddr is the address the attempt is counted against. The legacy server names the
	// first X-Forwarded-For entry with its port stripped, or the connection's own address
	// with its port stripped; the REST layer resolves that before building the command,
	// because it is the only layer that sees the request.
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
	clock ports.Clock,
) *AdminLoginHandler {
	return &AdminLoginHandler{
		options:   options,
		logs:      logs,
		tokens:    tokens,
		passwords: passwords,
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
			handler.log(ctx, domain.LogLevelWarn, fmt.Sprintf("too many login attempts for ip=\"%v\"", cmd.RemoteAddr))
		}

		return AdminLoginResult{Outcome: AdminLoginAttemptLimit}, nil
	}

	options, err := handler.options.ReadOptions(ctx)
	if err != nil {
		return AdminLoginResult{Outcome: AdminLoginFailed}, err
	}

	// An empty credential is refused here rather than handed to the verifier: the legacy
	// server never compares an empty submission against the stored credential, which
	// matters on a server whose stored credential is itself empty.
	if cmd.Credential == "" || !handler.passwords.Matches(ctx, options.AdminPasswordHash, cmd.Credential) {
		handler.log(ctx, domain.LogLevelWarn, fmt.Sprintf("invalid login attempt for ip %v", cmd.RemoteAddr))

		return AdminLoginResult{Outcome: AdminLoginInvalidCredential}, nil
	}

	token, err := handler.tokens.Issue(ctx)
	if err != nil {
		return AdminLoginResult{Outcome: AdminLoginFailed}, err
	}

	handler.forgetAttempts()

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

	return attempt
}

// forgetAttempts clears every address's attempt history.
//
// That is the legacy behavior, kept deliberately: the legacy server compares each history's age
// against its zero window on the success path only, so any successful login clears the count for
// every address, including one that was locked out. Repairing the window without repairing this
// would leave the documented limit true for an address nobody else signs in for; repairing this one
// too is a separate decision, recorded as a residual risk in
// docs/specs/FEAT-004-legacy-defect-repairs/decision-login-attempt-window.md.
//
// It runs only after a session has been started: a login that failed on the way out leaves
// the counts where they were.
func (handler *AdminLoginHandler) forgetAttempts() {
	handler.mutex.Lock()
	defer handler.mutex.Unlock()

	for remoteAddr := range handler.attempts {
		delete(handler.attempts, remoteAddr)
	}
}

func (handler *AdminLoginHandler) log(ctx context.Context, level, message string) {
	if err := handler.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: handler.clock.Now(),
		Level:    level,
		Message:  message,
	}); err != nil {
		// A log entry that cannot be written must not change the login decision that has
		// already been taken.
		return
	}
}
