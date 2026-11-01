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

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"gopkg.in/ini.v1"

	"rdio-scanner/internal/app"
)

// Supported database backend names: they appear in the ini file and
// on the command line, and the composition root names the same values.
const (
	DbTypeMariadb    = app.DbTypeMariadb
	DbTypeMysql      = app.DbTypeMysql
	DbTypePostgresql = app.DbTypePostgresql
	DbTypeSqlite     = app.DbTypeSqlite
)

const (
	defaultConfigFile       = "rdio-scanner.ini"
	defaultDbFile           = "rdio-scanner.db"
	defaultDbHost           = "localhost"
	defaultBcryptCost       = 10
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

// Config is the effective server configuration: command-line flags
// overlaid with the rdio-scanner.ini file found in the base directory.
type Config struct {
	app.Config

	ConfigFile string

	// Special-mode inputs, dispatched by the composition root.
	Command string

	// CommandArgs are the tokens after the -cmd action: parsing stops at the
	// first non-flag argument, which is every option this mode takes.
	CommandArgs []string

	ServiceAction    string
	NewAdminPassword string

	// Vacuum asks for the one-shot compacting task: the composition root
	// runs it and exits, so it never reaches the ini file.
	Vacuum bool
}

// newConfig parses the command line and overlays the ini file. exePath is
// injected so the base-directory default can be tested without an install.
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
	fs.StringVar(&config.DbConnectTimeout, "db_connect_timeout", "", "database connection timeout, a Go duration such as 5s")
	fs.StringVar(&config.DbName, "db_name", "", "database name")
	fs.StringVar(&config.DbPassword, "db_pass", "", "database password")
	fs.StringVar(&config.DbSslMode, "db_sslmode", "", "postgres sslmode, one of the postgres sslmode names")
	fs.StringVar(&config.DbTLS, "db_tls", "", "mysql tls, one of true, false, skip-verify, or a registered tls config name")
	fs.UintVar(&config.DbPort, "db_port", defaultDbPortMariaDb, "database host port")
	fs.StringVar(&config.DbType, "db_type", defaultDbType, fmt.Sprintf("database type, one of %s, %s, %s, %s", DbTypeSqlite, DbTypeMariadb, DbTypeMysql, DbTypePostgresql))
	fs.IntVar(&config.BcryptCost, "bcrypt_cost", defaultBcryptCost, "admin credential bcrypt cost, 10 to 14")
	fs.StringVar(&config.DbUsername, "db_user", "", "database user name")
	fs.StringVar(&config.Listen, "listen", defaultListen, "listening address")
	fs.StringVar(&config.NewAdminPassword, "admin_password", "", "change admin password")
	fs.BoolVar(&config.Vacuum, "vacuum", false, "compact the sqlite database and exit, refusing while another instance holds the deployment")
	fs.StringVar(&config.SslAutoCert, "ssl_auto_cert", "", "domain name for Let's Encrypt automatic certificate")
	fs.StringVar(&config.SslCertFile, "ssl_cert_file", "", "ssl PEM formated certificate")
	fs.StringVar(&config.SslKeyFile, "ssl_key_file", "", "ssl PEM formated key")
	fs.StringVar(&config.SslListen, "ssl_listen", "", "listening address for ssl")
	fs.StringVar(&config.TrustedProxies, "trusted_proxies", "", "comma-separated proxy addresses (CIDR or bare) whose X-Forwarded-For is honored")
	fs.BoolVar(&config.McpServer, "mcp_server", false, "serve the Model Context Protocol endpoint")

	if err := fs.Parse(args); err != nil {
		return nil, ModeExit, usageError{err}
	}

	config.Command = *command
	config.CommandArgs = fs.Args()
	config.ServiceAction = *serviceAction

	// The default base directory is deliberately empty for a program sitting in
	// a Go build cache, and an empty base directory is not inert: every relative
	// path is joined onto it, so the folder the server really writes its database
	// file, its configuration file and its state into is the folder it was
	// started in. Naming that folder here makes it the one folder the paths
	// resolve against, the folder the write check below probes, and the folder the
	// server reports as the one it is serving.
	// see the base-dir-default-heuristic maintainer note.
	if config.BaseDir == "" {
		launchDir, err := os.Getwd()
		if err != nil {
			return nil, ModeExit, fmt.Errorf("the folder the server was started in cannot be read: %w", err)
		}

		config.BaseDir = launchDir
	}

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

	// A bcrypt cost outside 10 to 14 is a typo, and a typo here would hash
	// the administrator credential at a cost the operator never asked for.
	// The connect timeout must parse as a duration, or the server would
	// connect on a timing the operator never named.
	if config.DbConnectTimeout != "" {
		if _, err := time.ParseDuration(config.DbConnectTimeout); err != nil {
			return nil, ModeExit, fmt.Errorf("database connect timeout %s is not a duration: %w", config.DbConnectTimeout, err)
		}
	}

	if config.BcryptCost < 10 || config.BcryptCost > 14 {
		return nil, ModeExit, fmt.Errorf("bcrypt cost %d is outside the range 10 to 14", config.BcryptCost)
	}

	// The -cmd mode is decided during config parsing, after the ini file is
	// read, and it outranks -admin_password.
	// see the cmd-mode-decision-order maintainer note.
	if config.Command != "" {
		return config, ModeCommand, nil
	}

	return config, ModeServe, nil
}

