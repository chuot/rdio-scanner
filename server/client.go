// Copyright (C) 2019-2021 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

	"github.com/gorilla/websocket"
)

type Client struct {
	initialized bool
	Access      *Access
	AuthCount   int
	Controller  *Controller
	Conn        *websocket.Conn
	Send        chan *Message
	Systems     []System
	GroupsMap   GroupsMap
	TagsMap     TagsMap
	LivefeedMap LivefeedMap
	SystemsMap  SystemsMap
}

func (client *Client) Init(controller *Controller, conn *websocket.Conn) error {
	if client.initialized {
		return errors.New("client.init: already initialized")
	}

	if conn == nil {
		return errors.New("client.init: no websocket connection")
	}

	client.Access = &Access{}
	client.Controller = controller
	client.Conn = conn
	client.Send = make(chan *Message, 100)
	client.LivefeedMap = LivefeedMap{}

	controller.Register <- client

	go func() {
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

		controller.Unregister <- client
	}()

	go func() {
		for {
			message, ok := <-client.Send
			if !ok {
				break
			}

			if client.Conn == nil {
				break
			}

			b, err := message.ToJson()
			if err != nil {
				log.Println(fmt.Errorf("client.message.tojson: %v", err))

			} else {
				if err = client.Conn.WriteMessage(websocket.TextMessage, b); err != nil {
					log.Println(fmt.Errorf("client.conn.writemessage: %v", err))
					break
				}
			}
		}

		controller.Unregister <- client
	}()

	return nil
}
