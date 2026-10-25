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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The administrator's configuration socket is how the administration interface learns
// that something changed while it was open: it sends its credential and then only ever
// reads.
// The first frame must be the bare token and every later frame is checked the same way,
// as the legacy server re-validated whatever arrived, so a retired token ends the
// connection the next time it says anything.

const (
	socketToken    = "capture-administrator-token"
	captureRefused = "not-a-token"

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
	logs     recordedLogs
	sessions *Sessions
	server   *httptest.Server

	configuration domain.Configuration
}

// recordedLogs is the log a test hands the socket and then reads back: the port the
// socket writes through, plus the entries it was given, in order. It is an interface so
// a test can hand the socket a log of its own, one that holds a write open, and still
// read what was written.
type recordedLogs interface {
	ports.LogRepository

	Recorded() []domain.LogEntry
}

// heldLogs is a server log that holds one departure open until the test lets it finish
// and announces the moment that write begins: how a test says an entry is still on its
// way without waiting on a wall clock.
type heldLogs struct {
	*fakes.Logs

	begun   chan domain.LogEntry
	release chan struct{}
}

var _ recordedLogs = (*heldLogs)(nil)

func (logs *heldLogs) AppendLog(ctx context.Context, entry domain.LogEntry) error {
	if strings.Contains(entry.Message, "disconnected") {
		logs.begun <- entry

		<-logs.release
	}

	return logs.Logs.AppendLog(ctx, entry)
}

// adminSocketTime is the instant the fixture's clock stamps every entry with, so a test can tell an entry this
// socket wrote from one it merely hopes it wrote.
var adminSocketTime = time.Date(2026, 4, 2, 6, 30, 0, 0, time.UTC)

func newFixture(t *testing.T) *fixture {
	t.Helper()

	return newFixtureLogging(t, &fakes.Logs{})
}

// newFixtureLogging is the same socket with a log of the test's own handed to it.
func newFixtureLogging(t *testing.T, logs recordedLogs) *fixture {
	t.Helper()

	fixture := &fixture{
		tokens: fakes.NewTokens(),
		logs:   logs,
	}

	fixture.tokens.Window = []string{socketToken}

	fixture.sessions = NewSessions(SessionDeps{
		Session: application.NewCheckAdminSessionHandler(fixture.tokens),
		Version: captureVersion,
		Logs:    fixture.logs,
		Clock:   fakes.NewClock(adminSocketTime),
		Trusted: trustedLocalhostProxy(),
	})

	fixture.server = httptest.NewServer(fixture.sessions)

	t.Cleanup(fixture.server.Close)

	fixture.configuration = capturedConfiguration(t)

	return fixture
}

// open connects to the socket the way the administration interface does: no credential in the request.
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

// present sends the frame the administration interface sends first: the token, on its own, not wrapped in JSON.
func (fixture *fixture) present(t *testing.T, conn *websocket.Conn, token string) {
	t.Helper()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(token)); err != nil {
		t.Fatalf("presenting the token: %v", err)
	}
}

// awaitCredential waits for the server to have accepted credentials from n connections:
// the socket acknowledges nothing, so a test about to push lets the read loops get there
// first, waiting for the fact it depends on rather than the capture's fixed pause. The
// count is of connections because the server holds its own end of each socket.
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

// awaitEntry waits for the socket to have written an entry under a part of the server
// whose message begins with a sentence, and hands the entry back. The socket writes on
// the goroutine serving the connection, so a test waits for the entry it depends on
// rather than for a fixed pause.
func (fixture *fixture) awaitEntry(t *testing.T, source domain.LogSource, sentence string) domain.LogEntry {
	t.Helper()

	var wrote domain.LogEntry

	fixture.await(t, func() bool {
		for _, entry := range fixture.logs.Recorded() {
			if entry.Source == source && strings.HasPrefix(entry.Message, sentence) {
				wrote = entry

				return true
			}
		}

		return false
	}, "the configuration socket never wrote an entry saying "+sentence)

	return wrote
}

// countEntries counts the entries saying exactly that sentence: a departure filed by the
// socket and again by the read loop it just ended would read as two administrators
// leaving and tell an operator a number that never existed.
func (fixture *fixture) countEntries(sentence string) int {
	count := 0

	for _, entry := range fixture.logs.Recorded() {
		if entry.Message == sentence {
			count++
		}
	}

	return count
}
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

