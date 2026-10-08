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

package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"io"
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
	"rdio-scanner/internal/presentation/adminws"
	"rdio-scanner/internal/presentation/rest"
	"rdio-scanner/internal/presentation/serving/admin"
	"rdio-scanner/internal/presentation/serving/scanner"
	"rdio-scanner/internal/presentation/ws"
)

// everyInterface is the address the server binds when the configuration names an address with nothing in
// it, which is the legacy server's default and means every interface on the machine.
const everyInterface = "0.0.0.0"

// httpTimeout bounds how long one request may take in either direction. It is the interval the legacy
// server gives its HTTP servers, and it is long enough that an upload of a recorded call is not cut off
// by it.
const httpTimeout = 30 * time.Second

// server is the assembled server: the adapters, the use cases, and the endpoints the composition root put
// together, and the thing that starts them, answers with them, and stops them.
//
// The fields are the pieces the composition root itself reaches for after assembly — what it reads at
// startup, what it has to stop again, and what a test looks at. Everything else is held by the use case it
// was built into, and the composition root never touches it again.
type server struct {
	config  *Config
	console *consoleLog

	// service is the service manager's arrangement this server is running under, or nil for a server an
	// operator started in a terminal. It decides whose copy of the log entries the operator reads.
	service *daemon

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

	// ingest is the path every recorded call goes through, kept because the calls whose hold has run
	// out are delivered through the same path rather than through a copy of it.
	ingest *application.IngestCallHandler

	// serving holds the addresses this server is answering on, so that stopping the server stops them.
	serving []*http.Server

	routes http.Handler

	// stopping guards the stop, so that the two goroutines that can ask for it — the one waiting for the
	// operator's interrupt and the one that was serving — do not both do it, and the second one waits for
	// the first to finish.
	stopping sync.Once
}

