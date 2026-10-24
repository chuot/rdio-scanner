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
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// legacyStoredCredential is the bcrypt hash the legacy server wrote into a real deployment's
// options table; this adapter reads it.
const legacyStoredCredential = `$2a$10$1GFizQ/sWHxheggO6YSNk.D4RnbiFbnTnuiMEuuf3mCsYQhRcsBQ6`

func TestACredentialTheLegacyServerStoredIsAcceptedByThisServer(t *testing.T) {
	hashes := NewBcrypt(bcrypt.DefaultCost)

	if !hashes.Matches(context.Background(), legacyStoredCredential, "rdio-scanner") {
		t.Error("the credential the legacy server stored does not verify here, so a deployment that swaps servers locks its administrator out")
	}
}

func TestAStoredCredentialRefusesAnotherOne(t *testing.T) {
	hashes := NewBcrypt(bcrypt.DefaultCost)

	for _, submitted := range []string{"rdio-scanner ", "Rdio-Scanner", "", "password"} {
		if hashes.Matches(context.Background(), legacyStoredCredential, submitted) {
			t.Errorf("the stored credential accepts %q", submitted)
		}
	}
}

// TestAStoredCredentialThatIsNotACredentialRefuses: a stored value that is not a hash,
// an empty column included, makes every comparison fail, and the adapter never turns
// that into a success.
func TestAStoredCredentialThatIsNotACredentialRefuses(t *testing.T) {
	hashes := NewBcrypt(bcrypt.DefaultCost)

	for _, stored := range []string{"", "not a bcrypt hash", "$2a$10$", `"$2a$10$1GFizQ/sWHxheggO6YSNk."`} {
		if hashes.Matches(context.Background(), stored, "rdio-scanner") {
			t.Errorf("a stored credential of %q is accepted, which would let anybody log in", stored)
		}
	}
}

func TestHashingACredentialProducesAHashThatVerifiesAgainstIt(t *testing.T) {
	hashes := NewBcrypt(bcrypt.DefaultCost)

	hash, err := hashes.Hash(context.Background(), "rdio-scanner")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if !hashes.Matches(context.Background(), hash, "rdio-scanner") {
		t.Fatalf("the hash this adapter wrote, %s, does not verify against the credential it was made from", hash)
	}

	if hashes.Matches(context.Background(), hash, "another credential") {
		t.Error("the hash accepts a credential it was not made from")
	}
}

// TestHashingIsBcryptAtTheLegacyCost: hashes are written at bcrypt's default cost,
// readable by the legacy server, keeping the two servers interchangeable.
func TestHashingIsBcryptAtTheLegacyCost(t *testing.T) {
	hashes := NewBcrypt(bcrypt.DefaultCost)

	hash, err := hashes.Hash(context.Background(), "rdio-scanner")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if !strings.HasPrefix(hash, "$2a$10$") {
		t.Errorf("hash = %s, want the same bcrypt form and cost the legacy server writes", hash)
	}
}

// TestHashingTheSameCredentialTwiceProducesDifferentHashes: two hashes of one credential
// never match, so a stored hash cannot be compared to another hash.
func TestHashingTheSameCredentialTwiceProducesDifferentHashes(t *testing.T) {
	hashes := NewBcrypt(bcrypt.DefaultCost)

	first, err := hashes.Hash(context.Background(), "rdio-scanner")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	second, err := hashes.Hash(context.Background(), "rdio-scanner")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if first == second {
		t.Error("the same credential hashed to the same value twice, which means the hash carries no salt")
	}

	for _, hash := range []string{first, second} {
		if !hashes.Matches(context.Background(), hash, "rdio-scanner") {
			t.Errorf("hash %s does not verify against the credential it was made from", hash)
		}
	}
}

// TestHashingACredentialLongerThanBcryptReadsIsAFailure: bcrypt stores at most 72 bytes,
// so hashing a longer credential fails, as it does in the legacy password change.
func TestHashingACredentialLongerThanBcryptReadsIsAFailure(t *testing.T) {
	hashes := NewBcrypt(bcrypt.DefaultCost)

	hash, err := hashes.Hash(context.Background(), strings.Repeat("a", 73))

	if err == nil {
		t.Fatalf("a 73-byte credential was stored as %s, which bcrypt cannot do", hash)
	}

	if !strings.Contains(err.Error(), "72 bytes") {
		t.Errorf("error = %v, want the reason bcrypt gave", err)
	}

	if !strings.HasPrefix(err.Error(), "passwords.hash: ") {
		t.Errorf("error = %v, want it named for the operation that failed", err)
	}

	if _, err := hashes.Hash(context.Background(), strings.Repeat("a", 72)); err != nil {
		t.Errorf("the 72-byte credential bcrypt can store failed: %v", err)
	}
}

// TestACredentialLongerThanBcryptReadsIsCheckedOnlyUpToItsSeventySecondByte: a login
// credential longer than 72 bytes is compared on its first 72 bytes and accepted on the
// strength of a prefix; the legacy has this and it stays, as tightening it would refuse
// credentials a deployment may use today. Only logins trip it — a credential longer than
// 72 bytes can never be stored.
func TestACredentialLongerThanBcryptReadsIsCheckedOnlyUpToItsSeventySecondByte(t *testing.T) {
	hashes := NewBcrypt(bcrypt.DefaultCost)

	hash, err := hashes.Hash(context.Background(), strings.Repeat("a", 72))
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if !hashes.Matches(context.Background(), hash, strings.Repeat("a", 72)+" and more") {
		t.Error("a longer credential that shares the stored one's first 72 bytes is refused, which is not what bcrypt does")
	}

	if hashes.Matches(context.Background(), hash, strings.Repeat("b", 72)+" and more") {
		t.Error("a longer credential that does not share the stored one's first 72 bytes is accepted")
	}
}

// TestHashUsesTheConfiguredCost: the deployment's bcrypt cost drives the hash the server
// writes; both servers default to 10, so the default keeps their hashes identical.
func TestHashUsesTheConfiguredCost(t *testing.T) {
	hashes := NewBcrypt(12)

	hash, err := hashes.Hash(context.Background(), "credential")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if !strings.HasPrefix(hash, "$2a$12$") {
		t.Errorf("the stored hash is %q, want it hashed at the configured cost 12", hash)
	}
}

// TestNeedsUpgradeReportsAStoredCredentialHashedBelowTheConfiguredCost: the upgrade question
// is answered from the stored hash itself, whose cost lives inside it, so a raised cost keeps
// answering logins with the cheap stored hash until the credential is renewed.
func TestNeedsUpgradeReportsAStoredCredentialHashedBelowTheConfiguredCost(t *testing.T) {
	cheap, err := NewBcrypt(10).Hash(context.Background(), "credential")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	expensive, err := NewBcrypt(12).Hash(context.Background(), "credential")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	raised := NewBcrypt(12)

	if !raised.NeedsUpgrade(cheap) {
		t.Error("a hash at 10 beside a configured cost of 12 reports no upgrade, want the renewal")
	}

	if raised.NeedsUpgrade(expensive) {
		t.Error("a hash at the configured cost reports an upgrade, want none")
	}

	if NewBcrypt(10).NeedsUpgrade(cheap) {
		t.Error("a hash at the configured cost reports an upgrade, want none")
	}

	if NewBcrypt(12).NeedsUpgrade("not a hash") {
		t.Error("a stored value that is not a hash reports an upgrade, want none: there is no cost to raise it to")
	}
}