// defaultBaseDir picks the executable's directory, falling back to
// ~/Rdio Scanner when it is not writable; go run caches are excluded.
// see the base-dir-default-heuristic maintainer note.
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

// applyIniFile overlays the ini file over the parsed flags: a missing or
// unreadable file leaves the flag values untouched.
func (config *Config) applyIniFile() error {
	cfg, err := ini.Load(config.GetConfigFilePath())
	if err != nil {
		return nil
	}

	section := cfg.Section("")

	overlayString(section, "db_file", &config.DbFile)
	overlayString(section, "db_host", &config.DbHost)
	overlayString(section, "db_name", &config.DbName)
	overlayString(section, "db_connect_timeout", &config.DbConnectTimeout)
	overlayString(section, "db_pass", &config.DbPassword)
	overlayString(section, "db_sslmode", &config.DbSslMode)
	overlayString(section, "db_tls", &config.DbTLS)
	overlayString(section, "db_type", &config.DbType)
	overlayString(section, "db_user", &config.DbUsername)
	overlayString(section, "listen", &config.Listen)
	overlayString(section, "ssl_auto_cert", &config.SslAutoCert)
	overlayString(section, "ssl_cert_file", &config.SslCertFile)
	overlayString(section, "ssl_key_file", &config.SslKeyFile)
	overlayString(section, "ssl_listen", &config.SslListen)
	overlayString(section, "trusted_proxies", &config.TrustedProxies)

	overlayBool(section, "mcp_server", &config.McpServer)

	if cost, err := section.Key("bcrypt_cost").Int(); err == nil {
		config.BcryptCost = cost
	} else {
		config.BcryptCost = defaultBcryptCost
	}

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

// overlayBool overlays one switch from the ini file: an absent key, or
// one that does not read as a boolean, leaves the flag's value alone.
func overlayBool(section *ini.Section, key string, target *bool) {
	if v, err := section.Key(key).Bool(); err == nil {
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

// saveConfig writes the ini file in the legacy server's key order.
// see the ini-file-compatibility maintainer note.
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

		if config.DbTLS != "" {
			ini = append(ini, fmt.Sprintf("db_tls = %s", config.DbTLS))
		}

		if config.DbSslMode != "" {
			ini = append(ini, fmt.Sprintf("db_sslmode = %s", config.DbSslMode))
		}

		if config.DbConnectTimeout != "" {
			ini = append(ini, fmt.Sprintf("db_connect_timeout = %s", config.DbConnectTimeout))
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

	if config.TrustedProxies != "" {
		ini = append(ini, fmt.Sprintf("trusted_proxies = %s", config.TrustedProxies))
	}

	// The mcp_server key is saved only when it is on, like bcrypt_cost only
	// when it differs from its default.
	// see the ini-file-compatibility maintainer note.
	if config.McpServer {
		ini = append(ini, "mcp_server = true")
	}

	if config.BcryptCost != defaultBcryptCost {
		ini = append(ini, fmt.Sprintf("bcrypt_cost = %d", config.BcryptCost))
	}

	// The ini holds the database password, so it is created readable and
	// writable by its owner alone.
	file, err := os.OpenFile(config.GetConfigFilePath(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
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
