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
	"sort"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The status view: what this server is, what it is running right now, what it recorded lately, and
// what it lately had to complain about. It is a projection for the administrator's landing page and not
// a business thing: nothing here is a rule the domain enforces, and nothing here is written down.
//
// The window every tally is read over is this use case's own constant rather than a criterion the
// caller submits. The page shows one window, and a caller that could widen it would be a caller asking
// this server to count more than the page has room for.

const (
	// StatusWindow is how far back the activity and log tallies are read over.
	StatusWindow = 24 * time.Hour

	// StatusHourBuckets is the number of whole hours StatusWindow is cut into. The chart always shows
	// all of them, whatever the history happens to contain.
	StatusHourBuckets = 24

	// StatusBusiestLimit is how many talkgroups the busiest list names.
	StatusBusiestLimit uint = 5

	// StatusRecentLimit is how many of the newest calls the view carries.
	StatusRecentLimit uint = 5
)

// GetRuntimeStatusQuery asks what this server is doing right now. It takes nothing: the operator asking
// is already known, and the window every tally is read over is this use case's own constant.
type GetRuntimeStatusQuery struct{}

// RuntimeStatus is the whole status view.
type RuntimeStatus struct {
	Server   RuntimeServerStatus
	Live     RuntimeLiveStatus
	Activity RuntimeActivityStatus
	Logs     RuntimeLogStatus
}

// RuntimeServerStatus carries only what this server can measure about itself. The identity strings the
// published payload also names — the version, the container answer, the folder it serves — are already
// held by the administrator surface and are added there: this use case cannot measure them and should
// not be handed strings it can only repeat.
type RuntimeServerStatus struct {
	StartedAt time.Time
	Now       time.Time
	Uptime    time.Duration

	DatabaseBytes int64
	DatabaseKnown bool
}

// RuntimeLiveStatus is what exists at the instant the read ran, as opposed to what the configuration
// says should exist.
type RuntimeLiveStatus struct {
	Listeners             int
	AdministratorSessions int
	DirwatchersRunning    int
	HeldCalls             int

	AudioConversion bool
	AudioProbe      bool
}

// RuntimeActivityStatus is what this server recorded over the window.
type RuntimeActivityStatus struct {
	WindowStart time.Time
	WindowEnd   time.Time

	CallsToday    uint64
	CallsLastHour uint64

	// LastCallAt is the instant of the newest recorded call, and the zero instant when the history has
	// no calls in it at all. It is not bounded by the window: a deployment whose last call was last week
	// still has a last call, and the operator is shown when it was.
	LastCallAt time.Time

	Hours   []ports.CallHourCount
	Busiest []ports.CallTalkgroupCount
	Recent  []ports.CallSummary
}

// RuntimeLogStatus is what this server complained about over the window.
type RuntimeLogStatus struct {
	WindowStart time.Time

	Errors   uint64
	Warnings uint64

	Sources []RuntimeLogSourceTally
}

// RuntimeLogSourceTally is the one shape this read model declares itself: the two severities one
// subsystem wrote, carried together for the one card that shows them. The repository reports them as
// two separate counts, and pairing them is this handler's work.
type RuntimeLogSourceTally struct {
	Source   domain.LogSource
	Errors   uint64
	Warnings uint64
}

// GetRuntimeStatusHandler reads the status view.
type GetRuntimeStatusHandler struct {
	// state answers what the server is running, which nothing outside the composition root can answer
	// in one read.
	state ports.RuntimeState

	// activity and tallies are the two aggregates the window is read over. Neither is the repository
	// the history pages through: the status view counts, and never pages.
	activity ports.CallActivityRepository
	tallies  ports.LogTallyRepository

	clock ports.Clock
}

// NewGetRuntimeStatusHandler builds the status read over the live state, the two tallies, and the
// server's own clock.
func NewGetRuntimeStatusHandler(
	state ports.RuntimeState,
	activity ports.CallActivityRepository,
	tallies ports.LogTallyRepository,
	clock ports.Clock,
) *GetRuntimeStatusHandler {
	return &GetRuntimeStatusHandler{
		state:    state,
		activity: activity,
		tallies:  tallies,
		clock:    clock,
	}
}

