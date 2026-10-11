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

package tokens

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

// windowSize is how many issued tokens the server keeps accepting at once. It is the
// legacy `len(admin.Tokens) < 5` limit, and it is what retires an administrator session
// on its own: the sixth login drops the first token from the window, and the browser that
// still holds it is refused the next time it asks for anything.
const windowSize = 5

// ErrNotIssued reports a credential this server has never handed out, or has stopped
// handing out: it is not in the window of tokens the process issued.
//
// The window is checked before the signature is, exactly as the legacy checks it, so a
// credential that is perfectly well signed with the right secret is still refused when
// this process did not issue it.
var ErrNotIssued = errors.New("token was not issued by this server")

// ErrExpired reports a credential whose seven-day life has run out: this
// server issued it and its window may still hold it, and the token is refused
// beside that. The window is the primary rule — it never ages a token on its
// own, only the sixth login does — and the expiry bounds the blast radius the
// window cannot.
var ErrExpired = errors.New("token has expired")

// tokenLifetime is the life the decision gives an issued credential.
const tokenLifetime = 7 * 24 * time.Hour

// Secrets is the signing secret, read every time it is needed rather than once when the
// adapter is built.
//
// The legacy server signs and verifies with `Options.secret` as it stands at that moment,
// and the options are re-derived after a password change. When what is stored is not a
// full-length secret — which is what a deployment that has never written one has — the
// re-derivation generates a new one, and every token issued before that change stops
// verifying. Reading the secret once at startup would hide that behind a server that
// appears to keep its sessions alive.
type Secrets interface {
	// Secret answers the signing secret in effect. A read that fails is reported,
	// not answered with the empty string: an empty key signs tokens
	// every server accepts as signature-valid text, and a login that "worked"
	// against no secret is the failure this seam must make audible.
	Secret() (string, error)
}

// Tokens issues and checks the administrator session tokens.
//
// A token is a signed credential whose claim is the session identifier, with the
// seven-day expiry the decision adds, and what makes it valid is that this process issued it:
// nothing is persisted, so a restart retires every session the administrator had, and
// a token that worked before the restart is refused after it.
type Tokens struct {
	secrets Secrets

	// now is the clock the token's expiry is measured against. The server runs on
	// the wall clock; a test replaces it to watch a token age past its expiry.
	now func() time.Time

	// issued is the rolling window, oldest first.
	//
	// The legacy server mutates this slice from concurrent HTTP handlers without any
	// protection, which is a data race rather than a behavior: there is no legacy outcome
	// to reproduce here, so the window is guarded.
	issued []string

	mu sync.Mutex
}

// New builds the token issuer for the signing secret a deployment runs with.
func New(secrets Secrets) *Tokens {
	return &Tokens{
		secrets: secrets,
		now:     time.Now,
		issued:  []string{},
	}
}

// Issue hands out a new session token and puts it at the end of the window.
func (tokens *Tokens) Issue(ctx context.Context) (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("tokens.issue: %w", err)
	}

	// The secret is read first and the failure reported: signing with
	// the empty string — what a failed read used to answer with — makes a token
	// whose signature is the HMAC of nothing, and the login is reported as a
	// success against no secret at all.
	secret, err := tokens.secrets.Secret()
	if err != nil {
		return "", fmt.Errorf("tokens.issue: %w", err)
	}

	if secret == "" {
		return "", errors.New("tokens.issue: the server holds no signing secret to sign with")
	}

	// The session identifier is the claim, and the seven-day expiry rides with it
	//: a token the server stops holding is what ends a session here — that
	// stays the primary rule — and the expiry bounds the life of a stolen token
	// the window cannot evict on its own.
	credential, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		ID:        id.String(),
		ExpiresAt: jwt.NewNumericDate(tokens.now().Add(tokenLifetime)),
	}).SignedString([]byte(secret))

	if err != nil {
		return "", fmt.Errorf("tokens.issue: %w", err)
	}

	tokens.mu.Lock()
	defer tokens.mu.Unlock()

	if len(tokens.issued) >= windowSize {
		tokens.issued = tokens.issued[1:]
	}

	tokens.issued = append(tokens.issued, credential)

	return credential, nil
}

// Validate accepts a credential only when this server issued it and it still verifies
// against the secret the server is signing with now.
func (tokens *Tokens) Validate(ctx context.Context, token string) error {
	if !tokens.wasIssued(token) {
		return ErrNotIssued
	}

	claims := &jwt.RegisteredClaims{}

	parsed, err := jwt.ParseWithClaims(token, claims, func(credential *jwt.Token) (any, error) {
		if _, ok := credential.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", credential.Header["alg"])
		}

		secret, err := tokens.secrets.Secret()
		if err != nil {
			return nil, err
		}

		if secret == "" {
			return nil, errors.New("the server holds no signing secret to check against")
		}

		return []byte(secret), nil
	})

	if err != nil {
		return fmt.Errorf("tokens.validate: %w", err)
	}

	if !parsed.Valid {
		return fmt.Errorf("tokens.validate: token is not valid")
	}

	// The expiry is measured on this server's clock: a credential with no
	// expiry claim is not one this server issued, and one whose claim has run out
	// is refused even while the window still holds it.
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.Before(tokens.now()) {
		return fmt.Errorf("tokens.validate: %w", ErrExpired)
	}

	return nil
}

// Revoke retires a credential this server issued.
//
// A credential the server does not hold is not a failure: there is nothing to retire, and
// the legacy logout reports the same thing. The logout use case checks the credential
// first, so a caller whose session is already gone finds that out before it gets here.
func (tokens *Tokens) Revoke(ctx context.Context, token string) error {
	tokens.mu.Lock()
	defer tokens.mu.Unlock()

	remaining := tokens.issued[:0]

	for _, issued := range tokens.issued {
		if issued != token {
			remaining = append(remaining, issued)
		}
	}

	tokens.issued = remaining

	return nil
}

// wasIssued reports whether the credential is in the window of tokens this process handed
// out.
func (tokens *Tokens) wasIssued(token string) bool {
	tokens.mu.Lock()
	defer tokens.mu.Unlock()

	// The compare is constant-time: the window membership is checked
	// against tokens an attacker is guessing, and a compare that fails earlier on
	// a prefix is a channel for learning a token one character at a time.
	for _, issued := range tokens.issued {
		if subtle.ConstantTimeCompare([]byte(issued), []byte(token)) == 1 {
			return true
		}
	}

	return false
}
