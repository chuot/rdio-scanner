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
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Progress is what the migration pass reports to while it runs: one line, and the level that line is
// filed under. A table the pass found and carried is a notice; a row it could not carry, and a unit the
// legacy server left damaged, are problems. The pass cannot write these as log entries itself — it is the
// pass rebuilding the log table — so the level is decided here, where the pass knows which kind of line it
// has, and not guessed afterwards from the shape of a sentence.
type Progress func(level, message string)

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
// `progress` receives every line the pass says while it runs, with the level the line is filed under: the
// `migrating groups...` announcement of each v6 table it found is a notice, and each row it could not carry
// and each unit the legacy server left damaged is a problem. A nil `progress` is accepted and simply
// discards them.
//
// `clock` is the server's clock, and it stamps every row the pass archives. A nil `clock` is accepted
// for a caller that archives nothing; the pass falls back to the wall clock, which is why the server
// itself always hands it its own.
//
// The lines are said while the pass runs rather than collected by this package, because the pass is what
// is rebuilding the log table: an entry written here is written into the table whose ids the deployment's
// own history is being moved into. The composition root prints them as they arrive, keeps them, and writes
// them once the pass has returned.
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
// the row was lost. Those rows migrate here.
//
// The `server/…` names in this file are the legacy server's own files, which are not in this
// repository. They are named so a reader can see which behavior is being mirrored, and they resolve
// at git tree 02ae33d4651fee4c46a93bdcf574d0aead89e542.
func Migrate(ctx context.Context, db *sql.DB, dialect Dialect, clock ports.Clock, progress Progress) error {
	if progress == nil {
		progress = func(_, _ string) {}
	}

	if clock == nil {
		clock = wallClock{}
	}

	if err := ApplySchema(ctx, db, dialect); err != nil {
		// The schema step keeps the name `ApplySchema` gave it. The legacy server wrapped it in
		// `database.migrate:` too, but that wrapper only ever renamed a failure that already
		// names the statement it failed on.
		return err
	}

	s := newStore(db, dialect)
	s.clock = clock

	// The log table is brought up to the shape this server writes before anything reads or moves a row:
	// the column that names the part of the server that wrote each entry, and the indexes the log search
	// is written against. It is a step of the migration pass rather than of the schema because the schema
	// is the legacy server's and has to leave a database the other server wrote exactly as it found it.
	if err := ensureLogSourceColumn(ctx, s); err != nil {
		return operationError("database", "migrate", err)
	}

	// The calls table is given the column that says how long each call's recording lasts,
	// for the same reason the log table got its source column: the schema is the legacy
	// server's, and the column a legacy-written database arrives with is the zero that
	// means never measured — no row needs a length the migration cannot know.
	if err := ensureCallAudioLengthColumn(ctx, s); err != nil {
		return operationError("database", "migrate", err)
	}

	// The archive the pass writes left-behind rows into is given the same treatment for the same
	// reason: it is a table this server owns, and the schema step is not allowed to add tables to a
	// database the other server wrote. See migration_archive.go.
	if err := ensureMigrationArchive(ctx, s); err != nil {
		return operationError("database", "migrate", err)
	}

	// The options table is given its one-row-per-key rule before any step writes
	// option rows: the duplicates a MySQL deployment's saves left behind are removed, and the
	// unique index that refuses them from then on is created
	// over the deduplicated table, not over the duplicates.
	if err := ensureOptionsKeyIndex(ctx, s); err != nil {
		return operationError("database", "migrate", err)
	}

	for _, migrate := range []func(context.Context, store, Progress) error{
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

// reportFailure hands a failure the migration met while carrying one table across to `progress`, in the
// shape every log entry this server writes has: a sentence saying which part of the upgrade could not be
// finished, then the failure's own text, which names the statement that failed.
//
// The legacy shape was `log.Println(formatError(err, query))` — the failure's text alone. That is kept,
// after the sentence, because it is the only part of the line that names the statement, and a migration
// failure an operator cannot trace to a statement is a failure they cannot fix.
//
// It used to have a call site that handed it a nil error, because the legacy migration it mirrored did:
// the legacy error formatter called `Error` on that nil and took the server down, where this rendered it
// as `%!s(<nil>)` and carried on. The repaired calls migration reports only failures it actually has, so
// no call site hands it a nil error.
func reportFailure(progress Progress, part, section, operation string, err error, query string) {
	progress(domain.LogLevelWarn, fmt.Sprintf("the migration of %s could not be finished: %s", part,
		statementError(section, operation, err, query).Error()))
}

// alreadyCarried reports whether the v8 table already holds a row with the identity the v6 row was
// read for.
//
// It is what makes a second pass over a v6 table the first pass left behind a no-op rather than a table
// full of constraint failures. Such a table survives only because some row of it could not be read, so
// the next start reads every row of it again — including the ones already written under the identity
// this pass would write them under. Those rows are not lost: they are in the v8 table, under their own
// identity. Writing them again would fail, reporting that failure would tell the operator about a damage
// that is not there, and archiving them would say a carried row was left behind.
func alreadyCarried(ctx context.Context, tx transaction, table, column string, identity any) (bool, error) {
	query := `SELECT COUNT(*) FROM "` + table + `" WHERE "` + column + `" = ?`

	var count int

	if err := tx.queryRow(ctx, query, identity).Scan(&count); err != nil {
		return false, statementError("migration", "alreadyCarried", err, query)
	}

	return count > 0, nil
}

// readV6Table opens the v6 table a migration is carrying rows out of.
//
// Where it opens that cursor depends on the backend, and the two answers are opposite.
//
// On the two server backends it reads on the pool, not on the transaction those rows are written through,
// because the row loop reads one row and writes it before moving to the next: the cursor is still open on
// the connection while the carry, the archive, and the `alreadyCarried` pre-check all run. PostgreSQL's
// driver refuses a second statement on a transaction that has a result set open on it — the read ends at
// the first row the loop carries — so a pass that reads and writes on one transaction is a pass that only
// works on the backends tolerant enough to ignore it. The legacy server has the same shape for the same
// reason: it reads with `db.Sql.Query`, collects what it read, and only then begins the transaction that
// writes (v6.6.3:server/database.go:407).
//
// On the file backend it has to read on that very transaction, because sqlite locks the file rather than
// the table, and a second connection is a second holder of the read lock. A write transaction on another
// connection carries on until its page cache fills with dirty pages and it has to flush them; the journal
// sync that flush needs waits for every reader to leave the file, and the only reader is the loop waiting
// for that write. The pass therefore stops partway through a table once the table is big enough to fill
// the cache, and every statement it then attempts waits out the connection's busy timeout before failing.
// The 100,000-call timing found exactly that: 2,000 calls holding 1 KiB of audio each was enough, the run
// never finished, and the goroutine dump named the calls insert in this package and `_pager_wait_on_lock`
// under it. Reading on the transaction is what this pass did before the server backends moved it off, and
// it is what the legacy server does: one connection reading and writing its own file cannot deadlock
// against itself.
//
// Reading outside the transaction on the server backends costs nothing the pass relies on. A v6 table is
// never written to while it is being read, the only thing that removes one is `dropV6Table`, and that runs
// after this cursor is closed. A carry that fails halfway rolls back, and the next run reads the same rows
// again.
func readV6Table(ctx context.Context, s store, tx transaction, query string) (*sql.Rows, error) {
	if s.dialect == DialectSqlite {
		return tx.query(ctx, query)
	}

	rows, err := s.query(ctx, query)
	if err != nil {
		return nil, err
	}

	return rows, nil
}

// v6DateTime reads one v6 datetime column into the instant it names, in whatever shape the driver that
// carried it hands back.
//
// MariaDB hands a `datetime` as bytes: its connection string has no `parseTime`, exactly as the legacy
// server's has none, which is why the legacy server has a `ParseDateTime` at all
// (v6.6.3:server/database.go:79). PostgreSQL hands back a Go time. sqlite hands back whatever the affinity
// of the column ended up holding, which is the one case where a v6 database holds text in a column it
// declared `datetime`. Scanning such a column straight into a time is a demand only two of the three can
// meet, and the third is the one a deployment runs on.
//
// A value that fits none of those shapes is not a datetime a v6 deployment held. The caller treats it the
// way it treats a row it could not read at all: the row's table stays where the pass found it.
func v6DateTime(value any) (sql.NullTime, error) {
	switch typed := value.(type) {
	case nil:
		return sql.NullTime{}, nil

	case time.Time:
		return sql.NullTime{Time: typed, Valid: true}, nil

	case []byte:
		value = string(typed)
	}

	when, err := parseV6DateTime(fmt.Sprintf("%v", value))
	if err != nil {
		return sql.NullTime{}, fmt.Errorf("%v is not a datetime in the shape a v6 database writes", value)
	}

	return sql.NullTime{Time: when, Valid: true}, nil
}

// v6DateTimeLayouts are the shapes a v6 datetime arrives in. The first is what MariaDB and MySQL write
// and read back (v6.6.3:server/database.go:52), and Go's parser takes the fractional seconds a v6 sqlite
// file holds after it without the layout naming them. The rest are the instants a Go server printed into
// a sqlite column: the legacy server's own format, and the one with the zone spelled the way the time
// package spells it.
var v6DateTimeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05 -07:00",
	"2006-01-02 15:04:05 -0700 MST",
}

// parseV6DateTime reads one of those shapes back as the instant it names.
func parseV6DateTime(text string) (time.Time, error) {
	for _, layout := range v6DateTimeLayouts {
		if when, err := time.Parse(layout, text); err == nil {
			return when, nil
		}
	}

	return time.Time{}, fmt.Errorf("%q is not a datetime in any of the shapes a v6 database writes", text)
}

// dropV6Table drops the v6 table a migration has just read, unless some row of it could not be read.
//
// The pass keeps a record of every row it read and could not carry, which is what lets it drop the v6
// table at the end of the table's transaction at all. A row the driver could not scan — text in a
// column sqlite declared `datetime` or `integer` and did not enforce — is a row the pass has no values
// for: it can neither carry it nor archive it. The only way such a row survives is for its table to
// survive, so the pass leaves the table where it found it and says so, and carries on with the rest of
// the upgrade rather than refusing to start over one damaged row.
//
// `unread` is set by the row loop when a scan fails; the cursor error covers a read that stopped
// before the end of the table. Both are the same case: a row the pass never saw.
func dropV6Table(ctx context.Context, tx transaction, progress Progress, part, section, operation, table string, unread bool) {
	if unread {
		progress(domain.LogLevelWarn, fmt.Sprintf("the migration left the %s table where it found it: a row of it could not be read", part))

		return
	}

	query := `DROP TABLE "` + table + `"`

	if _, err := tx.exec(ctx, query); err != nil {
		reportFailure(progress, part, section, operation, err, query)
	}
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
func migrateGroups(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateGroups", "groups"

	exists, err := tableExists(ctx, s, "rdioScannerGroups")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating groups...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerGroups", "group", "groups")

	query := `SELECT "_id", "label" FROM "rdioScannerGroups"`

	unread := false

	rows, err := readV6Table(ctx, s, tx, query)
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
			unread = true

			continue
		}

		if !id.Valid {
			if err := arch.skip(0, "a group row", "it holds no identity", map[string]any{
				"_id":   v6Int32(id),
				"label": v6Text(label),
			}); err != nil {
				tx.rollback()

				return err
			}

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
		if carried, err := alreadyCarried(ctx, tx, "groups", "groupId", g.id); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "groups" ("groupId", "label", "order") VALUES (?, ?, ?)`

		if _, err := tx.exec(ctx, query, g.id, g.label, i+1); err != nil {
			reportFailure(progress, part, section, operation, err, query)

			if err := arch.skip(g.id,
				namedRow("group", g.id, sql.NullString{String: g.label, Valid: true}), "the database refused it", map[string]any{
					"_id":   g.id,
					"label": g.label,
				}); err != nil {
				tx.rollback()

				return err
			}
		}
	}

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerGroups", unread || rows.Err() != nil)

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateTags mirrors `migrateTags` in `server/migrations.go`:
// the same probe, the same sort by label, the same 1..n display order, the same v6 identifiers.
func migrateTags(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateTags", "tags"

	exists, err := tableExists(ctx, s, "rdioScannerTags")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating tags...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerTags", "tag", "tags")

	query := `SELECT "_id", "label" FROM "rdioScannerTags"`

	unread := false

	rows, err := readV6Table(ctx, s, tx, query)
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
			unread = true

			continue
		}

		if !id.Valid {
			if err := arch.skip(0, "a tag row", "it holds no identity", map[string]any{
				"_id":   v6Int32(id),
				"label": v6Text(label),
			}); err != nil {
				tx.rollback()

				return err
			}

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
		if carried, err := alreadyCarried(ctx, tx, "tags", "tagId", t.id); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "tags" ("tagId", "label", "order") VALUES (?, ?, ?)`

		if _, err := tx.exec(ctx, query, t.id, t.label, i+1); err != nil {
			reportFailure(progress, part, section, operation, err, query)

			if err := arch.skip(t.id,
				namedRow("tag", t.id, sql.NullString{String: t.label, Valid: true}), "the database refused it", map[string]any{
					"_id":   t.id,
					"label": t.label,
				}); err != nil {
				tx.rollback()

				return err
			}
		}
	}

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerTags", unread || rows.Err() != nil)

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
func migrateSystems(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateSystems", "systems"

	exists, err := tableExists(ctx, s, "rdioScannerSystems")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating systems...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerSystems", "system", "systems")

	query := `SELECT "_id", "autoPopulate", "blacklists", "id", "label", "led", "order" FROM "rdioScannerSystems"`

	unread := false

	rows, err := readV6Table(ctx, s, tx, query)
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
			unread = true

			continue
		}

		// The v6 system as the table holds it: the blacklist with its brackets, which is how the
		// deployment wrote it, not as this server stores it.
		original := map[string]any{
			"_id":          v6Int(systemID),
			"autoPopulate": v6Bool(autoPopulate),
			"blacklists":   v6Text(blacklists),
			"id":           v6Int32(systemRef),
			"label":        v6Text(label),
			"led":          v6Text(led),
			"order":        v6Int32(order),
		}

		if !systemID.Valid {
			if err := arch.skip(0, "a system row", "it holds no identity", original); err != nil {
				tx.rollback()

				return err
			}

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

		if carried, err := alreadyCarried(ctx, tx, "systems", "systemId", systemID.Int64); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "systems" ("systemId", "autoPopulate", "blacklists", "label", "led", "order", "systemRef") VALUES (?, ?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, systemID.Int64, populate, blacklist, name, color, rank, ref); err != nil {
			reportFailure(progress, part, section, operation, err, query)

			if err := arch.skip(systemID.Int64,
				namedRow("system", systemID.Int64, label), "the database refused it", original); err != nil {
				tx.rollback()

				return err
			}
		}
	}

	rows.Close()

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerSystems", unread || rows.Err() != nil)

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
// filled. A talkgroup whose system is not in that table used to be skipped without a word and
// dropped with the v6 table; since the archive exists it is archived with the reason and the reason is said
// at warn level.
//
// Its group is resolved the same way, because the group link is a foreign key in this server's
// schema and was not one in v6's. v6 stored 0 for a talkgroup in no group, and the migration used to
// hand that 0 — or any group number the v6 database made up — to the `talkgroupGroups` insert and
// report whatever the backend called the refusal. The pass now asks the `groups` table first: the
// talkgroup is carried either way, a link is written only for a group that is actually here, and the
// group the row named is said in the pass's own words. That also matters on PostgreSQL, which ends
// the whole transaction at the first statement it refuses rather than the row.
func migrateTalkgroups(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateTalkgroups", "talkgroups"

	exists, err := tableExists(ctx, s, "rdioScannerTalkgroups")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating talkgroups...")

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
			// A cancelled read is felt at the scan and not at the iterator: the
			// preload map must not be built from the rows that happened to
			// survive.
			if readErr := rows.Err(); readErr != nil {
				rows.Close()

				return statementError("migration", "migrateTalkgroups", readErr, query)
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				rows.Close()

				return statementError("migration", "migrateTalkgroups", ctxErr, query)
			}

			continue
		}

		if systemID.Valid && systemRef.Valid {
			systems[systemRef.Int64] = systemID.Int64
		}
	}

	// The preload is the migration's map of what the server already holds:
	// a read that ended early leaves the map missing entries, and the rows
	// built from it point at the wrong identities.
	if err := rows.Err(); err != nil {
		rows.Close()

		return statementError("migration", "migrateTalkgroups", err, query)
	}

	rows.Close()

	// The groups this server holds. The groups migration keeps the v6 identity, so a v6 groupId is the
	// number that group is stored under here, and the only question about a talkgroup's group is whether a
	// group with that number was carried.
	groups := map[int64]bool{}

	query = `SELECT "groupId" FROM "groups"`

	rows, err = s.query(ctx, query)
	if err != nil {
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var groupID int64

		if err := rows.Scan(&groupID); err != nil {
			// A cancelled read is felt at the scan and not at the iterator: the
			// preload map must not be built from the rows that happened to
			// survive.
			if readErr := rows.Err(); readErr != nil {
				rows.Close()

				return statementError("migration", "migrateTalkgroups", readErr, query)
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				rows.Close()

				return statementError("migration", "migrateTalkgroups", ctxErr, query)
			}

			continue
		}

		groups[groupID] = true
	}

	// The preload is the migration's map of what the server already holds:
	// a read that ended early leaves the map missing entries, and the rows
	// built from it point at the wrong identities.
	if err := rows.Err(); err != nil {
		rows.Close()

		return statementError("migration", "migrateTalkgroups", err, query)
	}

	rows.Close()

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerTalkgroups", "talkgroup", "talkgroups")

	query = `SELECT "_id", "frequency", "groupId", "id", "label", "led", "name", "order", "systemId", "tagId" FROM "rdioScannerTalkgroups"`

	unread := false

	rows, err = readV6Table(ctx, s, tx, query)
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
			unread = true

			continue
		}

		// The v6 row as the pass read it, every column the v6 table has, including the identity column
		// under the name the v6 table gives it. Every branch below that cannot carry it archives this,
		// so that dropping the v6 table at the end of the pass never destroys a row quietly.
		original := map[string]any{
			"_id":       v6Int(talkgroupID),
			"frequency": v6Int32(frequency),
			"groupId":   v6Int(groupID),
			"id":        v6Int32(talkgroupRef),
			"label":     v6Text(label),
			"led":       v6Text(led),
			"name":      v6Text(name),
			"order":     v6Int32(order),
			"systemId":  v6Int(systemID),
			"tagId":     v6Int(tagID),
		}

		// The v6 table declares `_id` its primary key, so a row with no identity is a damaged database
		// rather than a shape the legacy server ever wrote. It is archived at identity 0 all the same:
		// the pass may not drop a row it read without leaving a record of it, and the archive's unique
		// index holds a second such row in the same table once.
		if !talkgroupID.Valid {
			if err := arch.skip(0, "a talkgroup row", "it holds no identity", original); err != nil {
				tx.rollback()

				return err
			}

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
			reason := "it names no system"

			if systemID.Valid {
				reason = fmt.Sprintf("its system %d is not in this database", systemID.Int64)
			}

			if err := arch.skip(talkgroupID.Int64,
				namedRow("talkgroup", talkgroupID.Int64, name), reason, original); err != nil {
				tx.rollback()

				return err
			}

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

		if carried, err := alreadyCarried(ctx, tx, "talkgroups", "talkgroupId", talkgroupID.Int64); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "talkgroups" ("talkgroupId", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, talkgroupID.Int64, freq, text, color, named, rank, systems[systemID.Int64], tag, ref); err == nil {
			// The link is written only for a group this database actually has. v6 wrote 0 for a talkgroup
			// in no group, this server's group link is a foreign key, and the pass knows the reference
			// does not resolve before it asks for it: the talkgroup is carried, the link is not written,
			// and the group the row named is said.
			if !groups[groupIDs[0]] {
				progress(domain.LogLevelWarn, fmt.Sprintf("the migration carried %s without its group: group %d is not in this database",
					namedRow("talkgroup", talkgroupID.Int64, name), groupIDs[0]))

			} else {
				query = `INSERT INTO "talkgroupGroups" ("groupId", "talkgroupId") VALUES (?, ?)`

				if _, err := tx.exec(ctx, query, groupIDs[0], talkgroupID.Int64); err != nil {
					reportFailure(progress, part, section, operation, err, query)
				}
			}

		} else {
			reportFailure(progress, part, section, operation, err, query)

			// The talkgroup itself never reaches the v8 table, and the v6 table is dropped at the end
			// of this pass. The row is archived so that the operator who reads the failure line above
			// still has the row the failure cost them.
			if err := arch.skip(talkgroupID.Int64,
				namedRow("talkgroup", talkgroupID.Int64, name), "the database refused it", original); err != nil {
				tx.rollback()

				return err
			}
		}
	}

	rows.Close()

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerTalkgroups", unread || rows.Err() != nil)

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateUnits mirrors `migrateUnits` in `server/migrations.go`.
//
// The v6 unit names its system by the v6 system's `id`, resolved the same way the talkgroups
// migration resolves it, so a unit whose system is not in the `systems` table used to be skipped
// without a word and dropped with the v6 table; since the archive exists it is archived with the reason and
// the reason is said at warn level.
func migrateUnits(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateUnits", "units"

	exists, err := tableExists(ctx, s, "rdioScannerUnits")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating units...")

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
			// A cancelled read is felt at the scan and not at the iterator: the
			// preload map must not be built from the rows that happened to
			// survive.
			if readErr := rows.Err(); readErr != nil {
				rows.Close()

				return statementError("migration", "migrateUnits", readErr, query)
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				rows.Close()

				return statementError("migration", "migrateUnits", ctxErr, query)
			}

			continue
		}

		if systemID.Valid && systemRef.Valid {
			systems[systemRef.Int32] = systemID.Int32
		}
	}

	// The preload is the migration's map of what the server already holds:
	// a read that ended early leaves the map missing entries, and the rows
	// built from it point at the wrong identities.
	if err := rows.Err(); err != nil {
		rows.Close()

		return statementError("migration", "migrateUnits", err, query)
	}

	rows.Close()

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerUnits", "unit", "units")

	query = `SELECT "_id", "id", "label", "order", "systemId" FROM "rdioScannerUnits"`

	unread := false

	rows, err = readV6Table(ctx, s, tx, query)
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
			unread = true

			continue
		}

		original := map[string]any{
			"_id":      v6Int(unitID),
			"id":       v6Int32(unitRef),
			"label":    v6Text(label),
			"order":    v6Int32(order),
			"systemId": v6Int32(systemID),
		}

		if !unitID.Valid {
			if err := arch.skip(0, "a unit row", "it holds no identity", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		if !systemID.Valid || systems[systemID.Int32] == 0 {
			reason := "it names no system"

			if systemID.Valid {
				reason = fmt.Sprintf("its system %d is not in this database", systemID.Int32)
			}

			if err := arch.skip(unitID.Int64,
				namedRow("unit", unitID.Int64, label), reason, original); err != nil {
				tx.rollback()

				return err
			}

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
		// `NewUnit` never gave the unit — in this position instead, so every migrated unit landed with
		// `unitRef` 0 and the number its row was read for was thrown away. Repaired rather than
		// preserved.
		if carried, err := alreadyCarried(ctx, tx, "units", "unitId", unitID.Int64); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "units" ("unitId", "label", "order", "systemId", "unitRef") VALUES (?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, unitID.Int64, unit.label, unit.order, systems[systemID.Int32], unit.unitRef); err != nil {
			reportFailure(progress, part, section, operation, err, query)

			if err := arch.skip(unitID.Int64,
				namedRow("unit", unitID.Int64, label), "the database refused it", original); err != nil {
				tx.rollback()

				return err
			}
		}
	}

	rows.Close()

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerUnits", unread || rows.Err() != nil)

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
// expects; anything else the blob holds is not carried, and since the archive exists the config row is archived
// whole with every such value named. Every other row of the v6 table is copied across as it stands,
// which is how the persisted `secret` survives.
//
// The unpacked values are stored as the JSON text of the value, so a string option is stored
// with its quotes — which is what the legacy options reader unmarshals.
// ensureOptionsKeyIndex gives the options table the one rule its own writes assume: a
// key appears once.
//
// The save updates a key's row and inserts only when no row carries the key, and on
// MySQL the update of an unchanged value reports no changed rows, the insert fires, and
// every save duplicates every key — the effective
// values become whichever duplicate the reader happens to reach. The duplicates a
// deployment already has are removed first, keeping the last row written for each key,
// which is the value the server has been running with; the unique index is what
// refuses duplicates from then on.
//
// It is a step of the migration pass rather than of the schema, the way the log
// table's column and indexes are: the schema is the legacy server's and has to leave a
// database the other server wrote exactly as it found it.
func ensureOptionsKeyIndex(ctx context.Context, s store) error {
	const section, operation = "migration", "ensureOptionsKeyIndex"

	// The last row written for each key wins. The derived table is what lets the same
	// statement run on MySQL, which will not delete rows selected from the very table
	// it deletes from, as plainly as SQLite and PostgreSQL read it.
	dedup := `DELETE FROM "options" WHERE "optionId" NOT IN (SELECT "kept" FROM (SELECT MAX("optionId") AS "kept" FROM "options" GROUP BY "key") AS "survivors")`

	if _, err := s.exec(ctx, dedup); err != nil {
		return statementError(section, operation, err, dedup)
	}

	created, err := hasOptionsKeyIndex(ctx, s)
	if err != nil {
		return operationError(section, operation, err)
	}

	if created {
		return nil
	}

	// MySQL is the dialect with no `CREATE INDEX IF NOT EXISTS`, which is why the index
	// is probed rather than asked for, the same way the log indexes are.
	const index = `CREATE UNIQUE INDEX "options_key" ON "options" ("key")`

	if _, err := s.exec(ctx, index); err != nil {
		return statementError(section, operation, err, index)
	}

	return nil
}

// tableExists asks the database's own catalog whether a table is there.
//
// The v6 gates used to answer this by running `SELECT COUNT(*) FROM <table>` and
// reading every failure as "not there": a locked database, an expired context, a
// schema the driver cannot read — all of them became a silent skip, the pass moved
// no rows and said no word, and a deployment's v6 rows sat un-migrated behind a
// startup log that named nothing. The catalog answers the question the gates ask —
// is the table there — and a probe that cannot read is a failure the caller is
// told about, naming the step that asked.
func tableExists(ctx context.Context, s store, table string) (bool, error) {
	// Views count as there: the legacy probe read a view as easily as a table, and
	// the migrations that consume the v6 names meet the same failures over a view as
	// the legacy met — the drop of a table refuses a view, and that failure is the
	// legacy's own, returned, not a silent skip. The MySQL and PostgreSQL catalogs
	// this queries already answer for views as well as tables.
	query := `SELECT COUNT(*) FROM "sqlite_master" WHERE ("type" = 'table' OR "type" = 'view') AND "name" = ?`

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		query = `SELECT COUNT(*) FROM "information_schema"."TABLES" WHERE "TABLE_SCHEMA" = DATABASE() AND "TABLE_NAME" = ?`

	case DialectPostgresql:
		query = `SELECT COUNT(*) FROM "information_schema"."tables" WHERE "table_name" = ?`
	}

	var count int

	if err := s.queryRow(ctx, query, table).Scan(&count); err != nil {
		return false, err
	}

	return count > 0, nil
}

// hasOptionsKeyIndex asks the database's own catalog whether the options table already
// carries the unique index on its key.
func hasOptionsKeyIndex(ctx context.Context, s store) (bool, error) {
	query := `SELECT COUNT(*) FROM "sqlite_master" WHERE "type" = 'index' AND "tbl_name" = 'options' AND "name" = ?`

	switch s.dialect {
	case DialectMariadb, DialectMysql:
		query = `SELECT COUNT(DISTINCT "INDEX_NAME") FROM "information_schema"."STATISTICS" WHERE "TABLE_SCHEMA" = DATABASE() AND "TABLE_NAME" = 'options' AND "INDEX_NAME" = ?`

	case DialectPostgresql:
		query = `SELECT COUNT(*) FROM "pg_indexes" WHERE "tablename" = 'options' AND "indexname" = ?`
	}

	var count int

	if err := s.queryRow(ctx, query, "options_key").Scan(&count); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}

		return false, err
	}

	return count > 0, nil
}

