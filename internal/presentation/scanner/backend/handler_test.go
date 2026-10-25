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
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

// trustedLocalhostProxy names the peers these tests connect from as proxies the
// operator named, so the forwarding header these tests carry is honored and the
// address the endpoint names is the address the access warnings are keyed on. The
// list is a fixed literal; a parse failure is a bug in this file.
func trustedLocalhostProxy() *httpaddr.Trusted {
	trusted, err := httpaddr.ParseTrusted("127.0.0.0/8, 192.0.2.0/24")
	if err != nil {
		panic("the test's own proxy list does not parse: " + err.Error())
	}

	return trusted
}

// The version report is answered before anything else, including the request for an
// access code.
func TestTheVersionRequestIsAnsweredBeforeAnyAccessCode(t *testing.T) {
	f := newBareFixture(t, restrictedSettings)

	w := wiredTo(t, f)

	w.restrict(openAccess())

	conn := f.dialEndpoint(t)

	f.send(t, conn, Message{Command: CommandVersion})

	frame := f.expectOn(t, conn)

	if frame.Command != CommandVersion {
		t.Fatalf("the version request was answered with %v, want a version report", frame.Command)
	}

	if got, want := jsonOf(t, frame.Payload), `{"version":"8.0.0"}`; got != want {
		t.Errorf("the version report = %s, want %s", got, want)
	}

	// Being answered does not mean being let in: the very next command is still gated.
	f.send(t, conn, Message{Command: CommandConfig})

	if frame := f.expectOn(t, conn); frame.Command != CommandAccessCode {
		t.Errorf("the command that followed the version report was answered with %v, want a request for the access code", frame.Command)
	}
}

// On a server that requires an access code, every command except the code itself is
// answered with a request for it, including a command the server does not know.
func TestACommandThatNeedsAnAccessCodeIsAnsweredWithARequestForOne(t *testing.T) {
	f := newBareFixture(t, restrictedSettings)

	w := wiredTo(t, f)

	w.restrict(openAccess())

	conn := f.dialEndpoint(t)

	for _, command := range []any{CommandConfig, CommandCallList, CommandCall, CommandLivefeedMap, "not-a-command"} {
		f.send(t, conn, Message{Command: command})

		if frame := f.expectOn(t, conn); frame.Command != CommandAccessCode {
			t.Errorf("%v was answered with %v, want a request for the access code", command, frame.Command)
		}
	}

	if !f.quietOn(t, conn, 200*time.Millisecond) {
		t.Error("a gated command was answered with something besides the request for the access code")
	}
}

// An accepted access code is answered with the configuration that access allows this listener to see,
// and it is the configuration that makes the session a listener.
func TestAnAcceptedAccessCodeBringsThatListenerItsConfiguration(t *testing.T) {
	f := newBareFixture(t, restrictedSettings)

	w := wiredTo(t, f)

	w.restrict(openAccess())

	conn := f.dialEndpoint(t)

	f.send(t, conn, Message{Command: CommandAccessCode, Payload: base64.StdEncoding.EncodeToString([]byte("capture-open"))})

	if frame := f.expectOn(t, conn); frame.Command != CommandConfig {
		t.Fatalf("the accepted code was answered with %v, want the configuration it allows", frame.Command)
	}

	if got := f.hub.ListenerCount(); got != 1 {
		t.Errorf("ListenerCount = %d after the code was accepted, want 1", got)
	}

	// The gate is open now: a command that was refused before the code is processed after it.
	f.send(t, conn, Message{Command: CommandConfig})

	if frame := f.expectOn(t, conn); frame.Command != CommandConfig {
		t.Errorf("the configuration request that followed was answered with %v, want the configuration", frame.Command)
	}
}

// A code the server does not have is answered with another request for the code and
// said out loud in the server log with the address that presented it.
func TestARefusedAccessCodeAsksAgainAndSaysSoInTheServerLog(t *testing.T) {
	f := newBareFixture(t, restrictedSettings)

	w := wiredTo(t, f)

	w.restrict(openAccess())

	conn := f.dialEndpoint(t)

	f.send(t, conn, Message{Command: CommandAccessCode, Payload: base64.StdEncoding.EncodeToString([]byte("guessed"))})

	if frame := f.expectOn(t, conn); frame.Command != CommandAccessCode {
		t.Fatalf("a code the server does not have was answered with %v, want another request for the code", frame.Command)
	}

	f.waitFor(t, func() bool { return len(f.logs.Recorded()) > 0 }, "the server to say something about the refused code")

	if got := f.logAt(t, 0); got != "a listener from 127.0.0.1 presented an access code that is not valid" {
		t.Errorf("the refused code was reported as %q, want the listener and the address it came from", got)
	}

	if got := f.logs.Recorded()[0].Source; got != domain.LogSourceAccess {
		t.Errorf("the refused code was filed under %q, want it filed with the part of the server that admits listeners", got)
	}
}

