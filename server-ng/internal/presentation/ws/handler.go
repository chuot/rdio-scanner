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
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"

	"rdio-scanner/server-ng/internal/application"
	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
	"rdio-scanner/server-ng/internal/presentation/dto"
)

// Deps are the use cases the listener endpoint answers each command with, and the things it needs in
// order to reach a listener at all.
type Deps struct {
	// Access decides whether a listener may hold an access code, and sends that listener its
	// configuration when it may.
	Access *application.AuthenticateAccessHandler

	// Call reads one recorded call in full, audio included.
	Call *application.GetCallQueryHandler

	// Config reads the configuration the server is running with.
	Config *application.GetConfigHandler

	// Console is where the endpoint writes the lines it says about a connection it could not take
	// over. They are not the server's log and do not go into it.
	Console io.Writer

	// Gate decides whether a session may send a command at all, or is still being asked for its access
	// code.
	Gate *application.AccessGate

	// Hub is the set of listener sessions this endpoint answers.
	Hub *Hub

	// Livefeed stores a listener's live-watch selection and answers it.
	Livefeed *application.SetLivefeedHandler

	// Search reads one page of the call history.
	Search *application.SearchCallsQueryHandler

	// Settings answers what the server requires of listeners right now, which is the same answer the
	// hub is given: one configuration, asked at the moment of the question.
	Settings ServerSettings

	// Version is the version the server reports about itself.
	Version string
}

// Handler is the listener endpoint: it takes over an upgraded connection, hands each frame that arrives
// on it to the use case that answers that command, and has nothing to do with the connection afterwards.
//
// It is the only place that knows both the wire and the use cases, which is why the rules it applies are
// the protocol's own and not the application's: which command is answered before the access gate, what
// a refused command is answered with, and what a frame the server cannot act on costs the connection.
type Handler struct {
	access   *application.AuthenticateAccessHandler
	call     *application.GetCallQueryHandler
	config   *application.GetConfigHandler
	console  io.Writer
	gate     *application.AccessGate
	hub      *Hub
	livefeed *application.SetLivefeedHandler
	search   *application.SearchCallsQueryHandler
	settings ServerSettings
	version  string
}

var _ http.Handler = (*Handler)(nil)

// NewHandler builds the endpoint from the use cases it answers with.
func NewHandler(deps Deps) *Handler {
	console := deps.Console
	if console == nil {
		console = io.Discard
	}

	return &Handler{
		access:   deps.Access,
		call:     deps.Call,
		config:   deps.Config,
		console:  console,
		gate:     deps.Gate,
		hub:      deps.Hub,
		livefeed: deps.Livefeed,
		search:   deps.Search,
		settings: deps.Settings,
		version:  deps.Version,
	}
}

// ServeHTTP takes over one listener connection.
//
// Any path answers a websocket request, because that is what today's clients ask for: they open a
// connection on the page they are on, and a server that only answers one path loses them. The origin is
// not checked, which is what the legacy server does and what a native application on a phone depends on.
//
// The connection is not answered here. The hub holds it from here on, and this handler is called back
// for each frame that arrives.
func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool {
			return true
		},

		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Fprintf(handler.console, "websocket.upgrade: %v\n", err)

		return
	}

	handler.hub.Attach(conn, remoteAddress(r), handler.dispatch)
}

// dispatch decides which use case answers one frame, and reports a failure the hub writes to its console
// and then ignores.
//
// The order is the legacy order and it is what the clients see. The version report is answered before
// the access gate, because a client working out what server it has reached has not been asked for a code
// yet. Everything else is gated: on a server that requires access codes, a session that has not
// presented one is asked for it instead of having its command processed — including a command this
// server does not know, because the server has not gotten as far as deciding what to do with it.
func (handler *Handler) dispatch(ctx context.Context, clientID string, frame Message) error {
	if frame.Command == CommandVersion {
		handler.hub.SendVersion(clientID, handler.version)

		return nil
	}

	gated, err := handler.gate.RequiresAccessCode(ctx, clientID)
	if err != nil {
		return err
	}

	if gated && frame.Command != CommandAccessCode {
		handler.hub.RequestAccessCode(clientID)

		return nil
	}

	switch frame.Command {
	case CommandAccessCode:
		return handler.authenticate(ctx, clientID, frame)

	case CommandCall:
		return handler.fetchCall(ctx, clientID, frame)

	case CommandCallList:
		return handler.searchCalls(ctx, clientID, frame)

	case CommandConfig:
		return handler.sendConfig(ctx, clientID)

	case CommandLivefeedMap:
		return handler.setLivefeed(ctx, clientID, frame)
	}

	// A command the server has no use for is answered with nothing, and is not a failure. The native
	// applications send commands this server does not act on, and a server that reports them as errors
	// is a server its own clients have to work around.
	return nil
}

