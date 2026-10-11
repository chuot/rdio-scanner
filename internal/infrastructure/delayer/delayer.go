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

package delayer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// waitingBuffer is how many calls whose hold has run out the delayer can be holding when nobody is
// collecting them. It is small on purpose: a held call carries its recording, and the point of the
// buffer is to keep a timer from waiting on a collector, not to store calls here.
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

// Delayer holds back calls that have a hold to wait out and hands them back when the hold is over.
//
// It is the server's one holder of delayed calls, and it is the reason the ingest path does not have
// to remember anything about a call it has already stored: it asks whether the call is still being
// held back, and if it is, the call is no longer the ingest path's business.
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

	// timers are the holds that have been armed and have not run yet. They are guarded by mutex
	// because a call arriving in a watched directory and a hold running out are not the same
	// goroutine, and Close ends holds that were armed for a server that is no longer starting.
	mutex  sync.Mutex
	timers map[uint64]ports.Timer
	closed bool

	// sending counts the delivers in flight, so Close knows the last one has landed and can
	// close the stream its collector reads — telling that collector, by the stream ending,
	// that the server is going away (REQ-007, CA-007.6).
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

// Hold reports whether a call is still being held back at this moment, and takes it when it is.
//
// The answer is the domain's rule, not a table lookup: a call is being held back when it names a
// delay and the moment it happened plus that delay has not arrived yet. A call that arrived late
// enough for its hold to have run out on its own is not held, and is the caller's to deliver.
func (delayer *Delayer) Hold(ctx context.Context, call domain.Call) (bool, error) {
	return delayer.take(ctx, call)
}

// Waiting is the stream of calls whose hold has run out. Each one arrives carrying the mark that
// says it was held back, which is what tells a listener's call history that it did.
//
// Nothing is delivered by the delayer itself. The composition root hands this stream to the same
// delivery step an ingested call ends with, so that a call that waited is told to the other servers
// and to the listeners by the one path every other call takes.
func (delayer *Delayer) Waiting() <-chan domain.Call {
	return delayer.waiting
}

// Restore picks up the calls this server was holding when it last stopped.
//
// Each one is read back from what was written down. A call whose hold has run out while the server
// was not running is handed over at once; a call still holding is armed again for the instant its
// delay names *now*, which is what lets an administrator change a talkgroup's delay and have the
// calls already waiting follow the new one.
//
// The calls that were written down are cleared first, and the ones still holding are written down
// again, so a server that stops in the middle of a hold leaves exactly the holds it was running.
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
			// A call that cannot be read back is a call that was pruned, or that was never really
			// stored. The legacy server drops it without saying anything, and so does this one: the
			// alternative is a startup that stops because of one row nobody can deliver.
			continue
		}

		call.Delayed = true

		if row.Timestamp.Before(now) {
			delayer.deliver(call)

			continue
		}

		taken, err := delayer.take(ctx, call)
		if err != nil {
			// A hold that cannot be written down again is reported and left, which is what the legacy
			// does with it: one call that will not wait is not a reason for the server to stop starting.
			// The line says what the delayer was trying to do, and the failure's own text names the step
			// that hit it.
			delayer.logf(ctx, domain.LogLevelError, "a held call could not be put back on hold: %s", err.Error())

			continue
		}

		if !taken {
			delayer.deliver(call)
		}
	}

	return nil
}

// Close abandons the holds this delayer armed, stops it taking any more, and ends the
// stream of calls whose hold has run out.
//
// What was written down stays written down. A server that stops is not a server that forgot the
// calls it was holding, and the next startup reads them back.
//
// The order is what makes closing the stream safe: the wake-up goes out first, the delivers in
// flight land and are counted out, and only then, with no sender left, is the stream closed —
// the collector's own exit signal (REQ-007, CA-007.6).
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

// take is the one place the decision is made, for a call that has just been recorded and for a call
// that was written down by a previous startup alike: a call is held when it names a delay and the
// moment it happened plus that delay has not arrived yet.
//
// A call that is held is written down with the instant it stops being held back, so that a server
// that stops in the middle of the hold finds it again, and is armed for that instant.
//
// A hold that cannot be written down is reported and the call is left undelivered, which is what the
// legacy does with it: the call is in the recording history, but nothing is waiting to tell anybody
// about it, and pretending otherwise would deliver a call the server has promised to deliver later.
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
		// The failure is named for the part of the delayer that hit it, the way the legacy server names
		// it and the way every repository failure here is named, so that whoever reports it has only to
		// say what it was given.
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
		// A hold that will not be cleared is reported and the call is delivered anyway. The call was
		// recorded and its hold is over; a row that is left behind is not a reason to keep a finished
		// recording from the operators who were waiting for it.
		delayer.logf(ctx, domain.LogLevelError, "the held calls could not be released: %s", err.Error())
	}

	call.Delayed = true

	delayer.deliver(call)
}

// deliver hands a call to whoever is collecting the ones whose hold has run out.
//
// It waits for the collector rather than dropping a recording nobody has been told about, but it
// stops waiting as soon as the delayer is closed: a server that is going away is not going to
// collect anything, and a timer goroutine held open by it would outlive the server. A closed
// delayer takes nothing at all, and a delivery in flight is counted, so Close knows when the
// stream has no sender left and can close it.
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

// logf writes one line to the server log. These lines are the server's log and not its console: a
// hold that went wrong is something an administrator reads afterwards.
//
// Every one of them is filed under the delayer, because the delayer is the part of the server they are
// about: the log view filters on it, and a line that does not name its author cannot be found by it.
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
