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
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Migrate applies the schema and then the v6 → v7 migration pass.
//
// It is the two halves of what the legacy server's `NewDatabase` does after it configures the
// connection pool: `ApplySchema` first, then the thirteen migrations of
// `server/migrations.go`, in the legacy order — groups, tags,
// systems, talkgroups, units, options, meta, logs, downstreams, dirwatches, calls, apikeys,
// accesses. The order is load-bearing rather than incidental: the talkgroup, unit, dirwatch, and
// call migrations resolve the v6 references against the tables the earlier migrations have
// already filled, so running them in another order migrates a different database.
//
// A database that has no v6 tables — every database this server itself creates, and every database
// a v7 deployment already migrated — is left exactly as it was: each migration probes its own
// v6 table, and a table that is not there ends that migration with nothing said and nothing
// done. See the note on `migrateGroups` for what the probe does, and does not, look at.
//
// `progress` receives every line the legacy server wrote with `log.Println` while migrating: the
// `migrating groups...` announcement of each table it found, and each per-row failure it printed
// and carried on from. The composition root (T042) routes them to the server log; a test can
// collect them. A nil `progress` is accepted and simply discards them. After the last migration,
// `progress` also receives the damage report of `reportMigrationDamage`, which the legacy server
// never wrote: it is what this server says about a database the legacy server already migrated.
//
// A failure the legacy migration *returned* is returned here, wrapped the way `Database.migrate`
// wrapped it (`database.migrate: migration.migrateGroups: …`). A failure it *printed* is not an
// error: it goes to `progress` and the migration continues, which is legacy behavior this server
// has to keep because a deployment's upgrade either finishes or does not, and a per-row failure
// in one table has never stopped the others.
//
// Every value the pass moves is a bound parameter, so the legacy `escapeQuotes` call is gone and
// the statement a failure names is the parameterized one. The one place that changes which rows a
// deployment keeps: the legacy migration escaped some of the text it wrote into its statements and
// not the system blacklist, the system and talkgroup color, the log level, the dirwatch type, or the
// call audio type, so a single quote in one of those columns made its `INSERT` a syntax error and
// the row was lost. Those rows migrate here. See Q32 of
// [decision-preserved-quirks.md](../../../../docs/specs/FEAT-003-server-ng-refactor/decision-preserved-quirks.md).
//
// The `server/…` names in this file are the legacy server's own files, which are not in this
// repository. They are named so a reader can see which behavior is being mirrored, and they resolve
// at git tree 02ae33d4651fee4c46a93bdcf574d0aead89e542, recorded in
// docs/specs/FEAT-005-server-ng-cutover/spec.md.
func Migrate(ctx context.Context, db *sql.DB, dialect Dialect, progress func(message string)) error {
	if progress == nil {
		progress = func(string) {}
	}

	if err := ApplySchema(ctx, db, dialect); err != nil {
		// The schema step keeps the name `ApplySchema` gave it. The legacy server wrapped it in
		// `database.migrate:` too, but that wrapper only ever renamed a failure that already
		// names the statement it failed on.
		return err
	}

	s := newStore(db, dialect)

	for _, migrate := range []func(context.Context, store, func(string)) error{
		migrateGroups,
		migrateTags,
		migrateSystems,
		migrateTalkgroups,
		migrateUnits,
		migrateOptions,
		migrateMeta,
		migrateLogs,
		migrateDownstreams,
		migrateDirwatches,
		migrateCalls,
		migrateApikeys,
		migrateAccesses,
	} {
		if err := migrate(ctx, s, progress); err != nil {
			return operationError("database", "migrate", err)
		}
	}

	// The pass is over, and the database is what it is. A database the legacy server already migrated
	// can hold damage this pass will not touch, because the v6 rows it was read from are gone; reading
	// what is left and saying what it is belongs at the end of the startup pass, before the server
	// starts answering from the result.
	reportMigrationDamage(ctx, s, progress)

	return nil
}

// reportFailure hands a per-row migration failure to `progress`, in the shape the legacy
// `log.Println(formatError(err, query))` line had. The statement it names is the parameterized one,
// as `statementError` documents.
//
// It used to have a call site that handed it a nil error, because the legacy migration it mirrored did:
// the legacy error formatter called `Error()` on that nil and took the server down, where this rendered it
// as `%!s(<nil>)` and carried on. The repaired calls migration reports only failures it actually has, so
// no call site hands it a nil error.
func reportFailure(progress func(message string), section, operation string, err error, query string) {
	progress(statementError(section, operation, err, query).Error())
}

