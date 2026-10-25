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
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// errNoCaller is what a capability says when it was reached with no caller
// decided for it. That is a wiring fault: the gate is the only thing that
// puts a caller in front of these capabilities, and a capability reached
// past the gate must stop rather than answer with an administrator's reach.
var errNoCaller = errors.New("the capability was reached with no caller decided for it")

// isNotFound is the store's own "that record is not there".
func isNotFound(err error) bool {
	return errors.Is(err, ports.ErrNotFound)
}

// The read-only capabilities an access-code caller reaches: the history
// search, one recorded call, and discovery. Each runs the use case the
// REST surface runs, over the scope the gate decided; see the capabilities-
// are-questions-not-routes maintainer note.

// registerCallTools adds the history search, one call, and the scoped systems projection.
func registerCallTools(target *mcp.Server, deps Deps) {
	mcp.AddTool(target, &mcp.Tool{
		Name:  "search_calls",
		Title: "Search the recorded call history",
		Description: "One page of recorded calls, filtered by system, talkgroup, group label, tag " +
			"label, day, paging, and sort direction. Every filter is an argument of this tool; there " +
			"is no per-filter sibling to choose between. The answer is the page itself: the total the " +
			"filters match, the earliest and latest call they match, the rows, oldest or newest " +
			"first, and the page as this server applied it, so the next page can be asked for by " +
			"adding that page size to the offset. A filter naming something outside what this caller " +
			"may see answers an empty page, the same answer a history with nothing in it gets. Built " +
			"from POST /api/calls/search, the entry point a listener's own history search uses.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchCallsInput) (*mcp.CallToolResult, callPage, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, callPage{}, errNoCaller
		}

		criteria := in.criteria()

		page, err := deps.Search.Handle(ctx, application.SearchCallsQuery{
			Criteria: criteria,
			Scope:    scopeOf(who),
		})
		if err != nil {
			return nil, callPage{}, err
		}

		return nil, pageAnswer(page, criteria), nil
	})

	mcp.AddTool(target, &mcp.Tool{
		Name:  "get_call",
		Title: "Read one recorded call",
		Description: "Everything one recorded call carries except its recording: the system and " +
			"talkgroup it ran on, when it began, how long its recording lasts, the channel type, the " +
			"site, the units heard on it, the talkgroups it was patched to, and where its recording " +
			"is served from. The recording itself is not in the answer: audioUrl names the entry " +
			"point that serves it, so the answer stays small and the bytes are fetched on their own. " +
			"A call this caller may not see is answered exactly as a call that is not there. Built " +
			"from GET /api/calls/{callId}; the recording is served by GET /api/calls/{callId}/audio.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getCallInput) (*mcp.CallToolResult, callDetail, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, callDetail{}, errNoCaller
		}

		call, err := deps.Call.Handle(ctx, application.GetCallByIdQuery{ID: in.CallID})
		if err != nil {
			if isNotFound(err) {
				return refused("no recorded call with that identifier"), callDetail{}, nil
			}

			return nil, callDetail{}, err
		}

		// The scope is answered before anything about the call is named: saying that the call
		// exists and is out of reach is telling the caller what they are missing.
		if who.Scoped && !who.Scope.Grants(call) {
			return refused("no recorded call with that identifier"), callDetail{}, nil
		}

		return nil, callAnswer(call), nil
	})

	mcp.AddTool(target, &mcp.Tool{
		Name:  "list_systems",
		Title: "List the systems, talkgroups, sites, and units this caller can name",
		Description: "The references every other capability asks for: system, talkgroup, site, and " +
			"unit identifiers, with their labels. The list is projected through what this caller is " +
			"allowed to see, so an access code that names two systems sees those two and nothing " +
			"else, and an administrator sees the whole configuration. Read this before searching: a " +
			"reference an assistant invented answers as an empty page. Built from the systems section " +
			"of GET /api/admin/config/{section}, projected through this caller's reach.",
		Annotations: readOnly(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listSystemsInput) (*mcp.CallToolResult, systemsView, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, systemsView{}, errNoCaller
		}

		configuration, err := deps.Config.Handle(ctx, application.GetConfigQuery{})
		if err != nil {
			return nil, systemsView{}, err
		}

		// The projection is the one the listener's own configuration payload uses, so an
		// assistant and a listener at a desk are naming the same set of things.
		view := configuration.Systems.Scoped(scopeOf(who), configuration.Tags)

		return nil, systemsAnswer(view), nil
	})
}

