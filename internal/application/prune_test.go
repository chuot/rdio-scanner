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
	"errors"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

var pruneTime = time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)

type pruneFixture struct {
	calls   *fakes.Calls
	logs    *fakes.Logs
	options *fakes.Options
	clock   *fakes.Clock

	prune *PruneOldCallsHandler
}

func newPruneFixture() *pruneFixture {
	fixture := &pruneFixture{
		calls:   fakes.NewCalls(),
		logs:    &fakes.Logs{},
		options: &fakes.Options{Stored: domain.DefaultOptions()},
		clock:   fakes.NewClock(pruneTime),
	}

	fixture.prune = NewPruneOldCallsHandler(fixture.calls, fixture.logs, fixture.options, fixture.clock)

	return fixture
}

// run prunes once, which is what the scheduler does every hour.
func (fixture *pruneFixture) run() (PruneOldCallsResult, error) {
	return fixture.prune.Handle(context.Background(), PruneOldCallsCommand{})
}

// filedUnder reports the entry the retention pass filed with itself, which is the only thing a deployment
// sees of a pass that ran, and of a pass that could not run.
func (fixture *pruneFixture) filedUnder() (domain.LogEntry, bool) {
	entry := domain.LogEntry{}
	found := false

	for _, stored := range fixture.logs.Stored {
		if stored.Source == domain.LogSourcePrune {
			entry = stored
			found = true
		}
	}

	return entry, found
}

// storeCall records a call whose only relevant detail is when it happened.
func (fixture *pruneFixture) storeCall(id uint64, at time.Time) {
	call := domain.NewCall()
	call.ID = id
	call.Timestamp = at

	fixture.calls.Stored[id] = call
}

// storeLog records a server event whose only relevant detail is when it happened.
func (fixture *pruneFixture) storeLog(at time.Time, message string) {
	fixture.logs.Stored = append(fixture.logs.Stored, domain.LogEntry{
		ID:       uint64(len(fixture.logs.Stored) + 1),
		DateTime: at,
		Level:    domain.LogLevelInfo,
		Message:  message,
	})
}

// retention is the instant the configured retention reaches back to, spelled out so the
// tests can name the boundary they are pinning.
func (fixture *pruneFixture) retention() time.Time {
	return fixture.options.Stored.PruneBefore(fixture.clock.Now())
}

func TestPruneOldCallsCommandRemovesCallsAndLogsPastTheRetention(t *testing.T) {
	fixture := newPruneFixture()

	fixture.storeCall(1, pruneTime.AddDate(0, 0, -8))
	fixture.storeCall(2, pruneTime.AddDate(0, 0, -6))

	fixture.storeLog(pruneTime.AddDate(0, 0, -8), "old event")
	fixture.storeLog(pruneTime.AddDate(0, 0, -6), "recent event")

	result, err := fixture.run()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.Outcome != PruneOldCallsDone {
		t.Errorf("outcome = %q, want the retention applied", result.Outcome)
	}

	if result.CallsPruned != 1 || result.LogsPruned != 1 {
		t.Errorf("pruned calls=%d logs=%d, want one of each: the pair older than the retention", result.CallsPruned, result.LogsPruned)
	}

	if _, kept := fixture.calls.Stored[1]; kept {
		t.Errorf("call 1 is still stored, want the call older than the retention gone")
	}

	if _, kept := fixture.calls.Stored[2]; !kept {
		t.Errorf("call 2 is gone, want the call inside the retention kept")
	}

	messages := []string{}

	for _, entry := range fixture.logs.Stored {
		messages = append(messages, entry.Message)
	}

	if len(messages) != 2 || messages[0] != "recent event" || messages[1] != "the retention pass removed 1 call and 1 log entry" {
		t.Errorf("server log = %v, want the entry inside the retention and the pass reporting what it removed", messages)
	}
}

func TestPruneOldCallsCommandKeepsWhatIsExactlyAtTheRetentionBoundary(t *testing.T) {
	fixture := newPruneFixture()

	fixture.storeCall(1, fixture.retention())
	fixture.storeCall(2, fixture.retention().Add(-time.Millisecond))

	result, err := fixture.run()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.CallsPruned != 1 {
		t.Errorf("pruned calls = %d, want only the call strictly older than the retention", result.CallsPruned)
	}

	if _, kept := fixture.calls.Stored[1]; !kept {
		t.Errorf("call 1 is gone, want a call exactly at the retention kept, as the legacy delete keeps it")
	}
}

