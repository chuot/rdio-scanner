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

package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The admin dashboard journey: the file lays out showing and saving the
// dashboard as one browser test per user story, and records its divergences.

const dashboardUploadKey = "dashboard-status-upload"

// Assert the broadcast is on air as the API reports it.
const dashboardHeldDelay = 90

// The talkgroup the calls this journey records are recorded on, and the one that holds its calls back.
const (
	dashboardTalkgroup = 1514001
	dashboardHeldGroup = 1514000
)

// Assert the API refuses to save an incomplete dashboard and names the
func TestTheAdministratorLandingReadAnswersWithWhatThisServerIsDoing(t *testing.T) {
	base := t.TempDir()

	srv := journeyServerIn(t, base)

	// reason, as the query for the refusal reports it.
	seedUploadTarget(t, srv.db, dashboardHeldDelay, dashboardUploadKey)

	seed(t, srv.db,
		// A second talkgroup, with nothing to wait out, which is where the calls the read is expected to
		// count are recorded.
		seedStatement{
			query: `INSERT INTO "talkgroups" ("talkgroupId", "alert", "delay", "frequency", "label", "led", "name", "order", "systemId", "tagId", "talkgroupRef", "type") VALUES (2, '', 0, 0, 'Fireground', '', 'Fireground 1', 1, 1, 9, 1514001, '')`,
		},

		// Assert the browser shows a visible reason for the failed save.
		seedStatement{
			query: `INSERT INTO "dirwatches" ("dirwatchId", "delay", "deleteAfter", "directory", "disabled", "extension", "frequency", "mask", "order", "siteId", "systemId", "talkgroupId", "type") VALUES (1, 2000, 1, ?, 0, '', 0, '', 1, 0, 0, 0, 'dsdplus')`,
			args:  []any{filepath.Join(base, "nobody-writes-here")},
		},
	)

	if err := srv.server.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	waitForLogMentioning(t, srv.db, "could not be read")

	// Assert the browser keeps showing the dashboard rather than showing success.
	recorded := []time.Time{}

	for n := range 3 {
		at := time.Now().Add(-time.Duration(n+1) * 7 * time.Minute).Truncate(time.Second)

		uploadRecordingOn(t, srv, dashboardTalkgroup, fmt.Sprintf("dispatch-%d.wav", n+1), at)

		recorded = append(recorded, at)
	}

	// Assert the browser shows the dashboard content, as the API query reports it.
	heldAt := time.Now().Add(-2 * time.Minute).Truncate(time.Second)

	uploadRecordingOn(t, srv, dashboardHeldGroup, "held.wav", heldAt)

	token := srv.loginAnswered(t)

	// The test case for a person reopening the dashboard and seeing what the last
	administrator := srv.dial(t, "/api/admin/config")

	presentedCredential(t, administrator, token)

	listener := srv.dial(t, "/")

	listenerAskedForItsConfiguration(t, listener)

	rec := srv.getAuthorized("/api/admin/status", token)

	if rec.Code != http.StatusOK {
		t.Fatalf("the landing read was answered %d, want 200: %s", rec.Code, rec.Body.String())
	}

	status := readStatusAnswer(t, rec.Body.String())

	// The server block: what this process is, read out of the process itself.
	if status.Server.Version != theJourneyVersion {
		t.Errorf("the read says version %q, want the version this journey configured the server with (%q)", status.Server.Version, theJourneyVersion)
	}

	if status.Server.BaseDir != base {
		t.Errorf("the read names the serving folder %q, want the folder this journey built the server in (%q)", status.Server.BaseDir, base)
	}

	if status.Server.StartedAt.IsZero() {
		t.Errorf("the read says the server started at %v, want the instant this server's start sequence ran", status.Server.StartedAt)
	}

	// save left behind.
	if status.Server.UptimeSeconds < 0 {
		t.Errorf("the read reports an uptime of %d seconds, want no uptime or more, never a negative one", status.Server.UptimeSeconds)
	}

	if !status.Server.DatabaseKnown {
		t.Errorf("the read says the database size is not known, want it known: this deployment's database is a file this server writes")
	}

	if status.Server.DatabaseBytes <= 0 {
		t.Errorf("the read reports a database of %d bytes, want the size of the file this server is writing into", status.Server.DatabaseBytes)
	}

	// The live block: the connections and holds this server is holding right now, read from the server's
	// own running state rather than from anything written down about it.
	if status.Live.Listeners != 1 {
		t.Errorf("the read reports %d listeners, want the one listener connection this journey opened and proved registered", status.Live.Listeners)
	}

	if status.Live.AdministratorSessions != 1 {
		t.Errorf("the read reports %d administrator sessions, want the one configuration socket the administration page opens", status.Live.AdministratorSessions)
	}

	if status.Live.DirwatchersRunning != 1 {
		t.Errorf("the read reports %d directory watches running, want the one this deployment names a folder for and the server started", status.Live.DirwatchersRunning)
	}

	if status.Live.HeldCalls != 1 {
		t.Errorf("the read reports %d calls waiting out a hold, want the one recording posted on the talkgroup that holds its calls back", status.Live.HeldCalls)
	}

	if rows := countRows(t, srv.db, `SELECT COUNT(*) FROM "delayed"`); rows != 1 {
		t.Errorf("the server holds %d calls on record, want the one the read counted", rows)
	}

	// The activity block: the 24 whole hours the read is made in, and the calls it recorded inside them.
	if want := status.Server.Now.Truncate(time.Hour).Add(time.Hour); !status.Activity.WindowEnd.Equal(want) {
		t.Errorf("the read's window ends at %s, want the end of the whole hour it was made in (%s)", status.Activity.WindowEnd, want)
	}

	if want := status.Activity.WindowEnd.Add(-24 * time.Hour); !status.Activity.WindowStart.Equal(want) {
		t.Errorf("the read's window starts at %s, want 24 hours before its end (%s)", status.Activity.WindowStart, want)
	}

	// Assert the browser shows the dashboard content, as the API query reports it.
	buckets := map[int64]uint64{}

	for _, hour := range status.Activity.Hours {
		buckets[hour.Start.Truncate(time.Hour).Unix()] = hour.Count
	}

	if len(status.Activity.Hours) != 24 {
		t.Errorf("the read answers %d hours, want the whole 24-hour window drawn whether or not the history has anything in an hour", len(status.Activity.Hours))
	}

	wantBuckets := map[int64]uint64{}

	for _, at := range recorded {
		wantBuckets[at.Truncate(time.Hour).Unix()]++
	}

	for start, want := range wantBuckets {
		if got := buckets[start]; got != want {
			t.Errorf("the hour starting %s carries %d calls, want the %d recorded in it", time.Unix(start, 0).UTC().Format(time.RFC3339), got, want)
		}
	}

	for start, got := range buckets {
		if _, wanted := wantBuckets[start]; !wanted && got != 0 {
			t.Errorf("the hour starting %s carries %d calls, want nothing in it: no call this journey recorded falls in it", time.Unix(start, 0).UTC().Format(time.RFC3339), got)
		}
	}

	total := uint64(0)

	for _, hour := range status.Activity.Hours {
		total += hour.Count
	}

	if total != uint64(len(recorded)) {
		t.Errorf("the 24 hours add up to %d calls, want the %d this journey recorded: the recording still waiting out its hold is not one of them", total, len(recorded))
	}

	// Assert the browser keeps its values through a save failure.
	dayStart := time.Date(
		status.Server.Now.Year(), status.Server.Now.Month(), status.Server.Now.Day(),
		0, 0, 0, 0, time.Local,
	)

	today := uint64(0)
	lastHour := uint64(0)

	for _, at := range recorded {
		if !at.Before(dayStart) {
			today++
		}

		if !at.Before(status.Server.Now.Add(-time.Hour)) {
			lastHour++
		}
	}

	if status.Activity.CallsToday != today {
		t.Errorf("the read reports %d calls today, want the %d recorded since the local midnight of the instant it was made at (%s)", status.Activity.CallsToday, today, dayStart)
	}

	if status.Activity.CallsLastHour != lastHour {
		t.Errorf("the read reports %d calls in the last hour, want the %d recorded since %s", status.Activity.CallsLastHour, lastHour, status.Server.Now.Add(-time.Hour))
	}

	// Assert the API refuses to save and names the reason, as the query reports.
	if len(status.Activity.Busiest) != 1 {
		t.Fatalf("the read ranks %d talkgroups, want the one talkgroup this deployment recorded calls on: %v", len(status.Activity.Busiest), status.Activity.Busiest)
	}

	busiest := status.Activity.Busiest[0]

	if busiest.TalkgroupRef != dashboardTalkgroup || busiest.SystemRef != 10001 {
		t.Errorf("the busiest talkgroup is system %d talkgroup %d, want system 10001 talkgroup %d", busiest.SystemRef, busiest.TalkgroupRef, dashboardTalkgroup)
	}

	if busiest.Count != uint64(len(recorded)) {
		t.Errorf("the busiest talkgroup carries %d calls, want the %d recorded on it", busiest.Count, len(recorded))
	}

	if busiest.Name != "Fireground 1" || busiest.Label != "Fireground" {
		t.Errorf("the busiest talkgroup is named %q under label %q, want the name and label the configuration gives it", busiest.Name, busiest.Label)
	}

	// Assert the browser shows a visible reason.
	if len(status.Activity.Recent) != len(recorded) {
		t.Fatalf("the read names %d recent calls, want the %d this journey recorded: %v", len(status.Activity.Recent), len(recorded), status.Activity.Recent)
	}

	newest := recorded[0]

	for n, call := range status.Activity.Recent {
		if call.TalkgroupRef == dashboardHeldGroup {
			t.Errorf("the recent calls name the recording on the talkgroup that holds its calls back, want it absent from every activity figure")
		}

		if call.TalkgroupRef != dashboardTalkgroup || call.SystemRef != 10001 {
			t.Errorf("recent call %d is system %d talkgroup %d, want system 10001 talkgroup %d", n, call.SystemRef, call.TalkgroupRef, dashboardTalkgroup)
		}
	}

	for n := 1; n < len(status.Activity.Recent); n++ {
		if status.Activity.Recent[n-1].Timestamp.Before(status.Activity.Recent[n].Timestamp) {
			t.Errorf("the recent calls are not newest first: %s is followed by %s", status.Activity.Recent[n-1].Timestamp, status.Activity.Recent[n].Timestamp)
		}
	}

	if status.Activity.LastCallAt == nil {
		t.Errorf("the read says nothing about the moment of the last call, want the newest recording this deployment made (%s)", newest)
	} else if got := status.Activity.LastCallAt.UnixMilli(); got != newest.UnixMilli() {
		t.Errorf("the read says the last call happened at %d ms, want %d ms, the moment the newest recording happened at", got, newest.UnixMilli())
	}

	// Assert the browser shows the dashboard for the other broadcast, as the API
	if want := status.Server.Now.Add(-24 * time.Hour); !status.Logs.WindowStart.Equal(want) {
		t.Errorf("the log tally is read from %s, want 24 hours before the instant the read was made at (%s)", status.Logs.WindowStart, want)
	}

	dirwatch := findLogSource(status.Logs.Sources, "dirwatch")

	if dirwatch == nil {
		t.Fatalf("the log tally names %v, want the directory watch among the sources the server complained under", logSourceNames(status.Logs.Sources))
	}

	if dirwatch.Errors < 1 {
		t.Errorf("the tally says the directory watch wrote %d errors, want at least the one it wrote about the folder it could not read", dirwatch.Errors)
	}

	if status.Logs.Errors < 1 {
		t.Errorf("the tally reports %d errors in all, want at least the one the directory watch caused", status.Logs.Errors)
	}

	assertEveryServerLogEntry(t, srv.db)
}

