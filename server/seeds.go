// Copyright (C) 2019-2024 Chrystian Huot <chrystian@huot.qc.ca>
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

package main

import "fmt"

func seedGroups(db *Database) error {
	var (
		count uint
		query string
	)

	formatError := errorFormatter("seeds", "seedgroups")

	query = `SELECT COUNT(*) FROM "groups"`
	if err := db.Sql.QueryRow(query).Scan(&count); err != nil {
		return formatError(err, query)
	}

	if count == 0 {
		if tx, err := db.Sql.Begin(); err == nil {
			for _, group := range defaults.groups {
				query := fmt.Sprintf(`INSERT INTO "groups" ("label") VALUES ('%s')`, group)
				if _, err := tx.Exec(query); err != nil {
					tx.Rollback()
					return formatError(err, query)
				}
			}

			if err := tx.Commit(); err != nil {
				return formatError(err, "")
			}

		} else {
			return formatError(err, "")
		}
	}

	return nil
}

func seedTags(db *Database) error {
	var (
		count uint
		query string
	)

	formatError := errorFormatter("seeds", "seedtags")

	query = `SELECT COUNT(*) FROM "tags"`
	if err := db.Sql.QueryRow(query).Scan(&count); err != nil {
		return formatError(err, query)
	}

	if count == 0 {
		if tx, err := db.Sql.Begin(); err == nil {
			for _, tag := range defaults.tags {
				query := fmt.Sprintf(`INSERT INTO "tags" ("label") VALUES ('%s')`, tag)
				if _, err := tx.Exec(query); err != nil {
					tx.Rollback()
					return formatError(err, query)
				}
			}

			if err := tx.Commit(); err != nil {
				return formatError(err, "")
			}

		} else {
			return formatError(err, "")
		}
	}

	return nil
}
