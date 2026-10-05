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

package passwords

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"rdio-scanner/server-ng/internal/application/ports"
)

// Bcrypt is the credential adapter the server runs with: it compares and stores administrator
// credentials the way the legacy server does, with bcrypt at its default cost.
//
// The legacy server keeps the administrator credential in its options table as a bcrypt hash —
// the one it ships with is generated at startup from the default credential, and every password
// change replaces it with a new hash — so a deployment that swaps servers keeps the credential its
// administrator already set. This adapter is what makes that hash readable by both.
type Bcrypt struct{}

var _ ports.PasswordVerifier = (*Bcrypt)(nil)
var _ ports.PasswordHasher = (*Bcrypt)(nil)

// NewBcrypt returns the credential adapter, which holds no state: the credential is always the
// one the caller hands it.
func NewBcrypt() *Bcrypt {
	return &Bcrypt{}
}

// Matches reports whether the submitted credential reproduces the stored hash.
//
// Every way bcrypt can say no is the same answer here — the credential is not the one on record,
// the stored value is not a hash at all, the stored value is empty, the credential is longer than
// bcrypt reads — because that is how the legacy server treats them. It compares the hash and
// refuses on any error, and the caller is never told which of those happened.
func (hashes *Bcrypt) Matches(ctx context.Context, storedCredential string, submittedCredential string) bool {
	return bcrypt.CompareHashAndPassword([]byte(storedCredential), []byte(submittedCredential)) == nil
}

// Hash returns the bcrypt hash the server stores for a credential.
//
// The cost is bcrypt's default, which is the cost the legacy server uses both for the credential
// it ships with and for every credential its administrator replaces, so the hashes the two servers
// write are the same shape.
//
// It fails for a credential bcrypt cannot store — longer than the 72 bytes bcrypt reads — which is
// the failure the legacy server returns from its password change as well.
func (hashes *Bcrypt) Hash(ctx context.Context, credential string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(credential), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("passwords.hash: %w", err)
	}

	return string(hash), nil
}
