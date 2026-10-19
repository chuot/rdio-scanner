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

package persistence

import (
	"context"
	"rdio-scanner/internal/application/ports"
	"testing"
	"time"
)

// The four tallies the status view reads. They are kept apart from the call history's page: the
// history view pages calls for a caller who has a scope, and the status view counts calls for the
// operator who administers the whole server.
//
// Every one of them is built on the join the call history uses, which is what keeps a call that is
// still waiting out its hold out of the history — and out of every count here. A dashboard that
// counted held calls would report calls the operator then cannot find.

// TestCountCallsSinceCountsTheCallsAtOrAfterTheInstant names the bound: at or after, not after.
func TestCountCallsSinceCountsTheCallsAtOrAfterTheInstant(t *testing.T) {
	fixture := newCallsFixture(t)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	fixture.storeCall(t, 10001, 1514000, base.Add(-2*time.Hour))
	fixture.storeCall(t, 10001, 1514000, base.Add(-1*time.Nanosecond))
	fixture.storeCall(t, 10001, 1514001, base)
	fixture.storeCall(t, 10001, 1514001, base.Add(30*time.Minute))
	fixture.storeCall(t, 10001, 1514002, base.Add(90*time.Minute))

	count, err := fixture.calls.CountCallsSince(context.Background(), base)
	if err != nil {
		t.Fatalf("CountCallsSince: %v", err)
	}

	if count != 3 {
		t.Errorf("CountCallsSince = %d, want the 3 calls at or after the instant, not the 5 stored", count)
	}
}

// TestCountCallsByHourBucketsTheWindowIntoWholeHours is the chart's data. The buckets are whole
// hours, oldest first, and an hour the window contains but the history has nothing in is not
// reported here — the query handler answers it as a zero, so that the chart's shape does not depend
// on what the database happened to contain.
func TestCountCallsByHourBucketsTheWindowIntoWholeHours(t *testing.T) {
	fixture := newCallsFixture(t)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	from := base.Add(-5 * time.Hour)

	fixture.storeCall(t, 10001, 1514000, from.Add(1*time.Minute))
	fixture.storeCall(t, 10001, 1514001, from.Add(59*time.Minute))
	// The hour from+1h is deliberately empty: the window contains it and the history does not.
	fixture.storeCall(t, 10001, 1514001, from.Add(2*time.Hour+30*time.Minute))
	fixture.storeCall(t, 10001, 1514002, from.Add(3*time.Hour+45*time.Minute))
	fixture.storeCall(t, 10001, 1514002, from.Add(4*time.Hour+10*time.Minute))

	// Older than the window, and the only call that must not appear in any bucket.
	fixture.storeCall(t, 10001, 1514000, from.Add(-1*time.Minute))

	counts, err := fixture.calls.CountCallsByHour(context.Background(), from)
	if err != nil {
		t.Fatalf("CountCallsByHour: %v", err)
	}

	want := []struct {
		hour  time.Time
		count uint64
	}{
		{from, 2},
		{from.Add(2 * time.Hour), 1},
		{from.Add(3 * time.Hour), 1},
		{from.Add(4 * time.Hour), 1},
	}

	if len(counts) != len(want) {
		t.Fatalf("CountCallsByHour returned %d buckets, want %d: %v", len(counts), len(want), counts)
	}

	for i, want := range want {
		got := counts[i]

		if !got.Hour.Equal(want.hour) {
			t.Errorf("bucket %d begins at %s, want %s", i, got.Hour, want.hour)
		}

		if got.Hour.UnixMilli()%3_600_000 != 0 {
			t.Errorf("bucket %d begins at %s, which is not a whole hour", i, got.Hour)
		}

		if got.Count != want.count {
			t.Errorf("bucket %s carries %d calls, want %d", got.Hour, got.Count, want.count)
		}
	}

	for _, count := range counts {
		if count.Hour.Equal(from.Add(time.Hour)) {
			t.Errorf("the empty hour %s is reported, when the window's empty hours are the query handler's to fill", count.Hour)
		}
	}
}

