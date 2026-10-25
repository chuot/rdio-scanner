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

package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
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
	mcpbackend "rdio-scanner/internal/presentation/mcp/backend"
	scannerbackend "rdio-scanner/internal/presentation/scanner/backend"
)

const pruneInterval = time.Hour

const requestDeadline = 25 * time.Second

const streamDeadline = 10 * time.Minute

const pruneDeadline = 5 * time.Minute

func withRequestDeadline(next http.Handler, at time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), at)
		defer cancel()

		answered := &deadlineAnswer{w: w}

		finished := make(chan struct{})

		go func() {
			defer func() {
				if problem := recover(); problem != nil {
					answered.panic = problem
				}

				close(finished)
			}()

			next.ServeHTTP(answered, r.WithContext(ctx))
		}()

		select {
		case <-finished:
			if answered.panic != nil {
				panic(answered.panic)
			}

			return
		case <-ctx.Done():
		}

		if answered.refuseDeadline() {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, "the request could not be finished in time")

			return
		}

		<-finished

		if answered.panic != nil {
			panic(answered.panic)
		}
	})
}

func withSubtreeDeadlines(routes http.Handler, streams http.Handler, streamAt, requestAt time.Duration) http.Handler {
	tree := http.NewServeMux()

	if streams != nil {
		bounded := withRequestDeadline(streams, streamAt)

		tree.Handle(mcpbackend.EndpointPath, bounded)
		tree.Handle(mcpbackend.EndpointPath+"/", bounded)
	}

	tree.Handle("/", withRequestDeadline(routes, requestAt))

	return tree
}

// deadlineAnswer is the response writer the deadline middleware hands the handler,
// passing everything through to the server's own writer, including the hijack the
// websocket endpoints take, while watching what has arrived.
// see the deadline-response-writer maintainer note.
type deadlineAnswer struct {
	w        http.ResponseWriter
	mu       sync.Mutex
	answered bool // the handler began answering, or took over its connection
	refused  bool // the deadline answered

	panic any
}

// refuseDeadline writes the deadline's failure if nothing has answered yet,
// and reports whether it wrote it.
func (d *deadlineAnswer) refuseDeadline() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.answered {
		return false
	}

	d.refused = true

	return true
}

func (d *deadlineAnswer) Header() http.Header { return d.w.Header() }

func (d *deadlineAnswer) WriteHeader(code int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.refused {
		return // the deadline answered; the handler's late answer is dropped
	}

	d.answered = true

	d.w.WriteHeader(code)
}

func (d *deadlineAnswer) Write(b []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.refused {
		return len(b), nil
	}

	d.answered = true

	return d.w.Write(b)
}

