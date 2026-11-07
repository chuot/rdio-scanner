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
	"fmt"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// ConfigSection names one part of the configuration an administrator reads and saves.
type ConfigSection string

const (
	AccessSection      ConfigSection = "access"
	ApikeysSection     ConfigSection = "apikeys"
	DirwatchSection    ConfigSection = "dirwatch"
	DownstreamsSection ConfigSection = "downstreams"
	GroupsSection      ConfigSection = "groups"
	OptionsSection     ConfigSection = "options"
	SystemsSection     ConfigSection = "systems"
	TagsSection        ConfigSection = "tags"
)

// configurationSectionOrder is the order the sections are saved in. A save is not one
// transaction, so when it fails partway through the earlier sections are changed and the
// later ones are not: the order decides what an administrator finds when that happens.
var configurationSectionOrder = []ConfigSection{
	AccessSection,
	ApikeysSection,
	DirwatchSection,
	DownstreamsSection,
	GroupsSection,
	OptionsSection,
	SystemsSection,
	TagsSection,
}

// IsConfigSection reports whether a name is one of the eight configuration sections. It
// belongs here rather than in the REST layer: the eight sections are part of the server's
// vocabulary, and "that is not a part of the configuration" is a business answer.
func IsConfigSection(name string) bool {
	for _, section := range configurationSectionOrder {
		if string(section) == name {
			return true
		}
	}

	return false
}

// ConfigurationChanges are the parts of the configuration a save replaces. A section that
// is not named is left exactly as it is; a section that is named but empty replaces the
// section with nothing — removing the last group leaves no groups, which is not the same
// as saying nothing about groups. That distinction is why this is eight optional sections
// rather than a map.
type ConfigurationChanges struct {
	Access      *domain.Accesses
	Apikeys     *domain.Apikeys
	Dirwatches  *domain.Dirwatches
	Downstreams *domain.Downstreams
	Groups      *domain.Groups
	Options     *domain.Options
	Systems     *domain.Systems
	Tags        *domain.Tags
}

// names reports whether the payload named a section.
func (changes ConfigurationChanges) names(section ConfigSection) bool {
	switch section {
	case AccessSection:
		return changes.Access != nil
	case ApikeysSection:
		return changes.Apikeys != nil
	case DirwatchSection:
		return changes.Dirwatches != nil
	case DownstreamsSection:
		return changes.Downstreams != nil
	case GroupsSection:
		return changes.Groups != nil
	case OptionsSection:
		return changes.Options != nil
	case SystemsSection:
		return changes.Systems != nil
	case TagsSection:
		return changes.Tags != nil
	}

	return false
}

// namedSections lists the sections the payload named, in save order.
func (changes ConfigurationChanges) namedSections() []ConfigSection {
	named := []ConfigSection{}

	for _, section := range configurationSectionOrder {
		if changes.names(section) {
			named = append(named, section)
		}
	}

	return named
}

// ErrNoConfigurationChange is reported when a save names a section but carries nothing for it,
// and when a whole-configuration save names nothing at all.
var ErrNoConfigurationChange = errors.New("no configuration section submitted")

// UpdateConfigurationOutcome reports what a save did.
type UpdateConfigurationOutcome string

const (
	// ConfigurationSaved means the save ran to the end. It does not mean every section took:
	// a whole-configuration save reports each section it could not store in the server log
	// and goes on with the rest.
	ConfigurationSaved UpdateConfigurationOutcome = "saved"

	// ConfigurationSaveFailed means the save stopped before it finished, and which half
	// stopped it is read from the lists it carries: a section the store refused appears in
	// Failed, a section the store holds while announcing the result failed appears in Saved.
	// Only a per-section save reports a refusal this way, so for a whole-configuration save
	// this outcome means the announcement failed.
	ConfigurationSaveFailed UpdateConfigurationOutcome = "failed"
)

// UpdateConfigurationResult is the acknowledgement a save returns: what it did, which sections
// it replaced, and which it could not. It carries no configuration — the caller reads the
// result back with GetConfigQuery.
type UpdateConfigurationResult struct {
	Outcome UpdateConfigurationOutcome

	// Saved lists the sections that were replaced, in the order they were saved.
	Saved []ConfigSection

	// Failed lists the sections the server could not store. They are already in the server
	// log; they are reported here so the REST layer can answer a per-section save with the
	// failure status the contract fixes.
	Failed []ConfigSection
}

