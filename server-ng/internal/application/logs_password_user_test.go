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
	"strings"
	"testing"
	"time"

	"rdio-scanner/server-ng/internal/application/fakes"
	"rdio-scanner/server-ng/internal/domain"
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

	fixture.searchLogs = NewSearchLogsHandler(fixture.logs)

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

// loggedContaining reports whether the server wrote a log line containing the fragment, which is
// how these tests pin the legacy wording byte for byte.
func (fixture *adminFixture) loggedContaining(fragment string) bool {
	for _, entry := range fixture.logs.Stored {
		if strings.Contains(entry.Message, fragment) {
			return true
		}
	}

	return false
}

func TestSearchLogsQueryReturnsTheMatchingEntriesAndTheTotal(t *testing.T) {
	fixture := newAdminFixture()

	fixture.logs.Stored = []domain.LogEntry{
		{DateTime: adminTime.Add(-3 * time.Hour), Level: domain.LogLevelInfo, Message: "server started"},
		{DateTime: adminTime.Add(-2 * time.Hour), Level: domain.LogLevelWarn, Message: "invalid access code nope for ip 203.0.113.7"},
		{DateTime: adminTime.Add(-1 * time.Hour), Level: domain.LogLevelWarn, Message: "configuration changed"},
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

	if page.Results[0].Message != "invalid access code nope for ip 203.0.113.7" {
		t.Errorf("results = %v, want the oldest matching entry first for an ascending search", page.Results)
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

func TestChangeAdminPasswordStoresTheNewCredentialAndSaysSoInItsLog(t *testing.T) {
	fixture := newAdminFixture()

	result, err := fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		CurrentCredential:      storedCredential,
		CurrentCredentialGiven: true,
		NewCredential:          newCredential,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AdminPasswordChanged {
		t.Fatalf("outcome = %q, want %q", result.Outcome, AdminPasswordChanged)
	}

	if fixture.options.Stored.AdminPasswordHash != newCredential {
		t.Errorf("stored credential = %q, want the new credential on record", fixture.options.Stored.AdminPasswordHash)
	}

	if !fixture.loggedContaining("admin password changed.") {
		t.Errorf("logs = %v, want the legacy notice with its trailing period", fixture.logs.Stored)
	}

	if fixture.loggedContaining("admin.passwordhandler.post") {
		t.Errorf("logs = %v, want no failure reported for a change that worked", fixture.logs.Stored)
	}
}

func TestChangeAdminPasswordRefusesACurrentCredentialThatDoesNotMatch(t *testing.T) {
	fixture := newAdminFixture()

	result, err := fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		CurrentCredential:      "not the credential",
		CurrentCredentialGiven: true,
		NewCredential:          newCredential,
	})

	if err == nil {
		t.Error("the reason the change was refused must reach the caller")
	}

	if result.Outcome != AdminPasswordRefused {
		t.Errorf("outcome = %q, want %q", result.Outcome, AdminPasswordRefused)
	}

	if fixture.passwords.AskedFor() != 1 {
		t.Fatalf("credential checks = %d, want the offered credential compared with the one on record", fixture.passwords.AskedFor())
	}

	asked := fixture.passwords.Asked[0]

	if asked.Stored != storedCredential || asked.Submitted != "not the credential" {
		t.Errorf("credential check = %+v, want the offered credential compared with the one on record", asked)
	}

	if fixture.options.Stored.AdminPasswordHash != storedCredential {
		t.Errorf("stored credential = %q, want the credential on record left alone", fixture.options.Stored.AdminPasswordHash)
	}

	if len(fixture.hashes.Hashed) != 0 {
		t.Errorf("credentials stored = %v, want the new credential never stored", fixture.hashes.Hashed)
	}

	if fixture.loggedContaining("admin password changed.") {
		t.Error("a refused change must not report that the password changed")
	}
}

func TestChangeAdminPasswordRefusesAnEmptyNewCredentialBeforeCheckingAnything(t *testing.T) {
	fixture := newAdminFixture()

	result, err := fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		CurrentCredential:      storedCredential,
		CurrentCredentialGiven: true,
		NewCredential:          "",
	})

	if err == nil {
		t.Error("an empty new credential is a refusal the caller is told about")
	}

	if result.Outcome != AdminPasswordRefused {
		t.Errorf("outcome = %q, want %q", result.Outcome, AdminPasswordRefused)
	}

	if fixture.passwords.AskedFor() != 0 {
		t.Errorf("credential checks = %d, want the refusal decided before the credential was examined", fixture.passwords.AskedFor())
	}

	if len(fixture.hashes.Hashed) != 0 {
		t.Errorf("credentials stored = %v, want nothing stored", fixture.hashes.Hashed)
	}
}

