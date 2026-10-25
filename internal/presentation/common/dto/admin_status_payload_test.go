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
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// AdminStatusPayload tests: the JSON names are the contract,
// not the read model's own. see the call-payload-field-order maintainer note.

var statusNow = time.Date(2026, 10, 17, 8, 45, 11, 0, time.UTC)

func aStatusRead() application.RuntimeStatus {
	return application.RuntimeStatus{
		Server: application.RuntimeServerStatus{
			StartedAt: statusNow.Add(-2*time.Hour - 14*time.Minute - 8*time.Second),
			Now:       statusNow,
			Uptime:    2*time.Hour + 14*time.Minute + 8*time.Second,

			DatabaseBytes: 4_831_838_208,
			DatabaseKnown: true,
		},
		Live: application.RuntimeLiveStatus{
			Listeners:             3,
			AdministratorSessions: 1,
			DirwatchersRunning:    2,
			HeldCalls:             2,

			AudioConversion: true,
			AudioProbe:      true,
		},
		Activity: application.RuntimeActivityStatus{
			WindowStart:   statusNow.Add(-24 * time.Hour).Truncate(time.Hour),
			WindowEnd:     statusNow.Truncate(time.Hour).Add(time.Hour),
			CallsToday:    214,
			CallsLastHour: 37,
			LastCallAt:    statusNow.Add(-16*time.Minute - 58*time.Second),

			Hours: []ports.CallHourCount{
				{Hour: statusNow.Truncate(time.Hour).Add(-2 * time.Hour), Count: 9},
			},
			Busiest: []ports.CallTalkgroupCount{
				{SystemRef: 1, TalkgroupRef: 101, Name: "Dispatch", Label: "T-101", Count: 41},
			},
			Recent: []ports.CallSummary{
				{ID: 91827, SystemRef: 1, TalkgroupRef: 101, Timestamp: statusNow.Add(-16*time.Minute - 58*time.Second), AudioLength: 12400 * time.Millisecond},
			},
		},
		Logs: application.RuntimeLogStatus{
			WindowStart: statusNow.Add(-24 * time.Hour),
			Errors:      3,
			Warnings:    11,
			Sources: []application.RuntimeLogSourceTally{
				{Source: domain.LogSourceDirwatch, Errors: 2, Warnings: 5},
			},
		},
	}
}

// TestAdminStatusPayloadNamesTheDeploymentBesideTheSnapshot proves
// the version, container flag, and served folder are named
// beside the snapshot, as the configuration read names them.
func TestAdminStatusPayloadNamesTheDeploymentBesideTheSnapshot(t *testing.T) {
	body := statusBody(t, AdminStatus(aStatusRead(), "8.0.0", true, "/data/rdio"))

	server := field(t, body, "server")

	if server["version"] != "8.0.0" {
		t.Errorf("the server block names %v as the version, want the version the server reports", server["version"])
	}

	if server["docker"] != true {
		t.Errorf("the server block reports docker=%v, want true: the same answer the configuration read gives", server["docker"])
	}

	if server["baseDir"] != "/data/rdio" {
		t.Errorf("the server block names %v as the folder it serves, want the folder the operator named", server["baseDir"])
	}

	// UptimeSeconds is asserted as whole seconds, the page
	// choosing the words. see the administrator-status-payload-shape maintainer note.
	if server["uptimeSeconds"] != float64(8048) {
		t.Errorf("the server block reports uptimeSeconds=%v, want 8048: 2h 14m 8s, whole seconds", server["uptimeSeconds"])
	}

	if server["startedAt"] != "2026-10-17T06:31:03Z" {
		t.Errorf("the server block names %v as the start, want the instant the start sequence ran", server["startedAt"])
	}

	if server["now"] != "2026-10-17T08:45:11Z" {
		t.Errorf("the server block names %v as now, want the instant the read ran", server["now"])
	}

	if server["databaseBytes"] != float64(4_831_838_208) || server["databaseKnown"] != true {
		t.Errorf("the server block reports %v bytes known=%v, want 4831838208 known=true",
			server["databaseBytes"], server["databaseKnown"])
	}
}

// TestAdminStatusPayloadReportsTheLiveStateUnderTheNamesThePageReads proves
// the live state under the contract names the page reads.
func TestAdminStatusPayloadReportsTheLiveStateUnderTheNamesThePageReads(t *testing.T) {
	body := statusBody(t, AdminStatus(aStatusRead(), "8.0.0", false, ""))

	live := field(t, body, "live")

	want := map[string]any{
		"listeners":             float64(3),
		"administratorSessions": float64(1),
		"dirwatchersRunning":    float64(2),
		"heldCalls":             float64(2),
		"audioConversion":       true,
		"audioProbe":            true,
	}

	for name, value := range want {
		if live[name] != value {
			t.Errorf("live.%s = %v, want %v", name, live[name], value)
		}
	}

	if len(live) != len(want) {
		t.Errorf("the live block carries %d fields, want the %d the contract names: %v", len(live), len(want), live)
	}
}

