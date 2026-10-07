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
	"fmt"
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
) *ListenerCountDebouncer {
	return &ListenerCountDebouncer{
		hub:       hub,
		logs:      logs,
		options:   options,
		scheduler: scheduler,
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

// report writes the count to the server log and, when the option asks for it, sends it to
// every listener. The order is the legacy order: the log first, then the listeners.
func (debouncer *ListenerCountDebouncer) report(ctx context.Context) {
	count := debouncer.hub.ListenerCount()

	if err := debouncer.logs.AppendLog(ctx, domain.LogEntry{
		Level:   domain.LogLevelInfo,
		Message: fmt.Sprintf("listeners count is %v", count),
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
