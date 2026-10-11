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
	"errors"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v4"
)

// secretSource is the signing secret an adapter test signs with, and the one knob that
// lets a test reproduce the legacy re-derivation: replacing it is what a password change
// does to a deployment whose stored secret is not a full-length one.
type secretSource struct {
	value string

	// failure is the read failure the seam answers with — what a failed options
	// read looks like to the token issuer (CA-013.3).
	failure error
}

func (source *secretSource) Secret() (string, error) {
	return source.value, source.failure
}

func newSecretSource() *secretSource {
	return &secretSource{value: strings.Repeat("a", 256)}
}

// TestIssueReturnsACredentialThisServerAccepts is the round trip every administrator
// request depends on.
func TestIssueReturnsACredentialThisServerAccepts(t *testing.T) {
	issuer := New(newSecretSource())

	credential, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if credential == "" {
		t.Fatal("Issue returned no credential")
	}

	if err := issuer.Validate(context.Background(), credential); err != nil {
		t.Errorf("Validate = %v, want the credential this server just issued to be accepted", err)
	}
}

// TestIssueClaimsOnlyTheSessionIdentifier pins the claim set the legacy issues: one
// identifier, and no expiry — a session ends here when the server stops holding the
// credential, not when a timestamp passes.
func TestIssueClaimsOnlyTheSessionIdentifier(t *testing.T) {
	source := newSecretSource()
	issuer := New(source)

	credential, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims := &jwt.RegisteredClaims{}

	parsed, err := jwt.ParseWithClaims(credential, claims, func(token *jwt.Token) (any, error) {
		secret, err := source.Secret()
		if err != nil {
			return nil, err
		}

		return []byte(secret), nil
	})

	if err != nil {
		t.Fatalf("ParseWithClaims: %v", err)
	}

	if !parsed.Valid {
		t.Fatal("the issued credential does not verify against the secret it was signed with")
	}

	if claims.ID == "" {
		t.Error("the credential carries no session identifier")
	}

	if claims.ExpiresAt != nil || claims.IssuedAt != nil || claims.NotBefore != nil {
		t.Errorf("the credential claims expiry %v, issued at %v, not before %v, want none of them",
			claims.ExpiresAt, claims.IssuedAt, claims.NotBefore)
	}

	if claims.Subject != "" || len(claims.Audience) != 0 {
		t.Errorf("the credential claims subject %q and audience %v, want only the identifier",
			claims.Subject, claims.Audience)
	}
}

// TestIssueGivesEachLoginItsOwnCredential is what makes a logout retire one browser and
// not every browser the administrator has open.
func TestIssueGivesEachLoginItsOwnCredential(t *testing.T) {
	issuer := New(newSecretSource())

	first, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	second, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if first == second {
		t.Error("two logins were handed the same credential")
	}
}

// TestIssueRetiresTheOldestCredentialBeyondTheWindow pins the rolling window: the sixth
// login ends the session of the first browser, and nothing else about that browser changes.
func TestIssueRetiresTheOldestCredentialBeyondTheWindow(t *testing.T) {
	issuer := New(newSecretSource())

	issued := []string{}

	for range windowSize {
		credential, err := issuer.Issue(context.Background())
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}

		issued = append(issued, credential)
	}

	for _, credential := range issued {
		if err := issuer.Validate(context.Background(), credential); err != nil {
			t.Errorf("Validate = %v for a credential inside the window, want it accepted", err)
		}
	}

	if _, err := issuer.Issue(context.Background()); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := issuer.Validate(context.Background(), issued[0]); !errors.Is(err, ErrNotIssued) {
		t.Errorf("Validate = %v for the credential the sixth login retired, want %v", err, ErrNotIssued)
	}

	if err := issuer.Validate(context.Background(), issued[1]); err != nil {
		t.Errorf("Validate = %v for the second credential, want it still accepted: only the oldest is retired", err)
	}
}

// TestValidateRefusesACredentialThisServerDidNotIssue pins the order the legacy checks in:
// the window first. A credential that is well signed with the right secret is still refused
// when this process never handed it out.
func TestValidateRefusesACredentialThisServerDidNotIssue(t *testing.T) {
	source := newSecretSource()
	issuer := New(source)

	secret, err := source.Secret()
	if err != nil {
		t.Fatalf("Secret: %v", err)
	}

	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		ID: "not-a-session-this-server-issued",
	}).SignedString([]byte(secret))

	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}

	if err := issuer.Validate(context.Background(), forged); !errors.Is(err, ErrNotIssued) {
		t.Errorf("Validate = %v for a credential this server did not issue, want %v", err, ErrNotIssued)
	}
}

// TestValidateRefusesAnEmptyCredential covers the request that names no credential at all,
// which every handler on the administrator surface has to answer.
func TestValidateRefusesAnEmptyCredential(t *testing.T) {
	issuer := New(newSecretSource())

	if err := issuer.Validate(context.Background(), ""); !errors.Is(err, ErrNotIssued) {
		t.Errorf("Validate = %v for an empty credential, want %v", err, ErrNotIssued)
	}
}

