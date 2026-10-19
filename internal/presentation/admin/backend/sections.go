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

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/dto"
	"rdio-scanner/internal/presentation/common/upgrade"
)

// unreadableBody says that a save was asked for with a body this endpoint cannot read, and keeps the reason
// the read failed. It is not a failure of the save: the save never started, and the endpoint answers it with
// 400 rather than the 422 a save that failed gets.
//
// The reason is kept because the two ways a body fails to read are different problems with different fixes —
// a client that sent no JSON at all, and a client that sent an object where this section wants a list — and
// the endpoint answers both with the same 400. The log is the only place they can be told apart.
type unreadableBody struct{ reason error }

func (body unreadableBody) Error() string {
	return fmt.Sprintf("submitted body cannot be read: %v", body.reason)
}

// Section reads or saves one section of the configuration, which is what an administrator's page actually
// does: it shows one list at a time, saves that list, and is answered with that list.
//
// The order of the first three decisions is the legacy order and it is what a client sees:
//
// - A request that arrives as a websocket upgrade is refused. The whole-configuration path is where an
// administrator's socket is opened, and a section path that accepted one would answer a socket with a
// list.
// - A section this server does not have is refused before the credential is looked at. The endpoint does
// not know what it would be protecting, and an administrator's page that asks for a section that does
// not exist is told which of its assumptions is wrong.
// - A credential the server does not accept is refused before the method is looked at.
func (admin *Admin) Section(w http.ResponseWriter, r *http.Request) {
	if upgrade.Wanted(r) {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	section := application.ConfigSection(strings.TrimPrefix(r.URL.Path, "/api/admin/config/"))

	// A path that names one level of one system belongs to the per-level endpoint. When
	// the request reaches this endpoint, the method is one the per-level endpoint does
	// not answer — GET and PUT for those paths are routed to it directly — so the
	// answer is the same refusal a section gives for a method it does not answer.
	if isSubSectionPath(string(section)) {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	if !application.IsConfigSection(string(section)) {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	if !admin.sessionIsOpen(r) {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	switch r.Method {
	case http.MethodGet:
		admin.sendSection(r.Context(), w, section)

	case http.MethodPut:
		admin.saveSection(w, r, section)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// isSubSectionPath reports whether a path under /api/admin/config/ names one level of
// one system: the systems prefix, a system identifier, and one of the three levels, and
// nothing else.
func isSubSectionPath(rest string) bool {
	under, levelPart, found := strings.Cut(strings.TrimPrefix(rest, "systems/"), "/")

	if !found {
		return false
	}

	if !application.IsSystemSubSection(levelPart) {
		return false
	}

	systemID, err := strconv.ParseUint(under, 10, 64)

	return systemID > 0 && err == nil
}

// saveSection saves one section.
//
// The watched directories are stopped and started whatever happens to the body, including a body that
// cannot be read at all: the legacy server stops the watchers before it reads what was submitted, and its
// deferred restart runs no matter how the read went. A server that answered 400 without restarting them
// would leave the watchers stopped by a request that changed nothing. Asking the use case to save a
// section with nothing submitted is how this endpoint reproduces that bracket without reaching past the
// use case to the watchers.
func (admin *Admin) saveSection(w http.ResponseWriter, r *http.Request, section application.ConfigSection) {
	changes, err := admin.submittedSection(r.Context(), section, r)

	_, failure := admin.configurationSection.Handle(r.Context(), application.UpdateConfigurationSectionCommand{
		Changes: changes,
		Section: section,
	})

	var body unreadableBody
	if errors.As(err, &body) {
		admin.reportUnreadableRequest(r, body.reason)

		w.WriteHeader(http.StatusBadRequest)

		return
	}

	if err != nil || failure != nil {
		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	admin.sendSection(r.Context(), w, section)
}

// submittedSection turns what was submitted into the one section a save replaces.
//
// A list section is read as a list. A body that is not a list is not a submission for that section, and
// the legacy server says so with 400 rather than saving nothing quietly — unlike the whole-configuration
// save, which looks at each section's name and leaves a section whose value it does not recognize alone.
// An empty list is a submission, and it replaces the section with nothing.
//
// The settings are not a list: they are merged into the settings in effect, which is what the legacy
// server does with them and what an administrator's page depends on when it submits only the settings that
// page shows.
func (admin *Admin) submittedSection(ctx context.Context, section application.ConfigSection, r *http.Request) (application.ConfigurationChanges, error) {
	changes := application.ConfigurationChanges{}

	if section == application.OptionsSection {
		var submitted map[string]any

		if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
			return changes, unreadableBody{reason: err}
		}

		current, err := admin.currentOptions(ctx)
		if err != nil {
			return changes, err
		}

		options := current.ApplyMap(submitted)

		changes.Options = &options

		return changes, nil
	}

	var submitted []any

	if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
		return changes, unreadableBody{reason: err}
	}

	switch section {
	case application.AccessSection:
		accesses := domain.ParseAccesses(submitted)

		changes.Access = &accesses

	case application.ApikeysSection:
		apikeys := domain.ParseApikeys(submitted)

		changes.Apikeys = &apikeys

	case application.DirwatchSection:
		dirwatches := domain.ParseDirwatches(submitted)

		changes.Dirwatches = &dirwatches

	case application.DownstreamsSection:
		downstreams := domain.ParseDownstreams(submitted)

		changes.Downstreams = &downstreams

	case application.GroupsSection:
		groups := domain.ParseGroups(submitted)

		changes.Groups = &groups

	case application.SystemsSection:
		systems := domain.ParseSystems(submitted)

		changes.Systems = &systems

	case application.TagsSection:
		tags := domain.ParseTags(submitted)

		changes.Tags = &tags
	}

	return changes, nil
}

// sendSection answers with one section, as an administrator is shown it.
func (admin *Admin) sendSection(ctx context.Context, w http.ResponseWriter, section application.ConfigSection) {
	configuration, err := admin.config.Handle(ctx, application.GetConfigQuery{})
	if err != nil {
		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	writeJSON(w, sectionPayload(configuration, section))
}

// SubSection reads or saves one level of one system: its sites, its talkgroups, or its
// units. It answers the way a section does, with one decision added: a system this server
// does not store is refused with 404, and so is a level that is not one of the three.
//
// The order of the decisions mirrors the section endpoint: a websocket upgrade is refused,
// a path this endpoint cannot name is refused before the credential is looked at, and a
// credential the server does not accept is refused before the method is looked at.
func (admin *Admin) SubSection(w http.ResponseWriter, r *http.Request) {
	if upgrade.Wanted(r) {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/admin/config/systems/")

	_, levelPart, found := strings.Cut(rest, "/")

	systemID, err := strconv.ParseUint(r.PathValue("systemId"), 10, 64)

	if !found || systemID == 0 || err != nil || !application.IsSystemSubSection(levelPart) {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	level := application.SystemSubSection(levelPart)

	if !admin.sessionIsOpen(r) {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	switch r.Method {
	case http.MethodGet:
		admin.sendSubSection(r.Context(), w, systemID, level)

	case http.MethodPut:
		admin.saveSubSection(w, r, systemID, level)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// saveSubSection saves one level of one system.
//
// A body this endpoint cannot read is answered with 400 before the save starts: the save
// never began, so nothing was bracketed and nothing was stored. A system the store does
// not have is answered with 404, a save that could not be stored with 417, and a save
// that worked with the level as re-read from the store.
func (admin *Admin) saveSubSection(w http.ResponseWriter, r *http.Request, systemID uint64, level application.SystemSubSection) {
	var submitted any

	if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
		admin.reportUnreadableRequest(r, err)

		w.WriteHeader(http.StatusBadRequest)

		return
	}

	rows, blacklists, readable := levelBody(level, submitted)

	if !readable {
		admin.reportUnreadableRequest(r, fmt.Errorf("the %s level of a system is saved as the list of its rows", level))

		w.WriteHeader(http.StatusBadRequest)

		return
	}

	cmd := application.UpdateSystemSubSectionCommand{SystemID: systemID, Level: level, Blacklists: blacklists}

	switch level {
	case application.SitesSubSection:
		cmd.Sites = domain.ParseSites(rows)
	case application.TalkgroupsSubSection:
		cmd.Talkgroups = domain.ParseTalkgroups(rows)
	case application.UnitsSubSection:
		cmd.Units = domain.ParseUnits(rows)
	}

	_, failure := admin.systemSubSection.Handle(r.Context(), cmd)

	if failure != nil {
		if errors.Is(failure, ports.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	admin.sendSubSection(r.Context(), w, systemID, level)
}

// levelBody is the body a level save sends. Every level has always taken the bare array of its
// rows. The talkgroups level also takes the object that carries its rows beside the blacklist the
// section edits with them, so the save that drops a talkgroup stores the entry that refuses it in
// the same request. A body that is neither is a body this endpoint cannot read.
func levelBody(level application.SystemSubSection, submitted any) ([]any, *domain.Blacklists, bool) {
	switch body := submitted.(type) {
	case []any:
		return body, nil, true

	case map[string]any:
		if level != application.TalkgroupsSubSection {
			return nil, nil, false
		}

		rows, ok := body["talkgroups"].([]any)
		if !ok {
			return nil, nil, false
		}

		text, ok := body["blacklists"].(string)
		if !ok {
			return rows, nil, true
		}

		blacklists := domain.Blacklists(text)

		return rows, &blacklists, true
	}

	return nil, nil, false
}

// sendSubSection answers with one level of one system, as an administrator is shown it.
// A system the configuration does not have is answered with 404.
func (admin *Admin) sendSubSection(ctx context.Context, w http.ResponseWriter, systemID uint64, level application.SystemSubSection) {
	configuration, err := admin.config.Handle(ctx, application.GetConfigQuery{})
	if err != nil {
		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	system, ok := configuration.Systems.ByID(systemID)
	if !ok {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	switch level {
	case application.SitesSubSection:
		writeJSON(w, dto.SiteListPayload(system.Sites))

	case application.TalkgroupsSubSection:
		writeJSON(w, dto.TalkgroupListPayload(system.Talkgroups))

	case application.UnitsSubSection:
		writeJSON(w, dto.UnitListPayload(system.Units))
	}
}

// sectionPayload is one section of the configuration, in the shape that section is shown in.
func sectionPayload(configuration domain.Configuration, section application.ConfigSection) any {
	switch section {
	case application.AccessSection:
		return dto.AccessListPayload(configuration.Access)

	case application.ApikeysSection:
		return dto.ApikeyListPayload(configuration.Apikeys)

	case application.DirwatchSection:
		return dto.DirwatchListPayload(configuration.Dirwatches)

	case application.DownstreamsSection:
		return dto.DownstreamListPayload(configuration.Downstreams)

	case application.GroupsSection:
		return dto.GroupListPayload(configuration.Groups)

	case application.OptionsSection:
		return dto.OptionsPayload(configuration.Options)

	case application.SystemsSection:
		return dto.SystemListPayload(configuration.Systems)

	case application.TagsSection:
		return dto.TagListPayload(configuration.Tags)
	}

	return nil
}
