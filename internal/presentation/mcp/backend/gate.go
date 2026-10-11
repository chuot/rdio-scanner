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
	"context"
	"net/http"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

// accessCodeHeader carries a listener access code to this surface. It is the header the call
// history endpoints read the same credential from: one credential, one header, across every
// stateless entry point.
const accessCodeHeader = "X-Access-Code"

// Tier is what a caller is entitled to do through this surface.
//
// The two tiers are the two the product has, not two invented for the protocol: the listener whose
// access code opens the history, and the administrator who logs in. An assistant is not a third kind
// of user, and it is handed no credential of its own.
type Tier uint8

const (
	// TierReadOnly is a caller who may read what their credential grants and change nothing. A
	// listener access code is this tier, and so is the anonymous caller of a server that asks
	// nobody for a code: reading is what a listener does, and an assistant is a listener.
	TierReadOnly Tier = iota

	// TierFull is the administrator's tier: read, and write. It is reached only by the credential
	// the administrator's own page presents, because writing is what the administrator's password
	// gates, and nothing a tool can do loosens that.
	TierFull
)

// Caller is who reached the surface: the tier their credential opened, and, for a listener, the
// identity and the scope that credential is configured with.
//
// The scope is carried because the capabilities answer with it: a listener's search is restricted
// by it, and a listener's read of one call is refused by it before the call is named. The
// administrator's credential is not scoped by the listener access list, so it carries none.
type Caller struct {
	Tier  Tier
	Ident string
	Scope domain.AccessScope

	// Scoped says whether the scope above restricts what this caller may see at all. An access
	// code that names no systems — and the anonymous caller of a server that asks nobody for a
	// code — sees everything its tier may see, and checking a scope that names nothing against a
	// call is how a whole history quietly disappears.
	Scoped bool
}

// callerKey is the request-context slot the gate leaves the caller in. It is unexported: the
// capabilities read the caller with CallerOf, and nothing outside this package can put one there.
type callerKey struct{}

// CallerOf is the caller the gate decided this request carries, and whether there was one at all.
// A capability reached with no caller is a capability mounted outside the gate, and it must stop
// rather than answer as an administrator.
func CallerOf(ctx context.Context) (Caller, bool) {
	who, ok := ctx.Value(callerKey{}).(Caller)
	return who, ok
}

// Gate is the surface's credential gate: the rule that decides who may reach the capabilities, and
// the tier they reach them as.
//
// It is the same rule the call history endpoints apply, in the same order, for the same reason: the
// administrator's credential is examined first, because an administrator is not scoped by a listener
// access list and must not be refused by a stale access-code header. A refused request is answered
// with the empty 401 every other entry point of this server gives, and the protocol handler is never
// reached, so a refusal says nothing about what the surface can do.
func (s *Server) Gate() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			who, ok := s.who(r)
			if !ok {
				// A caller the gate refuses is the event an operator wants to see: somebody knocking
				// on the assistant's endpoint with nothing this server accepts. The address is the
				// story. What the caller presented is a credential, and a credential is not what a
				// log view is for.
				s.log.Warn("the surface refused a caller", "address", httpaddr.Address(r, s.trusted))

				w.WriteHeader(http.StatusUnauthorized)

				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, who)))
		})
	}
}

// who decides who is asking, and refuses a caller whose credential the server does not accept.
func (s *Server) who(r *http.Request) (Caller, bool) {
	if s.session.Handle(r.Context(), application.CheckAdminSessionQuery{Token: r.Header.Get("Authorization")}) {
		return Caller{Tier: TierFull}, true
	}

	result, err := s.resolve.Handle(r.Context(), application.ResolveAccessQuery{
		Address: httpaddr.Address(r, s.trusted),
		Code:    r.Header.Get(accessCodeHeader),
	})
	if err != nil || result.Outcome.Refused() {
		return Caller{}, false
	}

	// A listener's own code is scoped by what it grants. The anonymous access is not: it is the
	// whole of what a server that asks nobody for a code lets anybody see.
	if result.Outcome == domain.AccessResolvedCode {
		return Caller{
			Tier:   TierReadOnly,
			Ident:  result.Access.Ident,
			Scope:  result.Access.Scope,
			Scoped: result.Access.Scope.Present,
		}, true
	}

	return Caller{Tier: TierReadOnly}, true
}
