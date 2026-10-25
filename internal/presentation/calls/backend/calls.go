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

// Package backend is the call history HTTP surface: the entry points a client uses to
// search the recorded calls, read one recorded call, and fetch one recording; see the
// call-history-over-http maintainer note.
package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/dto"
	"rdio-scanner/internal/presentation/common/httpaddr"
)

// accessCodeHeader carries a listener access code, never the Authorization header; see the
// listener-access-code-header maintainer note.
const accessCodeHeader = "X-Access-Code"

// CallsDeps is what the call history surface is built from: the two queries it answers with, the
// two credential checks it gates requests with, and the server's own clock and log.
type CallsDeps struct {
	// Call reads one recorded call in full.
	Call *application.GetCallQueryHandler

	// Search reads one page of the call history.
	Search *application.SearchCallsQueryHandler

	// Session is the administrator's session check: the same credential the administrator's own page
	// presents. An administrator session is not scoped by the listener access list.
	Session *application.CheckAdminSessionHandler

	// Resolve is what an access credential entitles a caller to. It is the hub-free half of the
	// listener access rule, and it is the rule that bounds guessing a code over a stateless surface.
	Resolve *application.ResolveAccessQueryHandler

	// Logs is where this surface files the requests it could not read, the way the administrator's
	// surface files its own.
	Logs ports.LogRepository

	// Clock is the server's clock, for the entries it files.
	Clock ports.Clock

	// Trusted names the proxies whose forwarding header this server honors, so the address the
	// guess budget is keyed on is the address the operator means.
	Trusted *httpaddr.Trusted
}

// Calls is the call history surface.
type Calls struct {
	call    *application.GetCallQueryHandler
	search  *application.SearchCallsQueryHandler
	session *application.CheckAdminSessionHandler
	resolve *application.ResolveAccessQueryHandler
	logs    ports.LogRepository
	clock   ports.Clock
	trusted *httpaddr.Trusted
}

// NewCalls builds the surface from the use cases it answers with.
func NewCalls(deps CallsDeps) *Calls {
	return &Calls{
		call:    deps.Call,
		search:  deps.Search,
		session: deps.Session,
		resolve: deps.Resolve,
		logs:    deps.Logs,
		clock:   deps.Clock,
		trusted: deps.Trusted,
	}
}

// Register hangs the three entry points on the routes the composition root is building; paths
// register without a method, and each entry point answers methods it does not serve with 405
// itself; see the method-agnostic-route-registration maintainer note.
func (calls *Calls) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/calls/search", calls.Search)
	mux.HandleFunc("/api/calls/{callId}", calls.Get)
	mux.HandleFunc("/api/calls/{callId}/audio", calls.Audio)
}

// caller is who is asking, and what they may see; see the caller-scope-semantics
// maintainer note.
type caller struct {
	// Scoped says the caller's view is limited by an access, and is the only state that makes the
	// server check a call against a scope at all.
	Scoped bool

	Access domain.Access
}