// authenticate hands the access code a listener presented to the use case that decides about it.
//
// The code arrives base64-encoded, and a code that cannot be decoded never became a code: it is reported
// to the console and it spends none of the attempts this session is allowed, exactly as it does today.
// Every reply the listener gets — another request for the code, the expiry notice, the limit notice, the
// configuration — is the use case's own.
func (handler *Handler) authenticate(ctx context.Context, clientID string, frame Message) error {
	code, ok := frame.Payload.(string)
	if !ok {
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(code)
	if err != nil {
		return fmt.Errorf("controller.processmessage.commandpin: %v", err)
	}

	_, err = handler.access.Handle(ctx, application.AuthenticateAccessCommand{
		ClientID: clientID,
		Code:     string(decoded),
	})

	return err
}

// fetchCall answers a listener's request for one recorded call.
//
// The identifier arrives as a number or as a piece of text, and a piece of text that is not a number is
// an error the server says something about rather than a call it cannot find. The flag the listener
// asked with is handed back unchanged whatever it was, because the server does not decide what a flag
// means — the client on the other end does.
//
// A call this listener's access does not name is answered with nothing at all: the listener is not told
// that the call exists.
func (handler *Handler) fetchCall(ctx context.Context, clientID string, frame Message) error {
	var id uint64

	switch v := frame.Payload.(type) {
	case float64:
		id = uint64(v)

	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}

		id = uint64(n)
	}

	call, err := handler.call.Handle(ctx, application.GetCallByIdQuery{ID: id})
	if err != nil {
		return err
	}

	if handler.settings.Settings().Restricted {
		access, ok := handler.hub.AccessOf(clientID)
		if !ok || !access.Grants(call) {
			return nil
		}
	}

	handler.hub.Send(clientID, Message{
		Command: CommandCall,
		Payload: dto.CallPayload{Call: call},
		Flag:    frame.Flag,
	})

	return nil
}

// searchCalls answers a listener's history request with the page and with the criteria the listener
// asked with.
//
// The criteria are echoed from the frame as it arrived and not from the search the query ran, because
// the echo says what this server did: a criterion it could not read is not echoed, the paging is echoed
// as it was asked for and not as it was used, and a criterion that was not asked for does not appear.
func (handler *Handler) searchCalls(ctx context.Context, clientID string, frame Message) error {
	criteria, ok := frame.Payload.(map[string]any)
	if !ok {
		return nil
	}

	access, _ := handler.hub.AccessOf(clientID)

	page, err := handler.search.Handle(ctx, application.SearchCallsQuery{
		Criteria: domain.ParseCallSearch(criteria),
		Scope:    access.Scope,
	})
	if err != nil {
		return fmt.Errorf("controller.processmessage.commandlistcall: %v", err)
	}

	handler.hub.Send(clientID, Message{
		Command: CommandCallList,
		Payload: dto.CallSearchPagePayload{Criteria: criteria, Page: page},
	})

	return nil
}

// sendConfig answers a listener's request for the configuration with the configuration that listener is
// allowed to see.
func (handler *Handler) sendConfig(ctx context.Context, clientID string) error {
	configuration, err := handler.config.Handle(ctx, application.GetConfigQuery{})
	if err != nil {
		return err
	}

	handler.hub.SendConfig(clientID, ports.ConfigSnapshot{
		Groups:  configuration.Groups,
		Options: configuration.Options,
		Systems: configuration.Systems,
		Tags:    configuration.Tags,
	})

	return nil
}

// setLivefeed stores a listener's live-watch selection on its session. The acknowledgement — whether
// anything is now switched on — is the use case's own.
func (handler *Handler) setLivefeed(ctx context.Context, clientID string, frame Message) error {
	_, err := handler.livefeed.Handle(ctx, application.SetLivefeedCommand{
		ClientID: clientID,
		Livefeed: domain.ParseLivefeed(frame.Payload),
	})

	return err
}

// remoteAddress is the address the server names when it talks about a listener: the address in the
// forwarding header when there is one, without its port, and the address of the connection when there is
// not.
//
// It is the same address the server names in its warnings about guessed access codes, so that a line
// about a guessed code and a line about a listener joining can be read as being about the same machine.
func remoteAddress(r *http.Request) string {
	withoutPort := regexp.MustCompile(`(.+):.*$`)

	for _, address := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
		if host := withoutPort.ReplaceAllString(address, "$1"); len(host) > 0 {
			return host
		}
	}

	if host := withoutPort.ReplaceAllString(r.RemoteAddr, "$1"); len(host) > 0 {
		return host
	}

	return r.RemoteAddr
}
