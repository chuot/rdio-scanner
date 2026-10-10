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

package backend

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// openSettings are the settings of a server that admits listeners, which is what most of these tests
// need and what the server's own options default to. A server configured to admit no listeners admits
// none, and that is a case of its own: see the limit test.
var (
	openSettings       = Settings{MaxClients: 200}
	restrictedSettings = Settings{MaxClients: 200, Restricted: true}
)

// hubClockTime is the instant the fixture's clock holds, which is the instant every entry the hub writes has
// to carry. The hub used to stamp its entries with the wall clock of the machine running it, which is how a
// join and the ingest line about the same recording could disagree.
var hubClockTime = time.Date(2026, 3, 19, 14, 22, 0, 0, time.UTC)

// A listener becomes a listener when it is told what it is watching. Until the server has written a
// configuration on its connection it is not counted, not sent a call, and not included in the count
// reported to anybody else — which is why a restricted server, which writes no configuration until an
// access code is accepted, can hold a hundred open connections and report no listeners.
func TestAListenerIsCountedOnlyOnceItsConfigurationHasBeenWritten(t *testing.T) {
	f := newFixture(t, openSettings)

	if got := f.hub.ListenerCount(); got != 0 {
		t.Fatalf("ListenerCount = %d with a connection open and nothing sent on it, want 0", got)
	}

	f.register(t, f.id)

	if got := f.hub.ListenerCount(); got != 1 {
		t.Errorf("ListenerCount = %d after the configuration was written, want 1", got)
	}

	if got := f.changes.count(); got != 1 {
		t.Errorf("the listener count was reported as changed %d times, want once, when the listener joined", got)
	}

	if !f.loggedAs(domain.LogSourceLivefeed, "a listener joined from 203.0.113.7:41000") {
		t.Errorf("the server log = %q, want the join notice naming the address it connected from, under the livefeed", f.logText())
	}
}

// A listener that unlocked an access code is joined by the identity it unlocked, and one that did not
// is joined by address alone.
func TestAListenerThatUnlockedAnAccessIsNamedByIt(t *testing.T) {
	f := newFixture(t, restrictedSettings)

	access, _ := domain.ParseAccessScope("*")
	f.hub.GrantAccess(f.id, domain.Access{ID: 1, Code: "open", Ident: "Capture open", Scope: access})

	f.register(t, f.id)

	if !f.loggedAs(domain.LogSourceLivefeed, "a listener joined from 203.0.113.7:41000 as Capture open") {
		t.Errorf("the join notice = %q, want the identity it unlocked, under the livefeed", f.logText())
	}
}

// TestWhatTheHubReportsIsStampedWithTheServersClock is the half of the rule the hub had wrong: it stamped its
// own entries with the wall clock of the machine it was running on, while every other writer stamps the
// server's clock. A join and the ingest line about the same recording could then disagree by however far the
// two were apart, on the one machine where an operator is reading the log because the clocks cannot be
// trusted.
func TestWhatTheHubReportsIsStampedWithTheServersClock(t *testing.T) {
	f := newFixture(t, openSettings)

	f.register(t, f.id)

	if err := f.client.Close(); err != nil {
		t.Fatalf("closing the listener connection: %v", err)
	}

	f.waitForDeparture(t, "a listener left from")

	f.assertStamped(t, "a listener joined from", hubClockTime)
	f.assertStamped(t, "a listener left from", hubClockTime)
}

// The limit on listeners is compared against the listeners the server is counting, and the comparison
// is `count >= limit`: a server configured to admit one listener answers the first one and closes the
// second without a word, and a server configured to admit none closes the first. Only listeners the
// server is counting are against the limit, so connections that never became listeners are not.
func TestTheServerStopsAdmittingListenersAtItsLimit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		maxClients uint
		want       []bool
	}{
		{name: "one listener", maxClients: 1, want: []bool{true, false}},
		{name: "no listeners", maxClients: 0, want: []bool{false, false}},
	} {
		f := newBareFixture(t, Settings{MaxClients: tc.maxClients})

		for i, want := range tc.want {
			id, _ := f.dial(t)

			if id != "" {
				f.register(t, id)
			}

			if got := id != ""; got != want {
				t.Errorf("%s: connection %d admitted = %v, want %v", tc.name, i+1, got, want)
			}
		}
	}
}

// A configuration change reaches every listener. On a server that requires an access code it does not
// reach them as a configuration: every listener is asked for its code again, because the change may
// have been the access that decides what that listener is allowed to see.
func TestAConfigurationChangeReachesEveryListener(t *testing.T) {
	f := newFixture(t, openSettings)

	f.register(t, f.id)

	second, _ := f.dial(t)
	f.register(t, second)

	f.settings.set(restrictedSettings)

	f.hub.BroadcastConfig(testSnapshot())

	for _, id := range []string{f.id, second} {
		if frame := f.expectFrame(t, id); frame.Command != CommandAccessCode {
			t.Errorf("listener %s was sent %v on a restricted server, want a request for its access code", id, frame.Command)
		}
	}
}

