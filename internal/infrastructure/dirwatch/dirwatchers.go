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

package dirwatch

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/dirwatch/parsers"
)

// minimumWatchDelay is how long a watched directory waits after a file stops changing before it
// reads it. A configured delay below it is raised to it, which is what the legacy raises it to:
// the recording tools write a call over several seconds, and a server that reads the file in the
// middle of that writes half a recording.
const minimumWatchDelay = 2000

// IngestSink hands one call a watched directory produced to the server's ingest path.
//
// The composition root adapts the ingest use case to it. The watcher is not told what became of
// the call, because the ingest path records what happened to every call it takes.
type IngestSink interface {
	IngestCall(ctx context.Context, call domain.Call) error
}

// Dirwatchers are the directory watchers the server keeps running, and the bracket a
// configuration save performs around them: stopped while the configuration is replaced, started
// again with what the configuration now says to watch.
type Dirwatchers struct {
	parsers *parsers.Parsers
	logs    ports.LogRepository
	sink    IngestSink
	clock   ports.Clock

	// running is what is watching right now, and cancel ends the work those watchers have in
	// flight. Both are guarded by mutex, because a configuration save and a file arriving in a
	// watched directory are not the same goroutine.
	mutex   sync.Mutex
	running []*watch
	cancel  context.CancelFunc
}

var _ ports.Dirwatchers = (*Dirwatchers)(nil)

// NewDirwatchers builds the watchers from the parsers that read what appears in a watched
// directory, the log the failures are reported to, the ingest path the calls are handed to, and
// the clock the log entries are stamped with.
func NewDirwatchers(parsers *parsers.Parsers, logs ports.LogRepository, sink IngestSink, clock ports.Clock) *Dirwatchers {
	return &Dirwatchers{
		parsers: parsers,
		logs:    logs,
		sink:    sink,
		clock:   clock,
	}
}

// Stop ends every running watcher and leaves nothing watching.
//
// The watchers are closed before the work they have in flight is cancelled: a call that is
// halfway through being ingested is allowed to finish rather than being cut off by a
// configuration save that happens to arrive at the same moment.
func (watchers *Dirwatchers) Stop(ctx context.Context) {
	watchers.mutex.Lock()

	running := watchers.running
	cancel := watchers.cancel

	watchers.running = nil
	watchers.cancel = nil

	watchers.mutex.Unlock()

	for _, w := range running {
		w.close()
	}

	if cancel != nil {
		cancel()
	}
}

// Start begins watching exactly the directories it is given, and nothing that was watching
// before.
//
// A watch that cannot be started is reported and costs the other watches nothing: a directory
// that no longer exists is one broken watch, not a server that stops watching the rest.
func (watchers *Dirwatchers) Start(ctx context.Context, watches domain.Dirwatches) {
	watchers.Stop(ctx)

	// The watching a directory watch does outlives the request that asked for it: a save
	// starts watchers that then run until the next save or the server stops, so their work
	// belongs to a context of its own and not to the caller's. The caller's context is used
	// for what Start itself reports.
	run, cancel := context.WithCancel(context.Background())

	watchers.mutex.Lock()
	watchers.cancel = cancel
	watchers.mutex.Unlock()

	for _, dirwatch := range watches {
		if !dirwatch.Watches() {
			continue
		}

		w := &watch{
			dirwatch: dirwatch,
			delay:    watchDelay(dirwatch),
			parsers:  watchers.parsers,
			sink:     watchers.sink,
			log:      watchers.log,
			dirs:     map[string]bool{},
			timers:   map[string]*time.Timer{},
		}

		if err := w.start(run); err != nil {
			watchers.log(ctx, domain.LogLevelError, fmt.Sprintf("the directory watchers could not start: %s", err.Error()))

			continue
		}

		// Said only for a directory that is there. A watch pointing at one that is not watches nothing, and
		// the pass over it already says so in a line that names the directory; claiming the server is
		// watching a directory it cannot read is a line an operator has to learn to distrust.
		//
		// It is said here, at the start, because this is the only place that knows the whole set the server
		// decided to watch. A recording that never becomes a call starts here.
		if isDirectory(dirwatch.Directory) {
			watchers.log(ctx, domain.LogLevelInfo, fmt.Sprintf("the server is watching %s", dirwatch.Directory))
		}

		watchers.mutex.Lock()
		watchers.running = append(watchers.running, w)
		watchers.mutex.Unlock()
	}
}

