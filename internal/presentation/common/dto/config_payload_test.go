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
// see the configuration-payload-field-order maintainer note.
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
// see the configuration-payload-field-order maintainer note.
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
// group and tag lists. see the configuration-payload-field-order maintainer note.
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
// the tone tables and unknown brands get empty lists. see the
// tones-fixtures-and-the-golden-capture-contract maintainer note.
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
// leaves out options that decide server behavior. see the
// two-configuration-payload-shapes maintainer note.
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
