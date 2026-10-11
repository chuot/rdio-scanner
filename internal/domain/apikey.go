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

import (
	"fmt"
	"sort"
	"strings"
)

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

// AcceptsUpload reports whether the credential an uploader presented covers the call it posted.
//
// It is the reference-boundary twin of Accepts, and the difference is the whole point of it. A call is
// matched against a system and a talkgroup the server has resolved, and at the upload boundary nothing
// about the call has been resolved yet — the server has only the references the upload itself names. The
// legacy server compares the resolved pair here anyway, which means a credential scoped to particular
// systems dereferences a system that does not exist yet and the connection ends with no reply at all
// (Q51, and the fix the owner approved on Q20). Comparing the submitted references is what that
// comparison was written to do.
func (apikeys Apikeys) AcceptsUpload(key string, call Call) bool {
	apikey, ok := apikeys.ByKey(key)
	if !ok || apikey.Disabled {
		return false
	}

	return apikey.Scope.GrantsReferences(call.Meta.SystemRef, call.Meta.TalkgroupRef)
}

// Apikeys is the server's upload credential list.
type Apikeys []Apikey

// Validate refuses a credential list that contains a row with no key. A row with no key matches
// every keyless upload, which turns an empty credential field on the administrator's page into an
// open door; the rule belongs to the list itself, so every path that takes a submitted list runs
// into it (decision D3).
func (apikeys Apikeys) Validate() error {
	for _, apikey := range apikeys {
		if len(strings.TrimSpace(apikey.Key)) == 0 {
			return fmt.Errorf("the apikey %q has no key to present", apikey.Ident)
		}
	}

	return nil
}

// ByKey finds the apikey an ingest source presented. A disabled credential is still
// found, so the caller can report why it was refused.
//
// A presentation of nothing matches nothing, even a keyless row an older server left in the list:
// the empty key is not a credential, and refusing it here is the second door behind the
// apply-time rule in Validate.
func (apikeys Apikeys) ByKey(key string) (Apikey, bool) {
	if len(key) == 0 {
		return Apikey{}, false
	}

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
