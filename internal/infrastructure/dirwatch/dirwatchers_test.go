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

package dirwatch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/dirwatch/parsers"
)

// The watched-directory tests are integration tests over real directories and real filesystem
// notifications; they wait in parallel for the two-second floor every watch has.
const (
	settleWait = 3 * time.Second
	liveWait   = 6 * time.Second
)

var (
	// recordedAt is the moment the server notices things, so that a call stamped with the
	// moment it was noticed has a timestamp a test can name.
	recordedAt = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	// recording is the audio of a watched file: long enough to be a recording rather than a
	// container header.
	recording = []byte(strings.Repeat("a recording long enough to be a recording", 4))
)

// ingestRecorder is the ingest path a watched directory hands its calls to.
type ingestRecorder struct {
	// arrived carries the calls the watchers handed over, in the order they arrived.
	arrived chan domain.Call

	// rejects makes the ingest path refuse every call, which is how a call the server will not
	// take is told about.
	rejects error

	// panics makes the ingest path panic, which is how the guard that catches a panic in the
	// first pass over a watched directory is exercised. The panic is a value the guard can name,
	// because that is what a panic in this server carries: the error the code was holding.
	panics error

	mutex    sync.Mutex
	recorded []domain.Call
}

func newIngestRecorder() *ingestRecorder {
	return &ingestRecorder{arrived: make(chan domain.Call, 32)}
}

var _ IngestSink = (*ingestRecorder)(nil)

func (recorder *ingestRecorder) IngestCall(ctx context.Context, call domain.Call) error {
	if recorder.rejects != nil {
		return recorder.rejects
	}

	if recorder.panics != nil {
		panic(recorder.panics)
	}

	recorder.mutex.Lock()
	recorder.recorded = append(recorder.recorded, call)
	recorder.mutex.Unlock()

	recorder.arrived <- call

	return nil
}

// next waits for one call to arrive, and fails the test if none does.
func (recorder *ingestRecorder) next(t *testing.T, within time.Duration) domain.Call {
	t.Helper()

	select {
	case call := <-recorder.arrived:
		return call

	case <-time.After(within):
		t.Fatalf("no recording was ingested within %v", within)

		return domain.Call{}
	}
}

// nothingArrivesWithin waits out the window and fails the test if a call arrives in it.
func (recorder *ingestRecorder) nothingArrivesWithin(t *testing.T, within time.Duration) {
	t.Helper()

	select {
	case call := <-recorder.arrived:
		t.Fatalf("the recording %q was ingested, want nothing ingested", call.AudioFilename)

	case <-time.After(within):
	}
}

// handed reports every call the watchers handed over.
func (recorder *ingestRecorder) handed() []domain.Call {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()

	return append([]domain.Call{}, recorder.recorded...)
}

// watchedDirectory is one running watch, the directory it watches, and the two places it reports
// to.
type watchedDirectory struct {
	watchers  *Dirwatchers
	directory string
	calls     *ingestRecorder
	logs      *fakes.Logs
}

// watching starts one watch over a fresh directory, configured by configure.
func watching(t *testing.T, configure func(domain.Dirwatch) domain.Dirwatch) *watchedDirectory {
	t.Helper()

	return watchingOver(t, t.TempDir(), configure)
}

// watchingOver starts one watch over a directory the test already has.
func watchingOver(t *testing.T, directory string, configure func(domain.Dirwatch) domain.Dirwatch) *watchedDirectory {
	t.Helper()

	return watchingOverWith(t, directory, configure, newIngestRecorder())
}

// watchingOverWith starts one watch over a directory the test already has, handing it a ingest path the
// test has prepared. A test that needs the ingest path to panic uses this.
func watchingOverWith(t *testing.T, directory string, configure func(domain.Dirwatch) domain.Dirwatch, calls *ingestRecorder) *watchedDirectory {
	t.Helper()

	return watchingOverWithSink(t, directory, configure, calls)
}

// watchingOverWithSink starts one watch over a directory the test already has, handing it
// an ingest path the test built — the blocking sink, for the tests that watch a watch
// working through a slow ingest.
func watchingOverWithSink(t *testing.T, directory string, configure func(domain.Dirwatch) domain.Dirwatch, sink IngestSink) *watchedDirectory {
	t.Helper()

	watch := domain.NewDirwatch()
	watch.Directory = directory
	watch.Mask = "#SYS-#TG-#UNIT-#DATE-#TIME"

	if configure != nil {
		watch = configure(watch)
	}

	logs := &fakes.Logs{}

	watchers := NewDirwatchers(
		parsers.New(&fakes.Systems{Stored: recordedSystems()}, fakes.NewClock(recordedAt)),
		logs,
		sink,
		fakes.NewClock(recordedAt),
	)

	watchers.Start(context.Background(), domain.Dirwatches{watch})

	dir := &watchedDirectory{watchers: watchers, directory: directory, logs: logs}

	// The recorder is the watched directory's own report when the test's sink is one:
	// the tests that dial the ingest path directly read it through calls.
	if recorder, ok := sink.(*ingestRecorder); ok {
		dir.calls = recorder
	}

	return dir
}

// recordedSystems is the configuration the parsers read the names in a filename against.
func recordedSystems() domain.Systems {
	return domain.Systems{
		{ID: 1, Label: "Grand Metro", SystemRef: 10001},
		{ID: 2, Label: "Harbour", SystemRef: 10002},
	}
}

// putRecording writes a recording into a directory and reports its path.
func putRecording(t *testing.T, directory, name string) string {
	t.Helper()

	path := filepath.Join(directory, name)

	if err := os.WriteFile(path, recording, 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}

	return path
}

// recordedFile is the name the fixture mask reads a call out of.
const recordedFile = "10001-1514000-6789-20261006-120000.wav"

// logLines reports what the watch wrote to the administrator's log.
func (watched *watchedDirectory) logLines() []string {
	lines := []string{}

	for _, entry := range watched.logs.Recorded() {
		lines = append(lines, entry.Level+": "+entry.Message)
	}

	return lines
}

// reportedAfterStarting is what the watch reported after it started, including the entry the
// start itself writes, so a test counting one problem line knows which line is its own.
func (watched *watchedDirectory) reportedAfterStarting() []string {
	lines := []string{}

	for _, entry := range watched.logs.Recorded() {
		if strings.HasPrefix(entry.Message, "the server is watching ") {
			continue
		}

		lines = append(lines, entry.Level+": "+entry.Message)
	}

	return lines
}

