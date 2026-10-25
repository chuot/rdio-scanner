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

package backend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

// ask posts to the listener surface: a 401 here can only have come from the gate.
// An empty POST is refused 400 by the SDK itself.
func ask(t *testing.T, bundle *serverBundle, addr string, headers map[string]string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	req.RemoteAddr = addr
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	bundle.handler.ServeHTTP(rec, req)

	return rec.Code, rec.Body.String()
}

func TestAListenerRefusesACallerCarryingNoCredential(t *testing.T) {
	bundle := newServer(t, true)

	code, body := ask(t, bundle, testAddress, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("a caller carrying no credential answered %d, want 401: %q", code, body)
	}
	if body != "" {
		t.Fatalf("the refusal carried %q, want an empty body", body)
	}
}

func TestAListenerAcceptsAValidAccessCode(t *testing.T) {
	bundle := newServer(t, true)

	code, body := ask(t, bundle, testAddress, map[string]string{"X-Access-Code": listenerCode})
	if code == http.StatusUnauthorized {
		t.Fatalf("a valid access code was refused: %q", body)
	}
}

func TestAListenerAcceptsAnAdministratorSession(t *testing.T) {
	bundle := newServer(t, true)

	code, body := ask(t, bundle, testAddress, map[string]string{"Authorization": adminToken})
	if code == http.StatusUnauthorized {
		t.Fatalf("an administrator session was refused: %q", body)
	}
}

func TestAListenerRefusesAnExpiredAccessCode(t *testing.T) {
	bundle := newServer(t, true)
	bundle.accesses.Stored = domain.Accesses{
		{
			ID:         1,
			Code:       "expired-code",
			Ident:      "expired-desk",
			Expiration: uint64(fixtureTime.Add(-time.Minute).Unix()),
			Scope:      domain.AccessScope{Present: true, Wildcard: true},
		},
	}

	code, _ := ask(t, bundle, testAddress, map[string]string{"X-Access-Code": "expired-code"})
	if code != http.StatusUnauthorized {
		t.Fatalf("an expired access code answered %d, want 401", code)
	}
}

func TestAListenerRefusesACredentialThatIsNotAnAccessCode(t *testing.T) {
	bundle := newServer(t, true)

	code, _ := ask(t, bundle, testAddress, map[string]string{"X-Access-Code": "not-a-listed-code"})
	if code != http.StatusUnauthorized {
		t.Fatalf("a credential the access list does not carry answered %d, want 401", code)
	}
}

func TestAListenerRefusesAnAdministratorCredentialThatIsNotASession(t *testing.T) {
	bundle := newServer(t, true)

	code, _ := ask(t, bundle, testAddress, map[string]string{"Authorization": "Bearer not-a-session"})
	if code != http.StatusUnauthorized {
		t.Fatalf("a credential that is not an open session answered %d, want 401", code)
	}
}

// An unrestricted server takes an anonymous caller, as the listener socket does,
// and the caller reads without writing.
func TestAListenerTakesAnAnonymousCallerOnAnUnrestrictedServer(t *testing.T) {
	bundle := newServer(t, false)

	code, body := ask(t, bundle, testAddress, nil)
	if code == http.StatusUnauthorized {
		t.Fatalf("an unrestricted server refused an anonymous caller: %q", body)
	}

	who, ok := CallerOf(bundle.caller(httptest.NewRequest(http.MethodPost, "/mcp", nil)))
	if !ok {
		t.Fatal("an unrestricted server carried no caller to the capabilities")
	}
	if who.Tier != TierReadOnly {
		t.Fatalf("an anonymous caller on an unrestricted server holds tier %v, want the read-only tier", who.Tier)
	}
}

func TestAListenerRefusesAnAnonymousCallerOnARestrictedServer(t *testing.T) {
	bundle := newServer(t, true)

	code, _ := ask(t, bundle, testAddress, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("a restricted server answered %d for an anonymous caller, want 401", code)
	}
}

// A spent guess budget is refused exactly like a wrong credential:
// same status, same body.
func TestAListenerRefusesASpentGuessBudgetIndistinguishablyFromAWrongCredential(t *testing.T) {
	bundle := newServer(t, true)

	var wrong string
	for i := 0; i < accessAttemptsMax; i++ {
		code, body := ask(t, bundle, testAddress, map[string]string{"X-Access-Code": "not-a-listed-code"})
		if code != http.StatusUnauthorized {
			t.Fatalf("guess %d answered %d, want 401", i+1, code)
		}
		wrong = body
	}

	code, body := ask(t, bundle, testAddress, map[string]string{"X-Access-Code": listenerCode})
	if code != http.StatusUnauthorized {
		t.Fatalf("a valid code answered %d after %d wrong guesses, want 401", code, accessAttemptsMax)
	}
	if body != wrong {
		t.Fatalf("a spent budget answered %q, want the same refusal a wrong credential gets %q", body, wrong)
	}
}

// Presenting nothing is not guessing, mirroring the socket's free prompt.
func TestPresentingNoCredentialDoesNotSpendTheGuessBudget(t *testing.T) {
	bundle := newServer(t, true)

	for i := 0; i < accessAttemptsMax+2; i++ {
		ask(t, bundle, testAddress, nil)
	}

	code, _ := ask(t, bundle, testAddress, map[string]string{"X-Access-Code": listenerCode})
	if code == http.StatusUnauthorized {
		t.Fatal("a valid code was refused after callers presented no credential")
	}
}

