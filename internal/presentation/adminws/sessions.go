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

// Package adminws is the administrator's configuration socket: the connection the web app holds open so it
// learns about configuration changes as they happen instead of on its next reload.
//
// It is not a listener connection and shares nothing with one. It has no commands, no live feed, no history,
// and no access codes; it sends its credential, and after that it only ever reads.
package adminws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/dto"
	"rdio-scanner/internal/presentation/httpaddr"
)

// writeWait bounds how long the server is willing to spend writing to one administrator connection.
//
// The legacy server writes with no deadline at all, which means an administrator connection that stops
// reading holds whatever save announced the change. The interval is the same one listener connections are
// written to, and it is far longer than a real client takes.
const writeWait = 10 * time.Second

// upgrader is the socket this endpoint hands over.
//
// It is left at its zero value because that is what the legacy server uses here, and the zero value is not
// neutral: with no CheckOrigin the upgrader insists the connection comes from the same origin the page was
// served from. The listener endpoint asks for the opposite. The asymmetry is the legacy server's own and is
// reproduced rather than tidied.
var upgrader = websocket.Upgrader{}

// Sessions is the server's set of open administrator connections.
//
// It is both the handler the socket upgrade is served by and the thing the application layer asks about when
// the configuration has changed.
type Sessions struct {
	// session answers whether a credential an administrator connection presents is one this server issued.
	session *application.CheckAdminSessionHandler

	// version is the version the server reports about itself, which is part of what an administrator is
	// shown.
	version string

	// logs and clock are how this socket reports what it did with a connection. The socket had no log at
	// all: a listener connection says who joined, who left and whose credential it would not take, and the
	// administrator's own connection — the one the web app holds open — said none of that, which is the
	// difference between a page that was closed and a server that let go of it.
	logs  ports.LogRepository
	clock ports.Clock

	// mutex guards the set of connections and what each one has been granted. It is never held while a
	// connection is being written to.
	mutex sync.Mutex
	conns map[*websocket.Conn]*administrator

	// held counts the goroutines reading the administrator connections. A server that is stopping has to
	// finish them before it closes the database the log lives in: the entry saying a connection ended is
	// written by the goroutine that was reading it, and it is written after that goroutine has already taken
	// the connection out of the set, so nothing else can tell that the entry is still on its way.
	held sync.WaitGroup
}

// administrator is one open administrator connection, whether it has said who it is, and the lock that
// decides which goroutine is allowed to be writing to it.
type administrator struct {
	conn *websocket.Conn

	// remoteAddr is the address the connection came from, which is what an operator reads when they are
	// looking for one connection among the ones the server has held. It is read the way every other entry
	// in this log reads an address — the machine, with the forwarding header honoured — rather than taken
	// off the socket, because the socket of a deployment behind a proxy belongs to the proxy.
	remoteAddr string

	// granted says this connection has presented a credential the server accepts, and so may be told what
	// the configuration is.
	granted bool

	// writing serializes the writes to this connection.
	//
	// The server writes to an administrator connection from two places: the push that announces a saved
	// configuration, and the close that ends the connection when its credential stops being accepted or its
	// peer goes away. A websocket connection is not something two goroutines may write to at the same time.
	// The legacy server does not care, and gets away with it only because the map it walks while broadcasting
	// is being changed by another goroutine at the same time anyway (quirk Q65).
	writing sync.Mutex
}

// Deps is what the administrator connections need: the use case that answers whether a credential is one
// this server issued, the version to report alongside the configuration, and the server log this socket
// reports its own connections to.
type Deps struct {
	Session *application.CheckAdminSessionHandler
	Version string
	Logs    ports.LogRepository
	Clock   ports.Clock
}

// NewSessions builds the server's set of administrator connections.
func NewSessions(deps Deps) *Sessions {
	return &Sessions{
		session: deps.Session,
		version: deps.Version,
		logs:    deps.Logs,
		clock:   deps.Clock,
		conns:   map[*websocket.Conn]*administrator{},
	}
}