// searchCallsInput is the history search, with every filter the history accepts as an argument of
// this one tool.
type searchCallsInput struct {
	System    uint   `json:"system,omitempty" jsonschema:"the system's reference, as list_systems names it"`
	Talkgroup uint   `json:"talkgroup,omitempty" jsonschema:"the talkgroup's reference"`
	Group     string `json:"group,omitempty" jsonschema:"a group label; it stands for the talkgroups of this label the caller may see"`
	Tag       string `json:"tag,omitempty" jsonschema:"a tag label; it stands for the talkgroups of this label the caller may see"`
	Date      string `json:"date,omitempty" jsonschema:"one day of the history, as an RFC 3339 instant inside it"`
	Limit     uint   `json:"limit,omitempty" jsonschema:"how many rows to answer, defaulted and clamped by the server"`
	Offset    uint   `json:"offset,omitempty" jsonschema:"how many rows to skip"`
	Sort      int    `json:"sort,omitempty" jsonschema:"negative for newest first, zero or positive for oldest first"`
}

// criteria is the history's own filter parsing, the same function the
// REST surface parses its body with. Only the filters the caller named
// are handed over: an absent page size means the default, a named zero
// means zero rows.
func (in searchCallsInput) criteria() domain.CallSearch {
	payload := map[string]any{}

	if in.System != 0 {
		payload["system"] = float64(in.System)
	}
	if in.Talkgroup != 0 {
		payload["talkgroup"] = float64(in.Talkgroup)
	}
	if in.Group != "" {
		payload["group"] = in.Group
	}
	if in.Tag != "" {
		payload["tag"] = in.Tag
	}
	if in.Date != "" {
		payload["date"] = in.Date
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

	return domain.ParseCallSearch(payload)
}

// callPage is one page of history, shaped for the question "what happened".
type callPage struct {
	// Count is every call the filters match, not the rows on this page.
	Count     uint        `json:"count"`
	DateStart string      `json:"dateStart,omitempty" jsonschema:"the earliest call the filters match"`
	DateStop  string      `json:"dateStop,omitempty" jsonschema:"the latest call the filters match"`
	Options   callOptions `json:"options"`
	Results   []callRow   `json:"results"`
}

// callOptions echoes what this server was asked for, which is not always what it used: a page
// asking for more rows than the history will ever return gets the ceiling.
type callOptions struct {
	Limit  uint `json:"limit"`
	Offset uint `json:"offset"`
	Sort   int  `json:"sort"`
}

type callRow struct {
	ID          uint64 `json:"id"`
	System      uint   `json:"system"`
	Talkgroup   uint   `json:"talkgroup"`
	DateTime    string `json:"dateTime"`
	AudioLength string `json:"audioLength,omitempty" jsonschema:"the recording's length as mm:ss, empty when it was never measured"`
}

// pageAnswer is one page with the criteria as this server applied them
// echoed back, so an assistant paging further knows the page size that
// was used.
func pageAnswer(page ports.CallSearchPage, criteria domain.CallSearch) callPage {
	answer := callPage{
		Count: page.Count,
		Options: callOptions{
			Limit:  criteria.Limit,
			Offset: criteria.Offset,
			Sort:   criteria.Sort,
		},
		Results: []callRow{},
	}

	if !page.DateStart.IsZero() {
		answer.DateStart = page.DateStart.Format(time.RFC3339)
	}
	if !page.DateStop.IsZero() {
		answer.DateStop = page.DateStop.Format(time.RFC3339)
	}

	for _, row := range page.Results {
		answer.Results = append(answer.Results, callRow{
			ID:          row.ID,
			System:      row.SystemRef,
			Talkgroup:   row.TalkgroupRef,
			DateTime:    row.Timestamp.Format(time.RFC3339),
			AudioLength: audioLength(row.AudioLength),
		})
	}

	return answer
}

// audioLength is a recording's length as the product states it, mm:ss, and
// nothing at all when the server never measured it.
func audioLength(length time.Duration) string {
	if length <= 0 {
		return ""
	}

	return fmt.Sprintf("%d:%02d", int(length.Minutes()), int(length.Seconds())-60*int(length.Minutes()))
}

// getCallInput is one recorded call, named by its identifier.
type getCallInput struct {
	CallID uint64 `json:"callId" jsonschema:"the identifier a search answered with"`
}

// callDetail is one recorded call without its recording, naming where the recording is served.
type callDetail struct {
	ID            uint64      `json:"id"`
	System        uint        `json:"system"`
	SystemLabel   string      `json:"systemLabel,omitempty"`
	Talkgroup     uint        `json:"talkgroup"`
	TalkgroupName string      `json:"talkgroupName,omitempty"`
	DateTime      string      `json:"dateTime"`
	AudioName     string      `json:"audioName,omitempty"`
	AudioType     string      `json:"audioType,omitempty"`
	AudioLength   string      `json:"audioLength,omitempty"`
	AudioURL      string      `json:"audioUrl"`
	ChannelType   string      `json:"channelType,omitempty"`
	Delayed       bool        `json:"delayed"`
	Patches       []uint      `json:"patches,omitempty"`
	Site          uint        `json:"site,omitempty"`
	Units         []unitHeard `json:"units,omitempty"`
}

// unitHeard is one unit heard inside the recording, named by where in it it was heard.
type unitHeard struct {
	Unit   uint   `json:"unit"`
	Offset string `json:"offset,omitempty" jsonschema:"where in the recording this unit was heard, as mm:ss"`
}

func callAnswer(call domain.Call) callDetail {
	answer := callDetail{
		ID:            call.ID,
		System:        call.System.SystemRef,
		SystemLabel:   call.System.Label,
		Talkgroup:     call.Talkgroup.TalkgroupRef,
		TalkgroupName: call.Talkgroup.Name,
		DateTime:      call.Timestamp.Format(time.RFC3339),
		AudioName:     call.AudioFilename,
		AudioType:     call.AudioMime,
		AudioLength:   audioLength(call.AudioLength),
		AudioURL:      "/api/calls/" + strconv.FormatUint(call.ID, 10) + "/audio",
		ChannelType:   call.Meta.ChannelType,
		Delayed:       call.Delayed,
		Patches:       call.PatchTalkgroupRefs,
		Site:          call.SiteRef,
	}

	for _, unit := range call.Units {
		answer.Units = append(answer.Units, unitHeard{
			Unit:   unit.UnitRef,
			Offset: audioLength(time.Duration(float64(unit.Offset) * float64(time.Second))),
		})
	}

	return answer
}

// listSystemsInput is the discovery step's empty argument list: what a
// caller may see is decided by the credential, not an argument.
type listSystemsInput struct{}

// systemsView is the scoped projection an assistant names its filters with.
type systemsView struct {
	Systems []systemView `json:"systems"`
}

type systemView struct {
	SystemRef  uint            `json:"systemRef"`
	Label      string          `json:"label,omitempty"`
	Talkgroups []talkgroupView `json:"talkgroups,omitempty"`
	Sites      []siteView      `json:"sites,omitempty"`
	Units      []unitView      `json:"units,omitempty"`
}

type talkgroupView struct {
	TalkgroupRef uint   `json:"talkgroupRef"`
	Label        string `json:"label,omitempty"`
	Name         string `json:"name,omitempty"`
	Frequency    uint   `json:"frequency,omitempty"`
}

type siteView struct {
	SiteRef uint   `json:"siteRef"`
	Label   string `json:"label,omitempty"`
}

type unitView struct {
	UnitRef uint   `json:"unitRef"`
	Label   string `json:"label,omitempty"`
}

func systemsAnswer(view domain.Systems) systemsView {
	answer := systemsView{Systems: []systemView{}}

	for _, system := range view {
		shown := systemView{SystemRef: system.SystemRef, Label: system.Label}

		for _, talkgroup := range system.Talkgroups {
			shown.Talkgroups = append(shown.Talkgroups, talkgroupView{
				TalkgroupRef: talkgroup.TalkgroupRef,
				Label:        talkgroup.Label,
				Name:         talkgroup.Name,
				Frequency:    talkgroup.Frequency,
			})
		}

		for _, site := range system.Sites {
			shown.Sites = append(shown.Sites, siteView{SiteRef: site.SiteRef, Label: site.Label})
		}

		for _, unit := range system.Units {
			shown.Units = append(shown.Units, unitView{UnitRef: unit.UnitRef, Label: unit.Label})
		}

		answer.Systems = append(answer.Systems, shown)
	}

	return answer
}

// readOnly is the annotation set every read capability carries: it changes nothing, it repeats
// harmlessly, and its world is this deployment.
func readOnly() *mcp.ToolAnnotations {
	destructive := false
	closed := false

	return &mcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: &destructive,
		IdempotentHint:  true,
		OpenWorldHint:   &closed,
	}
}

// refused is the protocol's own "this did not work" answer, carrying the reason as text so an
// assistant reads it and not a transport failure.
func refused(reason string) *mcp.CallToolResult {
	result := &mcp.CallToolResult{}
	result.SetError(fmt.Errorf("%s", reason))

	return result
}

// newRefusal is a capability's refusal as the protocol reports it. A tool has a failure
// answer a client shows as an answer; a resource read has none, so a resource that
// refuses answers the protocol's error carrying the same sentence.
func newRefusal(reason string) error {
	return fmt.Errorf("%s", reason)
}

// scopeOf is the scope a caller's view is restricted by, which is nothing at all for a caller
// whose view is not restricted: the whole history, and the whole configuration.
func scopeOf(who Caller) domain.AccessScope {
	if !who.Scoped {
		return domain.AccessScope{}
	}

	return who.Scope
}