// loggedAs reports whether the watch wrote an entry naming that part of the server and saying the
// fragment. The source is part of the assertion: a watcher that reports what went wrong and is filed
// under nothing is the state this feature exists to remove.
func (watched *watchedDirectory) loggedAs(source domain.LogSource, fragment string) bool {
	for _, entry := range watched.logs.Recorded() {
		if entry.Source == source && strings.Contains(entry.Message, fragment) {
			return true
		}
	}

	return false
}

// TestARecordingAlreadyInTheWatchedDirectoryIsIngested is the shortest path a watch has: the
// recording was already in the directory when the server started watching it, and the first pass
// over the directory reads it.
func TestARecordingAlreadyInTheWatchedDirectoryIsIngested(t *testing.T) {
	t.Parallel()

	watched := watching(t, nil)

	putRecording(t, watched.directory, recordedFile)

	call := watched.calls.next(t, settleWait)

	if call.AudioFilename != recordedFile {
		t.Errorf("the ingested call is named %q, want the recording the watch read", call.AudioFilename)
	}

	if call.AudioMime != "audio/wav" {
		t.Errorf("the ingested call's audio is %q, want the media type the recording's extension names", call.AudioMime)
	}

	if len(call.Audio) != len(recording) {
		t.Errorf("the ingested call carries %d bytes of audio, want the whole recording", len(call.Audio))
	}

	// The watch hands over what the parser read, before anything resolves it: the system and
	// the talkgroup arrive as the references the filename names.
	if call.Meta.SystemRef != 10001 {
		t.Errorf("the ingested call claims system reference %d, want the one the mask read out of the name", call.Meta.SystemRef)
	}

	if call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the ingested call claims talkgroup reference %d, want the one the mask read out of the name", call.Meta.TalkgroupRef)
	}

	if len(call.Units) != 1 || call.Units[0].UnitRef != 6789 {
		t.Errorf("the ingested call's units are %#v, want the radio the mask read out of the name", call.Units)
	}
}

// TestARecordingIsIngestedOnceEvenThoughWritingItIsTwoEvents: writing one recording is a
// create event and a write event for the same file; the watch ingests it once.
func TestARecordingIsIngestedOnceEvenThoughWritingItIsTwoEvents(t *testing.T) {
	t.Parallel()

	watched := watching(t, nil)

	// The directory is only watched once the first pass over it has run.
	time.Sleep(settleWait)

	putRecording(t, watched.directory, recordedFile)

	call := watched.calls.next(t, liveWait)

	if call.AudioFilename != recordedFile {
		t.Errorf("the ingested call is named %q, want the recording that appeared in the watched directory", call.AudioFilename)
	}

	watched.calls.nothingArrivesWithin(t, settleWait)

	if taken := len(watched.calls.handed()); taken != 1 {
		t.Errorf("the watch ingested %d calls from one recording, want one", taken)
	}
}

// TestARecordingIsReadOnlyOnceItStopsBeingWritten is the two-second floor doing its job: a
// recording written over several seconds is re-waited-for on every write, and read once at the
// end.
func TestARecordingIsReadOnlyOnceItStopsBeingWritten(t *testing.T) {
	t.Parallel()

	watched := watching(t, nil)

	time.Sleep(settleWait)

	path := putRecording(t, watched.directory, recordedFile)

	// The recording is still being written when the first wait would have ended.
	time.Sleep(time.Second)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the recording disappeared while it was being written: %v", err)
	}

	if err := os.WriteFile(path, append(recording, []byte(" and the rest of the recording")...), 0o644); err != nil {
		t.Fatalf("writing the rest of the recording: %v", err)
	}

	call := watched.calls.next(t, liveWait)

	if len(call.Audio) != len(recording)+len(" and the rest of the recording") {
		t.Errorf("the ingested call carries %d bytes of audio, want the finished recording", len(call.Audio))
	}
}

// TestARecordingThatDisappearsBeforeItIsReadIsNotIngested is a watch pointed at a directory
// another application uses as a hand-off: the recording appears, and is gone again before the
// watch reads it. Nothing is ingested and nothing is reported.
func TestARecordingThatDisappearsBeforeItIsReadIsNotIngested(t *testing.T) {
	t.Parallel()

	watched := watching(t, nil)

	time.Sleep(settleWait)

	path := putRecording(t, watched.directory, recordedFile)

	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the recording: %v", err)
	}

	watched.calls.nothingArrivesWithin(t, liveWait)

	if lines := watched.reportedAfterStarting(); len(lines) != 0 {
		t.Errorf("the watch reported %v, want a recording that moved elsewhere to cost nothing", lines)
	}
}

// TestAWatchDeletesTheRecordingItIngested is the delete-after-ingesting default: the watched
// directory is a hand-off, and the server is the thing that keeps the recording.
func TestAWatchDeletesTheRecordingItIngested(t *testing.T) {
	t.Parallel()

	watched := watching(t, nil)

	path := putRecording(t, watched.directory, recordedFile)

	watched.calls.next(t, settleWait)

	wantGone(t, path)
}

// wantGone wants a file the watch must delete gone, and says what it found when the file is
// still there: the call is handed over before the read file is deleted, the legacy order.
func wantGone(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(settleWait)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	_, err := os.Stat(path)

	t.Errorf("%s is still there (%v), want it deleted", filepath.Base(path), err)
}

