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

package backend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

// The endpoints answer with the same page and the same call the listener gets, reached over
// HTTP as the surface an HTTP client and the Model Context Protocol surface can use.

func TestSearchEndpointAnswersAPageOfCallHistory(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{
		method: http.MethodPost,
		path:   "/api/calls/search",
		token:  adminToken,
		body:   `{"limit":2,"sort":-1}`,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/calls/search = %d, want the page: %s", rec.Code, rec.Body.String())
	}

	var answered struct {
		Count     uint   `json:"count"`
		DateStart string `json:"dateStart"`
		DateStop  string `json:"dateStop"`
		Options   struct {
			Limit *uint `json:"limit"`
			Sort  *int  `json:"sort"`
		} `json:"options"`
		Results []struct {
			ID          uint64    `json:"id"`
			System      uint      `json:"system"`
			Talkgroup   uint      `json:"talkgroup"`
			Timestamp   time.Time `json:"dateTime"`
			AudioLength float64   `json:"audioLength"`
		} `json:"results"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the search answer is not a page: %v", err)
	}

	if answered.Count != 3 {
		t.Errorf("count = %d, want the whole history counted, not the page: %s", answered.Count, rec.Body.String())
	}

	if len(answered.Results) != 2 {
		t.Fatalf("results = %d rows, want the page size the caller asked for: %s", len(answered.Results), rec.Body.String())
	}

	// The newest first: sort -1 is the history view's descending order, and the page starts at
	// the most recent call.
	if answered.Results[0].ID != 3 {
		t.Errorf("first row = call %d, want call 3, the newest: %s", answered.Results[0].ID, rec.Body.String())
	}

	if answered.Results[0].System != systemTwoRef || answered.Results[0].Talkgroup != talkgroupTwoRef {
		t.Errorf("first row = system %d talkgroup %d, want system %d talkgroup %d: %s",
			answered.Results[0].System, answered.Results[0].Talkgroup, systemTwoRef, talkgroupTwoRef, rec.Body.String())
	}

	if answered.Results[0].AudioLength != 0.9 {
		t.Errorf("first row audioLength = %v, want 0.9 seconds: %s", answered.Results[0].AudioLength, rec.Body.String())
	}

	if answered.Options.Limit == nil || *answered.Options.Limit != 2 {
		t.Errorf("the echo does not carry the page size the caller asked for: %s", rec.Body.String())
	}

	if answered.DateStart == "" || answered.DateStop == "" {
		t.Errorf("the page does not name the bounds it spans: %s", rec.Body.String())
	}
}

// A search that filters narrows the page, and the criteria the caller asked with come back as the
// caller asked them: the echo says what this server was asked, not what it managed to use.
func TestSearchEndpointFiltersBySystemAndTalkgroup(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{
		method: http.MethodPost,
		path:   "/api/calls/search",
		token:  adminToken,
		body:   `{"system":10001,"talkgroup":1514000}`,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/calls/search = %d, want the page: %s", rec.Code, rec.Body.String())
	}

	var answered struct {
		Count   uint `json:"count"`
		Results []struct {
			ID uint64 `json:"id"`
		} `json:"results"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the search answer is not a page: %v", err)
	}

	if answered.Count != 2 {
		t.Errorf("count = %d, want the two calls on that talkgroup: %s", answered.Count, rec.Body.String())
	}

	for _, row := range answered.Results {
		if row.ID != 1 && row.ID != 2 {
			t.Errorf("row %d is not on the talkgroup the search named: %s", row.ID, rec.Body.String())
		}
	}
}

