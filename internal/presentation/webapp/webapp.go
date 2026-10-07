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

// Package webapp is the transitional home of the scanner bundle. The handler it used to
// own lives in internal/presentation/serving/spa, and the embed moves to
// internal/presentation/serving/scanner in the next step of the same feature. Until the
// composition root points there, this package keeps the bundle embedded where the client
// build writes it, so the server keeps serving a built application at every commit.
package webapp

import (
	"embed"
	"net/http"

	"rdio-scanner/internal/presentation/serving/spa"
)

// bundle is the scanner application compiled into the binary.
//
// `all:` is the departure from the legacy `//go:embed webapp/*`, and it exists only so
// this package compiles before the client has ever been built: `bundle/.gitkeep` is the
// placeholder that keeps the directory in the repository, and a pattern without `all:`
// refuses to match a directory whose only entry is a hidden file. A client build writes
// no hidden files, so once the bundle is in place the two patterns embed the same set.
//
//go:embed all:bundle
var bundle embed.FS

// Embedded returns the catch-all handler for the application compiled into the binary.
func Embedded() http.Handler {
	return spa.New(bundle, "")
}
