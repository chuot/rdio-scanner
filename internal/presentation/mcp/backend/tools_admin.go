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

package backend

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/dto"
)

// The administrator's reading capabilities. Each answers a question an
// operator asks, in one call, over the entry points the OpenAPI document
// names, and each stands behind the administrator's credential.

// registerAdminTools adds the deployment summary, the log search, and the explanation of a line.
func registerAdminTools(target *mcp.Server, deps Deps) {
	mcp.AddTool(target, &mcp.Tool{
		Name:  "inspect_system",
		Title: "What am I administering?",
		Description: "A summary of this deployment: its version, whether it runs in a container, how " +
			"many systems, talkgroups, sites, units, groups, tags, access codes, upload credentials, " +
			"watched directories, and downstream servers it holds, how many watched directories and " +
			"downstreams are disabled, the prune horizon, the listener ceiling, whether recordings are " +
			"converted and duplicates detected, whether the administrator password still needs " +
			"changing, and the alert sequences the server carries. Read this before changing anything: " +
			"it is a summary built from the configuration and the alert sequences, not the " +
			"configuration document. Composed from GET /api/admin/config and GET /api/admin/alerts.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ inspectSystemInput) (*mcp.CallToolResult, deploymentView, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, deploymentView{}, errNoCaller
		}
		if !isAdministrator(who) {
			return refused(administratorRequired), deploymentView{}, nil
		}

		configuration, err := deps.Config.Handle(ctx, application.GetConfigQuery{})
		if err != nil {
			return nil, deploymentView{}, err
		}

		return nil, summarize(deps, configuration), nil
	})

	mcp.AddTool(target, &mcp.Tool{
		Name:  "search_logs",
		Title: "Search the server's log view",
		Description: "One page of the server's own log, filtered by severity, subsystem, a substring of " +
			"the message, and day, with paging and sort direction. Every filter is an argument of this " +
			"one tool; there is no per-filter sibling to choose between. The answer is the page: the " +
			"total the filters match, the earliest and latest instant they match, the criteria as this " +
			"server applied them, and the entries. The subsystem filter takes the names the server " +
			"files entries under, and a name outside that vocabulary is reported, not ignored. Answers " +
			"POST /api/admin/logs.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchLogsInput) (*mcp.CallToolResult, logView, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, logView{}, errNoCaller
		}
		if !isAdministrator(who) {
			return refused(administratorRequired), logView{}, nil
		}

		criteria := in.criteria()

		// A subsystem this server does not have is not a filter that matches
		// nothing: it is a caller asking about a part of the server that does not
		// exist, and answering a page of everything is how a typo becomes an
		// assistant concluding that nothing happened.
		if criteria.SourceWasRejected() {
			return refused("the subsystem filter takes the names this server files its entries under: " +
				strings.Join(logSourceNames(), ", ") + ". " +
				"The name " + in.Source + " is not one of them, and this server does not answer a subsystem it " +
				"does not have with a page of everything."), logView{}, nil
		}

		page, err := deps.Logs.Handle(ctx, application.SearchLogsQuery{Search: criteria})
		if err != nil {
			return nil, logView{}, err
		}

		return nil, logAnswer(page, criteria), nil
	})

	mcp.AddTool(target, &mcp.Tool{
		Name:  "explain_error",
		Title: "Explain one log line by what was said around it",
		Description: "One line of the log, the lines the same subsystem wrote that day in order on both " +
			"sides of it, and the lines the other subsystems wrote in the same window, grouped by " +
			"subsystem. A single line is almost never the cause: the answer is the neighbourhood. Name " +
			"the line by its subsystem, a substring of its message, and its day. The subsystem's own " +
			"lines come back oldest first, so what led up to the line and what followed it are both " +
			"there. Composed from POST /api/admin/logs, asked three times: the line, the subsystem's " +
			"own day, and the same day across every subsystem.",
		Annotations: readOnly(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in explainErrorInput) (*mcp.CallToolResult, errorExplanation, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, errorExplanation{}, errNoCaller
		}
		if !isAdministrator(who) {
			return refused(administratorRequired), errorExplanation{}, nil
		}

		day := dayOf(in.Date)
		if day == "" {
			return refused("the explanation needs the day the line was written"), errorExplanation{}, nil
		}

		// The line itself: the first entry the subsystem and the message fragment match that day.
		named, err := deps.Logs.Handle(ctx, application.SearchLogsQuery{Search: domain.LogSearch{
			Date:      parseDay(day),
			Source:    domain.LogSource(in.Source),
			Text:      in.Text,
			Limit:     domain.DefaultLogSearchLimit,
			Ascending: true,
		}})
		if err != nil {
			return nil, errorExplanation{}, err
		}

		if len(named.Results) == 0 {
			return refused("no log entry matches that subsystem and that message fragment that day"), errorExplanation{}, nil
		}

		// The subsystem's own day, oldest first: what led to the line and what followed it.
		same, err := deps.Logs.Handle(ctx, application.SearchLogsQuery{Search: domain.LogSearch{
			Date:      parseDay(day),
			Source:    domain.LogSource(in.Source),
			Limit:     domain.DefaultLogSearchLimit,
			Ascending: true,
		}})
		if err != nil {
			return nil, errorExplanation{}, err
		}

		// The same window across every subsystem, so the answer says what else was going on while
		// the line was written.
		around, err := deps.Logs.Handle(ctx, application.SearchLogsQuery{Search: domain.LogSearch{
			Date:      parseDay(day),
			Limit:     domain.DefaultLogSearchLimit,
			Ascending: true,
		}})
		if err != nil {
			return nil, errorExplanation{}, err
		}

		return nil, explain(named.Results[0], same.Results, around.Results, domain.LogSource(in.Source)), nil
	})
}

