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

// appendServerLog writes an entry to the server's own log view, naming the part of the server that
// wrote it.
//
// A log entry that cannot be written never changes the decision that has already been taken: the
// legacy server writes its log entries and ignores the failure, and a use case that turned a
// failed log write into a failed operation would report a different outcome to the caller than
// the one it actually produced.
func appendServerLog(ctx context.Context, logs ports.LogRepository, clock ports.Clock, level string, source domain.LogSource, message string) {
	if err := logs.AppendLog(ctx, domain.LogEntry{
		DateTime: clock.Now(),
		Level:    level,
		Source:   source,
		Message:  message,
	}); err != nil {
		return
	}
}
