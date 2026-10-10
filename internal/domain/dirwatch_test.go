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

package domain

import (
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
		"delay":       float64(5000),
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

	if dirwatch.ID != 2 || dirwatch.Delay != 5000 || dirwatch.DeleteAfter || !dirwatch.Disabled ||
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
