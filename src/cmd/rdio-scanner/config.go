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
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"

	"gopkg.in/ini.v1"
)

// Supported database backends. The values are part of the configuration contract:
// they appear in rdio-scanner.ini and on the command line of deployed instances.
const (
	DbTypeMariadb    string = "mariadb"
	DbTypeMysql      string = "mysql"
	DbTypePostgresql string = "postgresql"
	DbTypeSqlite     string = "sqlite"
)

const (
	defaultConfigFile       = "rdio-scanner.ini"
	defaultDbFile           = "rdio-scanner.db"
	defaultDbHost           = "localhost"
	defaultDbPortMariaDb    = uint(3306)
	defaultDbPortPostgreSql = uint(5432)
	defaultDbType           = DbTypeSqlite
	defaultListen           = ":3000"
)

// ConfigMode tells the composition root which mode the command line requested.
type ConfigMode int

const (
	// ModeServe means the server should start.
	ModeServe ConfigMode = iota

	// ModeExit means a one-shot mode (-version, -config_save) already completed.
	ModeExit

	// ModeCommand means the -cmd mode was asked for: drive a server that is already running, and stop.
	ModeCommand
)

// Config is the effective server configuration: command-line flags overlaid with
// the rdio-scanner.ini file found in the base directory.
type Config struct {
	BaseDir    string
	ConfigFile string

	DbType     string
	DbFile     string
	DbHost     string
	DbPort     uint
	DbName     string
	DbUsername string
	DbPassword string

	Listen      string
	SslAutoCert string
	SslCertFile string
	SslKeyFile  string
	SslListen   string

	// Special-mode inputs, dispatched by the composition root.
	Command string

	// CommandArgs are the tokens that came after the -cmd action. The flag parser stops at the first
	// argument that is not a flag, and every option this mode takes (`+ident`, `+password`, …) is such an
	// argument, so what the parser leaves over is exactly the options the mode was given.
	CommandArgs []string

	ServiceAction    string
	NewAdminPassword string
}

// newConfig parses the command line and overlays the ini file. exePath is injected
// so the base-directory default can be exercised without a real installed binary.
func newConfig(args []string, exePath string, stdout io.Writer) (*Config, ConfigMode, error) {
	config := &Config{}

	fs := flag.NewFlagSet("rdio-scanner", flag.ContinueOnError)
	fs.SetOutput(stdout)

	var (
		command       = fs.String("cmd", "", fmt.Sprintf("advanced administrative tasks (use -%s %s for usage)", commandArg, commandHelp))
		configSave    = fs.Bool("config_save", false, fmt.Sprintf("save configuration to %s", defaultConfigFile))
		serviceAction = fs.String("service", "", "service command, one of start, stop, restart, install, uninstall")
		version       = fs.Bool("version", false, "show application version")
	)

	if baseDir := defaultBaseDir(exePath); baseDir != "" {
		config.BaseDir = baseDir
	}

	fs.StringVar(&config.BaseDir, "base_dir", config.BaseDir, "base directory where all data will be written")
	fs.StringVar(&config.ConfigFile, "config", defaultConfigFile, "server config file")
	fs.StringVar(&config.DbFile, "db_file", defaultDbFile, "sqlite database file")
	fs.StringVar(&config.DbHost, "db_host", defaultDbHost, "database host ip or hostname")
	fs.StringVar(&config.DbName, "db_name", "", "database name")
	fs.StringVar(&config.DbPassword, "db_pass", "", "database password")
	fs.UintVar(&config.DbPort, "db_port", defaultDbPortMariaDb, "database host port")
	fs.StringVar(&config.DbType, "db_type", defaultDbType, fmt.Sprintf("database type, one of %s, %s, %s, %s", DbTypeSqlite, DbTypeMariadb, DbTypeMysql, DbTypePostgresql))
	fs.StringVar(&config.DbUsername, "db_user", "", "database user name")
	fs.StringVar(&config.Listen, "listen", defaultListen, "listening address")
	fs.StringVar(&config.NewAdminPassword, "admin_password", "", "change admin password")
	fs.StringVar(&config.SslAutoCert, "ssl_auto_cert", "", "domain name for Let's Encrypt automatic certificate")
	fs.StringVar(&config.SslCertFile, "ssl_cert_file", "", "ssl PEM formated certificate")
	fs.StringVar(&config.SslKeyFile, "ssl_key_file", "", "ssl PEM formated key")
	fs.StringVar(&config.SslListen, "ssl_listen", "", "listening address for ssl")

	if err := fs.Parse(args); err != nil {
		return nil, ModeExit, usageError{err}
	}

	config.Command = *command
	config.CommandArgs = fs.Args()
	config.ServiceAction = *serviceAction

	if !config.isBaseDirWritable() {
		return nil, ModeExit, fmt.Errorf("no write permissions in %s", config.BaseDir)
	}

	switch {
	case *configSave:
		if err := config.saveConfig(); err != nil {
			return nil, ModeExit, err
		}

		fmt.Fprintf(stdout, "%s file created\n", config.ConfigFile)
		return config, ModeExit, nil

	case *version:
		fmt.Fprintln(stdout, Version)
		return config, ModeExit, nil
	}

	if err := config.applyIniFile(); err != nil {
		return nil, ModeExit, err
	}

	if !isDbTypeSupported(config.DbType) {
		return nil, ModeExit, fmt.Errorf("unknown database type %s", config.DbType)
	}

	// The -cmd mode is decided here rather than left for the composition root to notice, because this is
	// where the legacy server decides it: after the configuration file has been read and the database type
	// has been checked, and before the service mode or the server itself. It outranks -admin_password for
	// the same reason — the legacy reaches it before main.go looks at the password.
	if config.Command != "" {
		return config, ModeCommand, nil
	}

	return config, ModeServe, nil
}

