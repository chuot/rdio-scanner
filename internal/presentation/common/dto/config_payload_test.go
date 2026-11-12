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

package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// TestConfigPayloadMatchesTheCapturedConfig proves an unrestricted listener's
// configuration byte for byte, as the legacy server told it.
func TestConfigPayloadMatchesTheCapturedConfig(t *testing.T) {
	got := marshal(t, ConfigPayload{Snapshot: captureSnapshot()})
	want := capturedAnswer(t, "payload-cfg.json")

	if got != want {
		t.Errorf("config payload =\n  %s\nwant\n  %s", got, want)
	}
}

// TestConfigPayloadMatchesTheCapturedScopedConfig proves the same for a
// scoped listener: systems, groups, and tags all narrow to that listener's
// view, not the server's whole set.
func TestConfigPayloadMatchesTheCapturedScopedConfig(t *testing.T) {
	snapshot := captureSnapshot()
	snapshot.Options.ShowListenersCount = false

	scope, ok := domain.ParseAccessScope([]any{
		map[string]any{"id": float64(10001), "talkgroups": "*"},
	})

	if !ok {
		t.Fatal("the captured access scope does not read as a scope")
	}

	got := marshal(t, ConfigPayload{Snapshot: snapshot, Scope: scope})
	want := capturedAnswer(t, "payload-cfg-scoped.json")

	if got != want {
		t.Errorf("scoped config payload =\n  %s\nwant\n  %s", got, want)
	}
}

// TestConfigPayloadOrdersTalkgroupsByTheOptionAndNotByTheServer proves the
// talkgroups option decides talkgroup order and numbering.
func TestConfigPayloadOrdersTalkgroupsByTheOptionAndNotByTheServer(t *testing.T) {
	snapshot := captureSnapshot()

	snapshot.Options.SortTalkgroups = false

	if got, want := talkgroupRefs(t, ConfigPayload{Snapshot: snapshot}), []uint{1514000, 1514100, 1514200, 1515000}; !sameRefs(got, want) {
		t.Errorf("with the option off, talkgroups come as %v, want the order the server keeps them in", got)
	}

	if got, want := talkgroupOrders(t, ConfigPayload{Snapshot: snapshot}), []uint{0, 0, 0, 0}; !sameRefs(got, want) {
		t.Errorf("with the option off, talkgroup orders = %v, want %v: the number the server stores, which for a talkgroup that was never given one is zero", got, []uint{0, 0, 0, 0})
	}

	snapshot.Options.SortTalkgroups = true

	if got, want := talkgroupRefs(t, ConfigPayload{Snapshot: snapshot}), []uint{1514200, 1514000, 1514100, 1515000}; !sameRefs(got, want) {
		t.Errorf("with the option on, talkgroups come as %v, want label order: Air before the two Grounds", got)
	}

	if got, want := talkgroupOrders(t, ConfigPayload{Snapshot: snapshot}), []uint{1, 2, 3, 1}; !sameRefs(got, want) {
		t.Errorf("with the option on, talkgroup orders = %v, want %v: each system numbers its own talkgroups from one", got, want)
	}
}

// TestConfigPayloadKeepsASystemWhoseTalkgroupsAllLoseTheirTags shows a
// system still shows when all its talkgroups lose their tags.
func TestConfigPayloadKeepsASystemWhoseTalkgroupsAllLoseTheirTags(t *testing.T) {
	snapshot := captureSnapshot()

	snapshot.Tags = domain.Tags{{ID: 20, Label: "Something else"}}

	config := decodeConfig(t, ConfigPayload{Snapshot: snapshot})

	if systems := listOf(t, config, "systems"); len(systems) != 2 {
		t.Fatalf("the config carries %d systems, want both, with nothing under them", len(systems))
	} else {
		for _, entry := range systems {
			if talkgroups := listOf(t, entry, "talkgroups"); len(talkgroups) != 0 {
				t.Errorf("system %v carries %d talkgroups, want none", objectOf(t, entry)["label"], len(talkgroups))
			}
		}
	}

	for _, key := range []string{"groups", "groupsData", "tags", "tagsData"} {
		if got := jsonOf(t, config[key]); got != "[]" && got != "{}" {
			t.Errorf("%s = %s, want nothing in it", key, got)
		}
	}
}

