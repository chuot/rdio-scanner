// Copyright (C) 2019-2022 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

type Call struct {
	Id             any       `json:"id"`
	Audio          []byte    `json:"audio"`
	AudioName      any       `json:"audioName"`
	AudioType      any       `json:"audioType"`
	DateTime       time.Time `json:"dateTime"`
	Frequencies    any       `json:"frequencies"`
	Frequency      any       `json:"frequency"`
	Patches        any       `json:"patches"`
	Source         any       `json:"source"`
	Sources        any       `json:"sources"`
	System         uint      `json:"system"`
	Talkgroup      uint      `json:"talkgroup"`
	systemLabel    any
	talkgroupGroup any
	talkgroupLabel any
	talkgroupName  any
	talkgroupTag   any
	units          any
}

func NewCall() *Call {
	return &Call{
		Frequencies: []map[string]any{},
		Patches:     []uint{},
		Sources:     []map[string]any{},
	}
}

func (call *Call) IsValid() (ok bool, err error) {
	ok = true

	if len(call.Audio) <= 44 {
		ok = false
		err = errors.New("no audio")
	}

	if call.DateTime.Unix() == 0 {
		ok = false
		err = errors.New("no datetime")
	}

	if call.System < 1 {
		ok = false
		err = errors.New("no system")
	}

	if call.Talkgroup < 1 {
		ok = false
		err = errors.New("no talkgroup")
	}

	return ok, err
}

func (call *Call) MarshalJSON() ([]byte, error) {
	audio := fmt.Sprintf("%v", call.Audio)
	audio = strings.ReplaceAll(audio, " ", ",")

	return json.Marshal(map[string]any{
		"id": call.Id,
		"audio": map[string]any{
			"data": json.RawMessage(audio),
			"type": "Buffer",
		},
		"audioName":   call.AudioName,
		"audioType":   call.AudioType,
		"dateTime":    call.DateTime.Format(time.RFC3339),
		"frequencies": call.Frequencies,
		"frequency":   call.Frequency,
		"patches":     call.Patches,
		"source":      call.Source,
		"sources":     call.Sources,
		"system":      call.System,
		"talkgroup":   call.Talkgroup,
	})
}

func (call *Call) ToJson() (string, error) {
	if b, err := json.Marshal(call); err == nil {
		return string(b), nil
	} else {
		return "", fmt.Errorf("call.tojson: %v", err)
	}
}

type Calls struct {
	mutex sync.Mutex
}

func NewCalls() *Calls {
	return &Calls{
		mutex: sync.Mutex{},
	}
}

func (calls *Calls) CheckDuplicate(call *Call, msTimeFrame uint, db *Database) bool {
	var count uint

	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	d := time.Duration(msTimeFrame) * time.Millisecond
	from := call.DateTime.Add(-d)
	to := call.DateTime.Add(d)

	query := fmt.Sprintf("select count(*) from `rdioScannerCalls` where (`dateTime` between '%v' and '%v') and `system` = %v and `talkgroup` = %v", from, to, call.System, call.Talkgroup)
	if err := db.Sql.QueryRow(query).Scan(&count); err != nil {
		return false
	}

	return count > 0
}

func (calls *Calls) GetCall(id uint, db *Database) (*Call, error) {
	var (
		audioName   sql.NullString
		audioType   sql.NullString
		dateTime    any
		frequency   sql.NullFloat64
		source      sql.NullFloat64
		frequencies string
		patches     string
		sources     string
		t           time.Time
	)

	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	call := Call{Id: id}

	query := fmt.Sprintf("select `audio`, `audioName`, `audioType`, `DateTime`, `frequencies`, `frequency`, `patches`, `source`, `sources`, `system`, `talkgroup` from `rdioScannerCalls` where `id` = %v", id)
	err := db.Sql.QueryRow(query).Scan(&call.Audio, &audioName, &audioType, &dateTime, &frequencies, &frequency, &patches, &source, &sources, &call.System, &call.Talkgroup)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("getcall: %v, %v", err, query)
	}

	if audioName.Valid {
		call.AudioName = audioName.String
	}

	if audioType.Valid {
		call.AudioType = audioType.String
	}

	if frequency.Valid && frequency.Float64 > 0 {
		call.Frequency = uint(frequency.Float64)
	}

	if t, err = db.ParseDateTime(dateTime); err == nil {
		call.DateTime = t
	} else {
		call.DateTime = time.Time{}
	}

	if len(frequencies) > 0 {
		if err = json.Unmarshal([]byte(frequencies), &call.Frequencies); err != nil {
			call.Frequencies = []any{}
		}
	}

	if len(patches) > 0 {
		if err = json.Unmarshal([]byte(patches), &call.Patches); err != nil {
			call.Patches = []any{}
		}
	}

	if source.Valid && source.Float64 > 0 {
		call.Source = uint(source.Float64)
	}

	if len(sources) > 0 {
		if err = json.Unmarshal([]byte(sources), &call.Sources); err != nil {
			call.Sources = []any{}
		}
	}

	return &call, nil
}

