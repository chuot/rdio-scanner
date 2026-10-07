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

package fakes

import (
	"context"

	"rdio-scanner/server-ng/internal/application/ports"
)

// PasswordVerifier is an in-memory credential check. It reports a match when the submitted
// credential is the one the handler says the server has on record, which is the shape of a
// hashed-credential check without the test depending on a hashing module.
//
// It records every comparison it was asked to make, so a test can show that a refusal
// decided on the attempt count never reached the credential at all.
type PasswordVerifier struct {
	// Asked is the comparison the handler asked for, in the order it asked.
	Asked []CredentialComparison
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

// AskedFor reports how many times the handler examined a credential.
func (verifier *PasswordVerifier) AskedFor() int {
	return len(verifier.Asked)
}

// PasswordHasher is the storing side of the credential check. It records each credential it was
// asked to turn into a stored form and returns it unchanged, which is what lets the
// PasswordVerifier fake above compare two credentials by equality: in this in-memory world the
// stored form of a credential is the credential itself.
type PasswordHasher struct {
	// Hashed records each credential the handler asked to store, in order.
	Hashed []string

	// Err makes the storing itself fail, which is how a test exercises a password change the
	// server could not complete.
	Err error
}

var _ ports.PasswordHasher = (*PasswordHasher)(nil)

// NewPasswordHasher returns a hasher that has been asked for nothing.
func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{}
}

// Hash records the credential and returns it as its own stored form.
func (hasher *PasswordHasher) Hash(ctx context.Context, credential string) (string, error) {
	hasher.Hashed = append(hasher.Hashed, credential)

	if hasher.Err != nil {
		return "", hasher.Err
	}

	return credential, nil
}
