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

// Package fakes holds in-memory implementations of every application port, used by the
// use case tests. They are not mocks: they behave like the real adapters on the rules
// that matter, so a use case tested against them is tested against the rules rather than
// against a scripted answer.
package fakes

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// Calls is an in-memory call repository.
type Calls struct {
	// Stored is the call history, keyed by call identifier. Tests may seed it directly.
	Stored map[uint64]domain.Call

	// NextID is the identifier the next stored call gets.
	NextID uint64

	// Saved records the calls that were written, in the order they were written.
	Saved []domain.Call

	// Searched records the resolved criteria each history search ran with, in the order they ran.
	// The paging bounds and the caller's scope are resolved by the query handler, and the endpoint
	// tests need to see what the search actually ran with rather than infer it from a row count.
	Searched []ports.CallSearch

	// Entered, when set, is signalled when a write begins. Tests use it to know that the server is
	// inside a write rather than merely about to start one.
	Entered chan struct{}

	// Hold, when set, makes a write wait until it is closed. Tests use it to keep the server inside a
	// write and see whether it starts another one.
	Hold chan struct{}

	// Err, when set, is returned by every method instead of the stored state.
	Err error
}

var _ ports.CallRepository = (*Calls)(nil)

// NewCalls returns an empty call history.
func NewCalls() *Calls {
	return &Calls{Stored: map[uint64]domain.Call{}, NextID: 1}
}

// GetCall returns a stored call or ports.ErrNotFound.
func (calls *Calls) GetCall(ctx context.Context, id uint64) (domain.Call, error) {
	if calls.Err != nil {
		return domain.Call{}, calls.Err
	}

	call, ok := calls.Stored[id]
	if !ok {
		return domain.Call{}, ports.ErrNotFound
	}

	return call, nil
}

// SaveCall stores a call, assigning it an identifier when it has none.
func (calls *Calls) SaveCall(ctx context.Context, call domain.Call) (domain.Call, error) {
	if calls.Err != nil {
		return domain.Call{}, calls.Err
	}

	if calls.Entered != nil {
		select {
		case calls.Entered <- struct{}{}:

		default:
		}
	}

	if calls.Hold != nil {
		<-calls.Hold
	}

	if call.ID == 0 {
		call.ID = calls.NextID
		calls.NextID++
	}

	calls.Stored[call.ID] = call
	calls.Saved = append(calls.Saved, call)

	return call, nil
}

// FindDuplicateCandidates returns the stored calls on the same system, talkgroup and
// site whose timestamps fall between from and to, inclusive.
func (calls *Calls) FindDuplicateCandidates(ctx context.Context, systemID, talkgroupID uint64, siteRef uint, from, to time.Time) ([]domain.Call, error) {
	if calls.Err != nil {
		return nil, calls.Err
	}

	out := []domain.Call{}

	for _, call := range calls.Stored {
		if call.System.ID != systemID || call.Talkgroup.ID != talkgroupID || call.SiteRef != siteRef {
			continue
		}

		if call.Timestamp.Before(from) || call.Timestamp.After(to) {
			continue
		}

		out = append(out, call)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })

	return out, nil
}

// SearchCalls applies the resolved criteria to the stored history. Delayed calls are
// left out, because a call that is waiting out a delay is not part of the history until
// its delay has run.
func (calls *Calls) SearchCalls(ctx context.Context, search ports.CallSearch) (ports.CallSearchPage, error) {
	calls.Searched = append(calls.Searched, search)

	if calls.Err != nil {
		return ports.CallSearchPage{}, calls.Err
	}

	matching := []domain.Call{}

	for _, call := range calls.Stored {
		if call.Delayed {
			continue
		}

		if !searchMatches(call, search) {
			continue
		}

		matching = append(matching, call)
	}

	sort.Slice(matching, func(i, j int) bool {
		if search.Ascending {
			return matching[i].Timestamp.Before(matching[j].Timestamp)
		}

		return matching[i].Timestamp.After(matching[j].Timestamp)
	})

	// An empty page reports the Unix epoch at both ends, which is what the legacy server's
	// `QueryRow` leaves behind when no call matches and what the captured empty `LCL` frame carries.
	page := ports.CallSearchPage{
		Count:     uint(len(matching)),
		DateStart: time.UnixMilli(0),
		DateStop:  time.UnixMilli(0),
	}

	if len(matching) > 0 {
		page.DateStart, page.DateStop = matching[0].Timestamp, matching[0].Timestamp

		for _, call := range matching[1:] {
			if call.Timestamp.Before(page.DateStart) {
				page.DateStart = call.Timestamp
			}

			if call.Timestamp.After(page.DateStop) {
				page.DateStop = call.Timestamp
			}
		}
	}

	for i, call := range matching {
		if uint(i) < search.Offset {
			continue
		}

		// A limit of zero asks for no rows at all, which is what the stored query does.
		if uint(i)-search.Offset >= search.Limit {
			break
		}

		page.Results = append(page.Results, ports.CallSummary{
			ID:           call.ID,
			SystemRef:    call.System.SystemRef,
			TalkgroupRef: call.Talkgroup.TalkgroupRef,
			Timestamp:    call.Timestamp,
			AudioLength:  call.AudioLength,
		})
	}

	return page, nil
}

