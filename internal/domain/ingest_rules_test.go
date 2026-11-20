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
	"reflect"
	"testing"
)

func TestResolveSystemPrefersIdentifierThenReferenceThenLabel(t *testing.T) {
	systems := Systems{
		{ID: 1, SystemRef: 10, Label: "Alpha"},
		{ID: 2, SystemRef: 20, Label: "Bravo"},
	}

	// An identifier wins even when the reference and label point elsewhere.
	if system, ok := systems.ResolveSystem(IngestMeta{SystemID: 2, SystemRef: 10, SystemLabel: "Alpha"}); !ok || system.ID != 2 {
		t.Errorf("ResolveSystem = %+v, %v, want the system matched by identifier", system, ok)
	}

	if system, ok := systems.ResolveSystem(IngestMeta{SystemRef: 10, SystemLabel: "Bravo"}); !ok || system.SystemRef != 10 {
		t.Errorf("ResolveSystem = %+v, %v, want the system matched by reference", system, ok)
	}

	if system, ok := systems.ResolveSystem(IngestMeta{SystemLabel: "Bravo"}); !ok || system.ID != 2 {
		t.Errorf("ResolveSystem = %+v, %v, want the system matched by label", system, ok)
	}

	if _, ok := systems.ResolveSystem(IngestMeta{}); ok {
		t.Error("ResolveSystem with no claim reported a system, want none")
	}

	if _, ok := systems.ResolveSystem(IngestMeta{SystemRef: 99}); ok {
		t.Error("ResolveSystem with an unknown reference reported a system, want none")
	}
}

func TestResolveTalkgroupFindsATalkgroupByNumberOnly(t *testing.T) {
	system := System{Talkgroups: Talkgroups{
		{ID: 1, TalkgroupRef: 100, Label: "Fire"},
		{ID: 2, TalkgroupRef: 200, Label: "Law"},
	}}

	if talkgroup, ok := system.ResolveTalkgroup(IngestMeta{TalkgroupID: 2, TalkgroupRef: 100, TalkgroupLabel: "Fire"}); !ok || talkgroup.ID != 2 {
		t.Errorf("ResolveTalkgroup = %+v, %v, want the talkgroup matched by identifier", talkgroup, ok)
	}

	if talkgroup, ok := system.ResolveTalkgroup(IngestMeta{TalkgroupRef: 100, TalkgroupLabel: "Law"}); !ok || talkgroup.TalkgroupRef != 100 {
		t.Errorf("ResolveTalkgroup = %+v, %v, want the talkgroup matched by reference", talkgroup, ok)
	}

	// A label is a name an ingest source says the talkgroup has, not a number to look a
	// talkgroup up by. 6.6.3 looked a talkgroup up by its number, and so does the server.
	if _, ok := system.ResolveTalkgroup(IngestMeta{TalkgroupLabel: "Law"}); ok {
		t.Error("ResolveTalkgroup reported a talkgroup for a claim that named only a label, want none")
	}

	// A reference the system does not have resolves nothing, even beside a label one of its
	// talkgroups carries.
	if _, ok := system.ResolveTalkgroup(IngestMeta{TalkgroupRef: 300, TalkgroupLabel: "Law"}); ok {
		t.Error("ResolveTalkgroup reported a talkgroup for a reference the system does not have, want none")
	}

	if _, ok := system.ResolveTalkgroup(IngestMeta{}); ok {
		t.Error("ResolveTalkgroup with no claim reported a talkgroup, want none")
	}
}

func TestShouldCreateTalkgroupRequiresAReferenceAndAutoPopulate(t *testing.T) {
	auto := DefaultOptions()

	if !ShouldCreateTalkgroup(auto, System{}, IngestMeta{TalkgroupRef: 100}) {
		t.Error("with the server-wide auto-populate option, a claimed reference must create the talkgroup")
	}

	manual := DefaultOptions()
	manual.AutoPopulate = false

	if ShouldCreateTalkgroup(manual, System{}, IngestMeta{TalkgroupRef: 100}) {
		t.Error("with auto-populate off everywhere, the talkgroup must not be created")
	}

	if !ShouldCreateTalkgroup(manual, System{AutoPopulate: true}, IngestMeta{TalkgroupRef: 100}) {
		t.Error("the owning system's own auto-populate flag must be enough on its own")
	}

	if ShouldCreateTalkgroup(auto, System{}, IngestMeta{}) {
		t.Error("a call that claims no talkgroup reference must not create one")
	}
}

