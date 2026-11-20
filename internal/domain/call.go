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

import (
	"errors"
	"time"
)

// MinimumAudioLength is the number of audio bytes a call must exceed to be worth
// accepting: a payload that small is a bare container header with no recording in it.
const MinimumAudioLength = 44

// CallFrequency is one carrier a call was heard on, with the quality measurements the
// ingest source reported for it.
type CallFrequency struct {
	ID        uint64
	CallID    uint64
	Dbm       int
	Errors    uint
	Frequency uint
	Offset    float32
	Spikes    uint
}

// CallUnit is one unit heard during a call, and how far into the recording it spoke.
type CallUnit struct {
	ID      uint64
	CallID  uint64
	Offset  float32
	UnitRef uint
}

// Call is a recorded transmission: the aggregate root that owns the audio, the
// frequencies and units heard in it, the talkgroups it was patched to, and the system
// and talkgroup it belongs to. AudioLength is what the server learned of its duration;
// zero means it never learned one, and a call with no known length is still complete.
type Call struct {
	ID                 uint64
	Audio              []byte
	AudioFilename      string
	AudioLength        time.Duration
	AudioMime          string
	Delayed            bool
	Frequencies        []CallFrequency
	Meta               IngestMeta
	PatchTalkgroupRefs []uint
	SiteRef            uint
	System             System
	Talkgroup          Talkgroup
	Timestamp          time.Time
	Units              []CallUnit
}

// NewCall returns a call whose collections are empty rather than nil, so a call with
// no frequencies, patches or units still carries them as [] on the wire.
func NewCall() Call {
	return Call{
		Frequencies:        []CallFrequency{},
		Meta:               IngestMeta{TalkgroupGroup: []string{}, UnitRefs: []uint{}, UnitLabels: []string{}},
		PatchTalkgroupRefs: []uint{},
		Units:              []CallUnit{},
	}
}

// IsValid reports the last reason the call cannot be accepted, or nil when it can be.
// A call needs audio beyond its container header, a real timestamp, and either a
// resolved system and talkgroup or an ingest claim that names each one. All four are
// always checked, in that order, and a call that is short in several ways is refused for
// the last one: the sentence an ingest client or an operator reads names the last thing
// the submission was missing.
func (call Call) IsValid() error {
	reason := error(nil)

	if len(call.Audio) <= MinimumAudioLength {
		reason = errors.New("no audio")
	}

	if call.Timestamp.UnixMilli() == 0 {
		reason = errors.New("no timestamp")
	}

	if !call.hasSystemClaim() {
		reason = errors.New("no system")
	}

	if !call.hasTalkgroupClaim() {
		reason = errors.New("no talkgroup")
	}

	return reason
}

// IsConnectionCheck reports whether the submission is an uploader's connection check: the
// uploader's own claim that it only asks whether this server will take a recording, and no audio
// past a container header to put in one. A claim beside audio long enough to be a recording is
// read as that recording, and a submission that claims nothing is never a check however short it
// is.
func (call Call) IsConnectionCheck() bool {
	return call.Meta.ConnectionCheck && len(call.Audio) <= MinimumAudioLength
}

// HasSystem reports whether a system is attached to the call: one the server already
// stores, or one auto-populate has just created for it and has not stored yet.
func (call Call) HasSystem() bool {
	return call.System.ID > 0 || call.System.SystemRef > 0
}

// HasTalkgroup reports whether a talkgroup is attached to the call, in the same sense as
// HasSystem.
func (call Call) HasTalkgroup() bool {
	return call.Talkgroup.ID > 0 || call.Talkgroup.TalkgroupRef > 0
}

// hasSystemClaim reports whether the ingest source named a system in any form, which is
// not the same as the server having resolved one.
func (call Call) hasSystemClaim() bool {
	return call.System.ID > 0 || call.Meta.SystemID > 0 || call.Meta.SystemRef > 0 || call.Meta.SystemLabel != ""
}

// hasTalkgroupClaim reports whether the ingest source named the talkgroup the call is
// filed under, which it does with a number: the stored identity or the client-facing
// reference. A label is the name a stored talkgroup carries after the call has been
// filed, never something a call is filed under, so a claim that names only a label
// claims no talkgroup at all.
func (call Call) hasTalkgroupClaim() bool {
	return call.Talkgroup.ID > 0 || call.Meta.TalkgroupID > 0 || call.Meta.TalkgroupRef > 0
}

// DuplicateWindow returns the inclusive timestamp bounds in which another call would
// make this one a duplicate. The window is symmetric around the call, which is the
// range the legacy server searched the calls table with.
func (call Call) DuplicateWindow(window time.Duration) (time.Time, time.Time) {
	return call.Timestamp.Add(-window), call.Timestamp.Add(window)
}

// IsDuplicateOf reports whether another call already covers this one: the
// same system, talkgroup, and site, and a timestamp within the window on
// either side. A zero window switches detection off; unresolved system or
// talkgroup identifiers never match. The site rule and its history live.
func (call Call) IsDuplicateOf(other Call, window time.Duration) bool {
	if window <= 0 {
		return false
	}

	if call.System.ID == 0 || other.System.ID == 0 || call.System.ID != other.System.ID {
		return false
	}

	if call.Talkgroup.ID == 0 || other.Talkgroup.ID == 0 || call.Talkgroup.ID != other.Talkgroup.ID {
		return false
	}

	if call.SiteRef != other.SiteRef {
		return false
	}

	since := call.Timestamp.Sub(other.Timestamp)
	if since < 0 {
		since = -since
	}

	return since <= window
}
