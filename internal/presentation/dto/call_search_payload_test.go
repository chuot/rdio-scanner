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
	"testing"
	"time"

	"rdio-scanner/server-ng/internal/application/ports"
)

// TestCallSearchPageMatchesTheCapturedPage is the byte-for-byte half of CA-004.1 for a history page,
// and it is the reason the payload is written as fields in a fixed order instead of as a map: the
// captured frame carries `count, dateStart, dateStop, options, results` and each row carries
// `id, system, talkgroup, dateTime`, which is the order the legacy server declares them and not the
// order a map would sort them into.
func TestCallSearchPageMatchesTheCapturedPage(t *testing.T) {
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	page := ports.CallSearchPage{
		Count:     4,
		DateStart: at,
		DateStop:  at.Add(4 * time.Minute),
		Results: []ports.CallSummary{
			{ID: 1, SystemRef: 10001, TalkgroupRef: 1514000, Timestamp: at},
			{ID: 2, SystemRef: 10002, TalkgroupRef: 1515000, Timestamp: at.Add(time.Minute)},
			{ID: 3, SystemRef: 10001, TalkgroupRef: 1514000, Timestamp: at.Add(3 * time.Minute)},
			{ID: 4, SystemRef: 10001, TalkgroupRef: 1514000, Timestamp: at.Add(4 * time.Minute)},
		},
	}

	payload := CallSearchPagePayload{
		Page:     page,
		Criteria: map[string]any{"limit": float64(200), "offset": float64(0), "sort": "date"},
	}

	got := marshal(t, payload)
	want := readGolden(t, "payload-lcl.json")

	if got != want {
		t.Errorf("call search page =\n  %s\nwant\n  %s", got, want)
	}
}

// The listener gets back the criteria the server understood, and only those. A criterion that is the
// wrong shape is not corrected, not reported, and not mentioned again: it is simply absent from the
// answer, which is how the listener can tell that its date filter was ignored.
func TestCallSearchPageEchoesOnlyTheCriteriaItUnderstood(t *testing.T) {
	page := CallSearchPagePayload{
		Criteria: map[string]any{
			"date":      "last Tuesday",
			"group":     "Fire",
			"limit":     float64(200),
			"offset":    float64(0),
			"sort":      "date",
			"system":    float64(10001),
			"tag":       float64(5),
			"talkgroup": float64(1514000),
		},
	}

	got := marshal(t, page)

	want := `{"group":"Fire","limit":200,"offset":0,"system":10001,"talkgroup":1514000}`

	if !carriesKey(t, got, "options", want) {
		t.Errorf("criteria echo = %s, want %s", got, want)
	}
}

// A criterion that is understood is echoed as the listener wrote it, even when the search itself will
// not use that value: the legacy server clamps the page size when it runs the query and echoes what
// it was asked for, so an answer can promise 600 and deliver 500.
func TestCallSearchPageEchoesWhatWasAskedForNotWhatTheSearchUsed(t *testing.T) {
	page := CallSearchPagePayload{Criteria: map[string]any{"limit": float64(600)}}

	got := marshal(t, page)

	if !carriesKey(t, got, "options", `{"limit":600}`) {
		t.Errorf("criteria echo = %s, want the 600 the listener asked for, not the 500 the search used", got)
	}
}

// A search with nothing in its payload is answered with an empty echo, not a missing one and not a
// list of defaults.
func TestCallSearchPageEchoesNothingAsAnEmptyObject(t *testing.T) {
	for _, criteria := range []map[string]any{nil, {}} {
		got := marshal(t, CallSearchPagePayload{Criteria: criteria})

		if !carriesKey(t, got, "options", "{}") {
			t.Errorf("an empty search echoes %s, want an empty object", got)
		}
	}
}

// A search that matches nothing still answers with a list, and with the Unix epoch at both ends of a
// span that does not exist — see TestSearchCallsWithNothingToReturnReportsTheUnixEpoch for where that
// comes from and why it is kept.
func TestCallSearchPageAnswersAnEmptySearchWithAnEmptyListAndTheUnixEpoch(t *testing.T) {
	got := marshal(t, CallSearchPagePayload{
		Page:     ports.CallSearchPage{DateStart: time.UnixMilli(0), DateStop: time.UnixMilli(0)},
		Criteria: map[string]any{"limit": float64(200)},
	})

	// The epoch is rendered in whatever time zone the server runs in, because the legacy server
	// reaches it through time.UnixMilli. The capture was taken on a machine set to UTC and says
	// 1970-01-01T00:00:00Z; a server in another zone says the same moment in its own offset, and a
	// client sees the same thing it sees today.
	epoch := time.UnixMilli(0).Format(time.RFC3339)

	for key, want := range map[string]string{
		"results":   "[]",
		"count":     "0",
		"dateStart": `"` + epoch + `"`,
		"dateStop":  `"` + epoch + `"`,
	} {
		if !carriesKey(t, got, key, want) {
			t.Errorf("an empty search page = %s, want %s:%s", got, key, want)
		}
	}
}

// A history row renders the moment with its milliseconds and a call payload renders the same call
// without them. The legacy server marshals the row's moment as a Go time and formats the call's by
// hand, so a call recorded at half past a second appears in the two frames of one session with two
// different timestamps. It is preserved because a client that lines a row up with the call it fetches
// has to be able to make the same sense of it that a client today does.
func TestCallSearchRowKeepsTheMillisecondsACallPayloadDrops(t *testing.T) {
	at := time.Date(2026, 10, 6, 12, 0, 0, 500_000_000, time.UTC)

	page := CallSearchPagePayload{Page: ports.CallSearchPage{Results: []ports.CallSummary{{ID: 1, Timestamp: at}}}}

	got := marshal(t, page)

	if !carriesKey(t, got, "results", `[{"id":1,"system":0,"talkgroup":0,"dateTime":"2026-10-06T12:00:00.5Z"}]`) {
		t.Errorf("a history row at half past a second = %s, want the fraction kept", got)
	}
}
