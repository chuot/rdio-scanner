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

package domain

import (
	"math"
	"time"
)

// Severity levels a log entry carries. The values are part of the administrator
// payload and of the filters the log view offers.
const (
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

// Log search paging: how many entries a search returns when the caller does not ask,
// and the most it will ever return.
const (
	DefaultLogSearchLimit = uint(200)
	MaxLogSearchLimit     = uint(500)
)

// LogEntry is one recorded server event.
type LogEntry struct {
	ID       uint64
	DateTime time.Time
	Level    string
	Message  string
}

// LogSearch is the criteria the log view submits: which severity to keep, which day to
// look at, how many entries to return, where to resume, and which end to start from.
type LogSearch struct {
	Date      time.Time
	Level     string
	Limit     uint
	Offset    uint
	Ascending bool
}

// ParseLogSearch reads the criteria out of an administrator payload. Anything that does
// not parse leaves that criterion unfiltered, which is what the legacy server does.
func ParseLogSearch(m map[string]any) LogSearch {
	search := LogSearch{Limit: DefaultLogSearchLimit, Ascending: true}

	if v, ok := m["date"].(string); ok {
		if at, err := time.Parse(time.RFC3339, v); err == nil {
			search.Date = at
		}
	}

	if v, ok := m["level"].(string); ok {
		search.Level = v
	}

	if v, ok := m["limit"].(float64); ok {
		search.Limit = uint(math.Min(float64(MaxLogSearchLimit), v))
	}

	if v, ok := m["offset"].(float64); ok {
		search.Offset = uint(v)
	}

	if v, ok := m["sort"].(float64); ok {
		search.Ascending = v >= 0
	}

	return search
}

// HasLevel reports whether the criteria restricts the severity.
func (search LogSearch) HasLevel() bool {
	return search.Level != ""
}

// HasDate reports whether the criteria restricts the day.
func (search LogSearch) HasDate() bool {
	return !search.Date.IsZero()
}

// DateWindow returns the inclusive timestamp bounds the date criterion selects, or
// false when there is no date criterion.
//
// The window depends on the sort direction, which is how the legacy server behaves:
// ascending takes the submitted instant truncated to the minute and runs forward a
// full day; descending runs back a full day and then back again by the submitted hour
// and minute.
func (search LogSearch) DateWindow() (time.Time, time.Time, bool) {
	if !search.HasDate() {
		return time.Time{}, time.Time{}, false
	}

	// Seconds and sub-second parts of the submitted instant are discarded.
	truncated := time.Date(
		search.Date.Year(), search.Date.Month(), search.Date.Day(),
		search.Date.Hour(), search.Date.Minute(), 0, 0,
		time.UTC,
	)

	if search.Ascending {
		return truncated, truncated.Add(24*time.Hour - time.Millisecond), true
	}

	offset := time.Duration(search.Date.Hour())*time.Hour + time.Duration(search.Date.Minute())*time.Minute

	from := truncated.Add(-24*time.Hour - offset)
	to := from.Add(24*time.Hour - time.Millisecond - offset)

	return from, to, true
}
