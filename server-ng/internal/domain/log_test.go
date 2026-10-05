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
	"testing"
	"time"
)

func TestLogLevelsMatchTheLegacyServer(t *testing.T) {
	checks := map[string]struct{ got, want string }{
		"LogLevelInfo":  {LogLevelInfo, "info"},
		"LogLevelWarn":  {LogLevelWarn, "warn"},
		"LogLevelError": {LogLevelError, "error"},
	}

	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", name, c.got, c.want)
		}
	}
}

func TestLogSearchDefaults(t *testing.T) {
	search := ParseLogSearch(map[string]any{})

	if search.HasLevel() {
		t.Error("an empty criteria must not filter by level")
	}

	if search.HasDate() {
		t.Error("an empty criteria must not filter by date")
	}

	if search.Limit != DefaultLogSearchLimit {
		t.Errorf("Limit = %d, want the default %d", search.Limit, DefaultLogSearchLimit)
	}

	if !search.Ascending {
		t.Error("the default sort order is ascending")
	}
}

func TestLogSearchClampsTheLimitButHonoursAnExplicitZero(t *testing.T) {
	if got := ParseLogSearch(map[string]any{"limit": float64(5000)}).Limit; got != MaxLogSearchLimit {
		t.Errorf("Limit = %d, want it clamped to %d", got, MaxLogSearchLimit)
	}

	if got := ParseLogSearch(map[string]any{"limit": float64(50)}).Limit; got != 50 {
		t.Errorf("Limit = %d, want the submitted 50", got)
	}

	// The legacy server clamps a submitted limit but never replaces a submitted zero
	// with the default, so an explicit zero asks for no rows at all.
	if got := ParseLogSearch(map[string]any{"limit": float64(0)}).Limit; got != 0 {
		t.Errorf("Limit = %d, want the submitted 0 preserved", got)
	}
}

func TestLogSearchSortDirection(t *testing.T) {
	if !ParseLogSearch(map[string]any{"sort": float64(1)}).Ascending {
		t.Error("a positive sort asks for ascending order")
	}

	if ParseLogSearch(map[string]any{"sort": float64(-1)}).Ascending {
		t.Error("a negative sort asks for descending order")
	}

	if !ParseLogSearch(map[string]any{"sort": float64(0)}).Ascending {
		t.Error("a zero sort asks for ascending order")
	}

	if !ParseLogSearch(map[string]any{}).Ascending {
		t.Error("an absent sort asks for ascending order")
	}
}

func TestLogSearchDateMustParse(t *testing.T) {
	search := ParseLogSearch(map[string]any{"date": "2026-03-14T09:30:00Z"})

	if !search.HasDate() {
		t.Fatal("a valid RFC3339 date must set the date filter")
	}

	if want := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC); !search.Date.Equal(want) {
		t.Errorf("Date = %v, want %v", search.Date, want)
	}

	if ParseLogSearch(map[string]any{"date": "not a date"}).HasDate() {
		t.Error("an unparsable date must leave the criteria unfiltered, as the legacy server does")
	}
}

func TestLogSearchLevelFilter(t *testing.T) {
	search := ParseLogSearch(map[string]any{"level": LogLevelError})

	if !search.HasLevel() || search.Level != LogLevelError {
		t.Errorf("ParseLogSearch = %+v, want an error-level filter", search)
	}
}

// TestLogSearchDateWindowDependsOnSortDirection pins a legacy asymmetry: the day the
// date filter selects is a full 24 hours from the truncated timestamp when sorting
// ascending, but is shifted back by the submitted time of day when sorting descending.
func TestLogSearchDateWindowDependsOnSortDirection(t *testing.T) {
	date := time.Date(2026, 3, 14, 9, 30, 15, 500, time.UTC)

	ascending := LogSearch{Date: date, Ascending: true}

	from, to, _ := ascending.DateWindow()

	wantFrom := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)
	wantTo := wantFrom.Add(24*time.Hour - time.Millisecond)

	if !from.Equal(wantFrom) {
		t.Errorf("ascending window start = %v, want %v (seconds and sub-second dropped)", from, wantFrom)
	}

	if !to.Equal(wantTo) {
		t.Errorf("ascending window stop = %v, want %v", to, wantTo)
	}

	descending := LogSearch{Date: date, Ascending: false}

	from, to, _ = descending.DateWindow()

	// 24 hours back, then back again by the submitted hour and minute.
	wantFrom = time.Date(2026, 3, 13, 9, 30, 0, 0, time.UTC).Add(-9*time.Hour - 30*time.Minute)
	wantTo = wantFrom.Add(24*time.Hour - time.Millisecond - 9*time.Hour - 30*time.Minute)

	if !from.Equal(wantFrom) {
		t.Errorf("descending window start = %v, want %v", from, wantFrom)
	}

	if !to.Equal(wantTo) {
		t.Errorf("descending window stop = %v, want %v", to, wantTo)
	}
}

func TestLogSearchWithoutADateHasNoWindow(t *testing.T) {
	from, to, ok := LogSearch{}.DateWindow()

	if ok {
		t.Errorf("DateWindow with no date = %v..%v, want no window", from, to)
	}
}

func TestLogSearchOffset(t *testing.T) {
	if got := ParseLogSearch(map[string]any{"offset": float64(400)}).Offset; got != 400 {
		t.Errorf("Offset = %d, want the submitted 400", got)
	}
}

func TestLogEntryCarriesItsOwnValues(t *testing.T) {
	at := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

	entry := LogEntry{ID: 12, DateTime: at, Level: LogLevelWarn, Message: "duplicate call rejected"}

	if entry.ID != 12 || entry.Level != LogLevelWarn || entry.Message != "duplicate call rejected" || !entry.DateTime.Equal(at) {
		t.Errorf("LogEntry = %+v, want the submitted values", entry)
	}
}

func TestLogSearchLimitConstants(t *testing.T) {
	if DefaultLogSearchLimit != 200 {
		t.Errorf("DefaultLogSearchLimit = %d, want 200", DefaultLogSearchLimit)
	}

	if MaxLogSearchLimit != 500 {
		t.Errorf("MaxLogSearchLimit = %d, want 500", MaxLogSearchLimit)
	}
}
