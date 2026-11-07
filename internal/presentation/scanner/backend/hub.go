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
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/dto"
)

// The keepalive intervals a listener connection is held to. They are the
// protocol's own values, not choices this server makes: a client is
// written to expect this schedule.
const (
	// pongWait is how long a connection may go without answering before it is given up on.
	pongWait = 60 * time.Second

	// pingPeriod is how often the server asks, which leaves a listener more than one chance to answer
	// before its window closes.
	pingPeriod = pongWait / 10 * 9

	// writeWait is how long the server waits for one frame to leave.
	writeWait = 10 * time.Second

	// maxFrameSize is the largest frame the server reads from one listener;
	// a bigger frame is unusable and reading it spends memory on a message
	// that is never used. The socket ends the connection at the limit.
	maxFrameSize = 1 << 20

	// sendBuffer is how many frames may wait for one listener before the
	// server stops and waits for it to catch up, as the legacy server does.
	// A slow client must not silently lose a server its calls, and a bounded
	// queue keeps waiting frames from spending unbounded server memory.
	sendBuffer = 256

	// accessAttemptsWindow is the interval an address's access-code attempts
	// are measured over, the same budget shape as the login one.
	accessAttemptsWindow = 10 * time.Minute

	// closeWorkers is how many connections the stopping hub says goodbye to at the same
	// time, bounded like the administrator socket's goodbyes.
	closeWorkers = 64
)

// Settings are what a hub must know about the server before it delivers
// anything to a listener: whether listeners must present an access code,
// how many connections the server admits, and the branding and contact
// address the server reports about itself.
type Settings struct {
	Branding   string
	Email      string
	MaxClients uint
	Restricted bool
}

// ServerSettings answers what the server is currently configured to do
// about listeners, asked at the moment of the question rather than
// remembered by the hub: the settings can change while listeners are
// already connected, and those listeners are who the change affects.
type ServerSettings interface {
	Settings() Settings
}

// ListenerChanges is what the hub says when a listener joins or leaves:
// only that the listener count changed. Whoever is told answers that
// occurrence on its own schedule, debouncing repeated connects.
type ListenerChanges interface {
	ListenerChanged()
}

// Dispatch hands one frame to the server that owns this hub, and reports
// a failure to the server log and ignores it: a frame the server cannot
// act on is not a reason to end the connection it arrived on. The
// context belongs to the session, which outlives the request that opened
// it.
type Dispatch func(ctx context.Context, clientID string, message Message) error

// Hub is the set of listener connections the server is holding, and the
// thing the application talks to when it has something to tell listeners.
// A session is a taken-over connection; only a registered session is a
// listener, counted and sent calls.
type Hub struct {
	settings ServerSettings
	changes  ListenerChanges
	logs     ports.LogRepository

	// clock is the server's one clock, injected into every writer, so log
	// entries order against each other. The hub used to stamp its entries
	// with the machine's wall clock.
	clock ports.Clock

	mutex    sync.Mutex
	sessions map[string]*listener
	listens  map[string]*listener
	nextID   uint64

	// accessAttempts counts access-code attempts per address, in a window
	// that ages like the login budget. The per-session counter alone is
	// bypassable by reconnecting, so the address counter is what makes
	// "five tries" mean five tries per machine.
	accessAttempts map[string]*accessWindow

	// held counts the goroutines reading taken-over connections. A stopping
	// server must finish them before it closes the database the log lives
	// in.
	held sync.WaitGroup

	// closed marks a hub whose close has run: a connection arriving after it
	// is refused, because attaching one would add goroutines to a WaitGroup
	// the close is already waiting on.
	closed bool

	// The intervals above are what every hub starts with, held as fields so
	// this package's tests can shorten them; nothing outside this package
	// may pick values that break the protocol.
	handshakeWait time.Duration
	pongWait      time.Duration
	pingPeriod    time.Duration
	writeWait     time.Duration
}

var _ ports.Hub = (*Hub)(nil)

