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
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"rdio-scanner/internal/app"
)

// the assistant session journey weighs the request bound against a long-lived session:
// the endpoint needs its own bound for composite capabilities and open streams.

// requestBound is the bound the composition root puts on an ordinary request, named
// here because the journey's property is about time: the assistant's session must
// not be cut off by the administrator's bound.
const requestBound = 25 * time.Second

func TestAnAssistantSessionOutlivesTheRequestDeadlineTheAdministratorsRequestKeeps(t *testing.T) {
	srv := startedServerNamingTheAssistant(t)

	token := srv.loginAnswered(t)

	endpoint := httptest.NewServer(srv.server)

	// this cleanup is registered before the session's own: cleanups run in reverse,
	// so the assistant's stream is gone before the endpoint waits on connections.
	t.Cleanup(endpoint.Close)

	opened := time.Now()

	session := theAssistantConnected(t, endpoint.URL, token)

	// the session works, and the administrator's page works, before anything is locked.
	callTheAssistant(t, session, "list_systems")

	if rec := srv.authorizedRequest(t, http.MethodGet, "/api/admin/config", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("the administrator's configuration page answered %d, want %d: the journey's server is running",
			rec.Code, http.StatusOK)
	}

	// a database that does not answer: the administrator's save is the request the bound
	// is for, answered with a readable failure inside the bound, not held open forever.
	locked := lockTheServerLog(t, srv.base)

	administered := make(chan administratorAnswer, 1)

	go func() {
		started := time.Now()

		req, err := http.NewRequest(http.MethodPut, endpoint.URL+"/api/admin/config/options",
			strings.NewReader(`{"maxClients":60}`))
		if err != nil {
			administered <- administratorAnswer{err: err}

			return
		}

		req.Header.Set("Authorization", token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			administered <- administratorAnswer{err: err}

			return
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			administered <- administratorAnswer{err: err}

			return
		}

		administered <- administratorAnswer{status: resp.StatusCode, took: time.Since(started), body: string(body)}
	}()

	answer := awaitTheAdministratorAnswer(t, administered)

	if answer.status < http.StatusBadRequest {
		t.Fatalf("the administrator's request over a database that does not answer was answered %d in %v, "+
			"want a failure the administrator can read:\n%s", answer.status, answer.took, answer.body)
	}

	if answer.took > requestBound {
		t.Fatalf("the administrator's request was held %v, want it answered by the request bound of %v: a "+
			"database that does not answer must not hold an administrator's request open", answer.took, requestBound)
	}

	if err := locked.Close(); err != nil {
		t.Fatalf("releasing the database: %v", err)
	}

	// the assistant's session is not what the request bound is for: an operator thinks
	// for minutes, so the session outlives the bound and keeps its stream writing so
	// the socket's write bound does not close it.
	if elapsed := time.Since(opened); elapsed < requestBound+3*time.Second {
		time.Sleep(requestBound + 3*time.Second - elapsed)
	}

	callTheAssistant(t, session, "list_systems")
}

// startedServerNamingTheAssistant is the journey's server with the assistant's
// endpoint named in its configuration, the endpoint this journey measures.
func startedServerNamingTheAssistant(t *testing.T) *journeyServer {
	t.Helper()

	base := t.TempDir()

	config := journeyConfigIn(base)
	config.McpServer = true

	srv, err := app.New(config)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	t.Cleanup(func() { srv.Close() })

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	return &journeyServer{server: srv, base: base, db: openTestDatabase(t, base)}
}

// theAssistantConnected is an assistant holding a session against the running server,
// signed in with the administrator's session credential, its message stream left
// open: that stream is what the bounds are measured against.
func theAssistantConnected(t *testing.T, endpoint, token string) *mcp.ClientSession {
	t.Helper()

	client := mcp.NewClient(&mcp.Implementation{Name: "acceptance-journey", Version: "1"}, nil)

	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   endpoint + "/mcp",
		HTTPClient: &http.Client{Transport: signedInWith(token)},
	}, nil)
	if err != nil {
		t.Fatalf("connecting the assistant to the running server: %v", err)
	}

	t.Cleanup(func() { _ = session.Close() })

	return session
}

// signedInWith is the transport that puts the administrator's session credential on
// every assistant request, the way the assistant's configuration lines do.
func signedInWith(token string) http.RoundTripper {
	return credentialRoundTripper{token: token}
}

type credentialRoundTripper struct {
	token string
}

func (transport credentialRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	sent := req.Clone(req.Context())
	sent.Header.Set("Authorization", transport.token)

	return http.DefaultTransport.RoundTrip(sent)
}

// callTheAssistant asks the running server for one capability and insists it answers.
func callTheAssistant(t *testing.T, session *mcp.ClientSession, tool string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	answered, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}

	if answered.IsError {
		text := ""

		for _, content := range answered.Content {
			if line, ok := content.(*mcp.TextContent); ok {
				text += line.Text
			}
		}

		t.Fatalf("%s answered a failure on a running server: %s", tool, text)
	}
}

// administratorAnswer is what the administrator's request answered and how long the
// server took: the bound is measured by the time the request was held.
type administratorAnswer struct {
	status int
	took   time.Duration
	body   string

	err error
}

// awaitTheAdministratorAnswer waits for the administrator's request to answer: the
// bound under test is 25 seconds, a minute is when the journey calls it never.
func awaitTheAdministratorAnswer(t *testing.T, administered chan administratorAnswer) administratorAnswer {
	t.Helper()

	select {
	case answer := <-administered:
		if answer.err != nil {
			t.Fatalf("the administrator's request: %v", answer.err)
		}

		return answer
	case <-time.After(time.Minute):
		t.Fatal("the administrator's request never answered: the request bound answers every request it " +
			"bounds with a failure the administrator can read")

		return administratorAnswer{}
	}
}
