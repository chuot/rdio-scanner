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
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// session is the surface reached the way an assistant reaches it: one client,
// one server, the protocol's own round trips, the HTTP layer taken out, so a
// capability test measures the capability and not a socket.
type session struct {
	*mcp.ClientSession

	bundle *serverBundle
}

func newSession(t *testing.T, restricted bool) *session {
	t.Helper()

	return newCallerSession(t, Caller{Tier: TierFull})
}

// newCallerSession is the surface reached by a gate-decided caller over the
// in-memory transport: the caller's tier picks the capability set the way the
// HTTP handler does, and the caller sits in the context a capability handler
// is handed.
func newCallerSession(t *testing.T, who Caller) *session {
	t.Helper()

	bundle := newServer(t, true)

	target := bundle.server.readonly
	if who.Tier == TierFull {
		target = bundle.server.full
	}

	return newSessionOn(t, bundle, target, who)
}

// newAdministratorSetSession is the administrator's capability set reached by a
// caller of any tier: the shape when a write capability drifts onto the
// administrator's set in front of a weaker caller, the lock the handlers' own
// credential check is on.
func newAdministratorSetSession(t *testing.T, who Caller) *session {
	t.Helper()

	bundle := newServer(t, true)

	return newSessionOn(t, bundle, bundle.server.full, who)
}

func newSessionOn(t *testing.T, bundle *serverBundle, target *mcp.Server, who Caller,
	clientOpts ...*mcp.ClientOptions) *session {
	t.Helper()

	options := &mcp.ClientOptions{}
	if len(clientOpts) > 0 && clientOpts[0] != nil {
		options = clientOpts[0]
	}

	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := target.Connect(
		context.WithValue(context.Background(), callerKey{}, who), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the surface to an in-memory session: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "rdio-scanner-test-harness", Version: "1"}, options)

	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect the harness to the surface: %v", err)
	}

	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})

	return &session{ClientSession: clientSession, bundle: bundle}
}

func TestAnInMemorySessionInitializesAndReportsTheServer(t *testing.T) {
	s := newSession(t, true)

	opened := s.InitializeResult()
	if opened == nil {
		t.Fatal("the session reports no initialize result")
	}
	if opened.ProtocolVersion == "" {
		t.Fatal("the session reports no protocol version")
	}
	if opened.ServerInfo.Name != serverName {
		t.Fatalf("the surface announces itself as %q, want %q", opened.ServerInfo.Name, serverName)
	}
	if opened.ServerInfo.Version != testVersion {
		t.Fatalf("the surface announces version %q, want %q", opened.ServerInfo.Version, testVersion)
	}
}

func TestAnInMemorySessionReadsTheVersionResource(t *testing.T) {
	s := newSession(t, true)

	listed, err := s.ListResources(context.Background(), &mcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("list the resources: %v", err)
	}

	found := false
	for _, resource := range listed.Resources {
		if resource.URI == versionURI {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the version resource is not in the catalog: %+v", listed.Resources)
	}

	read, err := s.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: versionURI})
	if err != nil {
		t.Fatalf("read the version resource: %v", err)
	}
	if len(read.Contents) != 1 {
		t.Fatalf("the version resource carried %d contents, want one", len(read.Contents))
	}

	var reported struct {
		Version string `json:"version"`
		Docker  bool   `json:"docker"`
	}
	if err := json.Unmarshal([]byte(read.Contents[0].Text), &reported); err != nil {
		t.Fatalf("the version resource is not the JSON it declares: %v", err)
	}
	if reported.Version != testVersion {
		t.Fatalf("the version resource reports %q, want %q", reported.Version, testVersion)
	}
}

// The read-only tier's first capability: an assistant asks for recorded calls and gets the page.
func TestATierACallToolAnswersThroughTheSession(t *testing.T) {
	s := newSession(t, true)

	answered, err := s.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_calls",
		Arguments: map[string]any{"limit": 2},
	})
	if err != nil {
		t.Fatalf("call search_calls: %v", err)
	}
	if answered.IsError {
		t.Fatalf("search_calls answered a failure: %+v", answered.Content)
	}
	if answered.StructuredContent == nil {
		t.Fatal("search_calls carried no structured answer")
	}
}
