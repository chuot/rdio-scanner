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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"rdio-scanner/internal/domain"
)

// The -cmd mode drives a running server through the administrator
// endpoints and keeps its session token in a file; it is a mode of running
// the program, so it lives in the composition root, not the application layer.
// see the command-mode-client-of-running-server maintainer note.

const (
	// commandArg is the flag the mode is asked for with, and the name the usage text prints in front of
	// every example.
	commandArg = "cmd"

	commandArgCode       = "+code"
	commandArgExpiration = "+expiration"
	commandArgIdent      = "+ident"
	commandArgIn         = "+in"
	commandArgLimit      = "+limit"
	commandArgOut        = "+out"
	commandArgPassword   = "+password"
	commandArgSystems    = "+systems"
	commandArgToken      = "+token"
	commandArgURL        = "+url"

	commandAdminPassword = "admin-password"
	commandConfigGet     = "config-get"
	commandConfigSet     = "config-set"
	commandHelp          = "help"
	commandLogin         = "login"
	commandLogout        = "logout"
	commandUserAdd       = "user-add"
	commandUserRemove    = "user-remove"

	// commandDefaultURL is the server used when the
	// operator names none, on the default listen port.
	commandDefaultURL = "http://localhost:3000/"
)

// commandNoSession is this mode's own refusal, raised
// before any dealings with the server.
// see the command-wiring-legacy-behavior maintainer note.
var commandNoSession = errors.New("Not logged in.")

// commandInvalidURL is the refusal of an address this mode will
// not send a credential to.
// see the command-option-ordering maintainer note.
var commandInvalidURL = errors.New("invalid URL")

var commandURLScheme = regexp.MustCompile(`^https?://`)

// command is one -cmd invocation: the action asked for,
// its options, and the session the server handed out.
type command struct {
	app        string
	baseDir    string
	code       string
	ident      string
	expiration string
	in         string
	limit      string
	out        string
	password   string
	systems    string
	token      string
	tokenFile  string
	url        string

	args   []string
	stdout io.Writer
}

// runCommand is the -cmd mode: it drives a server that is already
// answering, and opens no database of its own.
func runCommand(config *Config, exePath string, stdout io.Writer) int {
	cmd := newCommand(config, exePath, os.Getenv, stdout)

	return cmd.do(config.Command)
}

// newCommand builds the mode from what the operator gave it. The
// credential comes from the environment first, so it stays out of
// the process list other accounts can read.
func newCommand(config *Config, exePath string, getenv func(string) string, stdout io.Writer) *command {
	app := filepath.Base(exePath)

	password := getenv("RDIO_ADMIN_PASSWORD")
	if password == "" {
		password = domain.DefaultAdminPassword
	}

	return &command{
		app:     app,
		baseDir: config.BaseDir,

		// The session token file sits inside the base directory,
		// named after the executable name plus .token.
		// see the command-session-token-file maintainer note.
		tokenFile: filepath.Join(config.BaseDir, app+".token"),

		password: password,
		url:      commandDefaultURL,

		args:   config.CommandArgs,
		stdout: stdout,
	}
}

// do reads the options after the action, picks up any session the
// operator already has, runs the action, and reports how it ended.
// Options and session are read before the action is chosen, so a bad
// address or unreadable session file is reported before the action is.
func (cmd *command) do(action string) int {
	if err := cmd.readOptions(); err != nil {
		fmt.Fprintf(cmd.stdout, "%v\n", err)

		return 1
	}

	if err := cmd.readToken(); err != nil {
		fmt.Fprintf(cmd.stdout, "%v\n", err)

		return 1
	}

	var err error

	switch action {
	case commandAdminPassword:
		err = cmd.adminPassword()

	case commandConfigGet:
		err = cmd.configGet()

	case commandConfigSet:
		err = cmd.configSet()

	case commandLogin:
		err = cmd.login()

	case commandLogout:
		err = cmd.logout()

	case commandUserAdd:
		err = cmd.userAdd()

	case commandUserRemove:
		err = cmd.userRemove()

	default:
		cmd.printUsage()

		return 0
	}

	if err != nil {
		// An action that could not be done is reported with the
		// failure and ends as a failure.
		// see the command-failure-reporting maintainer note.
		fmt.Fprintf(cmd.stdout, "%v\n", err)

		return 1
	}

	return 0
}