// The listeners count rides along with a configuration change when the option asks for it, and does
// not when it does not.
func TestAConfigurationChangeCarriesTheListenersCountWhenTheServerReportsIt(t *testing.T) {
	for _, show := range []bool{true, false} {
		f := newFixture(t, openSettings)

		snapshot := testSnapshot()
		snapshot.Options.ShowListenersCount = show

		f.register(t, f.id)

		f.hub.BroadcastConfig(snapshot)

		if frame := f.expectFrame(t, f.id); frame.Command != CommandConfig {
			t.Errorf("a listener was sent %v after a configuration change, want its own configuration", frame.Command)
		}

		if show {
			frame := f.expectFrame(t, f.id)

			if frame.Command != CommandListenersCount {
				t.Errorf("a listener was sent %v after a configuration change, want the listeners count", frame.Command)
			}

			if number, ok := frame.Payload.(float64); !ok || number != 1 {
				t.Errorf("the listeners count = %v, want 1", frame.Payload)
			}

			continue
		}

		if !f.quiet(t, f.id, 200*time.Millisecond) {
			t.Errorf("a listener was sent the listeners count with the option off")
		}
	}
}

// Two listeners on one server are not told the same thing. What each is shown is decided by the access
// it holds, and the groups, tags, and systems in the answer are the ones that listener was shown.
func TestEachListenerIsGivenItsOwnConfiguration(t *testing.T) {
	f := newFixture(t, restrictedSettings)

	open, _ := domain.ParseAccessScope("*")
	one, _ := domain.ParseAccessScope([]any{map[string]any{"id": float64(10002), "talkgroups": "*"}})

	f.hub.GrantAccess(f.id, domain.Access{ID: 1, Code: "open", Ident: "Open", Scope: open})

	second, _ := f.dial(t)

	f.hub.GrantAccess(second, domain.Access{ID: 2, Code: "one", Ident: "One", Scope: one})

	unrestricted := jsonOf(t, f.register(t, f.id).Payload)
	scoped := jsonOf(t, f.register(t, second).Payload)

	if !strings.Contains(unrestricted, `"id":10001`) || !strings.Contains(unrestricted, `"id":10002`) {
		t.Errorf("the unrestricted listener was shown %s, want both systems", unrestricted)
	}

	if !strings.Contains(scoped, `"id":10002`) || strings.Contains(scoped, `"id":10001`) {
		t.Errorf("the restricted listener was shown %s, want only the system its access names", scoped)
	}

	if !strings.Contains(scoped, `"Untagged"`) || strings.Contains(scoped, `"Fire Tac"`) {
		t.Errorf("the restricted listener was shown tags that are not the ones its own talkgroups use: %s", scoped)
	}
}

// A recorded call is delivered to a listener when both things say so: the listener's live-watch
// selection asks for that system or talkgroup, and — on a server that requires access codes — the
// listener's access grants it. On a server that requires nothing, the access is not consulted at all,
// which is how an anonymous listener on an open server sees every call.
func TestACallReachesTheListenersWhoseLiveWatchAsksForIt(t *testing.T) {
	f := newFixture(t, openSettings)

	f.register(t, f.id)

	second, _ := f.dial(t)
	f.register(t, second)

	f.hub.SetLivefeed(f.id, watching("10001", "1514000"))
	f.hub.SetLivefeed(second, watching("10002", "1515000"))

	f.hub.BroadcastCall(testCall(10001, 1514000))

	if frame := f.expectFrame(t, f.id); frame.Command != CommandCall {
		t.Errorf("the listener watching this talkgroup received %v, want the call", frame.Command)
	}

	if !f.quiet(t, second, 200*time.Millisecond) {
		t.Errorf("the listener watching a different system was sent the call")
	}
}

// On a server that requires an access code, a listener whose access does not grant the call does not
// get it — and a listener with no access at all gets nothing, because an access that names no systems
// grants nothing.
func TestACallIsRefusedToTheListenersWhoseAccessDoesNotGrantIt(t *testing.T) {
	f := newFixture(t, restrictedSettings)

	one, _ := domain.ParseAccessScope([]any{map[string]any{"id": float64(10002), "talkgroups": "*"}})

	f.hub.GrantAccess(f.id, domain.Access{ID: 1, Code: "one", Ident: "One", Scope: one})
	f.register(t, f.id)

	f.hub.SetLivefeed(f.id, watching("10001", "1514000"))

	f.hub.BroadcastCall(testCall(10001, 1514000))

	if !f.quiet(t, f.id, 200*time.Millisecond) {
		t.Errorf("a listener whose access names only system 10002 was sent a call on system 10001")
	}
}

// On a server that requires nothing, the access a listener holds is not consulted: an anonymous
// listener sees every call the server records.
func TestACallReachesEveryListenerWhenTheServerRequiresNoAccessCode(t *testing.T) {
	f := newFixture(t, openSettings)

	f.register(t, f.id)

	f.hub.SetLivefeed(f.id, watching("10001", "1514000"))

	f.hub.BroadcastCall(testCall(10001, 1514000))

	if frame := f.expectFrame(t, f.id); frame.Command != CommandCall {
		t.Errorf("an anonymous listener received %v, want the call", frame.Command)
	}
}

// A live-watch selection is stored on the session and answered with whether anything is now switched
// on, which is the only thing the listener is told back.
func TestALiveWatchSelectionIsAnsweredWithWhetherAnythingIsOn(t *testing.T) {
	f := newFixture(t, openSettings)

	f.register(t, f.id)

	f.hub.SetLivefeed(f.id, watching("10001", "1514000"))

	if f.hub.LivefeedWatchesNothing(f.id) {
		t.Error("a listener watching system 10001 reports that it watches nothing")
	}

	f.hub.SendLivefeedMap(f.id, true)

	frame := f.expectFrame(t, f.id)

	if frame.Command != CommandLivefeedMap {
		t.Errorf("the ack = %v, want a livefeed answer", frame.Command)
	} else if on, ok := frame.Payload.(bool); !ok || !on {
		t.Errorf("the ack payload = %v, want true", frame.Payload)
	}

	f.hub.SetLivefeed(f.id, domain.NewLivefeed())

	if !f.hub.LivefeedWatchesNothing(f.id) {
		t.Error("a listener that switched everything off still reports that it watches something")
	}

	f.hub.SendLivefeedMap(f.id, false)

	if frame := f.expectFrame(t, f.id); frame.Payload != false {
		t.Errorf("the ack payload = %v, want false: a listener has to be able to tell \"no\" from \"nothing yet\"", frame.Payload)
	}
}