// defaultBaseDir reproduces the legacy install heuristic: the executable's directory,
// falling back to ~/Rdio Scanner when that directory is not writable. Go's own
// `go run` build cache directory is excluded so development runs stay in place.
func defaultBaseDir(exePath string) string {
	if exePath == "" {
		return ""
	}

	if regexp.MustCompile(`go-build[0-9]+`).Match([]byte(exePath)) {
		return ""
	}

	baseDir := filepath.Dir(exePath)
	if isWritableDir(baseDir) {
		return baseDir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return baseDir
	}

	baseDir = filepath.Join(home, "Rdio Scanner")
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		os.MkdirAll(baseDir, 0770)
	}

	return baseDir
}

// applyIniFile overlays the ini file over the parsed flags. A missing or unreadable
// ini file leaves the flag values untouched, exactly as the legacy server does.
func (config *Config) applyIniFile() error {
	cfg, err := ini.Load(config.GetConfigFilePath())
	if err != nil {
		return nil
	}

	section := cfg.Section("")

	overlayString(section, "db_file", &config.DbFile)
	overlayString(section, "db_host", &config.DbHost)
	overlayString(section, "db_name", &config.DbName)
	overlayString(section, "db_pass", &config.DbPassword)
	overlayString(section, "db_type", &config.DbType)
	overlayString(section, "db_user", &config.DbUsername)
	overlayString(section, "listen", &config.Listen)
	overlayString(section, "ssl_auto_cert", &config.SslAutoCert)
	overlayString(section, "ssl_cert_file", &config.SslCertFile)
	overlayString(section, "ssl_key_file", &config.SslKeyFile)
	overlayString(section, "ssl_listen", &config.SslListen)

	if port, err := section.Key("db_port").Uint(); err == nil {
		config.DbPort = port
	} else {
		config.DbPort = defaultDbPortMariaDb
		if config.DbType == DbTypePostgresql {
			config.DbPort = defaultDbPortPostgreSql
		}
	}

	return nil
}

func overlayString(section *ini.Section, key string, target *string) {
	if v := section.Key(key).String(); len(v) > 0 {
		*target = v
	}
}

func isDbTypeSupported(dbType string) bool {
	switch dbType {
	case DbTypeMariadb, DbTypeMysql, DbTypePostgresql, DbTypeSqlite:
		return true
	default:
		return false
	}
}

// GetConfigFilePath returns the absolute path of the ini file.
func (config *Config) GetConfigFilePath() string {
	return config.GetPath(config.ConfigFile)
}

// GetDbFilePath returns the absolute path of the sqlite database file.
func (config *Config) GetDbFilePath() string {
	return config.GetPath(config.DbFile)
}

// GetPath resolves a possibly relative path against the base directory.
func (config *Config) GetPath(p string) string {
	if path.IsAbs(p) {
		return p
	}
	return filepath.Join(config.BaseDir, p)
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

func (config *Config) isBaseDirWritable() bool {
	return isWritableDir(config.BaseDir)
}

func isWritableDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".tmp*")
	if err != nil {
		return false
	}

	f.Close()
	os.Remove(f.Name())

	return true
}

// saveConfig writes the effective configuration back to the ini file, in the same
// key order the legacy server writes, so files stay comparable between binaries.
func (config *Config) saveConfig() error {
	ini := []string{}

	if config.DbType == DbTypeSqlite {
		if config.DbFile != "" {
			ini = append(ini, fmt.Sprintf("db_file = %s", config.DbFile))
		}

	} else {
		if config.DbHost != "" {
			ini = append(ini, fmt.Sprintf("db_host = %s", config.DbHost))
		}

		if config.DbName != "" {
			ini = append(ini, fmt.Sprintf("db_name = %s", config.DbName))
		}

		if config.DbPassword != "" {
			ini = append(ini, fmt.Sprintf("db_pass = %s", config.DbPassword))
		}

		if config.DbPort > 0 {
			ini = append(ini, fmt.Sprintf("db_port = %s", strconv.Itoa(int(config.DbPort))))
		}
	}

	if config.DbType != "" {
		ini = append(ini, fmt.Sprintf("db_type = %s", config.DbType))
	}

	if config.DbUsername != "" && config.DbType != DbTypeSqlite {
		ini = append(ini, fmt.Sprintf("db_user = %s", config.DbUsername))
	}

	if config.Listen != "" {
		ini = append(ini, fmt.Sprintf("listen = %s", config.Listen))
	}

	if config.SslAutoCert != "" {
		ini = append(ini, fmt.Sprintf("ssl_auto_cert = %s", config.SslAutoCert))
	}

	if config.SslCertFile != "" {
		ini = append(ini, fmt.Sprintf("ssl_cert_file = %s", config.SslCertFile))
	}

	if config.SslKeyFile != "" {
		ini = append(ini, fmt.Sprintf("ssl_key_file = %s", config.SslKeyFile))
	}

	if config.SslListen != "" {
		ini = append(ini, fmt.Sprintf("ssl_listen = %s", config.SslListen))
	}

	file, err := os.Create(config.GetConfigFilePath())
	if err != nil {
		return err
	}

	for _, line := range ini {
		if _, err := file.WriteString(line + "\n"); err != nil {
			return err
		}
	}

	return file.Close()
}
