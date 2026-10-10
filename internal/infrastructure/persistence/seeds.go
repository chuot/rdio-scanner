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

	"rdio-scanner/internal/domain"
)

// Seed writes the groups and tags a fresh server is expected to start with, and is the half of what
// the legacy server's `NewDatabase` does after migrating: `seedGroups` and then `seedTags`, from
// `server/seeds.go`, in that order.
//
// It runs after [Migrate](migrations.go), as the legacy server runs it after `migrate`, and the
// order is what makes a migrated deployment keep what it had: the migration has already filled
// `groups` and `tags` with the deployment's own, so the seed finds rows and writes none.
//
// Each half decides on its own. Groups are seeded when `groups` is empty and tags when `tags` is
// empty, so a deployment that had groups but no tags gets the default tags and keeps its groups.
//
// A failure is returned, wrapped the way `Database.seed` wrapped it (`database.seed:
// seeds.seedgroups: …`), including a failure of the count that decides whether to seed at all —
// unlike a migration, which shrugs off a table it cannot read, a seed that cannot ask how many
// rows a table holds is an error.
//
// The `server/…` names in this file are the legacy server's own files, which are not in this
// repository. They are named so a reader can see which behavior is being mirrored, and they resolve
// at git tree 02ae33d4651fee4c46a93bdcf574d0aead89e542.
func Seed(ctx context.Context, db *sql.DB, dialect Dialect) error {
	s := newStore(db, dialect)

	if err := seedGroups(ctx, s); err != nil {
		return operationError("database", "seed", err)
	}

	if err := seedTags(ctx, s); err != nil {
		return operationError("database", "seed", err)
	}

	return nil
}

// seedGroups mirrors `seedGroups` in `server/seeds.go`.
//
// The count that decides whether to seed is read into an unsigned integer, which is what the legacy
// server reads it into, and the labels are written in the order the domain lists them with the
// display order left at its column default of 0. That is the legacy behavior, and it is worth
// knowing: groups written by the migration are given a display order of 1..n, groups written by the
// seed are given none, so a fresh server starts with a table whose ordering is decided by identity
// alone. `TestSeedGroupsLeaveTheirDisplayOrderAtZero` pins it.
func seedGroups(ctx context.Context, s store) error {
	const section, operation = "seeds", "seedgroups"

	var count uint

	query := `SELECT COUNT(*) FROM "groups"`

	if err := s.queryRow(ctx, query).Scan(&count); err != nil {
		return statementError(section, operation, err, query)
	}

	if count > 0 {
		return nil
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	for _, label := range domain.DefaultGroupLabels() {
		query = `INSERT INTO "groups" ("label") VALUES (?)`

		if _, err := tx.exec(ctx, query, label); err != nil {
			tx.rollback()
			return statementError(section, operation, err, query)
		}
	}

	if err := tx.commit(); err != nil {
		return operationError(section, operation, err)
	}

	return nil
}

// seedTags mirrors `seedTags` in `server/seeds.go`, including the label
// the legacy server seeded with a trailing space in it, which
// [domain.DefaultTagLabels](../../domain/tag.go) keeps.
func seedTags(ctx context.Context, s store) error {
	const section, operation = "seeds", "seedtags"

	var count uint

	query := `SELECT COUNT(*) FROM "tags"`

	if err := s.queryRow(ctx, query).Scan(&count); err != nil {
		return statementError(section, operation, err, query)
	}

	if count > 0 {
		return nil
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	for _, label := range domain.DefaultTagLabels() {
		query = `INSERT INTO "tags" ("label") VALUES (?)`

		if _, err := tx.exec(ctx, query, label); err != nil {
			tx.rollback()
			return statementError(section, operation, err, query)
		}
	}

	if err := tx.commit(); err != nil {
		return operationError(section, operation, err)
	}

	return nil
}
