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
	"time"

	"rdio-scanner/internal/application/ports"
)

// AdminLoginReply is what a login is answered with: the credential that stands for the new session, and
// whether the password on record still needs changing.
//
// The flag is carried and not decided here. The use case reports it as true on every accepted login,
// whatever the stored option says, because that is what the legacy server has always answered at login;
// the option itself is only ever reported by the configuration endpoints.
type AdminLoginReply struct {
	PasswordNeedChange bool   `json:"passwordNeedChange"`
	Token              string `json:"token"`
}

// AdminPasswordReply is what a password change is answered with, and it is the same single flag the
// configuration endpoints report: what the administrator's page does next depends on it.
type AdminPasswordReply struct {
	PasswordNeedChange bool `json:"passwordNeedChange"`
}

// AlertsPayload is the nine alert sequences the server carries.
//
// They are not configuration: no option and no administrator changes them. The administrator's endpoint
// is where a client that shows alerts fetches them, and every client reads the same nine.
type AlertsPayload map[string][]Tone

// Alerts returns the nine alert sequences.
func Alerts() AlertsPayload {
	return AlertsPayload(alertTones)
}

// AdminLogsPagePayload is the log view's answer.
//
// The count is the total the criteria matches and not the number of entries on this page, because the log
// view pages by resubmitting the same criteria further along. The span is the same: it belongs to
// everything the criteria match, and it stays where it is while the page moves.
//
// The criteria are echoed back as they were submitted, so that the view can ask again for the next page
// without remembering anything of its own.
type AdminLogsPagePayload struct {
	Count     uint64            `json:"count"`
	DateStart time.Time         `json:"dateStart"`
	DateStop  time.Time         `json:"dateStop"`
	Options   logSearchCriteria `json:"options"`
	Logs      []AdminLogEntry   `json:"logs"`
}

// AdminLogEntry is one recorded server event, as the log view shows it.
type AdminLogEntry struct {
	ID       uint64    `json:"id"`
	DateTime time.Time `json:"dateTime"`
	Level    string    `json:"level"`
	Message  string    `json:"message"`
}

// AdminLogsPage builds the log view's answer from the page the query returned and the criteria the
// request carried.
//
// The criteria are echoed from the request and not from the parsed search, because the two are not the
// same thing: a limit larger than the most the search will ever return is used as the smaller number and
// echoed as the number that was asked for.
func AdminLogsPage(page ports.LogSearchPage, criteria map[string]any) AdminLogsPagePayload {
	logs := []AdminLogEntry{}

	for _, entry := range page.Results {
		logs = append(logs, AdminLogEntry{
			ID:       entry.ID,
			DateTime: entry.DateTime,
			Level:    entry.Level,
			Message:  entry.Message,
		})
	}

	return AdminLogsPagePayload{
		Count:     page.Count,
		DateStart: page.DateStart,
		DateStop:  page.DateStop,
		Options:   echoLogSearch(criteria),
		Logs:      logs,
	}
}

// logSearchCriteria is the submitted criteria, echoed back. A field is written only when the request named
// it and the server understood it: a criterion that was not submitted is not invented, and one that did
// not parse is not claimed.
//
// The five fields are written in this order, and the order is what the log view reads.
type logSearchCriteria struct {
	Date   any `json:"date,omitempty"`
	Level  any `json:"level,omitempty"`
	Limit  any `json:"limit,omitempty"`
	Offset any `json:"offset,omitempty"`
	Sort   any `json:"sort,omitempty"`
}

// echoLogSearch reads the criteria back out of the submitted request, with the same rules the search
// itself uses to read them, so that what is echoed is what the search actually ran with.
//
// The limit is echoed as submitted. Clamping it to the most the search will return is the search's
// business, and the log view is shown what it asked for.
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
