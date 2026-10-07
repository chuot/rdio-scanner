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

package adminws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The administrator's configuration socket is how the web app learns that something changed while it was
// open. It is not a listener connection: it has no commands, no live feed, and no history — it sends its
// credential and then only ever reads.
//
// The socket is authenticated by a frame and not by a header. The upgrade itself asks for nothing, the
// first frame must be the bare token, and every later frame is checked the same way: the legacy server
// re-validates whatever arrives, so a connection whose token has been retired is ended the next time it
// says anything.

const (
	captureToken   = "capture-administrator-token"
	captureRefused = "not-a-token"
	captureVersion = "7.0.0"

	// changedTagLabel names a tag that only the second of two pushes has, which is how a test tells them
	// apart.
	changedTagLabel = "Changed after the credential"
)

// The socket answers the application layer's question about open administrator connections.
var _ ports.AdministratorSessions = (*Sessions)(nil)

// And it is the thing the /api/admin/config upgrade is served by.
var _ http.Handler = (*Sessions)(nil)

type fixture struct {
	tokens   *fakes.Tokens
	sessions *Sessions
	server   *httptest.Server

	configuration domain.Configuration
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	fixture := &fixture{
		tokens: fakes.NewTokens(),
	}

	fixture.tokens.Window = []string{captureToken}

	fixture.sessions = NewSessions(Deps{
		Session: application.NewCheckAdminSessionHandler(fixture.tokens),
		Version: captureVersion,
	})

	fixture.server = httptest.NewServer(fixture.sessions)

	t.Cleanup(fixture.server.Close)

	fixture.configuration = capturedConfiguration(t)

	return fixture
}

// open connects to the socket the way the web app does: no credential in the request.
func (fixture *fixture) open(t *testing.T) *websocket.Conn {
	t.Helper()

	return fixture.openAs(t, nil)
}

// openAs connects with extra request headers, which is how a test asks about a connection that comes from
// somewhere else.
func (fixture *fixture) openAs(t *testing.T, header http.Header) *websocket.Conn {
	t.Helper()

	address := "ws://" + strings.TrimPrefix(fixture.server.URL, "http://")

	conn, _, err := websocket.DefaultDialer.Dial(address, header)
	if err != nil {
		t.Fatalf("connecting to the configuration socket: %v", err)
	}

	t.Cleanup(func() { conn.Close() })

	return conn
}

// closedOnTheServer watches the close the server sends, which is the only answer a refused socket gets.
func (fixture *fixture) closedOnTheServer(t *testing.T, conn *websocket.Conn) func() (int, string) {
	t.Helper()

	var (
		code   int
		reason string

		arrived = make(chan struct{}, 1)
	)

	conn.SetCloseHandler(func(closeCode int, text string) error {
		code, reason = closeCode, text

		arrived <- struct{}{}

		return conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, ""), time.Now().Add(time.Second))
	})

	return func() (int, string) {
		t.Helper()

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer conn.SetReadDeadline(time.Time{})

		if _, _, err := conn.ReadMessage(); err == nil {
			t.Fatalf("the connection was closed with a frame the server did not send")
		}

		select {
		case <-arrived:

		case <-time.After(time.Second):
			t.Fatalf("the connection ended without the server saying why")
		}

		return code, reason
	}
}

// present sends the frame the web app sends first: the token, on its own, not wrapped in JSON.
func (fixture *fixture) present(t *testing.T, conn *websocket.Conn, token string) {
	t.Helper()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(token)); err != nil {
		t.Fatalf("presenting the token: %v", err)
	}
}

// awaitCredential waits for the server to have accepted credentials from n connections.
//
// The socket acknowledges nothing, so a test that is about to push to connections it has just authenticated
// has to let the read loops get there first. The capture does the same with a fixed pause; a test waits for
// the thing it depends on instead, and says so when it never happens.
//
// The count is of connections rather than of a named one because the connection the server holds is not the
// one the test holds: the test has the peer's end of the socket, and the server has its own.
func (fixture *fixture) awaitCredential(t *testing.T, n int) {
	t.Helper()

	fixture.await(t, func() bool { return fixture.grantedCount() >= n },
		"the server never accepted the credentials the connections presented")
}

// awaitGone waits for the server to have noticed that a connection is no longer there, and to be left
// holding n connections.
func (fixture *fixture) awaitGone(t *testing.T, n int) {
	t.Helper()

	fixture.await(t, func() bool { return fixture.heldCount() == n },
		"the server kept holding a connection whose peer had closed it")
}

func (fixture *fixture) await(t *testing.T, settled func() bool, complaint string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if settled() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("%s", complaint)
}

