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
// It is a query and so it writes nothing. The legacy server's log endpoint does write — it
// records its own failures — and that belongs to the REST layer (T041), which is where a failure
// to answer becomes a server-log entry and a status code.
type SearchLogsHandler struct {
	logs ports.LogRepository
}

// NewSearchLogsHandler builds the handler from the log store.
func NewSearchLogsHandler(logs ports.LogRepository) *SearchLogsHandler {
	return &SearchLogsHandler{logs: logs}
}

// Handle returns the matching entries and the total the criteria matches.
func (handler *SearchLogsHandler) Handle(ctx context.Context, query SearchLogsQuery) (ports.LogSearchPage, error) {
	return handler.logs.SearchLogs(ctx, query.Search)
}
