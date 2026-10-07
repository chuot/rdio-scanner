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
	"rdio-scanner/server-ng/internal/domain"
)

// GetCallByIdQuery asks for one recorded call.
type GetCallByIdQuery struct {
	ID uint64
}

// GetCallQueryHandler reads one recorded call in full.
type GetCallQueryHandler struct {
	calls ports.CallRepository
}

// NewGetCallQueryHandler builds the handler from its port.
func NewGetCallQueryHandler(calls ports.CallRepository) *GetCallQueryHandler {
	return &GetCallQueryHandler{calls: calls}
}

// Handle returns the recorded call, or ports.ErrNotFound.
//
// This query returns the aggregate rather than a projection because the listener asked
// for the whole call, audio included: the call payload is the call. The history search
// (SearchCallsQuery) is the one that must not load audio, and it returns a projection.
func (handler *GetCallQueryHandler) Handle(ctx context.Context, query GetCallByIdQuery) (domain.Call, error) {
	return handler.calls.GetCall(ctx, query.ID)
}
