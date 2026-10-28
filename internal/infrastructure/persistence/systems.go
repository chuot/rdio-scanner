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
	"fmt"
	"strconv"
	"strings"
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Systems is the adapter for the stored system catalog: a system and everything under it —
// talkgroups, their group memberships, sites, and units — all saved inside one system
// transaction, per the systems aggregate transaction maintainer note. The catalog is the
// configuration in effect: loaded once, replaced with what was submitted before storing, and
// read back after a save that persisted.
type Systems struct {
	store store

	// live is the catalog in effect. known says whether it has been loaded yet, so that the
	// first read is the only one that goes to the database.
	mutex sync.Mutex
	live  domain.Systems
	known bool

	// warn tells the operator about a stored value the read had to clamp. The server
	// wires it to its log; without it the repository clamps silently.
	warn func(message string)
}

// OnWarn sets the listener that hears about stored values the read had to clamp.
// The server wires it at startup to file the line as a warning in the log view.
func (systems *Systems) OnWarn(report func(message string)) { systems.warn = report }

var _ ports.SystemRepository = (*Systems)(nil)

// NewSystems builds the adapter for one opened database.
func NewSystems(db *sql.DB, dialect Dialect) *Systems {
	return &Systems{store: newStore(db, dialect)}
}

// AllSystems returns the systems the server is running with, with their talkgroups, sites, and
// units; the catalog is read once and later reads answer from what is in effect.
func (systems *Systems) AllSystems(ctx context.Context) (domain.Systems, error) {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	if systems.known {
		return systems.live, nil
	}

	loaded, clamped, err := readSystems(ctx, systems.store)
	if err != nil {
		return nil, err
	}

	systems.live = loaded
	systems.known = true
	systems.reportClamps(clamped)

	return loaded, nil
}

// SaveSystems reconciles the stored systems with the given ones and leaves the
// reconciled result as what the server runs with: the submitted catalog is live
// before anything is stored, and a persisted save is followed by reading the
// stored rows back, per the reconciling saves maintainer note.
func (systems *Systems) SaveSystems(ctx context.Context, next domain.Systems) error {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	systems.live = next
	systems.known = true

	if err := writeSystems(ctx, systems.store, next); err != nil {
		return err
	}

	loaded, clamped, err := readSystems(ctx, systems.store)
	if err != nil {
		// The legacy server logs a failed re-read and carries on with the partial read;
		// here the failure is reported and what stays in effect is what was submitted.
		return err
	}

	systems.live = loaded
	systems.reportClamps(clamped)

	return nil
}

// SaveSystemSites replaces the sites of one system, renumbered 1-based and reconciled
// with the stored rows as the whole-catalog save does them; a system the store does not
// have is reported as ErrNotFound.
func (systems *Systems) SaveSystemSites(ctx context.Context, systemID uint64, sites domain.Sites) error {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	// The renumbering below is this save's own work: the submitted slice stays untouched.
	sites = append(domain.Sites{}, sites...)

	for i := range sites {
		sites[i].Order = uint(i + 1)
		sites[i].SystemID = systemID
	}

	if err := saveSystemLevel(ctx, systems.store, systemID, func(tx transaction) error {
		return writeSites(ctx, tx, systemID, sites)
	}); err != nil {
		return err
	}

	loaded, clamped, err := readSystems(ctx, systems.store)
	if err != nil {
		return err
	}

	systems.live = loaded
	systems.reportClamps(clamped)

	return nil
}

// SaveSystemSettings reconciles the systems table alone — the same reconcile the
// whole-catalog save performs — and writes nothing under a system, because the level
// saves keep the rows the store holds for them.
func (systems *Systems) SaveSystemSettings(ctx context.Context, next domain.Systems) error {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	systems.live = next
	systems.known = true

	if err := writeSystemSettings(ctx, systems.store, next); err != nil {
		return err
	}

	loaded, clamped, err := readSystems(ctx, systems.store)
	if err != nil {
		return err
	}

	systems.live = loaded
	systems.reportClamps(clamped)

	return nil
}