// TestConfigPayloadGivesOnlyTheGroupsAndTagsThisListenerCanSee proves scoped
// group and tag lists.
func TestConfigPayloadGivesOnlyTheGroupsAndTagsThisListenerCanSee(t *testing.T) {
	scope, _ := domain.ParseAccessScope([]any{
		map[string]any{"id": float64(10002), "talkgroups": "*"},
	})

	config := decodeConfig(t, ConfigPayload{Snapshot: captureSnapshot(), Scope: scope})

	for key, want := range map[string]string{
		"groups":     `{"Unknown":{"10002":[1515000]}}`,
		"groupsData": `[{"id":6,"label":"Unknown"}]`,
		"tags":       `{"Untagged":{"10002":[1515000]}}`,
		"tagsData":   `[{"id":9,"label":"Untagged"}]`,
	} {
		if got := jsonOf(t, config[key]); got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}
}

// TestConfigPayloadListsEveryGroupATalkgroupBelongsTo proves the group
// listing holds every group a talkgroup belongs to, whatever position it
// holds, including one that is never the first group.
func TestConfigPayloadListsEveryGroupATalkgroupBelongsTo(t *testing.T) {
	snapshot := captureSnapshot()

	// EMS is talkgroup 1514100's third group and the first group of no
	// talkgroup this listener can see. It has an alert tone and a colour.
	snapshot.Systems[0].Talkgroups[1].GroupIDs = []uint64{3, 7, 2}
	snapshot.Groups[1].Alert = "alert5"
	snapshot.Groups[1].LED = "red"

	config := decodeConfig(t, ConfigPayload{Snapshot: snapshot})

	groupsData := listOf(t, config, "groupsData")

	if len(groupsData) != 5 {
		t.Fatalf("the listing carries %d groups, want Air, Ground, Fire, EMS and Unknown", len(groupsData))
	}

	if got, want := jsonOf(t, groupsData), `[{"id":1,"label":"Air"},{"id":7,"label":"Ground"},{"id":3,"label":"Fire"},{"alert":"alert5","id":2,"label":"EMS","led":"red"},{"id":6,"label":"Unknown"}]`; got != want {
		t.Errorf("the listing =\n  %s\nwant\n  %s", got, want)
	}

	// A group no talkgroup belongs to stays out: the listing covers the
	// groups this listener's talkgroups carry, not the server's whole set.
	for _, entry := range groupsData {
		if label := objectOf(t, entry)["label"]; label == "Interop" || label == "Law" {
			t.Errorf("the listing carries %v, a group no talkgroup of this listener belongs to", label)
		}
	}

	// The group map already carried every label, and the widening leaves it as it is.
	if got, want := jsonOf(t, config["groups"]), `{"Air":{"10001":[1514200]},"EMS":{"10001":[1514100]},"Fire":{"10001":[1514100]},"Ground":{"10001":[1514000,1514100]},"Unknown":{"10002":[1515000]}}`; got != want {
		t.Errorf("the group map =\n  %s\nwant\n  %s", got, want)
	}
}

