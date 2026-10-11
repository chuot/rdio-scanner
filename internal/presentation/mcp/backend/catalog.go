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

import "fmt"

// The inventory of this surface: every capability it registers, with who may reach it, what it does
// to the deployment, and the entry point its answer comes from.
//
// The inventory is written down on purpose. An assistant sees the catalog the protocol hands it, and
// an operator needs a reading of what enabling this surface actually opened: which capabilities a
// listener's access code reaches, which ones only the administrator's credential reaches, and which
// ones change something. A capability that is not in this list is a capability nobody reviewed.

// Kind is what shape a capability takes.
type Kind string

const (
	// KindTool is a capability an assistant calls with arguments and gets an answer from.
	KindTool Kind = "tool"

	// KindResource is a capability an assistant reads by URI, with no arguments at all.
	KindResource Kind = "resource"

	// KindTemplate is a resource an assistant reads by a URI it fills in, for a dataset too large
	// and too paged to be one resource.
	KindTemplate Kind = "resource-template"

	// KindPrompt is a standing question an operator starts a session with.
	KindPrompt Kind = "prompt"
)

// Effect is what a capability does to the deployment when it is used.
type Effect string

const (
	// EffectRead changes nothing.
	EffectRead Effect = "read"

	// EffectWrite changes something, and is reversible by the same surface.
	EffectWrite Effect = "write"

	// EffectDestructive changes something by removing it. The assistant is expected to confirm with
	// the operator before reaching for one, and the surface says so in its annotations.
	EffectDestructive Effect = "destructive"
)

// Reach is who the gate lets in front of a capability.
type Reach int

const (
	// ReachReadonly is in front of both tiers: an access code, an administrator's session, and an
	// anonymous caller on a deployment that lists no access codes.
	ReachReadonly Reach = iota

	// ReachAdministrator stands behind the administrator's credential and nothing else.
	ReachAdministrator
)

// Capability is one entry of the inventory.
type Capability struct {
	// Name is the name the protocol carries: the tool name, the resource URI, the prompt name.
	Name string

	// Kind is the shape it takes.
	Kind Kind

	// Reach is who may use it.
	Reach Reach

	// Effect is what using it does.
	Effect Effect

	// Endpoints are the documented entry points its answer is built from, named the way the OpenAPI
	// document names them. A capability built on a socket is not in this inventory: the listener and
	// administrator sockets are reserved, and this surface reaches the deployment over its documented
	// entry points.
	Endpoints []string
}

// String is the inventory line an operator reads.
func (c Capability) String() string {
	return fmt.Sprintf("%s %s (%s, %s) from %v", c.Kind, c.Name, c.Reach, c.Effect, c.Endpoints)
}

// Reach is the reach of a capability in words, for the inventory line.
func (r Reach) String() string {
	if r == ReachAdministrator {
		return "administrator only"
	}

	return "read-only tier and administrator"
}

// capabilityCatalog is the inventory. Every capability the surface registers is listed here, and the
// tests keep the two in step in both directions: an unregistered entry is a promise nobody implemented,
// an unlisted capability is one nobody reviewed.
var capabilityCatalog = []Capability{
	{
		Name:      "search_calls",
		Kind:      KindTool,
		Reach:     ReachReadonly,
		Effect:    EffectRead,
		Endpoints: []string{"POST /api/calls/search"},
	},
	{
		Name:      "get_call",
		Kind:      KindTool,
		Reach:     ReachReadonly,
		Effect:    EffectRead,
		Endpoints: []string{"GET /api/calls/{callId}", "GET /api/calls/{callId}/audio"},
	},
	{
		Name:      "list_systems",
		Kind:      KindTool,
		Reach:     ReachReadonly,
		Effect:    EffectRead,
		Endpoints: []string{"GET /api/admin/config/{section}"},
	},
	{
		Name:      versionURI,
		Kind:      KindResource,
		Reach:     ReachReadonly,
		Effect:    EffectRead,
		Endpoints: []string{"GET /api/admin/config/{section}"},
	},
	{
		Name:      systemsURI,
		Kind:      KindResource,
		Reach:     ReachReadonly,
		Effect:    EffectRead,
		Endpoints: []string{"GET /api/admin/config/{section}"},
	},
	{
		Name:      callURITemplate,
		Kind:      KindTemplate,
		Reach:     ReachReadonly,
		Effect:    EffectRead,
		Endpoints: []string{"POST /api/calls/search"},
	},
	{
		Name:      "inspect_system",
		Kind:      KindTool,
		Reach:     ReachAdministrator,
		Effect:    EffectRead,
		Endpoints: []string{"GET /api/admin/config", "GET /api/admin/alerts"},
	},
	{
		Name:      "search_logs",
		Kind:      KindTool,
		Reach:     ReachAdministrator,
		Effect:    EffectRead,
		Endpoints: []string{"POST /api/admin/logs"},
	},
	{
		Name:      "explain_error",
		Kind:      KindTool,
		Reach:     ReachAdministrator,
		Effect:    EffectRead,
		Endpoints: []string{"POST /api/admin/logs"},
	},
	{
		Name:      "analyze_health",
		Kind:      KindTool,
		Reach:     ReachAdministrator,
		Effect:    EffectRead,
		Endpoints: []string{"POST /api/admin/logs", "GET /api/admin/config"},
	},
	{
		Name:   "diagnose_feed_problem",
		Kind:   KindTool,
		Reach:  ReachReadonly,
		Effect: EffectRead,
		Endpoints: []string{
			"POST /api/calls/search",
			"POST /api/admin/logs",
			"GET /api/admin/config/{section}",
		},
	},
}

// catalogOf is one capability of the inventory by its protocol name.
func catalogOf(name string) (Capability, bool) {
	for _, capability := range capabilityCatalog {
		if capability.Name == name {
			return capability, true
		}
	}

	return Capability{}, false
}