// The paging bounds are the history search's own, and the endpoints do not widen them: a caller
// asking for more rows than the history will ever return gets the ceiling, not what it asked for.
func TestSearchEndpointClampsThePagingTheHistorySearchUses(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{
		method: http.MethodPost,
		path:   "/api/calls/search",
		token:  adminToken,
		body:   `{"limit":900000,"offset":-5}`,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/calls/search = %d, want the page: %s", rec.Code, rec.Body.String())
	}

	searched := fixture.calls.Searched
	if len(searched) != 1 {
		t.Fatalf("the search ran %d times, want once", len(searched))
	}

	if searched[0].Limit != 500 {
		t.Errorf("the search ran with limit %d, want the ceiling the history search uses (500)", searched[0].Limit)
	}

	// A negative offset wraps into a huge number when it is read as an unsigned one, which is
	// what a hostile client means by it. The floor is the start of the table.
	if searched[0].Offset != 0 {
		t.Errorf("the search ran with offset %d, want the start of the table", searched[0].Offset)
	}
}

// A server with no access list is not restricted: the history is open to an anonymous caller, which
// is what the listener socket does with the same configuration today.
func TestSearchEndpointAnswersAnUnrestrictedServerAnonymously(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{method: http.MethodPost, path: "/api/calls/search", body: `{}`})

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/calls/search with no credential = %d, want the page: an unrestricted server "+
			"opens its history to an anonymous caller, as the listener socket does: %s", rec.Code, rec.Body.String())
	}
}

// A server with an access list is restricted, and these endpoints are listener-facing: a caller that
// proves nothing gets the answer the listener gets for presenting nothing.
func TestSearchEndpointRefusesARequestThatProvesNothingOnARestrictedServer(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.seedAccessList(t, wildcardScope())

	rec := fixture.serve(callsRequest{method: http.MethodPost, path: "/api/calls/search", body: `{}`})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /api/calls/search with no credential = %d, want %d: a restricted server asks a "+
			"caller to prove itself", rec.Code, http.StatusUnauthorized)
	}
}

