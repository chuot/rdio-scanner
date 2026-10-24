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

package tokens

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// secretSource is the signing secret an adapter test signs with, and the knob for the legacy
// re-derivation: replacing it is what a password change does to a deployment whose stored
// secret is not a full-length one.
type secretSource struct {
	value string

	// failure is the read failure the seam answers with — what a failed options
	// read looks like to the token issuer.
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

// TestIssueClaimsOnlyTheSessionIdentifier: the claim set is the session identifier and the
// seven-day expiry; the other time claims stay out, as the legacy issued none.
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

	// The seven-day expiry rides with the identifier; the other time claims
	// stay out, as the legacy issued none of them.
	if claims.ExpiresAt == nil {
		t.Error("the credential carries no expiry, want the seven-day expiry the decision gives it")
	}

	if claims.IssuedAt != nil || claims.NotBefore != nil {
		t.Errorf("the credential claims issued at %v, not before %v, want neither",
			claims.IssuedAt, claims.NotBefore)
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

// TestValidateRefusesACredentialThisServerDidNotIssue: the window check comes before the
// signature check, as in the legacy, so a well-signed credential this process never handed
// out is still refused.
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

// TestValidateRefusesACredentialThatIsNotSignedAtAll: garbage is refused as one this server
// did not issue, before the signature is looked at; the signing-method check inside the parse
// is defense behind that.
func TestValidateRefusesACredentialThatIsNotSignedAtAll(t *testing.T) {
	issuer := New(newSecretSource())

	unsigned := `eyJhbGciOiJub25lIn0.eyJpZCI6ImFkbWluIn0.`

	if err := issuer.Validate(context.Background(), unsigned); !errors.Is(err, ErrNotIssued) {
		t.Errorf("Validate = %v for an unsigned credential, want %v", err, ErrNotIssued)
	}
}

// TestValidateRefusesACredentialIssuedUnderASupersededSecret is the legacy consequence of
// re-deriving the options: a deployment whose stored secret is not full-length gets a new
// secret on every re-derivation, so a password change retires every open session.
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
// removes what it finds and says nothing about what it does not find.
func TestRevokeOnACredentialThisServerDidNotIssueIsNotAFailure(t *testing.T) {
	issuer := New(newSecretSource())

	if err := issuer.Revoke(context.Background(), "whatever"); err != nil {
		t.Errorf("Revoke = %v for a credential the server does not hold, want no failure", err)
	}
}

// TestIssueFailsWhenTheSigningSecretCannotBeRead: the signing secret is read from the options
// when a token is made, and a read that fails used to answer the empty string and sign with it,
// reporting the login as a success; now the seam reports it and Issue fails with it.
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

// TestIssueNeverSignsATokenWithAnEmptySecret: an empty signing secret signs a token whose
// signature is the HMAC of nothing, and the login reads as a success; Issue refuses it.
func TestIssueNeverSignsATokenWithAnEmptySecret(t *testing.T) {
	source := newSecretSource()

	source.value = ""

	tokens := New(source)

	token, err := tokens.Issue(context.Background())
	if err == nil {
		t.Fatalf("Issue: returned a token signed with the empty secret: %q", token)
	}
}

// TestValidateRefusesWhenTheSigningSecretCannotBeRead: a read that fails at validation must
// refuse the credential, not verify it against the empty string.
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

// TestIssuedTokensCarryASevenDayExpiry: the window staying primary is the rule, and the
// expiry bounds the life of a stolen token the window cannot evict on its own.
func TestIssuedTokensCarryASevenDayExpiry(t *testing.T) {
	source := newSecretSource()
	issuer := New(source)

	at := time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC)

	issuer.now = func() time.Time { return at }

	token, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims := &jwt.RegisteredClaims{}

	// The claims are read with their validation off: this test looks at what the
	// credential carries, not at whether the wall clock finds it live.
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())

	if _, err := parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return []byte(source.value), nil
	}); err != nil {
		t.Fatalf("parsing the issued credential: %v", err)
	}

	if claims.ExpiresAt == nil {
		t.Fatal("the issued credential carries no expiry, want the seven-day expiry the decision asks for")
	}

	if got, want := claims.ExpiresAt.UTC(), at.Add(7*24*time.Hour); !got.Equal(want) {
		t.Errorf("the credential expires at %v, want %v: seven days from the instant it was issued", got, want)
	}
}

// TestAnExpiredTokenIsRefusedInsideTheIssuedWindow: the window is the primary rule, and the
// expiry is the bound it cannot provide — the window only evicts on the sixth login.
func TestAnExpiredTokenIsRefusedInsideTheIssuedWindow(t *testing.T) {
	source := newSecretSource()
	issuer := New(source)

	// The issue instant is the real now, so the credential is live for every wall-clock check
	// on the way, and the fake clock eight days on is what must find it expired.
	at := time.Now().UTC().Truncate(time.Second)

	issuer.now = func() time.Time { return at }

	token, err := issuer.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Eight days on: the window still holds the token — the server issued it and
	// nothing evicted it — and its expiry has passed.
	issuer.now = func() time.Time { return at.Add(8 * 24 * time.Hour) }

	if err := issuer.Validate(context.Background(), token); err == nil {
		t.Error("Validate accepted an expired token the window still holds, want refusal: a stolen token must not stay valid until eviction")
	}
}