func TestGroupLabelsAndTagLabelFallBackToTheDefaults(t *testing.T) {
	if got := (IngestMeta{}).GroupLabels(); !reflect.DeepEqual(got, []string{DefaultGroupLabel}) {
		t.Errorf("GroupLabels = %v, want the default %q group", got, DefaultGroupLabel)
	}

	if got := (IngestMeta{TalkgroupGroup: []string{"Fire", "Law"}}).GroupLabels(); !reflect.DeepEqual(got, []string{"Fire", "Law"}) {
		t.Errorf("GroupLabels = %v, want the claimed labels", got)
	}

	if got := (IngestMeta{}).TagLabel(); got != DefaultTagLabel {
		t.Errorf("TagLabel = %q, want the default %q tag", got, DefaultTagLabel)
	}

	if got := (IngestMeta{TalkgroupTag: "Fire Dispatch"}).TagLabel(); got != "Fire Dispatch" {
		t.Errorf("TagLabel = %q, want the claimed tag", got)
	}
}

func TestNewTalkgroupUsesClaimedLabelsOrPlaceholders(t *testing.T) {
	claim := IngestMeta{
		TalkgroupRef:   1234,
		TalkgroupLabel: "Fire Dispatch",
		TalkgroupName:  "Main Fire Dispatch",
	}

	talkgroup := claim.NewTalkgroup([]uint64{1, 2}, 9)

	if talkgroup.Label != "Fire Dispatch" || talkgroup.Name != "Main Fire Dispatch" {
		t.Errorf("NewTalkgroup = %+v, want the claimed label and name", talkgroup)
	}

	if talkgroup.TalkgroupRef != 1234 || talkgroup.TagID != 9 {
		t.Errorf("NewTalkgroup = %+v, want the claimed reference and tag", talkgroup)
	}

	if want := []uint64{1, 2}; !reflect.DeepEqual(talkgroup.GroupIDs, want) {
		t.Errorf("GroupIDs = %v, want %v", talkgroup.GroupIDs, want)
	}

	placeholders := IngestMeta{TalkgroupRef: 1234}.NewTalkgroup(nil, 0)

	if placeholders.Label != "1234" {
		t.Errorf("Label = %q, want the numeric reference as the placeholder label", placeholders.Label)
	}

	if placeholders.Name != "Talkgroup 1234" {
		t.Errorf("Name = %q, want the generated placeholder name", placeholders.Name)
	}

	if placeholders.GroupIDs == nil || len(placeholders.GroupIDs) != 0 {
		t.Errorf("GroupIDs = %v, want an empty slice, never nil", placeholders.GroupIDs)
	}
}

