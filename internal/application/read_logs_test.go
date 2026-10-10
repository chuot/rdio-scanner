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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

var adminTime = time.Date(2026, 6, 14, 9, 30, 0, 0, time.UTC)

const (
	storedCredential = "the-credential-on-record"
	newCredential    = "a-replaced-credential"
)

func newAdminFixture() *adminFixture {
	fixture := &adminFixture{
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
		clock:       fakes.NewClock(adminTime),
		passwords:   fakes.NewPasswordVerifier(),
		hashes:      fakes.NewPasswordHasher(),
	}

	fixture.options.Stored.AdminPasswordHash = storedCredential
	fixture.options.Stored.AdminPasswordNeedChange = true

	fixture.accesses.Stored = domain.Accesses{
		domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch", Order: 5},
	}

	fixture.systems.Stored = domain.Systems{{ID: 1, SystemRef: 4, Label: "Grand Metro"}}
	fixture.dirwatches.Stored = domain.Dirwatches{{ID: 1, Directory: "/spool/live"}}

	fixture.searchLogs = NewSearchLogsHandler(fixture.logs, fixture.clock)

	fixture.changePassword = NewChangeAdminPasswordHandler(
		fixture.options, fixture.passwords, fixture.hashes, fixture.logs, fixture.clock,
	)

	userPorts := AccessUserPorts{
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
		Logs:  fixture.logs,
		Admin: fixture.admin,
		Clock: fixture.clock,
	}

	fixture.addUser = NewAddAccessUserHandler(userPorts)
	fixture.removeUser = NewRemoveAccessUserHandler(userPorts)

	return fixture
}

type adminFixture struct {
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
	clock       *fakes.Clock
	passwords   *fakes.PasswordVerifier
	hashes      *fakes.PasswordHasher

	searchLogs     *SearchLogsHandler
	changePassword *ChangeAdminPasswordHandler
	addUser        *AddAccessUserHandler
	removeUser     *RemoveAccessUserHandler
}

// loggedAs reports whether the server wrote a log line naming that part of the server and saying the
// fragment. The administrator endpoints write only about the administrator account, so the source is part
// of what these tests pin.

// loggedAs reports whether the server wrote a log line naming that part of the server and saying the
// fragment. The administrator endpoints write only about the administrator account, so the source is part
// of what these tests pin.
func (fixture *adminFixture) loggedAs(source domain.LogSource, fragment string) bool {
	for _, entry := range fixture.logs.Stored {
		if entry.Source == source && strings.Contains(entry.Message, fragment) {
			return true
		}
	}

	return false
}

func TestSearchLogsQueryReturnsTheMatchingEntriesAndTheTotal(t *testing.T) {
	fixture := newAdminFixture()

	fixture.logs.Stored = []domain.LogEntry{
		{DateTime: adminTime.Add(-3 * time.Hour), Level: domain.LogLevelInfo, Source: domain.LogSourceServer, Message: "server started"},
		{DateTime: adminTime.Add(-2 * time.Hour), Level: domain.LogLevelWarn, Source: domain.LogSourceAccess, Message: "a listener from 203.0.113.7 presented an access code that is not valid"},
		{DateTime: adminTime.Add(-1 * time.Hour), Level: domain.LogLevelWarn, Source: domain.LogSourceConfig, Message: "the configuration changed"},
	}

	page, err := fixture.searchLogs.Handle(context.Background(), SearchLogsQuery{
		Search: domain.LogSearch{Level: domain.LogLevelWarn, Limit: 1, Ascending: true},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if page.Count != 2 {
		t.Errorf("count = %d, want the total the criteria matches, not the number of rows on this page", page.Count)
	}

	if len(page.Results) != 1 {
		t.Fatalf("results = %d, want the limit applied to the rows", len(page.Results))
	}

	if page.Results[0].Message != "a listener from 203.0.113.7 presented an access code that is not valid" {
		t.Errorf("results = %v, want the oldest matching entry first for an ascending search", page.Results)
	}

	// The span belongs to everything the criteria match, and the page is one entry of it: the log view
	// pages through the span, so its ends stay where they are while the page moves.
	if !page.DateStart.Equal(adminTime.Add(-2*time.Hour)) || !page.DateStop.Equal(adminTime.Add(-1*time.Hour)) {
		t.Errorf("the span of the two matching entries is %v to %v, want the ends of both and not of the one that was returned", page.DateStart, page.DateStop)
	}
}

// The log view records its own failure, in the same view the search was asked for. An administrator who
// is shown an empty log has to be able to tell an empty log apart from a log that cannot be read, and
// the legacy server has always said which one in the log itself.

// The log view records its own failure, in the same view the search was asked for. An administrator who
// is shown an empty log has to be able to tell an empty log apart from a log that cannot be read, and
// the legacy server has always said which one in the log itself.
func TestSearchLogsRecordsTheFailureItCouldNotAnswerWith(t *testing.T) {
	fixture := newAdminFixture()

	fixture.logs.SearchErr = errors.New("logs.search: no such table: logs in SELECT COUNT(*) FROM \"logs\" WHERE TRUE")

	_, err := fixture.searchLogs.Handle(context.Background(), SearchLogsQuery{
		Search: domain.ParseLogSearch(map[string]any{}),
	})
	if err == nil {
		t.Fatal("a log store that cannot be read must reach the caller, which answers it as a failure")
	}

	var recorded bool

	for _, entry := range fixture.logs.Stored {
		if entry.Message == fmt.Sprintf("the log search failed: %v", err) {
			recorded = true

			if entry.Level != domain.LogLevelError {
				t.Errorf("the failure is recorded at level %q, want an error", entry.Level)
			}

			if entry.Source != domain.LogSourceLogs {
				t.Errorf("the failure is filed under %q, want it filed with the log view", entry.Source)
			}
		}
	}

	if !recorded {
		t.Errorf("the failure to read the log view is not in the log view naming what failed: %v", fixture.logs.Stored)
	}
}

func TestSearchLogsQueryReportsAFailingStore(t *testing.T) {
	fixture := newAdminFixture()
	fixture.logs.Err = errors.New("no such table: logs")

	_, err := fixture.searchLogs.Handle(context.Background(), SearchLogsQuery{
		Search: domain.ParseLogSearch(map[string]any{}),
	})

	if err == nil {
		t.Error("a log store that cannot be read must reach the caller, which answers it as a failure")
	}
}
