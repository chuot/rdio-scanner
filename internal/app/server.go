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

package app

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"log"
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

// everyInterface is the address the server binds when the configuration names an address with nothing in
// it, which is the legacy server's default and means every interface on the machine.
const everyInterface = "0.0.0.0"

// httpTimeout bounds how long one request may take in either direction. It is the interval the legacy
// server gives its HTTP servers, and it is long enough that an upload of a recorded call is not cut off
// by it.
const httpTimeout = 30 * time.Second

// Server is the assembled server: the adapters, the use cases, and the endpoints the composition root put
// together, and the thing that starts them, answers with them, and stops them.
//
// The fields are the pieces the composition root itself reaches for after assembly — what it reads at
// startup, what it has to stop again, and what a test looks at. Everything else is held by the use case it
// was built into, and the composition root never touches it again.
type Server struct {
	config  *Config
	console *consoleLog

	db      *sql.DB
	dialect persistence.Dialect

	// repos are the eight configuration sections and the recorded calls, as the ports the use cases
	// are given. The composition root reads them at startup, in the order the legacy server reads them.
	repos application.ConfigurationRepositories

	// options and accesses are kept as the adapters the composition root itself talks to: the options
	// because it supplies the credential a server starts with, the accesses because a test asks what
	// the server requires of listeners after it changes them.
	options  *persistence.Options
	accesses *persistence.Accesses

	// signing is the startup step that gives the server a signing secret it can sign sessions with and
	// puts it on record, so the key outlives the process that made it.
	signing *application.EnsureAdminSigningSecretHandler

	// logs is the log the whole server writes into, including the server's own notices.
	logs ports.LogRepository

	// converterNotice is what the operator has to be told about the ffmpeg this server converts with,
	// and is empty when there is nothing to say. It is decided once while the server is wired, and
	// said once when it starts.
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

	// collectorDone is closed when the loop carrying held calls back into the pipeline has
	// returned: the delayer closing its stream is what ends that loop, and a server that
	// has stopped does not leave a collector behind.
	collectorDone chan struct{}

	// ingest is the path every recorded call goes through, kept because the calls whose hold has run
	// out are delivered through the same path rather than through a copy of it.
	ingest *application.IngestCallHandler

	// serving holds the addresses this server is answering on, so that stopping the server stops them.
	//
	// The list is written by the goroutine that is serving and read by the goroutine that is
	// stopping — the operator's interrupt can ask for the stop while the addresses are still
	// being registered — so the list is carried under servingMutex. Building every address
	// before any goroutine runs keeps the registrations themselves in
	// one goroutine; the mutex is what makes the stop see a complete list, never a
	// half-built one.
	serving      []*http.Server
	servingMutex sync.Mutex

	// listeners, adminSessions, and listenerCounts are the connections the server has taken over and the
	// report it owes about how many of the first kind there are. Shutting an address down does not wait for
	// a connection the server has handed over, so stopping the server has to end these itself — and end them
	// while the log their departures are written into can still be written to.
	listeners      *scannerbackend.Hub
	adminSessions  *adminbackend.Sessions
	listenerCounts *application.ListenerCountDebouncer

	// mcpSurface is the Model Context Protocol endpoint, built only when the configuration names it. An
	// assistant holding a session is a connection the server took over and has to end itself, the same
	// reason the listener and the administrator's configuration sockets are ended here.
	mcpSurface *mcpbackend.Server

	routes http.Handler

	// stopping guards the stop, so that the two goroutines that can ask for it — the one waiting for the
	// operator's interrupt and the one that was serving — do not both do it, and the second one waits for
	// the first to finish.
	stopping sync.Once

	// trusted is the parsed TrustedProxies configuration: the proxy list every endpoint that names an
	// address honors, so the address an operator reads and the address the limiter counts are one address.
	trusted *httpaddr.Trusted

	// stopFailed is the failure the stop could not file: the database that would have held the report is
	// the database that failed to close.
	stopFailed error
}

