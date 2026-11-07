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

package passwords

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"rdio-scanner/internal/application/ports"
)

// Bcrypt is the credential adapter the server runs with: it compares and stores administrator
// credentials the way the legacy server does, so a deployment that swaps servers keeps the
// credential its administrator already set.
type Bcrypt struct {
	// cost is the bcrypt cost the deployment is configured for; the hash this
	// adapter writes and the bar NeedsUpgrade measures stored hashes against are
	// both set by it.
	cost int
}

var _ ports.PasswordVerifier = (*Bcrypt)(nil)
var _ ports.PasswordHasher = (*Bcrypt)(nil)
var _ ports.PasswordUpgrader = (*Bcrypt)(nil)

// NewBcrypt returns the credential adapter, hashing at the cost the deployment is configured
// for; a cost below bcrypt's own floor falls back to bcrypt's default, the cost the legacy
// server hashed at.
func NewBcrypt(cost int) *Bcrypt {
	if cost < bcrypt.MinCost {
		cost = bcrypt.DefaultCost
	}

	return &Bcrypt{cost: cost}
}

// Matches reports whether the submitted credential reproduces the stored hash; every way
// bcrypt can say no is the same refusal here, with the caller never told which happened.
func (hashes *Bcrypt) Matches(ctx context.Context, storedCredential string, submittedCredential string) bool {
	return bcrypt.CompareHashAndPassword([]byte(storedCredential), []byte(submittedCredential)) == nil
}

// Hash returns the bcrypt hash the server stores for a credential, at the configured cost,
// the legacy default until an operator raises it; it fails for a credential bcrypt cannot
// store.
func (hashes *Bcrypt) Hash(ctx context.Context, credential string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(credential), hashes.cost)
	if err != nil {
		return "", fmt.Errorf("passwords.hash: %w", err)
	}

	return string(hash), nil
}

// NeedsUpgrade reports whether a stored credential was hashed more cheaply than the deployment
// hashes at now; the cost lives in the stored hash itself, and a stored value that is not a
// hash reports no upgrade.
func (hashes *Bcrypt) NeedsUpgrade(storedCredential string) bool {
	cost, err := bcrypt.Cost([]byte(storedCredential))
	if err != nil {
		return false
	}

	return cost < hashes.cost
}
