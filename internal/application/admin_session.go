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

	"rdio-scanner/internal/application/ports"
)

// CheckAdminSessionQuery asks whether a credential presented to an administrator endpoint
// stands for a session this server still accepts; the credential arrives raw from the
// Authorization header, exactly as the request carried it.
type CheckAdminSessionQuery struct {
	Token string
}

// CheckAdminSessionHandler answers that question without changing what it asks about: a
// refused check does not end a session and an accepted one does not use the credential
// up, so every administrator endpoint can ask it before anything else.
type CheckAdminSessionHandler struct {
	tokens ports.TokenIssuer
}

// NewCheckAdminSessionHandler builds the handler from its port.
func NewCheckAdminSessionHandler(tokens ports.TokenIssuer) *CheckAdminSessionHandler {
	return &CheckAdminSessionHandler{tokens: tokens}
}

// Handle reports whether the credential is one this server still accepts. There is no
// failure to distinguish: the store refuses a credential in several ways and every one
// answers the same empty result, which is the only answer the presentation layer may use.
func (handler *CheckAdminSessionHandler) Handle(ctx context.Context, query CheckAdminSessionQuery) bool {
	return handler.tokens.Validate(ctx, query.Token) == nil
}
