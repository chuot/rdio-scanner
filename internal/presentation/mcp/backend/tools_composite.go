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
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The composites: the questions an operator asks that no entry point can answer on its own. Each
// one composes several reads into one answer, and names in its own description the entry points it
// composed, so an assistant reading the catalog knows what a call reaches and an operator reading
// the answer knows where each part came from.

// registerCompositeTools adds the health review and the feed diagnosis.
func (s *Server) registerCompositeTools(deps Deps) {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:  "analyze_health",
		Title: "Is this deployment healthy?",
		Description: "One day of the deployment read as a whole: the warning and error lines counted " +
			"per subsystem, the server's own restart markers, the watched directories and downstream " +
			"servers that are switched off, the retention horizon, the listener ceiling, the version, " +
			"and the server's notice that the administrator password still needs changing — assembled " +
			"into the findings that need a decision. Composed from POST /api/admin/logs, asked once " +
			"per severity and once for the restart markers, and from GET /api/admin/config. No single " +
			"entry point answers this question.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in analyzeHealthInput) (*mcp.CallToolResult, healthReport, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, healthReport{}, errNoCaller
		}
		if !isAdministrator(who) {
			return refused(administratorRequired), healthReport{}, nil
		}

		// The day the review reads: the one it was given, or the day this server is in.
		day := dayOf(in.Date)
		if day == "" {
			day = dayOf(deps.Clock.Now().Format(time.RFC3339))
		}

		at := parseDay(day)

		configuration, err := deps.Config.Handle(ctx, application.GetConfigQuery{})
		if err != nil {
			return nil, healthReport{}, err
		}

		// The log view is asked once per severity, because its per-subsystem counts describe the
		// criteria as submitted: asking for warnings answers how many each subsystem warned, and
		// asking for errors answers how many each subsystem failed. Oldest first, because the day
		// the review reads is the window: asked newest-first, the log view's own criterion reads
		// the day *before* an instant at its start.
		warnings, err := deps.Logs.Handle(ctx, application.SearchLogsQuery{Search: domain.LogSearch{
			Date:      at,
			Level:     domain.LogLevelWarn,
			Limit:     domain.DefaultLogSearchLimit,
			Ascending: true,
		}})
		if err != nil {
			return nil, healthReport{}, err
		}

		failures, err := deps.Logs.Handle(ctx, application.SearchLogsQuery{Search: domain.LogSearch{
			Date:      at,
			Level:     domain.LogLevelError,
			Limit:     domain.DefaultLogSearchLimit,
			Ascending: true,
		}})
		if err != nil {
			return nil, healthReport{}, err
		}

		// The server's own startup line is the restart marker: a server that came up twice in a day
		// has a reason an operator has to go and find.
		restarts, err := deps.Logs.Handle(ctx, application.SearchLogsQuery{Search: domain.LogSearch{
			Date:      at,
			Text:      restartMarker,
			Limit:     domain.DefaultLogSearchLimit,
			Ascending: true,
		}})
		if err != nil {
			return nil, healthReport{}, err
		}

		return nil, review(deps, configuration, at, warnings, failures, restarts), nil
	})

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:  "diagnose_feed_problem",
		Title: "This talkgroup has gone quiet. Why?",
		Description: "One talkgroup on one system read from every side at once: its last recorded " +
			"call and how many it has, the log lines naming it in the day reviewed, the system's own " +
			"delay, auto-populate, and blacklist count, the watched directories feeding that system " +
			"and whether they are switched off, and the downstream servers whose scope forwards it. " +
			"Composed from POST /api/calls/search, POST /api/admin/logs, and " +
			"GET /api/admin/config/{section}. An access-code caller gets the same question for the " +
			"talkgroups its code grants, and the log view and the configuration it cannot see are " +
			"named as unavailable, not left out. A talkgroup outside what this caller may see is " +
			"answered as a talkgroup with nothing recorded.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in diagnoseFeedInput) (*mcp.CallToolResult, feedDiagnosis, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, feedDiagnosis{}, errNoCaller
		}

		day := dayOf(in.Date)
		if day == "" {
			day = dayOf(deps.Clock.Now().Format(time.RFC3339))
		}

		// The last activity on the talkgroup, newest first, and how much history it has at all.
		// The day is not a filter here: "when did it last report" is asked of the whole history,
		// and the day the review names scopes the log lines, not the answer.
		page, err := deps.Search.Handle(ctx, application.SearchCallsQuery{
			Criteria: domain.ParseCallSearch(map[string]any{
				"system":    float64(in.System),
				"talkgroup": float64(in.Talkgroup),
				"limit":     float64(1),
				"sort":      float64(-1),
			}),
			Scope: scopeOf(who),
		})
		if err != nil {
			return nil, feedDiagnosis{}, err
		}

		answer := feedDiagnosis{
			System:      in.System,
			Talkgroup:   in.Talkgroup,
			Date:        parseDay(day).Format("2006-01-02"),
			Count:       page.Count,
			Unavailable: []string{},
		}

		if len(page.Results) > 0 {
			answer.LastCall = &lastCall{
				ID:       page.Results[0].ID,
				DateTime: page.Results[0].Timestamp.Format(time.RFC3339),
			}
		}

		if !isAdministrator(who) {
			// The parts of the deployment a listener's code does not open. Named, because an
			// answer that left them out reads like a healthy talkgroup.
			answer.Unavailable = append(answer.Unavailable,
				"the server's log view needs the administrator's credential",
				"the system's own settings, its watched directories, and its downstream servers need the administrator's credential",
			)

			return nil, answer, nil
		}

		configuration, err := deps.Config.Handle(ctx, application.GetConfigQuery{})
		if err != nil {
			return nil, feedDiagnosis{}, err
		}

		lines, err := deps.Logs.Handle(ctx, application.SearchLogsQuery{Search: domain.LogSearch{
			Date:      parseDay(day),
			Text:      fmt.Sprintf("%d", in.Talkgroup),
			Limit:     domain.DefaultLogSearchLimit,
			Ascending: true,
		}})
		if err != nil {
			return nil, feedDiagnosis{}, err
		}

		answer.LogLines = rowsOf(lines.Results)

		// The system is named by the reference a client names it with, which is not the database
		// identifier the watched directories are filed against.
		if system, found := configuration.Systems.ByRef(in.System); found {
			answer.Settings = &feedSettings{
				Delay:        system.Delay,
				AutoPopulate: system.AutoPopulate,
				Blacklists:   uint(len(system.Blacklists)),
			}

			for _, dirwatch := range configuration.Dirwatches {
				if dirwatch.SystemID == system.ID {
					answer.WatchedDirectories = append(answer.WatchedDirectories, watchedDirectory{
						Directory: dirwatch.Directory,
						Disabled:  dirwatch.Disabled,
					})
				}
			}

			for _, downstream := range configuration.Downstreams {
				if downstream.Scope.Grants(answer.grantingCall()) {
					answer.Forwards = append(answer.Forwards, forward{
						URL:      downstream.URL,
						Disabled: downstream.Disabled,
					})
				}
			}
		}

		return nil, answer, nil
	})
}

