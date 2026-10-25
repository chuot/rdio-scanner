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

// AdminStatusPayload is the administrator's status view: it
// names the deployment facts beside the measured snapshot.
// see the administrator-status-payload-shape maintainer note.
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

	// UptimeSeconds is whole seconds, 0 with no start.
	// see the administrator-status-payload-shape maintainer note.
	UptimeSeconds int64 `json:"uptimeSeconds"`

	BaseDir string `json:"baseDir"`

	// DatabaseBytes is the file size of this server's database,
	// and DatabaseKnown says whether it can be stated: 0 and
	// false when the data lives in a database server not ours.
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

	// LastCallAt is absent when the history has no calls.
	// see the administrator-status-payload-shape maintainer note.
	LastCallAt *time.Time `json:"lastCallAt,omitempty"`

	Hours   []adminStatusHour      `json:"hours"`
	Busiest []adminStatusTalkgroup `json:"busiest"`
	Recent  []adminStatusCall      `json:"recent"`
}

type adminStatusHour struct {
	// Start is the instant the whole hour begins, and Count
	// holds what falls in that hour, not the next.
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

	// AudioLengthSeconds is how long the recording lasts. The
	// payload omits the audio, the heaviest thing kept.
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

// AdminStatus turns the status read into the published payload,
// adding the three deployment facts the admin surface holds.
// Every list is answered as a list even when nothing was
// recorded, so a page can count it without guarding first.
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