// log writes one line to the administrator's log, which is the only place a watcher ever
// reports anything. Every line a watcher writes is about the directory watchers, and the lines
// name the directory they are about because the failure's own text names only the syscall.
func (watchers *Dirwatchers) log(ctx context.Context, level, message string) {
	if err := watchers.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: watchers.clock.Now(),
		Level:    level,
		Source:   domain.LogSourceDirwatch,
		Message:  message,
	}); err != nil {
		// A log entry that cannot be written is the end of the reporting: a watcher has
		// nowhere else to say what went wrong.
		return
	}
}

// watchDelay is how long one watched directory waits after a file stops changing.
func watchDelay(dirwatch domain.Dirwatch) time.Duration {
	return time.Duration(math.Max(float64(dirwatch.Delay), minimumWatchDelay)) * time.Millisecond
}

// isDirectory is what the legacy asks of every path an event names, and asks the same way: a
// path that cannot be read is not a directory.
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.IsDir()
}

// watch is one watched directory: the notification subscription, the sub-directories it has
// registered, and the files it is waiting to settle before reading.
type watch struct {
	dirwatch domain.Dirwatch
	delay    time.Duration
	parsers  *parsers.Parsers
	sink     IngestSink
	log      func(ctx context.Context, level, message string)

	watcher *fsnotify.Watcher

	// mutex guards dirs and timers, and is held for the whole of a file being read and
	// ingested. Holding it that long is what the legacy does, and it is what keeps one
	// recording from being ingested twice when the tool that wrote it produces a create event
	// and then a write event for the same file.
	mutex  sync.Mutex
	dirs   map[string]bool
	timers map[string]*time.Timer
}

// start subscribes to the directory and begins the two loops that read it: the event loop, and
// the one pass over what is already there.
func (w *watch) start(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	w.watcher = watcher

	go w.loop(ctx)
	go w.readWhatIsAlreadyThere(ctx)

	return nil
}

// close ends the watching: the pending reads are dropped and the subscription is ended, which
// ends the event loop.
func (w *watch) close() {
	w.stopTimers()

	if w.watcher != nil {
		w.watcher.Close()
	}
}

// loop reads the notifications until the watch ends.
func (w *watch) loop(ctx context.Context) {
	defer func() {
		// A panic here ends this watch. The legacy logs it, stops every pending timer, and
		// starts the watch again — which fails, because the watcher is still registered, and
		// the failure is discarded, leaving the watch registered and deaf until the server
		// restarts. That is quirk Q43, and the outcome here is the same one: nothing more is
		// ingested from this directory.
		if recovered := recover(); recovered != nil {
			if err, ok := recovered.(error); ok {
				w.log(ctx, domain.LogLevelError, fmt.Sprintf("the watcher for %s stopped unexpectedly: %s", w.dirwatch.Directory, err.Error()))
			}
		}

		w.stopTimers()
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			w.handle(ctx, event)

		case err, ok := <-w.watcher.Errors:
			if ok {
				w.log(ctx, domain.LogLevelError, fmt.Sprintf("the watcher for %s stopped: %s", w.dirwatch.Directory, err.Error()))
			}

			// The legacy ends the loop on a notification failure rather than carrying on
			// with a subscription it no longer trusts.
			return
		}
	}
}

// handle acts on one notification.
func (w *watch) handle(ctx context.Context, event fsnotify.Event) {
	switch event.Op {
	case fsnotify.Create:
		if isDirectory(event.Name) {
			// A directory that appeared is registered along with everything below it, and
			// nothing in it is read: a recording that was already in a directory that was
			// already there is not a recording that just arrived.
			if err := w.registerBelow(ctx, event.Name); err != nil {
				w.log(ctx, domain.LogLevelError, fmt.Sprintf("the directory %s could not be watched: %s", event.Name, err.Error()))
			}

			return
		}

		w.arm(ctx, event.Name)

	case fsnotify.Remove:
		w.mutex.Lock()
		registered := w.dirs[event.Name]
		w.mutex.Unlock()

		if !registered {
			return
		}

		if err := w.watcher.Remove(event.Name); err != nil {
			w.log(ctx, domain.LogLevelError, fmt.Sprintf("the directory %s could not be unwatched: %s", event.Name, err.Error()))

			return
		}

		w.mutex.Lock()
		delete(w.dirs, event.Name)
		w.mutex.Unlock()

	case fsnotify.Write:
		w.arm(ctx, event.Name)
	}
}

// arm waits for one file to stop being written, and then reads it.
//
// Every new event for the same file restarts the wait, which is what keeps a recording written
// over several seconds from being read before it is complete.
func (w *watch) arm(ctx context.Context, path string) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if timer := w.timers[path]; timer != nil {
		timer.Stop()
	}

	w.timers[path] = time.AfterFunc(w.delay, func() {
		w.settle(ctx, path)
	})
}

