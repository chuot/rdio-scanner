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

package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
	"rdio-scanner/server-ng/internal/presentation/dto"
)

// The intervals a listener connection is held to. They are the protocol's own and are not choices
// this server makes: a client is written to expect a ping nine tenths of the way through the window in
// which it must answer, and a server that asks on a different schedule is a server its clients have to
// be written around.
const (
	// pongWait is how long a connection may go without answering before it is given up on.
	pongWait = 60 * time.Second

	// pingPeriod is how often the server asks, which leaves a listener more than one chance to answer
	// before its window closes.
	pingPeriod = pongWait / 10 * 9

	// writeWait is how long the server waits for one frame to leave.
	writeWait = 10 * time.Second

	// sendBuffer is how many frames may be waiting for one listener before the server stops and waits
	// for it to catch up. A listener that cannot keep up holds up the delivery that is going to it,
	// which is what the legacy server does and what a slow client therefore cannot silently cost a
	// server its calls.
	sendBuffer = 8192
)

// Settings are the things about the server a hub has to know before it delivers anything to a
// listener: whether listeners must present an access code, how many of them the server admits at once,
// and the branding and contact address the server reports about itself.
type Settings struct {
	Branding   string
	Email      string
	MaxClients uint
	Restricted bool
}

// ServerSettings answers what the server is currently configured to do about listeners. It is asked at
// the moment of the question rather than remembered by the hub, because the two things it answers can
// change while listeners are already connected — and the listeners already connected are the ones the
// change affects. Adding the first access code asks every listener that is already watching for its
// code; lowering the client limit decides who is admitted from then on.
type ServerSettings interface {
	Settings() Settings
}

// ListenerChanges is what the hub says when a listener joins or leaves: not who joined or left, only
// that the number of listeners is no longer what it was. Whoever is told answers that occurrence on
// its own schedule, because a client that connects and drops repeatedly should cost one report and not
// one per connection.
type ListenerChanges interface {
	ListenerChanged()
}

// Dispatch hands one frame to the server that owns this hub, and reports a failure the hub writes to
// its console and then ignores. A frame the server cannot act on is not a reason to end the connection
// it arrived on: a client that sends one frame the server does not know is still a client, and the
// native applications send commands older servers do not have.
type Dispatch func(clientID string, message Message) error

// Hub is the set of listener connections the server is holding, and the thing the application talks
// to when it has something to tell a listener.
//
// A session and a listener are not the same thing. A session is a connection the server has taken over;
// a listener is a session it has told what it is watching, which happens the moment a configuration is
// written on the connection. Only listeners are counted, only listeners are sent calls, and only
// listeners are against the client limit — which is why a restricted server, which writes nothing until
// an access code is accepted, can hold a room full of connections and report no listeners at all.
type Hub struct {
	settings ServerSettings
	changes  ListenerChanges
	logs     ports.LogRepository
	console  io.Writer

	mutex    sync.Mutex
	sessions map[string]*listener
	listens  map[string]*listener
	nextID   uint64

	// The intervals above are what every hub starts with. They are fields rather than constants used
	// inline so that this package's own tests can hold a connection to a shorter version of them: a
	// test that checks a dropped connection has to wait for one, and no server should be able to pick
	// a value that breaks the protocol instead.
	handshakeWait time.Duration
	pongWait      time.Duration
	pingPeriod    time.Duration
	writeWait     time.Duration
}

var _ ports.Hub = (*Hub)(nil)

// NewHub builds the hub from what it needs to know about the server, who it tells about listeners,
// where its log goes, and where it writes the lines it says about a connection — which are not the
// server's log and do not go into it.
func NewHub(settings ServerSettings, changes ListenerChanges, logs ports.LogRepository, console io.Writer) *Hub {
	if console == nil {
		console = io.Discard
	}

	return &Hub{
		changes:       changes,
		console:       console,
		handshakeWait: pongWait,
		listens:       map[string]*listener{},
		logs:          logs,
		nextID:        0,
		pingPeriod:    pingPeriod,
		pongWait:      pongWait,
		sessions:      map[string]*listener{},
		settings:      settings,
		writeWait:     writeWait,
	}
}