// A code the server cannot decode is reported to its console and costs the session
// nothing; five good-attempt guesses still leave a good code accepted on the sixth.
func TestAnAccessCodeTheServerCannotDecodeSpendsNoAttempt(t *testing.T) {
	f := newBareFixture(t, restrictedSettings)

	w := wiredTo(t, f)

	w.restrict(openAccess())

	conn := f.dialEndpoint(t)

	for i := 0; i < 5; i++ {
		f.send(t, conn, Message{Command: CommandAccessCode, Payload: "not base64 at all !!"})
	}

	f.waitFor(t, func() bool {
		return f.loggedAs(domain.LogSourceLivefeed, "a message from a listener was not understood: controller.processmessage.commandpin:")
	}, "the server to file the code it could not decode")

	f.send(t, conn, Message{Command: CommandAccessCode, Payload: base64.StdEncoding.EncodeToString([]byte("capture-open"))})

	frame := f.expectOn(t, conn)

	if frame.Command != CommandConfig {
		t.Fatalf("a good code arriving after five undecodable ones was answered with %v, want the configuration: the undecodable ones spent nothing, and the good code is the next thing this connection reads", frame.Command)
	}
}

// An accepted access code clears the attempts this session has spent, even on a
// restricted server.
func TestAnAcceptedAccessCodeClearsTheAttemptsThatSessionHasSpent(t *testing.T) {
	f := newBareFixture(t, restrictedSettings)

	w := wiredTo(t, f)

	w.restrict(openAccess())

	conn := f.dialEndpoint(t)

	f.send(t, conn, Message{Command: CommandAccessCode, Payload: base64.StdEncoding.EncodeToString([]byte("capture-open"))})

	if frame := f.expectOn(t, conn); frame.Command != CommandConfig {
		t.Fatalf("the accepted code was answered with %v, want the configuration it allows", frame.Command)
	}

	for i := 0; i < 4; i++ {
		f.send(t, conn, Message{Command: CommandAccessCode, Payload: base64.StdEncoding.EncodeToString([]byte("guessed"))})

		if frame := f.expectOn(t, conn); frame.Command != CommandAccessCode {
			t.Fatalf("guessed code %d was answered with %v, want another request for the code", i+1, frame.Command)
		}
	}

	// The fifth attempt is the last one allowed, and a good code arriving on it is still
	// refused as a locked session: without the clearing this would be the sixth attempt
	// and the code would never be looked up.
	f.send(t, conn, Message{Command: CommandAccessCode, Payload: base64.StdEncoding.EncodeToString([]byte("capture-open"))})

	if frame := f.expectOn(t, conn); frame.Command != CommandAccessCode {
		t.Fatalf("a good code arriving on the fifth attempt was answered with %v, want a request for the code", frame.Command)
	}

	f.waitFor(t, func() bool {
		return strings.Contains(f.logText(), "the access code for ident Capture open is locked after too many attempts")
	}, "the server to say the session was locked")
}

// A fetch of one recorded call is answered with the call, and carries back the flag the listener asked
// with — whatever that flag was. The server does not decide what a flag means; it hands it back.
func TestAFetchOfOneCallCarriesTheFlagTheListenerSent(t *testing.T) {
	f := newBareFixture(t, openSettings)

	wiredTo(t, f)

	conn := f.dialEndpoint(t)

	f.send(t, conn, Message{Command: CommandConfig})

	if frame := f.expectOn(t, conn); frame.Command != CommandConfig {
		t.Fatalf("the configuration request was answered with %v, want the configuration", frame.Command)
	}

	for _, asked := range []Message{
		{Command: CommandCall, Payload: float64(1)},
		{Command: CommandCall, Payload: "1"},
		{Command: CommandCall, Payload: "1", Flag: "d"},
		{Command: CommandCall, Payload: "1", Flag: "p"},
	} {
		f.send(t, conn, asked)

		frame := f.expectOn(t, conn)

		if frame.Command != CommandCall {
			t.Fatalf("%v was answered with %v, want the call", asked.Payload, frame.Command)
		}

		if got := jsonOf(t, frame.Payload); !strings.Contains(got, `"id":1`) {
			t.Errorf("the call was answered as %s, want the call the listener asked for", got)
		}

		if frame.Flag != asked.Flag {
			t.Errorf("the flag %v came back as %v, want it handed back unchanged", asked.Flag, frame.Flag)
		}
	}
}

