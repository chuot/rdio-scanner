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

package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"rdio-scanner/server-ng/internal/application/fakes"
	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

var historyTime = time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

type historyFixture struct {
	calls   *fakes.Calls
	systems *fakes.Systems
	groups  *fakes.Groups
	tags    *fakes.Tags

	search *SearchCallsQueryHandler
	get    *GetCallQueryHandler
}

func newHistoryFixture() *historyFixture {
	fixture := &historyFixture{
		calls: fakes.NewCalls(),
		systems: &fakes.Systems{Stored: domain.Systems{
			{
				ID:        1,
				SystemRef: 4,
				Talkgroups: domain.Talkgroups{
					{ID: 100, TalkgroupRef: 1514000, GroupIDs: []uint64{1}, TagID: 11},
					{ID: 101, TalkgroupRef: 1514001, GroupIDs: []uint64{2}, TagID: 10},
				},
			},
			{
				ID:        2,
				SystemRef: 5,
				Talkgroups: domain.Talkgroups{
					{ID: 200, TalkgroupRef: 222000, GroupIDs: []uint64{1}, TagID: 10},
				},
			},
		}},
		groups: &fakes.Groups{Stored: domain.Groups{
			{ID: 1, Label: "Fire"},
			{ID: 2, Label: "EMS"},
		}},
		tags: &fakes.Tags{Stored: domain.Tags{
			{ID: 10, Label: "Untagged"},
			{ID: 11, Label: "Fire"},
		}},
	}

	fixture.search = NewSearchCallsQueryHandler(fixture.calls, fixture.systems, fixture.groups, fixture.tags)
	fixture.get = NewGetCallQueryHandler(fixture.calls)

	return fixture
}

func (fixture *historyFixture) store(id uint64, systemRef, talkgroupRef uint, at time.Time) {
	call := domain.NewCall()

	call.ID = id
	call.Timestamp = at
	call.System = domain.System{ID: uint64(systemRef), SystemRef: systemRef}
	call.Talkgroup = domain.Talkgroup{ID: uint64(talkgroupRef), TalkgroupRef: talkgroupRef}

	fixture.calls.Stored[id] = call
}

func (fixture *historyFixture) searchHistory(criteria domain.CallSearch, scope domain.AccessScope) (ports.CallSearchPage, error) {
	return fixture.search.Handle(context.Background(), SearchCallsQuery{Criteria: criteria, Scope: scope})
}

// historyCriteria parses a listener payload into criteria, which is what the presentation
// layer does before it builds the query.
func historyCriteria(payload ...map[string]any) domain.CallSearch {
	submitted := map[string]any{}

	for _, values := range payload {
		for key, value := range values {
			submitted[key] = value
		}
	}

	return domain.ParseCallSearch(submitted)
}

func TestGetCallByIdQueryReturnsTheRecordedCall(t *testing.T) {
	fixture := newHistoryFixture()
	fixture.store(7, 4, 1514000, historyTime)

	call, err := fixture.get.Handle(context.Background(), GetCallByIdQuery{ID: 7})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if call.ID != 7 {
		t.Errorf("Handle returned call %d, want the call that was asked for", call.ID)
	}
}

func TestGetCallByIdQueryReportsAMissingCall(t *testing.T) {
	fixture := newHistoryFixture()

	_, err := fixture.get.Handle(context.Background(), GetCallByIdQuery{ID: 7})

	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Handle error = %v, want ports.ErrNotFound, which is what the CAL fetch answers with", err)
	}
}

