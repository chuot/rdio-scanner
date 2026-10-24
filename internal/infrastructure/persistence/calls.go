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
	"errors"
	"strings"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Calls is the recorded call history: the calls table and its callFrequencies,
// callPatches, and callUnits children. Every statement keeps its values as
// placeholders, per the call-history statements maintainer note.
type Calls struct {
	store
}

var _ ports.CallRepository = (*Calls)(nil)

// NewCalls builds the call history repository on an open database and the dialect it was opened
// with.
func NewCalls(db *sql.DB, dialect Dialect) *Calls {
	return &Calls{store: newStore(db, dialect)}
}

// GetCall returns one recorded call with its audio, its frequencies, its source
// units, and the references of the talkgroups it was patched to, or
// ports.ErrNotFound when nothing was recorded under that identifier. The four
// reads run in one read-only transaction, per the call fetch transaction
// posture maintainer note.
func (calls *Calls) GetCall(ctx context.Context, id uint64) (domain.Call, error) {
	tx, err := calls.beginRead(ctx)
	if err != nil {
		return domain.Call{}, operationError("calls", "getcall", err)
	}

	call, err := readCall(ctx, tx, id)
	if err != nil {
		// This rollback is a preserved quirk fix: without it the open
		// transaction holds the SQLite read lock until the garbage collector
		// runs. Pinned by TestGetCallLeavesTheDatabaseFreeForTheNextWrite,
		// which fails at the connection string's busy_timeout if removed.
		tx.rollback()

		return domain.Call{}, err
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return domain.Call{}, operationError("calls", "getcall", err)
	}

	return call, nil
}

// readCall reads one call row and the three child tables that belong to it.
func readCall(ctx context.Context, tx transaction, id uint64) (domain.Call, error) {
	// Only the references, labels, and delays the call payload and the re-arm
	// rule need are joined in; the rest of the two aggregates belongs to the
	// configuration repositories.
	const query = `SELECT c."audio", c."audioFilename", c."audioMime", c."audioLength", c."siteRef", c."timestamp", c."systemId", c."talkgroupId", s."systemRef", t."talkgroupRef", t."label", t."name", s."delay", t."delay" FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" WHERE c."callId" = ?`

	var (
		audio          []byte
		filename       string
		mime           string
		audioLength    int64
		siteRef        int64
		timestamp      int64
		systemID       int64
		talkgroupID    int64
		systemRef      int64
		talkgroupRef   int64
		talkgroupLabel string
		talkgroupName  string
		systemDelay    int64
		talkgroupDelay int64
	)

	err := tx.queryRow(ctx, query, id).Scan(&audio, &filename, &mime, &audioLength, &siteRef, &timestamp, &systemID, &talkgroupID, &systemRef, &talkgroupRef, &talkgroupLabel, &talkgroupName, &systemDelay, &talkgroupDelay)

	if errors.Is(err, sql.ErrNoRows) {
		// The legacy server does not stop here: it falls through its own no-rows result with
		// every value still zero and then reports the missing call as
		// `cannot retrieve system id 0 for call id N`. The port asks for ErrNotFound, which is
		// what the CAL fetch answers with.
		return domain.Call{}, ports.ErrNotFound
	}

	if err != nil {
		return domain.Call{}, statementError("calls", "getcall", err, query)
	}

	call := domain.NewCall()

	call.ID = id
	call.Audio = audio
	call.AudioFilename = filename
	call.AudioMime = mime
	call.AudioLength = durationOf(audioLength)
	call.SiteRef = uint(siteRef)
	call.Timestamp = instantOf(timestamp)
	call.System = domain.System{ID: uint64(systemID), Delay: uint(systemDelay), SystemRef: uint(systemRef)}
	call.Talkgroup = domain.Talkgroup{ID: uint64(talkgroupID), Delay: uint(talkgroupDelay), TalkgroupRef: uint(talkgroupRef), Label: talkgroupLabel, Name: talkgroupName}

	if call.PatchTalkgroupRefs, err = readCallPatches(ctx, tx, id); err != nil {
		return domain.Call{}, err
	}

	if call.Frequencies, err = readCallFrequencies(ctx, tx, id); err != nil {
		return domain.Call{}, err
	}

	if call.Units, err = readCallUnits(ctx, tx, id); err != nil {
		return domain.Call{}, err
	}

	return call, nil
}

