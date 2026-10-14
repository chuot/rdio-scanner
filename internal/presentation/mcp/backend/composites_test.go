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

package backend

import (
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

// The composites: questions an operator asks that no single entry point can answer. Each one
// composes several reads into one answer, and says in its own description which entry points it
// composed, so an assistant reading the catalog knows what a call is going to cost and an operator
// reading the answer knows where each part came from.

func TestAnalyzeHealthAssemblesWhatNeedsADecision(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	day := fixtureTime.Format("2006-01-02")

	answered := call(t, s, "analyze_health", map[string]any{"date": day})

	var report healthReportShape
	decode(t, answered, &report)

	if report.Version != testVersion {
		t.Fatalf("the report names version %q, want %q", report.Version, testVersion)
	}

	// The window the report reviewed, named in the answer so an operator knows what it looked at.
	if report.Date != day {
		t.Fatalf("the report reviewed %q, want the day it was asked for, %q", report.Date, day)
	}

	// The severities counted per subsystem: the log view's own per-subsystem counts, one search per
	// severity, assembled side by side.
	if count := sourceCountOf(report.Errors, "ffmpeg"); count != 1 {
		t.Fatalf("the report counts %d error lines from the conversion subsystem, want the one it wrote: %+v",
			count, report.Errors)
	}
	if count := sourceCountOf(report.Warnings, "access"); count != 1 {
		t.Fatalf("the report counts %d warning lines from the access subsystem, want the one it wrote: %+v",
			count, report.Warnings)
	}

	// The restart markers: the server's own startup line, counted and shown, because a server that
	// came up twice in a day has a reason an operator has to find.
	if len(report.Restarts) != 1 {
		t.Fatalf("the report names %d restart markers, want the one the fixture's log carries: %+v",
			len(report.Restarts), report.Restarts)
	}
	if report.Restarts[0].Message != "server started" {
		t.Fatalf("the restart marker is %q, want the server's own startup line", report.Restarts[0].Message)
	}

	// The plumbing that is switched off, the retention horizon, the listener ceiling, and the
	// server's own notice that the default credential is still in place.
	if report.DisabledDirwatches != 1 || report.DisabledDownstreams != 1 {
		t.Fatalf("the report names %d disabled watchers and %d disabled forwarders, want one of each: %+v",
			report.DisabledDirwatches, report.DisabledDownstreams, report)
	}
	if report.PruneDays != 7 || report.MaxClients != 200 {
		t.Fatalf("the report names prune %d and ceiling %d, want the fixture's 7 and 200",
			report.PruneDays, report.MaxClients)
	}

	// The findings are the assembled answer: each part of the deployment that needs a decision,
	// named once, with the severity the operator reads it under.
	subjects := map[string]string{}
	for _, finding := range report.Findings {
		subjects[finding.Subject] = finding.Severity
	}

	for _, want := range []struct {
		subject  string
		severity string
	}{
		{"ffmpeg", "error"},
		{"dirwatch", "warn"},
		{"password", "warn"},
	} {
		severity, ok := subjects[want.subject]
		if !ok {
			t.Fatalf("the report finds nothing about %s: %+v", want.subject, report.Findings)
		}
		if severity != want.severity {
			t.Fatalf("the report finds %s as %q, want %q", want.subject, severity, want.severity)
		}
	}
}

func TestDiagnoseFeedProblemAssemblesTheAnswersAboutATalkgroup(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	answered := call(t, s, "diagnose_feed_problem", map[string]any{
		"system":    systemOneRef,
		"talkgroup": 1514000,
		"date":      fixtureTime.Format("2006-01-02"),
	})

	var diagnosis feedDiagnosisShape
	decode(t, answered, &diagnosis)

	// The last activity on the talkgroup, newest first: the answer to "it has gone quiet".
	if diagnosis.LastCall == nil {
		t.Fatalf("the diagnosis names no last call, and the fixture recorded two on that talkgroup: %+v", diagnosis)
	}
	if diagnosis.LastCall.ID != 2 {
		t.Fatalf("the diagnosis names call %d as the last activity, want the newer of the two: %+v",
			diagnosis.LastCall.ID, diagnosis.LastCall)
	}

	// The log lines naming that talkgroup in the window.
	found := false
	for _, row := range diagnosis.LogLines {
		if row.Source == "livefeed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the diagnosis carries no line from the feed subsystem about that talkgroup: %+v", diagnosis.LogLines)
	}

	// The system's own settings that decide how a call on it is handled.
	if diagnosis.Settings == nil {
		t.Fatalf("the diagnosis names no settings for the system, and an administrator asked it: %+v", diagnosis)
	}
	if !diagnosis.Settings.AutoPopulate {
		t.Fatalf("the fixture's system auto-populates, the diagnosis says it does not: %+v", diagnosis.Settings)
	}

	// The watched directory feeding that system, named as disabled, and the downstream forwarding
	// its calls.
	if len(diagnosis.WatchedDirectories) != 1 {
		t.Fatalf("the diagnosis names %d watched directories feeding that system, want the one the fixture points at it: %+v",
			len(diagnosis.WatchedDirectories), diagnosis.WatchedDirectories)
	}
	if !diagnosis.WatchedDirectories[0].Disabled {
		t.Fatalf("the watched directory feeding that system is not named as disabled: %+v",
			diagnosis.WatchedDirectories[0])
	}
	if len(diagnosis.Forwards) != 1 {
		t.Fatalf("the diagnosis names %d downstreams forwarding that system, want the one the fixture scopes to it: %+v",
			len(diagnosis.Forwards), diagnosis.Forwards)
	}
}