// TestARecordingAlreadyInTheDirectoryIsIngestedOnlyByAWatchThatDeletesAfterIngesting is a preserved quirk: the first pass over a watched directory reads the recordings that were already there only
// when the watch is one that deletes after ingesting. A watch that keeps its files starts, the
// files are there, and nothing arrives — because nothing changed.
func TestARecordingAlreadyInTheDirectoryIsIngestedOnlyByAWatchThatDeletesAfterIngesting(t *testing.T) {
	t.Parallel()

	deleting := t.TempDir()
	keeping := t.TempDir()

	putRecording(t, deleting, recordedFile)
	putRecording(t, keeping, recordedFile)

	calls := newIngestRecorder()

	watchers := NewDirwatchers(
		parsers.New(&fakes.Systems{Stored: recordedSystems()}, fakes.NewClock(recordedAt)),
		&fakes.Logs{},
		calls,
		fakes.NewClock(recordedAt),
	)

	keep := domain.NewDirwatch()
	keep.Directory = keeping
	keep.Mask = "#SYS-#TG-#UNIT-#DATE-#TIME"
	keep.DeleteAfter = false

	delete := domain.NewDirwatch()
	delete.Directory = deleting
	delete.Mask = "#SYS-#TG-#UNIT-#DATE-#TIME"

	watchers.Start(context.Background(), domain.Dirwatches{delete, keep})

	call := calls.next(t, settleWait)

	if call.AudioFilename != recordedFile {
		t.Errorf("the ingested call is named %q, want the recording in the directory that deletes after ingesting", call.AudioFilename)
	}

	calls.nothingArrivesWithin(t, settleWait)

	if taken := calls.handed(); len(taken) != 1 {
		t.Errorf("%d recordings were ingested, want the one in the directory that deletes after ingesting", len(taken))
	}

	if _, err := os.Stat(filepath.Join(keeping, recordedFile)); err != nil {
		t.Errorf("the recording in the directory that keeps its files is gone (%v), want it left there unread", err)
	}
}

// TestAWatchThatKeepsItsRecordingsLeavesTheOneItIngestedInPlace is the other half of the same
// setting: a watch that keeps its files does ingest what arrives while it is watching, and leaves
// it where it found it.
func TestAWatchThatKeepsItsRecordingsLeavesTheOneItIngestedInPlace(t *testing.T) {
	t.Parallel()

	watched := watching(t, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.DeleteAfter = false

		return watch
	})

	time.Sleep(settleWait)

	path := putRecording(t, watched.directory, recordedFile)

	watched.calls.next(t, liveWait)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the recording the watch ingested is gone (%v), want it left where it was found", err)
	}
}

// TestATrunkRecorderPairIsIngestedAndBothFilesGo is the one call a watch is built from two files:
// the sidecar the directory notified about, and the recording beside it.
func TestATrunkRecorderPairIsIngestedAndBothFilesGo(t *testing.T) {
	t.Parallel()

	sidecar := filepath.Join(t.TempDir(), "call.json")
	audio := strings.TrimSuffix(sidecar, ".json") + ".wav"

	if err := os.WriteFile(audio, recording, 0o644); err != nil {
		t.Fatalf("writing the recording: %v", err)
	}

	if err := os.WriteFile(sidecar, []byte(`{
		"short_name": "Grand Metro",
		"talkgroup": 1514000,
		"talkgroup_description": "Dispatch 1",
		"timestamp": 1791288000000
	}`), 0o644); err != nil {
		t.Fatalf("writing the sidecar: %v", err)
	}

	watched := watchingOver(t, filepath.Dir(sidecar), func(watch domain.Dirwatch) domain.Dirwatch {
		watch.Kind = domain.DirwatchTypeTrunkRecorder
		watch.Mask = ""

		return watch
	})

	call := watched.calls.next(t, settleWait)

	if call.AudioFilename != "call.wav" {
		t.Errorf("the ingested call is named %q, want the recording beside the sidecar", call.AudioFilename)
	}

	if call.Meta.TalkgroupRef != 1514000 {
		t.Errorf("the ingested call claims talkgroup reference %d, want the one the sidecar names", call.Meta.TalkgroupRef)
	}

	for _, path := range []string{sidecar, audio} {
		wantGone(t, path)
	}
}

// TestARecordingTheWatchCannotReadIsReportedToTheAdministratorLog is the only way an
// administrator learns a watch is configured for the wrong shape of recording.
func TestARecordingTheWatchCannotReadIsReportedToTheAdministratorLog(t *testing.T) {
	t.Parallel()

	watched := watching(t, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.Mask = "#SYS-#TG"

		return watch
	})

	path := putRecording(t, watched.directory, "not-a-recording.wav")

	watched.calls.nothingArrivesWithin(t, settleWait)

	lines := watched.reportedAfterStarting()

	if len(lines) != 1 {
		t.Fatalf("the watch wrote %v to the administrator's log, want one line saying what it could not read", lines)
	}

	if !strings.Contains(lines[0], "the recording "+path+" could not be ingested:") {
		t.Errorf("the watch wrote %q, want the ingest failure naming the file it could not read", lines[0])
	}

	if !watched.loggedAs(domain.LogSourceDirwatch, "could not be ingested") {
		t.Errorf("the watch wrote %v, want the line filed under the directory watchers", lines)
	}

	if !strings.Contains(lines[0], "warn:") {
		t.Errorf("the watch wrote %q at the wrong severity, want a warning", lines[0])
	}
}

// TestARecordingTheIngestPathRefusesStillCostsTheRecording is the order the legacy works in: the
// call is handed over and the recording is deleted before anything knows what became of the call.
func TestARecordingTheIngestPathRefusesStillCostsTheRecording(t *testing.T) {
	t.Parallel()

	watched := watching(t, nil)

	watched.calls.rejects = errors.New("the call names a system this server does not have")

	path := putRecording(t, watched.directory, recordedFile)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the recording is not there before the watch reads it: %v", err)
	}

	watched.calls.nothingArrivesWithin(t, settleWait)

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the recording a refused ingest path was handed is still there (%v), want it deleted as the legacy deletes it", err)
	}

	if lines := watched.reportedAfterStarting(); len(lines) != 0 {
		t.Errorf("the watch wrote %v to the administrator's log, want the ingest path's own report and no second copy of it", lines)
	}
}

// TestARecordingWhoseMaskNamesOnlyATalkgroupLabelIsLeftWhereItIs is the watch's half of a
// mask that reads the talkgroup out of the recording's name and never a number: 6.6.3 refused
// the call it read out of such a name, and the recording stayed where the recording tool wrote
// it for the operator to see. A call the watch itself refused never reaches the ingest path, so
// the watch reports it once in its own words.
func TestARecordingWhoseMaskNamesOnlyATalkgroupLabelIsLeftWhereItIs(t *testing.T) {
	t.Parallel()

	watched := watching(t, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.Mask = "#TGLBL"
		watch.SystemID = 1

		return watch
	})

	path := putRecording(t, watched.directory, "Dispatch 1.wav")

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the recording is not there before the watch reads it: %v", err)
	}

	watched.calls.nothingArrivesWithin(t, settleWait)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the recording whose mask named no talkgroup number is gone (%v), want it left where the recording tool wrote it", err)
	}

	lines := watched.reportedAfterStarting()

	if len(lines) != 1 {
		t.Fatalf("the watch wrote %v to the administrator's log, want one line saying the recording names no talkgroup", lines)
	}

	if !strings.Contains(lines[0], "the recording "+path+" could not be ingested: no talkgroup") {
		t.Errorf("the watch wrote %q, want the refusal naming the recording and saying it names no talkgroup", lines[0])
	}

	if !watched.loggedAs(domain.LogSourceDirwatch, "no talkgroup") {
		t.Errorf("the watch wrote %v, want the line filed under the directory watchers", lines)
	}

	if !strings.Contains(lines[0], "warn:") {
		t.Errorf("the watch wrote %q at the wrong severity, want a warning", lines[0])
	}
}

