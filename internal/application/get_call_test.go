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
	"errors"
	"testing"

	"rdio-scanner/internal/application/ports"
)

func TestGetCallByIdQueryReturnsTheRecordedCall(t *testing.T) {
	fixture := newHistoryFixture()
	fixture.store(7, 4, 1514000, historyTime)

	call, err := fixture.get.Handle(context.Background(), GetCallByIdQuery{ID: 7})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if call.ID != 7 {
		t.Errorf("Handle returned call %d, want the call that was asked for", call.ID)
	}
}

func TestGetCallByIdQueryReportsAMissingCall(t *testing.T) {
	fixture := newHistoryFixture()

	_, err := fixture.get.Handle(context.Background(), GetCallByIdQuery{ID: 7})

	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Handle error = %v, want ports.ErrNotFound, which is what the CAL fetch answers with", err)
	}
}