// TestValidateRefusesACredentialThatIsNotSignedAtAll pins the window check doing its job on
// garbage: the credential is refused as one this server did not issue, before the signature
// is looked at. The signing-method check inside the parse is defense behind it, matching the
// legacy order rather than being reachable on its own.
func TestValidateRefusesACredentialThatIsNotSignedAtAll(t *testing.T) {
	issuer := New(newSecretSource())

	unsigned := `eyJhbGciOiJub25lIn0.eyJpZCI6ImFkbWluIn0.`

	if err := issuer.Validate(context.Background(), unsigned); !errors.Is(err, ErrNotIssued) {
		t.Errorf("Validate = %v for an unsigned credential, want %v", err, ErrNotIssued)
	}
}

// TestValidateRefusesACredentialIssuedUnderASupersededSecret is the legacy consequence of
// re-deriving the options: a deployment whose stored secret is not a full-length one gets a
// new secret on every re-derivation, so a password change retires every session the
// administrator has open, even though those credentials are still in the window.
func TestValidateRefusesACredentialIssuedUnderASupersededSecret(t *testing.T) {
	source := newSecretSource()
	issuer := New(source)

	credential, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := issuer.Validate(context.Background(), credential); err != nil {
		t.Fatalf("Validate = %v before the secret changed", err)
	}

	source.value = strings.Repeat("b", 256)

	err = issuer.Validate(context.Background(), credential)
	if err == nil {
		t.Fatal("Validate accepted a credential signed with a secret the server no longer signs with")
	}

	if !strings.Contains(err.Error(), "tokens.validate") {
		t.Errorf("error = %v, want it named the operation the way the other adapters name theirs", err)
	}
}

// TestRevokeMakesACredentialStopBeingAccepted is the logout half: the browser that logs out
// is refused from that moment on.
func TestRevokeMakesACredentialStopBeingAccepted(t *testing.T) {
	issuer := New(newSecretSource())

	credential, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := issuer.Revoke(context.Background(), credential); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if err := issuer.Validate(context.Background(), credential); !errors.Is(err, ErrNotIssued) {
		t.Errorf("Validate = %v after the logout, want %v", err, ErrNotIssued)
	}
}

// TestRevokeLeavesTheOtherSessionsAlone is what makes a logout end one browser rather than
// the administrator's whole set of open sessions.
func TestRevokeLeavesTheOtherSessionsAlone(t *testing.T) {
	issuer := New(newSecretSource())

	first, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	second, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := issuer.Revoke(context.Background(), first); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if err := issuer.Validate(context.Background(), second); err != nil {
		t.Errorf("Validate = %v for the session that did not log out, want it accepted", err)
	}
}

// TestRevokeOnACredentialThisServerDidNotIssueIsNotAFailure matches the legacy logout, which
// removes what it finds and says nothing about what it does not. The logout use case checks
// the credential first, so this is the adapter's own boundary rather than the caller's.
func TestRevokeOnACredentialThisServerDidNotIssueIsNotAFailure(t *testing.T) {
	issuer := New(newSecretSource())

	if err := issuer.Revoke(context.Background(), "whatever"); err != nil {
		t.Errorf("Revoke = %v for a credential the server does not hold, want no failure", err)
	}
}

// TestIssueFailsWhenTheSigningSecretCannotBeRead (REQ-013, CA-013.3): the signing
// secret is read from the options when a token is made. A read that fails used to
// answer with the empty string, and the server signed the token with it — the
// login was reported as a success. The seam reports the failure, Issue fails with
// it, and the administrator is told the session could not be started.
func TestIssueFailsWhenTheSigningSecretCannotBeRead(t *testing.T) {
	source := newSecretSource()

	source.failure = errors.New("options.read: database is closed")

	tokens := New(source)

	token, err := tokens.Issue(context.Background())
	if err == nil {
		t.Fatalf("Issue: returned %q, want a failure: the secret behind the token cannot be read", token)
	}

	if !strings.Contains(err.Error(), "database is closed") {
		t.Errorf("Issue failed with %q, want it to name the secret read failure", err)
	}
}

// TestIssueNeverSignsATokenWithAnEmptySecret (REQ-013, CA-013.3): an empty signing
// secret — what a failed read used to answer with — signs a token whose signature
// is the HMAC of nothing, and the login is reported as a success. Issue refuses
// it: a server that holds no signing secret does not issue sessions.
func TestIssueNeverSignsATokenWithAnEmptySecret(t *testing.T) {
	source := newSecretSource()

	source.value = ""

	tokens := New(source)

	token, err := tokens.Issue(context.Background())
	if err == nil {
		t.Fatalf("Issue: returned a token signed with the empty secret: %q", token)
	}
}

// TestValidateRefusesWhenTheSigningSecretCannotBeRead (REQ-013, CA-013.3): the
// same seam answers every validation, and a read that fails there must refuse the
// credential, not verify it against the empty string.
func TestValidateRefusesWhenTheSigningSecretCannotBeRead(t *testing.T) {
	source := newSecretSource()

	tokens := New(source)

	token, err := tokens.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	source.failure = errors.New("options.read: database is closed")

	if err := tokens.Validate(context.Background(), token); err == nil {
		t.Error("Validate accepted a credential while the signing secret could not be read, want a refusal")
	}
}
