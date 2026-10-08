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

package fakes

import (
	"context"
	"errors"
	"testing"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

func testCall(id uint64, systemRef, talkgroupRef uint, at time.Time) domain.Call {
	return domain.Call{
		ID: id,
		System: domain.System{
			ID:        uint64(systemRef),
			SystemRef: systemRef,
		},
		Talkgroup: domain.Talkgroup{
			ID:           uint64(talkgroupRef),
			TalkgroupRef: talkgroupRef,
		},
		Timestamp: at,
	}
}

func baseTime() time.Time {
	return time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)
}

func TestCallsSaveAssignsIdentifiersAndGetFindsThem(t *testing.T) {
	calls := NewCalls()

	saved, err := calls.SaveCall(context.Background(), testCall(0, 4, 1514000, baseTime()))
	if err != nil {
		t.Fatalf("SaveCall: %v", err)
	}

	if saved.ID == 0 {
		t.Fatal("SaveCall must assign an identifier to a call that has none")
	}

	found, err := calls.GetCall(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}

	if found.ID != saved.ID {
		t.Errorf("GetCall returned call %d, want %d", found.ID, saved.ID)
	}
}

func TestCallsGetMissingCallReportsNotFound(t *testing.T) {
	_, err := NewCalls().GetCall(context.Background(), 99)

	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("GetCall error = %v, want ports.ErrNotFound", err)
	}
}

