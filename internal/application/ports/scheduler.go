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

package ports

import "time"

// Timer is a piece of scheduled work that has not happened yet.
type Timer interface {
	// Cancel abandons the scheduled work. A timer that has already run ignores it.
	Cancel()
}

// Scheduler runs work after a delay. It is a port rather than a clock timer because
// the application has rules about when something happens — the listener-count report
// is held back so a burst of listeners joining and leaving produces one report — and
// a rule about time that can only be tested by waiting for real time is not tested.
type Scheduler interface {
	// Schedule asks for work to run after the delay, and returns the handle that can
	// abandon it.
	Schedule(delay time.Duration, work func()) Timer
}