func (d *deadlineAnswer) Flush() {
	if flusher, ok := d.w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack lets the websocket endpoints take the connection through this writer,
// the whole reason this middleware is not the standard library's.
func (d *deadlineAnswer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.answered = true

	return d.w.(http.Hijacker).Hijack()
}

var (
	_ http.ResponseWriter = (*deadlineAnswer)(nil)
	_ http.Hijacker       = (*deadlineAnswer)(nil)
	_ http.Flusher        = (*deadlineAnswer)(nil)
)

// consoleLog is where the server writes the lines it says out loud: the startup
// lines an operator reads and the lines the endpoints say about a connection
// they could not take over. They are not the server's log and never go into it,
// written in the shape the legacy server's console wrote them.
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

// Write lets the console stand in for the plain writer the websocket endpoints
// write whole lines to, since what they say is already a finished line and a
// logger handed a finished line would say it twice.
// see the console-window-and-log-copies maintainer note.
func (console *consoleLog) Write(b []byte) (int, error) {
	console.logger.Print(strings.TrimRight(string(b), "\n"))

	return len(b), nil
}

// renderLogEntry is the operator's copy of a stored entry: the level it is filed
// under, the part of the server that wrote it, then what happened. This is the
// shape an operator greps for in the system journal. It is a rendering and not
// what the log view holds, so filtering never depends on how one copy is printed.
func renderLogEntry(level string, source domain.LogSource, message string) string {
	return level + " " + string(source) + ": " + message
}

// Echo is the terminal's copy of a log entry. The legacy server's console copy is
// the message on its own, with the moment in front and no level, so an operator
// watching a terminal saw the same line for an info and an error.
func (console *consoleLog) Echo(level string, source domain.LogSource, message string) {
	console.println(renderLogEntry(level, source, message))
}

var _ LogEcho = (*consoleLog)(nil)

// loggedEvents is the server's log and the operator's copy of it. The legacy
// server wrote every entry to the console as well as into the log view, unless
// it ran as a service, which took the line instead. The line is said before the
// entry is stored, and a log that cannot be stored does not take the line back.
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

// listenerSettings answers what the server requires of listeners, from the
// configuration in effect, asked at the moment of the question so the answer
// comes from the access list as it is now.
// see the listener-settings-from-the-access-list maintainer note.
type listenerSettings struct {
	accesses ports.AccessRepository
	options  ports.OptionsRepository
}

var _ scannerbackend.ServerSettings = (*listenerSettings)(nil)

func (settings *listenerSettings) Settings() scannerbackend.Settings {
	ctx := context.Background()

	options, err := settings.options.ReadOptions(ctx)
	if err != nil {
		// A server that cannot read its own options has nothing to report about itself,
		// so it answers with the shipped defaults, the least surprising thing to give
		// and not a lie about a fresh deployment.
		// see the listener-settings-from-the-access-list maintainer note.
		options = domain.DefaultOptions()
	}

	accesses, err := settings.accesses.AllAccesses(ctx)
	if err != nil {
		// Not knowing who is allowed is not the same as everybody being allowed, but it
		// is not a reason to turn every listener away: an unreadable access list is a
		// broken deployment, and the log view is where that becomes visible.
		// see the listener-settings-from-the-access-list maintainer note.
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

// optionsSecrets hands the token adapter the signing secret the server is running
// with, read when a token is made rather than remembered, and an options read
// that fails is reported instead of answered with an empty string.
// see the signing-secret-choice maintainer note.
type optionsSecrets struct {
	options ports.OptionsRepository
}

var _ tokens.Secrets = optionsSecrets{}

func (secrets optionsSecrets) Secret() (string, error) {
	options, err := secrets.options.ReadOptions(context.Background())
	if err != nil {
		// The failure is reported, not answered with the empty string: Issue fails beside
		// it, and the login handler's existing failure report is the one log entry the
		// operator reads.
		// see the signing-secret-choice maintainer note.
		return "", fmt.Errorf("tokens: reading the signing secret: %w", err)
	}

	return options.Secret, nil
}

// ingestSink hands a call a directory watcher found to the use case that decides
// what to do with it, discarding the outcome for the same reason the watcher
// discards the error: nobody waits for an answer about a file that appeared.
// see the ingest-sink-capture maintainer note.
type ingestSink struct {
	ingest *application.IngestCallHandler
}

var _ dirwatch.IngestSink = (*ingestSink)(nil)

func (sink *ingestSink) IngestCall(ctx context.Context, call domain.Call) error {
	_, err := sink.ingest.Handle(ctx, application.IngestCallCommand{Call: call})

	return err
}

// listenerChanges is what the hub tells when the number of listeners is no longer
// what it was. The hub and the debouncer need each other, and the composition
// root breaks the circle by giving the hub a forwarder first.
// see the hub-and-debouncer-contract maintainer note.
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

// catchAll routes a request that is not an API call: one that asks to become a
// listener connection goes to the listener endpoint, everything else to the
// application the binary carries, decided by the upgrade header alone.
// see the catch-all-handler maintainer note.
func catchAll(listener http.Handler, app http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if upgrade.Wanted(r) {
			listener.ServeHTTP(w, r)

			return
		}

		app.ServeHTTP(w, r)
	}
}

// serveApplications registers the two applications the binary carries and the
// paths they are served from. The scanner path is also the listener path, told
// apart by the upgrade header. The admin app keeps its own subtree, and /admin
// itself is left unregistered so the subtree redirect resolves its relative URLs.
func serveApplications(mux *http.ServeMux, listener, scannerApp, adminApp http.Handler) {
	mux.HandleFunc("/", catchAll(listener, scannerApp))
	mux.HandleFunc("/admin/", catchAll(listener, adminApp))
}

// pruneHourly reproduces the legacy scheduler: a pruning an hour, each reading
// the retention configured at the moment it runs, a failure reported in the log
// view and otherwise forgotten. It uses a fresh timer after each pruning, and
// cancelling ends the whole schedule, taking the scheduler so the schedule tests.
func pruneHourly(scheduler ports.Scheduler, prune pruneRunner) *hourlyPruning {
	schedule := &hourlyPruning{scheduler: scheduler, prune: prune}

	schedule.next()

	return schedule
}

// pruneRunner is the retention pass as the schedule sees it: the schedule runs
// the pass and nothing else, and the seam is what lets a test record the context
// the pass ran under, where the deadline is visible.
// see the request-and-stream-deadline-constants maintainer note.
type pruneRunner interface {
	Handle(ctx context.Context, cmd application.PruneOldCallsCommand) (application.PruneOldCallsResult, error)
}

var _ pruneRunner = (*application.PruneOldCallsHandler)(nil)

// hourlyPruning is the server's retention schedule.
type hourlyPruning struct {
	scheduler ports.Scheduler
	prune     pruneRunner

	mutex   sync.Mutex
	pending ports.Timer
	stopped bool
}

var _ ports.Timer = (*hourlyPruning)(nil)

// next puts the next pruning on the scheduler, unless the schedule has been ended.
func (schedule *hourlyPruning) next() {
	timer := schedule.scheduler.Schedule(pruneInterval, func() {
		// The pass runs under the deadline: a pass blocked on a database that does not
		// answer stops at its bound, the next hour always arrives, and the failure is
		// already in the log view for the legacy scheduler to keep going past.
		// see the request-and-stream-deadline-constants maintainer note.
		ctx, cancel := context.WithTimeout(context.Background(), pruneDeadline)
		defer cancel()

		_, _ = schedule.prune.Handle(ctx, application.PruneOldCallsCommand{})

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
