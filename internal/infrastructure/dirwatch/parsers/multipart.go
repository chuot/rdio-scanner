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

package parsers

import (
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"mime/multipart"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/domain"
)

// maxListEntries is the ceiling on a posted list field - frequencies,
// units, patches - because the lists land in server memory and are
// stored with the call: an over-cap list is truncated at it, and the
// endpoint files the truncation warning.
const maxListEntries = 4096

// unixSeconds is the shape of a Unix second count, which the pre-v7
// dateTime field may carry; compiled once, because the endpoint reads a
// field per upload.
var unixSeconds = regexp.MustCompile(`^[0-9]+$`)

// truncatedList is the warning one oversized list earns.
func truncatedList(field string) string {
	return fmt.Sprintf("the %s list was truncated to %d entries", field, maxListEntries)
}

// ApplyMultipart reads one form field of a call upload into the call:
// every field is optional, an unknown field reads as nothing, and the
// fields apply in arrival order so a later one writing the same thing
// wins; the second result is a truncation warning the endpoint files,
// and a field read whole says nothing.
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

	case "audioLength":
		// The recorder's own count of its recording, in seconds. The server
		// takes its word and measures nothing; a count it cannot read leaves
		// the length unmeasured rather than invented.
		if seconds, err := strconv.ParseFloat(string(data), 64); err == nil && seconds > 0 && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) {
			call.AudioLength = time.Duration(math.Round(seconds*1000)) * time.Millisecond
		}

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

	case "test":
		// The uploader's own word that its submission only asks whether this server will take a
		// recording. The number one and the word true are the only values that claim it; every
		// other value, and a body that posts no such part, claim nothing.
		if value := string(data); value == "1" || value == "true" {
			call.Meta.ConnectionCheck = true
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

// appendUnitMetadata pairs a unit's label with the reference it
// describes, once: a reference with no label or a label with no
// reference is not a pair.
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

// decodeList reads a form field whose value is a JSON array, says
// nothing when the field is not one, and cuts an over-cap list at the
// cap and says so.
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

// ApplyMultipart is this package's reading handed to the call-upload
// endpoints as a value, because by the server's contract they declare
// this parsing themselves and may not import the package.
func (parsers *Parsers) ApplyMultipart(part *multipart.Part, data []byte, call domain.Call) (domain.Call, string) {
	return ApplyMultipart(part, data, call)
}