// administratorRequired is the refusal an access-code caller gets from an administrator's
// capability. It names what is required and nothing about what the caller is missing.
const administratorRequired = "this capability needs the administrator's credential"

// isAdministrator is who may read the whole deployment: the administrator's
// credential, and nothing else. A listener's access code opens the scoped
// read-only tier, so an anonymous caller on a deployment that lists no
// access codes reads that tier too.
func isAdministrator(who Caller) bool {
	return who.Tier == TierFull
}

// inspectSystemInput is the deployment summary's empty argument list: what
// is being administered is decided by the credential, not an argument.
type inspectSystemInput struct{}

// deploymentView is the summary an operator reads before changing anything.
type deploymentView struct {
	Version string `json:"version"`
	Docker  bool   `json:"docker"`

	PasswordNeedsChange bool `json:"passwordNeedsChange"`

	Counts inventoryCounts `json:"counts"`

	DisabledDirwatches  uint `json:"disabledDirwatches"`
	DisabledDownstreams uint `json:"disabledDownstreams"`

	PruneDays                  uint `json:"pruneDays"`
	MaxClients                 uint `json:"maxClients"`
	AutoPopulate               bool `json:"autoPopulate"`
	DuplicateDetectionDisabled bool `json:"duplicateDetectionDisabled"`
	AudioConversion            uint `json:"audioConversion"`

	Alerts []alertSequence `json:"alerts"`
}

// inventoryCounts is how many of each thing this server holds.
type inventoryCounts struct {
	Systems     uint `json:"systems"`
	Talkgroups  uint `json:"talkgroups"`
	Sites       uint `json:"sites"`
	Units       uint `json:"units"`
	Groups      uint `json:"groups"`
	Tags        uint `json:"tags"`
	Accesses    uint `json:"accesses"`
	Apikeys     uint `json:"apikeys"`
	Dirwatches  uint `json:"dirwatches"`
	Downstreams uint `json:"downstreams"`
}

// alertSequence is one named alert the server carries, with the tones it is made of.
type alertSequence struct {
	Name  string      `json:"name"`
	Tones []alertTone `json:"tones"`
}

type alertTone struct {
	Begin     float32 `json:"begin"`
	End       float32 `json:"end"`
	Frequency uint    `json:"frequency"`
	Kind      string  `json:"type"`
}