// searchMatches applies one criterion at a time, in the same order the legacy server
// builds its filter: the caller's scope, then the system and talkgroup, then the group
// and tag labels.
func searchMatches(call domain.Call, search ports.CallSearch) bool {
	if search.Scope.Present && !search.Scope.Grants(call) {
		return false
	}

	if !search.From.IsZero() && call.Timestamp.Before(search.From) {
		return false
	}

	if !search.To.IsZero() && call.Timestamp.After(search.To) {
		return false
	}

	if search.SystemRef != 0 {
		if call.System.SystemRef != search.SystemRef {
			return false
		}

		if search.TalkgroupRef != 0 && call.Talkgroup.TalkgroupRef != search.TalkgroupRef {
			return false
		}
	}

	if len(search.GroupTalkgroups) > 0 && !matchesAnyPair(call, search.GroupTalkgroups) {
		return false
	}

	if len(search.TagTalkgroups) > 0 && !matchesAnyPair(call, search.TagTalkgroups) {
		return false
	}

	return true
}

func matchesAnyPair(call domain.Call, pairs []domain.TalkgroupScope) bool {
	for _, pair := range pairs {
		if pair.SystemRef != call.System.SystemRef {
			continue
		}

		for _, ref := range pair.TalkgroupRefs {
			if ref == call.Talkgroup.TalkgroupRef {
				return true
			}
		}
	}

	return false
}

// DeleteCallsBefore removes the calls older than the given instant.
func (calls *Calls) DeleteCallsBefore(ctx context.Context, before time.Time) (int, error) {
	if calls.Err != nil {
		return 0, calls.Err
	}

	removed := 0

	for id, call := range calls.Stored {
		if call.Timestamp.Before(before) {
			delete(calls.Stored, id)
			removed++
		}
	}

	return removed, nil
}

// Systems is an in-memory system repository.
type Systems struct {
	Stored  domain.Systems
	Err     error
	SaveErr error

	// SubSectionLevel, SubSectionSystemID and SubSectionSaved record the last
	// per-level save: which level was asked for, of which system, and the rows the
	// store kept, renumbered the way the stored tables renumber them.
	SubSectionLevel    string
	SubSectionSystemID uint64
	SubSectionSaved    []any

	// SubSectionDeletesSystem models the system being deleted under a per-level save:
	// the level is stored, and the system the save was aimed at is gone from the store
	// by the time the save is announced.
	SubSectionDeletesSystem bool
}

var _ ports.SystemRepository = (*Systems)(nil)

// AllSystems returns the stored systems.
func (systems *Systems) AllSystems(ctx context.Context) (domain.Systems, error) {
	if systems.Err != nil {
		return nil, systems.Err
	}

	return systems.Stored, nil
}

// SaveSystems replaces the stored systems, assigning identifiers to a system and to the
// talkgroups, sites, and units it carries, which is what the stored tables do to the
// aggregates auto-populate has just created.
func (systems *Systems) SaveSystems(ctx context.Context, next domain.Systems) error {
	if systems.Err != nil {
		return systems.Err
	}

	if systems.SaveErr != nil {
		// A save that cannot be persisted still leaves the submitted value in effect,
		// because the legacy server replaces what it is running with before it stores it.
		systems.Stored = next

		return systems.SaveErr
	}

	out := append(domain.Systems{}, next...)

	systemID := uint64(0)

	for _, system := range out {
		if system.ID > systemID {
			systemID = system.ID
		}
	}

	for i := range out {
		if out[i].ID == 0 {
			systemID++
			out[i].ID = systemID
		}

		out[i] = assignAggregateIDs(out[i])
	}

	systems.Stored = out

	return nil
}