// Attach takes over one upgraded listener connection and returns the identifier this session is known
// by. It reports false when the server already has as many listeners as it allows, in which case the
// connection is closed and nothing is said on it — a client that is refused because the server is full
// is refused in silence, which is what today's clients see.
//
// The connection is not yet a listener. It becomes one when a configuration is written on it.
func (hub *Hub) Attach(conn *websocket.Conn, remoteAddr string, dispatch Dispatch) (string, bool) {
	if conn == nil {
		return "", false
	}

	max := hub.settings.Settings().MaxClients

	hub.mutex.Lock()

	if uint(len(hub.listens)) >= max {
		hub.mutex.Unlock()

		conn.Close()

		return "", false
	}

	hub.nextID++

	id := strconv.FormatUint(hub.nextID, 10)
	session := newListener(hub, id, conn, remoteAddr)

	hub.sessions[id] = session

	hub.mutex.Unlock()

	go session.write()
	go session.read(dispatch)

	return id, true
}

// BroadcastCall delivers a recorded call to every listener it is for.
//
// Two things have to say so. The listener's live-watch selection has to ask for that system or
// talkgroup, and — on a server that requires access codes — the listener's access has to grant the call.
// On a server that requires nothing, the access is not consulted at all: that is how an anonymous
// listener on an open server sees every call the server records, and it is why an access that names no
// systems is not treated as a grant of nothing on such a server.
func (hub *Hub) BroadcastCall(call domain.Call) {
	settings := hub.settings.Settings()

	hub.mutex.Lock()

	targets := make([]*listener, 0, len(hub.listens))

	for _, session := range hub.listens {
		if settings.Restricted && !session.access.Grants(call) {
			continue
		}

		if !session.livefeed.Watches(call) {
			continue
		}

		targets = append(targets, session)
	}

	hub.mutex.Unlock()

	for _, session := range targets {
		hub.deliver(session, delivery{frame: Message{Command: CommandCall, Payload: dto.CallPayload{Call: call}}})
	}
}

// SendConfig delivers the configuration one listener is allowed to see. This is the frame that makes a
// session a listener, so it is also the moment the server starts counting it.
func (hub *Hub) SendConfig(clientID string, cfg ports.ConfigSnapshot) {
	hub.mutex.Lock()

	session, ok := hub.sessions[clientID]

	var out delivery

	if ok {
		out = configDelivery(session, cfg)
	}

	hub.mutex.Unlock()

	if ok {
		hub.deliver(session, out)
	}
}

// BroadcastConfig delivers the configuration to every listener, each scoped to what that listener is
// allowed to see.
//
// On a server that requires access codes it is not delivered as a configuration at all: every listener
// is asked for its code again, because the change that prompted it may be the access that decides what
// that listener may see, and a server that re-sent the old view would be showing a listener something
// it may no longer be entitled to.
func (hub *Hub) BroadcastConfig(cfg ports.ConfigSnapshot) {
	settings := hub.settings.Settings()

	hub.mutex.Lock()

	count := len(hub.listens)

	targets := make([]queued, 0, count)

	for _, session := range hub.listens {
		frames := []Message{}

		if settings.Restricted {
			frames = append(frames, Message{Command: CommandAccessCode})
		} else {
			frames = append(frames, Message{Command: CommandConfig, Payload: dto.ConfigPayload{Snapshot: cfg, Scope: session.access.Scope}})
		}

		if cfg.Options.ShowListenersCount {
			frames = append(frames, Message{Command: CommandListenersCount, Payload: count})
		}

		targets = append(targets, queued{frames: frames, session: session})
	}

	hub.mutex.Unlock()

	for _, target := range targets {
		for _, frame := range target.frames {
			hub.deliver(target.session, delivery{frame: frame})
		}
	}
}

// SetLivefeed replaces a session's live-watch selection.
func (hub *Hub) SetLivefeed(clientID string, livefeed domain.Livefeed) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	if session, ok := hub.sessions[clientID]; ok {
		session.livefeed = livefeed
	}
}

// LivefeedWatchesNothing reports whether a session has nothing switched on.
func (hub *Hub) LivefeedWatchesNothing(clientID string) bool {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	session, ok := hub.sessions[clientID]
	if !ok {
		return true
	}

	return session.livefeed.AllOff()
}

