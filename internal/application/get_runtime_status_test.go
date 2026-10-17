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

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The status view: what the server is, what it is running, what it recorded lately, and what it
// complained about lately. One read, one instant, and a shape the view does not have to repair.

var statusNow = time.Date(2026, 6, 14, 9, 30, 0, 0, time.UTC)

// statusWindowStart is where the 24 whole-hour buckets begin: the window ends at the end of the whole
// hour the clock sits in, and not at the instant the read ran.
var statusWindowStart = statusNow.Truncate(time.Hour).Add(time.Hour).Add(-24 * time.Hour)

func newStatusFixture() *statusFixture {
	fixture := &statusFixture{
		state: &fakes.Runtime{
			StartedAt:             statusNow.Add(-2*time.Hour - 14*time.Minute),
			Listeners:             3,
			AdministratorSessions: 1,
			RunningWatchers:       2,
			HeldCalls:             2,
			AudioConversion:       true,
			AudioProbe:            true,
			DatabaseBytes:         4_831_838_208,
			DatabaseKnown:         true,
		},
		activity: &fakes.CallActivity{
			SinceCount: 214,
			Hours: []ports.CallHourCount{
				{Hour: statusWindowStart, Count: 9},
				{Hour: statusWindowStart.Add(11 * time.Hour), Count: 4},
				{Hour: statusWindowStart.Add(23 * time.Hour), Count: 37},
			},
			Busiest: []ports.CallTalkgroupCount{
				{SystemRef: 1, TalkgroupRef: 101, Name: "Dispatch", Label: "T-101", Count: 41},
				{SystemRef: 2, TalkgroupRef: 202, Name: "Fireground", Label: "T-202", Count: 12},
			},
			Recent: []ports.CallSummary{
				{ID: 91827, SystemRef: 1, TalkgroupRef: 101, Timestamp: statusNow.Add(-69 * time.Second), AudioLength: 12400 * time.Millisecond},
			},
		},
		tallies: &fakes.LogTallies{
			Stored: []ports.LogLevelTally{
				{Level: domain.LogLevelError, Source: domain.LogSourceDirwatch, Count: 2},
				{Level: domain.LogLevelError, Source: domain.LogSourceFFmpeg, Count: 1},
				{Level: domain.LogLevelWarn, Source: domain.LogSourceDirwatch, Count: 5},
				{Level: domain.LogLevelWarn, Source: domain.LogSourceDownstream, Count: 6},
			},
		},
		clock: fakes.NewClock(statusNow),
	}

	fixture.handler = NewGetRuntimeStatusHandler(fixture.state, fixture.activity, fixture.tallies, fixture.clock)

	return fixture
}

type statusFixture struct {
	state    *fakes.Runtime
	activity *fakes.CallActivity
	tallies  *fakes.LogTallies
	clock    *fakes.Clock

	handler *GetRuntimeStatusHandler
}

func (fixture *statusFixture) read(t *testing.T) RuntimeStatus {
	t.Helper()

	status, err := fixture.handler.Handle(context.Background(), GetRuntimeStatusQuery{})
	if err != nil {
		t.Fatalf("GetRuntimeStatusQuery: %v", err)
	}

	return status
}

// TestGetRuntimeStatusReportsWhatTheServerIsAndHowLongItHasBeenUp.
func TestGetRuntimeStatusReportsWhatTheServerIsAndHowLongItHasBeenUp(t *testing.T) {
	fixture := newStatusFixture()

	status := fixture.read(t)

	if status.Server.StartedAt != fixture.state.StartedAt {
		t.Errorf("the snapshot names %s as the start, want the instant the server recorded", fixture.state.StartedAt)
	}

	if status.Server.Now != statusNow {
		t.Errorf("the snapshot names %s as now, want the clock's instant %s", status.Server.Now, statusNow)
	}

	if want := 2*time.Hour + 14*time.Minute; status.Server.Uptime != want {
		t.Errorf("the snapshot reports an uptime of %s, want %s", status.Server.Uptime, want)
	}

	if status.Server.DatabaseBytes != fixture.state.DatabaseBytes || !status.Server.DatabaseKnown {
		t.Errorf("the snapshot reports %d bytes known=%v, want %d bytes known=true",
			status.Server.DatabaseBytes, status.Server.DatabaseKnown, fixture.state.DatabaseBytes)
	}
}