// Attempts at an access code are counted per session, and only an accepted code clears the count. A
// session that is refused — for a bad code, for an expired access, for its connection limit, or for
// having tried too many times — keeps spending attempts.
func TestAccessAttemptsAreCountedPerSession(t *testing.T) {
	f := newFixture(t, restrictedSettings)

	for want := uint(1); want <= 6; want++ {
		if got := f.hub.RecordAccessAttempt(f.id); got != want {
			t.Fatalf("RecordAccessAttempt = %d, want %d", got, want)
		}
	}

	f.hub.ResetAccessAttempts(f.id)

	if got := f.hub.RecordAccessAttempt(f.id); got != 1 {
		t.Errorf("RecordAccessAttempt after a reset = %d, want 1", got)
	}

	if got := f.hub.RecordAccessAttempt("another-session"); got != 1 {
		t.Errorf("another session's first attempt = %d, want 1: attempts belong to a session and not to the server", got)
	}
}

// Sessions are counted against the access they hold, and an access nobody holds has no sessions. The
// count is of listeners the server is counting, so a connection that has not become a listener is not
// one of them.
func TestSessionsHoldingOneAccessAreCountedTogether(t *testing.T) {
	f := newFixture(t, restrictedSettings)

	access, _ := domain.ParseAccessScope("*")

	f.hub.GrantAccess(f.id, domain.Access{ID: 7, Code: "shared", Ident: "Shared", Scope: access})
	f.register(t, f.id)

	second, _ := f.dial(t)

	f.hub.GrantAccess(second, domain.Access{ID: 7, Code: "shared", Ident: "Shared", Scope: access})

	if got := f.hub.AccessSessionCount(7); got != 1 {
		t.Errorf("AccessSessionCount = %d while the second session is open but not a listener yet, want 1", got)
	}

	f.register(t, second)

	if got := f.hub.AccessSessionCount(7); got != 2 {
		t.Errorf("AccessSessionCount = %d, want the two sessions holding that access", got)
	}

	if got := f.hub.AccessSessionCount(8); got != 0 {
		t.Errorf("AccessSessionCount for an access nobody holds = %d, want 0", got)
	}
}

// The version report names the server, and names the branding and the contact address only when the
// server has been given them.
func TestTheVersionReportNamesOnlyWhatTheServerHasBeenGiven(t *testing.T) {
	f := newFixture(t, Settings{Branding: "Capture Dispatch", Email: "dispatch@example.com", MaxClients: 200})

	f.hub.SendVersion(f.id, "8.0.0")

	frame := f.expectFrame(t, f.id)

	if frame.Command != CommandVersion {
		t.Fatalf("the listener received %v, want a version report", frame.Command)
	}

	if got, want := jsonOf(t, frame.Payload), `{"branding":"Capture Dispatch","email":"dispatch@example.com","version":"8.0.0"}`; got != want {
		t.Errorf("the version report = %s, want %s", got, want)
	}

	quiet := newFixture(t, openSettings)

	quiet.hub.SendVersion(quiet.id, "8.0.0")

	if got := jsonOf(t, quiet.expectFrame(t, quiet.id).Payload); got != `{"version":"8.0.0"}` {
		t.Errorf("a server with no branding and no address reports %s, want only the version", got)
	}
}

// A listener that goes away stops being counted, stops being told anything, and is reported to the
// server log by the address it connected from — and by the identity it unlocked, when it unlocked one.
func TestADisconnectedListenerIsReportedAndForgotten(t *testing.T) {
	f := newFixture(t, restrictedSettings)

	access, _ := domain.ParseAccessScope("*")

	f.hub.GrantAccess(f.id, domain.Access{ID: 1, Code: "open", Ident: "Capture open", Scope: access})
	f.register(t, f.id)

	if err := f.client.Close(); err != nil {
		t.Fatalf("closing the listener connection: %v", err)
	}

	f.waitForDeparture(t, "a listener left from 203.0.113.7:41000 as Capture open")

	if got := f.changes.count(); got != 2 {
		t.Errorf("the listener count was reported as changed %d times, want twice: once when it joined and once when it left", got)
	}

	if !f.loggedAs(domain.LogSourceLivefeed, "a listener left from 203.0.113.7:41000 as Capture open") {
		t.Errorf("the server log = %q, want the departure naming the address and the identity it unlocked", f.logText())
	}

	f.hub.BroadcastCall(testCall(10001, 1514000))

	if got := f.hub.RemoteAddrOf(f.id); got != "" {
		t.Errorf("the hub still holds the session that disconnected: %q", got)
	}

	if got := f.hub.ListenerCount(); got != 0 {
		t.Errorf("ListenerCount = %d after the listener disconnected, want 0", got)
	}
}

