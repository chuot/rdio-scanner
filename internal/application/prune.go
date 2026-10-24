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

package application

import (
	"context"
	"fmt"

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
	// nothing was said about it: a deployment with the retention off shows no pruning
	// notice in its log view.
	PruneOldCallsSkipped PruneOldCallsOutcome = "skipped"

	// PruneOldCallsFailed means a step the pruning depends on could not be completed; the
	// scheduler that runs this command records it at the error level.
	PruneOldCallsFailed PruneOldCallsOutcome = "failed"
)

// PruneOldCallsResult is the acknowledgement the scheduler gets: what happened and how much
// went.
type PruneOldCallsResult struct {
	Outcome     PruneOldCallsOutcome
	CallsPruned int
	LogsPruned  int
}

// PruneOldCallsCommand asks the server to discard the recorded calls and the recorded log
// entries that are older than the retention it is configured with. It carries nothing: the
// retention is a setting the administrator owns, read fresh on every run.
type PruneOldCallsCommand struct{}

// PruneOldCallsHandler applies the retention to the call history and the server log.
// Pruning is one command rather than two because the two passes run together, in that
// order: the call history goes first, and a history that cannot be pruned leaves the log
// alone.
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

// Handle discards everything older than the retention and then reports what it removed.
// The report is written after the deletes and carries their counts: a retention that has
// stopped removing anything is the failure an operator needs to see before the disk fills,
// and the counts do not exist before the deletes.
func (handler *PruneOldCallsHandler) Handle(ctx context.Context, cmd PruneOldCallsCommand) (PruneOldCallsResult, error) {
	options, err := handler.options.ReadOptions(ctx)
	if err != nil {
		handler.reportFailure(ctx, "the retention pass could not read the retention it is configured with", err)

		return PruneOldCallsResult{Outcome: PruneOldCallsFailed}, err
	}

	if options.PruneDays == 0 {
		return PruneOldCallsResult{Outcome: PruneOldCallsSkipped}, nil
	}

	// One instant bounds both the call history and the log; the legacy server asked the
	// clock separately for each, which is not something a client can observe.
	before := options.PruneBefore(handler.clock.Now())

	callsPruned, err := handler.calls.DeleteCallsBefore(ctx, before)
	if err != nil {
		handler.reportFailure(ctx, "the retention pass could not prune the call history", err)

		return PruneOldCallsResult{Outcome: PruneOldCallsFailed}, err
	}

	logsPruned, err := handler.logs.DeleteLogsBefore(ctx, before)
	if err != nil {
		handler.reportFailure(ctx, "the retention pass could not prune the log", err)

		return PruneOldCallsResult{Outcome: PruneOldCallsFailed, CallsPruned: callsPruned}, err
	}

	appendServerLog(ctx, handler.logs, handler.clock, domain.LogLevelInfo, domain.LogSourcePrune, retentionReport(callsPruned, logsPruned))

	return PruneOldCallsResult{
		Outcome:     PruneOldCallsDone,
		CallsPruned: callsPruned,
		LogsPruned:  logsPruned,
	}, nil
}

// retentionReport is what the retention pass says about itself once it has finished.
func retentionReport(callsPruned, logsPruned int) string {
	return fmt.Sprintf("the retention pass removed %s and %s",
		counted(callsPruned, "call", "calls"),
		counted(logsPruned, "log entry", "log entries"),
	)
}

// reportFailure records the half of the retention the server could not do: the hourly
// schedule discards this command's error, so a log entry is the only trace a broken
// retention leaves.
func (handler *PruneOldCallsHandler) reportFailure(ctx context.Context, what string, reason error) {
	appendServerLog(ctx, handler.logs, handler.clock, domain.LogLevelError, domain.LogSourcePrune, fmt.Sprintf("%s: %v", what, reason))
}
