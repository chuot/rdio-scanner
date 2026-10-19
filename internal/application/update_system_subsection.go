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

package application

import (
	"context"
	"errors"
	"fmt"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// SystemSubSection names one level of a system an administrator saves on its own:
// the sites, the talkgroups, or the units of one system.
type SystemSubSection string

const (
	SitesSubSection      SystemSubSection = "sites"
	TalkgroupsSubSection SystemSubSection = "talkgroups"
	UnitsSubSection      SystemSubSection = "units"
)

// IsSystemSubSection reports whether a name is one of the three system levels.
//
// It belongs here for the same reason IsConfigSection does: the levels are part of
// the server's vocabulary, and "that is not a level of a system" is a business
// answer rather than a routing one.
func IsSystemSubSection(name string) bool {
	switch SystemSubSection(name) {
	case SitesSubSection, TalkgroupsSubSection, UnitsSubSection:
		return true
	}

	return false
}

// UpdateSystemSubSectionCommand replaces one level of one system. The level decides
// which of the three payloads is read; the others are ignored.
//
// Blacklists is the blacklist the talkgroups level edits beside its rows, saved with them.
// A command that names none — the sites and units levels, and a talkgroups save that did
// not touch the field — leaves the stored text alone.
type UpdateSystemSubSectionCommand struct {
	SystemID   uint64
	Level      SystemSubSection
	Sites      domain.Sites
	Talkgroups domain.Talkgroups
	Units      domain.Units
	Blacklists *domain.Blacklists
}

// UpdateSystemSubSectionHandler saves one level of one system, then announces the
// configuration that resulted.
//
// It shares the configuration saver with the section save: the directory watchers are
// bracketed the same way, the full configuration is broadcast the same way, and a
// failure stops the save and is reported the same way. The level is saved through the
// system repository, which knows the system it belongs to; a system the store does
// not have is reported as ErrNotFound, which the REST layer answers with 404.
type UpdateSystemSubSectionHandler struct {
	saver configurationSaver
}

// NewUpdateSystemSubSectionHandler builds the handler from the configuration ports.
func NewUpdateSystemSubSectionHandler(configuration ConfigurationPorts) *UpdateSystemSubSectionHandler {
	return &UpdateSystemSubSectionHandler{saver: newConfigurationSaver(configuration)}
}

// Handle saves one level of one system, or reports that it could not.
func (handler *UpdateSystemSubSectionHandler) Handle(ctx context.Context, cmd UpdateSystemSubSectionCommand) (UpdateConfigurationResult, error) {
	result := UpdateConfigurationResult{Outcome: ConfigurationSaved}

	handler.saver.dirwatchers.Stop(ctx)

	failure := handler.save(ctx, cmd)

	if failure != nil {
		if !errors.Is(failure, ports.ErrNotFound) {
			handler.saver.reportFailure(ctx, failure)
		}

		result.Outcome = ConfigurationSaveFailed
		result.Failed = append(result.Failed, ConfigSection(cmd.Level))
	} else {
		result.Saved = append(result.Saved, ConfigSection(cmd.Level))
	}

	// The watchers are restarted whatever happens, reading the stored directories
	// back, exactly as a per-section save does: stopping them emptied the list, and
	// this save said nothing about what to watch instead.
	watches, err := handler.saver.repos.Dirwatches.AllDirwatches(ctx)
	if err != nil {
		handler.saver.reportFailure(ctx, err)

		watches = domain.Dirwatches{}
	}

	handler.saver.dirwatchers.Start(ctx, watches)

	if failure != nil {
		return result, failure
	}

	if err := handler.saver.publish(ctx); err != nil {
		return UpdateConfigurationResult{Outcome: ConfigurationSaveFailed}, err
	}

	handler.announce(ctx, cmd)

	return result, nil
}

// save writes the level's rows through the system repository, which renumbers their
// order and refuses a system it does not store. The talkgroups level carries the blacklist
// that was edited beside the rows, and the repository stores both in one save.
func (handler *UpdateSystemSubSectionHandler) save(ctx context.Context, cmd UpdateSystemSubSectionCommand) error {
	switch cmd.Level {
	case SitesSubSection:
		return handler.saver.repos.Systems.SaveSystemSites(ctx, cmd.SystemID, cmd.Sites)
	case TalkgroupsSubSection:
		return handler.saver.repos.Systems.SaveSystemTalkgroups(ctx, cmd.SystemID, cmd.Talkgroups, cmd.Blacklists)
	case UnitsSubSection:
		return handler.saver.repos.Systems.SaveSystemUnits(ctx, cmd.SystemID, cmd.Units)
	}

	return fmt.Errorf("unknown system sub-section %s", cmd.Level)
}

// announce records that one level of one system changed, naming the level and the system's reference
// number. The reference is the number the administrator reads in the section and types into the
// fields, so it is the number a log line has to carry for the line to be searched for: the identity
// the store gave the row is reassigned by the server and never appears on the screen. The number is
// read from the systems the save's own store holds. A system the store no longer has names no
// identifier, and the line is the sentence a per-section save writes.
func (handler *UpdateSystemSubSectionHandler) announce(ctx context.Context, cmd UpdateSystemSubSectionCommand) {
	reference, found := uint(0), false

	if systems, err := handler.saver.repos.Systems.AllSystems(ctx); err == nil {
		for _, system := range systems {
			if system.ID == cmd.SystemID {
				reference, found = system.SystemRef, true

				break
			}
		}
	}

	if !found {
		handler.saver.writeLog(ctx, domain.LogLevelWarn, domain.LogSourceConfig,
			fmt.Sprintf("the %s configuration changed", cmd.Level))

		return
	}

	handler.saver.writeLog(ctx, domain.LogLevelWarn, domain.LogSourceConfig,
		fmt.Sprintf("the %s configuration of system %d changed", cmd.Level, reference))
}
