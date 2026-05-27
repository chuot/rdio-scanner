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

package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CallFrequency struct {
	Id        uint64
	CallId    uint64
	Dbm       int
	Errors    uint
	Frequency uint
	Offset    float32
	Spikes    uint
}

type CallMeta struct {
	SiteId          uint64
	SiteLabel       string
	SiteRef         uint
	SystemId        uint64
	SystemLabel     string
	SystemRef       uint
	TalkgroupGroups []string
	TalkgroupId     uint64
	TalkgroupLabel  string
	TalkgroupName   string
	TalkgroupRef    uint
	TalkgroupTag    string
	UnitLabels      []string
	UnitRefs        []uint
}

type CallUnit struct {
	Id      uint64
	CallId  uint64
	Offset  float32
	UnitRef uint
}

type Call struct {
	Id            uint64
	Audio         []byte
	AudioFilename string
	AudioMime     string
	AudioPath     string
	Delayed       bool
	Frequencies   []CallFrequency
	Meta          CallMeta
	Patches       []uint
	SiteRef       uint
	System        *System
	Talkgroup     *Talkgroup
	Timestamp     time.Time
	Units         []CallUnit
}

func NewCall() *Call {
	return &Call{
		Frequencies: []CallFrequency{},
		Meta: CallMeta{
			TalkgroupGroups: []string{},
			UnitLabels:      []string{},
			UnitRefs:        []uint{},
		},
		Patches: []uint{},
		Units:   []CallUnit{},
	}
}

func (call *Call) IsValid() (ok bool, err error) {
	ok = true

	if len(call.Audio) <= 44 {
		ok = false
		err = errors.New("no audio")

	} else if call.Timestamp.UnixMilli() == 0 {
		ok = false
		err = errors.New("no timestamp")

	} else if !(call.System != nil || call.Meta.SystemId > 0 || len(call.Meta.SystemLabel) > 0 || call.Meta.SystemRef > 0) {
		ok = false
		err = errors.New("no system")

	} else if !(call.Talkgroup != nil || call.Meta.TalkgroupId > 0 || len(call.Meta.TalkgroupLabel) > 0 || call.Meta.TalkgroupRef > 0) {
		ok = false
		err = errors.New("no talkgroup")
	}

	return ok, err
}

func (call *Call) MarshalJSON() ([]byte, error) {
	audio := strings.ReplaceAll(fmt.Sprintf("%v", call.Audio), " ", ",")

	callMap := map[string]any{
		"id": call.Id,
		"audio": map[string]any{
			"data": json.RawMessage(audio),
			"type": "Buffer",
		},
		"audioName": call.AudioFilename,
		"audioType": call.AudioMime,
		"dateTime":  call.Timestamp.Format(time.RFC3339),
		"delayed":   call.Delayed,
		"patches":   call.Patches,
	}

	if len(call.Frequencies) > 0 {
		freqs := []map[string]any{}
		for _, f := range call.Frequencies {
			freq := map[string]any{
				"errorCount": f.Errors,
				"freq":       f.Frequency,
				"pos":        f.Offset,
				"spikeCount": f.Spikes,
			}

			if f.Dbm > 0 {
				freq["dbm"] = f.Dbm
			}

			freqs = append(freqs, freq)
		}

		callMap["frequencies"] = freqs
	}

	if call.SiteRef > 0 {
		callMap["site"] = call.SiteRef
	}

	if call.System != nil {
		callMap["system"] = call.System.SystemRef
	}

	if call.Talkgroup != nil {
		callMap["talkgroup"] = call.Talkgroup.TalkgroupRef
	}

	if len(call.Units) > 0 {
		sources := []map[string]any{}
		for _, unit := range call.Units {
			sources = append(sources, map[string]any{
				"pos": unit.Offset,
				"src": unit.UnitRef,
			})
		}
		callMap["sources"] = sources
	}

	return json.Marshal(callMap)
}

func (call *Call) ToJson() (string, error) {
	if b, err := json.Marshal(call); err == nil {
		return string(b), nil
	} else {
		return "", fmt.Errorf("call.tojson: %v", err)
	}
}

