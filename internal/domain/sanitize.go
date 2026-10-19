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

// SanitizeText makes text that arrived from outside the server safe to write into a log line,
// to store as a label, and to broadcast to every listener: control characters — newline and
// carriage return, which would forge a second log line; tab, which forges a column; and
// escape, the introducer of an ANSI sequence that commands the terminal reading the log — all
// become spaces, so the text keeps its shape and loses its power.
//
// The limit is a count of characters, and zero means no limit: the label paths cap at
// MaxLabelLength, and the converter's report bounds its text by its own rule.
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
// recording's name, the system, talkgroup, group, tag and unit labels the call claims.
//
// This is the one door all of that text passes through on its way to the server's log lines,
// the administrator's configuration, and the live broadcast, so the ingest use case runs it
// once at its entry — an upload must not be able to forge a log line
// or drive a terminal).
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
