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

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"rdio-scanner/internal/application"
	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
	"rdio-scanner/internal/presentation/common/dto"
	"rdio-scanner/internal/presentation/common/httpaddr"
	"rdio-scanner/internal/presentation/common/upgrade"
)

// AdminDeps are the use cases the administrator endpoints answer with, plus what these
// endpoints report that no use case decided: the version that answers, the container
// answer, and what happened to a request that never reached a use case at all.
type AdminDeps struct {
	AddUser  *application.AddAccessUserHandler
	Config   *application.GetConfigHandler
	Login    *application.AdminLoginHandler
	Logout   *application.AdminLogoutHandler
	Password *application.ChangeAdminPasswordHandler

	// SearchLogs reads the server's own log view, which is what the administrator's log page asks for.
	SearchLogs *application.SearchLogsHandler

	// Logs is where this surface files what it did with a request: a request it could not
	// read used to leave no trace at all, and an operator could not tell a refused change
	// from a request that made no sense on its way in.
	Logs  ports.LogRepository
	Clock ports.Clock

	// Configuration saves the whole configuration an administrator submits at once, which is the older
	// of the two ways the administrator's page saves and the one command-line tooling uses.
	Configuration *application.UpdateConfigurationHandler

	// ConfigurationSection saves one section, which is what the administrator's page does.
	ConfigurationSection *application.UpdateConfigurationSectionHandler

	// SystemSubSection saves one level of one system - its sites, talkgroups or units -
	// which is what the administrator's page does since each level is saved on its own.
	SystemSubSection *application.UpdateSystemSubSectionHandler

	// ConfigSocket is the administrator's configuration socket: the whole-configuration
	// path hands it a request that asks to become a websocket connection. It is an
	// http.Handler because this package has no business knowing what a socket is, and the
	// composition root wires the real one; left unset, the path answers only as an endpoint.
	ConfigSocket http.Handler

	RemoveUser *application.RemoveAccessUserHandler
	Session    *application.CheckAdminSessionHandler

	// Trusted is the proxy list the operator named: the connections whose forwarding
	// header this surface honors when it names a request, the same list the login budget
	// is keyed on, so the address an operator reads and the address the limiter counts are
	// one address.
	Trusted *httpaddr.Trusted

	// Docker is whether the server thinks it is running in a container. The answer cannot
	// change while the server runs, so the composition root asks once and hands the answer
	// here; the legacy server asked the environment each time it answered.
	Docker bool

	// BaseDir is the folder the server was told to serve, which the status read names beside the version
	// and the container answer. An empty string is the operator naming none, and is answered as one.
	BaseDir string

	// Status answers what this server is doing right now: the whole of what the administrator's landing
	// page is built from. It is the only read in the server that answers live state, and it is read-only.
	Status *application.GetRuntimeStatusHandler

	Version string
}

// Admin is the administrator's own surface: the endpoints an administrator's page and
// the command-line tooling talk to. It decides nothing: every endpoint reads a
// credential, hands what was submitted to the use case that owns the decision, and
// turns the outcome into the status code and body the frozen contract fixes. Each
// endpoint writes down its own order of those steps rather than sharing a helper; see
// the administrator-endpoint-gate-order maintainer note.
type Admin struct {
	addUser              *application.AddAccessUserHandler
	config               *application.GetConfigHandler
	configSocket         http.Handler
	configuration        *application.UpdateConfigurationHandler
	configurationSection *application.UpdateConfigurationSectionHandler
	systemSubSection     *application.UpdateSystemSubSectionHandler
	docker               bool
	baseDir              string
	logout               *application.AdminLogoutHandler
	searchLogs           *application.SearchLogsHandler
	status               *application.GetRuntimeStatusHandler
	login                *application.AdminLoginHandler
	password             *application.ChangeAdminPasswordHandler
	removeUser           *application.RemoveAccessUserHandler
	session              *application.CheckAdminSessionHandler
	version              string

	// logs and clock are how this surface reports what it did with a request: reading the
	// log view and writing to it are different jobs, kept separate from searchLogs, and
	// the administrator's page does both.
	logs  ports.LogRepository
	clock ports.Clock

	// trusted is the proxy list the operator named, handed down from Deps.
	trusted *httpaddr.Trusted
}