// nothingArrivedYet reports whether the connection has been given nothing for a moment,
// which is how these tests say the server is not pushing to it. A read timeout ends the
// connection, so a test that uses this cannot read from the same socket again.
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

	fixture.present(t, conn, socketToken)
	fixture.awaitCredential(t, 1)
	fixture.changed(t)

	got := fixture.nextFrame(t, conn)
	want := capturedConfigGolden(t)

	if got != want {
		t.Errorf("the pushed configuration is\n  %s\nwant\n  %s", got, want)
	}
}

// A credential the server does not recognize ends the connection with the close the
// capture recorded: the upgrade itself asks for nothing, so this is the only thing
// standing between a stranger and the configuration.
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

	if err := json.Unmarshal([]byte(capturedAnswer(t, "adminws-bad-token.json")), &captured); err != nil {
		t.Fatalf("the captured close is not a close: %v", err)
	}

	if code != captured.Code || reason != captured.Reason {
		t.Errorf("the close is %d %q, want the captured close %d %q", code, reason, captured.Code, captured.Reason)
	}

	// The peer sees the close; the operator sees an entry saying a connection presented a
	// credential this server does not have, which separates an expired administration
	// session from somebody prying at the socket. The close is the last thing the server
	// writes on the way out, so waiting for it is enough to know the entry is written.
	entry := fixture.awaitEntry(t, domain.LogSourceAdmin, "an administrator from ")

	if entry.Message != "an administrator from 127.0.0.1 presented a session token that is not one this server issued" {
		t.Errorf("the entry is %q, want it to name the address the credential came from the way the join and "+
			"the departure name it", entry.Message)
	}

	if !strings.Contains(entry.Message, "presented a session token that is not one this server issued") {
		t.Errorf("the entry is %q, want it to say what the connection presented and why it was refused",
			entry.Message)
	}

	// The token is a credential. An entry that names it would put a live session credential in a log an
	// operator can read, and the listener socket's version of this line does not name the access code either.
	if strings.Contains(entry.Message, captureRefused) {
		t.Errorf("the entry is %q, want it to refuse the credential without repeating it", entry.Message)
	}

	if entry.Level != domain.LogLevelWarn {
		t.Errorf("the entry level is %q, want a warning: a credential this server does not have is not an "+
			"ordinary connection", entry.Level)
	}
}

// An administrator connection that joined is recorded in the server log: when the
// administration interface stops working, whether it ever connected and when is the
// first question an operator asks.
func TestAnAdministratorConnectionThatJoinedIsRecorded(t *testing.T) {
	fixture := newFixture(t)

	fixture.open(t)

	entry := fixture.awaitEntry(t, domain.LogSourceAdmin, "an administrator connected from ")

	if entry.Message != "an administrator connected from 127.0.0.1" {
		t.Errorf("the entry is %q, want the address named the way every other entry in this log names it: "+
			"the machine, not the socket it arrived on", entry.Message)
	}

	if entry.Level != domain.LogLevelInfo {
		t.Errorf("the entry level is %q, want %q: a configuration socket opening is not a problem",
			entry.Level, domain.LogLevelInfo)
	}

	if got := entry.DateTime; !got.Equal(adminSocketTime) {
		t.Errorf("the entry is stamped %v, want the instant the server's clock holds", got)
	}
}

// An administrator connection that ended is recorded: a connection that dropped in the
// middle of the night is the difference between a page that was closed and a server that
// let go of it, and only the log can say which.
func TestAnAdministratorConnectionThatEndedIsRecorded(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	fixture.await(t, func() bool { return fixture.heldCount() == 1 }, "the server never took the connection")

	conn.Close()

	fixture.awaitGone(t, 0)

	entry := fixture.awaitEntry(t, domain.LogSourceAdmin, "an administrator disconnected from ")

	if entry.Message != "an administrator disconnected from 127.0.0.1" {
		t.Errorf("the entry is %q, want the address named the way the connection was filed when it joined: "+
			"an operator searching the log for one machine should get both ends of it", entry.Message)
	}

	if entry.Level != domain.LogLevelInfo {
		t.Errorf("the entry level is %q, want %q", entry.Level, domain.LogLevelInfo)
	}
}

