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

// pruneInterval is how often the server applies its retention, which is the legacy server's hourly
// ticker. The retention itself is read when a pruning runs and not when the timer is set, so a change
// the administrator makes takes effect on the next hour.
const pruneInterval = time.Hour

// requestDeadline is thebound on one request. It sits inside
// httpTimeout, the socket bound: the socket timeouts fire when the socket goes
// silent, and a handler blocked on a database that does not answer is a request
// held open with the socket quietly connected — the administrator's browser
// spins with no answer at all. The deadline gives every request a context that
// expires, and the answer to a request whose work outlives it is a failure the
// administrator can read.
const requestDeadline = 25 * time.Second

// streamDeadline is the bound on one Model Context Protocol exchange, and it is materially larger
// than the request bound on purpose.
//
// The request bound is the administrator's: a request that answers in a few seconds, bounded so a
// handler blocked on a database that does not answer is answered with a failure the administrator can
// read. The assistant's endpoint carries two kinds of work that bound would cut off: a composite
// capability runs several queries over a deployment with a full history, and the session's own stream
// stays open for the life of the session. A bound chosen for a request that answers in a few seconds
// is not the bound for either of them, so the endpoint is hung as its own subtree with its own bound,
// and every other route keeps the administrator's.
//
// It is a bound and not a licence to run for ever: a capability that outlives it is answered with the
// same failure the administrator's blocked request gets, and each capability carries a bound of its
// own, well inside this one, so the assistant hears the refusal from the capability it asked for.
const streamDeadline = 10 * time.Minute

// pruneDeadline bounds the hourly retention pass: the pass runs on
// the server's own schedule with nobody waiting for its answer, and a pass
// blocked on a database that does not answer would hold the schedule and write
// no report. Five minutes is long enough for the pass on a full deployment and
// short enough that the next hour always arrives.
const pruneDeadline = 5 * time.Minute

// withRequestDeadline is the middleware that gives each request a context with
// the given deadline and answers with a failure when the work outlives it. The
// server runs it with requestDeadline; a test runs it with a shorter one to
// watch it fire.
//
// It is not the standard library's http.TimeoutHandler: that hands its handler
// a response writer that does not implement http.Hijacker, and the listener
// and configuration sockets are built by a library that refuses a writer
// without it. This middleware passes the server's own writer through, wrapped
// so the deadline answer can be written exactly once: a request that has
// answered, or has taken over its connection, is left alone when the deadline
// arrives — those connections live by what their sockets do, not by a request
// context, and the middleware waits for the handler rather than returning in
// the middle of a stream it has begun — and a request that has answered
// nothing gets the failure.
func withRequestDeadline(next http.Handler, at time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), at)
		defer cancel()

		answered := &deadlineAnswer{w: w}

		finished := make(chan struct{})

		go func() {
			// The handler's panic rides back to the server goroutine that is
			// answering this request, where the server's own panic rules
			// apply; a panic swallowed in this goroutine would leave the
			// server running with a bug it never hears about.
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

		// The handler has begun answering, and what it began is a connection that lives by what
		// its socket does, not by a request context: a stream it is still writing, a content
		// endpoint naming its status before its bytes, a socket it has taken over. Returning here
		// would end a stream mid-sentence — the assistant's session stream, an event stream
		// carrying a capability's answer — so the deadline waits for the handler to finish, and
		// the socket's own bounds decide when the connection is given up.
		<-finished

		if answered.panic != nil {
			panic(answered.panic)
		}
	})
}

// withSubtreeDeadlines hangs the server's surfaces on the routes the server serves, with each subtree
// under the bound its work needs: the request bound on every route the server answers, and the stream
// bound on the Model Context Protocol endpoint alone.
//
// The two bounds cannot be nested — a request context takes the earlier of the deadlines put on it, so
// a bound hung inside the administrator's bound is the administrator's bound, and the assistant's
// endpoint would be cut off by a bound it was given a larger one to escape. The routes are therefore
// split into two subtrees, each wrapped once, and the tree that hangs them carries no bound of its
// own.
//
// The endpoint is hung on its exact path and on the subtree below it, so the assistant's address is
// answered by the surface and not by the web application behind it, whichever form the assistant was
// given to configure.
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

// deadlineAnswer is the response writer the deadline middleware hands the
// handler: everything passes through to the server's own writer — including
// the hijack the websocket endpoints take — and the middleware watches what
// arrived, so the deadline's failure is written only for a request that has
// answered nothing.
//
// Two facts are kept apart, and the difference is the whole point of this writer.
// `answered` means the handler began answering; `refused` means the deadline
// answered first. A handler that has begun answering keeps answering: a content
// endpoint names its status and then writes its bytes in as many writes as the
// content needs, and a writer that delivered only the first of them would
// truncate every such endpoint to an empty body. Only after the deadline has
// written its own failure does the handler's late answer get dropped, because
// by then the request has an answer and two bodies would be one answer too many.
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
// which is the whole reason this middleware is not the standard library's.
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
// configuration in effect and a re-derivation can replace the secret. An options read that fails is
// reported, not answered with the empty string: Issue fails beside it, and a token that
// cannot be signed is a login the administrator is told failed — which is the answer the legacy server
// gives when its own signing fails.
type optionsSecrets struct {
	options ports.OptionsRepository
}

var _ tokens.Secrets = optionsSecrets{}

func (secrets optionsSecrets) Secret() (string, error) {
	options, err := secrets.options.ReadOptions(context.Background())
	if err != nil {
		// The failure is reported, not answered with the empty string:
		// Issue fails beside it, and the login handler's existing failure report is
		// the one log entry the operator reads.
		return "", fmt.Errorf("tokens: reading the signing secret: %w", err)
	}

	return options.Secret, nil
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
func pruneHourly(scheduler ports.Scheduler, prune pruneRunner) *hourlyPruning {
	schedule := &hourlyPruning{scheduler: scheduler, prune: prune}

	schedule.next()

	return schedule
}

// pruneRunner is the retention pass as the schedule sees it: the schedule runs
// the pass and nothing else, and the seam is what lets a test record the
// context the pass ran under — where the deadline is visible.
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
		// The pass runs under the deadline: a pass blocked on a
		// database that does not answer stops at its bound, and the next hour
		// always arrives.
		//
		// The legacy server's scheduler.run writes the failure into the log view and keeps going, and the
		// pruning handler has already written it there. The next hour tries again.
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
