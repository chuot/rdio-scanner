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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/domain"
)

// The surface's log lines are filed under the surface's own subsystem, never
// the administrator's: the operator reading the log view on Monday has to
// tell an assistant's work apart. And no credential is ever written to the
// log an operator reads, quotes, or screenshots.

// TestTheSurfaceFilesItsActivityUnderItsOwnSubsystem: a refused call is filed
// under the surface's subsystem, where the operator can filter for it.
func TestTheSurfaceFilesItsActivityUnderItsOwnSubsystem(t *testing.T) {
	bundle := newSurfaceWithDeps(t, func(b *serverBundle, deps Deps) Deps {
		deps.Logger = slog.New(NewLogViewHandler(b.logs, b.clock))

		return deps
	})

	s := newSessionOn(t, bundle, bundle.server.full, readScoped())

	refusedCall(t, s, "manage_config", map[string]any{
		"section": "options",
		"payload": map[string]any{"maxClients": float64(50)},
	})

	entries := filedUnder(bundle.logs.Stored, domain.LogSourceMcp)

	if len(entries) == 0 {
		t.Fatal("the surface filed nothing under its own subsystem: an assistant refused by a capability " +
			"is an event in the deployment, and the operator reads the deployment in the log view")
	}

	if !anyEntryNames(entries, "manage_config") {
		t.Fatalf("the surface filed %v, and none of it names the capability the caller asked for: a line "+
			"saying a caller was refused, without the capability, tells the operator nothing", messagesOf(entries))
	}

	if !anyEntryNames(entries, listenerIdent) {
		t.Fatalf("the surface filed %v, and none of it names the caller by the identity the code is filed "+
			"under: the operator's question is who, and the identity is the answer that is not a credential",
			messagesOf(entries))
	}
}

// TestTheSurfaceFilesACallerItWouldNotLetInByItsAddress: the gate's refusal is
// filed by address, as the administrator's login attempts are, and a refused
// caller is filed with no credential.
func TestTheSurfaceFilesACallerItWouldNotLetInByItsAddress(t *testing.T) {
	bundle := newSurfaceWithDeps(t, func(b *serverBundle, deps Deps) Deps {
		deps.Logger = slog.New(NewLogViewHandler(b.logs, b.clock))

		return deps
	})

	rec := httptest.NewRecorder()

	bundle.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, EndpointPath, strings.NewReader("{}")))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a caller with no credential was answered %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	entries := filedUnder(bundle.logs.Stored, domain.LogSourceMcp)

	if len(entries) == 0 {
		t.Fatal("the surface refused a caller and filed nothing: a refusal at the credential gate is the " +
			"one event an operator wants to see in the log view")
	}

	if !anyEntryNames(entries, "192.0.2.1") {
		t.Fatalf("the surface filed %v, and none of it names the address the request came from: the "+
			"operator's question is who kept trying, and the address is the answer", messagesOf(entries))
	}
}

// TestNoEntryTheSurfaceFilesCarriesACredential: the surface runs on credentials
// at every layer, the session token, access code, upload keys, credential hash,
// and the credential an operator replaces: none belong in the log view.
func TestNoEntryTheSurfaceFilesCarriesACredential(t *testing.T) {
	bundle := newSurfaceWithDeps(t, func(b *serverBundle, deps Deps) Deps {
		deps.Logger = slog.New(NewLogViewHandler(b.logs, b.clock))

		return deps
	})

	// The administrator's read and write, an operator refusing a write, a
	// listener's read, a listener refused a write, and a caller the gate shuts out.
	administrator := newSessionOn(t, bundle, bundle.server.full, Caller{Tier: TierFull})

	call(t, administrator, "list_systems", map[string]any{})
	call(t, administrator, "analyze_health", map[string]any{})

	confirmed, operator := confirmingSessionOn(t, bundle, "accept")

	call(t, confirmed, "manage_config", map[string]any{
		"section": "options",
		"payload": map[string]any{"maxClients": float64(50)},
	})

	declined, _ := confirmingSessionOn(t, bundle, "decline")

	refusedCall(t, declined, "manage_access_user", map[string]any{
		"action": "add", "ident": "volunteer", "systems": "*",
	})

	listener := newSessionOn(t, bundle, bundle.server.readonly, readScoped())

	call(t, listener, "list_systems", map[string]any{})

	// A listener asking for a write is refused before the write is named: the
	// write capabilities are not on the read-only set at all, whatever shape the
	// protocol answers the question in.
	refusedOrRejected(t, listener, "manage_config", map[string]any{"section": "options", "payload": map[string]any{"maxClients": float64(50)}})

	rec := httptest.NewRecorder()

	bundle.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, EndpointPath, strings.NewReader("{}")))

	if len(operator.asks) != 1 {
		t.Fatalf("the operator was asked %d times, want the one confirmation the write names: %s",
			len(operator.asks), operator.asked())
	}

	credentials := []string{
		listenerCode,
		adminToken,
		fixtureApikey,
		"second-key",
		"a-long-new-administrator-credential",
	}

	for _, entry := range bundle.logs.Stored {
		for _, credential := range credentials {
			if strings.Contains(entry.Message, credential) {
				t.Fatalf("the surface filed %q under %s, and the entry carries the credential %q: a log "+
					"view is read out loud, pasted into a ticket, and handed to a client",
					entry.Message, entry.Source, credential)
			}
		}
	}
}

