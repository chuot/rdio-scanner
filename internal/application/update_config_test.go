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
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

var configTime = time.Date(2026, 5, 9, 11, 45, 0, 0, time.UTC)

func newConfigFixture() *configFixture {
	fixture := &configFixture{
		accesses:    &fakes.Accesses{},
		apikeys:     &fakes.Apikeys{},
		dirwatches:  &fakes.Dirwatches{},
		downstreams: &fakes.Downstreams{},
		groups:      &fakes.Groups{},
		options:     &fakes.Options{Stored: domain.DefaultOptions()},
		systems:     &fakes.Systems{},
		tags:        &fakes.Tags{},
		logs:        &fakes.Logs{},
		hub:         fakes.NewHub(),
		admin:       fakes.NewAdministratorSessions(),
		watchers:    fakes.NewDirwatchers(),
		clock:       fakes.NewClock(configTime),
	}

	fixture.dirwatches.Stored = domain.Dirwatches{
		domain.Dirwatch{ID: 1, Directory: "/spool/live"},
		domain.Dirwatch{ID: 2, Directory: "/spool/finished"},
	}

	fixture.systems.Stored = domain.Systems{{ID: 1, SystemRef: 4, Label: "Grand Metro"}}
	fixture.groups.Stored = domain.Groups{{ID: 1, Label: "Fire"}}
	fixture.tags.Stored = domain.Tags{{ID: 10, Label: "Dispatch"}}

	repos := ConfigurationRepositories{
		Accesses:    fixture.accesses,
		Apikeys:     fixture.apikeys,
		Dirwatches:  fixture.dirwatches,
		Downstreams: fixture.downstreams,
		Groups:      fixture.groups,
		Options:     fixture.options,
		Systems:     fixture.systems,
		Tags:        fixture.tags,
	}

	ports := ConfigurationPorts{
		ConfigurationRepositories: repos,
		Logs:                      fixture.logs,
		Hub:                       fixture.hub,
		Admin:                     fixture.admin,
		Dirwatchers:               fixture.watchers,
		Clock:                     fixture.clock,
	}

	fixture.get = NewGetConfigHandler(repos)
	fixture.update = NewUpdateConfigurationHandler(ports)
	fixture.updateSection = NewUpdateConfigurationSectionHandler(ports)

	return fixture
}

type configFixture struct {
	accesses    *fakes.Accesses
	apikeys     *fakes.Apikeys
	dirwatches  *fakes.Dirwatches
	downstreams *fakes.Downstreams
	groups      *fakes.Groups
	options     *fakes.Options
	systems     *fakes.Systems
	tags        *fakes.Tags
	logs        *fakes.Logs
	hub         *fakes.Hub
	admin       *fakes.AdministratorSessions
	watchers    *fakes.Dirwatchers
	clock       *fakes.Clock

	get           *GetConfigHandler
	update        *UpdateConfigurationHandler
	updateSection *UpdateConfigurationSectionHandler
	subsection    *UpdateSystemSubSectionHandler
}

func (fixture *configFixture) save(changes ConfigurationChanges) (UpdateConfigurationResult, error) {
	return fixture.update.Handle(context.Background(), UpdateConfigurationCommand{Changes: changes})
}

func (fixture *configFixture) saveSection(section ConfigSection, changes ConfigurationChanges) (UpdateConfigurationResult, error) {
	return fixture.updateSection.Handle(context.Background(), UpdateConfigurationSectionCommand{Section: section, Changes: changes})
}

func (fixture *configFixture) read() (domain.Configuration, error) {
	return fixture.get.Handle(context.Background(), GetConfigQuery{})
}

// loggedAs reports whether the server wrote an entry that names that part of the server and says the
// fragment. The source is part of the assertion: a line that says the right thing and is filed under
// nothing is the state this feature exists to remove.
func (fixture *configFixture) loggedAs(source domain.LogSource, fragment string) bool {
	for _, entry := range fixture.logs.Stored {
		if entry.Source == source && strings.Contains(entry.Message, fragment) {
			return true
		}
	}

	return false
}

