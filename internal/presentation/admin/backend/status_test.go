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

package backend

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The status read is the one thing the administrator's landing page asks for, and it is the only
// published boundary in the server that answers what the server is doing right now. It has no legacy
// capture to be compared against — the server this one replaced had no status read at all — so the
// shape it is held to is the one written down for it.

// The status read is answered with the identity the administrator surface already carries beside the
// snapshot the use case measured: the version, the container answer, and the folder the server was told
// to serve.
func TestStatusEndpointAnswersWhatTheServerIsAndWhatItIsRunning(t *testing.T) {
	fixture := newStatusFixture()

	rec := fixture.authorized(http.MethodGet, "/api/admin/status", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if got, want := rec.Body.String(), statusAnswer(t, fixture); got != want {
		t.Errorf("status answer =\n  %s\nwant\n  %s", got, want)
	}
}

// A snapshot with nothing recorded is answered with empty lists and no last call, and not with nulls.
// A page that has to check each list for being absent before it can count it is a page that will
// eventually stop checking.
func TestStatusEndpointAnswersEmptyListsRatherThanNothingWhenNothingIsRecorded(t *testing.T) {
	fixture := newStatusFixture()

	fixture.activity.Hours = nil
	fixture.activity.Busiest = nil
	fixture.activity.Recent = nil
	fixture.activity.SinceAnswers = nil
	fixture.tallies.Stored = nil

	rec := fixture.authorized(http.MethodGet, "/api/admin/status", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status status = %d, want 200 for a server that has recorded nothing: %s", rec.Code, rec.Body.String())
	}

	body := decodedBody(t, rec)

	// `busiest` and `recent` are empty lists and not absent. A page that has to check each list for being
	// absent before it can count it is a page that will eventually stop checking.
	for _, name := range []string{"busiest", "recent"} {
		list, ok := field(t, body, "activity", name)
		if !ok {
			t.Errorf("the activity block carries no %s at all", name)

			continue
		}

		entries, isArray := list.([]any)
		if !isArray {
			t.Errorf("activity.%s is %v, want an empty list", name, list)

			continue
		}

		if len(entries) != 0 {
			t.Errorf("activity.%s carries %d entries, want none: %v", name, len(entries), entries)
		}
	}

	sources, ok := field(t, body, "logs", "sources")
	if !ok {
		t.Errorf("the logs block carries no sources at all")
	} else if entries, isArray := sources.([]any); !isArray || len(entries) != 0 {
		t.Errorf("logs.sources is %v, want an empty list", sources)
	}

	if _, ok := field(t, body, "activity", "lastCallAt"); ok {
		t.Errorf("the activity block names a last call for a history with no calls in it")
	}

	// The 24 buckets are the window's shape and not the history's: a server that has recorded nothing
	// is still showing a 24-hour window.
	hours, ok := field(t, body, "activity", "hours")
	if !ok {
		t.Fatalf("the activity block carries no hours at all")
	}

	if entries, isArray := hours.([]any); !isArray || len(entries) != application.StatusHourBuckets {
		t.Errorf("activity.hours carries %v, want the %d buckets of the window", hours, application.StatusHourBuckets)
	}
}

// The status read is asked for by a page that polls it, and a page polls with a GET. A method this
// endpoint does not answer is refused, and it is refused after the credential has been settled: the
// endpoint does not tell an anonymous caller which methods it answers.
func TestStatusEndpointRefusesMethodsItDoesNotAnswer(t *testing.T) {
	fixture := newStatusFixture()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := fixture.authorized(method, "/api/admin/status", "")

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status status for %s = %d, want 405: %s", method, rec.Code, rec.Body.String())
		}
	}

	// A request that asks to become a websocket connection is answered as the ordinary read it is. The
	// composition root's websocket takeover covers the pages, and this path is not one of them; a status
	// read that quietly became a connection would leave the page polling a socket it never asked for.
	rec := fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/status", token: captureToken, Upgrade: true})

	if rec.Code != http.StatusOK {
		t.Errorf("status status for an upgrade request = %d, want the ordinary 200 read: %s", rec.Code, rec.Body.String())
	}
}

// A status read is asked for by a page that shows it to whoever is looking at it. Without a credential
// the endpoint answers 401, and it answers it without reading anything: an anonymous request that cost
// the server six statements is a way for anyone to make this server work.
func TestStatusEndpointRefusesARequestWithoutACredentialWithoutReadingAnything(t *testing.T) {
	fixture := newStatusFixture()

	rec := fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/status"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	if rec.Body.Len() != 0 {
		t.Errorf("an unauthorized status request answers %q, want nothing", rec.Body.String())
	}

	if reads := fixture.statusReads(); reads != 0 {
		t.Errorf("a request with no credential cost the server %d reads, want none", reads)
	}
}

