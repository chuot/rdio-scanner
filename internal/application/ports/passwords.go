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

package ports

import "context"

// PasswordVerifier reports whether the credential somebody submitted is the credential
// the server has on record for its administrator account.
//
// It is a port because comparing a password with the stored credential is a hashing
// detail: the credential is stored as a bcrypt hash, and bcrypt is a third-party module
// the application layer is not allowed to import. The use case owns the business rule —
// an administrator session is only started for the account's own credential — and the
// adapter owns how the two credentials are compared.
//
// The port reports a plain yes or no rather than an error because that is what the legacy
// server ends up doing with every answer it gets: a credential that does not match, a
// stored credential that cannot be read, and a hash that is malformed all produce the
// same refusal, and the caller is never told which one happened.
type PasswordVerifier interface {
	// Matches reports whether the submitted credential is the one on record.
	//
	// storedCredential is what the server keeps for the administrator account, as it was
	// read from the configuration; submittedCredential is what the caller offered. The
	// verifier decides how they are compared, so that nothing outside infrastructure has
	// to know that the stored form is a hash.
	Matches(ctx context.Context, storedCredential string, submittedCredential string) bool
}

// PasswordHasher turns a credential into the form the server keeps for it.
//
// It is a port for the same reason PasswordVerifier is: the form the legacy server keeps is a
// bcrypt hash, and the application layer is not allowed to know that. The use case decides when
// a credential is worth storing; the adapter decides what storing it looks like.
type PasswordHasher interface {
	// Hash returns the form of a credential the server can store and later compare against.
	Hash(ctx context.Context, credential string) (string, error)
}

// PasswordUpgrader reports whether a stored credential was hashed more cheaply
// than the deployment is configured to hash now (CA-013.5). The verifier proved
// the credential; the upgrade renews the stored form at the configured cost, so
// a deployment that raised its bcrypt cost does not keep answering logins with
// the cheap hash. It is a separate port from the verifier and the hasher
// because the login use case asks only this question and the renewal answer —
// it does not decide what "cheaper" means, and bcrypt's cost lives in the
// stored hash, which is the adapter's to read.
type PasswordUpgrader interface {
	// NeedsUpgrade reports whether the stored credential is below the configured cost.
	NeedsUpgrade(storedCredential string) bool
}
