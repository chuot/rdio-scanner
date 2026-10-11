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

	"rdio-scanner/internal/application/ports"
)

// Bcrypt is the credential adapter the server runs with: it compares and stores administrator
// credentials the way the legacy server does, with bcrypt at its default cost.
//
// The legacy server keeps the administrator credential in its options table as a bcrypt hash —
// the one it ships with is generated at startup from the default credential, and every password
// change replaces it with a new hash — so a deployment that swaps servers keeps the credential its
// administrator already set. This adapter is what makes that hash readable by both.
type Bcrypt struct {
	// cost is the bcrypt cost the deployment is configured for. The hash this
	// adapter writes, and the bar `NeedsUpgrade` measures stored hashes against,
	// are both set by it.
	cost int
}

var _ ports.PasswordVerifier = (*Bcrypt)(nil)
var _ ports.PasswordHasher = (*Bcrypt)(nil)
var _ ports.PasswordUpgrader = (*Bcrypt)(nil)

// NewBcrypt returns the credential adapter, hashing at the cost the deployment is
// configured for. A cost below bcrypt's own floor — the zero value of a
// configuration that never heard of the option — is answered with bcrypt's
// default, the cost the legacy server hashed at, so a deployment that says
// nothing hashes exactly as it does today.
func NewBcrypt(cost int) *Bcrypt {
	if cost < bcrypt.MinCost {
		cost = bcrypt.DefaultCost
	}

	return &Bcrypt{cost: cost}
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
// The cost is the one the deployment is configured for, whose default
// is bcrypt's default — the cost the legacy server uses both for the credential it
// ships with and for every credential its administrator replaces — so the hashes
// the two servers write are the same shape until an operator raises the cost.
//
// It fails for a credential bcrypt cannot store — longer than the 72 bytes bcrypt reads — which is
// the failure the legacy server returns from its password change as well.
func (hashes *Bcrypt) Hash(ctx context.Context, credential string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(credential), hashes.cost)
	if err != nil {
		return "", fmt.Errorf("passwords.hash: %w", err)
	}

	return string(hash), nil
}

// NeedsUpgrade reports whether a stored credential was hashed more cheaply than
// the deployment hashes at now. The cost lives in the hash itself —
// bcrypt writes it into the stored form — so the question is answered from the
// stored value, and a stored value that is not a hash reports no upgrade: there
// is no cost in it to raise.
func (hashes *Bcrypt) NeedsUpgrade(storedCredential string) bool {
	cost, err := bcrypt.Cost([]byte(storedCredential))
	if err != nil {
		return false
	}

	return cost < hashes.cost
}
