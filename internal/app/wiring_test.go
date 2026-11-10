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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"path/filepath"

	"github.com/kardianos/service"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/persistence"
)

// console-and-log copies: proves the composition root hands the hub and the
// listener endpoint a log an operator actually reads.

// a server in a terminal says every log entry it files out loud.
func TestAServerAnOperatorStartedSaysItsLogEntriesToTheTerminal(t *testing.T) {
	var terminal bytes.Buffer

	srv := testServerOn(t, &terminal)

	const message = "the recording could not be stored"

	if err := srv.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Now(),
		Level:    domain.LogLevelError,
		Source:   domain.LogSourceCalls,
		Message:  message,
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	if !bytes.Contains(terminal.Bytes(), []byte("error calls: the recording could not be stored")) {
		t.Errorf("the terminal holds %q, want the entry said with its level and the part of the server that "+
			"wrote it", terminal.String())
	}

	// the stored entry is the sentence alone; level and source belong to the
	// rendering.
	if !hasLogMessage(t, srv.db, message) {
		t.Errorf("the log view does not hold the entry the server wrote")
	}
}

// a server the service manager started files entries to the manager and
// stays silent on the terminal.
func TestAServerTheServiceManagerStartedSaysItsLogEntriesToTheManager(t *testing.T) {
	var terminal bytes.Buffer

	manager := &recordedServiceLogger{}

	srv := testServerUnderTheServiceManager(t, &terminal, manager)

	const message = "the recording could not be stored"

	if err := srv.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Now(),
		Level:    domain.LogLevelError,
		Source:   domain.LogSourceCalls,
		Message:  message,
	}); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	if !manager.has(domain.LogLevelError, "error calls: the recording could not be stored") {
		t.Errorf("the service manager's log holds %q, want the entry at the level it names, with the part of "+
			"the server that wrote it", manager.String())
	}

	if terminal.Len() != 0 {
		t.Errorf("the terminal holds %q, want a server running as a service to have nothing to say to one", terminal.String())
	}

	// the administrator's copy is the same either way.
	if !hasLogMessage(t, srv.db, message) {
		t.Errorf("the log view does not hold the entry the server wrote")
	}
}

// What the startup pass says about what it added is said through the copy the
// server was told to speak through, at the level the line carries: a run the
// service manager started hears it there and keeps its terminal silent, as it
// does for every other entry.
func TestWhatTheStartupPassSaysIsSaidThroughTheConfiguredEcho(t *testing.T) {
	var terminal bytes.Buffer

	manager := &recordedServiceLogger{}

	srv := testServerUnderTheServiceManager(t, &terminal, manager)

	// The one key a database this server builds for itself does not already carry.
	const message = "created the call history index calls_timestamp_cover on calls over (timestamp, callId, audioLength, systemId, talkgroupId)"

	if !manager.has(domain.LogLevelInfo, "info database: "+message) {
		t.Errorf("the service manager's log holds %q, want the line the startup pass says for the call history "+
			"key it adds, at info under the part of the server that owns it", manager.String())
	}

	if terminal.Len() != 0 {
		t.Errorf("the terminal holds %q, want a server running as a service to have nothing to say to one while "+
			"the startup pass runs", terminal.String())
	}

	if !hasLogMessage(t, srv.db, message) {
		t.Errorf("the log view does not hold the line the startup pass said")
	}
}

// A run in a terminal hears the same line, in the shape every other line that
// run says has.
func TestWhatTheStartupPassSaysReachesATerminalRun(t *testing.T) {
	var terminal bytes.Buffer

	testServerOn(t, &terminal)

	const line = "info database: created the call history index calls_timestamp_cover on calls over (timestamp, callId, audioLength, systemId, talkgroupId)"

	if !bytes.Contains(terminal.Bytes(), []byte(line)) {
		t.Errorf("the terminal holds %q, want the line naming the call history key the startup pass adds, with its "+
			"level and the part of the server that owns it", terminal.String())
	}
}