// restartMarker is the line the server writes when it comes up, which is what a restart leaves in
// the log view.
const restartMarker = "server started"

// analyzeHealthInput is the day to review. The day is the window the log view reads, so the review
// is one day wide and says so.
type analyzeHealthInput struct {
	Date string `json:"date,omitempty" jsonschema:"the day to review, as an RFC 3339 instant inside it, or a date; defaults to the day this server is in"`
}

// healthReport is one day of the deployment, read as a whole.
type healthReport struct {
	Version string `json:"version"`
	Date    string `json:"date"`

	Errors   []sourceCount `json:"errors"`
	Warnings []sourceCount `json:"warnings"`

	Restarts []logRow `json:"restarts"`

	DisabledDirwatches  uint `json:"disabledDirwatches"`
	DisabledDownstreams uint `json:"disabledDownstreams"`
	PruneDays           uint `json:"pruneDays"`
	MaxClients          uint `json:"maxClients"`
	PasswordNeedsChange bool `json:"passwordNeedsChange"`

	Findings []finding `json:"findings"`
}

// finding is one part of the deployment that needs a decision, named once.
type finding struct {
	Subject  string `json:"subject"`
	Severity string `json:"severity"`
	Note     string `json:"note"`
}

func review(deps Deps, configuration domain.Configuration, at time.Time, warnings, failures, restarts ports.LogSearchPage) healthReport {
	answer := healthReport{
		Version: deps.Version,

		// The day the review read, named as a day: the instant the criteria carry is the instant
		// the log view was asked at, and an operator reading the answer wants to know which day
		// it looked at.
		Date:                at.Format("2006-01-02"),
		Errors:              countsOf(failures),
		Warnings:            countsOf(warnings),
		Restarts:            rowsOf(restarts.Results),
		PruneDays:           configuration.Options.PruneDays,
		MaxClients:          configuration.Options.MaxClients,
		PasswordNeedsChange: configuration.Options.AdminPasswordNeedChange,
		Findings:            []finding{},
	}

	// The subsystems that failed, named with how much they failed: the log view's own counts, not a
	// tally invented from the page.
	for _, count := range answer.Errors {
		answer.Findings = append(answer.Findings, finding{
			Subject:  count.Source,
			Severity: domain.LogLevelError,
			Note:     fmt.Sprintf("%d error line(s) that day", count.Count),
		})
	}

	for _, count := range answer.Warnings {
		// The server's own warnings are the restart markers, and they are named as themselves.
		if count.Source == string(domain.LogSourceServer) {
			continue
		}

		answer.Findings = append(answer.Findings, finding{
			Subject:  count.Source,
			Severity: domain.LogLevelWarn,
			Note:     fmt.Sprintf("%d warning line(s) that day", count.Count),
		})
	}

	if len(answer.Restarts) > 0 {
		answer.Findings = append(answer.Findings, finding{
			Subject:  string(domain.LogSourceServer),
			Severity: domain.LogLevelWarn,
			Note:     fmt.Sprintf("the server came up %d time(s) that day", len(answer.Restarts)),
		})
	}

	for _, dirwatch := range configuration.Dirwatches {
		if dirwatch.Disabled {
			answer.DisabledDirwatches++
		}
	}

	for _, downstream := range configuration.Downstreams {
		if downstream.Disabled {
			answer.DisabledDownstreams++
		}
	}

	if answer.DisabledDirwatches > 0 {
		answer.Findings = append(answer.Findings, finding{
			Subject:  "dirwatch",
			Severity: domain.LogLevelWarn,
			Note:     fmt.Sprintf("%d watched directory(ies) are switched off", answer.DisabledDirwatches),
		})
	}

	if answer.DisabledDownstreams > 0 {
		answer.Findings = append(answer.Findings, finding{
			Subject:  "downstream",
			Severity: domain.LogLevelWarn,
			Note:     fmt.Sprintf("%d downstream server(s) are switched off", answer.DisabledDownstreams),
		})
	}

	if answer.PruneDays == 0 {
		answer.Findings = append(answer.Findings, finding{
			Subject:  "prune",
			Severity: domain.LogLevelInfo,
			Note:     "the server keeps every recorded call: no retention horizon is set",
		})
	}

	if answer.PasswordNeedsChange {
		answer.Findings = append(answer.Findings, finding{
			Subject:  "password",
			Severity: domain.LogLevelWarn,
			Note:     "the administrator password still needs changing; changing it invalidates the sessions that are standing",
		})
	}

	return answer
}

