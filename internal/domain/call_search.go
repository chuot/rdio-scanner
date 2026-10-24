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
	"time"
)

// Call-history paging: how many calls a search returns when the listener does not ask,
// the most it will ever return, and the furthest page it will ever resume from.
const (
	DefaultCallSearchLimit = uint(200)
	MaxCallSearchLimit     = uint(500)

	// MaxSearchOffset is the largest page position any search will resume from. A JSON
	// number above what uint can hold, converted as the legacy server converts it, is not
	// a large number — it is whatever the conversion lands on, and a hostile client picks
	// that on purpose. The bound is the largest value the paging SQL can carry.
	MaxSearchOffset = uint(1<<31 - 1)
)

// clampPage reads one submitted page parameter: a negative value becomes the floor, a
// value above the ceiling becomes the ceiling, and a value between them keeps its
// integer part. An explicit zero is left alone: for a limit it means no rows, for an
// offset the start of the table.
func clampPage(v float64, floor uint, ceiling uint) uint {
	if v < 0 {
		return floor
	}

	if v > float64(ceiling) {
		return ceiling
	}

	return uint(v)
}

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
		search.Limit = clampPage(v, 1, MaxCallSearchLimit)
	} else {
		search.Limit = DefaultCallSearchLimit
	}

	if v, ok := m["offset"].(float64); ok {
		search.Offset = clampPage(v, 0, MaxSearchOffset)
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
// when there is no date criterion. This is the call-history window: descending runs back
// one day from the truncated instant and stops at that instant. The log-search window
// shifts additionally by the submitted hour and minute; the two rules stay separate.
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

// Scoped returns the catalog as one listener's view sees it. A listener with
// no scope named sees every system here — unlike live delivery, where no
// scope grants nothing. Talkgroups the server no longer knows are left out;
// a system keeps its place even empty. The scope-list distinction and the
// ordering quirks live in the domain maintainer note.
func (systems Systems) Scoped(scope AccessScope, tags Tags) Systems {
	out := Systems{}

	if !scope.Present || scope.Wildcard {
		for _, system := range systems {
			system.Talkgroups = taggedTalkgroups(system, tags)

			out = append(out, system)
		}

		return out
	}

	for _, entry := range scope.Systems {
		system, ok := systems.ByRef(entry.SystemRef)
		if !ok {
			continue
		}

		// A scope entry that named its talkgroups as neither the wildcard nor a list names a system
		// this listener does not see.
		if !entry.TalkgroupsGiven && !entry.TalkgroupsWildcard {
			continue
		}

		named := Talkgroups{}

		if entry.TalkgroupsWildcard {
			for _, talkgroup := range system.Talkgroups {
				if _, ok := tags.ByID(talkgroup.TagID); !ok {
					continue
				}

				named = append(named, talkgroup)
			}
		} else {
			// In the order the scope names them, which is not the order the system keeps them in,
			// and a reference that is not on this system is left out rather than looked for
			// somewhere else.
			for _, ref := range entry.TalkgroupRefs {
				talkgroup, ok := system.Talkgroups.ByRef(ref)
				if !ok {
					continue
				}

				if _, ok := tags.ByID(talkgroup.TagID); !ok {
					continue
				}

				named = append(named, talkgroup)
			}
		}

		// A system with nothing left under it is still a system the listener sees.
		system.Talkgroups = named

		out = append(out, system)
	}

	return out
}

// taggedTalkgroups returns the talkgroups of a system whose tag the server still knows.
func taggedTalkgroups(system System, tags Tags) Talkgroups {
	kept := Talkgroups{}

	for _, talkgroup := range system.Talkgroups {
		if _, ok := tags.ByID(talkgroup.TagID); !ok {
			continue
		}

		kept = append(kept, talkgroup)
	}

	return kept
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
