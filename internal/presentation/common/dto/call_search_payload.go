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

package dto

import (
	"encoding/json"
	"time"

	"rdio-scanner/internal/application/ports"
)

// CallSearchPagePayload is the answer to a call search: the page, and the criteria the listener asked
// with echoed back so the listener can tell which of its filters this server actually used.
type CallSearchPagePayload struct {
	Page ports.CallSearchPage

	// Criteria is the search as it arrived on the wire, not the search the query ran. The echo says
	// what the listener asked for, and the query says what this server could do about it.
	Criteria map[string]any
}

// MarshalJSON writes the answer.
//
// Unlike a call payload, this object is not built from a map: the legacy server marshals it from a
// struct, so its keys come out in the order its fields are declared — count, dateStart, dateStop,
// options, results — and a row comes out as id, system, talkgroup, dateTime. A call and a history row
// describing the same call therefore disagree about the order of the same keys, and the captured
// frames say so.
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

// CallSearchRow is one call in a history page. It carries the four things a history view lists, and
// the system is the system reference itself and not an object holding one — a listener that reads
// `row.system.systemRef` gets nothing and does not say so.
//
// The moment is a Go time and so keeps its milliseconds, where the same call in a call payload is
// formatted to the second.
//
// The length is on every row, named in seconds to the millisecond: the row is a struct, and a
// struct always names its fields, so a row the server stored before any source could measure it
// names the zero that means "never measured" — the same zero the column carries.
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

// callSearchEcho is the criteria the listener asked with, in the order the legacy server declares
// them. A criterion that is absent is left out, which is what an `any` holding nothing means here: a
// criterion that was asked for and happens to be zero is still there, and a criterion that was not
// asked for is not.
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

// echoCallSearch accepts a criterion only in the shape the search understands it in, which is the same
// rule domain.ParseCallSearch uses, with two differences that belong to the echo and not to the
// search: a page size that was not asked for is not invented here, and a page size that was asked for
// is not clamped here. The search clamps it to its own maximum and still answers with what was asked.
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
