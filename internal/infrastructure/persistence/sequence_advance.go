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

package persistence

import (
	"context"
	"fmt"
)

// migratedSequence names one v8 table whose primary key the migration pass writes
// itself: the v6 row already carries an identifier, and the pass carries it across.
// MariaDB and SQLite move their counters past an identifier they are handed, but
// PostgreSQL leaves the sequence behind a bigserial exactly where the schema created
// it, so the first row the server adds to a migrated table on its own would be
// assigned an identifier the migrated data is already using. The list is exactly the
// tables whose migration inserts name their own primary key — a table the pass fills
// without naming its key, like the options rows or the call detail rows under a
// call, leaves its counter to the backend and needs nothing.
type migratedSequence struct {
	table string
	key   string
}

var migratedSequenceTables = []migratedSequence{
	{table: "groups", key: "groupId"},
	{table: "tags", key: "tagId"},
	{table: "systems", key: "systemId"},
	{table: "talkgroups", key: "talkgroupId"},
	{table: "units", key: "unitId"},
	{table: "logs", key: "logId"},
	{table: "downstreams", key: "downstreamId"},
	{table: "dirwatches", key: "dirwatchId"},
	{table: "calls", key: "callId"},
	{table: "apikeys", key: "apikeyId"},
	{table: "accesses", key: "accessId"},
}

// advanceMigratedSequences moves each of those sequences past the highest identifier
// the migrated rows carry, so the next identifier the backend hands out is free. It
// runs after every carry stage has written its rows — the highest identifier is only
// knowable once they are all in — and only on PostgreSQL, the one backend that does
// not move a counter past an identifier it was handed.
func advanceMigratedSequences(ctx context.Context, s store) error {
	const section, operation = "migration", "advanceMigratedSequences"

	if s.dialect != DialectPostgresql {
		return nil
	}

	for _, sequence := range migratedSequenceTables {
		// A table the pass never reached ends this part doing nothing: a migration
		// step is not the place to decide that a table should exist.
		exists, err := tableExists(ctx, s, sequence.table)
		if err != nil {
			return operationError(section, operation, err)
		}

		if !exists {
			continue
		}

		// pg_get_serial_sequence names the sequence the column's own default reads
		// from, so the statement advances the backend's counter rather than a
		// look-alike the schema happened to leave behind. setval's third argument,
		// false, makes the value written the next one handed out: the highest
		// migrated identifier plus one.
		query := fmt.Sprintf(
			`SELECT setval(pg_get_serial_sequence('%s', '%s'), COALESCE(MAX("%s"), 0) + 1, false) FROM "%s"`,
			sequence.table, sequence.key, sequence.key, sequence.table,
		)

		if _, err := s.exec(ctx, query); err != nil {
			return statementError(section, operation, err, query)
		}
	}

	return nil
}
