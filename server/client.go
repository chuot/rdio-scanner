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

package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	Access     *Access
	AuthCount  int
	Controller *Controller
	Conn       *websocket.Conn
	Send       chan *Message
	Systems    []System
	GroupsMap  GroupsMap
	TagsMap    TagsMap
	Livefeed   *Livefeed
	SystemsMap SystemsMap
	request    *http.Request
}

func (client *Client) Init(controller *Controller, request *http.Request, conn *websocket.Conn) error {
	const (
		pongWait   = 60 * time.Second
		pingPeriod = pongWait / 10 * 9
		writeWait  = 10 * time.Second
	)

	if conn == nil {
		return errors.New("client.init: no websocket connection")
	}

	if controller.Clients.Count() >= int(controller.Options.MaxClients) {
		conn.Close()
		return nil
	}

	client.Access = &Access{}
	client.Controller = controller
	client.Conn = conn
	client.Livefeed = NewLivefeed()
	client.Send = make(chan *Message, 8192)
	client.request = request

	go func() {
		defer func() {
			controller.Unregister <- client

			if len(client.Access.Ident) > 0 {
				controller.Logs.LogEvent(LogLevelInfo, fmt.Sprintf("listener disconnected from ip %s with ident %s", client.GetRemoteAddr(), client.Access.Ident))

			} else {
				controller.Logs.LogEvent(LogLevelInfo, fmt.Sprintf("listener disconnected from ip %s", client.GetRemoteAddr()))
			}

			client.Conn.Close()
		}()

		client.Conn.SetReadDeadline(time.Now().Add(pongWait))

		client.Conn.SetPongHandler(func(string) error {
			client.Conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		for {
			_, b, err := client.Conn.ReadMessage()
			if err != nil {
				return
			}

			message := &Message{}
			if err = message.FromJson(b); err != nil {
				log.Println(fmt.Errorf("client.message.fromjson: %v", err))
				continue
			}

			if err = client.Controller.ProcessMessage(client, message); err != nil {
				log.Println(fmt.Errorf("client.processmessage: %v", err))
				continue
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(pingPeriod)

		timer := time.AfterFunc(pongWait, func() {
			client.Conn.Close()
		})

		defer func() {
			ticker.Stop()

			if timer != nil {
				timer.Stop()
			}

			client.Conn.Close()
		}()

		for {
			select {
			case message, ok := <-client.Send:
				if !ok {
					return
				}

				if message.Command == MessageCommandConfig {
					if timer != nil {
						timer.Stop()
						timer = nil

						controller.Register <- client

						if len(client.Access.Ident) > 0 {
							controller.Logs.LogEvent(LogLevelInfo, fmt.Sprintf("new listener from ip %s with ident %s", client.GetRemoteAddr(), client.Access.Ident))

						} else {
							controller.Logs.LogEvent(LogLevelInfo, fmt.Sprintf("new listener from ip %s", client.GetRemoteAddr()))
						}
					}
				}

				b, err := message.ToJson()
				if err != nil {
					log.Println(fmt.Errorf("client.message.tojson: %v", err))

				} else {
					client.Conn.SetWriteDeadline(time.Now().Add(writeWait))

					if err = client.Conn.WriteMessage(websocket.TextMessage, b); err != nil {
						return
					}
				}

			case <-ticker.C:
				client.Conn.SetWriteDeadline(time.Now().Add(writeWait))

				if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	return nil
}

func (client *Client) GetRemoteAddr() string {
	trust := false
	if client.Controller != nil && client.Controller.Config != nil {
		trust = client.Controller.Config.TrustProxy
	}
	return GetRemoteAddr(client.request, trust)
}

func (client *Client) SendConfig(groups *Groups, options *Options, systems *Systems, tags *Tags) {
	client.SystemsMap = systems.GetScopedSystems(client, groups, tags, options.SortTalkgroups)
	client.GroupsMap = groups.GetGroupsMap(&client.SystemsMap)
	client.TagsMap = tags.GetTagsMap(&client.SystemsMap)

	var payload = map[string]any{
		"branding":           options.Branding,
		"dimmerDelay":        options.DimmerDelay,
		"email":              options.Email,
		"groups":             client.GroupsMap,
		"keypadBeeps":        GetKeypadBeeps(options),
		"playbackGoesLive":   options.PlaybackGoesLive,
		"showListenersCount": options.ShowListenersCount,
		"systems":            client.SystemsMap,
		"tags":               client.TagsMap,
		"tagsToggle":         options.TagsToggle,
		"time12hFormat":      options.Time12hFormat,
	}

	if len(options.AfsSystems) > 0 {
		payload["afs"] = options.AfsSystems
	}

	select {
	case client.Send <- &Message{Command: MessageCommandConfig, Payload: payload}:
	default:
	}
}

func (client *Client) SendListenersCount(count int) {
	select {
	case client.Send <- &Message{
		Command: MessagecommandListenersCount,
		Payload: count,
	}:
	default:
	}
}

type Clients struct {
	Map   map[*Client]bool
	mutex sync.Mutex
}

func NewClients() *Clients {
	return &Clients{
		Map:   map[*Client]bool{},
		mutex: sync.Mutex{},
	}
}

// snapshot returns the current set of clients under the mutex so the
// caller can iterate without holding the lock (which would deadlock if a
// downstream Send blocks long enough for Add/Remove to fire). Without this
// the Map was iterated unlocked while Add/Remove mutated it under the
// lock, and once parallel finalize workers started calling EmitCall
// concurrently this would panic with "concurrent map read and map write".
func (clients *Clients) snapshot() []*Client {
	clients.mutex.Lock()
	defer clients.mutex.Unlock()

	out := make([]*Client, 0, len(clients.Map))
	for c := range clients.Map {
		out = append(out, c)
	}
	return out
}

func (clients *Clients) AccessCount(client *Client) int {
	count := 0

	for _, c := range clients.snapshot() {
		if c.Access == client.Access {
			count++
		}
	}

	return count
}

func (clients *Clients) Add(client *Client) {
	clients.mutex.Lock()
	defer clients.mutex.Unlock()

	clients.Map[client] = true
}

func (clients *Clients) Count() int {
	clients.mutex.Lock()
	defer clients.mutex.Unlock()

	return len(clients.Map)
}

func (clients *Clients) EmitCall(call *Call, restricted bool) {
	for _, c := range clients.snapshot() {
		if (!restricted || c.Access.HasAccess(call)) && c.Livefeed.IsEnabled(call) {
			// Non-blocking send so a stalled client can't back-pressure
			// the entire ingest pipeline. The Send channel is buffered
			// (8192) at construction; if it is full the listener is too
			// far behind to keep up and we drop this frame for them.
			select {
			case c.Send <- &Message{Command: MessageCommandCall, Payload: call}:
			default:
			}
		}
	}
}

func (clients *Clients) EmitConfig(groups *Groups, options *Options, systems *Systems, tags *Tags, restricted bool) {
	snap := clients.snapshot()
	count := len(snap)

	for _, c := range snap {
		if restricted {
			select {
			case c.Send <- &Message{Command: MessageCommandPin}:
			default:
			}
		} else {
			c.SendConfig(groups, options, systems, tags)
		}

		if options.ShowListenersCount {
			c.SendListenersCount(count)
		}
	}
}

func (clients *Clients) EmitListenersCount() {
	snap := clients.snapshot()
	count := len(snap)

	for _, c := range snap {
		c.SendListenersCount(count)
	}
}

func (clients *Clients) Remove(client *Client) {
	clients.mutex.Lock()
	defer clients.mutex.Unlock()

	delete(clients.Map, client)
}