// SaveSystemTalkgroups replaces the talkgroups of one system, renumbering their order
// and reconciling their group memberships. The blacklist edited beside the rows is stored in
// the same transaction, so the save that drops a talkgroup also stores the entry that
// refuses it; a save naming no blacklist leaves the stored text alone.
func (systems *Systems) SaveSystemTalkgroups(ctx context.Context, systemID uint64, talkgroups domain.Talkgroups, blacklists *domain.Blacklists) error {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	// The renumbering is this save's own work: the caller's slice stays untouched.
	talkgroups = append(domain.Talkgroups{}, talkgroups...)

	for i := range talkgroups {
		talkgroups[i].Order = uint(i + 1)
	}

	if err := saveSystemLevel(ctx, systems.store, systemID, func(tx transaction) error {
		if err := writeTalkgroups(ctx, tx, systems.store.dialect, systemID, talkgroups); err != nil {
			return err
		}

		return writeSystemBlacklists(ctx, tx, systemID, blacklists)
	}); err != nil {
		return err
	}

	loaded, clamped, err := readSystems(ctx, systems.store)
	if err != nil {
		return err
	}

	systems.live = loaded
	systems.reportClamps(clamped)

	return nil
}

// SaveSystemUnits replaces the units of one system, renumbering their order.
func (systems *Systems) SaveSystemUnits(ctx context.Context, systemID uint64, units domain.Units) error {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	// The renumbering is this save's own work: the caller's slice stays untouched.
	units = append(domain.Units{}, units...)

	for i := range units {
		units[i].Order = uint(i + 1)
		units[i].SystemID = systemID
	}

	if err := saveSystemLevel(ctx, systems.store, systemID, func(tx transaction) error {
		return writeUnits(ctx, tx, systemID, units)
	}); err != nil {
		return err
	}

	loaded, clamped, err := readSystems(ctx, systems.store)
	if err != nil {
		return err
	}

	systems.live = loaded
	systems.reportClamps(clamped)

	return nil
}

// saveSystemLevel performs the shared part of the three per-level saves: one
// transaction that checks the system exists, writes the level's rows, and commits; a
// system the table does not have is reported as ErrNotFound, which the REST layer
// answers with 404.
func saveSystemLevel(ctx context.Context, s store, systemID uint64, write func(transaction) error) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return operationError("systems", "savesystemlevel", err)
	}

	defer tx.rollback()

	const query = `SELECT COUNT(*) FROM "systems" WHERE "systemId" = ?`

	var count uint64
	if err := tx.queryRow(ctx, query, systemID).Scan(&count); err != nil {
		return statementError("systems", "savesystemlevel", err, query)
	}

	if count == 0 {
		return ports.ErrNotFound
	}

	if err := write(tx); err != nil {
		return err
	}

	return tx.commit()
}