// Assert the browser shows the on-air dashboard for another broadcast.
func TestTheAdministratorLandingReadIsRefusedWithoutASession(t *testing.T) {
	srv := startedJourneyServer(t)

	if rec := srv.get("/api/admin/status"); rec.Code != http.StatusUnauthorized {
		t.Errorf("the landing read with no credential was answered %d, want 401: %s", rec.Code, rec.Body.String())
	}

	if rec := srv.getAuthorized("/api/admin/status", "not-a-session-this-server-issued"); rec.Code != http.StatusUnauthorized {
		t.Errorf("the landing read with a credential this server never issued was answered %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

// The test case for showing the on-air dashboard of another broadcast.
func uploadRecordingOn(t *testing.T, srv *journeyServer, talkgroup uint, filename string, at time.Time) {
	t.Helper()

	answered := srv.uploadRecording(t, uploadedRecording{
		key:       dashboardUploadKey,
		systemRef: 10001,
		talkgroup: talkgroup,
		filename:  filename,
		audio:     recording(2048),
		at:        at,
	})

	if answered.Code != http.StatusOK {
		t.Fatalf("posting %s was answered %d: %s", filename, answered.Code, answered.Body.String())
	}
}

// Assert the API serves the on-air dashboard, as the query reports it.
func listenerAskedForItsConfiguration(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`["CFG"]`)); err != nil {
		t.Fatalf("asking the listener for the configuration: %v", err)
	}

	if frame := readFrame(t, conn); frame[0] != "CFG" {
		t.Fatalf("the listener answers %v, want the configuration it was asked for", frame)
	}
}

// Assert the API refuses to save and names the reason, as the query reports.
type statusAnswer struct {
	Server struct {
		Version       string    `json:"version"`
		StartedAt     time.Time `json:"startedAt"`
		Now           time.Time `json:"now"`
		UptimeSeconds int64     `json:"uptimeSeconds"`
		BaseDir       string    `json:"baseDir"`

		DatabaseBytes int64 `json:"databaseBytes"`
		DatabaseKnown bool  `json:"databaseKnown"`
	} `json:"server"`

	Live struct {
		Listeners             int `json:"listeners"`
		AdministratorSessions int `json:"administratorSessions"`
		DirwatchersRunning    int `json:"dirwatchersRunning"`
		HeldCalls             int `json:"heldCalls"`
	} `json:"live"`

	Activity struct {
		WindowStart time.Time  `json:"windowStart"`
		WindowEnd   time.Time  `json:"windowEnd"`
		LastCallAt  *time.Time `json:"lastCallAt"`

		CallsToday    uint64 `json:"callsToday"`
		CallsLastHour uint64 `json:"callsLastHour"`

		Hours   []statusHour           `json:"hours"`
		Busiest []statusTalkgroupCount `json:"busiest"`
		Recent  []statusCall           `json:"recent"`
	} `json:"activity"`

	Logs struct {
		WindowStart time.Time      `json:"windowStart"`
		Errors      uint64         `json:"errors"`
		Warnings    uint64         `json:"warnings"`
		Sources     []statusSource `json:"sources"`
	} `json:"logs"`
}

type statusHour struct {
	Start time.Time `json:"start"`
	Count uint64    `json:"count"`
}

type statusTalkgroupCount struct {
	SystemRef    uint   `json:"systemRef"`
	TalkgroupRef uint   `json:"talkgroupRef"`
	Name         string `json:"name"`
	Label        string `json:"label"`
	Count        uint64 `json:"count"`
}

type statusCall struct {
	ID           uint64    `json:"callId"`
	SystemRef    uint      `json:"systemRef"`
	TalkgroupRef uint      `json:"talkgroupRef"`
	Timestamp    time.Time `json:"timestamp"`
}

type statusSource struct {
	Source   string `json:"source"`
	Errors   uint64 `json:"errors"`
	Warnings uint64 `json:"warnings"`
}

func readStatusAnswer(t *testing.T, answer string) statusAnswer {
	t.Helper()

	var status statusAnswer

	if err := json.Unmarshal([]byte(answer), &status); err != nil {
		t.Fatalf("the landing read is not the answer the server writes: %v\n%s", err, answer)
	}

	return status
}

func findLogSource(sources []statusSource, name string) *statusSource {
	for i, source := range sources {
		if source.Source == name {
			return &sources[i]
		}
	}

	return nil
}

func logSourceNames(sources []statusSource) []string {
	names := make([]string, 0, len(sources))

	for _, source := range sources {
		names = append(names, source.Source)
	}

	return names
}