// NewHub builds the hub from what it needs to know about the server,
// who it tells about listener changes, where its log goes, and the
// server's clock to stamp the entries it writes. The hub keeps no
// console of its own: every line it says about a connection goes into
// the server log, which the composition root reads out loud.
func NewHub(settings ServerSettings, changes ListenerChanges, logs ports.LogRepository, clock ports.Clock) *Hub {
	return &Hub{
		accessAttempts: map[string]*accessWindow{},
		changes:        changes,
		clock:          clock,
		handshakeWait:  pongWait,
		listens:        map[string]*listener{},
		logs:           logs,
		nextID:         0,
		pingPeriod:     pingPeriod,
		pongWait:       pongWait,
		sessions:       map[string]*listener{},
		settings:       settings,
		writeWait:      writeWait,
	}
}

// accessWindow is one address's access-attempt history: tries spent in
// the current window, when the most recent one was, and whether the
// refused-code warning for this window has been written.
type accessWindow struct {
	count  uint
	warned bool
	date   time.Time
}

// Attach takes over one upgraded listener connection and returns the
// session identifier it is known by. It reports false, closing the
// connection in silence, when the server already holds as many
// connections as it allows.
func (hub *Hub) Attach(conn *websocket.Conn, remoteAddr string, dispatch Dispatch) (string, bool) {
	if conn == nil {
		return "", false
	}

	// The read limit is set before the connection is held: an oversized
	// frame ends the connection at the socket and is never read into memory
	// to be looked at.
	conn.SetReadLimit(maxFrameSize)

	max := hub.settings.Settings().MaxClients

	hub.mutex.Lock()

	// A hub whose close has run does not attach anything: the close has already waited for
	// every goroutine the hub had started.
	if hub.closed {
		hub.mutex.Unlock()

		conn.Close()

		return "", false
	}

	// The admission gate is on the connections the server holds, not the
	// ones that finished registering: a client that never answers must not
	// keep a slot open.
	if uint(len(hub.sessions)) >= max {
		hub.mutex.Unlock()

		conn.Close()

		return "", false
	}

	hub.nextID++

	id := strconv.FormatUint(hub.nextID, 10)
	session := newListener(hub, id, conn, remoteAddr)

	hub.sessions[id] = session

	hub.mutex.Unlock()

	// Both goroutines a connection runs are counted, because both write to
	// the server log, and closing the hub waits for them so no entry is
	// still on its way when the log's database closes.
	hub.held.Add(2)

	go func() {
		defer hub.held.Done()

		session.write()
	}()

	go func() {
		defer hub.held.Done()

		session.read(dispatch)
	}()

	return id, true
}

// BroadcastCall delivers a recorded call to every listener it is for:
// the listener's live-watch selection must ask for that system or
// talkgroup, and on a server that requires access codes the listener's
// access must grant it.
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

// BroadcastConfig delivers the configuration to every listener, each
// scoped to what that listener is allowed to see. On a server that
// requires access codes it arrives as a fresh request for the code.
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

// SendLivefeedMap answers a listener's live-watch selection with whether
// anything is now switching on it, told even when the answer is no. The
// session is chosen under the hub lock and delivered outside it, like
// every other delivery: a stalled listener must not stop the server
// from reaching everyone else.
func (hub *Hub) SendLivefeedMap(clientID string, watching bool) {
	hub.mutex.Lock()

	session, ok := hub.sessions[clientID]

	hub.mutex.Unlock()

	if ok {
		hub.deliver(session, delivery{frame: Message{Command: CommandLivefeedMap, Payload: watching}})
	}
}

// RequestAccessCode asks a listener for its access code.
func (hub *Hub) RequestAccessCode(clientID string) {
	hub.Send(clientID, Message{Command: CommandAccessCode})
}

// ReportAccessExpired tells a listener that its access has run out.
func (hub *Hub) ReportAccessExpired(clientID string) {
	hub.Send(clientID, Message{Command: CommandAccessExpired})
}

