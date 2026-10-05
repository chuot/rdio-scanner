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

// ScopedSystem is one system an access or apikey may reach, named by the reference
// clients use rather than by the database identifier.
type ScopedSystem struct {
	SystemRef          uint
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
	if scope.Wildcard {
		return true
	}

	for _, system := range scope.Systems {
		if system.SystemRef != call.System.SystemRef {
			continue
		}

		if system.TalkgroupsWildcard {
			return true
		}

		for _, ref := range system.TalkgroupRefs {
			if ref == call.Talkgroup.TalkgroupRef {
				return true
			}
		}
	}

	return false
}
