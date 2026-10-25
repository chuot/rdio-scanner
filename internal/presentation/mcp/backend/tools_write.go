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

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/domain"
)

// The write capabilities: each states the change, asks the operator to
// confirm through the protocol's own elicitation, and writes only in the
// confirmed round; the saves use the same use cases the page saves with.
// see the every-write-asks-the-operator-before-it-saves maintainer note.

// registerWriteTools adds the writes to the administrator's set alone,
// and their handlers check the credential again whatever the catalog
// says; registering each in changes keeps the activity list from drifting.
// see the every-write-asks-the-operator-before-it-saves maintainer note.
func registerWriteTools(target *mcp.Server, deps Deps, changes map[string]bool) {
	addWriteTool(target, changes, &mcp.Tool{
		Name:  "manage_config",
		Title: "Change one section of the configuration",
		Description: "Change one section of this server's configuration, or one level of one system, " +
			"as a whole-section replace: the payload is the section as it should be after the change, " +
			"in the shape the configuration endpoints take. The tool reads the section as it is, works " +
			"out the entries it will add, change, and remove, shows them, and asks the operator to " +
			"confirm; it writes only in the confirmed round, and a submission that changes nothing " +
			"asks nothing and writes nothing. A section replace deletes the entries the payload does " +
			"not carry, and the ask names them. For the settings, the server's own save resets every " +
			"setting a submission does not name to its default, and the ask names those too. " +
			"Credentials the change touches are masked in the answer. Reads and writes " +
			"GET and PUT /api/admin/config/{section}, and GET and PUT " +
			"/api/admin/config/systems/{systemId}/{sites|talkgroups|units}.",
		Annotations: writes(true),
	}, manageConfigHandler(deps))

	addWriteTool(target, changes, &mcp.Tool{
		Name:  "manage_access_user",
		Title: "Onboard or remove a listener",
		Description: "Add a listener access code, or remove every code carrying one identity. The add " +
			"form names the identity, the systems and talkgroups the code is entitled to, and " +
			"optionally when it stops working, how many listeners it lets in at once, its display " +
			"order, and the code itself; when no code is named the server generates one, and no " +
			"answer of this tool carries a code — the operator reads it from the administrator's " +
			"access page. The remove form deletes every code sharing the identity, and the ask names " +
			"each one that will go. Both forms show the change and ask the operator first, and write " +
			"only in the confirmed round. Reads GET /api/admin/config/{access}, and writes " +
			"POST /api/admin/user-add and POST /api/admin/user-remove.",
		Annotations: writes(true),
	}, manageAccessUserHandler(deps))

	addWriteTool(target, changes, &mcp.Tool{
		Name:  "rotate_admin_password",
		Title: "Replace the administrator credential",
		Description: "Replace the credential an administrator signs in with. Every open administrator " +
			"session stops working the moment it is replaced, and the operator has to sign in again. " +
			"The tool shows what it is about to do, asks the operator to confirm, and writes only in " +
			"the confirmed round; the new credential is never repeated in the answer. A current " +
			"credential is compared only when the caller names one, which is what this server's " +
			"credential change does. Writes POST /api/admin/password.",
		Annotations: writes(false),
	}, rotateAdminPasswordHandler(deps))
}

// addWriteTool registers one write capability and records its name, by the
// code that registers it, so the list of capabilities that change the
// deployment cannot name one that is not a write and cannot miss one that
// is.
func addWriteTool[In, Out any](target *mcp.Server, changes map[string]bool, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	changes[tool.Name] = true

	mcp.AddTool(target, tool, handler)
}

// writes is the annotation set a write capability carries: it is not a
// read, it can modify or delete data, and the world it touches is this
// deployment. An assistant reads these before it plans a sequence of
// calls.
func writes(idempotent bool) *mcp.ToolAnnotations {
	destructive := true
	closed := false

	return &mcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: &destructive,
		IdempotentHint:  idempotent,
		OpenWorldHint:   &closed,
	}
}

// The confirmation. The operator in the loop is the point of a write
// capability: the surface states the change, the client shows it, and the
// write starts on the operator's answer. A client that cannot reach the
// operator cannot make the change.

// confirmationForm is the operator's answer at an ask. The confirmation is
// an explicit yes in the form, not the absence of a no.
type confirmationForm struct {
	Confirmed bool `json:"confirmed"`
}

// confirmationKey is the label the ask is answered under. The client echoes
// it back with the operator's answer, and the handler reads under it.
const confirmationKey = "confirm"

const (
	// confirmationUnavailable is the answer to a caller whose client cannot ask the operator.
	confirmationUnavailable = "this change needs the operator's confirmation, and this client cannot ask the operator for it"

	// confirmationDeclined is the answer to a caller whose operator would not confirm.
	confirmationDeclined = "the operator did not confirm the change, and nothing was written"
)

// confirmationState is the token the client echoes back on the confirmed
// round. It names the capability and the change the operator was asked
// about, so the round that writes proves it is answering that ask.
type confirmationState struct {
	Stage   string `json:"stage"`
	Tool    string `json:"tool"`
	Summary string `json:"summary"`
}

// canAsk reports whether the caller's client can show the operator an ask.
// A client that advertises no elicitation, or only URL elicitation, has
// no way to bring its operator into the loop.
func canAsk(req *mcp.CallToolRequest) bool {
	params := req.Session.InitializeParams()
	if params == nil || params.Capabilities == nil || params.Capabilities.Elicitation == nil {
		return false
	}

	elicitation := params.Capabilities.Elicitation

	// A client that names neither form nor URL is read as supporting form
	// elicitation, which is what the protocol says a client that predates the
	// modes means.
	if elicitation.Form == nil && elicitation.URL != nil {
		return false
	}

	return true
}