// TestADirectoryThatAppearsInsideAWatchedDirectoryIsWatchedToo is how the recording tools that
// write one directory per day work: the watch is pointed at the parent, and the day's directory
// appears while the server is running.
func TestADirectoryThatAppearsInsideAWatchedDirectoryIsWatchedToo(t *testing.T) {
	t.Parallel()

	watched := watching(t, nil)

	time.Sleep(settleWait)

	day := filepath.Join(watched.directory, "20261006")

	if err := os.Mkdir(day, 0o755); err != nil {
		t.Fatalf("creating the day's directory: %v", err)
	}

	// The directory has to be subscribed to before anything is written into it, and that
	// happens when the notification for its arrival is processed. A recording written into a
	// directory that appeared at the same moment is missed, which is a preserved quirk.
	time.Sleep(500 * time.Millisecond)

	putRecording(t, day, recordedFile)

	call := watched.calls.next(t, liveWait)

	if call.AudioFilename != recordedFile {
		t.Errorf("the ingested call is named %q, want the recording in the directory that appeared while the server was watching", call.AudioFilename)
	}
}

// TestARecordingInASubdirectoryIsIngestedByTheFirstPassOverTheWatchedDirectory covers the
// recording tools that already have their day directories when the server starts.
func TestARecordingInASubdirectoryIsIngestedByTheFirstPassOverTheWatchedDirectory(t *testing.T) {
	t.Parallel()

	watched := watching(t, nil)

	day := filepath.Join(watched.directory, "20261006")

	if err := os.Mkdir(day, 0o755); err != nil {
		t.Fatalf("creating the day's directory: %v", err)
	}

	putRecording(t, day, recordedFile)

	call := watched.calls.next(t, settleWait)

	if call.AudioFilename != recordedFile {
		t.Errorf("the ingested call is named %q, want the recording the first pass found below the watched directory", call.AudioFilename)
	}
}

// TestAWatchedDirectoryThatIsNotThereIsReportedAndWatchesNothing is a watch left pointing at a
// directory that has been moved or removed: the failure is one error line, and the server carries
// on watching everything else.
func TestAWatchedDirectoryThatIsNotThereIsReportedAndWatchesNothing(t *testing.T) {
	t.Parallel()

	gone := t.TempDir()

	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("removing the watched directory: %v", err)
	}

	watched := watchingOver(t, gone, nil)

	watched.calls.nothingArrivesWithin(t, settleWait)

	lines := watched.logLines()

	if len(lines) != 1 {
		t.Fatalf("the watch wrote %v to the administrator's log, want one line saying the directory is not there", lines)
	}

	if !strings.Contains(lines[0], "the directory "+gone+" could not be read:") {
		t.Errorf("the watch wrote %q, want the failure of the pass over the directory, naming the directory it was reading", lines[0])
	}

	if !watched.loggedAs(domain.LogSourceDirwatch, "could not be read") {
		t.Errorf("the watch wrote %v, want the line filed under the directory watchers", lines)
	}

	// And not a line saying the server is watching it. A watch pointing at a directory that is not there
	// watches nothing, and the one line that is written already says so.
	if watched.loggedAs(domain.LogSourceDirwatch, "the server is watching") {
		t.Errorf("the watch wrote %v, want no claim to be watching a directory that is not there", lines)
	}
}

// TestADirectoryThatAppearsButCannotBeWatchedIsReportedAndTheWatchCarriesOn is the site where a
// directory shows up inside a watched one and the server cannot register it: the watch keeps running and
// keeps ingesting, so the line must not claim a watcher stopped.
func TestADirectoryThatAppearsButCannotBeWatchedIsReportedAndTheWatchCarriesOn(t *testing.T) {
	t.Parallel()

	watched := watching(t, nil)

	time.Sleep(settleWait)

	unreadable := filepath.Join(watched.directory, "20261006")

	if err := os.Mkdir(unreadable, 0o000); err != nil {
		t.Fatalf("creating a directory the server cannot read: %v", err)
	}

	t.Cleanup(func() {
		// The directory has to be readable again for the temporary tree to be removable.
		_ = os.Chmod(unreadable, 0o755)
	})

	time.Sleep(500 * time.Millisecond)

	lines := watched.reportedAfterStarting()

	if len(lines) != 1 {
		t.Fatalf("the watch wrote %v to the administrator's log, want one line saying the directory could not be watched", lines)
	}

	if !strings.Contains(lines[0], "the directory "+unreadable+" could not be watched:") {
		t.Errorf("the watch wrote %q, want the directory it could not watch, named", lines[0])
	}

	if !watched.loggedAs(domain.LogSourceDirwatch, "could not be watched") {
		t.Errorf("the watch wrote %v, want the line filed under the directory watchers", lines)
	}

	// The watch is still watching: a recording written into the directory it does watch arrives.
	putRecording(t, watched.directory, recordedFile)

	if call := watched.calls.next(t, liveWait); call.AudioFilename != recordedFile {
		t.Errorf("the ingested call is named %q, want the recording written after the directory that could not be watched", call.AudioFilename)
	}
}

// TestAPanicInTheFirstPassOverAWatchedDirectoryIsReportedNamesTheDirectory covers the guard that keeps a
// panic in the startup pass from taking the server down. The legacy stored a bare error with nothing
// saying which directory the pass was reading.
func TestAPanicInTheFirstPassOverAWatchedDirectoryIsReportedNamesTheDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	putRecording(t, directory, recordedFile)

	sink := newIngestRecorder()
	sink.panics = errors.New("the ingest path was not prepared for this recording")

	watched := watchingOverWith(t, directory, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.DeleteAfter = true

		return watch
	}, sink)

	watched.calls.nothingArrivesWithin(t, settleWait)

	lines := watched.reportedAfterStarting()

	if len(lines) != 1 {
		t.Fatalf("the watch wrote %v to the administrator's log, want one line saying the startup pass was not completed", lines)
	}

	if !strings.Contains(lines[0], "the directory "+directory+" could not be read at startup: the ingest path was not prepared for this recording") {
		t.Errorf("the watch wrote %q, want the directory the pass was reading and the panic's own text", lines[0])
	}

	if !watched.loggedAs(domain.LogSourceDirwatch, "could not be read at startup") {
		t.Errorf("the watch wrote %v, want the line filed under the directory watchers", lines)
	}
}

