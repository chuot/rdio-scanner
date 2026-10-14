// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without the implied warranty of
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
	"errors"
	"testing"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

func newSubSectionFixture() *configFixture {
	fixture := newConfigFixture()

	fixture.systems.Stored = domain.Systems{
		{
			ID: 1, SystemRef: 4, Label: "Grand Metro",
			Sites:      domain.Sites{{ID: 10, SiteRef: 101, Label: "Metro East", Order: 1}},
			Talkgroups: domain.Talkgroups{{ID: 20, TalkgroupRef: 1514000, Label: "Ground", Order: 1}},
			Units:      domain.Units{{ID: 30, UnitRef: 1001, Label: "Car 1", Order: 1}},
		},
		{
			ID: 2, SystemRef: 5, Label: "Harbour",
			Sites: domain.Sites{{ID: 11, SiteRef: 102, Label: "Harbour West", Order: 1}},
		},
	}

	fixture.subsection = NewUpdateSystemSubSectionHandler(ConfigurationPorts{
		ConfigurationRepositories: ConfigurationRepositories{
			Accesses:    fixture.accesses,
			Apikeys:     fixture.apikeys,
			Dirwatches:  fixture.dirwatches,
			Downstreams: fixture.downstreams,
			Groups:      fixture.groups,
			Options:     fixture.options,
			Systems:     fixture.systems,
			Tags:        fixture.tags,
		},
		Logs:        fixture.logs,
		Hub:         fixture.hub,
		Admin:       fixture.admin,
		Dirwatchers: fixture.watchers,
		Clock:       fixture.clock,
	})

	return fixture
}

func (fixture *configFixture) subSectionLogMessages() []string {
	messages := []string{}

	for _, entry := range fixture.logs.Stored {
		messages = append(messages, entry.Message)
	}

	return messages
}

func (fixture *configFixture) saveSubSection(systemID uint64, level SystemSubSection, sites domain.Sites, talkgroups domain.Talkgroups, units domain.Units) (UpdateConfigurationResult, error) {
	return fixture.subsection.Handle(context.Background(), UpdateSystemSubSectionCommand{
		SystemID:   systemID,
		Level:      level,
		Sites:      sites,
		Talkgroups: talkgroups,
		Units:      units,
	})
}

func TestIsSystemSubSectionKnowsTheThreeLevels(t *testing.T) {
	for _, name := range []string{"sites", "talkgroups", "units"} {
		if !IsSystemSubSection(name) {
			t.Errorf("IsSystemSubSection(%q) = false, want true", name)
		}
	}

	for _, name := range []string{"systems", "groups", "tags", ""} {
		if IsSystemSubSection(name) {
			t.Errorf("IsSystemSubSection(%q) = true, want false", name)
		}
	}
}

