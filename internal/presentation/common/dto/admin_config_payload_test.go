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
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// TestAdminConfigPayloadMatchesTheCapturedConfig compares the whole
// admin configuration byte for byte against the capture, rewriting
// only the site-system divergence.
func TestAdminConfigPayloadMatchesTheCapturedConfig(t *testing.T) {
	got := marshal(t, AdminConfigReply{Config: captureConfiguration(), PasswordNeedChange: true})
	want := capturedAnswer(t, "rest-config.json")

	want = replaceExactly(t, want, `"systemId":0`, `"systemId":1`, 2)

	if got != want {
		t.Errorf("admin config reply =\n  %s\nwant\n  %s", got, want)
	}
}

// TestAdminSectionPayloadsMatchTheCapturedSections proves each config
// section is also answered bare, as the list itself with no envelope.
func TestAdminSectionPayloadsMatchTheCapturedSections(t *testing.T) {
	configuration := captureConfiguration()

	for _, tc := range []struct {
		name    string
		golden  string
		payload any
	}{
		{name: "apikeys", golden: "rest-section-apikeys.json", payload: ApikeyListPayload(configuration.Apikeys)},
		{name: "groups", golden: "rest-section-groups.json", payload: GroupListPayload(configuration.Snapshot.Groups)},
		{name: "options", golden: "rest-section-options.json", payload: OptionsPayload(configuration.Snapshot.Options)},
		{name: "systems", golden: "rest-section-systems.json", payload: SystemListPayload(configuration.Snapshot.Systems)},
		{name: "tags", golden: "rest-section-tags.json", payload: TagListPayload(configuration.Snapshot.Tags)},
		{name: "dirwatches", golden: "rest-section-dirwatch.json", payload: DirwatchListPayload(configuration.Dirwatches)},
		{name: "downstreams", golden: "rest-section-downstreams.json", payload: DownstreamListPayload(configuration.Downstreams)},
		{name: "accesses", golden: "rest-section-access.json", payload: AccessListPayload(nil)},
	} {
		got := marshal(t, tc.payload)
		want := capturedAnswer(t, tc.golden)

		if tc.name == "systems" {
			want = replaceExactly(t, want, `"systemId":0`, `"systemId":1`, 2)
		}

		if got != want {
			t.Errorf("%s section =\n  %s\nwant\n  %s", tc.name, got, want)
		}
	}
}

// TestAdminAccessSectionMatchesTheCapturedAccesses proves expiry is
// answered as an instant, not seconds, and a never-expiring access
// omits expiry rather than answering the epoch.
func TestAdminAccessSectionMatchesTheCapturedAccesses(t *testing.T) {
	open, _ := domain.ParseAccessScope("*")
	scoped, _ := domain.ParseAccessScope([]any{map[string]any{"id": float64(10001), "talkgroups": "*"}})

	payload := AccessListPayload([]domain.Access{
		{ID: 1, Code: "capture-open", Ident: "Capture open", Scope: open},
		{ID: 2, Code: "capture-scoped", Ident: "Capture scoped", Scope: scoped},
		{ID: 3, Code: "capture-expired", Ident: "Capture expired", Expiration: 1, Scope: open},
		{ID: 4, Code: "capture-limited", Ident: "Capture limited", Limit: 1, Scope: open},
	})

	got := marshal(t, payload)

	// An expiry renders in the server's time zone, as the legacy server
	// reached it through time.Unix; the capture states the moment in UTC.
	want := replaceExactly(t, capturedAnswer(t, "rest-section-access-restricted.json"),
		`"1970-01-01T00:00:01Z"`, `"`+time.Unix(1, 0).Format(time.RFC3339)+`"`, 1)

	if got != want {
		t.Errorf("access section =\n  %s\nwant\n  %s", got, want)
	}
}

