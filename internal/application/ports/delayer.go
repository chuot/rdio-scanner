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

	"rdio-scanner/server-ng/internal/domain"
)

// CallDelayer is the part of the server that takes responsibility for the calls that have a delay
// to wait out.
//
// It is a port rather than a rule the ingest path applies itself because holding a call back is not
// a decision that can be made and forgotten: the call has to be kept somewhere that survives a
// restart, and something has to remember to deliver it later. Both of those belong outside the use
// case, and the use case only needs to know whether it still owns the call.
type CallDelayer interface {
	// Hold reports whether a call is still being held back at the moment it is asked, and takes it
	// when it is.
	//
	// A call that is held is not delivered now, and the caller must not deliver it: whoever is
	// watching the delayer delivers it when the delay has run out. A call that is not held is the
	// caller's to deliver.
	//
	// A hold that fails is neither of those. The call was not delivered and nothing is going to
	// deliver it, which is what the failure has to be reported for.
	Hold(ctx context.Context, call domain.Call) (held bool, err error)
}