// New opens the database the configuration names, brings it to the schema this server reads, seeds
// what a fresh deployment starts with, and wires everything to everything else.
//
// It starts nothing: opening a database and assembling a server should not begin watching directories or
// pruning history, because a caller that finds the assembly unusable has nothing to undo.
//
// The configuration says where the server writes the lines it says out loud, whose copy of the log entries
// an operator reads when the server has no terminal in front of it, which ffmpeg the server converts with,
// and the version the server reports.
func New(config Config) (*Server, error) {
	ctx := context.Background()

	srv := &Server{config: &config, console: newConsoleLog(config.Out)}

	// The proxy list is parsed before anything opens, because a list the operator wrote that is not a
	// list of addresses is a configuration error the server refuses at startup, not one it discovers
	// while deciding who to trust mid-request.
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
		return nil, err
	}

	srv.db = db

	// The clock is created before the migration because the migration stamps the rows it archives with
	// the server's clock, the way every writer this server owns stamps its entries.
	srv.clock = clock.NewClock()

	// The migration and the seed are the legacy server's NewDatabase: the schema first, then the groups
	// and tags a deployment that has never been started is expected to have. Both do nothing on a
	// database that already has them, which is what makes an upgrade in place the ordinary case.
	//
	// What the pass says is printed as it happens — that is what an operator watching an upgrade sees —
	// and kept, because it cannot be written to the log view yet: the pass is rebuilding that table, and
	// an entry written now takes an id in the very range the deployment's own history is being moved into.
	// wire writes them once there is a log repository to write them through.
	migrationLines := []domain.LogEntry{}

	if err := persistence.Migrate(ctx, db, dialect, srv.clock, func(level, message string) {
		// Said out loud in the same shape every other line the server says has, so that the word an
		// operator greps for finds this line too. It is not the echo of an entry — there is no entry yet —
		// which is why the level and the subsystem are put on it here.
		srv.console.println(renderLogEntry(level, domain.LogSourceDatabase, message))

		migrationLines = append(migrationLines, domain.LogEntry{Level: level, Message: message})
	}); err != nil {
		return nil, err
	}

	if err := persistence.Seed(ctx, db, dialect); err != nil {
		return nil, err
	}

	if err := srv.wire(migrationLines); err != nil {
		return nil, err
	}

	return srv, nil
}