// An administrator session opens the whole history: the administrator administers the deployment,
// and is not scoped by a listener's access list.
func TestSearchEndpointAcceptsAnAdministratorSessionAsTheWholeHistory(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.seedAccessList(t, scopedTo(systemOneRef))

	rec := fixture.serve(callsRequest{method: http.MethodPost, path: "/api/calls/search", token: adminToken, body: `{}`})

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/calls/search with an administrator session = %d, want the page: %s",
			rec.Code, rec.Body.String())
	}

	var answered struct {
		Count uint `json:"count"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the search answer is not a page: %v", err)
	}

	if answered.Count != 3 {
		t.Errorf("count = %d, want the whole history: an administrator session is not scoped by the "+
			"listener access list: %s", answered.Count, rec.Body.String())
	}
}

// An access whose date has passed is refused, the way the listener socket refuses it: the code is
// real, it belongs to this deployment, and it no longer opens anything.
func TestEndpointsRefuseAnExpiredAccessCode(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.seedExpiredAccess(t, wildcardScope())

	for _, request := range []callsRequest{
		{method: http.MethodPost, path: "/api/calls/search", code: listenerCode, body: `{}`},
		{method: http.MethodGet, path: "/api/calls/1", code: listenerCode},
		{method: http.MethodGet, path: "/api/calls/1/audio", code: listenerCode},
	} {
		rec := fixture.serve(request)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with an expired access code = %d, want %d",
				request.method, request.path, rec.Code, http.StatusUnauthorized)
		}
	}
}

// A code that belongs to somebody else's deployment is refused: the access list is this server's,
// and a caller holding a code from a different deployment has no claim on this history.
func TestEndpointsRefuseACodeThisServersAccessListDoesNotCarry(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.seedAccessList(t, wildcardScope())

	for _, request := range []callsRequest{
		{method: http.MethodPost, path: "/api/calls/search", code: "a-code-from-another-deployment", body: `{}`},
		{method: http.MethodGet, path: "/api/calls/1", code: "a-code-from-another-deployment"},
		{method: http.MethodGet, path: "/api/calls/1/audio", code: "a-code-from-another-deployment"},
	} {
		rec := fixture.serve(request)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with a code this server does not carry = %d, want %d",
				request.method, request.path, rec.Code, http.StatusUnauthorized)
		}
	}
}

// The refusals are indistinguishable from one another: a caller learns that it did not get in, and
// learns nothing about why.
func TestEndpointsRefuseEveryFailedCredentialWithTheSameAnswer(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.seedAccessList(t, scopedTo(systemOneRef))

	var answers []string

	for _, code := range []string{"", "not-a-code", "a-code-from-another-deployment"} {
		rec := fixture.serve(callsRequest{method: http.MethodPost, path: "/api/calls/search", code: code, body: `{}`})

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("a refused credential = %d, want %d", rec.Code, http.StatusUnauthorized)
		}

		answers = append(answers, fmt.Sprintf("%d|%s", rec.Code, rec.Body.String()))
	}

	if answers[0] != answers[1] || answers[1] != answers[2] {
		t.Errorf("the refusals are distinguishable (%v): a caller is not told which credential failed, "+
			"or how far it got", answers)
	}
}

func TestSearchEndpointRefusesAnUnreadableBodyWithBadRequest(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{method: http.MethodPost, path: "/api/calls/search", token: adminToken, body: `{"limit"`})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /api/calls/search with an unreadable body = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSearchEndpointRefusesAMethodItDoesNotAnswer(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{method: http.MethodGet, path: "/api/calls/search", token: adminToken})

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/calls/search = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// The history cannot be read: the server's own step failed, which is the status the administrator's
// surface uses for the same shape of failure.
func TestSearchEndpointAnswersTheFailureStatusWhenTheHistoryCannotBeRead(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.calls.Err = errFixture

	rec := fixture.serve(callsRequest{method: http.MethodPost, path: "/api/calls/search", token: adminToken, body: `{}`})

	if rec.Code != http.StatusExpectationFailed {
		t.Errorf("POST /api/calls/search with a history that cannot be read = %d, want %d",
			rec.Code, http.StatusExpectationFailed)
	}
}

// One recorded call comes back in full: the frequencies, the units heard, the talkgroups it
// was patched to, and how long the recording is, but without the recording's bytes.
func TestGetEndpointAnswersOneCallWithoutItsRecording(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{method: http.MethodGet, path: "/api/calls/1", token: adminToken})

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/calls/1 = %d, want the call: %s", rec.Code, rec.Body.String())
	}

	var answered map[string]any

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the call answer is not an object: %v", err)
	}

	if answered["id"] != float64(1) {
		t.Errorf("answer names call %v, want call 1: %s", answered["id"], rec.Body.String())
	}

	if answered["system"] != float64(systemOneRef) || answered["talkgroup"] != float64(talkgroupOneRef) {
		t.Errorf("answer names system %v talkgroup %v, want %d and %d: %s",
			answered["system"], answered["talkgroup"], systemOneRef, talkgroupOneRef, rec.Body.String())
	}

	if answered["audioName"] != "fire-1.mp3" || answered["audioType"] != "audio/mpeg" {
		t.Errorf("answer names the recording %v / %v, want fire-1.mp3 / audio/mpeg: %s",
			answered["audioName"], answered["audioType"], rec.Body.String())
	}

	if answered["audioLength"] != 4.2 {
		t.Errorf("answer names audioLength %v, want 4.2 seconds: %s", answered["audioLength"], rec.Body.String())
	}

	// The recording is its own entry point, so a metadata answer carries the recording's name,
	// type, and length and none of its bytes.
	if _, ok := answered["audio"]; ok {
		t.Errorf("the metadata answer carries the recording's bytes; the recording is fetched at "+
			"/api/calls/{callId}/audio: %s", rec.Body.String())
	}

	// The link the caller follows to get the recording, so a caller does not have to build it.
	audio, ok := answered["audioUrl"].(string)
	if !ok || audio != "/api/calls/1/audio" {
		t.Errorf("answer names audioUrl %v, want /api/calls/1/audio: %s", answered["audioUrl"], rec.Body.String())
	}
}

func TestGetEndpointRefusesACallThatIsNotThere(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{method: http.MethodGet, path: "/api/calls/4812", token: adminToken})

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/calls/4812 = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestGetEndpointRefusesARequestThatProvesNothingOnARestrictedServer(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.seedAccessList(t, wildcardScope())

	rec := fixture.serve(callsRequest{method: http.MethodGet, path: "/api/calls/1"})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/calls/1 with no credential = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestGetEndpointRefusesAMethodItDoesNotAnswer(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{method: http.MethodPost, path: "/api/calls/1", token: adminToken})

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/calls/1 = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// The recording, as it is stored. The audio lives in the database beside the call, so the answer is
// the stored bytes with the stored type and the stored name — no filesystem, no conversion.
func TestAudioEndpointAnswersTheRecordingAsItIsStored(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{method: http.MethodGet, path: "/api/calls/1/audio", token: adminToken})

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/calls/1/audio = %d, want the recording: %s", rec.Code, rec.Body.String())
	}

	if got, want := rec.Header().Get("Content-Type"), "audio/mpeg"; got != want {
		t.Errorf("Content-Type = %q, want the type the call was stored with (%q)", got, want)
	}

	if got, want := rec.Header().Get("Content-Disposition"), `attachment; filename="fire-1.mp3"`; got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}

	if got, want := rec.Body.String(), "audio-bytes-of-fire-1.mp3"; got != want {
		t.Errorf("the recording = %q, want the stored bytes %q", got, want)
	}

	if got, want := rec.Header().Get("Content-Length"), fmt.Sprintf("%d", rec.Body.Len()); got != want {
		t.Errorf("Content-Length = %q, want the length of the answer (%q)", got, want)
	}
}

// A range request for a recording is served like any other content: the requested slice, with
// the total length named.
func TestAudioEndpointAnswersARangeRequest(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{
		method: http.MethodGet,
		path:   "/api/calls/1/audio",
		token:  adminToken,
		Range:  "bytes=0-3",
	})

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("GET /api/calls/1/audio with a range = %d, want %d", rec.Code, http.StatusPartialContent)
	}

	if got, want := rec.Body.String(), "audi"; got != want {
		t.Errorf("the range answer = %q, want %q", got, want)
	}

	if got := rec.Header().Get("Content-Range"); got == "" {
		t.Errorf("the range answer names no Content-Range: %v", rec.Header())
	}
}

func TestAudioEndpointRefusesACallThatIsNotThere(t *testing.T) {
	fixture := newCallsFixture(t)

	rec := fixture.serve(callsRequest{method: http.MethodGet, path: "/api/calls/4812/audio", token: adminToken})

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/calls/4812/audio = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAudioEndpointRefusesARequestThatProvesNothingOnARestrictedServer(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.seedAccessList(t, wildcardScope())

	rec := fixture.serve(callsRequest{method: http.MethodGet, path: "/api/calls/1/audio"})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/calls/1/audio with no credential = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// The caller's scope is checked before the bytes are read, and an out-of-scope call gets the
// not-there answer.
func TestAudioEndpointRefusesACallOutsideTheCallersScopeAsAcallThatIsNotThere(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.seedAccessList(t, scopedTo(systemOneRef))

	rec := fixture.serve(callsRequest{method: http.MethodGet, path: "/api/calls/3/audio", code: listenerCode})

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/calls/3/audio from a caller scoped to system %d = %d, want %d: the caller is "+
			"not told the call exists", systemOneRef, rec.Code, http.StatusNotFound)
	}
}

func TestGetEndpointRefusesACallOutsideTheCallersScopeAsAcallThatIsNotThere(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.seedAccessList(t, scopedTo(systemOneRef))

	rec := fixture.serve(callsRequest{method: http.MethodGet, path: "/api/calls/3", code: listenerCode})

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/calls/3 from a caller scoped to system %d = %d, want %d",
			systemOneRef, rec.Code, http.StatusNotFound)
	}
}

// A search outside the caller's scope answers with no rows, and says nothing about what exists
// outside it.
func TestSearchEndpointAnswersACallerOutsideTheScopeWithNoRows(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.seedAccessList(t, scopedTo(systemOneRef))

	rec := fixture.serve(callsRequest{
		method: http.MethodPost,
		path:   "/api/calls/search",
		code:   listenerCode,
		body:   `{"system":10002,"talkgroup":1515000}`,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/calls/search from a scoped caller = %d, want the page: %s", rec.Code, rec.Body.String())
	}

	var answered struct {
		Count   uint             `json:"count"`
		Results []map[string]any `json:"results"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &answered); err != nil {
		t.Fatalf("the search answer is not a page: %v", err)
	}

	if answered.Count != 0 || len(answered.Results) != 0 {
		t.Errorf("a scoped caller asking about a system it cannot see got count %d and %d rows, want "+
			"nothing at all: %s", answered.Count, len(answered.Results), rec.Body.String())
	}
}

