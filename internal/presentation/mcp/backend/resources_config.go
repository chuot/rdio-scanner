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

package backend

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/domain"
)

// The administrator's configuration resources: the sections an operator inspects often enough to
// read them directly, with no tool call in between, and the two long lists a client browses by
// template. A resource is read with no arguments to decide over, so the administrator's credential
// is asked inside the read.
//
// Two of these inventory credentials, and a credential inventory read by an assistant must not hand
// out the credentials it inventories: the secret fields are replaced by a stated marker, and the
// inventory answers the questions it is read for — who a code belongs to, when it stops working,
// and what it is entitled to.

const (
	optionsURI       = "rdio-scanner://config/options"
	configSystemsURI = "rdio-scanner://config/systems"
	groupsURI        = "rdio-scanner://config/groups"
	tagsURI          = "rdio-scanner://config/tags"
	accessURI        = "rdio-scanner://config/access"
	apikeysURI       = "rdio-scanner://config/apikeys"
	dirwatchesURI    = "rdio-scanner://config/dirwatch"
	downstreamsURI   = "rdio-scanner://config/downstreams"
	alertsURI        = "rdio-scanner://alerts"
	systemLevelURI   = "rdio-scanner://systems/{systemRef}/{level}"
	logSliceURI      = "rdio-scanner://logs/{level}{?date}"
)

// maskedCredential marks the field a credential inventory replaced, so a reader of the inventory
// knows the value is deliberately absent and not simply missing.
const maskedCredential = "masked"

