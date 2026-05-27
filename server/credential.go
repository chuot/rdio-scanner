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

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// credentialHashPrefix is the format marker for hashed listener access codes
// and uploader API keys. Anything stored in the DB that doesn't begin with
// this prefix is treated as a legacy plaintext value (and accepted during
// the migration period).
const credentialHashPrefix = "$h1$"

const credentialHashLen = len(credentialHashPrefix) + 2*sha256.Size

// HashCredential returns a deterministic HMAC-SHA256 hash of plaintext,
// keyed by the server-wide secret. Because it's keyed, a DB leak does not
// expose the credentials directly; rotating the secret invalidates every
// stored hash (which is the right behavior post-compromise).
func HashCredential(secret, plaintext string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(plaintext))
	return credentialHashPrefix + hex.EncodeToString(h.Sum(nil))
}

// LooksLikeCredentialHash reports whether s is in the HashCredential format.
// Used at admin-write time to decide whether to (re-)hash an incoming value.
func LooksLikeCredentialHash(s string) bool {
	if !strings.HasPrefix(s, credentialHashPrefix) || len(s) != credentialHashLen {
		return false
	}
	_, err := hex.DecodeString(s[len(credentialHashPrefix):])
	return err == nil
}

// VerifyCredential checks plaintext against a stored value that may be
// either the HashCredential format or a legacy plaintext value. Comparisons
// run in constant time. Legacy plaintext support exists so existing
// installs continue to authenticate until the admin next saves the entry,
// at which point it gets hashed.
func VerifyCredential(secret, stored, plaintext string) bool {
	if LooksLikeCredentialHash(stored) {
		candidate := HashCredential(secret, plaintext)
		return subtle.ConstantTimeCompare([]byte(candidate), []byte(stored)) == 1
	}
	return subtle.ConstantTimeCompare([]byte(plaintext), []byte(stored)) == 1
}

// EnsureHashed returns s as a HashCredential string. If s is already in
// the hash format, returns it unchanged. Otherwise hashes it. Used at
// write time so admin edits don't re-hash an already-hashed code (which
// would invalidate it).
func EnsureHashed(secret, s string) string {
	if LooksLikeCredentialHash(s) {
		return s
	}
	return HashCredential(secret, s)
}