// A call the server does not have is reported to the console and answered with nothing,
// and the connection stays open.
func TestAFetchOfOneCallThatDoesNotExistIsReportedAndDoesNotEndTheSession(t *testing.T) {
	f := newBareFixture(t, openSettings)

	wiredTo(t, f)

	conn := f.dialEndpoint(t)

	f.send(t, conn, Message{Command: CommandConfig})

	f.expectOn(t, conn)

	f.send(t, conn, Message{Command: CommandCall, Payload: float64(9999)})

	f.waitFor(t, func() bool {
		return f.loggedAs(domain.LogSourceLivefeed, "a message from a listener was not understood")
	}, "the server to file the call it could not find")

	// The next thing this connection reads is the answer to the next thing it asked for: nothing was
	// sent for the call the server could not find, and the connection is still there to be asked.
	f.send(t, conn, Message{Command: CommandVersion})

	frame := f.expectOn(t, conn)

	if frame.Command != CommandVersion {
		t.Errorf("the frame that followed a call the server could not find is %v, want the version report and nothing in between", frame.Command)
	}
}

// A call the listener asked for is refused when its access does not name it, and refused in silence:
// the listener is not told that the call exists.
func TestAFetchOfOneCallIsRefusedToAListenerWhoseAccessDoesNotGrantIt(t *testing.T) {
	f := newBareFixture(t, restrictedSettings)

	w := wiredTo(t, f)

	scope, _ := domain.ParseAccessScope([]any{map[string]any{"id": float64(10002), "talkgroups": "*"}})

	w.restrict(domain.Access{ID: 1, Code: "capture-scoped", Ident: "Capture scoped", Scope: scope})

	conn := f.dialEndpoint(t)

	f.send(t, conn, Message{Command: CommandAccessCode, Payload: base64.StdEncoding.EncodeToString([]byte("capture-scoped"))})

	if frame := f.expectOn(t, conn); frame.Command != CommandConfig {
		t.Fatalf("the accepted code was answered with %v, want the configuration it allows", frame.Command)
	}

	f.send(t, conn, Message{Command: CommandCall, Payload: float64(1)})

	if !f.quietOn(t, conn, 300*time.Millisecond) {
		t.Error("a listener whose access names only system 10002 was answered with a call on system 10001")
	}
}

// A history search is answered with the page and with the criteria the server
// understood; a criterion it could not use is not echoed.
func TestAHistorySearchIsAnsweredWithThePageAndTheCriteriaTheListenerAskedWith(t *testing.T) {
	f := newBareFixture(t, openSettings)

	wiredTo(t, f)

	conn := f.dialEndpoint(t)

	f.send(t, conn, Message{Command: CommandConfig})

	f.expectOn(t, conn)

	f.send(t, conn, Message{
		Command: CommandCallList,
		Payload: map[string]any{"limit": float64(200), "offset": float64(0), "sort": "date"},
	})

	frame := f.expectOn(t, conn)

	if frame.Command != CommandCallList {
		t.Fatalf("the search was answered with %v, want the page", frame.Command)
	}

	page, ok := frame.Payload.(map[string]any)

	if !ok {
		t.Fatalf("the search answer = %#v, want an object", frame.Payload)
	}

	if page["count"] != float64(3) {
		t.Errorf("the search reports %v calls, want the three the server has", page["count"])
	}

	options, _ := page["options"].(map[string]any)

	if options["limit"] != float64(200) || options["offset"] != float64(0) {
		t.Errorf("the criteria echoed back = %v, want the paging the listener asked with", options)
	}

	if _, sent := options["sort"]; sent {
		t.Errorf("the criteria echoed back %v, want a sort the server could not read to be left out", options["sort"])
	}
}

