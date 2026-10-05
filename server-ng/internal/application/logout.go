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

// AdminLogoutOutcome reports what the server decided about an attempt to end an
// administrator session. The REST layer maps it onto the status codes the frozen contract
// fixes: an accepted logout is an empty 200, a refused one is a 401.
type AdminLogoutOutcome string

const (
	// AdminLogoutAccepted means the token was one the server still accepts, and it has been
	// taken out of the accepted set: the session it stands for is over.
	AdminLogoutAccepted AdminLogoutOutcome = "accepted"

	// AdminLogoutRefused means the token is not one the server accepts — never issued,
	// already ended, or retired by the rolling window. The caller is answered the same way
	// any missing access is answered, and nothing else is ended.
	AdminLogoutRefused AdminLogoutOutcome = "refused"

	// AdminLogoutFailed means the server could not complete a step the logout depends on.
	AdminLogoutFailed AdminLogoutOutcome = "failed"
)

// AdminLogoutResult is the acknowledgement the logout endpoint gets. Ending a session
// produces nothing the client needs beyond whether it worked.
type AdminLogoutResult struct {
	Outcome AdminLogoutOutcome
}

// AdminLogoutCommand asks the server to end the administrator session one token stands
// for.
//
// The token arrives exactly as the client sent it: the frozen contract carries it raw in
// the Authorization header, with no scheme prefix, and the REST layer strips nothing.
type AdminLogoutCommand struct {
	Token string
}

// AdminLogoutHandler ends an administrator session.
//
// Only the presented token is retired. The legacy server keeps a rolling window of the
// tokens it has issued rather than one session per administrator, so logging out of one
// browser leaves another browser's session running, and a client that assumes otherwise
// loses access it still had.
type AdminLogoutHandler struct {
	tokens ports.TokenIssuer
}

// NewAdminLogoutHandler builds the handler from its port.
func NewAdminLogoutHandler(tokens ports.TokenIssuer) *AdminLogoutHandler {
	return &AdminLogoutHandler{tokens: tokens}
}

// Handle retires the presented token, or refuses the request when the server does not
// accept it.
//
// Checking before removing is what the legacy server does, and it is what makes a logout
// with a token that is no longer accepted a refusal rather than a silent success: the
// caller finds out that its session is already gone.
func (handler *AdminLogoutHandler) Handle(ctx context.Context, cmd AdminLogoutCommand) (AdminLogoutResult, error) {
	if err := handler.tokens.Validate(ctx, cmd.Token); err != nil {
		return AdminLogoutResult{Outcome: AdminLogoutRefused}, nil
	}

	if err := handler.tokens.Revoke(ctx, cmd.Token); err != nil {
		return AdminLogoutResult{Outcome: AdminLogoutFailed}, err
	}

	return AdminLogoutResult{Outcome: AdminLogoutAccepted}, nil
}