// TestConfigPayloadCarriesTheToneTablesTheServerOwns proves the server owns
// the tone tables and unknown brands get empty lists.
func TestConfigPayloadCarriesTheToneTablesTheServerOwns(t *testing.T) {
	snapshot := captureSnapshot()

	if alerts := decodeConfig(t, ConfigPayload{Snapshot: snapshot})["alerts"]; len(objectOf(t, alerts)) != 9 {
		t.Errorf("the alert table carries %d entries, want the nine the server carries", len(objectOf(t, alerts)))
	}

	for brand, want := range map[string]string{
		"uniden":   `{"activate":[{"begin":0,"end":0.05,"frequency":1200,"type":"square"}],"deactivate":[{"begin":0,"end":0.1,"frequency":1200,"type":"square"},{"begin":0.1,"end":0.2,"frequency":925,"type":"square"}],"denied":[{"begin":0,"end":0.05,"frequency":925,"type":"square"},{"begin":0.1,"end":0.15,"frequency":925,"type":"square"}]}`,
		"whistler": `{"activate":[{"begin":0,"end":0.05,"frequency":2000,"type":"triangle"}],"deactivate":[{"begin":0,"end":0.04,"frequency":1500,"type":"triangle"},{"begin":0.04,"end":0.08,"frequency":1400,"type":"triangle"}],"denied":[{"begin":0,"end":0.04,"frequency":1400,"type":"triangle"},{"begin":0.05,"end":0.09,"frequency":1400,"type":"triangle"},{"begin":0.1,"end":0.14,"frequency":1400,"type":"triangle"}]}`,
		"nobody":   `{"activate":[],"deactivate":[],"denied":[]}`,
	} {
		snapshot.Options.KeypadBeeps = brand

		if got := jsonOf(t, decodeConfig(t, ConfigPayload{Snapshot: snapshot})["keypadBeeps"]); got != want {
			t.Errorf("keypad beeps for %s = %s, want %s", brand, got, want)
		}
	}
}

// TestConfigPayloadCarriesOnlyTheOptionsAListenerIsTold proves the payload
// leaves out options that decide server behavior.
func TestConfigPayloadCarriesOnlyTheOptionsAListenerIsTold(t *testing.T) {
	snapshot := captureSnapshot()
	snapshot.Options.Secret = "a-secret"
	snapshot.Options.AdminPasswordHash = "a-hash"
	snapshot.Options.MaxClients = 200
	snapshot.Options.PruneDays = 7

	body := marshal(t, ConfigPayload{Snapshot: snapshot})

	for key, want := range map[string]string{
		"branding":           `"Capture Dispatch"`,
		"email":              `"dispatch@example.com"`,
		"showListenersCount": "true",
	} {
		if !carriesKey(t, body, key, want) {
			t.Errorf("%s = %s, want %s:%s", key, body, key, want)
		}
	}

	for _, key := range []string{"secret", "adminPassword", "maxClients", "pruneDays", "audioConversion", "autoPopulate", "sortTalkgroups", "duplicateDetectionTimeFrame", "disableDuplicateDetection", "dimmerDelay", "playbackGoesLive", "time12hFormat"} {
		if carriesKey(t, body, key, "") {
			t.Errorf("the listener payload carries %s: %s", key, body)
		}
	}
}

// TestConfigPayloadNamesAColourAtTheSystemLevel proves a system entry always names a
// colour a listener can light an LED with: the colour the system stores when the server
// knows the name, and green when it stores no colour or one the server does not know.
func TestConfigPayloadNamesAColourAtTheSystemLevel(t *testing.T) {
	snapshot := captureSnapshot()

	// Three systems in one payload, one answer each: a system that stores no
	// colour, one that stores a colour the server names, one that stores one it
	// does not.
	snapshot.Systems = append(snapshot.Systems, domain.System{
		ID: 3, SystemRef: 10003, Label: "Airport", Order: 3,
	})

	snapshot.Systems[0].LED = ""
	snapshot.Systems[1].LED = "red"
	snapshot.Systems[2].LED = "pink"

	config := decodeConfig(t, ConfigPayload{Snapshot: snapshot})

	for _, tc := range []struct {
		label string
		want  string
	}{
		{label: "Grand Metro", want: "green"},
		{label: "Harbour", want: "red"},
		{label: "Airport", want: "green"},
	} {
		if got := ledOf(t, systemEntryOf(t, config, tc.label), "the "+tc.label+" system entry"); got != tc.want {
			t.Errorf("the %q system entry carries led %q, want %q", tc.label, got, tc.want)
		}
	}
}