// TestStoppingTheWatchersEndsTheWatching is the half of the bracket a configuration save performs:
// a save that stops the watchers and then fails to start them again leaves the server watching
// nothing, and the recordings that arrive while nothing is watching are not read.
func TestStoppingTheWatchersEndsTheWatching(t *testing.T) {
	t.Parallel()

	watched := watching(t, nil)

	path := putRecording(t, watched.directory, recordedFile)

	watched.watchers.Stop(context.Background())

	if running := watched.watchers.running; len(running) != 0 {
		t.Errorf("%d watchers are still registered after stopping them, want none", len(running))
	}

	watched.calls.nothingArrivesWithin(t, liveWait)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the recording a stopped watch left behind is gone (%v), want it untouched", err)
	}
}

// TestAWatchThatStartedNamesTheDirectoryItIsWatching is the fact an operator otherwise has to guess: the
// server took a directory watch, and which directory it is now reading is not written down anywhere they can
// read. A recording that never becomes a call starts here.
func TestAWatchThatStartedNamesTheDirectoryItIsWatching(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	watched := watchingOver(t, directory, nil)

	lines := watched.logLines()

	if len(lines) != 1 {
		t.Fatalf("the watch wrote %v to the administrator's log, want one line naming the directory it is "+
			"watching", lines)
	}

	const want = "info: the server is watching "

	if !strings.HasPrefix(lines[0], want) {
		t.Errorf("the watch wrote %q, want it to name the directory it started watching", lines[0])
	}

	if !strings.Contains(lines[0], directory) {
		t.Errorf("the watch wrote %q, want the line to name %s", lines[0], directory)
	}

	if !watched.loggedAs(domain.LogSourceDirwatch, "the server is watching "+directory) {
		t.Errorf("the watch wrote %v, want the line filed under the directory watchers", lines)
	}

	stamped := watched.logs.Recorded()[0]

	if got := stamped.DateTime; !got.Equal(recordedAt) {
		t.Errorf("the entry is stamped %v, want the instant the server's clock holds", got)
	}
}

// TestStartingTheWatchersAgainReplacesWhatIsWatched is the other half of the bracket: a start
// watches exactly what it is given, and a directory it is not given stops being watched.
func TestStartingTheWatchersAgainReplacesWhatIsWatched(t *testing.T) {
	t.Parallel()

	first := t.TempDir()
	second := t.TempDir()

	putRecording(t, first, recordedFile)
	putRecording(t, second, recordedFile)

	calls := newIngestRecorder()

	watchers := NewDirwatchers(
		parsers.New(&fakes.Systems{Stored: recordedSystems()}, fakes.NewClock(recordedAt)),
		&fakes.Logs{},
		calls,
		fakes.NewClock(recordedAt),
	)

	watch := domain.NewDirwatch()
	watch.Directory = first
	watch.Mask = "#SYS-#TG-#UNIT-#DATE-#TIME"

	watchers.Start(context.Background(), domain.Dirwatches{watch})

	watch.Directory = second

	watchers.Start(context.Background(), domain.Dirwatches{watch})

	call := calls.next(t, settleWait)

	if got := call.AudioFilename; got != recordedFile {
		t.Errorf("the ingested call is named %q, want the recording in the directory the second start named", got)
	}

	calls.nothingArrivesWithin(t, settleWait)

	if taken := calls.handed(); len(taken) != 1 {
		t.Errorf("%d recordings were ingested, want the one in the directory that is still watched", len(taken))
	}

	if _, err := os.Stat(filepath.Join(first, recordedFile)); err != nil {
		t.Errorf("the recording in the directory that stopped being watched is gone (%v), want it left alone", err)
	}
}

// TestADisabledWatchOrAWatchWithNoDirectoryWatchesNothing is what the two settings that mean "do
// not watch anything" cost: no subscription is opened for them at all.
func TestADisabledWatchOrAWatchWithNoDirectoryWatchesNothing(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	disabled := domain.NewDirwatch()
	disabled.Directory = directory
	disabled.Disabled = true

	nowhere := domain.NewDirwatch()
	nowhere.Directory = ""

	logs := &fakes.Logs{}

	watchers := NewDirwatchers(
		parsers.New(&fakes.Systems{Stored: recordedSystems()}, fakes.NewClock(recordedAt)),
		logs,
		newIngestRecorder(),
		fakes.NewClock(recordedAt),
	)

	watchers.Start(context.Background(), domain.Dirwatches{disabled, nowhere})

	if running := watchers.running; len(running) != 0 {
		t.Errorf("%d watchers were started for a disabled watch and a watch with no directory, want none", len(running))
	}

	// Nothing was watched, so nothing is claimed. An entry naming a directory the server is not reading is
	// the kind of line an operator has to learn to distrust.
	if wrote := logs.Recorded(); len(wrote) != 0 {
		t.Errorf("the watchers wrote %v to the log for two watches that watch nothing, want nothing", wrote)
	}
}

// TestTheWatchDelayNeverFallsBelowTheTwoSecondFloor is the clamp every watched directory gets,
// whatever the administrator sets: the recording tools write a call over several seconds, and a
// server that reads in the middle of that stores half a recording.
func TestTheWatchDelayNeverFallsBelowTheTwoSecondFloor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		delay uint
		want  time.Duration
	}{
		{name: "no delay at all", delay: 0, want: 2 * time.Second},
		{name: "a delay below the floor", delay: 500, want: 2 * time.Second},
		{name: "the floor", delay: 2000, want: 2 * time.Second},
		{name: "a delay above the floor", delay: 15000, want: 15 * time.Second},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			watch := domain.NewDirwatch()
			watch.Delay = c.delay

			if got := watchDelay(watch); got != c.want {
				t.Errorf("the watch delay is %v, want %v", got, c.want)
			}
		})
	}
}