// UpdateConfigurationCommand replaces the parts of the configuration its payload named.
type UpdateConfigurationCommand struct {
	Changes ConfigurationChanges
}

// UpdateConfigurationSectionCommand replaces exactly one section of the configuration.
type UpdateConfigurationSectionCommand struct {
	Section ConfigSection
	Changes ConfigurationChanges
}

// ConfigurationPorts are everything a configuration save needs, gathered into one value:
// reading the configuration, saving part of it, and announcing the change all need the
// same set, and the composition root builds it once.
type ConfigurationPorts struct {
	ConfigurationRepositories

	Logs        ports.LogRepository
	Hub         ports.Hub
	Admin       ports.AdministratorSessions
	Dirwatchers ports.Dirwatchers
	Clock       ports.Clock
}

// configurationSaver is what both kinds of configuration save do with: the stores that own
// each section, the things that have to be told when a section changes, and the bracket the
// save performs around the directory watchers.
type configurationSaver struct {
	repos       ConfigurationRepositories
	logs        ports.LogRepository
	hub         ports.Hub
	admin       ports.AdministratorSessions
	dirwatchers ports.Dirwatchers
	clock       ports.Clock
}

func newConfigurationSaver(configuration ConfigurationPorts) configurationSaver {
	return configurationSaver{
		repos:       configuration.ConfigurationRepositories,
		logs:        configuration.Logs,
		hub:         configuration.Hub,
		admin:       configuration.Admin,
		dirwatchers: configuration.Dirwatchers,
		clock:       configuration.Clock,
	}
}

// save stores one section, writing the section's whole aggregate: the systems section
// together with the sites, talkgroups, and units under each system. A section the payload
// did not name is a request to save nothing, which is a refusal rather than a no-op: the
// caller and the server disagree about what was submitted.
func (saver configurationSaver) save(ctx context.Context, section ConfigSection, changes ConfigurationChanges) error {
	return saver.saveSystemsSection(ctx, section, changes, saver.repos.Systems.SaveSystems)
}

// saveSection stores one section of a per-section save. The systems section is the one
// section whose parts are saved on their own, elsewhere: its sites, talkgroups, and units are
// written by the save of that level, for the system that level shows, so the section save
// writes the system rows and leaves every level as the store holds it.
func (saver configurationSaver) saveSection(ctx context.Context, section ConfigSection, changes ConfigurationChanges) error {
	return saver.saveSystemsSection(ctx, section, changes, saver.repos.Systems.SaveSystemSettings)
}

func (saver configurationSaver) saveSystemsSection(ctx context.Context, section ConfigSection, changes ConfigurationChanges, saveSystems func(context.Context, domain.Systems) error) error {
	if !changes.names(section) {
		return ErrNoConfigurationChange
	}

	switch section {
	case AccessSection:
		return saver.repos.Accesses.SaveAccesses(ctx, *changes.Access)
	case ApikeysSection:
		// A credential list with a keyless row is refused here, at apply time, before it can
		// be stored: the domain owns the rule, this is the door every submitted list passes.
		if err := changes.Apikeys.Validate(); err != nil {
			return err
		}

		return saver.repos.Apikeys.SaveApikeys(ctx, *changes.Apikeys)
	case DirwatchSection:
		// Masks and extensions are refused at apply, before they can be stored: the domain
		// owns the rules, this is the door every submitted watch list passes.
		if err := changes.Dirwatches.Validate(); err != nil {
			return err
		}

		return saver.repos.Dirwatches.SaveDirwatches(ctx, *changes.Dirwatches)
	case DownstreamsSection:
		// Addresses are refused at apply: the forwarder posts recordings and the apikey to
		// whatever the administrator typed, and a scheme that is not http or https never
		// gets stored to be posted to.
		if err := changes.Downstreams.Validate(); err != nil {
			return err
		}

		return saver.repos.Downstreams.SaveDownstreams(ctx, *changes.Downstreams)
	case GroupsSection:
		return saver.repos.Groups.SaveGroups(ctx, *changes.Groups)
	case OptionsSection:
		return saver.repos.Options.WriteOptions(ctx, *changes.Options)
	case SystemsSection:
		return saveSystems(ctx, *changes.Systems)
	case TagsSection:
		return saver.repos.Tags.SaveTags(ctx, *changes.Tags)
	}

	return fmt.Errorf("unknown configuration section %s", section)
}