// TestTheSurfacesLinesLandInTheLogViewTheOperatorReads: the surface's lines are
// filed as log entries, the server clock's instant, the severity the surface
// asked for, and the surface's subsystem; the attributes travel into the
// message, because the log view carries one text.
func TestTheSurfacesLinesLandInTheLogViewTheOperatorReads(t *testing.T) {
	logs := bundleWithLogs(t)

	logger := slog.New(NewLogViewHandler(logs.logs, logs.clock))

	logger.Warn("the assistant's session was refused", "capability", "manage_config", "caller", listenerIdent)

	filed := filedUnder(logs.logs.Stored, domain.LogSourceMcp)

	if len(filed) != 1 {
		t.Fatalf("the surface filed %d entries under its own subsystem, want the one line it logged: %v",
			len(filed), messagesOf(filed))
	}

	entry := filed[0]

	if entry.Source != domain.LogSourceMcp {
		t.Errorf("the line was filed under %q, want %q: the operator filters for the assistant's work by "+
			"the subsystem it is filed under", entry.Source, domain.LogSourceMcp)
	}

	if entry.Level != domain.LogLevelWarn {
		t.Errorf("the line was filed at %q, want %q: a warning the surface logged is a warning in the log view",
			entry.Level, domain.LogLevelWarn)
	}

	if entry.DateTime.IsZero() {
		t.Error("the line was filed with no instant: the log view is a timeline, and a line without an " +
			"instant is not in it")
	}

	for _, want := range []string{"manage_config", listenerIdent} {
		if !strings.Contains(entry.Message, want) {
			t.Errorf("the line reads %q, and does not carry %q: the attributes are the part of the line "+
				"that says what happened to what", entry.Message, want)
		}
	}
}

// TestTheSeveritiesTheSurfaceLogsLandWhereAnOperatorLooks: the surface's info,
// warning, and failure lines land as the matching severities and its debug
// detail stays out of the log view, keeping failures visible to the
// operator's error filter.
func TestTheSeveritiesTheSurfaceLogsLandWhereAnOperatorLooks(t *testing.T) {
	logs := bundleWithLogs(t)

	logger := slog.New(NewLogViewHandler(logs.logs, logs.clock))

	logger.Debug("a debug line")
	logger.Info("an info line")
	logger.Warn("a warn line")
	logger.Error("an error line")

	want := []string{
		domain.LogLevelInfo, domain.LogLevelWarn, domain.LogLevelError,
	}

	filed := filedUnder(logs.logs.Stored, domain.LogSourceMcp)

	if len(filed) != len(want) {
		t.Fatalf("the surface filed %d entries under its own subsystem, want %d: the debug line is not a "+
			"log view's line: %v", len(filed), len(want), messagesOf(filed))
	}

	for i, entry := range filed {
		if entry.Level != want[i] {
			t.Errorf("the line %q was filed at %q, want %q", entry.Message, entry.Level, want[i])
		}
	}
}

// confirmingSessionOn is the administrator's session with an operator standing at the client, ready to
// answer the confirmation the way the test names it.
func confirmingSessionOn(t *testing.T, bundle *serverBundle, action string) (*session, *confirmer) {
	t.Helper()

	operator := &confirmer{action: action}

	return newSessionOn(t, bundle, bundle.server.full, Caller{Tier: TierFull}, &mcp.ClientOptions{
		ElicitationHandler: operator.handler(),
	}), operator
}

// bundleWithLogs is the fixture's stores, for a test that files lines and reads them back.
func bundleWithLogs(t *testing.T) *serverBundle {
	t.Helper()

	return newServer(t, true)
}

// filedUnder is the entries filed under one subsystem.
func filedUnder(entries []domain.LogEntry, source domain.LogSource) []domain.LogEntry {
	filed := []domain.LogEntry{}

	for _, entry := range entries {
		if entry.Source == source {
			filed = append(filed, entry)
		}
	}

	return filed
}

// messagesOf is what a set of entries says, for a failure message a reader can act on.
func messagesOf(entries []domain.LogEntry) []string {
	messages := []string{}

	for _, entry := range entries {
		messages = append(messages, entry.Message)
	}

	return messages
}

// anyEntryNames reports whether any line carries the name the test is asking about.
func anyEntryNames(entries []domain.LogEntry, name string) bool {
	for _, entry := range entries {
		if strings.Contains(entry.Message, name) {
			return true
		}
	}

	return false
}
