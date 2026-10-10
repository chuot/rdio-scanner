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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rdio-scanner/internal/app"
)

// The -cmd mode is a client of a server that is already running. Every test here runs the program the way an
// operator runs it — flags in, exit code and console out — against a server that is answering on a real
// address, because the mode has no other way to reach a server and the point of it is the operator's script.

// TestTheCommandModeLogsInAndKeepsTheSessionForTheCommandsThatFollow is the arrangement a script depends on:
// log in once, and the commands that follow need no credential of their own because the session is in the
// token file.
func TestTheCommandModeLogsInAndKeepsTheSessionForTheCommandsThatFollow(t *testing.T) {
	url := commandTestServer(t)

	baseDir := t.TempDir()
	tokenFile := tokenFileOf(baseDir)

	code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url, commandArgPassword, "rdio-scanner")

	if code != 0 {
		t.Fatalf("the login ended with %d, want the code of a login that worked: %s", code, out)
	}

	if out != "Logged in.\n" {
		t.Errorf("the login said %q, want the operator to be told it worked", out)
	}

	// The session is kept inside the deployment's base directory, named after the executable. The legacy
	// server concatenated the base directory and the executable name with no separator between them, so
	// with the base directory an installed server actually has — one with no trailing slash — the file
	// landed beside the data directory rather than inside it. Repaired rather than preserved; see
	// docs/specs/FEAT-004-legacy-defect-repairs/decision-command-token-file.md.
	if filepath.Dir(tokenFile) != baseDir {
		t.Errorf("the session is kept at %s, want it inside the base directory %s", tokenFile, baseDir)
	}

	if name := filepath.Base(tokenFile); !strings.HasSuffix(name, ".token") || name == ".token" {
		t.Errorf("the session file is named %s, want the name of the executable with the token suffix", name)
	}

	info, err := os.Stat(tokenFile)
	if err != nil {
		t.Fatalf("the session token is not where the mode keeps it: %v", err)
	}

	// A session credential is readable by the operator who logged in and by nobody else.
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("the session token file has permissions %o, want %o", perm, 0600)
	}

	if token, err := os.ReadFile(tokenFile); err != nil || len(token) == 0 {
		t.Fatalf("the session token file holds nothing: %v", err)
	}

	// The command that follows names no credential at all, and is answered.
	out2 := filepath.Join(t.TempDir(), "snapshot")

	code, out = runTheCommandMode(t, baseDir, "config-get", commandArgURL, url, commandArgOut, out2)

	if code != 0 {
		t.Fatalf("config-get ended with %d, want the code of a command that worked: %s", code, out)
	}

	if !strings.Contains(out, "Server's configuration saved to") {
		t.Errorf("config-get said %q, want the operator to be told where the configuration went", out)
	}
}

// TestTheCommandModeWritesTheServersConfigurationToAFileTheOperatorNames is config-get: the configuration
// arrives unwrapped, in the file the operator named, with the extension the mode adds when the operator did
// not name one.
func TestTheCommandModeWritesTheServersConfigurationToAFileTheOperatorNames(t *testing.T) {
	url := commandTestServer(t)

	baseDir := t.TempDir()

	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url); code != 0 {
		t.Fatalf("login ended with %d: %s", code, out)
	}

	named := filepath.Join(t.TempDir(), "configuration")

	code, out := runTheCommandMode(t, baseDir, "config-get", commandArgURL, url, commandArgOut, named)

	if code != 0 {
		t.Fatalf("config-get ended with %d, want the code of a command that worked: %s", code, out)
	}

	// The mode writes the file the operator named with `.json` added, and says the name it wrote.
	if _, err := os.Stat(named); !os.IsNotExist(err) {
		t.Errorf("config-get wrote %s, want the name the operator gave with .json added", named)
	}

	if !strings.Contains(out, named+".json") {
		t.Errorf("config-get said %q, want it to name %s", out, named+".json")
	}

	config := readConfigurationFile(t, named+".json")

	// What is written is the configuration itself, not the answer the endpoint gave: the wrapper the
	// administrator endpoint puts around it is taken off.
	for _, section := range []string{"access", "apikeys", "dirwatch", "downstreams", "groups", "options", "systems", "tags"} {
		if _, found := config[section]; !found {
			t.Errorf("the configuration file holds no %s section, want the whole configuration", section)
		}
	}

	if _, found := config["config"]; found {
		t.Errorf("the configuration file is wrapped in a config entry, want the configuration on its own")
	}
}