// SaveSystemSettings records the submitted systems and, like the stored systems table, writes
// nothing under a system: the levels keep the rows the fake holds for them.
func (systems *Systems) SaveSystemSettings(ctx context.Context, next domain.Systems) error {
	if systems.Err != nil {
		return systems.Err
	}

	if systems.SaveErr != nil {
		systems.Stored = next

		return systems.SaveErr
	}

	out := append(domain.Systems{}, next...)

	systemID := uint64(0)

	for _, system := range out {
		if system.ID > systemID {
			systemID = system.ID
		}
	}

	for i := range out {
		if out[i].ID == 0 {
			systemID++
			out[i].ID = systemID
		}

		// The levels under a system are written by the save of that level. A systems-section
		// save keeps the rows the store holds for a system it stores, and a system the store
		// does not have starts with none, as the stored tables do.
		if stored, ok := systems.Stored.ByID(out[i].ID); ok {
			out[i].Sites = stored.Sites
			out[i].Talkgroups = stored.Talkgroups
			out[i].Units = stored.Units
		} else {
			out[i].Sites = nil
			out[i].Talkgroups = nil
			out[i].Units = nil
		}
	}

	systems.Stored = out

	return nil
}

// SaveSystemSites records the sites of one system, renumbering their order the way the
// stored table renumbers them. A system the fake does not store reports ErrNotFound.
func (systems *Systems) SaveSystemSites(ctx context.Context, systemID uint64, sites domain.Sites) error {
	return systems.saveSubSection("sites", systemID, sites, nil)
}

// SaveSystemTalkgroups records the talkgroups of one system, renumbering their order, and the
// blacklist that was edited beside them. A nil blacklist leaves the stored text alone.
func (systems *Systems) SaveSystemTalkgroups(ctx context.Context, systemID uint64, talkgroups domain.Talkgroups, blacklists *domain.Blacklists) error {
	return systems.saveSubSection("talkgroups", systemID, talkgroups, blacklists)
}

// SaveSystemUnits records the units of one system, renumbering their order.
func (systems *Systems) SaveSystemUnits(ctx context.Context, systemID uint64, units domain.Units) error {
	return systems.saveSubSection("units", systemID, units, nil)
}

// saveSubSection performs the shared part of the three per-level saves: the store
// checks the system, renumbers the submitted rows 1-based, and keeps them.
func (systems *Systems) saveSubSection(level string, systemID uint64, rows any, blacklists *domain.Blacklists) error {
	if systems.Err != nil {
		return systems.Err
	}

	found := false

	for _, system := range systems.Stored {
		if system.ID == systemID {
			found = true

			break
		}
	}

	if !found {
		return ports.ErrNotFound
	}

	if systems.SaveErr != nil {
		return systems.SaveErr
	}

	systems.SubSectionLevel = level
	systems.SubSectionSystemID = systemID

	switch typed := rows.(type) {
	case domain.Sites:
		for i := range typed {
			typed[i].Order = uint(i + 1)
			typed[i].SystemID = systemID
		}

		systems.SubSectionSaved = append([]any{}, sitesToAny(typed)...)
	case domain.Talkgroups:
		for i := range typed {
			typed[i].Order = uint(i + 1)
		}

		systems.SubSectionSaved = append([]any{}, talkgroupsToAny(typed)...)
	case domain.Units:
		for i := range typed {
			typed[i].Order = uint(i + 1)
			typed[i].SystemID = systemID
		}

		systems.SubSectionSaved = append([]any{}, unitsToAny(typed)...)
	}

	// The save replaces the level's rows on the stored system, so the configuration
	// read back after the save answers what was submitted, as the stored tables do.
	for i := range systems.Stored {
		if systems.Stored[i].ID != systemID {
			continue
		}

		switch typed := rows.(type) {
		case domain.Sites:
			next := maxSiteID(systems.Stored) + 1

			for j := range typed {
				if typed[j].ID == 0 {
					typed[j].ID = next

					next++
				}
			}

			systems.Stored[i].Sites = typed
		case domain.Talkgroups:
			next := maxTalkgroupID(systems.Stored) + 1

			for j := range typed {
				if typed[j].ID == 0 {
					typed[j].ID = next

					next++
				}
			}

			systems.Stored[i].Talkgroups = typed
		case domain.Units:
			next := maxUnitID(systems.Stored) + 1

			for j := range typed {
				if typed[j].ID == 0 {
					typed[j].ID = next

					next++
				}
			}

			systems.Stored[i].Units = typed
		}

		// The blacklist the talkgroups section edits beside its rows is stored by the same
		// save. A save that names none leaves the text the stored system holds.
		if blacklists != nil {
			systems.Stored[i].Blacklists = *blacklists
		}
	}

	// The system is deleted under the save: the level is stored, and by the time the save is
	// announced the system it was aimed at is no longer in the store.
	if systems.SubSectionDeletesSystem {
		kept := domain.Systems{}

		for _, system := range systems.Stored {
			if system.ID != systemID {
				kept = append(kept, system)
			}
		}

		systems.Stored = kept
	}

	return nil
}

