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
	"testing"
	"time"
)

func searchCatalog() (Systems, Tags, Groups) {
	tags := Tags{
		{ID: 10, Label: "Untagged"},
		{ID: 11, Label: "Fire"},
	}

	groups := Groups{
		{ID: 1, Label: "Fire"},
		{ID: 2, Label: "EMS"},
	}

	systems := Systems{
		{
			ID:        1,
			SystemRef: 4,
			Talkgroups: Talkgroups{
				{ID: 100, TalkgroupRef: 1514000, GroupIDs: []uint64{1}, TagID: 11},
				{ID: 101, TalkgroupRef: 1514001, GroupIDs: []uint64{2}, TagID: 10},
				{ID: 102, TalkgroupRef: 1514002, GroupIDs: []uint64{1}},
			},
		},
		{
			ID:        2,
			SystemRef: 5,
			Talkgroups: Talkgroups{
				{ID: 200, TalkgroupRef: 222000, GroupIDs: []uint64{1}, TagID: 10},
			},
		},
	}

	return systems, tags, groups
}

func TestCallSearchDefaults(t *testing.T) {
	search := ParseCallSearch(map[string]any{})

	if search.Limit != DefaultCallSearchLimit {
		t.Errorf("Limit = %d, want the default %d", search.Limit, DefaultCallSearchLimit)
	}

	if !search.Ascending() {
		t.Error("the default sort order is ascending")
	}

	if search.HasDate() || search.HasGroup() || search.HasTag() {
		t.Error("an empty criteria must filter nothing")
	}
}

func TestCallSearchClampsTheLimitButHonoursAnExplicitZero(t *testing.T) {
	if got := ParseCallSearch(map[string]any{"limit": float64(5000)}).Limit; got != MaxCallSearchLimit {
		t.Errorf("Limit = %d, want it clamped to %d", got, MaxCallSearchLimit)
	}

	if got := ParseCallSearch(map[string]any{"limit": float64(50)}).Limit; got != 50 {
		t.Errorf("Limit = %d, want the submitted 50", got)
	}

	if got := ParseCallSearch(map[string]any{"limit": float64(0)}).Limit; got != 0 {
		t.Errorf("Limit = %d, want the submitted 0 preserved", got)
	}
}

func TestCallSearchSortDirection(t *testing.T) {
	if !ParseCallSearch(map[string]any{"sort": float64(1)}).Ascending() {
		t.Error("a positive sort asks for ascending order")
	}

	if ParseCallSearch(map[string]any{"sort": float64(-1)}).Ascending() {
		t.Error("a negative sort asks for descending order")
	}

	if !ParseCallSearch(map[string]any{"sort": float64(0)}).Ascending() {
		t.Error("a zero sort asks for ascending order")
	}
}

func TestCallSearchFilters(t *testing.T) {
	search := ParseCallSearch(map[string]any{
		"group":     "Fire",
		"tag":       "Untagged",
		"system":    float64(4),
		"talkgroup": float64(1514000),
		"offset":    float64(400),
	})

	if search.Group != "Fire" || search.Tag != "Untagged" {
		t.Errorf("ParseCallSearch = %+v, want the submitted labels", search)
	}

	if search.System != 4 || search.Talkgroup != 1514000 {
		t.Errorf("ParseCallSearch = %+v, want the submitted references", search)
	}

	if search.Offset != 400 {
		t.Errorf("Offset = %d, want the submitted 400", search.Offset)
	}
}

func TestCallSearchDateMustParse(t *testing.T) {
	if ParseCallSearch(map[string]any{"date": "not a date"}).HasDate() {
		t.Error("an unparsable date must leave the criteria unfiltered")
	}

	search := ParseCallSearch(map[string]any{"date": "2026-03-14T09:30:15Z"})

	if !search.HasDate() {
		t.Fatal("a valid RFC3339 date must set the date filter")
	}
}

// TestCallSearchDateWindowIsNotTheLogDateWindow pins the difference between the two
// history views: the call history runs back a day and stops at the submitted instant,
// while the log view shifts its window back by the submitted hour and minute as well.
func TestCallSearchDateWindowIsNotTheLogDateWindow(t *testing.T) {
	date := time.Date(2026, 3, 14, 9, 30, 15, 0, time.UTC)

	ascending := CallSearch{Date: date}

	from, to, ok := ascending.DateWindow()
	if !ok {
		t.Fatal("a criteria with a date must produce a window")
	}

	wantFrom := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)
	wantTo := wantFrom.Add(24*time.Hour - time.Millisecond)

	if !from.Equal(wantFrom) {
		t.Errorf("ascending window start = %v, want %v with the seconds discarded", from, wantFrom)
	}

	if !to.Equal(wantTo) {
		t.Errorf("ascending window stop = %v, want %v", to, wantTo)
	}

	descending := CallSearch{Date: date, Sort: -1}

	from, to, _ = descending.DateWindow()

	wantFrom = time.Date(2026, 3, 13, 9, 30, 0, 0, time.UTC).Add(time.Millisecond)
	wantTo = time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

	if !from.Equal(wantFrom) {
		t.Errorf("descending window start = %v, want %v", from, wantFrom)
	}

	if !to.Equal(wantTo) {
		t.Errorf("descending window stop = %v, want the submitted instant itself", wantTo)
	}

	if _, _, ok := (CallSearch{}).DateWindow(); ok {
		t.Error("a criteria without a date must produce no window")
	}
}

