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

package ports

import (
	"context"

	"rdio-scanner/internal/domain"
)

// Dirwatchers are the directory watchers the server keeps running to ingest calls from
// watched folders.
//
// Saving a configuration brackets itself around these watchers: they are stopped before
// anything changes and started again afterwards, because a watcher that fires in the middle
// of a save is reading a configuration that is half replaced — a talkgroup that is about to
// be deleted, a system that does not exist yet.
//
// Start takes the directories to watch rather than going to fetch them, because *which*
// directories end up watched after a save is a decision the save makes, and the legacy
// server's decision is not always the obvious one: a whole-configuration save that does not
// name the dirwatch section leaves the server watching nothing until it restarts.
type Dirwatchers interface {
	// Stop ends the running watchers and leaves nothing watching.
	Stop(ctx context.Context)

	// Start begins watching exactly the directories it is given.
	Start(ctx context.Context, watches domain.Dirwatches)
}

// AdministratorSessions are the open administrator connections that exist so the web app
// learns about configuration changes as they happen, rather than on its next reload.
type AdministratorSessions interface {
	// NotifyConfigurationChanged tells every open administrator connection what the
	// configuration now is.
	//
	// It carries the whole configuration, which is what the administrator's own view shows:
	// every section, not the four a listener is shown.
	NotifyConfigurationChanged(ctx context.Context, configuration domain.Configuration)
}
