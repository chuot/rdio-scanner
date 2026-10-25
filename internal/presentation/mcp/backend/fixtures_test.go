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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/fakes"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

var fixtureTime = time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC)

const (
	testVersion = "8.0.0-test"

	// testAddress is the documentation address a caller reaches the listener from,
	// so a guess budget keyed on it cannot collide with a real host.
	testAddress = "192.0.2.1"

	// adminToken is an open administrator session: the server's accepted window
	// holds the token, the way the administrator's own tests open one.
	adminToken = "capture-admin-token"

	// listenerCode is a listener access code and listenerIdent the identity it is
	// configured under: a restricted server has a non-empty access list, so the
	// restricted fixture seeds this one access.
	listenerCode  = "753159"
	listenerIdent = "dispatch-desk"

	// fixtureApikey is the upload credential the fixture's configuration holds,
	// the value a masking test checks is absent.
	fixtureApikey = "0123456789abcdef-secr3t"

	// The two systems the fixture's history is spread over.
	systemOneRef = uint(10001)
	systemTwoRef = uint(10002)

	// accessAttemptsMax is the access-code attempts one address may make before
	// the server stops looking at what it presents: the administrator login's
	// bound, restated so the test that protects it names it.
	accessAttemptsMax = 3
)

// serverBundle is the MCP listener surface, built from the in-memory stand-ins the application
// tests use, reached through the real gate.
type serverBundle struct {
	server  *Server
	handler http.Handler

	accesses *fakes.Accesses
	logs     *fakes.Logs
	tokens   *fakes.Tokens
	clock    *fakes.Clock

	// resolve is the access-code resolver the surface gates with: its guess budget
	// lives in the handler, so two surfaces sharing a handler share one budget.
	resolve *application.ResolveAccessQueryHandler

	// The recorded history the two surfaces read, and the configuration the search answers from.
	calls   *fakes.Calls
	systems *fakes.Systems
	groups  *fakes.Groups
	tags    *fakes.Tags

	// The rest of the configuration the deployment summary counts: the upload credentials and the
	// ingestion plumbing, disabled entries included, and the options the summary reports.
	apikeys     *fakes.Apikeys
	dirwatches  *fakes.Dirwatches
	downstreams *fakes.Downstreams
	options     *fakes.Options

	// What a configuration save needs beside the stores: the listener hub, the
	// administrator hub, the directory watchers a save brackets, and the
	// credential machinery a password change compares and hashes with.
	hub      *fakes.Hub
	sessions *fakes.AdministratorSessions
	watches  *fakes.Dirwatchers
	password *fakes.PasswordVerifier
	hashes   *fakes.PasswordHasher
}