func maxSiteID(systems domain.Systems) uint64 {
	max := uint64(0)

	for _, system := range systems {
		for _, site := range system.Sites {
			if site.ID > max {
				max = site.ID
			}
		}
	}

	return max
}

func maxTalkgroupID(systems domain.Systems) uint64 {
	max := uint64(0)

	for _, system := range systems {
		for _, talkgroup := range system.Talkgroups {
			if talkgroup.ID > max {
				max = talkgroup.ID
			}
		}
	}

	return max
}

func maxUnitID(systems domain.Systems) uint64 {
	max := uint64(0)

	for _, system := range systems {
		for _, unit := range system.Units {
			if unit.ID > max {
				max = unit.ID
			}
		}
	}

	return max
}

func sitesToAny(sites domain.Sites) []any {
	out := []any{}

	for _, site := range sites {
		out = append(out, site)
	}

	return out
}

func talkgroupsToAny(talkgroups domain.Talkgroups) []any {
	out := []any{}

	for _, talkgroup := range talkgroups {
		out = append(out, talkgroup)
	}

	return out
}

func unitsToAny(units domain.Units) []any {
	out := []any{}

	for _, unit := range units {
		out = append(out, unit)
	}

	return out
}

// assignAggregateIDs gives the talkgroups, sites, and units of one system the identifiers
// their tables would give them, and records which system they belong to.
func assignAggregateIDs(system domain.System) domain.System {
	system.Talkgroups = append(domain.Talkgroups{}, system.Talkgroups...)
	system.Sites = append(domain.Sites{}, system.Sites...)
	system.Units = append(domain.Units{}, system.Units...)

	talkgroupID := uint64(0)
	siteID := uint64(0)
	unitID := uint64(0)

	for _, talkgroup := range system.Talkgroups {
		if talkgroup.ID > talkgroupID {
			talkgroupID = talkgroup.ID
		}
	}

	for _, site := range system.Sites {
		if site.ID > siteID {
			siteID = site.ID
		}
	}

	for _, unit := range system.Units {
		if unit.ID > unitID {
			unitID = unit.ID
		}
	}

	for i := range system.Talkgroups {
		if system.Talkgroups[i].ID == 0 {
			talkgroupID++
			system.Talkgroups[i].ID = talkgroupID
		}
	}

	for i := range system.Sites {
		if system.Sites[i].ID == 0 {
			siteID++
			system.Sites[i].ID = siteID
			system.Sites[i].SystemID = system.ID
		}
	}

	for i := range system.Units {
		if system.Units[i].ID == 0 {
			unitID++
			system.Units[i].ID = unitID
			system.Units[i].SystemID = system.ID
		}
	}

	return system
}

// Groups is an in-memory group repository.
type Groups struct {
	Stored  domain.Groups
	Err     error
	SaveErr error
}

var _ ports.GroupRepository = (*Groups)(nil)

// AllGroups returns the stored groups.
func (groups *Groups) AllGroups(ctx context.Context) (domain.Groups, error) {
	if groups.Err != nil {
		return nil, groups.Err
	}

	return groups.Stored, nil
}

// SaveGroups replaces the stored groups, assigning identifiers to the ones that have
// none, which is what the stored table does to a group auto-populate just created.
func (groups *Groups) SaveGroups(ctx context.Context, next domain.Groups) error {
	if groups.Err != nil {
		return groups.Err
	}

	if groups.SaveErr != nil {
		// A save that cannot be persisted still leaves the submitted value in effect,
		// because the legacy server replaces what it is running with before it stores it.
		groups.Stored = next

		return groups.SaveErr
	}

	out := append(domain.Groups{}, next...)

	id := maxGroupID(out)

	for i := range out {
		if out[i].ID == 0 {
			id++
			out[i].ID = id
		}
	}

	groups.Stored = out

	return nil
}

func maxGroupID(groups domain.Groups) uint64 {
	var top uint64

	for _, group := range groups {
		if group.ID > top {
			top = group.ID
		}
	}

	return top
}

// Tags is an in-memory tag repository.
type Tags struct {
	Stored  domain.Tags
	Err     error
	SaveErr error
}

var _ ports.TagRepository = (*Tags)(nil)

