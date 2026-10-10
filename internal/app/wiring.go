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

package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/dirwatch"
	"rdio-scanner/internal/infrastructure/tokens"
	"rdio-scanner/internal/presentation/common/upgrade"
	scannerbackend "rdio-scanner/internal/presentation/scanner/backend"
)

// pruneInterval is how often the server applies its retention, which is the legacy server's hourly
// ticker. The retention itself is read when a pruning runs and not when the timer is set, so a change
// the administrator makes takes effect on the next hour.
const pruneInterval = time.Hour

// consoleLog is where the server writes the lines it says out loud: the startup lines an operator reads,
// and the lines the endpoints say about a connection they could not take over. They are not the server's
// log and never go into it.
//
// They are written in the shape the legacy server writes them, because the legacy server's console is Go's
// log package: the moment the line was said, then the line.
type consoleLog struct {
	logger *log.Logger
}

func newConsoleLog(out io.Writer) *consoleLog {
	return &consoleLog{logger: log.New(out, "", log.LstdFlags)}
}

func (console *consoleLog) println(a ...any) {
	console.logger.Println(a...)
}

func (console *consoleLog) printf(format string, a ...any) {
	console.logger.Printf(format, a...)
}

// Write lets the console stand in for the plain writer the websocket endpoints write whole lines to.
//
// Those endpoints are given a writer rather than a logger because what they say is already a finished
// line, and a logger that is handed a finished line would say it twice.
func (console *consoleLog) Write(b []byte) (int, error) {
	console.logger.Print(strings.TrimRight(string(b), "\n"))

	return len(b), nil
}

// renderLogEntry is the operator's copy of a stored entry: the level it is filed under, the part of the
// server that wrote it, then what happened.
//
// This is the shape an operator of a deployment that never opens the web log view greps for — `journalctl
// -u rdio-scanner | grep dirwatch` — which is what carries the subsystem split to the deployments this
// feature exists for.
//
// It is a rendering and not what the log view holds. The stored message says what happened and nothing else,
// so filtering the log view by subsystem never depends on how one particular copy of it happens to be printed.
func renderLogEntry(level string, source domain.LogSource, message string) string {
	return level + " " + string(source) + ": " + message
}

// Echo is the terminal's copy of a log entry. The legacy server's console copy is the message on its own,
// with the moment in front of it and no level, so an operator watching a terminal saw the same line for an
// info and an error.
func (console *consoleLog) Echo(level string, source domain.LogSource, message string) {
	console.println(renderLogEntry(level, source, message))
}

var _ LogEcho = (*consoleLog)(nil)

// loggedEvents is the server's log, and the operator's copy of it.
//
// The legacy server's LogEvent writes every entry to the console as well as into the log view — unless the
// server is running as a service, in which case the service's own logger takes the line instead. The log
// view is the administrator's copy and the console copy is the operator's, and a server that writes only
// the first leaves the terminal silent while the log fills up.
//
// The line is said before the entry is stored, which is the order the legacy server says it in, and a
// log that cannot be stored does not take the line back.
type loggedEvents struct {
	logs ports.LogRepository
	echo LogEcho
}

var _ ports.LogRepository = (*loggedEvents)(nil)

func (events *loggedEvents) AppendLog(ctx context.Context, entry domain.LogEntry) error {
	events.echo.Echo(entry.Level, entry.Source, entry.Message)

	return events.logs.AppendLog(ctx, entry)
}

func (events *loggedEvents) SearchLogs(ctx context.Context, search domain.LogSearch) (ports.LogSearchPage, error) {
	return events.logs.SearchLogs(ctx, search)
}

func (events *loggedEvents) DeleteLogsBefore(ctx context.Context, before time.Time) (int, error) {
	return events.logs.DeleteLogsBefore(ctx, before)
}

// listenerSettings answers what the server requires of listeners, from the configuration in effect.
//
// It is asked at the moment of the question rather than told in advance, which is what makes adding the
// first access code change what listeners that are already connected are asked for: the answer comes from
// the access list as it is now, and not as it was when each connection joined.
//
// The access list is what decides whether listeners are asked for a code, exactly as it is in the legacy
// server: a server with no access codes is open, and one code is enough to close it.
type listenerSettings struct {
	accesses ports.AccessRepository
	options  ports.OptionsRepository
}

var _ scannerbackend.ServerSettings = (*listenerSettings)(nil)

func (settings *listenerSettings) Settings() scannerbackend.Settings {
	ctx := context.Background()

	options, err := settings.options.ReadOptions(ctx)
	if err != nil {
		// A server that cannot read its own options has nothing to report about itself, and the
		// answer it gives is the one a server with no configuration at all runs with. The legacy
		// server keeps its options in memory and cannot fail here; this one reads them, and the
		// failure it would report has nowhere to go because the question has no answer to fail
		// with. The shipped defaults are the least surprising thing to answer with, and they are
		// not a lie about a fresh deployment.
		options = domain.DefaultOptions()
	}

	accesses, err := settings.accesses.AllAccesses(ctx)
	if err != nil {
		// Not knowing who is allowed is not the same as everybody being allowed, but it is also not
		// a reason to turn every listener away: an unreadable access list is a broken deployment,
		// and the log view is where that becomes visible.
		return settingsOf(options, false)
	}

	return settingsOf(options, accesses.Restricted())
}

func settingsOf(options domain.Options, restricted bool) scannerbackend.Settings {
	return scannerbackend.Settings{
		Branding:   options.Branding,
		Email:      options.Email,
		MaxClients: options.MaxClients,
		Restricted: restricted,
	}
}

