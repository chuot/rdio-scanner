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
	"fmt"
	"math"
	"strings"
	"time"
)

type SearchOptions struct {
	Date                    interface{} `json:"date,omitempty"`
	Group                   interface{} `json:"group,omitempty"`
	Limit                   interface{} `json:"limit,omitempty"`
	Offset                  interface{} `json:"offset,omitempty"`
	Sort                    interface{} `json:"sort,omitempty"`
	System                  interface{} `json:"system,omitempty"`
	Tag                     interface{} `json:"tag,omitempty"`
	Talkgroup               interface{} `json:"talkgroup,omitempty"`
	searchPatchedTalkgroups bool
}

func (searchOptions *SearchOptions) fromMap(m map[string]interface{}) error {
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

type SearchResult struct {
	Id        uint      `json:"id"`
	DateTime  time.Time `json:"dateTime"`
	System    uint      `json:"system"`
	Talkgroup uint      `json:"talkgroup"`
}

type SearchResults struct {
	Count     uint           `json:"count"`
	DateStart time.Time      `json:"dateStart"`
	DateStop  time.Time      `json:"dateStop"`
	Options   *SearchOptions `json:"options"`
	Results   []SearchResult `json:"results"`
}

func NewSearchResults(searchOptions *SearchOptions, client *Client) (*SearchResults, error) {
	const (
		ascOrder  = "asc"
		descOrder = "desc"
	)

	var (
		dateTime interface{}
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

	db := client.Controller.Database

	formatError := func(err error) error {
		return fmt.Errorf("newSearchResults: %v", err)
	}

	searchResults := &SearchResults{
		Options: searchOptions,
		Results: []SearchResult{},
	}

	if client.Access != nil {
		switch v := client.Access.Systems.(type) {
		case []interface{}:
			a := []string{}
			for _, scope := range v {
				var c string
				switch v := scope.(type) {
				case map[string]interface{}:
					switch v["talkgroups"].(type) {
					case []interface{}:
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

	if dateTime == nil {
		return searchResults, nil
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
			start = time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), 0, 0, time.UTC).Add(time.Hour*-24 - time.Duration(v.Hour())).Add(time.Minute * time.Duration(-v.Minute()))
			stop = start.Add(time.Hour*24 - time.Millisecond - time.Duration(v.Hour())).Add(time.Minute * time.Duration(-v.Minute()))
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
		searchResult := SearchResult{}
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
