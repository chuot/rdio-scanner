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

package app

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/clock"
	"rdio-scanner/internal/infrastructure/dblock"
	"rdio-scanner/internal/infrastructure/delayer"
	"rdio-scanner/internal/infrastructure/dirwatch"
	"rdio-scanner/internal/infrastructure/dirwatch/parsers"
	"rdio-scanner/internal/infrastructure/downstream"
	"rdio-scanner/internal/infrastructure/passwords"
	"rdio-scanner/internal/infrastructure/persistence"
	"rdio-scanner/internal/infrastructure/tokens"
	adminbackend "rdio-scanner/internal/presentation/admin/backend"
	adminfrontend "rdio-scanner/internal/presentation/admin/frontend"
	callsbackend "rdio-scanner/internal/presentation/calls/backend"
	"rdio-scanner/internal/presentation/common/httpaddr"
	"rdio-scanner/internal/presentation/common/upload"
	mcpbackend "rdio-scanner/internal/presentation/mcp/backend"
	scannerbackend "rdio-scanner/internal/presentation/scanner/backend"
	scannerfrontend "rdio-scanner/internal/presentation/scanner/frontend"
)

const everyInterface = "0.0.0.0"

// httpTimeout bounds how long one request may take in either direction, long enough
// that an upload of a recorded call is not cut off by it.
// see the request-and-stream-deadline-constants maintainer note.
const httpTimeout = 30 * time.Second

// Server is the assembled server: the adapters, the use cases, and the endpoints the
// composition root put together, and the thing that starts, answers with, and stops them.
// Its fields are only the pieces the composition root reaches for after assembly: what it
// reads at startup, what it has to stop again, and what a test looks at.
type Server struct {
	config  *Config
	console *consoleLog

	db      *sql.DB
	dialect persistence.Dialect

	// dbLock is the claim this process holds on the deployment while its
	// database is open. It is taken before the database is opened and
	// released after the database is closed, so nothing else can open the
	// same files while this server is writing them.
	dbLock *dblock.Handle

	repos application.ConfigurationRepositories

	// options and accesses are the adapters the composition root itself talks to: the options
	// because it supplies the credential a server starts with, the accesses because a test
	// asks what the server requires of listeners after it changes them.
	options  *persistence.Options
	accesses *persistence.Accesses

	// signing is the startup step that gives the server a signing secret to sign sessions
	// with and puts it on record, so the key outlives the process that made it.
	signing *application.EnsureAdminSigningSecretHandler

	// logs is the log the whole server writes into, including the server's own notices.
	logs ports.LogRepository

	// converterNotice is what the operator must be told about the ffmpeg this server
	// converts with, empty when there is nothing to say. It is decided once while the
	// server is wired and said once when it starts.
	converterNotice string

	clock     ports.Clock
	scheduler ports.Scheduler
	hashes    ports.PasswordHasher

	// settings answers what the server requires of listeners, which the hub is asked rather than told.
	settings *listenerSettings

	// watches, pruning, and held calls are the things the server starts running and has to stop again.
	watches *dirwatch.Dirwatchers
	pruning ports.Timer
	prune   *application.PruneOldCallsHandler
	held    *delayer.Delayer

	// collectorDone closes when the loop carrying held calls back into the pipeline has
	// returned: a server that has stopped does not leave a collector behind.
	collectorDone chan struct{}

	// ingest is the path every recorded call goes through, kept because calls whose hold
	// has run out are delivered through that same path rather than through a copy of it.
	ingest *application.IngestCallHandler

	// serving holds the addresses this server is answering on, so stopping the server stops
	// them. The serving goroutine writes the list while the stopping goroutine reads it,
	// so the list is carried under servingMutex: the stop sees a complete list, never a
	// half-built one.
	serving      []*http.Server
	servingMutex sync.Mutex

	// listeners, adminSessions, and listenerCounts are the connections the server has taken
	// over and the report it owes about how many listeners there are. Shutting an address
	// down does not wait for a handed-over connection, so the stop ends these itself, while
	// the log their departures are written into can still be written to.
	listeners      *scannerbackend.Hub
	adminSessions  *adminbackend.Sessions
	listenerCounts *application.ListenerCountDebouncer

	// runtime is the composition root's own answer to what it is running. It is marked
	// started when the start sequence runs, so a server assembled but never started
	// reports no uptime instead of an uptime counted backwards from the Unix epoch.
	// see the status-endpoint-reads maintainer note.
	runtime *runtimeState

	// mcpSurface is the Model Context Protocol endpoint, built only when the configuration
	// names it. An assistant holding a session is a connection the server took over and
	// has to end itself, the same reason the listener and admin sockets are ended here.
	mcpSurface *mcpbackend.Server

	routes http.Handler

	// stopping guards the stop, so the two goroutines that can ask for it do not both run
	// it, and the second one waits for the first to finish.
	stopping sync.Once

	// trusted is the parsed TrustedProxies configuration: the proxy list every endpoint
	// that names an address honors, so the address an operator reads and the address the
	// limiter counts are one address.
	trusted *httpaddr.Trusted

	// stopFailed is the failure the stop could not file: the database that would have held
	// the report is the database that failed to close.
	stopFailed error
}