// TestConfigPayloadNamesTheResolvedColourForATalkgroupEntry proves a talkgroup entry names
// the colour the server resolves for that talkgroup — its group, then its tag, then its
// system, then the talkgroup itself, skipping a candidate the server does not know — and
// that the system candidate is the colour the system stores and never the green that
// system's own entry answers.
func TestConfigPayloadNamesTheResolvedColourForATalkgroupEntry(t *testing.T) {
	for _, tc := range []struct {
		name       string
		group      string
		tag        string
		system     string
		talkgroup  string
		wantEntry  string
		wantSystem string
	}{
		{name: "nothing configured at all", wantEntry: "green", wantSystem: "green"},
		{name: "the group alone", group: "red", wantEntry: "red", wantSystem: "green"},
		{name: "the tag alone", tag: "red", wantEntry: "red", wantSystem: "green"},
		{name: "the system alone", system: "red", wantEntry: "red", wantSystem: "red"},
		{name: "the talkgroup alone, beside a system entry that answers the default", talkgroup: "red", wantEntry: "red", wantSystem: "green"},
		{name: "the group over the tag", group: "red", tag: "white", wantEntry: "red", wantSystem: "green"},
		{name: "the group over the system", group: "red", system: "magenta", wantEntry: "red", wantSystem: "magenta"},
		{name: "the group over the talkgroup", group: "red", talkgroup: "yellow", wantEntry: "red", wantSystem: "green"},
		{name: "the tag over the system", tag: "white", system: "magenta", wantEntry: "white", wantSystem: "magenta"},
		{name: "the tag over the talkgroup", tag: "white", talkgroup: "yellow", wantEntry: "white", wantSystem: "green"},
		{name: "the system over the talkgroup", system: "magenta", talkgroup: "yellow", wantEntry: "magenta", wantSystem: "magenta"},
		{name: "every level coloured", group: "red", tag: "white", system: "magenta", talkgroup: "yellow", wantEntry: "red", wantSystem: "magenta"},
		{name: "the talkgroup stores a name the server does not know", talkgroup: "pink", wantEntry: "green", wantSystem: "green"},
		{name: "the group stores a name the server does not know, past which the talkgroup answers", group: "pink", talkgroup: "red", wantEntry: "red", wantSystem: "green"},
		{name: "the tag stores a name the server does not know, past which the talkgroup answers", tag: "pink", talkgroup: "red", wantEntry: "red", wantSystem: "green"},
		{name: "the group stores a name the server does not know, past which the tag answers", group: "pink", tag: "blue", wantEntry: "blue", wantSystem: "green"},
		{name: "the system stores a name the server does not know", system: "pink", wantEntry: "green", wantSystem: "green"},
		{name: "the system stores a name the server does not know, past which the talkgroup answers", system: "pink", talkgroup: "red", wantEntry: "red", wantSystem: "green"},
		{name: "no candidate anywhere is one the server knows", group: "pink", tag: "pink", system: "pink", talkgroup: "pink", wantEntry: "green", wantSystem: "green"},
	} {
		snapshot := ladderSnapshot()

		snapshot.Groups[0].LED = tc.group
		snapshot.Tags[0].LED = tc.tag
		snapshot.Systems[0].LED = tc.system
		snapshot.Systems[0].Talkgroups[0].LED = tc.talkgroup

		config := decodeConfig(t, ConfigPayload{Snapshot: snapshot})

		if got := ledOf(t, talkgroupEntryOf(t, config, 3000100), "the talkgroup entry"); got != tc.wantEntry {
			t.Errorf("%s: the talkgroup entry carries led %q, want %q", tc.name, got, tc.wantEntry)
		}

		if got := ledOf(t, systemEntryOf(t, config, "Ladder"), "the Ladder system entry"); got != tc.wantSystem {
			t.Errorf("%s: the system entry carries led %q, want %q", tc.name, got, tc.wantSystem)
		}
	}
}