// readOptions walks the tokens after the action and takes the value
// after each option; an option with nothing after it names nothing.
// see the command-option-ordering maintainer note.
func (cmd *command) readOptions() error {
	i := 0

	next := func() string {
		i++

		if i < len(cmd.args) {
			return cmd.args[i]
		}

		return ""
	}

	for i < len(cmd.args) {
		switch cmd.args[i] {
		case commandArgCode:
			cmd.code = next()

		case commandArgExpiration:
			cmd.expiration = next()

		case commandArgIdent:
			cmd.ident = next()

		case commandArgIn:
			cmd.in = next()

		case commandArgLimit:
			cmd.limit = next()

		case commandArgOut:
			cmd.out = next()

			if !strings.HasSuffix(strings.ToLower(cmd.out), ".json") {
				cmd.out = cmd.out + ".json"
			}

		case commandArgPassword:
			cmd.password = next()

		case commandArgSystems:
			cmd.systems = next()

		case commandArgToken:
			cmd.tokenFile = next()

		case commandArgURL:
			cmd.url = next()

			if !commandURLScheme.Match([]byte(cmd.url)) {
				return commandInvalidURL
			}
		}

		i++
	}

	return nil
}

// readToken picks up an existing session from the token
// file: a file it cannot read is reported, not ignored.
func (cmd *command) readToken() error {
	if _, err := os.Stat(cmd.tokenFile); !os.IsNotExist(err) {
		token, err := os.ReadFile(cmd.tokenFile)
		if err != nil {
			return err
		}

		cmd.token = string(token)
	}

	return nil
}

// adminPassword gives the server a new administrator credential.
func (cmd *command) adminPassword() error {
	if cmd.password == "" {
		return fmt.Errorf("missing %s <password> arguments.", commandArgPassword)
	}

	body, err := json.Marshal(map[string]any{"newPassword": cmd.password})
	if err != nil {
		return err
	}

	res, err := cmd.submit(http.MethodPost, "/api/admin/password", body, true)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}

	fmt.Fprintln(cmd.stdout, "New admin password applied")

	return nil
}

// configGet writes the server's configuration to a file the operator names.
func (cmd *command) configGet() error {
	if cmd.out == "" {
		return fmt.Errorf("missing %s <file.json> arguments.", commandArgOut)
	}

	res, err := cmd.submit(http.MethodGet, "/api/admin/config", nil, true)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}

	var data any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return err
	}

	config, ok := data.(map[string]any)
	if !ok {
		return errors.New("invalid response")
	}

	configuration, ok := config["config"].(map[string]any)
	if !ok {
		return nil
	}

	// The file holds the server's secrets, so it is
	// created readable and writable by its owner alone.
	f, err := os.OpenFile(cmd.out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(configuration); err != nil {
		return err
	}

	fmt.Fprintf(cmd.stdout, "Server's configuration saved to %s.\n", cmd.out)

	return nil
}

// configSet applies a configuration the operator has in a file.
func (cmd *command) configSet() error {
	if cmd.in == "" {
		return fmt.Errorf("missing %s <file.json> arguments.", commandArgIn)
	}

	f, err := os.Open(cmd.in)
	if err != nil {
		return err
	}

	defer f.Close()

	var data any
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return err
	}

	configuration, ok := data.(map[string]any)
	if !ok {
		return errors.New("Invalid Data")
	}

	body, err := json.Marshal(configuration)
	if err != nil {
		return err
	}

	res, err := cmd.submit(http.MethodPut, "/api/admin/config", body, true)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}

	fmt.Fprintln(cmd.stdout, "Server's configuration applied.")

	return nil
}