// New opens the database the configuration names, brings it to this server's schema,
// seeds what a fresh deployment starts with, and wires everything to everything else.
// It starts nothing: a caller that finds the assembly unusable has nothing to undo.
// The configuration names the console, the operator's log copy, the ffmpeg, and the version.
func New(config Config) (*Server, error) {
	ctx := context.Background()

	srv := &Server{config: &config, console: newConsoleLog(config.Out)}

	// The proxy list is parsed before anything opens, so a list the operator wrote that is
	// not a list of addresses is a configuration error refused at startup, not one
	// discovered while deciding who to trust mid-request.
	trusted, err := httpaddr.ParseTrusted(config.TrustedProxies)
	if err != nil {
		return nil, err
	}

	srv.trusted = trusted

	dialect, err := dialectOf(config.DbType)
	if err != nil {
		return nil, err
	}

	srv.dialect = dialect

	// The deployment is claimed before its database is opened: a second
	// rdio-scanner process pointed at the same files is refused here rather
	// than sharing the database and risking what an operator has recorded.
	// The claim is released with the database in the stop.
	lock, err := dblock.Acquire(config.GetDbFilePath())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInUse, err)
	}

	srv.dbLock = lock

	db, err := persistence.Open(persistence.Settings{
		Dialect:        dialect,
		FilePath:       config.GetDbFilePath(),
		Host:           config.DbHost,
		Port:           config.DbPort,
		Username:       config.DbUsername,
		Password:       config.DbPassword,
		Name:           config.DbName,
		TLS:            config.DbTLS,
		SslMode:        config.DbSslMode,
		ConnectTimeout: config.DbConnectTimeout,
	})
	if err != nil {
		_ = lock.Close()
		return nil, err
	}

	srv.db = db

	// The clock is created before the migration because the migration stamps the rows it
	// archives with the server's clock, as every writer this server owns stamps its entries.
	srv.clock = clock.NewClock()

	migrationLines := []domain.LogEntry{}

	if err := persistence.Migrate(ctx, db, dialect, srv.clock, func(level, message string) {
		// Said out loud in the shape every other line the server says has, so the word an
		// operator greps for finds this line too. It is not the echo of an entry: there is no
		// entry yet, which is why the level and the subsystem are put on it here.
		srv.console.println(renderLogEntry(level, domain.LogSourceDatabase, message))

		migrationLines = append(migrationLines, domain.LogEntry{Level: level, Message: message})
	}); err != nil {
		_ = lock.Close()
		return nil, err
	}

	if err := persistence.Seed(ctx, db, dialect); err != nil {
		_ = lock.Close()
		return nil, err
	}

	if err := srv.wire(migrationLines); err != nil {
		_ = lock.Close()
		return nil, err
	}

	return srv, nil
}