// Handle reads the whole snapshot in one pass.
//
// A snapshot that could not be read is answered as nothing and with the failure. A half-read snapshot
// presented as current is how a server whose database is unreachable looks like a server that is healthy
// and idle, which is the one confusion this view must never cause.
func (handler *GetRuntimeStatusHandler) Handle(ctx context.Context, _ GetRuntimeStatusQuery) (RuntimeStatus, error) {
	now := handler.clock.Now()

	status := RuntimeStatus{
		Server: handler.serverStatus(ctx, now),
		Live:   handler.liveStatus(ctx),
	}

	// The activity window is cut on whole hours and not on the instant the read ran: two dashboards
	// opened a second apart see the same buckets, and a bucket that is still filling is not shown as a
	// partial hour.
	windowEnd := now.Truncate(time.Hour).Add(time.Hour)
	windowStart := windowEnd.Add(-StatusWindow)

	activity, err := handler.activityStatus(ctx, now, windowStart, windowEnd)
	if err != nil {
		return RuntimeStatus{}, err
	}

	status.Activity = activity

	logs, err := handler.logStatus(ctx, now)
	if err != nil {
		return RuntimeStatus{}, err
	}

	status.Logs = logs

	return status, nil
}

// serverStatus reports what this server can measure about itself.
func (handler *GetRuntimeStatusHandler) serverStatus(ctx context.Context, now time.Time) RuntimeServerStatus {
	startedAt := handler.state.ServerStarted()

	// A start that has not run yet — the zero instant, or one the clock says is still to come — is a
	// server that has been up for nothing. An uptime counted backwards from it is a number no operator
	// can use.
	if startedAt.IsZero() || startedAt.After(now) {
		startedAt = time.Time{}
	}

	bytes, known := handler.state.DatabaseSize()

	if !known {
		bytes = 0
	}

	return RuntimeServerStatus{
		StartedAt: startedAt,
		Now:       now,
		Uptime:    uptimeBetween(startedAt, now),

		DatabaseBytes: bytes,
		DatabaseKnown: known,
	}
}

// liveStatus reports what exists at this instant. None of it is read out of the log view, and none of it
// is the configuration's own claim about itself.
func (handler *GetRuntimeStatusHandler) liveStatus(ctx context.Context) RuntimeLiveStatus {
	return RuntimeLiveStatus{
		Listeners:             handler.state.ListenerCount(),
		AdministratorSessions: handler.state.AdministratorSessionCount(),
		DirwatchersRunning:    handler.state.DirwatchersRunning(),
		HeldCalls:             handler.state.HeldCallCount(),

		AudioConversion: handler.state.AudioConversionAvailable(),
		AudioProbe:      handler.state.AudioProbeAvailable(),
	}
}

// activityStatus reads the window's tallies and the newest rows the view shows.
func (handler *GetRuntimeStatusHandler) activityStatus(ctx context.Context, now, windowStart, windowEnd time.Time) (RuntimeActivityStatus, error) {
	// The two headline counts are two different questions, and answering one with the other is how a
	// dashboard reports a quiet hour as a busy day: the day starts at the server's own local midnight,
	// and the last hour is a rolling hour that can start mid-hour. The window's own total is not read at
	// all: the 24 buckets the view is handed already add up to it, and add up to it more exactly, because
	// they stop at the window's end.
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	callsToday, err := handler.activity.CountCallsSince(ctx, dayStart)
	if err != nil {
		return RuntimeActivityStatus{}, err
	}

	callsLastHour, err := handler.activity.CountCallsSince(ctx, now.Add(-time.Hour))
	if err != nil {
		return RuntimeActivityStatus{}, err
	}

	hours, err := handler.hoursOver(ctx, windowStart, windowEnd)
	if err != nil {
		return RuntimeActivityStatus{}, err
	}

	busiest, err := handler.activity.CountCallsByTalkgroup(ctx, windowStart, StatusBusiestLimit)
	if err != nil {
		return RuntimeActivityStatus{}, err
	}

	recent, err := handler.activity.RecentCalls(ctx, StatusRecentLimit)
	if err != nil {
		return RuntimeActivityStatus{}, err
	}

	activity := RuntimeActivityStatus{
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
		CallsToday:    callsToday,
		CallsLastHour: callsLastHour,
		Hours:         hours,
		Busiest:       busiest,
		Recent:        recent,
	}

	// The newest row the history holds is the last call, whatever window it falls in.
	if len(recent) > 0 {
		activity.LastCallAt = recent[0].Timestamp
	}

	return activity, nil
}

