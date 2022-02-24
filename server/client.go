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
	"time"

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
	Livefeed    *Livefeed
	SystemsMap  SystemsMap
}

func (client *Client) Init(controller *Controller, conn *websocket.Conn) error {
	const (
		pongWait   = 60 * time.Second
		pingPeriod = pongWait / 10 * 9
		writeWait  = 10 * time.Second
		closeWait  = 10 * time.Second
	)

	if client.initialized {
		return errors.New("client.init: already initialized")
	}

	if conn == nil {
		return errors.New("client.init: no websocket connection")
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
			controller.Unregister <- client

			ticker.Stop()
			timer.Stop()

			client.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			client.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

			time.Sleep(closeWait)

			client.Conn.Close()
		}()

	Loop:
		for {
			select {
			case message, ok := <-client.Send:
				if !ok {
					break Loop
				}

				if client.Conn == nil {
					break Loop
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
						break Loop
					}
				}

			case <-ticker.C:
				if err := client.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait)); err != nil {
					break Loop
				}
			}
		}
	}()

	return nil
}