// grantedCount is how many connections the server holds have presented a credential it accepts.
func (fixture *fixture) grantedCount() int {
	fixture.sessions.mutex.Lock()
	defer fixture.sessions.mutex.Unlock()

	granted := 0

	for _, admin := range fixture.sessions.conns {
		if admin.granted {
			granted++
		}
	}

	return granted
}

// heldCount is how many administrator connections the server holds, credential or not.
func (fixture *fixture) heldCount() int {
	fixture.sessions.mutex.Lock()
	defer fixture.sessions.mutex.Unlock()

	return len(fixture.sessions.conns)
}

// changed is what a configuration save does: it tells the server that the configuration is now this.
func (fixture *fixture) changed(t *testing.T) {
	t.Helper()

	fixture.sessions.NotifyConfigurationChanged(context.Background(), fixture.configuration)
}

// changedTo is a save that leaves the configuration as something else.
func (fixture *fixture) changedTo(t *testing.T, configuration domain.Configuration) {
	t.Helper()

	fixture.sessions.NotifyConfigurationChanged(context.Background(), configuration)
}

// nextFrame reads the next thing the server pushed.
func (fixture *fixture) nextFrame(t *testing.T, conn *websocket.Conn) string {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	_, b, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("waiting for the server to push the configuration: %v", err)
	}

	return string(b)
}

// nothingArrivedYet reports whether the connection has been given nothing for a moment, which is how these
// tests say the server is not pushing to it.
//
// A connection whose read timed out is not a connection any more, so a test that uses this cannot go on to
// read from the same socket.
func (fixture *fixture) nothingArrivedYet(t *testing.T, conn *websocket.Conn) bool {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	defer conn.SetReadDeadline(time.Time{})

	_, _, err := conn.ReadMessage()
	if err == nil {
		return false
	}

	var netErr interface{ Timeout() bool }

	if ok := errors.As(err, &netErr); ok && netErr.Timeout() {
		return true
	}

	t.Fatalf("the connection ended instead of staying quiet: %v", err)

	return false
}

// An administrator connection that has presented its credential is pushed the whole configuration when
// somebody saves it, byte for byte as the legacy server put it on the wire.
func TestConfigSocketPushesTheCapturedConfigurationAfterAChange(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	fixture.present(t, conn, captureToken)
	fixture.awaitCredential(t, 1)
	fixture.changed(t)

	got := fixture.nextFrame(t, conn)
	want := capturedConfigGolden(t)

	if got != want {
		t.Errorf("the pushed configuration is\n  %s\nwant\n  %s", got, want)
	}
}

// A credential the server does not recognize ends the connection with the close the capture recorded. The
// upgrade itself asked for nothing, so this is the only thing standing between a stranger and the
// configuration.
func TestConfigSocketClosesAConnectionWhoseTokenItDoesNotRecognize(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	closed := fixture.closedOnTheServer(t, conn)

	fixture.present(t, conn, captureRefused)

	code, reason := closed()

	var captured struct {
		Code   int    `json:"code"`
		Reason string `json:"reason"`
	}

	if err := json.Unmarshal([]byte(readGolden(t, "adminws-bad-token.json")), &captured); err != nil {
		t.Fatalf("the captured close is not a close: %v", err)
	}

	if code != captured.Code || reason != captured.Reason {
		t.Errorf("the close is %d %q, want the captured close %d %q", code, reason, captured.Code, captured.Reason)
	}
}

// A connection that has not presented a credential is not pushed anything.
//
// The legacy server registers the connection the moment it upgrades and pushes to everything it has
// registered, so a peer that opens the socket and says nothing receives the whole configuration — every
// access code, every apikey, and every downstream credential — as soon as anybody saves it. Recorded as
// quirk Q64 and fixed by the owner's decision: the connection is open, and it is told nothing until it
// says who it is.
func TestConfigSocketSendsNothingToAConnectionThatHasNotPresentedACredential(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	// A change while the connection has said nothing. The legacy server pushes it to this connection; this
	// one does not.
	fixture.changed(t)

	// The connection then says who it is, and a second change is made — to a configuration that is visibly
	// not the first one. Which of the two arrives first is the answer to the question: a connection that was
	// handed the configuration while it had said nothing is given the first one, and a connection kept out of
	// it is given the second.
	fixture.present(t, conn, captureToken)
	fixture.awaitCredential(t, 1)

	fixture.changedTo(t, configurationChangedAfter(fixture.configuration))

	got := fixture.nextFrame(t, conn)

	if got == capturedConfigGolden(t) {
		t.Fatalf("the connection was pushed the configuration that changed while it had said nothing")
	}

	if !strings.Contains(got, changedTagLabel) {
		t.Errorf("the connection was not pushed the configuration that changed after it said who it is: %s", got)
	}
}

