// Copyright (C) 2019-2024 Chrystian Huot <chrystian@huot.qc.ca>
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
)

const (
	COMMAND_ARG            = "cmd"
	COMMAND_ARG_CODE       = "+code"
	COMMAND_ARG_EXPIRATION = "+expiration"
	COMMAND_ARG_IDENT      = "+ident"
	COMMAND_ARG_IN         = "+in"
	COMMAND_ARG_LIMIT      = "+limit"
	COMMAND_ARG_OUT        = "+out"
	COMMAND_ARG_PASSWORD   = "+password"
	COMMAND_ARG_SYSTEMS    = "+systems"
	COMMAND_ARG_TOKEN      = "+token"
	COMMAND_ARG_URL        = "+url"
	COMMAND_ADMIN_PASSWORD = "admin-password"
	COMMAND_CONFIG_GET     = "config-get"
	COMMAND_CONFIG_SET     = "config-set"
	COMMAND_HELP           = "help"
	COMMAND_LOGIN          = "login"
	COMMAND_LOGOUT         = "logout"
	COMMAND_USER_ADD       = "user-add"
	COMMAND_USER_REMOVE    = "user-remove"

	COMMAND_DEF_PASSWORD = "rdio-scanner"
	COMMAND_DEF_URL      = "http://localhost:3000/"
)

type Command struct {
	app        string
	code       string
	command    string
	expiration string
	ident      string
	in         string
	limit      string
	out        string
	password   string
	systems    string
	token      string
	tokenFile  string
	url        string
}

func NewCommand(baseDir string) *Command {
	app, _ := os.Executable()
	pass := os.Getenv("RDIO_ADMIN_PASSWORD")

	if pass == "" {
		pass = COMMAND_DEF_PASSWORD
	}

	return &Command{
		app:       filepath.Base(app),
		command:   COMMAND_HELP,
		password:  pass,
		tokenFile: baseDir + filepath.Base(app) + ".token",
		url:       COMMAND_DEF_URL,
	}
}

func (command *Command) Do(action string) {
	var err error

	i := 1

	readVal := func() string {
		i++
		if i < len(os.Args) {
			return os.Args[i]
		}
		return ""
	}

	for i < len(os.Args) {
		switch os.Args[i] {
		case COMMAND_ARG_CODE:
			command.code = readVal()

		case COMMAND_ARG_EXPIRATION:
			command.expiration = readVal()

		case COMMAND_ARG_IDENT:
			command.ident = readVal()

		case COMMAND_ARG_IN:
			command.in = readVal()

		case COMMAND_ARG_LIMIT:
			command.limit = readVal()

		case COMMAND_ARG_OUT:
			command.out = readVal()
			if !strings.HasSuffix(strings.ToLower(command.out), ".json") {
				command.out = command.out + ".json"
			}

		case COMMAND_ARG_PASSWORD:
			command.password = readVal()

		case COMMAND_ARG_SYSTEMS:
			command.systems = readVal()

		case COMMAND_ARG_TOKEN:
			command.tokenFile = readVal()

		case COMMAND_ARG_URL:
			command.url = readVal()

			if err != nil || !regexp.MustCompile(`^https?://`).Match([]byte(command.url)) {
				command.exitWithError(errors.New("invalid URL"))
			}
		}

		i++
	}

	if _, err := os.Stat(command.tokenFile); !os.IsNotExist(err) {
		if b, err := os.ReadFile(command.tokenFile); err == nil {
			command.token = string(b)
		} else {
			command.exitWithError(err)
		}
	}

	switch action {
	case COMMAND_CONFIG_GET:
		command.configGet()

	case COMMAND_CONFIG_SET:
		command.configSet()

	case COMMAND_LOGIN:
		command.login()

	case COMMAND_LOGOUT:
		command.logout()

	case COMMAND_ADMIN_PASSWORD:
		command.adminPassword()

	case COMMAND_USER_ADD:
		command.userAdd()

	case COMMAND_USER_REMOVE:
		command.userRemove()

	default:
		command.printUsage()
	}

	os.Exit(0)
}

