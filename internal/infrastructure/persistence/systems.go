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
	"strconv"
	"strings"
	"sync"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Systems is the stored system catalog: the repository adapter that owns a system and everything
// hanging off it — its talkgroups, the talkgroupGroups rows that say which groups a talkgroup
// belongs to, its sites, and its units — because the legacy server writes all of it inside one
// system transaction (`server/system.go` `Systems.Write`, calling
// `Sites.WriteTx`, `Talkgroups.WriteTx`, and `Units.WriteTx`).
//
// It is the configuration the server is running with, not a view of the tables: the catalog is
// loaded once, `SaveSystems` replaces it with what was submitted before it stores anything, and a
// save that stored successfully is followed by reading the stored rows back — which is how the
// legacy server hands the identifiers its tables assigned to the next save and to the
// configuration broadcast.
//
// The `server/…` names in this file are the legacy server's own files, which are not in this
// repository. They are named so a reader can see which behavior is being mirrored, and they resolve
// at git tree 02ae33d4651fee4c46a93bdcf574d0aead89e542.
type Systems struct {
	store store

	// live is the catalog in effect. known says whether it has been loaded yet, so that the
	// first read is the only one that goes to the database.
	mutex sync.Mutex
	live  domain.Systems
	known bool
}

var _ ports.SystemRepository = (*Systems)(nil)

// NewSystems builds the adapter for one opened database.
func NewSystems(db *sql.DB, dialect Dialect) *Systems {
	return &Systems{store: newStore(db, dialect)}
}

// AllSystems returns the systems the server is running with, with their talkgroups, sites, and
// units loaded. The catalog is read from the database once and kept; a later read answers from
// what is in effect, exactly as the legacy server answers from `Systems.List`.
func (systems *Systems) AllSystems(ctx context.Context) (domain.Systems, error) {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	if systems.known {
		return systems.live, nil
	}

	loaded, err := readSystems(ctx, systems.store)
	if err != nil {
		return nil, err
	}

	systems.live = loaded
	systems.known = true

	return loaded, nil
}

// SaveSystems reconciles the stored systems with the given ones and leaves the reconciled result
// as what the server runs with.
//
// The submitted catalog becomes the live one before anything is stored, so a save that could not
// be persisted is still in effect for the rest of the run, the legacy server's order of operations.
// After a save that did persist, the stored rows are read back and cached: that re-read is what
// assigns the identities the next save and the configuration broadcast use.
func (systems *Systems) SaveSystems(ctx context.Context, next domain.Systems) error {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	systems.live = next
	systems.known = true

	if err := writeSystems(ctx, systems.store, next); err != nil {
		return err
	}

	loaded, err := readSystems(ctx, systems.store)
	if err != nil {
		// The legacy server logs a failed re-read and carries on with whatever the partial read
		// left in the list. Here the failure is reported to the caller, and what stays in effect
		// is what was submitted.
		return err
	}

	systems.live = loaded

	return nil
}

// SaveSystemSites replaces the sites of one system: the submitted rows are renumbered
// 1-based and reconciled with the stored rows, exactly as the whole-systems save does
// them, and a system the store does not have is reported as ErrNotFound.
func (systems *Systems) SaveSystemSites(ctx context.Context, systemID uint64, sites domain.Sites) error {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	// The renumbering below is this save's own work, not a service to the caller:
	// the submitted slice stays untouched (CA-017.4).
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

	loaded, err := readSystems(ctx, systems.store)
	if err != nil {
		return err
	}

	systems.live = loaded

	return nil
}

// SaveSystemTalkgroups replaces the talkgroups of one system, renumbering their order
// and reconciling their group memberships.
func (systems *Systems) SaveSystemTalkgroups(ctx context.Context, systemID uint64, talkgroups domain.Talkgroups) error {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	// The renumbering is this save's own work: the caller's slice stays untouched
	// (CA-017.4).
	talkgroups = append(domain.Talkgroups{}, talkgroups...)

	for i := range talkgroups {
		talkgroups[i].Order = uint(i + 1)
	}

	if err := saveSystemLevel(ctx, systems.store, systemID, func(tx transaction) error {
		return writeTalkgroups(ctx, tx, systems.store.dialect, systemID, talkgroups)
	}); err != nil {
		return err
	}

	loaded, err := readSystems(ctx, systems.store)
	if err != nil {
		return err
	}

	systems.live = loaded

	return nil
}

// SaveSystemUnits replaces the units of one system, renumbering their order.
func (systems *Systems) SaveSystemUnits(ctx context.Context, systemID uint64, units domain.Units) error {
	systems.mutex.Lock()
	defer systems.mutex.Unlock()

	// The renumbering is this save's own work: the caller's slice stays untouched
	// (CA-017.4).
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

	loaded, err := readSystems(ctx, systems.store)
	if err != nil {
		return err
	}

	systems.live = loaded

	return nil
}