// TestTheCommandModeAppliesAConfigurationTheOperatorHoldsInAFile is config-set, and it is the reason
// config-get exists: a configuration read out of one server is applied to another.
func TestTheCommandModeAppliesAConfigurationTheOperatorHoldsInAFile(t *testing.T) {
	url := commandTestServer(t)

	baseDir := t.TempDir()

	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url); code != 0 {
		t.Fatalf("login ended with %d: %s", code, out)
	}

	file := filepath.Join(t.TempDir(), "incoming.json")

	body := `{"access":[{"id":0,"code":"dispatch-code","expiration":"2027-01-01T00:00:00Z","ident":"Dispatch","limit":3,"systems":"*"}]}`

	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}

	code, out := runTheCommandMode(t, baseDir, "config-set", commandArgURL, url, commandArgIn, file)

	if code != 0 {
		t.Fatalf("config-set ended with %d, want the code of a command that worked: %s", code, out)
	}

	if out != "Server's configuration applied.\n" {
		t.Errorf("config-set said %q, want the operator to be told it was applied", out)
	}

	// The configuration the file held is the configuration the server runs with now.
	access := accessNamed(t, readConfigurationFile(t, getConfiguration(t, baseDir, url)), "Dispatch")

	if access == nil {
		t.Fatalf("the server runs with no access named Dispatch, want the one the file held")
	}

	if access["code"] != "dispatch-code" {
		t.Errorf("the access code is %v, want the one the file held", access["code"])
	}

	if access["limit"] != float64(3) {
		t.Errorf("the access limit is %v, want the one the file held", access["limit"])
	}
}

// TestTheCommandModeChangesTheAdministratorCredential is admin-password on a running server: the credential
// the server answers with afterwards is the one the operator named, and the one it used to answer with is
// refused.
func TestTheCommandModeChangesTheAdministratorCredential(t *testing.T) {
	url := commandTestServer(t)

	baseDir := t.TempDir()

	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url); code != 0 {
		t.Fatalf("login ended with %d: %s", code, out)
	}

	code, out := runTheCommandMode(t, baseDir, "admin-password", commandArgURL, url, commandArgPassword, "a-new-credential")

	if code != 0 {
		t.Fatalf("admin-password ended with %d, want the code of a command that worked: %s", code, out)
	}

	// The notice is the legacy server's, period and all: it has none.
	if out != "New admin password applied\n" {
		t.Errorf("admin-password said %q, want the notice the legacy server writes", out)
	}

	// The credential the server used to answer with is refused.
	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url, commandArgPassword, "rdio-scanner"); code == 0 {
		t.Errorf("the old credential logged in after the change, want it refused: %s", out)
	} else if !strings.Contains(out, "401") {
		t.Errorf("the old credential was answered %q, want the operator to be told the credential was refused", out)
	}

	// The credential the operator named is the one the server answers with.
	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url, commandArgPassword, "a-new-credential"); code != 0 {
		t.Fatalf("the new credential did not log in, want it to: %s", out)
	}
}