// askResult is the ask in the protocol's multi-round-trip pattern: the
// change stated in the message, and the state the confirmed round resumes
// from; the write starts only in the round carrying the operator's answer.
// see the every-write-asks-the-operator-before-it-saves maintainer note.
func askResult(message, state string) (*mcp.CallToolResult, error) {
	schema, err := jsonschema.For[confirmationForm](nil)
	if err != nil {
		return nil, fmt.Errorf("building the confirmation form: %w", err)
	}

	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{
			confirmationKey: &mcp.ElicitParams{
				Mode:            "form",
				Message:         message,
				RequestedSchema: schema,
			},
		},
		RequestState: state,
	}, nil
}

// confirmationStateOf is the state token for one capability's ask.
func confirmationStateOf(tool, summary string) string {
	body, err := json.Marshal(confirmationState{Stage: "confirm", Tool: tool, Summary: summary})
	if err != nil {
		return ""
	}

	return string(body)
}

// awaitingConfirmation reports whether this round is the one that asks. A
// call with no answers, or with answers and no state to resume from, has to
// be shown the ask.
func awaitingConfirmation(req *mcp.CallToolRequest) bool {
	return len(req.Params.InputResponses) == 0 || req.Params.RequestState == ""
}

// confirmed reports whether the operator answered the ask with a yes in the
// form. A decline, a dismissal, an answer that is not the ask's answer, and
// a form whose confirmation is not a yes all confirm nothing.
func confirmed(req *mcp.CallToolRequest) bool {
	answer, ok := req.Params.InputResponses[confirmationKey]
	if !ok {
		return false
	}

	elicited, ok := answer.(*mcp.ElicitResult)
	if !ok || elicited.Action != "accept" {
		return false
	}

	yes, ok := elicited.Content["confirmed"].(bool)

	return ok && yes
}

// askToConfirm is the confirmation round every write capability goes
// through, so none can write on a round that carries no answer: it
// answers error, refusal, or ask, in that reading order, and only when
// all three are empty has the operator confirmed and may the handler write.
func askToConfirm(req *mcp.CallToolRequest, tool, message, summary string) (*mcp.CallToolResult, string, error) {
	if !awaitingConfirmation(req) {
		if confirmed(req) {
			return nil, "", nil
		}

		return nil, confirmationDeclined, nil
	}

	if !canAsk(req) {
		return nil, confirmationUnavailable, nil
	}

	asked, err := askResult(message, confirmationStateOf(tool, summary))
	if err != nil {
		return nil, "", err
	}

	return asked, "", nil
}

// proposedChange is a change the capability has worked out and can make:
// the answer that shows it, the words the ask says, and the write that
// makes it. The write is a closure over the command, so nothing is
// submitted that the diff did not describe.
type proposedChange struct {
	answer  configChangeAnswer
	message string
	apply   func(context.Context) (application.UpdateConfigurationResult, error)
}

// empty reports whether the change would change nothing. A no-op is not
// worth the operator's attention, and asking for one trains the operator to
// confirm without reading.
func (proposal proposedChange) empty() bool {
	answer := proposal.answer

	return len(answer.Added) == 0 && len(answer.Changed) == 0 && len(answer.Removed) == 0 &&
		len(answer.Settings) == 0 && len(answer.Reset) == 0
}

// summary is the change in the fewest words the state token can carry: the
// counts of what will change. The client echoes the state back, so it is
// a state a reader of the wire can follow.
func (proposal proposedChange) summary() string {
	return fmt.Sprintf("%s: %d added, %d changed, %d removed, %d settings, %d reset",
		proposal.answer.Section, len(proposal.answer.Added), len(proposal.answer.Changed),
		len(proposal.answer.Removed), len(proposal.answer.Settings), len(proposal.answer.Reset))
}

// manage_config

// manageConfigInput is a section change: the section, the system a level
// belongs to, and the section as it should be after the change.
type manageConfigInput struct {
	// Section is one of the eight configuration sections, or one of the three levels of a system.
	Section string `json:"section"`

	// System is the reference of the system a level belongs to. It is required for a level and
	// means nothing for a section.
	System uint `json:"system,omitempty"`

	// Payload is the section after the change: an array of rows for a section and a level, an
	// object of the settings to write for the settings.
	Payload any `json:"payload"`
}

// configChangeAnswer is a section change shown back. The rows are the rows
// the configuration resources answer, so the section read and the section
// shown before a change are one shape, with the same fields masked.
type configChangeAnswer struct {
	Section  string          `json:"section"`
	System   uint            `json:"system,omitempty"`
	Outcome  string          `json:"outcome"`
	Current  any             `json:"current,omitempty"`
	Added    []any           `json:"added,omitempty"`
	Changed  []changedEntry  `json:"changed,omitempty"`
	Removed  []any           `json:"removed,omitempty"`
	Settings []settingChange `json:"settings,omitempty"`
	Reset    []string        `json:"reset,omitempty"`
	Saved    []string        `json:"saved,omitempty"`
	Note     string          `json:"note,omitempty"`
}

// changedEntry is one entry as it is and as the change would make it.
type changedEntry struct {
	Before any `json:"before"`
	After  any `json:"after"`
}

