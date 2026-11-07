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

// IsValid reports the first reason the call cannot be accepted, or nil when it can be.
// A call needs audio beyond its container header, a real timestamp, and either a
// resolved system and talkgroup or an ingest claim that names each one.
func (call Call) IsValid() error {
	if len(call.Audio) <= MinimumAudioLength {
		return errors.New("no audio")
	}

	if call.Timestamp.UnixMilli() == 0 {
		return errors.New("no timestamp")
	}

	if !call.hasSystemClaim() {
		return errors.New("no system")
	}

	if !call.hasTalkgroupClaim() {
		return errors.New("no talkgroup")
	}

	return nil
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

// hasTalkgroupClaim reports whether the ingest source named a talkgroup in any form.
func (call Call) hasTalkgroupClaim() bool {
	return call.Talkgroup.ID > 0 || call.Meta.TalkgroupID > 0 || call.Meta.TalkgroupRef > 0 || call.Meta.TalkgroupLabel != ""
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
