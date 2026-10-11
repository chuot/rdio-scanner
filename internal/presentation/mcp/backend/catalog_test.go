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
	"os/exec"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The inventory is checked against what the surface actually registers, in both directions, and
// against the rule that this surface reaches the deployment over its documented entry points and
// never over a socket.

// registered is every capability the surface registered, named the way the protocol names it, with
// the shape the client reads it as.
type registered struct {
	tools     []string
	resources []string
	templates []string
}

func listRegistered(t *testing.T, s *session) registered {
	t.Helper()

	ctx := context.Background()

	tools, err := s.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list the tools: %v", err)
	}

	resources, err := s.ListResources(ctx, &mcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("list the resources: %v", err)
	}

	templates, err := s.ListResourceTemplates(ctx, &mcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("list the resource templates: %v", err)
	}

	answer := registered{}

	for _, tool := range tools.Tools {
		answer.tools = append(answer.tools, tool.Name)
	}
	for _, resource := range resources.Resources {
		answer.resources = append(answer.resources, resource.URI)
	}
	for _, template := range templates.ResourceTemplates {
		answer.templates = append(answer.templates, template.URITemplate)
	}

	return answer
}

func TestEveryRegisteredCapabilityIsInventoried(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	got := listRegistered(t, s)

	want := registered{tools: []string{}, resources: []string{}, templates: []string{}}
	for _, capability := range capabilityCatalog {
		switch capability.Kind {
		case KindTool:
			want.tools = append(want.tools, capability.Name)
		case KindResource:
			want.resources = append(want.resources, capability.Name)
		case KindTemplate:
			want.templates = append(want.templates, capability.Name)
		case KindPrompt:
			// Prompts are listed by their own request, and are covered by the prompt tests.
		default:
			t.Fatalf("the inventory names %s with kind %q, which is not a shape", capability.Name, capability.Kind)
		}
	}

	assertSame(t, "tools", want.tools, got.tools)
	assertSame(t, "resources", want.resources, got.resources)
	assertSame(t, "resource templates", want.templates, got.templates)
}

func TestEveryInventoriedCapabilityDeclaresWhoAndWhatItDoes(t *testing.T) {
	for _, capability := range capabilityCatalog {
		if capability.Name == "" {
			t.Fatal("the inventory carries a capability with no name")
		}

		if capability.Effect != EffectRead && capability.Effect != EffectWrite && capability.Effect != EffectDestructive {
			t.Fatalf("%s declares effect %q, which is not a side-effect class", capability.Name, capability.Effect)
		}

		if len(capability.Endpoints) == 0 {
			t.Fatalf("%s names no entry point its answer comes from", capability.Name)
		}

		for _, endpoint := range capability.Endpoints {
			// Every entry point is named the way the OpenAPI document names it: a method and a path.
			parts := strings.SplitN(endpoint, " ", 2)
			if len(parts) != 2 || parts[1] == "" {
				t.Fatalf("%s names %q, which is not a method and a path", capability.Name, endpoint)
			}

			// A socket is not an entry point this surface reaches. The listener and administrator
			// sockets are reserved, and a capability built on one would reach the deployment past
			// the documented boundary.
			for _, reserved := range []string{"ws://", "wss://", "websocket", "socket"} {
				if strings.Contains(strings.ToLower(endpoint), reserved) {
					t.Fatalf("%s names %q, which is a socket and not a documented entry point",
						capability.Name, endpoint)
				}
			}
		}
	}
}

// TestNoCapabilityPathReachesTheListenerSocket is the structural proof: the surface's own import
// closure — the code a capability can actually call — names no listener or administrator
// presentation package. Nothing this surface can call is a socket handler, and nothing it can call
// is another presentation surface at all. The dependency rule is the guarantee; this test keeps it
// from being quietly broken by the next capability added.
func TestNoCapabilityPathReachesTheListenerSocket(t *testing.T) {
	for _, line := range importClosure(t, ".") {
		if !strings.HasPrefix(line, "rdio-scanner/internal/presentation/") {
			continue
		}

		// The listener and administrator surfaces are the sockets. A capability reaching them
		// would reach the deployment past its documented boundary.
		for _, reserved := range []string{
			"rdio-scanner/internal/presentation/scanner",
			"rdio-scanner/internal/presentation/admin",
		} {
			if strings.HasPrefix(line, reserved) {
				t.Fatalf("the surface reaches %s, so a capability can resolve through a socket surface", line)
			}
		}

		// And it reaches no surface sideways: this surface is built from the use cases, over the
		// stores, and the only presentation code it uses is its own and the shared address reading.
		if line != "rdio-scanner/internal/presentation/mcp/backend" &&
			!strings.HasPrefix(line, "rdio-scanner/internal/presentation/common/") {
			t.Fatalf("the surface depends on %s", line)
		}
	}
}

// TestTheTestClosureReachesNoSocketSurface is the same rule over the tests: the suite that proves
// the two stateless surfaces agree mounts the call history endpoints beside this one, which is a
// documented dependency of the suite and not of the surface. The sockets stay out of both.
func TestTheTestClosureReachesNoSocketSurface(t *testing.T) {
	for _, line := range importClosure(t, "-test", ".") {
		for _, reserved := range []string{
			"rdio-scanner/internal/presentation/scanner",
			"rdio-scanner/internal/presentation/admin",
		} {
			if strings.HasPrefix(line, reserved) {
				t.Fatalf("the surface reaches %s, so a capability can resolve through a socket surface", line)
			}
		}
	}
}

// importClosure is the surface's import closure, tests included when asked for.
func importClosure(t *testing.T, flags ...string) []string {
	t.Helper()

	args := append([]string{"list", "-deps"}, flags...)

	out, err := exec.Command("go", args...).Output()
	if err != nil {
		t.Fatalf("list the surface's import closure: %v", err)
	}

	var closure []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "rdio-scanner/") {
			closure = append(closure, line)
		}
	}

	return closure
}

// assertSame compares two capability lists, naming the ones each side is missing.
func assertSame(t *testing.T, what string, want []string, got []string) {
	t.Helper()

	wanted := map[string]bool{}
	for _, name := range want {
		wanted[name] = true
	}

	gotten := map[string]bool{}
	for _, name := range got {
		gotten[name] = true
	}

	for _, name := range want {
		if !gotten[name] {
			t.Fatalf("the inventory lists %s %s, the surface does not register it: %v", what, name, got)
		}
	}

	for _, name := range got {
		if !wanted[name] {
			t.Fatalf("the surface registers %s %s, the inventory does not name it: %v", what, name, want)
		}
	}
}