// each entry reaches the manager's log at the level it is filed under,
// including levels with no word for them.
func TestTheServiceLogTakesEachEntryAtTheLevelItIsFiledUnder(t *testing.T) {
	manager := &recordedServiceLogger{}

	echo := NewServiceLog(manager)

	echo.Echo(domain.LogLevelError, domain.LogSourceCalls, "the recording could not be stored")
	echo.Echo(domain.LogLevelWarn, domain.LogSourceDirwatch, "the watched directory is not there")
	echo.Echo(domain.LogLevelInfo, domain.LogSourceServer, "server started")
	echo.Echo("verbose", domain.LogSourceServer, "an entry filed under a level the manager has no word for")

	for _, want := range []struct{ level, message string }{
		{domain.LogLevelError, "error calls: the recording could not be stored"},
		{domain.LogLevelWarn, "warn dirwatch: the watched directory is not there"},
		{domain.LogLevelInfo, "info server: server started"},
	} {
		if !manager.has(want.level, want.message) {
			t.Errorf("the service manager's log holds %q, want %q at the level %s", manager.String(), want.message, want.level)
		}
	}

	if got := manager.count(); got != 3 {
		t.Errorf("the service manager was told about %d entries, want the three whose levels it has a word for", got)
	}
}

// a refused connection is said out loud and stored, not printed alone.
func TestAConnectionTheServerWouldNotTakeOverIsSaidOutLoud(t *testing.T) {
	var terminal bytes.Buffer

	srv := testServerOn(t, &terminal)

	if rec := srv.askForAnUpgrade("/"); rec.Code != http.StatusBadRequest {
		t.Fatalf("a request that asks to become a listener connection and is not one was answered %d, want "+
			"the answer Go's upgrader gives", rec.Code)
	}

	const message = "a listener connection was refused"

	waitForLogMentioning(t, srv.db, message)

	if !bytes.Contains(terminal.Bytes(), []byte("warn livefeed: "+message)) {
		t.Errorf("the terminal holds %q, want the entry said with its level and the part of the server that "+
			"wrote it", terminal.String())
	}
}

// hub lines about a listener reach the operator through the log the
// composition root handed the hub.
func TestWhatTheHubReportsAboutAListenerIsSaidOutLoud(t *testing.T) {
	var terminal bytes.Buffer

	srv := testServerOn(t, &terminal)

	conn := srv.dial(t, "/")

	sendFrame(t, conn, `["CFG"]`)

	const message = "a listener joined from 127.0.0.1"

	waitForLogMentioning(t, srv.db, message)

	if !bytes.Contains(terminal.Bytes(), []byte("info livefeed: "+message)) {
		t.Errorf("the terminal holds %q, want the hub's entry said with its level and the part of the server "+
			"that wrote it", terminal.String())
	}
}

// the manager-started server: the manager's log is the operator's copy.
func testServerUnderTheServiceManager(t *testing.T, out io.Writer, manager *recordedServiceLogger) *Server {
	t.Helper()

	config := testConfigWith(t, out)
	config.Echo = NewServiceLog(manager)

	srv, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { srv.stop(context.Background()) })

	return srv
}

// the manager's log has three formatted levels; none of them matter to
// the server.
type recordedServiceLogger struct {
	mutex   sync.Mutex
	levels  []string
	entries []string
}

var _ service.Logger = (*recordedServiceLogger)(nil)

func (manager *recordedServiceLogger) Error(v ...any) error {
	return manager.record(domain.LogLevelError, v)
}
func (manager *recordedServiceLogger) Info(v ...any) error {
	return manager.record(domain.LogLevelInfo, v)
}
func (manager *recordedServiceLogger) Warning(v ...any) error {
	return manager.record(domain.LogLevelWarn, v)
}

func (manager *recordedServiceLogger) Errorf(format string, a ...any) error {
	return manager.record(domain.LogLevelError, []any{fmt.Sprintf(format, a...)})
}

func (manager *recordedServiceLogger) Infof(format string, a ...any) error {
	return manager.record(domain.LogLevelInfo, []any{fmt.Sprintf(format, a...)})
}

func (manager *recordedServiceLogger) Warningf(format string, a ...any) error {
	return manager.record(domain.LogLevelWarn, []any{fmt.Sprintf(format, a...)})
}

func (manager *recordedServiceLogger) record(level string, v []any) error {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	manager.levels = append(manager.levels, level)
	manager.entries = append(manager.entries, fmt.Sprint(v...))

	return nil
}

// has reports whether the manager was told this line at this level.
func (manager *recordedServiceLogger) has(level, message string) bool {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	for i, entry := range manager.entries {
		if entry == message && manager.levels[i] == level {
			return true
		}
	}

	return false
}

// count reports how many lines the manager was given.
func (manager *recordedServiceLogger) count() int {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	return len(manager.entries)
}

// String is what a failing test says the manager was told.
func (manager *recordedServiceLogger) String() string {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	out := ""

	for i, entry := range manager.entries {
		out += fmt.Sprintf("%s: %s; ", manager.levels[i], entry)
	}

	return out
}

