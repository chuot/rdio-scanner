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

package dto

import (
	"encoding/json"
	"time"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// AdminConfigPayload is the whole configuration an administrator is shown: every section of it, and
// the version of the server that is answering.
//
// It is a different payload from the one a listener is given. A listener is shown what its access
// reaches, with the technical numbers replaced by the names a display uses; an administrator is shown
// what the server stores, including the identifiers the server hands out and the settings a listener
// has no business knowing.
type AdminConfigPayload struct {
	Accesses    []domain.Access
	Apikeys     []domain.Apikey
	Dirwatches  []domain.Dirwatch
	Downstreams []domain.Downstream
	Snapshot    ports.ConfigSnapshot
	Version     string
}

// MarshalJSON writes the configuration.
func (payload AdminConfigPayload) MarshalJSON() ([]byte, error) {
	body := map[string]any{
		"access":      AccessListPayload(payload.Accesses),
		"apikeys":     ApikeyListPayload(payload.Apikeys),
		"dirwatch":    DirwatchListPayload(payload.Dirwatches),
		"downstreams": DownstreamListPayload(payload.Downstreams),
		"groups":      GroupListPayload(payload.Snapshot.Groups),
		"options":     OptionsPayload(payload.Snapshot.Options),
		"systems":     SystemListPayload(payload.Snapshot.Systems),
		"tags":        TagListPayload(payload.Snapshot.Tags),
		"version":     payload.Version,
	}

	return json.Marshal(body)
}

// AdminConfigReply is what the configuration endpoint answers with: the configuration, whether the
// stored administrator password is still the one the server ships with, and — only when the server was
// started with DOCKER in its environment — the fact that it thinks it is running in a container.
type AdminConfigReply struct {
	Config             AdminConfigPayload
	PasswordNeedChange bool
	Docker             bool
}

// MarshalJSON writes the answer.
func (reply AdminConfigReply) MarshalJSON() ([]byte, error) {
	body := map[string]any{
		"config":             reply.Config,
		"passwordNeedChange": reply.PasswordNeedChange,
	}

	if reply.Docker {
		body["docker"] = true
	}

	return json.Marshal(body)
}

// AccessListPayload is the access codes an administrator is shown. Each section of the configuration
// is also answered on its own, as this list with nothing around it.
type AccessListPayload []domain.Access

// MarshalJSON writes the access codes.
//
// An expiry is written as the moment it is, and not as the number of seconds the server counts it in.
// An access that never expires is written without an expiry at all.
func (payload AccessListPayload) MarshalJSON() ([]byte, error) {
	out := []map[string]any{}

	for _, access := range payload {
		entry := accessEntry(access)

		out = append(out, entry)
	}

	return json.Marshal(out)
}

func accessEntry(access domain.Access) map[string]any {
	entry := map[string]any{
		"id":      access.ID,
		"code":    access.Code,
		"ident":   access.Ident,
		"systems": access.Scope.Value(),
	}

	if access.Expiration > 0 {
		entry["expiration"] = time.Unix(int64(access.Expiration), 0)
	}

	if access.Limit > 0 {
		entry["limit"] = access.Limit
	}

	if access.Order > 0 {
		entry["order"] = access.Order
	}

	return entry
}

// ApikeyListPayload is the upload keys an administrator is shown.
type ApikeyListPayload []domain.Apikey

// MarshalJSON writes the upload keys. Whether a key is switched off is always written, including when
// it is not.
func (payload ApikeyListPayload) MarshalJSON() ([]byte, error) {
	out := []map[string]any{}

	for _, apikey := range payload {
		entry := map[string]any{
			"id":       apikey.ID,
			"disabled": apikey.Disabled,
			"ident":    apikey.Ident,
			"key":      apikey.Key,
			"systems":  apikey.Scope.Value(),
		}

		if apikey.Order > 0 {
			entry["order"] = apikey.Order
		}

		out = append(out, entry)
	}

	return json.Marshal(out)
}

// DirwatchListPayload is the recording directories an administrator is shown.
type DirwatchListPayload []domain.Dirwatch

// MarshalJSON writes the directory watches.
func (payload DirwatchListPayload) MarshalJSON() ([]byte, error) {
	out := []map[string]any{}

	for _, dirwatch := range payload {
		entry := map[string]any{
			"id":          dirwatch.ID,
			"delay":       dirwatch.Delay,
			"deleteAfter": dirwatch.DeleteAfter,
			"directory":   dirwatch.Directory,
			"disabled":    dirwatch.Disabled,
			"extension":   dirwatch.Extension,
			"frequency":   dirwatch.Frequency,
			"mask":        dirwatch.Mask,
		}

		if len(dirwatch.Kind) > 0 {
			entry["type"] = dirwatch.Kind
		}

		if dirwatch.Order > 0 {
			entry["order"] = dirwatch.Order
		}

		if dirwatch.SiteID > 0 {
			entry["siteId"] = dirwatch.SiteID
		}

		if dirwatch.SystemID > 0 {
			entry["systemId"] = dirwatch.SystemID
		}

		if dirwatch.TalkgroupID > 0 {
			entry["talkgroupId"] = dirwatch.TalkgroupID
		}

		out = append(out, entry)
	}

	return json.Marshal(out)
}

// DownstreamListPayload is the servers this one forwards its calls to.
type DownstreamListPayload []domain.Downstream

// MarshalJSON writes the downstream servers.
func (payload DownstreamListPayload) MarshalJSON() ([]byte, error) {
	out := []map[string]any{}

	for _, downstream := range payload {
		entry := map[string]any{
			"id":       downstream.ID,
			"apikey":   downstream.Apikey,
			"disabled": downstream.Disabled,
			"systems":  downstream.Scope.Value(),
			"url":      downstream.URL,
		}

		if downstream.Order > 0 {
			entry["order"] = downstream.Order
		}

		out = append(out, entry)
	}

	return json.Marshal(out)
}

// GroupListPayload is the talkgroup groups an administrator is shown.
type GroupListPayload domain.Groups

// MarshalJSON writes the groups.
func (payload GroupListPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(groupEntries([]domain.Group(payload)))
}

// TagListPayload is the talkgroup tags an administrator is shown.
type TagListPayload domain.Tags

// MarshalJSON writes the tags.
func (payload TagListPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(tagEntries([]domain.Tag(payload)))
}

// SystemListPayload is the systems an administrator is shown, with everything stored under each one.
type SystemListPayload domain.Systems

// MarshalJSON writes the systems.
func (payload SystemListPayload) MarshalJSON() ([]byte, error) {
	out := []map[string]any{}

	for _, system := range payload {
		entry := map[string]any{
			"id":           system.ID,
			"autoPopulate": system.AutoPopulate,
			"label":        system.Label,
			"sites":        siteEntries(system.Sites),
			"systemRef":    system.SystemRef,
			"talkgroups":   talkgroupEntriesForAdmin(system.Talkgroups),
			"units":        unitEntries(system.Units),
		}

		if len(system.Alert) > 0 {
			entry["alert"] = system.Alert
		}

		if len(system.Blacklists) > 0 {
			entry["blacklists"] = system.Blacklists
		}

		if system.Delay > 0 {
			entry["delay"] = system.Delay
		}

		if len(system.Kind) > 0 {
			entry["type"] = system.Kind
		}

		if len(system.LED) > 0 {
			entry["led"] = system.LED
		}

		if system.Order > 0 {
			entry["order"] = system.Order
		}

		out = append(out, entry)
	}

	return json.Marshal(out)
}

// talkgroupEntriesForAdmin writes the talkgroups stored under a system.
//
// A talkgroup's display order is written under the key `talkgroup`. That is not a name this server
// would choose — the key says talkgroup and holds a number — but it is the key the legacy server
// writes it under, and the web application has had to correct for it before it can show a talkgroup
// list in order. Recorded as Q59.
func talkgroupEntriesForAdmin(talkgroups domain.Talkgroups) []map[string]any {
	out := []map[string]any{}

	for _, talkgroup := range talkgroups {
		entry := map[string]any{
			"id":           talkgroup.ID,
			"groupIds":     talkgroup.GroupIDs,
			"label":        talkgroup.Label,
			"name":         talkgroup.Name,
			"talkgroupRef": talkgroup.TalkgroupRef,
		}

		if len(talkgroup.Alert) > 0 {
			entry["alert"] = talkgroup.Alert
		}

		if talkgroup.Delay > 0 {
			entry["delay"] = talkgroup.Delay
		}

		if talkgroup.Frequency > 0 {
			entry["frequency"] = talkgroup.Frequency
		}

		if len(talkgroup.Kind) > 0 {
			entry["type"] = talkgroup.Kind
		}

		if len(talkgroup.LED) > 0 {
			entry["led"] = talkgroup.LED
		}

		if talkgroup.Order > 0 {
			entry["talkgroup"] = talkgroup.Order
		}

		if talkgroup.TagID > 0 {
			entry["tagId"] = talkgroup.TagID
		}

		out = append(out, entry)
	}

	return out
}

// siteEntries writes the sites stored under a system.
//
// A site is written as belonging to the system it is stored under. The legacy server writes every site
// as belonging to no system at all, because the query that reads sites never asks for that column —
// the approved divergence recorded in decision-parity-verification.md as Q58.
func siteEntries(sites domain.Sites) []map[string]any {
	out := []map[string]any{}

	for _, site := range sites {
		entry := map[string]any{
			"id":       site.ID,
			"label":    site.Label,
			"siteRef":  site.SiteRef,
			"systemId": site.SystemID,
		}

		if site.Order > 0 {
			entry["order"] = site.Order
		}

		out = append(out, entry)
	}

	return out
}

// OptionsPayload is the settings an administrator is shown.
//
// It is not everything the server holds about its settings: the stored password, whether that password
// still needs changing, and the secret that signs administrator sessions are never in this payload.
type OptionsPayload domain.Options

// optionsBody is the settings an administrator is shown, in the order they are written.
type optionsBody struct {
	AudioConversion             uint   `json:"audioConversion"`
	AutoPopulate                bool   `json:"autoPopulate"`
	Branding                    string `json:"branding"`
	DimmerDelay                 uint   `json:"dimmerDelay"`
	DisableDuplicateDetection   bool   `json:"disableDuplicateDetection"`
	DuplicateDetectionTimeFrame uint   `json:"duplicateDetectionTimeFrame"`
	Email                       string `json:"email"`
	KeypadBeeps                 string `json:"keypadBeeps"`
	MaxClients                  uint   `json:"maxClients"`
	PlaybackGoesLive            bool   `json:"playbackGoesLive"`
	PruneDays                   uint   `json:"pruneDays"`
	ShowListenersCount          bool   `json:"showListenersCount"`
	SortTalkgroups              bool   `json:"sortTalkgroups"`
	Time12hFormat               bool   `json:"time12hFormat"`
}

// MarshalJSON writes the settings.
func (payload OptionsPayload) MarshalJSON() ([]byte, error) {
	options := domain.Options(payload)

	return json.Marshal(optionsBody{
		AudioConversion:             options.AudioConversion,
		AutoPopulate:                options.AutoPopulate,
		Branding:                    options.Branding,
		DimmerDelay:                 options.DimmerDelay,
		DisableDuplicateDetection:   options.DisableDuplicateDetection,
		DuplicateDetectionTimeFrame: options.DuplicateDetectionTimeFrame,
		Email:                       options.Email,
		KeypadBeeps:                 options.KeypadBeeps,
		MaxClients:                  options.MaxClients,
		PlaybackGoesLive:            options.PlaybackGoesLive,
		PruneDays:                   options.PruneDays,
		ShowListenersCount:          options.ShowListenersCount,
		SortTalkgroups:              options.SortTalkgroups,
		Time12hFormat:               options.Time12hFormat,
	})
}