func TestPruneOldCallsCommandDoesNothingWhenTheRetentionIsZero(t *testing.T) {
	fixture := newPruneFixture()
	fixture.options.Stored.PruneDays = 0

	fixture.storeCall(1, pruneTime.AddDate(0, 0, -400))
	fixture.storeLog(pruneTime.AddDate(0, 0, -400), "ancient event")

	result, err := fixture.run()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.Outcome != PruneOldCallsSkipped {
		t.Errorf("outcome = %q, want the retention switched off to leave everything alone", result.Outcome)
	}

	if len(fixture.calls.Stored) != 1 {
		t.Errorf("stored calls = %d, want a switched-off retention to prune nothing", len(fixture.calls.Stored))
	}

	if len(fixture.logs.Stored) != 1 {
		t.Errorf("server log = %v, want a switched-off retention to say nothing about pruning either", fixture.logs.Stored)
	}
}

// The legacy server writes "database pruning" before it prunes, which tells an operator that a pass was
// attempted and nothing about what it did. The counts DeleteCallsBefore and DeleteLogsBefore already return
// are the part an operator reads: a retention that is not removing anything is the failure they need to see
// before the disk fills.
func TestPruneOldCallsCommandReportsWhatTheRetentionPassRemoved(t *testing.T) {
	fixture := newPruneFixture()

	fixture.storeLog(pruneTime.AddDate(0, 0, -8), "old event")

	result, err := fixture.run()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.Outcome != PruneOldCallsDone {
		t.Fatalf("outcome = %q, want the retention applied", result.Outcome)
	}

	notice := domain.LogEntry{}
	found := false

	for _, entry := range fixture.logs.Stored {
		if entry.Source == domain.LogSourcePrune {
			notice = entry
			found = true
		}
	}

	if !found {
		t.Fatalf("server log = %v, want the retention pass to report what it removed, under the retention pass", fixture.logs.Stored)
	}

	if notice.Level != domain.LogLevelInfo {
		t.Errorf("report level = %q, want the retention pass reported as information", notice.Level)
	}

	if notice.Message != "the retention pass removed 0 calls and 1 log entry" {
		t.Errorf("report = %q, want the counts the pass was given, in the words an operator reads", notice.Message)
	}

	if result.LogsPruned != 1 {
		t.Errorf("pruned logs = %d, want the one entry older than the retention", result.LogsPruned)
	}
}

// A pass that removes nothing is the ordinary case on a healthy server, and it is the case an operator
// needs to be able to tell apart from a pass that did not run.
func TestPruneOldCallsCommandSaysWhenTheRetentionPassRemovedNothing(t *testing.T) {
	fixture := newPruneFixture()

	fixture.storeCall(1, pruneTime.AddDate(0, 0, -1))
	fixture.storeLog(pruneTime.AddDate(0, 0, -1), "recent event")

	result, err := fixture.run()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.Outcome != PruneOldCallsDone {
		t.Fatalf("outcome = %q, want the retention applied", result.Outcome)
	}

	report := ""

	for _, entry := range fixture.logs.Stored {
		if entry.Source == domain.LogSourcePrune {
			report = entry.Message
		}
	}

	if report != "the retention pass removed 0 calls and 0 log entries" {
		t.Errorf("report = %q, want the pass to say it removed nothing: %v", report, fixture.logs.Stored)
	}
}

func TestPruneOldCallsCommandLeavesTheLogAloneWhenTheCallHistoryCannotBePruned(t *testing.T) {
	fixture := newPruneFixture()
	fixture.calls.Err = errors.New("calls table is missing")

	fixture.storeCall(1, pruneTime.AddDate(0, 0, -8))
	fixture.storeLog(pruneTime.AddDate(0, 0, -8), "old event")

	result, err := fixture.run()

	if !errors.Is(err, fixture.calls.Err) {
		t.Fatalf("prune error = %v, want the history failure reported to the scheduler", err)
	}

	if result.Outcome != PruneOldCallsFailed {
		t.Errorf("outcome = %q, want a failed pruning", result.Outcome)
	}

	if len(fixture.logs.Stored) == 0 {
		t.Errorf("server log = %v, want the log left alone when the call history could not be pruned", fixture.logs.Stored)
	}

	found := false

	for _, entry := range fixture.logs.Stored {
		if entry.Message == "old event" {
			found = true
		}
	}

	if !found {
		t.Errorf("server log = %v, want the old entry still there, because the legacy pruning stops at the call history", fixture.logs.Stored)
	}
}