// TestChangeAdminPasswordChangesTheCredentialWhenNoCurrentCredentialIsOffered pins preserved
// quirk Q14: the legacy server compares a credential it was given and skips the comparison
// entirely for a payload that names none, so an absent field is not the same as an empty one.
func TestChangeAdminPasswordChangesTheCredentialWhenNoCurrentCredentialIsOffered(t *testing.T) {
	fixture := newAdminFixture()

	result, err := fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		CurrentCredentialGiven: false,
		NewCredential:          newCredential,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AdminPasswordChanged {
		t.Fatalf("outcome = %q, want %q", result.Outcome, AdminPasswordChanged)
	}

	if fixture.passwords.AskedFor() != 0 {
		t.Errorf("credential checks = %d, want no comparison at all: an empty credential is checked and refused, an absent one is not offered", fixture.passwords.AskedFor())
	}

	if fixture.options.Stored.AdminPasswordHash != newCredential {
		t.Errorf("stored credential = %q, want the new credential on record", fixture.options.Stored.AdminPasswordHash)
	}
}

// TestChangeAdminPasswordRefusesAnEmptyCurrentCredentialThatIsOffered is the other half of Q14:
// an empty credential that was actually offered is compared, and does not match.
func TestChangeAdminPasswordRefusesAnEmptyCurrentCredentialThatIsOffered(t *testing.T) {
	fixture := newAdminFixture()

	result, err := fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		CurrentCredential:      "",
		CurrentCredentialGiven: true,
		NewCredential:          newCredential,
	})

	if err == nil {
		t.Error("an offered credential that does not match is a refusal")
	}

	if result.Outcome != AdminPasswordRefused {
		t.Errorf("outcome = %q, want %q", result.Outcome, AdminPasswordRefused)
	}

	if fixture.passwords.AskedFor() != 1 {
		t.Errorf("credential checks = %d, want an offered credential compared even when it is empty", fixture.passwords.AskedFor())
	}
}

func TestChangeAdminPasswordReportsTheNeedToChangeFromTheReReadConfiguration(t *testing.T) {
	fixture := newAdminFixture()

	// The submitted change would leave the server reporting that the password still needs
	// changing; the configuration re-derived from the store says otherwise, and that is what the
	// administrator is told.
	reloaded := fixture.options.Stored
	reloaded.AdminPasswordNeedChange = false

	fixture.options.Reloaded = &reloaded

	result, err := fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		CurrentCredentialGiven: false,
		NewCredential:          domain.DefaultAdminPassword,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.PasswordNeedChange {
		t.Error("the administrator is told what the configuration re-derived from the store says, not what was just submitted")
	}

	// The submitted credential was the initial one, which is what makes a server report that the
	// password still needs changing. Had the answer come from the submitted options, the result
	// above would have been true.
}

func TestChangeAdminPasswordStillReportsTheNeedToChangeForTheDefaultCredential(t *testing.T) {
	fixture := newAdminFixture()

	result, err := fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		CurrentCredentialGiven: false,
		NewCredential:          domain.DefaultAdminPassword,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !result.PasswordNeedChange {
		t.Errorf("passwordNeedChange = %v, want a credential that is still the initial one reported as needing changing", result.PasswordNeedChange)
	}

	result, err = fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		CurrentCredentialGiven: false,
		NewCredential:          newCredential,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.PasswordNeedChange {
		t.Error("a credential that is not the initial one stops being reported as needing changing")
	}
}

