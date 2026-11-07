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

package dto

import (
	"encoding/json"
	"sort"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// ConfigPayload is the configuration one listener is allowed to see, built from
// what that listener's access reaches rather than from the whole server.
type ConfigPayload struct {
	Snapshot ports.ConfigSnapshot
	Scope    domain.AccessScope
}

// MarshalJSON writes the configuration.
//
// All keys are map keys, so they come out sorted, as the captures compare them.
func (payload ConfigPayload) MarshalJSON() ([]byte, error) {
	options := payload.Snapshot.Options

	view := ordered(payload.Snapshot.Systems.Scoped(payload.Scope, payload.Snapshot.Tags), options.SortTalkgroups)

	systems := visibleSystems(view, payload.Snapshot.Groups, payload.Snapshot.Tags)

	body := map[string]any{
		"alerts":             alertTones,
		"branding":           options.Branding,
		"email":              options.Email,
		"groups":             groupsMap(systems, payload.Snapshot.Groups),
		"groupsData":         groupList(systems, payload.Snapshot.Groups),
		"keypadBeeps":        keypadBeeps(options.KeypadBeeps),
		"showListenersCount": options.ShowListenersCount,
		"systems":            systemEntries(systems),
		"tags":               tagsMap(systems, payload.Snapshot.Tags),
		"tagsData":           tagList(systems, payload.Snapshot.Tags),
	}

	return json.Marshal(body)
}

// visibleSystem is one system as a listener sees it, plus what the group and
// tag listings need and the entry itself does not carry.
type visibleSystem struct {
	Entry      map[string]any
	SystemRef  uint
	Talkgroups []visibleTalkgroup
}

type visibleTalkgroup struct {
	Entry map[string]any

	Ref uint

	// Group is the talkgroup's first label, the one its entry names and the
	// group listing is built from. Groups is every label it has, and the group
	// map is built from. A talkgroup in three groups puts all three in the map
	// and only the first in the listing.
	Group  string
	Groups []string

	Tag string
}

// visibleSystems turns the systems one listener may see into the entries that listener receives.
func visibleSystems(view domain.Systems, groups domain.Groups, tags domain.Tags) []visibleSystem {
	out := []visibleSystem{}

	for _, system := range view {
		talkgroups := []visibleTalkgroup{}

		for _, talkgroup := range system.Talkgroups {
			// A talkgroup whose tag the server no longer knows is not shown at all.
			tag, ok := tags.ByID(talkgroup.TagID)
			if !ok {
				continue
			}

			labels := []string{}

			for _, id := range talkgroup.GroupIDs {
				if group, ok := groups.ByID(id); ok {
					labels = append(labels, group.Label)
				}
			}

			group := ""

			if len(labels) > 0 {
				group = labels[0]
			}

			talkgroups = append(talkgroups, visibleTalkgroup{
				Entry: map[string]any{
					"id":        talkgroup.TalkgroupRef,
					"alert":     talkgroup.Alert,
					"frequency": talkgroup.Frequency,
					"group":     group,
					"groups":    labels,
					"label":     talkgroup.Label,
					"led":       talkgroup.LED,
					"name":      talkgroup.Name,
					"order":     talkgroup.Order,
					"tag":       tag.Label,
					"type":      talkgroup.Kind,
				},
				Ref:    talkgroup.TalkgroupRef,
				Group:  group,
				Groups: labels,
				Tag:    tag.Label,
			})
		}

		out = append(out, visibleSystem{
			Entry: map[string]any{
				"id":         system.SystemRef,
				"alert":      system.Alert,
				"label":      system.Label,
				"led":        system.LED,
				"order":      system.Order,
				"talkgroups": talkgroupEntries(talkgroups),
				"units":      unitEntries(system.Units),
				"type":       system.Kind,
			},
			SystemRef:  system.SystemRef,
			Talkgroups: talkgroups,
		})
	}

	return out
}

// ordered puts systems in their display order and their talkgroups in theirs,
// numbering them by the talkgroups option. Lists are copied first.
func ordered(systems domain.Systems, sortTalkgroups bool) domain.Systems {
	out := append(domain.Systems{}, systems...)

	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })

	for i := range out {
		talkgroups := append(domain.Talkgroups{}, out[i].Talkgroups...)

		if sortTalkgroups {
			sort.SliceStable(talkgroups, func(a, b int) bool { return talkgroups[a].Label < talkgroups[b].Label })

			for j := range talkgroups {
				talkgroups[j].Order = uint(j + 1)
			}
		} else {
			sort.SliceStable(talkgroups, func(a, b int) bool { return talkgroups[a].Order < talkgroups[b].Order })
		}

		out[i].Talkgroups = talkgroups
	}

	return out
}

func systemEntries(systems []visibleSystem) []map[string]any {
	out := make([]map[string]any, 0, len(systems))

	for _, system := range systems {
		out = append(out, system.Entry)
	}

	return out
}

