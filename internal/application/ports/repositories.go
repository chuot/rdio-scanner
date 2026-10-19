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

package ports

import (
	"context"
	"errors"
	"time"

	"rdio-scanner/internal/domain"
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
	// earlier one: the calls on the same system, talkgroup and site whose timestamps fall
	// between from and to.
	FindDuplicateCandidates(ctx context.Context, systemID, talkgroupID uint64, siteRef uint, from, to time.Time) ([]domain.Call, error)

	// DeleteCallsBefore removes the recorded calls older than the given instant and
	// reports how many went.
	DeleteCallsBefore(ctx context.Context, before time.Time) (int, error)
}

// CallActivityRepository reads the tallies and the newest rows a status view draws. It is kept apart
// from CallRepository: the history view pages through calls for a caller who has a scope, and the status
// view counts calls for the operator who administers the whole server. Neither should depend on the
// other's method, and neither is handed the audio the history's one full read carries.
type CallActivityRepository interface {
	// CountCallsSince counts the recorded calls at or after the given instant.
	CountCallsSince(ctx context.Context, from time.Time) (uint64, error)

	// CountCallsByHour counts the recorded calls grouped into whole hours, oldest first. An hour with
	// nothing in it is not reported; the query handler fills it.
	CountCallsByHour(ctx context.Context, from time.Time) ([]CallHourCount, error)

	// CountCallsByTalkgroup counts the recorded calls per talkgroup, most first, cut at limit.
	CountCallsByTalkgroup(ctx context.Context, from time.Time, limit uint) ([]CallTalkgroupCount, error)

	// RecentCalls returns the newest recorded calls, newest first, as summaries — never with audio.
	RecentCalls(ctx context.Context, limit uint) ([]CallSummary, error)
}

// CallHourCount is one whole hour of the activity window and how many calls fall in it.
type CallHourCount struct {
	Hour  time.Time
	Count uint64
}

// CallTalkgroupCount is one talkgroup's share of the window, named the way the configuration names it
// so that the status view can show a talkgroup instead of a pair of references.
type CallTalkgroupCount struct {
	SystemRef    uint
	TalkgroupRef uint
	Name         string
	Label        string
	Count        uint64
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
	GroupTalkgroups []domain.TalkgroupScope
	TagTalkgroups   []domain.TalkgroupScope

	// Limit is the resolved page size. It is never left unset by the query handler, and
	// a zero Limit asks for no rows at all, which is what the stored query does with it.
	Limit     uint
	Offset    uint
	Ascending bool
}