func TestCallsFindDuplicateCandidatesFiltersByAggregatesAndWindow(t *testing.T) {
	calls := NewCalls()

	calls.Stored = map[uint64]domain.Call{
		1: testCall(1, 4, 1514000, baseTime()),
		2: testCall(2, 4, 1514000, baseTime().Add(300*time.Millisecond)),
		3: testCall(3, 4, 1514000, baseTime().Add(5*time.Second)),
		4: testCall(4, 5, 1514000, baseTime()),
		5: testCall(5, 4, 1514001, baseTime()),
	}

	candidates, err := calls.FindDuplicateCandidates(context.Background(), 4, 1514000, baseTime().Add(-time.Second), baseTime().Add(time.Second))
	if err != nil {
		t.Fatalf("FindDuplicateCandidates: %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("FindDuplicateCandidates produced %d calls, want the two on the same system and talkgroup inside the window", len(candidates))
	}

	if candidates[0].ID != 1 || candidates[1].ID != 2 {
		t.Errorf("FindDuplicateCandidates = %+v, want them ordered by timestamp", candidates)
	}
}

func TestCallsSearchPagesSortsAndExcludesDelayed(t *testing.T) {
	calls := NewCalls()

	calls.Stored = map[uint64]domain.Call{
		1: testCall(1, 4, 1514000, baseTime()),
		2: testCall(2, 4, 1514001, baseTime().Add(time.Hour)),
		3: testCall(3, 5, 1514000, baseTime().Add(2*time.Hour)),
	}

	delayed := testCall(4, 4, 1514000, baseTime().Add(3*time.Hour))
	delayed.Delayed = true
	calls.Stored[4] = delayed

	page, err := calls.SearchCalls(context.Background(), ports.CallSearch{Limit: 200, Ascending: true})
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	if page.Count != 3 {
		t.Errorf("Count = %d, want the delayed call left out of the history", page.Count)
	}

	if len(page.Results) != 3 {
		t.Fatalf("SearchCalls returned %d results, want all three visible calls", len(page.Results))
	}

	if page.Results[0].ID != 1 || page.Results[2].ID != 3 {
		t.Errorf("SearchCalls = %+v, want ascending order", page.Results)
	}

	if !page.DateStart.Equal(baseTime()) {
		t.Errorf("DateStart = %v, want the earliest matching call", page.DateStart)
	}

	if want := baseTime().Add(2 * time.Hour); !page.DateStop.Equal(want) {
		t.Errorf("DateStop = %v, want the latest matching call across the whole history, not just this page", want)
	}

	page, err = calls.SearchCalls(context.Background(), ports.CallSearch{Limit: 200})
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	if page.Results[0].ID != 3 {
		t.Errorf("SearchCalls = %+v, want descending order when the criteria does not ask for ascending", page.Results)
	}

	page, err = calls.SearchCalls(context.Background(), ports.CallSearch{Limit: 1, Offset: 1, Ascending: true})
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	if len(page.Results) != 1 || page.Results[0].ID != 2 {
		t.Errorf("SearchCalls = %+v, want the second call only", page.Results)
	}

	if page.Count != 3 {
		t.Errorf("Count = %d, want the total across the whole history, not the page size", page.Count)
	}
}

func TestCallsSearchHonoursScopeSystemAndTalkgroup(t *testing.T) {
	calls := NewCalls()

	calls.Stored = map[uint64]domain.Call{
		1: testCall(1, 4, 1514000, baseTime()),
		2: testCall(2, 4, 1514001, baseTime().Add(time.Hour)),
		3: testCall(3, 5, 1514000, baseTime().Add(2*time.Hour)),
	}

	scoped, _ := domain.ParseAccessScope([]any{map[string]any{"id": float64(4), "talkgroups": []any{float64(1514000)}}})

	page, err := calls.SearchCalls(context.Background(), ports.CallSearch{Scope: scoped, Limit: 200, Ascending: true})
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	if page.Count != 1 || page.Results[0].ID != 1 {
		t.Errorf("SearchCalls scoped to one talkgroup produced %+v with count %d, want only call 1", page.Results, page.Count)
	}

	page, err = calls.SearchCalls(context.Background(), ports.CallSearch{SystemRef: 4, Limit: 200, Ascending: true})
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	if page.Count != 2 {
		t.Errorf("Count = %d, want both calls on system reference 4", page.Count)
	}

	page, err = calls.SearchCalls(context.Background(), ports.CallSearch{
		SystemRef:       4,
		TalkgroupRef:    1514001,
		Limit:           200,
		Ascending:       true,
		GroupTalkgroups: []domain.TalkgroupScope{{SystemRef: 5, TalkgroupRefs: []uint{1514000}}},
	})
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	if page.Count != 0 {
		t.Errorf("Count = %d, want nothing: the system criterion and the group criterion cannot both hold", page.Count)
	}
}

func TestCallsSearchDateBounds(t *testing.T) {
	calls := NewCalls()

	calls.Stored = map[uint64]domain.Call{
		1: testCall(1, 4, 1514000, baseTime()),
		2: testCall(2, 4, 1514000, baseTime().Add(24*time.Hour)),
	}

	from, to, _ := domain.LogSearch{Date: baseTime(), Ascending: true}.DateWindow()

	page, err := calls.SearchCalls(context.Background(), ports.CallSearch{From: from, To: to, Limit: 200, Ascending: true})
	if err != nil {
		t.Fatalf("SearchCalls: %v", err)
	}

	if page.Count != 1 || page.Results[0].ID != 1 {
		t.Errorf("SearchCalls bounded to one day produced %+v, want only the call inside the day", page.Results)
	}
}

func TestCallsDeleteCallsBeforeReportsWhatWent(t *testing.T) {
	calls := NewCalls()

	calls.Stored = map[uint64]domain.Call{
		1: testCall(1, 4, 1514000, baseTime()),
		2: testCall(2, 4, 1514000, baseTime().Add(48*time.Hour)),
	}

	removed, err := calls.DeleteCallsBefore(context.Background(), baseTime().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteCallsBefore: %v", err)
	}

	if removed != 1 {
		t.Errorf("DeleteCallsBefore removed %d calls, want 1", removed)
	}

	if _, err := calls.GetCall(context.Background(), 1); !errors.Is(err, ports.ErrNotFound) {
		t.Error("the older call must be gone")
	}

	if _, err := calls.GetCall(context.Background(), 2); err != nil {
		t.Errorf("the newer call must survive: %v", err)
	}
}

func TestLogsSearchAppliesTheDateWindowAndPaging(t *testing.T) {
	logs := &Logs{Stored: []domain.LogEntry{
		{ID: 1, DateTime: baseTime(), Level: domain.LogLevelWarn, Message: "one"},
		{ID: 2, DateTime: baseTime().Add(24 * 2 * time.Hour), Level: domain.LogLevelError, Message: "two"},
	}}

	page, err := logs.SearchLogs(context.Background(), domain.ParseLogSearch(map[string]any{"level": domain.LogLevelError}))
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Count != 1 || page.Results[0].ID != 2 {
		t.Errorf("SearchLogs by level produced %+v, want only the error entry", page.Results)
	}

	page, err = logs.SearchLogs(context.Background(), domain.ParseLogSearch(map[string]any{"limit": float64(0)}))
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if len(page.Results) != 0 {
		t.Errorf("SearchLogs with an explicit zero limit returned %d entries, want none", len(page.Results))
	}

	if page.Count != 2 {
		t.Errorf("Count = %d, want the total matched regardless of the limit", page.Count)
	}
}

// TestLogsSearchAppliesTheSourceAndTextCriteriaAndCountsEverySource keeps the in-memory log repository a
// stand-in for the stored one: a component test that filters by subsystem or by text has to be answered
// the way the real repository answers it, or the component test proves nothing.
//
// An entry written without a source is reported as legacy, which is what the stored repository does with
// the rows the other server writes. Recorded is the other half: it reports what a writer actually wrote,
// blank included, which is what the guard over the writers reads.
func TestLogsSearchAppliesTheSourceAndTextCriteriaAndCountsEverySource(t *testing.T) {
	logs := &Logs{Stored: []domain.LogEntry{
		{ID: 1, DateTime: baseTime(), Level: domain.LogLevelError, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming stopped"},
		{ID: 2, DateTime: baseTime().Add(time.Minute), Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Message: "the watcher for /data/incoming stopped"},
		{ID: 3, DateTime: baseTime().Add(2 * time.Minute), Level: domain.LogLevelWarn, Source: domain.LogSourceIngest, Message: "the recording could not be ingested"},
		{ID: 4, DateTime: baseTime().Add(3 * time.Minute), Level: domain.LogLevelInfo, Source: domain.LogSourceUpload, Message: "the upload was refused: 100% full"},
		{ID: 5, DateTime: baseTime().Add(4 * time.Minute), Level: domain.LogLevelInfo, Message: "written by a server that names no subsystem"},
	}}

	for _, tc := range []struct {
		what   string
		search domain.LogSearch
		want   []uint64
		counts []ports.LogSourceCount
	}{
		{
			"one subsystem",
			domain.LogSearch{Source: domain.LogSourceDirwatch, Limit: domain.DefaultLogSearchLimit},
			[]uint64{1, 2},
			[]ports.LogSourceCount{
				{Source: domain.LogSourceDirwatch, Count: 2},
				{Source: domain.LogSourceIngest, Count: 1},
				{Source: domain.LogSourceLegacy, Count: 1},
				{Source: domain.LogSourceUpload, Count: 1},
			},
		},
		{
			"the text the operator typed, in any case",
			domain.LogSearch{Text: "COULD NOT", Limit: domain.DefaultLogSearchLimit},
			[]uint64{3},
			[]ports.LogSourceCount{{Source: domain.LogSourceIngest, Count: 1}},
		},
		{
			"the text the operator typed, where a percent sign is a percent sign",
			domain.LogSearch{Text: "100%", Limit: domain.DefaultLogSearchLimit},
			[]uint64{4},
			[]ports.LogSourceCount{{Source: domain.LogSourceUpload, Count: 1}},
		},
		{
			"legacy, which is where an entry naming nothing is filed",
			domain.LogSearch{Source: domain.LogSourceLegacy, Limit: domain.DefaultLogSearchLimit},
			[]uint64{5},
			[]ports.LogSourceCount{
				{Source: domain.LogSourceDirwatch, Count: 2},
				{Source: domain.LogSourceIngest, Count: 1},
				{Source: domain.LogSourceLegacy, Count: 1},
				{Source: domain.LogSourceUpload, Count: 1},
			},
		},
		{
			"a page far smaller than the match, which does not shrink the counts",
			domain.LogSearch{Limit: 1},
			[]uint64{1},
			[]ports.LogSourceCount{
				{Source: domain.LogSourceDirwatch, Count: 2},
				{Source: domain.LogSourceIngest, Count: 1},
				{Source: domain.LogSourceLegacy, Count: 1},
				{Source: domain.LogSourceUpload, Count: 1},
			},
		},
	} {
		tc.search.Ascending = true

		page, err := logs.SearchLogs(context.Background(), tc.search)
		if err != nil {
			t.Fatalf("SearchLogs for %s: %v", tc.what, err)
		}

		if len(page.Results) != len(tc.want) {
			t.Fatalf("%s returned %d entries, want %d", tc.what, len(page.Results), len(tc.want))
		}

		for i, id := range tc.want {
			if page.Results[i].ID != id {
				t.Errorf("%s returned entry %d at position %d, want entry %d", tc.what, page.Results[i].ID, i, id)
			}
		}

		if len(page.SourceCounts) != len(tc.counts) {
			t.Fatalf("%s reports %d source counts %v, want %v", tc.what, len(page.SourceCounts), page.SourceCounts, tc.counts)
		}

		for i, want := range tc.counts {
			if page.SourceCounts[i] != want {
				t.Errorf("%s reports %s %d at position %d, want %s %d", tc.what, page.SourceCounts[i].Source, page.SourceCounts[i].Count, i, want.Source, want.Count)
			}
		}
	}

	// The entry that names nothing is reported as legacy by the search, and Recorded still reports what
	// the writer actually wrote.
	page, err := logs.SearchLogs(context.Background(), domain.LogSearch{Limit: domain.DefaultLogSearchLimit, Ascending: true})
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}

	if page.Results[4].Source != domain.LogSourceLegacy {
		t.Errorf("the search reports source %q for an entry written without one, want %q", page.Results[4].Source, domain.LogSourceLegacy)
	}

	if got := logs.Recorded()[4].Source; got != "" {
		t.Errorf("Recorded reports source %q for an entry written without one, want the blank the writer wrote: the guard over the writers reads this, not the search", got)
	}
}

func TestTokensRollingWindowRetiresTheOldest(t *testing.T) {
	tokens := NewTokens()

	issued := []string{}

	for i := 0; i < 6; i++ {
		token, err := tokens.Issue(context.Background())
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}

		issued = append(issued, token)
	}

	if err := tokens.Validate(context.Background(), issued[0]); err == nil {
		t.Error("the first token must be retired once a sixth is issued")
	}

	if err := tokens.Validate(context.Background(), issued[1]); err != nil {
		t.Errorf("the second token must still be accepted: %v", err)
	}

	if err := tokens.Revoke(context.Background(), issued[1]); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if err := tokens.Validate(context.Background(), issued[1]); err == nil {
		t.Error("a revoked token must no longer be accepted")
	}
}