var errFixture = fixtureError{}

type fixtureError struct{}

func (fixtureError) Error() string { return "the fixture's own store refuses" }

// The search screen's filters reach the published endpoint as the search reads them: system
// and talkgroup as numbers, group and tag as labels, the date as an instant. The reading is
// taken through the search's own parser, not against a copy of the rule in the test.
func TestSearchEndpointNarrowsByTheShapeEachCriterionIsReadIn(t *testing.T) {
	fixture := newCallsFixture(t)

	// The configuration as the listener is given it: the filters name labels over the references
	// the history is stored under, and the second system names a group and a tag of its own, so a
	// filter that stands narrows the history instead of answering with all of it.
	fixture.systems.Stored = domain.Systems{
		{
			ID:        1,
			SystemRef: systemOneRef,
			Label:     "Grand Metro",
			Talkgroups: domain.Talkgroups{
				{ID: 10, TalkgroupRef: talkgroupOneRef, Label: "Fire dispatch", GroupIDs: []uint64{1}, TagID: 3},
			},
		},
		{
			ID:        2,
			SystemRef: systemTwoRef,
			Label:     "Harbour",
			Talkgroups: domain.Talkgroups{
				{ID: 20, TalkgroupRef: talkgroupTwoRef, Label: "Marine", GroupIDs: []uint64{2}, TagID: 4},
			},
		},
	}

	fixture.groups.Stored = domain.Groups{{ID: 1, Label: "Fire"}, {ID: 2, Label: "Marine"}}
	fixture.tags.Stored = domain.Tags{{ID: 3, Label: "Fire Dispatch"}, {ID: 4, Label: "Marine Ops"}}

	type page struct {
		Count   uint           `json:"count"`
		Options map[string]any `json:"options"`
		Results []struct {
			ID        uint64 `json:"id"`
			System    uint   `json:"system"`
			Talkgroup uint   `json:"talkgroup"`
		} `json:"results"`
	}

	ask := func(t *testing.T, criteria string) page {
		t.Helper()

		rec := fixture.serve(callsRequest{
			method: http.MethodPost,
			path:   "/api/calls/search",
			token:  adminToken,
			body:   criteria,
		})

		if rec.Code != http.StatusOK {
			t.Fatalf("POST /api/calls/search %s = %d, want the page: %s", criteria, rec.Code, rec.Body.String())
		}

		var got page

		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding the answer to %s: %v", criteria, err)
		}

		return got
	}

	// The criteria the four filters stand for, sent the way the screen sends them.
	narrowed := ask(t, `{"limit":200,"offset":0,"sort":-1,"system":10001,"talkgroup":1514000,"group":"Fire","tag":"Fire Dispatch"}`)

	if narrowed.Count != 2 || len(narrowed.Results) != 2 {
		t.Fatalf("the four criteria together answered with %d calls (%d rows), want the 2 calls the history holds on that talkgroup", narrowed.Count, len(narrowed.Results))
	}

	for _, row := range narrowed.Results {
		if row.System != systemOneRef || row.Talkgroup != talkgroupOneRef {
			t.Errorf("call %d answered under %d/%d, want %d/%d", row.ID, row.System, row.Talkgroup, systemOneRef, talkgroupOneRef)
		}
	}

	// The answer names back what it was asked with, in the shape it was asked in.
	if got := narrowed.Options["system"]; !sameNumber(got, systemOneRef) {
		t.Errorf("the answer echoes system = %v, want the reference 10001 it was asked with", got)
	}

	if got := narrowed.Options["talkgroup"]; !sameNumber(got, talkgroupOneRef) {
		t.Errorf("the answer echoes talkgroup = %v, want the reference 1514000 it was asked with", got)
	}

	if got := narrowed.Options["group"]; got != "Fire" {
		t.Errorf("the answer echoes group = %v, want the label Fire it was asked with", got)
	}

	if got := narrowed.Options["tag"]; got != "Fire Dispatch" {
		t.Errorf("the answer echoes tag = %v, want the label Fire Dispatch it was asked with", got)
	}

	// A group and a tag travel as labels and expand over the listener's talkgroups; the same
	// criterion as a number expands to nothing and leaves the search unfiltered.
	if got := ask(t, `{"limit":200,"offset":0,"sort":-1,"group":"Marine"}`); got.Count != 1 || got.Results[0].System != systemTwoRef {
		t.Errorf("the group label Marine answered with %d calls, want the 1 call on the talkgroup in that group", got.Count)
	}

	if got := ask(t, `{"limit":200,"offset":0,"sort":-1,"tag":"Marine Ops"}`); got.Count != 1 || got.Results[0].Talkgroup != talkgroupTwoRef {
		t.Errorf("the tag label Marine Ops answered with %d calls, want the 1 call on the talkgroup under that tag", got.Count)
	}

	if got := ask(t, `{"limit":200,"offset":0,"sort":-1,"group":1}`); got.Count != 3 {
		t.Errorf("a number sent as a group label answered with %d calls, want the whole history: the search reads a group as a label and takes nothing from a number", got.Count)
	}

	// A system and a talkgroup arriving as the labels the operator read are read as no filter at
	// all, and the whole history comes back with no echo of a filter asked for.
	labelled := ask(t, `{"limit":200,"offset":0,"sort":-1,"system":"Grand Metro","talkgroup":"Fire dispatch"}`)

	if labelled.Count != 3 {
		t.Errorf("the labels Grand Metro and Fire dispatch answered with %d calls, want the whole history: the search reads a reference, and a label is not one", labelled.Count)
	}

	for _, name := range []string{"system", "talkgroup"} {
		if _, named := labelled.Options[name]; named {
			t.Errorf("the answer echoes %s = %v, want nothing: the criterion was not read, so it was not asked with", name, labelled.Options[name])
		}
	}

	// The date is an instant, written at the wall-clock minute the operator typed; a value the
	// parser cannot read leaves the day unfiltered, the same silent answer a label where a
	// reference is read produces.
	day := fixtureTime.Add(-2 * time.Hour).Format(time.RFC3339)

	if got := ask(t, fmt.Sprintf(`{"limit":200,"offset":0,"sort":-1,"date":%q}`, day)); got.Count != 2 {
		t.Errorf("the day %s answered with %d calls, want the 2 calls recorded in it", day, got.Count)
	}

	if got := ask(t, `{"limit":200,"offset":0,"sort":-1,"date":"2026-05-11T07:00"}`); got.Count != 3 {
		t.Errorf("a wall-clock minute answered with %d calls, want the whole history: the search reads an instant, and a minute without a zone is not one", got.Count)
	}
}

// sameNumber reports whether the echoed criterion is the reference the search was asked with. The
// echo carries the JSON number the body carried, so a reference that arrived as text is not one.
func sameNumber(got any, want uint) bool {
	n, ok := got.(float64)

	return ok && uint(n) == want
}
