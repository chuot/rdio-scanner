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

package backend

import (
	"context"
	"net/http"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

// accessCodeHeader carries a listener access code to this surface: one
// credential, one header, across every stateless entry point.
const accessCodeHeader = "X-Access-Code"

// Tier is what a caller is entitled to do through this surface: the two
// tiers the product has, the listener and the administrator. An
// assistant is not a third kind of user and is handed no credential of
// its own; see the credential-gate-decides-the-tier-before-the-protocol maintainer note.
type Tier uint8

const (
	// TierReadOnly may read what its credential grants and change nothing. A
	// listener access code is this tier, and so is the anonymous caller of a
	// server that asks nobody for a code.
	TierReadOnly Tier = iota

	// TierFull is the administrator's tier: read, and write. Only the
	// credential the administrator's own page presents reaches it.
	TierFull
)

// Caller is who reached the surface: the tier their credential opened and,
// for a listener, the identity and scope it is configured with, which
// capabilities answer with; an administrator's credential is unscoped.
// see the credential-gate-decides-the-tier-before-the-protocol maintainer note.
type Caller struct {
	Tier  Tier
	Ident string
	Scope domain.AccessScope

	// Scoped says whether the scope above restricts what this caller may see.
	// An access code that names no systems, and the anonymous caller of a
	// server that asks nobody for one, see everything their tier may.
	Scoped bool
}

// callerKey is the request-context slot the gate leaves the caller in. It is
// unexported: only CallerOf reads it.
type callerKey struct{}

// CallerOf is the caller the gate decided this request carries, and whether
// there was one at all. A capability reached with no caller is mounted
// outside the gate and must stop rather than answer as an administrator.
func CallerOf(ctx context.Context) (Caller, bool) {
	who, ok := ctx.Value(callerKey{}).(Caller)
	return who, ok
}

// Gate is the rule that decides who may reach the capabilities, and the
// tier they reach them as: the same rule the call history endpoints
// apply, in the same order, answered by the same empty 401; see the
// credential-gate-decides-the-tier-before-the-protocol maintainer note.
func (s *Server) Gate() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			who, ok := s.who(r)
			if !ok {
				// A refused caller is the event an operator wants to see; the address
				// is the story, and what the caller presented is a credential.
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
