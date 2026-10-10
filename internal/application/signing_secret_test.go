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

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

// signingSecretFixture is the signing-secret step together with the server log it reports to and the
// instant it stamps an entry with. The step writes to the log the way the other startup steps do, so a
// test of what it decided and a test of what it said are the same fixture.
type signingSecretFixture struct {
	options *fakes.Options
	logs    *fakes.Logs
	clock   *fakes.Clock

	handler *EnsureAdminSigningSecretHandler
}

var signingSecretTime = time.Date(2026, 4, 2, 6, 30, 0, 0, time.UTC)

func newSigningSecretFixture(stored string) *signingSecretFixture {
	options := &fakes.Options{Stored: domain.Options{Secret: stored}}
	logs := &fakes.Logs{}

	return &signingSecretFixture{
		options: options,
		logs:    logs,
		clock:   fakes.NewClock(signingSecretTime),
		handler: NewEnsureAdminSigningSecretHandler(options, options, logs, fakes.NewClock(signingSecretTime)),
	}
}

// TestEnsureAdminSigningSecretStoresASecretWhenTheServerHasNone is the repaired half of Q66: a server
// whose stored configuration holds no secret makes one and puts it on record, so the key it signs with
// is the key the database holds and the next start finds it.
func TestEnsureAdminSigningSecretStoresASecretWhenTheServerHasNone(t *testing.T) {
	fixture := newSigningSecretFixture("")
	options := fixture.options

	result, err := fixture.handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AdminSigningSecretStored {
		t.Errorf("outcome = %q, want the secret made and stored", result.Outcome)
	}

	if len(options.Secrets) != 1 {
		t.Fatalf("stored secrets = %v, want the one secret the server had to make", options.Secrets)
	}

	if len(options.Secrets[0]) != domain.SecretLength {
		t.Errorf("the stored secret is %d characters, want the %d the server signs with", len(options.Secrets[0]), domain.SecretLength)
	}

	if options.Stored.Secret != options.Secrets[0] {
		t.Errorf("the options hold %q, want the secret that was put on record", options.Stored.Secret)
	}
}

// TestEnsureAdminSigningSecretReplacesAStoredSecretTheServerCannotSignWith is the legacy database's
// case: a stored secret of the wrong length cannot sign a token, and the replacement is written down
// rather than used once and forgotten.
func TestEnsureAdminSigningSecretReplacesAStoredSecretTheServerCannotSignWith(t *testing.T) {
	fixture := newSigningSecretFixture("legacy secret value")
	options := fixture.options

	result, err := fixture.handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AdminSigningSecretStored {
		t.Errorf("outcome = %q, want the unusable stored secret replaced", result.Outcome)
	}

	if len(options.Secrets) != 1 || len(options.Secrets[0]) != domain.SecretLength {
		t.Fatalf("stored secrets = %v, want one secret of %d characters", options.Secrets, domain.SecretLength)
	}
}

// TestEnsureAdminSigningSecretLeavesAStoredSecretAlone is what makes a token outlive a restart: the
// secret on record is used as it stands, and nothing is written.
func TestEnsureAdminSigningSecretLeavesAStoredSecretAlone(t *testing.T) {
	kept := strings.Repeat("a", domain.SecretLength)

	fixture := newSigningSecretFixture(kept)
	options := fixture.options

	result, err := fixture.handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AdminSigningSecretAlreadyOnRecord {
		t.Errorf("outcome = %q, want the stored secret left where it is", result.Outcome)
	}

	if len(options.Secrets) != 0 {
		t.Errorf("stored secrets = %v, want nothing written over a secret that already works", options.Secrets)
	}

	if options.Stored.Secret != kept {
		t.Errorf("the options hold %q, want the stored secret kept exactly", options.Stored.Secret)
	}

	if len(fixture.logs.Stored) != 0 {
		t.Errorf("server log = %v, want a secret that was already on record to say nothing", fixture.logs.Stored)
	}
}

