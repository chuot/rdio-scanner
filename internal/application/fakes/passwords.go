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
)

// PasswordVerifier is an in-memory credential check: it reports a match when the
// submitted credential is the one on record, the shape of a hashed check without
// a hashing module. It records every comparison, so a test can show a refusal
// decided on the attempt count never reached the credential.
type PasswordVerifier struct {
	// Asked is the comparison the handler asked for, in the order it asked.
	Asked []CredentialComparison

	// Upgrade makes the verifier report that the stored credential was hashed
	// below the cost the deployment is configured for.
	Upgrade bool
}

// CredentialComparison is one credential check that was asked for.
type CredentialComparison struct {
	Stored    string
	Submitted string
}

var _ ports.PasswordVerifier = (*PasswordVerifier)(nil)

// NewPasswordVerifier returns a credential check that has not been asked anything.
func NewPasswordVerifier() *PasswordVerifier {
	return &PasswordVerifier{}
}

// Matches reports whether the submitted credential is the one on record.
func (verifier *PasswordVerifier) Matches(ctx context.Context, storedCredential string, submittedCredential string) bool {
	verifier.Asked = append(verifier.Asked, CredentialComparison{
		Stored:    storedCredential,
		Submitted: submittedCredential,
	})

	return submittedCredential == storedCredential
}

// NeedsUpgrade reports whether the stored credential was hashed below the cost
// the deployment is configured for — the fake answers from its Upgrade knob.
func (verifier *PasswordVerifier) NeedsUpgrade(storedCredential string) bool {
	return verifier.Upgrade
}

// AskedFor reports how many times the handler examined a credential.
func (verifier *PasswordVerifier) AskedFor() int {
	return len(verifier.Asked)
}

// PasswordHasher is the storing side: it records each credential it was asked to
// store and returns it unchanged, so the PasswordVerifier fake can compare
// credentials by equality — in this world the stored form is the credential.
type PasswordHasher struct {
	// Hashed records each credential the handler asked to store, in order.
	Hashed []string

	// Err makes the storing itself fail, how a password change the server could
	// not complete is tested.
	Err error

	// Form is the stored form Hash answers with when set — how a login test
	// tells a renewed hash from the credential it was made from.
	Form string
}

var _ ports.PasswordHasher = (*PasswordHasher)(nil)

// NewPasswordHasher returns a hasher that has been asked for nothing.
func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{}
}

// Hash records the credential and returns it as its own stored form, or the
// Form the test set.
func (hasher *PasswordHasher) Hash(ctx context.Context, credential string) (string, error) {
	hasher.Hashed = append(hasher.Hashed, credential)

	if hasher.Err != nil {
		return "", hasher.Err
	}

	if hasher.Form != "" {
		return hasher.Form, nil
	}

	return credential, nil
}

// HashFor reports how many credentials the handler asked to hash.
func (hasher *PasswordHasher) HashFor() int {
	return len(hasher.Hashed)
}
