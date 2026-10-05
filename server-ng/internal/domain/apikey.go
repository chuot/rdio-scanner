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

package domain

import "sort"

// Apikey is the credential an ingest source presents when it uploads calls to this
// server, and the scope of systems and talkgroups it is allowed to upload to.
type Apikey struct {
	ID       uint64
	Disabled bool
	Ident    string
	Key      string
	Order    uint
	Scope    AccessScope
}

// NewApikey returns an apikey that may upload to anything, which is what a freshly
// created one is in the legacy server.
func NewApikey() Apikey {
	return Apikey{Scope: AccessScope{Present: true, Wildcard: true}}
}

// ApplyMap reads an apikey out of an administrator configuration payload. As with an
// access, the scope is assigned unconditionally: a payload that omits it clears it.
func (apikey Apikey) ApplyMap(m map[string]any) Apikey {
	if v, ok := m["id"].(float64); ok {
		apikey.ID = uint64(v)
	}

	if v, ok := m["disabled"].(bool); ok {
		apikey.Disabled = v
	}

	if v, ok := m["ident"].(string); ok {
		apikey.Ident = v
	}

	if v, ok := m["key"].(string); ok {
		apikey.Key = v
	}

	if v, ok := m["order"].(float64); ok {
		apikey.Order = uint(v)
	}

	apikey.Scope, _ = ParseAccessScope(m["systems"])

	return apikey
}

// Accepts reports whether this apikey may upload a given call. A disabled apikey may
// upload nothing.
func (apikey Apikey) Accepts(call Call) bool {
	if apikey.Disabled {
		return false
	}

	return apikey.Scope.Grants(call)
}

// Apikeys is the server's upload credential list.
type Apikeys []Apikey

// ByKey finds the apikey an ingest source presented. A disabled credential is still
// found, so the caller can report why it was refused.
func (apikeys Apikeys) ByKey(key string) (Apikey, bool) {
	for _, apikey := range apikeys {
		if apikey.Key == key {
			return apikey, true
		}
	}

	return Apikey{}, false
}

// ByIdent finds an apikey by the identity it is configured under.
func (apikeys Apikeys) ByIdent(ident string) (Apikey, bool) {
	for _, apikey := range apikeys {
		if apikey.Ident == ident {
			return apikey, true
		}
	}

	return Apikey{}, false
}

// Ordered returns the apikeys sorted by their display order.
func (apikeys Apikeys) Ordered() Apikeys {
	out := append(Apikeys{}, apikeys...)

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}