// TestAWatchDelayFromTheConfigurationScreenReachesTheWatch is the settle time an operator
// names in the configuration screen reaching the watcher that has to wait it. The screen,
// the published guide, and the floor test above all read the number as milliseconds, and
// the guide promises the clamp only bites past a day. The save path clamps at 1440 of the
// same unit, so a watch configured to settle for fifteen seconds settles for the two-second
// floor instead, and the half-written recording the longer wait was set for is read anyway.
func TestAWatchDelayFromTheConfigurationScreenReachesTheWatch(t *testing.T) {
	t.Parallel()

	dirwatch := domain.NewDirwatch().ApplyMap(map[string]any{
		"directory": t.TempDir(),
		"delay":     float64(15000),
	})

	if got := watchDelay(dirwatch); got != 15*time.Second {
		t.Errorf("the watch settles for %v, want the 15000 ms its configuration named", got)
	}
}

// TestAPanicInAFilesSettledReadIsReportedAndTheWatchCarriesOn: the settle timer's read had no
// guard; a parser or ingest path panicking there took the whole process down. The panic is
// caught and reported, naming the recording and the panic's own text, and the watch keeps
// ingesting later recordings. Only the settle path reads for a watch that keeps its files.
func TestAPanicInAFilesSettledReadIsReportedAndTheWatchCarriesOn(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	sink := newIngestRecorder()

	watched := watchingOverWith(t, directory, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.DeleteAfter = false

		return watch
	}, sink)

	putRecordingUntilIngested(t, watched, "10001-1514000-1111-20261006-110000.wav")

	// From here the ingest path panics at the first call it is handed. Today the panic is
	// unrecovered in the settle timer's goroutine and the test process dies here — that
	// death is the failure this test is written to prevent.
	sink.panics = errors.New("the ingest path was not prepared for this recording")

	putRecording(t, directory, recordedFile)

	watched.calls.nothingArrivesWithin(t, settleWait)

	lines := watched.reportedAfterStarting()

	if len(lines) != 1 {
		t.Fatalf("the watch wrote %v to the administrator's log, want one line saying the settled read was not completed", lines)
	}

	if !strings.Contains(lines[0], recordedFile) || !strings.Contains(lines[0], "the ingest path was not prepared for this recording") {
		t.Errorf("the watch wrote %q, want the recording it was reading and the panic's own text", lines[0])
	}

	if !watched.loggedAs(domain.LogSourceDirwatch, "could not be ingested") {
		t.Errorf("the watch wrote %v, want the line filed under the directory watchers", lines)
	}

	// The watch is alive: the panic was one file's read, not the watch's end.
	sink.panics = nil

	putRecording(t, directory, "10002-1514001-6790-20261006-120100.wav")

	call := watched.calls.next(t, settleWait+settleWait)

	if call.AudioFilename != "10002-1514001-6790-20261006-120100.wav" {
		t.Errorf("the watch ingested %q after the panic, want the recording that arrived after it", call.AudioFilename)
	}
}

// putRecordingUntilIngested keeps presenting a probe recording until the settle path reads
// it: a file written before the startup pass registered the directory is named by no event.
func putRecordingUntilIngested(t *testing.T, watched *watchedDirectory, name string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		putRecording(t, watched.directory, name)

		select {
		case <-watched.calls.arrived:
			return

		case <-time.After(settleWait):
		}
	}

	t.Fatalf("the watch never ingested the probe %q, want the settle path to read it", name)

	return
}

// blockingSink is an ingest path that tells the test when each read starts and ends, and
// blocks the calls it is told to block on a gate the test closes — the shape of an ingest
// that takes a long time, which is what the watch must keep working through.
type blockingSink struct {
	events chan string

	// gate is closed by the test to release the blocked reads.
	gate chan struct{}

	mutex  sync.Mutex
	blocks int
}

func newBlockingSink() *blockingSink {
	return &blockingSink{
		events: make(chan string, 32),
		gate:   make(chan struct{}),
	}
}

var _ IngestSink = (*blockingSink)(nil)

// blockNext makes the next call the sink is handed sit on the gate.
func (sink *blockingSink) blockNext() {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()

	sink.blocks++
}

func (sink *blockingSink) IngestCall(ctx context.Context, call domain.Call) error {
	sink.events <- "start:" + call.AudioFilename

	sink.mutex.Lock()
	blocks := sink.blocks > 0
	if blocks {
		sink.blocks--
	}
	sink.mutex.Unlock()

	if blocks {
		<-sink.gate
	}

	sink.events <- "end:" + call.AudioFilename

	return nil
}

// release ends the blocking: every read the gate was holding carries on.
func (sink *blockingSink) release() {
	close(sink.gate)
}

// nextEvent waits for one start/end event, and fails the test if none comes.
func (sink *blockingSink) nextEvent(t *testing.T, within time.Duration) string {
	t.Helper()

	select {
	case event := <-sink.events:
		return event

	case <-time.After(within):
		t.Fatalf("no ingest event within %v", within)

		return ""
	}
}

// noEventWithin insists on silence: a watch that keeps working through a slow ingest
// hands over nothing extra, and the test says so by the quiet.
func (sink *blockingSink) noEventWithin(t *testing.T, within time.Duration) {
	t.Helper()

	select {
	case event := <-sink.events:
		t.Fatalf("the ingest event %q arrived, want silence within %v", event, within)

	case <-time.After(within):
	}
}

// probeRecording is the recording the blocking-sink tests ingest first, as the test's own
// proof that the subscription is live before the blocked read begins.
const probeRecording = "10001-1514000-1111-20261006-110000.wav"

// secondRecording is a recording with a system, talkgroup, unit, and stamp of its own.
const secondRecording = "10002-1514001-6790-20261006-120100.wav"

// putProbeUntilIngested keeps presenting the probe to the watch until it is read: the
// subscription is only live once the startup pass has registered the directory, and every
// re-write is a fresh event for a watch that has begun listening.
func putProbeUntilIngested(t *testing.T, watched *watchedDirectory, sink *blockingSink) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		putRecording(t, watched.directory, probeRecording)

		start := time.Now()

		for time.Since(start) < settleWait {
			select {
			case event := <-sink.events:
				if event == "end:"+probeRecording {
					return
				}

			case <-time.After(settleWait):
			}
		}
	}

	t.Fatal("the watch never ingested the probe, want the settle path to read it")
}