// registerConfigResources adds the administrator's configuration resources.
func registerConfigResources(target *mcp.Server, deps Deps) {
	section := func(uri, name, title, description string, build func(Deps, domain.Configuration) any) {
		target.AddResource(&mcp.Resource{
			URI:         uri,
			Name:        name,
			Title:       title,
			MIMEType:    "application/json",
			Description: description,
		}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			who, ok := CallerOf(ctx)
			if !ok {
				return nil, errNoCaller
			}
			if !isAdministrator(who) {
				return nil, newRefusal(administratorRequired)
			}

			configuration, err := deps.Config.Handle(ctx, application.GetConfigQuery{})
			if err != nil {
				return nil, err
			}

			return jsonResource(uri, build(deps, configuration))
		})
	}

	section(optionsURI, "options", "The server's options",
		"The options section as this server runs it: branding, the listener ceiling, the retention "+
			"horizon, audio conversion, duplicate detection, auto-populate, and the server's notice "+
			"that the administrator password still needs changing. The session-signing secret and the "+
			"password hash are not in the answer: they are marked as masked. Changing an option is "+
			"done with the configuration tool, not by reading this.",

		func(deps Deps, configuration domain.Configuration) any {
			return optionsView{
				AudioConversion:           configuration.Options.AudioConversion,
				AutoPopulate:              configuration.Options.AutoPopulate,
				Branding:                  configuration.Options.Branding,
				DisableDuplicateDetection: configuration.Options.DisableDuplicateDetection,
				Email:                     configuration.Options.Email,
				KeypadBeeps:               configuration.Options.KeypadBeeps,
				MaxClients:                configuration.Options.MaxClients,
				PruneDays:                 configuration.Options.PruneDays,
				ShowListenersCount:        configuration.Options.ShowListenersCount,
				SortTalkgroups:            configuration.Options.SortTalkgroups,
				PasswordNeedsChange:       configuration.Options.AdminPasswordNeedChange,
				Version:                   deps.Version,
				Masked:                    []string{"adminPasswordHash", "secret"},
			}
		})

	section(configSystemsURI, "systems", "The whole systems tree",
		"Every system with its talkgroups, sites, and units under it, as this server holds them. The "+
			"whole tree is long: an assistant that needs one system's talkgroups reads the level "+
			"template beside this one, and an assistant that needs the references to name filters "+
			"reads the caller's own projection.",

		func(_ Deps, configuration domain.Configuration) any {
			return systemsTree(configuration.Systems)
		})

	section(groupsURI, "groups", "The group inventory",
		"The groups this server sorts talkgroups into, with the alert and the display hint each one "+
			"carries. A search by group label stands for the talkgroups of that label the caller may "+
			"see.",

		func(_ Deps, configuration domain.Configuration) any {
			return groupsOf(configuration.Groups)
		})

	section(tagsURI, "tags", "The tag inventory",
		"The tags this server sorts talkgroups into, with the alert and the display hint each one "+
			"carries. A search by tag label stands for the talkgroups of that tag the caller may see.",

		func(_ Deps, configuration domain.Configuration) any {
			return tagsOf(configuration.Tags)
		})

	section(accessURI, "access", "The listener access codes, masked",
		"One row per access code this server entitles: whose code it is, when it stops working, how "+
			"many listeners it lets in at once, and which systems and talkgroups it grants. The code "+
			"itself is marked as masked: the point of reading this inventory is to know which "+
			"listeners exist and what they may see, and a code repeated into a chat log is a leaked "+
			"credential. An expired code is named as expired against this server's own clock.",

		func(deps Deps, configuration domain.Configuration) any {
			rows := []accessRow{}

			for _, access := range configuration.Access {
				rows = append(rows, accessRow{
					ID:      access.ID,
					Ident:   access.Ident,
					Expires: access.Expiration,
					Expired: access.Expiration > 0 && uint64(deps.Clock.Now().Unix()) > access.Expiration,
					Limit:   access.Limit,
					Order:   access.Order,
					Systems: scopeView(access.Scope),
					Code:    maskedCredential,
				})
			}

			return rows
		})

	section(apikeysURI, "apikeys", "The capture upload credentials, masked",
		"One row per upload credential this server accepts: which capture tool it belongs to, whether "+
			"it is switched off, and which systems and talkgroups it may upload to. The key itself is "+
			"marked as masked, for the same reason the access codes are.",

		func(_ Deps, configuration domain.Configuration) any {
			rows := []apikeyRow{}

			for _, apikey := range configuration.Apikeys {
				rows = append(rows, apikeyRow{
					ID:       apikey.ID,
					Ident:    apikey.Ident,
					Disabled: apikey.Disabled,
					Order:    apikey.Order,
					Systems:  scopeView(apikey.Scope),
					Key:      maskedCredential,
				})
			}

			return rows
		})

	section(dirwatchesURI, "dirwatch", "The watched directories",
		"One row per directory this server watches for recordings arriving from another application: "+
			"the folder, the filename mask it matches, the delay before a file is read, whether the "+
			"source file is deleted, and the system, site, and talkgroup the recordings are filed "+
			"under. A switched-off watch is named as switched off, because that is the answer when "+
			"the history has stopped growing.",

		func(_ Deps, configuration domain.Configuration) any {
			rows := []dirwatchRow{}

			for _, dirwatch := range configuration.Dirwatches {
				rows = append(rows, dirwatchRow{
					ID:          dirwatch.ID,
					Directory:   dirwatch.Directory,
					Kind:        dirwatch.Kind,
					Disabled:    dirwatch.Disabled,
					Delay:       dirwatch.Delay,
					Extension:   dirwatch.Extension,
					Frequency:   dirwatch.Frequency,
					Mask:        dirwatch.Mask,
					Order:       dirwatch.Order,
					DeleteAfter: dirwatch.DeleteAfter,
					SiteID:      dirwatch.SiteID,
					SystemID:    dirwatch.SystemID,
					TalkgroupID: dirwatch.TalkgroupID,
				})
			}

			return rows
		})

	section(downstreamsURI, "downstreams", "The downstream servers",
		"One row per server this server re-posts its recordings to: the address, whether it is "+
			"switched off, and which systems and talkgroups it forwards. The credential it posts with "+
			"is marked as masked: a forwarder's credential is a credential, and this inventory is read "+
			"to know where recordings go, not to hand the credential out.",

		func(_ Deps, configuration domain.Configuration) any {
			rows := []downstreamRow{}

			for _, downstream := range configuration.Downstreams {
				rows = append(rows, downstreamRow{
					ID:       downstream.ID,
					URL:      downstream.URL,
					Disabled: downstream.Disabled,
					Order:    downstream.Order,
					Systems:  scopeView(downstream.Scope),
					Apikey:   maskedCredential,
				})
			}

			return rows
		})

	section(alertsURI, "alerts", "The alert sequences",
		"The named alert sequences this server carries, with the tones each one is made of. They are "+
			"not configuration: nothing here changes them, and every client that shows alerts reads "+
			"the same set. An alert is named by its name in a capture payload.",

		func(_ Deps, _ domain.Configuration) any {
			return alerts()
		})

	// One level of one system, read by a template: the level lists are long enough that an
	// assistant asks for one system's talkgroups, and the whole tree is beside it.
	target.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: systemLevelURI,
		Name:        "system-level",
		Title:       "One level under one system",
		MIMEType:    "application/json",
		Description: "The talkgroups, the sites, or the units of one system, named by the reference the " +
			"systems projection names it with, and the level's name: talkgroups, sites, or units. A " +
			"system this server does not hold answers the empty list, the same answer a system with " +
			"nothing under it gets.",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, errNoCaller
		}
		if !isAdministrator(who) {
			return nil, newRefusal(administratorRequired)
		}

		systemRef, level, readable := parseSystemLevel(req.Params.URI)
		if !readable {
			return jsonResource(req.Params.URI, []jsonRow{})
		}

		configuration, err := deps.Config.Handle(ctx, application.GetConfigQuery{})
		if err != nil {
			return nil, err
		}

		system, found := configuration.Systems.ByRef(systemRef)
		if !found {
			return jsonResource(req.Params.URI, []jsonRow{})
		}

		switch level {
		case "talkgroups":
			return jsonResource(req.Params.URI, talkgroupsOf(system.Talkgroups))

		case "sites":
			return jsonResource(req.Params.URI, sitesOf(system.Sites))

		case "units":
			return jsonResource(req.Params.URI, unitsOf(system.Units))
		}

		return jsonResource(req.Params.URI, []jsonRow{})
	})

	// A browsable slice of the log: one severity, one day. The search tool takes every filter;
	// this is what a client reads while it is browsing.
	target.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: logSliceURI,
		Name:        "log-slice",
		Title:       "A slice of the server's log",
		MIMEType:    "application/json",
		Description: "One severity of the server's own log, narrowed to one day, oldest first: info, " +
			"warn, or error. A severity the server does not file entries under answers the empty " +
			"slice. Use the log search tool when the slice is not enough: it takes the subsystem, " +
			"the message substring, and the paging the log view accepts.",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, errNoCaller
		}
		if !isAdministrator(who) {
			return nil, newRefusal(administratorRequired)
		}

		level, day, readable := parseLogSlice(req.Params.URI)
		if !readable {
			return jsonResource(req.Params.URI, []logRow{})
		}

		page, err := deps.Logs.Handle(ctx, application.SearchLogsQuery{Search: domain.LogSearch{
			Date:      parseDay(day),
			Level:     level,
			Limit:     domain.DefaultLogSearchLimit,
			Ascending: true,
		}})
		if err != nil {
			return nil, err
		}

		return jsonResource(req.Params.URI, rowsOf(page.Results))
	})
}

