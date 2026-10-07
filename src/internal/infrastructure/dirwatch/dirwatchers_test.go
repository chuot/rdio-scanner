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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/dirwatch/parsers"
)

// The watched-directory tests are integration tests over real directories and real filesystem
// notifications, so they wait for the two-second floor every watch has. They wait in parallel,
// and each one waits for the shortest thing it can: a recording that is already in the directory
// is read by the first pass over it, which is the fastest way a watch ever ingests anything.
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

	watch := domain.NewDirwatch()
	watch.Directory = directory
	watch.Mask = "#SYS-#TG-#UNIT-#DATE-#TIME"

	if configure != nil {
		watch = configure(watch)
	}

	calls := newIngestRecorder()
	logs := &fakes.Logs{}

	watchers := NewDirwatchers(
		parsers.New(&fakes.Systems{Stored: recordedSystems()}, fakes.NewClock(recordedAt)),
		logs,
		calls,
		fakes.NewClock(recordedAt),
	)

	watchers.Start(context.Background(), domain.Dirwatches{watch})

	return &watchedDirectory{watchers: watchers, directory: directory, calls: calls, logs: logs}
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

// TestARecordingIsIngestedOnceEvenThoughWritingItIsTwoEvents is the reason the watch waits for a
// file to stop changing, and the reason the wait is guarded: a recording tool produces a create
// event and a write event for the same file, and a watch that read on both would record the call
// twice.
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

	if lines := watched.logLines(); len(lines) != 0 {
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

// wantGone waits for a file the watch is supposed to delete, and says what it found when it is still
// there. The watch hands the call over before it deletes the files it read from, which is the order the
// legacy server has them in, so the moment a test can see the call is a moment the files may not be gone
// yet.
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

// TestARecordingAlreadyInTheDirectoryIsIngestedOnlyByAWatchThatDeletesAfterIngesting is quirk
// Q49: the first pass over a watched directory reads the recordings that were already there only
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

	lines := watched.logLines()

	if len(lines) != 1 {
		t.Fatalf("the watch wrote %v to the administrator's log, want one line saying what it could not read", lines)
	}

	if !strings.Contains(lines[0], "dirwatch.ingest:") || !strings.Contains(lines[0], path) {
		t.Errorf("the watch wrote %q, want the ingest failure naming the file it could not read", lines[0])
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

	if lines := watched.logLines(); len(lines) != 0 {
		t.Errorf("the watch wrote %v to the administrator's log, want the ingest path's own report and no second copy of it", lines)
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
	// directory that appeared at the same moment is missed, which is quirk Q50.
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

	if !strings.Contains(lines[0], "dirwatch.walkdir:") {
		t.Errorf("the watch wrote %q, want the failure of the pass over the directory", lines[0])
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

	watchers := NewDirwatchers(
		parsers.New(&fakes.Systems{Stored: recordedSystems()}, fakes.NewClock(recordedAt)),
		&fakes.Logs{},
		newIngestRecorder(),
		fakes.NewClock(recordedAt),
	)

	watchers.Start(context.Background(), domain.Dirwatches{disabled, nowhere})

	if running := watchers.running; len(running) != 0 {
		t.Errorf("%d watchers were started for a disabled watch and a watch with no directory, want none", len(running))
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