// A retention pass that cannot do its work currently writes nothing at all, and its caller throws the error
// away, so a deployment whose retention has been broken for a month has a log that looks healthy. Each of
// the three things the pass depends on gets its own entry naming what it was trying to do.
func TestPruneOldCallsCommandReportsWhatItWasTryingToDoWhenItFailed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stub   func(fixture *pruneFixture, err error)
		want   string
		reason string
	}{
		{
			name:   "the retention cannot be read",
			stub:   func(fixture *pruneFixture, err error) { fixture.options.Err = err },
			want:   "the retention pass could not read the retention it is configured with: options table is missing",
			reason: "options table is missing",
		},
		{
			name:   "the call history cannot be pruned",
			stub:   func(fixture *pruneFixture, err error) { fixture.calls.Err = err },
			want:   "the retention pass could not prune the call history: calls table is missing",
			reason: "calls table is missing",
		},
		{
			name:   "the log cannot be pruned",
			stub:   func(fixture *pruneFixture, err error) { fixture.logs.DeleteErr = err },
			want:   "the retention pass could not prune the log: logs table is missing",
			reason: "logs table is missing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPruneFixture()

			tc.stub(fixture, errors.New(tc.reason))

			fixture.storeCall(1, pruneTime.AddDate(0, 0, -8))
			fixture.storeLog(pruneTime.AddDate(0, 0, -8), "old event")

			result, err := fixture.run()
			if err == nil {
				t.Fatalf("prune reported no error, want the failure reported to the scheduler")
			}

			if result.Outcome != PruneOldCallsFailed {
				t.Errorf("outcome = %q, want a failed retention pass", result.Outcome)
			}

			entry, found := fixture.filedUnder()
			if !found {
				t.Fatalf("server log = %v, want the retention pass to say what it was trying to do", fixture.logs.Stored)
			}

			if entry.Level != domain.LogLevelError {
				t.Errorf("level = %q, want a failed retention pass reported as an error", entry.Level)
			}

			if entry.Message != tc.want {
				t.Errorf("report = %q, want what the pass was trying to do, then the failure's own text", entry.Message)
			}
		})
	}
}

func TestPruneOldCallsCommandMeasuresTheRetentionFromTheCurrentInstant(t *testing.T) {
	fixture := newPruneFixture()

	fixture.storeCall(1, pruneTime.AddDate(0, 0, -6))

	result, err := fixture.run()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.CallsPruned != 0 {
		t.Fatalf("pruned calls = %d, want a six-day-old call inside a seven-day retention", result.CallsPruned)
	}

	fixture.clock.At = pruneTime.AddDate(0, 0, 2)

	result, err = fixture.run()
	if err != nil {
		t.Fatalf("prune after time has passed: %v", err)
	}

	if result.CallsPruned != 1 {
		t.Errorf("pruned calls = %d, want the same call gone once the current instant has carried it past the retention", result.CallsPruned)
	}
}

func TestPruneOldCallsCommandAppliesTheRetentionTheConfigurationSetsNow(t *testing.T) {
	fixture := newPruneFixture()

	fixture.storeCall(1, pruneTime.AddDate(0, 0, -5))

	result, err := fixture.run()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.CallsPruned != 0 {
		t.Fatalf("pruned calls = %d, want a five-day-old call inside the default seven-day retention", result.CallsPruned)
	}

	fixture.options.Stored.PruneDays = 3

	result, err = fixture.run()
	if err != nil {
		t.Fatalf("prune after the administrator shortened the retention: %v", err)
	}

	if result.CallsPruned != 1 {
		t.Errorf("pruned calls = %d, want the retention the configuration names now, read afresh each run", result.CallsPruned)
	}
}