func newServer(t *testing.T, restricted bool) *serverBundle {
	t.Helper()

	bundle := &serverBundle{
		accesses:    &fakes.Accesses{},
		logs:        &fakes.Logs{},
		tokens:      fakes.NewTokens(),
		clock:       fakes.NewClock(fixtureTime),
		apikeys:     &fakes.Apikeys{},
		dirwatches:  &fakes.Dirwatches{},
		downstreams: &fakes.Downstreams{},
		options:     &fakes.Options{},
		hub:         fakes.NewHub(),
		sessions:    fakes.NewAdministratorSessions(),
		watches:     fakes.NewDirwatchers(),
		password:    fakes.NewPasswordVerifier(),
		hashes:      fakes.NewPasswordHasher(),
	}

	bundle.tokens.Window = []string{adminToken}

	if restricted {
		bundle.accesses.Stored = domain.Accesses{
			{ID: 1, Code: listenerCode, Ident: listenerIdent, Scope: domain.AccessScope{Present: true, Wildcard: true}},
		}
	}

	bundle.resolve = application.NewResolveAccessQueryHandler(bundle.accesses, bundle.logs, bundle.clock)

	// The recorded history the capabilities answer from, over two systems so scoped
	// and unscoped callers differ, and two days so a narrowed slice differs from
	// a slice that was not narrowed.
	bundle.calls = fakes.NewCalls()
	bundle.calls.Stored = map[uint64]domain.Call{
		1: fixtureCall(1, systemOneRef, fixtureTime.Add(-3*time.Hour)),
		2: fixtureCall(2, systemOneRef, fixtureTime.Add(-2*time.Hour)),
		3: fixtureCall(3, systemTwoRef, fixtureTime.Add(-1*time.Hour)),
		4: fixtureCall(4, systemTwoRef, fixtureTime.Add(-27*time.Hour)),
	}
	bundle.groups = &fakes.Groups{Stored: domain.Groups{{ID: 1, Label: "Fire"}}}
	bundle.tags = &fakes.Tags{Stored: domain.Tags{{ID: 3, Label: "Fire Dispatch"}}}

	// The systems carry talkgroups, sites, and units, so the deployment summary
	// counts them as real things and not as none.
	bundle.systems = &fakes.Systems{Stored: domain.Systems{
		{
			ID: 1, Label: "System", SystemRef: systemOneRef, AutoPopulate: true, Delay: 5000,
			Talkgroups: domain.Talkgroups{
				{ID: 1, TalkgroupRef: 1514000, Label: "Talkgroup", TagID: 3},
				{ID: 2, TalkgroupRef: 1514001, Label: "Second Talkgroup", TagID: 3},
			},
			Sites: domain.Sites{{ID: 1, SiteRef: 1, Label: "Site"}},
			Units: domain.Units{{ID: 1, UnitRef: 1001, Label: "Unit"}},
		},
		{
			ID: 2, Label: "System", SystemRef: systemTwoRef,
			Talkgroups: domain.Talkgroups{{ID: 3, TalkgroupRef: 1514002, Label: "Other Talkgroup", TagID: 3}},
			Sites:      domain.Sites{{ID: 2, SiteRef: 2, Label: "Site"}},
			Units:      domain.Units{{ID: 2, UnitRef: 1002, Label: "Unit"}},
		},
	}}

	// The options the summary reports: prune horizon, listener ceiling, recordings
	// converted, duplicates detected, auto-populate on, and the notice that the
	// default credential is still in place.
	bundle.options = &fakes.Options{Stored: domain.Options{
		AudioConversion:         domain.AudioConversionEnabled,
		AutoPopulate:            true,
		MaxClients:              200,
		PruneDays:               7,
		AdminPasswordNeedChange: true,
	}}

	// The ingestion plumbing and the upload credentials, so the deployment summary counts real
	// things and counts disabled ones as disabled.
	bundle.apikeys = &fakes.Apikeys{Stored: domain.Apikeys{
		{ID: 1, Ident: "trunk-recorder", Key: fixtureApikey},
		{ID: 2, Ident: "dirwatch", Key: "second-key", Disabled: true},
	}}
	bundle.dirwatches = &fakes.Dirwatches{Stored: domain.Dirwatches{
		{ID: 1, Directory: "/var/spool/asterisk/monitor", Disabled: true, SystemID: 1},
		{ID: 2, Directory: "/var/spool/asterisk/monitor-second"},
	}}
	bundle.downstreams = &fakes.Downstreams{Stored: domain.Downstreams{
		{ID: 1, URL: "https://peer.example/api/trunk-recorder-call-upload", Disabled: true},
		{ID: 2, URL: "https://second.example/api/call-upload", Scope: domain.AccessScope{
			Present: true,
			Systems: []domain.ScopedSystem{{SystemRef: systemOneRef, TalkgroupsGiven: true, TalkgroupsWildcard: true}},
		}},
	}}

	// The log view, over four subsystems so a subsystem search, a severity search,
	// and a neighbourhood explanation all have something to read.
	bundle.logs.Stored = fixtureLogEntries()

	bundle.server = New(bundle.Deps())
	bundle.handler = bundle.server.Handler()

	return bundle
}

// fixtureCall is one recorded call on one system.
func fixtureCall(id uint64, systemRef uint, at time.Time) domain.Call {
	call := domain.NewCall()

	call.ID = id
	call.Audio = []byte("audio-bytes")
	call.AudioFilename = "call.wav"
	call.AudioLength = time.Second
	call.AudioMime = "audio/wav"
	call.System = domain.System{ID: uint64(systemRef), Label: "System", SystemRef: systemRef}
	call.Talkgroup = domain.Talkgroup{ID: 1, Label: "Talkgroup", TalkgroupRef: 1514000}
	call.Timestamp = at

	return call
}

// fixtureLogEntries is the log view the diagnostics read: four subsystems, three severities, and a
// failure with lines on both sides of it in its own subsystem and in a neighbour's.
func fixtureLogEntries() []domain.LogEntry {
	return []domain.LogEntry{
		{ID: 1, DateTime: fixtureTime.Add(-4 * time.Hour), Level: domain.LogLevelInfo, Source: domain.LogSourceFFmpeg, Message: "ffmpeg is converting call 12"},
		{ID: 2, DateTime: fixtureTime.Add(-3 * time.Hour), Level: domain.LogLevelError, Source: domain.LogSourceFFmpeg, Message: "ffmpeg conversion failed for call 12"},
		{ID: 3, DateTime: fixtureTime.Add(-3*time.Hour + 30*time.Minute), Level: domain.LogLevelInfo, Source: domain.LogSourceIngest, Message: "call 12 arrived on system 10001"},
		{ID: 4, DateTime: fixtureTime.Add(-2 * time.Hour), Level: domain.LogLevelWarn, Source: domain.LogSourceAccess, Message: "the access code of dispatch-desk expired"},
		{ID: 5, DateTime: fixtureTime.Add(-1 * time.Hour), Level: domain.LogLevelInfo, Source: domain.LogSourcePrune, Message: "pruned 3 calls"},

		// The server's own startup line, which is what a restart leaves in the log
		// view, and a feed line naming the talkgroup the diagnosis is asked about.
		{ID: 6, DateTime: fixtureTime.Add(-5 * time.Hour), Level: domain.LogLevelWarn, Source: domain.LogSourceServer, Message: "server started"},
		{ID: 7, DateTime: fixtureTime.Add(-90 * time.Minute), Level: domain.LogLevelWarn, Source: domain.LogSourceLivefeed, Message: "the livefeed for talkgroup 1514000 on system 10001 stopped"},
	}
}