func (command *Command) printUsage() {
	var prompt string

	switch runtime.GOOS {
	case "windows":
		prompt = "C:\\>"
	default:
		prompt = "$ ./"
	}

	fmt.Printf("\nAvailable Commands:\n\n")
	fmt.Printf("  %-11s – Change administrator password.\n\n", COMMAND_ADMIN_PASSWORD)
	fmt.Printf("    %-11s %s%s -%s %s %s <password>\n\n", "", prompt, command.app, COMMAND_ARG, COMMAND_ADMIN_PASSWORD, COMMAND_ARG_PASSWORD)
	fmt.Printf("  %-11s – Retrieve server's configuration.\n\n", COMMAND_CONFIG_GET)
	fmt.Printf("    %-11s %s%s -%s %s %s <file.json>\n\n", "", prompt, command.app, COMMAND_ARG, COMMAND_CONFIG_GET, COMMAND_ARG_OUT)
	fmt.Printf("  %-11s – Set server's configuration.\n\n", COMMAND_CONFIG_SET)
	fmt.Printf("    %-11s %s%s -%s %s %s <file.json>\n\n", "", prompt, command.app, COMMAND_ARG, COMMAND_CONFIG_SET, COMMAND_ARG_IN)
	fmt.Printf("  %-11s – Login to server.\n\n", COMMAND_LOGIN)
	if runtime.GOOS != "windows" {
		fmt.Printf("    %-11s $ RDIO_ADMIN_PASSWORD=<password> ./%s -%s %s\n", "", command.app, COMMAND_ARG, COMMAND_LOGIN)
	}
	fmt.Printf("    %-11s %s%s -%s %s %s <password>\n\n", "", prompt, command.app, COMMAND_ARG, COMMAND_LOGIN, COMMAND_ARG_PASSWORD)
	fmt.Printf("  %-11s – Logout from server.\n\n", COMMAND_LOGOUT)
	fmt.Printf("    %-11s %s%s -%s %s\n\n", "", prompt, command.app, COMMAND_ARG, COMMAND_LOGOUT)
	fmt.Printf("  %-11s – Add a user access.\n\n", COMMAND_USER_ADD)
	fmt.Printf("    %-11s %s%s -%s %s %s <ident> %s <code>\n\n", "", prompt, command.app, COMMAND_ARG, COMMAND_USER_ADD, COMMAND_ARG_IDENT, COMMAND_ARG_CODE)
	fmt.Printf("    %-11s Optional:\n\n", "")
	fmt.Printf("      %-11s %-11s <RFC3339 format>      – User access expiration date.\n", "", COMMAND_ARG_EXPIRATION)
	fmt.Printf("      %-11s %-11s <limit>               – Concurrent user access limit.\n", "", COMMAND_ARG_LIMIT)
	fmt.Printf("      %-11s %-11s <sysid1[,sysid2,...]> – Specific system access.\n\n", "", COMMAND_ARG_SYSTEMS)
	fmt.Printf("  %-11s – Remove a user access.\n\n", COMMAND_USER_REMOVE)
	fmt.Printf("    %-11s %s%s -%s %s %s <ident>\n\n", "", prompt, command.app, COMMAND_ARG, COMMAND_USER_REMOVE, COMMAND_ARG_IDENT)
	fmt.Printf("Global Options:\n\n")
	fmt.Printf("  %-11s – Session token keystore. Default is `.%s.token`.\n", COMMAND_ARG_TOKEN, command.app)
	fmt.Printf("  %-11s – Server remote address. Default is `%s`.\n\n", COMMAND_ARG_URL, COMMAND_DEF_URL)
}