// publish tells every listener and every open administrator connection what the
// configuration now is. The two audiences are told different things on purpose: a listener
// is shown the sections that decide what calls it sees, and only as much of them as its
// access allows; an administrator is shown the whole configuration.
func (saver configurationSaver) publish(ctx context.Context) error {
	configuration, err := readConfiguration(ctx, saver.repos)
	if err != nil {
		return err
	}

	saver.hub.BroadcastConfig(ports.ConfigSnapshot{
		Groups:  configuration.Groups,
		Tags:    configuration.Tags,
		Systems: configuration.Systems,
		Options: configuration.Options,
	})

	saver.admin.NotifyConfigurationChanged(ctx, configuration)

	return nil
}

// announce records that the configuration changed, naming the section when the save was a
// per-section one; an empty section is the whole-configuration save, which names no single
// part of it.
func (saver configurationSaver) announce(ctx context.Context, section ConfigSection) {
	if section == "" {
		saver.writeLog(ctx, domain.LogLevelWarn, domain.LogSourceConfig, "the configuration changed")

		return
	}

	saver.writeLog(ctx, domain.LogLevelWarn, domain.LogSourceConfig, fmt.Sprintf("the %s configuration changed", section))
}

// reportFailure records a section the server could not store, followed by the failure's own
// text: the error names the statement that failed, which is the part an operator needs.
func (saver configurationSaver) reportFailure(ctx context.Context, err error) {
	saver.writeLog(ctx, domain.LogLevelError, domain.LogSourceConfig, fmt.Sprintf("the configuration could not be saved: %s", err))
}

func (saver configurationSaver) writeLog(ctx context.Context, level string, source domain.LogSource, message string) {
	appendServerLog(ctx, saver.logs, saver.clock, level, source, message)
}

// UpdateConfigurationHandler saves the whole configuration. The save brackets itself around
// the directory watchers: they are stopped before anything changes and started again
// afterwards, so a watcher cannot ingest a call against a half replaced configuration.
type UpdateConfigurationHandler struct {
	saver configurationSaver
}

// NewUpdateConfigurationHandler builds the handler from the configuration ports.
func NewUpdateConfigurationHandler(configuration ConfigurationPorts) *UpdateConfigurationHandler {
	return &UpdateConfigurationHandler{saver: newConfigurationSaver(configuration)}
}

// Handle saves each named section in order, then announces the configuration that resulted.
// A section that cannot be stored is reported and the save continues with the next one: a
// whole-configuration save is not one transaction, and an administrator finds out what
// failed from the server log rather than from the status code.
func (handler *UpdateConfigurationHandler) Handle(ctx context.Context, cmd UpdateConfigurationCommand) (UpdateConfigurationResult, error) {
	result := UpdateConfigurationResult{Outcome: ConfigurationSaved}

	handler.saver.dirwatchers.Stop(ctx)

	saved := map[ConfigSection]bool{}

	for _, section := range cmd.Changes.namedSections() {
		if err := handler.saver.save(ctx, section, cmd.Changes); err != nil {
			handler.saver.reportFailure(ctx, err)

			result.Failed = append(result.Failed, section)

			continue
		}

		saved[section] = true

		result.Saved = append(result.Saved, section)
	}

	handler.saver.dirwatchers.Start(ctx, handler.watchesToStart(ctx, cmd.Changes, saved))

	if err := handler.saver.publish(ctx); err != nil {
		// Every section this save named is in the store, and reading back the configuration
		// that resulted is what failed. The result carries both halves — which sections
		// reached the store, and that the save as a whole did not complete — because the two
		// answers send an administrator to different actions.
		result.Outcome = ConfigurationSaveFailed

		handler.saver.reportFailure(ctx, err)

		return result, err
	}

	handler.saver.announce(ctx, "")

	return result, nil
}