func TestIsConfigSectionKnowsTheEightSections(t *testing.T) {
	for _, name := range []string{
		"access", "apikeys", "dirwatch", "downstreams", "groups", "options", "systems", "tags",
	} {
		if !IsConfigSection(name) {
			t.Errorf("IsConfigSection(%q) = false, want true", name)
		}
	}

	// "dirwatch" is the section name; the plural is what the stored collection is called, and
	// an administrator asking for "dirwatches" is asking for something the server has no such
	// section for.
	//
	// "dashboard" is a section of the administrator's page and not of the configuration: it reads
	// the server and saves nothing, so it has no list to replace and no path under the
	// configuration to answer.
	for _, name := range []string{"", "dirwatches", "delayed", "calls", "Access", "dashboard"} {
		if IsConfigSection(name) {
			t.Errorf("IsConfigSection(%q) = true, want false", name)
		}
	}
}

func TestUpdateConfigurationSavesEveryNamedSection(t *testing.T) {
	fixture := newConfigFixture()

	accesses := domain.Accesses{{ID: 1, Code: "dispatch-code", Ident: "dispatch"}}
	apikeys := domain.Apikeys{{ID: 2, Key: "recorder-key"}}
	groups := domain.Groups{{ID: 3, Label: "EMS"}}
	tags := domain.Tags{{ID: 11, Label: "Interop"}}
	systems := domain.Systems{{ID: 4, SystemRef: 5, Label: "Harbour"}}
	downstreams := domain.Downstreams{{ID: 5, URL: "https://other.example/api/call-upload"}}
	dirwatches := domain.Dirwatches{{ID: 6, Directory: "/spool/other"}}
	options := domain.DefaultOptions()
	options.PruneDays = 14

	result, err := fixture.save(ConfigurationChanges{
		Access:      &accesses,
		Apikeys:     &apikeys,
		Dirwatches:  &dirwatches,
		Downstreams: &downstreams,
		Groups:      &groups,
		Options:     &options,
		Systems:     &systems,
		Tags:        &tags,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != ConfigurationSaved {
		t.Fatalf("outcome = %q, want %q", result.Outcome, ConfigurationSaved)
	}

	want := []ConfigSection{
		AccessSection, ApikeysSection, DirwatchSection, DownstreamsSection,
		GroupsSection, OptionsSection, SystemsSection, TagsSection,
	}

	if len(result.Saved) != len(want) {
		t.Fatalf("saved = %v, want all eight sections in save order", result.Saved)
	}

	for i, section := range want {
		if result.Saved[i] != section {
			t.Errorf("saved = %v, want the sections saved in the order %v", result.Saved, want)
			break
		}
	}

	if len(fixture.accesses.Stored) != 1 || len(fixture.apikeys.Stored) != 1 ||
		len(fixture.groups.Stored) != 1 || len(fixture.tags.Stored) != 1 ||
		len(fixture.systems.Stored) != 1 || len(fixture.downstreams.Stored) != 1 ||
		len(fixture.dirwatches.Stored) != 1 || fixture.options.Stored.PruneDays != 14 {
		t.Errorf("the stores were not all replaced: accesses=%v apikeys=%v groups=%v tags=%v systems=%v downstreams=%v dirwatches=%v pruneDays=%d",
			fixture.accesses.Stored, fixture.apikeys.Stored, fixture.groups.Stored, fixture.tags.Stored,
			fixture.systems.Stored, fixture.downstreams.Stored, fixture.dirwatches.Stored, fixture.options.Stored.PruneDays)
	}
}

func TestUpdateConfigurationLeavesAnUnnamedSectionAlone(t *testing.T) {
	fixture := newConfigFixture()

	groups := domain.Groups{{ID: 3, Label: "EMS"}}

	result, err := fixture.save(ConfigurationChanges{Groups: &groups})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(result.Saved) != 1 || result.Saved[0] != GroupsSection {
		t.Fatalf("saved = %v, want only the groups section", result.Saved)
	}

	if len(fixture.systems.Stored) != 1 || fixture.systems.Stored[0].SystemRef != 4 {
		t.Errorf("systems = %v, want the systems left exactly as they were", fixture.systems.Stored)
	}

	if len(fixture.tags.Stored) != 1 {
		t.Errorf("tags = %v, want the tags left exactly as they were", fixture.tags.Stored)
	}
}

// Naming a section but leaving it empty replaces it with nothing; saying nothing about it
// leaves it alone.
func TestUpdateConfigurationReplacesASectionWithNothingWhenItIsNamedButEmpty(t *testing.T) {
	fixture := newConfigFixture()

	empty := domain.Groups{}

	result, err := fixture.save(ConfigurationChanges{Groups: &empty})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(result.Saved) != 1 {
		t.Fatalf("saved = %v, want the groups section reported as saved", result.Saved)
	}

	if len(fixture.groups.Stored) != 0 {
		t.Errorf("groups = %v, want the section replaced with nothing", fixture.groups.Stored)
	}

	if len(fixture.tags.Stored) != 1 {
		t.Errorf("tags = %v, want a section that was not named left alone", fixture.tags.Stored)
	}
}

func TestUpdateConfigurationKeepsGoingWhenASectionCannotBeStored(t *testing.T) {
	fixture := newConfigFixture()
	fixture.apikeys.SaveErr = errors.New("constraint failed")

	apikeys := domain.Apikeys{{ID: 2, Key: "recorder-key"}}
	groups := domain.Groups{{ID: 3, Label: "EMS"}}

	result, err := fixture.save(ConfigurationChanges{Apikeys: &apikeys, Groups: &groups})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != ConfigurationSaved {
		t.Errorf("outcome = %q, want %q: a whole-configuration save is not one transaction", result.Outcome, ConfigurationSaved)
	}

	if len(result.Failed) != 1 || result.Failed[0] != ApikeysSection {
		t.Errorf("failed = %v, want the apikeys section reported as not stored", result.Failed)
	}

	if len(result.Saved) != 1 || result.Saved[0] != GroupsSection {
		t.Errorf("saved = %v, want the later section saved despite the earlier one failing", result.Saved)
	}

	if len(fixture.groups.Stored) != 1 || fixture.groups.Stored[0].Label != "EMS" {
		t.Errorf("groups = %v, want the sections after a failure still saved", fixture.groups.Stored)
	}

	if !fixture.loggedAs(domain.LogSourceConfig, "the configuration could not be saved: constraint failed") {
		t.Errorf("logs = %v, want the failure said as what happened, with the failure's own text after it", fixture.logs.Stored)
	}
}

// A whole-configuration save names a section the store refused in `Failed` and a section the
// store holds while the announcement failed separately; the distinction lives in the
// configuration-save-failure maintainer note. The announcement here reads a configuration
// through a repository that cannot answer its read — `tags`, which this save never writes.
func TestUpdateConfigurationReportsTheSectionsItStoredWhenTheAnnouncementFailed(t *testing.T) {
	fixture := newConfigFixture()

	fixture.tags.Err = errors.New("no such table: tags")

	groups := domain.Groups{{ID: 3, Label: "EMS"}}

	result, err := fixture.save(ConfigurationChanges{Groups: &groups})
	if err == nil {
		t.Fatal("Handle = nil for a configuration the server could not announce, want the failure to reach the caller")
	}

	if result.Outcome != ConfigurationSaveFailed {
		t.Errorf("outcome = %q, want %q: the announcement did not work", result.Outcome, ConfigurationSaveFailed)
	}

	if len(result.Saved) != 1 || result.Saved[0] != GroupsSection {
		t.Errorf("saved = %v, want the groups section named as stored: the store holds what was submitted, and the failure is the announcement's", result.Saved)
	}

	if len(result.Failed) != 0 {
		t.Errorf("failed = %v, want no section named as not stored: every section this save named reached the store", result.Failed)
	}

	if !fixture.loggedAs(domain.LogSourceConfig, "the configuration could not be saved") {
		t.Errorf("logs = %v, want the reason the announcement gave filed where every other refusal's reason is filed", fixture.logs.Stored)
	}

	if len(fixture.groups.Stored) != 1 || fixture.groups.Stored[0].Label != "EMS" {
		t.Errorf("the store holds %v, want the submitted groups left in effect: the save worked", fixture.groups.Stored)
	}

	if len(fixture.hub.BroadcastConfigs) != 0 {
		t.Error("a configuration the server could not read back must not be broadcast as though it had")
	}
}

func TestUpdateConfigurationAnnouncesTheNewConfigurationToListenersAndAdministrators(t *testing.T) {
	fixture := newConfigFixture()

	systems := domain.Systems{{ID: 4, SystemRef: 5, Label: "Harbour"}}

	if _, err := fixture.save(ConfigurationChanges{Systems: &systems}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(fixture.hub.BroadcastConfigs) != 1 {
		t.Fatalf("listener broadcasts = %d, want the new configuration sent to the listeners once", len(fixture.hub.BroadcastConfigs))
	}

	if len(fixture.hub.BroadcastConfigs[0].Systems) != 1 || fixture.hub.BroadcastConfigs[0].Systems[0].SystemRef != 5 {
		t.Error("the listeners must be broadcast the configuration that resulted from the save, not the one before it")
	}

	if fixture.admin.AnnouncedCount() != 1 {
		t.Fatalf("administrator announcements = %d, want the open administrator connections told once", fixture.admin.AnnouncedCount())
	}

	announced := fixture.admin.Announced[0]

	if len(announced.Dirwatches) != 2 || len(announced.Tags) != 1 {
		t.Errorf("announced = %+v, want an administrator told the whole configuration, not the four sections a listener sees", announced)
	}
}

func TestUpdateConfigurationRecordsThatTheConfigurationChanged(t *testing.T) {
	fixture := newConfigFixture()

	groups := domain.Groups{{ID: 3, Label: "EMS"}}

	if _, err := fixture.save(ConfigurationChanges{Groups: &groups}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !fixture.loggedAs(domain.LogSourceConfig, "the configuration changed") {
		t.Errorf("logs = %v, want the notice that the configuration changed, filed under the part of the server that saved it", fixture.logs.Stored)
	}
}

func TestUpdateConfigurationStopsAndRestartsTheDirectoryWatchersAroundTheSave(t *testing.T) {
	fixture := newConfigFixture()

	groups := domain.Groups{{ID: 3, Label: "EMS"}}

	if _, err := fixture.save(ConfigurationChanges{Groups: &groups}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if fixture.watchers.Stops != 1 {
		t.Errorf("watchers stopped %d times, want the save to stop them once before it changes anything", fixture.watchers.Stops)
	}

	if len(fixture.watchers.Starts) != 1 {
		t.Fatalf("watchers started %d times, want the save to start them once after it changes everything", len(fixture.watchers.Starts))
	}
}

// A whole-configuration save that names no dirwatch section leaves the server watching nothing,
// the preserved quirk the dirwatch-save maintainer note records.
func TestUpdateConfigurationWithoutADirwatchSectionLeavesTheServerWatchingNothing(t *testing.T) {
	fixture := newConfigFixture()

	groups := domain.Groups{{ID: 3, Label: "EMS"}}

	if _, err := fixture.save(ConfigurationChanges{Groups: &groups}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !fixture.watchers.WatchingNothing() {
		t.Errorf("watching = %v, want nothing: a whole-configuration save that says nothing about dirwatch leaves the server watching nothing until it restarts", fixture.watchers.Watching())
	}

	if len(fixture.dirwatches.Stored) != 2 {
		t.Errorf("stored dirwatches = %v, want the stored watched directories left untouched", fixture.dirwatches.Stored)
	}
}

func TestUpdateConfigurationWatchesTheStoredDirectoriesWhenItSavesTheDirwatchSection(t *testing.T) {
	fixture := newConfigFixture()

	submitted := domain.Dirwatches{{ID: 0, Directory: "/spool/new"}}

	if _, err := fixture.save(ConfigurationChanges{Dirwatches: &submitted}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	watching := fixture.watchers.Watching()

	if len(watching) != 1 || watching[0].Directory != "/spool/new" {
		t.Errorf("watching = %v, want what the store has after the save, because the legacy server reads back what it saved", watching)
	}
}

// A failed dirwatch section save leaves the submitted directories being watched, the preserved
// quirk the dirwatch-save maintainer note records.
func TestUpdateConfigurationWatchesWhatWasSubmittedWhenTheDirwatchSaveFails(t *testing.T) {
	fixture := newConfigFixture()
	fixture.dirwatches.SaveErr = errors.New("disk full")

	submitted := domain.Dirwatches{{ID: 0, Directory: "/spool/submitted"}}

	result, err := fixture.save(ConfigurationChanges{Dirwatches: &submitted})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(result.Failed) != 1 || result.Failed[0] != DirwatchSection {
		t.Fatalf("failed = %v, want the dirwatch section reported as not stored", result.Failed)
	}

	watching := fixture.watchers.Watching()

	if len(watching) != 1 || watching[0].Directory != "/spool/submitted" {
		t.Errorf("watching = %v, want the submitted directories: the legacy server keeps watching what it was given when it could not store it", watching)
	}
}

// A whole-configuration save naming no recognized section stops the watchers, still announces
// the configuration, and still records the change.
func TestUpdateConfigurationWithNoSectionsStillAnnouncesAndLeavesNothingWatching(t *testing.T) {
	fixture := newConfigFixture()

	result, err := fixture.save(ConfigurationChanges{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(result.Saved) != 0 || len(result.Failed) != 0 {
		t.Errorf("saved = %v failed = %v, want a save that names nothing to report nothing either way", result.Saved, result.Failed)
	}

	if fixture.watchers.Stops != 1 || len(fixture.watchers.Starts) != 1 {
		t.Errorf("watchers stopped %d started %d, want the bracket performed anyway", fixture.watchers.Stops, len(fixture.watchers.Starts))
	}

	if !fixture.watchers.WatchingNothing() {
		t.Error("the watchers must be left watching nothing, which is what the legacy server leaves them watching")
	}

	if fixture.admin.AnnouncedCount() != 1 {
		t.Error("the configuration is still announced, as the legacy server announces it")
	}

	if !fixture.loggedAs(domain.LogSourceConfig, "the configuration changed") {
		t.Errorf("logs = %v, want the notice written even though nothing changed", fixture.logs.Stored)
	}
}

func TestUpdateConfigurationSectionSavesOnlyThatSection(t *testing.T) {
	fixture := newConfigFixture()

	systems := domain.Systems{{ID: 4, SystemRef: 5, Label: "Harbour"}}

	result, err := fixture.saveSection(SystemsSection, ConfigurationChanges{Systems: &systems})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != ConfigurationSaved {
		t.Fatalf("outcome = %q, want %q", result.Outcome, ConfigurationSaved)
	}

	if len(result.Saved) != 1 || result.Saved[0] != SystemsSection {
		t.Errorf("saved = %v, want only the systems section", result.Saved)
	}

	if len(fixture.systems.Stored) != 1 || fixture.systems.Stored[0].SystemRef != 5 {
		t.Errorf("systems = %v, want the section replaced", fixture.systems.Stored)
	}

	if len(fixture.groups.Stored) != 1 || fixture.groups.Stored[0].Label != "Fire" {
		t.Errorf("groups = %v, want every other section left alone", fixture.groups.Stored)
	}

	if !fixture.loggedAs(domain.LogSourceConfig, "the systems configuration changed") {
		t.Errorf("logs = %v, want the notice naming the section that was saved", fixture.logs.Stored)
	}
}

func TestUpdateConfigurationSectionReportsAFailureAndChangesNothingElse(t *testing.T) {
	fixture := newConfigFixture()
	fixture.systems.Err = errors.New("constraint failed")

	systems := domain.Systems{{ID: 4, SystemRef: 5, Label: "Harbour"}}

	result, err := fixture.saveSection(SystemsSection, ConfigurationChanges{Systems: &systems})

	if err == nil {
		t.Error("the reason a per-section save failed must reach the caller, which logs it")
	}

	if result.Outcome != ConfigurationSaveFailed {
		t.Errorf("outcome = %q, want %q: a per-section save reports its failure", result.Outcome, ConfigurationSaveFailed)
	}

	if len(result.Failed) != 1 || result.Failed[0] != SystemsSection {
		t.Errorf("failed = %v, want the section reported as not stored", result.Failed)
	}

	if !fixture.loggedAs(domain.LogSourceConfig, "the configuration could not be saved: constraint failed") {
		t.Errorf("logs = %v, want the failure said as what happened, with the failure's own text after it", fixture.logs.Stored)
	}

	if fixture.admin.AnnouncedCount() != 0 {
		t.Error("a save that did not work must not announce a configuration change")
	}

	if len(fixture.hub.BroadcastConfigs) != 0 {
		t.Error("a save that did not work must not re-send the configuration to the listeners")
	}

	if fixture.loggedAs(domain.LogSourceConfig, "configuration changed") {
		t.Error("a save that did not work must not record that the configuration changed")
	}

	// The watchers are restarted whatever happens, which is what the legacy server does: a
	// rejected save leaves the server watching rather than silently stopped.
	if fixture.watchers.Stops != 1 || len(fixture.watchers.Starts) != 1 {
		t.Errorf("watchers stopped %d started %d, want them restarted even though the save failed", fixture.watchers.Stops, len(fixture.watchers.Starts))
	}
}

// A per-section save distinguishes a section the store holds while the announcement failed from
// one it refused, even though the administration page answers both the same way; the
// distinction lives in the configuration-save-failure maintainer note.
//
// The watchers are restarted before the announcement is attempted, exactly as they are for a save the
// store refused, so an announcement failure cannot leave the server stopped watching. That is pinned
// here as well, because the branch that fails sits after the restart.
func TestUpdateConfigurationSectionReportsASectionItStoredWhenTheAnnouncementFailed(t *testing.T) {
	fixture := newConfigFixture()

	fixture.tags.Err = errors.New("no such table: tags")

	groups := domain.Groups{{ID: 3, Label: "EMS"}}

	result, err := fixture.saveSection(GroupsSection, ConfigurationChanges{Groups: &groups})
	if err == nil {
		t.Fatal("Handle = nil for a configuration the server could not announce, want the failure to reach the caller")
	}

	if result.Outcome != ConfigurationSaveFailed {
		t.Errorf("outcome = %q, want %q: the announcement did not work", result.Outcome, ConfigurationSaveFailed)
	}

	if len(result.Saved) != 1 || result.Saved[0] != GroupsSection {
		t.Errorf("saved = %v, want the groups section named as stored: the store holds what the page submitted, and the failure is the announcement's", result.Saved)
	}

	if len(result.Failed) != 0 {
		t.Errorf("failed = %v, want no section named as not stored", result.Failed)
	}

	if !fixture.loggedAs(domain.LogSourceConfig, "the configuration could not be saved") {
		t.Errorf("logs = %v, want the reason the announcement gave filed where every other refusal's reason is filed", fixture.logs.Stored)
	}

	if len(fixture.groups.Stored) != 1 || fixture.groups.Stored[0].Label != "EMS" {
		t.Errorf("the store holds %v, want the submitted groups left in effect: the save worked", fixture.groups.Stored)
	}

	if fixture.watchers.Stops != 1 || len(fixture.watchers.Starts) != 1 {
		t.Errorf("watchers stopped %d started %d, want them restarted before the announcement was attempted", fixture.watchers.Stops, len(fixture.watchers.Starts))
	}

	if len(fixture.hub.BroadcastConfigs) != 0 {
		t.Error("a configuration the server could not read back must not be broadcast as though it had")
	}
}

func TestUpdateConfigurationSectionRefusesAChangeThatDoesNotMatchTheSection(t *testing.T) {
	fixture := newConfigFixture()

	groups := domain.Groups{{ID: 3, Label: "EMS"}}

	result, err := fixture.saveSection(SystemsSection, ConfigurationChanges{Groups: &groups})

	if !errors.Is(err, ErrNoConfigurationChange) {
		t.Errorf("error = %v, want %v: the caller and the server disagree about what was submitted", err, ErrNoConfigurationChange)
	}

	if result.Outcome != ConfigurationSaveFailed {
		t.Errorf("outcome = %q, want %q", result.Outcome, ConfigurationSaveFailed)
	}

	if len(fixture.groups.Stored) != 1 || fixture.groups.Stored[0].Label != "Fire" {
		t.Errorf("groups = %v, want nothing saved when the section and the change disagree", fixture.groups.Stored)
	}
}

func TestUpdateConfigurationSectionOfAnotherSectionRestartsTheStoredDirwatchers(t *testing.T) {
	fixture := newConfigFixture()

	groups := domain.Groups{{ID: 3, Label: "EMS"}}

	if _, err := fixture.saveSection(GroupsSection, ConfigurationChanges{Groups: &groups}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	watching := fixture.watchers.Watching()

	if len(watching) != 2 {
		t.Errorf("watching = %v, want the stored watched directories read back and watched again, which is what a per-section save does and a whole-configuration save does not", watching)
	}
}

func TestUpdateConfigurationSectionOfDirwatchWatchesWhatWasSubmittedWhenTheSaveFails(t *testing.T) {
	fixture := newConfigFixture()
	fixture.dirwatches.SaveErr = errors.New("disk full")

	submitted := domain.Dirwatches{{ID: 0, Directory: "/spool/submitted"}}

	result, err := fixture.saveSection(DirwatchSection, ConfigurationChanges{Dirwatches: &submitted})

	if err == nil {
		t.Error("the reason the save failed must reach the caller")
	}

	if result.Outcome != ConfigurationSaveFailed {
		t.Fatalf("outcome = %q, want %q", result.Outcome, ConfigurationSaveFailed)
	}

	watching := fixture.watchers.Watching()

	if len(watching) != 1 || watching[0].Directory != "/spool/submitted" {
		t.Errorf("watching = %v, want the submitted directories, as a whole-configuration save also leaves them", watching)
	}
}

func TestUpdateConfigurationSectionAnnouncesTheWholeConfigurationForOneSection(t *testing.T) {
	fixture := newConfigFixture()

	tags := domain.Tags{{ID: 11, Label: "Interop"}}

	if _, err := fixture.saveSection(TagsSection, ConfigurationChanges{Tags: &tags}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if fixture.admin.AnnouncedCount() != 1 {
		t.Fatalf("administrator announcements = %d, want the open administrator connections told once", fixture.admin.AnnouncedCount())
	}

	announced := fixture.admin.Announced[0]

	if len(announced.Systems) != 1 || len(announced.Dirwatches) != 2 {
		t.Errorf("announced = %+v, want the whole configuration announced even though one section was saved", announced)
	}

	if len(announced.Tags) != 1 || announced.Tags[0].Label != "Interop" {
		t.Errorf("announced tags = %v, want the announced configuration to be the one that resulted from the save", announced.Tags)
	}
}

// A broken server log does not stop the save: every section is stored and the listeners still
// hear about the new configuration.
func TestUpdateConfigurationSavesEverySectionWhenTheServerLogCannotBeWritten(t *testing.T) {
	fixture := newConfigFixture()
	fixture.logs.Err = errors.New("no such table: logs")

	systems := domain.Systems{{ID: 4, SystemRef: 5, Label: "Harbour"}}

	result, err := fixture.save(ConfigurationChanges{Systems: &systems})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != ConfigurationSaved {
		t.Fatalf("outcome = %q, want the configuration saved even though the log could not be written", result.Outcome)
	}

	if len(result.Saved) != 1 || result.Saved[0] != SystemsSection {
		t.Errorf("saved = %v, want the section the request named", result.Saved)
	}

	if len(fixture.systems.Stored) != 1 || fixture.systems.Stored[0].Label != "Harbour" {
		t.Errorf("stored systems = %v, want the submitted system on record", fixture.systems.Stored)
	}

	if len(fixture.hub.BroadcastConfigs) != 1 {
		t.Errorf("configuration broadcasts = %d, want the listeners still told the new configuration", len(fixture.hub.BroadcastConfigs))
	}
}

// An apikey row with no key is refused at apply time: the section save reports the failure, the
// list is not stored, and the server keeps the credentials it had.
func TestUpdateConfigurationSectionRefusesAKeylessApikey(t *testing.T) {
	fixture := newConfigFixture()

	before := append(domain.Apikeys{}, fixture.apikeys.Stored...)

	apikeys := domain.Apikeys{
		{ID: 1, Ident: "dispatch", Key: "dispatch-key"},
		{ID: 2, Ident: "nameless"},
	}

	result, err := fixture.saveSection(ApikeysSection, ConfigurationChanges{Apikeys: &apikeys})

	if err == nil {
		t.Error("the reason the apikey save failed must reach the caller, which logs it")
	}

	if result.Outcome != ConfigurationSaveFailed {
		t.Errorf("outcome = %q, want %q: a per-section save reports its failure", result.Outcome, ConfigurationSaveFailed)
	}

	if len(result.Failed) != 1 || result.Failed[0] != ApikeysSection {
		t.Errorf("failed = %v, want the apikeys section reported as not stored", result.Failed)
	}

	if len(fixture.apikeys.Stored) != len(before) {
		t.Errorf("apikeys = %v, want the stored list left alone: a refused save stores nothing", fixture.apikeys.Stored)
	}
}

// A dirwatch payload whose mask does not compile is refused at apply: the section save reports
// the failure, the list is not stored, and the server keeps the watches it had.
func TestUpdateConfigurationSectionRefusesAnUncompilableMask(t *testing.T) {
	fixture := newConfigFixture()

	before := append(domain.Dirwatches{}, fixture.dirwatches.Stored...)

	dirwatches := domain.Dirwatches{
		{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: "(", Extension: "wav"},
	}

	result, err := fixture.saveSection(DirwatchSection, ConfigurationChanges{Dirwatches: &dirwatches})

	if err == nil {
		t.Error("the reason the dirwatch save failed must reach the caller, which logs it")
	}

	if result.Outcome != ConfigurationSaveFailed {
		t.Errorf("outcome = %q, want %q: a per-section save reports its failure", result.Outcome, ConfigurationSaveFailed)
	}

	if len(result.Failed) != 1 || result.Failed[0] != DirwatchSection {
		t.Errorf("failed = %v, want the dirwatch section reported as not stored", result.Failed)
	}

	if len(fixture.dirwatches.Stored) != len(before) {
		t.Errorf("dirwatches = %v, want the stored list left alone: a refused save stores nothing", fixture.dirwatches.Stored)
	}
}

// A watch carrying a mask that compiles is stored.
func TestUpdateConfigurationSectionStoresAWatchWithAValidMask(t *testing.T) {
	fixture := newConfigFixture()

	dirwatches := domain.Dirwatches{
		{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: "#(UNITlbl)", Extension: "wav"},
	}

	result, err := fixture.saveSection(DirwatchSection, ConfigurationChanges{Dirwatches: &dirwatches})

	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if result.Outcome != ConfigurationSaved {
		t.Errorf("outcome = %q, want %q", result.Outcome, ConfigurationSaved)
	}

	if len(fixture.dirwatches.Stored) != 1 || fixture.dirwatches.Stored[0].Mask != "#(UNITlbl)" {
		t.Errorf("dirwatches = %v, want the watch with the valid mask stored", fixture.dirwatches.Stored)
	}
}

// A dirwatch payload whose mask only becomes impossible to compile once the watch tokens
// are replaced by the patterns they name is refused at apply as well: the administrator's
// brackets survive the plain check, and the mask that reaches the watcher cannot be
// compiled from them.
func TestUpdateConfigurationSectionRefusesAMaskThatTokensMakeImpossibleToCompile(t *testing.T) {
	fixture := newConfigFixture()

	before := append(domain.Dirwatches{}, fixture.dirwatches.Stored...)

	dirwatches := domain.Dirwatches{
		{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: "[#DATE]", Extension: "wav"},
	}

	result, err := fixture.saveSection(DirwatchSection, ConfigurationChanges{Dirwatches: &dirwatches})

	if err == nil {
		t.Error("a watch whose mask cannot be compiled after its tokens are replaced must not be stored")
	}

	if result.Outcome != ConfigurationSaveFailed {
		t.Errorf("outcome = %q, want %q: a per-section save reports its failure", result.Outcome, ConfigurationSaveFailed)
	}

	if len(result.Failed) != 1 || result.Failed[0] != DirwatchSection {
		t.Errorf("failed = %v, want the dirwatch section reported as not stored", result.Failed)
	}

	if len(fixture.dirwatches.Stored) != len(before) {
		t.Errorf("dirwatches = %v, want the stored list left alone: a refused save stores nothing", fixture.dirwatches.Stored)
	}
}
