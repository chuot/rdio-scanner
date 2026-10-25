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
	"encoding/json"
)

// The commands are wire values the native applications send and switch on: they are
// frozen by the protocol contract and are not free to become more descriptive; see
// the scanner-wire-format-and-completeness maintainer note.
const (
	// CommandCall fetches one recorded call, and pushes one that has just been recorded.
	CommandCall = "CAL"

	// CommandConfig asks for the configuration this listener is allowed to see.
	CommandConfig = "CFG"

	// CommandAccessExpired says the access code this listener unlocked has run out.
	CommandAccessExpired = "XPR"

	// CommandIOS is for the iOS applications and is never sent to a web client.
	CommandIOS = "IOS"

	// CommandCallList searches the recorded calls.
	CommandCallList = "LCL"

	// CommandListenersCount reports how many listeners are watching.
	CommandListenersCount = "LSC"

	// CommandLivefeedMap says which systems and talkgroups this listener wants live, and acks it.
	CommandLivefeedMap = "LFM"

	// CommandAccessLimit says this access code already has as many connections as it is allowed.
	CommandAccessLimit = "MAX"

	// CommandAccessCode asks for an access code, reports a refused one, and unlocks a good one.
	CommandAccessCode = "PIN"

	// CommandPushId is for the mobile applications and is never sent to a web client.
	CommandPushID = "PID"

	// CommandServer is for the native applications and is never sent to a web client.
	CommandServer = "SRV"

	// CommandVersion reports what server this is.
	CommandVersion = "VER"
)

// Message is one frame on the listener WebSocket: a JSON array of at most three
// elements - a command, an optional payload, and an optional flag. All three are
// `any` because the frame is positional: a frame matching no known command is
// answered with nothing and the connection stays open.
type Message struct {
	Command any
	Payload any
	Flag    any
}

// ReadMessage reads one frame by position, leaving whatever is not there unset. Only
// a frame that is not an array fails, and the caller writes `client.message.fromjson:
// <cause>` and reads the next frame.
func ReadMessage(b []byte) (Message, error) {
	var values []any

	if err := json.Unmarshal(b, &values); err != nil {
		return Message{}, err
	}

	message := Message{}

	if len(values) > 0 {
		message.Command = values[0]
	}

	if len(values) > 1 {
		message.Payload = values[1]
	}

	if len(values) > 2 {
		message.Flag = values[2]
	}

	return message, nil
}

// MarshalJSON writes the frame: an absent or empty-string payload or flag is left out
// (that is what keeps `["PIN"]` two characters), a present false payload is kept (the
// livefeed ack is `["LFM",false]`), and the flag is appended on its own because the
// client reads the array by position; see the frame-marshalling-rules maintainer
// note.
func (message Message) MarshalJSON() ([]byte, error) {
	frame := []any{message.Command}

	if message.Payload != nil && message.Payload != "" {
		frame = append(frame, message.Payload)
	}

	if message.Flag != nil && message.Flag != "" {
		frame = append(frame, message.Flag)
	}

	return json.Marshal(frame)
}
