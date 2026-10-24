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

package clock

import (
	"fmt"
	"testing"
	"time"
)

// The scheduler is the server's one way of saying "do this later": the application's held-back
// listener-count report and hourly pruning are only testable if it really runs work later.
func TestSchedulerRunsTheWorkAfterTheDelay(t *testing.T) {
	ran := make(chan struct{}, 1)

	NewScheduler(nil).Schedule(20*time.Millisecond, func() { ran <- struct{}{} })

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatalf("the scheduled work never ran")
	}
}

// Work that has not run yet can be abandoned, and abandoning it means it never runs: the
// listener-count debouncer holds the handle of the report it has already scheduled, and an
// abandoned report has to stay abandoned.
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

// Abandoning work that already ran is not an error and does not stop another run: the handle
// outlives the work it stands for, and its holder does not know which half of that life the
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

// TestScheduledWorkThatPanicsIsRecoveredAndReported: a panic on the timer's own
// goroutine is recovered and told to the report the scheduler was built with.
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