// A live-watch selection arrives through the endpoint, is stored on the session, and is answered with
// whether anything is now switched on.
func TestALiveWatchSelectionIsAnsweredThroughTheEndpoint(t *testing.T) {
	f := newBareFixture(t, openSettings)

	wiredTo(t, f)

	conn := f.dialEndpoint(t)

	f.send(t, conn, Message{Command: CommandConfig})

	f.expectOn(t, conn)

	f.send(t, conn, Message{Command: CommandLivefeedMap, Payload: map[string]any{"10001": map[string]any{"1514000": true}}})

	frame := f.expectOn(t, conn)

	if frame.Command != CommandLivefeedMap {
		t.Fatalf("the selection was answered with %v, want the acknowledgement", frame.Command)
	}

	if on, ok := frame.Payload.(bool); !ok || !on {
		t.Errorf("the acknowledgement = %v, want true", frame.Payload)
	}

	f.hub.BroadcastCall(wiredCall(4, 10001, 1514000))

	if frame := f.expectOn(t, conn); frame.Command != CommandCall {
		t.Errorf("the listener that switched on this talkgroup received %v, want the call", frame.Command)
	}
}

// A command the server has no use for is answered with nothing, and is not an error.
// The next frame's answer arriving proves this one was answered with nothing.
func TestACommandTheServerDoesNotActOnIsAnsweredWithNothing(t *testing.T) {
	f := newBareFixture(t, openSettings)

	wiredTo(t, f)

	conn := f.dialEndpoint(t)

	f.send(t, conn, Message{Command: CommandConfig})

	f.expectOn(t, conn)

	for _, command := range []any{CommandIOS, "not-a-command"} {
		f.send(t, conn, Message{Command: command})

		// The answer to the next thing this connection asks for arrives next, which is what proves the
		// command before it was answered with nothing.
		f.send(t, conn, Message{Command: CommandVersion})

		if frame := f.expectOn(t, conn); frame.Command != CommandVersion {
			t.Errorf("%v was answered with %v, want nothing", command, frame.Command)
		}
	}

	if f.loggedAs(domain.LogSourceLivefeed, "a message from a listener was not understood") {
		t.Errorf("logs = %v, want an unknown command to be answered with nothing and filed nowhere", f.logs.Recorded())
	}
}

// TestAConnectionTheServerWouldNotTakeOverIsFiled: a connection that never became a
// listener still leaves an entry in the log view; without it a listener problem and a
// broken client look identical.
func TestAConnectionTheServerWouldNotTakeOverIsFiled(t *testing.T) {
	f := newBareFixture(t, openSettings)

	wiredTo(t, f)

	resp, err := http.Get(f.server.URL + "/")
	if err != nil {
		t.Fatalf("reaching the listener endpoint without asking for an upgrade: %v", err)
	}

	defer resp.Body.Close()

	if !f.loggedAt(domain.LogSourceLivefeed, domain.LogLevelWarn, "a listener connection was refused") {
		t.Fatalf("logs = %v, want the connection the server would not take over, filed as a problem under the "+
			"part of the server it reached", f.logs.Recorded())
	}

	// Stamped with the server's clock, like every other entry: this is the endpoint's own writer, and it had
	// no clock injected into it.
	f.assertStamped(t, "a listener connection was refused", wiredClockTime)
}

// The address the server names for a listener is the forwarding-header address without
// its port when the connection came from the named proxy, and the connection address
// otherwise: the same address the guessed-code warnings name, so two log lines can be
// read as about the same machine.
func TestTheAddressTheServerNamesIsTheForwardedOneWithoutItsPort(t *testing.T) {
	f := newBareFixture(t, openSettings)

	wiredTo(t, f)

	conn := f.openWith(t, http.Header{"X-Forwarded-For": []string{"198.51.100.9:1234, 203.0.113.7"}})

	f.send(t, conn, Message{Command: CommandConfig})

	f.expectOn(t, conn)

	f.waitFor(t, func() bool { return len(f.logs.Recorded()) > 0 }, "the join notice")

	if got := f.logAt(t, 0); got != "a listener joined from 198.51.100.9" {
		t.Errorf("the join notice = %q, want the forwarded address without its port", got)
	}

	if got := f.logs.Recorded()[0].Source; got != domain.LogSourceLivefeed {
		t.Errorf("the join notice was filed under %q, want it filed with the part of the server that feeds listeners", got)
	}
}