// ServeHTTP hands over an administrator configuration socket.
//
// The upgrade asks for nothing: no credential in a header, no access code. What the connection is allowed to
// see is decided afterwards, by the frames it sends.
//
// A request that is not an upgrade is answered by the upgrader itself, with nothing said about it. The
// composition root routes those requests to the configuration endpoint instead, which is the same thing the
// legacy server's one handler does by checking the upgrade header first.
func (sessions *Sessions) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// The upgrader has already answered the request. The legacy server returns without a word here, and
		// unlike the listener endpoint it writes no line about it — not to the console and not into the
		// server's log.
		return
	}

	admin := sessions.join(conn, httpaddr.Address(r))

	sessions.held.Add(1)

	go func() {
		defer sessions.held.Done()

		sessions.listen(admin)
	}()
}

// NotifyConfigurationChanged pushes the whole configuration to every administrator connection that has
// presented a credential.
//
// It is the administrator's own view of the configuration — every section, with the identifiers this server
// hands out and the settings a listener has no business knowing — and not the trimmed one a listener is
// given.
//
// The legacy server pushes to everything it has registered, including a connection that has presented
// nothing at all, which means a peer that opens this socket and stays silent is handed every access code,
// every apikey, and every downstream credential as soon as anybody saves a configuration. Recorded as quirk
// Q64, and fixed by the owner's decision: a connection is open before it is trusted, and it is told nothing
// until it says who it is.
func (sessions *Sessions) NotifyConfigurationChanged(ctx context.Context, configuration domain.Configuration) {
	body, err := json.Marshal(dto.AdminConfigPayload{
		Accesses:    configuration.Access,
		Apikeys:     configuration.Apikeys,
		Dirwatches:  configuration.Dirwatches,
		Downstreams: configuration.Downstreams,
		Snapshot: ports.ConfigSnapshot{
			Groups:  configuration.Groups,
			Tags:    configuration.Tags,
			Systems: configuration.Systems,
			Options: configuration.Options,
		},
		Version: sessions.version,
	})
	if err != nil {
		// The legacy server marshals the configuration and says nothing when it cannot. A configuration this
		// server is unable to write is a fault in this server and not something an administrator can act on.
		return
	}

	sessions.mutex.Lock()

	targets := make([]*administrator, 0, len(sessions.conns))

	for _, admin := range sessions.conns {
		if admin.granted {
			targets = append(targets, admin)
		}
	}

	sessions.mutex.Unlock()

	for _, admin := range targets {
		if err := sessions.write(admin, body); err != nil {
			sessions.leave(admin)
		}
	}
}

// listen reads the frames an administrator connection sends.
//
// Every frame is checked against the credentials this server still accepts, and not only the first one: the
// rolling window retires an old login's credential, and a connection holding one is ended the next time it
// says anything.
func (sessions *Sessions) listen(admin *administrator) {
	// An administrator connection is not held to an interval. A listener that stops answering is ended
	// because the server is waiting for an answer; an administrator that has nothing to say is not, because
	// there is nothing it is being asked to answer.
	admin.conn.SetReadDeadline(time.Time{})

	for {
		_, b, err := admin.conn.ReadMessage()
		if err != nil {
			break
		}

		// The credential check belongs to no request's lifetime: the connection outlives the request that
		// created it, and a frame arriving a minute later is not part of that request either.
		if !sessions.session.Handle(context.Background(), application.CheckAdminSessionQuery{Token: string(b)}) {
			// Said without naming the credential: a token is something the server keeps secret, and an entry
			// that repeats it puts a live session credential in a log an operator can read.
			sessions.log(context.Background(), domain.LogLevelWarn,
				fmt.Sprintf("an administrator from %s presented a session token that is not one this server issued",
					admin.remoteAddr))

			break
		}

		sessions.grant(admin)
	}

	sessions.leave(admin)
}

// join puts a connection in the set of open administrator connections and hands back the record of it. The
// connection is open from the moment it is handed over; being open is not the same as being told anything.
func (sessions *Sessions) join(conn *websocket.Conn, remoteAddr string) *administrator {
	admin := &administrator{conn: conn, remoteAddr: remoteAddr}

	sessions.mutex.Lock()
	sessions.conns[conn] = admin
	sessions.mutex.Unlock()

	// Said outside the mutex: the same lock is what a configuration save takes to find the connections to
	// tell, and a save must not wait on a log write.
	sessions.log(context.Background(), domain.LogLevelInfo,
		fmt.Sprintf("an administrator connected from %s", remoteAddr))

	return admin
}

