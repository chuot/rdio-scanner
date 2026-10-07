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

package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// TestConfigPayloadMatchesTheCapturedConfig is the byte-for-byte half of CA-004.1 for a
// configuration: what an unrestricted listener is told about a server, as the legacy server told it.
func TestConfigPayloadMatchesTheCapturedConfig(t *testing.T) {
	got := marshal(t, ConfigPayload{Snapshot: captureSnapshot()})
	want := readGolden(t, "payload-cfg.json")

	if got != want {
		t.Errorf("config payload =\n  %s\nwant\n  %s", got, want)
	}
}

// TestConfigPayloadMatchesTheCapturedScopedConfig is the same for a listener whose access code names
// one system: the systems list narrows, and so do the groups, the tags, and the data behind them,
// because all four are built from what this listener was shown and not from what the server has.
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
	want := readGolden(t, "payload-cfg-scoped.json")

	if got != want {
		t.Errorf("scoped config payload =\n  %s\nwant\n  %s", got, want)
	}
}

// The talkgroups option decides how a system's talkgroups are ordered and numbered. With the option
// on they come in label order and are numbered from one; with it off they come in the order the server
// keeps them and keep the number the server stores, which for a talkgroup that was never given one is
// zero.
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

// A talkgroup whose tag the server no longer knows is not shown, and a system left with nothing under
// it is still shown. A listener that asks for a system and gets it empty is being told the truth about
// a server that has lost the tags its talkgroups pointed at.
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

// The groups and tags a listener is given are the ones its own systems and talkgroups use, not every
// group and tag the server has.
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

// The alert tones are a table the server carries and not something an administrator configures, and
// the keypad beeps are one of three tables picked by an option. An option naming a brand the server
// does not know is answered with three empty lists rather than with somebody else's beeps.
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

// A listener is given the options that change what it shows. The ones that decide what the server does
// — how long it keeps its history, how many clients it admits, the secret that signs administrator
// sessions, the password it stores — are not in this payload.
func TestConfigPayloadCarriesOnlyTheOptionsAListenerIsTold(t *testing.T) {
	snapshot := captureSnapshot()
	snapshot.Options.Secret = "a-secret"
	snapshot.Options.AdminPasswordHash = "a-hash"
	snapshot.Options.MaxClients = 200
	snapshot.Options.PruneDays = 7

	body := marshal(t, ConfigPayload{Snapshot: snapshot})

	for key, want := range map[string]string{
		"branding":           `"Capture Dispatch"`,
		"dimmerDelay":        "5000",
		"email":              `"dispatch@example.com"`,
		"playbackGoesLive":   "true",
		"showListenersCount": "true",
		"time12hFormat":      "true",
	} {
		if !carriesKey(t, body, key, want) {
			t.Errorf("%s = %s, want %s:%s", key, body, key, want)
		}
	}

	for _, key := range []string{"secret", "adminPassword", "maxClients", "pruneDays", "audioConversion", "autoPopulate", "sortTalkgroups", "duplicateDetectionTimeFrame", "disableDuplicateDetection"} {
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
			DimmerDelay:        5000,
			Email:              "dispatch@example.com",
			KeypadBeeps:        "uniden",
			PlaybackGoesLive:   true,
			ShowListenersCount: true,
			SortTalkgroups:     true,
			Time12hFormat:      true,
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