// TestStoppingTheHubReportsEveryConnectionItIsHolding is the shutdown half of the departure report.
//
// Shutting an address down does not wait for a connection the server has taken over: the moment the
// listener endpoint hands a connection to the hub, the address that upgraded it stops tracking it. So a
// server that stops reaches the end of its stop with listener connections still open — and the end of its
// stop is where the database the log lives in is closed. A departure left to the goroutine that was
// reading one of those connections is an entry written to a log that has just been closed, which is an
// entry nobody can read afterwards.
func TestStoppingTheHubReportsEveryConnectionItIsHolding(t *testing.T) {
	f := newFixture(t, restrictedSettings)

	access, _ := domain.ParseAccessScope("*")

	f.hub.GrantAccess(f.id, domain.Access{ID: 1, Code: "open", Ident: "Capture open", Scope: access})
	f.register(t, f.id)

	second, secondConn := f.dial(t)

	f.hub.GrantAccess(second, domain.Access{ID: 1, Code: "open", Ident: "Capture open", Scope: access})
	f.register(t, second)

	f.hub.Close()

	const left = "a listener left from 203.0.113.7:41000 as Capture open"

	// Filed while the hub ends the connection, not afterwards: this is the moment the server can still
	// write it down.
	if got := f.countLogged(left); got != 2 {
		t.Errorf("the server log holds %d departure entries when the hub closes, want one for each of the "+
			"two listeners it is holding: %s", got, f.logText())
	}

	if got := f.hub.ListenerCount(); got != 0 {
		t.Errorf("ListenerCount = %d after the hub closed, want no listener left", got)
	}

	// Both connections are ended, which is what lets a client reconnect to the next server instead of
	// waiting out a read timeout.
	for _, conn := range []*websocket.Conn{f.client, secondConn} {
		if _, _, err := conn.ReadMessage(); err == nil {
			t.Error("the server kept a listener connection open after closing the hub")
		}
	}

	// And the read loops that are still unwinding have nothing left to report: a departure is filed by the
	// hub that ends the connection, once.
	if got := f.countLogged(left); got != 2 {
		t.Errorf("the server log holds %d departure entries once both read loops have ended, want the same "+
			"two: %s", got, f.logText())
	}
}

// TestClosingTheHubWaitsForADepartureAlreadyOnItsWay is the half of the shutdown that ending the connections
// does not cover. A connection that let go a moment earlier is already being reported by the goroutine that
// was reading it — which had taken the session out of the hub before it wrote the entry, so the close finds
// nothing to report and nothing to wait for. The server then says it stopped and closes the database the log
// lives in, and the entry that was already on its way arrives at a log that has just been closed. That is the
// shape of the flake this feature spent a suite chasing: a departure missing from the log view, and a
// database left holding a rollback journal nobody can account for.
func TestClosingTheHubWaitsForADepartureAlreadyOnItsWay(t *testing.T) {
	logs := &heldLogs{
		Logs:    &fakes.Logs{},
		begun:   make(chan domain.LogEntry, 4),
		release: make(chan struct{}),
	}

	f := newFixtureLogging(t, restrictedSettings, logs)

	access, _ := domain.ParseAccessScope("*")

	f.hub.GrantAccess(f.id, domain.Access{ID: 1, Code: "open", Ident: "Capture open", Scope: access})
	f.register(t, f.id)

	// The connection lets go on its own, which is what a client does when a server is being restarted.
	if err := f.client.Close(); err != nil {
		t.Fatalf("closing the listener connection: %v", err)
	}

	// The departure is being written now, and is being held: this is the moment the server must not be
	// allowed to finish stopping.
	entry := <-logs.begun

	if !strings.Contains(entry.Message, "a listener left") {
		t.Fatalf("the write being held is %q, want the entry saying a listener left", entry.Message)
	}

	closed := make(chan struct{})

	go func() {
		f.hub.Close()

		close(closed)
	}()

	select {
	case <-closed:
		close(logs.release)

		t.Error("the hub finished closing while the entry saying a listener left was still being written: " +
			"the server closes the database the log lives in the moment the close returns, and this entry " +
			"could only have arrived afterwards")

	case <-time.After(250 * time.Millisecond):
		close(logs.release)

		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			t.Error("the hub never finished closing once the entry it was waiting for had been written")
		}
	}

	if !f.loggedAs(domain.LogSourceLivefeed, "a listener left from 203.0.113.7:41000 as Capture open") {
		t.Errorf("the server log = %q, want the departure of the connection that let go before the hub closed",
			f.logText())
	}
}

// A listener that never gets a configuration is not left open forever. The server gives a connection a
// window to become a listener and ends it when the window closes, which is what drops a connection on
// a restricted server whose access code never arrives.
func TestAConnectionThatNeverBecomesAListenerIsEnded(t *testing.T) {
	f := newFixture(t, restrictedSettings)

	f.expectClosed(t)
}

// A listener that stops answering the server is dropped. The server asks every so often, and a
// connection that does not answer is not one it can send anything to.
func TestAListenerThatStopsAnsweringIsEnded(t *testing.T) {
	f := newBareFixture(t, openSettings)

	// The window a connection has to answer is set before the connection arrives, because the server
	// starts counting it the moment it takes the connection over.
	f.hub.handshakeWait = 10 * time.Second
	f.hub.pingPeriod = 50 * time.Millisecond
	f.hub.pongWait = 200 * time.Millisecond

	f.id, f.client = f.dial(t)

	f.client.SetPingHandler(func(string) error { return nil })

	f.register(t, f.id)

	f.expectClosed(t)
}

