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

package ports

import "context"

// PasswordVerifier reports whether a submitted credential is the one the server has
// on record for its administrator account. The comparison is a hashing detail the
// adapter owns. The answer is a plain yes or no: a mismatch, an unreadable stored
// credential, and a malformed hash all refuse the same way, and the caller is never
// told which happened.
type PasswordVerifier interface {
	// Matches reports whether the submitted credential is the one on record. The
	// verifier decides how the stored and submitted forms compare, so nothing outside
	// infrastructure has to know the stored form is a hash.
	Matches(ctx context.Context, storedCredential string, submittedCredential string) bool
}

// PasswordHasher turns a credential into the form the server stores for it, for the
// same reason: the application decides when a credential is worth storing, the adapter
// decides what storing it looks like.
type PasswordHasher interface {
	// Hash returns the form of a credential the server can store and compare later.
	Hash(ctx context.Context, credential string) (string, error)
}

// PasswordUpgrader reports whether a stored credential was hashed more cheaply than
// the deployment hashes now, so a deployment that raised its cost renews old
// credentials at the next successful login instead of going on answering with the
// cheap hash. The login use case asks only this question; what "cheaper" means lives
// inside the stored hash, which is the adapter's to read.
type PasswordUpgrader interface {
	// NeedsUpgrade reports whether the stored credential is below the configured cost.
	NeedsUpgrade(storedCredential string) bool
}