// AllTags returns the stored tags.
func (tags *Tags) AllTags(ctx context.Context) (domain.Tags, error) {
	if tags.Err != nil {
		return nil, tags.Err
	}

	return tags.Stored, nil
}

// SaveTags replaces the stored tags, assigning identifiers to the ones that have none.
func (tags *Tags) SaveTags(ctx context.Context, next domain.Tags) error {
	if tags.Err != nil {
		return tags.Err
	}

	if tags.SaveErr != nil {
		// A save that cannot be persisted still leaves the submitted value in effect,
		// because the legacy server replaces what it is running with before it stores it.
		tags.Stored = next

		return tags.SaveErr
	}

	out := append(domain.Tags{}, next...)

	id := uint64(0)

	for _, tag := range out {
		if tag.ID > id {
			id = tag.ID
		}
	}

	for i := range out {
		if out[i].ID == 0 {
			id++
			out[i].ID = id
		}
	}

	tags.Stored = out

	return nil
}

// Accesses is an in-memory access repository.
type Accesses struct {
	Stored  domain.Accesses
	Err     error
	SaveErr error
}

var _ ports.AccessRepository = (*Accesses)(nil)

// AllAccesses returns the stored accesses.
func (accesses *Accesses) AllAccesses(ctx context.Context) (domain.Accesses, error) {
	if accesses.Err != nil {
		return nil, accesses.Err
	}

	return accesses.Stored, nil
}

// SaveAccesses replaces the stored accesses.
func (accesses *Accesses) SaveAccesses(ctx context.Context, next domain.Accesses) error {
	if accesses.Err != nil {
		return accesses.Err
	}

	if accesses.SaveErr != nil {
		// A save that cannot be persisted still leaves the submitted value in effect,
		// because the legacy server replaces what it is running with before it stores it.
		accesses.Stored = next

		return accesses.SaveErr
	}

	accesses.Stored = next

	return nil
}

// Apikeys is an in-memory apikey repository.
type Apikeys struct {
	Stored  domain.Apikeys
	Err     error
	SaveErr error
}

var _ ports.ApikeyRepository = (*Apikeys)(nil)

// AllApikeys returns the stored apikeys.
func (apikeys *Apikeys) AllApikeys(ctx context.Context) (domain.Apikeys, error) {
	if apikeys.Err != nil {
		return nil, apikeys.Err
	}

	return apikeys.Stored, nil
}

// SaveApikeys replaces the stored apikeys.
func (apikeys *Apikeys) SaveApikeys(ctx context.Context, next domain.Apikeys) error {
	if apikeys.Err != nil {
		return apikeys.Err
	}

	if apikeys.SaveErr != nil {
		// A save that cannot be persisted still leaves the submitted value in effect,
		// because the legacy server replaces what it is running with before it stores it.
		apikeys.Stored = next

		return apikeys.SaveErr
	}

	apikeys.Stored = next

	return nil
}

// Options is an in-memory options repository.
type Options struct {
	Stored  domain.Options
	Err     error
	SaveErr error

	// Reloaded is what re-deriving the options from the store produces, when a test wants that
	// to differ from what was last written. The legacy server's re-derivation resets every
	// option to its default and then applies the stored rows, so an option it stores under one
	// name and reads under another comes back as the default.
	Reloaded *domain.Options

	// Secrets records what was put on record by StoreSecret, in the order it was put there. A test
	// asserts that the secret the server signs with is the secret the store holds.
	Secrets []string

	// SecretErr makes storing the secret fail, the way SaveErr makes saving the configuration fail.
	SecretErr error
}

var (
	_ ports.OptionsRepository  = (*Options)(nil)
	_ ports.SigningSecretStore = (*Options)(nil)
)

// ReadOptions returns the stored options.
func (options *Options) ReadOptions(ctx context.Context) (domain.Options, error) {
	if options.Err != nil {
		return domain.Options{}, options.Err
	}

	return options.Stored, nil
}

// WriteOptions replaces the stored options.
func (options *Options) WriteOptions(ctx context.Context, next domain.Options) error {
	if options.Err != nil {
		return options.Err
	}

	if options.SaveErr != nil {
		// A save that cannot be persisted still leaves the submitted value in effect,
		// because the legacy server replaces what it is running with before it stores it.
		options.Stored = next

		return options.SaveErr
	}

	options.Stored = next

	return nil
}

// ReloadOptions re-derives the options from what the store holds. Without a Reloaded value it
// reports what was last written, which is what a store that round-trips every key produces.
func (options *Options) ReloadOptions(ctx context.Context) (domain.Options, error) {
	if options.Err != nil {
		return domain.Options{}, options.Err
	}

	if options.Reloaded != nil {
		options.Stored = *options.Reloaded
	}

	return options.Stored, nil
}