// TestAdminTalkgroupWritesItsOrderUnderTheTalkgroupKey proves the
// display order arrives under key 'talkgroup', the legacy key clients
// keep reading.
func TestAdminTalkgroupWritesItsOrderUnderTheTalkgroupKey(t *testing.T) {
	got := marshal(t, SystemListPayload(domain.Systems{
		{
			ID: 1, SystemRef: 10001, Label: "Grand Metro",
			Talkgroups: domain.Talkgroups{{ID: 1, TalkgroupRef: 1514000, Label: "Ground", Name: "Dispatch 1", Order: 4}},
		},
	}))

	if !strings.Contains(got, `"talkgroup":4`) {
		t.Errorf("talkgroup with an order of 4 = %s, want the order written under the talkgroup key", got)
	}

	if strings.Contains(got, `"order":4`) {
		t.Errorf("talkgroup with an order of 4 = %s, want no order key: the legacy server never writes one", got)
	}
}

// TestAdminSiteReportsTheSystemItIsOn proves a site is reported as
// belonging to the system it is stored on, where the legacy server
// reported none.
func TestAdminSiteReportsTheSystemItIsOn(t *testing.T) {
	got := marshal(t, SystemListPayload(domain.Systems{
		{
			ID: 7, SystemRef: 10001, Label: "Grand Metro",
			Sites: domain.Sites{{ID: 1, SiteRef: 101, Label: "Metro East", SystemID: 7}},
		},
	}))

	if !strings.Contains(got, `"systemId":7`) {
		t.Errorf("site = %s, want the system it is stored under", got)
	}
}

// TestAdminOptionsCarryNoPasswordAndNoSecret proves options carry no secret and no
// password.
func TestAdminOptionsCarryNoPasswordAndNoSecret(t *testing.T) {
	options := captureConfiguration().Snapshot.Options

	options.AdminPasswordHash = "a-bcrypt-hash"
	options.Secret = "a-signing-secret"

	got := marshal(t, OptionsPayload(options))

	for _, key := range []string{"adminPassword", "secret", "passwordNeedChange"} {
		if strings.Contains(got, `"`+key+`"`) {
			t.Errorf("the options payload carries %s: %s", key, got)
		}
	}

	want := `{"audioConversion":0,"autoPopulate":false,"branding":"Capture Dispatch",` +
		`"disableDuplicateDetection":true,"email":"dispatch@example.com",` +
		`"keypadBeeps":"uniden","maxClients":200,"pruneDays":7,` +
		`"showListenersCount":true,"sortTalkgroups":true}`

	if got != want {
		t.Errorf("options payload =\n  %s\nwant\n  %s", got, want)
	}
}

// TestAdminConfigReplyMentionsTheContainerOnlyWhenToldTo proves the
// container flag appears only when DOCKER was in the environment.
func TestAdminConfigReplyMentionsTheContainerOnlyWhenToldTo(t *testing.T) {
	configuration := captureConfiguration()

	got := marshal(t, AdminConfigReply{Config: configuration, PasswordNeedChange: true})

	if strings.Contains(got, `"docker"`) {
		t.Errorf("config reply = %s, want no mention of a container", got)
	}

	got = marshal(t, AdminConfigReply{Config: configuration, Docker: true, PasswordNeedChange: true})

	if !strings.Contains(got, `"docker":true`) {
		t.Errorf("config reply = %s, want the container flag", got)
	}
}

// TestAdminUnitListingCarriesOnlyTheUnitsAMaintenanceRunLeftStanding proves the same account through the
// listing an administrator is shown for one system: the two rows a maintenance run removes are gone from the
// listing, the entries of the units it left standing keep the bytes and the order the same builder wrote for
// them while those rows were still stored, and the listing built after the run is the bytes the captured
// admin section carries for that system's units.
func TestAdminUnitListingCarriesOnlyTheUnitsAMaintenanceRunLeftStanding(t *testing.T) {
	kept := migrationKeptUnits()

	before := marshal(t, UnitListPayload(append(append(domain.Units{}, kept...), migrationDamageUnits...)))
	after := marshal(t, UnitListPayload(kept))

	if golden := capturedAnswer(t, "rest-section-systems.json"); !strings.Contains(golden, after) {
		t.Errorf("the unit listing a run leaves behind =\n  %s\nis not the bytes the captured admin section carries for that system's units:\n  %s", after, golden)
	}

	if got, want := strings.Count(before, `{"id":`), len(kept)+len(migrationDamageUnits); got != want {
		t.Errorf("the unit listing built while the rows stand carries %d entries, want the %d the deployment named plus the %d the migration left",
			got, want, len(migrationDamageUnits))
	}

	if got, want := strings.Count(after, `{"id":`), len(kept); got != want {
		t.Errorf("the unit listing built after the run carries %d entries, want the %d it left standing", got, want)
	}

	// The surviving entries are the first bytes of the listing built while the rows stood, in the same
	// order: a run takes rows out of the catalog, it does not re-write what is shown of what remains.
	if head := strings.TrimSuffix(after, "]"); !strings.HasPrefix(before, head) {
		t.Errorf("the listing built while the rows stand does not start with the units a run leaves standing:\n  %s\nwant it to start with\n  %s", before, head)
	}

	for _, entry := range unitEntryBytes(t, migrationDamageUnits) {
		if !strings.Contains(before, entry) {
			t.Errorf("the listing built while the rows stand does not carry %s:\n  %s", entry, before)
		}

		if strings.Contains(after, entry) {
			t.Errorf("the listing built after the run still carries %s:\n  %s", entry, after)
		}
	}

	if want := cutEntryBytes(t, before, unitEntryBytes(t, migrationDamageUnits)); want != after {
		t.Errorf("the listing built after the run =\n  %s\nwant the listing built while the rows stood with only the removed entries cut out:\n  %s", after, want)
	}
}

