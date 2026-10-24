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

package domain

import (
	"crypto/subtle"
	"sort"
	"time"
)

// MaxAccessAuthAttempts is how many times one listener session may present an access
// code before it is locked out.
const MaxAccessAuthAttempts = 5

// Access is a listener's permission to watch a server: code, identity,
// expiration, session limit, display order, and visibility scope.
type Access struct {
	ID         uint64
	Code       string
	Expiration uint64
	Ident      string
	Limit      uint
	Order      uint
	Scope      AccessScope
}

// NewAccess returns the anonymous access a listener session holds before it
// has authenticated: no code, no identity, no scope.
func NewAccess() Access {
	return Access{}
}

// ApplyMap reads an access out of an administrator configuration payload; a
// payload that omits the scope clears the scope.
func (access Access) ApplyMap(m map[string]any) Access {
	if v, ok := m["id"].(float64); ok {
		access.ID = uint64(v)
	}

	if v, ok := m["code"].(string); ok {
		access.Code = v
	}

	if v, ok := m["expiration"].(string); ok {
		if at, err := time.Parse(time.RFC3339, v); err == nil {
			access.Expiration = uint64(at.Unix())
		}
	}

	if v, ok := m["ident"].(string); ok {
		access.Ident = v
	}

	if v, ok := m["limit"].(float64); ok {
		access.Limit = uint(v)
	}

	if v, ok := m["order"].(float64); ok {
		access.Order = uint(v)
	}

	access.Scope, _ = ParseAccessScope(m["systems"])

	return access
}

// Expired reports whether the access has run out; one with no expiration
// never expires.
func (access Access) Expired(now time.Time) bool {
	if access.Expiration == 0 {
		return false
	}

	return time.Unix(int64(access.Expiration), 0).Before(now)
}

// HasScope reports whether the access names what its holder may see. An
// access without a scope is treated as unauthenticated, and the server
// answers such a client with a PIN request instead of live data.
func (access Access) HasScope() bool {
	return access.Scope.Present
}

// Grants reports whether this access may see a given call.
func (access Access) Grants(call Call) bool {
	return access.Scope.Grants(call)
}

// Accesses is the server's access list. A non-empty list is what makes a server
// restricted: an empty one lets any listener in without a code.
type Accesses []Access

// Restricted reports whether the server requires a listener to present an access code.
func (accesses Accesses) Restricted() bool {
	return len(accesses) > 0
}

// ByCode finds the access a listener presented. The comparison is
// constant-time because a code is a credential: a comparison that stops at
// the first wrong character would tell a guessing tool how far each guess got.
func (accesses Accesses) ByCode(code string) (Access, bool) {
	given := []byte(code)

	for _, access := range accesses {
		if subtle.ConstantTimeCompare([]byte(access.Code), given) == 1 {
			return access, true
		}
	}

	return Access{}, false
}

// ByIdent finds an access by the identity it is configured under.
func (accesses Accesses) ByIdent(ident string) (Access, bool) {
	for _, access := range accesses {
		if access.Ident == ident {
			return access, true
		}
	}

	return Access{}, false
}

// UpsertByCode adds an access, or updates every access sharing the same
// code: expiration, identity, limit, and scope are replaced, display order is
// kept. It reports whether the access was new.
func (accesses Accesses) UpsertByCode(access Access) (Accesses, bool) {
	out := append(Accesses{}, accesses...)

	added := true

	for i, existing := range out {
		if existing.Code != access.Code {
			continue
		}

		out[i].Expiration = access.Expiration
		out[i].Ident = access.Ident
		out[i].Limit = access.Limit
		out[i].Scope = access.Scope

		added = false
	}

	if added {
		out = append(out, access)
	}

	return out, added
}

// RemoveAllByIdent removes every access configured under an identity; more
// than one access may share one. It reports whether anything was removed.
func (accesses Accesses) RemoveAllByIdent(ident string) (Accesses, bool) {
	out := Accesses{}

	removed := false

	for _, access := range accesses {
		if access.Ident == ident {
			removed = true

			continue
		}

		out = append(out, access)
	}

	return out, removed
}

// Ordered returns the accesses sorted by their display order.
func (accesses Accesses) Ordered() Accesses {
	out := append(Accesses{}, accesses...)

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}
