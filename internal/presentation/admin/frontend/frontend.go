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

package frontend

import (
	"embed"
	"net/http"

	"rdio-scanner/internal/presentation/common/spa"
)

// bundle is the administration application compiled into the binary.
//
// The directory has to be inside this package: a `go:embed` pattern cannot name a path
// outside the package directory it is written in, so the client build writes its admin
// output here, at `bundle/`, rather than somewhere this package could not reach.
//
// `all:` exists only so this package compiles before the client has ever been built:
// `bundle/.gitkeep` keeps the directory in the repository, and a pattern without `all:`
// refuses to match a directory whose only entry is a hidden file. A client build writes
// no hidden files, so once the bundle is in place the two patterns embed the same set.
//
//go:embed all:bundle
var bundle embed.FS

// Embedded returns the catch-all handler for the administration application, mounted at
// /admin. The mount is removed before a path is resolved against the bundle, so the
// bundle is flat and holds no copy of the prefix inside itself.
//
// The mount is written without its trailing slash because the composition root registers
// the subtree pattern `/admin/`, and `http.ServeMux` redirects a request for `/admin` to
// `/admin/` before this handler sees it. That redirect is what makes every relative URL
// in the shell resolve inside the admin bundle.
func Embedded() http.Handler {
	return spa.New(bundle, "/admin")
}
