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
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// Format markers in the DB for shared credentials (listener access codes
// and uploader API keys).
//
// These are *shared secrets* the admin must be able to retrieve and
// distribute (to uploader scripts, listeners, etc.), not passwords. So
// they are encrypted, not hashed: a DB leak alone does not expose the
// plaintext, but the admin UI can decrypt and display it.
const (
	// credentialHashPrefix is the older HMAC-SHA256 format briefly used
	// before we realised these credentials need to be retrievable. Rows
	// in this format cannot be recovered — the admin must re-enter the
	// value. They continue to authenticate via the HMAC path below
	// until that re-entry happens.
	credentialHashPrefix = "$h1$"

	// credentialCipherPrefix marks AES-256-GCM ciphertext. The key is
	// SHA-256(server secret); rotating the secret invalidates every
	// stored ciphertext (which is the right behavior post-compromise).
	credentialCipherPrefix = "$e1$"
)

const (
	credentialHashLen  = len(credentialHashPrefix) + 2*sha256.Size
	credentialGCMNonce = 12 // AES-GCM standard nonce size
	credentialGCMTag   = 16 // AES-GCM tag size
)

// credentialKey derives a 32-byte AES key from the server secret. SHA-256
// is used so any length / encoding of secret works.
func credentialKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// EncryptCredential returns plaintext as a $e1$-formatted ciphertext keyed
// by the server-wide secret. A fresh nonce is used on each call, so
// identical plaintexts produce distinct ciphertexts.
func EncryptCredential(secret, plaintext string) string {
	block, err := aes.NewCipher(credentialKey(secret))
	if err != nil {
		// AES with a 32-byte key cannot fail construction. If it
		// somehow does, returning empty would silently corrupt the
		// row — panic so the failure is loud.
		panic("credential: aes.NewCipher: " + err.Error())
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic("credential: cipher.NewGCM: " + err.Error())
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		panic("credential: rand.Read: " + err.Error())
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return credentialCipherPrefix + base64.RawURLEncoding.EncodeToString(out)
}

// DecryptCredential reverses EncryptCredential. ok=false means the input
// was not in the expected format or the secret is wrong (e.g., rotated
// since the row was written).
func DecryptCredential(secret, stored string) (string, bool) {
	if !strings.HasPrefix(stored, credentialCipherPrefix) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(stored[len(credentialCipherPrefix):])
	if err != nil || len(raw) < credentialGCMNonce+credentialGCMTag {
		return "", false
	}
	block, err := aes.NewCipher(credentialKey(secret))
	if err != nil {
		return "", false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", false
	}
	nonce := raw[:gcm.NonceSize()]
	ct := raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", false
	}
	return string(pt), true
}

// LooksLikeCredentialCiphertext reports whether s appears to be in
// EncryptCredential format. It does NOT verify that s decrypts under any
// particular secret.
func LooksLikeCredentialCiphertext(s string) bool {
	if !strings.HasPrefix(s, credentialCipherPrefix) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(s[len(credentialCipherPrefix):])
	if err != nil {
		return false
	}
	return len(raw) >= credentialGCMNonce+credentialGCMTag
}

// LooksLikeCredentialHash reports whether s is in the legacy HMAC format.
// Rows in this format are unrecoverable but still verify via the HMAC
// path so existing installs continue to authenticate.
func LooksLikeCredentialHash(s string) bool {
	if !strings.HasPrefix(s, credentialHashPrefix) || len(s) != credentialHashLen {
		return false
	}
	_, err := hex.DecodeString(s[len(credentialHashPrefix):])
	return err == nil
}

// hashCredentialHMAC is the legacy HMAC-SHA256 hash used by $h1$ rows.
// Retained so VerifyCredential continues to accept those rows.
func hashCredentialHMAC(secret, plaintext string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(plaintext))
	return credentialHashPrefix + hex.EncodeToString(h.Sum(nil))
}

// VerifyCredential checks plaintext against stored, which may be a $e1$
// ciphertext, a $h1$ legacy hash, or legacy plaintext. Comparison runs in
// constant time within the matched branch.
func VerifyCredential(secret, stored, plaintext string) bool {
	if LooksLikeCredentialCiphertext(stored) {
		if pt, ok := DecryptCredential(secret, stored); ok {
			return subtle.ConstantTimeCompare([]byte(pt), []byte(plaintext)) == 1
		}
		return false
	}
	if LooksLikeCredentialHash(stored) {
		candidate := hashCredentialHMAC(secret, plaintext)
		return subtle.ConstantTimeCompare([]byte(candidate), []byte(stored)) == 1
	}
	return subtle.ConstantTimeCompare([]byte(plaintext), []byte(stored)) == 1
}

// EnsureEncrypted returns s in $e1$ form. Idempotent for already-$e1$
// values (admin UI may echo back what it received). $h1$ legacy hashes
// are returned unchanged — the plaintext is unrecoverable, and the
// existing HMAC verify path keeps them functional.
func EnsureEncrypted(secret, s string) string {
	if LooksLikeCredentialCiphertext(s) {
		return s
	}
	if LooksLikeCredentialHash(s) {
		return s
	}
	return EncryptCredential(secret, s)
}

// CredentialForDisplay returns the plaintext form of stored, suitable for
// rendering in the admin UI. Encrypted values are decrypted; legacy hash
// rows return "" (unrecoverable); legacy plaintext rows return as-is.
// A $e1$ row that fails to decrypt (e.g., secret rotated) also returns "".
func CredentialForDisplay(secret, stored string) string {
	if LooksLikeCredentialCiphertext(stored) {
		if pt, ok := DecryptCredential(secret, stored); ok {
			return pt
		}
		return ""
	}
	if LooksLikeCredentialHash(stored) {
		return ""
	}
	return stored
}