// ReportAccessLimit tells a listener that its identity already has as many sessions open as it is
// allowed.
func (hub *Hub) ReportAccessLimit(clientID string) {
	hub.Send(clientID, Message{Command: CommandAccessLimit})
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

	hub.Send(clientID, Message{Command: CommandVersion, Payload: payload})
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

// Close ends every connection the server has taken over and reports
// each one leaving, filing the departure while the log can still be
// written, and making the goodbyes together within the shutdown
// deadline.
func (hub *Hub) Close(ctx context.Context) {
	hub.mutex.Lock()

	// From here on, a connection arriving is refused: the goroutines this close is about
	// to wait for are all the goroutines the hub will ever have.
	hub.closed = true

	sessions := make([]*listener, 0, len(hub.sessions))

	for _, session := range hub.sessions {
		sessions = append(sessions, session)
	}

	hub.mutex.Unlock()

	workers := make(chan struct{}, closeWorkers)

	var wg sync.WaitGroup

	for _, session := range sessions {
		wg.Add(1)

		go func(session *listener) {
			defer wg.Done()

			workers <- struct{}{}
			defer func() { <-workers }()

			hub.disconnect(session)

			// The close code is the protocol's answer to a server that is going
			// away: a listener that reads it knows the connection ended at the far
			// end and can look for the next server instead of waiting out a read
			// timeout.
			hub.deliver(session, delivery{code: websocket.CloseGoingAway})
		}(session)
	}

	wg.Wait()

	// The close also waits for the goroutines that were reading and writing
	// those connections, so it reports its own departures without leaving
	// someone else's on its way.
	hub.held.Wait()
}

// GrantAccess attaches an access to a session, which is what makes that
// session authenticated. The access is kept even when the presenting
// attempt is then refused for a reason that does not take it away,
// so the server can name the identity a refused session was using.
func (hub *Hub) GrantAccess(clientID string, access domain.Access) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	if session, ok := hub.sessions[clientID]; ok {
		session.access = access
		session.granted = true
	}
}

// AccessOf returns the access a session holds, if it has been given one;
// a session that has never presented a code is not unauthenticated by
// an empty access, it has not been given one.
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

// ResetAccessAttempts clears a session's attempt counter, which happens
// only when an access code is accepted, and with it the history of the
// address the session connected from.
func (hub *Hub) ResetAccessAttempts(clientID string) {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	if session, ok := hub.sessions[clientID]; ok {
		session.attempts = 0
		delete(hub.accessAttempts, session.remoteAddr)
	}
}

// RecordAddressAccessAttempt counts one attempt against the address the
// session connected from and reports the attempts spent in the current
// window.
func (hub *Hub) RecordAddressAccessAttempt(clientID string) uint {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	session, ok := hub.sessions[clientID]
	if !ok {
		return 1
	}

	now := hub.clock.Now()

	history, ok := hub.accessAttempts[session.remoteAddr]
	if !ok {
		history = &accessWindow{}
		hub.accessAttempts[session.remoteAddr] = history
	}

	if now.Sub(history.date) > accessAttemptsWindow {
		history.count = 0
		history.warned = false
	}

	history.count++
	history.date = now

	for address, aged := range hub.accessAttempts {
		if address != session.remoteAddr && now.Sub(aged.date) > accessAttemptsWindow {
			delete(hub.accessAttempts, address)
		}
	}

	return history.count
}

// ShouldWarnAccessAttempt reports whether the refused-code warning is
// due for this session's address, once per window.
func (hub *Hub) ShouldWarnAccessAttempt(clientID string) bool {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()

	session, ok := hub.sessions[clientID]
	if !ok {
		return false
	}

	history, ok := hub.accessAttempts[session.remoteAddr]
	if !ok || history.warned {
		return false
	}

	history.warned = true

	return true
}

// AccessSessionCount reports how many listeners are watching under one
// access: a session attached but not yet registered is not watching and
// is not counted, which is how an access with a limit of one admits
// exactly one listener.
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

