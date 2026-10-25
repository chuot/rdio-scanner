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

// Package backend is the administration console's own surface: the endpoints an
// administrator's page and the command-line tooling talk to, the configuration
// sections that page saves one at a time, and the configuration socket the page holds
// open to learn about a change as it happens. Nothing here decides anything: each
// endpoint reads a credential, hands what was submitted to the use case that owns the
// decision, and turns the outcome into the status code and body the frozen contract
// fixes, and the socket sends its credential and pushes the configuration to every
// connection that presented one. The inbound recording upload endpoints are not here:
// they are authenticated by an apikey scoped to systems, not by an administrator
// session, and neither web application posts to them; they live in common/upload.
package backend
