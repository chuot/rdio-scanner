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
	"strings"
	"testing"
)

func TestNewDirwatchCarriesTheLegacyDefaults(t *testing.T) {
	dirwatch := NewDirwatch()

	if dirwatch.Delay != 2000 {
		t.Errorf("Delay = %d, want the legacy default of 2000 ms", dirwatch.Delay)
	}

	if !dirwatch.DeleteAfter {
		t.Error("DeleteAfter must default to true")
	}

	if dirwatch.Kind != DirwatchTypeDefault {
		t.Errorf("Kind = %q, want the legacy default %q", dirwatch.Kind, DirwatchTypeDefault)
	}
}

// TestDirwatchApplyMapReadsTheTypeKey pins the key drift: the administrator payload
// names the parser "type", while the server calls it Kind everywhere else.
func TestDirwatchApplyMapReadsTheTypeKey(t *testing.T) {
	dirwatch := NewDirwatch().ApplyMap(map[string]any{
		"id":          float64(2),
		"delay":       float64(500),
		"deleteAfter": false,
		"directory":   "/var/spool/dsdplus",
		"disabled":    true,
		"extension":   ".wav",
		"frequency":   float64(100),
		"type":        DirwatchTypeDSDPlus,
		"mask":        "{TYPE} {TGID} {UID} {TIME}",
		"order":       float64(3),
		"siteId":      float64(11),
		"systemId":    float64(7),
		"talkgroupId": float64(21),
	})

	if dirwatch.Kind != DirwatchTypeDSDPlus {
		t.Errorf("Kind = %q, want %q read from the type key", dirwatch.Kind, DirwatchTypeDSDPlus)
	}

	// The delay submitted here is under the day-long ceiling so that this test
	// can keep asking the question it asks: that every submitted field is carried through.
	// The clamp itself is the test next to this one.
	if dirwatch.ID != 2 || dirwatch.Delay != 500 || dirwatch.DeleteAfter || !dirwatch.Disabled ||
		dirwatch.Directory != "/var/spool/dsdplus" || dirwatch.Extension != ".wav" || dirwatch.Frequency != 100 ||
		dirwatch.Mask != "{TYPE} {TGID} {UID} {TIME}" || dirwatch.Order != 3 ||
		dirwatch.SiteID != 11 || dirwatch.SystemID != 7 || dirwatch.TalkgroupID != 21 {
		t.Errorf("ApplyMap = %+v, want every submitted field carried through", dirwatch)
	}
}

func TestDirwatchWatchesNeedsADirectoryAndNotBeingDisabled(t *testing.T) {
	if !(Dirwatch{Directory: "/tmp/x"}).Watches() {
		t.Error("an enabled watch with a directory must be watched")
	}

	if (Dirwatch{}).Watches() {
		t.Error("a watch with no directory watches nothing")
	}

	if (Dirwatch{Directory: "/tmp/x", Disabled: true}).Watches() {
		t.Error("a disabled watch must not be watched")
	}
}

func TestDirwatchesApplyListReplacesTheSetAndKeepsDefaults(t *testing.T) {
	previous := Dirwatches{Dirwatch{Directory: "/gone"}, Dirwatch{Directory: "/kept"}}

	next := previous.ApplyList([]any{
		map[string]any{"directory": "/kept"},
		map[string]any{"directory": "/new", "type": DirwatchTypeTrunkRecorder},
		"not a watch",
	})

	if len(next) != 2 {
		t.Fatalf("ApplyList produced %d watches, want the two usable entries of the payload", len(next))
	}

	if next[0].Directory != "/kept" || next[1].Directory != "/new" {
		t.Errorf("ApplyList = %+v, want the set replaced by the payload", next)
	}

	if next[0].Delay != 2000 || next[0].Kind != DirwatchTypeDefault {
		t.Errorf("ApplyList = %+v, want the defaults applied to entries that do not override them", next[0])
	}

	if next[1].Kind != DirwatchTypeTrunkRecorder {
		t.Errorf("Kind = %q, want the submitted parser", next[1].Kind)
	}
}

func TestDirwatchesEnabledSkipsTheUnwatchable(t *testing.T) {
	all := Dirwatches{
		Dirwatch{Directory: "/a"},
		Dirwatch{Directory: "/b", Disabled: true},
		Dirwatch{},
	}

	enabled := all.Enabled()

	if len(enabled) != 1 || enabled[0].Directory != "/a" {
		t.Errorf("Enabled = %+v, want only the enabled watch with a directory", enabled)
	}
}