// TestChangeAdminPasswordLogsTheSameWordsWhateverWentWrong pins that the legacy server discards
// the real reason a change failed and writes one fixed sentence naming the current password.
func TestChangeAdminPasswordLogsTheSameWordsWhateverWentWrong(t *testing.T) {
	fixture := newAdminFixture()
	fixture.options.SaveErr = errors.New("disk full")

	result, err := fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		CurrentCredentialGiven: false,
		NewCredential:          newCredential,
	})

	if result.Outcome != AdminPasswordRefused {
		t.Fatalf("outcome = %q, want %q", result.Outcome, AdminPasswordRefused)
	}

	if !errors.Is(err, fixture.options.SaveErr) {
		t.Errorf("error = %v, want the real reason returned to the caller even though the log does not name it", err)
	}

	if !fixture.loggedContaining("admin.passwordhandler.post: unable to change admin password, current password is invalid") {
		t.Errorf("logs = %v, want the fixed legacy wording, which names the current password whatever actually failed", fixture.logs.Stored)
	}
}

func TestChangeAdminPasswordRefusesWhenTheCredentialCannotBeStored(t *testing.T) {
	fixture := newAdminFixture()
	fixture.hashes.Err = errors.New("no such hash")

	result, err := fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		CurrentCredentialGiven: false,
		NewCredential:          newCredential,
	})

	if err == nil {
		t.Error("a credential that could not be stored is a refusal the caller is told about")
	}

	if result.Outcome != AdminPasswordRefused {
		t.Errorf("outcome = %q, want %q", result.Outcome, AdminPasswordRefused)
	}

	if fixture.options.Stored.AdminPasswordHash != storedCredential {
		t.Errorf("stored credential = %q, want the credential on record left alone", fixture.options.Stored.AdminPasswordHash)
	}
}