// migrateGroups mirrors `migrateGroups` in `server/migrations.go`.
//
// The probe every legacy migration starts with is worth reading carefully, because it decides
// "is there anything to migrate" by asking a question whose answer it throws away: it runs
// `SELECT COUNT(*)` on the v6 table and looks only at whether the query could be run at all. A
// v6 table that exists but holds no rows therefore *is* migrated — announced, opened a
// transaction for, and dropped — and a v6 table that is not there is silently skipped. The
// difference is invisible in the end state, which is why it is easy to mistake the probe for a
// row count. It is not one.
//
// The rows are re-ordered by label and given a display order of 1..n, while keeping their v6
// identifiers, so a group keeps the identity its talkgroups refer to.
func migrateGroups(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateGroups"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerGroups"`); err != nil {
		return nil
	}

	progress("migrating groups...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query := `SELECT "_id", "label" FROM "rdioScannerGroups"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	type group struct {
		id    int64
		label string
	}

	groups := []group{}

	for rows.Next() {
		var (
			id    sql.NullInt32
			label sql.NullString
		)

		if err := rows.Scan(&id, &label); err != nil {
			continue
		}

		if !id.Valid {
			continue
		}

		g := group{id: int64(id.Int32)}

		if label.Valid {
			g.label = label.String
		}

		groups = append(groups, g)
	}

	rows.Close()

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].label < groups[j].label
	})

	for i, g := range groups {
		query = `INSERT INTO "groups" ("groupId", "label", "order") VALUES (?, ?, ?)`

		if _, err := tx.exec(ctx, query, g.id, g.label, i+1); err != nil {
			reportFailure(progress, section, operation, err, query)
		}
	}

	query = `DROP TABLE "rdioScannerGroups"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateTags mirrors `migrateTags` in `server/migrations.go`:
// the same probe, the same sort by label, the same 1..n display order, the same v6 identifiers.
func migrateTags(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateTags"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerTags"`); err != nil {
		return nil
	}

	progress("migrating tags...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query := `SELECT "_id", "label" FROM "rdioScannerTags"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	type tag struct {
		id    int64
		label string
	}

	tags := []tag{}

	for rows.Next() {
		var (
			id    sql.NullInt32
			label sql.NullString
		)

		if err := rows.Scan(&id, &label); err != nil {
			continue
		}

		if !id.Valid {
			continue
		}

		t := tag{id: int64(id.Int32)}

		if label.Valid {
			t.label = label.String
		}

		tags = append(tags, t)
	}

	rows.Close()

	sort.Slice(tags, func(i, j int) bool {
		return tags[i].label < tags[j].label
	})

	for i, t := range tags {
		query = `INSERT INTO "tags" ("tagId", "label", "order") VALUES (?, ?, ?)`

		if _, err := tx.exec(ctx, query, t.id, t.label, i+1); err != nil {
			reportFailure(progress, section, operation, err, query)
		}
	}

	query = `DROP TABLE "rdioScannerTags"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateSystems mirrors `migrateSystems` in `server/migrations.go`.
//
// The v6 system's `id` column is its radio-system reference — the number a call upload names —
// and it becomes `systemRef`; the v6 `_id` becomes `systemId`, which is what every later
// migration resolves the v6 references against. The v6 blacklist is stored as a JSON array and
// the legacy migration stores it with the brackets removed, which is the shape the v7 blacklist
// is kept in.
func migrateSystems(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateSystems"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerSystems"`); err != nil {
		return nil
	}

	progress("migrating systems...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query := `SELECT "_id", "autoPopulate", "blacklists", "id", "label", "led", "order" FROM "rdioScannerSystems"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			systemID     sql.NullInt64
			autoPopulate sql.NullBool
			blacklists   sql.NullString
			systemRef    sql.NullInt32
			label        sql.NullString
			led          sql.NullString
			order        sql.NullInt32
		)

		if err := rows.Scan(&systemID, &autoPopulate, &blacklists, &systemRef, &label, &led, &order); err != nil {
			continue
		}

		if !systemID.Valid {
			continue
		}

		var (
			populate  bool
			blacklist string
			name      string
			color     string
			rank      int64
			ref       int64
		)

		if autoPopulate.Valid {
			populate = autoPopulate.Bool
		}

		if blacklists.Valid {
			blacklist = strings.ReplaceAll(strings.ReplaceAll(blacklists.String, "[", ""), "]", "")
		}

		if label.Valid {
			name = label.String
		}

		if led.Valid {
			color = led.String
		}

		if order.Valid {
			rank = int64(order.Int32)
		}

		if systemRef.Valid {
			ref = int64(systemRef.Int32)
		}

		query = `INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "label", "led", "order", "systemRef") VALUES (?, ?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, systemID.Int64, populate, blacklist, name, color, rank, ref); err != nil {
			reportFailure(progress, section, operation, err, query)
		}
	}

	query = `DROP TABLE "rdioScannerSystems"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateTalkgroups mirrors `migrateTalkgroups` in `server/migrations.go`.
