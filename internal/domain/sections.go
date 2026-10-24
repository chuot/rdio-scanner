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

// The seven configuration sections that are lists of entries are saved by replacing the whole
// list: what the server runs with afterwards is what was submitted, entry for entry. These
// parsers are the entry point for a submitted list; the save semantics every section shares
// are the two rules below — sectionEntries and fresh-entry apply — and live in the domain note.

// ParseAccesses builds the access codes from the list an administrator submitted.
func ParseAccesses(value any) Accesses {
	accesses := Accesses{}

	for _, entry := range sectionEntries(value) {
		accesses = append(accesses, NewAccess().ApplyMap(entry))
	}

	return accesses
}

// ParseApikeys builds the upload keys from the list an administrator submitted.
func ParseApikeys(value any) Apikeys {
	apikeys := Apikeys{}

	for _, entry := range sectionEntries(value) {
		apikeys = append(apikeys, NewApikey().ApplyMap(entry))
	}

	return apikeys
}

// ParseDirwatches builds the watched directories from the list an administrator submitted.
func ParseDirwatches(value any) Dirwatches {
	dirwatches := Dirwatches{}

	for _, entry := range sectionEntries(value) {
		dirwatches = append(dirwatches, NewDirwatch().ApplyMap(entry))
	}

	return dirwatches
}

// ParseDownstreams builds the downstream servers from the list an administrator submitted.
func ParseDownstreams(value any) Downstreams {
	downstreams := Downstreams{}

	for _, entry := range sectionEntries(value) {
		downstreams = append(downstreams, Downstream{}.ApplyMap(entry))
	}

	return downstreams
}

// ParseGroups builds the talkgroup groups from the list an administrator submitted.
func ParseGroups(value any) Groups {
	groups := Groups{}

	for _, entry := range sectionEntries(value) {
		groups = append(groups, Group{}.ApplyMap(entry))
	}

	return groups
}

// ParseSystems builds the systems — and the talkgroups, sites, and units each of them carries — from the
// list an administrator submitted.
func ParseSystems(value any) Systems {
	systems := Systems{}

	for _, entry := range sectionEntries(value) {
		systems = append(systems, System{}.ApplyMap(entry))
	}

	return systems
}

// ParseTags builds the talkgroup tags from the list an administrator submitted.
func ParseTags(value any) Tags {
	tags := Tags{}

	for _, entry := range sectionEntries(value) {
		tags = append(tags, Tag{}.ApplyMap(entry))
	}

	return tags
}

// ParseSites builds the sites of one system from the list an administrator submitted.
func ParseSites(value any) Sites {
	sites := Sites{}

	for _, entry := range sectionEntries(value) {
		sites = append(sites, Site{}.ApplyMap(entry))
	}

	return sites
}

// ParseTalkgroups builds the talkgroups of one system from the list an administrator
// submitted. The empty group list is kept non-nil, as every talkgroup on the wire is.
func ParseTalkgroups(value any) Talkgroups {
	talkgroups := Talkgroups{}

	for _, entry := range sectionEntries(value) {
		talkgroups = append(talkgroups, Talkgroup{GroupIDs: []uint64{}}.ApplyMap(entry))
	}

	return talkgroups
}

// ParseUnits builds the units of one system from the list an administrator submitted.
func ParseUnits(value any) Units {
	units := Units{}

	for _, entry := range sectionEntries(value) {
		units = append(units, Unit{}.ApplyMap(entry))
	}

	return units
}

// sectionEntries is the rule every list section shares: the submitted value is a list of
// entries, each entry is a set of fields, and anything that is neither is not part of the
// submission. A value that is not a list yields no entries and saves nothing for that
// section; an empty list is a submission that replaces the section with nothing.
func sectionEntries(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}

	entries := []map[string]any{}

	for _, item := range items {
		if entry, ok := item.(map[string]any); ok {
			entries = append(entries, entry)
		}
	}

	return entries
}
