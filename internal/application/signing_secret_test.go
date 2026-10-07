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

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
)

func newSigningSecretFixture(stored string) (*EnsureAdminSigningSecretHandler, *fakes.Options) {
	options := &fakes.Options{Stored: domain.Options{Secret: stored}}

	return NewEnsureAdminSigningSecretHandler(options, options), options
}

// TestEnsureAdminSigningSecretStoresASecretWhenTheServerHasNone is the repaired half of Q66: a server
// whose stored configuration holds no secret makes one and puts it on record, so the key it signs with
// is the key the database holds and the next start finds it. See
// docs/specs/FEAT-004-legacy-defect-repairs/decision-signing-secret-persisted.md.
func TestEnsureAdminSigningSecretStoresASecretWhenTheServerHasNone(t *testing.T) {
	handler, options := newSigningSecretFixture("")

	result, err := handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})
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
	handler, options := newSigningSecretFixture("legacy secret value")

	result, err := handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})
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

	handler, options := newSigningSecretFixture(kept)

	result, err := handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})
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
}

// TestEnsureAdminSigningSecretReportsASecretItCouldNotPutOnRecord keeps the failure visible: a secret
// that was made but not stored is a secret the next start will not find.
func TestEnsureAdminSigningSecretReportsASecretItCouldNotPutOnRecord(t *testing.T) {
	handler, options := newSigningSecretFixture("")
	options.SecretErr = errors.New("the options table is read-only")

	result, err := handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})

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
	handler, options := newSigningSecretFixture("")
	options.Err = errors.New("the options table is not there")

	result, err := handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})

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
	handler, options := newSigningSecretFixture("")

	result, err := handler.Handle(context.Background(), EnsureAdminSigningSecretCommand{})
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