// SendLivefeedMap answers a listener's live-watch selection with whether anything is now switched on.
// The answer is the only thing the listener is told, and it is told even when it is no.
func (hub *Hub) SendLivefeedMap(clientID string, watching bool) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	if session, ok := hub.sessions[clientID]; ok {
		hub.deliver(session, delivery{frame: Message{Command: CommandLivefeedMap, Payload: watching}})
	}
}

// RequestAccessCode asks a listener for its access code.
func (hub *Hub) RequestAccessCode(clientID string) {
	hub.send(clientID, Message{Command: CommandAccessCode})
}

// ReportAccessExpired tells a listener that its access has run out.
func (hub *Hub) ReportAccessExpired(clientID string) {
	hub.send(clientID, Message{Command: CommandAccessExpired})
}

// ReportAccessLimit tells a listener that its identity already has as many sessions open as it is
// allowed.
func (hub *Hub) ReportAccessLimit(clientID string) {
	hub.send(clientID, Message{Command: CommandAccessLimit})
}

// SendVersion reports what server this is. The branding and the contact address are the ones the server
// has been given, and are left out when it has not been given them.
func (hub *Hub) SendVersion(clientID string, version string) {
	settings := hub.settings.Settings()

	payload := map[string]any{"version": version}

	if len(settings.Branding) > 0 {
		payload["branding"] = settings.Branding
	}

	if len(settings.Email) > 0 {
		payload["email"] = settings.Email
	}

	hub.send(clientID, Message{Command: CommandVersion, Payload: payload})
}

// BroadcastListenersCount reports how many listeners are watching, to every listener.
func (hub *Hub) BroadcastListenersCount(count int) {
	hub.mutex.Lock()

	targets := make([]*listener, 0, len(hub.listens))

	for _, session := range hub.listens {
		targets = append(targets, session)
	}

	hub.mutex.Unlock()

	for _, session := range targets {
		hub.deliver(session, delivery{frame: Message{Command: CommandListenersCount, Payload: count}})
	}
}

// CloseClient ends a listener session, with the close code the listener sees.
func (hub *Hub) CloseClient(clientID string, code int) {
	hub.mutex.Lock()

	session, ok := hub.sessions[clientID]

	hub.mutex.Unlock()

	if ok {
		hub.deliver(session, delivery{code: code})
	}
}

// GrantAccess attaches an access to a session, which is what makes that session authenticated. It is
// kept even when the attempt that presented it is then refused for a reason that does not throw the
// access away — an expired access, or one that is already at its connection limit — which is why the
// server can name the identity a refused session was trying to use when it disconnects.
func (hub *Hub) GrantAccess(clientID string, access domain.Access) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	if session, ok := hub.sessions[clientID]; ok {
		session.access = access
		session.granted = true
	}
}

// AccessOf returns the access a session holds, if it has been given one. A session that has not
// presented an access code holds nothing: it is not unauthenticated by way of an empty access, it simply
// has not been given one.
func (hub *Hub) AccessOf(clientID string) (domain.Access, bool) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	session, ok := hub.sessions[clientID]
	if !ok || !session.granted {
		return domain.Access{}, false
	}

	return session.access, true
}

// RecordAccessAttempt counts one access-code attempt for a session and reports which attempt number
// that was.
func (hub *Hub) RecordAccessAttempt(clientID string) uint {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	session, ok := hub.sessions[clientID]
	if !ok {
		return 1
	}

	session.attempts++

	return session.attempts
}

// ResetAccessAttempts clears a session's attempt counter, which happens only when an access code is
// accepted.
func (hub *Hub) ResetAccessAttempts(clientID string) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	if session, ok := hub.sessions[clientID]; ok {
		session.attempts = 0
	}
}

// AccessSessionCount reports how many listeners are watching under one access. A session that is open
// but has not been told what it is watching is not watching, and is not counted — which is what makes a
// limit of one allow two connections and refuse the third.
func (hub *Hub) AccessSessionCount(accessID uint64) uint {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	count := uint(0)

	for _, session := range hub.listens {
		if session.access.ID == accessID {
			count++
		}
	}

	return count
}