// TestEnsureAdminSigningSecretReportsASecretItCouldNotPutOnRecord keeps the failure visible: a secret
// that was made but not stored is a secret the next start will not find.
func TestEnsureAdminSigningSecretReportsASecretItCouldNotPutOnRecord(t *testing.T) {
	fixture := newSigningSecretFixture("")
	options := fixture.options
	options.SecretErr = errors.New("the options table is read-only")

	result, err := fixture.handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})

	if err == nil {
		t.Fatal("Handle reported no failure, want the store's failure returned")
	}

	if result.Outcome != AdminSigningSecretFailed {
		t.Errorf("outcome = %q, want the failure reported", result.Outcome)
	}

	if len(options.Secrets) != 0 {
		t.Errorf("stored secrets = %v, want nothing recorded by a store that refused", options.Secrets)
	}
}

// TestEnsureAdminSigningSecretReportsOptionsItCouldNotRead is the other way the step can fail before
// it decides anything.
func TestEnsureAdminSigningSecretReportsOptionsItCouldNotRead(t *testing.T) {
	fixture := newSigningSecretFixture("")
	options := fixture.options
	options.Err = errors.New("the options table is not there")

	result, err := fixture.handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})

	if err == nil {
		t.Fatal("Handle reported no failure, want the read's failure returned")
	}

	if result.Outcome != AdminSigningSecretFailed {
		t.Errorf("outcome = %q, want the failure reported", result.Outcome)
	}
}

// TestEnsureAdminSigningSecretCarriesNoCredential keeps the acknowledgement an acknowledgement: the
// secret the server signs with never leaves the handler.
func TestEnsureAdminSigningSecretCarriesNoCredential(t *testing.T) {
	fixture := newSigningSecretFixture("")
	options := fixture.options

	result, err := fixture.handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AdminSigningSecretStored {
		t.Fatalf("outcome = %q, want the secret stored", result.Outcome)
	}

	if len(result.Outcome) > 32 {
		t.Errorf("outcome = %q, want a status and not the secret the server signs with", result.Outcome)
	}

	if len(options.Secrets) != 1 {
		t.Errorf("stored secrets = %v, want the secret handed to the store and nowhere else", options.Secrets)
	}
}

// TestANewSigningSecretIsRecordedAsTheEndOfEverySessionBeforeIt is the line the legacy server printed on
// a terminal and never stored. A secret that changes ends every administrator session signed with the
// previous one, so "everybody was logged out at six thirty" is a question this entry answers — but only if
// it is in the log the operator can search.
func TestANewSigningSecretIsRecordedAsTheEndOfEverySessionBeforeIt(t *testing.T) {
	fixture := newSigningSecretFixture("")

	result, err := fixture.handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result.Outcome != AdminSigningSecretStored {
		t.Fatalf("outcome = %q, want the secret made and stored", result.Outcome)
	}

	wrote := fixture.logs.Stored

	if len(wrote) != 1 {
		t.Fatalf("server log = %v, want one entry saying the server made a new secret", wrote)
	}

	entry := wrote[0]

	if entry.Source != domain.LogSourceAdmin {
		t.Errorf("entry source = %q, want %q", entry.Source, domain.LogSourceAdmin)
	}

	if entry.Level != domain.LogLevelWarn {
		t.Errorf("entry level = %q, want a warning: the sessions signed with the secret it replaces stop "+
			"working", entry.Level)
	}

	const message = "the server generated a new administrator signing secret, which ends every session that " +
		"was open before it"

	if entry.Message != message {
		t.Errorf("entry message = %q, want %q", entry.Message, message)
	}

	if got := entry.DateTime; !got.Equal(signingSecretTime) {
		t.Errorf("entry stamped %v, want the instant the server made the secret", got)
	}

	if strings.Contains(entry.Message, fixture.options.Secrets[0]) {
		t.Error("the entry names the secret it is reporting on, want the fact and not the credential")
	}
}
