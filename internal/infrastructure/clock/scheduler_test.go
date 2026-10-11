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
	"fmt"
	"testing"
	"time"
)

// The scheduler is the server's one way of saying "do this later", and the application layer's rules
// about time — the listener-count report held back so a burst of connections costs one report, the
// pruning that runs once an hour — are only testable if this thing actually does it later.
func TestSchedulerRunsTheWorkAfterTheDelay(t *testing.T) {
	ran := make(chan struct{}, 1)

	NewScheduler(nil).Schedule(20*time.Millisecond, func() { ran <- struct{}{} })

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatalf("the scheduled work never ran")
	}
}

// Work that has not run yet can be abandoned, and abandoning it means it never runs. The listener-count
// debouncer holds the handle of the report it has already scheduled, and a report that is abandoned has
// to stay abandoned.
func TestSchedulerCancelsWorkThatHasNotRunYet(t *testing.T) {
	ran := make(chan struct{}, 1)

	timer := NewScheduler(nil).Schedule(50*time.Millisecond, func() { ran <- struct{}{} })

	timer.Cancel()

	select {
	case <-ran:
		t.Fatalf("the scheduled work ran after it was cancelled")
	case <-time.After(200 * time.Millisecond):
	}
}

// Abandoning work that already ran is not an error and does not stop something else from running: the
// handle outlives the work it stands for, and the holder of it does not know which half of its life the
// handle is in.
func TestSchedulerIgnoresACancelOfWorkThatAlreadyRan(t *testing.T) {
	ran := make(chan struct{}, 1)

	timer := NewScheduler(nil).Schedule(time.Millisecond, func() { ran <- struct{}{} })

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatalf("the scheduled work never ran")
	}

	timer.Cancel()

	other := make(chan struct{}, 1)

	NewScheduler(nil).Schedule(time.Millisecond, func() { other <- struct{}{} })

	select {
	case <-other:
	case <-time.After(2 * time.Second):
		t.Fatalf("a cancel of work that had already run stopped later work from running")
	}
}

// TestScheduledWorkThatPanicsIsRecoveredAndReported (REQ-020, CA-020.1): scheduled
// work runs on the timer's own goroutine, and today a panic there is a panic in a
// goroutine nobody owns — the whole server goes down with no report in its own log.
// The scheduler recovers the panic and tells the report it was built with.
func TestScheduledWorkThatPanicsIsRecoveredAndReported(t *testing.T) {
	reported := make(chan any, 1)

	scheduler := NewScheduler(func(problem any) { reported <- problem })

	scheduler.Schedule(10*time.Millisecond, func() {
		panic("the scheduled report fell over")
	})

	select {
	case problem := <-reported:
		if got, want := fmt.Sprint(problem), "the scheduled report fell over"; got != want {
			t.Errorf("the report was told %v, want the panic's own value", problem)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the scheduled panic was never reported: the server took the panic silently, or not at all")
	}
}
