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
	var count uint64

	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	formatError := errorFormatter("calls", "checkduplicate")

	d := time.Duration(msTimeFrame) * time.Millisecond
	from := call.Timestamp.Add(-d)
	to := call.Timestamp.Add(d)

	query := fmt.Sprintf(`SELECT COUNT(*) FROM "calls" WHERE ("timestamp" BETWEEN %d and %d) AND "systemId" = %d AND "talkgroupId" = %d`, from.UnixMilli(), to.UnixMilli(), call.System.Id, call.Talkgroup.Id)
	if err := db.Sql.QueryRow(query).Scan(&count); err != nil {
		return false, formatError(err, query)
	}

	return count > 0, nil
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

	if calls.controller.Database.Config.DbType == DbTypePostgresql {
		query = fmt.Sprintf(`SELECT c."audio", c."audioFilename", c."audioMime", c."siteRef", c."timestamp", STRING_AGG(CAST(COALESCE(cpt."talkgroupRef", 0) AS text), ','), c."siteRef", sy."systemId", t."talkgroupId" FROM "calls" AS c LEFT JOIN "callPatches" AS cp on cp."callId" = c."callId" LEFT JOIN "talkgroups" AS cpt ON cpt."talkgroupId" = cp."talkgroupId" LEFT JOIN "systems" AS sy ON sy."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" WHERE c."callId" = %d GROUP BY c."callId"`, id)

	} else {
		query = fmt.Sprintf(`SELECT c."audio", c."audioFilename", c."audioMime", c."siteRef", c."timestamp", GROUP_CONCAT(COALESCE(cpt."talkgroupRef", 0)), c."siteRef", sy."systemId", t."talkgroupId" FROM "calls" AS c LEFT JOIN "callPatches" AS cp on cp."callId" = c."callId" LEFT JOIN "talkgroups" AS cpt ON cpt."talkgroupId" = cp."talkgroupId" LEFT JOIN "systems" AS sy ON sy."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" WHERE c."callId" = %d GROUP BY c."callId"`, id)
	}

	if err = tx.QueryRow(query).Scan(&call.Audio, &call.AudioFilename, &call.AudioMime, &patch, &timestamp, &patch, &call.SiteRef, &systemId, &talkgroupId); err != nil && err != sql.ErrNoRows {
		tx.Rollback()
		return nil, formatError(err, query)
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

func (calls *Calls) Prune(db *Database, pruneDays uint) error {
	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	timestamp := time.Now().Add(-24 * time.Hour * time.Duration(pruneDays)).UnixMilli()
	query := fmt.Sprintf(`DELETE FROM "calls" WHERE "timestamp" < %d`, timestamp)

	if _, err := db.Sql.Exec(query); err != nil {
		return fmt.Errorf("%s in %s", err, query)
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

	query = fmt.Sprintf(`SELECT c."timestamp" FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" LEFT JOIN "delayed" AS d ON d."callId" = c."callId" WHERE %s ORDER BY c."timestamp" ASC`, where)
	if err = db.Sql.QueryRow(query).Scan(&timestamp); err != nil && err != sql.ErrNoRows {
		return nil, formatError(err, query)
	}

	searchResults.DateStart = time.UnixMilli(timestamp)

	query = fmt.Sprintf(`SELECT c."timestamp" FROM "calls" AS c LEFT JOIN "systems" AS s ON s."systemId" = c."systemId" LEFT JOIN "talkgroups" AS t ON t."talkgroupId" = c."talkgroupId" LEFT JOIN "delayed" AS d ON d."callId" = c."callId" WHERE %s ORDER BY c."timestamp" DESC`, where)
	if err = db.Sql.QueryRow(query).Scan(&timestamp); err != nil && err != sql.ErrNoRows {
		return nil, formatError(err, query)
	}

	searchResults.DateStop = time.UnixMilli(timestamp)

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

	if tx, err = db.Sql.Begin(); err != nil {
		return 0, formatError(err, "")
	}

	if db.Config.DbType == DbTypePostgresql {
		query = fmt.Sprintf(`INSERT INTO "calls" ("audio", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES ($1, '%s', '%s', %d, %d, %d, %d) RETURNING "callId"`, call.AudioFilename, call.AudioMime, call.SiteRef, call.System.Id, call.Talkgroup.Id, call.Timestamp.UnixMilli())

		err = tx.QueryRow(query, call.Audio).Scan(&call.Id)

	} else {
		query = fmt.Sprintf(`INSERT INTO "calls" ("audio", "audioFilename", "audioMime", "siteRef", "systemId", "talkgroupId", "timestamp") VALUES (?, '%s', '%s', %d, %d, %d, %d)`, call.AudioFilename, call.AudioMime, call.SiteRef, call.System.Id, call.Talkgroup.Id, call.Timestamp.UnixMilli())

		if res, err = tx.Exec(query, call.Audio); err == nil {
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
