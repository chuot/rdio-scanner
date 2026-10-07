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

package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/dto"
)

// errUnreadableBody says that a save was asked for with a body this endpoint cannot read. It is not a
// failure of the save: the save never started.
var errUnreadableBody = errors.New("submitted body cannot be read")

// Section reads or saves one section of the configuration, which is what an administrator's page actually
// does: it shows one list at a time, saves that list, and is answered with that list.
//
// The order of the first three decisions is the legacy order and it is what a client sees:
//
//   - A request that arrives as a websocket upgrade is refused. The whole-configuration path is where an
//     administrator's socket is opened, and a section path that accepted one would answer a socket with a
//     list.
//   - A section this server does not have is refused before the credential is looked at. The endpoint does
//     not know what it would be protecting, and an administrator's page that asks for a section that does
//     not exist is told which of its assumptions is wrong.
//   - A credential the server does not accept is refused before the method is looked at.
func (admin *Admin) Section(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("upgrade"), "websocket") {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	section := application.ConfigSection(strings.TrimPrefix(r.URL.Path, "/api/admin/config/"))

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

	if errors.Is(err, errUnreadableBody) {
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
			return changes, errUnreadableBody
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
		return changes, errUnreadableBody
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
