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
	"testing"
	"time"

	"rdio-scanner/internal/domain"
)

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

	if !fixture.loggedAs(domain.LogSourceAdmin, "the administrator user could not be added: constraint failed") {
		t.Errorf("logs = %v, want what the server was trying to do and the failure's own text after it", fixture.logs.Stored)
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

// A removal matching no access stores, announces, and logs nothing, and still answers as a success.
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

	if !fixture.loggedAs(domain.LogSourceAdmin, "the administrator user could not be removed: constraint failed") {
		t.Errorf("logs = %v, want what the server was trying to do and the failure's own text after it", fixture.logs.Stored)
	}
}