// The gate 6.6.3 tests is the only condition: the call naming a label, and the server-wide
// option or the owning system's own flag. What the stored label was does not decide it.
func TestShouldRepairTalkgroupLabelsIsTheAutoPopulateGate(t *testing.T) {
	auto := DefaultOptions()
	manual := DefaultOptions()
	manual.AutoPopulate = false

	claim := IngestMeta{TalkgroupRef: 100, TalkgroupLabel: "Fire Dispatch", TalkgroupName: "Main Fire Dispatch"}

	// A talkgroup auto-populated from a previous, unlabeled call is repaired.
	generated := Talkgroup{TalkgroupRef: 100, Label: "100", Name: "Talkgroup 100"}

	if !ShouldRepairTalkgroupLabels(auto, System{}, claim, generated) {
		t.Error("a talkgroup carrying only generated placeholders must be repaired")
	}

	// A talkgroup the administrator configured is renamed too: 6.6.3 renamed it, and a
	// recording naming #TGLBL is the same claim whichever talkgroup it lands on.
	configured := Talkgroup{TalkgroupRef: 100, Label: "Dispatch 1", Name: "Dispatch One"}

	if !ShouldRepairTalkgroupLabels(auto, System{}, claim, configured) {
		t.Error("a labeled call must rename the configured label 6.6.3 renamed")
	}

	// Repairing follows the same auto-populate switches as creating.
	if ShouldRepairTalkgroupLabels(manual, System{}, claim, generated) {
		t.Error("with auto-populate off everywhere, the placeholder must be left alone")
	}

	if ShouldRepairTalkgroupLabels(manual, System{}, claim, configured) {
		t.Error("with auto-populate off everywhere, the configured label must be left alone")
	}

	if !ShouldRepairTalkgroupLabels(manual, System{AutoPopulate: true}, claim, configured) {
		t.Error("the owning system's own auto-populate flag must allow the repair")
	}

	// A call that claims no label has nothing to repair with.
	if ShouldRepairTalkgroupLabels(auto, System{}, IngestMeta{TalkgroupRef: 100}, generated) {
		t.Error("a call with no claimed label must not trigger a repair")
	}

	if ShouldRepairTalkgroupLabels(auto, System{}, IngestMeta{TalkgroupRef: 100}, configured) {
		t.Error("a call with no claimed label must not touch a configured talkgroup either")
	}
}

