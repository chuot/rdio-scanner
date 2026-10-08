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
	"errors"
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

// TestEveryServerLogEntryNamesItsSubsystemAndSaysWhatHappened is the guard that closes the writers.
//
// Every message this test reaches is also pinned word for word by the test of the use case that writes it.
// What it adds is the shape, asserted over everything the application layer's writers file, for the two
// rules that are easy to break by accident and hard to notice afterwards:
//
//   - an entry that does not say which part of the server wrote it, which is the entry an operator cannot
//     filter for and which the store now refuses outright;
//   - a message that begins with the internal name of the code that wrote it — dirwatch.watcher:,
//     admin.confighandler.put:, api: — which is the shape the log view used to be made of and the reason an
//     operator could not diagnose anything from it.
//
// Each writer is run once, doing the thing it exists to report, so the guard reaches every source the
// application layer owns. The writers outside this package — the watched directories, the livefeed hub, the
// upload endpoint, the migration, the startup notices — are reached by the same guard in cmd/rdio-scanner,
// where a real server run touches them.
func TestEveryServerLogEntryNamesItsSubsystemAndSaysWhatHappened(t *testing.T) {
	writers := []struct {
		what   string
		source domain.LogSource
		filed  func(t *testing.T) []domain.LogEntry
	}{
		{"the administrator sign-in and sign-out", domain.LogSourceLogin, entriesFromSignInAndSignOut},
		{"the listener access code", domain.LogSourceAccess, entriesFromAccessAuthentication},
		{"the configuration save", domain.LogSourceConfig, entriesFromConfigurationSave},
		{"the administrator password and users", domain.LogSourceAdmin, entriesFromAdministratorUse},
		{"the log view", domain.LogSourceLogs, entriesFromLogSearchFailure},
		{"the retention pass", domain.LogSourcePrune, entriesFromRetentionPass},
		{"the accepted recording", domain.LogSourceIngest, entriesFromAcceptedCall},
		{"the recording the server could not place", domain.LogSourceIngest, entriesFromUnknownRecording},
		{"the recording the server could not store", domain.LogSourceIngest, entriesFromFailedCall},
		{"the audio conversion", domain.LogSourceFFmpeg, entriesFromFailedConversion},
		{"the held-back call", domain.LogSourceDelayer, entriesFromFailedHold},
		{"the downstream servers", domain.LogSourceDownstream, entriesFromFailedDownstream},
		{"the listener count", domain.LogSourceLivefeed, entriesFromListenerCount},
	}

	for _, writer := range writers {
		entries := writer.filed(t)

		filedUnderItself := false

		for _, entry := range entries {
			if entry.Source == writer.source {
				filedUnderItself = true
			}

			if entry.Message == "" {
				t.Errorf("%s filed an entry with no message: %+v", writer.what, entry)
				continue
			}

			if entry.Source == "" {
				t.Errorf("%s filed %q without naming the part of the server that wrote it", writer.what, entry.Message)
				continue
			}

			if !domain.IsKnownLogSource(entry.Source) {
				t.Errorf("%s filed %q under %q, which is not a part of this server", writer.what, entry.Message, entry.Source)
				continue
			}

			if prefix := internalPrefixAt(entry.Message); prefix != "" {
				t.Errorf("%s filed %q, which begins with the internal name %q instead of saying what happened",
					writer.what, entry.Message, prefix)
			}
		}

		if !filedUnderItself {
			t.Errorf("%s filed nothing under %s, so this guard proves nothing about it: %v",
				writer.what, writer.source, entries)
		}
	}
}

// internalPrefixAt is the front-of-message shape the wording rule forbids: a token naming a section and an
// operation, or a bare section name, ending in a colon.
//
// Only the front counts. The failure's own text is kept verbatim after the sentence this feature puts in
// front of it, because that text names the statement or the syscall that failed, so `the call could not be
// ingested: calls.save: no such table: calls` is the rule being followed and not broken.
func internalPrefixAt(message string) string {
	first := message

	if space := strings.Index(message, " "); space >= 0 {
		first = message[:space]
	}

	if strings.HasSuffix(first, ":") {
		return first
	}

	return ""
}

func entriesFromSignInAndSignOut(t *testing.T) []domain.LogEntry {
	fixture := newLoginFixture()

	token := fixture.mustLogin(t, adminCredential)

	if _, err := fixture.attempt("not the credential on record"); err != nil {
		t.Fatalf("refused sign-in: %v", err)
	}

	if _, err := fixture.logout.Handle(context.Background(), AdminLogoutCommand{Token: token}); err != nil {
		t.Fatalf("sign-out: %v", err)
	}

	return fixture.logs.Recorded()
}

func entriesFromAccessAuthentication(t *testing.T) []domain.LogEntry {
	fixture := newAccessFixture()

	fixture.addAccess(domain.Access{ID: 1, Code: "dispatch-code", Ident: "dispatch"})
	fixture.hub.Open("listener", "10.0.0.1")

	if _, err := fixture.presentCode("listener", "wrong-code"); err != nil {
		t.Fatalf("presenting the code: %v", err)
	}

	return fixture.logs.Recorded()
}