// wire builds the adapters, hands each one to the use cases that need it, and hangs the
// endpoints on the paths the legacy server answers, in the order the pieces need each other.
// migrationLines are what the migration said while it rebuilt the log table. They are
// filed here, first, once a log repository exists.
func (s *Server) wire(migrationLines []domain.LogEntry) error {
	ctx := context.Background()

	accesses := persistence.NewAccesses(s.db, s.dialect)
	apikeys := persistence.NewApikeys(s.db, s.dialect)
	calls := persistence.NewCalls(s.db, s.dialect)
	delayed := persistence.NewDelayedCalls(s.db, s.dialect)
	dirwatches := persistence.NewDirwatches(s.db, s.dialect)
	downstreams := persistence.NewDownstreams(s.db, s.dialect)
	groups := persistence.NewGroups(s.db, s.dialect)
	logs := persistence.NewLogs(s.db, s.dialect)
	options := persistence.NewOptions(s.db, s.dialect)
	systems := persistence.NewSystems(s.db, s.dialect)
	tags := persistence.NewTags(s.db, s.dialect)

	s.accesses = accesses
	s.options = options
	s.repos = application.ConfigurationRepositories{
		Accesses:    accesses,
		Apikeys:     apikeys,
		Dirwatches:  dirwatches,
		Downstreams: downstreams,
		Groups:      groups,
		Options:     options,
		Systems:     systems,
		Tags:        tags,
	}

	// The scheduler runs the server's timed work, pruning and delayed holds among it, and
	// recovers a job that panics: the failure is filed in the log view for the operator
	// instead of taking the server down on a goroutine nobody owns.
	// see the configuration-option-name-reuse maintainer note.
	s.scheduler = clock.NewScheduler(func(problem any) {
		_ = logs.AppendLog(ctx, domain.LogEntry{
			DateTime: s.clock.Now(),
			Level:    domain.LogLevelWarn,
			Source:   domain.LogSourceServer,
			Message:  fmt.Sprintf("scheduled work panicked: %v", problem),
		})
	})

	// A stored value the read cannot hold, such as a negative expiration, limit, or order,
	// is clamped to the safe reading, and the row and its values are filed as a problem in
	// the log view, naming what the server enforces instead.
	// see the request-and-stream-deadline-constants maintainer note.
	clampWarning := func(message string) {
		_ = logs.AppendLog(ctx, domain.LogEntry{
			DateTime: s.clock.Now(),
			Level:    domain.LogLevelWarn,
			Source:   domain.LogSourceConfig,
			Message:  message,
		})
	}

	accesses.OnWarn(clampWarning)
	apikeys.OnWarn(clampWarning)
	calls.OnWarn(clampWarning)
	dirwatches.OnWarn(clampWarning)
	downstreams.OnWarn(clampWarning)
	groups.OnWarn(clampWarning)
	systems.OnWarn(clampWarning)
	tags.OnWarn(clampWarning)

	credentials := passwords.NewBcrypt(s.config.BcryptCost)
	s.hashes = credentials

	// The migration said these things while the log table was still being rebuilt, which is
	// why they were kept rather than written: an entry written during the pass takes an id
	// in the table the pass is rebuilding. They are filed now, in the order they were said,
	// through the plain repository and not the echoing one.
	for _, entry := range migrationLines {
		entry.DateTime = s.clock.Now()
		entry.Source = domain.LogSourceDatabase

		_ = logs.AppendLog(ctx, entry)
	}

	// Every log entry the server writes is also said out loud, which is what an operator
	// running the server in a terminal expects. A server running as a service says them to
	// the manager instead, at the level each entry is filed under.
	echo := s.config.Echo

	if echo == nil {
		echo = s.console
	}

	s.logs = &loggedEvents{logs: logs, echo: echo}

	// The converter and prober come from the server's bundle, written out of the binary into
	// the server's own folder; when the bundle produced nothing, the host-installed pair
	// converts and measures instead, as those deployments have always done.
	// see the ffmpeg-carried-by-the-server-and-hosted-by-the-operator maintainer note.
	converter, prober, notice := s.config.Converter(s.config.GetFFmpegDirPath())

	s.converterNotice = notice
	forwarder := downstream.NewForwarder(groups, tags)
	reader := parsers.New(systems, s.clock)

	// Held calls are written down with the instant they stop being held back, so a server
	// that stops and starts again picks them up, and the delayer hands them to the same
	// delivery step an ingested call ends with.
	// see the hub-and-debouncer-contract maintainer note.
	held := delayer.New(delayer.Deps{
		Delayed:   delayed,
		Calls:     calls,
		Scheduler: s.scheduler,
		Clock:     s.clock,
		Logs:      s.logs,
	})

	s.held = held

	// The default admin credential is made at every startup and never written down, which is
	// why a session started before a restart does not survive it. It has to be supplied
	// before the first read, because that read is the one that puts it in place.
	initial, err := credentials.Hash(ctx, domain.DefaultAdminPassword)
	if err != nil {
		return err
	}

	options.StoreInitialCredential(initial)

	// signing is the startup step that makes sure the server has a signing secret worth
	// having and puts it on record. It is built here because it needs the options adapter,
	// and start runs it after the configuration has been read, so it decides about the
	// options the server is running with.
	s.signing = application.NewEnsureAdminSigningSecretHandler(options, options, s.logs, s.clock)

	s.settings = &listenerSettings{accesses: accesses, options: options}

	changes := &listenerChanges{}

	hub := scannerbackend.NewHub(s.settings, changes, s.logs, s.clock)

	debouncer := application.NewListenerCountDebouncer(hub, s.logs, options, s.scheduler, s.clock)

	// The hub counts listeners and says when the count is no longer what it was, and the
	// debouncer decides when to report it, so a client that connects and drops repeatedly
	// costs one report.
	// see the hub-and-debouncer-contract maintainer note.
	changes.debouncer = debouncer

	// The composition root keeps what it has to end itself: the hub's connections, the
	// admin's connections, and the listener-count report that is owed but not yet written.
	// see the stop-and-shutdown-ordering maintainer note.
	s.listeners = hub
	s.listenerCounts = debouncer

	tokensAdapter := tokens.New(optionsSecrets{options: options})

	ingest := application.NewIngestCallHandler(
		calls, systems, groups, tags, options, s.logs, converter, prober, hub, held, downstreams, forwarder, s.clock,
	)

	s.ingest = ingest

	watches := dirwatch.NewDirwatchers(reader, s.logs, &ingestSink{ingest: ingest}, s.clock)
	s.watches = watches

	s.prune = application.NewPruneOldCallsHandler(calls, s.logs, options, s.clock)

	adminSessions := adminbackend.NewSessions(adminbackend.SessionDeps{
		Session: application.NewCheckAdminSessionHandler(tokensAdapter),
		Version: s.config.Version,
		Logs:    s.logs,
		Clock:   s.clock,
		Trusted: s.trusted,
	})

	s.adminSessions = adminSessions

	// The status read asks what this server is running, and only the composition root holds
	// all of it at once: the connections, the watchers, the holds, the audio programs, and
	// the file the database is written into.
	// see the status-endpoint-reads maintainer note.
	s.runtime = &runtimeState{
		hub:       hub,
		watches:   watches,
		held:      held,
		sessions:  adminSessions,
		converter: converter,
		prober:    prober,

		dialect: s.dialect,
		dbPath:  s.config.GetDbFilePath(),
	}

	configurationPorts := application.ConfigurationPorts{
		ConfigurationRepositories: s.repos,
		Admin:                     adminSessions,
		Clock:                     s.clock,
		Dirwatchers:               watches,
		Hub:                       hub,
		Logs:                      s.logs,
	}

	accessUserPorts := application.AccessUserPorts{
		ConfigurationRepositories: s.repos,
		Admin:                     adminSessions,
		Clock:                     s.clock,
		Logs:                      s.logs,
	}

	adminAPI := adminbackend.NewAdmin(adminbackend.AdminDeps{
		AddUser:              application.NewAddAccessUserHandler(accessUserPorts),
		Config:               application.NewGetConfigHandler(s.repos),
		ConfigSocket:         adminSessions,
		Configuration:        application.NewUpdateConfigurationHandler(configurationPorts),
		ConfigurationSection: application.NewUpdateConfigurationSectionHandler(configurationPorts),
		SystemSubSection:     application.NewUpdateSystemSubSectionHandler(configurationPorts),
		BaseDir:              s.config.BaseDir,
		Docker:               os.Getenv("DOCKER") != "",
		Logout:               application.NewAdminLogoutHandler(tokensAdapter, s.logs, s.clock),
		Logs:                 s.logs,
		Clock:                s.clock,
		SearchLogs:           application.NewSearchLogsHandler(s.logs, s.clock),
		Login:                application.NewAdminLoginHandler(options, s.logs, tokensAdapter, credentials, credentials, credentials, s.clock),
		Password:             application.NewChangeAdminPasswordHandler(options, credentials, credentials, s.logs, s.clock),
		RemoveUser:           application.NewRemoveAccessUserHandler(accessUserPorts),
		Session:              application.NewCheckAdminSessionHandler(tokensAdapter),

		// Status answers what this server is doing right now: the whole of
		// what the administrator's landing page is built from.
		Status: application.NewGetRuntimeStatusHandler(s.runtime, calls, logs, s.clock),

		Version: s.config.Version,
		Trusted: s.trusted,
	})

	uploads := upload.New(upload.Deps{
		Ingest: application.NewUploadCallHandler(apikeys, ingest),
		Logs:   s.logs,
		Parser: reader,
		Clock:  s.clock,
	})

	listener := scannerbackend.NewHandler(scannerbackend.Deps{
		Access:   application.NewAuthenticateAccessHandler(accesses, systems, groups, tags, options, s.logs, hub, s.clock),
		Call:     application.NewGetCallQueryHandler(calls),
		Clock:    s.clock,
		Config:   application.NewGetConfigHandler(s.repos),
		Gate:     application.NewAccessGate(accesses, hub),
		Hub:      hub,
		Livefeed: application.NewSetLivefeedHandler(hub),
		Logs:     s.logs,
		Search:   application.NewSearchCallsQueryHandler(calls, systems, groups, tags),
		Settings: s.settings,
		Version:  s.config.Version,
		Trusted:  s.trusted,
	})

	// The access-code resolver is built once for the whole listener. Its guess budget
	// belongs to the address that spends it, not to the entry point the address arrives at:
	// one address guessing at several surfaces spends one budget.
	resolveAccess := application.NewResolveAccessQueryHandler(accesses, s.logs, s.clock)

	// The Model Context Protocol surface is built only when the configuration names it. It
	// answers with the same use cases the administrator's page and the listener socket
	// answer with, adds no use case of its own, and reaches no socket.
	if s.config.McpServer {
		s.mcpSurface = mcpbackend.New(mcpbackend.Deps{
			Version: s.config.Version,
			Docker:  os.Getenv("DOCKER") != "",

			// MCP lines land in the operator's log view, filed under the
			// surface's subsystem and echoed like every other writer's lines:
			// same repository, clock, and view.
			Logger:  slog.New(mcpbackend.NewLogViewHandler(s.logs, s.clock)),
			Session: application.NewCheckAdminSessionHandler(tokensAdapter),
			Resolve: resolveAccess,
			Trusted: s.trusted,
			Config:  application.NewGetConfigHandler(s.repos),
			Logs:    application.NewSearchLogsHandler(s.logs, s.clock),
			Call:    application.NewGetCallQueryHandler(calls),
			Search:  application.NewSearchCallsQueryHandler(calls, systems, groups, tags),
			Clock:   s.clock,

			Section:    application.NewUpdateConfigurationSectionHandler(configurationPorts),
			SubSection: application.NewUpdateSystemSubSectionHandler(configurationPorts),
			AddUser:    application.NewAddAccessUserHandler(accessUserPorts),
			RemoveUser: application.NewRemoveAccessUserHandler(accessUserPorts),
			Password:   application.NewChangeAdminPasswordHandler(options, credentials, credentials, s.logs, s.clock),
		})
	}

	mux := http.NewServeMux()

	adminAPI.Register(mux)
	uploads.Register(mux)

	// The call history surface is registered unconditionally: it is the HTTP half of the
	// recorded-call capability, open to any client under the server's GNU General
	// Public License. A deployment without the MCP surface still answers call history.
	// see the api-unconditional-mcp-an-option maintainer note.
	callsAPI := callsbackend.NewCalls(callsbackend.CallsDeps{
		Call:    application.NewGetCallQueryHandler(calls),
		Clock:   s.clock,
		Logs:    s.logs,
		Resolve: resolveAccess,
		Search:  application.NewSearchCallsQueryHandler(calls, systems, groups, tags),
		Session: application.NewCheckAdminSessionHandler(tokensAdapter),
		Trusted: s.trusted,
	})

	callsAPI.Register(mux)

	// The MCP surface is served only when the operator named it. A deployment that never
	// asked for it never learns the path exists: an assistant there gets the answer every
	// unmatched path gets.
	var assistant http.Handler

	if s.mcpSurface != nil {
		assistant = s.mcpSurface.Handler()
	}

	serveApplications(mux, listener, scannerfrontend.Embedded(), adminfrontend.Embedded())

	// Every request the server answers carries a deadline, so an administrator's request
	// fails cleanly instead of hanging on a silent database. The assistant endpoint carries
	// the stream bound instead, because a session's stream lives as long as the session.
	// see the request-and-stream-deadline-constants maintainer note.
	s.routes = withSubtreeDeadlines(mux, assistant, streamDeadline, requestDeadline)

	return nil
}