func (calls *Calls) Prune(db *Database, pruneDays uint) error {
	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	date := time.Now().Add(-24 * time.Hour * time.Duration(pruneDays)).Format(db.DateTimeFormat)
	_, err := db.Sql.Exec("delete from `rdioScannerCalls` where `dateTime` < ?", date)

	return err
}

func (calls *Calls) Search(searchOptions *CallsSearchOptions, client *Client) (*CallsSearchResults, error) {
	const (
		ascOrder  = "asc"
		descOrder = "desc"
	)

	var (
		dateTime any
		err      error
		id       sql.NullFloat64
		limit    uint
		offset   uint
		order    string
		query    string
		rows     *sql.Rows
		t        time.Time
		where    string = "true"
	)

	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	db := client.Controller.Database

	formatError := func(err error) error {
		return fmt.Errorf("calls.search: %v", err)
	}

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
						c = fmt.Sprintf("(`system` = %v and `talkgroup` in %v)", v["id"], b)
					case string:
						if v["talkgroups"] == "*" {
							c = fmt.Sprintf("`system` = %v", v["id"])
						}
					}
				}
				if len(c) > 0 {
					a = append(a, c)
				}
			}
			where = fmt.Sprintf("(%s)", strings.Join(a, " or "))
		}
	}

	switch v := searchOptions.System.(type) {
	case uint:
		a := []string{
			fmt.Sprintf("`system` = %v", v),
		}
		switch v := searchOptions.Talkgroup.(type) {
		case uint:
			if searchOptions.searchPatchedTalkgroups {
				a = append(a, fmt.Sprintf("`talkgroup` = %v or patches = '%v' or patches like '[%v,%%' or patches like '%%,%v,%%' or patches like '%%,%v]'", v, v, v, v, v))
			} else {
				a = append(a, fmt.Sprintf("`talkgroup` = %v", v))
			}
		}
		where += fmt.Sprintf(" and (%s)", strings.Join(a, " and "))
	}

	switch v := searchOptions.Group.(type) {
	case string:
		a := []string{}
		for id, m := range client.GroupsMap[v] {
			b := strings.ReplaceAll(fmt.Sprintf("%v", m), " ", ", ")
			b = strings.ReplaceAll(b, "[", "(")
			b = strings.ReplaceAll(b, "]", ")")
			a = append(a, fmt.Sprintf("(`system` = %v and `talkgroup` in %v)", id, b))
		}
		if len(a) > 0 {
			where += fmt.Sprintf(" and (%s)", strings.Join(a, " or "))
		}
	}

	switch v := searchOptions.Tag.(type) {
	case string:
		a := []string{}
		for id, m := range client.TagsMap[v] {
			b := strings.ReplaceAll(fmt.Sprintf("%v", m), " ", ", ")
			b = strings.ReplaceAll(b, "[", "(")
			b = strings.ReplaceAll(b, "]", ")")
			a = append(a, fmt.Sprintf("(`system` = %v and `talkgroup` in %v)", id, b))
		}
		if len(a) > 0 {
			where += fmt.Sprintf(" and (%s)", strings.Join(a, " or "))
		}
	}

	query = fmt.Sprintf("select `dateTime` from `rdioScannerCalls` where %v order by `dateTime` asc", where)
	if err = db.Sql.QueryRow(query).Scan(&dateTime); err != nil && err != sql.ErrNoRows {
		return nil, formatError(fmt.Errorf("%v, %v", err, query))
	}

	if t, err = db.ParseDateTime(dateTime); err == nil {
		searchResults.DateStart = t
	}

	query = fmt.Sprintf("select `dateTime` from `rdioScannerCalls` where %v order by `dateTime` desc", where)
	if err = db.Sql.QueryRow(query).Scan(&dateTime); err != nil && err != sql.ErrNoRows {
		return nil, formatError(fmt.Errorf("%v, %v", err, query))
	}

	if t, err = db.ParseDateTime(dateTime); err == nil {
		searchResults.DateStop = t
	} else {
		searchResults.DateStop = time.Now()
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
			df    string = client.Controller.Database.DateTimeFormat
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

		where += fmt.Sprintf(" and (`dateTime` between '%v' and '%v')", start.Format(df), stop.Format(df))
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

	query = fmt.Sprintf("select count(*) from `rdioScannerCalls` where %v", where)
	if err = db.Sql.QueryRow(query).Scan(&searchResults.Count); err != nil && err != sql.ErrNoRows {
		return nil, formatError(fmt.Errorf("%v, %v", err, query))
	}

	query = fmt.Sprintf("select `id`, `DateTime`, `system`, `talkgroup` from `rdioScannerCalls` where %v order by `dateTime` %v limit %v offset %v", where, order, limit, offset)
	if rows, err = db.Sql.Query(query); err != nil && err != sql.ErrNoRows {
		return nil, formatError(fmt.Errorf("%v, %v", err, query))
	}

	for rows.Next() {
		searchResult := CallsSearchResult{}
		if err = rows.Scan(&id, &dateTime, &searchResult.System, &searchResult.Talkgroup); err != nil {
			break
		}

		if id.Valid && id.Float64 > 0 {
			searchResult.Id = uint(id.Float64)
		}

		if t, err = db.ParseDateTime(dateTime); err == nil {
			searchResult.DateTime = t

		} else {
			continue
		}

		searchResults.Results = append(searchResults.Results, searchResult)
	}

	rows.Close()

	if err != nil {
		return nil, formatError(err)
	}

	return searchResults, err
}