// TestTheCommandModeAddsAListenerAccessWithTheOptionsTheOperatorNames is user-add: the access the operator
// described is the access the server runs with, including the parts that only the optional arguments name.
func TestTheCommandModeAddsAListenerAccessWithTheOptionsTheOperatorNames(t *testing.T) {
	url := commandTestServer(t)

	baseDir := t.TempDir()

	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url); code != 0 {
		t.Fatalf("login ended with %d: %s", code, out)
	}

	code, out := runTheCommandMode(t, baseDir,
		"user-add", commandArgURL, url,
		commandArgIdent, "Dispatch",
		commandArgCode, "dispatch-code",
		commandArgLimit, "3",
		commandArgExpiration, "2027-01-01T00:00:00Z",
		commandArgSystems, "10001,20002",
	)

	if code != 0 {
		t.Fatalf("user-add ended with %d, want the code of a command that worked: %s", code, out)
	}

	if out != "User Dispatch added.\n" {
		t.Errorf("user-add said %q, want the operator to be told the access was added", out)
	}

	access := accessNamed(t, readConfigurationFile(t, getConfiguration(t, baseDir, url)), "Dispatch")

	if access == nil {
		t.Fatalf("the server runs with no access named Dispatch, want the one the operator added")
	}

	if access["code"] != "dispatch-code" {
		t.Errorf("the access code is %v, want the one the operator named", access["code"])
	}

	if access["limit"] != float64(3) {
		t.Errorf("the access limit is %v, want the one +limit named", access["limit"])
	}

	if access["expiration"] != "2027-01-01T00:00:00Z" {
		t.Errorf("the access expires at %v, want the moment +expiration named", access["expiration"])
	}

	// The systems the operator named by reference become the access's scope, with every talkgroup of each.
	scopes, ok := access["systems"].([]any)
	if !ok {
		t.Fatalf("the access scope is %v, want the two systems +systems named", access["systems"])
	}

	if len(scopes) != 2 {
		t.Fatalf("the access scope covers %d systems, want the two +systems named", len(scopes))
	}

	for i, want := range []any{float64(10001), float64(20002)} {
		scope, ok := scopes[i].(map[string]any)
		if !ok {
			t.Fatalf("the %d system in the access scope is %v, want an entry naming a system", i+1, scopes[i])
		}

		if scope["id"] != want {
			t.Errorf("the %d system in the access scope is %v, want the reference %v", i+1, scope["id"], want)
		}

		if scope["talkgroups"] != "*" {
			t.Errorf("the %d system in the access scope covers talkgroups %v, want every one of them", i+1, scope["talkgroups"])
		}
	}
}

// TestTheCommandModeRemovesAListenerAccess is user-remove.
func TestTheCommandModeRemovesAListenerAccess(t *testing.T) {
	url := commandTestServer(t)

	baseDir := t.TempDir()

	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url); code != 0 {
		t.Fatalf("login ended with %d: %s", code, out)
	}

	if code, out := runTheCommandMode(t, baseDir, "user-add", commandArgURL, url, commandArgIdent, "Dispatch", commandArgCode, "dispatch-code"); code != 0 {
		t.Fatalf("user-add ended with %d: %s", code, out)
	}

	code, out := runTheCommandMode(t, baseDir, "user-remove", commandArgURL, url, commandArgIdent, "Dispatch")

	if code != 0 {
		t.Fatalf("user-remove ended with %d, want the code of a command that worked: %s", code, out)
	}

	if out != "User Dispatch removed.\n" {
		t.Errorf("user-remove said %q, want the operator to be told the access was removed", out)
	}

	if access := accessNamed(t, readConfigurationFile(t, getConfiguration(t, baseDir, url)), "Dispatch"); access != nil {
		t.Errorf("the server still runs with the access the operator removed: %v", access)
	}
}

// TestTheCommandModeEndsTheSessionAndThrowsTheTokenAway is logout: the server stops honoring the credential
// and the operator's script stops having one.
func TestTheCommandModeEndsTheSessionAndThrowsTheTokenAway(t *testing.T) {
	url := commandTestServer(t)

	baseDir := t.TempDir()

	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url); code != 0 {
		t.Fatalf("login ended with %d: %s", code, out)
	}

	code, out := runTheCommandMode(t, baseDir, "logout", commandArgURL, url)

	if code != 0 {
		t.Fatalf("logout ended with %d, want the code of a command that worked: %s", code, out)
	}

	if out != "Logged out.\n" {
		t.Errorf("logout said %q, want the operator to be told the session is over", out)
	}

	if _, err := os.Stat(tokenFileOf(baseDir)); !os.IsNotExist(err) {
		t.Errorf("the session token file is still there after a logout, want it thrown away")
	}

	// The commands that need a session now say so, instead of doing something with no credential.
	if code, out := runTheCommandMode(t, baseDir, "config-get", commandArgURL, url, commandArgOut, filepath.Join(t.TempDir(), "none")); code == 0 {
		t.Errorf("config-get worked without a session, want it refused: %s", out)
	}
}

// TestACommandThatNeedsASessionSaysItIsNotLoggedIn is what an operator sees when they ran the command and not
// the login: a refusal that names the reason, before anything is sent to the server.
func TestACommandThatNeedsASessionSaysItIsNotLoggedIn(t *testing.T) {
	url := commandTestServer(t)

	code, out := runTheCommandMode(t, t.TempDir(), "config-get", commandArgURL, url, commandArgOut, filepath.Join(t.TempDir(), "none"))

	if code == 0 {
		t.Fatalf("config-get ended with the code of a command that worked, want a failure: %s", out)
	}

	if out != "Not logged in.\n" {
		t.Errorf("config-get said %q, want the operator to be told there is no session", out)
	}
}