// wire builds the adapters, hands each one to the use cases that need it, and hangs the endpoints on the
// paths the legacy server answers.
//
// The order is the order the pieces need each other in: the repositories before the adapters that read
// them, the hub before the use cases that report through it, and the use cases before the endpoints that
// answer with them.
//
// migrationLines are what the migration pass said while it was rebuilding the log table. They are filed
// here, at the first moment there is a log repository to file them in, and before anything else the server
// writes.
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

	// s.clock was created in newServer, before the migration, because the migration stamps the rows it
	// archives with the server's clock.
	// Scheduled work that panics is recovered by the scheduler and said here
	//: a debounced listener report, an hourly pruning, or a delayed
	// call that falls over is a failure the operator sees in the log view, not
	// a server that dies on a goroutine nobody owns.
	s.scheduler = clock.NewScheduler(func(problem any) {
		_ = logs.AppendLog(ctx, domain.LogEntry{
			DateTime: s.clock.Now(),
			Level:    domain.LogLevelWarn,
			Source:   domain.LogSourceServer,
			Message:  fmt.Sprintf("scheduled work panicked: %v", problem),
		})
	})

	// The clamp lines: a stored value the read cannot hold — a negative
	// expiration, limit, or order — is clamped to the safe reading, and the row and
	// its values are filed as a problem in the log view, naming what the server
	// enforces instead. A deployment's policy changing in silence is the failure
	// this ends.
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

	credentials := passwords.NewBcrypt(s.config.BcryptCost)
	s.hashes = credentials

	// The migration said these things while the log table was still being rebuilt, which is why they were
	// kept rather than written: an entry written during the pass takes an id in the log table, and the pass
	// is busy moving the deployment's own history into that table with the ids that history arrived with.
	// They are filed now, in the order they were said, through the plain repository and not the echoing one
	// — the terminal already heard every one of them as it happened, and saying them again would be two
	// lines for one event.
	for _, entry := range migrationLines {
		entry.DateTime = s.clock.Now()
		entry.Source = domain.LogSourceDatabase

		_ = logs.AppendLog(ctx, entry)
	}

	// Every log entry the server writes is also said out loud, which is what the legacy server's
	// LogEvent does and what an operator running the server in a terminal expects. A server running as a
	// service says them to the manager instead, at the level each entry is filed under.
	echo := s.config.Echo

	if echo == nil {
		echo = s.console
	}

	s.logs = &loggedEvents{logs: logs, echo: echo}

	// The ffmpeg this server converts with is the one compiled for the architecture this binary was
	// built for, written out of the binary into the server's own folder. A server whose target carries
	// none — macOS, FreeBSD, and a developer's plain go build — converts with the ffmpeg its host
	// installs instead, which is what those deployments have always done. The ffprobe that measures
	// the lengths of the recordings that conversion did not report travels the same decision: the
	// pair the server carries, or the pair its host installs.
	converter, prober, notice := s.config.Converter(s.config.GetFFmpegDirPath())

	s.converterNotice = notice
	forwarder := downstream.NewForwarder(groups, tags)
	reader := parsers.New(systems, s.clock)

	// The calls that are being held back are held here: written down with the instant they stop being
	// held back, so that a server that stops and starts again picks them up. What the delayer hands
	// back is delivered by the same step an ingested call ends with, which is the wiring the legacy
	// server gets from its one ingest queue and this server has to do on purpose.
	held := delayer.New(delayer.Deps{
		Delayed:   delayed,
		Calls:     calls,
		Scheduler: s.scheduler,
		Clock:     s.clock,
		Logs:      s.logs,
	})

	s.held = held

	// The credential a server accepts when its stored configuration has none is made at every startup and
	// never written down, which is what the legacy server does — and why a session started before a
	// restart does not survive it. It has to be supplied before the first read, because the read that
	// follows is the one that puts it in place.
	initial, err := credentials.Hash(ctx, domain.DefaultAdminPassword)
	if err != nil {
		return err
	}

	options.StoreInitialCredential(initial)

	// The startup step that makes sure the server has a signing secret worth having, and the one that puts
	// it on record. It is built here because it needs the options adapter, and it is run by start after the
	// configuration has been read: the options the step decides about are the ones the server is running
	// with, and a step that read them before the start sequence had loaded them would decide about nothing.
	s.signing = application.NewEnsureAdminSigningSecretHandler(options, options, s.logs, s.clock)

	s.settings = &listenerSettings{accesses: accesses, options: options}

	changes := &listenerChanges{}

	hub := scannerbackend.NewHub(s.settings, changes, s.logs, s.clock)

	debouncer := application.NewListenerCountDebouncer(hub, s.logs, options, s.scheduler, s.clock)

	// The hub counts listeners and says when the count is no longer what it was; the debouncer decides
	// when to report it, so that a client that connects and drops repeatedly costs one report and not
	// one per connection.
	changes.debouncer = debouncer

	// The composition root keeps what it has to end: the hub's connections, the administrator's connections,
	// and a listener-count report that is owed but not yet written. None of them are things shutting an
	// address down reaches, and all of them are things the log stops holding once the stop closes the
	// database it lives in.
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
		Docker:               os.Getenv("DOCKER") != "",
		Logout:               application.NewAdminLogoutHandler(tokensAdapter, s.logs, s.clock),
		Logs:                 s.logs,
		Clock:                s.clock,
		SearchLogs:           application.NewSearchLogsHandler(s.logs, s.clock),
		Login:                application.NewAdminLoginHandler(options, s.logs, tokensAdapter, credentials, credentials, credentials, s.clock),
		Password:             application.NewChangeAdminPasswordHandler(options, credentials, credentials, s.logs, s.clock),
		RemoveUser:           application.NewRemoveAccessUserHandler(accessUserPorts),
		Session:              application.NewCheckAdminSessionHandler(tokensAdapter),
		Version:              s.config.Version,
		Trusted:              s.trusted,
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

	// The access-code resolver is built once for the whole listener. Its guess budget belongs to
	// the address that spends it, not to the entry point the address arrives at: an address that
	// guesses at the call history and then at the Model Context Protocol surface is one address
	// guessing, and it spends one budget.
	resolveAccess := application.NewResolveAccessQueryHandler(accesses, s.logs, s.clock)

	// The Model Context Protocol surface is built only when the configuration names it. The use cases it
	// answers with are the same ones the administrator's own page and the listener socket answer with:
	// the surface adds no use case of its own, and no path from it reaches a socket.
	if s.config.McpServer {
		s.mcpSurface = mcpbackend.New(mcpbackend.Deps{
			Version: s.config.Version,
			Docker:  os.Getenv("DOCKER") != "",
			Session: application.NewCheckAdminSessionHandler(tokensAdapter),
			Resolve: resolveAccess,
			Trusted: s.trusted,
			Config:  application.NewGetConfigHandler(s.repos),
			Logs:    application.NewSearchLogsHandler(s.logs, s.clock),
			Call:    application.NewGetCallQueryHandler(calls),
			Search:  application.NewSearchCallsQueryHandler(calls, systems, groups, tags),
			Clock:   s.clock,
		})
	}

	mux := http.NewServeMux()

	adminAPI.Register(mux)
	uploads.Register(mux)

	// The call history surface is registered unconditionally. It is the GPL HTTP half of the
	// recorded-call capability — the side the access policy leaves unrestricted — and it is what
	// the Model Context Protocol surface is built on. A deployment that never enabled the MCP
	// surface still answers its call history over REST, the way it answers every other REST route.
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

	// The Model Context Protocol surface is hung on the mux only when the operator named it. A
	// deployment that never asked for it never learns the path exists: the unmatched-path answer an
	// assistant gets from it is the answer every unmatched path gets.
	if s.mcpSurface != nil {
		s.mcpSurface.Register(mux)
	}

	serveApplications(mux, listener, scannerfrontend.Embedded(), adminfrontend.Embedded())

	// Every request the server answers carries thedeadline: the
	// administrator's request is answered with a failure by 25 seconds, not
	// held open by a database that does not answer.
	s.routes = withRequestDeadline(mux, requestDeadline)

	return nil
}

