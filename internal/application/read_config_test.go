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
	"errors"
	"testing"
)

func TestGetConfigQueryReturnsEverySection(t *testing.T) {
	fixture := newConfigFixture()

	configuration, err := fixture.read()
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(configuration.Systems) != 1 || configuration.Systems[0].SystemRef != 4 {
		t.Errorf("systems = %v, want the stored systems", configuration.Systems)
	}

	if len(configuration.Groups) != 1 || configuration.Groups[0].Label != "Fire" {
		t.Errorf("groups = %v, want the stored groups", configuration.Groups)
	}

	if len(configuration.Tags) != 1 || configuration.Tags[0].Label != "Dispatch" {
		t.Errorf("tags = %v, want the stored tags", configuration.Tags)
	}

	if len(configuration.Dirwatches) != 2 {
		t.Errorf("dirwatches = %v, want the two stored watched directories", configuration.Dirwatches)
	}

	if configuration.Options.MaxClients == 0 {
		t.Errorf("options = %+v, want the stored options", configuration.Options)
	}
}

func TestGetConfigQueryReportsAFailingStore(t *testing.T) {
	fixture := newConfigFixture()
	fixture.tags.Err = errors.New("no such table: tags")

	_, err := fixture.read()

	if err == nil {
		t.Error("a store that cannot be read must reach the caller rather than report a partial configuration")
	}
}
