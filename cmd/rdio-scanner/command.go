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

// The -cmd mode is how an operator drives a server that is already running: log in, read the configuration,
// write it back, change the administrator credential, add or remove a listener access. It is a client of the
// same administrator endpoints the web application uses, and it keeps the session token in a file so that a
// script can log in once and then run several commands against the server.
//
// It is a mode of running the program rather than a thing the server does while it runs, which is why it
// lives in the composition root and not in the application layer: it has no access to the server's own
// configuration or database, only to its public administrator paths.

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

	// commandDefaultURL is the server the command mode talks to when the operator does not name one. The
	// port is the one the server listens on by default.
	commandDefaultURL = "http://localhost:3000/"
)

// commandNoSession is the refusal this mode produces itself, before it has any dealings with the server. The
// legacy server reports it for every command, which is what separates it from a request that could not be
// made.
var commandNoSession = errors.New("Not logged in.")

// commandInvalidURL is the address the operator named that this mode will not send a credential to. The
// legacy server stops there, with the message, before it reads anything else the operator named and before
// it looks at any session.
var commandInvalidURL = errors.New("invalid URL")

var commandURLScheme = regexp.MustCompile(`^https?://`)

// command is one invocation of the -cmd mode: what the operator asked for, the options that came with it,
// and the session the server handed out.
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

// runCommand is the -cmd mode: it drives a server that is already answering, and stops.
//
// It does not open the database, and it does not care which database this deployment uses: the whole point
// of the mode is that it works against a server somebody else started.
func runCommand(config *Config, exePath string, stdout io.Writer) int {
	cmd := newCommand(config, exePath, os.Getenv, stdout)

	return cmd.do(config.Command)
}

// newCommand builds the mode from what the operator gave it. The credential comes from the environment
// first, because the way the usage text shows a login is `RDIO_ADMIN_PASSWORD=<password>./rdio-scanner
// -cmd login`, and a credential in a command line is visible to every other process on the machine.
func newCommand(config *Config, exePath string, getenv func(string) string, stdout io.Writer) *command {
	app := filepath.Base(exePath)

	password := getenv("RDIO_ADMIN_PASSWORD")
	if password == "" {
		password = domain.DefaultAdminPassword
	}

	return &command{
		app:     app,
		baseDir: config.BaseDir,

		// The session token is kept inside the deployment's base directory, named after the executable.
		// The legacy server joined the base directory and the executable name with nothing between them,
		// so with the base directory an installed server actually has — one with no trailing slash — the
		// file landed beside the data directory instead of inside it. Repaired rather than preserved.
		tokenFile: filepath.Join(config.BaseDir, app+".token"),

		password: password,
		url:      commandDefaultURL,

		args:   config.CommandArgs,
		stdout: stdout,
	}
}

// do reads the options that came after the action, picks up any session this operator already has, runs the
// action, and reports how it ended.
//
// The options and the session are read before the action is chosen, as the legacy server reads them: an
// address that is not one, or a session file that cannot be read, is reported before the mode says anything
// about what it was asked to do.
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
		// Every action that could not be done is reported, with the failure as it happened. The legacy
		// server said nothing at all for five of the seven commands and ended as if the command had
		// worked, which is a preserved quirk and is repaired rather than preserved: an operator whose script ran a
		// command against a server that was not answering is told the command did not happen.
		fmt.Fprintf(cmd.stdout, "%v\n", err)

		return 1
	}

	return 0
}

// readOptions walks the tokens that came after the action and takes the value following each option. An
// option with nothing after it names nothing, which is what the legacy server ends up with.
//
// An address that is not one is the one option that stops the mode outright: the legacy server reports it
// and stops there, so an operator who named the address before the file is told about the address and not
// about the file.
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

// readToken picks up the session this operator already has, if the token file is there. A file that is there
// and cannot be read is reported, which is what the legacy server does with it: a session this mode cannot
// read is not a session this mode pretends not to have.
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

	// The file holds the server's whole configuration — signing secret, database
	// password, administrator hash — so it is created for its owner alone
	//, wherever the operator pointed it.
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

// logout ends the session and throws the token away. A token the server no longer honors is a session that
// is already over, so a refusal is treated the same as a success: what the operator asked for has happened.
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

// submit makes one request of the server, carrying the session when the path needs one.
//
// The address is not checked here: the mode checked it when the operator named it, and an address the
// operator did not name is the one this mode listens on by default.
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
		// The failure as it happened, for the caller to report. It used to be the five callers that
		// dropped it, and the mode ended as if the command had worked.
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