func migrateOptions(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateOptions", "options"

	exists, err := tableExists(ctx, s, "rdioScannerConfigs")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating options...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerConfigs", "config row", "config rows")

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

	// The name the unpack list knows, and how each of them is read, so that a name the blob holds and
	// the list does not is recognisable as what it is: a v6 option this server has no setting for.
	extractOf := map[string]func(map[string]any, string) (string, bool){}

	for _, option := range unpacked {
		extractOf[option.key] = option.extract
	}

	query := `SELECT "_id", "key", "val" FROM "rdioScannerConfigs"`

	unread := false

	rows, err := readV6Table(ctx, s, tx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			configID sql.NullInt64
			key      sql.NullString
			value    sql.NullString
		)

		if err := rows.Scan(&configID, &key, &value); err != nil {
			unread = true

			continue
		}

		// The v6 configuration row as the table holds it. The options blob is one row that unpacks
		// into many, so it is archived once, whole: two values the pass could not carry are still one
		// row, and the archive holds each row once.
		original := map[string]any{
			"_id": v6Int(configID),
			"key": v6Text(key),
			"val": v6Text(value),
		}

		named, rowID := "a config row", int64(0)

		if key.Valid {
			named = fmt.Sprintf("the config row %q", key.String)
		}

		if configID.Valid {
			rowID = configID.Int64
		}

		if !key.Valid || !value.Valid {
			reason := "it holds no key"

			if key.Valid {
				reason = "it holds no value"
			}

			if err := arch.skip(rowID, named, reason, original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		if key.String != "options" {
			if carried, err := alreadyCarried(ctx, tx, "options", "key", key.String); err != nil {
				tx.rollback()

				return err
			} else if carried {
				continue
			}

			query = `INSERT INTO "options" ("key", "value") VALUES (?, ?)`

			if _, err := tx.exec(ctx, query, key.String, value.String); err != nil {
				reportFailure(progress, part, section, operation, err, query)

				if err := arch.skip(rowID, named, "the database refused it", original); err != nil {
					tx.rollback()

					return err
				}
			}

			continue
		}

		var blob map[string]any

		if err := json.Unmarshal([]byte(value.String), &blob); err != nil {
			if err := arch.skip(rowID, named, "it could not be read", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		refused := false

		for _, option := range unpacked {
			stored, ok := option.extract(blob, option.key)
			if !ok {
				continue
			}

			if carried, err := alreadyCarried(ctx, tx, "options", "key", option.key); err != nil {
				tx.rollback()

				return err
			} else if carried {
				continue
			}

			query = `INSERT INTO "options" ("key", "value") VALUES (?, ?)`

			if _, err := tx.exec(ctx, query, option.key, stored); err != nil {
				reportFailure(progress, part, section, operation, err, query)

				refused = true
			}
		}

		// Which values of the blob the pass could not carry, named in the order the blob's names
		// sort, so that the same deployment says the same sentence at every start.
		cannot := []string{}

		names := make([]string, 0, len(blob))

		for name := range blob {
			names = append(names, name)
		}

		sort.Strings(names)

		for _, name := range names {
			extract, ok := extractOf[name]
			if !ok {
				cannot = append(cannot, fmt.Sprintf("%q (v8 has no option of that name)", name))

				continue
			}

			if _, ok := extract(blob, name); !ok {
				cannot = append(cannot, fmt.Sprintf("%q (the value has the wrong shape)", name))
			}
		}

		reason := ""

		if len(cannot) > 0 {
			reason = "it holds values this server cannot carry: " + strings.Join(cannot, ", ")
		}

		if refused {
			if reason != "" {
				reason += ", and the database refused another value of it"
			} else {
				reason = "the database refused it"
			}
		}

		if reason == "" {
			continue
		}

		if err := arch.skip(rowID, named, reason, original); err != nil {
			tx.rollback()

			return err
		}
	}

	rows.Close()

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerConfigs", unread || rows.Err() != nil)

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
//
// The rows are archived before the drop. The deployment's migration history is what
// this table holds, it is exactly what the drop takes away, and an archive that exists so that
// no row leaves a migration without a record should not let the smallest of them — the record
// of the migrations themselves — go without one.
func migrateMeta(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateMeta", "meta"

	exists, err := tableExists(ctx, s, "rdioScannerMeta")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating meta...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerMeta", "migration record", "migration records")

	query := `SELECT "name" FROM "rdioScannerMeta" ORDER BY "name"`

	rows, err := readV6Table(ctx, s, tx, query)
	if err != nil {
		tx.rollback()

		return statementError(section, operation, err, query)
	}

	unread := false
	rowId := int64(0)

	for rows.Next() {
		var name sql.NullString

		if err := rows.Scan(&name); err != nil {
			unread = true

			break
		}

		rowId++

		if err := arch.skip(rowId, namedRow("migration record", rowId, name),
			"this server has no migration bookkeeping to carry it into", name.String); err != nil {
			tx.rollback()

			return err
		}
	}

	rows.Close()

	arch.report()

	if unread || rows.Err() != nil {
		// A row the pass never saw: the table keeps it, as every other migration
		// leaves a table it could not read whole. The drop is all this pass does,
		// and a table it could not read whole is not a table it drops.
		progress(domain.LogLevelWarn, fmt.Sprintf("the migration left the %s table where it found it: a row of it could not be read", part))

		if err := tx.commit(); err != nil {
			tx.rollback()

			return operationError(section, operation, err)
		}

		return nil
	}

	// The legacy server returned the meta drop's failure rather than printing it,
	// and stopped the whole upgrade there; that stays. The drop runs inside this
	// table's transaction, so its failure rolls back the archive rows with it and
	// the pass stops with the v6 tables standing, exactly as the pinned test reads.
	drop := `DROP TABLE "rdioScannerMeta"`

	if _, err := tx.exec(ctx, drop); err != nil {
		tx.rollback()

		return statementError(section, operation, err, drop)
	}

	if err := tx.commit(); err != nil {
		tx.rollback()

		return operationError(section, operation, err)
	}

	return nil
}

// migrateLogs mirrors `migrateLogs` in `server/migrations.go`.
//
// The v6 log timestamp is a datetime; the v7 one is Unix milliseconds, which is what every
// timestamp in this schema is. A row with no timestamp, no level, or no message is not carried — and
// since the archive exists it is archived with the reason and the reason is said, where the legacy migration
// skipped it silently.
//
// The rows it carries are filed afterwards. They were written by a server that had no source column, and
// the step that files the rows a database already has ran before this one inserted them, so without this
// the same deployment has one log view if it was already on v7 and another if it upgraded here.
func migrateLogs(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateLogs", "logs"

	exists, err := tableExists(ctx, s, "rdioScannerLogs")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating logs...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerLogs", "log", "logs")

	query := `SELECT "_id", "dateTime", "level", "message" FROM "rdioScannerLogs"`

	unread := false

	rows, err := readV6Table(ctx, s, tx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			logID       sql.NullInt32
			rawDateTime any
			level       sql.NullString
			message     sql.NullString
		)

		if err := rows.Scan(&logID, &rawDateTime, &level, &message); err != nil {
			unread = true

			continue
		}

		// Read the way the legacy server reads it: MariaDB hands a datetime back as bytes, and a scan
		// into a time is something it cannot answer.
		dateTime, err := v6DateTime(rawDateTime)
		if err != nil {
			unread = true

			continue
		}

		original := map[string]any{
			"_id":      v6Int32(logID),
			"dateTime": v6Time(dateTime),
			"level":    v6Text(level),
			"message":  v6Text(message),
		}

		if !logID.Valid {
			if err := arch.skip(0, "a log row", "it holds no identity", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		named := fmt.Sprintf("log %d", logID.Int32)

		if !dateTime.Valid {
			if err := arch.skip(int64(logID.Int32), named, "it holds no timestamp", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		if !level.Valid || len(level.String) == 0 {
			if err := arch.skip(int64(logID.Int32), named, "it holds no level", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		if !message.Valid || len(message.String) == 0 {
			if err := arch.skip(int64(logID.Int32), named, "it holds no message", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		if carried, err := alreadyCarried(ctx, tx, "logs", "logId", int64(logID.Int32)); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "logs" ("logId", "level", "message", "timestamp") VALUES (?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, int64(logID.Int32), level.String, message.String, dateTime.Time.UnixMilli()); err != nil {
			reportFailure(progress, part, section, operation, err, query)

			if err := arch.skip(int64(logID.Int32), named, "the database refused it", original); err != nil {
				tx.rollback()

				return err
			}
		}
	}

	rows.Close()

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerLogs", unread || rows.Err() != nil)

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	// The rows just carried across arrived with nothing in the source column: they were written by a
	// server that had no such column, and the add-column step that files the rows a database already has
	// ran before them. They are filed here, with the same reading of the prefix the legacy server wrote,
	// so that a deployment upgrading through this server ends up with the same log view as one that was
	// already on v7 — and an operator filtering it by login finds the failed logins of both.
	return backfillLogSources(ctx, s)
}

// migrateDownstreams mirrors `migrateDownstreams` in `server/migrations.go`.
// A downstream with no apikey, no scope, or no url is not carried — and since the archive exists it is archived
// with the reason and the reason is said, where the legacy migration skipped it silently: those three
// columns are the whole row's meaning.
func migrateDownstreams(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateDownstreams", "downstreams"

	exists, err := tableExists(ctx, s, "rdioScannerDownstreams")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating downstreams...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerDownstreams", "downstream", "downstreams")

	query := `SELECT "_id", "apiKey", "disabled", "order", "systems", "url" FROM "rdioScannerDownstreams"`

	unread := false

	rows, err := readV6Table(ctx, s, tx, query)
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
			unread = true

			continue
		}

		original := map[string]any{
			"_id":      v6Int(downstreamID),
			"apiKey":   v6Text(apikey),
			"disabled": v6Bool(disabled),
			"order":    v6Int32(order),
			"systems":  v6Text(systems),
			"url":      v6Text(url),
		}

		if !downstreamID.Valid {
			if err := arch.skip(0, "a downstream row", "it holds no identity", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		named := fmt.Sprintf("downstream %d", downstreamID.Int64)

		reason := ""

		if !apikey.Valid || len(apikey.String) == 0 {
			reason = "it holds no api key"
		} else if !systems.Valid || len(systems.String) == 0 {
			reason = "it holds no systems"
		} else if !url.Valid || len(url.String) == 0 {
			reason = "it holds no url"
		}

		if reason != "" {
			if err := arch.skip(downstreamID.Int64, named, reason, original); err != nil {
				tx.rollback()

				return err
			}

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

		if carried, err := alreadyCarried(ctx, tx, "downstreams", "downstreamId", downstreamID.Int64); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "downstreams" ("downstreamId", "apikey", "disabled", "order", "systems", "url") VALUES (?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, downstreamID.Int64, apikey.String, isDisabled, rank, systems.String, url.String); err != nil {
			reportFailure(progress, part, section, operation, err, query)

			if err := arch.skip(downstreamID.Int64, named, "the database refused it", original); err != nil {
				tx.rollback()

				return err
			}
		}
	}

	rows.Close()

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerDownstreams", unread || rows.Err() != nil)

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// v6DirwatchesTable is the name a v6 deployment's dirwatch table has in its own schema. The probe, the
// read, the archive, and the drop all name this one string, because they used to name three different
// spellings and only sqlite's case-insensitive table names hid that.
const v6DirwatchesTable = "rdioScannerDirWatches"

// migrateDirwatches mirrors `migrateDirwatches` in `server/migrations.go`.
//
// Two legacy details are reproduced here rather than tidied away.
//
// The lookup query is a cross product of systems and talkgroups. It is what fills the two lookup maps,
// and it only records a system when the row it came from had a talkgroup too: a system with no
// talkgroups is not in the maps at all, and its dirwatches then resolve to nothing.
//
// It is spelled `CROSS JOIN`, which is what the same query looked like as a `LEFT JOIN` with no `ON`
// clause until a live run asked MariaDB to parse it: MariaDB refuses a
// `LEFT JOIN` with no `ON` as a syntax error, before it looks at any data, and the two maps came out
// identical on the other three backends.
//
// The dirwatch rows are scanned into two of the four variables the lookup query used, so the
// other two keep whatever the lookup query left in them. Whether a dirwatch's talkgroup is
// resolved at all depends on `talkgroupID`, which the dirwatch rows never scan into — see
// `TestMigrateDirwatchesResolveNothingWhenNoTalkgroupWasMigrated`.
//
// The three statements that name the v6 table now name it once, in the spelling v6 created it with.
// They used not to agree: the probe and the read asked for `rdioScannerDirwatches`, the drop named
// `RdioScannerDirWatches`, and the table a v6 deployment actually has is `rdioScannerDirWatches`. sqlite
// matches table names whatever their case, so a v6 sqlite deployment lost nothing and the disagreement
// stayed invisible. A backend that does not match them case-insensitively — MySQL and MariaDB on a
// case-sensitive filesystem, which is the normal case — failed the probe, and its v6 dirwatches were
// neither migrated nor dropped, on that start or on any later one: the one table the migration skipped
// quietly, which is the shape the archive exists to make impossible. It is proved by
// `TestMigrateDirwatchesNameTheTableTheV6DatabaseCreated` and by the live MariaDB run.
func migrateDirwatches(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateDirwatches", "dirwatches"

	if _, err := s.exec(ctx, `SELECT COUNT(*) FROM "`+v6DirwatchesTable+`"`); err != nil {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating dirwatches...")

	var (
		systems    = map[int32]int32{}
		talkgroups = map[int32]map[int32]int32{}

		systemID     sql.NullInt32
		systemRef    sql.NullInt32
		talkgroupID  sql.NullInt32
		talkgroupRef sql.NullInt32
	)

	query := `SELECT s."systemId", s."systemRef", t."talkgroupId", t."talkgroupRef" FROM "systems" AS s CROSS JOIN "talkgroups" AS t`

	rows, err := s.query(ctx, query)
	if err != nil {
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		if err := rows.Scan(&systemID, &systemRef, &talkgroupID, &talkgroupRef); err != nil {
			// A cancelled read is felt at the scan and not at the iterator: the
			// preload map must not be built from the rows that happened to
			// survive.
			if readErr := rows.Err(); readErr != nil {
				rows.Close()

				return statementError("migration", "migrateDirwatches", readErr, query)
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				rows.Close()

				return statementError("migration", "migrateDirwatches", ctxErr, query)
			}

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

	// The preload is the migration's map of what the server already holds:
	// a read that ended early leaves the map missing entries, and the rows
	// built from it point at the wrong identities.
	if err := rows.Err(); err != nil {
		rows.Close()

		return statementError("migration", "migrateDirwatches", err, query)
	}

	rows.Close()

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, v6DirwatchesTable, "dirwatch", "dirwatches")

	query = `SELECT "_id", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "systemId", "talkgroupId", "type" FROM "` + v6DirwatchesTable + `"`

	unread := false

	rows, err = readV6Table(ctx, s, tx, query)
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
			unread = true

			continue
		}

		// The v6 dirwatch as the pass reads it. The v6 polling column is not in the read at all — v8
		// has no polling column, so the value has no destination here or anywhere else in this server,
		// and an archived dirwatch is no different from a carried one in that respect.
		original := map[string]any{
			"_id":         v6Int(dirwatchID),
			"delay":       v6Int32(delay),
			"deleteAfter": v6Bool(deleteAfter),
			"directory":   v6Text(directory),
			"disabled":    v6Bool(disabled),
			"extension":   v6Text(extension),
			"frequency":   v6Int32(frequency),
			"mask":        v6Text(mask),
			"order":       v6Int32(order),
			"systemId":    v6Int32(systemRef),
			"talkgroupId": v6Int32(talkgroupRef),
			"type":        v6Text(kind),
		}

		if !dirwatchID.Valid {
			if err := arch.skip(0, "a dirwatch row", "it holds no identity", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		if !directory.Valid || len(directory.String) == 0 {
			if err := arch.skip(dirwatchID.Int64,
				namedRow("dirwatch", dirwatchID.Int64, directory), "it holds no directory", original); err != nil {
				tx.rollback()

				return err
			}

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
		if carried, err := alreadyCarried(ctx, tx, "dirwatches", "dirwatchId", dirwatchID.Int64); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "dirwatches" ("dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "systemId", "talkgroupId", "type") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, dirwatchID.Int64, wait, drops, directory.String, isDisabled, suffix, freq, filter, rank, refSystem, refTalkgroup, parser); err != nil {
			reportFailure(progress, part, section, operation, err, query)

			if err := arch.skip(dirwatchID.Int64,
				namedRow("dirwatch", dirwatchID.Int64, directory), "the database refused it", original); err != nil {
				tx.rollback()

				return err
			}
		}
	}

	rows.Close()

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, v6DirwatchesTable, unread || rows.Err() != nil)

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateCalls mirrors `migrateCalls` in `server/migrations.go`.
//
// A call is migrated only when its audio, its timestamp, and both of its aggregate references
// resolve against the tables the earlier migrations filled; anything that does not resolve used to be
// skipped without a word and dropped with the v6 table, and since the archive exists it is archived with its audio
// and the reason is said. The v6 call has no site, so the migrated call is stored with site reference
// 0, as the legacy migration stores it.
//
// The call's frequencies, patches, and source units are written from the JSON the v6 row holds.
// The `frequencies` column is checked for a non-empty *string*, so a v6 call whose frequency list
// is the empty array takes the list branch and stores no frequency at all, even when its single
// `frequency` column names one; the `sources` column is checked for the empty array as well, so
// an empty source list does fall through to the single `source` column.
func migrateCalls(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateCalls", "calls"

	exists, err := tableExists(ctx, s, "rdioScannerCalls")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating calls...")

	var (
		systems    = map[int32]int32{}
		talkgroups = map[int32]map[int32]int32{}
	)

	query := `SELECT s."systemId", s."systemRef", t."talkgroupId", t."talkgroupRef" FROM "systems" AS s CROSS JOIN "talkgroups" AS t`

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
			// A cancelled read is felt at the scan and not at the iterator: the
			// preload map must not be built from the rows that happened to
			// survive.
			if readErr := rows.Err(); readErr != nil {
				rows.Close()

				return statementError("migration", "migrateCalls", readErr, query)
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				rows.Close()

				return statementError("migration", "migrateCalls", ctxErr, query)
			}

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

	// The preload is the migration's map of what the server already holds:
	// a read that ended early leaves the map missing entries, and the rows
	// built from it point at the wrong identities.
	if err := rows.Err(); err != nil {
		rows.Close()

		return statementError("migration", "migrateCalls", err, query)
	}

	rows.Close()

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerCalls", "call", "calls")

	query = `SELECT "id", "audio", "audioName", "audioType", "dateTime", "frequencies", "frequency", "patches", "source", "sources", "system", "talkgroup" FROM "rdioScannerCalls"`

	unread := false

	rows, err = readV6Table(ctx, s, tx, query)
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
			rawDateTime  any
			frequencies  sql.NullString
			frequency    sql.NullInt32
			patches      sql.NullString
			source       sql.NullInt32
			sources      sql.NullString
			systemRef    sql.NullInt32
			talkgroupRef sql.NullInt32
		)

		if err := rows.Scan(&callID, &audio, &audioName, &audioType, &rawDateTime, &frequencies, &frequency, &patches, &source, &sources, &systemRef, &talkgroupRef); err != nil {
			unread = true

			continue
		}

		// Read the way the legacy server reads it: MariaDB hands a datetime back as bytes, and a scan
		// into a time is something it cannot answer.
		dateTime, err := v6DateTime(rawDateTime)
		if err != nil {
			unread = true

			continue
		}

		// The v6 call as the pass read it, audio included: the archive holds the audio base64-encoded,
		// which is what makes a call this server could not carry recoverable afterwards.
		original := map[string]any{
			"audio":       v6Audio(audio),
			"audioName":   v6Text(audioName),
			"audioType":   v6Text(audioType),
			"dateTime":    v6Time(dateTime),
			"frequency":   v6Int32(frequency),
			"frequencies": v6Text(frequencies),
			"id":          v6Int32(callID),
			"patches":     v6Text(patches),
			"source":      v6Int32(source),
			"sources":     v6Text(sources),
			"system":      v6Int32(systemRef),
			"talkgroup":   v6Int32(talkgroupRef),
		}

		named := ""

		if callID.Valid {
			named = fmt.Sprintf("call %d", callID.Int32)
		}

		if !callID.Valid {
			if err := arch.skip(0, "a call row", "it holds no identity", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		if !audio.Valid || len(audio.String) == 0 {
			if err := arch.skip(int64(callID.Int32), named, "it holds no audio", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		if !dateTime.Valid {
			if err := arch.skip(int64(callID.Int32), named, "it holds no timestamp", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		if !systemRef.Valid || systems[systemRef.Int32] == 0 {
			reason := "it names no system"

			if systemRef.Valid {
				reason = fmt.Sprintf("its system %d is not in this database", systemRef.Int32)
			}

			if err := arch.skip(int64(callID.Int32), named, reason, original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		if !talkgroupRef.Valid || talkgroups[systemRef.Int32][talkgroupRef.Int32] == 0 {
			reason := "it names no talkgroup"

			if talkgroupRef.Valid {
				reason = fmt.Sprintf("its talkgroup %d is not in this database", talkgroupRef.Int32)
			}

			if err := arch.skip(int64(callID.Int32), named, reason, original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		var filename, mime string

		if audioName.Valid {
			filename = audioName.String
		}

		if audioType.Valid {
			mime = audioType.String
		}

		if carried, err := alreadyCarried(ctx, tx, "calls", "callId", int64(callID.Int32)); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "calls" ("callId", "audio", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES (?, ?, ?, ?, 0, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, int64(callID.Int32), []byte(audio.String), filename, mime, systems[systemRef.Int32], talkgroups[systemRef.Int32][talkgroupRef.Int32], dateTime.Time.UnixMilli()); err != nil {
			reportFailure(progress, part, section, operation, err, query)

			if err := arch.skip(int64(callID.Int32), named, "the database refused it", original); err != nil {
				tx.rollback()

				return err
			}

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
							reportFailure(progress, part, section, operation, err, query)
						}
					}
				}
			}

		} else if frequency.Valid && frequency.Int32 > 0 {
			query = `INSERT INTO "callFrequencies" ("callId", "errors", "frequency", "offset", "spikes") VALUES (?, 0, ?, 0, 0)`

			if _, err := tx.exec(ctx, query, int64(callID.Int32), int64(frequency.Int32)); err != nil {
				reportFailure(progress, part, section, operation, err, query)
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
							reportFailure(progress, part, section, operation, err, query)
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
							reportFailure(progress, part, section, operation, err, query)
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
			// preserved.
			var count int

			query = `SELECT COUNT(*) FROM "units" WHERE "systemId" = ? AND "unitRef" = ?`

			if err := tx.queryRow(ctx, query, systems[systemRef.Int32], int64(source.Int32)).Scan(&count); err != nil {
				reportFailure(progress, part, section, operation, err, query)

				continue
			}

			if count == 0 {
				query = `INSERT INTO "units" ("label", "systemId", "unitRef") VALUES (?, ?, ?)`

				// The label is the number as text, which is what the legacy statement — a number
				// interpolated straight into a text column — leaves in that column.
				if _, err := tx.exec(ctx, query, strconv.FormatInt(int64(source.Int32), 10), systems[systemRef.Int32], int64(source.Int32)); err != nil {
					reportFailure(progress, part, section, operation, err, query)

					continue
				}
			}

			query = `INSERT INTO "callUnits" ("callId", "offset", "unitRef") VALUES (?, ?, ?)`

			if _, err := tx.exec(ctx, query, int64(callID.Int32), 0, int64(source.Int32)); err != nil {
				reportFailure(progress, part, section, operation, err, query)
			}
		}
	}

	rows.Close()

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerCalls", unread || rows.Err() != nil)

	if err := tx.commit(); err != nil {
		tx.rollback()
		return operationError(section, operation, err)
	}

	return nil
}

// migrateApikeys mirrors `migrateApikeys` in `server/migrations.go`.
// Only a missing identifier skips a row: an apikey with no identity or no key is still stored,
// with the empty value the legacy migration stored.
func migrateApikeys(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateApikeys", "apikeys"

	exists, err := tableExists(ctx, s, "rdioScannerApiKeys")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating apikeys...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerApiKeys", "apikey", "apikeys")

	query := `SELECT "_id", "disabled", "ident", "key", "order", "systems" FROM "rdioScannerApiKeys"`

	unread := false

	rows, err := readV6Table(ctx, s, tx, query)
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
			unread = true

			continue
		}

		original := map[string]any{
			"_id":      v6Int(apikeyID),
			"disabled": v6Bool(disabled),
			"ident":    v6Text(ident),
			"key":      v6Text(key),
			"order":    v6Int32(order),
			"systems":  v6Text(systems),
		}

		if !apikeyID.Valid {
			if err := arch.skip(0, "an api key row", "it holds no identity", original); err != nil {
				tx.rollback()

				return err
			}

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

		if carried, err := alreadyCarried(ctx, tx, "apikeys", "apikeyId", apikeyID.Int64); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "apikeys" ("apikeyId", "disabled", "ident", "key", "order", "systems") VALUES (?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, apikeyID.Int64, isDisabled, name, value, rank, scope); err != nil {
			reportFailure(progress, part, section, operation, err, query)

			if err := arch.skip(apikeyID.Int64,
				namedRow("apikey", apikeyID.Int64, ident), "the database refused it", original); err != nil {
				tx.rollback()

				return err
			}
		}
	}

	rows.Close()

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerApiKeys", unread || rows.Err() != nil)

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
func migrateAccesses(ctx context.Context, s store, progress Progress) error {
	const section, operation, part = "migration", "migrateAccesses", "accesses"

	exists, err := tableExists(ctx, s, "rdioScannerAccesses")
	if err != nil {
		return operationError(section, operation, err)
	}

	if !exists {
		return nil
	}

	progress(domain.LogLevelInfo, "migrating accesses...")

	tx, err := s.begin(ctx)
	if err != nil {
		return operationError(section, operation, err)
	}

	arch := archiving(ctx, tx, progress, "rdioScannerAccesses", "access", "accesses")

	query := `SELECT "_id", "code", "expiration", "ident", "limit", "order", "systems" FROM "rdioScannerAccesses"`

	unread := false

	rows, err := readV6Table(ctx, s, tx, query)
	if err != nil {
		tx.rollback()
		return statementError(section, operation, err, query)
	}

	for rows.Next() {
		var (
			accessID    sql.NullInt64
			code        sql.NullString
			rawDateTime any
			ident       sql.NullString
			limit       sql.NullInt32
			order       sql.NullInt32
			systems     sql.NullString
		)

		if err := rows.Scan(&accessID, &code, &rawDateTime, &ident, &limit, &order, &systems); err != nil {
			unread = true

			continue
		}

		// Read the way the legacy server reads it: MariaDB hands a datetime back as bytes, and a scan
		// into a time is something it cannot answer. An access with no expiration is a nil here, and
		// stays the row with no expiration that it was.
		expiration, err := v6DateTime(rawDateTime)
		if err != nil {
			unread = true

			continue
		}

		original := map[string]any{
			"_id":        v6Int(accessID),
			"code":       v6Text(code),
			"expiration": v6Time(expiration),
			"ident":      v6Text(ident),
			"limit":      v6Int32(limit),
			"order":      v6Int32(order),
			"systems":    v6Text(systems),
		}

		if !accessID.Valid {
			if err := arch.skip(0, "an access row", "it holds no identity", original); err != nil {
				tx.rollback()

				return err
			}

			continue
		}

		named := fmt.Sprintf("access %d", accessID.Int64)

		if !code.Valid || len(code.String) == 0 {
			if err := arch.skip(accessID.Int64, named, "it holds no code", original); err != nil {
				tx.rollback()

				return err
			}

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

		if carried, err := alreadyCarried(ctx, tx, "accesses", "accessId", accessID.Int64); err != nil {
			tx.rollback()

			return err
		} else if carried {
			continue
		}

		query = `INSERT INTO "accesses" ("accessId", "code", "expiration", "ident", "limit", "order", "systems") VALUES (?, ?, ?, ?, ?, ?, ?)`

		if _, err := tx.exec(ctx, query, accessID.Int64, code.String, expires, name, max, rank, scope); err != nil {
			reportFailure(progress, part, section, operation, err, query)

			if err := arch.skip(accessID.Int64, named, "the database refused it", original); err != nil {
				tx.rollback()

				return err
			}
		}
	}

	rows.Close()

	arch.report()

	dropV6Table(ctx, tx, progress, part, section, operation, "rdioScannerAccesses", unread || rows.Err() != nil)

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
// Neither is repaired here, because neither can be.
//
// The report is console-only: it writes no row and no log entry. A startup notice is not something the
// two servers are asked to agree about, and a log entry would put a difference in the `logs` table the
// parity fixtures are read from.
//
// The comparison that finds the second shape is done here rather than in the query, because `label` is
// text and `unitRef` is a number, and the three dialects this package serves cast between them three
// different ways.
func reportMigrationDamage(ctx context.Context, s store, progress Progress) {
	const section, operation = "migration", "damage"

	query := `SELECT u."unitId", u."label", u."unitRef", s."label", s."systemRef" FROM "units" AS u LEFT JOIN "systems" AS s ON s."systemId" = u."systemId" ORDER BY u."unitId"`

	rows, err := s.query(ctx, query)
	if err != nil {
		progress(domain.LogLevelWarn, fmt.Sprintf("the migration could not report what it left behind: %s",
			statementError(section, operation, err, query).Error()))

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

		line := fmt.Sprintf("the migration left unit %d %q of %s damaged", unitID.Int64, label.String,
			damagedSystem(systemLabel, systemRef))

		switch {
		case unitRef.Int64 == 0:
			lost = append(lost, line)

		case label.String == strconv.FormatInt(unitRef.Int64, 10):
			made = append(made, line)
		}
	}

	reportDamaged(progress, lost, "it holds no radio reference", "they hold no radio reference",
		"the numbers they were migrated from are not in this database")

	reportDamaged(progress, made, "it is labelled with its own number",
		"they are labelled with their own numbers",
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

// reportDamaged writes one summary line and then one line per unit, and says nothing at all when there is
// nothing to say.
//
// The summary names the count, what is wrong with the units, and why it matters; the per-unit lines name
// each unit an operator can go and look at. The legacy shape put both behind one `migration.damage:`
// prefix, which said nothing to anybody who had not read the source.
func reportDamaged(progress Progress, found []string, one, many, cause string) {
	if len(found) == 0 {
		return
	}

	if len(found) == 1 {
		progress(domain.LogLevelWarn, fmt.Sprintf("the migration left 1 unit damaged: %s, %s", one, cause))
	} else {
		progress(domain.LogLevelWarn, fmt.Sprintf("the migration left %d units damaged: %s, %s", len(found), many, cause))
	}

	for _, line := range found {
		progress(domain.LogLevelWarn, line)
	}
}