//
// The v6 talkgroup names its system by the v6 system's `id` — its radio-system reference — and
// the migration resolves that against the `systems` table the systems migration has already
// filled, so a talkgroup whose system is not in that table is skipped without a word and is then
// dropped with the v6 table. A talkgroup whose group is 0 — which is what v6 stored for a
// talkgroup in no group — reaches the `talkgroupGroups` insert with 0, fails the foreign key, and
// is reported per row while the talkgroup itself stays.
func migrateTalkgroups(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateTalkgroups"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerTalkgroups"`); err != nil {
		return nil
	}

	progress("migrating talkgroups...")

	systems := map[int64]int64{}

	query := `SELECT "systemId", "systemRef" FROM "systems"`

	rows, err := s.query(ctx, query)
	if err != nil {
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			systemID  sql.NullInt64
			systemRef sql.NullInt64
		)

		if err := rows.Scan(&systemID, &systemRef); err != nil {
			continue
		}

		if systemID.Valid && systemRef.Valid {
			systems[systemRef.Int64] = systemID.Int64
		}
	}

	rows.Close()

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query = `SELECT "_id", "frequency", "groupId", "id", "label", "led", "name", "order", "systemId", "tagId" FROM "rdioScannerTalkgroups"`

	rows, err = tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			talkgroupID  sql.NullInt64
			frequency    sql.NullInt32
			groupID      sql.NullInt64
			talkgroupRef sql.NullInt32
			label        sql.NullString
			led          sql.NullString
			name         sql.NullString
			order        sql.NullInt32
			systemID     sql.NullInt64
			tagID        sql.NullInt64
		)

		if err := rows.Scan(&talkgroupID, &frequency, &groupID, &talkgroupRef, &label, &led, &name, &order, &systemID, &tagID); err != nil {
			continue
		}

		if !talkgroupID.Valid {
			continue
		}

		// The legacy migration keeps the group in a slice and then reads its first element
		// unguarded, so a talkgroup whose `groupId` column is NULL takes the migration down with
		// an index-out-of-range panic. The v6 table declares the column `not null`, which is the
		// only reason that is unreachable. Reproduced rather than guarded.
		groupIDs := []int64{}

		if groupID.Valid {
			groupIDs = append(groupIDs, groupID.Int64)
		}

		if !systemID.Valid || systems[systemID.Int64] == 0 {
			continue
		}

		var (
			freq  int64
			text  string
			color string
			named string
			rank  int64
			tag   int64
			ref   int64
		)

		if frequency.Valid {
			freq = int64(frequency.Int32)
		}

		if label.Valid {
			text = label.String
		}

		if led.Valid {
			color = led.String
		}

		if name.Valid {
			named = name.String
		}

		if order.Valid {
			rank = int64(order.Int32)
		}

		if tagID.Valid {
			tag = tagID.Int64
		}

		if talkgroupRef.Valid {
			ref = int64(talkgroupRef.Int32)
		}

		query = `INSERT INTO "talkgroups" ("talkgroupId", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, talkgroupID.Int64, freq, text, color, named, rank, systems[systemID.Int64], tag, ref); err == nil {
			query = `INSERT INTO "talkgroupGroups" ("groupId", "talkgroupId") VALUES (?, ?)`

			if _, err := tx.exec(ctx, query, groupIDs[0], talkgroupID.Int64); err != nil {
				reportFailure(progress, section, operation, err, query)
			}

		} else {
			reportFailure(progress, section, operation, err, query)
		}
	}

	query = `DROP TABLE "rdioScannerTalkgroups"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateUnits mirrors `migrateUnits` in `server/migrations.go`.
//
// The v6 unit names its system by the v6 system's `id`, resolved the same way the talkgroups
// migration resolves it, so a unit whose system is not in the `systems` table is skipped without
// a word and dropped with the v6 table.
func migrateUnits(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateUnits"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerUnits"`); err != nil {
		return nil
	}

	progress("migrating units...")

	systems := map[int32]int32{}

	query := `SELECT "systemId", "systemRef" FROM "systems"`

	rows, err := s.query(ctx, query)
	if err != nil {
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			systemID  sql.NullInt32
			systemRef sql.NullInt32
		)

		if err := rows.Scan(&systemID, &systemRef); err != nil {
			continue
		}

		if systemID.Valid && systemRef.Valid {
			systems[systemRef.Int32] = systemID.Int32
		}
	}

	rows.Close()

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query = `SELECT "_id", "id", "label", "order", "systemId" FROM "rdioScannerUnits"`

	rows, err = tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			unitID   sql.NullInt64
			unitRef  sql.NullInt32
			label    sql.NullString
			order    sql.NullInt32
			systemID sql.NullInt32
		)

		if err := rows.Scan(&unitID, &unitRef, &label, &order, &systemID); err != nil {
			continue
		}

		if !unitID.Valid {
			continue
		}

		if !systemID.Valid || systems[systemID.Int32] == 0 {
			continue
		}

		unit := struct {
			label   string
			order   int64
			unitRef int64
		}{}

		if label.Valid {
			unit.label = label.String
		}

		if order.Valid {
			unit.order = int64(order.Int32)
		}

		if unitRef.Valid {
			unit.unitRef = int64(unitRef.Int32)
		}

		// The v6 `id` column is the radio's own number, and it is what `callUnits.unitRef` and the call
		// migration's unit lookup go looking for. The legacy insert named `unit.Id` — the identity
		// `NewUnit()` never gave the unit — in this position instead, so every migrated unit landed with
		// `unitRef` 0 and the number its row was read for was thrown away. Repaired rather than
		// preserved; see docs/specs/FEAT-004-legacy-defect-repairs/decision-migration-unit-references.md.
		query = `INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef") VALUES (?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, unitID.Int64, unit.label, unit.order, systems[systemID.Int32], unit.unitRef); err != nil {
			reportFailure(progress, section, operation, err, query)
		}
	}

	query = `DROP TABLE "rdioScannerUnits"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateOptions mirrors `migrateOptions` in `server/migrations.go`.
