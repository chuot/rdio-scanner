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

package fakes

import (
	"context"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// AdministratorSessions records what the open administrator connections were told.
type AdministratorSessions struct {
	// Announced records each configuration an announcement carried, in the order it was sent.
	Announced []domain.Configuration
}

var _ ports.AdministratorSessions = (*AdministratorSessions)(nil)

// NewAdministratorSessions returns a set of administrator connections that have been told
// nothing.
func NewAdministratorSessions() *AdministratorSessions {
	return &AdministratorSessions{}
}

// NotifyConfigurationChanged records the configuration the connections were given.
func (sessions *AdministratorSessions) NotifyConfigurationChanged(ctx context.Context, configuration domain.Configuration) {
	sessions.Announced = append(sessions.Announced, configuration)
}

// AnnouncedCount reports how many announcements were made.
func (sessions *AdministratorSessions) AnnouncedCount() int {
	return len(sessions.Announced)
}

// Dirwatchers records the brackets a configuration save performed around the directory watchers,
// and what each bracket ended up watching.
//
// What a save leaves watching is the part a test needs to see: a save can store the watched
// directories correctly and still leave the server watching nothing, which is exactly the
// difference between the whole-configuration save and the per-section one.
type Dirwatchers struct {
	// Stops records how many times the watchers were stopped.
	Stops int

	// Starts records the directories each Start was given, in order.
	Starts []domain.Dirwatches
}

var _ ports.Dirwatchers = (*Dirwatchers)(nil)

// NewDirwatchers returns watchers that are not watching anything.
func NewDirwatchers() *Dirwatchers {
	return &Dirwatchers{}
}

// Stop records that the watchers were stopped.
func (dirwatchers *Dirwatchers) Stop(ctx context.Context) {
	dirwatchers.Stops++
}

// Start records the directories the watchers were started with.
func (dirwatchers *Dirwatchers) Start(ctx context.Context, watches domain.Dirwatches) {
	dirwatchers.Starts = append(dirwatchers.Starts, watches)
}

// Watching reports the directories the most recent Start was given, which is what the server is
// left watching.
func (dirwatchers *Dirwatchers) Watching() domain.Dirwatches {
	if len(dirwatchers.Starts) == 0 {
		return nil
	}

	return dirwatchers.Starts[len(dirwatchers.Starts)-1]
}

// WatchingNothing reports whether the most recent Start was given no directories to watch.
func (dirwatchers *Dirwatchers) WatchingNothing() bool {
	return len(dirwatchers.Watching()) == 0
}
