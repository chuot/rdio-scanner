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

package delayer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// waitingBuffer is how many finished calls the delayer can hold when nobody is
// collecting them; it stays small; see the call-delayer maintainer note.
const waitingBuffer = 8

// Deps are the parts a delayer is built from.
type Deps struct {
	// Delayed is where a held call is written down, and where it is read back when the server starts
	// again.
	Delayed ports.DelayedCallRepository

	// Calls reads a held call back when its hold runs out at a moment this server was not running
	// for.
	Calls ports.CallRepository

	// Scheduler arms the instant a held call stops being held back.
	Scheduler ports.Scheduler

	// Clock is what a hold is measured against.
	Clock ports.Clock

	// Logs is where a hold that could not be written down, or a written hold that could not be
	// cleared, is reported.
	Logs ports.LogRepository
}

// Delayer holds back calls with a hold to wait out and hands them back when
// the hold is over; see the call-delayer maintainer note.
type Delayer struct {
	delayed   ports.DelayedCallRepository
	calls     ports.CallRepository
	scheduler ports.Scheduler
	clock     ports.Clock
	logs      ports.LogRepository

	// waiting carries the calls whose hold has run out to whoever is collecting them, and done says
	// that nobody is going to collect any more.
	waiting chan domain.Call
	done    chan struct{}

	// timers are the armed holds that have not run yet, guarded by mutex because
	// a call arriving and a hold running out are different goroutines; see the
	// call-delayer maintainer note.
	mutex  sync.Mutex
	timers map[uint64]ports.Timer
	closed bool

	// sending counts the delivers in flight, so Close closes the stream only
	// once the last one has landed; see the call-delayer maintainer note.
	sending sync.WaitGroup
}

var _ ports.CallDelayer = (*Delayer)(nil)

// New builds a delayer from its parts.
func New(deps Deps) *Delayer {
	return &Delayer{
		delayed:   deps.Delayed,
		calls:     deps.Calls,
		scheduler: deps.Scheduler,
		clock:     deps.Clock,
		logs:      deps.Logs,

		waiting: make(chan domain.Call, waitingBuffer),
		done:    make(chan struct{}),
		timers:  map[uint64]ports.Timer{},
	}
}

// Hold reports whether a call is still being held back at this moment, and
// takes it when it is; the answer is the domain's rule, and a call whose
// hold ran out on its own is the caller's to deliver; see the call-delayer maintainer note.
func (delayer *Delayer) Hold(ctx context.Context, call domain.Call) (bool, error) {
	return delayer.take(ctx, call)
}

// Waiting is the stream of calls whose hold has run out, each carrying the
// mark that says it was held back; the delayer delivers nothing itself, the
// composition root feeds this stream into the server's one delivery step;
// see the call-delayer maintainer note.
func (delayer *Delayer) Waiting() <-chan domain.Call {
	return delayer.waiting
}

// Restore picks up the calls this server was holding when it last stopped:
// a call whose hold ran out while the server was not running is handed over
// at once, a call still holding is armed again for the instant its delay
// names now; what was written down is cleared and the live holds are written
// down again; see the call-delayer maintainer note.
func (delayer *Delayer) Restore(ctx context.Context) error {
	stored, err := delayer.delayed.AllDelayed(ctx)
	if err != nil {
		return err
	}

	if err := delayer.delayed.ClearDelayed(ctx); err != nil {
		return err
	}

	now := delayer.clock.Now()

	for _, row := range stored {
		call, err := delayer.calls.GetCall(ctx, row.CallID)
		if err != nil {
			// A call that cannot be read back is a call that was pruned, or that was
			// never really stored, and it is dropped without saying anything, as the
			// legacy server drops it; see the call-delayer maintainer note.
			continue
		}

		call.Delayed = true

		if row.Timestamp.Before(now) {
			delayer.deliver(call)

			continue
		}

		taken, err := delayer.take(ctx, call)
		if err != nil {
			// A hold that cannot be written down again is reported and left, as the
			// legacy leaves it, and the failure's own text names the step that hit it;
			// see the call-delayer maintainer note.
			delayer.logf(ctx, domain.LogLevelError, "a held call could not be put back on hold: %s", err.Error())

			continue
		}

		if !taken {
			delayer.deliver(call)
		}
	}

	return nil
}