type Calls struct {
	controller *Controller
	mutex      sync.Mutex
}

func NewCalls(controller *Controller) *Calls {
	return &Calls{
		controller: controller,
		mutex:      sync.Mutex{},
	}
}

func (calls *Calls) CheckDuplicate(call *Call, msTimeFrame uint, db *Database) (bool, error) {
	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	formatError := errorFormatter("calls", "checkduplicate")

	d := time.Duration(msTimeFrame) * time.Millisecond
	from := call.Timestamp.Add(-d)
	to := call.Timestamp.Add(d)

	// SELECT 1 ... LIMIT 1 short-circuits at the first matching row,
	// rather than counting every match across the whole window.
	query := fmt.Sprintf(`SELECT 1 FROM "calls" WHERE ("timestamp" BETWEEN %d and %d) AND "systemId" = %d AND "talkgroupId" = %d LIMIT 1`, from.UnixMilli(), to.UnixMilli(), call.System.Id, call.Talkgroup.Id)
	var found int
	switch err := db.Sql.QueryRow(query).Scan(&found); err {
	case nil:
		return true, nil
	case sql.ErrNoRows:
		return false, nil
	default:
		return false, formatError(err, query)
	}
}

func (calls *Calls) GetCall(id uint64) (*Call, error) {
	var (
		err   error
		query string
		rows  *sql.Rows
		tx    *sql.Tx

		patch       string
		systemId    uint64
		talkgroupId uint64
		timestamp   int64
	)

	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	formatError := errorFormatter("calls", "getcall")

	if tx, err = calls.controller.Database.Sql.Begin(); err != nil {
		return nil, formatError(err, "")
	}

	call := Call{Id: id}

	// c.systemId and c.talkgroupId are taken directly from the calls row;
	// the prior LEFT JOINs to systems/talkgroups existed only to re-select
	// those columns and are pure overhead (FK constraints guarantee the
	// references resolve, and Go re-looks up against in-memory caches anyway).
	if calls.controller.Database.Config.DbType == DbTypePostgresql {
		query = fmt.Sprintf(`SELECT c."audio", c."audioFilename", c."audioMime", c."audioPath", c."siteRef", c."timestamp", STRING_AGG(CAST(COALESCE(cpt."talkgroupRef", 0) AS text), ','), c."systemId", c."talkgroupId" FROM "calls" AS c LEFT JOIN "callPatches" AS cp on cp."callId" = c."callId" LEFT JOIN "talkgroups" AS cpt ON cpt."talkgroupId" = cp."talkgroupId" WHERE c."callId" = %d GROUP BY c."callId"`, id)

	} else {
		query = fmt.Sprintf(`SELECT c."audio", c."audioFilename", c."audioMime", c."audioPath", c."siteRef", c."timestamp", GROUP_CONCAT(COALESCE(cpt."talkgroupRef", 0)), c."systemId", c."talkgroupId" FROM "calls" AS c LEFT JOIN "callPatches" AS cp on cp."callId" = c."callId" LEFT JOIN "talkgroups" AS cpt ON cpt."talkgroupId" = cp."talkgroupId" WHERE c."callId" = %d GROUP BY c."callId"`, id)
	}

	if err = tx.QueryRow(query).Scan(&call.Audio, &call.AudioFilename, &call.AudioMime, &call.AudioPath, &call.SiteRef, &timestamp, &patch, &systemId, &talkgroupId); err != nil && err != sql.ErrNoRows {
		tx.Rollback()
		return nil, formatError(err, query)
	}

	// File-backed rows have audio on disk; legacy rows still have it in
	// the BLOB column. Load from disk when we have a path.
	if call.AudioPath != "" {
		if b, readErr := readAudioFile(call.AudioPath); readErr == nil {
			call.Audio = b
		} else {
			tx.Rollback()
			return nil, formatError(readErr, call.AudioPath)
		}
	}

	call.Timestamp = time.UnixMilli(timestamp)

	if len(patch) > 0 {
		for _, s := range strings.Split(patch, ",") {
			if i, err := strconv.Atoi(s); err == nil && i > 0 {
				call.Patches = append(call.Patches, uint(i))
			}
		}
	}

	if system, ok := calls.controller.Systems.GetSystemById(systemId); ok {
		call.System = system

	} else {
		return nil, formatError(fmt.Errorf("cannot retrieve system id %d for call id %d", systemId, call.Id), "")
	}

	if talkgroup, ok := call.System.Talkgroups.GetTalkgroupById(talkgroupId); ok {
		call.Talkgroup = talkgroup

	} else {
		return nil, formatError(fmt.Errorf("cannot retrieve talkgroup id %d for call id %d", talkgroupId, call.Id), "")
	}

	query = fmt.Sprintf(`SELECT "dbm", "errors", "frequency", "offset", "spikes" FROM "callFrequencies" WHERE "callId" = %d`, id)
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return nil, formatError(err, query)
	}

	for rows.Next() {
		f := CallFrequency{}

		if err = rows.Scan(&f.Dbm, &f.Errors, &f.Frequency, &f.Offset, &f.Spikes); err != nil {
			break
		}

		call.Frequencies = append(call.Frequencies, f)
	}

	rows.Close()

	if err != nil {
		tx.Rollback()
		return nil, formatError(err, query)
	}

	query = fmt.Sprintf(`SELECT "offset", "unitRef" FROM "callUnits" WHERE "callId" = %d`, id)
	if rows, err = tx.Query(query); err != nil {
		tx.Rollback()
		return nil, formatError(err, query)
	}

	for rows.Next() {
		u := CallUnit{}

		if err = rows.Scan(&u.Offset, &u.UnitRef); err != nil {
			break
		}

		call.Units = append(call.Units, u)
	}

	rows.Close()

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return nil, formatError(err, "")
	}

	return &call, nil
}

