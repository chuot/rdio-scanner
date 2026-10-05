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

package application

import (
	"context"
	"fmt"

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

// IngestCallOutcome reports what the server decided about a call an ingest source
// produced. The callers that surface the outcome — the upload endpoints, the directory
// watchers — map it to their own responses; the outcome itself carries no wire detail.
type IngestCallOutcome string

const (
	// IngestCallAccepted means the call was stored and delivered.
	IngestCallAccepted IngestCallOutcome = "accepted"

	// IngestCallIncomplete means the call did not carry what a call needs. The upload
	// endpoints answer this with the "incomplete call" status the frozen contract fixes.
	IngestCallIncomplete IngestCallOutcome = "incomplete"

	// IngestCallBlacklisted means the owning system refuses that talkgroup.
	IngestCallBlacklisted IngestCallOutcome = "blacklisted"

	// IngestCallUnknown means the server could not work out which system and talkgroup
	// the call belongs to, and auto-populate is not enabled to invent them.
	IngestCallUnknown IngestCallOutcome = "unknown"

	// IngestCallDuplicate means an identical call was already recorded inside the
	// duplicate-detection window.
	IngestCallDuplicate IngestCallOutcome = "duplicate"

	// IngestCallFailed means the server could not complete a step it depends on.
	IngestCallFailed IngestCallOutcome = "failed"
)

// IngestCallResult is the acknowledgement an ingest source gets: what happened, and the
// identifier of the stored call when there is one. It carries no domain data.
type IngestCallResult struct {
	Outcome IngestCallOutcome
	CallID  uint64
}

// IngestCallCommand asks the server to take one call produced by an ingest source: a
// watched directory, an upload endpoint, or another server forwarding to this one.
type IngestCallCommand struct {
	Call domain.Call
}

// IngestCallHandler turns an ingest claim into a stored, delivered call.
//
// The order of the steps is the order the legacy server performs them in, because that
// order is observable: a blacklisted call is never checked for duplication, a call that
// auto-populates broadcasts the new configuration before it is stored, and a duplicate is
// rejected before its audio is converted.
type IngestCallHandler struct {
	calls     ports.CallRepository
	systems   ports.SystemRepository
	groups    ports.GroupRepository
	tags      ports.TagRepository
	options   ports.OptionsRepository
	logs      ports.LogRepository
	converter ports.AudioConverter
	hub       ports.Hub
	clock     ports.Clock
}

// NewIngestCallHandler builds the handler from its ports.
//
// The options are read through the options port rather than held, so that the composition
// root decides whether that read reaches the database or an in-memory copy refreshed when
// the configuration changes.
func NewIngestCallHandler(
	calls ports.CallRepository,
	systems ports.SystemRepository,
	groups ports.GroupRepository,
	tags ports.TagRepository,
	options ports.OptionsRepository,
	logs ports.LogRepository,
	converter ports.AudioConverter,
	hub ports.Hub,
	clock ports.Clock,
) *IngestCallHandler {
	return &IngestCallHandler{
		calls:     calls,
		systems:   systems,
		groups:    groups,
		tags:      tags,
		options:   options,
		logs:      logs,
		converter: converter,
		hub:       hub,
		clock:     clock,
	}
}

// Handle resolves, filters, stores, and delivers one ingested call.
func (handler *IngestCallHandler) Handle(ctx context.Context, cmd IngestCallCommand) (IngestCallResult, error) {
	call := cmd.Call

	if err := call.IsValid(); err != nil {
		return IngestCallResult{Outcome: IngestCallIncomplete}, err
	}

	options, err := handler.options.ReadOptions(ctx)
	if err != nil {
		return handler.failed(err)
	}

	systems, err := handler.systems.AllSystems(ctx)
	if err != nil {
		return handler.failed(err)
	}

	groups, err := handler.groups.AllGroups(ctx)
	if err != nil {
		return handler.failed(err)
	}

	tags, err := handler.tags.AllTags(ctx)
	if err != nil {
		return handler.failed(err)
	}

	// The log line the legacy server writes for every ingest decision, reproduced byte
	// for byte because the log view is part of what a deployment sees.
	logCall := func(level string, call domain.Call, message string) {
		handler.log(ctx, level, fmt.Sprintf("newcall: system=%v talkgroup=%v file=%v %v",
			call.System.SystemRef, call.Talkgroup.TalkgroupRef, call.AudioFilename, message))
	}

	logError := func(err error) (IngestCallResult, error) {
		handler.log(ctx, domain.LogLevelError, fmt.Sprintf("controller.ingestcall: %v", err.Error()))

		return IngestCallResult{Outcome: IngestCallFailed}, err
	}

	populated := false

	// Resolution: the database identifier the source claims, then the client-facing
	// reference, then the label.
	if !call.HasSystem() {
		if system, ok := systems.ResolveSystem(call.Meta); ok {
			call.System = system
		}
	}

	if !call.HasTalkgroup() && call.HasSystem() {
		if talkgroup, ok := call.System.ResolveTalkgroup(call.Meta); ok {
			call.Talkgroup = talkgroup
		}
	}

	// The units the source names become part of the call itself, independently of the
	// units the server keeps on the system.
	if len(call.Units) == 0 && call.Meta.UnitRefs != nil {
		for _, unitRef := range call.Meta.UnitRefs {
			if unitRef > 0 {
				call.Units = append(call.Units, domain.CallUnit{UnitRef: unitRef})
			}
		}
	}

	if call.HasSystem() && call.HasTalkgroup() && !call.System.AcceptsTalkgroup(call.Talkgroup.TalkgroupRef) {
		logCall(domain.LogLevelInfo, call, "blacklisted")

		return IngestCallResult{Outcome: IngestCallBlacklisted}, nil
	}

	// A previous auto-populated call may have created this talkgroup with its numeric
	// reference as its label. A later upload that does describe it repairs that
	// placeholder, and only that placeholder. As in the legacy server, this applies only
	// when both the system and the talkgroup are already attached to the call.
	if call.HasSystem() && call.HasTalkgroup() {
		if repaired, ok := domain.RepairedTalkgroup(options, call.System, call.Meta, call.Talkgroup); ok {
			call.System = call.System.WithTalkgroup(repaired)
			call.Talkgroup = repaired

			systems = systems.Replace(call.System)

			populated = true
		}
	}

	// Auto-populate invents a system only when the server-wide option enables it, as the
	// legacy server does.
	if domain.ShouldCreateSystem(options, call.Meta) && !call.HasSystem() {
		populated = true

		system := domain.System{}

		if call.Meta.SystemRef > 0 {
			system.SystemRef = call.Meta.SystemRef
		} else {
			system.SystemRef = systems.NextSystemRef()
		}

		if call.Meta.SystemLabel != "" {
			system.Label = call.Meta.SystemLabel
		} else {
			system.Label = system.GeneratedLabel()
		}

		call.System = system
		systems = systems.Replace(system)
	}

	// A talkgroup may be invented by the server-wide option or by the owning system's own
	// flag.
	if call.HasSystem() && !call.HasTalkgroup() && domain.ShouldCreateTalkgroup(options, call.System, call.Meta) {
		populated = true

		groupLabels := call.Meta.GroupLabels()

		for _, groupLabel := range groupLabels {
			if _, ok := groups.ByLabel(groupLabel); ok {
				continue
			}

			groups = append(groups, domain.Group{Label: groupLabel})

			if err := handler.groups.SaveGroups(ctx, groups); err != nil {
				return logError(err)
			}

			if groups, err = handler.groups.AllGroups(ctx); err != nil {
				return logError(err)
			}
		}

		tagLabel := call.Meta.TagLabel()

		if _, ok := tags.ByLabel(tagLabel); !ok {
			tags = append(tags, domain.Tag{Label: tagLabel})

			if err := handler.tags.SaveTags(ctx, tags); err != nil {
				return logError(err)
			}

			if tags, err = handler.tags.AllTags(ctx); err != nil {
				return logError(err)
			}
		}

		tagID := uint64(0)

		if tag, ok := tags.ByLabel(tagLabel); ok {
			tagID = tag.ID
		}

		talkgroup := call.Meta.NewTalkgroup(groups.IDsForLabels(groupLabels), tagID)

		updated, _ := call.System.AddTalkgroup(talkgroup)

		call.System = updated
		call.Talkgroup = talkgroup

		systems = systems.Replace(call.System)
	}

	// Units are added to the system under the same auto-populate condition that invents
	// a talkgroup, whether or not a talkgroup was invented.
	if call.HasSystem() && domain.AutoPopulates(options, call.System) {
		merged, changed := call.System.MergeUnits(call.Meta.ClaimedUnits())
		if changed {
			populated = true

			call.System = merged
			systems = systems.Replace(call.System)
		}
	}

	if populated {
		if err := handler.systems.SaveSystems(ctx, systems); err != nil {
			return logError(err)
		}

		if systems, err = handler.systems.AllSystems(ctx); err != nil {
			return logError(err)
		}

		// The stored system and talkgroup are the ones the call is recorded against, so
		// that the identifiers the call carries are the persisted ones. The lookup uses
		// the references the call resolved to, not the claims the ingest source made: a
		// call whose talkgroup was already resolved carries no claim to re-resolve from.
		if stored, ok := systems.ByRef(call.System.SystemRef); ok {
			call.System = stored
		} else {
			return IngestCallResult{Outcome: IngestCallUnknown}, nil
		}

		if stored, ok := call.System.Talkgroups.ByRef(call.Talkgroup.TalkgroupRef); ok {
			call.Talkgroup = stored
		} else {
			return IngestCallResult{Outcome: IngestCallUnknown}, nil
		}

		handler.hub.BroadcastConfig(ports.ConfigSnapshot{
			Groups:  groups,
			Tags:    tags,
			Systems: systems,
			Options: options,
		})
	}

	if !call.HasSystem() || !call.HasTalkgroup() {
		logCall(domain.LogLevelWarn, call, "no matching system/talkgroup")

		return IngestCallResult{Outcome: IngestCallUnknown}, nil
	}

	if !options.DisableDuplicateDetection {
		window := options.DuplicateDetectionWindow()

		from, to := call.DuplicateWindow(window)

		candidates, err := handler.calls.FindDuplicateCandidates(ctx, call.System.ID, call.Talkgroup.ID, from, to)
		if err != nil {
			return logError(err)
		}

		for _, candidate := range candidates {
			if call.IsDuplicateOf(candidate, window) {
				logCall(domain.LogLevelWarn, call, "duplicate call rejected")

				return IngestCallResult{Outcome: IngestCallDuplicate}, nil
			}
		}
	}

	if err := handler.convertAudio(ctx, &call, options, tags); err != nil {
		handler.log(ctx, domain.LogLevelWarn, err.Error())
	}

	stored, err := handler.calls.SaveCall(ctx, call)
	if err != nil {
		return logError(err)
	}

	logCall(domain.LogLevelInfo, stored, "success")

	handler.hub.BroadcastCall(stored)

	return IngestCallResult{Outcome: IngestCallAccepted, CallID: stored.ID}, nil
}

// convertAudio converts the call's audio and replaces it with the converted file. When
// the converter cannot convert, the original audio is kept, which is what the legacy
// server ends up storing; the difference is that server-ng records why.
func (handler *IngestCallHandler) convertAudio(ctx context.Context, call *domain.Call, options domain.Options, tags domain.Tags) error {
	if options.AudioConversion == domain.AudioConversionDisabled {
		return nil
	}

	var metadata *ports.AudioMetadata

	if tag, ok := tags.ByID(call.Talkgroup.TagID); ok {
		metadata = &ports.AudioMetadata{
			Album:  call.Talkgroup.Label,
			Artist: call.System.Label,
			Date:   call.Timestamp,
			Genre:  tag.Label,
			Title:  call.Talkgroup.Name,
		}
	}

	converted, err := handler.converter.Convert(ctx, ports.AudioFile{
		Filename: call.AudioFilename,
		Mime:     call.AudioMime,
		Data:     call.Audio,
	}, options.AudioConversion, metadata)
	if err != nil {
		return err
	}

	call.Audio = converted.Data
	call.AudioFilename = converted.Filename
	call.AudioMime = converted.Mime

	return nil
}

func (handler *IngestCallHandler) log(ctx context.Context, level, message string) {
	if err := handler.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: handler.clock.Now(),
		Level:    level,
		Message:  message,
	}); err != nil {
		// A log entry that cannot be written must not change what the server did; the
		// ingest decision has already been taken and delivered.
		return
	}
}

func (handler *IngestCallHandler) failed(err error) (IngestCallResult, error) {
	return IngestCallResult{Outcome: IngestCallFailed}, err
}
