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

// Package backend is the Model Context Protocol surface: the endpoint an AI assistant administers the
// deployment through.
//
// It is a presentation package, and it decides nothing. Every capability here reads a credential, hands
// what the assistant asked for to the use case that owns the decision, and shapes the answer into the
// tool result the assistant reads. No business rule lives in this package, and no MCP type reaches the
// application layer: the dependency points inward, the same way the administrator's REST surface points
// inward.
//
// The capabilities are derived from what an administrator asks, not from the REST routes. One capability
// composes several endpoints where the question needs several; a filter is an argument of one capability
// and never a capability of its own. The catalog and the reason for each entry live beside the
// implementation as tool descriptions that name the endpoints each capability uses.
//
// Two principals reach this surface. An administrator session token, the same credential the
// administrator's own page uses, which may read and write. A listener access code, which is read-only and
// sees only the systems and talkgroups its access grants. A server with no access list is not restricted,
// and answers the read-only capabilities anonymously — which is what the listener socket does today.
//
// Nothing here touches a websocket surface. The listener socket and the administrator configuration
// socket are under the same GNU General Public License as this surface, and this surface is built on the
// HTTP REST surface and the use cases behind it.
package backend
