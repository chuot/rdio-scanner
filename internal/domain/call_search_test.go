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
			Label:     "Grand Metro",
			Talkgroups: Talkgroups{
				{ID: 100, TalkgroupRef: 1514000, Label: "Fire dispatch", GroupIDs: []uint64{1}, TagID: 11},
				{ID: 101, TalkgroupRef: 1514001, Label: "EMS dispatch", GroupIDs: []uint64{2}, TagID: 10},
				{ID: 102, TalkgroupRef: 1514002, Label: "Fire car", GroupIDs: []uint64{1}},
			},
		},
		{
			ID:        2,
			SystemRef: 5,
			Label:     "County Trunk",
			Talkgroups: Talkgroups{
				{ID: 200, TalkgroupRef: 222000, Label: "Sheriff", GroupIDs: []uint64{1}, TagID: 10},
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

// A scope that names a system and names its talkgroups as an empty list is not the same as a scope
// that does not name talkgroups at all: the first is answered with the system and nothing under it,
// and the second is answered with nothing. Both come out of a configuration payload a person wrote,
// and a listener that shows a system with no talkgroups under it is showing what the server agreed to.
func TestScopedViewKeepsASystemNamedWithAnEmptyTalkgroupList(t *testing.T) {
	systems, tags, _ := searchCatalog()

	scope, _ := ParseAccessScope([]any{
		map[string]any{"id": float64(4), "talkgroups": []any{}},
	})

	view := systems.Scoped(scope, tags)

	if len(view) != 1 || view[0].SystemRef != 4 {
		t.Fatalf("Scoped produced %+v, want system 4 with nothing under it", view)
	}

	if len(view[0].Talkgroups) != 0 {
		t.Errorf("scoped system 4 talkgroups = %+v, want none", view[0].Talkgroups)
	}
}

// A scope that names talkgroups as something that is neither the wildcard nor a list — a single word,
// or nothing at all — names a system the listener never sees. The server does not guess what the word
// meant and does not fall back to the whole system.
func TestScopedViewSkipsASystemWhoseTalkgroupsAreNamedAsNeitherAListNorTheWildcard(t *testing.T) {
	systems, tags, _ := searchCatalog()

	for name, talkgroups := range map[string]any{
		"a word":       "Fire",
		"nothing":      nil,
		"a number":     float64(7),
		"an empty map": map[string]any{},
	} {
		scope, _ := ParseAccessScope([]any{
			map[string]any{"id": float64(4), "talkgroups": talkgroups},
		})

		if view := systems.Scoped(scope, tags); len(view) != 0 {
			t.Errorf("a scope naming talkgroups as %s produced %+v, want nothing", name, view)
		}
	}
}

// A system whose talkgroups all have tags the server no longer knows is still a system the listener
// sees — with nothing under it. Dropping the talkgroups is what the server does; dropping the system
// is not.
func TestScopedViewKeepsASystemWhoseTalkgroupsAllLoseTheirTags(t *testing.T) {
	_, tags, _ := searchCatalog()

	untagged := Systems{{ID: 1, SystemRef: 6, Talkgroups: Talkgroups{{ID: 300, TalkgroupRef: 1616000}}}}

	view := untagged.Scoped(AccessScope{}, tags)

	if len(view) != 1 || view[0].SystemRef != 6 {
		t.Fatalf("Scoped produced %+v, want system 6 with nothing under it", view)
	}

	if len(view[0].Talkgroups) != 0 {
		t.Errorf("scoped system 6 talkgroups = %+v, want none", view[0].Talkgroups)
	}
}

// The talkgroups under a scoped system come in the order the scope names them, not the order the
// system keeps them in.
func TestScopedViewListsNamedTalkgroupsInTheOrderTheScopeNamesThem(t *testing.T) {
	systems, tags, _ := searchCatalog()

	scope, _ := ParseAccessScope([]any{
		map[string]any{"id": float64(4), "talkgroups": []any{float64(1514001), float64(1514000)}},
	})

	view := systems.Scoped(scope, tags)

	if len(view) != 1 {
		t.Fatalf("Scoped produced %+v, want one system", view)
	}

	got := []uint{}

	for _, talkgroup := range view[0].Talkgroups {
		got = append(got, talkgroup.TalkgroupRef)
	}

	if len(got) != 2 || got[0] != 1514001 || got[1] != 1514000 {
		t.Errorf("scoped talkgroups = %v, want [1514001 1514000]", got)
	}
}

// A scope that names the same system twice is answered with that system twice, because the server
// appends what it is given and does not collapse what it was asked for.
func TestScopedViewNamesASystemOncePerScopeEntry(t *testing.T) {
	systems, tags, _ := searchCatalog()

	scope, _ := ParseAccessScope([]any{
		map[string]any{"id": float64(4), "talkgroups": "*"},
		map[string]any{"id": float64(4), "talkgroups": []any{float64(1514000)}},
	})

	view := systems.Scoped(scope, tags)

	if len(view) != 2 {
		t.Fatalf("Scoped produced %+v, want system 4 twice", view)
	}
}

// A scope entry that did not name its talkgroups is stored without naming them, so that reading the
// row back produces the same view it produced the first time.
func TestScopeEntryThatNamedNoTalkgroupsIsStoredThatWay(t *testing.T) {
	scope, _ := ParseAccessScope([]any{
		map[string]any{"id": float64(4)},
	})

	value, ok := scope.Value().([]any)
	if !ok {
		t.Fatalf("scope value = %#v, want a list of systems", scope.Value())
	}

	entry, _ := value[0].(map[string]any)

	if _, present := entry["talkgroups"]; present {
		t.Errorf("stored scope entry = %#v, want no talkgroups key", entry)
	}

	if stored, ok := ParseAccessScope(scope.Value()); !ok {
		t.Error("the stored scope no longer reads as a scope")
	} else if view := systemsOf(4).Scoped(stored, tagsOf()); len(view) != 0 {
		t.Errorf("the stored scope produced %+v, want nothing, as it did before it was stored", view)
	}
}

func systemsOf(ref uint) Systems {
	return Systems{{ID: 1, SystemRef: ref, Talkgroups: Talkgroups{{ID: 100, TalkgroupRef: 1514000, TagID: 11}}}}
}

func tagsOf() Tags {
	return Tags{{ID: 11, Label: "Fire"}}
}

// TestParseCallSearchClampsThePageParameters: limit and offset are page
// parameters, and a negative one is not a page. Converting a negative float to uint wraps it into
// a huge number, which is exactly the "no limit" a hostile client wants, and a huge offset is a
// scan past the end of the table. Both clamp.
func TestParseCallSearchClampsThePageParameters(t *testing.T) {
	tests := []struct {
		name       string
		limit      float64
		offset     float64
		wantLimit  uint
		wantOffset uint
	}{
		{"negative limit", -5, 0, 1, 0},
		{"zero limit", 0, 0, 0, 0},
		{"huge limit", 1e18, 0, MaxCallSearchLimit, 0},
		{"negative offset", 100, -1, 100, 0},
		{"huge offset", 100, 1e18, 100, MaxSearchOffset},
		{"ordinary page", 50, 200, 50, 200},
	}

	for _, tc := range tests {
		search := ParseCallSearch(map[string]any{"limit": tc.limit, "offset": tc.offset})

		if search.Limit != tc.wantLimit || search.Offset != tc.wantOffset {
			t.Errorf("%s: limit = %d offset = %d, want limit = %d offset = %d",
				tc.name, search.Limit, search.Offset, tc.wantLimit, tc.wantOffset)
		}
	}
}

// The search screen's System and Talkgroup filters name the label the operator read, and the
// history is narrowed by reference: ParseCallSearch reads a system and a talkgroup as numbers, and
// a criterion arriving as text is read as no criterion at all. The screen answers with the whole
// history, silently.
//
// The catalog the screen is given is keyed by the same references the history is stored under, and
// the group and tag filters are expanded by label on the server. So the label the operator read
// becomes a reference through the catalog the listener was already given, and a group and a tag
// travel as labels.
func TestCallSearchCriteriaFromTheFilterLabelsTheOperatorRead(t *testing.T) {
	systems, tags, groups := searchCatalog()

	criteria := func(t *testing.T, systemLabel, talkgroupLabel string) (uint, uint) {
		t.Helper()

		system, ok := systems.ByLabel(systemLabel)
		if !ok {
			t.Fatalf("the catalog names no system labelled %q", systemLabel)
		}

		talkgroup, ok := system.Talkgroups.ByLabel(talkgroupLabel)
		if !ok {
			t.Fatalf("system %q names no talkgroup labelled %q", systemLabel, talkgroupLabel)
		}

		return system.SystemRef, talkgroup.TalkgroupRef
	}

	// A group label and a tag label are expanded on the server, from the catalog as the listener
	// sees it. A reference sent in their place names nothing the expansion knows.
	view := systems.Scoped(AccessScope{}, tags)

	if len(ScopeForGroup(groups, view, "4")) != 0 {
		t.Error("a reference sent as a group label found talkgroups, want none")
	}

	if len(ScopeForTag(tags, view, "11")) != 0 {
		t.Error("a reference sent as a tag label found talkgroups, want none")
	}

	if len(ScopeForGroup(groups, view, "Fire")) == 0 {
		t.Error("the group label the operator read expanded to nothing, want the talkgroups it names")
	}

	if len(ScopeForTag(tags, view, "Fire")) == 0 {
		t.Error("the tag label the operator read expanded to nothing, want the talkgroups it names")
	}

	// The criteria the labels stand for, read the way the search reads them.
	search := ParseCallSearch(map[string]any{
		"system":    float64(4),
		"talkgroup": float64(1514000),
		"group":     "Fire",
		"tag":       "Untagged",
	})

	if !search.HasGroup() || !search.HasTag() {
		t.Errorf("ParseCallSearch = %+v, want the labels the operator read to stand", search)
	}

	if search.System != 4 || search.Talkgroup != 1514000 {
		t.Errorf("ParseCallSearch = %+v, want the references the labels stand for", search)
	}

	if ref, tg := criteria(t, "Grand Metro", "Fire dispatch"); ref != search.System || tg != search.Talkgroup {
		t.Errorf("the labels stand for %d/%d, the criteria carry %d/%d", ref, tg, search.System, search.Talkgroup)
	}

	// A filter arriving as the label the operator read is read as no filter at all.
	labelled := ParseCallSearch(map[string]any{
		"system":    "Grand Metro",
		"talkgroup": "Fire dispatch",
		"group":     "Fire",
		"tag":       "Untagged",
	})

	if labelled.System != 0 || labelled.Talkgroup != 0 {
		t.Errorf("ParseCallSearch = %+v, want a label where a reference is read to leave the search unfiltered", labelled)
	}

	if !labelled.HasGroup() || !labelled.HasTag() {
		t.Errorf("ParseCallSearch = %+v, want the group and the tag to stand as the labels they are", labelled)
	}
}
