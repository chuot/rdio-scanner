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

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The administrator's own endpoints answer with payloads of their own, and the captures taken from the
// legacy server are compared byte for byte.

func TestAdminLoginReplyMatchesTheCapturedLogin(t *testing.T) {
	// The flag is carried and not decided here: the use case reports it as true on every accepted login,
	// and this is what the capture shows the legacy server answering. The captured credential is a
	// placeholder, because a session credential is different on every run of the capture.
	got := marshal(t, AdminLoginReply{PasswordNeedChange: true, Token: "capture-token"})
	want := replaceInOrder(t, readGolden(t, "rest-login.json"), "<token>", "capture-token")

	if got != want {
		t.Errorf("admin login reply =\n  %s\nwant\n  %s", got, want)
	}
}

// A password change answers with the one thing the administrator's page acts on, and nothing else. The
// captured answer says the credential on record no longer needs changing, because the capture changed it.
func TestAdminPasswordReplyMatchesTheCapturedPasswordChange(t *testing.T) {
	got := marshal(t, AdminPasswordReply{PasswordNeedChange: false})
	want := readGolden(t, "rest-password.json")

	if got != want {
		t.Errorf("admin password reply =\n  %s\nwant\n  %s", got, want)
	}
}

// The nine alert sequences are not configuration and are not stored anywhere: every client that shows an
// alert reads the same nine, and the administrator's endpoint is where a client fetches them.
func TestAlertsPayloadMatchesTheCapturedAlerts(t *testing.T) {
	got := marshal(t, Alerts())
	want := readGolden(t, "rest-alerts.json")

	if got != want {
		t.Errorf("alerts payload =\n  %s\nwant\n  %s", got, want)
	}
}

// The log view is answered with the total the criteria matches, the span of time everything that matches
// covers, the criteria echoed back, the per-subsystem counts, and the entries of this page.
//
// The captured view is the one the capture script asked for: warnings only, a page of two hundred from
// the start, ascending. Its timestamps are placeholders in the capture, and the test fills them back in
// with the instants the test's own log holds, in the order the payload writes them.
//
// The capture is no longer the bytes the legacy server wrote for this endpoint: the legacy server has no
// subsystem to report. It is the shape this server's contract declares, taken over the same scenario the
// capture script ran — a deployment whose log holds the startup line the other server wrote, and the two
// configuration changes an administrator made afterwards. The manifest records that.
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

	want := replaceInOrder(t, readGolden(t, "rest-logs.json"), "<datetime>",
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

// The criteria are echoed back as they were submitted, because the log view pages by resubmitting what
// it was last answered with. Two things follow, and both are the legacy server's behavior: a criterion
// that was not submitted is not echoed, and a limit larger than the most the search will ever return is
// echoed as the number that was asked for and not as the number that was used.
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

// A date that the log view submitted is echoed back as the instant it parsed to, and a date that is not
// an instant is not echoed at all: the search runs without that criterion, and the view is answered with
// the criteria the search actually had.
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

// Every entry the log view is answered with says which part of the server wrote it. That is what the
// stored column is for: an operator looking at a page of warnings is looking for the ones the watcher
// wrote, and a page that does not say cannot be narrowed at all.
//
// The field sits between the level and the message, which is the order the contract declares.
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

// The per-subsystem counts are what the log view offers the operator before they narrow to anything, so
// they are answered on every search and not only on one that asks for them. They describe everything the
// criteria match, they arrive in the order the search reported them — most first — and a search that
// matches nothing answers an empty list rather than a null.
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

	// The counts belong to the whole match and not to the page, so they are answered even when the page is
	// empty — which is the case an operator is most likely to be in when they are deciding what to narrow.
	if !strings.Contains(got, `"count":183`) {
		t.Errorf("the total =\n  %s\nwant 183, the total the criteria match", got)
	}

	got = marshal(t, AdminLogsPage(ports.LogSearchPage{Results: []domain.LogEntry{}}, map[string]any{}))

	if !strings.Contains(got, `"sources":[]`) {
		t.Errorf("the per-subsystem counts of a search that matches nothing =\n  %s\nwant an empty list", got)
	}
}

// The subsystem and the text join the echoed criteria, in the order the contract declares them:
// date, level, source, text, limit, offset, sort. The log view pages by resubmitting what it was last
// answered with, so a criterion it asked for has to come back or the next page is a different search.
//
// A subsystem that is not a name this server has is not echoed, because the search did not run with one —
// and the endpoint refuses that request outright, so the echo is the second half of the same decision.
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
