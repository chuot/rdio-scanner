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
	"sync"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// ListenerCountReportDelay is how long the server holds back its listener-count report.
//
// A listener connecting and immediately reconnecting, or a native app cycling its
// sessions, would otherwise produce a report per event. The legacy server collapses a
// burst into one report five seconds after the first event, and the clients that display
// the count depend on that calmness.
const ListenerCountReportDelay = 5 * time.Second

// SetLivefeedCommand replaces one listener's live-watch selection: which talkgroups on
// which systems that listener wants delivered as they happen.
//
// The selection replaces the previous one entirely. A listener that submits a selection
// without a talkgroup that was previously on has switched that talkgroup off.
type SetLivefeedCommand struct {
	ClientID string
	Livefeed domain.Livefeed
}

// SetLivefeedResult is the acknowledgement the protocol asks for: whether the listener is
// now watching anything.
type SetLivefeedResult struct {
	Watching bool
}

// SetLivefeedHandler applies a listener's live-watch selection.
type SetLivefeedHandler struct {
	hub ports.Hub
}

// NewSetLivefeedHandler builds the handler from its port.
func NewSetLivefeedHandler(hub ports.Hub) *SetLivefeedHandler {
	return &SetLivefeedHandler{hub: hub}
}

// Handle stores the selection and answers the listener with whether anything is switched
// on, which is the reply the frozen protocol fixes.
//
// The selection itself lives on the listener's session, because it is session state: the
// hub decides whether a live call reaches that session.
func (handler *SetLivefeedHandler) Handle(ctx context.Context, cmd SetLivefeedCommand) (SetLivefeedResult, error) {
	handler.hub.SetLivefeed(cmd.ClientID, cmd.Livefeed)

	watching := !handler.hub.LivefeedWatchesNothing(cmd.ClientID)

	handler.hub.SendLivefeedMap(cmd.ClientID, watching)

	return SetLivefeedResult{Watching: watching}, nil
}

// ListenerCountDebouncer reports how many listeners are watching, once per burst of
// joins and leaves rather than once per event.
//
// The report is always written to the server log. It is only sent to the listeners when
// the server option asks for the count to be shown, which is what the legacy server does.
type ListenerCountDebouncer struct {
	hub       ports.Hub
	logs      ports.LogRepository
	options   ports.OptionsRepository
	scheduler ports.Scheduler
	clock     ports.Clock

	// mutex guards pending, because the hub drives this from its own event loop while a
	// configuration change or shutdown can reach it from elsewhere.
	mutex   sync.Mutex
	pending ports.Timer
}

// NewListenerCountDebouncer builds the debouncer from its ports.
func NewListenerCountDebouncer(
	hub ports.Hub,
	logs ports.LogRepository,
	options ports.OptionsRepository,
	scheduler ports.Scheduler,
	clock ports.Clock,
) *ListenerCountDebouncer {
	return &ListenerCountDebouncer{
		hub:       hub,
		logs:      logs,
		options:   options,
		scheduler: scheduler,
		clock:     clock,
	}
}

// ListenerChanged records that a listener joined or left.
//
// Only the first change in a burst schedules a report; the rest are absorbed by the one
// already pending. The report counts the listeners that are there when it is written, not
// the ones that were there when it was scheduled — that is what makes collapsing a burst
// correct rather than merely quieter.
func (debouncer *ListenerCountDebouncer) ListenerChanged() {
	debouncer.mutex.Lock()
	defer debouncer.mutex.Unlock()

	if debouncer.pending != nil {
		return
	}

	// The report is a background occurrence rather than the answer to a request, so it is
	// not tied to any request's lifetime.
	debouncer.pending = debouncer.scheduler.Schedule(ListenerCountReportDelay, func() {
		debouncer.mutex.Lock()
		debouncer.pending = nil
		debouncer.mutex.Unlock()

		debouncer.report(context.Background())
	})
}

// Pending reports whether a report is already scheduled, which is what a test checks to
// confirm a burst collapsed into one report.
func (debouncer *ListenerCountDebouncer) Pending() bool {
	debouncer.mutex.Lock()
	defer debouncer.mutex.Unlock()

	return debouncer.pending != nil
}

// Cancel drops a report that is scheduled and not yet written.
//
// A server that is stopping does not owe a count of listeners: the connections it is about to end are each
// reported by their own departure, and a count of listeners that no longer exist is not something an
// operator can act on. And the report is written into the database the stop closes at its end, so a report
// still pending at that moment is an entry that cannot be written at all — a write that gets half way
// through is what leaves a database holding a rollback journal.
func (debouncer *ListenerCountDebouncer) Cancel() {
	debouncer.mutex.Lock()
	defer debouncer.mutex.Unlock()

	if debouncer.pending == nil {
		return
	}

	debouncer.pending.Cancel()

	debouncer.pending = nil
}

// report writes the count to the server log and, when the option asks for it, sends it to every listener.
// The order is the legacy order: the log first, then the listeners.
//
// The entry is stamped with the clock at the moment the report is written. The legacy
// `server/listener.go` builds it with a level and a message and nothing else, so the zero time reaches the
// timestamp column as -62135596800000 and the log view then drops the row from its results while still
// counting it — which is what the year-one rows in a deployment's log are.
func (debouncer *ListenerCountDebouncer) report(ctx context.Context) {
	count := debouncer.hub.ListenerCount()

	if err := debouncer.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: debouncer.clock.Now(),
		Level:    domain.LogLevelInfo,
		Source:   domain.LogSourceLivefeed,
		Message:  listenersCountPhrase(count),
	}); err != nil {
		return
	}

	options, err := debouncer.options.ReadOptions(ctx)
	if err != nil {
		return
	}

	if options.ShowListenersCount {
		debouncer.hub.BroadcastListenersCount(count)
	}
}

// listenersCountPhrase is the count in the words an operator reads. "there are 1 listeners" is what the
// approved wording produces for the most common count there is, and a log line that reads like a
// substitution mistake is the kind of thing this feature exists to remove.
func listenersCountPhrase(count int) string {
	if count == 1 {
		return "there is " + counted(count, "listener", "listeners")
	}

	return "there are " + counted(count, "listener", "listeners")
}