// optionsView is the options section, with the secrets marked as masked.
type optionsView struct {
	AudioConversion           uint   `json:"audioConversion"`
	AutoPopulate              bool   `json:"autoPopulate"`
	Branding                  string `json:"branding"`
	DisableDuplicateDetection bool   `json:"disableDuplicateDetection"`
	Email                     string `json:"email"`
	KeypadBeeps               string `json:"keypadBeeps"`
	MaxClients                uint   `json:"maxClients"`
	PruneDays                 uint   `json:"pruneDays"`
	ShowListenersCount        bool   `json:"showListenersCount"`
	SortTalkgroups            bool   `json:"sortTalkgroups"`
	PasswordNeedsChange       bool   `json:"passwordNeedsChange"`
	Version                   string `json:"version"`

	// Masked names the fields this inventory deliberately does not answer.
	Masked []string `json:"masked"`
}

// accessRow is one listener access code, without its code.
type accessRow struct {
	ID      uint64 `json:"id"`
	Ident   string `json:"ident"`
	Expires uint64 `json:"expires"`
	Expired bool   `json:"expired"`
	Limit   uint   `json:"limit"`
	Order   uint   `json:"order"`

	// Systems is what the code grants: the star for everything, or the systems it names with the
	// talkgroups under each.
	Systems any    `json:"systems"`
	Code    string `json:"code"`
}