// Opening the socket is not how an administrator gets the configuration. The web app reads it from the
// endpoint and opens the socket only to hear about what changes afterwards.
func TestConfigSocketSendsNothingWhenAConnectionOpens(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	fixture.present(t, conn, captureToken)

	if !fixture.nothingArrivedYet(t, conn) {
		t.Fatalf("the server pushed a configuration the administrator did not ask for")
	}
}

// Every frame is checked, not only the first one. A credential that has been retired — which is what the
// rolling window does to an old login — ends the connection the next time it says anything.
func TestConfigSocketChecksEveryFrameTheConnectionSends(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	closed := fixture.closedOnTheServer(t, conn)

	fixture.present(t, conn, captureToken)
	fixture.present(t, conn, captureRefused)

	code, _ := closed()

	if code != 1000 {
		t.Errorf("the connection was closed with %d, want 1000: the legacy server ends a socket whose frame is "+
			"not a credential the same way it ends one whose first frame was not", code)
	}
}

// The socket is not held to an interval. A listener connection that stops answering is ended; an
// administrator connection that simply has nothing to say is not, because it has nothing to answer.
func TestConfigSocketKeepsAConnectionThatSaysNothing(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	fixture.present(t, conn, captureToken)
	fixture.awaitCredential(t, 1)

	// A listener connection that stops answering is ended within a minute. This one is not held to any
	// interval at all, because there is nothing it is being asked to answer: a test cannot wait the minute a
	// listener is given, so it waits a period that would already have ended a socket being timed out at any
	// interval a server would plausibly pick for one that has to stay open across a whole editing session.
	time.Sleep(600 * time.Millisecond)

	fixture.changed(t)

	if fixture.nextFrame(t, conn) == "" {
		t.Fatalf("a connection that said nothing for a moment was no longer pushed the configuration")
	}
}

// The socket is served by the same upgrader the legacy server used, which refuses a connection that comes
// from another origin. The listener connection does not have this rule, and the difference is the legacy
// server's own: one upgrader is asked to accept any origin and the other is left at its default.
func TestConfigSocketRefusesAConnectionFromAnotherOrigin(t *testing.T) {
	fixture := newFixture(t)

	address := "ws://" + strings.TrimPrefix(fixture.server.URL, "http://")

	header := http.Header{}
	header.Set("Origin", "http://elsewhere.example")

	conn, resp, err := websocket.DefaultDialer.Dial(address, header)
	if err == nil {
		conn.Close()

		t.Fatalf("a configuration socket from another origin was accepted; the legacy server's upgrader asks " +
			"for the same origin")
	}

	if resp == nil {
		t.Fatalf("the refused connection got no answer at all: %v", err)
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("the refused connection was answered %d, want the answer the default upgrader gives to a "+
			"connection from another origin", resp.StatusCode)
	}
}

// A save is pushed to every administrator connection that has earned it, and one connection cannot stop
// the others from being told.
func TestConfigurationChangeReachesEveryCredentialPresentingConnection(t *testing.T) {
	fixture := newFixture(t)

	first := fixture.open(t)
	second := fixture.open(t)

	fixture.present(t, first, captureToken)
	fixture.present(t, second, captureToken)

	fixture.awaitCredential(t, 1)
	fixture.awaitCredential(t, 2)

	fixture.changed(t)

	want := fixture.nextFrame(t, first)

	if got := fixture.nextFrame(t, second); got != want {
		t.Errorf("the second administrator connection was pushed a different configuration")
	}

	if want != capturedConfigGolden(t) {
		t.Errorf("the pushed configuration is not the captured one:\n  %s\nwant\n  %s", want, capturedConfigGolden(t))
	}
}

// A connection that has gone away is forgotten rather than written to again, and one connection going away
// does not stop the others from being told.
//
// The legacy server writes to every connection it holds and unregisters the ones that fail. Here the read
// loop notices a closed peer on its own, which is the same thing the legacy server's read loop does; the
// branch that forgets a connection a push could not reach is defensive, and is recorded rather than
// contrived — see docs/specs/FEAT-003-server-ng-refactor/validation.md.
func TestConfigSocketStopsPushingToAConnectionThatWentAway(t *testing.T) {
	fixture := newFixture(t)

	gone := fixture.open(t)
	alive := fixture.open(t)

	fixture.present(t, gone, captureToken)
	fixture.present(t, alive, captureToken)

	fixture.awaitCredential(t, 2)
	fixture.awaitCredential(t, 2)

	gone.Close()

	fixture.awaitGone(t, 1)

	fixture.changed(t)

	if fixture.nextFrame(t, alive) == "" {
		t.Fatalf("a connection that went away stopped the configuration being pushed to the one still open")
	}
}