// NewAdmin builds the administrator surface from the use cases it answers with.
func NewAdmin(deps AdminDeps) *Admin {
	return &Admin{
		addUser:              deps.AddUser,
		config:               deps.Config,
		configSocket:         deps.ConfigSocket,
		configuration:        deps.Configuration,
		configurationSection: deps.ConfigurationSection,
		systemSubSection:     deps.SystemSubSection,
		docker:               deps.Docker,
		baseDir:              deps.BaseDir,
		logout:               deps.Logout,
		searchLogs:           deps.SearchLogs,
		status:               deps.Status,
		login:                deps.Login,
		password:             deps.Password,
		removeUser:           deps.RemoveUser,
		session:              deps.Session,
		version:              deps.Version,
		logs:                 deps.Logs,
		clock:                deps.Clock,
		trusted:              deps.Trusted,
	}
}

// Register installs the administrator routes. The paths and the methods they answer
// are part of the frozen contract, and the whole-configuration path and the section
// subtree hanging off it are two different handlers, exactly as in the legacy server.
// The whole-configuration path is also where the administrator's configuration socket
// is opened: an upgrade request is handed over before any credential is asked for; see
// the administrator-endpoint-gate-order maintainer note.
func (admin *Admin) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/admin/alerts", admin.Alerts)
	mux.HandleFunc("/api/admin/config", admin.Config)
	mux.HandleFunc("/api/admin/config/", admin.Section)

	// The per-level endpoints hang off the systems path, registered as six specific
	// patterns rather than a subtree: a subtree pattern would redirect the systems
	// section path itself, and it keeps answering as a section.
	for _, level := range []string{"sites", "talkgroups", "units"} {
		mux.HandleFunc("GET /api/admin/config/systems/{systemId}/"+level, admin.SubSection)
		mux.HandleFunc("PUT /api/admin/config/systems/{systemId}/"+level, admin.SubSection)
	}
	mux.HandleFunc("/api/admin/login", admin.Login)
	mux.HandleFunc("/api/admin/logs", admin.Logs)
	mux.HandleFunc("/api/admin/logout", admin.Logout)
	mux.HandleFunc("/api/admin/password", admin.Password)
	mux.HandleFunc("/api/admin/status", admin.Status)
	mux.HandleFunc("/api/admin/user-add", admin.UserAdd)
	mux.HandleFunc("/api/admin/user-remove", admin.UserRemove)
}

