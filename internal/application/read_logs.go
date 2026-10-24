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

package application

import (
	"context"
	"fmt"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// SearchLogsQuery asks for the server's own log view: the entries the criteria match, and
// how many match in total so the view can page. The criteria arrive already parsed, by
// domain.ParseLogSearch.
type SearchLogsQuery struct {
	Search domain.LogSearch
}

// SearchLogsHandler reads the log view; a failed search is recorded as a notice in the
// same view, which is how an administrator learns the log view is broken rather than
// seeing an empty one.
type SearchLogsHandler struct {
	logs  ports.LogRepository
	clock ports.Clock
}

// NewSearchLogsHandler builds the handler from the log store and the clock the notice is stamped with.
func NewSearchLogsHandler(logs ports.LogRepository, clock ports.Clock) *SearchLogsHandler {
	return &SearchLogsHandler{logs: logs, clock: clock}
}

// Handle returns the matching entries and the total the criteria match. A search the
// store cannot answer is recorded as a notice in the same view; the notice text names the
// failed statement, and its fixed sentence is what the log view filters on.
func (handler *SearchLogsHandler) Handle(ctx context.Context, query SearchLogsQuery) (ports.LogSearchPage, error) {
	page, err := handler.logs.SearchLogs(ctx, query.Search)
	if err != nil {
		appendServerLog(ctx, handler.logs, handler.clock, domain.LogLevelError, domain.LogSourceLogs, fmt.Sprintf("the log search failed: %v", err))

		return ports.LogSearchPage{}, err
	}

	return page, nil
}