// settle is what a settle timer runs when a file's window runs out.
//
// The guard is the point (CA-008.1, decision D7): this callback runs on the server's own
// clock, in a goroutine the runtime started and nobody can see, and the legacy server had
// no recovery here — a parser that panicked on a mask took the whole process down from a
// timer, which is how an uncompilable mask stored by the old server crashes a server run
// with it. The panic is caught and reported, naming the recording and the panic's own
// text, and the watch stays alive: the subscription and the event loop never touched what
// one file's read did.
func (w *watch) settle(ctx context.Context, path string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			text := fmt.Sprintf("%v", recovered)

			if err, ok := recovered.(error); ok {
				text = err.Error()
			}

			w.log(ctx, domain.LogLevelError, fmt.Sprintf("the recording %s could not be ingested: %s", path, text))
		}
	}()

	w.mutex.Lock()
	defer w.mutex.Unlock()

	delete(w.timers, path)

	// The file has to still be there. A recording tool that writes into a watched
	// directory and moves the finished recording elsewhere, or another application that
	// cleans the directory up, costs nothing and ingests nothing.
	if _, err := os.Stat(path); err != nil {
		return
	}

	w.ingest(ctx, path)
}

// readWhatIsAlreadyThere registers the directory's sub-directories and reads the recordings that
// were already in it when the server started watching.
//
// Both happen after one delay, so that a recording a tool is still writing when the server starts
// is not read half-written.
func (w *watch) readWhatIsAlreadyThere(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if err, ok := recovered.(error); ok {
				w.log(ctx, domain.LogLevelError, fmt.Sprintf("the directory %s could not be read at startup: %s", w.dirwatch.Directory, err.Error()))
			}
		}
	}()

	select {
	case <-time.After(w.delay):
	case <-ctx.Done():
		return
	}

	if err := fs.WalkDir(os.DirFS(w.dirwatch.Directory), ".", func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		path := filepath.Join(w.dirwatch.Directory, p)

		if isDirectory(path) {
			w.register(path)

			return nil
		}

		// A recording that was already there is read only by a watch that deletes after
		// ingesting. A watch that keeps its files never reads what was in the directory when
		// the server started, which is quirk Q49: the files are there, the watch is running,
		// and nothing arrives, because nothing changed.
		if w.dirwatch.DeleteAfter {
			w.ingest(ctx, path)
		}

		return nil
	}); err != nil {
		w.log(ctx, domain.LogLevelError, fmt.Sprintf("the directory %s could not be read: %s", w.dirwatch.Directory, err.Error()))
	}
}

// registerBelow registers a directory that appeared inside a watched directory, and every
// directory below it.
func (w *watch) registerBelow(ctx context.Context, directory string) error {
	return fs.WalkDir(os.DirFS(directory), ".", func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path := filepath.Join(directory, p); isDirectory(path) {
			w.register(path)
		}

		return nil
	})
}

// register subscribes to one directory, once.
func (w *watch) register(path string) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.dirs[path] {
		return
	}

	w.dirs[path] = true

	// A directory the subscription cannot be opened for is remembered as watched and is not
	// watched, and nothing is said about it. That is what the legacy does, and it is quirk
	// Q48: the recordings in that directory are never read and the administrator's log has no
	// line saying why.
	w.watcher.Add(path)
}

// ingest reads one file and hands what it found to the ingest path.
func (w *watch) ingest(ctx context.Context, path string) {
	extracted, recognized, err := w.parsers.Extract(ctx, w.dirwatch, path)

	if err != nil {
		w.log(ctx, domain.LogLevelWarn, fmt.Sprintf("the recording %s could not be ingested: %s", path, err.Error()))

		return
	}

	if !recognized {
		return
	}

	// What became of the call is the ingest path's own report, in the same log: the watcher
	// adds no second copy of it.
	w.sink.IngestCall(ctx, extracted.Call)

	if !w.dirwatch.DeleteAfter {
		return
	}

	// The files go whether the ingest path took the call or refused it: the legacy hands the
	// call over and deletes the recording before it knows what became of the call.
	for _, file := range extracted.Files {
		if err := os.Remove(file); err != nil {
			w.log(ctx, domain.LogLevelWarn, fmt.Sprintf("the recording %s could not be ingested: %s", path, err.Error()))

			return
		}
	}
}

// stopTimers drops the reads the watch is waiting to make.
func (w *watch) stopTimers() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	for path, timer := range w.timers {
		timer.Stop()
		delete(w.timers, path)
	}
}