// TestEveryCommandAgainstAServerThatIsNotAnsweringIsReported is the repaired half of Q70. The legacy server
// reported a request it could not make for two of the seven commands and said nothing at all for the other
// five — no message, and the exit code of a command that worked — so a script that ran a command while the
// server was down was told the command had happened. Every action now names the failure and ends as a
// failure. See docs/specs/FEAT-004-legacy-defect-repairs/decision-command-failure-reporting.md.
func TestEveryCommandAgainstAServerThatIsNotAnsweringIsReported(t *testing.T) {
	url := commandTestServer(t)

	baseDir := t.TempDir()

	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url); code != 0 {
		t.Fatalf("login ended with %d: %s", code, out)
	}

	dead := anAddressNothingAnswersOn(t)

	in := filepath.Join(t.TempDir(), "configuration.json")
	if err := os.WriteFile(in, []byte(`{"options":{"dimmerDelay":6000}}`), 0600); err != nil {
		t.Fatalf("writing the configuration the operator holds: %v", err)
	}

	for _, action := range []struct {
		name string
		args []string
	}{
		{commandConfigGet, []string{commandArgOut, filepath.Join(t.TempDir(), "none")}},
		{commandConfigSet, []string{commandArgIn, in}},
		{commandAdminPassword, []string{commandArgPassword, "a new credential"}},
		{commandUserAdd, []string{commandArgIdent, "car-4", commandArgCode, "a-code"}},
		{commandUserRemove, []string{commandArgIdent, "car-4"}},
	} {
		code, console := runTheCommandMode(t, baseDir, append([]string{action.name, commandArgURL, dead}, action.args...)...)

		if code == 0 {
			t.Errorf("%s against a server that is not answering ended with the code of a command that worked, "+
				"want a failure: %s", action.name, console)
		}

		if console == "" {
			t.Errorf("%s against a server that is not answering said nothing, want the operator to be told "+
				"the request could not be made", action.name)

			continue
		}

		if !strings.Contains(console, strings.TrimPrefix(dead, "http://")) {
			t.Errorf("%s said %q, want a message naming the address it could not reach, %s", action.name, console, dead)
		}
	}
}

// TestALoginAgainstAServerThatIsNotAnsweringIsReported is the half of Q70 the legacy server already got
// right: login and logout report a request they could not make, and a script that cannot log in is told so.
func TestALoginAgainstAServerThatIsNotAnsweringIsReported(t *testing.T) {
	dead := anAddressNothingAnswersOn(t)

	code, out := runTheCommandMode(t, t.TempDir(), "login", commandArgURL, dead)

	if code == 0 {
		t.Fatalf("login against a server that is not answering ended with the code of a command that worked, want a failure: %s", out)
	}

	if out == "" {
		t.Errorf("login against a server that is not answering said nothing, want the operator to be told it could not be done")
	}

	// Logout is the other command the legacy server reported, and it keeps its report.
	baseDir := t.TempDir()

	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, commandTestServer(t)); code != 0 {
		t.Fatalf("login ended with %d: %s", code, out)
	}

	code, out = runTheCommandMode(t, baseDir, "logout", commandArgURL, dead)

	if code == 0 {
		t.Errorf("logout against a server that is not answering ended with the code of a command that worked, want a failure: %s", out)
	}

	if out == "" {
		t.Errorf("logout against a server that is not answering said nothing, want the operator to be told it could not be done")
	}
}

// TestAnActionTheCommandModeDoesNotKnowPrintsTheUsageAndChangesNothing is -cmd help, and -cmd anything else:
// the operator is shown what the mode can do, and the mode does not treat an action it does not know as a
// failure.
func TestAnActionTheCommandModeDoesNotKnowPrintsTheUsageAndChangesNothing(t *testing.T) {
	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	app := filepath.Base(exePath)

	baseDir := t.TempDir()

	code, out := runTheCommandMode(t, baseDir, "bogus")

	if code != 0 {
		t.Fatalf("an unknown action ended with %d, want the code of a command that printed the usage: %s", code, out)
	}

	for _, want := range []string{
		"Available Commands:",
		"admin-password – Change administrator password.",
		"$ RDIO_ADMIN_PASSWORD=<password> ./" + app + " -cmd login",

		// The session file the usage text promises is the one the mode keeps: inside the base directory,
		// named after the executable. The legacy text promised `.<executable>.token`, a path in the
		// directory the operator happened to be standing in.
		"Session token keystore. Default is `" + tokenFileOf(baseDir) + "`.",

		"Server remote address. Default is `http://localhost:3000/`.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the usage text does not hold %q, want it to:\n%s", want, out)
		}
	}
}

