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

package application

import (
	"context"
	"errors"

	"rdio-scanner/server-ng/internal/application/ports"
	"rdio-scanner/server-ng/internal/domain"
)

// ChangeAdminPasswordCommand replaces the administrator credential.
type ChangeAdminPasswordCommand struct {
	// CurrentCredential is the credential the caller offers as the one it is replacing, and
	// CurrentCredentialGiven says whether the payload named one at all.
	//
	// The distinction is not pedantry: the legacy server compares a credential it was given, and
	// skips the comparison entirely for a payload that named none. An absent field is therefore
	// not the same as an empty one — an empty credential is checked and refused, while no
	// credential at all changes the password without checking anything. Recorded as Q14 in
	// decision-preserved-quirks.md.
	CurrentCredential      string
	CurrentCredentialGiven bool

	// NewCredential is the credential the administrator wants on record from now on. The REST
	// layer has already refused a payload that does not name one as a string.
	NewCredential string
}

// ChangeAdminPasswordOutcome reports what the server decided about the change.
type ChangeAdminPasswordOutcome string

const (
	// AdminPasswordChanged means the new credential is on record and the server has said so in
	// its own log.
	AdminPasswordChanged ChangeAdminPasswordOutcome = "changed"

	// AdminPasswordRefused means the change did not happen. One outcome covers every reason it
	// did not — an empty new credential, a current credential that does not match, a credential
	// that could not be stored — because the legacy server answers all of them the same way and
	// logs all of them with the same words. Splitting them into separate outcomes would give the
	// REST layer a distinction to invent a status code for.
	AdminPasswordRefused ChangeAdminPasswordOutcome = "refused"
)

// ChangeAdminPasswordResult is the acknowledgement the change returns.
//
// PasswordNeedChange is what the administrator is told back: whether the credential now on
// record is one the server thinks should be replaced. It is read from the configuration *after*
// the change has been re-derived from what is stored, which is where the legacy server reads it
// from, and not from the credential that was just submitted.
type ChangeAdminPasswordResult struct {
	Outcome            ChangeAdminPasswordOutcome
	PasswordNeedChange bool
}

// ChangeAdminPasswordHandler changes the administrator credential.
type ChangeAdminPasswordHandler struct {
	options   ports.OptionsRepository
	passwords ports.PasswordVerifier
	hashes    ports.PasswordHasher
	logs      ports.LogRepository
	clock     ports.Clock
}

// NewChangeAdminPasswordHandler builds the handler from the options store and the credential
// ports.
func NewChangeAdminPasswordHandler(
	options ports.OptionsRepository,
	passwords ports.PasswordVerifier,
	hashes ports.PasswordHasher,
	logs ports.LogRepository,
	clock ports.Clock,
) *ChangeAdminPasswordHandler {
	return &ChangeAdminPasswordHandler{
		options:   options,
		passwords: passwords,
		hashes:    hashes,
		logs:      logs,
		clock:     clock,
	}
}

// Handle changes the credential, in the order the legacy server does.
//
// The order is the behavior: an empty new credential is refused before anything is compared, a
// current credential is compared only when one was offered, and the options are re-derived from
// what was stored before the server reports whether the password still needs changing.
func (handler *ChangeAdminPasswordHandler) Handle(ctx context.Context, cmd ChangeAdminPasswordCommand) (ChangeAdminPasswordResult, error) {
	refused := ChangeAdminPasswordResult{Outcome: AdminPasswordRefused}

	if len(cmd.NewCredential) == 0 {
		return handler.reject(ctx, errors.New("newPassword is empty"), refused)
	}

	options, err := handler.options.ReadOptions(ctx)
	if err != nil {
		return handler.reject(ctx, err, refused)
	}

	// Only a credential that was actually offered is compared. A payload that names none skips
	// the comparison, which is Q14 and is preserved rather than repaired: deciding what "no
	// current credential offered" should mean is a decision about the administrator contract.
	if cmd.CurrentCredentialGiven && !handler.passwords.Matches(ctx, options.AdminPasswordHash, cmd.CurrentCredential) {
		return handler.reject(ctx, errors.New("current password is invalid"), refused)
	}

	hash, err := handler.hashes.Hash(ctx, cmd.NewCredential)
	if err != nil {
		return handler.reject(ctx, err, refused)
	}

	options.AdminPasswordHash = hash
	options.AdminPasswordNeedChange = cmd.NewCredential == domain.DefaultAdminPassword

	if err := handler.options.WriteOptions(ctx, options); err != nil {
		return handler.reject(ctx, err, refused)
	}

	// The legacy server reads the options back from what it stored, and that read resets every
	// option to its default before applying the stored rows. An option stored under a name the
	// reader does not look for (Q1) therefore comes back as the default: changing the
	// administrator password quietly restores the 500 ms duplicate-detection window. Recorded as
	// Q15.
	reloaded, err := handler.options.ReloadOptions(ctx)
	if err != nil {
		return handler.reject(ctx, err, refused)
	}

	handler.writeLog(ctx, domain.LogLevelWarn, "admin password changed.")

	return ChangeAdminPasswordResult{
		Outcome:            AdminPasswordChanged,
		PasswordNeedChange: reloaded.AdminPasswordNeedChange,
	}, nil
}

// reject records a refused change and returns the reason to the caller.
//
// The server-log wording is fixed by the legacy server: whatever the actual reason was, it
// writes that the current password is invalid. That is misleading, and it is what an operator
// reading the log today sees, so it is reproduced rather than improved. The real reason is
// returned so the composition root can surface it somewhere the log is not.
func (handler *ChangeAdminPasswordHandler) reject(ctx context.Context, reason error, result ChangeAdminPasswordResult) (ChangeAdminPasswordResult, error) {
	handler.writeLog(ctx, domain.LogLevelError, "admin.passwordhandler.post: unable to change admin password, current password is invalid")

	return result, reason
}

func (handler *ChangeAdminPasswordHandler) writeLog(ctx context.Context, level, message string) {
	appendServerLog(ctx, handler.logs, handler.clock, level, message)
}
