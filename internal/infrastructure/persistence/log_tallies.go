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

package persistence

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The tally the status view's log-health card is built from. It is kept apart from the log view's
// search: the search pages and filters entries for an operator reading them, and this counts what the
// server complained about for an operator watching a page.
//
// Only the two severities that mean trouble are tallied. An hour of ordinary notices is not a reason to
// say a subsystem needs attention, and a card that counted everything would say it about every
// deployment there is.

var _ ports.LogTallyRepository = (*Logs)(nil)

// TallyLogLevels counts the error and warning entries written at or after the given instant, per
// severity and per subsystem. A subsystem that wrote neither is not one of the tallies: an absent
// subsystem is a subsystem that had nothing to complain about, and listing every part of the server
// with a zero each would bury the one that did.
func (logs *Logs) TallyLogLevels(ctx context.Context, from time.Time) ([]ports.LogLevelTally, error) {
	query := `SELECT "level", "source", COUNT(*) FROM "logs"` +
		` WHERE "timestamp" >= ? AND "level" IN (?, ?)` +
		` GROUP BY "level", "source"`

	rows, err := logs.store.query(ctx, query, millisOf(from), domain.LogLevelWarn, domain.LogLevelError)
	if err != nil {
		return nil, statementError("logs", "tally", err, query)
	}

	defer rows.Close()

	// The two spellings a row without a subsystem can carry — the blank another server's insert
	// leaves, and the column the row never had at all — are one group in the log view, so they are one
	// group here. The same entry counted one way on the status page and another way on the log page is
	// two numbers an operator cannot reconcile.
	counted := map[string]map[domain.LogSource]uint64{}

	for rows.Next() {
		var (
			level  string
			source sql.NullString
			count  int64
		)

		if err := rows.Scan(&level, &source, &count); err != nil {
			return nil, operationError("logs", "tally", err)
		}

		bySource, ok := counted[level]
		if !ok {
			bySource = map[domain.LogSource]uint64{}

			counted[level] = bySource
		}

		bySource[logSourceOf(source)] += clampNonNegative(count)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("logs", "tally", err)
	}

	tallies := []ports.LogLevelTally{}

	for level, bySource := range counted {
		for source, count := range bySource {
			tallies = append(tallies, ports.LogLevelTally{Level: level, Source: source, Count: count})
		}
	}

	// The grouping comes back in whatever order the backend chose. The status view sorts its own card,
	// and a tally list that changed order between two polls of the same window would be read as the
	// server changing when nothing did.
	sort.Slice(tallies, func(i, j int) bool {
		if tallies[i].Count != tallies[j].Count {
			return tallies[i].Count > tallies[j].Count
		}

		if tallies[i].Level != tallies[j].Level {
			return tallies[i].Level < tallies[j].Level
		}

		return tallies[i].Source < tallies[j].Source
	})

	return tallies, nil
}