// BackfillBlobs migrates legacy rows (audio bytes in the DB blob, no audioPath)
// to file-backed storage. Safe to run alongside ingestion: legacy rows are
// effectively immutable except for Prune, which races harmlessly with the
// per-row UPDATE here (an UPDATE that affects 0 rows is a no-op). Resumable
// on restart because the SELECT keeps finding un-migrated rows.
func (calls *Calls) BackfillBlobs() {
	const (
		batchSize     = 100
		batchInterval = 50 * time.Millisecond
	)

	formatError := errorFormatter("calls", "backfillblobs")

	db := calls.controller.Database
	config := calls.controller.Config

	type legacyRow struct {
		id            uint64
		audio         []byte
		audioFilename string
		audioMime     string
		systemId      uint64
		talkgroupId   uint64
		timestamp     int64
	}

	migrated := uint64(0)
	failed := uint64(0)
	scanned := false

	for {
		batch := []legacyRow{}

		query := fmt.Sprintf(`SELECT "callId", "audio", "audioFilename", "audioMime", "systemId", "talkgroupId", "timestamp" FROM "calls" WHERE "audioPath" = '' LIMIT %d`, batchSize)
		rows, err := db.Sql.Query(query)
		if err != nil {
			log.Println(formatError(err, query))
			return
		}
		for rows.Next() {
			var r legacyRow
			if err := rows.Scan(&r.id, &r.audio, &r.audioFilename, &r.audioMime, &r.systemId, &r.talkgroupId, &r.timestamp); err == nil {
				batch = append(batch, r)
			}
		}
		rows.Close()

		if len(batch) == 0 {
			if scanned {
				log.Printf("audio backfill: complete (%d migrated, %d skipped)", migrated, failed)
			}
			return
		}
		if !scanned {
			log.Println("audio backfill: starting")
			scanned = true
		}

		for _, r := range batch {
			// Empty BLOB = legacy row with no audio at all; mark it as
			// migrated by setting audioPath to a sentinel that won't match
			// real files, so the SELECT stops finding it.
			if len(r.audio) == 0 {
				update := fmt.Sprintf(`UPDATE "calls" SET "audioPath" = '-' WHERE "callId" = %d`, r.id)
				if _, err := db.Sql.Exec(update); err != nil {
					log.Println(formatError(err, update))
					failed++
				}
				continue
			}

			sys, ok := calls.controller.Systems.GetSystemById(r.systemId)
			if !ok {
				log.Println(formatError(fmt.Errorf("system %d not found for call %d", r.systemId, r.id), ""))
				failed++
				continue
			}
			tg, ok := sys.Talkgroups.GetTalkgroupById(r.talkgroupId)
			if !ok {
				log.Println(formatError(fmt.Errorf("talkgroup %d not found for call %d", r.talkgroupId, r.id), ""))
				failed++
				continue
			}

			stub := &Call{
				Id:            r.id,
				AudioFilename: r.audioFilename,
				AudioMime:     r.audioMime,
				System:        sys,
				Talkgroup:     tg,
				Timestamp:     time.UnixMilli(r.timestamp),
			}

			path, err := buildAudioFilePath(config, stub)
			if err != nil {
				log.Println(formatError(err, ""))
				failed++
				continue
			}
			if err := writeAudioFile(path, r.audio); err != nil {
				log.Println(formatError(err, ""))
				failed++
				continue
			}

			// Clear the BLOB and store the path. If the UPDATE fails we
			// leave the file on disk to be picked up on the next attempt
			// (the SELECT will return the row again).
			var update string
			if db.Config.DbType == DbTypePostgresql {
				update = fmt.Sprintf(`UPDATE "calls" SET "audioPath" = $1, "audio" = $2 WHERE "callId" = %d`, r.id)
			} else {
				update = fmt.Sprintf(`UPDATE "calls" SET "audioPath" = ?, "audio" = ? WHERE "callId" = %d`, r.id)
			}
			if _, err := db.Sql.Exec(update, path, []byte{}); err != nil {
				log.Println(formatError(err, update))
				if rmErr := deleteAudioFile(path); rmErr != nil {
					log.Println(formatError(rmErr, "cleanup"))
				}
				failed++
				continue
			}

			migrated++
		}

		if migrated > 0 && migrated%1000 < uint64(batchSize) {
			log.Printf("audio backfill: %d rows migrated so far", migrated)
		}

		time.Sleep(batchInterval)
	}
}

