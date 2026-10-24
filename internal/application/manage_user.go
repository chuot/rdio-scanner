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

package application

import (
	"context"
	"fmt"

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// AddAccessUserCommand adds a listener access, or updates the one already configured under the
// same access code. The user arrives as an access, because that is what an access user is: a code
// a listener can present, with an identity, an expiry, a connection limit, and a scope.
type AddAccessUserCommand struct {
	User domain.Access
}

// RemoveAccessUserCommand removes an access user by its identity. The identity, not the access
// code, identifies a user for removal: removing by name removes every access that carries that
// name.
type RemoveAccessUserCommand struct {
	Ident string
}

// ManageAccessUserOutcome reports what the server did about an access user.
type ManageAccessUserOutcome string

const (
	// AccessUserSaved means the access list was stored and the open administrator connections
	// were told the configuration that resulted.
	AccessUserSaved ManageAccessUserOutcome = "saved"

	// AccessUserSaveFailed means the access list could not be stored, or could not be read back
	// after storing it.
	AccessUserSaveFailed ManageAccessUserOutcome = "failed"

	// AccessUserNotRemoved means the removal matched no access. It is reported separately because
	// the answer for it is a success with nothing done, and the REST layer has to reproduce that
	// without inventing a status the frozen contract does not have.
	AccessUserNotRemoved ManageAccessUserOutcome = "not removed"
)

// ManageAccessUserResult is the acknowledgement an access-user change returns.
type ManageAccessUserResult struct {
	Outcome ManageAccessUserOutcome
}

// AccessUserPorts are what changing an access user needs: the stores that own the configuration,
// the open administrator connections, the server log, and the clock. The whole configuration is
// needed rather than just the access list because an administrator connection is told the whole
// configuration when any part of it changes.
type AccessUserPorts struct {
	ConfigurationRepositories

	Logs  ports.LogRepository
	Admin ports.AdministratorSessions
	Clock ports.Clock
}

// AddAccessUserHandler adds or updates an access user.
type AddAccessUserHandler struct {
	repos ConfigurationRepositories
	logs  ports.LogRepository
	admin ports.AdministratorSessions
	clock ports.Clock
}

// NewAddAccessUserHandler builds the handler from the access-user ports.
func NewAddAccessUserHandler(ports AccessUserPorts) *AddAccessUserHandler {
	return &AddAccessUserHandler{
		repos: ports.ConfigurationRepositories,
		logs:  ports.Logs,
		admin: ports.Admin,
		clock: ports.Clock,
	}
}

// Handle stores the access and tells the administrator connections about the configuration that
// resulted. An access code that is already on the list is updated rather than added, which is
// what the administration interface relies on when an administrator edits a user.
func (handler *AddAccessUserHandler) Handle(ctx context.Context, cmd AddAccessUserCommand) (ManageAccessUserResult, error) {
	failed := ManageAccessUserResult{Outcome: AccessUserSaveFailed}

	accesses, err := handler.repos.Accesses.AllAccesses(ctx)
	if err != nil {
		handler.reportFailure(ctx, err)

		return failed, err
	}

	updated, _ := accesses.UpsertByCode(cmd.User)

	if err := handler.repos.Accesses.SaveAccesses(ctx, updated); err != nil {
		handler.reportFailure(ctx, err)

		return failed, err
	}

	// The stored list is read back, because the store is what gives a new access its identifier,
	// and an administrator connection is told the configuration the store actually holds.
	if _, err := handler.repos.Accesses.AllAccesses(ctx); err != nil {
		handler.reportFailure(ctx, err)

		return failed, err
	}

	if err := handler.announce(ctx); err != nil {
		handler.reportFailure(ctx, err)

		return failed, err
	}

	return ManageAccessUserResult{Outcome: AccessUserSaved}, nil
}

// RemoveAccessUserHandler removes an access user.
type RemoveAccessUserHandler struct {
	repos ConfigurationRepositories
	logs  ports.LogRepository
	admin ports.AdministratorSessions
	clock ports.Clock
}

// NewRemoveAccessUserHandler builds the handler from the access-user ports.
func NewRemoveAccessUserHandler(ports AccessUserPorts) *RemoveAccessUserHandler {
	return &RemoveAccessUserHandler{
		repos: ports.ConfigurationRepositories,
		logs:  ports.Logs,
		admin: ports.Admin,
		clock: ports.Clock,
	}
}

// Handle removes every access carrying the identity, or reports that none did. A removal that
// matched nothing stores nothing, announces nothing, and logs nothing, and is still a success:
// making it a failure would change what the administration interface has always been told.
func (handler *RemoveAccessUserHandler) Handle(ctx context.Context, cmd RemoveAccessUserCommand) (ManageAccessUserResult, error) {
	failed := ManageAccessUserResult{Outcome: AccessUserSaveFailed}

	accesses, err := handler.repos.Accesses.AllAccesses(ctx)
	if err != nil {
		handler.reportFailure(ctx, err)

		return failed, err
	}

	remaining, removed := accesses.RemoveAllByIdent(cmd.Ident)
	if !removed {
		return ManageAccessUserResult{Outcome: AccessUserNotRemoved}, nil
	}

	if err := handler.repos.Accesses.SaveAccesses(ctx, remaining); err != nil {
		handler.reportFailure(ctx, err)

		return failed, err
	}

	if _, err := handler.repos.Accesses.AllAccesses(ctx); err != nil {
		handler.reportFailure(ctx, err)

		return failed, err
	}

	if err := handler.announce(ctx); err != nil {
		handler.reportFailure(ctx, err)

		return failed, err
	}

	return ManageAccessUserResult{Outcome: AccessUserSaved}, nil
}

// announce tells the open administrator connections the configuration that resulted. Only they
// are told: a listener keeps the scope it was given when it connected until it reconnects.
func (handler *AddAccessUserHandler) announce(ctx context.Context) error {
	return announceConfigurationToAdministrators(ctx, handler.repos, handler.admin)
}

// announce tells the same audience for a removal.
func (handler *RemoveAccessUserHandler) announce(ctx context.Context) error {
	return announceConfigurationToAdministrators(ctx, handler.repos, handler.admin)
}

// announceConfigurationToAdministrators reads the configuration in effect and hands it to the
// open administrator connections.
func announceConfigurationToAdministrators(
	ctx context.Context,
	repos ConfigurationRepositories,
	admin ports.AdministratorSessions,
) error {
	configuration, err := readConfiguration(ctx, repos)
	if err != nil {
		return err
	}

	admin.NotifyConfigurationChanged(ctx, configuration)

	return nil
}

// reportFailure records why an access-user change did not complete: what the server was trying
// to do, then the failure's own text, which names the statement that failed.
func (handler *AddAccessUserHandler) reportFailure(ctx context.Context, err error) {
	handler.writeLog(ctx, fmt.Sprintf("the administrator user could not be added: %v", err))
}

// reportFailure records why a removal did not complete.
func (handler *RemoveAccessUserHandler) reportFailure(ctx context.Context, err error) {
	handler.writeLog(ctx, fmt.Sprintf("the administrator user could not be removed: %v", err))
}

func (handler *AddAccessUserHandler) writeLog(ctx context.Context, message string) {
	appendServerLog(ctx, handler.logs, handler.clock, domain.LogLevelError, domain.LogSourceAdmin, message)
}

func (handler *RemoveAccessUserHandler) writeLog(ctx context.Context, message string) {
	appendServerLog(ctx, handler.logs, handler.clock, domain.LogLevelError, domain.LogSourceAdmin, message)
}