// Start runs the startup the legacy server runs: the notice that the server is up, the base folder it was
// given, the configuration read out of the database, the calls this server was holding when it last
// stopped, the pruning timer, and the directory watches.
//
// Three parts of the legacy startup are not here because they are not things this server has. The ingest
// consumer goroutine is gone: a call is finished before the thing that found it is answered, which is
// The register-and-forget loop is gone: the hub holds its own
// sessions, and the listener-count debouncer is the thing the hub tells when the count of listeners
// changes. The administrator's socket has nothing to start: it starts when an administrator connects.
func (s *Server) Start(ctx context.Context) error {
	// The legacy server ignores a startup notice it cannot store, and the line has already reached the
	// console by the time the log view is asked for it.
	_ = s.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: s.clock.Now(),
		Level:    domain.LogLevelWarn,
		Source:   domain.LogSourceServer,
		Message:  "server started",
	})

	if len(s.config.BaseDir) > 0 {
		// The folder the server was told to use is the fact behind every path it writes to, and until now
		// it existed only on a terminal, at the moment the server came up. It is filed as a notice and not
		// a warning: nothing is wrong, this is simply which folder this server is serving.
		_ = s.logs.AppendLog(ctx, domain.LogEntry{
			DateTime: s.clock.Now(),
			Level:    domain.LogLevelInfo,
			Source:   domain.LogSourceServer,
			Message:  "the server is serving " + s.config.BaseDir,
		})
	}

	// Which ffmpeg the server converts with is decided while it is wired and said here, once, so that
	// an operator whose server could not use the ffmpeg it carries learns that at startup rather than
	// from a recording that was never converted.
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

	// The signing secret is the startup rule the legacy server never finished: it made a fresh random key
	// every time the stored one was unusable and never wrote it down, so a deployment that had never saved
	// its configuration signed sessions with nothing, and a legacy database holding a short secret changed
	// keys at every start and killed every session issued before it. This step makes a key once and puts it
	// on record, after the configuration has been read and before anything is answered from it.
	// The secret is the server's own, and the step reports to the server log what it decided about it:
	// the composition root says that entry out loud the way it says every other one.
	if _, err := s.signing.Handle(ctx, application.EnsureAdminSigningSecretCommand{}); err != nil {
		return err
	}

	// The calls this server was holding when it last stopped are picked up here, which is where the
	// legacy server picks them up: before anything is answered, so that a hold that ran out while the
	// server was down is delivered now and a hold still running is armed again.
	//
	// The loop that carries them into the pipeline starts before they are read back, so that the ones
	// handed over at once have somewhere to go.
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

