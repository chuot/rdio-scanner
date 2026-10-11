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

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// AuthenticateAccessOutcome reports what the server decided about an access code. Each
// outcome maps to one reply on the listener socket — the access-code prompt, the expiry
// notice, or the connection-limit notice — and the websocket layer (T037) performs that
// mapping; the outcome itself carries no wire detail.
type AuthenticateAccessOutcome string

const (
	// AccessAuthenticated means the listener holds the access now and has been sent the
	// configuration that access allows it to see.
	AccessAuthenticated AuthenticateAccessOutcome = "authenticated"

	// AccessNotRequired means the server has no access list, so the listener was given the
	// anonymous access and its configuration without being asked for a code.
	AccessNotRequired AuthenticateAccessOutcome = "not required"

	// AccessAttemptsExhausted means the session has used up its tries. The session is asked
	// for a code again and nothing else is examined: a valid code arriving after the tries
	// are spent is not accepted.
	AccessAttemptsExhausted AuthenticateAccessOutcome = "attempts exhausted"

	// AccessCodeInvalid means no access carries that code.
	AccessCodeInvalid AuthenticateAccessOutcome = "code invalid"

	// AccessLocked means the code was valid but arrived on the last allowed attempt, so the
	// session is refused anyway. The legacy server behaves this way, and the behavior is
	// preserved rather than smoothed over.
	AccessLocked AuthenticateAccessOutcome = "locked"

	// AccessExpired means the access's expiration date has passed.
	AccessExpired AuthenticateAccessOutcome = "expired"

	// AccessLimitReached means the access already has as many sessions open as it allows.
	AccessLimitReached AuthenticateAccessOutcome = "limit reached"

	// AccessFailed means the server could not complete a step it depends on.
	AccessFailed AuthenticateAccessOutcome = "failed"
)

// AuthenticateAccessResult is the acknowledgement the command returns: what happened. It
// carries no access, no configuration, and no other domain data.
type AuthenticateAccessResult struct {
	Outcome AuthenticateAccessOutcome
}

// AuthenticateAccessCommand asks the server to authenticate one listener session with an
// access code.
//
// The code arrives on the socket base64-encoded. Decoding it is transport work and belongs
// to the websocket layer; the command carries the code the server compares.
type AuthenticateAccessCommand struct {
	ClientID string
	Code     string
}

// AuthenticateAccessHandler decides whether a listener may hold an access.
//
// The order of the checks is the legacy order and it is observable: the attempt counter is
// examined before the code is looked up, so a correct code arriving too late is refused; a
// valid code arriving on the last allowed attempt is refused as well; and expiry is only
// considered once the code is known to be real.
type AuthenticateAccessHandler struct {
	accesses ports.AccessRepository
	systems  ports.SystemRepository
	groups   ports.GroupRepository
	tags     ports.TagRepository
	options  ports.OptionsRepository
	logs     ports.LogRepository
	hub      ports.Hub
	clock    ports.Clock
}

// NewAuthenticateAccessHandler builds the handler from its ports.
func NewAuthenticateAccessHandler(
	accesses ports.AccessRepository,
	systems ports.SystemRepository,
	groups ports.GroupRepository,
	tags ports.TagRepository,
	options ports.OptionsRepository,
	logs ports.LogRepository,
	hub ports.Hub,
	clock ports.Clock,
) *AuthenticateAccessHandler {
	return &AuthenticateAccessHandler{
		accesses: accesses,
		systems:  systems,
		groups:   groups,
		tags:     tags,
		options:  options,
		logs:     logs,
		hub:      hub,
		clock:    clock,
	}
}

