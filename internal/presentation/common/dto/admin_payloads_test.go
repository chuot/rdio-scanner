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

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The administrator's own endpoints answer with payloads of their own,
// and the legacy server captures are compared byte for byte.

func TestAdminLoginReplyMatchesTheCapturedLogin(t *testing.T) {
	// The needs-changing flag is carried from the use case, true on every
	// accepted login, as the capture shows the legacy server answering.
	// The captured credential is a placeholder: it differs per capture run.
	got := marshal(t, AdminLoginReply{PasswordNeedChange: true, Token: "capture-token"})
	want := replaceInOrder(t, capturedAnswer(t, "rest-login.json"), "<token>", "capture-token")

	if got != want {
		t.Errorf("admin login reply =\n  %s\nwant\n  %s", got, want)
	}
}

// TestAdminPasswordReplyMatchesTheCapturedPasswordChange proves a
// password change answers with only the one flag the page acts on.
func TestAdminPasswordReplyMatchesTheCapturedPasswordChange(t *testing.T) {
	got := marshal(t, AdminPasswordReply{PasswordNeedChange: false})
	want := capturedAnswer(t, "rest-password.json")

	if got != want {
		t.Errorf("admin password reply =\n  %s\nwant\n  %s", got, want)
	}
}

// TestAlertsPayloadMatchesTheCapturedAlerts proves the nine alert
// sequences are fixed builtins, not stored configuration.
func TestAlertsPayloadMatchesTheCapturedAlerts(t *testing.T) {
	got := marshal(t, Alerts())
	want := capturedAnswer(t, "rest-alerts.json")

	if got != want {
		t.Errorf("alerts payload =\n  %s\nwant\n  %s", got, want)
	}
}

// TestAdminLogsPagePayloadMatchesTheCapturedLogView proves the log
// view answers the total match count, the match span, the echoed
// criteria, the per-subsystem counts, and this page's entries.
// see the call-search-echoes-and-status-shortcuts maintainer note.
func TestAdminLogsPagePayloadMatchesTheCapturedLogView(t *testing.T) {
	first := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	second := first.Add(time.Second)
	third := first.Add(2 * time.Second)

	page := ports.LogSearchPage{
		Count:     3,
		DateStart: first,
		DateStop:  third,
		SourceCounts: []ports.LogSourceCount{
			{Source: domain.LogSourceConfig, Count: 2},
			{Source: domain.LogSourceLegacy, Count: 1},
		},
		Results: []domain.LogEntry{
			{ID: 1, DateTime: first, Level: domain.LogLevelWarn, Source: domain.LogSourceLegacy, Message: "server started"},
			{ID: 2, DateTime: second, Level: domain.LogLevelWarn, Source: domain.LogSourceConfig, Message: "the groups configuration changed"},
			{ID: 3, DateTime: third, Level: domain.LogLevelWarn, Source: domain.LogSourceConfig, Message: "the options configuration changed"},
		},
	}

	got := marshal(t, AdminLogsPage(page, map[string]any{
		"level":  "warn",
		"limit":  float64(200),
		"offset": float64(0),
		"sort":   float64(0),
	}))

	want := replaceInOrder(t, capturedAnswer(t, "rest-logs.json"), "<datetime>",
		"2026-05-11T09:00:00Z",
		"2026-05-11T09:00:02Z",
		"2026-05-11T09:00:00Z",
		"2026-05-11T09:00:01Z",
		"2026-05-11T09:00:02Z",
	)

	if got != want {
		t.Errorf("admin logs page =\n  %s\nwant\n  %s", got, want)
	}
}

// TestAdminLogsPagePayloadEchoesTheCriteriaTheSearchWasAskedWith
// proves criteria echo as submitted: unsubmitted criteria stay absent,
// and an overlarge limit echoes as asked, not as clamped.
// see the call-search-echoes-and-status-shortcuts maintainer note.
func TestAdminLogsPagePayloadEchoesTheCriteriaTheSearchWasAskedWith(t *testing.T) {
	page := ports.LogSearchPage{Count: 0, Results: []domain.LogEntry{}}

	got := marshal(t, AdminLogsPage(page, map[string]any{"limit": float64(600)}))
	want := `{"count":0,"dateStart":"0001-01-01T00:00:00Z","dateStop":"0001-01-01T00:00:00Z","options":{"limit":600},"sources":[],"logs":[]}`

	if got != want {
		t.Errorf("echoed criteria =\n  %s\nwant\n  %s", got, want)
	}

	got = marshal(t, AdminLogsPage(page, map[string]any{}))
	want = `{"count":0,"dateStart":"0001-01-01T00:00:00Z","dateStop":"0001-01-01T00:00:00Z","options":{},"sources":[],"logs":[]}`

	if got != want {
		t.Errorf("echoed criteria =\n  %s\nwant\n  %s", got, want)
	}
}

// TestAdminLogsPagePayloadEchoesTheDateItUnderstood proves a parsable
// date echoes as the instant it parsed to, and an unparsable one is
// not echoed at all.
func TestAdminLogsPagePayloadEchoesTheDateItUnderstood(t *testing.T) {
	page := ports.LogSearchPage{Count: 0, Results: []domain.LogEntry{}}

	got := marshal(t, AdminLogsPage(page, map[string]any{"date": "2026-05-11T09:00:00Z"}))

	if !strings.Contains(got, `"date":"2026-05-11T09:00:00Z"`) {
		t.Errorf("echoed criteria =\n  %s\nwant the date the search was asked with", got)
	}

	got = marshal(t, AdminLogsPage(page, map[string]any{"date": "not a date"}))

	if strings.Contains(got, `"date"`) {
		t.Errorf("echoed criteria =\n  %s\nwant no date at all: the search did not have one", got)
	}
}