// Search answers one page of the call history.
func (calls *Calls) Search(w http.ResponseWriter, r *http.Request) {
	who, ok := calls.who(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	submitted, err := decodeObject(r)
	if err != nil {
		calls.reportUnreadableRequest(r, err)

		w.WriteHeader(http.StatusBadRequest)

		return
	}

	page, err := calls.search.Handle(r.Context(), application.SearchCallsQuery{
		Criteria: domain.ParseCallSearch(submitted),
		Scope:    calls.scopeOf(who),
	})
	if err != nil {
		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	// The criteria are echoed as they arrived, not as the query ran them: the echo says what this
	// server was asked, and the page says what it could do about it.
	writeJSON(w, dto.CallSearchPagePayload{Page: page, Criteria: submitted})
}

// Get answers one recorded call, without its recording.
func (calls *Calls) Get(w http.ResponseWriter, r *http.Request) {
	who, ok := calls.who(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	call, ok := calls.callInScope(w, r, who)
	if !ok {
		return
	}

	writeJSON(w, dto.CallMetadataPayload{Call: call})
}

// Audio answers one recording, as it is stored; see the recordings-served-as-stored-bytes
// maintainer note.
func (calls *Calls) Audio(w http.ResponseWriter, r *http.Request) {
	who, ok := calls.who(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	call, ok := calls.callInScope(w, r, who)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", call.AudioMime)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, call.AudioFilename))

	// The scope is checked above, before a single byte is read: the recording is the most
	// exfiltratable thing this surface carries, and a caller who may not see a call must not be
	// able to measure how big its recording is.
	http.ServeContent(w, r, call.AudioFilename, call.Timestamp, bytes.NewReader(call.Audio))
}

// who decides who is asking: the administrator credential is checked first, then a listener
// access code, then an anonymous caller on a restricted server; see the caller-scope-semantics
// maintainer note.
func (calls *Calls) who(r *http.Request) (caller, bool) {
	if calls.session.Handle(r.Context(), application.CheckAdminSessionQuery{Token: credential(r)}) {
		return caller{}, true
	}

	result, err := calls.resolve.Handle(r.Context(), application.ResolveAccessQuery{
		Address: httpaddr.Address(r, calls.trusted),
		Code:    r.Header.Get(accessCodeHeader),
	})
	if err != nil || result.Outcome.Refused() {
		return caller{}, false
	}

	// The anonymous access is the whole history, and the whole history is not a scope to check a
	// call against: a server with no access list does not scope anything.
	if result.Outcome == domain.AccessResolvedCode {
		return caller{Scoped: true, Access: result.Access}, true
	}

	return caller{}, true
}

// scopeOf is the scope the history search is restricted by, which is nothing at all for a caller
// whose view is not scoped.
func (calls *Calls) scopeOf(who caller) domain.AccessScope {
	if !who.Scoped {
		return domain.AccessScope{}
	}

	return who.Access.Scope
}

// callInScope reads one call and refuses a call its caller may not see, before answering
// anything about it, with the refusal a call that is not there gets; see the
// uniform-not-there-refusals maintainer note.
func (calls *Calls) callInScope(w http.ResponseWriter, r *http.Request, who caller) (domain.Call, bool) {
	id, err := strconv.ParseUint(r.PathValue("callId"), 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)

		return domain.Call{}, false
	}

	call, err := calls.call.Handle(r.Context(), application.GetCallByIdQuery{ID: id})
	if err != nil {
		if isNotFound(err) {
			w.WriteHeader(http.StatusNotFound)

			return domain.Call{}, false
		}

		w.WriteHeader(http.StatusExpectationFailed)

		return domain.Call{}, false
	}

	if who.Scoped && !who.Access.Grants(call) {
		w.WriteHeader(http.StatusNotFound)

		return domain.Call{}, false
	}

	return call, true
}

// reportUnreadableRequest files an unreadable request body only for an accepted credential,
// which keeps entry spam from anonymous callers out of the log.
func (calls *Calls) reportUnreadableRequest(r *http.Request, reason error) {
	if calls.logs == nil {
		return
	}

	_ = calls.logs.AppendLog(r.Context(), domain.LogEntry{
		DateTime: calls.clock.Now(),
		Level:    domain.LogLevelWarn,
		Source:   domain.LogSourceAccess,
		Message: fmt.Sprintf("the call history request to %s from %s could not be read: %v",
			r.URL.Path, httpaddr.Address(r, calls.trusted), reason),
	})
}

// credential is the administrator session credential the request carries, read the way the
// administrator's surface reads it: the raw token, with no scheme prefix.
func credential(r *http.Request) string {
	return r.Header.Get("Authorization")
}

func decodeObject(r *http.Request) (map[string]any, error) {
	var submitted map[string]any

	if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
		return nil, err
	}

	return submitted, nil
}

func isNotFound(err error) bool {
	return errors.Is(err, ports.ErrNotFound)
}

func writeJSON(w http.ResponseWriter, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	w.Write(body)
}
