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

package ports

import (
	"time"

	"rdio-scanner/internal/domain"
)

// MigrationDamageReport is what one clearing of the records a legacy migration left in problem
// answers: the unit rows it removed, the rows the migration archive held while it stood, the legacy
// tables it dropped with the rows each one held, and whether the archive table was there to drop at
// all. Every slice is a list even when empty, so a class with nothing to report reads as nothing
// rather than as a run that could not say.
type MigrationDamageReport struct {
	Units []domain.Unit

	ArchiveRows []ArchivedRow

	DroppedTables []DroppedTable

	// DroppedArchive says the archive table was in the deployment and went with it. A deployment
	// whose migration had nothing to archive has no such table, and its clearing says nothing about
	// one rather than failing for its absence.
	DroppedArchive bool
}

// ArchivedRow names one row of the migration archive by the values held beside it: the table it came
// from, its identifier there, the reason the migration stored with it, and the moment it was
// archived. The row's own values are not part of it — a run reports the row and drops the table that
// held it, and what a row holds can be a call's encoded audio, which no report repeats.
type ArchivedRow struct {
	Table      string
	RowID      uint64
	Reason     string
	ArchivedAt time.Time
}

// DroppedTable names one legacy table the clearing dropped and how many rows it held while it stood.
type DroppedTable struct {
	Name string
	Rows int
}