// The budget belongs to the address that spent it, and to no other address.
func TestTheGuessBudgetBelongsToTheAddressThatSpentIt(t *testing.T) {
	bundle := newServer(t, true)

	for i := 0; i < accessAttemptsMax; i++ {
		ask(t, bundle, testAddress, map[string]string{"X-Access-Code": "not-a-listed-code"})
	}

	code, _ := ask(t, bundle, "192.0.2.99", map[string]string{"X-Access-Code": listenerCode})
	if code == http.StatusUnauthorized {
		t.Fatalf("a valid code from another address was refused by the budget of %s", testAddress)
	}
}

// Behind a proxy the operator named, the budget follows the real client, not the proxy's socket.
func TestTheGuessBudgetFollowsTheRealClientBehindATrustedProxy(t *testing.T) {
	bundle := newServer(t, true)
	bundle.rebuild(t, trustedEverywhere(t))

	behind := "198.51.100.7"
	for i := 0; i < accessAttemptsMax; i++ {
		ask(t, bundle, testAddress, map[string]string{
			"X-Access-Code":   "not-a-listed-code",
			"X-Forwarded-For": behind,
		})
	}

	if !strings.Contains(bundle.logText(), behind) {
		t.Fatalf("the attempts were not attributed to the real client %s: the log says %q", behind, bundle.logText())
	}

	code, _ := ask(t, bundle, testAddress, map[string]string{"X-Access-Code": listenerCode, "X-Forwarded-For": behind})
	if code != http.StatusUnauthorized {
		t.Fatalf("a valid code from %s answered %d after its budget was spent, want 401", behind, code)
	}

	code, _ = ask(t, bundle, testAddress, map[string]string{"X-Access-Code": listenerCode, "X-Forwarded-For": "203.0.113.9"})
	if code == http.StatusUnauthorized {
		t.Fatal("a client behind the same proxy was refused by another client's budget")
	}
}

// Behind a proxy the operator did not name, the forwarding entry is a claim and nothing more.
func TestTheGuessBudgetIgnoresAForwardingEntryFromAnUntrustedPeer(t *testing.T) {
	bundle := newServer(t, true)

	claim := "198.51.100.7"
	for i := 0; i < accessAttemptsMax; i++ {
		ask(t, bundle, testAddress, map[string]string{
			"X-Access-Code":   "not-a-listed-code",
			"X-Forwarded-For": claim,
		})
	}

	if strings.Contains(bundle.logText(), claim) {
		t.Fatalf("the attempts were attributed to an unverified forwarding entry %s: the log says %q", claim, bundle.logText())
	}

	code, _ := ask(t, bundle, testAddress, map[string]string{"X-Access-Code": listenerCode, "X-Forwarded-For": claim})
	if code != http.StatusUnauthorized {
		t.Fatal("a caller spent another address's budget by naming it in a forwarding entry")
	}
}

func TestTheGateCarriesTheCallersTierToTheCapabilities(t *testing.T) {
	bundle := newServer(t, true)

	cases := []struct {
		name  string
		hdr   map[string]string
		tier  Tier
		ident string
	}{
		{"administrator session", map[string]string{"Authorization": adminToken}, TierFull, ""},
		{"access code", map[string]string{"X-Access-Code": listenerCode}, TierReadOnly, listenerIdent},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.RemoteAddr = testAddress
			for k, v := range c.hdr {
				req.Header.Set(k, v)
			}

			who, ok := CallerOf(bundle.caller(req))
			if !ok {
				t.Fatalf("no caller reached the capabilities for %s", c.name)
			}
			if who.Tier != c.tier {
				t.Fatalf("%s: tier %v, want %v", c.name, who.Tier, c.tier)
			}
			if who.Ident != c.ident {
				t.Fatalf("%s: ident %q, want %q", c.name, who.Ident, c.ident)
			}
		})
	}
}

// An access code scoped to systems carries that scope to the capabilities; the
// administrator's credential is not scoped by the listener access list.
func TestTheGateCarriesTheScopeOfTheCredentialItResolved(t *testing.T) {
	bundle := newServer(t, true)
	bundle.accesses.Stored = domain.Accesses{
		{
			ID:    1,
			Code:  listenerCode,
			Ident: listenerIdent,
			Scope: domain.AccessScope{Present: true, Systems: []domain.ScopedSystem{{SystemRef: 10001, TalkgroupsGiven: true, TalkgroupRefs: []uint{1514000}}}},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = testAddress
	req.Header.Set("X-Access-Code", listenerCode)

	who, ok := CallerOf(bundle.caller(req))
	if !ok {
		t.Fatal("no caller reached the capabilities")
	}
	if !who.Scoped {
		t.Fatal("an access code that names systems carried no scope to the capabilities")
	}
	if len(who.Scope.Systems) != 1 || who.Scope.Systems[0].SystemRef != 10001 {
		t.Fatalf("the scope reached the capabilities as %+v, want the one system the code names", who.Scope)
	}
}

// A credential is a credential: it is never written to the log an operator reads.
func TestTheGateNeverFilesACredentialInTheLog(t *testing.T) {
	bundle := newServer(t, true)

	ask(t, bundle, testAddress, map[string]string{"X-Access-Code": listenerCode})
	ask(t, bundle, testAddress, map[string]string{"Authorization": adminToken})
	ask(t, bundle, testAddress, map[string]string{"X-Access-Code": "not-a-listed-code"})

	logs := bundle.logText()
	if strings.Contains(logs, listenerCode) || strings.Contains(logs, "not-a-listed-code") {
		t.Fatalf("the log carries an access code: %q", logs)
	}
	if strings.Contains(logs, adminToken) {
		t.Fatalf("the log carries an administrator credential: %q", logs)
	}
}

func trustedEverywhere(t *testing.T) *httpaddr.Trusted {
	t.Helper()

	trusted, err := httpaddr.ParseTrusted("0.0.0.0/0,::/0")
	if err != nil {
		t.Fatal(err)
	}

	return trusted
}
