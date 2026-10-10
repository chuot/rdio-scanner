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
	"reflect"
	"testing"
)

func TestTalkgroupApplyMapReadsOrderFromTheOrderKeyOnly(t *testing.T) {
	fromOrder := Talkgroup{}.ApplyMap(map[string]any{"talkgroupRef": float64(100), "order": float64(7)})

	if fromOrder.Order != 7 {
		t.Errorf("Order = %d, want 7 read from the order key", fromOrder.Order)
	}

	// Quirk Q4: the legacy server emits the order under "talkgroup" but never reads
	// it back from that key. Reproduced verbatim.
	fromTalkgroupKey := Talkgroup{}.ApplyMap(map[string]any{"talkgroupRef": float64(100), "talkgroup": float64(7)})

	if fromTalkgroupKey.Order != 0 {
		t.Errorf("Order = %d, want 0: the legacy server ignores the talkgroup key on read", fromTalkgroupKey.Order)
	}
}

func TestTalkgroupApplyMapReadsGroupIds(t *testing.T) {
	talkgroup := Talkgroup{}.ApplyMap(map[string]any{
		"groupIds": []any{float64(1), "not a number", float64(2)},
		"tagId":    float64(9),
		"type":     "chat",
	})

	if want := []uint64{1, 2}; !reflect.DeepEqual(talkgroup.GroupIDs, want) {
		t.Errorf("GroupIDs = %v, want %v with non-numeric entries dropped", talkgroup.GroupIDs, want)
	}

	if talkgroup.TagID != 9 || talkgroup.Kind != "chat" {
		t.Errorf("ApplyMap = %+v, want tagId 9 and type chat", talkgroup)
	}
}

func TestTalkgroupGeneratedPlaceholders(t *testing.T) {
	talkgroup := Talkgroup{TalkgroupRef: 1234}

	if talkgroup.GeneratedLabel() != "1234" {
		t.Errorf("GeneratedLabel = %q, want 1234", talkgroup.GeneratedLabel())
	}

	if talkgroup.GeneratedName() != "Talkgroup 1234" {
		t.Errorf("GeneratedName = %q, want Talkgroup 1234", talkgroup.GeneratedName())
	}

	if !talkgroup.HasGeneratedLabel() || !talkgroup.HasGeneratedName() {
		t.Error("an empty label and name must count as generated placeholders")
	}

	talkgroup.Label = "1234"
	talkgroup.Name = "Talkgroup 1234"

	if !talkgroup.HasGeneratedLabel() || !talkgroup.HasGeneratedName() {
		t.Error("labels equal to the generated placeholder must count as generated")
	}

	talkgroup.Label = "Fire Dispatch"
	talkgroup.Name = "Main Fire Dispatch"

	if talkgroup.HasGeneratedLabel() || talkgroup.HasGeneratedName() {
		t.Error("a configured label and name must not count as generated")
	}
}
