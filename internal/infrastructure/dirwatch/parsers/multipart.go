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
	"fmt"
	"mime"
	"mime/multipart"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/domain"
)

// maxListEntries is the ceiling on a posted list field — frequencies, units, patches. These
// lists are read into memory and stored with the call, so an unbounded one is a way for an
// upload to spend the server's memory and storage at will.
// A real call carries tens of entries; a list over the cap is truncated, and the truncation is
// reported to the operator's log by the endpoint that files the upload.
const maxListEntries = 4096

// unixSeconds is the shape of a Unix second count, which the pre-v7 `dateTime` field may carry.
// Compiled once: the endpoint reads a field per upload, and compiling a pattern per field is
// work the server does for no reason.
var unixSeconds = regexp.MustCompile(`^[0-9]+$`)

// truncatedList is the warning one oversized list earns.
func truncatedList(field string) string {
	return fmt.Sprintf("the %s list was truncated to %d entries", field, maxListEntries)
}

// ApplyMultipart reads one form field of a call upload into the call.
//
// Every field is optional and every field is read on its own, so an upload that names a field
// this server does not know is read as nothing and the rest of the upload still lands. The
// field order is what decides which of two fields naming the same thing wins, because each one
// writes over what an earlier one wrote — which is why an upload naming both `frequency` and
// `frequencies` keeps whichever came last.
//
// The second result is what the reading had to say about the field: a list over the cap is
// truncated, and the endpoint files that warning where an administrator reads it. A field read
// whole says nothing.
func ApplyMultipart(part *multipart.Part, data []byte, call domain.Call) (domain.Call, string) {
	warning := ""

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
		if unixSeconds.Match(data) {
			if seconds, err := strconv.Atoi(string(data)); err == nil {
				call.Timestamp = time.Unix(int64(seconds), 0)
			}

			return call, warning
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
		list, truncated := decodeList(data)
		if list == nil {
			return call, warning
		}

		if truncated {
			warning = truncatedList("frequencies")
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
		list, truncated := decodeList(data)
		if truncated {
			warning = truncatedList(part.FormName())
		}

		for _, item := range list {
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
		list, truncated := decodeList(data)
		if truncated {
			warning = truncatedList("sources")
		}

		for _, item := range list {
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
		list, truncated := decodeList(data)
		if truncated {
			warning = truncatedList("units")
		}

		for _, item := range list {
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

	return call, warning
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
// field is not one. A list over the cap is cut at it, and the second result says that it was:
// the reading bounds the memory a posted list can spend, and the caller says so out loud.
func decodeList(data []byte) ([]any, bool) {
	var decoded any

	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, false
	}

	list, ok := decoded.([]any)
	if !ok {
		return nil, false
	}

	if len(list) > maxListEntries {
		return list[:maxListEntries], true
	}

	return list, false
}

// ApplyMultipart reads one posted field of a call upload into the call, for the value the call-upload
// endpoints are wired with.
//
// It is this package's ApplyMultipart reading and nothing new: the same knowledge is what the directory
// watchers use for the files they read. The method exists because the endpoints are wired with a parser and
// may not import this package directly, by the server's contract, so the composition root hands them this
// value and the endpoints reach the reading through the seam they declare themselves.
func (parsers *Parsers) ApplyMultipart(part *multipart.Part, data []byte, call domain.Call) (domain.Call, string) {
	return ApplyMultipart(part, data, call)
}