// Deps is the wiring the composition root does for this surface, with the use cases built over the
// fixture's in-memory stores.
func (bundle *serverBundle) Deps() Deps {
	return Deps{
		Version: testVersion,
		Call:    application.NewGetCallQueryHandler(bundle.calls),
		Search:  application.NewSearchCallsQueryHandler(bundle.calls, bundle.systems, bundle.groups, bundle.tags),
		Config: application.NewGetConfigHandler(application.ConfigurationRepositories{
			Accesses:    bundle.accesses,
			Apikeys:     bundle.apikeys,
			Dirwatches:  bundle.dirwatches,
			Downstreams: bundle.downstreams,
			Groups:      bundle.groups,
			Options:     bundle.options,
			Systems:     bundle.systems,
			Tags:        bundle.tags,
		}),
		Logs:    application.NewSearchLogsHandler(bundle.logs, bundle.clock),
		Resolve: bundle.resolve,
		Session: application.NewCheckAdminSessionHandler(bundle.tokens),
		Clock:   bundle.clock,

		Section:    application.NewUpdateConfigurationSectionHandler(bundle.configurationPorts()),
		SubSection: application.NewUpdateSystemSubSectionHandler(bundle.configurationPorts()),
		AddUser:    application.NewAddAccessUserHandler(bundle.accessUserPorts()),
		RemoveUser: application.NewRemoveAccessUserHandler(bundle.accessUserPorts()),
		Password: application.NewChangeAdminPasswordHandler(
			bundle.options, bundle.password, bundle.hashes, bundle.logs, bundle.clock),
	}
}

// repositories is the set of stores the configuration is spread over, the same set the
// administrator's own surface saves through.
func (bundle *serverBundle) repositories() application.ConfigurationRepositories {
	return application.ConfigurationRepositories{
		Accesses:    bundle.accesses,
		Apikeys:     bundle.apikeys,
		Dirwatches:  bundle.dirwatches,
		Downstreams: bundle.downstreams,
		Groups:      bundle.groups,
		Options:     bundle.options,
		Systems:     bundle.systems,
		Tags:        bundle.tags,
	}
}

// configurationPorts is everything a configuration save needs: the stores, the
// log view for reporting a failure, the listener and administrator hubs, the
// watchers a save brackets, and the clock.
func (bundle *serverBundle) configurationPorts() application.ConfigurationPorts {
	return application.ConfigurationPorts{
		ConfigurationRepositories: bundle.repositories(),
		Logs:                      bundle.logs,
		Hub:                       bundle.hub,
		Admin:                     bundle.sessions,
		Dirwatchers:               bundle.watches,
		Clock:                     bundle.clock,
	}
}

// accessUserPorts is what adding and removing a listener needs.
func (bundle *serverBundle) accessUserPorts() application.AccessUserPorts {
	return application.AccessUserPorts{
		ConfigurationRepositories: bundle.repositories(),
		Logs:                      bundle.logs,
		Admin:                     bundle.sessions,
		Clock:                     bundle.clock,
	}
}

// rebuild hangs the surface back up with a proxy configuration, the way the
// composition root hands the listener the trusted-proxy list, over the same
// stores, so spent attempts stay spent.
func (bundle *serverBundle) rebuild(t *testing.T, trusted *httpaddr.Trusted) {
	t.Helper()

	deps := bundle.Deps()
	deps.Trusted = trusted

	bundle.server = New(deps)
	bundle.handler = bundle.server.Handler()
}

// caller runs the gate around a capture handler and returns the context the gate handed inward.
// A refused caller hands back nothing at all.
func (bundle *serverBundle) caller(req *http.Request) context.Context {
	var ctx context.Context

	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() })
	bundle.server.Gate()(inner).ServeHTTP(httptest.NewRecorder(), req)

	return ctx
}

// logText is everything the surface filed in the server's log view, oldest first.
func (bundle *serverBundle) logText() string {
	var out string
	for _, entry := range bundle.logs.Recorded() {
		out += entry.Message + "\n"
	}
	return out
}

// wildcardScope is the scope that grants every system and talkgroup: a listener that names no
// systems of its own.
func wildcardScope() domain.AccessScope {
	return domain.AccessScope{Present: true, Wildcard: true}
}

// scopedTo is the scope naming systems with every talkgroup on them.
func scopedTo(systemRefs ...uint) domain.AccessScope {
	scope := domain.AccessScope{Present: true}

	for _, systemRef := range systemRefs {
		scope.Systems = append(scope.Systems, domain.ScopedSystem{
			SystemRef:          systemRef,
			TalkgroupsGiven:    true,
			TalkgroupsWildcard: true,
		})
	}

	return scope
}
