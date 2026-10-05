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

package ports

import (
	"context"
	"errors"
	"time"

	"rdio-scanner/server-ng/internal/domain"
)

// ErrNotFound is returned by a repository when the thing asked for is not there. It is
// declared here so that the use cases can recognise a missing call, system, or access
// without knowing which database produced the answer.
var ErrNotFound = errors.New("not found")

// CallRepository stores and reads recorded calls.
type CallRepository interface {
	// GetCall returns one recorded call, including its audio, or ErrNotFound.
	GetCall(ctx context.Context, id uint64) (domain.Call, error)

	// SaveCall stores a call and returns it with its assigned identity.
	SaveCall(ctx context.Context, call domain.Call) (domain.Call, error)

	// SearchCalls returns one page of the call history, together with the total number
	// of calls the criteria matches and the span of their timestamps.
	SearchCalls(ctx context.Context, search CallSearch) (CallSearchPage, error)

	// FindDuplicateCandidates returns the calls that could make a call a duplicate of an
	// earlier one: the calls on the same system and talkgroup whose timestamps fall
	// between from and to.
	FindDuplicateCandidates(ctx context.Context, systemID, talkgroupID uint64, from, to time.Time) ([]domain.Call, error)

	// DeleteCallsBefore removes the recorded calls older than the given instant and
	// reports how many went.
	DeleteCallsBefore(ctx context.Context, before time.Time) (int, error)
}

// CallSearch is the resolved criteria a call-history search runs with. The presentation
// layer submits raw criteria, the query handler resolves them — paging, sort direction,
// the caller's access scope, and group and tag labels expanded into concrete
// talkgroups — and the repository turns this into a query.
type CallSearch struct {
	// Scope restricts the search to what the caller is allowed to see. An absent scope
	// means the caller sees everything.
	Scope domain.AccessScope

	// From and To bound the timestamps to search, inclusive. A zero From leaves the
	// history unbounded.
	From time.Time
	To   time.Time

	// SystemRef and TalkgroupRef narrow the search to one talkgroup on one system. A
	// zero SystemRef leaves the search unfiltered; a zero TalkgroupRef with a system
	// means every talkgroup on that system.
	SystemRef    uint
	TalkgroupRef uint

	// GroupTalkgroups and TagTalkgroups are the talkgroups a group label or tag label
	// stands for, expanded by the query handler into the pairs the history is filtered
	// by.
	GroupTalkgroups []ScopedTalkgroups
	TagTalkgroups   []ScopedTalkgroups

	// Limit is the resolved page size. It is never left unset by the query handler, and
	// a zero Limit asks for no rows at all, which is what the stored query does with it.
	Limit     uint
	Offset    uint
	Ascending bool
}

// ScopedTalkgroups is the set of talkgroups on one system a search criterion stands for.
type ScopedTalkgroups struct {
	SystemRef     uint
	TalkgroupRefs []uint
}

// CallSummary is the read model the call history view needs: enough to list a call and
// to fetch it in full, without loading the audio of every call on the page.
type CallSummary struct {
	ID           uint64
	SystemRef    uint
	TalkgroupRef uint
	Timestamp    time.Time
}

// CallSearchPage is one page of call history.
//
// Count is the number of calls the criteria matches in total, not the number on this
// page. DateStart and DateStop are the earliest and latest timestamps the criteria
// matches across the whole history, which is what the history view uses to offer its
// day navigation.
type CallSearchPage struct {
	Count     uint
	DateStart time.Time
	DateStop  time.Time
	Results   []CallSummary
}

// SystemRepository stores systems together with the talkgroups, sites, and units that
// belong to them, because a system is the aggregate those parts are reached through.
type SystemRepository interface {
	// AllSystems returns every system with its talkgroups, sites, and units loaded.
	AllSystems(ctx context.Context) (domain.Systems, error)

	// SaveSystems reconciles the stored systems with the given ones: systems that are
	// not in the list are removed, and the rest are inserted or updated along with
	// their talkgroups, sites, and units.
	SaveSystems(ctx context.Context, systems domain.Systems) error
}

// GroupRepository stores the talkgroup groups the administrator configures.
type GroupRepository interface {
	AllGroups(ctx context.Context) (domain.Groups, error)

	// SaveGroups reconciles the stored groups with the given ones.
	SaveGroups(ctx context.Context, groups domain.Groups) error
}

// TagRepository stores the talkgroup tags the administrator configures.
type TagRepository interface {
	AllTags(ctx context.Context) (domain.Tags, error)

	// SaveTags reconciles the stored tags with the given ones.
	SaveTags(ctx context.Context, tags domain.Tags) error
}

// AccessRepository stores the listener accesses.
type AccessRepository interface {
	AllAccesses(ctx context.Context) (domain.Accesses, error)

	// SaveAccesses reconciles the stored accesses with the given ones.
	SaveAccesses(ctx context.Context, accesses domain.Accesses) error
}

// ApikeyRepository stores the upload credentials.
type ApikeyRepository interface {
	AllApikeys(ctx context.Context) (domain.Apikeys, error)

	// SaveApikeys reconciles the stored apikeys with the given ones.
	SaveApikeys(ctx context.Context, apikeys domain.Apikeys) error
}

// OptionsRepository stores the server options, including the signing secret.
type OptionsRepository interface {
	ReadOptions(ctx context.Context) (domain.Options, error)
	WriteOptions(ctx context.Context, options domain.Options) error
}

// LogSearchPage is the log view's envelope: the total number of entries the criteria
// matches and the matching entries themselves.
type LogSearchPage struct {
	Count   uint64
	Results []domain.LogEntry
}

// LogRepository stores recorded server events.
type LogRepository interface {
	AppendLog(ctx context.Context, entry domain.LogEntry) error

	SearchLogs(ctx context.Context, search domain.LogSearch) (LogSearchPage, error)

	// DeleteLogsBefore removes the log entries older than the given instant and reports
	// how many went.
	DeleteLogsBefore(ctx context.Context, before time.Time) (int, error)
}

// DirwatchRepository stores the watched directories.
type DirwatchRepository interface {
	AllDirwatches(ctx context.Context) (domain.Dirwatches, error)

	// SaveDirwatches reconciles the stored watches with the given ones.
	SaveDirwatches(ctx context.Context, dirwatches domain.Dirwatches) error
}

// DownstreamRepository stores the downstream servers.
type DownstreamRepository interface {
	AllDownstreams(ctx context.Context) (domain.Downstreams, error)

	// SaveDownstreams reconciles the stored downstreams with the given ones.
	SaveDownstreams(ctx context.Context, downstreams domain.Downstreams) error
}

// DelayedCall is a call that has been recorded but is waiting out a talkgroup or system
// delay before it becomes visible.
type DelayedCall struct {
	CallID    uint64
	Timestamp time.Time
}

// DelayedCallRepository stores the calls that are waiting out a delay. A delayed call
// is hidden from the call history until its delay has run out.
type DelayedCallRepository interface {
	// AllDelayed returns every call still waiting, which the server re-arms at startup.
	AllDelayed(ctx context.Context) ([]DelayedCall, error)

	// ScheduleDelayed records that a call is waiting until the given instant.
	ScheduleDelayed(ctx context.Context, callID uint64, until time.Time) error

	// UnscheduledDelayed removes one call from the waiting list once its delay has run.
	UnscheduleDelayed(ctx context.Context, callID uint64) error

	// ClearDelayed drops every waiting call.
	ClearDelayed(ctx context.Context) error
}