// A credential the server no longer accepts is not a reason to keep pushing to a connection, and a
// connection that is closed by the other side is not a reason to stop pushing to the rest.
func TestConfigSocketEndsAConnectionWhoseCredentialHasBeenRetired(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	fixture.present(t, conn, captureToken)
	fixture.awaitCredential(t, 1)

	fixture.changed(t)

	if fixture.nextFrame(t, conn) == "" {
		t.Fatalf("the connection was not pushed the configuration")
	}

	// The rolling window retires a credential; the next thing the connection says is checked against what
	// the server still accepts.
	fixture.tokens.Window = []string{"a-newer-token"}

	closed := fixture.closedOnTheServer(t, conn)

	fixture.present(t, conn, captureToken)

	if code, _ := closed(); code != 1000 {
		t.Errorf("a connection whose credential had been retired was closed with %d, want 1000", code)
	}
}

// configurationChangedAfter is a configuration with one tag the capture does not have, which is how a test
// tells two pushes apart.
func configurationChangedAfter(configuration domain.Configuration) domain.Configuration {
	changed := configuration

	changed.Tags = append(append(domain.Tags{}, configuration.Tags...), domain.Tag{ID: 99, Label: changedTagLabel})

	return changed
}

// capturedConfiguration is the configuration the capture had just been saved to when the socket push was
// recorded, read back through the same parsers a save goes through.
func capturedConfiguration(t *testing.T) domain.Configuration {
	t.Helper()

	captured, ok := decodeGolden(t, "adminws-config.json").(map[string]any)
	if !ok {
		t.Fatalf("the captured configuration socket payload is not a configuration")
	}

	configuration := domain.Configuration{
		Access:      domain.ParseAccesses(captured["access"]),
		Apikeys:     domain.ParseApikeys(captured["apikeys"]),
		Dirwatches:  domain.ParseDirwatches(captured["dirwatch"]),
		Downstreams: domain.ParseDownstreams(captured["downstreams"]),
		Groups:      domain.ParseGroups(captured["groups"]),
		Systems:     domain.ParseSystems(captured["systems"]),
		Tags:        domain.ParseTags(captured["tags"]),
	}

	submitted, ok := captured["options"].(map[string]any)
	if !ok {
		t.Fatalf("the captured configuration socket payload has no settings in it")
	}

	configuration.Options = domain.DefaultOptions().ApplyMap(submitted)

	// The capture records every site as belonging to no system at all, because the legacy server's query
	// for sites never asks for the system a site is on. This server reports the system it is stored under,
	// which is the approved divergence recorded as Q58, and the comparison states it instead of skipping
	// the capture.
	for i := range configuration.Systems {
		for j := range configuration.Systems[i].Sites {
			configuration.Systems[i].Sites[j].SystemID = configuration.Systems[i].ID
		}
	}

	return configuration
}

func decodeGolden(t *testing.T, name string) any {
	t.Helper()

	var value any

	if err := json.Unmarshal([]byte(readGolden(t, name)), &value); err != nil {
		t.Fatalf("the capture %s is not JSON: %v", name, err)
	}

	return value
}

func readGolden(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("../../../testdata/golden", name))
	if err != nil {
		t.Fatalf("reading the golden capture %s: %v", name, err)
	}

	return string(b)
}

// capturedConfigGolden is the captured socket push with the two approved divergences stated on it.
//
// The first is Q58: the capture records every site as belonging to no system at all, because the legacy
// server's query for sites never asks for the system a site is on, and this server reports the system a site
// is stored under.
//
// The second is Q60. The captured push carries a display order for every talkgroup, and those numbers exist
// only because computing a listener's configuration sorts the talkgroups the server holds and rewrites each
// one's order to its new position — on the objects the server itself keeps. The listeners were configured
// before this push was captured, so the administrator was handed numbers the server had invented. This server
// copies a system's talkgroups before it sorts or numbers them, so its push carries no talkgroup order keys
// here at all, and the comparison says so instead of skipping the capture.
func capturedConfigGolden(t *testing.T) string {
	t.Helper()

	golden := replaceExactly(t, readGolden(t, "adminws-config.json"), `"systemId":0`, `"systemId":1`, 2)

	for _, invented := range []struct {
		text  string
		times int
	}{
		{`"talkgroup":1,`, 2},
		{`"talkgroup":2,`, 1},
		{`"talkgroup":3,`, 1},
	} {
		golden = replaceExactly(t, golden, invented.text, "", invented.times)
	}

	return golden
}

// replaceExactly rewrites a captured payload to state a divergence, and insists that the divergence is
// still the one the capture recorded: a capture that no longer says what this server disagrees with is a
// capture that has changed, and the test would rather stop than quietly agree with something new.
func replaceExactly(t *testing.T, body, from, to string, times int) string {
	t.Helper()

	if n := strings.Count(body, from); n != times {
		t.Fatalf("the captured payload says %q %d times, want %d: the divergence this test states is no "+
			"longer the one the capture recorded", from, n, times)
	}

	return strings.ReplaceAll(body, from, to)
}