// readSystems is the legacy `Systems.Read`: one transaction that reads the catalog and each
// system's sites, talkgroups, and units through it, returning the result sorted by display
// order; the transaction is asked for as read-only, per the systems aggregate transaction
// maintainer note.
func readSystems(ctx context.Context, s store) (domain.Systems, []string, error) {
	tx, err := s.beginRead(ctx)
	if err != nil {
		return nil, nil, operationError("systems", "read", err)
	}

	defer tx.rollback()

	const query = `SELECT "systemId", "alert", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type" FROM "systems"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		return nil, nil, statementError("systems", "read", err, query)
	}

	defer rows.Close()

	out := domain.Systems{}

	clamped := []string{}

	for rows.Next() {
		var (
			id        int64
			delay     int64
			order     int64
			systemRef int64
		)

		system := domain.System{}

		if err := rows.Scan(&id, &system.Alert, &system.AutoPopulate, &system.Blacklists, &delay, &system.Label, &system.LED, &order, &systemRef, &system.Kind); err != nil {
			return nil, nil, operationError("systems", "read", err)
		}

		// The signed delay, order, and systemRef columns are clamped to zero on the
		// way into the unsigned fields; why, recorded in the signed-column-clamp
		// maintainer note.
		clamps := []string{}

		if delay < 0 {
			clamps = append(clamps, fmt.Sprintf("delay %d read as 0", delay))
		}

		if order < 0 {
			clamps = append(clamps, fmt.Sprintf("order %d read as 0", order))
		}

		if systemRef < 0 {
			clamps = append(clamps, fmt.Sprintf("systemRef %d read as 0", systemRef))
		}

		if len(clamps) > 0 {
			clamped = append(clamped, fmt.Sprintf("systems.read: system %d %q holds %s: these are not values this server can hold, and it enforces the clamped ones",
				id, system.Label, strings.Join(clamps, ", ")))
		}

		system.ID = uint64(id)
		system.Delay = uint(clampNonNegative(delay))
		system.Order = uint(clampNonNegative(order))
		system.SystemRef = uint(clampNonNegative(systemRef))

		out = append(out, system)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, operationError("systems", "read", err)
	}

	rows.Close()

	for i := range out {
		var siteClamps, talkgroupClamps, unitClamps []string

		if out[i].Sites, siteClamps, err = readSites(ctx, tx, out[i].ID); err != nil {
			return nil, nil, err
		}

		if out[i].Talkgroups, talkgroupClamps, err = readTalkgroups(ctx, tx, s.dialect, out[i].ID); err != nil {
			return nil, nil, err
		}

		if out[i].Units, unitClamps, err = readUnits(ctx, tx, out[i].ID); err != nil {
			return nil, nil, err
		}

		clamped = append(clamped, siteClamps...)
		clamped = append(clamped, talkgroupClamps...)
		clamped = append(clamped, unitClamps...)
	}

	if err := tx.commit(); err != nil {
		return nil, nil, operationError("systems", "read", err)
	}

	// The clamp lines are collected, not said, inside the reads: the catalog read
	// holds the transaction open while it walks it, and a line said here is a write
	// into the database the cursor is reading — the server's own read blocks its own
	// report.
	return out.Ordered(), clamped, nil
}

// reportClamps says the collected clamp lines, once the read that collected them
// has closed its cursor.
func (systems *Systems) reportClamps(clamped []string) {
	if systems.warn == nil {
		return
	}

	for _, line := range clamped {
		systems.warn(line)
	}
}

// readSites is the legacy `Sites.ReadTx`: the stored `systemId` filters the query and the site
// is given the system it was found under, per the patched systems catalog reads maintainer
// note.
func readSites(ctx context.Context, tx transaction, systemID uint64) (domain.Sites, []string, error) {
	const query = `SELECT "siteId", "label", "order", "siteRef" FROM "sites" WHERE "systemId" = ?`

	rows, err := tx.query(ctx, query, systemID)
	if err != nil {
		return nil, nil, statementError("sites", "read", err, query)
	}

	defer rows.Close()

	out := domain.Sites{}

	clamped := []string{}

	for rows.Next() {
		var (
			id      int64
			order   int64
			siteRef int64
		)

		site := domain.Site{}

		if err := rows.Scan(&id, &site.Label, &order, &siteRef); err != nil {
			return nil, nil, operationError("sites", "read", err)
		}

		// The signed order and siteRef columns are clamped to zero on the way into
		// the unsigned fields; why, recorded in the signed-column-clamp maintainer
		// note.
		clamps := []string{}

		if order < 0 {
			clamps = append(clamps, fmt.Sprintf("order %d read as 0", order))
		}

		if siteRef < 0 {
			clamps = append(clamps, fmt.Sprintf("siteRef %d read as 0", siteRef))
		}

		if len(clamps) > 0 {
			clamped = append(clamped, fmt.Sprintf("systems.read: site %d %q holds %s: these are not values this server can hold, and it enforces the clamped ones",
				id, site.Label, strings.Join(clamps, ", ")))
		}

		site.ID = uint64(id)
		site.Order = uint(clampNonNegative(order))
		site.SiteRef = uint(clampNonNegative(siteRef))
		site.SystemID = systemID

		out = append(out, site)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, operationError("sites", "read", err)
	}

	return out.Ordered(), clamped, nil
}

// readTalkgroups is the legacy `Talkgroups.ReadTx`: the talkgroup columns plus its group
// memberships aggregated out of `talkgroupGroups` in the same row; `order` and `talkgroupRef`
// are read from the columns they are stored in, never from each other, per the patched systems
// catalog reads maintainer note.
func readTalkgroups(ctx context.Context, tx transaction, dialect Dialect, systemID uint64) (domain.Talkgroups, []string, error) {
	query := talkgroupsReadQuery(dialect)

	rows, err := tx.query(ctx, query, systemID)
	if err != nil {
		return nil, nil, statementError("talkgroups", "read", err, query)
	}

	defer rows.Close()

	out := domain.Talkgroups{}

	clamped := []string{}

	for rows.Next() {
		var (
			id           int64
			delay        int64
			frequency    int64
			order        int64
			tagID        int64
			talkgroupRef int64
			groups       string
		)

		talkgroup := domain.Talkgroup{}

		if err := rows.Scan(&id, &talkgroup.Alert, &delay, &frequency, &talkgroup.Label, &talkgroup.LED, &talkgroup.Name, &order, &tagID, &talkgroupRef, &talkgroup.Kind, &groups); err != nil {
			return nil, nil, operationError("talkgroups", "read", err)
		}

		// The signed delay, frequency, order, tagId, and talkgroupRef columns are
		// clamped to zero on the way into the unsigned fields; why, recorded in the
		// signed-column-clamp maintainer note.
		clamps := []string{}

		if delay < 0 {
			clamps = append(clamps, fmt.Sprintf("delay %d read as 0", delay))
		}

		if frequency < 0 {
			clamps = append(clamps, fmt.Sprintf("frequency %d read as 0", frequency))
		}

		if order < 0 {
			clamps = append(clamps, fmt.Sprintf("order %d read as 0", order))
		}

		if tagID < 0 {
			clamps = append(clamps, fmt.Sprintf("tagId %d read as 0", tagID))
		}

		if talkgroupRef < 0 {
			clamps = append(clamps, fmt.Sprintf("talkgroupRef %d read as 0", talkgroupRef))
		}

		if len(clamps) > 0 {
			clamped = append(clamped, fmt.Sprintf("systems.read: talkgroup %d %q holds %s: these are not values this server can hold, and it enforces the clamped ones",
				id, talkgroup.Label, strings.Join(clamps, ", ")))
		}

		talkgroup.ID = uint64(id)
		talkgroup.Delay = uint(clampNonNegative(delay))
		talkgroup.Frequency = uint(clampNonNegative(frequency))
		talkgroup.Order = uint(clampNonNegative(order))
		talkgroup.TagID = clampNonNegative(tagID)
		talkgroup.TalkgroupRef = uint(clampNonNegative(talkgroupRef))

		talkgroup.GroupIDs = parseGroupIDs(groups)

		out = append(out, talkgroup)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, operationError("talkgroups", "read", err)
	}

	return out.Ordered(), clamped, nil
}

// readUnits is the legacy `Units.ReadTx`. Like the sites read, it reads the unit columns and
// gives the unit the system it was found under.
func readUnits(ctx context.Context, tx transaction, systemID uint64) (domain.Units, []string, error) {
	const query = `SELECT "unitId", "label", "order", "unitRef", "unitFrom", "unitTo" FROM "units" WHERE "systemId" = ?`

	rows, err := tx.query(ctx, query, systemID)
	if err != nil {
		return nil, nil, statementError("units", "read", err, query)
	}

	defer rows.Close()

	out := domain.Units{}

	clamped := []string{}

	for rows.Next() {
		var (
			id       int64
			order    int64
			unitRef  int64
			unitFrom int64
			unitTo   int64
		)

		unit := domain.Unit{}

		if err := rows.Scan(&id, &unit.Label, &order, &unitRef, &unitFrom, &unitTo); err != nil {
			return nil, nil, operationError("units", "read", err)
		}

		// The signed order, unitRef, unitFrom, and unitTo columns are clamped to
		// zero on the way into the unsigned fields; why, recorded in the
		// signed-column-clamp maintainer note.
		clamps := []string{}

		if order < 0 {
			clamps = append(clamps, fmt.Sprintf("order %d read as 0", order))
		}

		if unitRef < 0 {
			clamps = append(clamps, fmt.Sprintf("unitRef %d read as 0", unitRef))
		}

		if unitFrom < 0 {
			clamps = append(clamps, fmt.Sprintf("unitFrom %d read as 0", unitFrom))
		}

		if unitTo < 0 {
			clamps = append(clamps, fmt.Sprintf("unitTo %d read as 0", unitTo))
		}

		if len(clamps) > 0 {
			clamped = append(clamped, fmt.Sprintf("systems.read: unit %d %q holds %s: these are not values this server can hold, and it enforces the clamped ones",
				id, unit.Label, strings.Join(clamps, ", ")))
		}

		unit.ID = uint64(id)
		unit.Order = uint(clampNonNegative(order))
		unit.UnitRef = uint(clampNonNegative(unitRef))
		unit.UnitFrom = uint(clampNonNegative(unitFrom))
		unit.UnitTo = uint(clampNonNegative(unitTo))
		unit.SystemID = systemID

		out = append(out, unit)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, operationError("units", "read", err)
	}

	return out.Ordered(), clamped, nil
}

// talkgroupsReadQuery is the one statement in this file spelled two ways: the aggregate that
// folds a talkgroup's group rows into one value is named differently by the backends, per the
// patched systems catalog reads maintainer note. The value the query is filtered by is always a
// placeholder.
func talkgroupsReadQuery(dialect Dialect) string {
	aggregate := `GROUP_CONCAT(COALESCE(tg."groupId", 0))`

	if dialect == DialectPostgresql {
		aggregate = `STRING_AGG(CAST(COALESCE(tg."groupId", 0) AS text), ',')`
	}

	return `SELECT t."talkgroupId", t."alert", t."delay", t."frequency", t."label", t."led", t."name", t."order", t."tagId", t."talkgroupRef", t."type", ` + aggregate + ` FROM "talkgroups" AS t LEFT JOIN "talkgroupGroups" AS tg ON tg."talkgroupId" = t."talkgroupId" WHERE t."systemId" = ? GROUP BY t."talkgroupId"`
}

// parseGroupIDs reads the aggregated group identifiers back. A talkgroup with no groups arrives as
// the single zero the left join produces, and a zero is not a group.
func parseGroupIDs(text string) []uint64 {
	ids := []uint64{}

	for _, entry := range strings.Split(text, ",") {
		if id, err := strconv.Atoi(entry); err == nil && id > 0 {
			ids = append(ids, uint64(id))
		}
	}

	return ids
}

// writeSystems is the legacy `Systems.Write`: one transaction that selects the stored identifiers,
// deletes the ones the submitted list no longer names, and inserts or updates the rest — each
// system together with its sites, talkgroups, and units; a failure anywhere discards the whole
// section, per the systems aggregate transaction maintainer note.
func writeSystems(ctx context.Context, s store, next domain.Systems) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return operationError("systems", "write", err)
	}

	defer tx.rollback()

	stored, err := readColumnIDs(ctx, tx, "systems", "write", `SELECT "systemId" FROM "systems"`)
	if err != nil {
		return err
	}

	// Deleting a system takes its talkgroups, sites, units, and recorded calls with it:
	// every one of those tables references "systems" ON DELETE CASCADE, which the
	// connection's foreign-keys pragma enforces.
	if removed := droppedIdentifiers(idsOfSystems(next), stored); len(removed) > 0 {
		query := `DELETE FROM "systems" WHERE "systemId" IN (` + placeholders(len(removed)) + `)`

		if _, err := tx.exec(ctx, query, valuesOf(removed)...); err != nil {
			return statementError("systems", "write", err, query)
		}
	}

	for _, system := range next {
		id, err := writeSystemRow(ctx, tx, s, system)
		if err != nil {
			return err
		}

		if err := writeSites(ctx, tx, id, system.Sites); err != nil {
			return err
		}

		if err := writeTalkgroups(ctx, tx, s.dialect, id, system.Talkgroups); err != nil {
			return err
		}

		if err := writeUnits(ctx, tx, id, system.Units); err != nil {
			return err
		}
	}

	if err := tx.commit(); err != nil {
		return operationError("systems", "write", err)
	}

	return nil
}

// writeSystemSettings is the systems-section save: the same reconcile of the systems table
// the whole-catalog save performs, and nothing under a system.
func writeSystemSettings(ctx context.Context, s store, next domain.Systems) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return operationError("systems", "writesystemsettings", err)
	}

	defer tx.rollback()

	stored, err := readColumnIDs(ctx, tx, "systems", "writesystemsettings", `SELECT "systemId" FROM "systems"`)
	if err != nil {
		return err
	}

	if removed := droppedIdentifiers(idsOfSystems(next), stored); len(removed) > 0 {
		query := `DELETE FROM "systems" WHERE "systemId" IN (` + placeholders(len(removed)) + `)`

		if _, err := tx.exec(ctx, query, valuesOf(removed)...); err != nil {
			return statementError("systems", "writesystemsettings", err, query)
		}
	}

	for _, system := range next {
		if _, err := writeSystemRow(ctx, tx, s, system); err != nil {
			return err
		}
	}

	if err := tx.commit(); err != nil {
		return operationError("systems", "writesystemsettings", err)
	}

	return nil
}

// writeSystemRow inserts or updates one system row and answers with the identity the table holds
// for it. Both the whole-catalog save and the systems-section save write the row this way.
func writeSystemRow(ctx context.Context, tx transaction, s store, system domain.System) (uint64, error) {
	var (
		id    = system.ID
		count uint64
	)

	if id > 0 {
		const query = `SELECT COUNT(*) FROM "systems" WHERE "systemId" = ?`

		if err := tx.queryRow(ctx, query, id).Scan(&count); err != nil {
			return 0, statementError("systems", "write", err, query)
		}
	}

	if count == 0 {
		const query = `INSERT INTO "systems" ("alert", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

		id, err := insertAssignedID(ctx, tx, s.dialect, query, `RETURNING "systemId"`,
			system.Alert, system.AutoPopulate, string(system.Blacklists), system.Delay, system.Label, system.LED, system.Order, system.SystemRef, system.Kind)
		if err != nil {
			return 0, statementError("systems", "write", err, query)
		}

		return id, nil
	}

	const query = `UPDATE "systems" SET "alert" = ?, "autoPopulate" = ?, "blacklists" = ?, "delay" = ?, "label" = ?, "led" = ?, "order" = ?, "systemRef" = ?, "type" = ? WHERE "systemId" = ?`

	if _, err := tx.exec(ctx, query,
		system.Alert, system.AutoPopulate, string(system.Blacklists), system.Delay, system.Label, system.LED, system.Order, system.SystemRef, system.Kind, id); err != nil {
		return 0, statementError("systems", "write", err, query)
	}

	return id, nil
}