// a failed read of the options signing secret is reported, not swallowed
// as the empty string.
func TestTheOptionsSecretReportsAReadFailure(t *testing.T) {
	db, err := persistence.Open(persistence.Settings{Dialect: persistence.DialectSqlite, FilePath: filepath.Join(t.TempDir(), "rdio-scanner.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	secrets := optionsSecrets{options: persistence.NewOptions(db, persistence.DialectSqlite)}

	secret, err := secrets.Secret()
	if err == nil {
		t.Fatalf("Secret: returned %q with no error, want the read failure reported", secret)
	}

	if secret != "" {
		t.Errorf("Secret returned %q beside the failure, want nothing to sign with", secret)
	}
}

// a ServeContent-style endpoint keeps every byte behind the deadline
// writer.
func TestARequestThatNamesItsStatusThenWritesItsBodyKeepsEveryByte(t *testing.T) {
	handler := withRequestDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		w.WriteHeader(http.StatusOK)

		for _, part := range []string{"first", "second", "third"} {
			if _, err := w.Write([]byte(part)); err != nil {
				t.Errorf("Write: %v", err)
			}
		}
	}), time.Second)

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/calls/1/audio", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got, want := rec.Body.String(), "firstsecondthird"; got != want {
		t.Errorf("the body = %q, want %q: an endpoint that names its status before its bytes must "+
			"keep every byte it writes", got, want)
	}
}

// a blocked handler is answered with a failure once its work outlives the
// request deadline.
func TestARequestWhoseWorkBlocksIsAnsweredWithAFailureByTheDeadline(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	handler := withRequestDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the repository answers a minute later; the deadline's cancellation is
		// the work's to notice.
		select {
		case <-time.After(time.Minute):
		case <-release:
		}
	}), 50*time.Millisecond)

	server := httptest.NewServer(handler)
	defer server.Close()

	started := time.Now()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}

	defer resp.Body.Close()

	elapsed := time.Since(started)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d: a request whose work outlives the deadline is answered with a failure", resp.StatusCode, http.StatusServiceUnavailable)
	}

	if elapsed > 25*time.Second {
		t.Errorf("the request was held %v, want it answered by 25s: a database that does not answer must not hold an administrator's request open", elapsed)
	}
}

// the request deadline is twenty-five seconds.
func TestTheRequestDeadlineIsTwentyFiveSeconds(t *testing.T) {
	if got, want := requestDeadline, 25*time.Second; got != want {
		t.Errorf("request deadline = %v, want %v: inside the socket timeouts, so the answer is a readable failure", got, want)
	}
}

// the mcp subtree rides its own stream bound on separate route subtrees.
func TestTheModelContextProtocolSubtreeIsBoundedByItsOwnDeadline(t *testing.T) {
	release := make(chan struct{})

	var once sync.Once

	unblock := func() { once.Do(func() { close(release) }) }

	t.Cleanup(unblock)

	blocked := func(answer string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// both handlers sit behind the one-minute repository; the bound's
			// cancellation is theirs to notice.
			select {
			case <-time.After(time.Minute):
			case <-release:
			}

			io.WriteString(w, answer)
		}
	}

	const (
		requestBound = 50 * time.Millisecond
		streamBound  = 500 * time.Millisecond
	)

	server := httptest.NewServer(withSubtreeDeadlines(
		blocked("the administrator's answer"),
		blocked("the assistant's answer"),
		streamBound,
		requestBound,
	))

	defer server.Close()

	// the assistant endpoint refuses at the stream bound, not the request
	// bound.
	refused := awaitTheAnswer(t, startTheRequest(server.URL+"/mcp"))

	if refused.status != http.StatusServiceUnavailable {
		t.Fatalf("the assistant's request was answered %d, want %d: a request whose work outlives the "+
			"bound it is given is answered with a failure, the way the administrator's is",
			refused.status, http.StatusServiceUnavailable)
	}

	if refused.elapsed < 2*requestBound {
		t.Fatalf("the assistant's request was refused after %v, want it past %v: the assistant's endpoint "+
			"is bounded by the stream bound, not by the request bound the administrator's routes carry",
			refused.elapsed, 2*requestBound)
	}

	if refused.elapsed > 10*streamBound {
		t.Fatalf("the assistant's request was held %v, want it answered by %v: the stream bound bounds the "+
			"assistant's work too, and a database that does not answer must not hold an assistant open",
			refused.elapsed, 10*streamBound)
	}

	// a request past the request bound keeps working and answers with its own
	// result.
	assistant := startTheRequest(server.URL + "/mcp")

	administered := awaitTheAnswer(t, startTheRequest(server.URL+"/api/admin/alerts"))

	if administered.status != http.StatusServiceUnavailable {
		t.Fatalf("the administrator's request was answered %d, want %d: the request bound is the "+
			"administrator's bound and keeps it", administered.status, http.StatusServiceUnavailable)
	}

	if administered.elapsed > requestBound+5*time.Second {
		t.Fatalf("the administrator's request was held %v, want it answered by the request bound of %v",
			administered.elapsed, requestBound)
	}

	select {
	case answered := <-assistant:
		t.Fatalf("the assistant's request was answered %d after %v, before the request bound fired: it "+
			"is not bounded by the request bound and must still be working", answered.status, answered.elapsed)
	default:
	}

	unblock()

	completed := awaitTheAnswer(t, assistant)

	if completed.status != http.StatusOK {
		t.Fatalf("the assistant's request was answered %d, want %d: a capability that runs past the "+
			"request bound completes with its own answer\n%s",
			completed.status, http.StatusOK, completed.body)
	}

	if got, want := completed.body, "the assistant's answer"; got != want {
		t.Errorf("the assistant's request answered %q, want %q", got, want)
	}

	if completed.elapsed < requestBound {
		t.Errorf("the assistant's request completed in %v, want it past the request bound of %v: the "+
			"work that completes is work that outlived the administrator's bound", completed.elapsed, requestBound)
	}
}

