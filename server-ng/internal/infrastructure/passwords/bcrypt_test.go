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
	"strings"
	"testing"
)

// legacyStoredCredential is the hash the legacy server wrote into
// server-ng/testdata/legacydb/rdio-scanner.db for the default administrator credential, taken from
// that database's options table with its JSON quoting removed. It is the credential a real
// deployment has on record, and the point of these tests is that this adapter reads it.
const legacyStoredCredential = `$2a$10$1GFizQ/sWHxheggO6YSNk.D4RnbiFbnTnuiMEuuf3mCsYQhRcsBQ6`

func TestACredentialTheLegacyServerStoredIsAcceptedByThisServer(t *testing.T) {
	hashes := NewBcrypt()

	if !hashes.Matches(context.Background(), legacyStoredCredential, "rdio-scanner") {
		t.Error("the credential the legacy server stored does not verify here, so a deployment that swaps servers locks its administrator out")
	}
}

func TestAStoredCredentialRefusesAnotherOne(t *testing.T) {
	hashes := NewBcrypt()

	for _, submitted := range []string{"rdio-scanner ", "Rdio-Scanner", "", "password"} {
		if hashes.Matches(context.Background(), legacyStoredCredential, submitted) {
			t.Errorf("the stored credential accepts %q", submitted)
		}
	}
}

// TestAStoredCredentialThatIsNotACredentialRefuses covers the state a real deployment can end up
// in: the legacy server reads its stored credential out of the options table, and a value that is
// not a hash — an empty column included — makes every comparison fail. The login use case treats
// that as a refusal, and this adapter must not turn it into a success.
func TestAStoredCredentialThatIsNotACredentialRefuses(t *testing.T) {
	hashes := NewBcrypt()

	for _, stored := range []string{"", "not a bcrypt hash", "$2a$10$", `"$2a$10$1GFizQ/sWHxheggO6YSNk."`} {
		if hashes.Matches(context.Background(), stored, "rdio-scanner") {
			t.Errorf("a stored credential of %q is accepted, which would let anybody log in", stored)
		}
	}
}

func TestHashingACredentialProducesAHashThatVerifiesAgainstIt(t *testing.T) {
	hashes := NewBcrypt()

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

// TestHashingIsBcryptAtTheLegacyCost is what keeps the two servers interchangeable: the hash a
// deployment already has was written at bcrypt's default cost, and a hash this server writes has
// to be readable by the legacy one.
func TestHashingIsBcryptAtTheLegacyCost(t *testing.T) {
	hashes := NewBcrypt()

	hash, err := hashes.Hash(context.Background(), "rdio-scanner")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if !strings.HasPrefix(hash, "$2a$10$") {
		t.Errorf("hash = %s, want the same bcrypt form and cost the legacy server writes", hash)
	}
}

// TestHashingTheSameCredentialTwiceProducesDifferentHashes is the property that makes a stored
// hash useless on its own: two deployments with the same credential do not share a database value,
// and a hash cannot be compared to another hash.
func TestHashingTheSameCredentialTwiceProducesDifferentHashes(t *testing.T) {
	hashes := NewBcrypt()

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

// TestHashingACredentialLongerThanBcryptReadsIsAFailure pins the boundary the legacy server has
// too: bcrypt stores at most 72 bytes, so a password change to a longer credential fails, and the
// failure is returned rather than swallowed.
func TestHashingACredentialLongerThanBcryptReadsIsAFailure(t *testing.T) {
	hashes := NewBcrypt()

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

// TestACredentialLongerThanBcryptReadsIsCheckedOnlyUpToItsSeventySecondByte records a property of
// bcrypt that a deployment can trip over: a login credential longer than 72 bytes is compared on
// its first 72 bytes, so it is accepted on the strength of a prefix. The legacy server has this
// behavior because it uses the same module, and it is preserved here rather than quietly tightened
// — tightening it would refuse credentials a deployment may be using today.
//
// The asymmetry is worth stating: a credential longer than 72 bytes can never be stored, so this
// only bites a deployment that logs in with a longer credential than it could ever set.
func TestACredentialLongerThanBcryptReadsIsCheckedOnlyUpToItsSeventySecondByte(t *testing.T) {
	hashes := NewBcrypt()

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
