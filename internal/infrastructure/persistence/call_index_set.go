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

package persistence

// indexSpec names one secondary index: the name it is created under, the table it
// sits on, and the columns it carries in the order it carries them. A key is
// identified by its table and column list rather than by its name, because the
// schema's foreign keys make an index of their own over several of these column
// lists under names of the engine's choosing.
type indexSpec struct {
	name    string
	table   string
	columns []string
}

// callIndexSet is the set of secondary indexes the call history reads are written
// against, in the order the set is documented. It is the one list the schema copies,
// the startup pass and the tests all read, so a key cannot exist in one of them and be
// missing from another.
//
// The keys over a foreign key's own column serve the child lookups a call detail read
// performs, the joins that scope a history page to a system, a tag or a group, and the
// duplicate guard and drop that the delayed table takes part in.
//
// The covering key leads with the timestamp every history read orders on and carries the
// columns those reads report, so an unfiltered page answers from the index without reading
// a table row. It is the one member the copied schema does not declare: it indexes the
// recording length column, which a database the legacy server wrote gains only after that
// copy has been applied, and an index created over a column the table does not have yet is
// refused outright on PostgreSQL and stored wrong on SQLite. The startup pass creates it
// after that column exists, on every backend.
//
// The talkgroup search path is not indexed by this set: its reads look for a reference or
// a label inside a talkgroup, which is a scan a key over a fixed column list would not
// serve.
var callIndexSet = []indexSpec{
	{name: "callfrequencies_call", table: "callFrequencies", columns: []string{"callId"}},
	{name: "callpatches_call", table: "callPatches", columns: []string{"callId"}},
	{name: "callunits_call", table: "callUnits", columns: []string{"callId"}},
	{name: "delayed_call", table: "delayed", columns: []string{"callId"}},
	{name: "talkgroups_system_ref", table: "talkgroups", columns: []string{"systemId", "talkgroupRef"}},
	{name: "talkgroups_tag", table: "talkgroups", columns: []string{"tagId"}},
	{name: "talkgroupgroups_talkgroup", table: "talkgroupGroups", columns: []string{"talkgroupId"}},
	{name: "talkgroupgroups_group", table: "talkgroupGroups", columns: []string{"groupId"}},
	{name: "sites_system", table: "sites", columns: []string{"systemId"}},
	{name: "calls_timestamp_cover", table: "calls", columns: []string{"timestamp", "callId", "audioLength", "systemId", "talkgroupId"}},
}