// readCallPatches reads the references of the talkgroups a call was patched
// to, per the patched talkgroup reads maintainer note.
func readCallPatches(ctx context.Context, tx transaction, id uint64) ([]uint, error) {
	// COALESCE and the > 0 skip together make a patch whose talkgroup is gone
	// disappear, as in the legacy server.
	const query = `SELECT COALESCE(t."talkgroupRef", 0) FROM "callPatches" AS cp LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = cp."talkgroupId" WHERE cp."callId" = ? ORDER BY cp."callPatchId"`

	rows, err := tx.query(ctx, query, id)
	if err != nil {
		return nil, statementError("calls", "getcall", err, query)
	}

	defer rows.Close()

	refs := []uint{}

	for rows.Next() {
		var ref int64

		if err := rows.Scan(&ref); err != nil {
			return nil, statementError("calls", "getcall", err, query)
		}

		if ref > 0 {
			refs = append(refs, uint(ref))
		}
	}

	if err := rows.Err(); err != nil {
		return nil, statementError("calls", "getcall", err, query)
	}

	return refs, nil
}

// readCallFrequencies reads the carriers a call was heard on.
func readCallFrequencies(ctx context.Context, tx transaction, id uint64) ([]domain.CallFrequency, error) {
	const query = `SELECT "callFrequencyId", "dbm", "errors", "frequency", "offset", "spikes" FROM "callFrequencies" WHERE "callId" = ? ORDER BY "callFrequencyId"`

	rows, err := tx.query(ctx, query, id)
	if err != nil {
		return nil, statementError("calls", "getcall", err, query)
	}

	defer rows.Close()

	frequencies := []domain.CallFrequency{}

	for rows.Next() {
		var (
			rowID     int64
			dbm       float64
			errorText int64
			frequency int64
			offset    float64
			spikes    int64
		)

		if err := rows.Scan(&rowID, &dbm, &errorText, &frequency, &offset, &spikes); err != nil {
			return nil, statementError("calls", "getcall", err, query)
		}

		// dbm is a REAL column in all three schemas although it counts whole
		// decibels, so convert here rather than casting in SQL.
		frequencies = append(frequencies, domain.CallFrequency{
			ID:        uint64(rowID),
			CallID:    id,
			Dbm:       int(dbm),
			Errors:    uint(errorText),
			Frequency: uint(frequency),
			Offset:    float32(offset),
			Spikes:    uint(spikes),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, statementError("calls", "getcall", err, query)
	}

	return frequencies, nil
}

// readCallUnits reads the units heard during a call and how far into the recording each spoke.
func readCallUnits(ctx context.Context, tx transaction, id uint64) ([]domain.CallUnit, error) {
	const query = `SELECT "callUnitId", "offset", "unitRef" FROM "callUnits" WHERE "callId" = ? ORDER BY "callUnitId"`

	rows, err := tx.query(ctx, query, id)
	if err != nil {
		return nil, statementError("calls", "getcall", err, query)
	}

	defer rows.Close()

	units := []domain.CallUnit{}

	for rows.Next() {
		var (
			rowID   int64
			offset  float64
			unitRef int64
		)

		if err := rows.Scan(&rowID, &offset, &unitRef); err != nil {
			return nil, statementError("calls", "getcall", err, query)
		}

		units = append(units, domain.CallUnit{
			ID:      uint64(rowID),
			CallID:  id,
			Offset:  float32(offset),
			UnitRef: uint(unitRef),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, statementError("calls", "getcall", err, query)
	}

	return units, nil
}

// SaveCall stores a call together with its frequency, patch, and unit rows in
// one transaction, and returns it with the identity the database assigned —
// via RETURNING on PostgreSQL, LastInsertId elsewhere, as the legacy server
// takes it.
func (calls *Calls) SaveCall(ctx context.Context, call domain.Call) (domain.Call, error) {
	tx, err := calls.begin(ctx)
	if err != nil {
		return domain.Call{}, operationError("calls", "writecall", err)
	}

	const insertCall = `INSERT INTO "calls" ("audio", "audioFilename", "audioLength", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	id := call.ID

	if calls.dialect == DialectPostgresql {
		query := insertCall + ` RETURNING "callId"`

		err := tx.queryRow(ctx, query, call.Audio, call.AudioFilename, nanosecondsOf(call.AudioLength), call.AudioMime, call.SiteRef, call.System.ID, call.Talkgroup.ID, millisOf(call.Timestamp)).Scan(&id)
		if err != nil {
			tx.rollback()
			return domain.Call{}, statementError("calls", "writecall", err, query)
		}

	} else {
		res, err := tx.exec(ctx, insertCall, call.Audio, call.AudioFilename, nanosecondsOf(call.AudioLength), call.AudioMime, call.SiteRef, call.System.ID, call.Talkgroup.ID, millisOf(call.Timestamp))
		if err != nil {
			tx.rollback()
			return domain.Call{}, statementError("calls", "writecall", err, insertCall)
		}

		assigned, err := res.LastInsertId()
		if err != nil {
			tx.rollback()
			return domain.Call{}, operationError("calls", "writecall", err)
		}

		id = uint64(assigned)
	}

	if err := writeCallFrequencies(ctx, tx, id, call.Frequencies); err != nil {
		tx.rollback()
		return domain.Call{}, err
	}

	if err := writeCallPatches(ctx, tx, call, id); err != nil {
		tx.rollback()
		return domain.Call{}, err
	}

	if err := writeCallUnits(ctx, tx, id, call.Units); err != nil {
		tx.rollback()
		return domain.Call{}, err
	}

	if err := tx.commit(); err != nil {
		tx.rollback()
		return domain.Call{}, operationError("calls", "writecall", err)
	}

	call.ID = id

	return call, nil
}

// writeCallFrequencies stores the carriers the call was heard on.
func writeCallFrequencies(ctx context.Context, tx transaction, id uint64, frequencies []domain.CallFrequency) error {
	const query = `INSERT INTO "callFrequencies" ("callId", "dbm", "errors", "frequency", "offset", "spikes") VALUES (?, ?, ?, ?, ?, ?)`

	for _, freq := range frequencies {
		if _, err := tx.exec(ctx, query, id, freq.Dbm, freq.Errors, freq.Frequency, freq.Offset, freq.Spikes); err != nil {
			return statementError("calls", "writecall", err, query)
		}
	}

	return nil
}

// writeCallPatches stores the talkgroups a call was patched to. Patches are
// submitted as talkgroup references, resolved against the call's own system,
// and an unresolved reference is dropped rather than failing the write, per
// the patch reference resolution maintainer note.
func writeCallPatches(ctx context.Context, tx transaction, call domain.Call, id uint64) error {
	const findTalkgroup = `SELECT "talkgroupId" FROM "talkgroups" WHERE "systemId" = ? AND "talkgroupRef" = ?`
	const insertPatch = `INSERT INTO "callPatches" ("callId", "talkgroupId") VALUES (?, ?)`

	for _, ref := range call.PatchTalkgroupRefs {
		var talkgroupID sql.NullInt64

		err := tx.queryRow(ctx, findTalkgroup, call.System.ID, ref).Scan(&talkgroupID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return statementError("calls", "writecall", err, findTalkgroup)
		}

		if !talkgroupID.Valid {
			continue
		}

		if _, err := tx.exec(ctx, insertPatch, id, talkgroupID.Int64); err != nil {
			return statementError("calls", "writecall", err, insertPatch)
		}
	}

	return nil
}

// writeCallUnits stores the units heard during the call.
func writeCallUnits(ctx context.Context, tx transaction, id uint64, units []domain.CallUnit) error {
	const query = `INSERT INTO "callUnits" ("callId", "offset", "unitRef") VALUES (?, ?, ?)`

	for _, unit := range units {
		if _, err := tx.exec(ctx, query, id, unit.Offset, unit.UnitRef); err != nil {
			return statementError("calls", "writecall", err, query)
		}
	}

	return nil
}

// callHistoryFrom is the join every history query is built on. The `delayed` join is what keeps
// a call that is waiting out a delay out of the history: it is recorded, but it is not part of
// the history until its delay has run.
const callHistoryFrom = `FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" LEFT JOIN "delayed" AS d ON d."callId" = c."callId"`

// SearchCalls returns one page of the recorded history, together with how many
// calls the criteria matches in total and the span of their timestamps — over
// the whole match, not the page, per the history search statements maintainer
// note.
func (calls *Calls) SearchCalls(ctx context.Context, search ports.CallSearch) (ports.CallSearchPage, error) {
	where, args := callSearchFilters(search)

	from := callHistoryFrom + ` WHERE ` + where

	page := ports.CallSearchPage{Results: []ports.CallSummary{}}

	// Each history end is the first row of a sort order, not an aggregate over
	// the whole match, so the database stops once it has the instant.
	span := `SELECT c."timestamp" ` + from

	if err := calls.readSearchSpan(ctx, span+` ORDER BY c."timestamp" ASC LIMIT 1`, args, &page.DateStart); err != nil {
		return ports.CallSearchPage{}, err
	}

	if err := calls.readSearchSpan(ctx, span+` ORDER BY c."timestamp" DESC LIMIT 1`, args, &page.DateStop); err != nil {
		return ports.CallSearchPage{}, err
	}

	countQuery := `SELECT COUNT(*) ` + from

	// The count is scanned as a 64-bit integer and clamped, not cast:
	// page.Count is a uint, and on a 32-bit build a negative value cast
	// through it is the largest count there is.
	var count int64

	if err := calls.queryRow(ctx, countQuery, args...).Scan(&count); err != nil {
		return ports.CallSearchPage{}, statementError("calls", "search", err, countQuery)
	}

	page.Count = uint(clampNonNegative(count))

	order := "DESC"
	if search.Ascending {
		order = "ASC"
	}

	// The page carries summary fields only, never the audio a page of 200
	// calls would otherwise cost.
	pageQuery := `SELECT c."callId", c."timestamp", c."audioLength", s."systemRef", t."talkgroupRef" ` + from + ` ORDER BY c."timestamp" ` + order + ` LIMIT ? OFFSET ?`

	rows, err := calls.query(ctx, pageQuery, append(append([]any{}, args...), search.Limit, search.Offset)...)
	if err != nil {
		return ports.CallSearchPage{}, statementError("calls", "search", err, pageQuery)
	}

	defer rows.Close()

	for rows.Next() {
		var (
			id           int64
			timestamp    int64
			audioLength  int64
			systemRef    int64
			talkgroupRef int64
		)

		if err := rows.Scan(&id, &timestamp, &audioLength, &systemRef, &talkgroupRef); err != nil {
			return ports.CallSearchPage{}, statementError("calls", "search", err, pageQuery)
		}

		page.Results = append(page.Results, ports.CallSummary{
			ID:           uint64(id),
			SystemRef:    uint(systemRef),
			TalkgroupRef: uint(talkgroupRef),
			Timestamp:    instantOf(timestamp),
			AudioLength:  durationOf(audioLength),
		})
	}

	if err := rows.Err(); err != nil {
		return ports.CallSearchPage{}, operationError("calls", "search", err)
	}

	return page, nil
}

// readSearchSpan reads one end of the matching history into at; an empty
// match answers the Unix epoch, per the span empty match parity maintainer
// note.
func (calls *Calls) readSearchSpan(ctx context.Context, query string, args []any, at *time.Time) error {
	var timestamp int64

	err := calls.queryRow(ctx, query, args...).Scan(&timestamp)

	if errors.Is(err, sql.ErrNoRows) {
		// Parity with the legacy server's empty search, which reports the Unix
		// epoch at both ends; the captured empty frame carries those bytes.
		*at = instantOf(0)

		return nil
	}

	if err != nil {
		return statementError("calls", "search", err, query)
	}

	*at = instantOf(timestamp)

	return nil
}

// callSearchFilters turns the resolved criteria into the history query's
// filter and the values that go with it, written to stay usable through the
// calls index, per the search filter index shape maintainer note.
func callSearchFilters(search ports.CallSearch) (string, []any) {
	filters := []string{
		`c."systemId" > 0`,
		`c."talkgroupId" > 0`,
		`s."systemRef" IS NOT NULL`,
		`t."talkgroupRef" IS NOT NULL`,
		`d."callId" IS NULL`,
	}

	args := []any{}

	if search.Scope.Present && !search.Scope.Wildcard {
		clauses, scopeArgs := accessScopeFilters(search.Scope)

		filters = append(filters, "("+strings.Join(clauses, " OR ")+")")
		args = append(args, scopeArgs...)
	}

	if search.SystemRef != 0 {
		clauses := []string{`s."systemRef" = ?`}

		args = append(args, search.SystemRef)

		if search.TalkgroupRef != 0 {
			clauses = append(clauses, `t."talkgroupRef" = ?`)
			args = append(args, search.TalkgroupRef)
		}

		filters = append(filters, "("+strings.Join(clauses, " AND ")+")")
	}

	if len(search.GroupTalkgroups) > 0 {
		clauses, groupArgs := callSearchPairFilters(expandedTalkgroups(search.GroupTalkgroups))

		filters = append(filters, "("+strings.Join(clauses, " OR ")+")")
		args = append(args, groupArgs...)
	}

	if len(search.TagTalkgroups) > 0 {
		clauses, tagArgs := callSearchPairFilters(expandedTalkgroups(search.TagTalkgroups))

		filters = append(filters, "("+strings.Join(clauses, " OR ")+")")
		args = append(args, tagArgs...)
	}

	// The legacy server writes these two as one `BETWEEN`; they are kept apart because the port
	// allows either bound on its own.
	if !search.From.IsZero() {
		filters = append(filters, `c."timestamp" >= ?`)
		args = append(args, millisOf(search.From))
	}

	if !search.To.IsZero() {
		filters = append(filters, `c."timestamp" <= ?`)
		args = append(args, millisOf(search.To))
	}

	return strings.Join(filters, " AND "), args
}

// accessScopeFilters is the caller's own limit on what it may read: the systems its access names,
// and within each of them either every talkgroup or the listed ones.
func accessScopeFilters(scope domain.AccessScope) ([]string, []any) {
	if len(scope.Systems) == 0 {
		// A supplied scope that names nothing sees nothing; the legacy server
		// answers the same configuration with a filter it cannot run at all.
		return []string{nothingMatches}, nil
	}

	pairs := make([]callSearchPair, 0, len(scope.Systems))

	for _, system := range scope.Systems {
		pairs = append(pairs, callSearchPair{
			SystemRef:          system.SystemRef,
			TalkgroupsWildcard: system.TalkgroupsWildcard,
			TalkgroupRefs:      system.TalkgroupRefs,
		})
	}

	return callSearchPairFilters(pairs)
}

// callSearchPair is one system and the talkgroups on it a single history filter stands for. The
// caller's access scope and the expanded group and tag labels are both lists of these, and the
// filter they turn into is the same shape.
type callSearchPair struct {
	SystemRef          uint
	TalkgroupsWildcard bool
	TalkgroupRefs      []uint
}

// callSearchPairFilters turns a list of `(system, talkgroups)` pairs into the OR of the filters
// they stand for.
func callSearchPairFilters(pairs []callSearchPair) ([]string, []any) {
	clauses := []string{}
	args := []any{}

	for _, pair := range pairs {
		if pair.TalkgroupsWildcard {
			clauses = append(clauses, `s."systemRef" = ?`)
			args = append(args, pair.SystemRef)

			continue
		}

		if len(pair.TalkgroupRefs) == 0 {
			clauses = append(clauses, nothingMatches)

			continue
		}

		clauses = append(clauses, `(s."systemRef" = ? AND t."talkgroupRef" IN (`+placeholders(len(pair.TalkgroupRefs))+`))`)
		args = append(args, pair.SystemRef)

		for _, ref := range pair.TalkgroupRefs {
			args = append(args, ref)
		}
	}

	return clauses, args
}

// expandedTalkgroups is the pair list the query handler produced when it expanded a group or tag
// label into the concrete talkgroups it stands for. A label never stands for "every talkgroup on
// a system", so the wildcard half is left off.
func expandedTalkgroups(pairs []domain.TalkgroupScope) []callSearchPair {
	out := make([]callSearchPair, 0, len(pairs))

	for _, pair := range pairs {
		out = append(out, callSearchPair{SystemRef: pair.SystemRef, TalkgroupRefs: pair.TalkgroupRefs})
	}

	return out
}

// nothingMatches is a filter that matches no recorded call.
const nothingMatches = `1 = 0`

// FindDuplicateCandidates returns the recorded calls that could make a call a
// duplicate of an earlier one: the calls on the same system, talkgroup and
// site whose timestamps fall between from and to, inclusive, read lean and
// including held calls, per the duplicate candidate window maintainer note.
func (calls *Calls) FindDuplicateCandidates(ctx context.Context, systemID, talkgroupID uint64, siteRef uint, from, to time.Time) ([]domain.Call, error) {
	// No delayed join here: the legacy duplicate check counts every call in the
	// window, held or not.
	const query = `SELECT c."callId", c."timestamp", c."systemId", c."talkgroupId", c."siteRef", s."systemRef", t."talkgroupRef" FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" WHERE c."systemId" = ? AND c."talkgroupId" = ? AND c."siteRef" = ? AND c."timestamp" BETWEEN ? AND ? ORDER BY c."timestamp"`

	rows, err := calls.query(ctx, query, systemID, talkgroupID, siteRef, millisOf(from), millisOf(to))
	if err != nil {
		return nil, statementError("calls", "checkduplicate", err, query)
	}

	defer rows.Close()

	candidates := []domain.Call{}

	for rows.Next() {
		var (
			id            int64
			timestamp     int64
			candidateSys  int64
			candidateTalk int64
			candidateSite int64
			systemRef     int64
			talkgroupRef  int64
		)

		if err := rows.Scan(&id, &timestamp, &candidateSys, &candidateTalk, &candidateSite, &systemRef, &talkgroupRef); err != nil {
			return nil, statementError("calls", "checkduplicate", err, query)
		}

		call := domain.NewCall()

		call.ID = uint64(id)
		call.Timestamp = instantOf(timestamp)
		call.SiteRef = uint(candidateSite)
		call.System = domain.System{ID: uint64(candidateSys), SystemRef: uint(systemRef)}
		call.Talkgroup = domain.Talkgroup{ID: uint64(candidateTalk), TalkgroupRef: uint(talkgroupRef)}

		candidates = append(candidates, call)
	}

	if err := rows.Err(); err != nil {
		return nil, operationError("calls", "checkduplicate", err)
	}

	return candidates, nil
}

// DeleteCallsBefore removes the recorded calls older than the given instant
// and reports how many went. Only the calls table is written: the child rows
// go with their call through cascading foreign keys.
func (calls *Calls) DeleteCallsBefore(ctx context.Context, before time.Time) (int, error) {
	const query = `DELETE FROM "calls" WHERE "timestamp" < ?`

	res, err := calls.exec(ctx, query, millisOf(before))
	if err != nil {
		return 0, statementError("calls", "prune", err, query)
	}

	removed, err := res.RowsAffected()
	if err != nil {
		return 0, operationError("calls", "prune", err)
	}

	return int(removed), nil
}