// TestConfigPayloadResolvesATalkgroupsColourFromTheGroupListing proves the group
// candidates are walked in the order the payload's own group listing puts them: for a group
// several talkgroups belong to, that is where the group first appears anywhere, which is not
// the place a given talkgroup's own entry happens to list it in.
func TestConfigPayloadResolvesATalkgroupsColourFromTheGroupListing(t *testing.T) {
	config := decodeConfig(t, ConfigPayload{Snapshot: sharedGroupSnapshot()})

	if got, want := jsonOf(t, config["groupsData"]), `[{"id":11,"label":"Shared","led":"blue"},{"id":12,"label":"Own","led":"red"}]`; got != want {
		t.Fatalf("the group listing =\n  %s\nwant\n  %s", got, want)
	}

	// Bravo belongs to both groups and names Own first in its own entry, while
	// Alpha, which comes first in the payload, belongs only to Shared.
	entry := talkgroupEntryOf(t, config, 3000200)

	if got, want := jsonOf(t, entry["groups"]), `["Own","Shared"]`; got != want {
		t.Errorf("the talkgroup entry lists its groups as %s, want %s: the order that talkgroup's own record carries them in", got, want)
	}

	if entry["group"] != "Own" {
		t.Errorf("the talkgroup entry names %v as its group, want Own, the first label of its own record", entry["group"])
	}

	if got := ledOf(t, entry, "the Bravo talkgroup entry"); got != "blue" {
		t.Errorf("the talkgroup entry carries led %q, want blue: Shared is the first coloured group in the listing, ahead of Own, which that entry lists first", got)
	}

	if got, want := ledOf(t, talkgroupEntryOf(t, config, 3000100), "the Alpha talkgroup entry"), "blue"; got != want {
		t.Errorf("the shared group's other talkgroup carries led %q, want %q", got, want)
	}
}

// TestConfigPayloadWritesTheGroupAndTagListingsAsTheServerStoresThem pins the two listings
// this feature leaves alone: a group or tag with no colour carries no led key at all, and a
// stored name the server does not know reaches the listing exactly as it was written.
func TestConfigPayloadWritesTheGroupAndTagListingsAsTheServerStoresThem(t *testing.T) {
	snapshot := captureSnapshot()

	snapshot.Groups[0].LED = "pink"   // Air, a name the server does not know
	snapshot.Groups[6].LED = "orange" // Ground
	snapshot.Groups[2].LED = ""       // Fire
	snapshot.Tags[0].LED = "pink"     // Air Traffic Control
	snapshot.Tags[2].LED = ""         // Fire Dispatch

	entries := map[string]map[string]any{}

	for _, key := range []string{"groupsData", "tagsData"} {
		for _, raw := range listOf(t, decodeConfig(t, ConfigPayload{Snapshot: snapshot}), key) {
			entry := objectOf(t, raw)

			label, ok := entry["label"].(string)
			if !ok {
				t.Fatalf("a %s entry has label %#v, want a name", key, entry["label"])
			}

			entries[label] = entry
		}
	}

	for _, tc := range []struct {
		label string
		want  string
	}{
		{label: "Air", want: "pink"},
		{label: "Ground", want: "orange"},
		{label: "Fire", want: ""},
		{label: "Air Traffic Control", want: "pink"},
		{label: "Fire Dispatch", want: ""},
	} {
		entry, ok := entries[tc.label]
		if !ok {
			t.Fatalf("the listings carry no entry for %q, which a talkgroup of this listener belongs to", tc.label)
		}

		value, present := entry["led"]

		if tc.want == "" {
			if present {
				t.Errorf("the %q listing entry carries led %v, want no led key at all", tc.label, value)
			}

			continue
		}

		if !present {
			t.Errorf("the %q listing entry carries no led key, want %q", tc.label, tc.want)
		} else if value != tc.want {
			t.Errorf("the %q listing entry carries led %v, want %q", tc.label, value, tc.want)
		}
	}
}

// ladderSnapshot is a configuration small enough for one colour rule to be read in it: one
// system, one talkgroup under it, and that talkgroup's one group and one tag. The tests over
// it set one colour per level.
func ladderSnapshot() ports.ConfigSnapshot {
	return ports.ConfigSnapshot{
		Groups: domain.Groups{{ID: 11, Label: "Ladder Group"}},
		Tags:   domain.Tags{{ID: 21, Label: "Ladder Tag"}},
		Systems: domain.Systems{
			{
				ID: 31, SystemRef: 20001, Label: "Ladder", Order: 1,
				Talkgroups: domain.Talkgroups{
					{ID: 41, TalkgroupRef: 3000100, Label: "Ladder Talkgroup", Name: "Ladder 1", GroupIDs: []uint64{11}, TagID: 21},
				},
			},
		},
		Options: domain.Options{Branding: "Ladder", SortTalkgroups: true},
	}
}

