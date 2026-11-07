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
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// AdminLoginReply is what a login is answered with: the session
// credential and the needs-changing flag. The flag is carried, not
// decided here: the use case reports it true on every accepted login,
// as the legacy server always answered at login.
type AdminLoginReply struct {
	PasswordNeedChange bool   `json:"passwordNeedChange"`
	Token              string `json:"token"`
}

// AdminPasswordReply is what a password change is answered with: the
// same single flag the configuration endpoints report; the page acts on it.
type AdminPasswordReply struct {
	PasswordNeedChange bool `json:"passwordNeedChange"`
}

// AlertsPayload is the nine alert sequences the server carries. They
// are not configuration and no option changes them: every alert client
// reads the same nine from the administrator's endpoint.
type AlertsPayload map[string][]Tone

// Alerts returns the nine alert sequences.
func Alerts() AlertsPayload {
	return AlertsPayload(alertTones)
}

// AdminLogsPagePayload is the log view's answer. Count and span cover
// the whole match, not this page, and the criteria are echoed so the
// view can page again; subsystem counts accompany every search.
type AdminLogsPagePayload struct {
	Count     uint64            `json:"count"`
	DateStart time.Time         `json:"dateStart"`
	DateStop  time.Time         `json:"dateStop"`
	Options   logSearchCriteria `json:"options"`
	Sources   []AdminLogSource  `json:"sources"`
	Logs      []AdminLogEntry   `json:"logs"`
}

// AdminLogSource is how many entries one part of the server wrote among everything the criteria match.
type AdminLogSource struct {
	Source string `json:"source"`
	Count  uint64 `json:"count"`
}

// AdminLogEntry is one recorded server event, as the log view shows it.
// The source names the part of the server that wrote the entry, which
// is what the log view narrows on.
type AdminLogEntry struct {
	ID       uint64    `json:"id"`
	DateTime time.Time `json:"dateTime"`
	Level    string    `json:"level"`
	Source   string    `json:"source"`
	Message  string    `json:"message"`
}

// AdminLogsPage builds the log view's answer from the query's page and
// the request's criteria: the echo comes from the request, not the parsed
// search, and subsystem counts pass through in the search's order.
func AdminLogsPage(page ports.LogSearchPage, criteria map[string]any) AdminLogsPagePayload {
	logs := []AdminLogEntry{}

	for _, entry := range page.Results {
		logs = append(logs, AdminLogEntry{
			ID:       entry.ID,
			DateTime: entry.DateTime,
			Level:    entry.Level,
			Source:   string(entry.Source),
			Message:  entry.Message,
		})
	}

	// sources stays an empty list, not a null: a matchless search has no subsystems.
	sources := []AdminLogSource{}

	for _, count := range page.SourceCounts {
		sources = append(sources, AdminLogSource{Source: string(count.Source), Count: count.Count})
	}

	return AdminLogsPagePayload{
		Count:     page.Count,
		DateStart: page.DateStart,
		DateStop:  page.DateStop,
		Options:   echoLogSearch(criteria),
		Sources:   sources,
		Logs:      logs,
	}
}

// logSearchCriteria is the submitted criteria, echoed back. A field is
// written only when the request named it and the server understood it;
// the seven fields keep the order the log view reads.
type logSearchCriteria struct {
	Date   any `json:"date,omitempty"`
	Level  any `json:"level,omitempty"`
	Source any `json:"source,omitempty"`
	Text   any `json:"text,omitempty"`
	Limit  any `json:"limit,omitempty"`
	Offset any `json:"offset,omitempty"`
	Sort   any `json:"sort,omitempty"`
}

// echoLogSearch reads the criteria out of the submitted request under
// the search's own reading rules, so the echo is what the search ran
// with, and the limit is echoed as submitted, not clamped.
func echoLogSearch(criteria map[string]any) logSearchCriteria {
	echo := logSearchCriteria{}

	if v, ok := criteria["date"].(string); ok {
		if at, err := time.Parse(time.RFC3339, v); err == nil {
			echo.Date = at
		}
	}

	if v, ok := criteria["level"].(string); ok {
		echo.Level = v
	}

	// A subsystem is read through the vocabulary, as the search reads it.
	// A name outside it applied no filter, so echoing it would claim a
	// criterion the search never ran with.
	if named, ok := criteria["source"]; ok {
		if source, valid := domain.ParseLogSource(named); valid {
			echo.Source = string(source)
		}
	}

	if v, ok := criteria["text"].(string); ok {
		echo.Text = v
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

	return echo
}