// The read-only tier's form of the same question: the history it may see, and the parts of the
// deployment it cannot see named as unavailable rather than silently dropped. A listener's access
// code does not read the server's log view or a system's settings, and an answer that left those
// out without saying so would read like a healthy talkgroup.
func TestDiagnoseFeedProblemForAnAccessCodeCallerNamesWhatItCannotSee(t *testing.T) {
	s := newCallerSession(t, readScoped())

	answered := call(t, s, "diagnose_feed_problem", map[string]any{
		"system":    systemOneRef,
		"talkgroup": 1514000,
		"date":      fixtureTime.Format("2006-01-02"),
	})

	var diagnosis feedDiagnosisShape
	decode(t, answered, &diagnosis)

	if diagnosis.LastCall == nil || diagnosis.LastCall.ID != 2 {
		t.Fatalf("the scoped diagnosis names no last call for a talkgroup its code grants: %+v", diagnosis)
	}

	if len(diagnosis.LogLines) != 0 {
		t.Fatalf("the scoped diagnosis answered the server's log view: %+v", diagnosis.LogLines)
	}
	if diagnosis.Settings != nil || len(diagnosis.WatchedDirectories) != 0 || len(diagnosis.Forwards) != 0 {
		t.Fatalf("the scoped diagnosis answered the configuration: %+v", diagnosis)
	}

	unavailable := strings.Join(diagnosis.Unavailable, " | ")
	for _, part := range []string{"log", "settings"} {
		if !strings.Contains(strings.ToLower(unavailable), part) {
			t.Fatalf("the scoped diagnosis does not name the %s as unavailable: %v", part, diagnosis.Unavailable)
		}
	}
}

// A talkgroup the caller's scope does not grant is answered as a talkgroup with no recorded
// activity, the same answer a quiet talkgroup gets. Naming the scope refused would say what the
// caller is missing.
func TestDiagnoseFeedProblemForAnAccessCodeCallerAnswersATalkgroupItCannotSeeAsQuiet(t *testing.T) {
	s := newCallerSession(t, readScoped())

	answered := call(t, s, "diagnose_feed_problem", map[string]any{
		"system":    systemTwoRef,
		"talkgroup": 1514002,
		"date":      fixtureTime.Format("2006-01-02"),
	})

	var diagnosis feedDiagnosisShape
	decode(t, answered, &diagnosis)

	if diagnosis.LastCall != nil {
		t.Fatalf("the diagnosis answered %d as a recorded call on a talkgroup outside the caller's scope",
			diagnosis.LastCall.ID)
	}
	if diagnosis.Count != 0 {
		t.Fatalf("the diagnosis counts %d recorded calls on a talkgroup outside the caller's scope", diagnosis.Count)
	}
}

// healthReportShape is the shape analyze_health answers.
type healthReportShape struct {
	Version string `json:"version"`
	Date    string `json:"date"`

	Errors   []sourceCountShape `json:"errors"`
	Warnings []sourceCountShape `json:"warnings"`

	Restarts []logEntryShape `json:"restarts"`

	DisabledDirwatches  uint `json:"disabledDirwatches"`
	DisabledDownstreams uint `json:"disabledDownstreams"`
	PruneDays           uint `json:"pruneDays"`
	MaxClients          uint `json:"maxClients"`
	PasswordNeedsChange bool `json:"passwordNeedsChange"`

	Findings []struct {
		Subject  string `json:"subject"`
		Severity string `json:"severity"`
		Note     string `json:"note"`
	} `json:"findings"`
}

type sourceCountShape struct {
	Source string `json:"source"`
	Count  uint64 `json:"count"`
}

