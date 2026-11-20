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

import "fmt"

// DefaultGroupLabel is the group an undescribed auto-populated talkgroup joins.
const DefaultGroupLabel = "Unknown"

// DefaultTagLabel is the tag an undescribed auto-populated talkgroup receives.
const DefaultTagLabel = "Untagged"

// IngestMeta is what an ingest source claims about a call: the identifiers it knows,
// in whichever form it knows them. The resolver turns that claim into a system and a
// talkgroup.
type IngestMeta struct {
	SystemID       uint64
	SystemRef      uint
	SystemLabel    string
	TalkgroupID    uint64
	TalkgroupRef   uint
	TalkgroupLabel string
	TalkgroupName  string
	TalkgroupGroup []string
	TalkgroupTag   string
	UnitRefs       []uint
	UnitLabels     []string

	// ConnectionCheck is the uploader's own word that its submission only asks whether this
	// server will take a recording, and brings no recording to put in one. Only the call-upload
	// endpoints set it, from the form part named test; a directory watch never claims it.
	ConnectionCheck bool

	// ChannelType is what the upload endpoint says the talkgroup is — trunked or
	// conventional — and nothing else in the server acts on it. It is kept because the call
	// payload a client is handed for a call that is happening now carries it, and the calls
	// table has no column for it, so the live frame is the only place it ever appears.
	ChannelType string
}

// ResolveSystem finds the system an ingested call belongs to, trying the database
// identifier first, then the client-facing reference, then the label.
func (systems Systems) ResolveSystem(meta IngestMeta) (System, bool) {
	if meta.SystemID > 0 {
		return systems.ByID(meta.SystemID)
	}

	if meta.SystemRef > 0 {
		return systems.ByRef(meta.SystemRef)
	}

	if meta.SystemLabel != "" {
		return systems.ByLabel(meta.SystemLabel)
	}

	return System{}, false
}

// ResolveTalkgroup finds the talkgroup an ingested call belongs to within a known
// system, trying the database identifier first, then the client-facing reference,
// then the label.
func (system System) ResolveTalkgroup(meta IngestMeta) (Talkgroup, bool) {
	if meta.TalkgroupID > 0 {
		return system.Talkgroups.ByID(meta.TalkgroupID)
	}

	if meta.TalkgroupRef > 0 {
		return system.Talkgroups.ByRef(meta.TalkgroupRef)
	}

	if meta.TalkgroupLabel != "" {
		return system.Talkgroups.ByLabel(meta.TalkgroupLabel)
	}

	return Talkgroup{}, false
}

// ShouldCreateSystem reports whether auto-populate should invent the system the call
// claims. Only the server-wide option creates a system, as in the legacy server.
func ShouldCreateSystem(options Options, meta IngestMeta) bool {
	return options.AutoPopulate
}

// AutoPopulates reports whether auto-populate applies to calls on this system at all:
// either the server-wide option or the system's own flag. It is the condition the legacy
// server tests before inventing a talkgroup *or* adding units to a system.
func AutoPopulates(options Options, system System) bool {
	return options.AutoPopulate || system.AutoPopulate
}

// ShouldCreateTalkgroup reports whether auto-populate should invent the talkgroup the
// call claims. Either the server-wide option or the owning system's own flag enables
// it, and a talkgroup reference is required.
func ShouldCreateTalkgroup(options Options, system System, meta IngestMeta) bool {
	if meta.TalkgroupRef == 0 {
		return false
	}

	return AutoPopulates(options, system)
}

// GroupLabels returns the group labels the call claims, or the default group when it
// claims none.
func (meta IngestMeta) GroupLabels() []string {
	if len(meta.TalkgroupGroup) > 0 {
		return meta.TalkgroupGroup
	}

	return []string{DefaultGroupLabel}
}

// TagLabel returns the tag the call claims, or the default tag when it claims none.
func (meta IngestMeta) TagLabel() string {
	if meta.TalkgroupTag != "" {
		return meta.TalkgroupTag
	}

	return DefaultTagLabel
}

// NewTalkgroup builds the talkgroup auto-populate creates for a call, using the
// claimed labels or the generated placeholders.
func (meta IngestMeta) NewTalkgroup(groupIDs []uint64, tagID uint64) Talkgroup {
	label := meta.TalkgroupLabel
	if label == "" {
		label = fmt.Sprintf("%d", meta.TalkgroupRef)
	}

	name := meta.TalkgroupName
	if name == "" {
		name = fmt.Sprintf("Talkgroup %d", meta.TalkgroupRef)
	}

	if groupIDs == nil {
		groupIDs = []uint64{}
	}

	return Talkgroup{
		GroupIDs:     groupIDs,
		Label:        label,
		Name:         name,
		TalkgroupRef: meta.TalkgroupRef,
		TagID:        tagID,
	}
}

// ShouldRepairTalkgroupLabels reports whether an ingested call may rename the labels
// of an existing talkgroup. The gate is the auto-populate one: the server-wide option
// or the owning system's own flag. A call that names a talkgroup label renames the
// label of whatever talkgroup it lands on, configured or generated, the way the legacy
// server did, and renames nothing when neither switch is on.
func ShouldRepairTalkgroupLabels(options Options, system System, meta IngestMeta, talkgroup Talkgroup) bool {
	if meta.TalkgroupLabel == "" {
		return false
	}

	// There is nothing to repair on a talkgroup the server has not attached to the call.
	// The legacy server guards this by testing its pointer for nil; with value semantics
	// the equivalent guard is that the talkgroup is there at all.
	if talkgroup.ID == 0 && talkgroup.TalkgroupRef == 0 {
		return false
	}

	if !options.AutoPopulate && !system.AutoPopulate {
		return false
	}

	return true
}

// RepairedTalkgroup applies the claimed labels to an existing talkgroup and reports
// whether anything changed. A claim the talkgroup already carries is not a change: the
// legacy server marked the talkgroup changed only where the claim differed from what was
// stored, so a recording that repeats a stored label saves nothing. A configured name
// stands unless the call names a name over it; a name that is the placeholder
// auto-populate itself wrote is filled from the claimed label.
func RepairedTalkgroup(options Options, system System, meta IngestMeta, talkgroup Talkgroup) (Talkgroup, bool) {
	if !ShouldRepairTalkgroupLabels(options, system, meta, talkgroup) {
		return talkgroup, false
	}

	repaired := talkgroup
	changed := false

	if meta.TalkgroupLabel != talkgroup.Label {
		repaired.Label = meta.TalkgroupLabel
		changed = true
	}

	if meta.TalkgroupName != "" {
		if meta.TalkgroupName != talkgroup.Name {
			repaired.Name = meta.TalkgroupName
			changed = true
		}
	} else if talkgroup.HasGeneratedName() && talkgroup.Name != meta.TalkgroupLabel {
		repaired.Name = meta.TalkgroupLabel
		changed = true
	}

	if !changed {
		return talkgroup, false
	}

	return repaired, true
}

// ClaimedUnits pairs the unit references a call claims with the labels it claims for
// them, dropping references without a label and references outside the paired range.
func (meta IngestMeta) ClaimedUnits() Units {
	units := Units{}

	for i, ref := range meta.UnitRefs {
		if i >= len(meta.UnitLabels) {
			continue
		}

		if meta.UnitLabels[i] == "" {
			continue
		}

		units = append(units, Unit{Label: meta.UnitLabels[i], UnitRef: ref})
	}

	return units
}