//
// The v6 configuration table is a key/value table whose `options` row holds the whole options
// section as one JSON blob. The migration unpacks a fixed list of names out of that blob, in the
// order the legacy source lists them, and only when the value has the type the legacy source
// expects; anything else the blob holds is dropped. Every other row of the v6 table is copied
// across as it stands, which is how the persisted `secret` survives.
//
// The unpacked values are stored as the JSON text of the value, so a string option is stored
// with its quotes — which is what the legacy options reader unmarshals.
func migrateOptions(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateOptions"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerConfigs"`); err != nil {
		return nil
	}

	progress("migrating options...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	boolean := func(m map[string]any, key string) (string, bool) {
		v, ok := m[key].(bool)
		if !ok {
			return "", false
		}

		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}

		return string(b), true
	}

	number := func(m map[string]any, key string) (string, bool) {
		v, ok := m[key].(float64)
		if !ok {
			return "", false
		}

		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}

		return string(b), true
	}

	text := func(m map[string]any, key string) (string, bool) {
		v, ok := m[key].(string)
		if !ok {
			return "", false
		}

		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}

		return string(b), true
	}

	unpacked := []struct {
		key     string
		extract func(map[string]any, string) (string, bool)
	}{
		{"audioConversion", boolean},
		{"autoPopulate", boolean},
		{"branding", text},
		{"dimmerDelay", number},
		{"disableDuplicateDetection", boolean},
		{"duplicateDetectionTimeFrame", number},
		{"email", text},
		{"keypadBeeps", text},
		{"maxClients", number},
		{"playbackGoesLive", boolean},
		{"pruneDays", number},
		{"showListenersCount", boolean},
		{"sortTalkgroups", boolean},
		{"time12hFormat", boolean},
	}

	query := `SELECT "key", "val" FROM "rdioScannerConfigs"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			key   sql.NullString
			value sql.NullString
		)

		if err := rows.Scan(&key, &value); err != nil {
			continue
		}

		if !key.Valid || !value.Valid {
			continue
		}

		if key.String != "options" {
			query = `INSERT INTO "options" ("key", "value") VALUES (?, ?)`

			if _, err := tx.exec(ctx, query, key.String, value.String); err != nil {
				reportFailure(progress, section, operation, err, query)
			}

			continue
		}

		var blob map[string]any

		if err := json.Unmarshal([]byte(value.String), &blob); err != nil {
			continue
		}

		for _, option := range unpacked {
			stored, ok := option.extract(blob, option.key)
			if !ok {
				continue
			}

			query = `INSERT INTO "options" ("key", "value") VALUES (?, ?)`

			if _, err := tx.exec(ctx, query, option.key, stored); err != nil {
				reportFailure(progress, section, operation, err, query)
			}
		}
	}

	query = `DROP TABLE "rdioScannerConfigs"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateMeta mirrors `migrateMeta` in `server/migrations.go`:
// the v6 migration bookkeeping table has no v7 counterpart, so it is dropped, and unlike every
// other migration its failure is returned rather than printed — the legacy server stops there,
// with the v6 tables the later migrations would have consumed still in place.
func migrateMeta(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateMeta"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerMeta"`); err != nil {
		return nil
	}

	progress("migrating meta...")

	query := `DROP TABLE "rdioScannerMeta"`
	if _, err := s.exec(ctx, query); err != nil {
		return statementError(section, operation, err, query)
	}

	return nil
}

// migrateLogs mirrors `migrateLogs` in `server/migrations.go`.
//
// The v6 log timestamp is a datetime; the v7 one is Unix milliseconds, which is what every
// timestamp in this schema is. A row with no timestamp, no level, or no message is skipped
// silently, as the legacy migration skips it.
func migrateLogs(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateLogs"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerLogs"`); err != nil {
		return nil
	}

	progress("migrating logs...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query := `SELECT "_id", "dateTime", "level", "message" FROM "rdioScannerLogs"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			logID    sql.NullInt32
			dateTime sql.NullTime
			level    sql.NullString
			message  sql.NullString
		)

		if err := rows.Scan(&logID, &dateTime, &level, &message); err != nil {
			continue
		}

		if !logID.Valid {
			continue
		}

		if !dateTime.Valid {
			continue
		}

		if !level.Valid || len(level.String) == 0 {
			continue
		}

		if !message.Valid || len(message.String) == 0 {
			continue
		}

		query = `INSERT INTO "logs" ("logId", "level", "message", "timestamp") VALUES (?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, int64(logID.Int32), level.String, message.String, dateTime.Time.UnixMilli()); err != nil {
			reportFailure(progress, section, operation, err, query)
		}
	}

	query = `DROP TABLE "rdioScannerLogs"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateDownstreams mirrors `migrateDownstreams` in `server/migrations.go`.
// A downstream with no apikey, no scope, or no url is skipped silently, as the legacy migration
// skips it: those three columns are the whole row's meaning.
func migrateDownstreams(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateDownstreams"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerDownstreams"`); err != nil {
		return nil
	}

	progress("migrating downstreams...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query := `SELECT "_id", "apiKey", "disabled", "order", "systems", "url" FROM "rdioScannerDownstreams"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			downstreamID sql.NullInt64
			apikey       sql.NullString
			disabled     sql.NullBool
			order        sql.NullInt32
			systems      sql.NullString
			url          sql.NullString
		)

		if err := rows.Scan(&downstreamID, &apikey, &disabled, &order, &systems, &url); err != nil {
			continue
		}

		if !downstreamID.Valid {
			continue
		}

		if !apikey.Valid || len(apikey.String) == 0 {
			continue
		}

		if !systems.Valid || len(systems.String) == 0 {
			continue
		}

		if !url.Valid || len(url.String) == 0 {
			continue
		}

		var (
			isDisabled bool
			rank       int64
		)

		if disabled.Valid {
			isDisabled = disabled.Bool
		}

		if order.Valid {
			rank = int64(order.Int32)
		}

		query = `INSERT INTO "downstreams" ("downstreamId", "apikey", "disabled", "order", "systems", "url") VALUES (?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, downstreamID.Int64, apikey.String, isDisabled, rank, systems.String, url.String); err != nil {
			reportFailure(progress, section, operation, err, query)
		}
	}

	query = `DROP TABLE "rdioScannerDownstreams"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateDirwatches mirrors `migrateDirwatches` in `server/migrations.go`.