// feedDiagnosisShape is the shape diagnose_feed_problem answers.
type feedDiagnosisShape struct {
	System    uint `json:"system"`
	Talkgroup uint `json:"talkgroup"`
	Count     uint `json:"count"`

	LastCall *struct {
		ID       uint64 `json:"id"`
		DateTime string `json:"dateTime"`
	} `json:"lastCall"`

	LogLines []logEntryShape `json:"logLines"`

	Settings *struct {
		Delay        uint `json:"delay"`
		AutoPopulate bool `json:"autoPopulate"`
		Blacklists   uint `json:"blacklists"`
	} `json:"settings"`

	WatchedDirectories []struct {
		Directory string `json:"directory"`
		Disabled  bool   `json:"disabled"`
	} `json:"watchedDirectories"`

	Forwards []struct {
		URL      string `json:"url"`
		Disabled bool   `json:"disabled"`
	} `json:"forwards"`

	Unavailable []string `json:"unavailable"`
}

// sourceCountOf is the count one subsystem is reported under, zero when the report does not name it.
func sourceCountOf(counts []sourceCountShape, source string) uint64 {
	for _, count := range counts {
		if count.Source == source {
			return count.Count
		}
	}

	return 0
}

// TestDiagnoseFeedProblemCountsTheBlacklistedTalkgroups: the number the diagnosis gives for a
// system's blacklisted talkgroups is the number of references the list names. An administrator
// asking why a talkgroup is refused needs to know how many the system refuses; the length of the
// text the field holds says nothing an operator can use.
func TestDiagnoseFeedProblemCountsTheBlacklistedTalkgroups(t *testing.T) {
	for _, tc := range []struct {
		name       string
		blacklists string
		want       uint
	}{
		{name: "the two references the fixture stores", blacklists: "40000,40001", want: 2},
		{name: "one reference", blacklists: "1514000", want: 1},
		{name: "the references with the spaces the field allows", blacklists: "1514000, 1514100", want: 2},
		{name: "an empty list", blacklists: "", want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newConfirmingSession(t, "accept")

			call(t, s, "manage_config", map[string]any{
				"section": "systems",
				"payload": systemsPayloadWithBlacklists(s.bundle.systems.Stored, systemOneRef, tc.blacklists),
			})

			answered := call(t, s, "diagnose_feed_problem", map[string]any{
				"system":    systemOneRef,
				"talkgroup": 1514000,
				"date":      fixtureTime.Format("2006-01-02"),
			})

			var diagnosis feedDiagnosisShape
			decode(t, answered, &diagnosis)

			if diagnosis.Settings == nil {
				t.Fatalf("the diagnosis names no settings for the system: %+v", diagnosis)
			}

			if diagnosis.Settings.Blacklists != tc.want {
				t.Errorf("the diagnosis counts %d blacklisted talkgroups for a system storing %q, want %d",
					diagnosis.Settings.Blacklists, tc.blacklists, tc.want)
			}
		})
	}
}

// systemsPayloadWithBlacklists is the systems section payload as the section's save takes it, with
// one system's blacklist set to the text given.
func systemsPayloadWithBlacklists(systems domain.Systems, systemRef uint, blacklists string) []any {
	rows := []any{}

	for _, system := range systems {
		text := string(system.Blacklists)

		if system.SystemRef == systemRef {
			text = blacklists
		}

		talkgroups := []any{}

		for _, talkgroup := range system.Talkgroups {
			talkgroups = append(talkgroups, map[string]any{
				"alert":        talkgroup.Alert,
				"delay":        float64(talkgroup.Delay),
				"frequency":    float64(talkgroup.Frequency),
				"groupIds":     talkgroup.GroupIDs,
				"id":           float64(talkgroup.ID),
				"label":        talkgroup.Label,
				"led":          talkgroup.LED,
				"name":         talkgroup.Name,
				"order":        float64(talkgroup.Order),
				"tagId":        float64(talkgroup.TagID),
				"talkgroupRef": float64(talkgroup.TalkgroupRef),
				"type":         talkgroup.Kind,
			})
		}

		sites := []any{}

		for _, site := range system.Sites {
			sites = append(sites, map[string]any{
				"id":      float64(site.ID),
				"label":   site.Label,
				"order":   float64(site.Order),
				"siteRef": float64(site.SiteRef),
			})
		}

		units := []any{}

		for _, unit := range system.Units {
			units = append(units, map[string]any{
				"id":       float64(unit.ID),
				"label":    unit.Label,
				"order":    float64(unit.Order),
				"unitFrom": float64(unit.UnitFrom),
				"unitRef":  float64(unit.UnitRef),
				"unitTo":   float64(unit.UnitTo),
			})
		}

		rows = append(rows, map[string]any{
			"alert":        system.Alert,
			"autoPopulate": system.AutoPopulate,
			"blacklists":   text,
			"delay":        float64(system.Delay),
			"id":           float64(system.ID),
			"label":        system.Label,
			"led":          system.LED,
			"order":        float64(system.Order),
			"sites":        sites,
			"systemRef":    float64(system.SystemRef),
			"talkgroups":   talkgroups,
			"type":         system.Kind,
			"units":        units,
		})
	}

	return rows
}
