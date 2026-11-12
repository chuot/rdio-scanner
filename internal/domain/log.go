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

package domain

import (
	"time"
)

// Severity levels a log entry carries. The values are part of the administrator
// payload and of the filters the log view offers.
const (
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

// LogSource is the part of the server that recorded an entry. The names are
// the operator's names for the parts, not the functions that write them, and
// the list is closed on purpose: a filter for a name outside it is rejected,
// not answered with an empty view. The vocabulary's history and the names no
// writer of this server writes live.
type LogSource string

const (
	LogSourceServer     LogSource = "server"
	LogSourceConfig     LogSource = "config"
	LogSourceDirwatch   LogSource = "dirwatch"
	LogSourceDownstream LogSource = "downstream"
	LogSourceDelayer    LogSource = "delayer"
	LogSourceIngest     LogSource = "ingest"
	LogSourceLogin      LogSource = "login"
	LogSourceAccess     LogSource = "access"
	LogSourceLivefeed   LogSource = "livefeed"
	LogSourceFFmpeg     LogSource = "ffmpeg"
	LogSourceUpload     LogSource = "upload"
	LogSourcePrune      LogSource = "prune"
	LogSourceLogs       LogSource = "logs"
	LogSourceDatabase   LogSource = "database"
	LogSourceAdmin      LogSource = "admin"

	// LogSourceMcp names the work an assistant does through this server's Model Context Protocol
	// surface. It is its own name and not the administrator's because the two are different things the
	// operator may want to look at apart: the administrator's own page, and what came through an
	// assistant. An operator filtering for it is asking who did what through an assistant, and that
	// question has to have a name to filter by.
	LogSourceMcp LogSource = "mcp"

	// LogSourceMaintenance names what an operator-initiated maintenance run did to the deployment: the
	// rows it removed and the tables it dropped. It is its own name and not the database's because the two
	// answer different questions — what the server did on its own at startup, and what it did because a
	// person ran the switch — and a deployment cleared months ago is explained by these entries alone.
	LogSourceMaintenance LogSource = "maintenance"

	// LogSourceCalls names the call repository of the server this one replaced. No writer here files under
	// it: see backfillOnlyLogSources.
	LogSourceCalls LogSource = "calls"

	// LogSourceLegacy is what an entry is filed under when it arrived before this field existed, or
	// when it was written by a server that does not write it. It is a read-side name as much as a
	// stored one: the log view never shows a blank source.
	LogSourceLegacy LogSource = "legacy"
)

// writtenLogSources are the parts of this server that write entries into the log view. A name is here
// because a writer files under it, and a test over the server's own source holds the two together: a name
// recorded as written that nothing writes is a filter the log view offers and nothing can ever answer.
var writtenLogSources = []LogSource{
	LogSourceServer, LogSourceConfig, LogSourceDirwatch, LogSourceDownstream, LogSourceDelayer,
	LogSourceIngest, LogSourceLogin, LogSourceAccess, LogSourceLivefeed, LogSourceFFmpeg,
	LogSourceUpload, LogSourcePrune, LogSourceLogs, LogSourceDatabase, LogSourceAdmin, LogSourceMcp,
	LogSourceMaintenance,
}

// backfillOnlyLogSources are vocabulary names no writer of this server
// writes: the log view still offers them so an operator can find rows a
// database written before the source column existed already holds. The
// calls and legacy names and where their rows come from live.
var backfillOnlyLogSources = []LogSource{
	LogSourceCalls, LogSourceLegacy,
}

// KnownLogSources is the vocabulary, in the order the log view lists it: the parts of the server that write
// entries, then the names that exist for reading rows this server never wrote.
func KnownLogSources() []LogSource {
	known := make([]LogSource, 0, len(writtenLogSources)+len(backfillOnlyLogSources))

	known = append(known, writtenLogSources...)

	return append(known, backfillOnlyLogSources...)
}

// LogSourceIsWritten reports whether a writer of this server files entries under this name. The names it
// answers false for are still in the vocabulary: they are the ones only the backfill reaches, and the log view
// offers them because the rows are already there.
func LogSourceIsWritten(source LogSource) bool {
	for _, written := range writtenLogSources {
		if written == source {
			return true
		}
	}

	return false
}

// IsKnownLogSource reports whether a name is one of the parts of this server.
func IsKnownLogSource(source LogSource) bool {
	for _, known := range KnownLogSources() {
		if source == known {
			return true
		}
	}

	return false
}

// ParseLogSource reads one source out of an administrator payload. Anything that is not a name in the
// vocabulary is refused, which is the one criterion the log search refuses rather than ignores: the
// others are filters the caller did not mean to apply, and this one is a subsystem the server does not
// have.
func ParseLogSource(value any) (LogSource, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}

	source := LogSource(text)
	if !IsKnownLogSource(source) {
		return "", false
	}

	return source, true
}

// Log search paging: how many entries a search returns when the caller does not ask,
// and the most it will ever return.
const (
	DefaultLogSearchLimit = uint(200)
	MaxLogSearchLimit     = uint(500)
)

// LogEntry is one recorded server event. Source is the part of the server that recorded it, and it is
// required: an entry that does not say where it came from is the defect this feature exists to remove.
type LogEntry struct {
	ID       uint64
	DateTime time.Time
	Level    string
	Source   LogSource
	Message  string
}

// LogSearch is the criteria the log view submits: which severity to keep, which subsystem wrote the
// entries, which substring of the message to look for, which day to look at, how many entries to return,
// where to resume, and which end to start from.
type LogSearch struct {
	Date      time.Time
	Level     string
	Source    LogSource
	Text      string
	Limit     uint
	Offset    uint
	Ascending bool

	// sourceRejected records that the caller did name a source and that the name is not one of the
	// parts of this server. It is kept apart from Source itself so that a search never runs with a
	// criterion nothing can match, while the endpoint can still answer the caller with a refusal
	// rather than an empty log view.
	sourceRejected bool
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

	// A source is read through the vocabulary. A name that is not in it leaves no filter applied and
	// is reported, which is the one criterion the log search does not silently ignore.
	if named, ok := m["source"]; ok {
		if source, valid := ParseLogSource(named); valid {
			search.Source = source
		} else {
			search.sourceRejected = true
		}
	}

	if v, ok := m["text"].(string); ok {
		search.Text = v
	}

	if v, ok := m["limit"].(float64); ok {
		search.Limit = clampPage(v, 1, MaxLogSearchLimit)
	}

	if v, ok := m["offset"].(float64); ok {
		search.Offset = clampPage(v, 0, MaxSearchOffset)
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

// HasSource reports whether the criteria restricts the subsystem that wrote the entries.
func (search LogSearch) HasSource() bool {
	return search.Source != ""
}

// HasText reports whether the criteria restricts the entries to those whose message contains the
// submitted substring.
func (search LogSearch) HasText() bool {
	return search.Text != ""
}

// SourceWasRejected reports whether the caller named a source that is not a part of this server. The
// criteria carry no source filter in that case; what the caller is told is the endpoint's decision, and it
// is a refusal rather than an empty page.
func (search LogSearch) SourceWasRejected() bool {
	return search.sourceRejected
}

// HasDate reports whether the criteria restricts the day.
func (search LogSearch) HasDate() bool {
	return !search.Date.IsZero()
}

// DateWindow returns the inclusive timestamp bounds the date criterion selects, or
// false when there is no date criterion. The window depends on the sort direction:
// ascending takes the submitted instant truncated to the minute and runs forward a
// day; descending runs back a day and then back again by the submitted hour and minute.
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