// ListenerCount reports how many listeners the server is counting.
func (hub *Hub) ListenerCount() int {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	return len(hub.listens)
}

// RemoteAddrOf reports where a session connected from, which is what the server names in its warnings
// about refused access codes.
func (hub *Hub) RemoteAddrOf(clientID string) string {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	session, ok := hub.sessions[clientID]
	if !ok {
		return ""
	}

	return session.remoteAddr
}

// send hands one frame to one session.
func (hub *Hub) send(clientID string, frame Message) {
	hub.mutex.Lock()

	session, ok := hub.sessions[clientID]

	hub.mutex.Unlock()

	if ok {
		hub.deliver(session, delivery{frame: frame})
	}
}

// configDelivery is the frame that tells one session what it is allowed to see. It reaches into the
// access that session holds, so it is only called with the hub lock held.
func configDelivery(session *listener, cfg ports.ConfigSnapshot) delivery {
	return delivery{
		frame: Message{Command: CommandConfig, Payload: dto.ConfigPayload{Snapshot: cfg, Scope: session.access.Scope}},
	}
}

// deliver hands one thing to a session's writer.
//
// A session whose writer has stopped is skipped, and a send that has to wait for room stops waiting the
// moment that writer stops, so a connection that has already gone cannot hold the server. A listener
// that is merely slow does hold it: the frame waits for room in that listener's queue, which is what the
// legacy server does and what means a server does not quietly throw calls away because one client cannot
// keep up.
//
// The hub lock is deliberately not held while this waits. The legacy server holds its clients lock
// across the whole delivery, and a listener that cannot keep up therefore blocks the server that is
// trying to reach it; here the sessions are chosen under the lock and delivered outside it, so a slow
// listener cannot stop the server from doing the one thing it would need the lock for — finishing the
// registration of a session whose queue is full.
func (hub *Hub) deliver(session *listener, out delivery) {
	if session.stopped.Load() {
		return
	}

	select {
	case session.send <- out:

	case <-session.quit:
	}
}

// register makes a session a listener. It happens once, when the first configuration is written, and it
// is what puts the session in the count every listener is told.
func (hub *Hub) register(session *listener) {
	hub.mutex.Lock()

	if _, counted := hub.listens[session.id]; counted {
		hub.mutex.Unlock()

		return
	}

	hub.listens[session.id] = session

	hub.mutex.Unlock()

	hub.changes.ListenerChanged()

	if len(session.access.Ident) > 0 {
		hub.logf("new listener from ip %s with ident %s", session.remoteAddr, session.access.Ident)

		return
	}

	hub.logf("new listener from ip %s", session.remoteAddr)
}

// disconnect forgets a session and says so. Every connection the server has taken over is reported when
// it goes, including one that never became a listener, because a connection that arrived and left is
// something the person running the server can see happening.
func (hub *Hub) disconnect(session *listener) {
	hub.mutex.Lock()

	_, was := hub.sessions[session.id]
	delete(hub.sessions, session.id)
	delete(hub.listens, session.id)

	hub.mutex.Unlock()

	if !was {
		return
	}

	// Every connection the server has taken over is reported when it goes, even one that never became
	// a listener: the legacy server reports the departure of a connection it was holding, and the
	// listeners count is re-reported five seconds later either way.
	hub.changes.ListenerChanged()

	if len(session.access.Ident) > 0 {
		hub.logf("listener disconnected from ip %s with ident %s", session.remoteAddr, session.access.Ident)

		return
	}

	hub.logf("listener disconnected from ip %s", session.remoteAddr)
}

// logf writes one line to the server log. These lines are the server's log and not its console: they
// are what an administrator reads afterwards, and they name the address a listener connected from.
func (hub *Hub) logf(format string, args ...any) {
	if hub.logs == nil {
		return
	}

	// A join or a departure is not the answer to anybody's request, so it belongs to no request's
	// lifetime and is not cancelled when one ends.
	hub.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: time.Now(),
		Level:    domain.LogLevelInfo,
		Message:  fmt.Sprintf(format, args...),
	})
}

// delivery is one thing a session's writer is asked to do: write a frame, or end the connection with a
// close code.
type delivery struct {
	frame Message
	code  int
}