// login asks the server for a session and keeps it, so that the commands that need one do not each need a
// credential of their own.
func (cmd *command) login() error {
	body, err := json.Marshal(map[string]any{"password": cmd.password})
	if err != nil {
		return err
	}

	res, err := cmd.submit(http.MethodPost, "/api/admin/login", body, false)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}

	var data any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return err
	}

	answer, ok := data.(map[string]any)
	if !ok {
		return errors.New("invalid response")
	}

	token, ok := answer["token"].(string)
	if !ok {
		return errors.New("no token in response")
	}

	cmd.token = token

	if err := os.WriteFile(cmd.tokenFile, []byte(cmd.token), 0600); err != nil {
		return err
	}

	fmt.Fprintln(cmd.stdout, "Logged in.")

	return nil
}

// web request: station search endpoint, query params, session token
// header, bearer prefix added when missing.
func (cmd *command) logout() error {
	res, err := cmd.submit(http.MethodPost, "/api/admin/logout", nil, true)
	if err != nil {
		return err
	}

	switch res.StatusCode {
	case http.StatusOK, http.StatusUnauthorized:
		cmd.token = ""

		if _, err := os.Stat(cmd.tokenFile); !os.IsNotExist(err) {
			if err := os.Remove(cmd.tokenFile); err != nil {
				return err
			}
		}

		fmt.Fprintln(cmd.stdout, "Logged out.")

		return nil

	default:
		return errors.New(res.Status)
	}
}

// userAdd gives a listener an access code. The scope is every system unless the operator names the systems
// the access may see.
func (cmd *command) userAdd() error {
	if cmd.ident == "" {
		return fmt.Errorf("missing %s <ident> arguments.", commandArgIdent)
	}

	if cmd.code == "" {
		return fmt.Errorf("missing %s <code> arguments.", commandArgCode)
	}

	user := map[string]any{
		"code":    cmd.code,
		"ident":   cmd.ident,
		"systems": "*",
	}

	if cmd.expiration != "" {
		at, err := time.Parse(time.RFC3339, cmd.expiration)
		if err != nil {
			return fmt.Errorf("invalid date format for %s", commandArgExpiration)
		}

		user["expiration"] = at.UTC().Format(time.RFC3339)
	}

	if cmd.limit != "" {
		i, err := strconv.Atoi(cmd.limit)
		if err != nil {
			return fmt.Errorf("invalid number for %s", commandArgLimit)
		}

		user["limit"] = i
	}

	if cmd.systems != "" {
		refs := []int{}

		for _, value := range strings.Split(cmd.systems, ",") {
			i, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("the value '%s' is invalid for %s", value, commandArgSystems)
			}

			refs = append(refs, i)
		}

		if len(refs) == 0 {
			return fmt.Errorf("invalid system ids list for %s", commandArgSystems)
		}

		systems := []map[string]any{}

		for _, ref := range refs {
			systems = append(systems, map[string]any{"id": ref, "talkgroups": "*"})
		}

		user["systems"] = systems
	}

	body, err := json.Marshal(user)
	if err != nil {
		return err
	}

	res, err := cmd.submit(http.MethodPost, "/api/admin/user-add", body, true)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}

	fmt.Fprintf(cmd.stdout, "User %s added.\n", cmd.ident)

	return nil
}

// userRemove takes a listener access away.
func (cmd *command) userRemove() error {
	if cmd.ident == "" {
		return fmt.Errorf("missing %s <ident> arguments.", commandArgIdent)
	}

	body, err := json.Marshal(map[string]any{"ident": cmd.ident})
	if err != nil {
		return err
	}

	res, err := cmd.submit(http.MethodPost, "/api/admin/user-remove", body, true)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}

	fmt.Fprintf(cmd.stdout, "User %s removed.\n", cmd.ident)

	return nil
}

// submit makes one request to the server, carrying the session
// where the path needs one; the address was checked when the
// operator named it, so it is not checked here.
func (cmd *command) submit(method, path string, body []byte, auth bool) (*http.Response, error) {
	reader := io.Reader(nil)
	if body != nil {
		reader = bytes.NewReader(body)
	}

	request, err := http.NewRequest(method, strings.TrimSuffix(cmd.url, "/")+path, reader)
	if err != nil {
		return nil, err
	}

	if auth {
		if cmd.token == "" {
			return nil, commandNoSession
		}

		request.Header.Add("Authorization", cmd.token)
	}

	if body != nil {
		request.Header.Add("Content-Type", "application/json")
	}

	res, err := http.DefaultClient.Do(request)
	if err != nil {
		// submit hands the failure to the caller for reporting.
		// see the command-failure-reporting maintainer note.
		return nil, err
	}

	return res, nil
}