// TestAListenerSessionOnAnOpenServerFollowsTheCapturedSession replays the captured
// legacy session in capture order and compares this server's answers with the frames
// the legacy server sent.
func TestAListenerSessionOnAnOpenServerFollowsTheCapturedSession(t *testing.T) {
	f := newBareFixture(t, openSettings)

	w := wiredTo(t, f)

	conn := f.dialEndpoint(t)

	got := []string{}

	ask := func(frame Message) {
		t.Helper()

		f.send(t, conn, frame)

		got = append(got, sentCommandOf(t, f.expectOn(t, conn)))
	}

	ask(Message{Command: CommandVersion})
	ask(Message{Command: CommandConfig})
	ask(Message{Command: CommandLivefeedMap, Payload: map[string]any{"10001": map[string]any{"1514000": true}}})

	// The listeners count is not an answer to anything the listener asked: it arrives on its own, a
	// held-back report of the fact that this session became a listener.
	w.scheduler.RunDue(application.ListenerCountReportDelay)

	got = append(got, sentCommandOf(t, f.expectOn(t, conn)))

	f.hub.BroadcastCall(w.calls.Stored[4])

	got = append(got, sentCommandOf(t, f.expectOn(t, conn)))

	ask(Message{Command: CommandCallList, Payload: map[string]any{"limit": float64(200), "offset": float64(0), "sort": "date"}})
	ask(Message{Command: CommandCall, Payload: "4", Flag: "p"})
	ask(Message{Command: CommandCall, Payload: "1", Flag: "d"})

	want := capturedSessionCommands(t)

	if len(got) != len(want) {
		t.Fatalf("the session answered %d frames, the captured session received %d: got %v, want %v", len(got), len(want), got, want)
	}

	for i, command := range want {
		if got[i] != command {
			t.Errorf("frame %d = %v, want %v: the captured session is %v", i, got[i], command, want)
		}
	}
}

// wiredClockTime is the instant every clock a wired fixture injects holds, so that a test can tell an entry
// the server stamped from one a writer stamped with the wall clock.
var wiredClockTime = time.Unix(1759752000, 0)

// wiring is the listener endpoint with the use cases it answers with, the stand-ins those use cases
// read, and the connection a test is talking on.
type wiring struct {
	f *fixture

	accesses    *fakes.Accesses
	apikeys     *fakes.Apikeys
	calls       *fakes.Calls
	dirwatches  *fakes.Dirwatches
	downstreams *fakes.Downstreams
	groups      *fakes.Groups
	options     *fakes.Options
	scheduler   *fakes.Scheduler
	systems     *fakes.Systems
	tags        *fakes.Tags

	conn *websocket.Conn
}

// wiredTo builds the endpoint over the fixture's hub and stand-ins and points the fixture's endpoint at
// it, so that the next connection a fixture opens is a session on a whole server.
func wiredTo(t *testing.T, f *fixture) *wiring {
	t.Helper()

	snapshot := testSnapshot()

	snapshot.Options.ShowListenersCount = true

	w := &wiring{
		f:           f,
		accesses:    &fakes.Accesses{},
		apikeys:     &fakes.Apikeys{},
		calls:       fakes.NewCalls(),
		dirwatches:  &fakes.Dirwatches{},
		downstreams: &fakes.Downstreams{},
		groups:      &fakes.Groups{Stored: snapshot.Groups},
		options:     &fakes.Options{Stored: snapshot.Options},
		scheduler:   fakes.NewScheduler(),
		systems:     &fakes.Systems{Stored: snapshot.Systems},
		tags:        &fakes.Tags{Stored: snapshot.Tags},
	}

	w.calls.Stored[1] = wiredCall(1, 10001, 1514000)
	w.calls.Stored[2] = wiredCall(2, 10002, 1515000)
	w.calls.Stored[4] = wiredCall(4, 10001, 1514000)

	debouncer := application.NewListenerCountDebouncer(f.hub, f.logs, w.options, w.scheduler, fakes.NewClock(wiredClockTime))

	f.changes.also = []ListenerChanges{debouncer}

	handler := NewHandler(Deps{
		Access: application.NewAuthenticateAccessHandler(
			w.accesses, w.systems, w.groups, w.tags, w.options, f.logs, f.hub, fakes.NewClock(wiredClockTime),
		),
		Call:  application.NewGetCallQueryHandler(w.calls),
		Clock: fakes.NewClock(wiredClockTime),
		Config: application.NewGetConfigHandler(application.ConfigurationRepositories{
			Accesses:    w.accesses,
			Apikeys:     w.apikeys,
			Dirwatches:  w.dirwatches,
			Downstreams: w.downstreams,
			Groups:      w.groups,
			Options:     w.options,
			Systems:     w.systems,
			Tags:        w.tags,
		}),
		Gate:     application.NewAccessGate(w.accesses, f.hub),
		Hub:      f.hub,
		Livefeed: application.NewSetLivefeedHandler(f.hub),
		Logs:     f.logs,
		Search:   application.NewSearchCallsQueryHandler(w.calls, w.systems, w.groups, w.tags),
		Settings: f.settings,
		Version:  "8.0.0",
		Trusted:  trustedLocalhostProxy(),
	})

	// A session on a whole server needs a longer window than the hub tests': these tests
	// reach the first configuration by asking for it over the connection.
	f.hub.handshakeWait = 10 * time.Second

	f.endpoint = handler.ServeHTTP

	return w
}

