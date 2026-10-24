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

import "testing"

// Configuration sections arrive as lists and a save replaces the whole section, so the
// rules every section shares are pinned here rather than once per section.

// Each entry starts from the defaults that section's entries start with, plus whatever the
// submitted object names: an entry naming only its label is a complete entry.
func TestParseGroupsBuildsEachEntryFromWhatTheSubmittedObjectNames(t *testing.T) {
	groups := ParseGroups([]any{
		map[string]any{"id": float64(1), "label": "Dispatch Renamed", "led": "green"},
		map[string]any{"label": "New"},
	})

	if len(groups) != 2 {
		t.Fatalf("the submitted list holds %d groups, want the two it names: %#v", len(groups), groups)
	}

	if groups[0].ID != 1 || groups[0].Label != "Dispatch Renamed" || groups[0].LED != "green" {
		t.Errorf("the first group is %#v, want the identity, label, and light the submitted object names", groups[0])
	}

	if groups[1].ID != 0 || groups[1].Label != "New" || groups[1].LED != "" {
		t.Errorf("the second group is %#v, want only the label it names and the defaults for everything else", groups[1])
	}
}

// An entry that is not an object is left out rather than guessed at: the legacy server looks at each
// entry, and one that is not a set of fields is not an entry. A list that holds junk alongside real
// entries is saved as the real entries.
func TestParseGroupsLeavesOutAnEntryThatIsNotAnObject(t *testing.T) {
	groups := ParseGroups([]any{
		map[string]any{"label": "Real"},
		"not an object",
		float64(42),
		nil,
	})

	if len(groups) != 1 {
		t.Fatalf("the submitted list yields %d groups, want the one entry that is an object: %#v", len(groups), groups)
	}

	if groups[0].Label != "Real" {
		t.Errorf("the group that survived is %#v, want the entry the list actually named", groups[0])
	}
}

// A section submitted as nothing is an empty section and not a missing one: the save replaces what the
// server had with nothing, which is how an administrator clears a section. A value that is not a list at
// all is not a submission either — the legacy server looks at it, does not recognize it, and saves
// nothing at all, which the presentation layer has to be able to tell apart from an empty list.
func TestParseGroupsOfNothingIsAnEmptySection(t *testing.T) {
	for _, submitted := range []any{[]any{}, nil, "not a list", map[string]any{"label": "not a list"}} {
		groups := ParseGroups(submitted)

		if groups == nil {
			t.Errorf("a section submitted as %#v is no section at all, want an empty one", submitted)

			continue
		}

		if len(groups) != 0 {
			t.Errorf("a section submitted as %#v yields %d groups, want none", submitted, len(groups))
		}
	}
}

// A system carries its talkgroups, sites, and units inside it, and a save of the systems section is
// where all of them are replaced. A parser that only read the system's own fields would quietly drop
// the talkgroups an administrator just named, and the calls that arrive for them would find nowhere to
// go.
func TestParseSystemsKeepsWhatEachSystemCarriesInsideIt(t *testing.T) {
	systems := ParseSystems([]any{
		map[string]any{
			"label":      "Test System",
			"systemRef":  float64(10),
			"talkgroups": []any{map[string]any{"label": "TG1", "talkgroupRef": float64(101), "order": float64(1)}},
			"sites":      []any{map[string]any{"label": "Site 1", "siteRef": float64(1)}},
		},
	})

	if len(systems) != 1 {
		t.Fatalf("the submitted list yields %d systems, want the one it names: %#v", len(systems), systems)
	}

	system := systems[0]

	if system.Label != "Test System" || system.SystemRef != 10 {
		t.Errorf("the system is %#v, want the label and reference the submitted object names", system)
	}

	if len(system.Talkgroups) != 1 || system.Talkgroups[0].Label != "TG1" || system.Talkgroups[0].TalkgroupRef != 101 {
		t.Errorf("the system carries %v, want the one talkgroup it names: %#v", system.Talkgroups, system.Talkgroups)
	}

	if len(system.Sites) != 1 || system.Sites[0].Label != "Site 1" {
		t.Errorf("the system carries %v, want the one site it names: %#v", system.Sites, system.Sites)
	}
}

// An access code carries the scope that decides what a listener holding it may see, and the scope is
// read from the submitted entry the same way it is read anywhere else in the server. A save that lost
// the scope would turn a restricted server's carefully narrowed code into one that sees everything, or
// one that sees nothing.
func TestParseAccessesKeepsTheScopeEachAccessNames(t *testing.T) {
	accesses := ParseAccesses([]any{
		map[string]any{"code": "open", "ident": "Open", "systems": "*"},
		map[string]any{"code": "narrow", "ident": "Narrow", "systems": []any{map[string]any{"id": float64(10002), "talkgroups": "*"}}},
	})

	if len(accesses) != 2 {
		t.Fatalf("the submitted list yields %d access codes, want the two it names: %#v", len(accesses), accesses)
	}

	if !accesses[0].Scope.Present || !accesses[0].Scope.Wildcard {
		t.Errorf("the access named %q carries the scope %v, want one that names everything", "open", accesses[0].Scope)
	}

	if !accesses[1].Scope.Present || accesses[1].Scope.Wildcard {
		t.Errorf("the access named %q carries the scope %v, want one that names system 10002", "narrow", accesses[1].Scope)
	}

	if got := len(accesses[1].Scope.Systems); got != 1 {
		t.Errorf("the access named %q names %d systems, want the one it lists", "narrow", got)
	}
}

// Every section an administrator can save is a list of entries, and the seven of them are parsed by the
// same two rules. This test exists so that a section added to the configuration without a parser is
// noticed here rather than at the endpoint that answers it with an empty list forever.
func TestEveryConfigurationSectionIsParsedFromTheListTheAdministratorSubmitted(t *testing.T) {
	entry := map[string]any{"label": "One"}

	if got := len(ParseAccesses([]any{map[string]any{"code": "one"}})); got != 1 {
		t.Errorf("the access section yields %d entries, want 1", got)
	}

	if got := len(ParseApikeys([]any{map[string]any{"key": "capture-key"}})); got != 1 {
		t.Errorf("the apikeys section yields %d entries, want 1", got)
	}

	if got := len(ParseDirwatches([]any{map[string]any{"directory": "/tmp/opencode/one"}})); got != 1 {
		t.Errorf("the dirwatch section yields %d entries, want 1", got)
	}

	if got := len(ParseDownstreams([]any{map[string]any{"url": "http://127.0.0.1:3000"}})); got != 1 {
		t.Errorf("the downstreams section yields %d entries, want 1", got)
	}

	if got := len(ParseGroups([]any{entry})); got != 1 {
		t.Errorf("the groups section yields %d entries, want 1", got)
	}

	if got := len(ParseSystems([]any{map[string]any{"label": "One", "systemRef": float64(1)}})); got != 1 {
		t.Errorf("the systems section yields %d entries, want 1", got)
	}

	if got := len(ParseTags([]any{entry})); got != 1 {
		t.Errorf("the tags section yields %d entries, want 1", got)
	}
}