// A credential this server did not issue is refused the same way no credential is, and for the same
// reason and at the same point: before the method is looked at and before anything is read.
func TestStatusEndpointRefusesACredentialItDidNotIssue(t *testing.T) {
	fixture := newStatusFixture()

	rec := fixture.serve(adminRequest{method: http.MethodGet, path: "/api/admin/status", token: "not-a-token-the-server-issued"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	if reads := fixture.statusReads(); reads != 0 {
		t.Errorf("a request carrying a credential this server did not issue cost %d reads, want none", reads)
	}

	// The method is settled after the credential: an anonymous POST is told the session is gone, and not
	// that the method is wrong.
	rec = fixture.serve(adminRequest{method: http.MethodPost, path: "/api/admin/status"})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status status for an anonymous POST = %d, want 401 and not 405", rec.Code)
	}
}

// A status that cannot be read is answered with the failure status every other administrator read uses,
// and with nothing in the body. The page has to be able to tell "the server could not be read" apart
// from "the server is fine and has nothing to report", and a body that is a zeroed snapshot is how it
// would fail to.
func TestStatusEndpointReportsAFailureToReadTheStatus(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fails func(fixture *adminFixture, err error)
	}{
		{"the call activity", func(fixture *adminFixture, err error) { fixture.activity.Err = err }},
		{"the log tallies", func(fixture *adminFixture, err error) { fixture.tallies.Err = err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newStatusFixture()

			tc.fails(fixture, errors.New("calls.activity: no such table: calls in SELECT COUNT(*)"))

			rec := fixture.authorized(http.MethodGet, "/api/admin/status", "")

			if rec.Code != http.StatusExpectationFailed {
				t.Fatalf("status status = %d, want 417: %s", rec.Code, rec.Body.String())
			}

			if body := rec.Body.String(); body != "" {
				t.Errorf("the answer to a failed status read is %q, want nothing: a zeroed snapshot would be read "+
					"as a server that is healthy and idle", body)
			}
		})
	}
}