// TestEventsKeepArmingWhileARecordingIsBeingIngested: the event loop keeps arming new files
// while an ingest is in progress; the legacy settle read held the mutex through the ingest.
func TestEventsKeepArmingWhileARecordingIsBeingIngested(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	sink := newBlockingSink()

	watched := watchingOverWithSink(t, directory, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.DeleteAfter = false

		return watch
	}, sink)

	putProbeUntilIngested(t, watched, sink)

	sink.blockNext()

	putRecording(t, directory, recordedFile)

	if event := sink.nextEvent(t, settleWait+settleWait); event != "start:"+recordedFile {
		t.Fatalf("the watch handed over %q, want the recording's read to start", event)
	}

	// The recording tool writes a second recording while the first is being ingested.
	sink.blockNext()

	putRecording(t, directory, secondRecording)

	// The second read starts while the first is still held on the gate. Today the arming
	// waits behind the first file's ingest and this event never comes within the window —
	// this is where this test fails today.
	if event := sink.nextEvent(t, settleWait); event != "start:"+secondRecording {
		t.Fatalf("the watch handed over %q, want the second recording's read to start while the first is still being ingested", event)
	}

	sink.release()

	ends := map[string]bool{}

	for i := 0; i < 2; i++ {
		event := sink.nextEvent(t, settleWait)

		if !strings.HasPrefix(event, "end:") {
			t.Fatalf("the watch handed over %q, want both reads to complete", event)
		}

		ends[event] = true
	}

	for _, name := range []string{recordedFile, secondRecording} {
		if !ends["end:"+name] {
			t.Errorf("the watch completed %v, want the read of %s among them", ends, name)
		}
	}
}

// TestAFileWrittenDuringTheStartupSettleWindowIsIngestedOnce: a file the startup pass is
// handing over is not read again by a settle timer firing over it; the legacy read it twice.
func TestAFileWrittenDuringTheStartupSettleWindowIsIngestedOnce(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	// The recording is there when the server starts watching.
	putRecording(t, directory, recordedFile)

	sink := newBlockingSink()
	sink.blockNext()

	watchingOverWithSink(t, directory, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.DeleteAfter = true

		return watch
	}, sink)

	if event := sink.nextEvent(t, settleWait+settleWait); event != "start:"+recordedFile {
		t.Fatalf("the watch handed over %q, want the startup pass to start reading the recording", event)
	}

	// The tool writes the same recording again while the startup pass is holding it.
	putRecording(t, directory, recordedFile)

	// The settle timer for that write runs while the startup pass is still reading the
	// same file. Today the startup pass claims nothing, and the settle read ingests the
	// recording a second time — this is where this test fails today.
	sink.noEventWithin(t, settleWait)

	sink.release()

	if event := sink.nextEvent(t, settleWait); event != "end:"+recordedFile {
		t.Fatalf("the watch handed over %q, want the one read to complete", event)
	}

	sink.noEventWithin(t, settleWait)
}

// TestStoppingTheWatchersDoesNotWaitForAnIngestInProgress: a configuration save stops the
// watchers and must not wait for a recording halfway through being handed over.
func TestStoppingTheWatchersDoesNotWaitForAnIngestInProgress(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	sink := newBlockingSink()

	watched := watchingOverWithSink(t, directory, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.DeleteAfter = false

		return watch
	}, sink)

	putProbeUntilIngested(t, watched, sink)

	sink.blockNext()

	putRecording(t, directory, recordedFile)

	if event := sink.nextEvent(t, settleWait+settleWait); event != "start:"+recordedFile {
		t.Fatalf("the watch handed over %q, want the recording's read to start", event)
	}

	// The read is being held on the gate. Today the stop waits behind the mutex the
	// ingest is holding — this is where this test fails today.
	stopped := make(chan struct{})

	go func() {
		watched.watchers.Stop(context.Background())

		close(stopped)
	}()

	select {
	case <-stopped:

	case <-time.After(settleWait):
		t.Fatal("the stop is waiting for the ingest in progress, want it to end the watching and leave")
	}

	sink.release()

	if event := sink.nextEvent(t, settleWait); event != "end:"+recordedFile {
		t.Fatalf("the watch handed over %q, want the read in progress to finish", event)
	}
}

// TestASymlinkedDirectoryInsideAWatchedDirectoryIsNotWatched: a linked directory is not one
// the server was told to watch; the legacy watch followed the link and read recordings from
// outside the watched tree.
func TestASymlinkedDirectoryInsideAWatchedDirectoryIsNotWatched(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	outside := t.TempDir()

	if err := os.Symlink(outside, filepath.Join(directory, "linked")); err != nil {
		t.Fatalf("linking the outside directory: %v", err)
	}

	watched := watchingOver(t, directory, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.DeleteAfter = false

		return watch
	})

	// The link is there when the server starts watching. Today the watch follows it, and
	// this recording is ingested from outside the watched tree — this is where this test
	// fails today.
	putRecording(t, outside, recordedFile)

	watched.calls.nothingArrivesWithin(t, settleWait+settleWait)
}

// TestAMaskWhoseGroupsOutnumberItsTokensIsReportedAtStart: a mask carrying literal
// parentheses captured groups nobody named and lined the filename's pieces up with the wrong
// values; the mismatch is reported at start, where the operator can read it.
func TestAMaskWhoseGroupsOutnumberItsTokensIsReportedAtStart(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	watched := watchingOver(t, directory, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.Mask = "call(1)-#TG"

		return watch
	})

	if !watched.loggedAs(domain.LogSourceDirwatch, "groups") {
		lines := watched.reportedAfterStarting()

		t.Errorf("the watch wrote %v, want a line saying the mask captures more groups than it names tokens", lines)
	}
}

// TestARecordingMovedIntoAWatchedDirectoryIsIngested: a file moved in on one filesystem
// arrives as a rename whose name is there; the legacy exact-match event switch saw neither
// bit and never read it.
func TestARecordingMovedIntoAWatchedDirectoryIsIngested(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	staging := t.TempDir()

	watched := watchingOver(t, directory, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.DeleteAfter = false

		return watch
	})

	putRecordingUntilIngested(t, watched, probeRecording)

	// The finished recording is staged outside the watched directory on the same
	// filesystem and moved in: the watch sees one rename event naming the file.
	putRecording(t, staging, recordedFile)

	if err := os.Rename(filepath.Join(staging, recordedFile), filepath.Join(directory, recordedFile)); err != nil {
		t.Fatalf("moving the recording into the watched directory: %v", err)
	}

	call := watched.calls.next(t, settleWait+settleWait)

	if call.AudioFilename != recordedFile {
		t.Errorf("the watch ingested %q, want the recording that was moved in", call.AudioFilename)
	}
}

