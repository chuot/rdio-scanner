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

package application

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// IngestCallOutcome reports what the server decided about a call an ingest source
// produced. The callers that surface the outcome — the upload endpoints, the directory
// watchers — map it to their own responses; the outcome itself carries no wire detail.
type IngestCallOutcome string

const (
	// IngestCallAccepted means the call was stored and delivered.
	IngestCallAccepted IngestCallOutcome = "accepted"

	// IngestCallIncomplete means the call did not carry what a call needs. The upload
	// endpoints answer this with the "incomplete call" status the contract fixes.
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

// IngestCallHandler turns an ingest claim into a stored, delivered call. The order of the
// steps is observable: a blacklisted call is never checked for duplication, a call that
// auto-populates broadcasts the new configuration before it is stored, and a duplicate is
// rejected before its audio is converted.
type IngestCallHandler struct {
	calls       ports.CallRepository
	systems     ports.SystemRepository
	groups      ports.GroupRepository
	tags        ports.TagRepository
	options     ports.OptionsRepository
	logs        ports.LogRepository
	converter   ports.AudioConverter
	prober      ports.AudioLengthProber
	hub         ports.Hub
	delayer     ports.CallDelayer
	downstreams ports.DownstreamRepository
	forwarder   ports.DownstreamForwarder
	clock       ports.Clock

	// writes holds one call being ingested at a time. Duplicate detection and auto-
	// populate's create-once both depend on that serialization, and with watchers on
	// their own goroutines and uploads answered on their own connections the rule has
	// to be held on purpose.
	writes sync.Mutex
}

// NewIngestCallHandler builds the handler from its ports. The options are read through the
// options port rather than held, so the composition root decides whether that read reaches
// the database or an in-memory copy. The downstream repository and the forwarder belong to
// the same step: a deployment with no downstreams is a repository that returns nothing, not
// a handler built without them. The delayer decides whether a stored call is told to anybody
// now or later: a deployment naming no delay is a delayer that never takes a call, not a
// handler built without one.
func NewIngestCallHandler(
	calls ports.CallRepository,
	systems ports.SystemRepository,
	groups ports.GroupRepository,
	tags ports.TagRepository,
	options ports.OptionsRepository,
	logs ports.LogRepository,
	converter ports.AudioConverter,
	prober ports.AudioLengthProber,
	hub ports.Hub,
	delayer ports.CallDelayer,
	downstreams ports.DownstreamRepository,
	forwarder ports.DownstreamForwarder,
	clock ports.Clock,
) *IngestCallHandler {
	return &IngestCallHandler{
		calls:       calls,
		systems:     systems,
		groups:      groups,
		tags:        tags,
		options:     options,
		logs:        logs,
		converter:   converter,
		prober:      prober,
		hub:         hub,
		delayer:     delayer,
		downstreams: downstreams,
		forwarder:   forwarder,
		clock:       clock,
	}
}

// Handle resolves, filters, stores, and delivers one ingested call. The delivery is made
// after the ingest lock is released: the store, the log, and the duplicate decision are the
// server's own work and run under the lock, while the delivery is the part a listener can
// stall — a listener that cannot keep up waits in its own delivery, and the next ingested
// call is not serialized behind it.
func (handler *IngestCallHandler) Handle(ctx context.Context, cmd IngestCallCommand) (IngestCallResult, error) {
	var deliver domain.Call

	handler.writes.Lock()
	defer func() {
		handler.writes.Unlock()

		if deliver.ID != 0 {
			handler.DeliverCall(ctx, deliver)
		}
	}()

	call := cmd.Call.Sanitized()

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

	// The line every ingest decision gets: the call it is about, the decision, and the
	// recording the call came from.
	logCall := func(level string, call domain.Call, decision string) {
		handler.log(ctx, level, domain.LogSourceIngest, fmt.Sprintf("call from system %v talkgroup %v %s, %s",
			call.System.SystemRef, call.Talkgroup.TalkgroupRef, decision, call.AudioFilename))
	}

	// The line for a call the server could not finish with: what it was trying to do,
	// then the failure's own text, which names the statement that failed.
	logError := func(err error) (IngestCallResult, error) {
		handler.log(ctx, domain.LogLevelError, domain.LogSourceIngest, fmt.Sprintf("the call could not be ingested: %v", err.Error()))

		return IngestCallResult{Outcome: IngestCallFailed}, err
	}

	// What this recording changed in the administrator's configuration, in the order
	// the changes were made: auto-populate creates what the recording named it, and
	// an operator has to be able to find the recording that named it.
	created := []string{}

	logCreated := func(what string) {
		created = append(created, fmt.Sprintf("the recording %s %s", call.AudioFilename, what))
	}

	reportCreated := func() {
		for _, what := range created {
			handler.log(ctx, domain.LogLevelInfo, domain.LogSourceConfig, what)
		}
	}

	// The units this recording added, as opposed to the ones the system already had:
	// merging reports only that something changed, and the entry has to name what.
	addedUnits := func(before, after domain.System) domain.Units {
		added := domain.Units{}

		for _, unit := range after.Units {
			if _, already := before.Units.ByRef(unit.UnitRef); !already {
				added = append(added, unit)
			}
		}

		return added
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

	// A site is a row that belongs to a system, so it is settled under the system the call
	// landed on, and only where the source claimed one: a source that wrote the site on the call
	// itself keeps what it wrote. A claim naming nothing that system has leaves the call with no
	// site, and the call is still filed.
	if call.HasSystem() && call.SiteRef == 0 {
		if site, ok := call.System.ResolveSite(call.Meta); ok {
			call.SiteRef = site.SiteRef
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

	// A call that names a talkgroup label renames the label of the talkgroup it lands
	// on, whether that label is the numeric placeholder an earlier auto-populated call
	// wrote or one the administrator configured, and only under the auto-populate gate.
	// A claim equal to what is stored renames nothing, so no configuration entry, save,
	// or broadcast follows it.
	if call.HasSystem() && call.HasTalkgroup() {
		if repaired, ok := domain.RepairedTalkgroup(options, call.System, call.Meta, call.Talkgroup); ok {
			stored := call.Talkgroup

			call.System = call.System.WithTalkgroup(repaired)
			call.Talkgroup = repaired

			systems = systems.Replace(call.System)

			populated = true

			// Only a label that actually moved is worth a line, and the two wordings tell an
			// operator which talkgroup the recording changed: one replaced a placeholder the
			// server itself wrote, the other renamed a label that was already there. Filling
			// a name the server wrote is a change to the talkgroup, but not a rename of a
			// label, and the legacy server filled it without reporting it.
			if repaired.Label != stored.Label {
				if stored.HasGeneratedLabel() {
					logCreated(fmt.Sprintf("replaced the placeholder talkgroup %v with %q on the system %q",
						repaired.TalkgroupRef, repaired.Label, call.System.Label))
				} else {
					logCreated(fmt.Sprintf("renamed the talkgroup %v from %q to %q on the system %q",
						repaired.TalkgroupRef, stored.Label, repaired.Label, call.System.Label))
				}
			}
		}
	}

	// Auto-populate invents a system only when the server-wide option enables it.
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

		logCreated(fmt.Sprintf("created the system %q (system %v)", system.Label, system.SystemRef))
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

			logCreated(fmt.Sprintf("created the group %q", groupLabel))
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

			logCreated(fmt.Sprintf("created the tag %q", tagLabel))
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

		logCreated(fmt.Sprintf("created the talkgroup %q (talkgroup %v) on the system %q",
			talkgroup.Label, talkgroup.TalkgroupRef, call.System.Label))
	}

	// Units are added to the system under the same auto-populate condition that invents
	// a talkgroup, whether or not a talkgroup was invented.
	if call.HasSystem() && domain.AutoPopulates(options, call.System) {
		merged, changed := call.System.MergeUnits(call.Meta.ClaimedUnits())
		if changed {
			populated = true

			for _, unit := range addedUnits(call.System, merged) {
				logCreated(fmt.Sprintf("added the unit %q (unit %v) to the system %q",
					unit.Label, unit.UnitRef, merged.Label))
			}

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

		// The configuration this recording changed is on record now, which is the moment
		// the log can say it changed.
		reportCreated()

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
		// Not the shared shape: this call has no system and no talkgroup to name, and
		// naming the zeros they are stored as would tell an operator the call came from
		// system 0. The recording is the only thing this line can name.
		handler.log(ctx, domain.LogLevelWarn, domain.LogSourceIngest, fmt.Sprintf("the recording %s does not match any system or talkgroup the server knows", call.AudioFilename))

		return IngestCallResult{Outcome: IngestCallUnknown}, nil
	}

	if !options.DisableDuplicateDetection {
		window := options.DuplicateDetectionWindow()

		from, to := call.DuplicateWindow(window)

		candidates, err := handler.calls.FindDuplicateCandidates(ctx, call.System.ID, call.Talkgroup.ID, call.SiteRef, from, to)
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
		handler.log(ctx, domain.LogLevelWarn, domain.LogSourceFFmpeg, fmt.Sprintf("the recording could not be converted: %v", err.Error()))
	}

	stored, err := handler.calls.SaveCall(ctx, call)
	if err != nil {
		return logError(err)
	}

	logCall(domain.LogLevelInfo, stored, "success")

	deliver = stored

	return IngestCallResult{Outcome: IngestCallAccepted, CallID: stored.ID}, nil
}

// DeliverCall tells everyone about a call this server has stored: the other servers first,
// then the listeners. It is the step the ingest path ends with, and the step a held call
// returns to when its hold runs out — which is why the delay is decided here and nowhere
// else, and why a call that waited is not delivered by a copy of this fan-out.
func (handler *IngestCallHandler) DeliverCall(ctx context.Context, call domain.Call) {
	held, err := handler.delayer.Hold(ctx, call)

	if err != nil {
		// A call that cannot be held for delivery stays in the recording history and is
		// never delivered: preserved legacy behavior.
		handler.log(ctx, domain.LogLevelError, domain.LogSourceDelayer, fmt.Sprintf("the call could not be held for delivery: %v", err.Error()))

		return
	}

	if held {
		return
	}

	// The other servers are told first, the listeners second: preserved legacy order.
	// Neither waits for the other, and neither waits for a downstream server to answer.
	handler.forwardCall(ctx, call)

	handler.hub.BroadcastCall(call)
}

// forwardSlots caps how many calls the server posts to its downstreams at the same
// time: a burst of ingested calls must not become an unbounded flood of posts on the
// receiving servers, and the posts queue behind the eight in flight.
var forwardSlots = make(chan struct{}, 8)

// maxForwardingCalls caps how many calls are inside the forwarding path at once,
// posted or waiting behind the slots: a downstream that accepts the connection but
// answers slower than calls arrive would otherwise make the server grow one goroutine
// per call, each holding its recording, until the server runs out of memory. Past the
// cap a call is dropped from forwarding like any other forwarding failure — a
// server-log line, not this use case's failure.
const maxForwardingCalls = 32

// forwardingCalls counts the calls inside the forwarding path right now. The count
// is checked before it is taken, so it is a bound, not a fence: a few calls arriving
// at the same instant may pass it together, which costs nothing worth guarding.
var forwardingCalls atomic.Int64

// forwardCall re-posts a stored call to every downstream server whose scope covers it.
// It runs on its own goroutine, bounded only by the forwarder's own timeout, so a slow
// or unreachable downstream cannot hold up later calls; the call was already stored
// and delivered here, so a forwarding failure is a server-log line, not this use
// case's failure.
func (handler *IngestCallHandler) forwardCall(ctx context.Context, call domain.Call) {
	if forwardingCalls.Load() >= maxForwardingCalls {
		handler.log(ctx, domain.LogLevelWarn, domain.LogSourceDownstream, fmt.Sprintf("call from system %d talkgroup %d was not forwarded: too many calls were already waiting for the downstream servers",
			call.System.SystemRef, call.Talkgroup.TalkgroupRef))

		return
	}

	forwardingCalls.Add(1)

	go func() {
		defer forwardingCalls.Add(-1)

		ctx = context.WithoutCancel(ctx)

		downstreams, err := handler.downstreams.AllDownstreams(ctx)
		if err != nil {
			handler.log(ctx, domain.LogLevelError, domain.LogSourceDownstream, fmt.Sprintf("the downstream servers could not be read: %v", err.Error()))

			return
		}

		for _, downstream := range downstreams.Accepting(call) {
			forwardSlots <- struct{}{}

			err := handler.forwarder.Forward(ctx, ports.DownstreamTarget{
				URL:    downstream.URL,
				Apikey: downstream.Apikey,
			}, call)

			<-forwardSlots

			if err != nil {
				handler.log(ctx, domain.LogLevelError, domain.LogSourceDownstream, downstreamLine(call, downstream.URL, err.Error()))

				continue
			}

			handler.log(ctx, domain.LogLevelInfo, domain.LogSourceDownstream, downstreamLine(call, downstream.URL, "success"))
		}
	}()
}

// downstreamLine is the line for every downstream the server posts a call to, whether
// that server took it or refused it: this line names the receiving server, and the
// neighbouring ingest line already names the recording.
func downstreamLine(call domain.Call, url string, outcome string) string {
	return fmt.Sprintf("call from system %d talkgroup %d sent to %s: %s",
		call.System.SystemRef, call.Talkgroup.TalkgroupRef, url, outcome)
}

// convertAudio converts the call's audio and replaces it with the converted file; when
// the converter cannot convert, the original audio is kept, as in the legacy server, and
// this server records why. It is also the step that gives the call its known length,
// from the first source that can name it: the uploader's own count, which the server
// believes and never re-measures; the conversion's report of the file it wrote; and
// ffprobe's measurement of the audio actually stored. A call can end with no known
// length: the length is a learned fact, never a reason to lose a recording.
func (handler *IngestCallHandler) convertAudio(ctx context.Context, call *domain.Call, options domain.Options, tags domain.Tags) error {
	// The recorder counted its own recording: the server takes its word, measures
	// nothing, and runs the configured conversion all the same.
	known := call.AudioLength > 0

	if options.AudioConversion == domain.AudioConversionDisabled {
		if !known {
			handler.probeLength(ctx, call)
		}

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
		// The original audio is kept, and it is that kept audio the probe measures, so
		// the stored call's length describes the stored call's audio.
		if !known {
			handler.probeLength(ctx, call)
		}

		return err
	}

	call.Audio = converted.Data
	call.AudioFilename = converted.Filename
	call.AudioMime = converted.Mime

	if !known {
		if converted.Duration > 0 {
			call.AudioLength = converted.Duration
		} else {
			// The conversion wrote the stored recording without naming its length;
			// the probe measures that recording, not the one that went in.
			handler.probeLength(ctx, call)
		}
	}

	return nil
}

// probeLength asks ffprobe how long the audio the server is about to store lasts, and
// records the answer on the call. A probe that cannot answer says so once, under the
// conversion's log subsystem, and the call keeps no length.
func (handler *IngestCallHandler) probeLength(ctx context.Context, call *domain.Call) {
	length, err := handler.prober.Probe(ctx, ports.AudioFile{
		Filename: call.AudioFilename,
		Mime:     call.AudioMime,
		Data:     call.Audio,
	})
	if err != nil {
		handler.log(ctx, domain.LogLevelWarn, domain.LogSourceFFmpeg, fmt.Sprintf("the recording length could not be measured: %v", err.Error()))

		return
	}

	call.AudioLength = length
}

// log writes one entry to the server log, naming the part of the server the entry is
// about: ingest, ffmpeg, delayer, or downstream, because the log view filters on it.
func (handler *IngestCallHandler) log(ctx context.Context, level string, source domain.LogSource, message string) {
	if err := handler.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: handler.clock.Now(),
		Level:    level,
		Source:   source,
		Message:  message,
	}); err != nil {
		// A log entry that cannot be written must not change what the server did: the
		// ingest decision is already taken and delivered.
		return
	}
}

func (handler *IngestCallHandler) failed(err error) (IngestCallResult, error) {
	return IngestCallResult{Outcome: IngestCallFailed}, err
}