// sharedGroupSnapshot is two systems whose talkgroups both belong to Shared, which the
// payload's group listing therefore sees first, while the second talkgroup's own record
// names its own group first.
func sharedGroupSnapshot() ports.ConfigSnapshot {
	return ports.ConfigSnapshot{
		Groups: domain.Groups{
			{ID: 11, Label: "Shared", LED: "blue"},
			{ID: 12, Label: "Own", LED: "red"},
		},
		Tags: domain.Tags{{ID: 21, Label: "Shared Tag"}},
		Systems: domain.Systems{
			{
				ID: 31, SystemRef: 20001, Label: "First", Order: 1,
				Talkgroups: domain.Talkgroups{
					{ID: 41, TalkgroupRef: 3000100, Label: "Alpha", Name: "Alpha 1", GroupIDs: []uint64{11}, TagID: 21},
				},
			},
			{
				ID: 32, SystemRef: 20002, Label: "Second", Order: 2,
				Talkgroups: domain.Talkgroups{
					{ID: 42, TalkgroupRef: 3000200, Label: "Bravo", Name: "Bravo 1", GroupIDs: []uint64{12, 11}, TagID: 21},
				},
			},
		},
		Options: domain.Options{Branding: "Shared Groups", SortTalkgroups: true},
	}
}

// captureSnapshot is the configuration the capture script put on the server, as the server holds it
// after reading it back out of its own database.
//
// The group memberships come back in the order the server's query produces them, which is by group
// identifier and not in the order the configuration named them: the capture asked for groups 7 and 3
// on talkgroup 1514100, and the captured configuration says Fire and then Ground.
func captureSnapshot() ports.ConfigSnapshot {
	return ports.ConfigSnapshot{
		Groups: domain.Groups{
			{ID: 1, Label: "Air"},
			{ID: 2, Label: "EMS"},
			{ID: 3, Label: "Fire"},
			{ID: 4, Label: "Interop"},
			{ID: 5, Label: "Law"},
			{ID: 6, Label: "Unknown"},
			{ID: 7, Label: "Ground"},
		},
		Tags: domain.Tags{
			{ID: 1, Label: "Air Traffic Control"},
			{ID: 2, Label: "Emergency "},
			{ID: 3, Label: "Fire Dispatch"},
			{ID: 4, Label: "Fire Tac"},
			{ID: 5, Label: "Fire Talk"},
			{ID: 6, Label: "Interop"},
			{ID: 7, Label: "Security"},
			{ID: 8, Label: "Service"},
			{ID: 9, Label: "Untagged"},
			{ID: 10, Label: "Capture Tag"},
		},
		Systems: domain.Systems{
			{
				ID: 1, SystemRef: 10001, Label: "Grand Metro", Order: 1,
				Talkgroups: domain.Talkgroups{
					{ID: 1, TalkgroupRef: 1514000, Label: "Ground", Name: "Dispatch 1", Kind: "Aggregate", Frequency: 151400000, GroupIDs: []uint64{7}, TagID: 3},
					{ID: 2, TalkgroupRef: 1514100, Label: "Ground", Name: "Dispatch 2", GroupIDs: []uint64{3, 7}, TagID: 4},
					{ID: 3, TalkgroupRef: 1514200, Label: "Air", Name: "Air 1", GroupIDs: []uint64{1}, TagID: 1},
				},
				Units: domain.Units{
					{ID: 1, UnitRef: 1001, Label: "Car 1"},
					{ID: 2, UnitRef: 1002, Label: "Car 2"},
				},
				Sites: domain.Sites{
					{ID: 1, SiteRef: 101, Label: "Metro East", SystemID: 1},
					{ID: 2, SiteRef: 102, Label: "Metro West", SystemID: 1},
				},
			},
			{
				ID: 2, SystemRef: 10002, Label: "Harbour", Order: 2,
				Talkgroups: domain.Talkgroups{
					{ID: 4, TalkgroupRef: 1515000, Label: "Marine", Name: "Marine 16", GroupIDs: []uint64{6}, TagID: 9},
				},
			},
		},
		Options: domain.Options{
			Branding:           "Capture Dispatch",
			Email:              "dispatch@example.com",
			KeypadBeeps:        "uniden",
			ShowListenersCount: true,
			SortTalkgroups:     true,
		},
	}
}

