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

// SearchLogsQuery asks for the server's own log view: the entries the criteria match, and how
// many entries match in total so the view can page through them.
//
// The criteria arrive already parsed. Reading a date out of a payload, deciding what an
// unparseable date means, and what an absent limit means is the domain's job
// (domain.ParseLogSearch, T011); this query is what the caller does once it knows them.
type SearchLogsQuery struct {
	Search domain.LogSearch
}

// SearchLogsHandler reads the log view.
//
// It is a query: it changes nothing about what the log holds. The one thing it does is record its own
// failure, which is what the legacy server has always done with a log search it could not answer — the
// notice goes into the very view the search was asked for. That is not the query mutating the log view's
// contents; it is the server saying out loud that it could not answer, and an administrator shown an
// empty log view has no other way to learn that the log view is broken.
type SearchLogsHandler struct {
	logs  ports.LogRepository
	clock ports.Clock
}

// NewSearchLogsHandler builds the handler from the log store and the clock the notice is stamped with.
func NewSearchLogsHandler(logs ports.LogRepository, clock ports.Clock) *SearchLogsHandler {
	return &SearchLogsHandler{logs: logs, clock: clock}
}

// Handle returns the matching entries and the total the criteria matches.
//
// A search the store cannot answer is recorded with the failure's own text and nothing added to it, which
// is how the legacy server recorded it: the failure already names the statement that failed, and a prefix
// would only make the two servers' log lines differ.
func (handler *SearchLogsHandler) Handle(ctx context.Context, query SearchLogsQuery) (ports.LogSearchPage, error) {
	page, err := handler.logs.SearchLogs(ctx, query.Search)
	if err != nil {
		appendServerLog(ctx, handler.logs, handler.clock, domain.LogLevelError, err.Error())

		return ports.LogSearchPage{}, err
	}

	return page, nil
}
