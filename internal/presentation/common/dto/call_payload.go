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

package dto

import (
	"encoding/json"
	"strconv"
	"time"

	"rdio-scanner/internal/domain"
)

// CallPayload is one recorded call, as the listener protocol carries it — in a live push, in the
// answer to a fetch, and in nothing else: a call search sends a row of its own, which is
// CallSearchRow.
type CallPayload struct {
	Call domain.Call
}

// MarshalJSON writes the call.
//
// The keys come out sorted because the legacy server builds this object from a map and every client
// that reads it today reads the bytes that produced. A struct would put them in the order the fields
// are declared, which is a different byte stream for the same call.
//
// Seven keys are always there — id, audio, audioName, audioType, dateTime, delayed, patches — and the
// rest only when the call has the thing they describe. `patches` is the exception worth knowing: a
// call read out of the database has no list at all and comes out as `null`, and a call being made has
// an empty one and comes out as `[]`.
func (payload CallPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(callPayloadMap(payload.Call))
}

// CallMetadataPayload is one recorded call as the HTTP call-history surface answers it: the same
// keys the listener's call payload carries, with the recording's own bytes left out and the link to
// them named instead.
//
// The recording is a separate entry point, and that is the reason this shape exists. A metadata
// question is asked far more often than a recording is listened to, and a metadata answer that
// carries the recording carries a download in every answer. The name, the type, and the length of
// the recording stay: they are metadata, and they are what a caller needs before it asks for the
// bytes.
type CallMetadataPayload struct {
	Call domain.Call
}

// MarshalJSON writes the call without its recording, and names where the recording is.
func (payload CallMetadataPayload) MarshalJSON() ([]byte, error) {
	body := callPayloadMap(payload.Call)

	delete(body, "audio")

	body["audioUrl"] = "/api/calls/" + strconv.FormatUint(payload.Call.ID, 10) + "/audio"

	return json.Marshal(body)
}

// callPayloadMap is the call as the wire carries it, built as a map because the legacy server builds
// it from a map and every client that reads it today reads the bytes that produced. A struct would
// put the keys in the order its fields are declared, which is a different byte stream for the same
// call.
//
// Seven keys are always there — id, audio, audioName, audioType, dateTime, delayed, patches — and
// the rest only when the call has the thing they describe. `patches` is the exception worth knowing:
// a call read out of the database has no list at all and comes out as `null`, and a call being made
// has an empty one and comes out as `[]`.
func callPayloadMap(call domain.Call) map[string]any {
	body := map[string]any{
		"id":        call.ID,
		"audio":     audioBuffer(call.Audio),
		"audioName": call.AudioFilename,
		"audioType": call.AudioMime,
		"dateTime":  call.Timestamp.Format(time.RFC3339),
		"delayed":   call.Delayed,
		"patches":   call.PatchTalkgroupRefs,
	}

	if len(call.Frequencies) > 0 {
		frequencies := make([]map[string]any, 0, len(call.Frequencies))

		for _, frequency := range call.Frequencies {
			entry := map[string]any{
				"errorCount": frequency.Errors,
				"freq":       frequency.Frequency,
				"pos":        frequency.Offset,
				"spikeCount": frequency.Spikes,
			}

			// A preserved quirk: a signal level of zero is stored and a negative one is not stored at all,
			// and this is where the stored zero is dropped again. A level the server kept is a level
			// nobody ever sees.
			if frequency.Dbm > 0 {
				entry["dbm"] = frequency.Dbm
			}

			frequencies = append(frequencies, entry)
		}

		body["frequencies"] = frequencies
	}

	if call.AudioLength > 0 {
		// The length of the recording, in seconds to the millisecond, and only when some
		// source named it: a call whose length was never measured says nothing about its
		// length, rather than claiming a zero-second recording.
		body["audioLength"] = audioLengthSeconds(call.AudioLength)
	}

	if call.SiteRef > 0 {
		body["site"] = call.SiteRef
	}

	if call.HasSystem() {
		body["system"] = call.System.SystemRef
	}

	if call.HasTalkgroup() {
		body["talkgroup"] = call.Talkgroup.TalkgroupRef

		if call.Talkgroup.Label != "" {
			body["talkgroupLabel"] = call.Talkgroup.Label
		}

		if call.Talkgroup.Name != "" {
			body["talkgroupName"] = call.Talkgroup.Name
		}
	}

	if call.Meta.ChannelType != "" {
		body["channelType"] = call.Meta.ChannelType
	}

	if len(call.Units) > 0 {
		sources := make([]map[string]any, 0, len(call.Units))

		for _, unit := range call.Units {
			sources = append(sources, map[string]any{
				"pos": unit.Offset,
				"src": unit.UnitRef,
			})
		}

		body["sources"] = sources
	}

	return body
}

// audioBuffer writes the recording as the number of each byte under a wrapper that says it is a
// buffer, which is the shape the clients already read. A call with no audio has an empty list of
// bytes rather than no audio.
func audioBuffer(audio []byte) map[string]any {
	numbers := make([]byte, 0, 2+4*len(audio))

	numbers = append(numbers, '[')

	for i, b := range audio {
		if i > 0 {
			numbers = append(numbers, ',')
		}

		numbers = strconv.AppendUint(numbers, uint64(b), 10)
	}

	numbers = append(numbers, ']')

	return map[string]any{
		"data": json.RawMessage(numbers),
		"type": "Buffer",
	}
}

// audioLengthSeconds writes the length of a recording as the JSON number of seconds it is,
// written to the millisecond — 3.500 for three and a half seconds — which is the count the
// uploaders name in their `audioLength` field and the precision the server measures to. The
// number is written as text so it keeps its three decimals on the wire, where a Go float
// would drop them.
func audioLengthSeconds(length time.Duration) json.RawMessage {
	return json.RawMessage(strconv.FormatFloat(float64(length)/float64(time.Second), 'f', 3, 64))
}