// queued is what a broadcast has to say to one session. The frames are chosen while the hub lock is
// held, because choosing them means reading what that session is allowed to see, and they are written
// after the lock is given back.
type queued struct {
	frames  []Message
	session *listener
}

// listener is one session the server has taken over.
type listener struct {
	hub        *Hub
	id         string
	conn       *websocket.Conn
	remoteAddr string
	send       chan delivery

	// quit is closed when the writer stops. It is not the channel frames travel on: it is how a send
	// that is waiting for room finds out that there is nobody left to give it to.
	quit    chan struct{}
	stopped atomic.Bool

	// handshake is the window a connection has to become a listener. It is stopped by the first
	// configuration written on the connection, and when it is not stopped the connection is ended —
	// which is what drops a connection on a restricted server whose access code never arrives.
	handshake *time.Timer

	// The per-session state is guarded by the hub lock, because the hub reaches it while it is
	// delivering to every listener.
	access   domain.Access
	granted  bool
	attempts uint
	livefeed domain.Livefeed

	// registered belongs to the writer alone: it is the writer that notices the first configuration it
	// is being asked to write, and it is the only thing that ever looks at this.
	registered bool
}

func newListener(hub *Hub, id string, conn *websocket.Conn, remoteAddr string) *listener {
	return &listener{
		handshake:  time.AfterFunc(hub.handshakeWait, func() { conn.Close() }),
		hub:        hub,
		id:         id,
		conn:       conn,
		livefeed:   domain.NewLivefeed(),
		quit:       make(chan struct{}),
		remoteAddr: remoteAddr,
		send:       make(chan delivery, sendBuffer),
	}
}

// read takes the frames a session sends, in the order they arrive, and hands each one to the server
// behind the hub.
//
// A frame the server cannot read, or cannot act on, is written to the console and skipped: the
// connection stays open and the next frame is read. Ending a connection over one bad frame is how a
// server loses a client that is sending something the server is too old to know.
func (session *listener) read(dispatch Dispatch) {
	defer func() {
		session.hub.disconnect(session)

		session.conn.Close()
	}()

	if err := session.conn.SetReadDeadline(time.Now().Add(session.hub.pongWait)); err != nil {
		return
	}

	session.conn.SetPongHandler(func(string) error {
		return session.conn.SetReadDeadline(time.Now().Add(session.hub.pongWait))
	})

	for {
		_, b, err := session.conn.ReadMessage()
		if err != nil {
			return
		}

		message, err := ReadMessage(b)
		if err != nil {
			fmt.Fprintf(session.hub.console, "client.message.fromjson: %v\n", err)

			continue
		}

		if err := dispatch(session.id, message); err != nil {
			fmt.Fprintf(session.hub.console, "client.processmessage: %v\n", err)
		}
	}
}

// write is the only thing that ever writes on the connection, because a websocket connection has one
// writer and a frame written from somewhere else would be interleaved with one already on its way.
func (session *listener) write() {
	ticker := time.NewTicker(session.hub.pingPeriod)

	defer func() {
		ticker.Stop()

		session.handshake.Stop()

		session.stopped.Store(true)
		close(session.quit)

		session.conn.Close()
	}()

	for {
		select {
		case out := <-session.send:
			if out.code != 0 {
				if err := session.conn.SetWriteDeadline(time.Now().Add(session.hub.writeWait)); err != nil {
					return
				}

				if err := session.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(out.code, "")); err != nil {
					return
				}

				return
			}

			// The first configuration is what makes this session a listener, and it is also what ends
			// the window a connection has to become one.
			if out.frame.Command == CommandConfig && !session.registered {
				session.registered = true

				session.handshake.Stop()

				session.hub.register(session)
			}

			b, err := json.Marshal(out.frame)
			if err != nil {
				fmt.Fprintf(session.hub.console, "client.message.tojson: %v\n", err)

				continue
			}

			if err := session.conn.SetWriteDeadline(time.Now().Add(session.hub.writeWait)); err != nil {
				return
			}

			if err := session.conn.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}

		case <-ticker.C:
			if err := session.conn.SetWriteDeadline(time.Now().Add(session.hub.writeWait)); err != nil {
				return
			}

			if err := session.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