// Send hands one frame to one session. It is deliberately not part of
// ports.Hub: the application holds commands, queries, and domain events,
// not protocol frames, and only the listener endpoint builds frames.
// Frames come from presentation.
func (hub *Hub) Send(clientID string, frame Message) {
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

// deliver hands one thing to a session's writer. A stopped writer is
// skipped, and a send waiting for room stops the moment the writer
// stops, while a merely slow listener does hold its delivery. The hub
// lock is not held while this waits.
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
		hub.logf(domain.LogLevelInfo, "a listener joined from %s as %s", session.remoteAddr, session.access.Ident)

		return
	}

	hub.logf(domain.LogLevelInfo, "a listener joined from %s", session.remoteAddr)
}

// disconnect forgets a session and says so. Every connection the server
// has taken over is reported when it goes, including one that never
// became a listener, because a connection that came and went is
// something the operator can see happening.
func (hub *Hub) disconnect(session *listener) {
	hub.mutex.Lock()

	_, was := hub.sessions[session.id]
	delete(hub.sessions, session.id)
	delete(hub.listens, session.id)

	hub.mutex.Unlock()

	if !was {
		return
	}

	session.cancel()

	// Every taken-over connection is reported when it goes, even one that
	// never became a listener: the legacy server reports the departures it
	// was holding, and the listeners count is re-reported five seconds
	// later either way.
	hub.changes.ListenerChanged()

	if len(session.access.Ident) > 0 {
		hub.logf(domain.LogLevelInfo, "a listener left from %s as %s", session.remoteAddr, session.access.Ident)

		return
	}

	hub.logf(domain.LogLevelInfo, "a listener left from %s", session.remoteAddr)
}

// logf files one notice about a listener session in the server log. The
// hub files them itself because joins, departures and unreadable
// messages are what operators filter the log view for, and the level is
// part of the call: a join is a notice, an unreadable frame a problem.
func (hub *Hub) logf(level string, format string, args ...any) {
	if hub.logs == nil {
		return
	}

	// A join or a departure is not the answer to anybody's request, so it belongs to no request's
	// lifetime and is not cancelled when one ends.
	hub.logs.AppendLog(context.Background(), domain.LogEntry{
		DateTime: hub.clock.Now(),
		Level:    level,
		Source:   domain.LogSourceLivefeed,
		Message:  fmt.Sprintf(format, args...),
	})
}

// delivery is one thing a session's writer is asked to do: write a frame, or end the connection with a
// close code.
type delivery struct {
	frame Message
	code  int
}

// queued is what a broadcast hands one session: frames chosen while the
// hub lock is held, because choosing them reads what that session may
// see, and delivered after the lock is given back.
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

	// ctx belongs to the session and not to the request that opened it: a
	// listener connection outlives the handler that upgraded it, and the
	// session going away is what should stop the work done for it.
	ctx    context.Context
	cancel context.CancelFunc

	// handshake is the window a connection has to become a listener. The
	// first configuration written on the connection stops it, and an
	// unstopped window ends the connection, dropping a restricted server's
	// sessions whose access code never arrives.
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
	ctx, cancel := context.WithCancel(context.Background())

	return &listener{
		cancel:     cancel,
		conn:       conn,
		ctx:        ctx,
		handshake:  time.AfterFunc(hub.handshakeWait, func() { conn.Close() }),
		hub:        hub,
		id:         id,
		livefeed:   domain.NewLivefeed(),
		quit:       make(chan struct{}),
		remoteAddr: remoteAddr,
		send:       make(chan delivery, sendBuffer),
	}
}

// read takes the frames a session sends, in order, and hands each to
// the server behind the hub. A frame the server cannot read or act on
// is filed in the log and skipped, keeping the connection open.
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
			session.hub.logf(domain.LogLevelWarn, "a message from a listener could not be read: %v", err)

			continue
		}

		if err := dispatch(session.ctx, session.id, message); err != nil {
			session.hub.logf(domain.LogLevelWarn, "a message from a listener was not understood: %v", err)
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
				session.hub.logf(domain.LogLevelWarn, "a message for a listener could not be written: %v", err)

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