// Start runs the startup the legacy server runs: the up notice, the base folder, the
// configuration read out of the database, the held calls from the last stop, the
// pruning timer, and the directory watches.
// Three legacy parts are absent because this server does not have them: the ingest
func (s *Server) Start(ctx context.Context) error {
	// The instant the start sequence ran is what the status read counts an uptime from. It
	// is recorded here and not read back from a log line: the log is something the operator
	// prunes, and an uptime should not depend on how much past the deployment keeps.
	s.runtime.markStarted(s.clock.Now())

	// The legacy server ignores a startup notice it cannot store, and the line has already
	// reached the console by the time the log view is asked for it.
	_ = s.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: s.clock.Now(),
		Level:    domain.LogLevelWarn,
		Source:   domain.LogSourceServer,
		Message:  "server started",
	})

	if len(s.config.BaseDir) > 0 {
		// The base folder is the fact behind every path the server writes to, and until now it
		// existed only on the terminal at startup. It is filed as a notice, not a warning:
		// nothing is wrong, this records which folder this deployment is serving.
		// see the stop-and-shutdown-ordering maintainer note.
		_ = s.logs.AppendLog(ctx, domain.LogEntry{
			DateTime: s.clock.Now(),
			Level:    domain.LogLevelInfo,
			Source:   domain.LogSourceServer,
			Message:  "the server is serving " + s.config.BaseDir,
		})
	}

	// Which ffmpeg the server converts with is decided while it is wired and said here once,
	// so an operator whose server could not use the ffmpeg it carries learns that at
	// startup rather than from a recording that was never converted.
	// see the ffmpeg-carried-by-the-server-and-hosted-by-the-operator maintainer note.
	if s.converterNotice != "" {
		_ = s.logs.AppendLog(ctx, domain.LogEntry{
			DateTime: s.clock.Now(),
			Level:    domain.LogLevelWarn,
			Source:   domain.LogSourceFFmpeg,
			Message:  s.converterNotice,
		})
	}

	if err := s.readConfiguration(ctx); err != nil {
		return err
	}

	// The signing secret is the startup rule the legacy server never finished: it made a
	// fresh random key whenever the stored one was unusable and never wrote it down, so
	// sessions died at every start. This step makes and records a key once.
	// see the signing-secret-choice maintainer note.
	if _, err := s.signing.Handle(ctx, application.EnsureAdminSigningSecretCommand{}); err != nil {
		return err
	}

	// The calls this server was holding when it last stopped are picked up here, before
	// anything is answered: a hold that ran out while the server was down is delivered now,
	// and a hold still running is armed again.
	// see the stop-and-shutdown-ordering maintainer note.
	s.collectorDone = make(chan struct{})

	go s.deliverHeldCalls(ctx)

	if err := s.held.Restore(ctx); err != nil {
		return err
	}

	s.pruning = pruneHourly(s.scheduler, s.prune)

	watches, err := s.repos.Dirwatches.AllDirwatches(ctx)
	if err != nil {
		return err
	}

	s.watches.Start(ctx, watches)

	return nil
}