// Login starts an administrator session. The credential is counted before it is looked
// at, which is the use case's decision and not this endpoint's; what matters here is
// that the address handed to it is the address the server names, so the attempts
// counted here and the warning about a guessed access code on a listener connection
// are about the same machine.
func (admin *Admin) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	submitted, err := decodeObject(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	credential, _ := submitted["password"].(string)

	result, err := admin.login.Handle(r.Context(), application.AdminLoginCommand{
		Credential: credential,
		RemoteAddr: httpaddr.Address(r, admin.trusted),
	})
	if err != nil {
		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	switch result.Outcome {
	case application.AdminLoginAccepted:
		writeJSON(w, dto.AdminLoginReply{
			PasswordNeedChange: result.PasswordNeedChange,
			Token:              result.Token,
		})

	case application.AdminLoginAttemptLimit, application.AdminLoginInvalidCredential:
		w.WriteHeader(http.StatusUnauthorized)

	default:
		w.WriteHeader(http.StatusExpectationFailed)
	}
}

// Logout ends the administrator session the presented credential stands for. The use
// case is asked directly: the legacy server checked the credential and then removed
// it, and a session that is refused is not ended.
func (admin *Admin) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	result, err := admin.logout.Handle(r.Context(), application.AdminLogoutCommand{
		Token: credential(r),
	})
	if err != nil {
		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	switch result.Outcome {
	case application.AdminLogoutAccepted:
		w.WriteHeader(http.StatusOK)

	case application.AdminLogoutRefused:
		w.WriteHeader(http.StatusUnauthorized)

	default:
		w.WriteHeader(http.StatusExpectationFailed)
	}
}

// Config reads or saves the whole configuration, or hands the request to the
// configuration socket. The upgrade header is looked at first, before anything is
// asked for, and the socket authenticates by the frames it sends rather than by a
// credential in a header; see the administrator-socket-trust maintainer note. For an
// ordinary request the session is asked before the method, so a request asking
// something this endpoint does not answer is told the session is gone rather than that
// the question is wrong.
func (admin *Admin) Config(w http.ResponseWriter, r *http.Request) {
	if admin.configSocket != nil && upgrade.Wanted(r) {
		admin.configSocket.ServeHTTP(w, r)

		return
	}

	if !admin.sessionIsOpen(r) {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	switch r.Method {
	case http.MethodGet:
		admin.sendConfig(r.Context(), w)

	case http.MethodPut:
		submitted, err := decodeObject(r)
		if err != nil {
			admin.reportUnreadableRequest(r, err)

			w.WriteHeader(http.StatusBadRequest)

			return
		}

		changes, err := admin.submittedConfiguration(r.Context(), submitted)
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)

			return
		}

		_, err = admin.configuration.Handle(r.Context(), application.UpdateConfigurationCommand{
			Changes: changes,
		})
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)

			return
		}

		// A whole-configuration save answers with the configuration either way: a section
		// the server could not store is already in the log view, so the administrator is
		// shown what the server is running with rather than a failure that would throw away
		// the sections that did save.
		admin.sendConfig(r.Context(), w)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// Alerts answers with the nine alert sequences the server carries. The method is
