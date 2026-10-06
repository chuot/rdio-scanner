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
)

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