// printUsage is the text the operator sees for `-cmd help`, and for an action this mode does not know.
func (cmd *command) printUsage() {
	prompt := "$ ./"
	if runtime.GOOS == "windows" {
		prompt = "C:\\>"
	}

	fmt.Fprintf(cmd.stdout, "\nAvailable Commands:\n\n")
	fmt.Fprintf(cmd.stdout, "  %-11s – Change administrator password.\n\n", commandAdminPassword)
	fmt.Fprintf(cmd.stdout, "    %-11s %s%s -%s %s %s <password>\n\n", "", prompt, cmd.app, commandArg, commandAdminPassword, commandArgPassword)
	fmt.Fprintf(cmd.stdout, "  %-11s – Retrieve server's configuration.\n\n", commandConfigGet)
	fmt.Fprintf(cmd.stdout, "    %-11s %s%s -%s %s %s <file.json>\n\n", "", prompt, cmd.app, commandArg, commandConfigGet, commandArgOut)
	fmt.Fprintf(cmd.stdout, "  %-11s – Set server's configuration.\n\n", commandConfigSet)
	fmt.Fprintf(cmd.stdout, "    %-11s %s%s -%s %s %s <file.json>\n\n", "", prompt, cmd.app, commandArg, commandConfigSet, commandArgIn)
	fmt.Fprintf(cmd.stdout, "  %-11s – Login to server.\n\n", commandLogin)

	if runtime.GOOS != "windows" {
		fmt.Fprintf(cmd.stdout, "    %-11s $ RDIO_ADMIN_PASSWORD=<password> ./%s -%s %s\n", "", cmd.app, commandArg, commandLogin)
	}

	fmt.Fprintf(cmd.stdout, "    %-11s %s%s -%s %s %s <password>\n\n", "", prompt, cmd.app, commandArg, commandLogin, commandArgPassword)
	fmt.Fprintf(cmd.stdout, "  %-11s – Logout from server.\n\n", commandLogout)
	fmt.Fprintf(cmd.stdout, "    %-11s %s%s -%s %s\n\n", "", prompt, cmd.app, commandArg, commandLogout)
	fmt.Fprintf(cmd.stdout, "  %-11s – Add a user access.\n\n", commandUserAdd)
	fmt.Fprintf(cmd.stdout, "    %-11s %s%s -%s %s %s <ident> %s <code>\n\n", "", prompt, cmd.app, commandArg, commandUserAdd, commandArgIdent, commandArgCode)
	fmt.Fprintf(cmd.stdout, "    %-11s Optional:\n\n", "")
	fmt.Fprintf(cmd.stdout, "      %-11s %-11s <RFC3339 format>      – User access expiration date.\n", "", commandArgExpiration)
	fmt.Fprintf(cmd.stdout, "      %-11s %-11s <limit>               – Concurrent user access limit.\n", "", commandArgLimit)
	fmt.Fprintf(cmd.stdout, "      %-11s %-11s <sysid1[,sysid2,...]> – Specific system access.\n\n", "", commandArgSystems)
	fmt.Fprintf(cmd.stdout, "  %-11s – Remove a user access.\n\n", commandUserRemove)
	fmt.Fprintf(cmd.stdout, "    %-11s %s%s -%s %s %s <ident>\n\n", "", prompt, cmd.app, commandArg, commandUserRemove, commandArgIdent)
	fmt.Fprintf(cmd.stdout, "Global Options:\n\n")
	fmt.Fprintf(cmd.stdout, "  %-11s – Session token keystore. Default is `%s`.\n", commandArgToken, filepath.Join(cmd.baseDir, cmd.app+".token"))
	fmt.Fprintf(cmd.stdout, "  %-11s – Server remote address. Default is `%s`.\n\n", commandArgURL, commandDefaultURL)
}