func summarize(deps Deps, configuration domain.Configuration) deploymentView {
	counts := inventoryCounts{
		Systems:     uint(len(configuration.Systems)),
		Groups:      uint(len(configuration.Groups)),
		Tags:        uint(len(configuration.Tags)),
		Accesses:    uint(len(configuration.Access)),
		Apikeys:     uint(len(configuration.Apikeys)),
		Dirwatches:  uint(len(configuration.Dirwatches)),
		Downstreams: uint(len(configuration.Downstreams)),
	}

	for _, system := range configuration.Systems {
		counts.Talkgroups += uint(len(system.Talkgroups))
		counts.Sites += uint(len(system.Sites))
		counts.Units += uint(len(system.Units))
	}

	// A disabled watcher is not reading its directory, and a disabled
	// forwarder is not forwarding: the two counts an operator looks for when
	// the history has stopped growing.
	disabledDirwatches, disabledDownstreams := uint(0), uint(0)

	for _, dirwatch := range configuration.Dirwatches {
		if dirwatch.Disabled {
			disabledDirwatches++
		}
	}

	for _, downstream := range configuration.Downstreams {
		if downstream.Disabled {
			disabledDownstreams++
		}
	}

	return deploymentView{
		Version:                    deps.Version,
		Docker:                     deps.Docker,
		PasswordNeedsChange:        configuration.Options.AdminPasswordNeedChange,
		Counts:                     counts,
		DisabledDirwatches:         disabledDirwatches,
		DisabledDownstreams:        disabledDownstreams,
		PruneDays:                  configuration.Options.PruneDays,
		MaxClients:                 configuration.Options.MaxClients,
		AutoPopulate:               configuration.Options.AutoPopulate,
		DuplicateDetectionDisabled: configuration.Options.DisableDuplicateDetection,
		AudioConversion:            configuration.Options.AudioConversion,
		Alerts:                     alerts(),
	}
}

// alerts is the alert sequences the server carries, named so an operator can point at one.
func alerts() []alertSequence {
	payload := dto.Alerts()

	answer := []alertSequence{}

	for _, name := range sortedKeys(payload) {
		tones := []alertTone{}

		for _, tone := range payload[name] {
			tones = append(tones, alertTone{
				Begin:     tone.Begin,
				End:       tone.End,
				Frequency: tone.Frequency,
				Kind:      tone.Kind,
			})
		}

		answer = append(answer, alertSequence{Name: name, Tones: tones})
	}

	return answer
}

// sortedKeys names a payload's keys in order, so the same read answers the same way twice.
func sortedKeys(payload dto.AlertsPayload) []string {
	keys := make([]string, 0, len(payload))

	for name := range payload {
		keys = append(keys, name)
	}

	sort.Strings(keys)

	return keys
}

// searchLogsInput is the log view's filters, every one of them an argument of this one tool.
type searchLogsInput struct {
	Level  string `json:"level,omitempty" jsonschema:"info, warn, or error"`
	Source string `json:"source,omitempty" jsonschema:"the subsystem an entry is filed under"`
	Text   string `json:"text,omitempty" jsonschema:"a substring of the message"`
	Date   string `json:"date,omitempty" jsonschema:"one day of the log, as an RFC 3339 instant inside it, or a date"`
	Limit  uint   `json:"limit,omitempty" jsonschema:"how many entries to answer, defaulted and clamped by the server"`
	Offset uint   `json:"offset,omitempty" jsonschema:"how many entries to skip"`
	Sort   int    `json:"sort,omitempty" jsonschema:"negative for newest first, zero or positive for oldest first"`
}

func (in searchLogsInput) criteria() domain.LogSearch {
	payload := map[string]any{}

	if in.Level != "" {
		payload["level"] = in.Level
	}
	if in.Source != "" {
		payload["source"] = in.Source
	}
	if in.Text != "" {
		payload["text"] = in.Text
	}
	if in.Date != "" {
		payload["date"] = dayOf(in.Date)
	}
	if in.Limit != 0 {
		payload["limit"] = float64(in.Limit)
	}
	if in.Offset != 0 {
		payload["offset"] = float64(in.Offset)
	}
	if in.Sort != 0 {
		payload["sort"] = float64(in.Sort)
	}

	return domain.ParseLogSearch(payload)
}

// logView is one page of the log view, with the criteria echoed as this server applied them.
type logView struct {
	Count     uint64 `json:"count"`
	DateStart string `json:"dateStart,omitempty"`
	DateStop  string `json:"dateStop,omitempty"`

	Options logCriteria   `json:"options"`
	Sources []sourceCount `json:"sources"`
	Entries []logRow      `json:"entries"`
}

// sourceCount is how many entries one part of the server wrote among everything the criteria match.
type sourceCount struct {
	Source string `json:"source"`
	Count  uint64 `json:"count"`
}