// Handle authenticates one listener session.
func (handler *AuthenticateAccessHandler) Handle(ctx context.Context, cmd AuthenticateAccessCommand) (AuthenticateAccessResult, error) {
	attempt := handler.hub.RecordAccessAttempt(cmd.ClientID)
	addressAttempts := handler.hub.RecordAddressAccessAttempt(cmd.ClientID)

	if attempt > domain.MaxAccessAuthAttempts || addressAttempts > domain.MaxAccessAuthAttempts {
		handler.hub.RequestAccessCode(cmd.ClientID)

		return AuthenticateAccessResult{Outcome: AccessAttemptsExhausted}, nil
	}

	accesses, err := handler.accesses.AllAccesses(ctx)
	if err != nil {
		return handler.failed(err)
	}

	// A server with no access list is not restricted: every listener gets the anonymous
	// access and the whole configuration, and is never asked for a code.
	if !accesses.Restricted() {
		handler.hub.GrantAccess(cmd.ClientID, domain.NewAccess())
		handler.hub.ResetAccessAttempts(cmd.ClientID)

		if err := handler.sendConfig(ctx, cmd.ClientID); err != nil {
			return handler.failed(err)
		}

		return AuthenticateAccessResult{Outcome: AccessNotRequired}, nil
	}

	access, ok := accesses.ByCode(cmd.Code)
	if !ok {
		// The code the listener presented is not written down. It is a credential, and a wrong guess at a
		// credential is not something to keep in a log an operator reads. The warning names the address
		// once per window, not once per guess: a guessing tool must not fill the log view.
		if handler.hub.ShouldWarnAccessAttempt(cmd.ClientID) {
			handler.log(ctx, domain.LogLevelWarn, fmt.Sprintf("a listener from %s presented an access code that is not valid",
				handler.hub.RemoteAddrOf(cmd.ClientID)))
		}

		handler.hub.RequestAccessCode(cmd.ClientID)

		return AuthenticateAccessResult{Outcome: AccessCodeInvalid}, nil
	}

	// The access is attached before the remaining checks, as the legacy server does, so a
	// session refused for expiry or for its connection limit is still known by the identity
	// it presented — which is what the disconnect notice and the warnings name.
	handler.hub.GrantAccess(cmd.ClientID, access)

	if attempt == domain.MaxAccessAuthAttempts {
		handler.log(ctx, domain.LogLevelWarn, fmt.Sprintf("the access code for ident %s is locked after too many attempts", access.Ident))

		handler.hub.RequestAccessCode(cmd.ClientID)

		return AuthenticateAccessResult{Outcome: AccessLocked}, nil
	}

	if access.Expired(handler.clock.Now()) {
		handler.log(ctx, domain.LogLevelWarn, fmt.Sprintf("the access code for ident %s has expired", access.Ident))

		handler.hub.ReportAccessExpired(cmd.ClientID)

		return AuthenticateAccessResult{Outcome: AccessExpired}, nil
	}

	if access.Limit > 0 && handler.hub.AccessSessionCount(access.ID) > access.Limit {
		handler.log(ctx, domain.LogLevelWarn, fmt.Sprintf("ident %s is already at its limit of %d listeners, and the connection was refused",
			access.Ident, access.Limit))

		handler.hub.ReportAccessLimit(cmd.ClientID)

		return AuthenticateAccessResult{Outcome: AccessLimitReached}, nil
	}

	// An accepted code clears the session's attempts. A refused one does not, whichever
	// reason refused it, which is what makes the counter a limit on trying rather than a
	// record of the last attempt.
	handler.hub.ResetAccessAttempts(cmd.ClientID)

	if err := handler.sendConfig(ctx, cmd.ClientID); err != nil {
		return handler.failed(err)
	}

	return AuthenticateAccessResult{Outcome: AccessAuthenticated}, nil
}

// sendConfig hands the listener the configuration in effect. What that listener is allowed
// to see is decided by the hub and the payload mapper, not here: this only says what is now
// in effect.
func (handler *AuthenticateAccessHandler) sendConfig(ctx context.Context, clientID string) error {
	systems, err := handler.systems.AllSystems(ctx)
	if err != nil {
		return err
	}

	groups, err := handler.groups.AllGroups(ctx)
	if err != nil {
		return err
	}

	tags, err := handler.tags.AllTags(ctx)
	if err != nil {
		return err
	}

	options, err := handler.options.ReadOptions(ctx)
	if err != nil {
		return err
	}

	handler.hub.SendConfig(clientID, ports.ConfigSnapshot{
		Groups:  groups,
		Tags:    tags,
		Systems: systems,
		Options: options,
	})

	return nil
}

// log writes one entry to the server log, filed under the access subsystem: every line this use case
// writes is about a listener presenting its access code, and the log view filters on that.
func (handler *AuthenticateAccessHandler) log(ctx context.Context, level, message string) {
	if err := handler.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: handler.clock.Now(),
		Level:    level,
		Source:   domain.LogSourceAccess,
		Message:  message,
	}); err != nil {
		return
	}
}

func (handler *AuthenticateAccessHandler) failed(err error) (AuthenticateAccessResult, error) {
	return AuthenticateAccessResult{Outcome: AccessFailed}, err
}

// AccessGate decides whether a listener may send a command at all.
//
// On a restricted server, a session that has not presented a real access is asked for its
// access code instead of having its command processed. The version request is the one
// exception, because a client needs the server's version before it can authenticate.
type AccessGate struct {
	accesses ports.AccessRepository
	hub      ports.Hub
}

// NewAccessGate builds the gate from its ports.
func NewAccessGate(accesses ports.AccessRepository, hub ports.Hub) *AccessGate {
	return &AccessGate{accesses: accesses, hub: hub}
}

// RequiresAccessCode reports whether the server is restricted and this session still holds
// nothing but the anonymous access.
//
// The test is whether the session's access names any systems, which is how the legacy
// server recognises an unauthenticated session. It has a consequence worth naming: an
// access configured *without* systems stays gated for every command except the access code
// even after it authenticates, because the gate cannot tell that session apart from an
// anonymous one. That is preserved legacy behavior, recorded in
// decision-preserved-quirks.md, and pinned by a test rather than quietly repaired.
func (gate *AccessGate) RequiresAccessCode(ctx context.Context, clientID string) (bool, error) {
	accesses, err := gate.accesses.AllAccesses(ctx)
	if err != nil {
		return false, err
	}

	if !accesses.Restricted() {
		return false, nil
	}

	access, ok := gate.hub.AccessOf(clientID)
	if !ok {
		return true, nil
	}

	return !access.HasScope(), nil
}