// TestARenameEventNamingAFileThatIsThereReArmsIt: the watch re-arms a rename whose name is
// there; the legacy watch ignored every rename event. The file is placed before the watch
// starts, so the rename is the only event naming it.
func TestARenameEventNamingAFileThatIsThereReArmsIt(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	putRecording(t, directory, recordedFile)

	watched := watchingOver(t, directory, func(watch domain.Dirwatch) domain.Dirwatch {
		watch.DeleteAfter = false

		return watch
	})

	w := watched.watchers.running[0]

	w.handle(context.Background(), fsnotify.Event{
		Name: filepath.Join(directory, recordedFile),
		Op:   fsnotify.Rename,
	})

	call := watched.calls.next(t, settleWait+settleWait)

	if call.AudioFilename != recordedFile {
		t.Errorf("the watch ingested %q, want the file the rename named", call.AudioFilename)
	}
}

// The status view counts the watchers that are actually watching. It is shown beside the configured and
// enabled counts the operator's own page already holds, and the difference between them is what tells
// the operator that a watch the configuration names is doing nothing.
func TestDirwatchersRunningCountsTheWatchersThatAreWatching(t *testing.T) {
	watchers := watchingSeveral(t)

	watchers.Start(context.Background(), domain.Dirwatches{
		aWatchOver(t.TempDir()),
		aWatchOver(t.TempDir()),
	})

	if running := watchers.Running(); running != 2 {
		t.Errorf("Running() = %d, want the 2 watchers the server started", running)
	}

	watchers.Stop(context.Background())

	if running := watchers.Running(); running != 0 {
		t.Errorf("Running() = %d after Stop, want 0: nothing is watching any more", running)
	}
}

// A disabled watch is never started, and a watch with no directory is never started. Counting them
// would let the status view report a server watching two directories when it is watching one.
func TestDirwatchersRunningLeavesOutTheWatchesThatWatchNothing(t *testing.T) {
	watchers := watchingSeveral(t)

	disabled := aWatchOver(t.TempDir())
	disabled.Disabled = true

	nameless := domain.NewDirwatch()

	watchers.Start(context.Background(), domain.Dirwatches{aWatchOver(t.TempDir()), disabled, nameless})

	if running := watchers.Running(); running != 1 {
		t.Errorf("Running() = %d, want 1: a disabled watch and a watch naming no directory are never started", running)
	}
}

// Starting again replaces what is watching. A count that kept the previous set would report watchers
// the server stopped a moment ago.
func TestDirwatchersRunningReportsOnlyTheSetItStartedLast(t *testing.T) {
	watchers := watchingSeveral(t)

	watchers.Start(context.Background(), domain.Dirwatches{aWatchOver(t.TempDir()), aWatchOver(t.TempDir())})

	watchers.Start(context.Background(), domain.Dirwatches{aWatchOver(t.TempDir())})

	if running := watchers.Running(); running != 1 {
		t.Errorf("Running() = %d after starting again with one watch, want 1", running)
	}
}

// watchingSeveral is the watchers with nothing but a log and an ingest path they will not be asked to
// use: these tests count what is watching, and never hand it a file.
func watchingSeveral(t *testing.T) *Dirwatchers {
	t.Helper()

	return NewDirwatchers(
		parsers.New(&fakes.Systems{Stored: recordedSystems()}, fakes.NewClock(recordedAt)),
		&fakes.Logs{},
		&ingestRecorder{arrived: make(chan domain.Call, 16)},
		fakes.NewClock(recordedAt),
	)
}

// aWatchOver is one enabled watch over a directory the test already has.
func aWatchOver(directory string) domain.Dirwatch {
	watch := domain.NewDirwatch()
	watch.Directory = directory
	watch.Mask = "#SYS-#TG-#UNIT-#DATE-#TIME"

	return watch
}

// heldSink is an ingest path that keeps the one recording it is handed until the test lets
// it go, and then reports the context the watch was running the handover on. It stands for
// the ingest a configuration save finds busy: the shipped ingest path stores the call
// through that context and converts its audio under it.
type heldSink struct {
	started chan domain.Call
	finish  chan struct{}
	seen    chan context.Context
}

func newHeldSink() *heldSink {
	return &heldSink{
		started: make(chan domain.Call, 1),
		finish:  make(chan struct{}),
		seen:    make(chan context.Context, 1),
	}
}

var _ IngestSink = (*heldSink)(nil)

func (sink *heldSink) IngestCall(ctx context.Context, call domain.Call) error {
	sink.started <- call

	<-sink.finish

	sink.seen <- ctx

	return nil
}

// A save stops the watchers so the watch list it just wrote takes effect, and the stop's own
// comment says the watchers close first so that a call halfway through being ingested
// finishes. The stop closes each subscription and then ends the context every read is running
// on, without waiting for the read one of them is standing in front of: the handover that is
// running is the one that loses its context, so the ingest cannot store the call or convert
// its audio, and the watch deletes the recording it was reading either way.
func TestARecordingHalfwayThroughBeingIngestedWhenTheWatchersAreStoppedIsNotCutOff(t *testing.T) {
	t.Parallel()

	sink := newHeldSink()

	watched := watchingOverWithSink(t, t.TempDir(), nil, sink)

	// The subscription is only live once the pass over the directory has registered it, so the
	// recording is presented until the watch begins reading it.
	deadline := time.Now().Add(30 * time.Second)

	ingesting := false

	for !ingesting {
		if time.Now().After(deadline) {
			t.Fatalf("the watch never began reading %q, want the settle path to hand it over", recordedFile)
		}

		putRecording(t, watched.directory, recordedFile)

		select {
		case <-sink.started:
			ingesting = true

		case <-time.After(settleWait):
		}
	}

	// A configuration save, while that handover is still running.
	watched.watchers.Stop(context.Background())

	close(sink.finish)

	ctx := <-sink.seen

	if err := ctx.Err(); err != nil {
		t.Errorf("the handover a save found running sees %v when it is let carry on, want the save to leave a running handover alone: the ingest path stores the call through that context and converts its audio under it, and the watch deletes the recording it was reading either way", err)
	}
}