func (calls *Calls) Prune(db *Database, pruneDays uint) error {
	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	timestamp := time.Now().Add(-24 * time.Hour * time.Duration(pruneDays)).UnixMilli()

	// Collect the audio file paths for soon-to-be-deleted rows so we can
	// remove them from disk after the DB delete succeeds. Best-effort:
	// an orphan file is preferable to a row pointing at a missing file.
	var audioPaths []string
	selectQuery := fmt.Sprintf(`SELECT "audioPath" FROM "calls" WHERE "timestamp" < %d AND "audioPath" <> ''`, timestamp)
	if rows, err := db.Sql.Query(selectQuery); err == nil {
		for rows.Next() {
			var p sql.NullString
			if err := rows.Scan(&p); err == nil && p.Valid && p.String != "" {
				audioPaths = append(audioPaths, p.String)
			}
		}
		rows.Close()
	} else {
		log.Println(fmt.Errorf("calls.prune: %s in %s", err, selectQuery))
	}

	deleteQuery := fmt.Sprintf(`DELETE FROM "calls" WHERE "timestamp" < %d`, timestamp)
	if _, err := db.Sql.Exec(deleteQuery); err != nil {
		return fmt.Errorf("%s in %s", err, deleteQuery)
	}

	for _, p := range audioPaths {
		if err := deleteAudioFile(p); err != nil {
			log.Println(fmt.Errorf("calls.prune: %s", err))
		}
	}

	return nil
}

