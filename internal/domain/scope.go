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

// AccessScope limits a listener access or an upload apikey to particular systems and
// talkgroups. It is shared by both because the server scopes them identically.
//
// Three states are distinct and all three matter:
//
//	present=false  no scope was supplied at all; the PIN gate treats the client as
//	               unauthenticated, and nothing is granted
//	wildcard       the scope is "*", which grants every system and talkgroup
//	systems        an explicit list of systems, each with its own talkgroup list
type AccessScope struct {
	Present  bool
	Wildcard bool
	Systems  []ScopedSystem
}

// ScopedSystem is one system an access or apikey may see, named by the reference clients use rather
// than by the database identifier.
//
// TalkgroupsGiven is what separates two scope entries that otherwise look the same: one that named
// its talkgroups as a list, even an empty one, and one that named them some other way or not at all.
// The first sees that system with exactly those talkgroups under it, and the second does not see that
// system at all.
type ScopedSystem struct {
	SystemRef          uint
	TalkgroupsGiven    bool
	TalkgroupsWildcard bool
	TalkgroupRefs      []uint
}

// ParseAccessScope reads the scope out of a configuration payload or a stored scope.
// The second result reports whether a scope was present at all, which is not the same
// as the scope being empty.
func ParseAccessScope(value any) (AccessScope, bool) {
	switch v := value.(type) {
	case nil:
		return AccessScope{}, false

	case string:
		return AccessScope{Present: true, Wildcard: v == "*"}, true

	case []any:
		scope := AccessScope{Present: true}

		for _, item := range v {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}

			systemRef, ok := entry["id"].(float64)
			if !ok {
				continue
			}

			system := ScopedSystem{SystemRef: uint(systemRef)}

			switch talkgroups := entry["talkgroups"].(type) {
			case string:
				system.TalkgroupsWildcard = talkgroups == "*"

			case []any:
				// Naming talkgroups as a list is what makes the system visible at all, even when
				// the list is empty.
				system.TalkgroupsGiven = true

				for _, talkgroup := range talkgroups {
					if ref, ok := talkgroup.(float64); ok {
						system.TalkgroupRefs = append(system.TalkgroupRefs, uint(ref))
					}
				}
			}

			scope.Systems = append(scope.Systems, system)
		}

		return scope, true

	default:
		return AccessScope{}, false
	}
}

// Grants reports whether a call falls inside the scope. The comparison is made against
// the client-facing references, exactly as the legacy server compares them.
func (scope AccessScope) Grants(call Call) bool {
	return scope.GrantsReferences(call.System.SystemRef, call.Talkgroup.TalkgroupRef)
}

// GrantsReferences reports whether one system reference and one talkgroup reference fall
// inside the scope.
//
// It exists because the two things a scope is checked against are not always the same pair.
// A live call is checked after its system and talkgroup have been resolved; an upload is
// checked before that, when all the server has are the references the upload itself names.
// The legacy server compares against the resolved pair in both places, and at the upload
// boundary they are not resolved yet, which is what makes a scoped upload apikey dereference
// a nil aggregate. Checking the submitted
// references is what that comparison was written to do.
func (scope AccessScope) GrantsReferences(systemRef, talkgroupRef uint) bool {
	if scope.Wildcard {
		return true
	}

	for _, system := range scope.Systems {
		if system.SystemRef != systemRef {
			continue
		}

		if system.TalkgroupsWildcard {
			return true
		}

		for _, ref := range system.TalkgroupRefs {
			if ref == talkgroupRef {
				return true
			}
		}
	}

	return false
}

// Value is the scope as the server stores it and announces it: the wildcard is the string
// "*", an explicit scope is a list of systems each naming its talkgroups, and a scope that
// was never supplied is nothing at all.
//
// It is the mirror of ParseAccessScope, and both are needed because the same shape appears in
// the stored row and on the wire. A scope that names no systems is an empty list rather than
// nothing, because "an empty list was supplied" and "nothing was supplied" are the two states
// Q9 turns on. An entry that named no talkgroups gets no talkgroups key for the same reason:
// inventing an empty list would turn a system the listener cannot see into one the listener can.
func (scope AccessScope) Value() any {
	if !scope.Present {
		return nil
	}

	if scope.Wildcard {
		return "*"
	}

	systems := []any{}

	for _, system := range scope.Systems {
		entry := map[string]any{"id": system.SystemRef}

		if system.TalkgroupsWildcard {
			entry["talkgroups"] = "*"
		} else if system.TalkgroupsGiven {
			talkgroups := []any{}

			for _, ref := range system.TalkgroupRefs {
				talkgroups = append(talkgroups, ref)
			}

			entry["talkgroups"] = talkgroups
		}

		systems = append(systems, entry)
	}

	return systems
}