// StoreSecret records the signing secret, and holds it in the options from here on, which is what a
// store that round-trips every key does.
func (options *Options) StoreSecret(ctx context.Context, secret string) error {
	if options.SecretErr != nil {
		return options.SecretErr
	}

	options.Secrets = append(options.Secrets, secret)
	options.Stored.Secret = secret

	return nil
}

// Logs is an in-memory log repository.
//
// The entries are behind a mutex because a log is written from the goroutines that fail — a
// directory watcher, an ingest path — and a test that waits for a log line reads it from another
// one.
type Logs struct {
	Stored []domain.LogEntry
	Err    error

	// SearchErr, when set, is returned by SearchLogs and by nothing else. The log view records the
	// failures it cannot answer with, so a test needs the read to fail while the write still works.
	SearchErr error

	// DeleteErr, when set, is returned by DeleteLogsBefore and by nothing else. The retention pass records
	// the half of its work it could not do, so a test needs the delete to fail while the write still works.
	DeleteErr error

	// AppendErr, when set, is returned by AppendLog and by nothing else. Writing a log entry is
	// best-effort and must not change the operation that wrote it, while reading and pruning the log view
	// are part of the operations that do those things — so a test needs the write to fail on its own.
	AppendErr error

	mutex sync.Mutex
}

var _ ports.LogRepository = (*Logs)(nil)

// Recorded returns the entries as they were written, oldest first.
func (logs *Logs) Recorded() []domain.LogEntry {
	logs.mutex.Lock()
	defer logs.mutex.Unlock()

	return append([]domain.LogEntry{}, logs.Stored...)
}

// AppendLog records an entry.
func (logs *Logs) AppendLog(ctx context.Context, entry domain.LogEntry) error {
	logs.mutex.Lock()
	defer logs.mutex.Unlock()

	if logs.Err != nil {
		return logs.Err
	}

	if logs.AppendErr != nil {
		return logs.AppendErr
	}

	if entry.ID == 0 {
		entry.ID = uint64(len(logs.Stored) + 1)
	}

	logs.Stored = append(logs.Stored, entry)

	return nil
}

// SearchLogs applies the criteria to the stored entries, the way the stored repository applies them:
// the level, the subsystem, a case-insensitive substring of the message, and the date window.
//
// The per-source counts are computed over everything the criteria match except the subsystem itself, so a
// component test that checks what the log view offers sees the same shape the stored repository answers.
func (logs *Logs) SearchLogs(ctx context.Context, search domain.LogSearch) (ports.LogSearchPage, error) {
	logs.mutex.Lock()
	defer logs.mutex.Unlock()

	if logs.Err != nil {
		return ports.LogSearchPage{}, logs.Err
	}

	if logs.SearchErr != nil {
		return ports.LogSearchPage{}, logs.SearchErr
	}

	// The match one criterion wider than the entries: every criterion except the subsystem, which is the
	// match the counts describe.
	wider := []domain.LogEntry{}

	for _, entry := range logs.Stored {
		if search.HasLevel() && entry.Level != search.Level {
			continue
		}

		// The submitted text is matched literally and case-insensitively, exactly as the stored query
		// matches it after escaping: a percent sign in what the operator typed is a percent sign.
		if search.HasText() && !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(search.Text)) {
			continue
		}

		if search.HasDate() {
			from, to, _ := search.DateWindow()

			if entry.DateTime.Before(from) || entry.DateTime.After(to) {
				continue
			}
		}

		wider = append(wider, entry)
	}

	matching := wider

	if search.HasSource() {
		matching = []domain.LogEntry{}

		for _, entry := range wider {
			if logSourceOf(entry) != search.Source {
				continue
			}

			matching = append(matching, entry)
		}
	}

	sort.SliceStable(matching, func(i, j int) bool {
		if search.Ascending {
			return matching[i].DateTime.Before(matching[j].DateTime)
		}

		return matching[i].DateTime.After(matching[j].DateTime)
	})

	// The span belongs to everything the criteria match, and not to the page that is returned: the
	// log view pages through the span, and its ends stay where they are while the page moves. A
	// search that matches nothing leaves both ends unset.
	page := ports.LogSearchPage{Count: uint64(len(matching))}

	if len(matching) > 0 {
		page.DateStart, page.DateStop = matching[0].DateTime, matching[0].DateTime

		for _, entry := range matching[1:] {
			if entry.DateTime.Before(page.DateStart) {
				page.DateStart = entry.DateTime
			}

			if entry.DateTime.After(page.DateStop) {
				page.DateStop = entry.DateTime
			}
		}
	}

	counts := map[domain.LogSource]uint64{}

	for _, entry := range wider {
		counts[logSourceOf(entry)]++
	}

	for source, n := range counts {
		page.SourceCounts = append(page.SourceCounts, ports.LogSourceCount{Source: source, Count: n})
	}

	sort.Slice(page.SourceCounts, func(i, j int) bool {
		if page.SourceCounts[i].Count != page.SourceCounts[j].Count {
			return page.SourceCounts[i].Count > page.SourceCounts[j].Count
		}

		return page.SourceCounts[i].Source < page.SourceCounts[j].Source
	})

	for i, entry := range matching {
		if uint(i) < search.Offset {
			continue
		}

		// A limit of zero asks for no rows at all, which is what the stored query does.
		if uint(i)-search.Offset >= search.Limit {
			break
		}

		// Reported as the log view reports it, which is how the stored repository reads a row written
		// without a source. Recorded is the half that reports what a writer actually wrote.
		entry.Source = logSourceOf(entry)

		page.Results = append(page.Results, entry)
	}

	return page, nil
}