func (s *Server) deliverHeldCalls(ctx context.Context) {
	defer close(s.collectorDone)

	for {
		select {
		case call, ok := <-s.held.Waiting():
			if !ok {
				return
			}

			if ctx.Err() != nil {
				return
			}

			s.ingest.DeliverCall(ctx, call)

		case <-ctx.Done():
			return
		}
	}
}

// readConfiguration reads the stored configuration in the order the legacy server
// reads it and stops at the first section it cannot read. The order is not cosmetic:
// an operator comparing two startup logs sees the same first failure.
// see the startup-configuration-read-order maintainer note.
func (s *Server) readConfiguration(ctx context.Context) error {
	if _, err := s.repos.Accesses.AllAccesses(ctx); err != nil {
		return err
	}
	if _, err := s.repos.Apikeys.AllApikeys(ctx); err != nil {
		return err
	}
	if _, err := s.repos.Dirwatches.AllDirwatches(ctx); err != nil {
		return err
	}
	if _, err := s.repos.Downstreams.AllDownstreams(ctx); err != nil {
		return err
	}
	if _, err := s.repos.Groups.AllGroups(ctx); err != nil {
		return err
	}
	if _, err := s.repos.Options.ReadOptions(ctx); err != nil {
		return err
	}
	if _, err := s.repos.Systems.AllSystems(ctx); err != nil {
		return err
	}
	if _, err := s.repos.Tags.AllTags(ctx); err != nil {
		return err
	}

	return nil
}