// apikeyRow is one capture upload credential, without its key.
type apikeyRow struct {
	ID       uint64 `json:"id"`
	Ident    string `json:"ident"`
	Disabled bool   `json:"disabled"`
	Order    uint   `json:"order"`
	Systems  any    `json:"systems"`
	Key      string `json:"key"`
}

// dirwatchRow is one watched directory.
type dirwatchRow struct {
	ID          uint64 `json:"id"`
	Directory   string `json:"directory"`
	Kind        string `json:"type"`
	Disabled    bool   `json:"disabled"`
	Delay       uint   `json:"delay"`
	Extension   string `json:"extension"`
	Frequency   uint   `json:"frequency"`
	Mask        string `json:"mask"`
	Order       uint   `json:"order"`
	DeleteAfter bool   `json:"deleteAfter"`
	SiteID      uint64 `json:"siteId"`
	SystemID    uint64 `json:"systemId"`
	TalkgroupID uint64 `json:"talkgroupId"`
}

// downstreamRow is one downstream server, without the credential it posts with.
type downstreamRow struct {
	ID       uint64 `json:"id"`
	URL      string `json:"url"`
	Disabled bool   `json:"disabled"`
	Order    uint   `json:"order"`
	Systems  any    `json:"systems"`
	Apikey   string `json:"apikey"`
}

// systemRow is one system with its levels under it.
type systemRow struct {
	ID           uint64         `json:"id"`
	SystemRef    uint           `json:"systemRef"`
	Label        string         `json:"label"`
	Order        uint           `json:"order"`
	Kind         string         `json:"type"`
	Delay        uint           `json:"delay"`
	AutoPopulate bool           `json:"autoPopulate"`
	Alert        string         `json:"alert"`
	LED          string         `json:"led"`
	Talkgroups   []talkgroupRow `json:"talkgroups"`
	Sites        []siteRow      `json:"sites"`
	Units        []unitRow      `json:"units"`
}

type talkgroupRow struct {
	ID           uint64   `json:"id"`
	TalkgroupRef uint     `json:"talkgroupRef"`
	Label        string   `json:"label"`
	Name         string   `json:"name"`
	Kind         string   `json:"type"`
	Frequency    uint     `json:"frequency"`
	Delay        uint     `json:"delay"`
	Order        uint     `json:"order"`
	TagID        uint64   `json:"tagId"`
	GroupIDs     []uint64 `json:"groupIds"`
	Alert        string   `json:"alert"`
	LED          string   `json:"led"`
}

type siteRow struct {
	ID       uint64 `json:"id"`
	SiteRef  uint   `json:"siteRef"`
	Label    string `json:"label"`
	Order    uint   `json:"order"`
	SystemID uint64 `json:"systemId"`
}

type unitRow struct {
	ID       uint64 `json:"id"`
	UnitRef  uint   `json:"unitRef"`
	Label    string `json:"label"`
	Order    uint   `json:"order"`
	SystemID uint64 `json:"systemId"`
	UnitFrom uint   `json:"unitFrom"`
	UnitTo   uint   `json:"unitTo"`
}

// labelRow is one group or tag.
type labelRow struct {
	ID    uint64 `json:"id"`
	Label string `json:"label"`
	Alert string `json:"alert"`
	LED   string `json:"led"`
	Order uint   `json:"order"`
}

// jsonRow is a row of a level list, answered as the row itself.
type jsonRow = map[string]any

func systemsTree(systems domain.Systems) []systemRow {
	answer := []systemRow{}

	for _, system := range systems {
		answer = append(answer, systemRow{
			ID:           system.ID,
			SystemRef:    system.SystemRef,
			Label:        system.Label,
			Order:        system.Order,
			Kind:         system.Kind,
			Delay:        system.Delay,
			AutoPopulate: system.AutoPopulate,
			Alert:        system.Alert,
			LED:          system.LED,
			Talkgroups:   talkgroupsOf(system.Talkgroups),
			Sites:        sitesOf(system.Sites),
			Units:        unitsOf(system.Units),
		})
	}

	return answer
}