func (calls *Calls) Search(searchOptions *CallsSearchOptions, client *Client) (*CallsSearchResults, error) {
	const (
		ascOrder  = "ASC"
		descOrder = "DESC"
	)

	var (
		err  error
		rows *sql.Rows

		limit  uint
		offset uint
		order  string
		query  string
		where  string = `c."systemId" > 0 AND c."talkgroupId" > 0 AND s."systemRef" IS NOT NULL AND t."talkgroupRef" IS NOT NULL AND d."callId" IS NULL`

		timestamp int64
	)

	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	db := client.Controller.Database

	formatError := errorFormatter("calls", "search")

	searchResults := &CallsSearchResults{
		Options: searchOptions,
		Results: []CallsSearchResult{},
	}

	if client.Access != nil {
		switch v := client.Access.Systems.(type) {
		case []any:
			a := []string{}
			for _, scope := range v {
				var c string
				switch v := scope.(type) {
				case map[string]any:
					switch v["talkgroups"].(type) {
					case []any:
						b := strings.ReplaceAll(fmt.Sprintf("%v", v["talkgroups"]), " ", ", ")
						b = strings.ReplaceAll(b, "[", "(")
						b = strings.ReplaceAll(b, "]", ")")
						c = fmt.Sprintf(`(s."systemRef" = %d AND t."talkgroupRef" IN %v)`, v["id"], b)
					case string:
						if v["talkgroups"] == "*" {
							c = fmt.Sprintf(`s."systemRef" = %d`, v["id"])
						}
					}
				}
				if len(c) > 0 {
					a = append(a, c)
				}
			}
			where = fmt.Sprintf("(%s)", strings.Join(a, " OR "))
		}
	}

	switch v := searchOptions.System.(type) {
	case uint:
		a := []string{
			fmt.Sprintf(`s."systemRef" = %d`, v),
		}
		switch v := searchOptions.Talkgroup.(type) {
		case uint:
			a = append(a, fmt.Sprintf(`t."talkgroupRef" = %d`, v))
		}
		where += fmt.Sprintf(" AND (%s)", strings.Join(a, " AND "))
	}

	switch v := searchOptions.Group.(type) {
	case string:
		a := []string{}
		for id, m := range client.GroupsMap[v] {
			in := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(fmt.Sprintf("%v", m), " ", ", "), "[", "("), "]", ")")
			a = append(a, fmt.Sprintf(`(s."systemRef" = %d AND t."talkgroupRef" IN %s)`, id, in))
		}
		if len(a) > 0 {
			where += fmt.Sprintf(" AND (%s)", strings.Join(a, " OR "))
		}
	}

	switch v := searchOptions.Tag.(type) {
	case string:
		a := []string{}
		for id, m := range client.TagsMap[v] {
			in := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(fmt.Sprintf("%v", m), " ", ", "), "[", "("), "]", ")")
			a = append(a, fmt.Sprintf(`(s."systemRef" = %d AND t."talkgroupRef" IN %s)`, id, in))
		}
		if len(a) > 0 {
			where += fmt.Sprintf(" AND (%s)", strings.Join(a, " OR "))
		}
	}

	// Aggregate MIN/MAX in one round-trip; with idx_calls_timestamp these
	// resolve as index lookups rather than full sorts of the filtered set.
	var minTs, maxTs sql.NullInt64
	query = fmt.Sprintf(`SELECT MIN(c."timestamp"), MAX(c."timestamp") FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" LEFT JOIN "delayed" AS d ON d."callId" = c."callId" WHERE %s`, where)
	if err = db.Sql.QueryRow(query).Scan(&minTs, &maxTs); err != nil && err != sql.ErrNoRows {
		return nil, formatError(err, query)
	}

	if minTs.Valid {
		searchResults.DateStart = time.UnixMilli(minTs.Int64)
	}
	if maxTs.Valid {
		searchResults.DateStop = time.UnixMilli(maxTs.Int64)
	}

	switch v := searchOptions.Sort.(type) {
	case int:
		if v < 0 {
			order = descOrder
		} else {
			order = ascOrder
		}
	default:
		order = ascOrder
	}

	switch v := searchOptions.Date.(type) {
	case time.Time:
		var (
			start time.Time
			stop  time.Time
		)

		if order == ascOrder {
			start = time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), 0, 0, time.UTC)
			stop = start.Add(time.Hour*24 - time.Millisecond)

		} else {
			start = time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), 0, 0, time.UTC).Add(time.Hour*-24 + time.Millisecond)
			stop = time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), 0, 0, time.UTC)
		}

		where += fmt.Sprintf(` AND (c."timestamp" BETWEEN %d AND %d)`, start.UnixMilli(), stop.UnixMilli())
	}

	switch v := searchOptions.Limit.(type) {
	case uint:
		limit = uint(math.Min(float64(500), float64(v)))
	default:
		limit = 200
	}

	switch v := searchOptions.Offset.(type) {
	case uint:
		offset = v
	}

	query = fmt.Sprintf(`SELECT COUNT(*) FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" LEFT JOIN "delayed" AS d ON d."callId" = c."callId" WHERE %s`, where)
	if err = db.Sql.QueryRow(query).Scan(&searchResults.Count); err != nil && err != sql.ErrNoRows {
		return nil, formatError(err, query)
	}

	query = fmt.Sprintf(`SELECT c."callId", c."timestamp", s."systemRef", t."talkgroupRef" FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" LEFT JOIN "delayed" AS d ON d."callId" = c."callId" WHERE %s ORDER BY c."timestamp" %s LIMIT %d OFFSET %d`, where, order, limit, offset)
	if rows, err = db.Sql.Query(query); err != nil && err != sql.ErrNoRows {
		return nil, formatError(err, query)
	}

	for rows.Next() {
		searchResult := CallsSearchResult{}
		if err = rows.Scan(&searchResult.Id, &timestamp, &searchResult.System, &searchResult.Talkgroup); err != nil {
			break
		}

		searchResult.Timestamp = time.UnixMilli(timestamp)

		searchResults.Results = append(searchResults.Results, searchResult)
	}

	rows.Close()

	if err != nil {
		return nil, formatError(err, "")
	}

	return searchResults, err
}