// deliverHeldCalls carries the calls whose hold has run out back into the pipeline: the same delivery
// step an ingested call ends with, so that a call that waited out its hold is told to the other servers
// and to the listeners by the one path every other call takes, and not by a copy of it written for
// delayed calls.
//
// It runs for as long as the server does. A hold that runs out after the server has stopped is not
// delivered by this loop, and the call is still where it was written down, which is how the next
// startup finds it.
//
// The delayer closing its stream is the loop's own ending: the stream closes with the server's
// stop, and the loop leaves with it.
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

// readConfiguration reads the stored configuration in the order the legacy server reads it, and stops at
// the first section it cannot read.
//
// The order is not cosmetic: a deployment whose systems table is unreadable is told about the accesses it
// could not read first, which is what an operator comparing two startup logs is looking at. And the reads
// happen before the server answers anything, so a server that cannot load its configuration does not spend
// the interval answering requests with a configuration it never loaded.
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

// shutdownTimeout bounds how long the server waits for what it was answering to stop answering.
//
// The legacy server's Terminate called os.Exit, which stops everything at once. This server shuts its
// addresses down instead, and a graceful shutdown waits for the connections that are still in use. Most of
// them it closes on the spot — a client that connected and has sent nothing is closed, not waited for — so
// the wait is for the connection the server cannot let go of: one whose handler never returns. Without a
// bound, that is a server that stops stopping, and because the entry saying the server stopped is written
// after the addresses are shut down, the log never records that a stop was even asked for. With one, the
// server stops waiting, says which address it could not stop answering and why, and ends.
//
// The bound covers the waiting, not the closing: a listener whose accept loop never ends blocks the
// shutdown whatever deadline it is given, because the shutdown waits for that loop before it looks at the
// clock. That is why the failure entry names the address rather than the timeout.
const shutdownTimeout = 10 * time.Second