// TestTheCommandModeTakesTheCredentialFromTheEnvironmentWhenTheOperatorNamesNone is the login the usage text
// shows: the credential in the environment, so that it is not in the process list of every other account on
// the machine.
func TestTheCommandModeTakesTheCredentialFromTheEnvironmentWhenTheOperatorNamesNone(t *testing.T) {
	url := commandTestServer(t)

	t.Setenv("RDIO_ADMIN_PASSWORD", "rdio-scanner")

	if code, out := runTheCommandMode(t, t.TempDir(), "login", commandArgURL, url); code != 0 {
		t.Fatalf("login with the credential in the environment ended with %d: %s", code, out)
	}

	// The environment is what was used, not a credential the mode made up: a wrong one is refused.
	t.Setenv("RDIO_ADMIN_PASSWORD", "not-the-credential")

	if code, out := runTheCommandMode(t, t.TempDir(), "login", commandArgURL, url); code == 0 {
		t.Errorf("login with the wrong credential in the environment worked, want it refused: %s", out)
	}
}

// TestACommandGivenAnAddressThatIsNotAServerAddressIsRefused is the +url check: the mode will not send a
// session credential to an address that is not an address it can talk to.
func TestACommandGivenAnAddressThatIsNotAServerAddressIsRefused(t *testing.T) {
	code, out := runTheCommandMode(t, t.TempDir(), "config-get", commandArgOut, filepath.Join(t.TempDir(), "none"), commandArgURL, "ftp://elsewhere/")

	if code == 0 {
		t.Fatalf("config-get with a non-http address ended with the code of a command that worked, want a failure: %s", out)
	}

	if !strings.Contains(out, "invalid URL") {
		t.Errorf("config-get with a non-http address said %q, want the operator to be told the address is not one", out)
	}
}

// TestAnAddressThatIsNotOneIsRefusedBeforeAnythingElseIsNamed is what the mode says when the operator named
// the address before the file: the address is what is wrong, and that is what the operator is told, even
// though the file the command needs was named after it. The legacy server stops at the address.
func TestAnAddressThatIsNotOneIsRefusedBeforeAnythingElseIsNamed(t *testing.T) {
	code, out := runTheCommandMode(t, t.TempDir(), "config-get", commandArgURL, "ftp://elsewhere/", commandArgOut, filepath.Join(t.TempDir(), "none"))

	if code == 0 {
		t.Fatalf("config-get with the address first ended with the code of a command that worked, want a failure: %s", out)
	}

	if !strings.Contains(out, "invalid URL") {
		t.Errorf("config-get with the address first said %q, want the address to be what the operator was told about", out)
	}
}

// TestASessionFileThatCannotBeReadIsReported is a token file that is there and is not something the mode can
// read: the mode says so, rather than carrying on as if the operator had never logged in.
func TestASessionFileThatCannotBeReadIsReported(t *testing.T) {
	url := commandTestServer(t)

	baseDir := t.TempDir()

	// A directory where the session is kept is a file that is there and cannot be read, whatever account
	// these tests happen to run as.
	if err := os.Mkdir(tokenFileOf(baseDir), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	code, out := runTheCommandMode(t, baseDir, "config-get", commandArgURL, url, commandArgOut, filepath.Join(t.TempDir(), "none"))

	if code == 0 {
		t.Fatalf("config-get with an unreadable session file ended with the code of a command that worked, want a failure: %s", out)
	}

	if strings.Contains(out, "Not logged in.") {
		t.Errorf("config-get with an unreadable session file said %q, want the mode to report the file rather than claim there is no session", out)
	}
}

// TestACommandGivenAFileThatIsNotAConfigurationIsRefused is config-set's own check: what the operator named
// has to be a configuration, and a file that holds something else is not applied.
func TestACommandGivenAFileThatIsNotAConfigurationIsRefused(t *testing.T) {
	url := commandTestServer(t)

	baseDir := t.TempDir()

	if code, out := runTheCommandMode(t, baseDir, "login", commandArgURL, url); code != 0 {
		t.Fatalf("login ended with %d: %s", code, out)
	}

	file := filepath.Join(t.TempDir(), "not-a-configuration.json")

	if err := os.WriteFile(file, []byte(`["access","apikeys"]`), 0600); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}

	code, out := runTheCommandMode(t, baseDir, "config-set", commandArgURL, url, commandArgIn, file)

	if code == 0 {
		t.Fatalf("config-set with a file that is not a configuration worked, want it refused: %s", out)
	}

	if !strings.Contains(out, "Invalid Data") {
		t.Errorf("config-set with a file that is not a configuration said %q, want the legacy server's refusal", out)
	}
}