// TestAdminLogsPagePayloadCarriesTheSubsystemThatWroteEachEntry
// proves every entry names the subsystem that wrote it, so the operator
// can narrow the page, between level and message as the contract says.
func TestAdminLogsPagePayloadCarriesTheSubsystemThatWroteEachEntry(t *testing.T) {
	at := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)

	page := ports.LogSearchPage{
		Count:     2,
		DateStart: at,
		DateStop:  at.Add(time.Second),
		Results: []domain.LogEntry{
			{
				ID:       1,
				DateTime: at,
				Level:    domain.LogLevelWarn,
				Source:   domain.LogSourceDirwatch,
				Message:  "the watcher for /data/incoming stopped: no such file or directory",
			},
			{
				ID:       2,
				DateTime: at.Add(time.Second),
				Level:    domain.LogLevelWarn,
				Source:   domain.LogSourceLogin,
				Message:  "a login attempt from 203.0.113.7 was refused",
			},
		},
	}

	got := marshal(t, AdminLogsPage(page, map[string]any{}))

	want := `{"count":2,"dateStart":"2026-05-11T09:00:00Z","dateStop":"2026-05-11T09:00:01Z","options":{},` +
		`"sources":[],` +
		`"logs":[` +
		`{"id":1,"dateTime":"2026-05-11T09:00:00Z","level":"warn","source":"dirwatch","message":"the watcher for /data/incoming stopped: no such file or directory"},` +
		`{"id":2,"dateTime":"2026-05-11T09:00:01Z","level":"warn","source":"login","message":"a login attempt from 203.0.113.7 was refused"}` +
		`]}`

	if got != want {
		t.Errorf("log view entries =\n  %s\nwant\n  %s", got, want)
	}
}

// TestAdminLogsPagePayloadReportsHowManyEntriesEachSubsystemWrote
// proves subsystem counts arrive on every search, describe the whole
// match, come most first, and a matchless search answers an empty list.
func TestAdminLogsPagePayloadReportsHowManyEntriesEachSubsystemWrote(t *testing.T) {
	page := ports.LogSearchPage{
		Count: 183,
		SourceCounts: []ports.LogSourceCount{
			{Source: domain.LogSourceDirwatch, Count: 137},
			{Source: domain.LogSourceIngest, Count: 41},
			{Source: domain.LogSourceLegacy, Count: 5},
		},
		Results: []domain.LogEntry{},
	}

	got := marshal(t, AdminLogsPage(page, map[string]any{}))

	if !strings.Contains(got, `"sources":[{"source":"dirwatch","count":137},{"source":"ingest","count":41},{"source":"legacy","count":5}]`) {
		t.Errorf("the per-subsystem counts =\n  %s\nwant the counts the search reported, most first, each naming "+
			"the subsystem and how many entries it wrote", got)
	}

	// The counts belong to the whole match, not the page, so they answer
	// even when the page is empty, as the operator decides what to narrow.
	if !strings.Contains(got, `"count":183`) {
		t.Errorf("the total =\n  %s\nwant 183, the total the criteria match", got)
	}

	got = marshal(t, AdminLogsPage(ports.LogSearchPage{Results: []domain.LogEntry{}}, map[string]any{}))

	if !strings.Contains(got, `"sources":[]`) {
		t.Errorf("the per-subsystem counts of a search that matches nothing =\n  %s\nwant an empty list", got)
	}
}

// TestAdminLogsPagePayloadEchoesTheSubsystemAndTheTextItWasAskedFor
// proves subsystem and text echo in the contract's order, and an
// unknown subsystem is not echoed.
// see the call-search-echoes-and-status-shortcuts maintainer note.
func TestAdminLogsPagePayloadEchoesTheSubsystemAndTheTextItWasAskedFor(t *testing.T) {
	page := ports.LogSearchPage{Count: 0, Results: []domain.LogEntry{}}

	got := marshal(t, AdminLogsPage(page, map[string]any{
		"date":   "2026-05-11T09:00:00Z",
		"level":  "warn",
		"source": "dirwatch",
		"text":   "/data/incoming",
		"limit":  float64(200),
		"offset": float64(0),
		"sort":   float64(-1),
	}))

	want := `"options":{"date":"2026-05-11T09:00:00Z","level":"warn","source":"dirwatch","text":"/data/incoming","limit":200,"offset":0,"sort":-1}`

	if !strings.Contains(got, want) {
		t.Errorf("echoed criteria =\n  %s\nwant %s", got, want)
	}

	got = marshal(t, AdminLogsPage(page, map[string]any{"source": "nonsense"}))

	if strings.Contains(got, `"source"`) {
		t.Errorf("echoed criteria =\n  %s\nwant no subsystem at all: the search did not have one, because the "+
			"name was not a part of this server", got)
	}
}

// replaceInOrder fills a captured placeholder in the order it appears, because the same placeholder
// stands for several different values and each one has to be put back where it belongs.
func replaceInOrder(t *testing.T, body, placeholder string, values ...string) string {
	t.Helper()

	for _, value := range values {
		at := strings.Index(body, placeholder)
		if at < 0 {
			t.Fatalf("the captured payload has no %s left to fill in: %s", placeholder, body)
		}

		body = body[:at] + value + body[at+len(placeholder):]
	}

	if strings.Contains(body, placeholder) {
		t.Errorf("the captured payload still holds a %s placeholder: %s", placeholder, body)
	}

	return body
}
