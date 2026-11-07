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

// This file is the administrator's configuration socket: the connection the
// administration interface holds open so it learns about configuration changes as they
// happen instead of on its next reload. It is not a listener connection and shares
// nothing with one: it has no commands, no live feed, no history and no access codes;
// it sends its credential, and after that it only ever reads.
package backend

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
	"rdio-scanner/internal/presentation/common/dto"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

// writeWait bounds how long the server is willing to spend writing to one
// administrator connection, the interval listener connections are written to; the
// legacy server wrote with no deadline at all.
const writeWait = 10 * time.Second

// closeWait bounds the goodbye written on the way out: the close frame is small, and
// a peer that cannot take it in two seconds is not going to take it in ten, so the
// stopping server owes every connection a goodbye rather than ten seconds of patience
// each.
const closeWait = 2 * time.Second

// closeWorkers is how many connections the stopping server says goodbye to at the same
// time, bounded so a wall of stalled sockets does not add a goroutine per socket on
// top of the goroutines the stop is already waiting on.
const closeWorkers = 64

// maxFrameSize is the largest frame the socket reads from one administrator
// connection, the same bound the listener socket puts on its frames: a bigger frame
// is something the server cannot act on, and reading it is a way to make the server
// hold a message it will never use. The socket ends the connection at the limit.
const maxFrameSize = 1 << 20

// upgrader is the socket this endpoint hands over. It is left at its zero value
// because that is what the legacy server uses here, and the zero value is not
// neutral: with no origin check it insists the connection comes from the same origin
// the page was served from, the opposite of what the listener endpoint asks for.
var upgrader = websocket.Upgrader{}

// Sessions is the server's set of open administrator connections: both the handler
// the socket upgrade is served by and the thing the application layer asks about when
// the configuration has changed.
type Sessions struct {
	// session answers whether a credential an administrator connection presents is one this server issued.
	session *application.CheckAdminSessionHandler

	// version is the version the server reports about itself, which is part of what an administrator is
	// shown.
	version string

	// logs and clock are how this socket reports what it did with a connection: a
	// listener connection says who joined, who left and whose credential it would not
	// take, and the administrator's own connection said none of that, which is the
	// difference between a page that was closed and a server that let go of it.
	logs  ports.LogRepository
	clock ports.Clock

	// trusted is the proxy list the operator named: the connections whose forwarding header the socket
	// honors when it names them, the same list the login budget is keyed on.
	trusted *httpaddr.Trusted

	// mutex guards the set of connections and what each one has been granted. It is never held while a
	// connection is being written to.
	mutex sync.Mutex
	conns map[*websocket.Conn]*administrator

	// held counts the goroutines reading the administrator connections: the entry
	// saying a connection ended is written by its reading goroutine after it has left
	// the set, so a stopping server must finish them before closing the database the
	// log lives in.
	held sync.WaitGroup

	// pongWait is how long a connection may go without answering before it is given up
	// on, and pingPeriod is how often the server asks: the same discipline the listener
	// socket holds its connections to, and fields rather than constants because only
	// this package's tests need to pick a shorter one at all.
	pongWait   time.Duration
	pingPeriod time.Duration
}

// administrator is one open administrator connection, whether it has said who it is, and the lock that
// decides which goroutine is allowed to be writing to it.
type administrator struct {
	conn *websocket.Conn

	// remoteAddr is the address the connection came from, what an operator reads when
	// looking for one connection among the ones the server has held, read the way every
	// other entry in this log reads an address so that these lines name the address the
	// login budget counts.
	remoteAddr string

	// granted says this connection has presented a credential the server accepts, and so may be told what
	// the configuration is.
	granted bool

	// writing serializes the writes to this connection: the server writes from two
	// places, and a websocket connection is not something two goroutines may write to
	// at the same time.
	writing sync.Mutex

	// quit is closed when the read loop ends, telling the goroutine that keeps asking this
	// connection to answer that there is nothing left to ask.
	quit chan struct{}
}

// SessionDeps is what the administrator connections need: the use case that answers
// whether a credential is one this server issued, the version to report alongside the
// configuration, the server log this socket reports its connections to, and the proxy
// addresses whose forwarding header the socket honors when it names a connection.
type SessionDeps struct {
	Session *application.CheckAdminSessionHandler
	Version string
	Logs    ports.LogRepository
	Clock   ports.Clock
	Trusted *httpaddr.Trusted
}

// NewSessions builds the server's set of administrator connections.
func NewSessions(deps SessionDeps) *Sessions {
	return &Sessions{
		session: deps.Session,
		version: deps.Version,
		logs:    deps.Logs,
		clock:   deps.Clock,
		trusted: deps.Trusted,
		conns:   map[*websocket.Conn]*administrator{},

		pongWait:   60 * time.Second,
		pingPeriod: 60 * time.Second / 10 * 9,
	}
}

// ServeHTTP hands over an administrator configuration socket. The upgrade asks for
// nothing: no credential in a header, no access code, and what the connection may see
// is decided afterwards by the frames it sends. A request that is not an upgrade is
// answered by the upgrader itself and nothing said about it, the configuration
// endpoint being routed there by the composition root.
func (sessions *Sessions) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// The upgrader has already answered the request, and the legacy server returns
		// without a word here: unlike the listener endpoint it writes no line about it,
		// neither to the console nor into the server's log.
		return
	}

	// The read limit is set before the connection is held: a frame bigger than the
	// server will ever read ends the connection at the socket and is never read into
	// memory to be looked at, not even as a credential.
	conn.SetReadLimit(maxFrameSize)

	admin := sessions.join(conn, httpaddr.Address(r, sessions.trusted))

	sessions.held.Add(1)

	go func() {
		defer sessions.held.Done()

		sessions.listen(admin)
	}()
}

