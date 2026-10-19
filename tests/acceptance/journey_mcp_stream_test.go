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

// This is the assistant's session against a running server, and the bounds that apply to it.
//
// The server bounds every request it answers: 25 seconds, so a handler blocked on a database that
// does not answer is answered with a failure the administrator can read instead of a browser that
// spins. The assistant's endpoint carries a bound of its own beside it, because its work is not the
// administrator's: a composite capability runs several queries over a deployment with a full
// history, and the session's stream stays open for the life of the session.
//
// The journey runs the two against each other on a server that is really running: the administrator's
// request keeps its bound, and the assistant's session is outliving the request bound for as long as
// its operator is thinking.

// requestBound is the bound the composition root puts on an ordinary request. The journey names it
// here because the property it protects is a property about time: the bound is the administrator's,
// and the assistant's session is what must not be cut off by it.
const requestBound = 25 * time.Second

func TestAnAssistantSessionOutlivesTheRequestDeadlineTheAdministratorsRequestKeeps(t *testing.T) {
	srv := startedServerNamingTheAssistant(t)

	token := srv.loginAnswered(t)

	endpoint := httptest.NewServer(srv.server)

	// The endpoint is closed by a cleanup registered before the session's own: cleanups run in the
	// order they were registered reversed, so the assistant's stream is gone before the endpoint
	// waits for its connections.
	t.Cleanup(endpoint.Close)

	opened := time.Now()

	session := theAssistantConnected(t, endpoint.URL, token)

	// The session works, and the administrator's page works, before anything is locked.
	callTheAssistant(t, session, "list_systems")

	if rec := srv.authorizedRequest(t, http.MethodGet, "/api/admin/config", token, ""); rec.Code != http.StatusOK {
		t.Fatalf("the administrator's configuration page answered %d, want %d: the journey's server is running",
			rec.Code, http.StatusOK)
	}

	// A database that does not answer. The administrator's save is the request the request bound is
	// for: it is answered with a failure the administrator can read, inside the bound, and not held
	// open by a lock that is never going to clear.
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

	// The assistant's session is measured against the same bound, and is not what the bound is for.
	// The operator at the other end of a session takes minutes to read an answer and type a question;
	// the session the server opened for that operator is alive when the request bound has gone by,
	// and the stream carrying it is kept writing so the socket's own write bound does not close it.
	if elapsed := time.Since(opened); elapsed < requestBound+3*time.Second {
		time.Sleep(requestBound + 3*time.Second - elapsed)
	}

	callTheAssistant(t, session, "list_systems")
}

// startedServerNamingTheAssistant is the journey's server with the assistant's endpoint named in its
// configuration: the endpoint is served only when the operator asked for it, and this journey is
// about the endpoint, so this journey is the one that names it.
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

// theAssistantConnected is an assistant holding a session against the running server, signed in with
// the session the administrator's login handed out. The stream the session opens for the server's own
// messages is left open, because that is the stream the bounds are measured against.
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

// signedInWith is the transport that puts the administrator's session credential on every request the
// assistant's transport makes, the way the assistant's own configuration lines do.
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

// administratorAnswer is what the administrator's request answered, and how long the server took to
// answer it: the bound is measured by the time the request was held.
type administratorAnswer struct {
	status int
	took   time.Duration
	body   string

	err error
}

// awaitTheAdministratorAnswer waits for the administrator's request to answer. The bound under test
// is twenty-five seconds; a minute is the point at which the journey says the server never answered.
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