// settingChange is one setting as it is and as the change would make it.
type settingChange struct {
	Name   string `json:"name"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

func manageConfigHandler(deps Deps) func(context.Context, *mcp.CallToolRequest, manageConfigInput) (*mcp.CallToolResult, configChangeAnswer, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in manageConfigInput) (*mcp.CallToolResult, configChangeAnswer, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, configChangeAnswer{}, errNoCaller
		}
		if !isAdministrator(who) {
			return refused(administratorRequired), configChangeAnswer{}, nil
		}

		configuration, err := deps.Config.Handle(ctx, application.GetConfigQuery{})
		if err != nil {
			return nil, configChangeAnswer{}, err
		}

		proposal, refusal := proposeChange(deps, configuration, in)
		if refusal != "" {
			return refused(refusal), configChangeAnswer{}, nil
		}

		answer := proposal.answer

		if proposal.empty() {
			answer.Outcome = "nothing to change"
			answer.Note = "the section was submitted exactly as this server holds it, so there was " +
				"nothing to confirm and nothing was written"

			return nil, answer, nil
		}

		asked, refusal, err := askToConfirm(req, "manage_config", proposal.message, proposal.summary())
		if err != nil {
			return nil, configChangeAnswer{}, err
		}
		if refusal != "" {
			return refused(refusal), configChangeAnswer{}, nil
		}
		if asked != nil {
			return asked, configChangeAnswer{}, nil
		}

		result, err := proposal.apply(ctx)
		if err != nil {
			return refused("the change was confirmed, and this server could not store it: " +
				string(result.Outcome)), configChangeAnswer{}, nil
		}

		answer.Outcome = "saved"
		for _, section := range result.Saved {
			answer.Saved = append(answer.Saved, string(section))
		}
		answer.Note = "the change is stored, and this server announced the configuration that " +
			"resulted the way its own administrator page does"

		return nil, answer, nil
	}
}

// proposeChange works out what a submission would do, and hands back the
// change to show, the words to ask with, and the write to make it. A
// submission this server cannot make is refused before anything is asked.
func proposeChange(deps Deps, configuration domain.Configuration, in manageConfigInput) (proposedChange, string) {
	if application.IsSystemSubSection(in.Section) {
		return proposeLevel(deps, configuration, in)
	}

	if application.IsConfigSection(in.Section) {
		return proposeSection(deps, configuration, in)
	}

	return proposedChange{}, fmt.Sprintf("this server has no configuration section named %q: the "+
		"sections are %s, and a system's levels are sites, talkgroups, and units",
		in.Section, strings.Join(sectionNames(), ", "))
}

// sectionNames is the vocabulary of sections the tool takes, in the order the server saves them.
func sectionNames() []string {
	names := []string{}

	for _, section := range []application.ConfigSection{
		application.AccessSection, application.ApikeysSection, application.DirwatchSection,
		application.DownstreamsSection, application.GroupsSection, application.OptionsSection,
		application.SystemsSection, application.TagsSection,
	} {
		names = append(names, string(section))
	}

	return names
}

func proposeSection(deps Deps, configuration domain.Configuration, in manageConfigInput) (proposedChange, string) {
	if application.ConfigSection(in.Section) == application.OptionsSection {
		return proposeSettings(deps, configuration, in)
	}

	rows, ok := in.Payload.([]any)
	if !ok {
		return proposedChange{}, fmt.Sprintf("the %s section is submitted as the whole section: an "+
			"array of the rows the section should hold after the change", in.Section)
	}

	section := application.ConfigSection(in.Section)
	what := "the " + in.Section + " section"

	switch section {
	case application.AccessSection:
		parsed := domain.ParseAccesses(rows)

		return listProposal(in.Section, 0, what,
			anyRows(accessRowsOf(deps.Clock.Now(), configuration.Access)),
			listDiff(configuration.Access, parsed,
				func(access domain.Access) uint64 { return access.ID },
				func(access domain.Access) string { return "id " + strconv.FormatUint(access.ID, 10) },
				func(access domain.Access) any { return accessRowOf(deps.Clock.Now(), access) }),
			func(ctx context.Context) (application.UpdateConfigurationResult, error) {
				return deps.Section.Handle(ctx, application.UpdateConfigurationSectionCommand{
					Section: section,
					Changes: application.ConfigurationChanges{Access: &parsed},
				})
			}), ""

	case application.ApikeysSection:
		parsed := domain.ParseApikeys(rows)

		return listProposal(in.Section, 0, what,
			anyRows(apikeyRowsOf(configuration.Apikeys)),
			listDiff(configuration.Apikeys, parsed,
				func(apikey domain.Apikey) uint64 { return apikey.ID },
				func(apikey domain.Apikey) string { return "id " + strconv.FormatUint(apikey.ID, 10) },
				func(apikey domain.Apikey) any { return apikeyRowOf(apikey) }),
			func(ctx context.Context) (application.UpdateConfigurationResult, error) {
				return deps.Section.Handle(ctx, application.UpdateConfigurationSectionCommand{
					Section: section,
					Changes: application.ConfigurationChanges{Apikeys: &parsed},
				})
			}), ""

	case application.DirwatchSection:
		parsed := domain.ParseDirwatches(rows)

		return listProposal(in.Section, 0, what,
			anyRows(dirwatchRowsOf(configuration.Dirwatches)),
			listDiff(configuration.Dirwatches, parsed,
				func(dirwatch domain.Dirwatch) uint64 { return dirwatch.ID },
				func(dirwatch domain.Dirwatch) string {
					return "id " + strconv.FormatUint(dirwatch.ID, 10) + " " + dirwatch.Directory
				},
				func(dirwatch domain.Dirwatch) any { return dirwatchRowOf(dirwatch) }),
			func(ctx context.Context) (application.UpdateConfigurationResult, error) {
				return deps.Section.Handle(ctx, application.UpdateConfigurationSectionCommand{
					Section: section,
					Changes: application.ConfigurationChanges{Dirwatches: &parsed},
				})
			}), ""

	case application.DownstreamsSection:
		parsed := domain.ParseDownstreams(rows)

		return listProposal(in.Section, 0, what,
			anyRows(downstreamRowsOf(configuration.Downstreams)),
			listDiff(configuration.Downstreams, parsed,
				func(downstream domain.Downstream) uint64 { return downstream.ID },
				func(downstream domain.Downstream) string {
					return "id " + strconv.FormatUint(downstream.ID, 10) + " " + downstream.URL
				},
				func(downstream domain.Downstream) any { return downstreamRowOf(downstream) }),
			func(ctx context.Context) (application.UpdateConfigurationResult, error) {
				return deps.Section.Handle(ctx, application.UpdateConfigurationSectionCommand{
					Section: section,
					Changes: application.ConfigurationChanges{Downstreams: &parsed},
				})
			}), ""

	case application.GroupsSection:
		parsed := domain.ParseGroups(rows)

		return listProposal(in.Section, 0, what,
			anyRows(groupsOf(configuration.Groups)),
			listDiff(configuration.Groups, parsed,
				func(group domain.Group) uint64 { return group.ID },
				func(group domain.Group) string {
					return "id " + strconv.FormatUint(group.ID, 10) + " " + group.Label
				},
				func(group domain.Group) any { return groupRowOf(group) }),
			func(ctx context.Context) (application.UpdateConfigurationResult, error) {
				return deps.Section.Handle(ctx, application.UpdateConfigurationSectionCommand{
					Section: section,
					Changes: application.ConfigurationChanges{Groups: &parsed},
				})
			}), ""

	case application.SystemsSection:
		parsed := domain.ParseSystems(rows)

		return listProposal(in.Section, 0, what,
			anyRows(systemsTree(configuration.Systems)),
			listDiff(configuration.Systems, parsed,
				func(system domain.System) uint64 { return uint64(system.SystemRef) },
				func(system domain.System) string { return "system " + strconv.FormatUint(uint64(system.SystemRef), 10) },
				func(system domain.System) any { return systemRowOf(system) }),
			func(ctx context.Context) (application.UpdateConfigurationResult, error) {
				return deps.Section.Handle(ctx, application.UpdateConfigurationSectionCommand{
					Section: section,
					Changes: application.ConfigurationChanges{Systems: &parsed},
				})
			}), ""

	case application.TagsSection:
		parsed := domain.ParseTags(rows)

		return listProposal(in.Section, 0, what,
			anyRows(tagsOf(configuration.Tags)),
			listDiff(configuration.Tags, parsed,
				func(tag domain.Tag) uint64 { return tag.ID },
				func(tag domain.Tag) string {
					return "id " + strconv.FormatUint(tag.ID, 10) + " " + tag.Label
				},
				func(tag domain.Tag) any { return tagRowOf(tag) }),
			func(ctx context.Context) (application.UpdateConfigurationResult, error) {
				return deps.Section.Handle(ctx, application.UpdateConfigurationSectionCommand{
					Section: section,
					Changes: application.ConfigurationChanges{Tags: &parsed},
				})
			}), ""
	}

	return proposedChange{}, fmt.Sprintf("this server has no configuration section named %q", in.Section)
}

func proposeLevel(deps Deps, configuration domain.Configuration, in manageConfigInput) (proposedChange, string) {
	if in.System == 0 {
		return proposedChange{}, fmt.Sprintf("the %s of a system belong to one system: name the "+
			"system by its reference, and submit the whole list of its %s", in.Section, in.Section)
	}

	system, found := configuration.Systems.ByRef(in.System)
	if !found {
		return proposedChange{}, fmt.Sprintf("this server holds no system %d", in.System)
	}

	rows, ok := in.Payload.([]any)
	if !ok {
		return proposedChange{}, fmt.Sprintf("the %s of a system are submitted as the whole list: an "+
			"array of the rows the system should hold after the change", in.Section)
	}

	level := application.SystemSubSection(in.Section)
	what := "the " + in.Section + " of system " + strconv.FormatUint(uint64(in.System), 10)

	switch level {
	case application.TalkgroupsSubSection:
		parsed := domain.ParseTalkgroups(rows)

		return listProposal(in.Section, in.System, what,
			anyRows(talkgroupsOf(system.Talkgroups)),
			listDiff(system.Talkgroups, parsed,
				func(talkgroup domain.Talkgroup) uint64 { return uint64(talkgroup.TalkgroupRef) },
				func(talkgroup domain.Talkgroup) string {
					return strconv.FormatUint(uint64(talkgroup.TalkgroupRef), 10)
				},
				func(talkgroup domain.Talkgroup) any { return talkgroupRowOf(talkgroup) }),
			func(ctx context.Context) (application.UpdateConfigurationResult, error) {
				return deps.SubSection.Handle(ctx, application.UpdateSystemSubSectionCommand{
					SystemID:   system.ID,
					Level:      level,
					Talkgroups: parsed,
				})
			}), ""

	case application.SitesSubSection:
		parsed := domain.ParseSites(rows)

		return listProposal(in.Section, in.System, what,
			anyRows(sitesOf(system.Sites)),
			listDiff(system.Sites, parsed,
				func(site domain.Site) uint64 { return uint64(site.SiteRef) },
				func(site domain.Site) string { return strconv.FormatUint(uint64(site.SiteRef), 10) },
				func(site domain.Site) any { return siteRowOf(site) }),
			func(ctx context.Context) (application.UpdateConfigurationResult, error) {
				return deps.SubSection.Handle(ctx, application.UpdateSystemSubSectionCommand{
					SystemID: system.ID,
					Level:    level,
					Sites:    parsed,
				})
			}), ""

	case application.UnitsSubSection:
		parsed := domain.ParseUnits(rows)

		return listProposal(in.Section, in.System, what,
			anyRows(unitsOf(system.Units)),
			listDiff(system.Units, parsed,
				func(unit domain.Unit) uint64 { return uint64(unit.UnitRef) },
				func(unit domain.Unit) string { return strconv.FormatUint(uint64(unit.UnitRef), 10) },
				func(unit domain.Unit) any { return unitRowOf(unit) }),
			func(ctx context.Context) (application.UpdateConfigurationResult, error) {
				return deps.SubSection.Handle(ctx, application.UpdateSystemSubSectionCommand{
					SystemID: system.ID,
					Level:    level,
					Units:    parsed,
				})
			}), ""
	}

	return proposedChange{}, fmt.Sprintf("a system has no level named %q", in.Section)
}

// proposeSettings works out what a settings submission does. The server's
// own settings save applies every setting a submission does not name to its
// default, so the diff shows the settings it named and the ones it reset.
func proposeSettings(deps Deps, configuration domain.Configuration, in manageConfigInput) (proposedChange, string) {
	submitted, ok := in.Payload.(map[string]any)
	if !ok {
		return proposedChange{}, "the settings are submitted as an object of the settings to write, " +
			"named the way the settings are named: " + strings.Join(writableSettings, ", ")
	}

	for _, name := range sortedNames(submitted) {
		if !isWritableSetting(name) {
			return proposedChange{}, fmt.Sprintf("this server's settings have no setting named %q: "+
				"the settings are %s", name, strings.Join(writableSettings, ", "))
		}
	}

	resulting := configuration.Options.ApplyMap(submitted)

	settings := []settingChange{}
	reset := []string{}

	for _, name := range writableSettings {
		before, after := settingValue(configuration.Options, name), settingValue(resulting, name)

		if reflect.DeepEqual(before, after) {
			continue
		}

		settings = append(settings, settingChange{Name: name, Before: before, After: after})

		if _, named := submitted[name]; !named {
			reset = append(reset, name)
		}
	}

	answer := configChangeAnswer{
		Section:  in.Section,
		Current:  optionsViewOf(deps, configuration.Options),
		Settings: settings,
		Reset:    reset,
	}

	return proposedChange{
		answer:  answer,
		message: settingsAskMessage(settings, reset, len(submitted)),
		apply: func(ctx context.Context) (application.UpdateConfigurationResult, error) {
			return deps.Section.Handle(ctx, application.UpdateConfigurationSectionCommand{
				Section: application.OptionsSection,
				Changes: application.ConfigurationChanges{Options: &resulting},
			})
		},
	}, ""
}

// writableSettings is the vocabulary of the settings a submission may name,
// in the order the server writes them. The retired legacy spelling is
// accepted by the server's own save and lands on audio conversion, so it is
// accepted here and reported as that setting.
var writableSettings = []string{
	"audioConversion",
	"autoPopulate",
	"branding",
	"disableDuplicateDetection",
	"email",
	"keypadBeeps",
	"maxClients",
	"pruneDays",
	"showListenersCount",
	"sortTalkgroups",
}

// isWritableSetting reports whether a submission names a setting this server has.
func isWritableSetting(name string) bool {
	if name == "disableAudioConversion" {
		return true
	}

	for _, setting := range writableSettings {
		if setting == name {
			return true
		}
	}

	return false
}

// settingValue is one setting of the options, by the name a submission names it by.
func settingValue(options domain.Options, name string) any {
	switch name {
	case "audioConversion":
		return options.AudioConversion
	case "autoPopulate":
		return options.AutoPopulate
	case "branding":
		return options.Branding
	case "disableDuplicateDetection":
		return options.DisableDuplicateDetection
	case "email":
		return options.Email
	case "keypadBeeps":
		return options.KeypadBeeps
	case "maxClients":
		return options.MaxClients
	case "pruneDays":
		return options.PruneDays
	case "showListenersCount":
		return options.ShowListenersCount
	case "sortTalkgroups":
		return options.SortTalkgroups
	}

	return nil
}

// listProposal is a list section's change, shown and ready to write.
func listProposal(section string, system uint, what string, current []any, changes listChanges,
	save func(context.Context) (application.UpdateConfigurationResult, error)) proposedChange {
	return proposedChange{
		answer: configChangeAnswer{
			Section: section,
			System:  system,
			Current: current,
			Added:   changes.Added,
			Changed: changes.Changed,
			Removed: changes.Removed,
		},
		message: listAskMessage("manage_config", what, changes),
		apply:   save,
	}
}

// listChanges is what a submitted list does to the stored one. The names
// are the identities an operator reads the entries by, and what the ask is
// made of.
type listChanges struct {
	Added      []any
	Changed    []changedEntry
	Removed    []any
	addedNames []string
	changed    []string
	removed    []string
}

// sameEntry compares two rows of a section the way the configuration's
// parsers do, counting an empty list as an absent one, so a diff reports
// a change the operator could act on; see the write-diffs-read-a-save-the-
// way-the-server-replaces-it maintainer note.
func sameEntry(entry, other any) bool {
	return reflect.DeepEqual(normalize(entry), normalize(other))
}

// normalize is a value with every empty list replaced by an absent one, at any depth.
func normalize(value any) any {
	return normalizeValue(reflect.ValueOf(value)).Interface()
}

func normalizeValue(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Slice:
		if value.Len() == 0 {
			return reflect.Zero(value.Type())
		}

		list := reflect.MakeSlice(value.Type(), value.Len(), value.Len())

		for i := range value.Len() {
			list.Index(i).Set(normalizeValue(value.Index(i)))
		}

		return list

	case reflect.Struct:
		normalized := reflect.New(value.Type()).Elem()
		normalized.Set(value)

		for i := range value.NumField() {
			if !normalized.Field(i).CanSet() {
				continue
			}

			normalized.Field(i).Set(normalizeValue(value.Field(i)))
		}

		return normalized

	case reflect.Pointer:
		if value.IsNil() {
			return value
		}

		inner := normalizeValue(value.Elem())
		pointer := reflect.New(value.Type().Elem())
		pointer.Elem().Set(inner)

		return pointer

	default:
		return value
	}
}

// listDiff compares a submitted list with the stored one as the whole effect
// of the save: rows added, changed, deleted, each matched by the reference
// the store knows, compared as the server's own values, not the masked rows.
// see the write-diffs-read-a-save-the-way-the-server-replaces-it maintainer note.
func listDiff[T any](current []T, submitted []T, identity func(T) uint64, name func(T) string, describe func(T) any) listChanges {
	changes := listChanges{
		Added:   []any{},
		Changed: []changedEntry{},
		Removed: []any{},
	}

	stored := map[uint64]int{}
	for i, entry := range current {
		if key := identity(entry); key != 0 {
			stored[key] = i
		}
	}

	// A row submitted with no identity is one the store has never held, and is
	// added. A row submitted twice under one identity is one change, the last
	// spelling of it.
	added := []int{}
	submittedBy := map[uint64]int{}

	for i, entry := range submitted {
		key := identity(entry)

		if key == 0 {
			added = append(added, i)

			continue
		}

		submittedBy[key] = i
	}

	for _, i := range added {
		changes.Added = append(changes.Added, describe(submitted[i]))
		changes.addedNames = append(changes.addedNames, name(submitted[i]))
	}

	for _, key := range sortedUintKeys(submittedBy) {
		i := submittedBy[key]

		at, held := stored[key]
		if !held {
			changes.Added = append(changes.Added, describe(submitted[i]))
			changes.addedNames = append(changes.addedNames, name(submitted[i]))

			continue
		}

		if sameEntry(current[at], submitted[i]) {
			continue
		}

		changes.Changed = append(changes.Changed, changedEntry{
			Before: describe(current[at]),
			After:  describe(submitted[i]),
		})
		changes.changed = append(changes.changed, name(submitted[i]))
	}

	for _, key := range sortedUintKeys(stored) {
		if _, kept := submittedBy[key]; kept {
			continue
		}

		changes.Removed = append(changes.Removed, describe(current[stored[key]]))
		changes.removed = append(changes.removed, name(current[stored[key]]))
	}

	return changes
}

// listAskMessage is the ask for a list change: what is being changed, every
// entry that will change by the name an operator reads it by, and what the
// removals mean.
func listAskMessage(tool, what string, changes listChanges) string {
	parts := []string{}

	if len(changes.Added) > 0 {
		parts = append(parts, fmt.Sprintf("%d added (%s)", len(changes.Added), strings.Join(changes.addedNames, ", ")))
	}
	if len(changes.Changed) > 0 {
		parts = append(parts, fmt.Sprintf("%d changed (%s)", len(changes.Changed), strings.Join(changes.changed, ", ")))
	}
	if len(changes.Removed) > 0 {
		parts = append(parts, fmt.Sprintf("%d removed (%s)", len(changes.Removed), strings.Join(changes.removed, ", ")))
	}

	warning := ""
	if len(changes.Removed) > 0 {
		warning = " The entries named as removed are deleted, and this server cannot restore them."
	}

	return fmt.Sprintf("%s — replace %s on this server: %s.%s Confirm to write the change, decline "+
		"to leave it alone.", tool, what, strings.Join(parts, ", "), warning)
}

// settingsAskMessage is the ask for a settings change, naming the settings that change and the
// settings the submission resets to their defaults.
func settingsAskMessage(settings []settingChange, reset []string, named int) string {
	changed := []string{}

	for _, setting := range settings {
		changed = append(changed, fmt.Sprintf("%s %v -> %v", setting.Name, setting.Before, setting.After))
	}

	parts := []string{}

	if len(settings) > 0 {
		parts = append(parts, fmt.Sprintf("%d setting changes (%s)", len(settings), strings.Join(changed, ", ")))
	}

	resetting := ""
	if len(reset) > 0 {
		resetting = fmt.Sprintf(" The submission names %d of the %d settings, and this server's "+
			"settings save resets every setting a submission does not name to its default, so this "+
			"change also resets %s.", named, len(writableSettings), strings.Join(reset, ", "))
	}

	return fmt.Sprintf("manage_config — replace this server's settings: %s.%s Confirm to write the "+
		"change, decline to leave the settings alone.", strings.Join(parts, ", "), resetting)
}

// manage_access_user

// manageAccessUserInput is a listener access change: which form, whose
// code, and for the add form what the code is entitled to.
type manageAccessUserInput struct {
	// Action is the form: add or remove.
	Action string `json:"action"`

	// Ident is the identity the code belongs to. Both forms need it.
	Ident string `json:"ident"`

	// Systems is what the code is entitled to see: "*" for everything this server holds, or a
	// list of systems named by their reference with their talkgroups.
	Systems any `json:"systems,omitempty"`

	// Expiration is the moment the code stops working, as an instant the configuration takes.
	Expiration string `json:"expiration,omitempty"`

	// Code is the code to write. Left unnamed, the server generates one and no answer carries it.
	Code string `json:"code,omitempty"`

	// Limit is how many listeners the code lets in at once.
	Limit uint `json:"limit,omitempty"`

	// Order is the display order the administrator's page shows it under.
	Order uint `json:"order,omitempty"`
}

// accessUserAnswer is an access change shown back in masked rows: an answer
// of this tool never carries an access code.
type accessUserAnswer struct {
	Action  string         `json:"action"`
	Ident   string         `json:"ident"`
	Outcome string         `json:"outcome"`
	Added   []any          `json:"added,omitempty"`
	Changed []changedEntry `json:"changed,omitempty"`
	Removed []any          `json:"removed,omitempty"`
	Note    string         `json:"note,omitempty"`
}

// The forms the access tool has.
const (
	accessAddForm    = "add"
	accessRemoveForm = "remove"
)

// generatedCodeLength is the length of a code this tool generates when the
// caller names none. The alphabet is the server's own; the length keeps a
// code unguessable and typeable.
const generatedCodeLength = 16

func manageAccessUserHandler(deps Deps) func(context.Context, *mcp.CallToolRequest, manageAccessUserInput) (*mcp.CallToolResult, accessUserAnswer, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in manageAccessUserInput) (*mcp.CallToolResult, accessUserAnswer, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, accessUserAnswer{}, errNoCaller
		}
		if !isAdministrator(who) {
			return refused(administratorRequired), accessUserAnswer{}, nil
		}

		configuration, err := deps.Config.Handle(ctx, application.GetConfigQuery{})
		if err != nil {
			return nil, accessUserAnswer{}, err
		}

		switch in.Action {
		case accessAddForm:
			return addAccessUser(ctx, req, deps, configuration, in)

		case accessRemoveForm:
			return removeAccessUser(ctx, req, deps, configuration, in)
		}

		return refused(fmt.Sprintf("this capability has no %q form: the forms are %s and %s",
			in.Action, accessAddForm, accessRemoveForm)), accessUserAnswer{}, nil
	}
}

func addAccessUser(ctx context.Context, req *mcp.CallToolRequest, deps Deps, configuration domain.Configuration,
	in manageAccessUserInput) (*mcp.CallToolResult, accessUserAnswer, error) {
	answer := accessUserAnswer{Action: accessAddForm, Ident: in.Ident}

	if strings.TrimSpace(in.Ident) == "" {
		return refused("an access code is filed under an identity: name the identity the code belongs to"),
			accessUserAnswer{}, nil
	}

	if in.Systems == nil {
		return refused("an access code is entitled to the systems and talkgroups the caller names: " +
			"use \"*\" for everything this server holds, or a list of systems named by systemRef " +
			"with their talkgroups"), accessUserAnswer{}, nil
	}

	scope, refusal := scopePayload(in.Systems)
	if refusal != "" {
		return refused(refusal), accessUserAnswer{}, nil
	}

	submitted := map[string]any{
		"ident":   in.Ident,
		"systems": scope,
	}

	if in.Code != "" {
		submitted["code"] = in.Code
	} else {
		submitted["code"] = domain.GenerateSecret(generatedCodeLength)
	}

	if in.Expiration != "" {
		if _, err := time.Parse(time.RFC3339, in.Expiration); err != nil {
			return refused("the expiry is an instant the configuration takes, as " +
				"2026-12-31T00:00:00Z; the value submitted does not read as one"), accessUserAnswer{}, nil
		}

		submitted["expiration"] = in.Expiration
	}

	if in.Limit > 0 {
		submitted["limit"] = float64(in.Limit)
	}
	if in.Order > 0 {
		submitted["order"] = float64(in.Order)
	}

	access := domain.NewAccess().ApplyMap(submitted)

	// The server's own access save updates the code that is already on the list, so the change is
	// shown as what it is: a new code, or the entry a standing code becomes.
	replaced, held := configuration.Access.ByCode(access.Code)

	changes := listChanges{Added: []any{}, Changed: []changedEntry{}, Removed: []any{}}

	if held {
		changes.Changed = append(changes.Changed, changedEntry{
			Before: accessRowOf(deps.Clock.Now(), replaced),
			After:  accessRowOf(deps.Clock.Now(), access),
		})
		changes.changed = append(changes.changed, "id "+strconv.FormatUint(replaced.ID, 10))
	} else {
		changes.Added = append(changes.Added, accessRowOf(deps.Clock.Now(), access))
		changes.addedNames = append(changes.addedNames, in.Ident)
	}

	message := listAskMessage("manage_access_user",
		"the listener access code of "+in.Ident, changes) +
		" The access code itself is not shown here and is never answered back."

	asked, refusal, err := askToConfirm(req, "manage_access_user", message,
		fmt.Sprintf("access %s: %d added, %d changed", in.Ident, len(changes.Added), len(changes.Changed)))
	if err != nil {
		return nil, accessUserAnswer{}, err
	}
	if refusal != "" {
		return refused(refusal), accessUserAnswer{}, nil
	}
	if asked != nil {
		return asked, accessUserAnswer{}, nil
	}

	result, err := deps.AddUser.Handle(ctx, application.AddAccessUserCommand{User: access})
	if err != nil {
		return refused("the change was confirmed, and this server could not store it: " +
			string(result.Outcome)), accessUserAnswer{}, nil
	}

	answer.Outcome = "saved"
	answer.Added = changes.Added
	answer.Changed = changes.Changed
	answer.Note = "the code is on this server's access list; its code is not in this answer, and " +
		"the operator reads it from the administrator's access page"

	return nil, answer, nil
}

func removeAccessUser(ctx context.Context, req *mcp.CallToolRequest, deps Deps, configuration domain.Configuration,
	in manageAccessUserInput) (*mcp.CallToolResult, accessUserAnswer, error) {
	answer := accessUserAnswer{Action: accessRemoveForm, Ident: in.Ident}

	if strings.TrimSpace(in.Ident) == "" {
		return refused("a listener is removed by the identity its codes are filed under: name the " +
			"identity whose codes should go"), accessUserAnswer{}, nil
	}

	// Removal deletes every code sharing the identity, so the operator
	// confirming it is told the size of what is being deleted. An identity
	// carrying nothing is a question about a listener this server does not have,
	// answered without an ask.
	going := []domain.Access{}

	for _, access := range configuration.Access {
		if access.Ident == in.Ident {
			going = append(going, access)
		}
	}

	if len(going) == 0 {
		answer.Outcome = "nothing to remove"
		answer.Note = "no access code on this server carries that identity, so there was nothing to " +
			"confirm and nothing was written"

		return nil, answer, nil
	}

	names := []string{}
	rows := []any{}

	for _, access := range going {
		names = append(names, "id "+strconv.FormatUint(access.ID, 10))
		rows = append(rows, accessRowOf(deps.Clock.Now(), access))
	}

	// The count is read in the words the count needs: an operator deciding on one code is not
	// shown a sentence about several.
	noun := "listener access codes"

	if len(going) == 1 {
		noun = "listener access code"
	}

	message := fmt.Sprintf("manage_access_user — remove %d %s carrying identity %s "+
		"from this server (%s). The codes are deleted, the listeners using them are let out, and this "+
		"server cannot restore them. The codes themselves are not shown here. Confirm to remove them, "+
		"decline to leave the access list alone.", len(going), noun, in.Ident, strings.Join(names, ", "))

	asked, refusal, err := askToConfirm(req, "manage_access_user", message,
		fmt.Sprintf("access %s: %d removed", in.Ident, len(going)))
	if err != nil {
		return nil, accessUserAnswer{}, err
	}
	if refusal != "" {
		return refused(refusal), accessUserAnswer{}, nil
	}
	if asked != nil {
		return asked, accessUserAnswer{}, nil
	}

	result, err := deps.RemoveUser.Handle(ctx, application.RemoveAccessUserCommand{Ident: in.Ident})
	if err != nil {
		return refused("the change was confirmed, and this server could not store it: " +
			string(result.Outcome)), accessUserAnswer{}, nil
	}

	answer.Outcome = "saved"
	answer.Removed = rows
	answer.Note = "the codes are gone from this server's access list, and the open administrator " +
		"connections were told the configuration that resulted"

	return nil, answer, nil
}

// scopePayload translates the scope this surface spells, a system named by
// its reference, into the shape this server's configuration reads a scope
// from.
func scopePayload(value any) (any, string) {
	switch scope := value.(type) {
	case string:
		if scope == "*" {
			return "*", ""
		}

		return nil, `the scope is "*" for everything this server holds, or a list of systems`

	case []any:
		answer := []any{}

		for _, item := range scope {
			entry, ok := item.(map[string]any)
			if !ok {
				return nil, "each system of the scope is an object naming its reference and its talkgroups"
			}

			translated := map[string]any{}

			if ref, ok := entry["systemRef"].(float64); ok {
				translated["id"] = ref
			} else if ref, ok := entry["id"].(float64); ok {
				translated["id"] = ref
			} else {
				return nil, "each system of the scope names its reference as systemRef"
			}

			if talkgroups, ok := entry["talkgroups"]; ok {
				translated["talkgroups"] = talkgroups
			}

			answer = append(answer, translated)
		}

		return answer, ""
	}

	return nil, `the scope is "*" for everything this server holds, or a list of systems`
}

// rotate_admin_password

// rotateAdminPasswordInput is the credential change. The current credential
// is optional, and naming it or not is the distinction the server's own
// credential change makes.
type rotateAdminPasswordInput struct {
	NewPassword     string  `json:"newPassword"`
	CurrentPassword *string `json:"currentPassword,omitempty"`
}

// passwordAnswer is the credential change shown back. The credential itself is never in it.
type passwordAnswer struct {
	Outcome             string `json:"outcome"`
	PasswordNeedsChange bool   `json:"passwordNeedsChange"`
	Note                string `json:"note,omitempty"`
}

func rotateAdminPasswordHandler(deps Deps) func(context.Context, *mcp.CallToolRequest, rotateAdminPasswordInput) (*mcp.CallToolResult, passwordAnswer, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in rotateAdminPasswordInput) (*mcp.CallToolResult, passwordAnswer, error) {
		who, ok := CallerOf(ctx)
		if !ok {
			return nil, passwordAnswer{}, errNoCaller
		}
		if !isAdministrator(who) {
			return refused(administratorRequired), passwordAnswer{}, nil
		}

		if in.NewPassword == "" {
			return refused("the new credential has to be named: this server does not change the " +
				"administrator credential to an empty one"), passwordAnswer{}, nil
		}

		message := "rotate_admin_password — replace the credential an administrator signs in with on " +
			"this server. Every open administrator session stops working the moment it is replaced, " +
			"and the operator has to sign in again. The credential itself is not shown here. Confirm " +
			"to replace it, decline to leave the credential alone."

		asked, refusal, err := askToConfirm(req, "rotate_admin_password", message, "admin credential")
		if err != nil {
			return nil, passwordAnswer{}, err
		}
		if refusal != "" {
			return refused(refusal), passwordAnswer{}, nil
		}
		if asked != nil {
			return asked, passwordAnswer{}, nil
		}

		result, err := deps.Password.Handle(ctx, application.ChangeAdminPasswordCommand{
			CurrentCredential:      currentCredentialOf(in.CurrentPassword),
			CurrentCredentialGiven: in.CurrentPassword != nil,
			NewCredential:          in.NewPassword,
		})
		if err != nil || result.Outcome != application.AdminPasswordChanged {
			return refused("the server refused the credential change, and nothing was written"),
				passwordAnswer{}, nil
		}

		return nil, passwordAnswer{
			Outcome:             string(result.Outcome),
			PasswordNeedsChange: result.PasswordNeedChange,
			Note: "the credential is replaced, the open administrator sessions are gone, and the " +
				"operator signs in again with the new credential",
		}, nil
	}
}

// currentCredentialOf is the credential the caller offered, or nothing when it named none.
func currentCredentialOf(credential *string) string {
	if credential == nil {
		return ""
	}

	return *credential
}

// rowsOf is a stored section as the answer shows it. The row builders are
// shared with the configuration resources, so a section read and a section
// shown before it is changed are one shape, with credentials masked in both.
func accessRowsOf(now time.Time, accesses domain.Accesses) []accessRow {
	rows := []accessRow{}

	for _, access := range accesses {
		rows = append(rows, accessRowOf(now, access))
	}

	return rows
}

func apikeyRowsOf(apikeys domain.Apikeys) []apikeyRow {
	rows := []apikeyRow{}

	for _, apikey := range apikeys {
		rows = append(rows, apikeyRowOf(apikey))
	}

	return rows
}

func dirwatchRowsOf(dirwatches domain.Dirwatches) []dirwatchRow {
	rows := []dirwatchRow{}

	for _, dirwatch := range dirwatches {
		rows = append(rows, dirwatchRowOf(dirwatch))
	}

	return rows
}

func downstreamRowsOf(downstreams domain.Downstreams) []downstreamRow {
	rows := []downstreamRow{}

	for _, downstream := range downstreams {
		rows = append(rows, downstreamRowOf(downstream))
	}

	return rows
}

// systemRowOf is one system as a row of the answer, with the parts under it.
func systemRowOf(system domain.System) systemRow {
	return systemRow{
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
	}
}

// anyRows is a list of rows as an answer field carries it: each section has
// its own row shape, and added, changed, and removed entries show in the
// shape of the section they belong to.
func anyRows[T any](rows []T) []any {
	answer := []any{}

	for _, row := range rows {
		answer = append(answer, row)
	}

	return answer
}

// sortedNames is the keys of a name-keyed table in order. Map order would
// read differently every time, and an operator confirming a change should
// read the same words the assistant reads.
func sortedNames[V any](table map[string]V) []string {
	keys := []string{}

	for key := range table {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// sortedUintKeys is the keys of an identity-keyed table in order, for the same reason.
func sortedUintKeys[V any](table map[uint64]V) []uint64 {
	keys := []uint64{}

	for key := range table {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	return keys
}
