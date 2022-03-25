// Copyright (C) 2019-2022 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

package main

import (
	"errors"
	"fmt"
	"log"
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
}

func (client *Client) Init(controller *Controller, conn *websocket.Conn) error {
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
	client.Send = make(chan *Message, 8)
	client.Livefeed = NewLivefeed()

	controller.Register <- client

	go func() {
		defer func() {
			controller.Unregister <- client
		}()

		client.Conn.SetReadDeadline(time.Now().Add(pongWait))

		client.Conn.SetPongHandler(func(string) error {
			client.Conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		for {
			_, b, err := client.Conn.ReadMessage()
			if err != nil {
				break
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
			timer.Stop()

			client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
			client.Conn.Close()
		}()

		for {
			select {
			case message, ok := <-client.Send:
				if !ok {
					return
				}

				if client.Conn == nil {
					return
				}

				if message.Command == MessageCommandConfig {
					timer.Stop()
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

func (client *Client) SendConfig(groups *Groups, options *Options, systems *Systems, tags *Tags) {
	client.SystemsMap = systems.GetScopedSystems(client, groups, tags, options.SortTalkgroups)
	client.GroupsMap = groups.GetGroupsMap(&client.SystemsMap)
	client.TagsMap = tags.GetTagsMap(&client.SystemsMap)

	client.Send <- &Message{
		Command: MessageCommandConfig,
		Payload: map[string]interface{}{
			"dimmerDelay":        options.DimmerDelay,
			"groups":             client.GroupsMap,
			"keypadBeeps":        GetKeypadBeeps(options),
			"showListenersCount": options.ShowListenersCount,
			"systems":            client.SystemsMap,
			"tags":               client.TagsMap,
			"tagsToggle":         options.TagsToggle,
		},
	}
}

func (client *Client) SendListenersCount(count int) {
	client.Send <- &Message{
		Command: MessagecommandListenersCount,
		Payload: count,
	}
}

type Clients struct {
	Map   map[*Client]bool
	mutex sync.Mutex
}

func NewClients() *Clients {
	return &Clients{
		Map:   make(map[*Client]bool),
		mutex: sync.Mutex{},
	}
}

func (clients *Clients) AccessCount(client *Client) int {
	clients.mutex.Lock()
	defer clients.mutex.Unlock()

	count := 0

	for c := range clients.Map {
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
	clients.mutex.Lock()
	defer clients.mutex.Unlock()

	for c := range clients.Map {
		if (!restricted || c.Access.HasAccess(call)) && c.Livefeed.IsEnabled(call) {
			c.Send <- &Message{Command: MessageCommandCall, Payload: call}
		}
	}
}

func (clients *Clients) EmitConfig(groups *Groups, options *Options, systems *Systems, tags *Tags, restricted bool) {
	clients.mutex.Lock()
	defer clients.mutex.Unlock()

	for c := range clients.Map {
		if restricted {
			c.Send <- &Message{Command: MessageCommandPin}
		} else {
			c.SendConfig(groups, options, systems, tags)
		}

		if options.ShowListenersCount {
			c.SendListenersCount(len(clients.Map))
		}
	}
}

func (clients *Clients) EmitListenersCount() {
	clients.mutex.Lock()
	defer clients.mutex.Unlock()

	for c := range clients.Map {
		c.SendListenersCount(len(clients.Map))
	}
}

func (clients *Clients) Remove(client *Client) {
	clients.mutex.Lock()
	defer clients.mutex.Unlock()

	delete(clients.Map, client)

	close(client.Send)
}