func (command *Command) adminPassword() {
	if command.password == "" {
		command.exitWithError(fmt.Sprintf("Missing %s <password> arguments.", COMMAND_ARG_PASSWORD))
	}

	if body, err := command.writeBody(map[string]any{"newPassword": command.password}); err == nil {
		if res, err := command.submit(http.MethodPost, "/api/admin/password", body, true); err == nil {
			if res.StatusCode == http.StatusOK {
				fmt.Println("New admin password applied")
			} else {
				command.exitWithError(errors.New(res.Status))
			}
		}
	} else {
		command.exitWithError(err)
	}
}

func (command *Command) configGet() {
	if command.out == "" {
		command.exitWithError(fmt.Sprintf("Missing %s <file.json> arguments.", COMMAND_ARG_OUT))
	}

	if res, err := command.submit(http.MethodGet, "/api/admin/config", nil, true); err == nil {
		if res.StatusCode == http.StatusOK {
			if data, err := command.readBody(res.Body); err == nil {
				switch v := data.(type) {
				case map[string]any:
					switch v := v["config"].(type) {
					case map[string]any:
						if f, err := os.Create(command.out); err == nil {
							j := json.NewEncoder(f)
							j.SetIndent("", "  ")
							j.Encode(v)
							fmt.Printf("Server's configuration saved to %s.\n", command.out)
						} else {
							command.exitWithError(err)
						}
					}
				default:
					command.exitWithError(errors.New("invalid response"))
				}
			} else {
				command.exitWithError(err)
			}
		} else {
			command.exitWithError(errors.New(res.Status))
		}
	}
}

func (command *Command) configSet() {
	if command.in == "" {
		command.exitWithError(fmt.Sprintf("Missing %s <file.json> arguments.", COMMAND_ARG_IN))
	}

	if f, err := os.Open(command.in); err == nil {
		var d any
		json.NewDecoder(f).Decode(&d)
		switch v := d.(type) {
		case map[string]any:
			if body, err := command.writeBody(v); err == nil {
				if res, err := command.submit(http.MethodPut, "/api/admin/config", body, true); err == nil {
					if res.StatusCode == http.StatusOK {
						fmt.Println("Server's configuration applied.")
					} else {
						command.exitWithError(errors.New(res.Status))
					}
				}
			} else {
				command.exitWithError(err)
			}
		default:
			command.exitWithError("Invalid Data")
		}
	} else {
		command.exitWithError(err)
	}
}

func (command *Command) login() {
	if body, err := command.writeBody(map[string]any{"password": command.password}); err == nil {
		if res, err := command.submit(http.MethodPost, "/api/admin/login", body, false); err == nil {
			if res.StatusCode == http.StatusOK {
				if data, err := command.readBody(res.Body); err == nil {
					switch v := data.(type) {
					case map[string]any:
						switch v := v["token"].(type) {
						case string:
							command.token = v
							if err := os.WriteFile(command.tokenFile, []byte(command.token), 0600); err != nil {
								command.exitWithError(err)
							}
							fmt.Println("Logged in.")
						default:
							command.exitWithError(errors.New("no token in response"))
						}
					default:
						command.exitWithError(errors.New("invalid response"))
					}
				} else {
					command.exitWithError(err)
				}
			} else {
				command.exitWithError(res.Status)
			}
		} else {
			command.exitWithError(err)
		}
	} else {
		command.exitWithError(err)
	}
}

func (command *Command) logout() {
	if res, err := command.submit(http.MethodPost, "/api/admin/logout", nil, true); err == nil {
		switch res.StatusCode {
		case http.StatusOK, http.StatusUnauthorized:
			command.token = ""
			if _, err := os.Stat(command.tokenFile); !os.IsNotExist(err) {
				if err := os.Remove(command.tokenFile); err != nil {
					command.exitWithError(err)
				}
			}
			fmt.Println("Logged out.")
		default:
			command.exitWithError(res.Status)
		}
	} else {
		command.exitWithError(err)
	}
}

