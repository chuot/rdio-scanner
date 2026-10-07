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

	"rdio-scanner/server-ng/internal/application/ports"
)

// CheckAdminSessionQuery asks whether a credential presented to an administrator endpoint stands for a
// session this server still accepts.
//
// The credential arrives exactly as the request carried it: the frozen contract puts it raw in the
// Authorization header, with no scheme prefix, and nothing strips anything.
type CheckAdminSessionQuery struct {
	Token string
}

// CheckAdminSessionHandler answers that question without changing what it asks about.
//
// Every administrator endpoint asks it before it does anything else, and asking must be free: a session
// that is refused is not ended, and a session that is accepted is not used up. An endpoint that answered
// the question by consuming the credential would end an administrator's session the first time its page
// asked for anything.
type CheckAdminSessionHandler struct {
	tokens ports.TokenIssuer
}

// NewCheckAdminSessionHandler builds the handler from its port.
func NewCheckAdminSessionHandler(tokens ports.TokenIssuer) *CheckAdminSessionHandler {
	return &CheckAdminSessionHandler{tokens: tokens}
}

// Handle reports whether the credential is one this server still accepts.
//
// There is no failure to report, and that is not a shortcut. The credential store says no in several
// different ways — never issued, retired by the rolling window, ended by a logout, badly signed, signed
// by something that is not the server's own key — and the legacy server answers every one of them with
// the same empty 401. Returning the distinction would give the presentation layer a distinction it is
// not allowed to use.
func (handler *CheckAdminSessionHandler) Handle(ctx context.Context, query CheckAdminSessionQuery) bool {
	return handler.tokens.Validate(ctx, query.Token) == nil
}
