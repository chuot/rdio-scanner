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
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/kardianos/service"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/infrastructure/persistence"
)

const (
	DbTypeMariadb    string = "mariadb"
	DbTypeMysql      string = "mysql"
	DbTypePostgresql string = "postgresql"
	DbTypeSqlite     string = "sqlite"
)

// ErrInUse says the deployment is already claimed by a running server:
// its database is open and its files are being written, and opening
// them a second time could cost an operator their data. Every mode
// that opens the database — the server, the credential script, the
// vacuum task — refuses with this sentinel rather than sharing one.
var ErrInUse = errors.New("the database is in use")

type Config struct {
	BaseDir string

	DbType     string
	DbFile     string
	DbHost     string
	DbPort     uint
	DbName     string
	DbUsername string
	DbPassword string

	// DbTLS, DbSslMode, and DbConnectTimeout are the database transport
	// options the operator opts in to: db_tls for the mysql driver, db_sslmode
	// for postgres, and a connect timeout for both.
	DbTLS            string
	DbSslMode        string
	DbConnectTimeout string

	// BcryptCost is the bcrypt cost the deployment hashes the administrator
	// credential at. The zero value means bcrypt's own default cost.
	BcryptCost int

	Listen      string
	SslAutoCert string
	SslCertFile string
	SslKeyFile  string
	SslListen   string

	// TrustedProxies names the proxy addresses whose X-Forwarded-For the server
	// honors, a comma-separated list of CIDR prefixes and bare addresses.
	TrustedProxies string

	// McpServer is whether this server serves the Model Context Protocol
	// endpoint an AI assistant administers the deployment through. Off is the
	// default at every entry point.
	McpServer bool

	// Version is the release the server reports to clients, and is what the build tool resolved from
	// VERSION.txt and git.
	Version string

	Out io.Writer

	Echo LogEcho

	// Converter decides which ffmpeg the server converts with and which ffprobe it
	// measures lengths with, and it is decided once while the server is wired.
	Converter func(dir string) (ports.AudioConverter, ports.AudioLengthProber, string)
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

// GetFFmpegDirPath returns the disposable folder under the system temp
// directory, named for the server, where the carried programs and the
// probe files are written.
func (config *Config) GetFFmpegDirPath() string {
	return filepath.Join(os.TempDir(), "rdio-scanner.ffmpeg")
}

type LogEcho interface {
	Echo(level string, source domain.LogSource, message string)
}

// ChangeAdminPassword stores the chosen administrator credential, says so
// in the log view, and exits: the -admin_password mode, run instead of
// starting the server.
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

// Vacuum compacts the sqlite file the deployment lives in, in place, and
// returns the page measurements taken on either side of the statement: the
// -vacuum mode, run instead of starting the server. Only sqlite stores its
// whole database in one file the server can rewrite; the config layer
// refuses the mode for any other backend before this method is reached, and
// the check here keeps the composition root honest when the operation is
// called directly.
func (s *Server) Vacuum(ctx context.Context) (persistence.VacuumMeasurements, persistence.VacuumMeasurements, error) {
	if s.dialect != persistence.DialectSqlite {
		return persistence.VacuumMeasurements{}, persistence.VacuumMeasurements{},
			fmt.Errorf("the vacuum task needs the sqlite database, not %s", string(s.dialect))
	}

	return persistence.Vacuum(ctx, s.db)
}

// serviceLog is the copy of the server's log entries a service manager
// keeps, the log an operator of a service deployment reads.
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