// TestDirwatchesValidateRefusesAnUncompilableMask: a mask
// that does not compile is stored today and crashes the server from a timer goroutine on the
// next matching file — a panic with no recovery on the server's own clock. The list refuses
// it at apply, where an administrator can be told, not in the dark of a timer.
func TestDirwatchesValidateRefusesAnUncompilableMask(t *testing.T) {
	dirwatches := Dirwatches{
		{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: "(", Extension: "wav"},
	}

	if err := dirwatches.Validate(); err == nil {
		t.Error("Validate = nil, want the uncompilable mask refused: it crashes the server from a timer goroutine on the next matching file")
	}
}

// TestDirwatchesValidateAcceptsAListWithRealMasks is the other half: the rule must not
// refuse the watches the endpoint legitimately carries.
func TestDirwatchesValidateAcceptsAListWithRealMasks(t *testing.T) {
	dirwatches := Dirwatches{
		{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: "#(UNITlbl)", Extension: "wav"},
		{ID: 2, Directory: "/var/spool/other", Kind: "generic", Extension: "m4a"},
	}

	if err := dirwatches.Validate(); err != nil {
		t.Errorf("Validate = %v, want the list accepted", err)
	}
}

// maskTokenTexts are the twenty tokens a watch mask names a filename's pieces by, read from the table
// that replaces them: a token added to that table is a token the save has to answer when an operator
// types it between brackets, and one the watch has to keep reading when its brackets are escaped.
func maskTokenTexts() []string {
	texts := []string{}

	for _, token := range MaskTokens {
		texts = append(texts, token.Text)
	}

	return texts
}

// bracketsAroundTokens are each token wrapped in the brackets an operator types around one. Typed,
// those brackets are a character class, so a mask built from them is a string that parses; the
// tokens then carry their patterns into those brackets, and what the watcher compiles from the mask
// either no longer parses or keeps no capture group for the token it names.
func bracketsAroundTokens() []string {
	bracketed := []string{}

	for _, token := range maskTokenTexts() {
		bracketed = append(bracketed, "["+token+"]")
	}

	return bracketed
}

// escapedBracketsAroundTokens are the same masks with each bracket written escaped, which is how a
// mask says the brackets belong to the file name itself. The escaped brackets survive the
// substitution as the literal characters they are, and each token keeps the capture group it names.
func escapedBracketsAroundTokens() []string {
	escaped := []string{}

	for _, token := range maskTokenTexts() {
		escaped = append(escaped, "\\["+token+"\\]")
	}

	return escaped
}

// TestDirwatchValidateRefusesEachMaskWhoseTokensBreakThePatternTheWatcherCompiles is the door read
// against every token the mask table names: a mask the plain check accepts, and which the watcher
// could not compile once its tokens are replaced, is refused at the save that offered it, with a
// reason naming the mask. Stored, the same mask stops the server from a timer goroutine on the next
// file that reaches the watch.
func TestDirwatchValidateRefusesEachMaskWhoseTokensBreakThePatternTheWatcherCompiles(t *testing.T) {
	for _, mask := range bracketsAroundTokens() {
		dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: mask, Extension: "wav"}

		err := dirwatch.Validate()

		if err == nil {
			t.Errorf("Validate of the mask %q = nil, want it refused: the watcher cannot compile the string its tokens become", mask)

			continue
		}

		if !strings.Contains(err.Error(), mask) {
			t.Errorf("Validate refused the mask %q with %v, want the reason to name the mask", mask, err)
		}
	}
}

// TestDirwatchesValidateRefusesAListCarryingEachMaskWhoseTokensBreakItsPattern is the same rule
// through the list the endpoint applies, which is how the administration page is told.
func TestDirwatchesValidateRefusesAListCarryingEachMaskWhoseTokensBreakItsPattern(t *testing.T) {
	for _, mask := range bracketsAroundTokens() {
		dirwatches := Dirwatches{
			{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: mask, Extension: "wav"},
		}

		err := dirwatches.Validate()

		if err == nil {
			t.Errorf("Validate of a list carrying the mask %q = nil, want the mask refused", mask)

			continue
		}

		if !strings.Contains(err.Error(), mask) {
			t.Errorf("Validate refused the list carrying the mask %q with %v, want the reason to name the mask", mask, err)
		}
	}
}

