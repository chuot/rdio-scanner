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

package spa

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// bundleRoot is the directory a request path is resolved against,
// exactly as the legacy handler joins it, and that prefix is what
// bounds a parent directory. see the
// single-page-app-serving-rules-kept-from-legacy maintainer note.
const bundleRoot = "bundle"

// indexFile is the application shell every route that is not a file falls back to.
const indexFile = "index.html"

// New returns the catch-all handler serving the application out of
// files: an embedded file when the path names one, otherwise the
// shell, with files rooted at bundle/ and the mount stripped first.
// see the single-page-app-serving-rules-kept-from-legacy maintainer note.
func New(files fs.FS, mount string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The legacy handler works on the path with its leading slash removed,
		// and every rule below is a rule about that string. see the
		// single-page-app-serving-rules-kept-from-legacy maintainer note.
		rest := r.URL.Path

		if mount != "" {
			if !strings.HasPrefix(rest, mount) || (rest != mount && rest[len(mount)] != '/') {
				w.WriteHeader(http.StatusNotFound)

				return
			}

			rest = rest[len(mount):]
		}

		url := ""
		if rest != "" {
			url = rest[1:]
		}

		if url == "" {
			url = indexFile
		}

		if b, err := fs.ReadFile(files, path.Join(bundleRoot, url)); err == nil {
			w.Header().Set("Content-Type", contentTypeOf(url))
			w.Write(b)

			return
		}

		// The legacy condition, kept character for character: it drops the last
		// character and compares, so a route ending in / takes the fallback,
		// exactly as on a deployed server. Reproduced rather than corrected,
		// see the single-page-app-serving-rules-kept-from-legacy maintainer note.
		if url[:len(url)-1] != "/" {
			if b, err := fs.ReadFile(files, path.Join(bundleRoot, indexFile)); err == nil {
				// No Content-Type is set on this path, and none is added: the legacy
				// handler sets none, so Go sniffs the body. see the
				// single-page-app-serving-rules-kept-from-legacy maintainer note.
				w.Write(b)

				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
	})
}

// contentTypeOf is the Content-Type the legacy handler sends: .js is
// hardcoded to text/javascript with no charset, because Go's mime table
// adds one. see the
// single-page-app-serving-rules-kept-from-legacy maintainer note.
func contentTypeOf(url string) string {
	switch path.Ext(url) {
	case ".js":
		return "text/javascript" // see https://github.com/golang/go/issues/32350
	default:
		return mime.TypeByExtension(path.Ext(url))
	}
}