// hoursOver answers the whole window as whole hours, filling the hours the history said nothing about
// with a zero.
//
// The repository reports only the hours that have something in them. Filling the rest here is what keeps
// the chart's shape the same for every deployment: a quiet server shows the quiet hours, and not a chart
// whose bars depend on what the database happened to contain.
func (handler *GetRuntimeStatusHandler) hoursOver(ctx context.Context, windowStart, windowEnd time.Time) ([]ports.CallHourCount, error) {
	reported, err := handler.activity.CountCallsByHour(ctx, windowStart)
	if err != nil {
		return nil, err
	}

	counted := map[int64]uint64{}

	for _, hour := range reported {
		counted[hour.Hour.UnixMilli()] = hour.Count
	}

	buckets := make([]ports.CallHourCount, 0, StatusHourBuckets)

	for i := range StatusHourBuckets {
		hour := windowStart.Add(time.Duration(i) * time.Hour)

		buckets = append(buckets, ports.CallHourCount{Hour: hour, Count: counted[hour.UnixMilli()]})
	}

	return buckets, nil
}

// logStatus tallies what the server complained about over the last 24 hours.
//
// Its window is anchored on the instant the read ran and not on the activity window's whole hours: the
// log card answers "has this server been in trouble lately", and lately is measured from now.
func (handler *GetRuntimeStatusHandler) logStatus(ctx context.Context, now time.Time) (RuntimeLogStatus, error) {
	windowStart := now.Add(-StatusWindow)

	reported, err := handler.tallies.TallyLogLevels(ctx, windowStart)
	if err != nil {
		return RuntimeLogStatus{}, err
	}

	counted := map[domain.LogSource]RuntimeLogSourceTally{}

	status := RuntimeLogStatus{WindowStart: windowStart}

	for _, tally := range reported {
		source := counted[tally.Source]
		source.Source = tally.Source

		switch tally.Level {
		case domain.LogLevelError:
			source.Errors += tally.Count
			status.Errors += tally.Count

		case domain.LogLevelWarn:
			source.Warnings += tally.Count
			status.Warnings += tally.Count
		}

		counted[tally.Source] = source
	}

	for _, tally := range counted {
		// A subsystem that wrote neither severity is not named. Listing every part of the server with a
		// zero each would bury the one that did write something.
		if tally.Errors == 0 && tally.Warnings == 0 {
			continue
		}

		status.Sources = append(status.Sources, tally)
	}

	// Where the trouble is, first. Ties are broken by the subsystem's own name so that two polls of the
	// same window answer the same list.
	sort.Slice(status.Sources, func(i, j int) bool {
		left, right := status.Sources[i], status.Sources[j]

		if left.Errors+left.Warnings != right.Errors+right.Warnings {
			return left.Errors+left.Warnings > right.Errors+right.Warnings
		}

		return left.Source < right.Source
	})

	return status, nil
}

// uptimeBetween is how long the server has been up, never negative.
func uptimeBetween(startedAt, now time.Time) time.Duration {
	if startedAt.IsZero() || startedAt.After(now) {
		return 0
	}

	return now.Sub(startedAt)
}
