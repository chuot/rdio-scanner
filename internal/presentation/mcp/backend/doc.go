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

// Package backend wires the server's Model Context Protocol surface: the
// MCP endpoint. The credential gate resolves the caller's tier and
// talkgroup scope before the handshake, so both transports share one
// entry point, and the gateway decides nothing: every call uses the
// handlers the HTTP surface uses. Two principals shape the surface:
// capabilities carry what the deployment has. The protocol is JSON-RPC
// 2.0 over HTTP POST or the scanner websocket, and method and tool names
// follow the MCP specification's published shapes: scanner-specific
// behaviour sits in the result content.
package backend