// saveSystemLevel performs the shared part of the three per-level saves: one
// transaction that checks the system exists, writes the level's rows, and commits. A
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

// readSystems is the legacy `Systems.Read`: one transaction that reads the catalog and then reads
// each system's sites, talkgroups, and units through the same transaction, and hands back the
// result sorted by display order.
func readSystems(ctx context.Context, s store) (domain.Systems, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, operationError("systems", "read", err)
	}

	defer tx.rollback()

	const query = `SELECT "systemId", "alert", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type" FROM "systems"`

	rows, err := tx.query(ctx, query)
	if err != nil {
		return nil, statementError("systems", "read", err, query)
	}

	defer rows.Close()

	out := domain.Systems{}

	for rows.Next() {
		var system domain.System

		if err := rows.Scan(&system.ID, &system.Alert, &system.AutoPopulate, &system.Blacklists, &system.Delay, &system.Label, &system.LED, &system.Order, &system.SystemRef, &system.Kind); err != nil {
			return nil, operationError("systems", "read", err)
		}

		out = append(out, system)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("systems", "read", err)
	}

	rows.Close()

	for i := range out {
		if out[i].Sites, err = readSites(ctx, tx, out[i].ID); err != nil {
			return nil, err
		}

		if out[i].Talkgroups, err = readTalkgroups(ctx, tx, s.dialect, out[i].ID); err != nil {
			return nil, err
		}

		if out[i].Units, err = readUnits(ctx, tx, out[i].ID); err != nil {
			return nil, err
		}
	}

	if err := tx.commit(); err != nil {
		return nil, operationError("systems", "read", err)
	}

	return out.Ordered(), nil
}

// readSites is the legacy `Sites.ReadTx`. The stored `systemId` is what the query filters on, and
// the site is given the system it was found under; the legacy server leaves its in-memory
// `Site.SystemId` at zero because its read never names the column, which the legacy admin payload
// then reports as `"systemId": 0`.
func readSites(ctx context.Context, tx transaction, systemID uint64) (domain.Sites, error) {
	const query = `SELECT "siteId", "label", "order", "siteRef" FROM "sites" WHERE "systemId" = ?`

	rows, err := tx.query(ctx, query, systemID)
	if err != nil {
		return nil, statementError("sites", "read", err, query)
	}

	defer rows.Close()

	out := domain.Sites{}

	for rows.Next() {
		var site domain.Site

		if err := rows.Scan(&site.ID, &site.Label, &site.Order, &site.SiteRef); err != nil {
			return nil, operationError("sites", "read", err)
		}

		site.SystemID = systemID

		out = append(out, site)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("sites", "read", err)
	}

	return out.Ordered(), nil
}

// readTalkgroups is the legacy `Talkgroups.ReadTx`: the talkgroup columns plus its group
// memberships aggregated out of `talkgroupGroups` in the same row.
//
// The two keys this read has to keep apart are `order` and `talkgroupRef`: the legacy server
// marshals the display order out under the JSON key `"talkgroup"` (quirk Q4) while the table
// column is `"order"`, and the reference clients identify the talkgroup by the column
// `"talkgroupRef"`. Both are read from the column they are stored in, never from each other.
func readTalkgroups(ctx context.Context, tx transaction, dialect Dialect, systemID uint64) (domain.Talkgroups, error) {
	query := talkgroupsReadQuery(dialect)

	rows, err := tx.query(ctx, query, systemID)
	if err != nil {
		return nil, statementError("talkgroups", "read", err, query)
	}

	defer rows.Close()

	out := domain.Talkgroups{}

	for rows.Next() {
		var (
			talkgroup domain.Talkgroup
			groups    string
		)

		if err := rows.Scan(&talkgroup.ID, &talkgroup.Alert, &talkgroup.Delay, &talkgroup.Frequency, &talkgroup.Label, &talkgroup.LED, &talkgroup.Name, &talkgroup.Order, &talkgroup.TagID, &talkgroup.TalkgroupRef, &talkgroup.Kind, &groups); err != nil {
			return nil, operationError("talkgroups", "read", err)
		}

		talkgroup.GroupIDs = parseGroupIDs(groups)

		out = append(out, talkgroup)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("talkgroups", "read", err)
	}

	return out.Ordered(), nil
}

// readUnits is the legacy `Units.ReadTx`. Like the sites read, it reads the unit columns and
// gives the unit the system it was found under.
func readUnits(ctx context.Context, tx transaction, systemID uint64) (domain.Units, error) {
	const query = `SELECT "unitId", "label", "order", "unitRef", "unitFrom", "unitTo" FROM "units" WHERE "systemId" = ?`

	rows, err := tx.query(ctx, query, systemID)
	if err != nil {
		return nil, statementError("units", "read", err, query)
	}

	defer rows.Close()

	out := domain.Units{}

	for rows.Next() {
		var unit domain.Unit

		if err := rows.Scan(&unit.ID, &unit.Label, &unit.Order, &unit.UnitRef, &unit.UnitFrom, &unit.UnitTo); err != nil {
			return nil, operationError("units", "read", err)
		}

		unit.SystemID = systemID

		out = append(out, unit)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("units", "read", err)
	}

	return out.Ordered(), nil
}

