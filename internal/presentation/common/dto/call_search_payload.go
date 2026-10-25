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

package dto

import (
	"encoding/json"
	"time"

	"rdio-scanner/internal/application/ports"
)

// CallSearchPagePayload is one search answer: the page and the criteria the
// listener asked with, echoed back. see the call-search-page-ordering maintainer note.
type CallSearchPagePayload struct {
	Page ports.CallSearchPage

	// Criteria is the search as it arrived on the wire, not the
	// search the query ran. see the call-search-page-ordering maintainer note.
	Criteria map[string]any
}

// MarshalJSON writes the answer. The legacy server marshaled
// this one from a struct, so keys keep declaration order:
// count, dateStart, dateStop, options, results, a row as id,
// system, talkgroup, dateTime. see the call-search-page-ordering maintainer note.
func (payload CallSearchPagePayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(callSearchEnvelope{
		Count:     payload.Page.Count,
		DateStart: payload.Page.DateStart,
		DateStop:  payload.Page.DateStop,
		Options:   echoCallSearch(payload.Criteria),
		Results:   callSearchRows(payload.Page.Results),
	})
}

// callSearchEnvelope is the answer in the order the legacy server declares it.
type callSearchEnvelope struct {
	Count     uint            `json:"count"`
	DateStart time.Time       `json:"dateStart"`
	DateStop  time.Time       `json:"dateStop"`
	Options   callSearchEcho  `json:"options"`
	Results   []CallSearchRow `json:"results"`
}

// CallSearchRow is one call in a history page: the four things a
// history view lists. The system is the system reference itself,
// not an object holding one, the moment keeps milliseconds, and
// the length is on every row. see the call-search-page-ordering maintainer note.
type CallSearchRow struct {
	ID          uint64          `json:"id"`
	System      uint            `json:"system"`
	Talkgroup   uint            `json:"talkgroup"`
	Timestamp   time.Time       `json:"dateTime"`
	AudioLength json.RawMessage `json:"audioLength"`
}

func callSearchRows(summaries []ports.CallSummary) []CallSearchRow {
	rows := []CallSearchRow{}

	for _, summary := range summaries {
		rows = append(rows, CallSearchRow{
			ID:          summary.ID,
			System:      summary.SystemRef,
			Talkgroup:   summary.TalkgroupRef,
			Timestamp:   summary.Timestamp,
			AudioLength: audioLengthSeconds(summary.AudioLength),
		})
	}

	return rows
}

// callSearchEcho is the criteria the listener asked with, in the
// order the legacy server declares them. An absent criterion is
// left out; a zero criterion that was asked for is still there.
// see the call-search-page-ordering maintainer note.
type callSearchEcho struct {
	Date      any `json:"date,omitempty"`
	Group     any `json:"group,omitempty"`
	Limit     any `json:"limit,omitempty"`
	Offset    any `json:"offset,omitempty"`
	Sort      any `json:"sort,omitempty"`
	System    any `json:"system,omitempty"`
	Tag       any `json:"tag,omitempty"`
	Talkgroup any `json:"talkgroup,omitempty"`
}

// echoCallSearch accepts a criterion only in the shape the search
// understands, as domain.ParseCallSearch does, except the echo
// neither invents an unasked page size nor clamps an asked one.
// see the call-search-page-ordering maintainer note.
func echoCallSearch(criteria map[string]any) callSearchEcho {
	echo := callSearchEcho{}

	if criteria == nil {
		return echo
	}

	if v, ok := criteria["date"].(string); ok {
		if at, err := time.Parse(time.RFC3339, v); err == nil {
			echo.Date = at
		}
	}

	if v, ok := criteria["group"].(string); ok {
		echo.Group = v
	}

	if v, ok := criteria["limit"].(float64); ok {
		echo.Limit = uint(v)
	}

	if v, ok := criteria["offset"].(float64); ok {
		echo.Offset = uint(v)
	}

	if v, ok := criteria["sort"].(float64); ok {
		echo.Sort = int(v)
	}

	if v, ok := criteria["system"].(float64); ok {
		echo.System = uint(v)
	}

	if v, ok := criteria["tag"].(string); ok {
		echo.Tag = v
	}

	if v, ok := criteria["talkgroup"].(float64); ok {
		echo.Talkgroup = uint(v)
	}

	return echo
}