func (command *Command) userAdd() {
	if command.ident == "" {
		command.exitWithError(fmt.Sprintf("Missing %s <ident> arguments.", COMMAND_ARG_IDENT))
	}
	if command.code == "" {
		command.exitWithError(fmt.Sprintf("Missing %s <code> arguments.", COMMAND_ARG_CODE))
	}

	u := map[string]any{
		"code":    command.code,
		"ident":   command.ident,
		"systems": "*",
	}

	if command.expiration != "" {
		if t, err := time.Parse(time.RFC3339, command.expiration); err == nil {
			u["expiration"] = t.UTC().Format(time.RFC3339)
		} else {
			command.exitWithError(fmt.Sprintf("Invalid date format for %s", COMMAND_ARG_EXPIRATION))
		}
	}

	if command.limit != "" {
		if i, err := strconv.Atoi(command.limit); err == nil {
			u["limit"] = i
		} else {
			command.exitWithError(fmt.Sprintf("Invalid number for %s", COMMAND_ARG_LIMIT))
		}
	}

	if command.systems != "" {
		s := []int{}
		for _, v := range strings.Split(command.systems, ",") {
			if i, err := strconv.Atoi(v); err == nil {
				s = append(s, i)
			} else {
				command.exitWithError(fmt.Sprintf("The value '%s' is invalid for %s", v, COMMAND_ARG_SYSTEMS))
			}
		}
		if len(s) > 0 {
			systems := []map[string]any{}
			for _, i := range s {
				systems = append(systems, map[string]any{"id": i, "talkgroups": "*"})
			}
			u["systems"] = systems
		} else {
			command.exitWithError(fmt.Sprintf("Invalid system ids list for %s", COMMAND_ARG_SYSTEMS))
		}
	}
	if body, err := command.writeBody(u); err == nil {
		if res, err := command.submit(http.MethodPost, "/api/admin/user-add", body, true); err == nil {
			if res.StatusCode == http.StatusOK {
				fmt.Printf("User %s added.\n", command.ident)
			} else {
				command.exitWithError(errors.New(res.Status))
			}
		}
	} else {
		command.exitWithError(err)
	}
}

func (command *Command) userRemove() {
	if command.ident == "" {
		command.exitWithError(fmt.Sprintf("Missing %s <ident> arguments.", COMMAND_ARG_IDENT))
	}

	if body, err := command.writeBody(map[string]any{"ident": command.ident}); err == nil {
		if res, err := command.submit(http.MethodPost, "/api/admin/user-remove", body, true); err == nil {
			if res.StatusCode == http.StatusOK {
				fmt.Printf("User %s removed.\n", command.ident)
			} else {
				command.exitWithError(errors.New(res.Status))
			}
		}
	} else {
		command.exitWithError(err)
	}
}

func (c *Command) readBody(body io.ReadCloser) (data any, err error) {
	err = json.NewDecoder(body).Decode(&data)
	return data, err
}

func (c *Command) writeBody(data any) (body io.Reader, err error) {
	var b []byte
	if b, err = json.Marshal(data); err == nil {
		body = bytes.NewReader(b)
	}
	return body, err
}

func (c *Command) submit(method string, url string, body io.Reader, auth bool) (res *http.Response, err error) {
	var req *http.Request
	u := strings.TrimSuffix(c.url, "/") + url
	if req, err = http.NewRequest(method, u, body); err == nil {
		if auth {
			if c.token != "" {
				req.Header.Add("Authorization", c.token)
			} else {
				c.exitWithError("Not logged in.")
			}
		}
		if body != nil {
			req.Header.Add("Content-Type", "application/json")
		}
		res, err = http.DefaultClient.Do(req)
	}
	return res, err
}

func (c *Command) exitWithError(err any) {
	fmt.Printf("%v\n", err)
	os.Exit(1)
}