func TestAddAccessUserStoresANewAccessAndAnnouncesTheConfiguration(t *testing.T) {
	fixture := newAdminFixture()

	result, err := fixture.addUser.Handle(context.Background(), AddAccessUserCommand{
		User: domain.Access{Code: "harbour-code", Ident: "harbour", Limit: 2},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessUserSaved {
		t.Fatalf("outcome = %q, want %q", result.Outcome, AccessUserSaved)
	}

	if len(fixture.accesses.Stored) != 2 {
		t.Fatalf("accesses = %v, want the new access added to the list", fixture.accesses.Stored)
	}

	if fixture.admin.AnnouncedCount() != 1 {
		t.Fatalf("administrator announcements = %d, want the open administrator connections told once", fixture.admin.AnnouncedCount())
	}

	announced := fixture.admin.Announced[0]

	if len(announced.Access) != 2 || len(announced.Systems) != 1 || len(announced.Dirwatches) != 1 {
		t.Errorf("announced = %+v, want the whole configuration that resulted from the save", announced)
	}

	// Adding a user tells the administrator connections and not the listeners: a listener keeps
	// the scope it was given when it connected until it reconnects.
	if len(fixture.hub.BroadcastConfigs) != 0 {
		t.Errorf("listener broadcasts = %d, want none: an access-user change is not announced to listeners", len(fixture.hub.BroadcastConfigs))
	}
}

func TestAddAccessUserUpdatesTheAccessAlreadyConfiguredUnderTheSameCode(t *testing.T) {
	fixture := newAdminFixture()

	scope := domain.AccessScope{Present: true, Wildcard: true}

	result, err := fixture.addUser.Handle(context.Background(), AddAccessUserCommand{
		User: domain.Access{
			ID:         99,
			Code:       "dispatch-code",
			Ident:      "dispatch-renamed",
			Expiration: uint64(adminTime.Add(24 * time.Hour).Unix()),
			Limit:      3,
			Scope:      scope,
			Order:      1,
		},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessUserSaved {
		t.Fatalf("outcome = %q, want %q", result.Outcome, AccessUserSaved)
	}

	if len(fixture.accesses.Stored) != 1 {
		t.Fatalf("accesses = %v, want the access under the same code updated rather than added", fixture.accesses.Stored)
	}

	updated := fixture.accesses.Stored[0]

	if updated.Ident != "dispatch-renamed" || updated.Limit != 3 || !updated.HasScope() {
		t.Errorf("updated = %+v, want the identity, limit, and scope replaced", updated)
	}

	if updated.Expiration != uint64(adminTime.Add(24*time.Hour).Unix()) {
		t.Errorf("expiration = %d, want the expiry replaced", updated.Expiration)
	}

	// The display order belongs to the access already on the list: an update replaces everything
	// the administrator edits except the ordering.
	if updated.Order != 5 {
		t.Errorf("order = %d, want the order the access already had kept", updated.Order)
	}
}

func TestAddAccessUserReportsAFailureWhenTheAccessListCannotBeStored(t *testing.T) {
	fixture := newAdminFixture()
	fixture.accesses.SaveErr = errors.New("constraint failed")

	result, err := fixture.addUser.Handle(context.Background(), AddAccessUserCommand{
		User: domain.Access{Code: "harbour-code", Ident: "harbour"},
	})

	if err == nil {
		t.Error("the reason the save failed must reach the caller")
	}

	if result.Outcome != AccessUserSaveFailed {
		t.Errorf("outcome = %q, want %q", result.Outcome, AccessUserSaveFailed)
	}

	if !fixture.loggedContaining("admin.useraddhandler.post: constraint failed") {
		t.Errorf("logs = %v, want the legacy failure wording for this endpoint", fixture.logs.Stored)
	}

	if fixture.admin.AnnouncedCount() != 0 {
		t.Error("a save that did not work must not announce a configuration change")
	}
}

func TestRemoveAccessUserRemovesEveryAccessUnderTheIdentity(t *testing.T) {
	fixture := newAdminFixture()

	fixture.accesses.Stored = domain.Accesses{
		domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch"},
		domain.Access{ID: 2, Code: "dispatch-other-code", Ident: "dispatch"},
		domain.Access{ID: 3, Code: "harbour-code", Ident: "harbour"},
	}

	result, err := fixture.removeUser.Handle(context.Background(), RemoveAccessUserCommand{Ident: "dispatch"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessUserSaved {
		t.Fatalf("outcome = %q, want %q", result.Outcome, AccessUserSaved)
	}

	if len(fixture.accesses.Stored) != 1 || fixture.accesses.Stored[0].Ident != "harbour" {
		t.Errorf("accesses = %v, want every access carrying the identity removed, not one of them", fixture.accesses.Stored)
	}

	if fixture.admin.AnnouncedCount() != 1 {
		t.Error("the open administrator connections must be told the configuration that resulted")
	}
}

// TestRemoveAccessUserReportsNothingWasRemoved pins that a removal matching no access stores
// nothing, announces nothing, logs nothing — and is still answered as a success, which is what
// the legacy server answers.
func TestRemoveAccessUserReportsNothingWasRemoved(t *testing.T) {
	fixture := newAdminFixture()

	result, err := fixture.removeUser.Handle(context.Background(), RemoveAccessUserCommand{Ident: "nobody"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AccessUserNotRemoved {
		t.Errorf("outcome = %q, want %q", result.Outcome, AccessUserNotRemoved)
	}

	if len(fixture.accesses.Stored) != 1 {
		t.Errorf("accesses = %v, want the access list left exactly as it was", fixture.accesses.Stored)
	}

	if fixture.admin.AnnouncedCount() != 0 {
		t.Error("nothing was changed, so nothing is announced")
	}

	if len(fixture.logs.Stored) != 0 {
		t.Errorf("logs = %v, want nothing written for a removal that matched nothing", fixture.logs.Stored)
	}
}

func TestRemoveAccessUserReportsAFailureWhenTheAccessListCannotBeStored(t *testing.T) {
	fixture := newAdminFixture()
	fixture.accesses.SaveErr = errors.New("constraint failed")

	result, err := fixture.removeUser.Handle(context.Background(), RemoveAccessUserCommand{Ident: "dispatch"})

	if err == nil {
		t.Error("the reason the save failed must reach the caller")
	}

	if result.Outcome != AccessUserSaveFailed {
		t.Errorf("outcome = %q, want %q", result.Outcome, AccessUserSaveFailed)
	}

	if !fixture.loggedContaining("admin.userremovehandler.post: constraint failed") {
		t.Errorf("logs = %v, want the legacy failure wording for this endpoint", fixture.logs.Stored)
	}
}
