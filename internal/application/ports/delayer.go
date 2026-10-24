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

// CallDelayer is the part of the server that takes responsibility for the calls that
// have a delay to wait out. It is a port rather than an ingest-path rule because
// holding a call back needs state that survives a restart and something that
// remembers to deliver it later; the use case only asks whether it still owns the call.
type CallDelayer interface {
	// Hold reports whether a call is still being held back at the moment it is asked,
	// and takes it when it is. A held call is delivered later by whoever watches the
	// delayer; an unheld call is the caller's to deliver. A failed hold is neither:
	// the call was not delivered and nothing will deliver it, so the failure must be
	// reported.
	Hold(ctx context.Context, call domain.Call) (held bool, err error)
}