// TestGetRuntimeStatusReportsWhatTheServerIsRunningRightNow, and not what it was configured to run.
// Every field here is a count of something that exists at the instant the read ran.
func TestGetRuntimeStatusReportsWhatTheServerIsRunningRightNow(t *testing.T) {
	fixture := newStatusFixture()

	status := fixture.read(t)

	if status.Live.Listeners != 3 {
		t.Errorf("Live.Listeners = %d, want the 3 connections the hub is holding", status.Live.Listeners)
	}

	if status.Live.AdministratorSessions != 1 {
		t.Errorf("Live.AdministratorSessions = %d, want the 1 connection the server is holding", status.Live.AdministratorSessions)
	}

	if status.Live.DirwatchersRunning != 2 {
		t.Errorf("Live.DirwatchersRunning = %d, want the 2 watchers actually watching", status.Live.DirwatchersRunning)
	}

	if status.Live.HeldCalls != 2 {
		t.Errorf("Live.HeldCalls = %d, want the 2 holds armed and not yet run", status.Live.HeldCalls)
	}

	if !status.Live.AudioConversion || !status.Live.AudioProbe {
		t.Errorf("Live reports conversion=%v and probe=%v, want both available",
			status.Live.AudioConversion, status.Live.AudioProbe)
	}

	// A converter that is not there is a fact the operator has to be told, and it is the same
	// consequence the startup warning states.
	fixture.state.AudioConversion = false
	fixture.state.AudioProbe = false

	status = fixture.read(t)

	if status.Live.AudioConversion || status.Live.AudioProbe {
		t.Errorf("Live reports conversion=%v and probe=%v, want both unavailable when the probes found nothing",
			status.Live.AudioConversion, status.Live.AudioProbe)
	}
}

// TestGetRuntimeStatusReportsTheDatabaseSizeOnlyWhenThisServerCanKnowIt. A SQLite deployment knows
// the size of the file it is writing into; the other three do not, and a number that means something
// different depending on the backend is worse than nothing.
func TestGetRuntimeStatusReportsTheDatabaseSizeOnlyWhenThisServerCanKnowIt(t *testing.T) {
	fixture := newStatusFixture()

	fixture.state.DatabaseKnown = false
	fixture.state.DatabaseBytes = 0

	status := fixture.read(t)

	if status.Server.DatabaseKnown {
		t.Errorf("the snapshot says the database size is known, when this server's backend cannot know it")
	}

	if status.Server.DatabaseBytes != 0 {
		t.Errorf("the snapshot reports %d bytes for a database this server cannot measure, want 0", status.Server.DatabaseBytes)
	}
}

// TestGetRuntimeStatusHandsTheViewTwentyFourWholeHourBuckets is the chart's shape. The repository
// reports only the hours that have calls in them; the view is always handed the whole window, so a
// quiet deployment shows the quiet hours instead of a chart whose shape depends on what the database
// happened to contain.
func TestGetRuntimeStatusHandsTheViewTwentyFourWholeHourBuckets(t *testing.T) {
	fixture := newStatusFixture()

	status := fixture.read(t)

	hours := status.Activity.Hours

	if len(hours) != StatusHourBuckets {
		t.Fatalf("the snapshot carries %d hour buckets, want the %d the window is cut into", len(hours), StatusHourBuckets)
	}

	// The window ends at the end of the current whole hour, and not at the instant the read ran: two
	// dashboards opened a second apart see the same buckets.
	wantEnd := statusNow.Truncate(time.Hour).Add(time.Hour)
	wantStart := wantEnd.Add(-StatusWindow)

	if !status.Activity.WindowEnd.Equal(wantEnd) {
		t.Errorf("the activity window ends at %s, want the end of the current hour %s", status.Activity.WindowEnd, wantEnd)
	}

	if !status.Activity.WindowStart.Equal(wantStart) {
		t.Errorf("the activity window starts at %s, want %s", status.Activity.WindowStart, wantStart)
	}

	for i, bucket := range hours {
		want := wantStart.Add(time.Duration(i) * time.Hour)

		if !bucket.Hour.Equal(want) {
			t.Errorf("bucket %d begins at %s, want %s", i, bucket.Hour, want)
		}

		if bucket.Hour.UnixMilli()%3_600_000 != 0 {
			t.Errorf("bucket %d begins at %s, which is not a whole hour", i, bucket.Hour)
		}
	}

	// The hours the repository reported are carried, and the ones it did not are zeros.
	reported := map[int64]uint64{}

	for _, count := range fixture.activity.Hours {
		reported[count.Hour.UnixMilli()] = count.Count
	}

	for _, bucket := range hours {
		if want, ok := reported[bucket.Hour.UnixMilli()]; ok {
			if bucket.Count != want {
				t.Errorf("the bucket at %s carries %d calls, want the %d the repository reported", bucket.Hour, bucket.Count, want)
			}

			continue
		}

		if bucket.Count != 0 {
			t.Errorf("the bucket at %s carries %d calls, want 0: an hour the history said nothing about is a zero and not a gap",
				bucket.Hour, bucket.Count)
		}
	}
}

