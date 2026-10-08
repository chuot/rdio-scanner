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

	"rdio-scanner/internal/application/ports"
	"rdio-scanner/internal/domain"
)

// AdminSigningSecretOutcome reports what the server decided about its signing secret.
type AdminSigningSecretOutcome string

const (
	// AdminSigningSecretStored means the server had no secret it could sign with, made one, and put
	// it on record.
	AdminSigningSecretStored AdminSigningSecretOutcome = "stored"

	// AdminSigningSecretAlreadyOnRecord means the stored configuration already held a secret the
	// server can sign with, and nothing was written.
	AdminSigningSecretAlreadyOnRecord AdminSigningSecretOutcome = "already-on-record"

	// AdminSigningSecretFailed means the server could not read its configuration or could not put
	// the secret it made on record.
	AdminSigningSecretFailed AdminSigningSecretOutcome = "failed"
)

// EnsureAdminSigningSecretResult is the acknowledgement this step reports: what it decided about the
// secret. It carries no credential — the secret the server signs sessions with never leaves the
// handler.
type EnsureAdminSigningSecretResult struct {
	Outcome AdminSigningSecretOutcome
}

// EnsureAdminSigningSecretCommand asks the server to make sure it has a signing secret worth having.
//
// It carries nothing: the secret is the server's own, and nobody outside gets to name it.
type EnsureAdminSigningSecretCommand struct{}

// EnsureAdminSigningSecretHandler gives the server a signing secret and puts it on record.
//
// The legacy server generated a secret inside its options read whenever the stored one was unusable,
// and never wrote it down: a deployment that had never saved its configuration signed with an empty
// key, and a legacy database holding a short secret got a different key on every start, killing every
// token issued before it. The read no longer makes secrets; this step does, once, and stores what it
// makes. See docs/specs/FEAT-004-legacy-defect-repairs/decision-signing-secret-persisted.md.
//
// Making one is reported, because it is the fact behind every session that stopped working at that
// moment: the tokens issued under the secret it replaces no longer verify.
type EnsureAdminSigningSecretHandler struct {
	options ports.OptionsRepository
	secrets ports.SigningSecretStore
	logs    ports.LogRepository
	clock   ports.Clock
}

// NewEnsureAdminSigningSecretHandler builds the handler from the ports it needs: the configuration it
// reads its secret from, the record it stores a new one in, and the server log it reports a new one to.
func NewEnsureAdminSigningSecretHandler(
	options ports.OptionsRepository,
	secrets ports.SigningSecretStore,
	logs ports.LogRepository,
	clock ports.Clock,
) *EnsureAdminSigningSecretHandler {
	return &EnsureAdminSigningSecretHandler{
		options: options,
		secrets: secrets,
		logs:    logs,
		clock:   clock,
	}
}

// Handle reads the configuration the server is running with and, when it holds no secret the server
// can sign with, makes one and puts it on record.
//
// Whether a stored secret is usable, and what a usable one looks like, is the options' own rule; this
// handler only decides what to do about it and coordinates the write.
func (handler *EnsureAdminSigningSecretHandler) Handle(ctx context.Context, cmd EnsureAdminSigningSecretCommand) (EnsureAdminSigningSecretResult, error) {
	options, err := handler.options.ReadOptions(ctx)
	if err != nil {
		return EnsureAdminSigningSecretResult{Outcome: AdminSigningSecretFailed}, err
	}

	options, generated := options.WithSigningSecret()

	if !generated {
		return EnsureAdminSigningSecretResult{Outcome: AdminSigningSecretAlreadyOnRecord}, nil
	}

	if err := handler.secrets.StoreSecret(ctx, options.Secret); err != nil {
		return EnsureAdminSigningSecretResult{Outcome: AdminSigningSecretFailed}, err
	}

	// The entry is written after the secret is on record, because the entry says the secret exists: a
	// step that made one and could not store it has not generated the thing this line reports.
	appendServerLog(ctx, handler.logs, handler.clock, domain.LogLevelWarn, domain.LogSourceAdmin,
		"the server generated a new administrator signing secret, which ends every session that was open before it")

	return EnsureAdminSigningSecretResult{Outcome: AdminSigningSecretStored}, nil
}