//
// Two legacy details are reproduced here rather than tidied away.
//
// The lookup query is a `LEFT JOIN` with no `ON` clause, which every backend this server supports
// treats as a cross product of systems and talkgroups. It is what fills the two lookup maps, and
// it only records a system when the row it came from had a talkgroup too: a system with no
// talkgroups is not in the maps at all, and its dirwatches then resolve to nothing.
//
// The dirwatch rows are scanned into two of the four variables the lookup query used, so the
// other two keep whatever the lookup query left in them. Whether a dirwatch's talkgroup is
// resolved at all depends on `talkgroupID`, which the dirwatch rows never scan into — see
// `TestMigrateDirwatchesResolveNothingWhenNoTalkgroupWasMigrated`.
//
// The three statements that name the v6 table do not agree on its spelling. The probe and the read
// ask for `rdioScannerDirwatches`, the drop names `RdioScannerDirWatches`, and the table v6 created
// is `rdioScannerDirWatches`. sqlite matches table names whatever their case, so a v6 sqlite
// deployment loses nothing; a backend that does not match them case-sensitively — MySQL on a
// case-sensitive filesystem — fails the probe, and its v6 dirwatches are neither migrated nor
// dropped, on that start or on any later one.
func migrateDirwatches(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateDirwatches"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerDirwatches"`); err != nil {
		return nil
	}

	progress("migrating dirwatches...")

	var (
		systems    = map[int32]int32{}
		talkgroups = map[int32]map[int32]int32{}

		systemID     sql.NullInt32
		systemRef    sql.NullInt32
		talkgroupID  sql.NullInt32
		talkgroupRef sql.NullInt32
	)

	query := `SELECT s."systemId", s."systemRef", t."talkgroupId", t."talkgroupRef" FROM "systems" AS s LEFT JOIN "talkgroups" AS t`

	rows, err := s.query(ctx, query)
	if err != nil {
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		if err := rows.Scan(&systemID, &systemRef, &talkgroupID, &talkgroupRef); err != nil {
			continue
		}

		if systemID.Valid && systemRef.Valid && talkgroupID.Valid && talkgroupRef.Valid {
			if systems[systemRef.Int32] == 0 {
				systems[systemRef.Int32] = systemID.Int32
				talkgroups[systemRef.Int32] = map[int32]int32{}
			}

			talkgroups[systemRef.Int32][talkgroupRef.Int32] = talkgroupID.Int32
		}
	}

	rows.Close()

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query = `SELECT "_id", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "systemId", "talkgroupId", "type" FROM "rdioScannerDirwatches"`

	rows, err = tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			dirwatchID  sql.NullInt64
			delay       sql.NullInt32
			deleteAfter sql.NullBool
			directory   sql.NullString
			disabled    sql.NullBool
			extension   sql.NullString
			frequency   sql.NullInt32
			mask        sql.NullString
			order       sql.NullInt32
			kind        sql.NullString
		)

		if err := rows.Scan(&dirwatchID, &delay, &deleteAfter, &directory, &disabled, &extension, &frequency, &mask, &order, &systemRef, &talkgroupRef, &kind); err != nil {
			continue
		}

		if !dirwatchID.Valid {
			continue
		}

		if !directory.Valid || len(directory.String) == 0 {
			continue
		}

		var refSystem, refTalkgroup any

		if systemRef.Valid && systems[systemRef.Int32] > 0 {
			refSystem = systems[systemRef.Int32]
		}

		if talkgroupID.Valid {
			refTalkgroup = talkgroups[systemRef.Int32][talkgroupRef.Int32]
		}

		var (
			wait       int64
			drops      bool
			isDisabled bool
			suffix     string
			freq       int64
			filter     string
			rank       int64
			parser     string
		)

		if delay.Valid {
			wait = int64(delay.Int32)
		}

		if deleteAfter.Valid {
			drops = deleteAfter.Bool
		}

		if disabled.Valid {
			isDisabled = disabled.Bool
		}

		if extension.Valid {
			suffix = extension.String
		}

		if frequency.Valid {
			freq = int64(frequency.Int32)
		}

		if mask.Valid && len(mask.String) > 0 {
			filter = mask.String
		}

		if order.Valid {
			rank = int64(order.Int32)
		}

		if kind.Valid && len(kind.String) > 0 {
			parser = kind.String
		}

		// An unresolved reference is passed on as it is: the legacy migration formatted it into
		// the statement as `%!d(<nil>)`, which the database rejected as a syntax error, and the
		// row was reported and lost. Parameterized, it is a NULL in a NOT NULL column, which the
		// database rejects as a constraint failure. Same row lost, same line reported; only the
		// database's complaint differs.
		query = `INSERT INTO "dirwatches" ("dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "systemId", "talkgroupId", "type") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, dirwatchID.Int64, wait, drops, directory.String, isDisabled, suffix, freq, filter, rank, refSystem, refTalkgroup, parser); err != nil {
			reportFailure(progress, section, operation, err, query)
		}
	}

	query = `DROP TABLE "RdioScannerDirWatches"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateCalls mirrors `migrateCalls` in `server/migrations.go`.
//
// A call is migrated only when its audio, its timestamp, and both of its aggregate references
// resolve against the tables the earlier migrations filled; anything that does not resolve is
// skipped without a word and dropped with the v6 table. The v6 call has no site, so the migrated
// call is stored with site reference 0, as the legacy migration stores it.
//
// The call's frequencies, patches, and source units are written from the JSON the v6 row holds.
// The `frequencies` column is checked for a non-empty *string*, so a v6 call whose frequency list
// is the empty array takes the list branch and stores no frequency at all, even when its single
// `frequency` column names one; the `sources` column is checked for the empty array as well, so
// an empty source list does fall through to the single `source` column.
func migrateCalls(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateCalls"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerCalls"`); err != nil {
		return nil
	}

	progress("migrating calls...")

	var (
		systems    = map[int32]int32{}
		talkgroups = map[int32]map[int32]int32{}
	)

	query := `SELECT s."systemId", s."systemRef", t."talkgroupId", t."talkgroupRef" FROM "systems" AS s LEFT JOIN "talkgroups" AS t`

	rows, err := s.query(ctx, query)
	if err != nil {
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			systemID     sql.NullInt32
			systemRef    sql.NullInt32
			talkgroupID  sql.NullInt32
			talkgroupRef sql.NullInt32
		)

		if err := rows.Scan(&systemID, &systemRef, &talkgroupID, &talkgroupRef); err != nil {
			continue
		}

		if systemID.Valid && systemRef.Valid && talkgroupID.Valid && talkgroupRef.Valid {
			if systems[systemRef.Int32] == 0 {
				systems[systemRef.Int32] = systemID.Int32
				talkgroups[systemRef.Int32] = map[int32]int32{}
			}

			talkgroups[systemRef.Int32][talkgroupRef.Int32] = talkgroupID.Int32
		}
	}

	rows.Close()

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query = `SELECT "id", "audio", "audioName", "audioType", "dateTime", "frequencies", "frequency", "patches", "source", "sources", "system", "talkgroup" FROM "rdioScannerCalls"`

	rows, err = tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			callID       sql.NullInt32
			audio        sql.NullString
			audioName    sql.NullString
			audioType    sql.NullString
			dateTime     sql.NullTime
			frequencies  sql.NullString
			frequency    sql.NullInt32
			patches      sql.NullString
			source       sql.NullInt32
			sources      sql.NullString
			systemRef    sql.NullInt32
			talkgroupRef sql.NullInt32
		)

		if err := rows.Scan(&callID, &audio, &audioName, &audioType, &dateTime, &frequencies, &frequency, &patches, &source, &sources, &systemRef, &talkgroupRef); err != nil {
			continue
		}

		if !callID.Valid {
			continue
		}

		if !audio.Valid || len(audio.String) == 0 {
			continue
		}

		if !dateTime.Valid {
			continue
		}

		if !systemRef.Valid || systems[systemRef.Int32] == 0 {
			continue
		}

		if !talkgroupRef.Valid || talkgroups[systemRef.Int32][talkgroupRef.Int32] == 0 {
			continue
		}

		var filename, mime string

		if audioName.Valid {
			filename = audioName.String
		}

		if audioType.Valid {
			mime = audioType.String
		}

		query = `INSERT INTO "calls" ("callId", "audio", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES (?, ?, ?, ?, 0, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, int64(callID.Int32), []byte(audio.String), filename, mime, systems[systemRef.Int32], talkgroups[systemRef.Int32][talkgroupRef.Int32], dateTime.Time.UnixMilli()); err != nil {
			reportFailure(progress, section, operation, err, query)

			continue
		}

		if frequencies.Valid && len(frequencies.String) > 0 {
			var decoded any

			if err := json.Unmarshal([]byte(frequencies.String), &decoded); err == nil {
				if entries, ok := decoded.([]any); ok {
					for _, entry := range entries {
						m, ok := entry.(map[string]any)
						if !ok {
							continue
						}

						var errors, freq, spikes int64
						var offset float64

						if v, ok := m["errorCount"].(float64); ok {
							errors = int64(v)
						}

						if v, ok := m["freq"].(float64); ok {
							freq = int64(v)
						}

						if v, ok := m["pos"].(float64); ok {
							offset = v
						}

						if v, ok := m["spikeCount"].(float64); ok {
							spikes = int64(v)
						}

						query = `INSERT INTO "callFrequencies" ("callId", "errors", "frequency", "offset", "spikes") VALUES (?, ?, ?, ?, ?)`

						if _, err := tx.exec(ctx, query, int64(callID.Int32), errors, freq, offset, spikes); err != nil {
							reportFailure(progress, section, operation, err, query)
						}
					}
				}
			}

		} else if frequency.Valid && frequency.Int32 > 0 {
			query = `INSERT INTO "callFrequencies" ("callId", "errors", "frequency", "offset", "spikes") VALUES (?, 0, ?, 0, 0)`

			if _, err := tx.exec(ctx, query, int64(callID.Int32), int64(frequency.Int32)); err != nil {
				reportFailure(progress, section, operation, err, query)
			}
		}

		if patches.Valid && len(patches.String) > 0 {
			var decoded any

			if err := json.Unmarshal([]byte(patches.String), &decoded); err == nil {
				if refs, ok := decoded.([]any); ok {
					for _, ref := range refs {
						v, ok := ref.(float64)
						if !ok {
							continue
						}

						patched := talkgroups[systemRef.Int32][int32(v)]
						if patched <= 0 {
							continue
						}

						query = `INSERT INTO "callPatches" ("callId", "talkgroupId") VALUES (?, ?)`

						if _, err := tx.exec(ctx, query, int64(callID.Int32), patched); err != nil {
							reportFailure(progress, section, operation, err, query)
						}
					}
				}
			}
		}

		if sources.Valid && len(sources.String) > 0 && sources.String != "[]" {
			var decoded any

			if err := json.Unmarshal([]byte(sources.String), &decoded); err == nil {
				if entries, ok := decoded.([]any); ok {
					for _, entry := range entries {
						m, ok := entry.(map[string]any)
						if !ok {
							continue
						}

						src, ok := m["src"].(float64)
						if !ok || src <= 0 {
							continue
						}

						query = `INSERT INTO "callUnits" ("callId", "offset", "unitRef") VALUES (?, ?, ?)`

						if _, err := tx.exec(ctx, query, int64(callID.Int32), m["pos"], int64(src)); err != nil {
							reportFailure(progress, section, operation, err, query)
						}
					}
				}
			}

		} else if source.Valid && source.Int32 > 0 {
			// The unit the call came from is looked up by the number the call names, in the system the
			// call belongs to. A number no unit holds gets a unit of its own, once: the next call naming
			// the same number in the same system finds the unit this one made. The `callUnits` row is
			// written whether the unit was already there or was just created.
			//
			// The legacy migration wrote the row only on the branch where the unit insert had failed, and
			// reported the insert from the branch where it had succeeded — with a nil error its formatter
			// then dereferenced, which is what took the legacy upgrade down. Repaired rather than
			// preserved; see docs/specs/FEAT-004-legacy-defect-repairs/decision-migration-unit-references.md.
			var count int

			query = `SELECT COUNT(*) FROM "units" WHERE "systemId" = ? AND "unitRef" = ?`

			if err := tx.queryRow(ctx, query, systems[systemRef.Int32], int64(source.Int32)).Scan(&count); err != nil {
				reportFailure(progress, section, operation, err, query)

				continue
			}

			if count == 0 {
				query = `INSERT INTO "units" ("label", "systemId", "unitRef") VALUES (?, ?, ?)`

				// The label is the number as text, which is what the legacy statement — a number
				// interpolated straight into a text column — leaves in that column.
				if _, err := tx.exec(ctx, query, strconv.FormatInt(int64(source.Int32), 10), systems[systemRef.Int32], int64(source.Int32)); err != nil {
					reportFailure(progress, section, operation, err, query)

					continue
				}
			}

			query = `INSERT INTO "callUnits" ("callId", "offset", "unitRef") VALUES (?, ?, ?)`

			if _, err := tx.exec(ctx, query, int64(callID.Int32), 0, int64(source.Int32)); err != nil {
				reportFailure(progress, section, operation, err, query)
			}
		}
	}

	rows.Close()

	query = `DROP TABLE "rdioScannerCalls"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateApikeys mirrors `migrateApikeys` in `server/migrations.go`.