// newServer opens the database the configuration names, brings it to the schema this server reads, seeds
// what a fresh deployment starts with, and wires everything to everything else.
//
// It starts nothing: opening a database and assembling a server should not begin watching directories or
// pruning history, because a caller that finds the assembly unusable has nothing to undo.
//
// out is where the server writes the lines it says out loud. The legacy server's console is Go's log
// package, which writes to the standard error, so that is what the composition root hands it.
//
// as is the service this server is running under, or nil for a server an operator started. It changes one
// thing: whose copy of the server's log entries the operator reads. The legacy server keeps the same
// distinction as a daemon field on its log, and asks the same question.
func newServer(config *Config, out io.Writer, as *daemon) (*server, error) {
	ctx := context.Background()

	srv := &server{config: config, console: newConsoleLog(out), service: as}

	dialect, err := dialectOf(config.DbType)
	if err != nil {
		return nil, err
	}

	srv.dialect = dialect

	db, err := persistence.Open(persistence.Settings{
		Dialect:  dialect,
		FilePath: config.GetDbFilePath(),
		Host:     config.DbHost,
		Port:     config.DbPort,
		Username: config.DbUsername,
		Password: config.DbPassword,
		Name:     config.DbName,
	})
	if err != nil {
		return nil, err
	}

	srv.db = db

	// The migration and the seed are the legacy server's NewDatabase: the schema first, then the groups
	// and tags a deployment that has never been started is expected to have. Both do nothing on a
	// database that already has them, which is what makes an upgrade in place the ordinary case.
	if err := persistence.Migrate(ctx, db, dialect, func(message string) {
		srv.console.println(message)
	}); err != nil {
		return nil, err
	}

	if err := persistence.Seed(ctx, db, dialect); err != nil {
		return nil, err
	}

	if err := srv.wire(); err != nil {
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
func (s *server) wire() error {
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

	s.clock = clock.NewClock()
	s.scheduler = clock.NewScheduler()

	credentials := passwords.NewBcrypt()
	s.hashes = credentials

	// Every log entry the server writes is also said out loud, which is what the legacy server's
	// LogEvent does and what an operator running the server in a terminal expects. A server running as a
	// service says them to the manager instead, at the level each entry is filed under.
	var echo logEcho = s.console

	if s.service != nil {
		echo = serviceLog{logger: s.service.logger}
	}

	s.logs = &loggedEvents{logs: logs, echo: echo}

	// The ffmpeg this server converts with is the one compiled for the architecture this binary was
	// built for, written out of the binary into the server's own folder. A server whose target carries
	// none — macOS, FreeBSD, and a developer's plain go build — converts with the ffmpeg its host
	// installs instead, which is what those deployments have always done.
	bundled, failure := bundledFFmpeg(s.config)

	choice := theConverter(bundled, failure)

	s.converterNotice = choice.Notice

	converter := choice.converter()
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
	s.signing = application.NewEnsureAdminSigningSecretHandler(options, options)

	s.settings = &listenerSettings{accesses: accesses, options: options}

	changes := &listenerChanges{}

	hub := ws.NewHub(s.settings, changes, s.logs, s.console)

	debouncer := application.NewListenerCountDebouncer(hub, s.logs, options, s.scheduler, s.clock)

	// The hub counts listeners and says when the count is no longer what it was; the debouncer decides
	// when to report it, so that a client that connects and drops repeatedly costs one report and not
	// one per connection.
	changes.debouncer = debouncer

	tokensAdapter := tokens.New(optionsSecrets{options: options})

	ingest := application.NewIngestCallHandler(
		calls, systems, groups, tags, options, s.logs, converter, hub, held, downstreams, forwarder, s.clock,
	)

	s.ingest = ingest

	watches := dirwatch.NewDirwatchers(reader, s.logs, &ingestSink{ingest: ingest}, s.clock)
	s.watches = watches

	s.prune = application.NewPruneOldCallsHandler(calls, s.logs, options, s.clock)

	adminSessions := adminws.NewSessions(adminws.Deps{
		Session: application.NewCheckAdminSessionHandler(tokensAdapter),
		Version: Version,
	})

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

	adminAPI := rest.NewAdmin(rest.Deps{
		AddUser:              application.NewAddAccessUserHandler(accessUserPorts),
		Config:               application.NewGetConfigHandler(s.repos),
		ConfigSocket:         adminSessions,
		Configuration:        application.NewUpdateConfigurationHandler(configurationPorts),
		ConfigurationSection: application.NewUpdateConfigurationSectionHandler(configurationPorts),
		Docker:               os.Getenv("DOCKER") != "",
		Logout:               application.NewAdminLogoutHandler(tokensAdapter),
		Logs:                 application.NewSearchLogsHandler(s.logs, s.clock),
		Login:                application.NewAdminLoginHandler(options, s.logs, tokensAdapter, credentials, s.clock),
		Password:             application.NewChangeAdminPasswordHandler(options, credentials, credentials, s.logs, s.clock),
		RemoveUser:           application.NewRemoveAccessUserHandler(accessUserPorts),
		Session:              application.NewCheckAdminSessionHandler(tokensAdapter),
		Version:              Version,
	})

	upload := rest.NewUpload(rest.UploadDeps{
		Ingest: application.NewUploadCallHandler(apikeys, ingest),
		Logs:   s.logs,
		Parser: reader,
	})

	listener := ws.NewHandler(ws.Deps{
		Access:   application.NewAuthenticateAccessHandler(accesses, systems, groups, tags, options, s.logs, hub, s.clock),
		Call:     application.NewGetCallQueryHandler(calls),
		Config:   application.NewGetConfigHandler(s.repos),
		Console:  s.console,
		Gate:     application.NewAccessGate(accesses, hub),
		Hub:      hub,
		Livefeed: application.NewSetLivefeedHandler(hub),
		Search:   application.NewSearchCallsQueryHandler(calls, systems, groups, tags),
		Settings: s.settings,
		Version:  Version,
	})

	mux := http.NewServeMux()

	adminAPI.Register(mux)
	upload.Register(mux)

	serveApplications(mux, listener, scanner.Embedded(), admin.Embedded())

	s.routes = mux

	return nil
}

// start runs the startup the legacy server runs: the notice that the server is up, the base folder it was
// given, the configuration read out of the database, the calls this server was holding when it last
// stopped, the pruning timer, and the directory watches.
//
// Three parts of the legacy startup are not here because they are not things this server has. The ingest
// consumer goroutine is gone: a call is finished before the thing that found it is answered, which is
// recorded in decision-internal-ports.md §17. The register-and-forget loop is gone: the hub holds its own
// sessions, and the listener-count debouncer is the thing the hub tells when the count of listeners
// changes. The administrator's socket has nothing to start: it starts when an administrator connects.
func (s *server) start(ctx context.Context) error {
	// The legacy server ignores a startup notice it cannot store, and the line has already reached the
	// console by the time the log view is asked for it.
	_ = s.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: s.clock.Now(),
		Level:    domain.LogLevelWarn,
		Source:   domain.LogSourceServer,
		Message:  "server started",
	})

	if len(s.config.BaseDir) > 0 {
		s.console.printf("base folder is %s\n", s.config.BaseDir)
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
	// on record, after the configuration has been read and before anything is answered from it. See
	// docs/specs/FEAT-004-legacy-defect-repairs/decision-signing-secret-persisted.md.
	result, err := s.signing.Handle(ctx, application.EnsureAdminSigningSecretCommand{})
	if err != nil {
		return err
	}

	if result.Outcome == application.AdminSigningSecretStored {
		s.console.println("generated a new signing secret")
	}

	// The calls this server was holding when it last stopped are picked up here, which is where the
	// legacy server picks them up: before anything is answered, so that a hold that ran out while the
	// server was down is delivered now and a hold still running is armed again.
	//
	// The loop that carries them into the pipeline starts before they are read back, so that the ones
	// handed over at once have somewhere to go.
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
func (s *server) deliverHeldCalls(ctx context.Context) {
	for {
		select {
		case call := <-s.held.Waiting():
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
func (s *server) readConfiguration(ctx context.Context) error {
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
func (s *server) stop(ctx context.Context) {
	s.stopping.Do(func() {
		if s.pruning != nil {
			s.pruning.Cancel()
		}

		if s.held != nil {
			s.held.Close()
		}

		if s.watches != nil {
			s.watches.Stop(ctx)
		}

		for _, serving := range s.serving {
			if err := serving.Shutdown(ctx); err != nil {
				s.console.println(err)
			}
		}

		if s.db != nil {
			if err := s.db.Close(); err != nil {
				s.console.println(err)
			}
		}

		s.console.println("terminated")
	})
}

// listen answers the configured address, and the https address when the configuration names a certificate
// or asks for one obtained automatically. It does not return until the server stops.
func (s *server) listen() error {
	addresses := serverAddresses(s.config, hostname())

	s.announce(addresses)

	if len(s.config.SslCertFile) > 0 && len(s.config.SslKeyFile) > 0 {
		go s.listenTLS(addresses, nil)

	} else if s.config.SslAutoCert != "" {
		manager := &autocert.Manager{
			Cache:      autocert.DirCache(s.config.GetAutocertCachePath()),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(s.config.SslAutoCert),
		}

		go s.listenTLS(addresses, manager.TLSConfig())
	}

	serving := s.httpServer(fmt.Sprintf("%s:%s", addresses.addr, addresses.port))

	if err := serving.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

// listenTLS answers the https address, in the two ways the legacy server answers it: with a certificate the
// deployment provided, or with one the server obtains for itself.
//
// A listener that cannot start is fatal, which is what the legacy server does: a server that was asked to
// answer on https and cannot is not a server that should carry on with only http.
func (s *server) listenTLS(addresses addresses, tlsConfig *tls.Config) {
	s.announceTLS(addresses)

	server := s.httpServer(fmt.Sprintf("%s:%s", addresses.sslAddr, addresses.sslPort))
	server.TLSConfig = tlsConfig

	var err error

	if len(s.config.SslCertFile) > 0 && len(s.config.SslKeyFile) > 0 {
		err = server.ListenAndServeTLS(s.config.GetSslCertFilePath(), s.config.GetSslKeyFilePath())
	} else {
		err = server.ListenAndServeTLS("", "")
	}

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// httpServer builds one listener.
//
// The timeouts are the legacy server's, and so is keeping keep-alives on. What is not the legacy server's
// is the logger: the legacy server hands the HTTP server a logger that throws everything away, which is
// quirk Q52 — a recovered panic and its stack, a TLS handshake failure, a broken write, none of it reaching
// anybody. This server hands it the console the operator is reading. What the client sees is unchanged.
func (s *server) httpServer(addr string) *http.Server {
	server := &http.Server{
		Addr:         addr,
		ErrorLog:     log.New(s.console, "", log.LstdFlags),
		Handler:      s.routes,
		ReadTimeout:  httpTimeout,
		WriteTimeout: httpTimeout,
	}

	server.SetKeepAlivesEnabled(true)

	s.serving = append(s.serving, server)

	return server
}

// waitForInterrupt stops the server when the operator interrupts it, which is the legacy server's SIGINT
// handling.
func (s *server) waitForInterrupt() {
	signals := make(chan os.Signal, 8)

	signal.Notify(signals, os.Interrupt)

	go func() {
		<-signals

		s.stop(context.Background())
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
func (s *server) announce(addresses addresses) {
	s.console.printf("main interface at %s", httpInterface(addresses.hostname, addresses.port))

	if !s.tlsConfigured() {
		s.console.printf("admin interface at %s/admin", httpInterface(addresses.hostname, addresses.port))
	}

	s.console.println("please consider sponsoring the project at https://github.com/sponsors/chuot")
}

// announceTLS says the https half of the announcement, which the legacy server says from the goroutine
// that serves it.
func (s *server) announceTLS(addresses addresses) {
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

func (s *server) tlsConfigured() bool {
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
