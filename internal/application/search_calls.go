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

package application

import (
	"context"

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

// SearchCallsQuery asks for one page of the call history.
type SearchCallsQuery struct {
	// Criteria are the filters the listener submitted, already parsed into values.
	Criteria domain.CallSearch

	// Scope is what the listener is allowed to see. A listener with no scope named sees
	// the whole history, which is how an anonymous listener on an unrestricted server
	// behaves.
	Scope domain.AccessScope
}

// SearchCallsQueryHandler resolves a history request into a page of call summaries.
//
// Everything the legacy server folded into a SQL string is resolved here instead: the
// paging, the sort direction, the day bounds, the caller's scope, and the group and tag
// labels expanded into the concrete systems and talkgroups they stand for. The repository
// receives resolved criteria and turns them into a parameterized query.
type SearchCallsQueryHandler struct {
	calls   ports.CallRepository
	systems ports.SystemRepository
	groups  ports.GroupRepository
	tags    ports.TagRepository
}

// NewSearchCallsQueryHandler builds the handler from its ports.
func NewSearchCallsQueryHandler(
	calls ports.CallRepository,
	systems ports.SystemRepository,
	groups ports.GroupRepository,
	tags ports.TagRepository,
) *SearchCallsQueryHandler {
	return &SearchCallsQueryHandler{
		calls:   calls,
		systems: systems,
		groups:  groups,
		tags:    tags,
	}
}

// Handle returns the matching page of call history.
func (handler *SearchCallsQueryHandler) Handle(ctx context.Context, query SearchCallsQuery) (ports.CallSearchPage, error) {
	search := ports.CallSearch{
		Scope:        query.Scope,
		Limit:        query.Criteria.Limit,
		Offset:       query.Criteria.Offset,
		Ascending:    query.Criteria.Ascending(),
		SystemRef:    query.Criteria.System,
		TalkgroupRef: query.Criteria.Talkgroup,
	}

	if from, to, ok := query.Criteria.DateWindow(); ok {
		search.From = from
		search.To = to
	}

	if query.Criteria.HasGroup() || query.Criteria.HasTag() {
		systems, err := handler.systems.AllSystems(ctx)
		if err != nil {
			return ports.CallSearchPage{}, err
		}

		tags, err := handler.tags.AllTags(ctx)
		if err != nil {
			return ports.CallSearchPage{}, err
		}

		// A group or tag label stands for the talkgroups the *listener* can see, not for
		// every talkgroup the server knows: the legacy server builds these filters from the
		// configuration it already sent that listener.
		view := systems.Scoped(query.Scope, tags)

		if query.Criteria.HasGroup() {
			groups, err := handler.groups.AllGroups(ctx)
			if err != nil {
				return ports.CallSearchPage{}, err
			}

			search.GroupTalkgroups = domain.ScopeForGroup(groups, view, query.Criteria.Group)
		}

		if query.Criteria.HasTag() {
			search.TagTalkgroups = domain.ScopeForTag(tags, view, query.Criteria.Tag)
		}
	}

	return handler.calls.SearchCalls(ctx, search)
}