// A listener that does answer is kept, and is asked regularly enough that a dead connection does not
// go unnoticed.
func TestTheServerKeepsAskingALiveListener(t *testing.T) {
	f := newFixture(t, openSettings)

	pings := make(chan struct{}, 10)

	f.client.SetPingHandler(func(appData string) error {
		pings <- struct{}{}

		return f.client.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
	})

	f.register(t, f.id)

	// A websocket client only deals with what arrives on its connection when something reads it, and a
	// ping is no exception: nothing else on the connection answers it. So somebody has to keep reading
	// for the listener to keep answering.
	go func() {
		for {
			if _, _, err := f.client.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for i := 0; i < 3; i++ {
		select {
		case <-pings:
		case <-time.After(2 * time.Second):
			t.Fatalf("the server asked %d times, want at least three times in a row", i)
		}
	}

	if got := f.hub.ListenerCount(); got != 1 {
		t.Errorf("ListenerCount = %d while the listener is answering, want 1", got)
	}
}

// Ending a session is something the server can do on its own, and the close code it chooses is the
// one the listener sees.
func TestClosingAListenerEndsTheSession(t *testing.T) {
	f := newFixture(t, openSettings)

	f.register(t, f.id)

	f.hub.CloseClient(f.id, 1000)

	f.expectClosed(t)

	if got := f.hub.ListenerCount(); got != 0 {
		t.Errorf("ListenerCount = %d after the session was ended, want 0", got)
	}
}

// A frame the server cannot read is not a reason to end the connection it arrived on. The server says
// one line about it and reads the next frame, because a client that sends one broken frame is still a
// client.
func TestAFrameTheServerCannotReadDoesNotEndTheConnection(t *testing.T) {
	f := newFixture(t, openSettings)

	if err := f.client.WriteMessage(websocket.TextMessage, []byte(`["CAL"`)); err != nil {
		t.Fatalf("writing a broken frame: %v", err)
	}

	f.waitFor(t, func() bool {
		return f.loggedAt(domain.LogSourceLivefeed, domain.LogLevelWarn, "a message from a listener could not be read")
	}, "the server to file the frame it could not read")

	if err := f.client.WriteMessage(websocket.TextMessage, []byte(`["VER"]`)); err != nil {
		t.Fatalf("writing the frame that follows: %v", err)
	}

	f.waitFor(t, func() bool { return f.dispatch.lastCommand() == CommandVersion }, "the frame that follows to be read")
}

// A frame the server reads but cannot act on is reported the same way and ends the connection just as
// little.
func TestAFrameTheServerCannotActOnDoesNotEndTheConnection(t *testing.T) {
	f := newFixture(t, openSettings)

	f.dispatch.fail = errDispatch

	if err := f.client.WriteMessage(websocket.TextMessage, []byte(`["CAL",{"id":7}]`)); err != nil {
		t.Fatalf("writing the frame: %v", err)
	}

	f.waitFor(t, func() bool {
		return f.loggedAt(domain.LogSourceLivefeed, domain.LogLevelWarn, "a message from a listener was not understood")
	}, "the server to file the frame it could not act on")

	if got := f.hub.ListenerCount(); got != 0 {
		t.Errorf("ListenerCount = %d, want 0: a refused frame does not make a listener out of a connection", got)
	}
}

// A frame the server could not put on the connection is the one delivery failure an operator could not see:
// the listener stays connected, the frame is simply gone, and nothing was written about it. It is filed under
// the live feed at the level an operator filters problems with, naming the encoding failure itself, and the
// session survives it — the frame that follows reaches the listener.
func TestAFrameTheServerCouldNotWriteIsFiledAndTheListenerKeepsItsConnection(t *testing.T) {
	f := newFixture(t, openSettings)

	f.register(t, f.id)

	f.hub.Send(f.id, Message{Command: CommandVersion, Payload: math.NaN()})

	f.waitFor(t, func() bool {
		return f.loggedAt(domain.LogSourceLivefeed, domain.LogLevelWarn, "a message for a listener could not be written")
	}, "the server to file the frame it could not write")

	if !strings.Contains(f.logText(), "NaN") {
		t.Errorf("the entry does not name the encoding failure itself:\n%s", f.logText())
	}

	f.hub.Send(f.id, Message{Command: CommandLivefeedMap, Payload: true})

	frame := f.expectFrame(t, f.id)

	if frame.Command != CommandLivefeedMap {
		t.Errorf("the frame that follows = %v, want %v: a frame the server could not encode is not a reason "+
			"to lose a listener", frame.Command, CommandLivefeedMap)
	}
}

// The address a session connected from is what the server names when it refuses an access code, and it
// is the address of the connection and not of the listener behind it.
func TestASessionKnowsWhereItConnectedFrom(t *testing.T) {
	f := newFixture(t, openSettings)

	if got := f.hub.RemoteAddrOf(f.id); got != "203.0.113.7:41000" {
		t.Errorf("RemoteAddrOf = %q, want the address the connection came from", got)
	}

	if got := f.hub.RemoteAddrOf("not-a-session"); got != "" {
		t.Errorf("RemoteAddrOf for a session that is not here = %q, want nothing", got)
	}
}

// A session holds the access it was given, including when it was given one and then refused for a
// reason that kept it: a session refused for an expired access is still known by the identity it
// presented, and that is what the disconnect notice names.
func TestASessionHoldsTheAccessItWasGiven(t *testing.T) {
	f := newFixture(t, restrictedSettings)

	if _, ok := f.hub.AccessOf(f.id); ok {
		t.Error("a session that has not been given an access already holds one")
	}

	f.hub.GrantAccess(f.id, domain.Access{ID: 3, Code: "expired", Ident: "Capture expired"})

	access, ok := f.hub.AccessOf(f.id)

	if !ok {
		t.Fatal("the session does not hold the access it was given")
	}

	if access.Ident != "Capture expired" {
		t.Errorf("the session holds %+v, want the access it was given", access)
	}
}

// The frames the hub sends are the frames of the protocol: a request for an access code is the command
// alone, and the listeners count is a number and not a string.
func TestTheHubSendsTheProtocolFramesItIsAskedToSend(t *testing.T) {
	f := newFixture(t, openSettings)

	f.register(t, f.id)

	f.hub.RequestAccessCode(f.id)

	if frame := f.expectFrame(t, f.id); frame.Command != CommandAccessCode || frame.Payload != nil {
		t.Errorf("the request = %v %v, want the command with nothing after it", frame.Command, frame.Payload)
	}

	f.hub.ReportAccessExpired(f.id)

	if frame := f.expectFrame(t, f.id); frame.Command != CommandAccessExpired {
		t.Errorf("the expiry notice = %v, want %v", frame.Command, CommandAccessExpired)
	}

	f.hub.ReportAccessLimit(f.id)

	if frame := f.expectFrame(t, f.id); frame.Command != CommandAccessLimit {
		t.Errorf("the limit notice = %v, want %v", frame.Command, CommandAccessLimit)
	}

	f.hub.BroadcastListenersCount(3)

	if frame := f.expectFrame(t, f.id); frame.Command != CommandListenersCount || frame.Payload != float64(3) {
		t.Errorf("the listeners count = %v %v, want the number 3", frame.Command, frame.Payload)
	}
}

// errDispatch stands for a server that read a frame and could not do anything with it.
var errDispatch = errorString("no handler for this frame")

type errorString string

func (e errorString) Error() string { return string(e) }

// fixture is one hub, the things the hub was told about the server, and the listener connections that
// have dialed into it.
type fixture struct {
	hub      *Hub
	logs     recordedLogs
	settings *mutableSettings
	changes  *recordedChanges
	dispatch *recordingDispatch

	attached chan string
	server   *httptest.Server
	clients  map[string]*websocket.Conn

	// endpoint replaces the hub-only endpoint with the one a test is exercising. It is set before the
	// first connection dials.
	endpoint func(http.ResponseWriter, *http.Request)

	id     string
	client *websocket.Conn
}

// recordedLogs is the log a test hands the hub and then reads back: the port the hub writes through, plus
// the entries it was given, in the order it was given them. It is an interface rather than the fake so that
// a test can hand the hub a log of its own — one that holds a write open, say — and still read what the hub
// wrote.
type recordedLogs interface {
	ports.LogRepository

	Recorded() []domain.LogEntry
}

func newFixture(t *testing.T, settings Settings) *fixture {
	t.Helper()

	f := newBareFixture(t, settings)

	f.id, f.client = f.dial(t)

	return f
}

// newFixtureLogging is the same fixture with a log of the test's own handed to the hub.
func newFixtureLogging(t *testing.T, settings Settings, logs recordedLogs) *fixture {
	t.Helper()

	f := newBareFixtureLogging(t, settings, logs)

	f.id, f.client = f.dial(t)

	return f
}

func newBareFixture(t *testing.T, settings Settings) *fixture {
	t.Helper()

	return newBareFixtureLogging(t, settings, &fakes.Logs{})
}

func newBareFixtureLogging(t *testing.T, settings Settings, logs recordedLogs) *fixture {
	t.Helper()

	current := &mutableSettings{settings: settings}
	changes := &recordedChanges{}
	dispatch := &recordingDispatch{}

	f := &fixture{
		attached: make(chan string, 16),
		clients:  map[string]*websocket.Conn{},
		changes:  changes,
		dispatch: dispatch,
		logs:     logs,
		settings: current,
	}

	f.hub = NewHub(current, changes, logs, fakes.NewClock(hubClockTime))

	// The intervals a connection is held to are the protocol's own, and a test cannot wait a minute
	// for one of them. They are shortened here rather than made choices a server can pass to the
	// constructor: nothing outside this package should be able to pick a value that breaks the
	// protocol, and only this package's tests need to pick one at all.
	f.hub.handshakeWait = 200 * time.Millisecond
	f.hub.pingPeriod = 100 * time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.endpoint != nil {
			f.endpoint(w, r)

			return
		}

		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrading: %v", err)

			return
		}

		id, ok := f.hub.Attach(conn, "203.0.113.7:41000", dispatch.handle)

		if !ok {
			// An empty identifier is the server saying it refused the connection.
			f.attached <- ""

			return
		}

		f.attached <- id
	}))

	t.Cleanup(server.Close)

	f.server = server

	return f
}