// TestCallActivityLeavesOutEveryCallStillWaitingOutItsHold is the rule that decides what a dashboard
// number means. A call written into the delayed table is recorded but not yet part of the history, and
// it appears in no count, no bucket, no talkgroup tally, and no recent row.
func TestCallActivityLeavesOutEveryCallStillWaitingOutItsHold(t *testing.T) {
	fixture := newCallsFixture(t)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	from := base.Add(-3 * time.Hour)

	held := fixture.storeCall(t, 10001, 1514000, base.Add(-1*time.Hour))
	fixture.delay(t, held.ID, base.Add(10*time.Minute))

	fixture.storeCall(t, 10001, 1514000, base.Add(-2*time.Hour))
	fixture.storeCall(t, 10001, 1514001, base)

	count, err := fixture.calls.CountCallsSince(context.Background(), from)
	if err != nil {
		t.Fatalf("CountCallsSince: %v", err)
	}

	if count != 2 {
		t.Errorf("CountCallsSince = %d, want 2: the held call %d is not part of the history yet", count, held.ID)
	}

	counts, err := fixture.calls.CountCallsByHour(context.Background(), from)
	if err != nil {
		t.Fatalf("CountCallsByHour: %v", err)
	}

	if total := sumHourCounts(counts); total != 2 {
		t.Errorf("the hour buckets carry %d calls between them, want 2: the held call is in none of them", total)
	}

	tallies, err := fixture.calls.CountCallsByTalkgroup(context.Background(), from, 10)
	if err != nil {
		t.Fatalf("CountCallsByTalkgroup: %v", err)
	}

	if len(tallies) != 2 {
		t.Fatalf("CountCallsByTalkgroup tallies %d talkgroups, want 2: the held call is counted against none of them", len(tallies))
	}

	for _, tally := range tallies {
		if tally.TalkgroupRef == 1514000 && tally.Count != 1 {
			t.Errorf("talkgroup %d carries %d calls, want 1: the held call on it is not counted", tally.TalkgroupRef, tally.Count)
		}
	}

	recent, err := fixture.calls.RecentCalls(context.Background(), 10)
	if err != nil {
		t.Fatalf("RecentCalls: %v", err)
	}

	for _, summary := range recent {
		if summary.ID == held.ID {
			t.Errorf("RecentCalls returned the held call %d, which the call history does not show either", held.ID)
		}
	}
}

// TestCountCallsByTalkgroupOrdersByDescendingCountAndCutsAtTheLimit is the busiest-talkgroups list:
// most first, and no more than the caller asked for.
func TestCountCallsByTalkgroupOrdersByDescendingCountAndCutsAtTheLimit(t *testing.T) {
	fixture := newCallsFixture(t)

	fixture.storeSystem(t, 20001, 2014000)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)
	from := base.Add(-24 * time.Hour)

	// 1514002 is the busiest, 1514001 next, 1514000 and the second system's talkgroup quiet.
	fixture.storeCall(t, 10001, 1514002, base.Add(-3*time.Hour))
	fixture.storeCall(t, 10001, 1514002, base.Add(-2*time.Hour))
	fixture.storeCall(t, 10001, 1514002, base.Add(-1*time.Hour))
	fixture.storeCall(t, 10001, 1514001, base.Add(-2*time.Hour))
	fixture.storeCall(t, 10001, 1514001, base.Add(-1*time.Hour))
	fixture.storeCall(t, 10001, 1514000, base.Add(-1*time.Hour))
	fixture.storeCall(t, 20001, 2014000, base.Add(-1*time.Hour))
	// Outside the window, and therefore in no tally.
	fixture.storeCall(t, 10001, 1514000, base.Add(-30*time.Hour))

	tallies, err := fixture.calls.CountCallsByTalkgroup(context.Background(), from, 3)
	if err != nil {
		t.Fatalf("CountCallsByTalkgroup: %v", err)
	}

	if len(tallies) != 3 {
		t.Fatalf("CountCallsByTalkgroup returned %d tallies, want the 3 the limit asks for", len(tallies))
	}

	want := []struct {
		systemRef    uint
		talkgroupRef uint
		count        uint64
	}{
		{10001, 1514002, 3},
		{10001, 1514001, 2},
		{10001, 1514000, 1},
	}

	for i, want := range want {
		got := tallies[i]

		if got.SystemRef != want.systemRef || got.TalkgroupRef != want.talkgroupRef {
			t.Errorf("tally %d is system %d talkgroup %d, want system %d talkgroup %d",
				i, got.SystemRef, got.TalkgroupRef, want.systemRef, want.talkgroupRef)
		}

		if got.Count != want.count {
			t.Errorf("tally %d carries %d calls, want %d", i, got.Count, want.count)
		}

		if i > 0 && got.Count > tallies[i-1].Count {
			t.Errorf("tally %d carries %d calls, more than the tally before it: the list is not ordered by descending count", i, got.Count)
		}
	}

	// The list names itself: the operator reads a talkgroup, not a pair of identifiers.
	for _, tally := range tallies {
		if tally.Name == "" {
			t.Errorf("tally for talkgroup %d carries no name, which leaves the view naming a number the operator cannot read", tally.TalkgroupRef)
		}
	}
}

