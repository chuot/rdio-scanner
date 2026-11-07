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

package dto

import (
	"encoding/json"
	"strconv"
	"time"

	"rdio-scanner/internal/domain"
)

// CallPayload is one recorded call as the listener protocol
// carries it, in a live push and a fetch answer. A call
// search sends CallSearchRow instead.
type CallPayload struct {
	Call domain.Call
}

// MarshalJSON writes the call. The keys come out sorted, as the
// legacy map produced them, and clients read those bytes. Seven
// keys are always present; the rest appear only with their
// value.
func (payload CallPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(callPayloadMap(payload.Call))
}

// CallMetadataPayload is one recorded call as the HTTP
// call-history surface answers it: the call payload's keys
// without the recording's bytes, naming the link instead.
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

// callPayloadMap builds the call as the wire carries it, as a map, because the
// legacy server built it from a map and clients read those bytes. Seven keys are
// always present, the rest only with their value.
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

			// frequency signal levels are emitted only above zero: a
			// level the server stored can stay unseen.
			if frequency.Dbm > 0 {
				entry["dbm"] = frequency.Dbm
			}

			frequencies = append(frequencies, entry)
		}

		body["frequencies"] = frequencies
	}

	if call.AudioLength > 0 {
		// audioLength is the recording's length in seconds to the
		// millisecond, and only when some source named it: a call
		// whose length was never measured says nothing about it.
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

// audioBuffer writes the recording as the number of each
// byte under a wrapper naming it a buffer, the shape the
// clients already read. No audio means an empty byte list.
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

// audioLengthSeconds writes a recording's length as the JSON
// number of seconds to the millisecond, 3.500 for three and
// a half. The number is written as text so its three
// decimals survive the wire; a Go float would drop them.
func audioLengthSeconds(length time.Duration) json.RawMessage {
	return json.RawMessage(strconv.FormatFloat(float64(length)/float64(time.Second), 'f', 3, 64))
}