func (calls *Calls) WriteCall(call *Call, db *Database) (uint, error) {
	var (
		b           []byte
		err         error
		frequencies string
		id          int64
		patches     string
		res         sql.Result
		sources     string
	)

	calls.mutex.Lock()
	defer calls.mutex.Unlock()

	formatError := func(err error) error {
		return fmt.Errorf("call.write: %s", err.Error())
	}

	switch v := call.Frequencies.(type) {
	case []map[string]any:
		if b, err = json.Marshal(v); err == nil {
			frequencies = string(b)
		} else {
			return 0, formatError(err)
		}
	}

	switch v := call.Patches.(type) {
	case []uint:
		if b, err = json.Marshal(v); err == nil {
			patches = string(b)
		} else {
			return 0, formatError(err)
		}
	}

	switch v := call.Sources.(type) {
	case []map[string]any:
		if b, err = json.Marshal(v); err == nil {
			sources = string(b)
		} else {
			return 0, formatError(err)
		}
	}

	if res, err = db.Sql.Exec("insert into `rdioScannerCalls` (`id`, `audio`, `audioName`, `audioType`, `dateTime`, `frequencies`, `frequency`, `patches`, `source`, `sources`, `system`, `talkgroup`) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", call.Id, call.Audio, call.AudioName, call.AudioType, call.DateTime, frequencies, call.Frequency, patches, call.Source, sources, call.System, call.Talkgroup); err != nil {
		return 0, formatError(err)
	}

	if id, err = res.LastInsertId(); err == nil {
		return uint(id), nil
	} else {
		return 0, formatError(err)
	}
}

type CallsSearchOptions struct {
	Date                    any `json:"date,omitempty"`
	Group                   any `json:"group,omitempty"`
	Limit                   any `json:"limit,omitempty"`
	Offset                  any `json:"offset,omitempty"`
	Sort                    any `json:"sort,omitempty"`
	System                  any `json:"system,omitempty"`
	Tag                     any `json:"tag,omitempty"`
	Talkgroup               any `json:"talkgroup,omitempty"`
	searchPatchedTalkgroups bool
}

func (searchOptions *CallsSearchOptions) fromMap(m map[string]any) error {
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

	return nil
}

type CallsSearchResult struct {
	Id        uint      `json:"id"`
	DateTime  time.Time `json:"dateTime"`
	System    uint      `json:"system"`
	Talkgroup uint      `json:"talkgroup"`
}

type CallsSearchResults struct {
	Count     uint                `json:"count"`
	DateStart time.Time           `json:"dateStart"`
	DateStop  time.Time           `json:"dateStop"`
	Options   *CallsSearchOptions `json:"options"`
	Results   []CallsSearchResult `json:"results"`
}