// logSourceOf is the source an entry is reported under: an entry written without one is legacy, which is
// what the stored repository does with the rows the other server writes.
func logSourceOf(entry domain.LogEntry) domain.LogSource {
	if entry.Source == "" {
		return domain.LogSourceLegacy
	}

	return entry.Source
}

// DeleteLogsBefore removes the entries older than the given instant.
func (logs *Logs) DeleteLogsBefore(ctx context.Context, before time.Time) (int, error) {
	logs.mutex.Lock()
	defer logs.mutex.Unlock()

	if logs.Err != nil {
		return 0, logs.Err
	}

	if logs.DeleteErr != nil {
		return 0, logs.DeleteErr
	}

	kept := []domain.LogEntry{}
	removed := 0

	for _, entry := range logs.Stored {
		if entry.DateTime.Before(before) {
			removed++
			continue
		}

		kept = append(kept, entry)
	}

	logs.Stored = kept

	return removed, nil
}

// Dirwatches is an in-memory directory-watch repository.
type Dirwatches struct {
	Stored  domain.Dirwatches
	Err     error
	SaveErr error
}

var _ ports.DirwatchRepository = (*Dirwatches)(nil)

// AllDirwatches returns the stored watches.
func (dirwatches *Dirwatches) AllDirwatches(ctx context.Context) (domain.Dirwatches, error) {
	if dirwatches.Err != nil {
		return nil, dirwatches.Err
	}

	return dirwatches.Stored, nil
}

// SaveDirwatches replaces the stored watches.
func (dirwatches *Dirwatches) SaveDirwatches(ctx context.Context, next domain.Dirwatches) error {
	if dirwatches.Err != nil {
		return dirwatches.Err
	}

	if dirwatches.SaveErr != nil {
		// A save that cannot be persisted still leaves the submitted value in effect,
		// because the legacy server replaces what it is running with before it stores it.
		dirwatches.Stored = next

		return dirwatches.SaveErr
	}

	dirwatches.Stored = next

	return nil
}

// Downstreams is an in-memory downstream repository.
type Downstreams struct {
	Stored  domain.Downstreams
	Err     error
	SaveErr error
}

var _ ports.DownstreamRepository = (*Downstreams)(nil)

// AllDownstreams returns the stored downstreams.
func (downstreams *Downstreams) AllDownstreams(ctx context.Context) (domain.Downstreams, error) {
	if downstreams.Err != nil {
		return nil, downstreams.Err
	}

	return downstreams.Stored, nil
}

// SaveDownstreams replaces the stored downstreams.
func (downstreams *Downstreams) SaveDownstreams(ctx context.Context, next domain.Downstreams) error {
	if downstreams.Err != nil {
		return downstreams.Err
	}

	if downstreams.SaveErr != nil {
		// A save that cannot be persisted still leaves the submitted value in effect,
		// because the legacy server replaces what it is running with before it stores it.
		downstreams.Stored = next

		return downstreams.SaveErr
	}

	downstreams.Stored = next

	return nil
}

// DelayedCalls is an in-memory delayed-call repository.
type DelayedCalls struct {
	Stored []ports.DelayedCall
	Err    error
}

var _ ports.DelayedCallRepository = (*DelayedCalls)(nil)

// AllDelayed returns the calls still waiting.
func (delayed *DelayedCalls) AllDelayed(ctx context.Context) ([]ports.DelayedCall, error) {
	if delayed.Err != nil {
		return nil, delayed.Err
	}

	return delayed.Stored, nil
}