// Only a missing identifier skips a row: an apikey with no identity or no key is still stored,
// with the empty value the legacy migration stored.
func migrateApikeys(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateApikeys"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerApiKeys"`); err != nil {
		return nil
	}

	progress("migrating apikeys...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query := `SELECT "_id", "disabled", "ident", "key", "order", "systems" FROM "rdioScannerApiKeys"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			apikeyID sql.NullInt64
			disabled sql.NullBool
			ident    sql.NullString
			key      sql.NullString
			order    sql.NullInt32
			systems  sql.NullString
		)

		if err := rows.Scan(&apikeyID, &disabled, &ident, &key, &order, &systems); err != nil {
			continue
		}

		if !apikeyID.Valid {
			continue
		}

		var (
			isDisabled bool
			name       string
			value      string
			rank       int64
			scope      string
		)

		if disabled.Valid {
			isDisabled = disabled.Bool
		}

		if ident.Valid {
			name = ident.String
		}

		if key.Valid {
			value = key.String
		}

		if order.Valid {
			rank = int64(order.Int32)
		}

		if systems.Valid {
			scope = systems.String
		}

		query = `INSERT INTO "apikeys" ("apikeyId", "disabled", "ident", "key", "order", "systems") VALUES (?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, apikeyID.Int64, isDisabled, name, value, rank, scope); err != nil {
			reportFailure(progress, section, operation, err, query)
		}
	}

	query = `DROP TABLE "rdioScannerApiKeys"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateAccesses mirrors `migrateAccesses` in `server/migrations.go`.
