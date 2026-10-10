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

package backend

import (
	"encoding/json"
)

// The commands are wire values, not internal names. They are what the native applications send and
// what they switch on, so they are frozen by the protocol contract and are not free to become
// something more descriptive.
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

// Message is one frame on the listener WebSocket: a JSON array of at most three elements — a
// command, an optional payload, and an optional flag.
//
// All three are `any` because the frame is a positional array and not an object. A client may put
// anything in any of the three places, and the server's job is to read what is there and match it
// against the commands it knows: a frame whose command is the number 7 matches none of them, and the
// legacy server answers it with nothing and keeps the connection open. Refusing to read such a frame
// would close a connection that today stays connected, which is a different server.
type Message struct {
	Command any
	Payload any
	Flag    any
}

// ReadMessage reads one frame exactly as the legacy server reads it: the array is taken apart by
// position, and whatever is not there is left unset.
//
// The only thing that makes this fail is a frame that is not an array. The legacy server writes one
// line to its console and reads the next frame, so the caller does the same — see the connection
// loop, which writes it as `client.message.fromjson: <cause>`.
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

// MarshalJSON writes the frame.
//
// A payload or flag that is absent is left out, and so is one that is the empty string: that is what
// makes `["PIN"]` two characters rather than six. A payload that is present and says no is kept — the
// livefeed ack is `["LFM",false]`, and a client that is waiting for an ack cannot tell the difference
// between "no" and "nothing yet".
//
// The flag is appended on its own, without looking at whether a payload was there, because a client
// reads the array by position and expects a flag in the third place.
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