// countsOf is the log view's own per-subsystem tally, as the search reported it.
func countsOf(page ports.LogSearchPage) []sourceCount {
	answer := []sourceCount{}

	for _, count := range page.SourceCounts {
		answer = append(answer, sourceCount{Source: string(count.Source), Count: count.Count})
	}

	return answer
}

// diagnoseFeedInput names one talkgroup on one system, and the day to review.
type diagnoseFeedInput struct {
	System    uint   `json:"system" jsonschema:"the system's reference, as the systems projection names it"`
	Talkgroup uint   `json:"talkgroup" jsonschema:"the talkgroup's reference"`
	Date      string `json:"date,omitempty" jsonschema:"the day to review, as an RFC 3339 instant inside it, or a date; defaults to the day this server is in"`
}

// feedDiagnosis is one talkgroup read from every side at once.
type feedDiagnosis struct {
	System    uint   `json:"system"`
	Talkgroup uint   `json:"talkgroup"`
	Date      string `json:"date"`
	Count     uint   `json:"count"`

	LastCall *lastCall `json:"lastCall,omitempty"`

	LogLines []logRow `json:"logLines"`

	Settings *feedSettings `json:"settings,omitempty"`

	WatchedDirectories []watchedDirectory `json:"watchedDirectories"`
	Forwards           []forward          `json:"forwards"`

	Unavailable []string `json:"unavailable"`
}

type lastCall struct {
	ID       uint64 `json:"id"`
	DateTime string `json:"dateTime"`
}

type feedSettings struct {
	Delay        uint `json:"delay"`
	AutoPopulate bool `json:"autoPopulate"`
	Blacklists   uint `json:"blacklists"`
}

type watchedDirectory struct {
	Directory string `json:"directory"`
	Disabled  bool   `json:"disabled"`
}

type forward struct {
	URL      string `json:"url"`
	Disabled bool   `json:"disabled"`
}

// grantingCall is the call a downstream's scope is asked about: the scope is asked of a call, so
// the question is answered with the two references that identify one.
func (d feedDiagnosis) grantingCall() domain.Call {
	call := domain.NewCall()

	call.System = domain.System{SystemRef: d.System}
	call.Talkgroup = domain.Talkgroup{TalkgroupRef: d.Talkgroup}

	return call
}
