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

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
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
// the four-section listener payload, the per-section payloads — belong to the DTO layer,
// which is where a shape can differ per consumer without the read having to know about it.
type GetConfigHandler struct {
	repos ConfigurationRepositories
}

// NewGetConfigHandler builds the handler from the stores that own each section.
func NewGetConfigHandler(repos ConfigurationRepositories) *GetConfigHandler {
	return &GetConfigHandler{repos: repos}
}

// Handle gathers the configuration in effect.
func (handler *GetConfigHandler) Handle(ctx context.Context, query GetConfigQuery) (domain.Configuration, error) {
	return readConfiguration(ctx, handler.repos)
}

// ConfigurationRepositories are the stores that own each section of the configuration. It is
// exported because reading the configuration, saving part of it, and telling an administrator
// about a change all need the same set, and the composition root builds it once.
type ConfigurationRepositories struct {
	Accesses    ports.AccessRepository
	Apikeys     ports.ApikeyRepository
	Dirwatches  ports.DirwatchRepository
	Downstreams ports.DownstreamRepository
	Groups      ports.GroupRepository
	Options     ports.OptionsRepository
	Systems     ports.SystemRepository
	Tags        ports.TagRepository
}

// readConfiguration gathers the configuration in effect.
//
// The order the sections are read in is the order the legacy server reports them in, which is
// the order the administrator's view has always shown them in.
func readConfiguration(ctx context.Context, repos ConfigurationRepositories) (domain.Configuration, error) {
	var configuration domain.Configuration

	accesses, err := repos.Accesses.AllAccesses(ctx)
	if err != nil {
		return configuration, err
	}

	apikeys, err := repos.Apikeys.AllApikeys(ctx)
	if err != nil {
		return configuration, err
	}

	dirwatches, err := repos.Dirwatches.AllDirwatches(ctx)
	if err != nil {
		return configuration, err
	}

	downstreams, err := repos.Downstreams.AllDownstreams(ctx)
	if err != nil {
		return configuration, err
	}

	groups, err := repos.Groups.AllGroups(ctx)
	if err != nil {
		return configuration, err
	}

	options, err := repos.Options.ReadOptions(ctx)
	if err != nil {
		return configuration, err
	}

	systems, err := repos.Systems.AllSystems(ctx)
	if err != nil {
		return configuration, err
	}

	tags, err := repos.Tags.AllTags(ctx)
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