// writeSystemBlacklists stores a system's blacklist text, in the transaction that writes the
// talkgroups it was edited beside. A save that names no blacklist leaves the column as it is.
func writeSystemBlacklists(ctx context.Context, tx transaction, systemID uint64, blacklists *domain.Blacklists) error {
	if blacklists == nil {
		return nil
	}

	const query = `UPDATE "systems" SET "blacklists" = ? WHERE "systemId" = ?`

	if _, err := tx.exec(ctx, query, string(*blacklists), systemID); err != nil {
		return statementError("systems", "savetalkgroups", err, query)
	}

	return nil
}

// writeSites is the legacy `Sites.WriteTx`: the same reconcile, then insert or update, with
// the system being saved stored as the site's own — not the one the submitted site carries.
func writeSites(ctx context.Context, tx transaction, systemID uint64, sites domain.Sites) error {
	const storedQuery = `SELECT "siteId" FROM "sites" WHERE "systemId" = ?`

	stored, err := readColumnIDs(ctx, tx, "sites", "writetx", storedQuery, systemID)
	if err != nil {
		return err
	}

	if removed := droppedIdentifiers(idsOfSites(sites), stored); len(removed) > 0 {
		query := `DELETE FROM "sites" WHERE "siteId" IN (` + placeholders(len(removed)) + `)`

		if _, err := tx.exec(ctx, query, valuesOf(removed)...); err != nil {
			return statementError("sites", "writetx", err, query)
		}
	}

	for _, site := range sites {
		var (
			id    = site.ID
			count uint64
		)

		if id > 0 {
			const query = `SELECT COUNT(*) FROM "sites" WHERE "siteId" = ?`

			if err := tx.queryRow(ctx, query, id).Scan(&count); err != nil {
				return statementError("sites", "writetx", err, query)
			}
		}

		if count == 0 {
			const query = `INSERT INTO "sites" ("label", "order", "siteRef", "systemId") VALUES (?, ?, ?, ?)`

			if _, err := tx.exec(ctx, query, site.Label, site.Order, site.SiteRef, systemID); err != nil {
				return statementError("sites", "writetx", err, query)
			}

		} else {
			const query = `UPDATE "sites" SET "label" = ?, "order" = ?, "siteRef" = ? WHERE "siteId" = ?`

			if _, err := tx.exec(ctx, query, site.Label, site.Order, site.SiteRef, id); err != nil {
				return statementError("sites", "writetx", err, query)
			}
		}
	}

	return nil
}

