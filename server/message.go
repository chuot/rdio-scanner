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
	"encoding/json"
)

const (
	MessageCommandCall           = "CAL"
	MessageCommandConfig         = "CFG"
	MessageCommandExpired        = "XPR"
	MessageCommandIOS            = "IOS"
	MessageCommandListCall       = "LCL"
	MessagecommandListenersCount = "LSC"
	MessageCommandLivefeedMap    = "LFM"
	MessageCommandMax            = "MAX"
	MessageCommandPin            = "PIN"
	MessageCommandPushId         = "PID"
	MessageCommandServer         = "SRV"
	MessageCommandVersion        = "VER"
)

type Message struct {
	Command interface{}
	Payload interface{}
	Flag    interface{}
}

func (message *Message) FromJson(b []byte) error {
	var (
		err error
		f   []interface{}
	)

	if err = json.Unmarshal(b, &f); err != nil {
		return err
	}

	l := len(f)

	if l >= 1 {
		message.Command = f[0]
	}

	if l >= 2 {
		message.Payload = f[1]
	}

	if l >= 3 {
		message.Flag = f[2]
	}

	return nil
}

func (message *Message) ToJson() ([]byte, error) {
	str := []interface{}{message.Command}

	if message.Payload != nil && message.Payload != "" {
		str = append(str, message.Payload)
	}

	if message.Flag != nil && message.Flag != "" {
		str = append(str, message.Flag)
	}

	return json.Marshal(str)
}