// shutdownTimeout bounds how long the server waits for what it was answering to stop
// answering. The wait is only for handlers that never return, so without a bound that
// is a server that stops stopping and never logs that a stop was asked for.
// see the stop-and-shutdown-ordering maintainer note.
const shutdownTimeout = 10 * time.Second

// stop ends what the server started running: the pruning timer, the armed holds, the
// directory watches, and the addresses it was answering on, then the database.
// Unlike the legacy Terminate's os.Exit, a second stop request waits for the first.
// see the stop-and-shutdown-ordering maintainer note.
func (s *Server) stop(ctx context.Context) {
	s.stopping.Do(func() {
		shuttingDown, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		if s.pruning != nil {
			s.pruning.Cancel()
		}

		if s.held != nil {
			s.held.Close()
		}

		if s.watches != nil {
			s.watches.Stop(shuttingDown)
		}

		// The address list is read under the lock that registers it: the stop
		// is often an interrupt arriving mid-registration, and an unlocked
		// read can miss an address and leave it answering after the stop.
		s.servingMutex.Lock()

		serving := make([]*http.Server, len(s.serving))
		copy(serving, s.serving)

		s.servingMutex.Unlock()

		for _, server := range serving {
			if err := server.Shutdown(shuttingDown); err != nil {
				_ = s.logs.AppendLog(ctx, domain.LogEntry{
					DateTime: s.clock.Now(),
					Level:    domain.LogLevelError,
					Source:   domain.LogSourceServer,
					Message:  fmt.Sprintf("the server could not stop answering %s: %v", server.Addr, err),
				})
			}
		}

		// Shutting an address down does not wait for handed-over connections,
		// so the listener and admin sockets are closed here: their departure
		// entries belong to the log in the database closed a few lines below.
		if s.adminSessions != nil {
			s.adminSessions.CloseAll(shuttingDown)
		}

		// An MCP session is a handed-over connection too, so the surface's
		// sessions end here, beside the sockets the same reasoning covers.
		if s.mcpSurface != nil {
			if err := s.mcpSurface.CloseSessions(shuttingDown); err != nil {
				_ = s.logs.AppendLog(ctx, domain.LogEntry{
					DateTime: s.clock.Now(),
					Level:    domain.LogLevelError,
					Source:   domain.LogSourceServer,
					Message:  fmt.Sprintf("the server could not close a Model Context Protocol session: %v", err),
				})
			}
		}

		if s.listeners != nil {
			s.listeners.Close(shuttingDown)
		}

		// The listener-count report is dropped last: the departures above each
		// schedule a count after the database closes. A stopping server owes
		// no count, and a write to a closed database never finishes.
		if s.listenerCounts != nil {
			s.listenerCounts.Cancel()
		}

		// The stop is the last entry the server can write to its own log view:
		// the log lives in the database closed next. The legacy server wrote
		// nothing, so the first question about any log gap went unanswered.
		_ = s.logs.AppendLog(ctx, domain.LogEntry{
			DateTime: s.clock.Now(),
			Level:    domain.LogLevelInfo,
			Source:   domain.LogSourceServer,
			Message:  "the server stopped",
		})

		if s.db != nil {
			if err := s.db.Close(); err != nil {
				// This failure line cannot be filed: the log view lives in the
				// database that just failed to close. The terminal is the only
				// copy left, the one place the root says an entry itself.
				s.console.println(renderLogEntry(domain.LogLevelError, domain.LogSourceDatabase,
					fmt.Sprintf("the server could not close its database: %v", err)))

				s.stopFailed = err
			}
		}

		// The deployment is let go last: the database it protects is already
		// closed, so the next rdio-scanner process to claim it finds the files
		// idle rather than mid-write.
		if s.dbLock != nil {
			if err := s.dbLock.Close(); err != nil {
				s.console.println(renderLogEntry(domain.LogLevelError, domain.LogSourceDatabase,
					fmt.Sprintf("the server could not release its claim on the deployment: %v", err)))

				s.stopFailed = err
			}

			s.dbLock = nil
		}
	})
}