// ScheduleDelayed records that a call is waiting until the given instant.
func (delayed *DelayedCalls) ScheduleDelayed(ctx context.Context, callID uint64, until time.Time) error {
	if delayed.Err != nil {
		return delayed.Err
	}

	delayed.Stored = append(delayed.Stored, ports.DelayedCall{CallID: callID, Timestamp: until})

	return nil
}

// UnscheduleDelayed drops one waiting call.
func (delayed *DelayedCalls) UnscheduleDelayed(ctx context.Context, callID uint64) error {
	if delayed.Err != nil {
		return delayed.Err
	}

	kept := []ports.DelayedCall{}

	for _, entry := range delayed.Stored {
		if entry.CallID != callID {
			kept = append(kept, entry)
		}
	}

	delayed.Stored = kept

	return nil
}

// ClearDelayed drops every waiting call.
func (delayed *DelayedCalls) ClearDelayed(ctx context.Context) error {
	if delayed.Err != nil {
		return delayed.Err
	}

	delayed.Stored = nil

	return nil
}

// CallActivity is the fake the status read is tested against. It records the bounds and limits it was
// asked with, the way Calls records the criteria a search ran with: the window the status read cuts is
// the use case's own decision, and a test that cannot see the bounds it was handed cannot tell a window
// from a day from an hour.
type CallActivity struct {
	// SinceCount answers every CountCallsSince when SinceAnswers has nothing for the bound.
	SinceCount uint64

	// SinceAnswers answers a CountCallsSince by the bound it was given, in milliseconds since the Unix
	// epoch. It is what lets one fake answer the window, the day, and the last hour with three different
	// numbers.
	SinceAnswers map[int64]uint64

	// SinceAsked records every bound CountCallsSince was asked with, in the order it was asked.
	SinceAsked []time.Time

	Hours      []ports.CallHourCount
	HoursAsked []time.Time

	Busiest      []ports.CallTalkgroupCount
	BusiestAsked []uint

	Recent      []ports.CallSummary
	RecentAsked []uint

	// Err is reported by every read, which is how a status read that cannot be read is tested.
	Err error
}

var _ ports.CallActivityRepository = (*CallActivity)(nil)

// CountCallsSince counts the calls at or after the given bound.
func (activity *CallActivity) CountCallsSince(ctx context.Context, from time.Time) (uint64, error) {
	activity.SinceAsked = append(activity.SinceAsked, from)

	if activity.Err != nil {
		return 0, activity.Err
	}

	if count, ok := activity.SinceAnswers[from.UnixMilli()]; ok {
		return count, nil
	}

	return activity.SinceCount, nil
}

// CountCallsByHour answers the hour buckets over the given bound.
func (activity *CallActivity) CountCallsByHour(ctx context.Context, from time.Time) ([]ports.CallHourCount, error) {
	activity.HoursAsked = append(activity.HoursAsked, from)

	if activity.Err != nil {
		return nil, activity.Err
	}

	return activity.Hours, nil
}

// CountCallsByTalkgroup answers the busiest talkgroups over the given bound.
func (activity *CallActivity) CountCallsByTalkgroup(ctx context.Context, from time.Time, limit uint) ([]ports.CallTalkgroupCount, error) {
	activity.BusiestAsked = append(activity.BusiestAsked, limit)

	if activity.Err != nil {
		return nil, activity.Err
	}

	return activity.Busiest, nil
}

// RecentCalls answers the newest recorded calls.
func (activity *CallActivity) RecentCalls(ctx context.Context, limit uint) ([]ports.CallSummary, error) {
	activity.RecentAsked = append(activity.RecentAsked, limit)

	if activity.Err != nil {
		return nil, activity.Err
	}

	return activity.Recent, nil
}

// LogTallies is the fake the log-health card is tested against.
type LogTallies struct {
	// Stored are the tallies the read answers with.
	Stored []ports.LogLevelTally

	// TalliesAsked records every bound the tally was asked with.
	TalliesAsked []time.Time

	Err error
}

var _ ports.LogTallyRepository = (*LogTallies)(nil)

// TallyLogLevels counts what the server complained about at or after the given bound.
func (tallies *LogTallies) TallyLogLevels(ctx context.Context, from time.Time) ([]ports.LogLevelTally, error) {
	tallies.TalliesAsked = append(tallies.TalliesAsked, from)

	if tallies.Err != nil {
		return nil, tallies.Err
	}

	return tallies.Stored, nil
}
