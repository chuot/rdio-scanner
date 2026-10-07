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

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// PruneOldCallsOutcome reports what the server did about the calls and log entries that
// have outlived the retention the configuration sets. The scheduler that runs this command
// hourly records a failure in the server log; nothing about the outcome reaches a client.
type PruneOldCallsOutcome string

const (
	// PruneOldCallsDone means both the call history and the server log were pruned.
	PruneOldCallsDone PruneOldCallsOutcome = "done"

	// PruneOldCallsSkipped means the retention is switched off, so nothing was touched and
	// nothing was said about it. The legacy server returns before it even reports that it
	// is pruning, so a deployment with the retention off shows no pruning notice in its log
	// view.
	PruneOldCallsSkipped PruneOldCallsOutcome = "skipped"

	// PruneOldCallsFailed means a step the pruning depends on could not be completed. The
	// legacy scheduler writes it as `scheduler.run: <error>` at the error level, which is
	// where the scheduler that runs this command records it.
	PruneOldCallsFailed PruneOldCallsOutcome = "failed"
)

// PruneOldCallsResult is the acknowledgement the scheduler gets: what happened and how much
// went. The legacy pruning reports nothing at all; the counts are here so that the outcome
// can be tested and so that a later task can report it without reaching into the database.
type PruneOldCallsResult struct {
	Outcome     PruneOldCallsOutcome
	CallsPruned int
	LogsPruned  int
}

// PruneOldCallsCommand asks the server to discard the recorded calls and the recorded log
// entries that are older than the retention it is configured with.
//
// It carries nothing: the retention is a setting the administrator owns, not something the
// caller gets to choose. The legacy scheduler reads the option when it prunes rather than
// remembering it, so a change the administrator makes takes effect on the next run.
type PruneOldCallsCommand struct{}

// PruneOldCallsHandler applies the retention to the call history and the server log.
//
// Pruning is one command rather than two because the legacy server prunes them together,
// in that order: the call history goes first, and a history that cannot be pruned leaves
// the log alone.
type PruneOldCallsHandler struct {
	calls   ports.CallRepository
	logs    ports.LogRepository
	options ports.OptionsRepository
	clock   ports.Clock
}

// NewPruneOldCallsHandler builds the handler from its ports.
func NewPruneOldCallsHandler(
	calls ports.CallRepository,
	logs ports.LogRepository,
	options ports.OptionsRepository,
	clock ports.Clock,
) *PruneOldCallsHandler {
	return &PruneOldCallsHandler{
		calls:   calls,
		logs:    logs,
		options: options,
		clock:   clock,
	}
}

// Handle discards everything older than the retention, and says beforehand that it is
// doing so.
//
// The notice is written before the pruning rather than after, as the legacy server writes
// it, because a pruning that then fails is still something the administrator needs to see
// in the log view. It is written with the current timestamp, so the pruning does not remove
// its own notice.
func (handler *PruneOldCallsHandler) Handle(ctx context.Context, cmd PruneOldCallsCommand) (PruneOldCallsResult, error) {
	options, err := handler.options.ReadOptions(ctx)
	if err != nil {
		return PruneOldCallsResult{Outcome: PruneOldCallsFailed}, err
	}

	if options.PruneDays == 0 {
		return PruneOldCallsResult{Outcome: PruneOldCallsSkipped}, nil
	}

	handler.log(ctx, domain.LogLevelInfo, "database pruning")

	// One instant bounds both the call history and the log. The legacy server asks the
	// clock twice, once for each, which differs by the time it takes to run the first
	// delete and is not something a client can observe.
	before := options.PruneBefore(handler.clock.Now())

	callsPruned, err := handler.calls.DeleteCallsBefore(ctx, before)
	if err != nil {
		return PruneOldCallsResult{Outcome: PruneOldCallsFailed}, err
	}

	logsPruned, err := handler.logs.DeleteLogsBefore(ctx, before)
	if err != nil {
		return PruneOldCallsResult{Outcome: PruneOldCallsFailed, CallsPruned: callsPruned}, err
	}

	return PruneOldCallsResult{
		Outcome:     PruneOldCallsDone,
		CallsPruned: callsPruned,
		LogsPruned:  logsPruned,
	}, nil
}

func (handler *PruneOldCallsHandler) log(ctx context.Context, level, message string) {
	if err := handler.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: handler.clock.Now(),
		Level:    level,
		Message:  message,
	}); err != nil {
		// A notice that cannot be recorded does not stop the retention from being applied;
		// the administrator's data is what the pruning is for.
		return
	}
}
