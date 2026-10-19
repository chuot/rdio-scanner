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

// bundleRoot is the directory a request path is resolved against, exactly as the
// legacy handler resolves it with `path.Join("webapp", url)`.
//
// The prefix is what bounds `..`. Joining cleans the path lexically, so a request for
// `/../secret` is resolved to `secret` — the root of the file set — and the root of an
// embedded file set contains nothing but this directory. Nothing above it can be read.
const bundleRoot = "bundle"

// indexFile is the application shell every route that is not a file falls back to.
const indexFile = "index.html"

// New returns the catch-all handler that serves the single-page application out of
// files: an embedded file when the path names one, the application shell when it does
// not, and an empty 404 in the one case the legacy handler answers 404.
//
// files is shaped the way `//go:embed all:bundle` shapes it: every path inside it is
// rooted at `bundle/`. That is not an artifact of the embed. The handler resolves a
// request against that prefix because the prefix is what stops `..` from climbing out
// of the bundle, and a file set handed to this handler is expected to be a bundle, not
// an arbitrary directory tree.
//
// mount is the prefix the composition root registered this handler under, `""` for the
// application served from the origin and `/admin` for the one served from that subtree.
// The handler removes it before resolving the rest against the bundle, so the bundle
// holds one flat file set and never a copy of the mount inside itself. A path that is
// not under the mount is not this application's to answer, and gets the 404.
//
// The websocket branch of the legacy catch-all (`server/main.go:140`) is deliberately
// absent, and a reader comparing the two files should look for it and not find it. The
// legacy handler decided for itself whether a request was a listener upgrade; in
// This server the composition root makes that decision before a request reaches this
// package, so this handler only serves files and answers an upgrade request with the
// file the path names.
func New(files fs.FS, mount string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The legacy handler works on the path with its leading slash removed, and
		// every rule below — including the one that looks wrong — is a rule about that
		// string. net/http always hands a handler a rooted path, so the slice is safe.
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

		// This is the legacy condition, `url[:len(url)-1] != "/"`, kept character for
		// character. It is not "the path ends with a slash": it drops the last character
		// and compares what is left to a single slash, so it is false only for a
		// two-character path that starts with one — the request `//x`. A request for
		// `/some/route/`, and a request for a directory inside the bundle, therefore take
		// the fallback here exactly as they do on a deployed server, and the 404 below is
		// reached only by a path that is a slash plus one more character, or by a bundle
		// with no application shell in it.
		//
		// Reproduced rather than corrected: the deployed server answers `/some/route/`
		// with the application shell, and a browser cannot tell a deliberate rule from an
		// off-by-one. Correcting it here would make this server answer 404 to URLs the
		// legacy server answers 200.
		if url[:len(url)-1] != "/" {
			if b, err := fs.ReadFile(files, path.Join(bundleRoot, indexFile)); err == nil {
				// No Content-Type is set on this path, and none is added here: the
				// legacy handler does not set one, so Go sniffs the body. The same bytes
				// therefore reach the client with a different header than the same bytes
				// served as `bundle/index.html`.
				w.Write(b)

				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
	})
}

// contentTypeOf is the Content-Type the legacy handler puts on the wire for a path.
//
// `.js` is hardcoded to `text/javascript` with no charset, because Go's mime table
// answers `text/javascript; charset=utf-8` and that is the bug the legacy comment
// points at (https://github.com/golang/go/issues/32350). The comparison is against the
// lowercase extension only, so a file whose extension is `.JS` takes the default branch
// and does get the charset.
//
// Every other extension is answered by mime.TypeByExtension, which returns the empty
// string for an extension Go does not know — `.webmanifest` is one, and so is a path
// with no extension at all, because `path.Ext("sw.js/")` is empty. The empty string is
// then *set* as the header, which is not the same as leaving it unset: the response
// carries `Content-Type:` with an empty value and Go does not sniff the body. That is
// what a deployed server answers for `manifest.webmanifest` today, so it is reproduced.
func contentTypeOf(url string) string {
	switch path.Ext(url) {
	case ".js":
		return "text/javascript" // see https://github.com/golang/go/issues/32350
	default:
		return mime.TypeByExtension(path.Ext(url))
	}
}