// open connects to the fixture's endpoint and returns the connection, without waiting for the hub to
// take it over.
func (f *fixture) open(t *testing.T) *websocket.Conn {
	t.Helper()

	return f.openWith(t, nil)
}

// openWith connects to the fixture's endpoint with extra request headers, which is how a test hands the
// server a forwarding header the way another proxy would.
func (f *fixture) openWith(t *testing.T, header http.Header) *websocket.Conn {
	t.Helper()

	address := "ws://" + strings.TrimPrefix(f.server.URL, "http://")

	conn, _, err := websocket.DefaultDialer.Dial(address, header)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}

	conn.SetPingHandler(func(appData string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
	})

	t.Cleanup(func() { conn.Close() })

	return conn
}

// send writes one frame on a connection, the way a listener does.
func (f *fixture) send(t *testing.T, conn *websocket.Conn, frame Message) {
	t.Helper()

	b, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshaling %v: %v", frame, err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatalf("writing %v: %v", frame, err)
	}
}

// dial opens another listener connection and waits for the server to take it over. The identifier is
// empty when the server refused the connection.
func (f *fixture) dial(t *testing.T) (string, *websocket.Conn) {
	t.Helper()

	conn := f.open(t)

	select {
	case id := <-f.attached:
		f.clients[id] = conn

		return id, conn

	case <-time.After(2 * time.Second):
		return "", nil
	}
}