// captureConfiguration is the whole configuration the capture script put on the server, as the server
// answers it after reading every part back out of its own database.
func captureConfiguration() AdminConfigPayload {
	snapshot := captureSnapshot()

	snapshot.Options.AudioConversion = 0
	snapshot.Options.DisableDuplicateDetection = true
	snapshot.Options.MaxClients = 200
	snapshot.Options.PruneDays = 7

	open, _ := domain.ParseAccessScope("*")
	scoped, _ := domain.ParseAccessScope([]any{map[string]any{"id": float64(10001), "talkgroups": "*"}})
	oneTalkgroup, _ := domain.ParseAccessScope([]any{map[string]any{"id": float64(10001), "talkgroups": []any{float64(1514000)}}})

	return AdminConfigPayload{
		Accesses: []domain.Access{},
		Apikeys: []domain.Apikey{
			{ID: 1, Ident: "Capture open", Key: "capture-apikey-open", Scope: open},
			{ID: 2, Ident: "Capture scoped", Key: "capture-apikey-scoped", Scope: scoped},
		},
		Dirwatches: []domain.Dirwatch{
			{
				ID: 1, Delay: 1000, DeleteAfter: true, Directory: "/tmp/opencode/capture-watch",
				Disabled: true, Extension: ".wav", Frequency: 151400000, Kind: "default",
				Mask: "#S#TG#YYYY-MM-DDTHH-mm-ss", Order: 1, SiteID: 1, SystemID: 1, TalkgroupID: 1,
			},
		},
		Downstreams: []domain.Downstream{
			{
				ID: 1, Apikey: "capture-apikey-open", Disabled: true, Order: 1,
				Scope: oneTalkgroup, URL: "http://127.0.0.1:39999/api/call-upload",
			},
		},
		Snapshot: snapshot,
		Version:  "8.0.0",
	}
}

// unitEntryBytes writes each unit's own listing entry, on its own, so a test can name the entries it cuts
// out of a longer listing byte for byte.
func unitEntryBytes(t *testing.T, units domain.Units) []string {
	t.Helper()

	out := make([]string, 0, len(units))

	for _, unit := range units {
		body := marshal(t, UnitListPayload(domain.Units{unit}))

		if body == "[]" {
			t.Fatalf("the unit listing carries no entry for unit %q, which the same builder wrote for it when asked for the whole system", unit.Label)
		}

		out = append(out, strings.TrimSuffix(strings.TrimPrefix(body, "["), "]"))
	}

	return out
}

// replaceExactly rewrites a captured payload and insists that it found exactly the number of places it
// expected to. A divergence that quietly stopped applying would otherwise keep passing.
func replaceExactly(t *testing.T, body, from, to string, times int) string {
	t.Helper()

	if n := strings.Count(body, from); n != times {
		t.Fatalf("the captured payload says %q %d times, want %d: the divergence this test states is no "+
			"longer the one the capture recorded", from, n, times)
	}

	return strings.ReplaceAll(body, from, to)
}
