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

	"rdio-scanner/internal/application"
)

// AdminStatusPayload is the administrator's status view: what this server is, what it is running right
// now, what it recorded over the last 24 hours, and what it lately had to complain about.
//
// The identity of the deployment — the version, the container answer, the folder it serves — is named
// here beside the snapshot the use case measured, because the administrator surface already carries those
// three and the use case cannot measure them.
type AdminStatusPayload struct {
	Server   adminStatusServer   `json:"server"`
	Live     adminStatusLive     `json:"live"`
	Activity adminStatusActivity `json:"activity"`
	Logs     adminStatusLogs     `json:"logs"`
}

type adminStatusServer struct {
	Version string `json:"version"`
	Docker  bool   `json:"docker"`

	StartedAt time.Time `json:"startedAt"`
	Now       time.Time `json:"now"`

	// UptimeSeconds is whole seconds, and 0 for a server that has not recorded a start. The page decides
	// how to say it — "2h 14m", "41s" — and the payload does not decide the words.
	UptimeSeconds int64 `json:"uptimeSeconds"`

	BaseDir string `json:"baseDir"`

	// DatabaseBytes is the size of the file this server writes its database into, and DatabaseKnown says
	// whether this deployment can state it at all. A deployment whose data lives inside a database server
	// it does not own answers 0 and false rather than a number that means something else on each backend.
	DatabaseBytes int64 `json:"databaseBytes"`
	DatabaseKnown bool  `json:"databaseKnown"`
}

type adminStatusLive struct {
	Listeners             int `json:"listeners"`
	AdministratorSessions int `json:"administratorSessions"`
	DirwatchersRunning    int `json:"dirwatchersRunning"`
	HeldCalls             int `json:"heldCalls"`

	AudioConversion bool `json:"audioConversion"`
	AudioProbe      bool `json:"audioProbe"`
}

type adminStatusActivity struct {
	WindowStart time.Time `json:"windowStart"`
	WindowEnd   time.Time `json:"windowEnd"`

	CallsToday    uint64 `json:"callsToday"`
	CallsLastHour uint64 `json:"callsLastHour"`

	// LastCallAt is absent when the history has no calls in it, and not a zero instant the page would
	// have to recognize. A page that has to notice a date is the year 1 is a page that will eventually
	// stop checking.
	LastCallAt *time.Time `json:"lastCallAt,omitempty"`

	Hours   []adminStatusHour      `json:"hours"`
	Busiest []adminStatusTalkgroup `json:"busiest"`
	Recent  []adminStatusCall      `json:"recent"`
}

type adminStatusHour struct {
	// Start is the instant the whole hour begins. The count is what falls in that hour and not in the
	// next, which is what lets the page draw the bar without deciding what an hour is.
	Start time.Time `json:"start"`
	Count uint64    `json:"count"`
}

type adminStatusTalkgroup struct {
	SystemRef    uint   `json:"systemRef"`
	TalkgroupRef uint   `json:"talkgroupRef"`
	Name         string `json:"name"`
	Label        string `json:"label"`
	Count        uint64 `json:"count"`
}

type adminStatusCall struct {
	ID           uint64    `json:"callId"`
	SystemRef    uint      `json:"systemRef"`
	TalkgroupRef uint      `json:"talkgroupRef"`
	Timestamp    time.Time `json:"timestamp"`

	// AudioLengthSeconds is how long the recording lasts. The recording itself is not here: this is read
	// every few seconds, and audio is the heaviest thing the server keeps.
	AudioLengthSeconds float64 `json:"audioLengthSeconds"`
}

type adminStatusLogs struct {
	WindowStart time.Time `json:"windowStart"`

	Errors   uint64 `json:"errors"`
	Warnings uint64 `json:"warnings"`

	Sources []adminStatusLogSource `json:"sources"`
}

type adminStatusLogSource struct {
	Source   string `json:"source"`
	Errors   uint64 `json:"errors"`
	Warnings uint64 `json:"warnings"`
}

// AdminStatus turns the status read into the published payload, adding the three facts about the
// deployment the administrator surface already holds.
//
// Every list is answered as a list even when nothing was recorded. A page that has to check each of them
// for being absent before it can count it is a page that will eventually stop checking, and an empty list
// is the answer "nothing" for a list.
func AdminStatus(status application.RuntimeStatus, version string, docker bool, baseDir string) AdminStatusPayload {
	payload := AdminStatusPayload{
		Server: adminStatusServer{
			Version: version,
			Docker:  docker,

			StartedAt:     status.Server.StartedAt,
			Now:           status.Server.Now,
			UptimeSeconds: int64(status.Server.Uptime / time.Second),

			BaseDir: baseDir,

			DatabaseBytes: status.Server.DatabaseBytes,
			DatabaseKnown: status.Server.DatabaseKnown,
		},
		Live: adminStatusLive{
			Listeners:             status.Live.Listeners,
			AdministratorSessions: status.Live.AdministratorSessions,
			DirwatchersRunning:    status.Live.DirwatchersRunning,
			HeldCalls:             status.Live.HeldCalls,

			AudioConversion: status.Live.AudioConversion,
			AudioProbe:      status.Live.AudioProbe,
		},
		Activity: adminStatusActivity{
			WindowStart:   status.Activity.WindowStart,
			WindowEnd:     status.Activity.WindowEnd,
			CallsToday:    status.Activity.CallsToday,
			CallsLastHour: status.Activity.CallsLastHour,

			Hours:   []adminStatusHour{},
			Busiest: []adminStatusTalkgroup{},
			Recent:  []adminStatusCall{},
		},
		Logs: adminStatusLogs{
			WindowStart: status.Logs.WindowStart,
			Errors:      status.Logs.Errors,
			Warnings:    status.Logs.Warnings,
			Sources:     []adminStatusLogSource{},
		},
	}

	if !status.Activity.LastCallAt.IsZero() {
		last := status.Activity.LastCallAt

		payload.Activity.LastCallAt = &last
	}

	for _, hour := range status.Activity.Hours {
		payload.Activity.Hours = append(payload.Activity.Hours, adminStatusHour{Start: hour.Hour, Count: hour.Count})
	}

	for _, talkgroup := range status.Activity.Busiest {
		payload.Activity.Busiest = append(payload.Activity.Busiest, adminStatusTalkgroup{
			SystemRef:    talkgroup.SystemRef,
			TalkgroupRef: talkgroup.TalkgroupRef,
			Name:         talkgroup.Name,
			Label:        talkgroup.Label,
			Count:        talkgroup.Count,
		})
	}

	for _, call := range status.Activity.Recent {
		payload.Activity.Recent = append(payload.Activity.Recent, adminStatusCall{
			ID:                 call.ID,
			SystemRef:          call.SystemRef,
			TalkgroupRef:       call.TalkgroupRef,
			Timestamp:          call.Timestamp,
			AudioLengthSeconds: call.AudioLength.Seconds(),
		})
	}

	for _, source := range status.Logs.Sources {
		payload.Logs.Sources = append(payload.Logs.Sources, adminStatusLogSource{
			Source:   string(source.Source),
			Errors:   source.Errors,
			Warnings: source.Warnings,
		})
	}

	return payload
}