// dialEndpoint opens a connection against the endpoint the fixture is exercising. It does not wait for
// the hub to name the session, because an endpoint test talks to its connection and not to the hub.
func (f *fixture) dialEndpoint(t *testing.T) *websocket.Conn {
	t.Helper()

	return f.open(t)
}

// register makes one dialed connection a listener: the server writes its configuration on it, and the
// connection reads that configuration back.
func (f *fixture) register(t *testing.T, id string) Message {
	t.Helper()

	f.hub.SendConfig(id, testSnapshot())

	frame := f.expectFrame(t, id)

	if frame.Command != CommandConfig {
		t.Fatalf("listener %s was sent %v, want its configuration", id, frame.Command)
	}

	return frame
}

// expectFrame reads the next frame the server sent to one listener.
func (f *fixture) expectFrame(t *testing.T, id string) Message {
	t.Helper()

	conn, ok := f.clients[id]
	if !ok {
		t.Fatalf("there is no listener connection %s", id)
	}

	return f.expectOn(t, conn)
}

// expectOn reads the next frame the server sent on one connection.
func (f *fixture) expectOn(t *testing.T, conn *websocket.Conn) Message {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("setting a read deadline: %v", err)
	}

	_, b, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("reading a frame: %v", err)
	}

	frame, err := ReadMessage(b)
	if err != nil {
		t.Fatalf("reading a frame: %v", err)
	}

	return frame
}

// quiet reports whether nothing arrived at one listener for a while, which is how a test proves a
// listener was deliberately sent nothing.
func (f *fixture) quiet(t *testing.T, id string, window time.Duration) bool {
	t.Helper()

	conn, ok := f.clients[id]
	if !ok {
		t.Fatalf("there is no listener connection %s", id)
	}

	return f.quietOn(t, conn, window)
}

// quietOn reports whether nothing arrived on one connection for a while.
func (f *fixture) quietOn(t *testing.T, conn *websocket.Conn, window time.Duration) bool {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(window)); err != nil {
		t.Fatalf("setting a read deadline: %v", err)
	}

	_, _, err := conn.ReadMessage()

	return err != nil
}

// expectClosed waits for the server to end the first connection.
func (f *fixture) expectClosed(t *testing.T) {
	t.Helper()

	if err := f.client.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("setting a read deadline: %v", err)
	}

	if _, _, err := f.client.ReadMessage(); err == nil {
		t.Fatal("the connection is still open, want the server to have ended it")
	}

	f.waitFor(t, func() bool { return f.hub.ListenerCount() == 0 }, "the session to be forgotten")
}

// logText is everything the hub has written to the server log, oldest first, which is how a test looks
// for one notice without having to know how many other notices came first.
func (f *fixture) logText() string {
	recorded := f.logs.Recorded()

	lines := make([]string, 0, len(recorded))

	for _, entry := range recorded {
		lines = append(lines, entry.Message)
	}

	return strings.Join(lines, "\n")
}

// logAt reads one line the hub wrote to the server log, oldest first.
func (f *fixture) logAt(t *testing.T, at int) string {
	t.Helper()

	recorded := f.logs.Recorded()

	if at >= len(recorded) {
		t.Fatalf("the server log holds %d entries, want at least %d", len(recorded), at+1)
	}

	return recorded[at].Message
}

// assertStamped insists that every entry mentioning a fragment carries the instant the server's clock holds,
// and not the wall clock of the machine the test is running on.
//
// The server has one clock, injected into every writer, so that the entries in the log view can be put in
// order against each other. A writer that stamps `time.Now()` instead is a writer whose entries cannot be
// lined up with the rest of the log on a machine whose clock is wrong — which is the situation an operator
// is reading the log view because of.
func (f *fixture) assertStamped(t *testing.T, fragment string, instant time.Time) {
	t.Helper()

	found := 0

	for _, entry := range f.logs.Recorded() {
		if !strings.Contains(entry.Message, fragment) {
			continue
		}

		found++

		if !entry.DateTime.Equal(instant) {
			t.Errorf("the entry %q is stamped %v, want the instant the server's clock holds (%v)",
				entry.Message, entry.DateTime, instant)
		}
	}

	if found == 0 {
		t.Fatalf("the server log = %q, want an entry mentioning %s", f.logText(), fragment)
	}
}

// loggedAs reports whether the hub wrote a log line naming that part of the server and saying the
// fragment. A join, a departure and a refused message are all about feeding listeners, so the source is
// part of what these tests pin.
func (f *fixture) loggedAs(source domain.LogSource, fragment string) bool {
	for _, entry := range f.logs.Recorded() {
		if entry.Source == source && strings.Contains(entry.Message, fragment) {
			return true
		}
	}

	return false
}

// countLogged counts the entries saying exactly that sentence. It is how a test says "once each": a
// departure filed by the hub and then filed again by the read loop it ended would read as two listeners
// leaving, and an operator counting listeners from the log would be told a number that never existed.
func (f *fixture) countLogged(sentence string) int {
	count := 0

	for _, entry := range f.logs.Recorded() {
		if entry.Message == sentence {
			count++
		}
	}

	return count
}

