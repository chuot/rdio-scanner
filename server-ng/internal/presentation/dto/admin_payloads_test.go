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

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
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
// covers, the criteria echoed back, and the entries of this page.
//
// The captured view is the one the capture script asked for: warnings only, a page of two hundred from
// the start, ascending. Its timestamps are placeholders in the capture, and the test fills them back in
// with the instants the test's own log holds, in the order the payload writes them.
func TestAdminLogsPagePayloadMatchesTheCapturedLogView(t *testing.T) {
	first := time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)
	second := first.Add(time.Second)
	third := first.Add(2 * time.Second)

	page := ports.LogSearchPage{
		Count:     3,
		DateStart: first,
		DateStop:  third,
		Results: []domain.LogEntry{
			{ID: 1, DateTime: first, Level: domain.LogLevelWarn, Message: "server started"},
			{ID: 2, DateTime: second, Level: domain.LogLevelWarn, Message: "configuration changed"},
			{ID: 3, DateTime: third, Level: domain.LogLevelWarn, Message: "configuration changed"},
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
	want := `{"count":0,"dateStart":"0001-01-01T00:00:00Z","dateStop":"0001-01-01T00:00:00Z","options":{"limit":600},"logs":[]}`

	if got != want {
		t.Errorf("echoed criteria =\n  %s\nwant\n  %s", got, want)
	}

	got = marshal(t, AdminLogsPage(page, map[string]any{}))
	want = `{"count":0,"dateStart":"0001-01-01T00:00:00Z","dateStop":"0001-01-01T00:00:00Z","options":{},"logs":[]}`

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
