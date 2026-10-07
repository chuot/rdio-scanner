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

package parsers

import (
	"encoding/json"
	"mime"
	"mime/multipart"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/domain"
)

// ApplyMultipart reads one form field of a call upload into the call.
//
// Every field is optional and every field is read on its own, so an upload that names a field
// this server does not know is read as nothing and the rest of the upload still lands. The
// field order is what decides which of two fields naming the same thing wins, because each one
// writes over what an earlier one wrote — which is why an upload naming both `frequency` and
// `frequencies` keeps whichever came last.
func ApplyMultipart(part *multipart.Part, data []byte, call domain.Call) domain.Call {
	switch part.FormName() {
	case "audio":
		call.Audio = data
		call.AudioFilename = part.FileName()

	case "audioFilename", "audioName":
		call.AudioFilename = string(data)
		call.AudioMime = mime.TypeByExtension(path.Ext(string(data)))

	case "audioMime", "audioType":
		call.AudioMime = string(data)

	case "channelType":
		channelType := strings.ToLower(strings.TrimSpace(string(data)))

		if channelType == "trunked" || channelType == "conventional" {
			call.Meta.ChannelType = channelType
		}

	case "dateTime":
		// The pre-v7 name for the call instant, which a client may send as a Unix second count
		// or as an RFC 3339 string.
		if regexp.MustCompile(`^[0-9]+$`).Match(data) {
			if seconds, err := strconv.Atoi(string(data)); err == nil {
				call.Timestamp = time.Unix(int64(seconds), 0)
			}

			return call
		}

		if timestamp, err := time.Parse(time.RFC3339, string(data)); err == nil {
			call.Timestamp = timestamp
		}

	case "frequency":
		if frequency, err := strconv.Atoi(string(data)); err == nil && frequency > 0 {
			call.Frequencies = []domain.CallFrequency{
				{
					Frequency: uint(frequency),
					Offset:    0,
				},
			}
		}

	case "frequencies":
		list := decodeList(data)
		if list == nil {
			return call
		}

		frequencies := []domain.CallFrequency{}

		for _, item := range list {
			frequency := domain.CallFrequency{}

			entry, ok := item.(map[string]any)
			if ok {
				if dbm, ok := entry["dbm"].(float64); ok && dbm >= 0 {
					frequency.Dbm = int(dbm)
				}

				if errors, ok := entry["errorCount"].(float64); ok && errors >= 0 {
					frequency.Errors = uint(errors)
				}

				if value, ok := entry["freq"].(float64); ok && value > 0 {
					frequency.Frequency = uint(value)
				}

				if offset, ok := entry["pos"].(float64); ok && offset >= 0 {
					frequency.Offset = float32(offset)
				}

				if spikes, ok := entry["spikeCount"].(float64); ok && spikes >= 0 {
					frequency.Spikes = uint(spikes)
				}
			}

			frequencies = append(frequencies, frequency)
		}

		call.Frequencies = frequencies

	case "patches", "patched_talkgroups":
		for _, item := range decodeList(data) {
			if ref, ok := item.(float64); ok && ref > 0 {
				call.PatchTalkgroupRefs = append(call.PatchTalkgroupRefs, uint(ref))
			}
		}

	case "site":
		if ref, err := strconv.Atoi(string(data)); err == nil && ref > 0 {
			call.SiteRef = uint(ref)
		}

	case "source":
		if source, err := strconv.Atoi(string(data)); err == nil && source > 0 {
			call.Units = append(call.Units, domain.CallUnit{
				Offset:  0,
				UnitRef: uint(source),
			})
		}

	case "sources":
		for _, item := range decodeList(data) {
			unit := domain.CallUnit{}

			entry, ok := item.(map[string]any)
			if ok {
				if offset, ok := entry["pos"].(float64); ok && offset >= 0 {
					unit.Offset = float32(offset)
				}

				if source, ok := entry["src"].(float64); ok && source > 0 {
					unit.UnitRef = uint(source)
				}

				if label, ok := entry["tag"].(string); ok {
					appendUnitMetadata(&call, unit.UnitRef, label)
				}
			}

			call.Units = append(call.Units, unit)
		}

	case "system":
		if ref, err := strconv.Atoi(string(data)); err == nil && ref > 0 {
			call.Meta.SystemRef = uint(ref)
		}

	case "systemLabel":
		call.Meta.SystemLabel = string(data)

	case "talkgroup":
		if ref, err := strconv.Atoi(string(data)); err == nil && ref > 0 {
			call.Meta.TalkgroupRef = uint(ref)
		}

	case "talkgroupGroup":
		if group := string(data); len(group) > 0 && group != "-" {
			call.Meta.TalkgroupGroup = []string{group}
		}

	case "talkgroupGroups":
		if groups := string(data); len(groups) > 0 && groups != "-" {
			call.Meta.TalkgroupGroup = strings.Split(groups, ",")
		}

	case "talkgroupLabel":
		if label := string(data); len(label) > 0 && label != "-" {
			call.Meta.TalkgroupLabel = label
		}

	case "talkgroupName":
		if name := string(data); len(name) > 0 && name != "-" {
			call.Meta.TalkgroupName = name
		}

	case "talkgroupTag":
		if tag := string(data); len(tag) > 0 && tag != "-" {
			call.Meta.TalkgroupTag = tag
		}

	case "timestamp":
		if millis, err := strconv.Atoi(string(data)); err == nil {
			call.Timestamp = time.UnixMilli(int64(millis))
		}

	case "unit":
		if ref, err := strconv.Atoi(string(data)); err == nil && ref > 0 {
			call.Units = append(call.Units, domain.CallUnit{
				Offset:  0,
				UnitRef: uint(ref),
			})
		}

	case "units":
		for _, item := range decodeList(data) {
			unit := domain.CallUnit{}

			entry, ok := item.(map[string]any)
			if ok {
				if ref, ok := entry["id"].(float64); ok && ref > 0 {
					unit.UnitRef = uint(ref)
				}

				if label, ok := entry["label"].(string); ok {
					appendUnitMetadata(&call, unit.UnitRef, label)
				}

				if offset, ok := entry["offset"].(float64); ok && offset >= 0 {
					unit.Offset = float32(offset)
				}
			}

			call.Units = append(call.Units, unit)
		}
	}

	return call
}

