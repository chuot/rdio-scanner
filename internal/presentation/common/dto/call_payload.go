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
	call := payload.Call

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

			// Quirk Q41: a signal level of zero is stored and a negative one is not stored at all,
			// and this is where the stored zero is dropped again. A level the server kept is a level
			// nobody ever sees.
			if frequency.Dbm > 0 {
				entry["dbm"] = frequency.Dbm
			}

			frequencies = append(frequencies, entry)
		}

		body["frequencies"] = frequencies
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

	return json.Marshal(body)
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