// TestDirwatchValidateRefusesAMaskWhoseTokensCaptureFewerPiecesThanItNames: this mask compiles
// before and after its tokens are replaced — the brackets sit between two tokens, and one of them
// swallows a token's capture group into a character class — and it names two tokens while capturing
// one piece. A filename's pieces are then lined up with the wrong values, which is a watch reading
// its recordings wrong rather than one that says it cannot read them.
func TestDirwatchValidateRefusesAMaskWhoseTokensCaptureFewerPiecesThanItNames(t *testing.T) {
	dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: "#DATE[#TG]", Extension: "wav"}

	err := dirwatch.Validate()

	if err == nil {
		t.Fatal("Validate of the mask \"#DATE[#TG]\" = nil, want it refused: it captures one piece and names two tokens")
	}

	if !strings.Contains(err.Error(), "#DATE[#TG]") {
		t.Errorf("Validate refused the mask \"#DATE[#TG]\" with %v, want the reason to name the mask", err)
	}
}

// TestDirwatchValidateAcceptsEachMaskThatMeansTheBracketsItCarries keeps the refusal narrow: the
// rule refuses a token that lost the capture group it stands for, not regular-expression
// punctuation. A mask that means the brackets a recording tool wrote into the file name escapes
// them, and stays storable.
func TestDirwatchValidateAcceptsEachMaskThatMeansTheBracketsItCarries(t *testing.T) {
	for _, mask := range escapedBracketsAroundTokens() {
		dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: mask, Extension: "wav"}

		if err := dirwatch.Validate(); err != nil {
			t.Errorf("Validate of the mask %q = %v, want the mask accepted: its escaped brackets are the file name's own", mask, err)
		}
	}
}

// TestDirwatchValidateRefusesAMaskWhoseTokensCaptureFewerPiecesThanItNamesWhereTheBracketsLead is
// the same shape with the brackets at the front of the mask: it compiles before and after its
// tokens are replaced, and it names two tokens while capturing one piece — the watch then files the
// digits the class swallowed as the date, which is a recording read wrong rather than one refused.
func TestDirwatchValidateRefusesAMaskWhoseTokensCaptureFewerPiecesThanItNamesWhereTheBracketsLead(t *testing.T) {
	dirwatch := Dirwatch{ID: 1, Directory: "/var/spool/rdio", Kind: "wild", Mask: "[#TG]-#DATE", Extension: "wav"}

	err := dirwatch.Validate()

	if err == nil {
		t.Fatal("Validate of the mask \"[#TG]-#DATE\" = nil, want it refused: it captures one piece and names two tokens")
	}

	if !strings.Contains(err.Error(), "[#TG]-#DATE") {
		t.Errorf("Validate refused the mask \"[#TG]-#DATE\" with %v, want the reason to name the mask", err)
	}
}

// TestDirwatchDelayOverTheDayIsClamped: the legacy minute counter wraps
// silently at its ceiling, and a watch naming a week's hold is a watch nobody configured.
// A delay over one day is clamped to one day at apply.
func TestDirwatchDelayOverTheDayIsClamped(t *testing.T) {
	dirwatch := NewDirwatch().ApplyMap(map[string]any{"delay": float64(24*60 + 60)})

	if dirwatch.Delay != 24*60 {
		t.Errorf("delay = %d minutes, want the clamp at 24 hours", dirwatch.Delay)
	}

	// A delay under the ceiling is the administrator's own number, untouched.
	kept := NewDirwatch().ApplyMap(map[string]any{"delay": float64(24 * 60)})

	if kept.Delay != 24*60 {
		t.Errorf("delay = %d minutes, want 1440 kept as named", kept.Delay)
	}
}

// TestDirwatchesValidateRefusesAPathWalkingExtension: the
// extension composes filesystem paths, so a name carrying a slash, a backslash, a dot, or
// ".." is refused at apply.
func TestDirwatchesValidateRefusesAPathWalkingExtension(t *testing.T) {
	for _, extension := range []string{"ws/../wav", "a/b", "a\\b", "..", "wav.old", "wav x"} {
		dirwatches := Dirwatches{{ID: 1, Directory: "/var/spool/rdio", Extension: extension}}

		if err := dirwatches.Validate(); err == nil {
			t.Errorf("extension %q was accepted, want it refused: it is a path walk wearing an extension", extension)
		}
	}
}
