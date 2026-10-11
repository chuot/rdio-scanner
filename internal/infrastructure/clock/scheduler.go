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

package clock

import (
	"time"

	"rdio-scanner/internal/application/ports"
)

// Scheduler runs work after a delay, which is the other half of what this package is for: the clock
// says when it is, and the scheduler says when something will be.
//
// It is the adapter behind the application's Scheduler port. The application owns the rules about time —
// a listener-count report held back so that a burst of connections costs one report rather than one per
// connection, a pruning that runs once an hour — and a rule about time that can only be tested by
// waiting for real time is not a tested rule, so the rules are written against this port and tested
// against a fake of it.
type Scheduler struct {
	// report is told the panic a piece of scheduled work recovered, so the
	// server says the failure in its own log instead of dying by it.
	report func(problem any)
}

var _ ports.Scheduler = (*Scheduler)(nil)

// NewScheduler returns a scheduler that runs work on the clock's own timers.
//
// report is told the value a panicking piece of scheduled work recovered with; a nil report discards it, which is what the tests that do not care about
// the report pass.
func NewScheduler(report func(problem any)) *Scheduler {
	if report == nil {
		report = func(problem any) {}
	}

	return &Scheduler{report: report}
}

// Schedule asks for work to run after the delay and hands back the handle that can abandon it.
//
// The work runs on a timer of its own, on a goroutine the scheduler owns rather than on the caller's:
// a report that is scheduled from the connection that triggered it should not be held to that
// connection's lifetime, which is what the application's rules about a listener that joined and left
// again assume.
//
// The work is run under a recover: a panic on a timer's goroutine is a panic in a
// goroutine nobody owns, and a server that loses a debounced listener report, an hourly pruning, or
// a delayed call to a panic dies by it with no report in its own log. The recovered value is told
// to the report the scheduler was built with, and the schedule goes on.
func (scheduler *Scheduler) Schedule(delay time.Duration, work func()) ports.Timer {
	return scheduled{timer: time.AfterFunc(delay, func() {
		defer func() {
			if problem := recover(); problem != nil {
				scheduler.report(problem)
			}
		}()

		work()
	})}
}

// scheduled is one piece of work that has not happened yet.
type scheduled struct {
	timer *time.Timer
}

var _ ports.Timer = scheduled{}

// Cancel abandons the scheduled work. Work that has already run is left alone: the handle outlives the
// work it stands for, and whoever holds it does not know which half of its life the handle is in.
func (s scheduled) Cancel() {
	s.timer.Stop()
}