// An administrator connection behind a proxy is recorded by the address in the
// forwarding header rather than the proxy's own: every other entry in this log names the
// client, and naming the proxy here would make a search for one machine answer with half
// the truth.
func TestAnAdministratorConnectionBehindAProxyIsRecordedByTheAddressItCameFrom(t *testing.T) {
	fixture := newFixture(t)

	fixture.openAs(t, http.Header{"X-Forwarded-For": []string{"203.0.113.7:51234"}})

	entry := fixture.awaitEntry(t, domain.LogSourceAdmin, "an administrator connected from ")

	if entry.Message != "an administrator connected from 203.0.113.7" {
		t.Errorf("the entry is %q, want the address the forwarding header names, without the port: the socket "+
			"arrived from the proxy and the administrator did not", entry.Message)
	}
}

// Stopping the sessions reports every connection the server is still holding: the end of
// a stop is where the log's database closes, so a departure noticed later could never be
// written and the log view would hold an administrator connection that began and never
// ended.
func TestStoppingTheSessionsReportsEveryConnectionItIsHolding(t *testing.T) {
	fixture := newFixture(t)

	first := fixture.open(t)
	second := fixture.open(t)

	fixture.await(t, func() bool { return fixture.heldCount() == 2 }, "the server never took both connections")

	fixture.sessions.CloseAll(context.Background())

	const left = "an administrator disconnected from 127.0.0.1"

	// Filed while the socket ends the connections, not afterwards: this is the moment the server can still
	// write it down.
	if got := fixture.countEntries(left); got != 2 {
		t.Fatalf("the server log holds %d departure entries when the socket is closed, want one for each of "+
			"the two connections it is holding", got)
	}

	if got := fixture.heldCount(); got != 0 {
		t.Errorf("the server still holds %d administrator connections after closing the socket, want none", got)
	}

	// Both connections are ended, so the administration interface asks for a new one instead of waiting out a read timeout.
	for _, conn := range []*websocket.Conn{first, second} {
		if _, _, err := conn.ReadMessage(); err == nil {
			t.Error("the server kept an administrator connection open after closing the socket")
		}
	}

	// And the read loops that are still unwinding have nothing left to report: a departure is filed by the
	// socket that ends the connection, once.
	if got := fixture.countEntries(left); got != 2 {
		t.Errorf("the server log holds %d departure entries once both read loops have ended, want the same "+
			"two", got)
	}
}

// Closing the sessions waits for a departure already on its way: the read loop took the
// connection out of the set before writing its entry, so the close finds nothing to wait
// for and would otherwise close the log's database while that entry is still coming.
func TestClosingTheSessionsWaitsForADepartureAlreadyOnItsWay(t *testing.T) {
	logs := &heldLogs{
		Logs:    &fakes.Logs{},
		begun:   make(chan domain.LogEntry, 4),
		release: make(chan struct{}),
	}

	fixture := newFixtureLogging(t, logs)

	conn := fixture.open(t)

	fixture.await(t, func() bool { return fixture.heldCount() == 1 }, "the server never took the connection")

	conn.Close()

	// The departure is being written now, and is being held: this is the moment the server must not be
	// allowed to finish stopping.
	entry := <-logs.begun

	if !strings.Contains(entry.Message, "an administrator disconnected") {
		t.Fatalf("the write being held is %q, want the entry saying an administrator disconnected", entry.Message)
	}

	closed := make(chan struct{})

	go func() {
		fixture.sessions.CloseAll(context.Background())

		close(closed)
	}()

	select {
	case <-closed:
		close(logs.release)

		t.Error("the configuration socket finished closing while the entry saying an administrator " +
			"disconnected was still being written: the server closes the database the log lives in the moment " +
			"the close returns, and this entry could only have arrived afterwards")

	case <-time.After(250 * time.Millisecond):
		close(logs.release)

		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			t.Error("the configuration socket never finished closing once the entry it was waiting for had " +
				"been written")
		}
	}

	if got := fixture.countEntries("an administrator disconnected from 127.0.0.1"); got != 1 {
		t.Errorf("the server log holds %d departure entries, want the one connection that let go before the "+
			"socket closed", got)
	}
}

