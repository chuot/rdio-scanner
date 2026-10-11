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

// Package app is the composition root: it opens the database the configuration names, wires the concrete
// infrastructure adapters into the application use cases, and serves the HTTP and WebSocket interfaces.
//
// It is a drop-in for the legacy server: the same configuration files, the same database a legacy
// deployment left behind, and the same interfaces clients already speak to — including the version it
// reports, which is pinned in the captures recorded beside the tests that prove them.
//
// cmd/rdio-scanner is the thin entry point that parses the command line and calls it. Nothing else
// depends on this package.
package app

import (
	"context"
	"io"
	"path"
	"path/filepath"

	"github.com/kardianos/service"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// The database backends the server talks to. The values are part of the configuration contract: they
// appear in rdio-scanner.ini and on the command line of deployed instances.
const (
	DbTypeMariadb    string = "mariadb"
	DbTypeMysql      string = "mysql"
	DbTypePostgresql string = "postgresql"
	DbTypeSqlite     string = "sqlite"
)

// Config is the configuration the composition root builds a server from. It is the effective server
// configuration — command-line flags overlaid with the ini file the deployment left in the base directory
// — with the three things the composition root decides from the way the program was run: where the server
// says what it says out loud, whose copy of the log entries an operator reads when there is no terminal,
// and which ffmpeg the server converts with.
type Config struct {
	BaseDir string

	DbType     string
	DbFile     string
	DbHost     string
	DbPort     uint
	DbName     string
	DbUsername string
	DbPassword string

	// DbTLS, DbSslMode, and DbConnectTimeout are the transport options the
	// operator opts in to (D12): db_tls for the mysql driver, db_sslmode for
	// postgres, and a connect timeout for both. Empty — the default — is
	// today's connection, byte for byte: plaintext, the driver's own timing.
	DbTLS            string
	DbSslMode        string
	DbConnectTimeout string

	// BcryptCost is the bcrypt cost the deployment hashes the administrator
	// credential at (CA-013.5). The ini option is validated to 10–14 at the
	// composition root; the zero value — a Config built without the option —
	// means bcrypt's default, the legacy server's cost.
	BcryptCost int

	Listen      string
	SslAutoCert string
	SslCertFile string
	SslKeyFile  string
	SslListen   string

	// TrustedProxies names the proxy addresses whose X-Forwarded-For the server honors: a
	// comma-separated list of CIDR prefixes and bare addresses. Empty — the default — means the
	// server trusts no proxy and names every request by the address of its connection, which is what
	// the login budget and the access warnings are keyed on.
	TrustedProxies string

	// Version is the release the server reports to clients, and is what the build tool resolved from
	// VERSION.txt and git.
	Version string

	// Out is where the server writes the lines it says out loud. The legacy server's console is Go's log
	// package, which writes to the standard error, so that is what a deployment hands it.
	Out io.Writer

	// Echo is whose copy of the server's log entries an operator reads when the server has no terminal in
	// front of it — the service manager's log for a server running as a service. It is nil for a server an
	// operator started, which says its entries to the console.
	Echo LogEcho

	// Converter decides which ffmpeg the server converts with, given the folder the server writes the
	// program it carries into, and answers with the converter to use and what the operator has to be told
	// about it. It is decided once while the server is wired, and said once when it starts.
	Converter func(dir string) (ports.AudioConverter, string)
}

// GetPath resolves a possibly relative path against the base directory.
func (config *Config) GetPath(p string) string {
	if path.IsAbs(p) {
		return p
	}

	return filepath.Join(config.BaseDir, p)
}

// GetDbFilePath returns the absolute path of the sqlite database file.
func (config *Config) GetDbFilePath() string {
	return config.GetPath(config.DbFile)
}

// GetSslCertFilePath returns the absolute path of the TLS certificate.
func (config *Config) GetSslCertFilePath() string {
	return config.GetPath(config.SslCertFile)
}

// GetSslKeyFilePath returns the absolute path of the TLS private key.
func (config *Config) GetSslKeyFilePath() string {
	return config.GetPath(config.SslKeyFile)
}

// GetAutocertCachePath returns the directory the Let's Encrypt cache is kept in.
func (config *Config) GetAutocertCachePath() string {
	return config.GetPath("autocert")
}

// GetFFmpegDirPath returns the directory the server writes the ffmpeg it carries inside its own binary
// into, so that it has a program it can actually run.
func (config *Config) GetFFmpegDirPath() string {
	return config.GetPath("ffmpeg")
}

// LogEcho is where a log entry is said out loud, as well as being written to the log view the
// administrator reads. A server an operator is watching says them to the terminal; a server running as a
// service says them to the service manager, which is what an operator of that deployment reads.
type LogEcho interface {
	Echo(level string, source domain.LogSource, message string)
}

// ChangeAdminPassword stores the credential the administrator chose, says so in the log view, and stops.
// It is the -admin_password mode: the way a deployment sets its credential from a script, or the way an
// operator recovers a server whose credential was lost. It writes the whole configuration, as the legacy
// server does, which is also what stores the signing secret the options read generated — the one thing
// this mode does that a startup does not.
//
// It is a way of running the program and not a thing the server does while it is running, so it is not a
// use case: the server is not started, nothing is answered, and the database is opened and closed here.
func (s *Server) ChangeAdminPassword(ctx context.Context, password string) error {
	hash, err := s.hashes.Hash(ctx, password)
	if err != nil {
		return err
	}

	options, err := s.options.ReadOptions(ctx)
	if err != nil {
		return err
	}

	options.AdminPasswordHash = hash

	// A credential that is still the one the server ships with is one the administrator has to replace,
	// which is what the web application insists on until they do.
	options.AdminPasswordNeedChange = password == domain.DefaultAdminPassword

	if err := s.options.WriteOptions(ctx, options); err != nil {
		return err
	}

	return s.logs.AppendLog(ctx, domain.LogEntry{
		DateTime: s.clock.Now(),
		Level:    domain.LogLevelInfo,
		Source:   domain.LogSourceAdmin,
		Message:  "the administrator password was changed",
	})
}

// serviceLog is the copy of the server's log entries a service manager keeps: the manager's own log, at the
// level the entry is filed under. A server running as a service has no terminal in front of it, and the
// manager's log is what an operator reads.
type serviceLog struct {
	logger service.Logger
}

var _ LogEcho = serviceLog{}

func (log serviceLog) Echo(level string, source domain.LogSource, message string) {
	line := renderLogEntry(level, source, message)

	switch level {
	case domain.LogLevelError:
		log.logger.Error(line)

	case domain.LogLevelWarn:
		log.logger.Warning(line)

	case domain.LogLevelInfo:
		log.logger.Info(line)
	}
}

// NewServiceLog is the operator's copy of the log entries for a server the service manager started.
func NewServiceLog(logger service.Logger) LogEcho {
	return serviceLog{logger: logger}
}
