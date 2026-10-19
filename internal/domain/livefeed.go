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

import "strconv"

// maxLivefeedEntries is the ceiling on a submitted live-watch selection: the talkgroup
// entries across all the systems it names. A real selection is tens of entries; a map
// over the cap is a way to spend the server's memory from one frame, and it is refused
// whole.
const maxLivefeedEntries = 4096

// Livefeed is one listener's live-watch selection: which talkgroups on which systems
// that listener wants delivered as they happen. A listener with nothing switched on
// receives no live calls.
//
// The selection is keyed by the client-facing references, not by database identifiers,
// because that is what the listener submits and what a call carries.
type Livefeed struct {
	Matrix map[uint]map[uint]bool
}

// NewLivefeed returns a selection with nothing switched on.
func NewLivefeed() Livefeed {
	return Livefeed{Matrix: map[uint]map[uint]bool{}}
}

// ParseLivefeed reads a listener-submitted selection. The selection replaces the
// previous one entirely: entries that are not in the payload are no longer switched on.
//
// Entries that are not a system reference with a talkgroup map, and values that are not
// booleans, are ignored.
func ParseLivefeed(value any) Livefeed {
	livefeed := NewLivefeed()

	systems, ok := value.(map[string]any)
	if !ok {
		return livefeed
	}

	// The payload is a listener's own selection, and a map over the cap is not a selection:
	// it is a way to spend the server's memory from one frame. The whole
	// payload is refused — nothing is switched on — because a half-read selection is a
	// selection the listener did not make.
	entries := 0

	for _, talkgroups := range systems {
		if inner, ok := talkgroups.(map[string]any); ok {
			entries += len(inner)
		}
	}

	if entries > maxLivefeedEntries {
		return livefeed
	}

	for system, talkgroups := range systems {
		systemRef, err := strconv.Atoi(system)
		if err != nil {
			continue
		}

		entries, ok := talkgroups.(map[string]any)
		if !ok {
			continue
		}

		for talkgroup, on := range entries {
			talkgroupRef, err := strconv.Atoi(talkgroup)
			if err != nil {
				continue
			}

			switching, ok := on.(bool)
			if !ok {
				continue
			}

			if livefeed.Matrix[uint(systemRef)] == nil {
				livefeed.Matrix[uint(systemRef)] = map[uint]bool{}
			}

			livefeed.Matrix[uint(systemRef)][uint(talkgroupRef)] = switching
		}
	}

	return livefeed
}

// Watches reports whether this listener's selection asks for a given call. A call that
// patches in another talkgroup counts as watched when that talkgroup is switched on.
func (livefeed Livefeed) Watches(call Call) bool {
	talkgroups := livefeed.Matrix[call.System.SystemRef]
	if talkgroups == nil {
		return false
	}

	if talkgroups[call.Talkgroup.TalkgroupRef] {
		return true
	}

	for _, patchRef := range call.PatchTalkgroupRefs {
		if talkgroups[patchRef] {
			return true
		}
	}

	return false
}

// AllOff reports whether the listener has nothing switched on, which is what the server
// answers a selection with.
func (livefeed Livefeed) AllOff() bool {
	for _, talkgroups := range livefeed.Matrix {
		for _, on := range talkgroups {
			if on {
				return false
			}
		}
	}

	return true
}