func TestScopedWithoutAScopeSeesEverySystem(t *testing.T) {
	systems, tags, _ := searchCatalog()

	view := systems.Scoped(AccessScope{}, tags)

	if len(view) != 2 {
		t.Fatalf("Scoped with no scope produced %d systems, want every system", len(view))
	}
}

func TestScopedDropsATalkgroupWhoseTagTheServerNoLongerKnows(t *testing.T) {
	systems, tags, _ := searchCatalog()

	view := systems.Scoped(AccessScope{}, tags)

	first, _ := view.ByRef(4)

	if len(first.Talkgroups) != 2 {
		t.Fatalf("scoped talkgroups = %+v, want the talkgroup with no tag left out", first.Talkgroups)
	}

	for _, talkgroup := range first.Talkgroups {
		if talkgroup.TalkgroupRef == 1514002 {
			t.Error("the tag-less talkgroup must not appear in the listener's view")
		}
	}
}

func TestScopedToAListOfSystemsAndTalkgroups(t *testing.T) {
	systems, tags, _ := searchCatalog()

	scope, _ := ParseAccessScope([]any{
		map[string]any{"id": float64(4), "talkgroups": []any{float64(1514000)}},
		map[string]any{"id": float64(5), "talkgroups": "*"},
		map[string]any{"id": float64(99), "talkgroups": "*"},
	})

	view := systems.Scoped(scope, tags)

	if len(view) != 2 {
		t.Fatalf("Scoped produced %d systems, want the two the scope names; an unknown system contributes nothing", len(view))
	}

	narrow, _ := view.ByRef(4)
	if len(narrow.Talkgroups) != 1 || narrow.Talkgroups[0].TalkgroupRef != 1514000 {
		t.Errorf("scoped system 4 talkgroups = %+v, want only the named talkgroup", narrow.Talkgroups)
	}

	wide, _ := view.ByRef(5)
	if len(wide.Talkgroups) != 1 {
		t.Errorf("scoped system 5 talkgroups = %+v, want the whole system under the talkgroup wildcard", wide.Talkgroups)
	}
}

func TestScopeForGroupExpandsALabelToConcreteTalkgroups(t *testing.T) {
	systems, tags, groups := searchCatalog()

	pairs := ScopeForGroup(groups, systems.Scoped(AccessScope{}, tags), "Fire")

	if len(pairs) != 2 {
		t.Fatalf("ScopeForGroup produced %d pairs, want one per system that has a talkgroup in the group", len(pairs))
	}

	for _, pair := range pairs {
		for _, ref := range pair.TalkgroupRefs {
			if ref == 1514002 {
				t.Errorf("pair %+v includes the tag-less talkgroup, which is not in the listener's view", pair)
			}
		}
	}

	if pairs[0].SystemRef != 4 || pairs[0].TalkgroupRefs[0] != 1514000 {
		t.Errorf("pair = %+v, want system reference 4 and talkgroup reference 1514000", pairs[0])
	}

	if pairs[1].SystemRef != 5 || pairs[1].TalkgroupRefs[0] != 222000 {
		t.Errorf("pair = %+v, want system reference 5 and talkgroup reference 222000", pairs[1])
	}
}

func TestScopeForGroupOfALabelNothingBelongsTo(t *testing.T) {
	systems, tags, groups := searchCatalog()

	if pairs := ScopeForGroup(groups, systems.Scoped(AccessScope{}, tags), "Police"); len(pairs) != 0 {
		t.Errorf("ScopeForGroup produced %+v, want nothing for a label no talkgroup uses", pairs)
	}
}

func TestScopeForTagExpandsALabelToConcreteTalkgroups(t *testing.T) {
	systems, tags, _ := searchCatalog()

	pairs := ScopeForTag(tags, systems.Scoped(AccessScope{}, tags), "Untagged")

	if len(pairs) != 2 {
		t.Fatalf("ScopeForTag produced %d pairs, want one per system holding an untagged talkgroup", len(pairs))
	}

	if pairs[0].SystemRef != 4 || pairs[0].TalkgroupRefs[0] != 1514001 {
		t.Errorf("pair = %+v, want system reference 4 and talkgroup reference 1514001", pairs[0])
	}
}

func TestScopeForGroupFollowsTheListenersViewNotTheWholeCatalog(t *testing.T) {
	systems, tags, groups := searchCatalog()

	scope, _ := ParseAccessScope([]any{
		map[string]any{"id": float64(5), "talkgroups": "*"},
	})

	pairs := ScopeForGroup(groups, systems.Scoped(scope, tags), "Fire")

	if len(pairs) != 1 || pairs[0].SystemRef != 5 {
		t.Errorf("ScopeForGroup produced %+v, want only the system the listener is allowed to see", pairs)
	}
}