// grant records that a connection has presented a credential this server accepts.
func (sessions *Sessions) grant(admin *administrator) {
	sessions.mutex.Lock()
	defer sessions.mutex.Unlock()

	admin.granted = true
}

// write pushes one frame to one connection.
//
// The write is bounded, which the legacy server's is not: a stalled administrator connection must not be able
// to hold a configuration save. A connection that cannot be written to is not a connection any more, and the
// caller forgets it.
func (sessions *Sessions) write(admin *administrator, body []byte) error {
	admin.writing.Lock()
	defer admin.writing.Unlock()

	if err := admin.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}

	return admin.conn.WriteMessage(websocket.TextMessage, body)
}

// leave ends a connection: it is removed from the set, told why in a close frame, and closed.
//
// The close is written before the socket is closed. The legacy server hands the connection to its
// register-and-forget loop and then writes the close, which races the loop closing the socket; the capture
// shows the close frame arriving, so this order is what the legacy server was observed to do and is the one
// kept.
// leave forgets one administrator connection and says it went, when this half is the one that found it.
func (sessions *Sessions) leave(admin *administrator) {
	sessions.mutex.Lock()

	_, was := sessions.conns[admin.conn]
	delete(sessions.conns, admin.conn)

	sessions.mutex.Unlock()

	if !was {
		// The socket ended this connection already, which is what happens when the server stops while it is
		// still holding it. The departure has been said; saying it again would tell an operator that two
		// administrators left when one did.
		return
	}

	sessions.depart(admin)
}

// CloseAll ends every administrator connection the server is holding and reports each one leaving.
//
// This is what a server that is stopping needs: shutting an address down does not wait for a connection the
// server has handed over, so these connections are still open at the point where the stop closes the
// database the log lives in, and a departure noticed after that is an entry the server can no longer write.
//
// The connections are taken out of the set first, so the read loop each one unblocks has nothing left to
// report: a departure is said by the half that ended the connection, once.
func (sessions *Sessions) CloseAll() {
	sessions.mutex.Lock()

	admins := make([]*administrator, 0, len(sessions.conns))

	for _, admin := range sessions.conns {
		admins = append(admins, admin)
	}

	sessions.conns = map[*websocket.Conn]*administrator{}

	sessions.mutex.Unlock()

	for _, admin := range admins {
		sessions.depart(admin)
	}

	// And the close waits for the goroutines that were reading those connections. The entry saying a
	// connection ended is written by the goroutine that was reading it, after that goroutine has already
	// taken the connection out of the set, so a close that only ended the connections would report its own
	// departures and return while someone else's was still on its way — into a log whose database is closed
	// on the next line.
	sessions.held.Wait()
}

// depart says that one administrator's connection ended and ends it, with the close code the web
// application reads as a socket the server let go of.
func (sessions *Sessions) depart(admin *administrator) {
	sessions.log(context.Background(), domain.LogLevelInfo,
		fmt.Sprintf("an administrator disconnected from %s", admin.remoteAddr))

	admin.writing.Lock()
	defer admin.writing.Unlock()

	if err := admin.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		admin.conn.Close()

		return
	}

	_ = admin.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

	admin.conn.Close()
}

// log writes one entry to the server log, filed under the administrator's own subsystem: these are the
// administrator's connections, and an operator tracing the web app filters the log to this.
//
// A connection outlives the request that upgraded it, so none of these entries belong to a request's
// lifetime and none are written with one's context.
func (sessions *Sessions) log(ctx context.Context, level, message string) {
	if err := sessions.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: sessions.clock.Now(),
		Level:    level,
		Source:   domain.LogSourceAdmin,
		Message:  message,
	}); err != nil {
		// A log entry that cannot be written does not change what the server decided about a connection.
		return
	}
}