// logCriteria is the filter set as the server used it: a request past the
// ceiling echoes the ceiling, and a request naming no page echoes the
// default.
type logCriteria struct {
	Level  string `json:"level,omitempty"`
	Source string `json:"source,omitempty"`
	Text   string `json:"text,omitempty"`
	Limit  uint   `json:"limit"`
	Offset uint   `json:"offset"`
	Sort   int    `json:"sort"`
}

// logRow is one entry of the log view.
type logRow struct {
	ID       uint64 `json:"id"`
	DateTime string `json:"dateTime"`
	Level    string `json:"level"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

func logAnswer(page ports.LogSearchPage, criteria domain.LogSearch) logView {
	answer := logView{
		Count: page.Count,
		Options: logCriteria{
			Level:  criteria.Level,
			Source: string(criteria.Source),
			Text:   criteria.Text,
			Limit:  criteria.Limit,
			Offset: criteria.Offset,
			Sort:   -1,
		},
		Entries: []logRow{},
	}

	if criteria.Ascending {
		answer.Options.Sort = 1
	}

	if !page.DateStart.IsZero() {
		answer.DateStart = page.DateStart.Format(time.RFC3339)
	}
	if !page.DateStop.IsZero() {
		answer.DateStop = page.DateStop.Format(time.RFC3339)
	}

	for _, count := range page.SourceCounts {
		answer.Sources = append(answer.Sources, sourceCount{
			Source: string(count.Source),
			Count:  count.Count,
		})
	}

	for _, entry := range page.Results {
		answer.Entries = append(answer.Entries, logRow{
			ID:       entry.ID,
			DateTime: entry.DateTime.Format(time.RFC3339),
			Level:    entry.Level,
			Source:   string(entry.Source),
			Message:  entry.Message,
		})
	}

	return answer
}

// explainErrorInput names one line: its subsystem, a fragment of its message, and its day.
type explainErrorInput struct {
	Source string `json:"source" jsonschema:"the subsystem the line is filed under"`
	Text   string `json:"text" jsonschema:"a substring of the message"`
	Date   string `json:"date" jsonschema:"the day the line was written"`
}

// errorExplanation is one line, its subsystem's own day, and the other subsystems in the same
// window.
type errorExplanation struct {
	Named      logRow       `json:"named"`
	Same       []logRow     `json:"same"`
	Neighbours []sourceLine `json:"neighbours"`
}

// sourceLine is one subsystem's lines in the window.
type sourceLine struct {
	Source  string   `json:"source"`
	Entries []logRow `json:"entries"`
}

func explain(named domain.LogEntry, same []domain.LogEntry, around []domain.LogEntry, source domain.LogSource) errorExplanation {
	answer := errorExplanation{
		Named:      logRowOf(named),
		Same:       rowsOf(same),
		Neighbours: []sourceLine{},
	}

	grouped := map[domain.LogSource][]logRow{}
	order := []domain.LogSource{}

	for _, entry := range around {
		if entry.Source == source {
			continue
		}

		if _, seen := grouped[entry.Source]; !seen {
			order = append(order, entry.Source)
		}

		grouped[entry.Source] = append(grouped[entry.Source], logRowOf(entry))
	}

	for _, name := range order {
		answer.Neighbours = append(answer.Neighbours, sourceLine{
			Source:  string(name),
			Entries: grouped[name],
		})
	}

	return answer
}

func rowsOf(entries []domain.LogEntry) []logRow {
	answer := []logRow{}

	for _, entry := range entries {
		answer = append(answer, logRowOf(entry))
	}

	return answer
}

func logRowOf(entry domain.LogEntry) logRow {
	return logRow{
		ID:       entry.ID,
		DateTime: entry.DateTime.Format(time.RFC3339),
		Level:    entry.Level,
		Source:   string(entry.Source),
		Message:  entry.Message,
	}
}

// parseDay is the day a capability was given, as the instant the log's own
// criterion reads. The day arrives normalized by dayOf, so an unparseable
// value is no day at all.
func parseDay(day string) time.Time {
	at, err := time.Parse(time.RFC3339, day)
	if err != nil {
		return time.Time{}
	}

	return at
}

// logSourceNames is the subsystem vocabulary, in the order the log view
// lists it. The answer to a wrong subsystem name is the vocabulary, and
// not just a no.
func logSourceNames() []string {
	known := domain.KnownLogSources()

	names := make([]string, 0, len(known))

	for _, source := range known {
		names = append(names, string(source))
	}

	return names
}