//
// `accesses.expiration` is the one timestamp in this schema that is not Unix milliseconds: the
// legacy server stores it in whole seconds, both here and in its own access writes, and the
// migration converts the v6 datetime to seconds to match.
func migrateAccesses(ctx context.Context, s store, progress func(string)) error {
	const section, operation = "migration", "migrateAccesses"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "rdioScannerAccesses"`); err != nil {
		return nil
	}

	progress("migrating accesses...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	query := `SELECT "_id", "code", "expiration", "ident", "limit", "order", "systems" FROM "rdioScannerAccesses"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			accessID   sql.NullInt64
			code       sql.NullString
			expiration sql.NullTime
			ident      sql.NullString
			limit      sql.NullInt32
			order      sql.NullInt32
			systems    sql.NullString
		)

		if err := rows.Scan(&accessID, &code, &expiration, &ident, &limit, &order, &systems); err != nil {
			continue
		}

		if !accessID.Valid {
			continue
		}

		if !code.Valid || len(code.String) == 0 {
			continue
		}

		var (
			expires int64
			name    string
			max     int64
			rank    int64
			scope   string
		)

		if expiration.Valid {
			expires = expiration.Time.Unix()
		}

		if ident.Valid {
			name = ident.String
		}

		if limit.Valid {
			max = int64(limit.Int32)
		}

		if order.Valid {
			rank = int64(order.Int32)
		}

		if systems.Valid {
			scope = systems.String
		}

		query = `INSERT INTO "accesses" ("accessId", "code", "expiration", "ident", "limit", "order", "systems") VALUES (?, ?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, accessID.Int64, code.String, expires, name, max, rank, scope); err != nil {
			reportFailure(progress, section, operation, err, query)
		}
	}

	query = `DROP TABLE "rdioScannerAccesses"`
	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, section, operation, err, query)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// reportMigrationDamage reads the two shapes the legacy migration pass leaves in a database that pass
// has already run over, and writes what it finds to `progress`. It reads only: it changes no row,
// invents no reference, and a failure to read is reported and carried on from, the way a per-row
// migration failure is, because a diagnostic is not a reason to refuse to start.
//
// The two shapes are the two the legacy statements of
// `server/migrations.go` write. A unit at `unitRef` 0 is a unit whose
// v6 reference the legacy unit insert threw away; the number it was migrated from is not in this
// database any more, and inventing one would hand a deployment a radio reference nobody can check. A
// unit labelled with its own number is a unit the migration made for a number it could not find rather
// than a unit a deployment named.
//
// Neither is repaired here, because neither can be. See
// [decision-migrated-database-repair.md](../../../../docs/specs/FEAT-004-legacy-defect-repairs/decision-migrated-database-repair.md).
//
// The report is console-only: it writes no row and no log entry. A startup notice is not something the
// two servers are asked to agree about, and a log entry would put a difference in the `logs` table the
// parity fixtures are read from.
//
// The comparison that finds the second shape is done here rather than in the query, because `label` is
// text and `unitRef` is a number, and the three dialects this package serves cast between them three
// different ways.
func reportMigrationDamage(ctx context.Context, s store, progress func(string)) {
	const section, operation = "migration", "damage"

	query := `SELECT u."unitId", u."label", u."unitRef", s."label", s."systemRef" FROM "units" AS u LEFT JOIN "systems" AS s ON s."systemId" = u."systemId" ORDER BY u."unitId"`

	rows, err := s.query(ctx, query)
	if err != nil {
		progress(statementError(section, operation, err, query).Error())

		return
	}

	defer rows.Close()

	lost, made := []string{}, []string{}

	for rows.Next() {
		var (
			unitID      sql.NullInt64
			label       sql.NullString
			unitRef     sql.NullInt64
			systemLabel sql.NullString
			systemRef   sql.NullInt64
		)

		if err := rows.Scan(&unitID, &label, &unitRef, &systemLabel, &systemRef); err != nil {
			continue
		}

		line := fmt.Sprintf("migration.damage:   unit %d %q of %s", unitID.Int64, label.String, damagedSystem(systemLabel, systemRef))

		switch {
		case unitRef.Int64 == 0:
			lost = append(lost, line)

		case label.String == strconv.FormatInt(unitRef.Int64, 10):
			made = append(made, line)
		}
	}

	reportDamaged(progress, lost, "unit holds no radio reference", "units hold no radio reference",
		"the numbers they were migrated from are not in this database")

	reportDamaged(progress, made, "unit is labelled with its own number",
		"units are labelled with their own numbers",
		"the shape of a unit the migration created rather than a deployment named")
}

// damagedSystem names the system a unit belongs to the way an operator names one: its label, and the
// radio system reference in parentheses. A unit whose system row is gone is named by what is left.
func damagedSystem(label sql.NullString, ref sql.NullInt64) string {
	if !label.Valid || label.String == "" {
		return fmt.Sprintf("system %d", ref.Int64)
	}

	return fmt.Sprintf("%s (%d)", label.String, ref.Int64)
}

// reportDamaged writes one summary line and then one line per unit, and says nothing at all when there
// is nothing to say.
func reportDamaged(progress func(string), found []string, singular, plural, cause string) {
	if len(found) == 0 {
		return
	}

	if len(found) == 1 {
		progress(fmt.Sprintf("migration.damage: 1 %s, %s", singular, cause))
	} else {
		progress(fmt.Sprintf("migration.damage: %d %s, %s", len(found), plural, cause))
	}

	for _, line := range found {
		progress(line)
	}
}