// Close abandons the holds this delayer armed, stops it taking any more,
// and ends the stream of calls whose hold has run out. What was written
// down stays written down for the next startup; the order of these steps
// is what makes closing the stream safe; see the call-delayer maintainer note.
func (delayer *Delayer) Close() {
	delayer.mutex.Lock()

	if delayer.closed {
		delayer.mutex.Unlock()

		return
	}

	delayer.closed = true

	timers := delayer.timers

	delayer.timers = map[uint64]ports.Timer{}

	delayer.mutex.Unlock()

	close(delayer.done)

	delayer.sending.Wait()

	close(delayer.waiting)

	for _, timer := range timers {
		timer.Cancel()
	}
}

// Holding reports how many calls are waiting out a hold right now: the
// armed holds that have not run yet, not the rows the delayed table holds;
// see the call-delayer maintainer note.
func (delayer *Delayer) Holding() int {
	delayer.mutex.Lock()
	defer delayer.mutex.Unlock()

	return len(delayer.timers)
}

// take is the one place the decision is made, for a call that has just been
// recorded and for one written down by a previous startup alike: a call is
// held when it names a delay and the moment it happened plus that delay has
// not arrived yet. A held call is written down with the instant it stops
// being held back and armed for that instant; a hold that cannot be written
// down is reported and the call is left undelivered; see the call-delayer maintainer note.
func (delayer *Delayer) take(ctx context.Context, call domain.Call) (bool, error) {
	if !call.IsWaiting(delayer.clock.Now()) {
		return false, nil
	}

	until := call.VisibleAt()

	delayer.mutex.Lock()
	defer delayer.mutex.Unlock()

	if delayer.closed {
		return false, nil
	}

	if err := delayer.delayed.ScheduleDelayed(ctx, call.ID, until); err != nil {
		// The failure is named for the part of the delayer that hit it, as the
		// legacy server names it and every repository failure here is named;
		// see the call-delayer maintainer note.
		return false, fmt.Errorf("delayer.delay: %w", err)
	}

	// The hold outlives whatever found the call — a watch cycle, an upload, a re-post from another
	// server — so the work it does when it runs is detached from that cycle's cancellation.
	ctx = context.WithoutCancel(ctx)

	delayer.timers[call.ID] = delayer.scheduler.Schedule(time.Until(until), func() {
		delayer.finished(ctx, call)
	})

	return true, nil
}

// finished is what happens when a hold runs out: the call stops being written down as waiting, and
// arrives on the stream the server delivers held calls from.
func (delayer *Delayer) finished(ctx context.Context, call domain.Call) {
	delayer.mutex.Lock()
	delete(delayer.timers, call.ID)
	delayer.mutex.Unlock()

	if err := delayer.delayed.UnscheduleDelayed(ctx, call.ID); err != nil {
		// A hold that will not be cleared is reported and the call is delivered
		// anyway: the call was recorded and its hold is over; see the call-delayer maintainer note.
		delayer.logf(ctx, domain.LogLevelError, "the held calls could not be released: %s", err.Error())
	}

	call.Delayed = true

	delayer.deliver(call)
}

// deliver hands a call to whoever is collecting the ones whose hold has
// run out. It waits for the collector rather than dropping a recording,
// but stops waiting once the delayer is closed, and a delivery in flight
// is counted so Close knows when the stream has no sender left; see the
// call-delayer maintainer note.
func (delayer *Delayer) deliver(call domain.Call) {
	delayer.mutex.Lock()

	if delayer.closed {
		delayer.mutex.Unlock()

		return
	}

	delayer.sending.Add(1)

	delayer.mutex.Unlock()

	select {
	case delayer.waiting <- call:
	case <-delayer.done:
	}

	delayer.sending.Done()
}

// logf writes one line to the server log, filed under the delayer because
// the log view filters on it; these lines are the server's log and not its
// console; see the call-delayer maintainer note for how short they stay.
func (delayer *Delayer) logf(ctx context.Context, level, format string, args ...any) {
	if delayer.logs == nil {
		return
	}

	delayer.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: delayer.clock.Now(),
		Level:    level,
		Source:   domain.LogSourceDelayer,
		Message:  fmt.Sprintf(format, args...),
	})
}