// ServeHTTP answers a request with the routes the composition root hung the endpoints
// on. It is the handler the addresses the server answers on are built with, and
// anything with a listener of its own answers with the same routes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.routes.ServeHTTP(w, r)
}

// Close stops the server and closes the database it was opened with. A server
// assembled but never started has nothing running to stop, and the stop is said once,
// so an interrupted and then closed server reports itself stopped once.
// see the stop-and-shutdown-ordering maintainer note.
func (s *Server) Close() error {
	s.stop(context.Background())

	return s.stopFailed
}

// Serve answers the configured address, and the https address when the configuration
// names a certificate or asks for one obtained automatically. It returns only when the
// server has stopped, and an operator's interrupt stops the server.
// see the stop-and-shutdown-ordering maintainer note.
func (s *Server) Serve(ctx context.Context) error {
	s.waitForInterrupt()

	addresses := serverAddresses(s.config, hostname())

	s.announce(addresses)

	// Both the plain and the https listeners are built and registered with the server before
	// any goroutine runs, so the list the stop walks to shut the addresses down is complete
	// before the serving and TLS goroutines could start appending to it.
	// see the serving-list-race maintainer note.
	serving := s.httpServer(fmt.Sprintf("%s:%s", addresses.addr, addresses.port))

	if len(s.config.SslCertFile) > 0 && len(s.config.SslKeyFile) > 0 {
		go s.listenTLS(s.tlsServer(addresses, nil), addresses)

	} else if s.config.SslAutoCert != "" {
		manager := &autocert.Manager{
			Cache:      autocert.DirCache(s.config.GetAutocertCachePath()),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(s.config.SslAutoCert),
		}

		go s.listenTLS(s.tlsServer(addresses, manager.TLSConfig()), addresses)
	}

	if err := serving.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

// tlsServer builds the https listener, registered with the server the way every address
// it answers on is registered, before the goroutine that answers it starts.
// see the stop-and-shutdown-ordering maintainer note.
func (s *Server) tlsServer(addresses addresses, tlsConfig *tls.Config) *http.Server {
	server := s.httpServer(fmt.Sprintf("%s:%s", addresses.sslAddr, addresses.sslPort))

	server.TLSConfig = tlsConfig

	return server
}

// listenTLS answers the https address with a certificate the deployment provided or
// one the server obtains for itself. A listener that cannot start stops the server:
// a deployment that asked for https must not go on answering over http only.
// see the tls-choice maintainer note.
func (s *Server) listenTLS(serving *http.Server, addresses addresses) {
	s.announceTLS(addresses)

	var err error

	if len(s.config.SslCertFile) > 0 && len(s.config.SslKeyFile) > 0 {
		err = serving.ListenAndServeTLS(s.config.GetSslCertFilePath(), s.config.GetSslKeyFilePath())
	} else {
		err = serving.ListenAndServeTLS("", "")
	}

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		_ = s.logs.AppendLog(context.Background(), domain.LogEntry{
			DateTime: s.clock.Now(),
			Level:    domain.LogLevelError,
			Source:   domain.LogSourceServer,
			Message:  fmt.Sprintf("the server could not answer its https address %s: %v", serving.Addr, err),
		})

		s.stop(context.Background())

		return
	}
}