// stop ends what the server started running — the pruning timer, the holds armed for calls that are
// waiting out a delay, the directory watches, and the addresses it was answering on — and then the
// database it was given.
//
// The legacy server's Terminate calls os.Exit, which is how a blocked ListenAndServe is stopped. This one
// shuts the addresses down instead, so the goroutine that was serving returns and the process ends by
// running out of work. That means two goroutines can ask for the stop at the same moment — the one that
// was waiting for the operator's interrupt and the one that was serving — and the second one waits for the
// first to finish rather than doing it again. Without that, the process would end while the first stop was
// still saying so.
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

		// The list of addresses is read under the lock that registers them: the stop is
		// often the operator's interrupt asking for it while the addresses are still being
		// registered, and a stop that reads the list unlocked can miss an address that is
		// about to be registered and leave it answering after the server has stopped.
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

		// Shutting an address down does not wait for a connection the server took over from it: an upgraded
		// connection is one the address no longer tracks. So the listener connections and the administrator's
		// configuration sockets are still open here, and the entries their departures are belong to a log that
		// lives in the database this method closes a few lines from now. Ending them here, and saying so here,
		// is what keeps the last entries a running server writes from being entries it can no longer write:
		// otherwise the log view holds a connection that began and never ended, which is the shape an operator
		// spends an afternoon chasing.
		if s.adminSessions != nil {
			s.adminSessions.CloseAll(shuttingDown)
		}

		// An assistant holding a Model Context Protocol session is a connection this server took over,
		// and shutting an address down does not wait for a connection it handed over. So the surface's
		// sessions are ended here, beside the sockets the same reasoning applies to.
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

		// The listener-count report is dropped last, because the departures above are themselves listener
		// changes: each one asks for a count of listeners that no longer exist, scheduled to be written after
		// this method has closed the database the log lives in. A server that is stopping does not owe a
		// count — the connections it is ending are each reported by their own departure — and a write that
		// reaches a database which has just been closed is a write that does not finish.
		if s.listenerCounts != nil {
			s.listenerCounts.Cancel()
		}

		// The stop is the last entry the server can write to its own log view: the log view lives in the
		// database this method closes next. The legacy server said `terminated` on a terminal and wrote
		// nothing, so a log that recorded everything else could not say when the server had stopped —
		// which is the first question about any gap in it.
		_ = s.logs.AppendLog(ctx, domain.LogEntry{
			DateTime: s.clock.Now(),
			Level:    domain.LogLevelInfo,
			Source:   domain.LogSourceServer,
			Message:  "the server stopped",
		})

		if s.db != nil {
			if err := s.db.Close(); err != nil {
				// This line cannot be filed: the log view is stored in the database that just failed to
				// close. The terminal is the only copy left, so this is the one place the composition
				// root says an entry itself rather than leaving the saying to the log.
				s.console.println(renderLogEntry(domain.LogLevelError, domain.LogSourceDatabase,
					fmt.Sprintf("the server could not close its database: %v", err)))

				s.stopFailed = err
			}
		}
	})
}

// ServeHTTP answers a request with the routes the composition root hung the endpoints on. It is the
// handler the addresses the server answers on are built with, and anything that has a listener of its own
// answers with the same routes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.routes.ServeHTTP(w, r)
}

// Close stops the server and closes the database it was opened with. A server that was assembled and never
// started has nothing running to stop, so closing it is closing its database. The stop is said once, so a
// server that was interrupted and then closed reports itself stopped once.
func (s *Server) Close() error {
	s.stop(context.Background())

	return s.stopFailed
}