func TestUpdateSystemSubSectionSavesTheSitesOfOneSystem(t *testing.T) {
	fixture := newSubSectionFixture()

	sites := domain.Sites{
		{SiteRef: 101, Label: "Metro East"},
		{SiteRef: 102, Label: "Metro West"},
	}

	result, err := fixture.saveSubSection(1, SitesSubSection, sites, nil, nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if result.Outcome != ConfigurationSaved {
		t.Errorf("outcome = %q, want the save to have worked", result.Outcome)
	}

	if fixture.systems.SubSectionLevel != "sites" || fixture.systems.SubSectionSystemID != 1 {
		t.Errorf("the store was asked for level %q of system %d, want the sites of system 1",
			fixture.systems.SubSectionLevel, fixture.systems.SubSectionSystemID)
	}

	if len(fixture.systems.SubSectionSaved) != 2 {
		t.Errorf("the store saved %d sites, want the two submitted rows", len(fixture.systems.SubSectionSaved))
	}

	if len(fixture.hub.BroadcastConfigs) == 0 {
		t.Error("the save did not broadcast the configuration to listeners")
	}

	if len(fixture.admin.Announced) == 0 {
		t.Error("the save did not notify the open administrator connections")
	}

	if !fixture.loggedAs(domain.LogSourceConfig, "the sites configuration of system 4 changed") {
		t.Errorf("logs = %v, want the announcement naming the level and the system's reference number, the number the operator reads and types, not the identity the store gave the row", fixture.subSectionLogMessages())
	}
}

func TestUpdateSystemSubSectionSavesTheTalkgroupsOfOneSystem(t *testing.T) {
	fixture := newSubSectionFixture()

	talkgroups := domain.Talkgroups{
		{TalkgroupRef: 1514000, Label: "Ground"},
		{TalkgroupRef: 1514100, Label: "Dispatch 2"},
	}

	result, err := fixture.saveSubSection(1, TalkgroupsSubSection, nil, talkgroups, nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if result.Outcome != ConfigurationSaved {
		t.Errorf("outcome = %q, want the save to have worked", result.Outcome)
	}

	if fixture.systems.SubSectionLevel != "talkgroups" {
		t.Errorf("level = %q, want the talkgroups method to have been used", fixture.systems.SubSectionLevel)
	}

	if len(fixture.systems.SubSectionSaved) != 2 {
		t.Errorf("the store saved %d talkgroups, want the two submitted rows", len(fixture.systems.SubSectionSaved))
	}
}

func TestUpdateSystemSubSectionSavesTheUnitsOfOneSystem(t *testing.T) {
	fixture := newSubSectionFixture()

	units := domain.Units{
		{UnitRef: 1001, Label: "Car 1"},
		{UnitRef: 1002, Label: "Car 2"},
	}

	result, err := fixture.saveSubSection(2, UnitsSubSection, nil, nil, units)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if result.Outcome != ConfigurationSaved {
		t.Errorf("outcome = %q, want the save to have worked", result.Outcome)
	}

	if fixture.systems.SubSectionLevel != "units" || fixture.systems.SubSectionSystemID != 2 {
		t.Errorf("the store was asked for level %q of system %d, want the units of system 2",
			fixture.systems.SubSectionLevel, fixture.systems.SubSectionSystemID)
	}

	if len(fixture.systems.SubSectionSaved) != 2 {
		t.Errorf("the store saved %d units, want the two submitted rows", len(fixture.systems.SubSectionSaved))
	}
}

func TestUpdateSystemSubSectionReportsAnUnknownSystem(t *testing.T) {
	fixture := newSubSectionFixture()

	result, err := fixture.saveSubSection(99, SitesSubSection, domain.Sites{{SiteRef: 1, Label: "Nowhere"}}, nil, nil)

	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("error = %v, want ports.ErrNotFound for a system the server does not store", err)
	}

	if result.Outcome != ConfigurationSaveFailed {
		t.Errorf("outcome = %q, want the save to have failed", result.Outcome)
	}

	if len(fixture.systems.SubSectionSaved) != 0 {
		t.Error("an unknown system must not reach the store")
	}

	if len(fixture.hub.BroadcastConfigs) != 0 {
		t.Error("a save that did not work must not broadcast a configuration")
	}

	if len(fixture.admin.Announced) != 0 {
		t.Error("a save that did not work must not notify the administrator connections")
	}
}

func TestUpdateSystemSubSectionReportsAPersistenceFailure(t *testing.T) {
	fixture := newSubSectionFixture()

	fixture.systems.SaveErr = errors.New("no such table: sites")

	result, err := fixture.saveSubSection(1, SitesSubSection, domain.Sites{{SiteRef: 101, Label: "Metro East"}}, nil, nil)

	if err == nil {
		t.Error("a store that cannot save must reach the caller")
	}

	if result.Outcome != ConfigurationSaveFailed {
		t.Errorf("outcome = %q, want the save to have failed", result.Outcome)
	}

	if !fixture.loggedAs(domain.LogSourceConfig, "the configuration could not be saved") {
		t.Errorf("logs = %v, want the failure recorded", fixture.subSectionLogMessages())
	}

	if len(fixture.hub.BroadcastConfigs) != 0 {
		t.Error("a save that did not work must not broadcast a configuration")
	}

	if len(fixture.watchers.Starts) == 0 {
		t.Error("the directory watchers must be restarted even when the save failed")
	}
}

// The system the save was aimed at is deleted under the save: the level is stored, and the
// announcement runs after it, with the system no longer in the store. The line says the level
// changed and names no identifier, because the number it would name belongs to a row that is gone.
func TestUpdateSystemSubSectionNamesNoSystemWhenTheSaveOutlivesTheSystem(t *testing.T) {
	fixture := newSubSectionFixture()

	fixture.systems.SubSectionDeletesSystem = true

	result, err := fixture.saveSubSection(1, SitesSubSection, domain.Sites{{SiteRef: 101, Label: "Metro East"}}, nil, nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if result.Outcome != ConfigurationSaved {
		t.Errorf("outcome = %q, want the save to have worked", result.Outcome)
	}

	if !fixture.loggedAs(domain.LogSourceConfig, "the sites configuration changed") {
		t.Errorf("logs = %v, want the announcement naming the level and no identifier, the system it was aimed at being gone from the store", fixture.subSectionLogMessages())
	}
}
