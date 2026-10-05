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
	"context"

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

// GetConfigQuery asks for the configuration the server is running with, which is what an
// administrator reads before changing it.
//
// The whole configuration is read as one thing even though an administrator may ask for one
// section on its own, because the sections are only meaningful together: the systems a
// listener may see, the groups and tags those systems' talkgroups are filed under, and the
// options that say how a call is handled are one state, not eight independent ones.
type GetConfigQuery struct{}

// GetConfigHandler reads the configuration in effect.
//
// It returns the configuration itself rather than a projection of it: the administrator's view
// *is* the configuration, and the shapes the wire carries — the eight-section admin payload,
// the four-section listener payload, the per-section payloads — belong to the DTO layer (T035),
// which is where a shape can differ per consumer without the read having to know about it.
type GetConfigHandler struct {
	repos configurationRepositories
}

// NewGetConfigHandler builds the handler from the repositories that own each section.
func NewGetConfigHandler(
	accesses ports.AccessRepository,
	apikeys ports.ApikeyRepository,
	dirwatches ports.DirwatchRepository,
	downstreams ports.DownstreamRepository,
	groups ports.GroupRepository,
	options ports.OptionsRepository,
	systems ports.SystemRepository,
	tags ports.TagRepository,
) *GetConfigHandler {
	return &GetConfigHandler{
		repos: configurationRepositories{
			accesses:    accesses,
			apikeys:     apikeys,
			dirwatches:  dirwatches,
			downstreams: downstreams,
			groups:      groups,
			options:     options,
			systems:     systems,
			tags:        tags,
		},
	}
}

// Handle gathers the configuration in effect.
func (handler *GetConfigHandler) Handle(ctx context.Context, query GetConfigQuery) (domain.Configuration, error) {
	return readConfiguration(ctx, handler.repos)
}

// configurationRepositories are the stores that own each section of the configuration. It is
// gathered into one value because both reading the configuration and saving part of it need
// all of them, and a handler that needs all eight should say so once.
type configurationRepositories struct {
	accesses    ports.AccessRepository
	apikeys     ports.ApikeyRepository
	dirwatches  ports.DirwatchRepository
	downstreams ports.DownstreamRepository
	groups      ports.GroupRepository
	options     ports.OptionsRepository
	systems     ports.SystemRepository
	tags        ports.TagRepository
}

// readConfiguration gathers the configuration in effect.
//
// The order the sections are read in is the order the legacy server reports them in, which is
// the order the administrator's view has always shown them in.
func readConfiguration(ctx context.Context, repos configurationRepositories) (domain.Configuration, error) {
	var configuration domain.Configuration

	accesses, err := repos.accesses.AllAccesses(ctx)
	if err != nil {
		return configuration, err
	}

	apikeys, err := repos.apikeys.AllApikeys(ctx)
	if err != nil {
		return configuration, err
	}

	dirwatches, err := repos.dirwatches.AllDirwatches(ctx)
	if err != nil {
		return configuration, err
	}

	downstreams, err := repos.downstreams.AllDownstreams(ctx)
	if err != nil {
		return configuration, err
	}

	groups, err := repos.groups.AllGroups(ctx)
	if err != nil {
		return configuration, err
	}

	options, err := repos.options.ReadOptions(ctx)
	if err != nil {
		return configuration, err
	}

	systems, err := repos.systems.AllSystems(ctx)
	if err != nil {
		return configuration, err
	}

	tags, err := repos.tags.AllTags(ctx)
	if err != nil {
		return configuration, err
	}

	configuration.Access = accesses
	configuration.Apikeys = apikeys
	configuration.Dirwatches = dirwatches
	configuration.Downstreams = downstreams
	configuration.Groups = groups
	configuration.Options = options
	configuration.Systems = systems
	configuration.Tags = tags

	return configuration, nil
}