func talkgroupsOf(talkgroups domain.Talkgroups) []talkgroupRow {
	answer := []talkgroupRow{}

	for _, talkgroup := range talkgroups {
		answer = append(answer, talkgroupRow{
			ID:           talkgroup.ID,
			TalkgroupRef: talkgroup.TalkgroupRef,
			Label:        talkgroup.Label,
			Name:         talkgroup.Name,
			Kind:         talkgroup.Kind,
			Frequency:    talkgroup.Frequency,
			Delay:        talkgroup.Delay,
			Order:        talkgroup.Order,
			TagID:        talkgroup.TagID,
			GroupIDs:     talkgroup.GroupIDs,
			Alert:        talkgroup.Alert,
			LED:          talkgroup.LED,
		})
	}

	return answer
}

func sitesOf(sites domain.Sites) []siteRow {
	answer := []siteRow{}

	for _, site := range sites {
		answer = append(answer, siteRow{
			ID: site.ID, SiteRef: site.SiteRef, Label: site.Label, Order: site.Order, SystemID: site.SystemID,
		})
	}

	return answer
}

func unitsOf(units domain.Units) []unitRow {
	answer := []unitRow{}

	for _, unit := range units {
		answer = append(answer, unitRow{
			ID: unit.ID, UnitRef: unit.UnitRef, Label: unit.Label, Order: unit.Order,
			SystemID: unit.SystemID, UnitFrom: unit.UnitFrom, UnitTo: unit.UnitTo,
		})
	}

	return answer
}

// groupsOf is the group inventory, and tagsOf the tag inventory: the same shape, because the
// operator reads them the same way.
func groupsOf(groups domain.Groups) []labelRow {
	answer := []labelRow{}

	for _, group := range groups {
		answer = append(answer, labelRow{
			ID: group.ID, Label: group.Label, Alert: group.Alert, LED: group.LED, Order: group.Order,
		})
	}

	return answer
}

func tagsOf(tags domain.Tags) []labelRow {
	answer := []labelRow{}

	for _, tag := range tags {
		answer = append(answer, labelRow{
			ID: tag.ID, Label: tag.Label, Alert: tag.Alert, LED: tag.LED, Order: tag.Order,
		})
	}

	return answer
}

// scopeView is what a credential is entitled to, in the shape the configuration payloads use: the
// star for everything the server holds, or the systems it names with the talkgroups under each.
func scopeView(scope domain.AccessScope) any {
	if !scope.Present {
		return nil
	}

	if scope.Wildcard {
		return "*"
	}

	type scopedSystem struct {
		SystemRef  uint `json:"systemRef"`
		Talkgroups any  `json:"talkgroups"`
	}

	answer := []scopedSystem{}

	for _, system := range scope.Systems {
		if system.TalkgroupsWildcard {
			answer = append(answer, scopedSystem{SystemRef: system.SystemRef, Talkgroups: "*"})

			continue
		}

		refs := []uint{}
		refs = append(refs, system.TalkgroupRefs...)

		answer = append(answer, scopedSystem{SystemRef: system.SystemRef, Talkgroups: refs})
	}

	return answer
}

// parseSystemLevel reads the system reference and the level out of a level URI. The second answer
// says whether the URI named a number at all.
func parseSystemLevel(uri string) (uint, string, bool) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return 0, "", false
	}

	fields := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(fields) != 2 {
		return 0, "", false
	}

	systemRef, err := strconv.ParseUint(fields[0], 10, 32)
	if err != nil {
		return 0, "", false
	}

	return uint(systemRef), fields[1], true
}

// parseLogSlice reads the severity and the day out of a log slice URI. A severity outside the
// vocabulary the server files entries under is not readable as a slice.
func parseLogSlice(uri string) (string, string, bool) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", false
	}

	level := strings.Trim(parsed.Path, "/")

	if level != domain.LogLevelInfo && level != domain.LogLevelWarn && level != domain.LogLevelError {
		return "", "", false
	}

	day := dayOf(parsed.Query().Get("date"))

	return level, day, true
}