// Serve answers the configured address, and the https address when the configuration names a certificate
// or asks for one obtained automatically. It does not return until the server stops, and it stops the
// server when the operator interrupts it.
func (s *Server) Serve(ctx context.Context) error {
	s.waitForInterrupt()

	addresses := serverAddresses(s.config, hostname())

	s.announce(addresses)

	// Both addresses are built and registered with the server before any goroutine runs
	//: the list the stop walks to shut the addresses down was being
	// appended by the serving goroutine and the TLS goroutine at the same moment — a data
	// race, and a stop racing with the registration could shut down a list that is missing
	// the TLS address.
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

// tlsServer builds the https listener, registered with the server the way every address it
// answers on is registered, before the goroutine that answers it starts.
func (s *Server) tlsServer(addresses addresses, tlsConfig *tls.Config) *http.Server {
	server := s.httpServer(fmt.Sprintf("%s:%s", addresses.sslAddr, addresses.sslPort))

	server.TLSConfig = tlsConfig

	return server
}

// listenTLS answers the https address, in the two ways the legacy server answers it: with a certificate the
// deployment provided, or with one the server obtains for itself.
//
// A listener that cannot start stops the server: a deployment that asked for https and
// cannot answer it is not a server that should carry on with only http. The legacy server made this fatal
// — log.Fatal killed the process with a line on the terminal, nothing in the server's log, and the plain
// address left answering until the process died. The failure is filed in the server's log, said on the
// terminal with it, and the server stops the way it stops for its operator's interrupt.
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

// httpServer builds one listener.
//
// The timeouts are the legacy server's, and so is keeping keep-alives on. What is not the legacy server's
// is the logger: the legacy server hands the HTTP server a logger that throws everything away, which is a
// preserved quirk — a recovered panic and its stack, a TLS handshake failure, a broken write, none of it reaching
// anybody. This server hands it the console the operator is reading. What the client sees is unchanged.
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

// waitForInterrupt stops the server when the operator interrupts it, which is the legacy server's SIGINT
// handling.
//
// The first interrupt asks for the stop, and the stop is the polite way out. A second interrupt is the
// operator saying the polite way is not working: the server says so on the terminal the
// operator is reading and ends the process, rather than hanging until the SIGKILL that leaves no trace.
// The second interrupt is watched for while the stop runs, because a stop that cannot finish is exactly
// the one that never returns to ask.
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
			// The stop finished on its own: the serving path returns the process to its
			// caller, and the watcher has nothing left to watch.
			return

		case <-signals:
			fmt.Fprintln(s.console, "warning server: the server cannot stop, ending the process on the second interrupt")

			os.Exit(1)
		}
	}()
}

// addresses is the pair of addresses the configuration names, split the way the legacy server splits them.
type addresses struct {
	hostname string

	addr    string
	port    string
	sslAddr string
	sslPort string
}

// serverAddresses splits the listening addresses the configuration names.
//
// The split is the legacy server's and so are its defaults: a name with no port gets 3000, an address with
// nothing in it means every interface, and the https address falls back to 3000 rather than to 443. That
// last one is not a mistake this server corrects — a deployment that configured ssl_listen without a port
// has always been answering on 3000, and moving it would take its clients with it.
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

// announce says what the server is listening on, in the order and the wording the legacy server uses.
func (s *Server) announce(addresses addresses) {
	s.console.printf("main interface at %s", httpInterface(addresses.hostname, addresses.port))

	if !s.tlsConfigured() {
		s.console.printf("admin interface at %s/admin", httpInterface(addresses.hostname, addresses.port))
	}

	s.console.println("please consider sponsoring the project at https://github.com/sponsors/chuot")
}

// announceTLS says the https half of the announcement, which the legacy server says from the goroutine
// that serves it.
func (s *Server) announceTLS(addresses addresses) {
	base := fmt.Sprintf("https://%s:%s", addresses.hostname, addresses.sslPort)
	if addresses.sslPort == "443" {
		base = fmt.Sprintf("https://%s", addresses.hostname)
	}

	s.console.printf("main interface at %s", base)
	s.console.printf("admin interface at %s/admin", base)
}

// httpInterface names an address the way the legacy server names one: a server on the port a browser
// assumes is not given the port.
func httpInterface(hostname, port string) string {
	if port == "80" {
		return fmt.Sprintf("http://%s", hostname)
	}

	return fmt.Sprintf("http://%s:%s", hostname, port)
}

func (s *Server) tlsConfigured() bool {
	return (len(s.config.SslCertFile) > 0 && len(s.config.SslKeyFile) > 0) || s.config.SslAutoCert != ""
}

// dialectOf names the database this server talks to. The configuration has already rejected a type it does
// not support; this is where that decision becomes the thing the repositories are built with.
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