// restrict gives the server the access list that makes it require an access code: a
// server with no access list does not ask for one, and the settings the hub is given
// have to agree with it.
func (w *wiring) restrict(accesses ...domain.Access) {
	w.accesses.Stored = accesses
}

// openAccess is an access that names everything, which is the code these tests hand to a listener that
// just needs to be let in.
func openAccess() domain.Access {
	scope, _ := domain.ParseAccessScope("*")

	return domain.Access{ID: 1, Code: "capture-open", Ident: "Capture open", Scope: scope}
}

// wiredCall is one recorded call, with only what the answers about it are built from.
func wiredCall(id uint64, systemRef, talkgroupRef uint) domain.Call {
	return domain.Call{
		ID:        id,
		System:    domain.System{SystemRef: systemRef},
		Talkgroup: domain.Talkgroup{TalkgroupRef: talkgroupRef},
		Timestamp: time.Unix(1759752000+int64(id)*60, 0).UTC(),
	}
}

// sentCommandOf names the command of a frame the server sent.
func sentCommandOf(t *testing.T, frame Message) string {
	t.Helper()

	command, ok := frame.Command.(string)
	if !ok {
		t.Fatalf("the server sent a frame whose command is %#v, want a command name", frame.Command)
	}

	return command
}

// capturedSessionCommands are the commands the legacy server sent to the first listener of the captured
// open session, in the order it sent them, up to the point this test replays.
func capturedSessionCommands(t *testing.T) []string {
	t.Helper()

	frames := capturedFrames("ws-frames-open.json", "listener")

	commands := []string{}

	for _, frame := range frames {
		parts := []any{}

		if err := json.Unmarshal([]byte(frame), &parts); err != nil {
			t.Fatalf("reading a captured frame: %v", err)
		}

		if len(parts) == 0 {
			t.Fatalf("the captured session holds a frame with no command: %s", frame)
		}

		command, ok := parts[0].(string)
		if !ok {
			t.Fatalf("the captured session holds a frame whose command is %#v", parts[0])
		}

		commands = append(commands, command)
	}

	// The captured session goes on from here with a second listener and two configuration changes,
	// which belong to the hub's own tests rather than to this one.
	return commands[:8]
}

// TestTheListenerSocketRefusesAWebPageFromAnotherOrigin: a browser page from anywhere
// other than the server's own address opening this socket is a cross-site request that
// would read every live call; a connection with no Origin (a native application)
// passes, and so does the hosted scanner webapp.
func TestTheListenerSocketRefusesAWebPageFromAnotherOrigin(t *testing.T) {
	f := newBareFixture(t, openSettings)

	wiredTo(t, f)

	address := "ws://" + strings.TrimPrefix(f.server.URL, "http://")
	host := strings.TrimPrefix(f.server.URL, "http://")

	cross, resp, err := websocket.DefaultDialer.Dial(address, http.Header{
		"Origin": []string{"https://evil.example.com"},
	})
	if err == nil {
		cross.Close()

		t.Fatal("a cross-origin upgrade was accepted, want it refused")
	}

	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("the cross-origin refusal = %v, want 403", resp)
	}

	native, _, err := websocket.DefaultDialer.Dial(address, nil)
	if err != nil {
		t.Fatalf("a connection with no Origin was refused: %v — native applications send no Origin", err)
	}

	native.Close()

	same, _, err := websocket.DefaultDialer.Dial(address, http.Header{
		"Origin": []string{"http://" + host},
	})
	if err != nil {
		t.Fatalf("a same-origin connection was refused: %v — the hosted scanner webapp connects from this very address", err)
	}

	same.Close()
}