// TestGetRuntimeStatusCountsTheLocalDayAndTheLastHourOnTheirOwnBounds. Neither is the current calendar
// bucket, and neither is the window's total: the day starts at the server's own local midnight, the last
// hour is a rolling hour that can start mid-hour, and the window's own total is the sum of the buckets
// the view is already handed.
func TestGetRuntimeStatusCountsTheLocalDayAndTheLastHourOnTheirOwnBounds(t *testing.T) {
	fixture := newStatusFixture()

	fixture.activity.SinceCount = 7

	status := fixture.read(t)

	if len(fixture.activity.SinceAsked) != 2 {
		t.Fatalf("the status read asked for %d counts, want 2: the local day and the last hour. The window's "+
			"own total is the sum of the buckets the view is handed, and reading it again is a statement for nothing",
			len(fixture.activity.SinceAsked))
	}

	wantDay := time.Date(statusNow.Year(), statusNow.Month(), statusNow.Day(), 0, 0, 0, 0, statusNow.Location())

	if !fixture.activity.SinceAsked[0].Equal(wantDay) {
		t.Errorf("the day's count was read from %s, want the server's local midnight %s",
			fixture.activity.SinceAsked[0], wantDay)
	}

	if !fixture.activity.SinceAsked[1].Equal(statusNow.Add(-time.Hour)) {
		t.Errorf("the last hour's count was read from %s, want the rolling hour before the clock's instant %s",
			fixture.activity.SinceAsked[1], statusNow.Add(-time.Hour))
	}

	if status.Activity.CallsToday != 7 {
		t.Errorf("CallsToday = %d, want the 7 the day's read answered", status.Activity.CallsToday)
	}

	if status.Activity.CallsLastHour != 7 {
		t.Errorf("CallsLastHour = %d, want the 7 the last hour's read answered", status.Activity.CallsLastHour)
	}

	// The rolling last hour and the current calendar bucket are different questions, and answering one
	// with the other is how a dashboard reports a quiet hour as a busy one.
	if status.Activity.CallsLastHour == status.Activity.Hours[StatusHourBuckets-1].Count && status.Activity.Hours[StatusHourBuckets-1].Count != 7 {
		t.Errorf("the last hour's count is the current bucket's count, which are not the same window")
	}
}

// TestGetRuntimeStatusReportsNoUptimeForAServerWhoseStartIsInTheFuture. A clock that moved backwards,
// or a read taken before the start sequence ran, is not a negative uptime.
func TestGetRuntimeStatusReportsNoUptimeForAServerWhoseStartIsInTheFuture(t *testing.T) {
	fixture := newStatusFixture()

	fixture.state.StartedAt = statusNow.Add(5 * time.Minute)

	status := fixture.read(t)

	if status.Server.Uptime != 0 {
		t.Errorf("the snapshot reports an uptime of %s, want 0: a server that has not started yet has been up for nothing",
			status.Server.Uptime)
	}

	if status.Server.StartedAt.After(status.Server.Now) {
		t.Errorf("the snapshot names %s as the start, which is after the instant it names as now: %s",
			status.Server.StartedAt, status.Server.Now)
	}
}

