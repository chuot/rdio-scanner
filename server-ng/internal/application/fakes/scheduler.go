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
	"sort"
	"time"

	"rdio-scanner/server-ng/internal/application/ports"
)

// Scheduler is an in-memory scheduler. Nothing runs on its own: a test schedules work and
// then decides when it happens, which is what makes a debouncing rule testable without
// waiting for the delay to pass.
type Scheduler struct {
	// Pending is the work that has been scheduled and not yet run, in the order it was
	// scheduled.
	Pending []*scheduledWork

	// Ran records the delays whose work has run, oldest delay first.
	Ran []time.Duration
}

var _ ports.Scheduler = (*Scheduler)(nil)

// scheduledWork is one scheduled piece of work.
type scheduledWork struct {
	Delay     time.Duration
	Work      func()
	Cancelled bool
}

// NewScheduler returns a scheduler with nothing pending.
func NewScheduler() *Scheduler {
	return &Scheduler{}
}

// Schedule records work and returns the handle that can abandon it.
func (scheduler *Scheduler) Schedule(delay time.Duration, work func()) ports.Timer {
	item := &scheduledWork{Delay: delay, Work: work}

	scheduler.Pending = append(scheduler.Pending, item)

	return item
}

// PendingCount reports how much scheduled work has not run yet.
func (scheduler *Scheduler) PendingCount() int {
	count := 0

	for _, item := range scheduler.Pending {
		if !item.Cancelled {
			count++
		}
	}

	return count
}

// RunDue runs every piece of work whose delay is at most the given one, shortest delay
// first, which is the order a real clock would run them in.
func (scheduler *Scheduler) RunDue(within time.Duration) {
	due := append([]*scheduledWork{}, scheduler.Pending...)

	sort.SliceStable(due, func(i, j int) bool { return due[i].Delay <= due[j].Delay })

	for _, item := range due {
		if item.Cancelled || item.Delay > within {
			continue
		}

		item.Cancelled = true

		scheduler.Ran = append(scheduler.Ran, item.Delay)

		if item.Work != nil {
			item.Work()
		}
	}
}

// Cancel abandons this piece of work.
func (item *scheduledWork) Cancel() {
	item.Cancelled = true
}
