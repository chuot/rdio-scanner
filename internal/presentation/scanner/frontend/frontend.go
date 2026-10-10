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
	"io/fs"
	"net/http"

	"rdio-scanner/internal/presentation/common/spa"
)

// bundle is the scanner application compiled into the binary.
//
// The directory has to be inside this package: a `go:embed` pattern cannot name a path
// outside the package directory it is written in, so the client build writes its scanner
// output here, at `bundle/`, rather than somewhere this package could not reach.
//
// `all:` is the departure from the legacy `//go:embed webapp/*`, and it exists only so
// this package compiles before the client has ever been built. `bundle/.gitkeep` is the
// placeholder that keeps the directory in the repository, and a pattern without `all:`
// refuses to match a directory whose only entry is a hidden file — the embed would fail
// with "matches no files". A client build writes no hidden files, so once the bundle is
// in place the two patterns embed exactly the same set.
//
//go:embed all:bundle
var bundle embed.FS

// bundleIsBuilt is the bundle question asked of a file set: the application shell is what a
// built bundle is judged by, the same file the build tool judges a bundle's freshness by. A
// bundle with nothing in it but the placeholder is a bundle no client build has ever written,
// and the server says so at startup.
func bundleIsBuilt(files fs.FS) bool {
	_, err := fs.Stat(files, "bundle/index.html")

	return err == nil
}

// BundleIsBuilt is the same question asked of the package's own embedded bundle.
func BundleIsBuilt() bool {
	return bundleIsBuilt(bundle)
}

// Embedded returns the catch-all handler for the scanner application, served from the
// root of the server: the path a listener connects to is the path the page is served
// from, and the composition root decides which one a request is before this handler
// ever sees it.
func Embedded() http.Handler {
	return spa.New(bundle, "")
}