func (calls *Calls) WriteCall(call *Call, db *Database) (uint64, error) {
	var (
		err   error
		query string
		res   sql.Result
		tx    *sql.Tx
	)

	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	formatError := errorFormatter("calls", "writecall")

	// Write audio to disk before opening the transaction. The DB only
	// stores the path; the BLOB column gets an empty value for new rows.
	audioPath, err := buildAudioFilePath(calls.controller.Config, call)
	if err != nil {
		return 0, formatError(err, "")
	}
	if err = writeAudioFile(audioPath, call.Audio); err != nil {
		return 0, formatError(err, "")
	}
	call.AudioPath = audioPath

	// If we don't reach a successful commit, remove the orphan file.
	committed := false
	defer func() {
		if !committed {
			if rmErr := deleteAudioFile(audioPath); rmErr != nil {
				log.Println(formatError(rmErr, "cleanup"))
			}
		}
	}()

	if tx, err = db.Sql.Begin(); err != nil {
		return 0, formatError(err, "")
	}

	emptyAudio := []byte{}

	// Parameterize all string fields, including the pre-existing audio blob.
	// AudioFilename/AudioMime arrive from untrusted multipart uploads
	// (parsers.go), and previously landed in the query via '%s' with no
	// escaping at all — a pre-auth SQLi vector.
	if db.Config.DbType == DbTypePostgresql {
		query = fmt.Sprintf(`INSERT INTO "calls" ("audio", "audioFilename", "audioMime", "audioPath", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES ($1, $2, $3, $4, %d, %d, %d, %d) RETURNING "callId"`, call.SiteRef, call.System.Id, call.Talkgroup.Id, call.Timestamp.UnixMilli())

		err = tx.QueryRow(query, emptyAudio, call.AudioFilename, call.AudioMime, call.AudioPath).Scan(&call.Id)

	} else {
		query = fmt.Sprintf(`INSERT INTO "calls" ("audio", "audioFilename", "audioMime", "audioPath", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES (?, ?, ?, ?, %d, %d, %d, %d)`, call.SiteRef, call.System.Id, call.Talkgroup.Id, call.Timestamp.UnixMilli())

		if res, err = tx.Exec(query, emptyAudio, call.AudioFilename, call.AudioMime, call.AudioPath); err == nil {
			if id, err := res.LastInsertId(); err == nil {
				call.Id = uint64(id)
			}
		}
	}

	if err != nil {
		tx.Rollback()
		return 0, formatError(err, query)
	}

	for _, freq := range call.Frequencies {
		query = fmt.Sprintf(`INSERT INTO "callFrequencies" ("callId", "dbm", "errors", "frequency", "offset", "spikes") VALUES (%d, %d, %d, %d, %f, %d)`, call.Id, freq.Dbm, freq.Errors, freq.Frequency, freq.Offset, freq.Spikes)
		if _, err = tx.Exec(query); err != nil {
			tx.Rollback()
			return 0, formatError(err, query)
		}
	}

	for _, ref := range call.Patches {
		var talkgroupId sql.NullInt64
		query = fmt.Sprintf(`SELECT "talkgroupId" FROM "talkgroups" WHERE "systemId" = %d and "talkgroupRef" = %d`, call.System.Id, ref)
		if err = tx.QueryRow(query).Scan(&talkgroupId); err != nil && err != sql.ErrNoRows {
			tx.Rollback()
			return 0, formatError(err, query)
		}
		if !talkgroupId.Valid {
			continue
		}
		query = fmt.Sprintf(`INSERT INTO "callPatches" ("callId", "talkgroupId") VALUES (%d, %d)`, call.Id, talkgroupId.Int64)
		if _, err = tx.Exec(query); err != nil {
			tx.Rollback()
			return 0, formatError(err, query)
		}
	}

	for _, unit := range call.Units {
		query = fmt.Sprintf(`INSERT INTO "callUnits" ("callId", "offset", "unitRef") VALUES (%d, %f, %d)`, call.Id, unit.Offset, unit.UnitRef)
		if _, err = tx.Exec(query); err != nil {
			tx.Rollback()
			return 0, formatError(err, query)
		}
	}

	if err = tx.Commit(); err != nil {
		tx.Rollback()
		return 0, formatError(err, "")
	}

	committed = true

	return uint64(call.Id), nil
}