func talkgroupEntries(talkgroups []visibleTalkgroup) []map[string]any {
	out := make([]map[string]any, 0, len(talkgroups))

	for _, talkgroup := range talkgroups {
		out = append(out, talkgroup.Entry)
	}

	return out
}

// unitEntries writes the units a system declares. The identifier on the wire
// is the unit reference, written twice, not the server's number.
func unitEntries(units domain.Units) []map[string]any {
	out := []map[string]any{}

	for _, unit := range units {
		entry := map[string]any{
			"id":    unit.UnitRef,
			"label": unit.Label,
		}

		if unit.Order > 0 {
			entry["order"] = unit.Order
		}

		if unit.UnitRef > 0 {
			entry["unitRef"] = unit.UnitRef
		}

		if unit.UnitFrom > 0 {
			entry["unitFrom"] = unit.UnitFrom
		}

		if unit.UnitTo > 0 {
			entry["unitTo"] = unit.UnitTo
		}

		out = append(out, entry)
	}

	return out
}

// groupList is the groups this listener's talkgroups use, in first-seen
// order, each named once, including groups that are only ever a talkgroup's
// second group.
func groupList(systems []visibleSystem, groups domain.Groups) []map[string]any {
	out := []domain.Group{}

	for _, system := range systems {
		for _, talkgroup := range system.Talkgroups {
			for _, label := range talkgroup.Groups {
				group, ok := groups.ByLabel(label)
				if !ok {
					continue
				}

				if !containsGroup(out, group) {
					out = append(out, group)
				}
			}
		}
	}

	return groupEntries(out)
}

func groupsMap(systems []visibleSystem, groups domain.Groups) map[string]map[uint][]uint {
	out := map[string]map[uint][]uint{}

	for _, system := range systems {
		for _, talkgroup := range system.Talkgroups {
			for _, label := range talkgroup.Groups {
				group, ok := groups.ByLabel(label)
				if !ok {
					continue
				}

				bySystem, ok := out[group.Label]
				if !ok {
					bySystem = map[uint][]uint{}
					out[group.Label] = bySystem
				}

				refs, ok := bySystem[system.SystemRef]
				if !ok {
					refs = []uint{}
				}

				if !containsRef(refs, talkgroup.Ref) {
					refs = append(refs, talkgroup.Ref)
				}

				bySystem[system.SystemRef] = refs
			}
		}
	}

	return out
}

func tagList(systems []visibleSystem, tags domain.Tags) []map[string]any {
	out := []domain.Tag{}

	for _, system := range systems {
		for _, talkgroup := range system.Talkgroups {
			tag, ok := tags.ByLabel(talkgroup.Tag)
			if !ok {
				continue
			}

			if !containsTag(out, tag) {
				out = append(out, tag)
			}
		}
	}

	return tagEntries(out)
}

func tagsMap(systems []visibleSystem, tags domain.Tags) map[string]map[uint][]uint {
	out := map[string]map[uint][]uint{}

	for _, system := range systems {
		for _, talkgroup := range system.Talkgroups {
			tag, ok := tags.ByLabel(talkgroup.Tag)
			if !ok {
				continue
			}

			bySystem, ok := out[tag.Label]
			if !ok {
				bySystem = map[uint][]uint{}
				out[tag.Label] = bySystem
			}

			refs, ok := bySystem[system.SystemRef]
			if !ok {
				refs = []uint{}
			}

			if !containsRef(refs, talkgroup.Ref) {
				refs = append(refs, talkgroup.Ref)
			}

			bySystem[system.SystemRef] = refs
		}
	}

	return out
}

func groupEntries(groups []domain.Group) []map[string]any {
	out := []map[string]any{}

	for _, group := range groups {
		entry := map[string]any{
			"id":    group.ID,
			"label": group.Label,
		}

		if group.Alert != "" {
			entry["alert"] = group.Alert
		}

		if group.LED != "" {
			entry["led"] = group.LED
		}

		if group.Order > 0 {
			entry["order"] = group.Order
		}

		out = append(out, entry)
	}

	return out
}

func tagEntries(tags []domain.Tag) []map[string]any {
	out := []map[string]any{}

	for _, tag := range tags {
		entry := map[string]any{
			"id":    tag.ID,
			"label": tag.Label,
		}

		if tag.Alert != "" {
			entry["alert"] = tag.Alert
		}

		if tag.LED != "" {
			entry["led"] = tag.LED
		}

		if tag.Order > 0 {
			entry["order"] = tag.Order
		}

		out = append(out, entry)
	}

	return out
}

func containsGroup(list []domain.Group, want domain.Group) bool {
	for _, group := range list {
		if group == want {
			return true
		}
	}

	return false
}

func containsTag(list []domain.Tag, want domain.Tag) bool {
	for _, tag := range list {
		if tag == want {
			return true
		}
	}

	return false
}

func containsRef(list []uint, want uint) bool {
	for _, ref := range list {
		if ref == want {
			return true
		}
	}

	return false
}