// the stream bound is materially larger than the request bound.
func TestTheStreamDeadlineIsMateriallyLargerThanTheRequestDeadline(t *testing.T) {
	if streamDeadline <= 10*requestDeadline {
		t.Fatalf("the stream bound is %v and the request bound is %v: the assistant's endpoint needs a "+
			"bound materially larger than the administrator's, not a slightly larger one",
			streamDeadline, requestDeadline)
	}
}

// startTheRequest issues a request and leaves it running.
func startTheRequest(url string) chan requestAnswer {
	running := make(chan requestAnswer, 1)

	go func() {
		started := time.Now()

		resp, err := http.Get(url)
		if err != nil {
			running <- requestAnswer{err: err}

			return
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			running <- requestAnswer{err: err}

			return
		}

		running <- requestAnswer{status: resp.StatusCode, body: string(body), elapsed: time.Since(started)}
	}()

	return running
}

// requestAnswer is what a request answered and how long it took.
type requestAnswer struct {
	status  int
	body    string
	elapsed time.Duration

	err error
}

// awaitTheAnswer waits for the running request; a minute means the bound
// never fired.
func awaitTheAnswer(t *testing.T, running chan requestAnswer) requestAnswer {
	t.Helper()

	select {
	case answered := <-running:
		if answered.err != nil {
			t.Fatalf("the request failed: %v", answered.err)
		}

		return answered
	case <-time.After(time.Minute):
		t.Fatal("the request never answered: a bound answers every request it bounds with a failure the " +
			"caller can read")

		return requestAnswer{}
	}
}

// the hourly retention pass runs under a context with a deadline.
func TestTheHourlyPruningRunsUnderAContextWithADeadline(t *testing.T) {
	scheduler := fakes.NewScheduler()

	pruned := &recordedPruning{}

	pruneHourly(scheduler, pruned)

	scheduler.RunDue(time.Hour)

	if pruned.calls != 1 {
		t.Fatalf("the retention pass ran %d times, want once", pruned.calls)
	}

	deadline, ok := pruned.ctx.Deadline()
	if !ok {
		t.Fatal("the retention pass ran under a context with no deadline, want the five-minute bound: a pass that cannot finish must stop, not hold the schedule forever")
	}

	if left := deadline.Sub(pruned.at); left <= 0 || left > 5*time.Minute {
		t.Errorf("the pass had %v of its bound when it started, want a positive bound no longer than 5m", left)
	}
}

// recordedPruning keeps the context the pass ran under.
type recordedPruning struct {
	calls int
	ctx   context.Context
	at    time.Time
}

func (pruned *recordedPruning) Handle(ctx context.Context, cmd application.PruneOldCallsCommand) (application.PruneOldCallsResult, error) {
	pruned.calls++
	pruned.ctx = ctx
	pruned.at = time.Now()

	return application.PruneOldCallsResult{}, nil
}