// CallSummary is the read model the call history view needs: enough to list a call and
// to fetch it in full, without loading the audio of every call on the page.
type CallSummary struct {
	ID           uint64
	SystemRef    uint
	TalkgroupRef uint
	Timestamp    time.Time

	// AudioLength is how long the call's recording lasts, as stored; zero is the
	// server's own "never measured", which is what every call carries until the
	// length is learned.
	AudioLength time.Duration
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

	// SaveSystemSettings reconciles the stored systems with the given ones and writes
	// nothing under a system. The sites, talkgroups, and units of a system are stored
	// by the save of that level, for the system that level shows, so a save of the
	// system row's own fields leaves every level as the store holds it.
	SaveSystemSettings(ctx context.Context, systems domain.Systems) error

	// SaveSystemSites reconciles the stored sites of one system with the given ones,
	// renumbering their order. A system the store does not have reports ErrNotFound.
	SaveSystemSites(ctx context.Context, systemID uint64, sites domain.Sites) error

	// SaveSystemTalkgroups reconciles the stored talkgroups of one system with the
	// given ones, renumbering their order, and stores the blacklist that was edited
	// beside them in the same save. A nil blacklist leaves the stored text alone. A
	// system the store does not have reports ErrNotFound.
	SaveSystemTalkgroups(ctx context.Context, systemID uint64, talkgroups domain.Talkgroups, blacklists *domain.Blacklists) error

	// SaveSystemUnits reconciles the stored units of one system with the given ones,
	// renumbering their order. A system the store does not have reports ErrNotFound.
	SaveSystemUnits(ctx context.Context, systemID uint64, units domain.Units) error
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

// OptionsRepository holds the server options, including the signing secret.
//
// It is the configuration in effect, not a database view: `ReadOptions` reports what the server
// is currently running with, and `WriteOptions` replaces what it runs with and then persists it.
// A reader that re-queried the table would answer the administrator with what is stored, not
// with what the server is running with, which is the configuration a save has already committed to.
type OptionsRepository interface {
	ReadOptions(ctx context.Context) (domain.Options, error)
	WriteOptions(ctx context.Context, options domain.Options) error

	// ReloadOptions re-derives the options from what is stored and makes that what the server
	// runs with. The legacy server does this in exactly one place — after changing the
	// administrator password — and it is not a no-op: re-deriving resets every option to its
	// default and then applies the stored rows, so an option stored under a name the reader does
	// not look for comes back as the default.
	ReloadOptions(ctx context.Context) (domain.Options, error)
}

// SigningSecretStore puts the signing secret the server issues its administrator sessions with on
// record.
//
// It is its own port rather than another method on OptionsRepository: the startup step that needs it
// reads the configuration and writes one value back, and it has no business with saving or
// re-deriving the rest of it. The options adapter satisfies both.
type SigningSecretStore interface {
	StoreSecret(ctx context.Context, secret string) error
}

// LogSearchPage is the log view's envelope: the total number of entries the criteria
// matches, the span of time everything that matches covers, and the matching entries
// themselves.
type LogSearchPage struct {
	Count uint64

	// DateStart and DateStop are the earliest and latest instants among everything the criteria
	// match, and not among the entries this page carries. The log view pages through the span, so
	// the span has to stay where it is while the page moves.
	//
	// A search that matches nothing leaves both ends unset, which is not the same as the Unix epoch:
	// the log view shows the instant the page reports, and an empty log view has always shown the
	// earliest instant a clock can name.
	DateStart time.Time
	DateStop  time.Time

	// SourceCounts says how many of the matching entries each part of the server wrote, ordered by
	// descending count, so the log view can show where the noise is before the operator narrows to it.
	//
	// They describe everything the criteria match and not the page this envelope carries, and they are
	// computed with the criteria as submitted except for the source itself: asking "how many of each"
	// while already filtered to one source answers one row, which is true and useless. So the search
	// reports two totals — `Count`, the narrowed match the page is paged through, and the total of the
	// wider match these counts describe — and they are the same number whenever no source filter is
	// submitted.
	SourceCounts []LogSourceCount

	Results []domain.LogEntry
}

// LogSourceCount is how many entries one part of the server wrote among everything a search matches.
type LogSourceCount struct {
	Source domain.LogSource
	Count  uint64
}

// LogRepository stores recorded server events.
type LogRepository interface {
	AppendLog(ctx context.Context, entry domain.LogEntry) error

	SearchLogs(ctx context.Context, search domain.LogSearch) (LogSearchPage, error)

	// DeleteLogsBefore removes the log entries older than the given instant and reports
	// how many went.
	DeleteLogsBefore(ctx context.Context, before time.Time) (int, error)
}

// LogTallyRepository counts what the server complained about. It is apart from LogRepository for the
// same reason CallActivityRepository is apart from CallRepository: the log view pages and filters
// entries for an operator reading them, and the status view tallies severities for an operator watching
// a page.
type LogTallyRepository interface {
	// TallyLogLevels counts the error and warning entries written at or after the given instant, per
	// severity and per subsystem. A severity with nothing written under it is not reported.
	TallyLogLevels(ctx context.Context, from time.Time) ([]LogLevelTally, error)
}

// LogLevelTally is how many entries one subsystem wrote at one severity over the window.
//
// The severity is the plain string the domain carries it as (internal/domain/log.go:177): the three
// names are the constants the log view filters on, and a level is not a thing with its own identity.
type LogLevelTally struct {
	Level  string
	Source domain.LogSource
	Count  uint64
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