// A connection that has not presented a credential is pushed nothing: the legacy server
// registered the connection at the upgrade and pushed to everything registered, so a
// silent peer received every access code and credential as soon as anybody saved. By the
// owner's decision a silent peer learns nothing until it says who it is.
func TestConfigSocketSendsNothingToAConnectionThatHasNotPresentedACredential(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	// A change while the connection has said nothing. The legacy server pushes it to this connection; this
	// one does not.
	fixture.changed(t)

	// The connection then says who it is, and a second, visibly different change is made:
	// which of the two arrives is the answer - a peer kept out of the configuration is
	// given the second.
	fixture.present(t, conn, socketToken)
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

// Opening the socket is not how an administrator gets the configuration. The administration interface reads it from the
// endpoint and opens the socket only to hear about what changes afterwards.
func TestConfigSocketSendsNothingWhenAConnectionOpens(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	fixture.present(t, conn, socketToken)

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

	fixture.present(t, conn, socketToken)
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

	fixture.present(t, conn, socketToken)
	fixture.awaitCredential(t, 1)

	// A listener connection that stops answering is ended within a minute, and this socket
	// is not held to any interval at all: the test waits a period that would already have
	// ended a socket being timed out at any interval a server would plausibly pick.
	time.Sleep(600 * time.Millisecond)

	fixture.changed(t)

	if fixture.nextFrame(t, conn) == "" {
		t.Fatalf("a connection that said nothing for a moment was no longer pushed the configuration")
	}
}

// The socket is served by the upgrader the legacy server used, which refuses a
// connection from another origin; the listener connection has no such rule, because that
// upgrader was asked to accept any origin.
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

	fixture.present(t, first, socketToken)
	fixture.present(t, second, socketToken)

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

// A connection that has gone away is forgotten rather than written to again, and one peer
// going away does not stop the others being told: the read loop notices a closed peer on
// its own, and the branch that forgets a connection a push could not reach is defensive,
// recorded rather than contrived.
func TestConfigSocketStopsPushingToAConnectionThatWentAway(t *testing.T) {
	fixture := newFixture(t)

	gone := fixture.open(t)
	alive := fixture.open(t)

	fixture.present(t, gone, socketToken)
	fixture.present(t, alive, socketToken)

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

	fixture.present(t, conn, socketToken)
	fixture.awaitCredential(t, 1)

	fixture.changed(t)

	if fixture.nextFrame(t, conn) == "" {
		t.Fatalf("the connection was not pushed the configuration")
	}

	// The rolling window retires a credential; the next thing the connection says is checked against what
	// the server still accepts.
	fixture.tokens.Window = []string{"a-newer-token"}

	closed := fixture.closedOnTheServer(t, conn)

	fixture.present(t, conn, socketToken)

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

	captured, ok := decodeCaptured(t, "adminws-config.json").(map[string]any)
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

	// The capture records every site as belonging to no system because the legacy server's
	// query for sites never asks for that column; this server reports the system the site
	// is stored under, the approved divergence recorded as a preserved quirk, and the
	// comparison states it instead of skipping the capture.
	for i := range configuration.Systems {
		for j := range configuration.Systems[i].Sites {
			configuration.Systems[i].Sites[j].SystemID = configuration.Systems[i].ID
		}
	}

	return configuration
}

// capturedConfigGolden is the captured socket push with the two approved divergences
// stated on it: sites under no system reported as the system they are stored under, and
// no talkgroup display-order keys, because this server copies a system's talkgroups
// before sorting or numbering them.
func capturedConfigGolden(t *testing.T) string {
	t.Helper()

	golden := replaceExactly(t, capturedAnswer(t, "adminws-config.json"), `"systemId":0`, `"systemId":1`, 2)

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

// The configuration socket reads frames up to the same fixed size as the listener
// socket: a bigger frame ends the connection and never reaches the credential check.
func TestConfigSocketEndsAConnectionThatSendsAnOversizedFrame(t *testing.T) {
	fixture := newFixture(t)

	conn := fixture.open(t)

	fixture.present(t, conn, socketToken)
	fixture.awaitCredential(t, 1)

	big := make([]byte, 2*1024*1024)

	// A write that fails here is the rule answering early: the socket ends the connection
	// at the limit, and the client's own write can be the frame's first contact with that
	// end; the test insists the connection is over, whichever half notices.
	if err := conn.WriteMessage(websocket.TextMessage, big); err != nil {
		t.Logf("the oversized frame's write failed: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("the configuration socket kept the connection open on an oversized frame, " +
			"want the server to have ended it")
	}

	fixture.awaitGone(t, 0)

	for _, entry := range fixture.logs.Recorded() {
		if strings.Contains(entry.Message, "presented a session token") {
			t.Errorf("the oversized frame was read and treated as a credential: %q — the "+
				"socket must refuse the frame itself, not judge its contents", entry.Message)
		}
	}
}

// The server asks its administrator connections every so often, and one that stops
// answering holds a goroutine and a slot that a stopping server waits on: the socket ends
// the connection within its pong window, the way the listener socket ends a silent
// listener.
func TestConfigSocketEndsAConnectionThatStopsAnswering(t *testing.T) {
	fixture := newFixture(t)

	fixture.sessions.pingPeriod = 50 * time.Millisecond
	fixture.sessions.pongWait = 200 * time.Millisecond

	conn := fixture.open(t)

	// The peer receives the server's pings and answers none of them.
	conn.SetPingHandler(func(string) error { return nil })

	fixture.present(t, conn, socketToken)
	fixture.awaitCredential(t, 1)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("the configuration socket kept a connection that stopped answering, " +
			"want the server to have ended it")
	}

	fixture.awaitGone(t, 0)
}

// shutdownWindow is the window the stalled-close test gives the socket, the same length the
// server's own stop gives its shutdown.
const shutdownWindow = 10 * time.Second

// A stopping server closes every administrator connection it holds, and a close is a
// write: a peer that stopped reading holds that write for its full deadline, so closes
// made one connection at a time blow past the shutdown window. The closes are made
// together, inside the deadline.
func TestConfigSocketClosesEveryStalledConnectionInsideTheShutdownDeadline(t *testing.T) {
	fixture := newFixture(t)

	const stalled = 50

	for i := 0; i < stalled; i++ {
		conn := fixture.open(t)

		fixture.present(t, conn, socketToken)
	}

	fixture.awaitCredential(t, stalled)

	fixture.sessions.mutex.Lock()

	admins := make([]*administrator, 0, len(fixture.sessions.conns))

	for _, admin := range fixture.sessions.conns {
		admins = append(admins, admin)
	}

	fixture.sessions.mutex.Unlock()

	// Stall every writer: frames bigger than any socket buffer, pushed through the
	// server's own write path, while the peers read nothing. Each flood stops when its
	// write blocks and runs out its deadline - the stalled writer the shutdown has to
	// close past.
	body := bytes.Repeat([]byte("x"), 4*1024*1024)

	wentStalled := make(chan struct{}, stalled)

	for _, admin := range admins {
		go func(admin *administrator) {
			for fixture.sessions.write(admin, body) == nil {
			}

			wentStalled <- struct{}{}
		}(admin)
	}

	for i := 0; i < stalled; i++ {
		select {
		case <-wentStalled:

		case <-time.After(30 * time.Second):
			t.Fatalf("only %d of the %d writers went stalled", i, stalled)
		}
	}

	deadline := time.Now().Add(shutdownWindow)

	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	fixture.sessions.CloseAll(ctx)

	if elapsed := time.Since(deadline); elapsed > 5*time.Second {
		t.Errorf("CloseAll finished %s after its deadline, want the stalled closes made "+
			"together, inside the shutdown window: %s past", elapsed, elapsed.Round(time.Second))
	}
}

// The status view counts the administrator connections the server is holding, shown as
// the connections the administration interface holds open and not as a count of other
// people.
func TestSessionsCountReportsTheConnectionsTheServerIsHolding(t *testing.T) {
	fixture := newFixture(t)

	if count := fixture.sessions.Count(); count != 0 {
		t.Errorf("Count() = %d with no connection open, want 0", count)
	}

	first := fixture.open(t)
	defer first.Close()

	second := fixture.open(t)
	defer second.Close()

	fixture.await(t, func() bool { return fixture.sessions.Count() == 2 },
		"the server never took its place in the set of connections it is holding")

	if count := fixture.sessions.Count(); count != 2 {
		t.Errorf("Count() = %d with two connections open, want 2: the connection asking is one of them", count)
	}

	fixture.sessions.CloseAll(context.Background())

	if count := fixture.sessions.Count(); count != 0 {
		t.Errorf("Count() = %d after CloseAll, want 0: the server is holding nothing", count)
	}
}
