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

	if len(messages) != 2 || messages[0] != "recent event" || messages[1] != "database pruning" {
		t.Errorf("server log = %v, want the entry inside the retention and the notice the pruning wrote", messages)
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

func TestPruneOldCallsCommandRecordsThatItIsPruning(t *testing.T) {
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
		if entry.Message == "database pruning" {
			notice = entry
			found = true
		}
	}

	if !found {
		t.Fatalf("server log = %v, want the pruning notice the legacy server writes before it prunes", fixture.logs.Stored)
	}

	if notice.Level != domain.LogLevelInfo {
		t.Errorf("notice level = %q, want the pruning notice reported as information", notice.Level)
	}

	if result.LogsPruned != 1 {
		t.Errorf("pruned logs = %d, want the notice itself kept: it is written with the current timestamp", result.LogsPruned)
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