// httpServer builds one listener with the legacy server's timeouts and keep-alives, but
// not with the legacy server's discarding logger: this one hands the HTTP server the
// console the operator is reading, while what the client sees is unchanged.
// see the tls-choice maintainer note.
func (s *Server) httpServer(addr string) *http.Server {
	server := &http.Server{
		Addr:         addr,
		ErrorLog:     log.New(s.console, "", log.LstdFlags),
		Handler:      s.routes,
		ReadTimeout:  httpTimeout,
		WriteTimeout: httpTimeout,
	}

	server.SetKeepAlivesEnabled(true)

	s.servingMutex.Lock()
	s.serving = append(s.serving, server)
	s.servingMutex.Unlock()

	return server
}

// waitForInterrupt stops the server when the operator interrupts it, the legacy
// SIGINT handling. The first interrupt asks for the polite stop; a second ends the
// process, and it stays watched while the stop runs, because a stop can hang.
// see the stop-and-shutdown-ordering maintainer note.
func (s *Server) waitForInterrupt() {
	signals := make(chan os.Signal, 8)

	signal.Notify(signals, os.Interrupt)

	go func() {
		<-signals

		stopped := make(chan struct{})

		go func() {
			s.stop(context.Background())

			close(stopped)
		}()

		select {
		case <-stopped:
			// The stop finished on its own: the serving path returns the
			// process to its caller, and the watcher has nothing to watch.
			return

		case <-signals:
			fmt.Fprintln(s.console, "warning server: the server cannot stop, ending the process on the second interrupt")

			os.Exit(1)
		}
	}()
}

// addresses is the pair of addresses the configuration names, split the way the legacy
// server splits them.
type addresses struct {
	hostname string

	addr    string
	port    string
	sslAddr string
	sslPort string
}

// serverAddresses splits the listening addresses the configuration names.
// The legacy server's defaults are kept: a name with no port gets 3000, an empty
// address means every interface, and the https address falls back to 3000 not 443.
// The https default is kept because moving it would take its clients with it.
func serverAddresses(config *Config, hostname string) addresses {
	addr, port := splitListenAddress(config.Listen)
	sslAddr, sslPort := splitListenAddress(config.SslListen)

	return addresses{
		addr:     addr,
		hostname: hostname,
		port:     port,
		sslAddr:  sslAddr,
		sslPort:  sslPort,
	}
}

func splitListenAddress(listen string) (string, string) {
	addr, port := listen, "3000"

	if parts := strings.Split(listen, ":"); len(parts) > 1 {
		addr, port = parts[0], parts[1]
	}

	if len(addr) == 0 {
		addr = everyInterface
	}

	return addr, port
}

// announce states the addresses the server is listening on, in the order and the wording
// the legacy server uses.
// see the legacy-announcement-wording maintainer note.
func (s *Server) announce(addresses addresses) {
	s.console.printf("main interface at %s", httpInterface(addresses.hostname, addresses.port))

	if !s.tlsConfigured() {
		s.console.printf("admin interface at %s/admin", httpInterface(addresses.hostname, addresses.port))
	}

	s.console.println("please consider sponsoring the project at https://github.com/sponsors/chuot")
}

// announceTLS states the https half of the listening announcement, which the legacy
// server states from the goroutine that serves it.
// see the legacy-announcement-wording maintainer note.
func (s *Server) announceTLS(addresses addresses) {
	base := fmt.Sprintf("https://%s:%s", addresses.hostname, addresses.sslPort)
	if addresses.sslPort == "443" {
		base = fmt.Sprintf("https://%s", addresses.hostname)
	}

	s.console.printf("main interface at %s", base)
	s.console.printf("admin interface at %s/admin", base)
}

// httpInterface names an address the way the legacy server names one: a server on the
// port a browser assumes is not given the port.
// see the legacy-announcement-wording maintainer note.
func httpInterface(hostname, port string) string {
	if port == "80" {
		return fmt.Sprintf("http://%s", hostname)
	}

	return fmt.Sprintf("http://%s:%s", hostname, port)
}

func (s *Server) tlsConfigured() bool {
	return (len(s.config.SslCertFile) > 0 && len(s.config.SslKeyFile) > 0) || s.config.SslAutoCert != ""
}

// dialectOf names the database this server talks to. The configuration has already
// rejected a type it does not support; this turns that decision into the thing the
// repositories are built with.
// see the legacy-configuration-relocation maintainer note.
func dialectOf(dbType string) (persistence.Dialect, error) {
	switch dbType {
	case DbTypeSqlite:
		return persistence.DialectSqlite, nil
	case DbTypeMariadb:
		return persistence.DialectMariadb, nil
	case DbTypeMysql:
		return persistence.DialectMysql, nil
	case DbTypePostgresql:
		return persistence.DialectPostgresql, nil
	default:
		return "", fmt.Errorf("unknown database type %s", dbType)
	}
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return everyInterface
	}

	return name
}
