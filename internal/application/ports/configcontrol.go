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

package ports

import (
	"context"

	"rdio-scanner/internal/domain"
)

// Dirwatchers are the directory watchers the server keeps running to ingest calls from
// watched folders. A configuration save brackets itself around them: stopped before
// anything changes, started again afterwards, because a watcher that fires mid-save
// reads a half-replaced configuration. Start takes the directories to watch rather
// than fetching them: which directories end up watched is a decision the save makes.
type Dirwatchers interface {
	// Stop ends the running watchers and leaves nothing watching.
	Stop(ctx context.Context)

	// Start begins watching exactly the directories it is given.
	Start(ctx context.Context, watches domain.Dirwatches)
}

// AdministratorSessions are the open administrator connections that exist so the
// administration interface learns about configuration changes as they happen, rather
// than on its next reload.
type AdministratorSessions interface {
	// NotifyConfigurationChanged tells every open administrator connection what the
	// configuration now is: every section, which is what the administrator's own view
	// shows, not the four a listener is shown.
	NotifyConfigurationChanged(ctx context.Context, configuration domain.Configuration)
}