// decided before the session, the reverse of the log view and what the legacy server
// does: a method this endpoint does not answer is refused without ever looking at the
// credential.
func (admin *Admin) Alerts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !admin.sessionIsOpen(r) {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		writeJSON(w, dto.Alerts())

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// Logs reads the server's own log view. The session is asked before the method, as the
// legacy server asks it.
func (admin *Admin) Logs(w http.ResponseWriter, r *http.Request) {
	if !admin.sessionIsOpen(r) {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	switch r.Method {
	case http.MethodPost:
		submitted, err := decodeObject(r)
		if err != nil {
			admin.reportUnreadableRequest(r, err)

			w.WriteHeader(http.StatusBadRequest)

			return
		}

		search := domain.ParseLogSearch(submitted)

		// The one criterion this endpoint refuses instead of ignoring: a subsystem the
		// server does not have is a caller asking about a part of the server that does not
		// exist, and an empty log view turns a typo into an operator concluding that
		// nothing happened.
		if search.SourceWasRejected() {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		page, err := admin.searchLogs.Handle(r.Context(), application.SearchLogsQuery{
			Search: search,
		})
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)

			return
		}

		writeJSON(w, dto.AdminLogsPage(page, submitted))

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// Status reads what this server is doing right now, the whole of what the
// administrator's landing page is built from. The credential is settled before the
// method, and the method before anything is read; a failed read is answered with 417
// and nothing in the body rather than a zeroed snapshot that would read as healthy and
// idle. This path is not an upgrade path. See the status-read-protection maintainer
// note.
func (admin *Admin) Status(w http.ResponseWriter, r *http.Request) {
	if !admin.sessionIsOpen(r) {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	status, err := admin.status.Handle(r.Context(), application.GetRuntimeStatusQuery{})
	if err != nil {
		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	writeJSON(w, dto.AdminStatus(status, admin.version, admin.docker, admin.baseDir))
}

// Password changes the credential an administrator logs in with.
func (admin *Admin) Password(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if !admin.sessionIsOpen(r) {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		submitted, err := decodeObject(r)
		if err != nil {
			admin.reportUnreadableRequest(r, err)

			w.WriteHeader(http.StatusBadRequest)

			return
		}

		// A new credential has to be named: the legacy server looks for a string under that key and
		// answers a request that names none with the status it uses for a body it cannot act on.
		newCredential, ok := submitted["newPassword"].(string)
		if !ok {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		// A current credential that was not offered at all is a different submission from
		// one that was offered empty, and the use case decides what each means; the
		// endpoint's job is to report which one was submitted.
		currentCredential, offered := submitted["currentPassword"].(string)

		result, err := admin.password.Handle(r.Context(), application.ChangeAdminPasswordCommand{
			CurrentCredential:      currentCredential,
			CurrentCredentialGiven: offered,
			NewCredential:          newCredential,
		})
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)

			return
		}

		switch result.Outcome {
		case application.AdminPasswordChanged:
			writeJSON(w, dto.AdminPasswordReply{PasswordNeedChange: result.PasswordNeedChange})

		default:
			w.WriteHeader(http.StatusExpectationFailed)
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// UserAdd adds an access code.
func (admin *Admin) UserAdd(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if !admin.sessionIsOpen(r) {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		submitted, err := decodeObject(r)
		if err != nil {
			admin.reportUnreadableRequest(r, err)

			w.WriteHeader(http.StatusBadRequest)

			return
		}

		result, err := admin.addUser.Handle(r.Context(), application.AddAccessUserCommand{
			User: domain.NewAccess().ApplyMap(submitted),
		})
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)

			return
		}

		switch result.Outcome {
		case application.AccessUserSaved:
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusExpectationFailed)
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// UserRemove removes every access code sharing the name the administrator gave. A
// name that matches nothing is answered with success, as the legacy server has always
// answered it: the page reloads the list either way, and a failure would show an error
// for a list that is already correct.
func (admin *Admin) UserRemove(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if !admin.sessionIsOpen(r) {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		submitted, err := decodeObject(r)
		if err != nil {
			admin.reportUnreadableRequest(r, err)

			w.WriteHeader(http.StatusBadRequest)

			return
		}

		result, err := admin.removeUser.Handle(r.Context(), application.RemoveAccessUserCommand{
			Ident: domain.NewAccess().ApplyMap(submitted).Ident,
		})
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)

			return
		}

		switch result.Outcome {
		case application.AccessUserSaved, application.AccessUserNotRemoved:
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusExpectationFailed)
		}

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// sessionIsOpen asks whether the credential the request carries stands for a session this server still
// accepts. Asking costs nothing, which is what lets an administrator's page ask it on every request.
func (admin *Admin) sessionIsOpen(r *http.Request) bool {
	return admin.session.Handle(r.Context(), application.CheckAdminSessionQuery{
		Token: credential(r),
	})
}

// sendConfig answers with the whole configuration, which is what the administrator's page reloads after a
// save and what it shows when it opens.
func (admin *Admin) sendConfig(ctx context.Context, w http.ResponseWriter) {
	configuration, err := admin.config.Handle(ctx, application.GetConfigQuery{})
	if err != nil {
		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	writeJSON(w, dto.AdminConfigReply{
		Config:             adminConfigPayload(configuration, admin.version),
		PasswordNeedChange: configuration.Options.AdminPasswordNeedChange,
		Docker:             admin.docker,
	})
}

// submittedConfiguration turns what was submitted into the sections a save replaces;
// see the configuration-save-semantics maintainer note.
func (admin *Admin) submittedConfiguration(ctx context.Context, submitted map[string]any) (application.ConfigurationChanges, error) {
	changes := application.ConfigurationChanges{}

	if entries, ok := submitted["access"].([]any); ok {
		accesses := domain.ParseAccesses(entries)

		changes.Access = &accesses
	}

	if entries, ok := submitted["apikeys"].([]any); ok {
		apikeys := domain.ParseApikeys(entries)

		changes.Apikeys = &apikeys
	}

	if entries, ok := submitted["dirwatch"].([]any); ok {
		dirwatches := domain.ParseDirwatches(entries)

		changes.Dirwatches = &dirwatches
	}

	if entries, ok := submitted["downstreams"].([]any); ok {
		downstreams := domain.ParseDownstreams(entries)

		changes.Downstreams = &downstreams
	}

	if entries, ok := submitted["groups"].([]any); ok {
		groups := domain.ParseGroups(entries)

		changes.Groups = &groups
	}

	if entries, ok := submitted["systems"].([]any); ok {
		systems := domain.ParseSystems(entries)

		changes.Systems = &systems
	}

	if entries, ok := submitted["tags"].([]any); ok {
		tags := domain.ParseTags(entries)

		changes.Tags = &tags
	}

	if settings, ok := submitted["options"].(map[string]any); ok {
		current, err := admin.currentOptions(ctx)
		if err != nil {
			return changes, err
		}

		options := current.ApplyMap(settings)

		changes.Options = &options
	}

	return changes, nil
}

// currentOptions reads the settings the server is running with, which is what a
// submitted set of settings is merged into; see the configuration-save-semantics
// maintainer note.
func (admin *Admin) currentOptions(ctx context.Context) (domain.Options, error) {
	configuration, err := admin.config.Handle(ctx, application.GetConfigQuery{})
	if err != nil {
		return domain.Options{}, err
	}

	return configuration.Options, nil
}

// adminConfigPayload is the whole configuration as an administrator is shown it.
func adminConfigPayload(configuration domain.Configuration, version string) dto.AdminConfigPayload {
	return dto.AdminConfigPayload{
		Accesses:    configuration.Access,
		Apikeys:     configuration.Apikeys,
		Dirwatches:  configuration.Dirwatches,
		Downstreams: configuration.Downstreams,
		Snapshot: ports.ConfigSnapshot{
			Groups:  configuration.Groups,
			Tags:    configuration.Tags,
			Systems: configuration.Systems,
			Options: configuration.Options,
		},
		Version: version,
	}
}

// credential is the administrator credential exactly as the request carried it. The frozen contract puts
// it raw in the Authorization header, with no scheme prefix, and nothing here strips anything.
func credential(r *http.Request) string {
	return r.Header.Get("Authorization")
}

// decodeObject reads a submitted body as a set of fields. A body that is not a set of
// fields is answered with 400; a body of null is a submission that names nothing, as
// the legacy server's decoder reads it.
func decodeObject(r *http.Request) (map[string]any, error) {
	var submitted map[string]any

	if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
		return nil, err
	}

	return submitted, nil
}

// reportUnreadableRequest files a request from an administrator that this server could
// not read. It is called only from endpoints that have already accepted the
// credential, which is what keeps one entry per unreadable body safe to write; the
// login endpoint reads a body before it has accepted anything and files nothing here,
// its failures being filed by the credential use case that counts attempts per
// address. The reason filed is the decoder's own, because that is the thing an
// operator acts on when the endpoint answers both shapes with the same 400.
func (admin *Admin) reportUnreadableRequest(r *http.Request, reason error) {
	if admin.logs == nil {
		return
	}

	if err := admin.logs.AppendLog(r.Context(), domain.LogEntry{
		DateTime: admin.clock.Now(),
		Level:    domain.LogLevelWarn,
		Source:   domain.LogSourceAdmin,
		Message: fmt.Sprintf("the administrator's request to %s from %s could not be read: %v",
			r.URL.Path, httpaddr.Address(r, admin.trusted), reason),
	}); err != nil {
		// A request this server could not read is still a request it refused the same way.
		return
	}
}

// writeJSON answers with a payload, and with the failure status the frozen contract
// fixes when the payload cannot be written at all; no Content-Type is named, as the
// legacy server never named one; see the administrator-answers-carry-no-content-type
// maintainer note.
func writeJSON(w http.ResponseWriter, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusExpectationFailed)

		return
	}

	w.Write(body)
}