// talkgroupsReadQuery is the one statement in this file that has to be spelled two ways, because
// the aggregate that folds a talkgroup's group rows into one value is named differently by the
// backends — the same two constants the legacy `Talkgroups.ReadTx` switches between. The choice is
// between constants this package owns; the value the query is filtered by is always a placeholder.
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
// system together with its sites, talkgroups, and units. A failure anywhere in the aggregate
// discards the whole systems section.
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

	// Deleting a system takes its talkgroups, sites, units, and recorded calls with it: every one
	// of those tables references "systems" ON DELETE CASCADE, and the connection is opened with
	// the pragma that enforces it. The legacy server names the child tables in its own delete
	// statements, which the cascade has already emptied.
	if removed := droppedIdentifiers(idsOfSystems(next), stored); len(removed) > 0 {
		query := `DELETE FROM "systems" WHERE "systemId" IN (` + placeholders(len(removed)) + `)`

		if _, err := tx.exec(ctx, query, valuesOf(removed)...); err != nil {
			return statementError("systems", "write", err, query)
		}
	}

	for _, system := range next {
		var (
			id    = system.ID
			count uint64
		)

		if id > 0 {
			const query = `SELECT COUNT(*) FROM "systems" WHERE "systemId" = ?`

			if err := tx.queryRow(ctx, query, id).Scan(&count); err != nil {
				return statementError("systems", "write", err, query)
			}
		}

		if count == 0 {
			const query = `INSERT INTO "systems" ("alert", "autoPopulate", "blacklists", "delay", "label", "led", "order", "systemRef", "type") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

			id, err = insertAssignedID(ctx, tx, s.dialect, query, `RETURNING "systemId"`,
				system.Alert, system.AutoPopulate, string(system.Blacklists), system.Delay, system.Label, system.LED, system.Order, system.SystemRef, system.Kind)
			if err != nil {
				return statementError("systems", "write", err, query)
			}

		} else {
			const query = `UPDATE "systems" SET "alert" = ?, "autoPopulate" = ?, "blacklists" = ?, "delay" = ?, "label" = ?, "led" = ?, "order" = ?, "systemRef" = ?, "type" = ? WHERE "systemId" = ?`

			if _, err := tx.exec(ctx, query,
				system.Alert, system.AutoPopulate, string(system.Blacklists), system.Delay, system.Label, system.LED, system.Order, system.SystemRef, system.Kind, id); err != nil {
				return statementError("systems", "write", err, query)
			}
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

// writeSites is the legacy `Sites.WriteTx`: the same reconcile, then insert or update, with the
// system the save is writing for — not the one the submitted site happens to carry — stored as the
// site's own.
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

// writeTalkgroups is the legacy `Talkgroups.WriteTx`: reconcile the system's talkgroups, then
// insert or update each one, then reconcile the `talkgroupGroups` rows that carry its groups.
//
// The update deliberately does not write `systemId`, exactly as the legacy update does: a submitted
// talkgroup whose identifier is already stored under another system is updated where it already
// lives instead of moved.
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

// writeTalkgroupGroups reconciles one talkgroup's group memberships: the stored memberships the
// submitted list does not name are removed, and a pair is inserted only when it is not already
// stored, so a membership that did not change is left alone.
//
// The legacy `Talkgroups.WriteTx` reads the join rows as `"groupId", "talkgroupGroupId"` and then
// tests the submitted group identifiers against `talkgroupGroupId` — the join row's own number
// rather than the group it names — so removing a membership removed whichever join row happened to
// be numbered like a group the save kept. This server compares against `groupId`, which is the column
// the comparison means; see Q34 in decision-preserved-quirks.md.
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

// droppedIdentifiers returns the stored identifiers the submitted collection does not name.
//
// This is the reconcile the legacy server intended and only the talkgroups actually got: the
// legacy `Systems.Write`, `Groups.Write`, `Tags.Write`, `Sites.WriteTx`, `Units.WriteTx`,
// `Accesses.Write`, `Apikeys.Write`, `Dirwatches.Write`, and `Downstreams.Write` each stop their
// scan at the first submitted entry that has no identity yet, so a save that adds something new
// silently removes nothing. This server repairs that rule for every section, which is the one place
// where it deliberately behaves unlike the legacy server; see Q33 in
// decision-preserved-quirks.md, and note that a system or talkgroup deleted by a corrected save
// takes its recorded calls with it through `ON DELETE CASCADE`.
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

// insertAssignedID runs an insert and reports the identifier the table assigned, which is what the
// rows written below a system or a talkgroup are keyed by. PostgreSQL is the one backend whose
// driver cannot report it, so the statement asks for it back, exactly as the legacy server does.
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
