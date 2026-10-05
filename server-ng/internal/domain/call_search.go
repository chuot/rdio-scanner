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
	"math"
	"time"
)

// Call-history paging: how many calls a search returns when the listener does not ask,
// and the most it will ever return.
const (
	DefaultCallSearchLimit = uint(200)
	MaxCallSearchLimit     = uint(500)
)

// CallSearch is the criteria a listener submits with a call-history request.
type CallSearch struct {
	Date      time.Time
	Group     string
	Limit     uint
	Offset    uint
	Sort      int
	System    uint
	Tag       string
	Talkgroup uint
}

// ParseCallSearch reads the criteria out of a listener payload. Anything that does not
// parse leaves that criterion unfiltered, which is what the legacy server does.
func ParseCallSearch(m map[string]any) CallSearch {
	search := CallSearch{}

	if v, ok := m["date"].(string); ok {
		if at, err := time.Parse(time.RFC3339, v); err == nil {
			search.Date = at
		}
	}

	if v, ok := m["group"].(string); ok {
		search.Group = v
	}

	if v, ok := m["limit"].(float64); ok {
		search.Limit = uint(math.Min(float64(MaxCallSearchLimit), v))
	} else {
		search.Limit = DefaultCallSearchLimit
	}

	if v, ok := m["offset"].(float64); ok {
		search.Offset = uint(v)
	}

	if v, ok := m["sort"].(float64); ok {
		search.Sort = int(v)
	}

	if v, ok := m["system"].(float64); ok {
		search.System = uint(v)
	}

	if v, ok := m["tag"].(string); ok {
		search.Tag = v
	}

	if v, ok := m["talkgroup"].(float64); ok {
		search.Talkgroup = uint(v)
	}

	return search
}

// Ascending reports which end of the history the search starts from. An absent or zero
// sort starts from the oldest, as the legacy server does.
func (search CallSearch) Ascending() bool {
	return search.Sort >= 0
}

// HasDate reports whether the criteria restricts the day.
func (search CallSearch) HasDate() bool {
	return !search.Date.IsZero()
}

// HasGroup reports whether the criteria filters by a group label.
func (search CallSearch) HasGroup() bool {
	return search.Group != ""
}

// HasTag reports whether the criteria filters by a tag label.
func (search CallSearch) HasTag() bool {
	return search.Tag != ""
}

// DateWindow returns the inclusive timestamp bounds the date criterion selects, or false
// when there is no date criterion.
//
// This is the call-history window, and it is **not** the log-search window: descending
// here runs back a day from the truncated instant and stops at that instant, while the
// log view shifts its window back by the submitted hour and minute as well. The two are
// separate rules in the legacy server and are kept separate here.
func (search CallSearch) DateWindow() (time.Time, time.Time, bool) {
	if !search.HasDate() {
		return time.Time{}, time.Time{}, false
	}

	// Seconds and sub-second parts of the submitted instant are discarded.
	truncated := time.Date(
		search.Date.Year(), search.Date.Month(), search.Date.Day(),
		search.Date.Hour(), search.Date.Minute(), 0, 0,
		time.UTC,
	)

	if search.Ascending() {
		return truncated, truncated.Add(24*time.Hour - time.Millisecond), true
	}

	return truncated.Add(-24*time.Hour + time.Millisecond), truncated, true
}

// TalkgroupScope names the talkgroups on one system that a search filter stands for.
type TalkgroupScope struct {
	SystemRef     uint
	TalkgroupRefs []uint
}

// Scoped returns the catalog as one listener's view sees it.
//
// The rule is deliberately different from Access.Grants, which decides whether a *live
// call* is delivered. For the catalog view, a listener with no scope named at all sees
// every system — that is how an anonymous listener on an unrestricted server sees the
// server. For live delivery, no scope named grants nothing.
//
// A talkgroup whose tag the server no longer knows is left out of the view entirely, as
// the legacy server leaves it out of the configuration it sends.
func (systems Systems) Scoped(scope AccessScope, tags Tags) Systems {
	out := Systems{}

	if !scope.Present || scope.Wildcard {
		for _, system := range systems {
			if scoped, ok := withTaggedTalkgroups(system, tags); ok {
				out = append(out, scoped)
			}
		}

		return out
	}

	for _, entry := range scope.Systems {
		system, ok := systems.ByRef(entry.SystemRef)
		if !ok {
			continue
		}

		if entry.TalkgroupsWildcard {
			if scoped, ok := withTaggedTalkgroups(system, tags); ok {
				out = append(out, scoped)
			}

			continue
		}

		named := Talkgroups{}

		for _, talkgroup := range system.Talkgroups {
			if _, ok := tags.ByID(talkgroup.TagID); !ok {
				continue
			}

			for _, ref := range entry.TalkgroupRefs {
				if ref == talkgroup.TalkgroupRef {
					named = append(named, talkgroup)

					break
				}
			}
		}

		if len(named) == 0 {
			continue
		}

		system.Talkgroups = named

		out = append(out, system)
	}

	return out
}

// withTaggedTalkgroups returns the system with only the talkgroups whose tag the server
// knows. It reports false when nothing is left.
func withTaggedTalkgroups(system System, tags Tags) (System, bool) {
	kept := Talkgroups{}

	for _, talkgroup := range system.Talkgroups {
		if _, ok := tags.ByID(talkgroup.TagID); !ok {
			continue
		}

		kept = append(kept, talkgroup)
	}

	if len(kept) == 0 {
		return System{}, false
	}

	system.Talkgroups = kept

	return system, true
}

// ScopeForGroup returns where a group label applies in the given catalog: the systems
// that have talkgroups in that group, and which talkgroups they are.
func ScopeForGroup(groups Groups, systems Systems, label string) []TalkgroupScope {
	out := []TalkgroupScope{}

	for _, system := range systems {
		refs := []uint{}

		for _, talkgroup := range system.Talkgroups {
			for _, groupID := range talkgroup.GroupIDs {
				if group, ok := groups.ByID(groupID); ok && group.Label == label {
					refs = append(refs, talkgroup.TalkgroupRef)

					break
				}
			}
		}

		if len(refs) > 0 {
			out = append(out, TalkgroupScope{SystemRef: system.SystemRef, TalkgroupRefs: refs})
		}
	}

	return out
}

// ScopeForTag returns where a tag label applies in the given catalog.
func ScopeForTag(tags Tags, systems Systems, label string) []TalkgroupScope {
	out := []TalkgroupScope{}

	for _, system := range systems {
		refs := []uint{}

		for _, talkgroup := range system.Talkgroups {
			if tag, ok := tags.ByID(talkgroup.TagID); ok && tag.Label == label {
				refs = append(refs, talkgroup.TalkgroupRef)
			}
		}

		if len(refs) > 0 {
			out = append(out, TalkgroupScope{SystemRef: system.SystemRef, TalkgroupRefs: refs})
		}
	}

	return out
}
