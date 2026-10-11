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
	"testing"

	"rdio-scanner/internal/domain"
)

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

	if !fixture.loggedAs(domain.LogSourceAdmin, "the administrator password was changed") {
		t.Errorf("logs = %v, want the change recorded under the administrator account", fixture.logs.Stored)
	}

	if fixture.loggedAs(domain.LogSourceAdmin, "the administrator password was not changed") {
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

	if fixture.loggedAs(domain.LogSourceAdmin, "the administrator password was changed") {
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

// TestChangeAdminPasswordChangesTheCredentialWhenNoCurrentCredentialIsOffered pins the preserved quirk
// that the legacy server compares a credential it was given and skips the comparison
// entirely for a payload that names none, so an absent field is not the same as an empty one.

// TestChangeAdminPasswordChangesTheCredentialWhenNoCurrentCredentialIsOffered pins the preserved quirk
// that the legacy server compares a credential it was given and skips the comparison
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

// TestChangeAdminPasswordRefusesAnEmptyCurrentCredentialThatIsOffered is the other half of the preserved quirk:
// an empty credential that was actually offered is compared, and does not match.

// TestChangeAdminPasswordRefusesAnEmptyCurrentCredentialThatIsOffered is the other half of the preserved quirk:
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

// TestChangeAdminPasswordSaysWhyTheChangeDidNotHappen replaces TestChangeAdminPasswordLogsTheSameWordsWhateverWentWrong,
// which pinned the legacy server discarding the reason a change failed and writing one fixed sentence
// naming the current password. The outcome and the error the caller receives are unchanged; only what the
// log view holds changes, and it now holds the reason.

// TestChangeAdminPasswordSaysWhyTheChangeDidNotHappen replaces TestChangeAdminPasswordLogsTheSameWordsWhateverWentWrong,
// which pinned the legacy server discarding the reason a change failed and writing one fixed sentence
// naming the current password. The outcome and the error the caller receives are unchanged; only what the
// log view holds changes, and it now holds the reason.
func TestChangeAdminPasswordSaysWhyTheChangeDidNotHappen(t *testing.T) {
	cases := []struct {
		what  string
		setup func(fixture *adminFixture)
		give  ChangeAdminPasswordCommand
		want  string
	}{
		{
			what:  "the credential would not hash",
			setup: func(fixture *adminFixture) { fixture.hashes.Err = errors.New("no such hash") },
			give:  ChangeAdminPasswordCommand{NewCredential: newCredential},
			want:  "the administrator password was not changed: no such hash",
		},
		{
			what:  "the options would not be written",
			setup: func(fixture *adminFixture) { fixture.options.SaveErr = errors.New("disk full") },
			give:  ChangeAdminPasswordCommand{NewCredential: newCredential},
			want:  "the administrator password was not changed: disk full",
		},
		{
			what:  "the current credential did not match",
			setup: func(fixture *adminFixture) {},
			give: ChangeAdminPasswordCommand{
				CurrentCredential:      "not the credential on record",
				CurrentCredentialGiven: true,
				NewCredential:          newCredential,
			},
			want: "the administrator password was not changed: current password is invalid",
		},
	}

	for _, tc := range cases {
		fixture := newAdminFixture()
		tc.setup(fixture)

		result, err := fixture.changePassword.Handle(context.Background(), tc.give)

		if result.Outcome != AdminPasswordRefused {
			t.Fatalf("%s: outcome = %q, want %q", tc.what, result.Outcome, AdminPasswordRefused)
		}

		if err == nil {
			t.Fatalf("%s: the caller is told the reason as well", tc.what)
		}

		if !fixture.loggedAs(domain.LogSourceAdmin, tc.want) {
			t.Errorf("%s: logs = %v, want the reason the change did not happen, under the administrator account", tc.what, fixture.logs.Stored)
		}
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
