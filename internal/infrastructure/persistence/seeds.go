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
	"database/sql"

	"rdio-scanner/internal/domain"
)

// Seed writes the groups and tags a fresh server is expected to start
// with, and runs after [Migrate](migrations.go) because a migrated
// deployment's own rows are already in place for it to find. Each half —
// groups and tags — decides on its own whether its table is empty, so a
// deployment that had groups but no tags keeps its groups and gets the
// default tags.
//
// A seed failure is returned wrapped, and unlike a migration a seed treats
// a failure of the count that decides whether to seed as a failure; the two
// postures and the legacy wrapping live in the seed-failure-posture
// maintainer note.
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

// seedGroups mirrors the legacy server's own seedGroups. Groups written by
// the seed keep the display order column's default of 0 while groups
// written by the migration are numbered 1..n, so a fresh server's ordering
// is decided by identity alone — TestSeedGroupsLeaveTheirDisplayOrderAtZero
// pins that.
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