type CallsSearchOptions struct {
	Date      any `json:"date,omitempty"`
	Group     any `json:"group,omitempty"`
	Limit     any `json:"limit,omitempty"`
	Offset    any `json:"offset,omitempty"`
	Sort      any `json:"sort,omitempty"`
	System    any `json:"system,omitempty"`
	Tag       any `json:"tag,omitempty"`
	Talkgroup any `json:"talkgroup,omitempty"`
}

func NewCallSearchOptions() *CallsSearchOptions {
	return &CallsSearchOptions{}
}

func (searchOptions *CallsSearchOptions) fromMap(m map[string]any) *CallsSearchOptions {
	switch v := m["date"].(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			searchOptions.Date = t
		}
	}

	switch v := m["group"].(type) {
	case string:
		searchOptions.Group = v
	}

	switch v := m["limit"].(type) {
	case float64:
		searchOptions.Limit = uint(v)
	}

	switch v := m["offset"].(type) {
	case float64:
		searchOptions.Offset = uint(v)
	}

	switch v := m["sort"].(type) {
	case float64:
		searchOptions.Sort = int(v)
	}

	switch v := m["system"].(type) {
	case float64:
		searchOptions.System = uint(v)
	}

	switch v := m["tag"].(type) {
	case string:
		searchOptions.Tag = v
	}

	switch v := m["talkgroup"].(type) {
	case float64:
		searchOptions.Talkgroup = uint(v)
	}

	return searchOptions
}

type CallsSearchResult struct {
	Id        uint64    `json:"id"`
	System    uint      `json:"system"`
	Talkgroup uint      `json:"talkgroup"`
	Timestamp time.Time `json:"dateTime"`
}

type CallsSearchResults struct {
	Count     uint                `json:"count"`
	DateStart time.Time           `json:"dateStart"`
	DateStop  time.Time           `json:"dateStop"`
	Options   *CallsSearchOptions `json:"options"`
	Results   []CallsSearchResult `json:"results"`
}