func TestConverterLeavesDisabledAudioAloneAndRenamesConvertedAudio(t *testing.T) {
	converter := NewConverter()

	in := ports.AudioFile{Filename: "call.wav", Mime: "audio/wav", Data: []byte("audio")}

	out, err := converter.Convert(context.Background(), in, domain.AudioConversionDisabled, nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if out.Filename != "call.wav" || out.Mime != "audio/wav" {
		t.Errorf("Convert with conversion disabled = %+v, want the input untouched", out)
	}

	out, err = converter.Convert(context.Background(), in, domain.AudioConversionEnabledLoudNorm, &ports.AudioMetadata{Album: "Dispatch"})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	if out.Filename != "call.m4a" || out.Mime != "audio/mp4" {
		t.Errorf("Convert = %+v, want the converted name and media type", out)
	}

	if converter.Metadata[1] == nil || converter.Metadata[1].Album != "Dispatch" {
		t.Error("the metadata asked for must reach the converter")
	}

	if converter.Modes[1] != domain.AudioConversionEnabledLoudNorm {
		t.Errorf("Convert recorded mode %d, want the level it was asked for", converter.Modes[1])
	}
}

func TestConverterReportsWhenItIsNotAvailable(t *testing.T) {
	converter := NewConverter()
	converter.Available = false

	_, err := converter.Convert(context.Background(), ports.AudioFile{Filename: "call.wav"}, domain.AudioConversionEnabled, nil)

	if err == nil {
		t.Error("a converter that is not installed must report it, so the use case can keep the original audio")
	}
}

func TestHubDeliversOnlyToSessionsThatGrantedAndAskedForTheCall(t *testing.T) {
	hub := NewHub()
	hub.Restricted = true

	hub.Open("watcher", "10.0.0.1")
	hub.Open("silent", "10.0.0.2")
	hub.Open("unscoped", "10.0.0.3")

	scoped := domain.Access{}.ApplyMap(map[string]any{"code": "c", "ident": "dispatch", "systems": "*"})

	hub.GrantAccess("watcher", scoped)
	hub.GrantAccess("silent", scoped)

	call := testCall(1, 4, 1514000, baseTime())

	hub.SetLivefeed("watcher", domain.ParseLivefeed(map[string]any{"4": map[string]any{"1514000": true}}))

	hub.BroadcastCall(call)

	if len(hub.DeliveredCalls["watcher"]) != 1 {
		t.Error("a session that granted the call and asked for it must receive it")
	}

	if len(hub.DeliveredCalls["silent"]) != 0 {
		t.Error("a session that asked for nothing must receive nothing")
	}

	if len(hub.DeliveredCalls["unscoped"]) != 0 {
		t.Error("a restricted server must not deliver to a session holding no access")
	}
}

func TestHubCountsSessionsPerAccess(t *testing.T) {
	hub := NewHub()

	shared := domain.Access{ID: 1, Code: "a", Ident: "dispatch", Scope: domain.AccessScope{Present: true, Wildcard: true}}
	other := domain.Access{ID: 2, Code: "b", Ident: "dispatch", Scope: domain.AccessScope{Present: true, Wildcard: true}}

	hub.Open("one", "10.0.0.1")
	hub.Open("two", "10.0.0.2")
	hub.Open("three", "10.0.0.3")

	hub.GrantAccess("one", shared)
	hub.GrantAccess("two", shared)
	hub.GrantAccess("three", other)

	if got := hub.AccessSessionCount(1); got != 2 {
		t.Errorf("AccessSessionCount(1) = %d, want the two sessions holding that access", got)
	}

	if got := hub.AccessSessionCount(2); got != 1 {
		t.Errorf("AccessSessionCount(2) = %d, want one session: two accesses sharing a label are not one access", got)
	}

	if got := hub.ListenerCount(); got != 3 {
		t.Errorf("ListenerCount = %d, want 3", got)
	}

	if got := hub.RemoteAddrOf("two"); got != "10.0.0.2" {
		t.Errorf("RemoteAddrOf = %q, want the address the session connected from", got)
	}
}

func TestHubCountsAccessAttemptsPerSession(t *testing.T) {
	hub := NewHub()

	hub.Open("one", "10.0.0.1")
	hub.Open("two", "10.0.0.2")

	for want := uint(1); want <= 3; want++ {
		if got := hub.RecordAccessAttempt("one"); got != want {
			t.Errorf("RecordAccessAttempt = %d, want attempt %d", got, want)
		}
	}

	if got := hub.RecordAccessAttempt("two"); got != 1 {
		t.Errorf("RecordAccessAttempt for another session = %d, want 1: attempts are counted per session", got)
	}

	hub.CloseClient("one", 1000)

	if got := hub.RecordAccessAttempt("one"); got != 1 {
		t.Errorf("RecordAccessAttempt after the session closed = %d, want the count to start over for a new session", got)
	}
}

func TestHubLivefeedAckReflectsTheSelection(t *testing.T) {
	hub := NewHub()
	hub.Open("watcher", "10.0.0.1")

	if !hub.LivefeedWatchesNothing("watcher") {
		t.Error("a session that has made no selection watches nothing")
	}

	hub.SetLivefeed("watcher", domain.ParseLivefeed(map[string]any{"4": map[string]any{"1514000": true}}))

	if hub.LivefeedWatchesNothing("watcher") {
		t.Error("a session that switched a talkgroup on is not watching nothing")
	}
}
