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

package domain

import "unicode"

// MaxLabelLength is the longest label the server will carry: labels arrive from uploads and
// watched files, and a label of a megabyte is not a name, it is a payload.
const MaxLabelLength = 256

// SanitizeText makes text that arrived from outside the server safe to log, store
// as a label, and broadcast: control characters — newline, carriage return, tab, and
// escape — become spaces, so outside text can never forge a log line or command a
// terminal. The limit counts characters, and zero means no limit.
func SanitizeText(s string, limit int) string {
	runes := []rune(s)

	for i, r := range runes {
		if unicode.IsControl(r) {
			runes[i] = ' '
		}
	}

	if limit > 0 && len(runes) > limit {
		runes = runes[:limit]
	}

	return string(runes)
}

// Sanitized returns the call with every text an outside source supplied made safe: the
// recording's name and the system, talkgroup, group, tag and unit labels the call claims.
// This is the one door all of that text passes through on its way to log lines,
// configuration, and the live broadcast, so the ingest use case runs it at its entry.
func (call Call) Sanitized() Call {
	call.AudioFilename = SanitizeText(call.AudioFilename, MaxLabelLength)
	call.Meta.SystemLabel = SanitizeText(call.Meta.SystemLabel, MaxLabelLength)
	call.Meta.TalkgroupLabel = SanitizeText(call.Meta.TalkgroupLabel, MaxLabelLength)
	call.Meta.TalkgroupName = SanitizeText(call.Meta.TalkgroupName, MaxLabelLength)
	call.Meta.TalkgroupTag = SanitizeText(call.Meta.TalkgroupTag, MaxLabelLength)

	// A length is a number from outside the server too, and a negative duration is not a
	// length: it clamps to the zero that means the server never measured one.
	if call.AudioLength < 0 {
		call.AudioLength = 0
	}

	groups := make([]string, 0, len(call.Meta.TalkgroupGroup))

	for _, group := range call.Meta.TalkgroupGroup {
		groups = append(groups, SanitizeText(group, MaxLabelLength))
	}

	call.Meta.TalkgroupGroup = groups

	labels := make([]string, 0, len(call.Meta.UnitLabels))

	for _, label := range call.Meta.UnitLabels {
		labels = append(labels, SanitizeText(label, MaxLabelLength))
	}

	call.Meta.UnitLabels = labels

	return call
}