// loggedAt reports whether the hub filed the entry under the level an operator filters for. A join is a
// notice and a frame the server could not act on is a problem, and the log view's level filter is how an
// operator finds one without reading the other.
func (f *fixture) loggedAt(source domain.LogSource, level string, fragment string) bool {
	for _, entry := range f.logs.Recorded() {
		if entry.Source == source && entry.Level == level && strings.Contains(entry.Message, fragment) {
			return true
		}
	}

	return false
}

// waitFor waits for something the hub does on its own, and says what it was waiting for when it does
// not happen.
func (f *fixture) waitFor(t *testing.T, until func() bool, want string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		if until() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", want)
}

// waitForDeparture waits until the hub has let go of the session and has written the entry saying so. The
// entry is written after the session has been taken out of the hub, so a test that waited only for the count
// could read the log before the entry it goes on to assert on had been written at all — which is how a test
// fails on a loaded machine over an entry that arrived a moment later.
func (f *fixture) waitForDeparture(t *testing.T, fragment string) {
	t.Helper()

	f.waitFor(t, func() bool {
		return f.hub.ListenerCount() == 0 && f.loggedAs(domain.LogSourceLivefeed, fragment)
	}, "the departure to be on record: "+fragment)
}

// mutableSettings is the configuration in effect, which a test can change the way an administrator
// does: while the listeners are already connected.
type mutableSettings struct {
	mutex    sync.Mutex
	settings Settings
}

func (s *mutableSettings) Settings() Settings {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.settings
}

func (s *mutableSettings) set(settings Settings) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.settings = settings
}

// recordedChanges counts the times the hub said the number of listeners is no longer what it was, and
// passes the occurrence on to whoever else was told — which is how a test wires the hub to the
// application's debounced report without either of them knowing about the other.
type recordedChanges struct {
	mutex sync.Mutex
	times int
	also  []ListenerChanges
}

func (c *recordedChanges) ListenerChanged() {
	c.mutex.Lock()

	c.times++

	also := append([]ListenerChanges{}, c.also...)

	c.mutex.Unlock()

	for _, next := range also {
		next.ListenerChanged()
	}
}

func (c *recordedChanges) count() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.times
}

// heldLogs is a server log that holds a departure open until the test lets it finish, and announces the
// moment that write begins. It is how a test says "this entry is still on its way" without waiting on a wall
// clock: the entries it holds are the hub's departures, which are the entries a server that is stopping must
// not still be writing when it closes the database the log lives in.
type heldLogs struct {
	*fakes.Logs

	begun   chan domain.LogEntry
	release chan struct{}
}

var _ recordedLogs = (*heldLogs)(nil)

func (logs *heldLogs) AppendLog(ctx context.Context, entry domain.LogEntry) error {
	if strings.Contains(entry.Message, "left") {
		logs.begun <- entry

		<-logs.release
	}

	return logs.Logs.AppendLog(ctx, entry)
}

// recordingDispatch is the server behind the hub: it remembers the frames it was handed and can be
// made to fail.
type recordingDispatch struct {
	mutex  sync.Mutex
	fail   error
	frames []Message
}

func (d *recordingDispatch) handle(ctx context.Context, clientID string, message Message) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.fail != nil {
		return d.fail
	}

	d.frames = append(d.frames, message)

	return nil
}

func (d *recordingDispatch) lastCommand() any {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if len(d.frames) == 0 {
		return nil
	}

	return d.frames[len(d.frames)-1].Command
}

// testSnapshot is a server with two systems, one talkgroup each, and the group and tag they use.
func testSnapshot() ports.ConfigSnapshot {
	return ports.ConfigSnapshot{
		Groups: domain.Groups{{ID: 1, Label: "Fire"}, {ID: 6, Label: "Unknown"}},
		Tags:   domain.Tags{{ID: 10, Label: "Fire Tac"}, {ID: 9, Label: "Untagged"}},
		Systems: domain.Systems{
			{
				ID: 1, SystemRef: 10001, Label: "Grand Metro", Order: 1,
				Talkgroups: domain.Talkgroups{
					{ID: 100, TalkgroupRef: 1514000, Label: "Ground", Name: "Dispatch 1", GroupIDs: []uint64{1}, TagID: 10},
				},
			},
			{
				ID: 2, SystemRef: 10002, Label: "Harbour", Order: 2,
				Talkgroups: domain.Talkgroups{
					{ID: 200, TalkgroupRef: 1515000, Label: "Marine", Name: "Marine 16", GroupIDs: []uint64{6}, TagID: 9},
				},
			},
		},
		Options: domain.Options{Branding: "Capture Dispatch"},
	}
}

// watching is a listener's live-watch selection: one system, with one talkgroup on it switched on.
func watching(system, talkgroup string) domain.Livefeed {
	return domain.ParseLivefeed(map[string]any{system: map[string]any{talkgroup: true}})
}

// testCall is a recorded call on one system and talkgroup, with only what the hub looks at to decide
// who is told about it.
func testCall(systemRef, talkgroupRef uint) domain.Call {
	return domain.Call{
		ID:        1,
		System:    domain.System{SystemRef: systemRef},
		Talkgroup: domain.Talkgroup{TalkgroupRef: talkgroupRef},
		Timestamp: time.Unix(1759752000, 0).UTC(),
	}
}

func jsonOf(t *testing.T, from any) string {
	t.Helper()

	b, err := json.Marshal(from)
	if err != nil {
		t.Fatalf("marshaling %#v: %v", from, err)
	}

	return string(b)
}