// optionsSecrets hands the token adapter the signing secret the server is running with.
//
// The secret is read when a token is made rather than remembered, because the options are the
// configuration in effect and a re-derivation can replace the secret. An options read that fails answers
// with nothing, and a token that cannot be signed is a login the administrator is told failed — which is
// the answer the legacy server gives when its own signing fails.
type optionsSecrets struct {
	options ports.OptionsRepository
}

var _ tokens.Secrets = optionsSecrets{}

func (secrets optionsSecrets) Secret() string {
	options, err := secrets.options.ReadOptions(context.Background())
	if err != nil {
		return ""
	}

	return options.Secret
}

// ingestSink hands a call a directory watcher found to the use case that decides what to do with it.
//
// The watcher's seam takes a call and returns an error; the use case takes a command and answers with an
// outcome as well as an error. The outcome is discarded here for the same reason the watcher discards the
// error: nobody is waiting for an answer about a file that appeared in a watched directory, and the ingest
// path writes its own report into the log view.
type ingestSink struct {
	ingest *application.IngestCallHandler
}

var _ dirwatch.IngestSink = (*ingestSink)(nil)

func (sink *ingestSink) IngestCall(ctx context.Context, call domain.Call) error {
	_, err := sink.ingest.Handle(ctx, application.IngestCallCommand{Call: call})

	return err
}

// listenerChanges is what the hub tells when the number of listeners is no longer what it was, before the
// thing that answers that news exists.
//
// The debouncer needs the hub — the hub is what knows how many listeners there are and who would be told
// the count — and the hub needs the debouncer, because a change in the count is the debouncer's to hold
// back. The composition root breaks the circle by giving the hub a forwarder first and telling it where to
// forward as soon as the debouncer is built. Nothing can reach the hub before the routes are being served,
// which is after the forwarder knows where it forwards to.
type listenerChanges struct {
	debouncer *application.ListenerCountDebouncer
}

var _ scannerbackend.ListenerChanges = (*listenerChanges)(nil)

func (changes *listenerChanges) ListenerChanged() {
	if changes.debouncer == nil {
		return
	}

	changes.debouncer.ListenerChanged()
}

// catchAll routes a request that is not an API call: a request that asks to become a listener connection
// goes to the listener endpoint, and everything else goes to the application the binary carries.
//
// This is the legacy server's one catch-all handler, and the only place in this server where a request can
// be either. The decision is the upgrade header alone, and it is made before anything is looked up, which
// is what lets a native application open a connection on whatever path it was on.
func catchAll(listener http.Handler, app http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if upgrade.Wanted(r) {
			listener.ServeHTTP(w, r)

			return
		}

		app.ServeHTTP(w, r)
	}
}

// serveApplications registers the two applications the binary carries and the paths they are served from.
//
// The path the scanner application is served from is also the path a listener connects to, and the upgrade
// header decides which. That is the legacy server's one catch-all handler, and this is the only place in this
// server where a request can be either.
//
// The administration application is served from its own subtree, and `/admin` itself is deliberately not
// registered: a subtree pattern redirects a request for the subtree root by adding the trailing slash, so
// `/admin` becomes a 307 to `/admin/`, and every relative URL in the admin shell then resolves inside the
// admin bundle. Registering `/admin` as well would answer it without the redirect and leave those relative
// URLs resolving at the root of the server instead.
//
// The listener stays on the admin route too. No native application opens a listener connection at `/admin`,
// which is what makes the redirect at that one path an accepted behavior change rather than a break.
func serveApplications(mux *http.ServeMux, listener, scannerApp, adminApp http.Handler) {
	mux.HandleFunc("/", catchAll(listener, scannerApp))
	mux.HandleFunc("/admin/", catchAll(listener, adminApp))
}

// pruneHourly reproduces the legacy server's scheduler: a pruning an hour, each one reading the retention
// the server is configured with at the moment it runs, and a failure reported in the log view and
// otherwise forgotten — the next hour tries again.
//
// The hour is a fresh timer after each pruning rather than a ticker, and cancelling ends the whole schedule
// rather than only the pruning that happens to be waiting when it is cancelled: a server that has pruned
// twice and is then stopped does not prune a third time an hour later. A pruning that is already running is
// not stopped by cancelling, which is the same thing cancelling a timer does.
//
// It takes the scheduler rather than using the server's own so that the schedule can be tested without
// waiting an hour for it.
func pruneHourly(scheduler ports.Scheduler, prune *application.PruneOldCallsHandler) *hourlyPruning {
	schedule := &hourlyPruning{scheduler: scheduler, prune: prune}

	schedule.next()

	return schedule
}

// hourlyPruning is the server's retention schedule.
type hourlyPruning struct {
	scheduler ports.Scheduler
	prune     *application.PruneOldCallsHandler

	mutex   sync.Mutex
	pending ports.Timer
	stopped bool
}

var _ ports.Timer = (*hourlyPruning)(nil)

// next puts the next pruning on the scheduler, unless the schedule has been ended.
func (schedule *hourlyPruning) next() {
	timer := schedule.scheduler.Schedule(pruneInterval, func() {
		// The legacy server's scheduler.run writes the failure into the log view and keeps going, and the
		// pruning handler has already written it there. The next hour tries again.
		_, _ = schedule.prune.Handle(context.Background(), application.PruneOldCallsCommand{})

		schedule.next()
	})

	schedule.mutex.Lock()
	defer schedule.mutex.Unlock()

	if schedule.stopped {
		timer.Cancel()

		return
	}

	schedule.pending = timer
}

// Cancel ends the schedule: the pruning that is waiting is abandoned, and no further hour is scheduled.
func (schedule *hourlyPruning) Cancel() {
	schedule.mutex.Lock()
	defer schedule.mutex.Unlock()

	schedule.stopped = true

	if schedule.pending != nil {
		schedule.pending.Cancel()
	}
}