// TestGetRuntimeStatusWithNothingRecordedAnswersZerosAndNoLastCall. A deployment that has recorded
// nothing is a real state, and it is reported as nothing rather than as a failure or as a missing
// field the view has to guard against.
func TestGetRuntimeStatusWithNothingRecordedAnswersZerosAndNoLastCall(t *testing.T) {
	fixture := newStatusFixture()

	fixture.activity.SinceCount = 0
	fixture.activity.Hours = nil
	fixture.activity.Busiest = nil
	fixture.activity.Recent = nil
	fixture.tallies.Stored = nil

	status := fixture.read(t)

	if status.Activity.CallsToday != 0 || status.Activity.CallsLastHour != 0 {
		t.Errorf("the snapshot reports %d calls today and %d in the last hour, want 0 and 0",
			status.Activity.CallsToday, status.Activity.CallsLastHour)
	}

	if len(status.Activity.Hours) != StatusHourBuckets {
		t.Errorf("the snapshot carries %d hour buckets, want the %d of the window, each a zero",
			len(status.Activity.Hours), StatusHourBuckets)
	}

	for _, bucket := range status.Activity.Hours {
		if bucket.Count != 0 {
			t.Errorf("the bucket at %s carries %d calls, want 0", bucket.Hour, bucket.Count)
		}
	}

	if len(status.Activity.Busiest) != 0 {
		t.Errorf("the snapshot names %d busiest talkgroups, want none", len(status.Activity.Busiest))
	}

	if len(status.Activity.Recent) != 0 {
		t.Errorf("the snapshot carries %d recent calls, want none", len(status.Activity.Recent))
	}

	if !status.Activity.LastCallAt.IsZero() {
		t.Errorf("the snapshot names %s as the last call, want no last call at all", status.Activity.LastCallAt)
	}

	if status.Logs.Errors != 0 || status.Logs.Warnings != 0 {
		t.Errorf("the snapshot reports %d errors and %d warnings, want 0 and 0", status.Logs.Errors, status.Logs.Warnings)
	}

	if len(status.Logs.Sources) != 0 {
		t.Errorf("the snapshot names %d subsystems that complained, want none", len(status.Logs.Sources))
	}
}

// TestGetRuntimeStatusTalliesTheLogLevelsIntoErrorsAndWarningsPerSubsystem is the log-health card:
// the two severities kept apart, and a subsystem named once with both of its numbers.
func TestGetRuntimeStatusTalliesTheLogLevelsIntoErrorsAndWarningsPerSubsystem(t *testing.T) {
	fixture := newStatusFixture()

	status := fixture.read(t)

	if status.Logs.Errors != 3 {
		t.Errorf("the snapshot reports %d errors, want the 3 the tallies carry", status.Logs.Errors)
	}

	if status.Logs.Warnings != 11 {
		t.Errorf("the snapshot reports %d warnings, want the 11 the tallies carry", status.Logs.Warnings)
	}

	if !status.Logs.WindowStart.Equal(statusNow.Add(-StatusWindow)) {
		t.Errorf("the log window starts at %s, want %s", status.Logs.WindowStart, statusNow.Add(-StatusWindow))
	}

	want := map[domain.LogSource][2]uint64{
		domain.LogSourceDirwatch:   {2, 5},
		domain.LogSourceFFmpeg:     {1, 0},
		domain.LogSourceDownstream: {0, 6},
	}

	if len(status.Logs.Sources) != len(want) {
		t.Fatalf("the snapshot names %d subsystems, want the %d that complained: %v",
			len(status.Logs.Sources), len(want), status.Logs.Sources)
	}

	for _, tally := range status.Logs.Sources {
		expected, ok := want[tally.Source]
		if !ok {
			t.Errorf("the snapshot names %s, which wrote nothing to complain about", tally.Source)

			continue
		}

		if tally.Errors != expected[0] || tally.Warnings != expected[1] {
			t.Errorf("%s reports %d errors and %d warnings, want %d and %d",
				tally.Source, tally.Errors, tally.Warnings, expected[0], expected[1])
		}
	}

	// Where the trouble is, first: the subsystem with the most to complain about is the one the
	// operator is looking for.
	for i := 1; i < len(status.Logs.Sources); i++ {
		previous := status.Logs.Sources[i-1]
		current := status.Logs.Sources[i]

		if current.Errors+current.Warnings > previous.Errors+previous.Warnings {
			t.Errorf("%s carries %d entries ahead of %s, which carries %d: the list is not ordered by most trouble first",
				current.Source, current.Errors+current.Warnings, previous.Source, previous.Errors+previous.Warnings)
		}
	}
}