// appendUnitMetadata pairs a unit's label with the reference it describes, once.
//
// A unit with no reference, or with no label, is not a pair: the call records the radio that
// spoke without a name for it, rather than a name attached to nothing.
func appendUnitMetadata(call *domain.Call, unitRef uint, label string) {
	if unitRef == 0 || len(label) == 0 {
		return
	}

	for _, ref := range call.Meta.UnitRefs {
		if ref == unitRef {
			return
		}
	}

	call.Meta.UnitRefs = append(call.Meta.UnitRefs, unitRef)
	call.Meta.UnitLabels = append(call.Meta.UnitLabels, label)
}

// decodeList reads a form field whose value is a JSON array, and reports nothing when the
// field is not one.
func decodeList(data []byte) []any {
	var decoded any

	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil
	}

	list, ok := decoded.([]any)
	if !ok {
		return nil
	}

	return list
}

// ApplyMultipart reads one posted field of a call upload into the call, for the value the call-upload
// endpoints are wired with.
//
// It is this package's ApplyMultipart reading and nothing new: the same knowledge is what the directory
// watchers use for the files they read. The method exists because the endpoints are wired with a parser and
// may not import this package directly (contract-server-ng.md §1), so the composition root hands them this
// value and the endpoints reach the reading through the seam they declare themselves.
func (parsers *Parsers) ApplyMultipart(part *multipart.Part, data []byte, call domain.Call) domain.Call {
	return ApplyMultipart(part, data, call)
}