func TestRepairedTalkgroupRenamesTheLabelAndKeepsAConfiguredName(t *testing.T) {
	auto := DefaultOptions()

	generated := Talkgroup{TalkgroupRef: 100, Label: "100", Name: "Talkgroup 100"}

	repaired, changed := RepairedTalkgroup(auto, System{}, IngestMeta{
		TalkgroupRef:   100,
		TalkgroupLabel: "Fire Dispatch",
		TalkgroupName:  "Main Fire Dispatch",
	}, generated)

	if !changed {
		t.Fatal("RepairedTalkgroup reported no change, want a repaired talkgroup")
	}

	if repaired.Label != "Fire Dispatch" || repaired.Name != "Main Fire Dispatch" {
		t.Errorf("repaired = %+v, want both placeholders replaced by the claimed values", repaired)
	}

	// A claimed label with no claimed name adopts the label for the generated name.
	repaired, _ = RepairedTalkgroup(auto, System{}, IngestMeta{TalkgroupRef: 100, TalkgroupLabel: "Fire Dispatch"}, generated)

	if repaired.Name != "Fire Dispatch" {
		t.Errorf("Name = %q, want the claimed label adopted for the generated name", repaired.Name)
	}

	// A configured name survives a label repair.
	halfConfigured := Talkgroup{TalkgroupRef: 100, Label: "100", Name: "Dispatch One"}

	repaired, _ = RepairedTalkgroup(auto, System{}, IngestMeta{TalkgroupRef: 100, TalkgroupLabel: "Fire Dispatch"}, halfConfigured)

	if repaired.Name != "Dispatch One" {
		t.Errorf("Name = %q, want the configured name preserved", repaired.Name)
	}

	// A configured label is renamed, and its configured name is not.
	configured := Talkgroup{TalkgroupRef: 100, Label: "Dispatch 1", Name: "Dispatch One"}

	renamed, changed := RepairedTalkgroup(auto, System{}, IngestMeta{TalkgroupRef: 100, TalkgroupLabel: "Fire Dispatch"}, configured)

	if !changed {
		t.Error("RepairedTalkgroup reported no change on a talkgroup whose stored label the call named differently")
	}

	if renamed.Label != "Fire Dispatch" {
		t.Errorf("Label = %q, want the label the call named", renamed.Label)
	}

	if renamed.Name != configured.Name || renamed.TalkgroupRef != configured.TalkgroupRef {
		t.Errorf("renamed = %+v, want the configured name and the reference kept", renamed)
	}

	// A claimed name replaces the stored name whenever it differs, which is how 6.6.3 wrote it,
	// and it does so without turning a repeated label into a rename of its own.
	renamed, changed = RepairedTalkgroup(auto, System{}, IngestMeta{
		TalkgroupRef:   100,
		TalkgroupLabel: "Dispatch 1",
		TalkgroupName:  "Fire Dispatch",
	}, configured)

	if !changed {
		t.Error("RepairedTalkgroup reported no change on a talkgroup whose stored name the call named differently")
	}

	if renamed.Label != "Dispatch 1" {
		t.Errorf("Label = %q, want the stored label the call repeated left as it was", renamed.Label)
	}

	if renamed.Name != "Fire Dispatch" {
		t.Errorf("Name = %q, want the name the call named", renamed.Name)
	}

	// With no name in the claim, the placeholder condition is what decides the name: a configured
	// one stands, and a claim equal to what is stored is no change at all.
	renamed, changed = RepairedTalkgroup(auto, System{}, IngestMeta{
		TalkgroupRef:   100,
		TalkgroupLabel: "Dispatch 1",
	}, configured)

	if changed {
		t.Error("RepairedTalkgroup reported a change on a talkgroup whose stored label and name the call repeated")
	}

	if renamed.Name != "Dispatch One" {
		t.Errorf("Name = %q, want the configured name kept: only a placeholder name is filled from the label", renamed.Name)
	}

	// A placeholder name is repaired by a claimed name, and the claim is what decides it.
	halfGenerated := Talkgroup{TalkgroupRef: 100, Label: "Dispatch 1", Name: "Talkgroup 100"}

	renamed, changed = RepairedTalkgroup(auto, System{}, IngestMeta{
		TalkgroupRef:   100,
		TalkgroupLabel: "Dispatch 1",
		TalkgroupName:  "Fire Dispatch",
	}, halfGenerated)

	if !changed {
		t.Error("RepairedTalkgroup reported no change on a talkgroup carrying a placeholder name the call named " +
			"differently")
	}

	if renamed.Name != "Fire Dispatch" {
		t.Errorf("Name = %q, want the name the call named for the placeholder", renamed.Name)
	}

	// Nothing to repair: a claim the talkgroup already carries changes nothing, so no
	// configuration entry, no re-save, and no log line follow it.
	already := Talkgroup{TalkgroupRef: 100, Label: "Dispatch", Name: "Dispatch"}

	unchanged, changed := RepairedTalkgroup(auto, System{}, IngestMeta{
		TalkgroupRef:   100,
		TalkgroupLabel: "Dispatch",
		TalkgroupName:  "Dispatch",
	}, already)

	if changed {
		t.Error("RepairedTalkgroup reported a change on a talkgroup that already carries what the call named")
	}

	if unchanged.Label != already.Label || unchanged.Name != already.Name || unchanged.TalkgroupRef != already.TalkgroupRef {
		t.Errorf("unchanged = %+v, want the stored talkgroup returned as-is", unchanged)
	}
}

func TestClaimedUnitsPairReferencesWithLabels(t *testing.T) {
	meta := IngestMeta{
		UnitRefs:   []uint{10, 20, 30, 40},
		UnitLabels: []string{"Alpha", "", "Charlie"},
	}

	units := meta.ClaimedUnits()

	if len(units) != 2 {
		t.Fatalf("ClaimedUnits = %+v, want two units", units)
	}

	if units[0].UnitRef != 10 || units[0].Label != "Alpha" {
		t.Errorf("units[0] = %+v, want the Alpha unit", units[0])
	}

	if units[1].UnitRef != 30 || units[1].Label != "Charlie" {
		t.Errorf("units[1] = %+v, want the Charlie unit; an empty label and an unpaired reference are dropped", units[1])
	}

	if got := (IngestMeta{}).ClaimedUnits(); len(got) != 0 {
		t.Errorf("ClaimedUnits with no claim = %+v, want an empty collection", got)
	}
}