// watchesToStart decides which directories the server watches after a whole-configuration
// save. Two preserved legacy quirks decide it, a save that says nothing about dirwatch
// leaves the server watching nothing, and when the section is named the watchers follow the
// store, except on a failed save.
func (handler *UpdateConfigurationHandler) watchesToStart(ctx context.Context, changes ConfigurationChanges, saved map[ConfigSection]bool) domain.Dirwatches {
	if !changes.names(DirwatchSection) {
		return domain.Dirwatches{}
	}

	submitted := *changes.Dirwatches

	if !saved[DirwatchSection] {
		return submitted
	}

	watches, err := handler.saver.repos.Dirwatches.AllDirwatches(ctx)
	if err != nil {
		handler.saver.reportFailure(ctx, err)

		return submitted
	}

	return watches
}

// UpdateConfigurationSectionHandler saves one section of the configuration.
type UpdateConfigurationSectionHandler struct {
	saver configurationSaver
}

// NewUpdateConfigurationSectionHandler builds the handler from the configuration ports.
func NewUpdateConfigurationSectionHandler(configuration ConfigurationPorts) *UpdateConfigurationSectionHandler {
	return &UpdateConfigurationSectionHandler{saver: newConfigurationSaver(configuration)}
}

// Handle saves one section, or reports that it could not. Unlike a whole-configuration save,
// a failure here stops the save and is reported, and the REST layer answers with the failure
// status the contract fixes. The directory watchers are restarted whatever happens —
// including for a save that failed — so a rejected save leaves the server watching rather
// than silently stopped.
func (handler *UpdateConfigurationSectionHandler) Handle(ctx context.Context, cmd UpdateConfigurationSectionCommand) (UpdateConfigurationResult, error) {
	result := UpdateConfigurationResult{Outcome: ConfigurationSaved}

	handler.saver.dirwatchers.Stop(ctx)

	failure := handler.saver.saveSection(ctx, cmd.Section, cmd.Changes)
	if failure != nil {
		if !errors.Is(failure, ErrNoConfigurationChange) {
			handler.saver.reportFailure(ctx, failure)
		}

		result.Outcome = ConfigurationSaveFailed
		result.Failed = append(result.Failed, cmd.Section)
	} else {
		result.Saved = append(result.Saved, cmd.Section)
	}

	handler.saver.dirwatchers.Start(ctx, handler.watchesToRestart(ctx, cmd, failure == nil))

	if failure != nil {
		// The watchers are back, but nothing was announced: a save that did not work is not
		// a configuration change. The reason is returned alongside the outcome — the outcome
		// is what the REST layer maps to a status, the error is what a caller logs.
		return result, failure
	}

	if err := handler.saver.publish(ctx); err != nil {
		// The section the page submitted is in the store, and reading back the configuration
		// that resulted is what failed. The page still gets the same 417 a refused section
		// gets, so the distinction lives in the use case's answer and the server's log line:
		// the section named as stored is one the server is now running with.
		result.Outcome = ConfigurationSaveFailed

		handler.saver.reportFailure(ctx, err)

		return result, err
	}

	handler.saver.announce(ctx, cmd.Section)

	return result, nil
}

// watchesToRestart decides which directories the server watches after a per-section save.
// Saving any other section reads the stored dirwatches back: stopping the watchers emptied
// them and this save said nothing about what to watch instead. Saving the dirwatch section
// itself watches the store when the save worked, and what was submitted when it did not,
// for the same reason a whole-configuration save does.
func (handler *UpdateConfigurationSectionHandler) watchesToRestart(ctx context.Context, cmd UpdateConfigurationSectionCommand, saved bool) domain.Dirwatches {
	if cmd.Section != DirwatchSection || saved {
		watches, err := handler.saver.repos.Dirwatches.AllDirwatches(ctx)
		if err != nil {
			handler.saver.reportFailure(ctx, err)

			return domain.Dirwatches{}
		}

		return watches
	}

	if cmd.Changes.Dirwatches == nil {
		return domain.Dirwatches{}
	}

	return *cmd.Changes.Dirwatches
}