// TestAdminStatusPayloadAnswersEmptyListsRatherThanNullsWhenNothingWasRecorded proves
// empty lists answer, never nulls, when nothing was recorded.
func TestAdminStatusPayloadAnswersEmptyListsRatherThanNullsWhenNothingWasRecorded(t *testing.T) {
	status := aStatusRead()
	status.Activity.Hours = nil
	status.Activity.Busiest = nil
	status.Activity.Recent = nil
	status.Activity.LastCallAt = time.Time{}
	status.Logs.Sources = nil

	body := statusBody(t, AdminStatus(status, "8.0.0", false, ""))

	activity := field(t, body, "activity")

	for _, name := range []string{"hours", "busiest", "recent"} {
		list, ok := activity[name].([]any)
		if !ok {
			t.Errorf("activity.%s is %v, want an empty list", name, activity[name])

			continue
		}

		if len(list) != 0 {
			t.Errorf("activity.%s carries %d entries, want none", name, len(list))
		}
	}

	if _, ok := activity["lastCallAt"]; ok {
		t.Error("the activity block names a last call for a history with no calls in it, want the field absent")
	}

	logs := field(t, body, "logs")

	if sources, ok := logs["sources"].([]any); !ok || len(sources) != 0 {
		t.Errorf("logs.sources is %v, want an empty list", logs["sources"])
	}
}

// TestAdminStatusPayloadCarriesNoAudioAndNoConfiguration proves the
// payload omits the recording, the heaviest thing the
// server keeps, and the configuration the page already
func TestAdminStatusPayloadCarriesNoAudioAndNoConfiguration(t *testing.T) {
	body := statusBody(t, AdminStatus(aStatusRead(), "8.0.0", false, ""))

	raw := string(mustMarshal(t, AdminStatus(aStatusRead(), "8.0.0", false, "")))

	// Log source names are not configuration sections, and
	// dirwatch is both: a configuration block is the page's own.
	for _, forbidden := range []string{"\"audio\"", "\"config\"", "\"options\"", "\"systems\"", "\"alerts\"", "\"apikeys\"", "\"downstreams\""} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("the status answer carries %s, which does not belong in a status read", forbidden)
		}
	}

	recent := field(t, body, "activity")["recent"].([]any)

	if len(recent) != 1 {
		t.Fatalf("the activity block carries %d recent calls, want the 1 the read answered", len(recent))
	}

	call := recent[0].(map[string]any)

	// audioLengthSeconds is asserted as a number of seconds,
	// not a duration string. see the administrator-status-payload-shape maintainer note.
	if call["audioLengthSeconds"] != 12.4 {
		t.Errorf("the recent row carries audioLengthSeconds=%v, want 12.4", call["audioLengthSeconds"])
	}

	if call["callId"] != float64(91827) {
		t.Errorf("the recent row names %v as the call, want 91827", call["callId"])
	}
}

// TestAdminStatusPayloadNamesTheHourAndTheTalkgroupTheWayTheChartAndTheCardReadThem.
func TestAdminStatusPayloadNamesTheHourAndTheTalkgroupTheWayTheChartAndTheCardReadThem(t *testing.T) {
	body := statusBody(t, AdminStatus(aStatusRead(), "8.0.0", false, ""))

	hours := field(t, body, "activity")["hours"].([]any)

	if len(hours) != 1 {
		t.Fatalf("the activity block carries %d hour buckets, want the 1 the read answered", len(hours))
	}

	hour := hours[0].(map[string]any)

	if hour["start"] != "2026-10-17T06:00:00Z" {
		t.Errorf("the hour bucket names %v as its start, want the instant the whole hour begins", hour["start"])
	}

	if hour["count"] != float64(9) {
		t.Errorf("the hour bucket carries count=%v, want 9", hour["count"])
	}

	busiest := field(t, body, "activity")["busiest"].([]any)

	if len(busiest) != 1 {
		t.Fatalf("the activity block names %d busiest talkgroups, want the 1 the read answered", len(busiest))
	}

	talkgroup := busiest[0].(map[string]any)

	if talkgroup["name"] != "Dispatch" || talkgroup["label"] != "T-101" {
		t.Errorf("the busiest row names %v / %v, want the talkgroup's own name and label: the operator reads a "+
			"talkgroup, not a pair of references", talkgroup["name"], talkgroup["label"])
	}

	sources := field(t, body, "logs")["sources"].([]any)

	source := sources[0].(map[string]any)

	if source["source"] != "dirwatch" || source["errors"] != float64(2) || source["warnings"] != float64(5) {
		t.Errorf("the log card names %v, want dirwatch with 2 errors and 5 warnings", source)
	}
}

func statusBody(t *testing.T, payload AdminStatusPayload) map[string]any {
	t.Helper()

	var body map[string]any

	if err := json.Unmarshal(mustMarshal(t, payload), &body); err != nil {
		t.Fatalf("the status payload is not the JSON the endpoint answers with: %v", err)
	}

	return body
}

func mustMarshal(t *testing.T, payload any) []byte {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("the status payload cannot be written: %v", err)
	}

	return body
}

func field(t *testing.T, body map[string]any, name string) map[string]any {
	t.Helper()

	block, ok := body[name].(map[string]any)
	if !ok {
		t.Fatalf("the status answer carries no %s block: %v", name, body[name])
	}

	return block
}