// The container answer and the folder the server serves are named by the administrator surface beside
// the version it already carries, so the status read says the same thing about the deployment as the
// configuration read does.
func TestStatusEndpointNamesTheDeploymentTheConfigurationReadNames(t *testing.T) {
	fixture := newStatusFixture()

	fixture.Docker = true
	fixture.BaseDir = ""

	fixture.routes = fixture.buildRoutes()

	rec := fixture.authorized(http.MethodGet, "/api/admin/status", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	body := decodedBody(t, rec)

	server, ok := body.(map[string]any)["server"]
	if !ok {
		t.Fatalf("the answer carries no server block: %s", rec.Body.String())
	}

	if docker := server.(map[string]any)["docker"]; docker != true {
		t.Errorf("the server block reports docker=%v, want true: the same answer the configuration read gives", docker)
	}

	if baseDir := server.(map[string]any)["baseDir"]; baseDir != "" {
		t.Errorf("the server block names %q as the folder it serves, want the empty string the operator named none with", baseDir)
	}
}

// newStatusFixture is the administrator's surface with a status read wired into it, holding a server
// that has been up for two hours and fourteen minutes and has recorded something lately.
func newStatusFixture() *adminFixture {
	fixture := newAdminFixture()

	fixture.BaseDir = "/data/rdio"

	fixture.runtime.StartedAt = captureTime.Add(-2*time.Hour - 14*time.Minute)
	fixture.runtime.Listeners = 3
	fixture.runtime.AdministratorSessions = 1
	fixture.runtime.RunningWatchers = 2
	fixture.runtime.HeldCalls = 2
	fixture.runtime.AudioConversion = true
	fixture.runtime.AudioProbe = true
	fixture.runtime.DatabaseBytes = 4_831_838_208
	fixture.runtime.DatabaseKnown = true

	fixture.activity.Hours = []ports.CallHourCount{
		{Hour: statusWindowStart().Add(3 * time.Hour), Count: 9},
		{Hour: statusWindowStart().Add(11 * time.Hour), Count: 4},
		{Hour: statusWindowStart().Add(23 * time.Hour), Count: 37},
	}
	// The three counts the status read asks for are three different windows, and the fixture answers
	// each of them with its own number so that an endpoint that mixed them up is caught.
	fixture.activity.SinceAnswers = map[int64]uint64{
		statusWindowStart().UnixMilli():         214,
		localDayStart().UnixMilli():             190,
		captureTime.Add(-time.Hour).UnixMilli(): 37,
	}
	fixture.activity.Busiest = []ports.CallTalkgroupCount{
		{SystemRef: 10001, TalkgroupRef: 1514000, Name: "Dispatch 1", Label: "T-1514000", Count: 41},
		{SystemRef: 10002, TalkgroupRef: 1515000, Name: "Marine 16", Label: "T-1515000", Count: 12},
	}
	fixture.activity.Recent = []ports.CallSummary{
		{ID: 91827, SystemRef: 10001, TalkgroupRef: 1514000, Timestamp: captureTime.Add(-16*time.Minute - 58*time.Second), AudioLength: 12400 * time.Millisecond},
	}

	fixture.tallies.Stored = []ports.LogLevelTally{
		{Level: domain.LogLevelError, Source: domain.LogSourceDirwatch, Count: 2},
		{Level: domain.LogLevelError, Source: domain.LogSourceFFmpeg, Count: 1},
		{Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Count: 5},
		{Level: domain.LogLevelWarn, Source: domain.LogSourceDownstream, Count: 6},
	}

	fixture.routes = fixture.buildRoutes()

	return fixture
}

// statusWindowEnd is the end of the whole hour the clock sits in, and statusWindowStart is 24 hours
// before it: the window the activity tallies are read over, which is the window's shape and not the
// history's.
func statusWindowEnd() time.Time {
	return captureTime.Truncate(time.Hour).Add(time.Hour)
}

func statusWindowStart() time.Time {
	return statusWindowEnd().Add(-24 * time.Hour)
}

// localDayStart is the instant the server's own local day begins, which is the bound the day's count is
// read from. It is not the window's start, and it is not the current hour's bucket.
func localDayStart() time.Time {
	return time.Date(captureTime.Year(), captureTime.Month(), captureTime.Day(), 0, 0, 0, 0, captureTime.Location())
}

// statusReads counts the reads the status use case actually ran against the fixtures. A request that was
// refused costs none of them: the status read is six statements over a database that can hold years of
// calls, and an anonymous request that cost them would be a way for anyone to make this server work.
func (fixture *adminFixture) statusReads() int {
	return len(fixture.activity.SinceAsked) + len(fixture.activity.HoursAsked) +
		len(fixture.activity.BusiestAsked) + len(fixture.activity.RecentAsked) +
		len(fixture.tallies.TalliesAsked)
}

// statusAnswer is the body the status read is held to, written out the way it is put on the wire.
func statusAnswer(t *testing.T, fixture *adminFixture) string {
	t.Helper()

	hours := make([]string, 0, application.StatusHourBuckets)

	reported := map[int64]uint64{}

	for _, count := range fixture.activity.Hours {
		reported[count.Hour.UnixMilli()] = count.Count
	}

	for i := range application.StatusHourBuckets {
		hour := statusWindowStart().Add(time.Duration(i) * time.Hour)

		hours = append(hours, fmt.Sprintf(`{"start":%q,"count":%d}`, hour.Format(time.RFC3339), reported[hour.UnixMilli()]))
	}

	return fmt.Sprintf(`{"server":{"version":%q,"docker":%v,"startedAt":%q,"now":%q,"uptimeSeconds":8040,`+
		`"baseDir":%q,"databaseBytes":4831838208,"databaseKnown":true},`+
		`"live":{"listeners":3,"administratorSessions":1,"dirwatchersRunning":2,"heldCalls":2,`+
		`"audioConversion":true,"audioProbe":true},`+
		`"activity":{"windowStart":%q,"windowEnd":%q,"callsToday":190,"callsLastHour":37,`+
		`"lastCallAt":%q,"hours":[%s],"busiest":[{"systemRef":10001,"talkgroupRef":1514000,`+
		`"name":"Dispatch 1","label":"T-1514000","count":41},{"systemRef":10002,"talkgroupRef":1515000,`+
		`"name":"Marine 16","label":"T-1515000","count":12}],`+
		`"recent":[{"callId":91827,"systemRef":10001,"talkgroupRef":1514000,"timestamp":%q,"audioLengthSeconds":12.4}]},`+
		`"logs":{"windowStart":%q,"errors":3,"warnings":11,"sources":[{"source":"dirwatch","errors":2,"warnings":5},`+
		`{"source":"downstream","errors":0,"warnings":6},{"source":"ffmpeg","errors":1,"warnings":0}]}}`,
		captureVersion,
		fixture.Docker,
		fixture.runtime.StartedAt.Format(time.RFC3339),
		captureTime.Format(time.RFC3339),
		fixture.BaseDir,
		statusWindowStart().Format(time.RFC3339),
		statusWindowEnd().Format(time.RFC3339),
		fixture.activity.Recent[0].Timestamp.Format(time.RFC3339),
		strings.Join(hours, ","),
		fixture.activity.Recent[0].Timestamp.Format(time.RFC3339),
		captureTime.Add(-24*time.Hour).Format(time.RFC3339),
	)
}

// field reads one field of one block of an answered body.
func field(t *testing.T, body any, block, name string) (any, bool) {
	t.Helper()

	blocks, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("the answer is not an object: %T", body)
	}

	entry, ok := blocks[block].(map[string]any)
	if !ok {
		return nil, false
	}

	value, ok := entry[name]

	return value, ok
}
