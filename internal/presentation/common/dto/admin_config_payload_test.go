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
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// TestAdminConfigPayloadMatchesTheCapturedConfig is the byte-for-byte half of the criterion for the whole
// configuration an administrator is shown.
//
// The captured configuration says something this server does not: it reports every site as belonging
// to no system, because the legacy server never reads that column back. This server reports the system
// the site is on. That is the approved divergence, and the
// test states it by rewriting the capture rather than by skipping the comparison.
func TestAdminConfigPayloadMatchesTheCapturedConfig(t *testing.T) {
	got := marshal(t, AdminConfigReply{Config: captureConfiguration(), PasswordNeedChange: true})
	want := capturedAnswer(t, "rest-config.json")

	want = replaceExactly(t, want, `"systemId":0`, `"systemId":1`, 2)

	if got != want {
		t.Errorf("admin config reply =\n  %s\nwant\n  %s", got, want)
	}
}

// Each section of the configuration is also answered on its own, and answered as the list itself with
// no envelope around it.
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

// The access codes are answered with their expiry as a moment and not as the number of seconds the
// server stores it as, and an access that never expires is answered with no expiry at all rather than
// with the epoch.
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

	// An expiry is rendered in whatever time zone the server runs in, because the legacy server reaches
	// it through time.Unix. The capture was taken on a machine set to UTC and says the moment in Z; a
	// server in another zone says the same moment in its own offset, and an administrator sees the same
	// thing they see today.
	want := replaceExactly(t, capturedAnswer(t, "rest-section-access-restricted.json"),
		`"1970-01-01T00:00:01Z"`, `"`+time.Unix(1, 0).Format(time.RFC3339)+`"`, 1)

	if got != want {
		t.Errorf("access section =\n  %s\nwant\n  %s", got, want)
	}
}

// A talkgroup's display order is written under the key `talkgroup` and not under the key `order`,
// which is what the legacy server writes and what the administration interface has to correct for itself
// before it can show a talkgroup list in order. It is preserved because clients that do not correct for
// it are reading the number from the key it is really written under.
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

// A site is reported as belonging to the system it is stored under. The legacy server reports every
// site as belonging to no system at all, because the query that reads sites never asks for that column.
// This is the divergence the capture comparison above states out loud.
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

// The configuration an administrator is shown carries the options that decide what the server does, and
// never the password it stores or the secret it signs its session tokens with.
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

// The server says it runs in a container when it was started with DOCKER in its environment, and says
// nothing at all about containers otherwise.
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