// TestRecentCallsReturnsTheNewestCallsFirstWithoutTheirAudio. A status view is asked every few
// seconds, and the audio column is the heaviest thing in the table.
func TestRecentCallsReturnsTheNewestCallsFirstWithoutTheirAudio(t *testing.T) {
	fixture := newCallsFixture(t)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	first := fixture.storeCall(t, 10001, 1514000, base.Add(-3*time.Hour))
	second := fixture.storeCall(t, 10001, 1514001, base.Add(-2*time.Hour))
	third := fixture.storeCall(t, 10001, 1514002, base.Add(-1*time.Hour))

	recent, err := fixture.calls.RecentCalls(context.Background(), 2)
	if err != nil {
		t.Fatalf("RecentCalls: %v", err)
	}

	if len(recent) != 2 {
		t.Fatalf("RecentCalls returned %d calls, want the 2 the limit asks for", len(recent))
	}

	if recent[0].ID != third.ID || recent[1].ID != second.ID {
		t.Errorf("RecentCalls returned calls %d and %d, want the newest two: %d then %d",
			recent[0].ID, recent[1].ID, third.ID, second.ID)
	}

	if first.ID == recent[0].ID || first.ID == recent[1].ID {
		t.Errorf("RecentCalls returned the oldest call %d, which the newest-first order leaves out", first.ID)
	}

	if recent[0].TalkgroupRef != 1514002 || recent[0].SystemRef != 10001 {
		t.Errorf("the newest row names system %d talkgroup %d, want system 10001 talkgroup 1514002",
			recent[0].SystemRef, recent[0].TalkgroupRef)
	}

	if !recent[0].Timestamp.Equal(base.Add(-1 * time.Hour)) {
		t.Errorf("the newest row is stamped %s, want %s", recent[0].Timestamp, base.Add(-1*time.Hour))
	}
}

// TestCallActivityWithNothingInTheWindowAnswersZeroAndNoRows: a deployment that has recorded nothing
// is a state the status view has to be able to report, and it is reported as zero and not as a
// failure.
func TestCallActivityWithNothingInTheWindowAnswersZeroAndNoRows(t *testing.T) {
	fixture := newCallsFixture(t)

	base := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	quiet := fixture.storeCall(t, 10001, 1514000, base.Add(-40*time.Hour))

	from := base.Add(-24 * time.Hour)

	count, err := fixture.calls.CountCallsSince(context.Background(), from)
	if err != nil {
		t.Fatalf("CountCallsSince: %v", err)
	}

	if count != 0 {
		t.Errorf("CountCallsSince = %d, want 0 for a window the history has nothing in", count)
	}

	counts, err := fixture.calls.CountCallsByHour(context.Background(), from)
	if err != nil {
		t.Fatalf("CountCallsByHour: %v", err)
	}

	if len(counts) != 0 {
		t.Errorf("CountCallsByHour returned %d buckets, want none: the empty hours are the query handler's to fill", len(counts))
	}

	tallies, err := fixture.calls.CountCallsByTalkgroup(context.Background(), from, 5)
	if err != nil {
		t.Fatalf("CountCallsByTalkgroup: %v", err)
	}

	if len(tallies) != 0 {
		t.Errorf("CountCallsByTalkgroup returned %d tallies, want none", len(tallies))
	}

	recent, err := fixture.calls.RecentCalls(context.Background(), 5)
	if err != nil {
		t.Fatalf("RecentCalls: %v", err)
	}

	// The newest recorded call is a fact about the history and not about the window: a deployment whose
	// last call was yesterday still has a last call, and the status view says when it was rather than
	// saying nothing.
	if len(recent) != 1 || recent[0].ID != quiet.ID {
		t.Errorf("RecentCalls returned %v, want the one recorded call %d: the newest rows are not bounded by the window the tallies are",
			recent, quiet.ID)
	}
}

func sumHourCounts(counts []ports.CallHourCount) uint64 {
	var total uint64

	for _, count := range counts {
		total += count.Count
	}

	return total
}