// TestGetRuntimeStatusReportsNoSnapshotWhenAReadFails. A half-read snapshot presented as current is
// worse than no snapshot: the view has to be able to say it could not read the status, and the only
// way it can is for the read to answer nothing.
func TestGetRuntimeStatusReportsNoSnapshotWhenAReadFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failed func(fixture *statusFixture, err error)
	}{
		{"the call activity", func(fixture *statusFixture, err error) { fixture.activity.Err = err }},
		{"the log tallies", func(fixture *statusFixture, err error) { fixture.tallies.Err = err }},
	} {
		t.Run(tc.name+" cannot be read", func(t *testing.T) {
			fixture := newStatusFixture()

			tc.failed(fixture, errors.New("the read failed"))

			status, err := fixture.handler.Handle(context.Background(), GetRuntimeStatusQuery{})
			if err == nil {
				t.Fatalf("the status read answered a snapshot, want the failure: %+v", status)
			}

			if !status.Activity.WindowEnd.IsZero() || len(status.Activity.Hours) != 0 {
				t.Errorf("the failed read still carried an activity block: %v", status.Activity.Hours)
			}

			if !status.Logs.WindowStart.IsZero() || len(status.Logs.Sources) != 0 {
				t.Errorf("the failed read still carried a log block: %v", status.Logs.Sources)
			}
		})
	}
}

// TestGetRuntimeStatusReadsTheNewestCallsAsSummariesAndNotAsCalls is the property that makes a read
// asked every few seconds safe. The handler is given the activity port and nothing else: the audio a
// call carries lives behind CallRepository, which this use case is never handed, so the newest rows
// arrive as the same summaries the call history view is handed.
func TestGetRuntimeStatusReadsTheNewestCallsAsSummariesAndNotAsCalls(t *testing.T) {
	fixture := newStatusFixture()

	status := fixture.read(t)

	if len(status.Activity.Recent) != 1 {
		t.Fatalf("the snapshot carries %d recent calls, want the 1 the read answered", len(status.Activity.Recent))
	}

	recent := status.Activity.Recent[0]

	if recent.ID != 91827 || recent.SystemRef != 1 || recent.TalkgroupRef != 101 {
		t.Errorf("the recent row names call %d on system %d talkgroup %d, want call 91827 on system 1 talkgroup 101",
			recent.ID, recent.SystemRef, recent.TalkgroupRef)
	}

	if recent.AudioLength != 12400*time.Millisecond {
		t.Errorf("the recent row carries a recording of %s, want 12.4s", recent.AudioLength)
	}

	if !status.Activity.LastCallAt.Equal(recent.Timestamp) {
		t.Errorf("the snapshot names %s as the last call, want the newest row's instant %s",
			status.Activity.LastCallAt, recent.Timestamp)
	}
}

// TestGetRuntimeStatusCutsTheBusiestTalkgroupsAtTheView'sOwnLimit keeps the list the size the card
// has room for, so the read never answers more than the page can show.
func TestGetRuntimeStatusCutsTheBusiestTalkgroupsAtTheViewsOwnLimit(t *testing.T) {
	fixture := newStatusFixture()

	fixture.read(t)

	if len(fixture.activity.BusiestAsked) != 1 {
		t.Fatalf("the status read asked for the busiest talkgroups %d times, want once", len(fixture.activity.BusiestAsked))
	}

	if fixture.activity.BusiestAsked[0] != StatusBusiestLimit {
		t.Errorf("the read asked for %d talkgroups, want the view's own limit of %d",
			fixture.activity.BusiestAsked[0], StatusBusiestLimit)
	}

	if fixture.activity.RecentAsked[0] != StatusRecentLimit {
		t.Errorf("the read asked for %d recent calls, want the view's own limit of %d",
			fixture.activity.RecentAsked[0], StatusRecentLimit)
	}
}