func TestSearchCallsQueryAppliesTheDefaultPageSize(t *testing.T) {
	fixture := newHistoryFixture()

	for i := 0; i < 250; i++ {
		fixture.store(uint64(i+1), 4, 1514000, historyTime.Add(time.Duration(i)*time.Second))
	}

	page, err := fixture.searchHistory(historyCriteria(), domain.AccessScope{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(page.Results) != 200 {
		t.Errorf("Handle returned %d results, want the default page of 200", len(page.Results))
	}

	if page.Count != 250 {
		t.Errorf("Count = %d, want the total matched, not the page size", page.Count)
	}
}

func TestSearchCallsQuerySortsBothEndsOfTheHistory(t *testing.T) {
	fixture := newHistoryFixture()

	fixture.store(1, 4, 1514000, historyTime)
	fixture.store(2, 4, 1514000, historyTime.Add(time.Hour))

	page, err := fixture.searchHistory(historyCriteria(map[string]any{"sort": float64(-1)}), domain.AccessScope{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if page.Results[0].ID != 2 {
		t.Errorf("Handle = %+v, want a negative sort to start from the newest call", page.Results)
	}

	page, err = fixture.searchHistory(historyCriteria(), domain.AccessScope{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if page.Results[0].ID != 1 {
		t.Errorf("Handle = %+v, want the default to start from the oldest call", page.Results)
	}
}

func TestSearchCallsQueryBoundsTheSubmittedDay(t *testing.T) {
	fixture := newHistoryFixture()

	fixture.store(1, 4, 1514000, historyTime)
	fixture.store(2, 4, 1514000, historyTime.Add(24*time.Hour))

	page, err := fixture.searchHistory(historyCriteria(map[string]any{
		"date": historyTime.Format(time.RFC3339),
	}), domain.AccessScope{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if page.Count != 1 || page.Results[0].ID != 1 {
		t.Errorf("Handle produced %+v with count %d, want only the call inside the submitted day", page.Results, page.Count)
	}
}

func TestSearchCallsQueryExpandsAGroupLabel(t *testing.T) {
	fixture := newHistoryFixture()

	fixture.store(1, 4, 1514000, historyTime)
	fixture.store(2, 4, 1514001, historyTime.Add(time.Second))
	fixture.store(3, 5, 222000, historyTime.Add(2*time.Second))

	page, err := fixture.searchHistory(historyCriteria(map[string]any{"group": "Fire"}), domain.AccessScope{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if page.Count != 2 {
		t.Fatalf("Handle produced %d calls, want the two talkgroups the group stands for", page.Count)
	}

	for _, result := range page.Results {
		if result.TalkgroupRef == 1514001 {
			t.Errorf("Handle included talkgroup %d, which belongs to the EMS group", result.TalkgroupRef)
		}
	}
}

func TestSearchCallsQueryExpandsATagLabel(t *testing.T) {
	fixture := newHistoryFixture()

	fixture.store(1, 4, 1514000, historyTime)
	fixture.store(2, 4, 1514001, historyTime.Add(time.Second))
	fixture.store(3, 5, 222000, historyTime.Add(2*time.Second))

	page, err := fixture.searchHistory(historyCriteria(map[string]any{"tag": "Untagged"}), domain.AccessScope{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if page.Count != 2 {
		t.Fatalf("Handle produced %d calls, want the two talkgroups carrying that tag", page.Count)
	}

	for _, result := range page.Results {
		if result.TalkgroupRef == 1514000 {
			t.Errorf("Handle included talkgroup %d, which carries the other tag", result.TalkgroupRef)
		}
	}
}

// TestSearchCallsQueryLeavesAnUnmatchedLabelUnfiltered pins the legacy behavior: a group
// or tag label that stands for nothing adds no filter at all, so the search returns the
// whole history rather than nothing.
func TestSearchCallsQueryLeavesAnUnmatchedLabelUnfiltered(t *testing.T) {
	fixture := newHistoryFixture()

	fixture.store(1, 4, 1514000, historyTime)
	fixture.store(2, 5, 222000, historyTime.Add(time.Second))

	page, err := fixture.searchHistory(historyCriteria(map[string]any{"group": "Police"}), domain.AccessScope{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if page.Count != 2 {
		t.Errorf("Handle produced %d calls, want the whole history when the label stands for nothing", page.Count)
	}
}

func TestSearchCallsQueryRestrictsToTheCallersScope(t *testing.T) {
	fixture := newHistoryFixture()

	fixture.store(1, 4, 1514000, historyTime)
	fixture.store(2, 5, 222000, historyTime.Add(time.Second))

	scope, _ := domain.ParseAccessScope([]any{
		map[string]any{"id": float64(5), "talkgroups": "*"},
	})

	page, err := fixture.searchHistory(historyCriteria(), scope)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if page.Count != 1 || page.Results[0].SystemRef != 5 {
		t.Errorf("Handle produced %+v with count %d, want only the system the caller is allowed to see", page.Results, page.Count)
	}
}

func TestSearchCallsQueryExpandsALabelWithinTheCallersOwnView(t *testing.T) {
	fixture := newHistoryFixture()

	fixture.store(1, 4, 1514000, historyTime)
	fixture.store(2, 5, 222000, historyTime.Add(time.Second))

	scope, _ := domain.ParseAccessScope([]any{
		map[string]any{"id": float64(5), "talkgroups": "*"},
	})

	page, err := fixture.searchHistory(historyCriteria(map[string]any{"group": "Fire"}), scope)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if page.Count != 1 || page.Results[0].SystemRef != 5 {
		t.Errorf("Handle produced %+v with count %d, want the group narrowed to the systems the caller can see", page.Results, page.Count)
	}
}

func TestSearchCallsQueryCombinesSystemAndTalkgroupFilters(t *testing.T) {
	fixture := newHistoryFixture()

	fixture.store(1, 4, 1514000, historyTime)
	fixture.store(2, 4, 1514001, historyTime.Add(time.Second))
	fixture.store(3, 5, 222000, historyTime.Add(2*time.Second))

	page, err := fixture.searchHistory(historyCriteria(map[string]any{
		"system":    float64(4),
		"talkgroup": float64(1514001),
	}), domain.AccessScope{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if page.Count != 1 || page.Results[0].ID != 2 {
		t.Errorf("Handle produced %+v with count %d, want only the named talkgroup on the named system", page.Results, page.Count)
	}
}
