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

// appendServerLog writes an entry to the server's own log view, naming the part of the server that
// wrote it; a failed log write never changes the decision already taken.
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

// counted is a count in the words an operator reads: "1 call", "3 calls", "0 log entries".
func counted(count int, singular, plural string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}

	return fmt.Sprintf("%d %s", count, plural)
}