// writeTalkgroups is the legacy `Talkgroups.WriteTx`: reconcile the system's talkgroups, insert
// or update each one, then reconcile the `talkgroupGroups` rows that carry its groups; the update
// never writes `systemId`, per the talkgroup save repairs maintainer note.
func writeTalkgroups(ctx context.Context, tx transaction, dialect Dialect, systemID uint64, talkgroups domain.Talkgroups) error {
	const storedQuery = `SELECT "talkgroupId" FROM "talkgroups" WHERE "systemId" = ?`

	stored, err := readColumnIDs(ctx, tx, "talkgroups", "writetx", storedQuery, systemID)
	if err != nil {
		return err
	}

	// The join rows follow them: "talkgroupGroups" references "talkgroups" ON DELETE CASCADE.
	if removed := droppedIdentifiers(idsOfTalkgroups(talkgroups), stored); len(removed) > 0 {
		query := `DELETE FROM "talkgroups" WHERE "talkgroupId" IN (` + placeholders(len(removed)) + `)`

		if _, err := tx.exec(ctx, query, valuesOf(removed)...); err != nil {
			return statementError("talkgroups", "writetx", err, query)
		}
	}

	for _, talkgroup := range talkgroups {
		var (
			id    = talkgroup.ID
			count uint64
		)

		if id > 0 {
			const query = `SELECT COUNT(*) FROM "talkgroups" WHERE "talkgroupId" = ?`

			if err := tx.queryRow(ctx, query, id).Scan(&count); err != nil {
				return statementError("talkgroups", "writetx", err, query)
			}
		}

		if count == 0 {
			const query = `INSERT INTO "talkgroups" ("alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

			id, err = insertAssignedID(ctx, tx, dialect, query, `RETURNING "talkgroupId"`,
				talkgroup.Alert, talkgroup.Delay, talkgroup.Frequency, talkgroup.Label, talkgroup.LED, talkgroup.Name, talkgroup.Order, systemID, talkgroup.TagID, talkgroup.TalkgroupRef, talkgroup.Kind)
			if err != nil {
				return statementError("talkgroups", "writetx", err, query)
			}

		} else {
			const query = `UPDATE "talkgroups" SET "alert" = ?, "delay" = ?, "frequency" = ?, "label" = ?, "led" = ?, "name" = ?, "order" = ?, "tagId" = ?, "talkgroupRef" = ?, "type" = ? WHERE "talkgroupId" = ?`

			if _, err := tx.exec(ctx, query,
				talkgroup.Alert, talkgroup.Delay, talkgroup.Frequency, talkgroup.Label, talkgroup.LED, talkgroup.Name, talkgroup.Order, talkgroup.TagID, talkgroup.TalkgroupRef, talkgroup.Kind, id); err != nil {
				return statementError("talkgroups", "writetx", err, query)
			}
		}

		if err := writeTalkgroupGroups(ctx, tx, id, talkgroup.GroupIDs); err != nil {
			return err
		}
	}

	return nil
}

// writeTalkgroupGroups reconciles one talkgroup's group memberships: stored memberships the
// submitted list does not name are removed, and a pair is inserted only when it is not already
// stored; the comparison uses `groupId`, per the talkgroup save repairs maintainer note.
func writeTalkgroupGroups(ctx context.Context, tx transaction, talkgroupID uint64, groupIDs []uint64) error {
	const storedQuery = `SELECT "groupId" FROM "talkgroupGroups" WHERE "talkgroupId" = ?`

	stored, err := readColumnIDs(ctx, tx, "talkgroups", "writetx", storedQuery, talkgroupID)
	if err != nil {
		return err
	}

	if removed := droppedIdentifiers(groupIDs, stored); len(removed) > 0 {
		query := `DELETE FROM "talkgroupGroups" WHERE "talkgroupId" = ? AND "groupId" IN (` + placeholders(len(removed)) + `)`

		if _, err := tx.exec(ctx, query, append([]any{talkgroupID}, valuesOf(removed)...)...); err != nil {
			return statementError("talkgroups", "writetx", err, query)
		}
	}

	for _, groupID := range groupIDs {
		const existsQuery = `SELECT COUNT(*) FROM "talkgroupGroups" WHERE "talkgroupId" = ? AND "groupId" = ?`

		var count uint64

		if err := tx.queryRow(ctx, existsQuery, talkgroupID, groupID).Scan(&count); err != nil {
			return statementError("talkgroups", "writetx", err, existsQuery)
		}

		if count > 0 {
			continue
		}

		const query = `INSERT INTO "talkgroupGroups" ("groupId", "talkgroupId") VALUES (?, ?)`

		if _, err := tx.exec(ctx, query, groupID, talkgroupID); err != nil {
			return statementError("talkgroups", "writetx", err, query)
		}
	}

	return nil
}

// writeUnits is the legacy `Units.WriteTx`.
func writeUnits(ctx context.Context, tx transaction, systemID uint64, units domain.Units) error {
	const storedQuery = `SELECT "unitId" FROM "units" WHERE "systemId" = ?`

	stored, err := readColumnIDs(ctx, tx, "units", "writetx", storedQuery, systemID)
	if err != nil {
		return err
	}

	if removed := droppedIdentifiers(idsOfUnits(units), stored); len(removed) > 0 {
		query := `DELETE FROM "units" WHERE "unitId" IN (` + placeholders(len(removed)) + `)`

		if _, err := tx.exec(ctx, query, valuesOf(removed)...); err != nil {
			return statementError("units", "writetx", err, query)
		}
	}

	for _, unit := range units {
		var (
			id    = unit.ID
			count uint64
		)

		if id > 0 {
			const query = `SELECT COUNT(*) FROM "units" WHERE "unitId" = ?`

			if err := tx.queryRow(ctx, query, id).Scan(&count); err != nil {
				return statementError("units", "writetx", err, query)
			}
		}

		if count == 0 {
			const query = `INSERT INTO "units" ("label", "order", "systemId", "unitRef", "unitFrom", "unitTo") VALUES (?, ?, ?, ?, ?, ?)`

			if _, err := tx.exec(ctx, query, unit.Label, unit.Order, systemID, unit.UnitRef, unit.UnitFrom, unit.UnitTo); err != nil {
				return statementError("units", "writetx", err, query)
			}

		} else {
			const query = `UPDATE "units" SET "label" = ?, "order" = ?, "unitRef" = ?, "unitFrom" = ?, "unitTo" = ? WHERE "unitId" = ?`

			if _, err := tx.exec(ctx, query, unit.Label, unit.Order, unit.UnitRef, unit.UnitFrom, unit.UnitTo, id); err != nil {
				return statementError("units", "writetx", err, query)
			}
		}
	}

	return nil
}

// readColumnIDs is the first step of every legacy reconcile: the identifiers the table currently
// holds.
func readColumnIDs(ctx context.Context, tx transaction, section, operation, query string, args ...any) ([]uint64, error) {
	rows, err := tx.query(ctx, query, args...)
	if err != nil {
		return nil, statementError(section, operation, err, query)
	}

	defer rows.Close()

	out := []uint64{}

	for rows.Next() {
		var id uint64

		if err := rows.Scan(&id); err != nil {
			return nil, operationError(section, operation, err)
		}

		out = append(out, id)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError(section, operation, err)
	}

	rows.Close()

	return out, nil
}

// droppedIdentifiers returns the stored identifiers the submitted collection does not name,
// the repaired keep-rule every section saves with, per the reconciling saves maintainer note.
func droppedIdentifiers(submitted, stored []uint64) []uint64 {
	out := []uint64{}

	for _, id := range stored {
		remove := true

		for _, candidate := range submitted {
			if candidate == id {
				remove = false

				break
			}
		}

		if remove {
			out = append(out, id)
		}
	}

	return out
}

// insertAssignedID runs an insert and reports the identifier the table assigned. PostgreSQL
// is the one backend whose driver cannot report it, so the statement asks for it back.
func insertAssignedID(ctx context.Context, tx transaction, dialect Dialect, query string, returning string, args ...any) (uint64, error) {
	if dialect == DialectPostgresql {
		var id uint64

		if err := tx.queryRow(ctx, query+` `+returning, args...).Scan(&id); err != nil {
			return 0, err
		}

		return id, nil
	}

	res, err := tx.exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return uint64(id), nil
}

// valuesOf turns identifiers into bound arguments.
func valuesOf(ids []uint64) []any {
	out := make([]any, 0, len(ids))

	for _, id := range ids {
		out = append(out, id)
	}

	return out
}

func idsOfSystems(systems domain.Systems) []uint64 {
	out := make([]uint64, 0, len(systems))

	for _, system := range systems {
		out = append(out, system.ID)
	}

	return out
}

func idsOfTalkgroups(talkgroups domain.Talkgroups) []uint64 {
	out := make([]uint64, 0, len(talkgroups))

	for _, talkgroup := range talkgroups {
		out = append(out, talkgroup.ID)
	}

	return out
}

func idsOfSites(sites domain.Sites) []uint64 {
	out := make([]uint64, 0, len(sites))

	for _, site := range sites {
		out = append(out, site.ID)
	}

	return out
}

func idsOfUnits(units domain.Units) []uint64 {
	out := make([]uint64, 0, len(units))

	for _, unit := range units {
		out = append(out, unit.ID)
	}

	return out
}
