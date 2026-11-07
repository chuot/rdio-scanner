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

package frontend

import (
	"embed"
	"io/fs"
	"net/http"

	"rdio-scanner/internal/presentation/common/spa"
)

// bundle is the administration application compiled into the binary.
//
//go:embed all:bundle
var bundle embed.FS

// bundleIsBuilt is the bundle question asked of a file set.
func bundleIsBuilt(files fs.FS) bool {
	_, err := fs.Stat(files, "bundle/index.html")

	return err == nil
}

// BundleIsBuilt is the same question asked of the package's own embedded bundle.
func BundleIsBuilt() bool {
	return bundleIsBuilt(bundle)
}

// Embedded returns the catch-all handler for the administration application, mounted at
// /admin.
func Embedded() http.Handler {
	return spa.New(bundle, "/admin")
}