func entriesFromConfigurationSave(t *testing.T) []domain.LogEntry {
	fixture := newConfigFixture()

	systems := domain.Systems{{ID: 4, SystemRef: 5, Label: "Harbour"}}

	if _, err := fixture.saveSection(SystemsSection, ConfigurationChanges{Systems: &systems}); err != nil {
		t.Fatalf("saving the systems: %v", err)
	}

	fixture.apikeys.SaveErr = errors.New("constraint failed")

	apikeys := domain.Apikeys{{ID: 2, Key: "recorder-key"}}

	if _, err := fixture.save(ConfigurationChanges{Apikeys: &apikeys}); err != nil {
		t.Fatalf("saving the apikeys: %v", err)
	}

	return fixture.logs.Recorded()
}

func entriesFromAdministratorUse(t *testing.T) []domain.LogEntry {
	fixture := newAdminFixture()

	if _, err := fixture.changePassword.Handle(context.Background(), ChangeAdminPasswordCommand{
		NewCredential: newCredential,
	}); err != nil {
		t.Fatalf("changing the password: %v", err)
	}

	fixture.accesses.SaveErr = errors.New("constraint failed")

	if _, err := fixture.addUser.Handle(context.Background(), AddAccessUserCommand{
		User: domain.Access{Code: "harbour-code", Ident: "harbour"},
	}); err == nil {
		t.Fatal("an access list that cannot be stored must report why")
	}

	return fixture.logs.Recorded()
}

func entriesFromLogSearchFailure(t *testing.T) []domain.LogEntry {
	fixture := newAdminFixture()

	fixture.logs.SearchErr = errors.New(`logs.search: no such table: logs in SELECT COUNT(*) FROM "logs" WHERE TRUE`)

	if _, err := fixture.searchLogs.Handle(context.Background(), SearchLogsQuery{
		Search: domain.ParseLogSearch(map[string]any{}),
	}); err == nil {
		t.Fatal("a log view that cannot be read must reach the caller")
	}

	return fixture.logs.Recorded()
}

func entriesFromRetentionPass(t *testing.T) []domain.LogEntry {
	fixture := newPruneFixture()

	fixture.storeCall(1, pruneTime.AddDate(0, 0, -8))
	fixture.storeLog(pruneTime.AddDate(0, 0, -8), "old event")

	if _, err := fixture.run(); err != nil {
		t.Fatalf("prune: %v", err)
	}

	fixture.calls.Err = errors.New("calls.delete: no such table: calls")

	if _, err := fixture.run(); err == nil {
		t.Fatal("a retention pass that could not prune must report it")
	}

	return fixture.logs.Recorded()
}

func entriesFromAcceptedCall(t *testing.T) []domain.LogEntry {
	fixture := newIngestFixture()

	system := fixture.seedKnownSystem()

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]

	if _, err := fixture.ingest(call); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	return fixture.logs.Recorded()
}

func entriesFromUnknownRecording(t *testing.T) []domain.LogEntry {
	fixture := newIngestFixture()
	fixture.options.Stored.AutoPopulate = false

	call := validIngestCall()
	call.Meta.SystemLabel = "A System Nobody Configured"
	call.Meta.TalkgroupRef = 1514000

	if _, err := fixture.ingest(call); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	return fixture.logs.Recorded()
}

func entriesFromFailedCall(t *testing.T) []domain.LogEntry {
	fixture := newIngestFixture()

	system := fixture.seedKnownSystem()

	fixture.calls.Err = errors.New(`calls.save: no such table: calls in INSERT INTO "calls" ("systemId", "talkgroupId") VALUES (1, 10)`)

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]

	if _, err := fixture.ingest(call); err == nil {
		t.Fatal("a call the store refused must reach the caller")
	}

	return fixture.logs.Recorded()
}

func entriesFromFailedConversion(t *testing.T) []domain.LogEntry {
	fixture := newIngestFixture()

	system := fixture.seedKnownSystem()

	fixture.converter.Err = errors.New("ffmpeg is not available, no audio conversion will be performed")

	call := validIngestCall()
	call.System = system
	call.Talkgroup = system.Talkgroups[0]

	if _, err := fixture.ingest(call); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	return fixture.logs.Recorded()
}

func entriesFromFailedHold(t *testing.T) []domain.LogEntry {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()
	fixture.systems.Stored[0].Talkgroups[0].Delay = 1

	fixture.delayer.Failure = errors.New(`delayer.push: no such table: delayed in INSERT INTO "delayed" ("callId", "timestamp") VALUES (7, 1791288060000)`)

	if _, err := fixture.ingest(delayedIngestCall(fixture.systems.Stored[0])); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	return fixture.logs.Recorded()
}

func entriesFromFailedDownstream(t *testing.T) []domain.LogEntry {
	fixture := newIngestFixture()

	fixture.seedKnownSystem()

	fixture.downstreams.Err = errors.New(`downstreams.all: no such table: downstreams in SELECT "id" FROM "downstreams"`)

	if _, err := fixture.ingest(claimedIngestCall()); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// The re-post is the one part of accepting a call that does not happen while the caller is waiting, so
	// the entry it writes arrives afterwards.
	if !fixture.loggedAsEventually(domain.LogSourceDownstream, "the downstream servers could not be read") {
		t.Fatal("the re-post never reported the list it could not read")
	}

	return fixture.logs.Recorded()
}

func entriesFromListenerCount(t *testing.T) []domain.LogEntry {
	fixture := newLivefeedFixture()
	fixture.options.Stored.ShowListenersCount = true

	fixture.hub.Open("one", "10.0.0.1")
	fixture.debouncer.ListenerChanged()

	fixture.scheduler.RunDue(ListenerCountReportDelay)

	return fixture.logs.Recorded()
}