// decodeConfig marshals a configuration and reads it back as JSON values, which is how the tests
// below look inside it without re-deciding anything about how it was written.
func decodeConfig(t *testing.T, payload ConfigPayload) map[string]any {
	t.Helper()

	var config map[string]any

	decoder := json.NewDecoder(strings.NewReader(marshal(t, payload)))
	decoder.UseNumber()

	if err := decoder.Decode(&config); err != nil {
		t.Fatalf("reading back the config payload: %v", err)
	}

	return config
}

func listOf(t *testing.T, from any, key string) []any {
	t.Helper()

	entry, ok := from.(map[string]any)
	if !ok {
		t.Fatalf("%#v is not an object", from)
	}

	list, ok := entry[key].([]any)
	if !ok {
		t.Fatalf("%s is %#v, want a list", key, entry[key])
	}

	return list
}

func objectOf(t *testing.T, from any) map[string]any {
	t.Helper()

	entry, ok := from.(map[string]any)
	if !ok {
		t.Fatalf("%#v is not an object", from)
	}

	return entry
}

func jsonOf(t *testing.T, from any) string {
	t.Helper()

	b, err := json.Marshal(from)
	if err != nil {
		t.Fatalf("marshaling %#v: %v", from, err)
	}

	return string(b)
}

// talkgroupRefs lists the talkgroup references a configuration carries, in the order it carries them.
func talkgroupRefs(t *testing.T, payload ConfigPayload) []uint {
	t.Helper()

	return talkgroupField(t, payload, "id")
}

func talkgroupOrders(t *testing.T, payload ConfigPayload) []uint {
	t.Helper()

	return talkgroupField(t, payload, "order")
}

func talkgroupField(t *testing.T, payload ConfigPayload, key string) []uint {
	t.Helper()

	out := []uint{}

	for _, system := range listOf(t, decodeConfig(t, payload), "systems") {
		for _, talkgroup := range listOf(t, system, "talkgroups") {
			entry := objectOf(t, talkgroup)

			number, ok := entry[key].(json.Number)
			if !ok {
				continue
			}

			value, err := number.Int64()
			if err != nil {
				t.Fatalf("talkgroup %s = %s, want a whole number", key, number)
			}

			out = append(out, uint(value))
		}
	}

	return out
}

func sameRefs(got []uint, want []uint) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}

// systemEntryOf finds the system entry the payload carries under a given label.
func systemEntryOf(t *testing.T, config map[string]any, label string) map[string]any {
	t.Helper()

	for _, raw := range listOf(t, config, "systems") {
		entry := objectOf(t, raw)

		if entry["label"] == label {
			return entry
		}
	}

	t.Fatalf("the payload carries no system named %q", label)

	return nil
}

// talkgroupEntryOf finds the talkgroup entry the payload carries for a given reference.
func talkgroupEntryOf(t *testing.T, config map[string]any, ref uint) map[string]any {
	t.Helper()

	for _, system := range listOf(t, config, "systems") {
		for _, raw := range listOf(t, system, "talkgroups") {
			entry := objectOf(t, raw)

			number, ok := entry["id"].(json.Number)
			if !ok {
				t.Fatalf("a talkgroup entry's id is %#v, want a number", entry["id"])
			}

			value, err := number.Int64()
			if err != nil {
				t.Fatalf("a talkgroup entry's id is %s, want a whole number", number)
			}

			if uint(value) == ref {
				return entry
			}
		}
	}

	t.Fatalf("the payload carries no talkgroup %d", ref)

	return nil
}

// ledOf reads the colour an entry carries and names the entry it came from, which is how the
// colour tests say which level answered wrong.
func ledOf(t *testing.T, entry map[string]any, from string) string {
	t.Helper()

	value, ok := entry["led"].(string)
	if !ok {
		t.Fatalf("%s carries led %#v, want a colour name", from, entry["led"])
	}

	return value
}
