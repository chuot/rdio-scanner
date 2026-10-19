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

// Handle discards everything older than the retention and then reports what it removed.
//
// The legacy server writes "database pruning" before it starts, which tells an operator that a pass was
// attempted and nothing about what it did. The counts the two deletes already return are the part an operator
// reads: a retention that has stopped removing anything is the failure they need to see before the disk
// fills, and a pass that removed nothing looks identical in the log view to a pass that never ran.
//
// The report is therefore written after the deletes, which is also what makes it possible: the counts do not
// exist before them. A pass that fails is reported by its own entry, not by a notice written too early to
// know it would fail.
func (handler *PruneOldCallsHandler) Handle(ctx context.Context, cmd PruneOldCallsCommand) (PruneOldCallsResult, error) {
	options, err := handler.options.ReadOptions(ctx)
	if err != nil {
		handler.reportFailure(ctx, "the retention pass could not read the retention it is configured with", err)

		return PruneOldCallsResult{Outcome: PruneOldCallsFailed}, err
	}

	if options.PruneDays == 0 {
		return PruneOldCallsResult{Outcome: PruneOldCallsSkipped}, nil
	}

	// One instant bounds both the call history and the log. The legacy server asks the
	// clock twice, once for each, which differs by the time it takes to run the first
	// delete and is not something a client can observe.
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

// reportFailure records the half of the retention the server could not do.
//
// The hourly schedule discards this command's error, as the legacy server's scheduler discards it, and until
// now the pass wrote nothing on a failure either: a deployment whose retention had been broken for a month
// had a log view that looked healthy. The error still goes back to the caller, because the outcome the
// caller reads is not this feature's to change.
func (handler *PruneOldCallsHandler) reportFailure(ctx context.Context, what string, reason error) {
	appendServerLog(ctx, handler.logs, handler.clock, domain.LogLevelError, domain.LogSourcePrune, fmt.Sprintf("%s: %v", what, reason))
}