// NotifyConfigurationChanged pushes the whole configuration, the administrator's own
// view of it rather than the trimmed one a listener is given, to every administrator
// connection that has presented a credential.
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

// listen reads the frames an administrator connection sends. Every frame is checked
// against the credentials this server still accepts, not only the first one.
func (sessions *Sessions) listen(admin *administrator) {
	// An administrator connection is held to the same ping and pong discipline a
	// listener connection is, and one that stops answering is ended: a half-open peer
	// holds its goroutine, its slot, and the stop that waits on them. The hosted web
	// app answers pings automatically; a native administrator client must answer them
	// too.
	admin.conn.SetReadDeadline(time.Now().Add(sessions.pongWait))
	admin.conn.SetPongHandler(func(string) error {
		return admin.conn.SetReadDeadline(time.Now().Add(sessions.pongWait))
	})

	defer close(admin.quit)

	// The asking happens while the reading waits: a connection that has gone quiet is
	// not one whose next frame the server is sitting down to read, and the ping
	// goroutine writes under the same lock every other writer of this connection takes
	// and is counted with the rest.
	sessions.held.Add(1)

	go func() {
		defer sessions.held.Done()
		defer admin.conn.Close()

		ticker := time.NewTicker(sessions.pingPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				admin.writing.Lock()

				if err := admin.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
					admin.writing.Unlock()

					return
				}

				err := admin.conn.WriteMessage(websocket.PingMessage, nil)

				admin.writing.Unlock()

				if err != nil {
					return
				}

			case <-admin.quit:
				return
			}
		}
	}()

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
	admin := &administrator{conn: conn, remoteAddr: remoteAddr, quit: make(chan struct{})}

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

// write pushes one frame to one connection. The write is bounded, which the legacy
// server's is not, and a connection that cannot be written to is not a connection any
// more and is forgotten by the caller.
func (sessions *Sessions) write(admin *administrator, body []byte) error {
	admin.writing.Lock()
	defer admin.writing.Unlock()

	if err := admin.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}

	return admin.conn.WriteMessage(websocket.TextMessage, body)
}

// leave ends a connection: it is removed from the set, told why in a close frame, and
// closed; the close is written before the socket is closed. leave forgets one
// administrator connection and says it went, when this half is the one that found it.
func (sessions *Sessions) leave(admin *administrator) {
	sessions.mutex.Lock()

	_, was := sessions.conns[admin.conn]
	delete(sessions.conns, admin.conn)

	sessions.mutex.Unlock()

	if !was {
		// The socket ended this connection already, which happens when the server stops
		// while still holding it: the departure has been said, and saying it again would
		// tell an operator that two administrators left when one did.
		return
	}

	sessions.depart(context.Background(), admin)
}

// Count reports how many administrator connections the server is holding, including
// the one asking. The status view shows it as what it is: the connections the
// administration interface holds open, the page reading the count among them; it is
// not a count of who is logged in - an administrator holding the page in two tabs is
// two connections.
func (sessions *Sessions) Count() int {
	sessions.mutex.Lock()
	defer sessions.mutex.Unlock()

	return len(sessions.conns)
}

// CloseAll ends every administrator connection the server is holding and reports each
// one leaving; the set is emptied first, the goodbyes are written together, and the
// close waits for the goroutines that were reading.
func (sessions *Sessions) CloseAll(ctx context.Context) {
	sessions.mutex.Lock()

	admins := make([]*administrator, 0, len(sessions.conns))

	for _, admin := range sessions.conns {
		admins = append(admins, admin)
	}

	sessions.conns = map[*websocket.Conn]*administrator{}

	sessions.mutex.Unlock()

	workers := make(chan struct{}, closeWorkers)

	var wg sync.WaitGroup

	for _, admin := range admins {
		wg.Add(1)

		go func(admin *administrator) {
			defer wg.Done()

			workers <- struct{}{}
			defer func() { <-workers }()

			sessions.depart(ctx, admin)
		}(admin)
	}

	wg.Wait()

	// And the close waits for the goroutines that were reading those connections:
	// each one writes its own departure entry after taking the connection out of the
	// set, so a close that only ended the connections would return while another's
	// entry was still on its way into a log whose database closes on the next line.
	sessions.held.Wait()
}

// depart says that one administrator's connection ended and ends it, with the close
// code the web application reads as a socket the server let go of, bounded by the
// shutdown context and closeWait whichever is shorter: every connection is owed a
// goodbye and a stalled peer collects only its own two seconds.
func (sessions *Sessions) depart(ctx context.Context, admin *administrator) {
	sessions.log(context.Background(), domain.LogLevelInfo,
		fmt.Sprintf("an administrator disconnected from %s", admin.remoteAddr))

	deadline := time.Now().Add(closeWait)

	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	admin.writing.Lock()
	defer admin.writing.Unlock()

	if err := admin.conn.SetWriteDeadline(deadline); err != nil {
		admin.conn.Close()

		return
	}

	_ = admin.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

	admin.conn.Close()
}

// log writes one entry to the server log, filed under the administrator's own
// subsystem: an operator tracing the administration interface filters the log to this.
// A connection outlives the request that upgraded it, so none of these entries are
// written with a request's context.
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
