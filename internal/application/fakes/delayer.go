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

package fakes

import (
	"context"
	"time"

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

// Delayer is the fake call delayer the use cases are tested against.
//
// It applies the same rule the real one applies — a call is held when it names a delay and the
// moment it happened plus that delay has not arrived yet — so that a use case tested against it is
// tested against whether a call would actually be held back, and not against a stub that always
// says yes.
type Delayer struct {
	// Now is the moment a hold is measured against.
	Now time.Time

	// Held records the calls this delayer took, oldest first. A call that appears here was not
	// delivered by the use case that handed it over.
	Held []domain.Call

	// Waiting names the calls to hold or not hold whatever their delay says, keyed by call identity.
	// A test that is only interested in what a use case does with a held call uses this; a test of
	// the rule itself belongs in the domain.
	Waiting map[uint64]bool

	// Failure makes every hold fail with this error, which is how what happens to a call whose hold
	// cannot be written down is tested.
	Failure error
}

var _ ports.CallDelayer = (*Delayer)(nil)

// Hold records the calls it takes, so a test can see which calls a use case delivered and which it
// handed over.
func (delayer *Delayer) Hold(_ context.Context, call domain.Call) (bool, error) {
	if delayer.Failure != nil {
		return false, delayer.Failure
	}

	held, named := delayer.Waiting[call.ID]

	if !named {
		held = call.IsWaiting(delayer.Now)
	}

	if !held {
		return false, nil
	}

	delayer.Held = append(delayer.Held, call)

	return true, nil
}
