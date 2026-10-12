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

// Package upload is the inbound recording ingestion boundary: the endpoints a radio bridge, a
// capture application, and another rdio-scanner server posting what it received downstream use to
// hand a recording to this server.
//
// It belongs to neither web application. The calls an upload creates are what the scanner webapp
// plays, but the uploader is a recorder and not the scanner webapp, and what lets an upload through
// is the apikey the upload carries and that key's scope — not an administrator session and not a
// listener's access code.
//
// The endpoints answer with plain text and not with JSON, because that is what every program
// posting to them has been reading since they existed.
package upload