// anAddressNothingAnswersOn is the address of a server that has stopped answering: the port is there and
// nothing is behind it, which is what an operator's script finds when the server it talks to is down.
func anAddressNothingAnswersOn(t *testing.T) string {
	t.Helper()

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	url := dead.URL

	dead.Close()

	return url
}

// commandTestServer is a server that is answering on a real address, which is what this mode needs: it is a
// client of a server, over the network, and has no way in that does not go through one.
func commandTestServer(t *testing.T) string {
	t.Helper()

	srv, err := app.New(commandTestConfig(t))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	t.Cleanup(func() { srv.Close() })

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	httpServer := httptest.NewServer(srv)

	t.Cleanup(httpServer.Close)

	return httpServer.URL
}

// runTheCommandMode runs the program the way an operator runs it and reports the exit code and what the
// operator saw. The base directory is the one the test prepared, because it is what decides where the session
// token lands.
func runTheCommandMode(t *testing.T, baseDir string, args ...string) (int, string) {
	t.Helper()

	var out bytes.Buffer

	code := run(append([]string{"-base_dir", baseDir, "-cmd"}, args...), &out)

	return code, out.String()
}

// tokenFileOf is where the command mode keeps the session of a deployment with this base directory: inside
// it, named after the executable.
func tokenFileOf(baseDir string) string {
	exePath, err := os.Executable()
	if err != nil {
		exePath = ""
	}

	return filepath.Join(baseDir, filepath.Base(exePath)+".token")
}

// getConfiguration asks a running server for its configuration through the command mode and reports the file
// it wrote, so that a test can see what the server runs with after the mode changed it.
func getConfiguration(t *testing.T, baseDir, url string) string {
	t.Helper()

	out := filepath.Join(t.TempDir(), "configuration")

	if code, console := runTheCommandMode(t, baseDir, "config-get", commandArgURL, url, commandArgOut, out); code != 0 {
		t.Fatalf("config-get ended with %d: %s", code, console)
	}

	return out + ".json"
}

// readConfigurationFile reads back a configuration the command mode wrote.
func readConfigurationFile(t *testing.T, path string) map[string]any {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}

	defer f.Close()

	var data map[string]any
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return data
}

// accessNamed finds the access code an operator with this ident is given, or nothing when the server gives
// none.
func accessNamed(t *testing.T, config map[string]any, ident string) map[string]any {
	t.Helper()

	entries, ok := config["access"].([]any)
	if !ok {
		t.Fatalf("the configuration holds %v where its access codes should be", config["access"])
	}

	for _, entry := range entries {
		access, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		if access["ident"] == ident {
			return access
		}
	}

	return nil
}

// The usage text is the operator's only documentation for this mode, and it names the executable the operator
// is holding. This is the line the mode prints for a login, spelled out for the binary these tests are
// running as, so that a change to the text has to be a change somebody noticed.
func TestTheUsageTextNamesTheLoginTheOperatorRuns(t *testing.T) {
	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	baseDir := t.TempDir()

	_, out := runTheCommandMode(t, baseDir, "help")

	if !strings.Contains(out, filepath.Base(exePath)+" -cmd login +password <password>") {
		t.Errorf("the usage text does not show the login with the name of the executable the operator is holding:\n%s", out)
	}

	// The token file the usage text promises is the one the mode actually writes: inside the base
	// directory, named after the executable.
	if !strings.Contains(out, "Default is `"+tokenFileOf(baseDir)+"`") {
		t.Errorf("the usage text does not name the session file the mode keeps, %s:\n%s", tokenFileOf(baseDir), out)
	}
}
