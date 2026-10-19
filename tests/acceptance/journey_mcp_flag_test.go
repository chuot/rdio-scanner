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

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The operator's way of turning the assistant on is a word on the command line. Everything the feature
// proves about the endpoint can be proven against a server built inside a test process, where the
// configuration is a struct the test fills in by hand — and that is exactly why the flag itself needs a
// journey: the deployment's entry point parses the word, hands the server a configuration built a second
// time, and the switch is only real if it survives that handoff. A server started the way an operator
// starts it is the only place the whole path exists.

func TestTheOperatorTurnsTheAssistantOnFromTheCommandLine(t *testing.T) {
	base := t.TempDir()

	port := aFreeTCPPort(t)

	exe := buildServerBinary(t)

	_, _, _, addr := startTheServerWithTheAssistant(t, exe, base, port)

	// The endpoint answers its own path: a path this server does not know falls to the application
	// that answers every unmatched path. A deployment that never asked for the endpoint answers both
	// of them the same way, which is what the journey below measures against a server started without
	// the word.
	knocked := theAnswerTo(t, "http://"+addr, "/mcp")
	unmatched := theAnswerTo(t, "http://"+addr, "/a/path/that/is/not/an/endpoint")

	if knocked.Code == unmatched.Code && bytes.Equal(knocked.body, unmatched.body) {
		t.Fatalf("the assistant's endpoint was answered %d and %q, exactly what a path this server does "+
			"not know answers: the operator typed -mcp_server and the server never heard it",
			knocked.Code, firstLinesOf(knocked.body))
	}

	if bytes.Contains(knocked.body, []byte("<!doctype html")) {
		t.Fatalf("the assistant's endpoint was answered %d with a page — %q — and not with its own "+
			"protocol's answer: the endpoint the operator turned on is not the endpoint this server built",
			knocked.Code, firstLinesOf(knocked.body))
	}

	// And the assistant gets in with the credential the administrator's login hands out, and answers
	// the capabilities the surface publishes.
	token := loginTheRunningAdministrator(t, "http://"+addr)

	session := theAssistantAtTheEndpoint(t, "http://"+addr, token)
	t.Cleanup(func() { _ = session.Close() })

	listed, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("asking the assistant what it can do: %v", err)
	}

	names := []string{}

	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}

	if len(names) == 0 {
		t.Fatal("the assistant answered no capabilities at all: the endpoint is answering and empty")
	}

	if answered, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_systems",
		Arguments: map[string]any{},
	}); err != nil || answered.IsError {
		t.Fatalf("the assistant's capability was answered %v %v, wanting an answer: the endpoint the "+
			"operator turned on from the command line is not answering its capabilities", answered, err)
	}
}

// TestTheAssistantIsAbsentWhenTheOperatorAskedForNothing is the switch's other half against the
// deployment's entry point: a server started without the word answers the assistant's path exactly as it
// answers every path it does not know, which is the answer the deployments that never heard of the
// endpoint give today.
func TestTheAssistantIsAbsentWhenTheOperatorAskedForNothing(t *testing.T) {
	base := t.TempDir()

	port := aFreeTCPPort(t)

	exe := buildServerBinary(t)

	_, _, _, addr := startServerBinary(t, exe, base, port)

	waitForServerAnswer(t, addr)

	knocked := theAnswerTo(t, "http://"+addr, "/mcp")
	unmatched := theAnswerTo(t, "http://"+addr, "/a/path/that/is/not/an/endpoint")

	if knocked.Code != unmatched.Code || !bytes.Equal(knocked.body, unmatched.body) {
		t.Fatalf("the assistant's endpoint was answered %d and %q on a server the operator never asked for "+
			"it on, where a path this server does not know is answered %d and %q: the endpoint is opt-in, "+
			"and the way it is opt-in is that a deployment without the word does not have it",
			knocked.Code, firstLinesOf(knocked.body), unmatched.Code, firstLinesOf(unmatched.body))
	}
}

// startTheServerWithTheAssistant starts the built server the way the shutdown journey starts it, with
// the operator's word for the assistant added to the command line.
func startTheServerWithTheAssistant(t *testing.T, exe, base, port string) (*lockedBuffer, *exec.Cmd, chan error, string) {
	t.Helper()

	addr := "127.0.0.1:" + port

	cmd := exec.Command(exe, "-base_dir", base, "-listen", addr, "-mcp_server")

	console := &lockedBuffer{}

	cmd.Stdout = console
	cmd.Stderr = console

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the server binary with the assistant named: %v", err)
	}

	ended := make(chan error, 1)

	go func() {
		ended <- cmd.Wait()
	}()

	t.Cleanup(func() {
		// The journey ends the process with the server's own stop; this is the net under a journey
		// that failed early, so no built server outlives the test that started it.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}

		select {
		case <-ended:

		case <-time.After(30 * time.Second):
		}
	})

	waitForServerAnswer(t, addr)

	return console, cmd, ended, addr
}

// journeyAnswer is what the running server answered a path with.
type journeyAnswer struct {
	Code int
	body []byte
}

// theAnswerTo asks the running server for one path and keeps the whole answer.
func theAnswerTo(t *testing.T, base, path string) journeyAnswer {
	t.Helper()

	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("asking the running server for %s: %v", path, err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the answer to %s: %v", path, err)
	}

	return journeyAnswer{Code: resp.StatusCode, body: body}
}

// firstLinesOf is the opening of an answer, for a failure message an operator can read.
func firstLinesOf(body []byte) string {
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")

	if len(lines) > 3 {
		lines = lines[:3]
	}

	return strings.Join(lines, " | ")
}

// loginTheRunningAdministrator logs in over the running server's address the way an administrator logs
// in, and hands back the session token the login answer carries.
func loginTheRunningAdministrator(t *testing.T, base string) string {
	t.Helper()

	resp, err := http.Post(base+"/api/admin/login", "application/json",
		strings.NewReader(`{"password":"`+defaultAdminCredential+`"}`))
	if err != nil {
		t.Fatalf("logging in against the running server: %v", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the login answer: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login against the running server was answered %d: %s", resp.StatusCode, firstLinesOf(body))
	}

	var answered struct {
		Token string `json:"token"`
	}

	if err := json.Unmarshal(body, &answered); err != nil {
		t.Fatalf("the login answer is not the answer the login endpoint gives: %v", err)
	}

	if answered.Token == "" {
		t.Fatalf("the login answer carries no session: %s", firstLinesOf(body))
	}

	return answered.Token
}

// theAssistantAtTheEndpoint connects the assistant to the endpoint the running server is answering,
// presenting the administrator's session token the way the gate reads it.
func theAssistantAtTheEndpoint(t *testing.T, base, token string) *mcp.ClientSession {
	t.Helper()

	client := mcp.NewClient(&mcp.Implementation{Name: "rdio-scanner-journey", Version: "1"}, nil)

	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   base + "/mcp",
		HTTPClient: &http.Client{Transport: journeyCredentialRoundTripper{token: token}},
	}, nil)
	if err != nil {
		t.Fatalf("connecting the assistant to the endpoint the server is answering: %v", err)
	}

	return session
}

// journeyCredentialRoundTripper presents the session token in the Authorization header, the way the
// administrator's own page presents it.
type journeyCredentialRoundTripper struct {
	token string
}

func (transport journeyCredentialRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	sent := req.Clone(req.Context())
	sent.Header.Set("Authorization", transport.token)

	return http.DefaultTransport.RoundTrip(sent)
}
