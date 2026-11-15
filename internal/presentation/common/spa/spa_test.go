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
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The fixtures are the shapes a `client-ng` build produces: an HTML shell, a module, a
// stylesheet, and a manifest whose extension Go does not know. Each test names exactly
// the files its bundle contains, so a fallback is always a fallback and never a file
// that happened to be there.
const (
	shellHTML    = "<!doctype html>\n<html>\n<head><title>Rdio Scanner</title></head>\n<body><app-root></app-root></body>\n</html>\n"
	shellText    = "Rdio Scanner application shell\n"
	appJS        = "export const app = 'rdio-scanner';\n"
	appCSS       = "body { font-family: sans-serif; }\n"
	manifestText = "{\n  \"name\": \"Rdio Scanner\"\n}\n"
)

// get issues one GET against the handler over files through a real HTTP server, so that
// what a test asserts is the response a browser sees: the status code, the headers Go
// actually put on the wire, and the body bytes.
func get(t *testing.T, files fs.FS, url string, header http.Header) (int, http.Header, []byte) {
	t.Helper()

	return getMounted(t, files, "", url, header)
}

// getMounted issues the same request against a handler mounted under a prefix, which is
// how an application served from a subtree rather than from the origin is exercised.
func getMounted(t *testing.T, files fs.FS, mount, url string, header http.Header) (int, http.Header, []byte) {
	t.Helper()

	srv := httptest.NewServer(New(files, mount))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+url, nil)
	if err != nil {
		t.Fatalf("GET %s: request: %v", url, err)
	}

	for name, values := range header {
		req.Header[name] = values
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: body: %v", url, err)
	}

	return resp.StatusCode, resp.Header, body
}

// wireContentType reports the Content-Type the response carries, and whether the header
// was there at all. An empty value and an absent header are different answers, and the
// legacy handler produces both.
func wireContentType(header http.Header) (string, bool) {
	values, ok := header["Content-Type"]
	if !ok {
		return "", false
	}

	return values[0], true
}

func TestRootServesTheApplicationShell(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
	}

	status, header, body := get(t, files, "/", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the application shell for the page URL", status)
	}

	if ct, _ := wireContentType(header); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the mime table's type for .html", ct)
	}

	if string(body) != shellHTML {
		t.Errorf("body = %q, want the application shell bytes", body)
	}
}

func TestJavaScriptIsServedAsTextJavaScriptWithoutACharset(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
		"bundle/sw.js":      &fstest.MapFile{Data: []byte(appJS)},
	}

	status, header, body := get(t, files, "/sw.js", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the embedded service worker", status)
	}

	// No charset, exactly: Go's mime table answers text/javascript with a
	// charset for .js, and the legacy server hardcodes the bare form
	// because of that.
	if ct, _ := wireContentType(header); ct != "text/javascript" {
		t.Errorf("Content-Type = %q, want text/javascript with no charset", ct)
	}

	if string(body) != appJS {
		t.Errorf("body = %q, want the embedded service worker bytes", body)
	}
}

func TestHtmlIsServedWithTheMimeTableContentType(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
	}

	status, header, body := get(t, files, "/index.html", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the embedded application shell", status)
	}

	if ct, _ := wireContentType(header); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the mime table's type for .html", ct)
	}

	if string(body) != shellHTML {
		t.Errorf("body = %q, want the embedded application shell bytes", body)
	}
}

func TestCssIsServedWithTheMimeTableContentType(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html":     &fstest.MapFile{Data: []byte(shellHTML)},
		"bundle/assets/app.css": &fstest.MapFile{Data: []byte(appCSS)},
	}

	status, header, body := get(t, files, "/assets/app.css", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the embedded stylesheet under assets/", status)
	}

	if ct, _ := wireContentType(header); ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the mime table's type for .css", ct)
	}

	if string(body) != appCSS {
		t.Errorf("body = %q, want the embedded stylesheet bytes", body)
	}
}

func TestUnknownExtensionIsServedWithAnEmptyContentType(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html":           &fstest.MapFile{Data: []byte(shellHTML)},
		"bundle/manifest.webmanifest": &fstest.MapFile{Data: []byte(manifestText)},
	}

	status, header, body := get(t, files, "/manifest.webmanifest", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the embedded manifest", status)
	}

	// Present but empty, not absent: the mime table answers the empty
	// string for an extension Go does not know, that is set as the header,
	// and Go then does not sniff the body.
	ct, present := wireContentType(header)

	if !present {
		t.Fatal("Content-Type is absent, want the header present with an empty value")
	}

	if ct != "" {
		t.Errorf("Content-Type = %q, want the empty value Go answers for an extension it does not know", ct)
	}

	if string(body) != manifestText {
		t.Errorf("body = %q, want the embedded manifest bytes", body)
	}
}

func TestUppercaseJavaScriptExtensionIsServedWithTheMimeTableContentType(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
		"bundle/SW.JS":      &fstest.MapFile{Data: []byte(appJS)},
	}

	status, header, _ := get(t, files, "/SW.JS", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the embedded file", status)
	}

	// The hardcoded text/javascript compares the lowercase extension, so an
	// uppercase one takes the mime-table branch and carries a charset.
	if ct, _ := wireContentType(header); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the mime table's type for .JS, charset included", ct)
	}
}

func TestMissingRouteServesTheApplicationShell(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
		"bundle/sw.js":      &fstest.MapFile{Data: []byte(appJS)},
	}

	status, header, body := get(t, files, "/listener/1234", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the SPA fallback for a route the bundle has no file for", status)
	}

	if ct, _ := wireContentType(header); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the type Go sniffs from the shell bytes", ct)
	}

	if string(body) != shellHTML {
		t.Errorf("body = %q, want the application shell bytes", body)
	}
}

// TestFallbackContentTypeComesFromTheBodyNotTheExtension: no Content-Type
// is set on the fallback, so Go sniffs the body and the header differs
// from the same file served as a bundle entry.
func TestFallbackContentTypeComesFromTheBodyNotTheExtension(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellText)},
	}

	_, fileHeader, fileBody := get(t, files, "/index.html", nil)
	_, fallbackHeader, fallbackBody := get(t, files, "/some/route", nil)

	if string(fileBody) != shellText || string(fallbackBody) != shellText {
		t.Fatalf("both responses want the same shell bytes, got %q and %q", fileBody, fallbackBody)
	}

	if ct, _ := wireContentType(fileHeader); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type for /index.html = %q, want the mime table's type for .html", ct)
	}

	if ct, _ := wireContentType(fallbackHeader); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type for /some/route = %q, want the type Go sniffs from the shell bytes, not the type of index.html", ct)
	}
}

// TestMissingRouteEndingInSlashServesTheApplicationShell pins the
// legacy url[:len(url)-1] condition as written: a route ending in /
// takes the fallback, not the 404.
func TestMissingRouteEndingInSlashServesTheApplicationShell(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
	}

	status, header, body := get(t, files, "/some/route/", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the deployed server's SPA fallback for a route ending in a slash", status)
	}

	if ct, _ := wireContentType(header); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the type Go sniffs from the shell bytes", ct)
	}

	if string(body) != shellHTML {
		t.Errorf("body = %q, want the application shell bytes", body)
	}
}

func TestDirectoryRequestEndingInSlashServesTheApplicationShell(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html":     &fstest.MapFile{Data: []byte(shellHTML)},
		"bundle/assets/app.css": &fstest.MapFile{Data: []byte(appCSS)},
	}

	status, _, body := get(t, files, "/assets/", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the fallback: a directory in the bundle is not a listing", status)
	}

	if string(body) != shellHTML {
		t.Errorf("body = %q, want the application shell bytes, not a directory listing", body)
	}
}

func TestDirectoryRequestServesTheApplicationShell(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html":     &fstest.MapFile{Data: []byte(shellHTML)},
		"bundle/assets/app.css": &fstest.MapFile{Data: []byte(appCSS)},
	}

	status, _, body := get(t, files, "/assets", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the fallback: a directory in the bundle is not a file", status)
	}

	if string(body) != shellHTML {
		t.Errorf("body = %q, want the application shell bytes", body)
	}
}

// TestTrailingSlashOnAnEmbeddedFileServesItWithAnEmptyContentType: path
// .Join cleans the trailing slash away so the file is found, while
// path.Ext was already taken from the uncleaned path.
func TestTrailingSlashOnAnEmbeddedFileServesItWithAnEmptyContentType(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
		"bundle/sw.js":      &fstest.MapFile{Data: []byte(appJS)},
	}

	status, header, body := get(t, files, "/sw.js/", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the embedded file the cleaned path names", status)
	}

	ct, present := wireContentType(header)

	if !present {
		t.Fatal("Content-Type is absent, want the header present with an empty value")
	}

	if ct != "" {
		t.Errorf("Content-Type = %q, want the empty value Go answers for a path with no extension", ct)
	}

	if string(body) != appJS {
		t.Errorf("body = %q, want the embedded service worker bytes", body)
	}
}

func TestDotDotCannotReadOutsideTheBundle(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
		"bundle/sw.js":      &fstest.MapFile{Data: []byte(appJS)},
	}

	for _, url := range []string{"/../secret", "/../../etc/passwd", "/.."} {
		status, _, body := get(t, files, url, nil)

		if status != http.StatusOK {
			t.Errorf("GET %s: status = %d, want the fallback", url, status)
		}

		if string(body) != shellHTML {
			t.Errorf("GET %s: body = %q, want the application shell: nothing outside the bundle is readable", url, body)
		}
	}
}

// TestDotDotResolvesAgainstTheBundleRoot pins where the cleaned path
// lands: .. resolves against the bundle prefix, so the file it reaches
// stays inside the bundle.
func TestDotDotResolvesAgainstTheBundleRoot(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
		"bundle/sw.js":      &fstest.MapFile{Data: []byte(appJS)},
	}

	status, header, body := get(t, files, "/../bundle/sw.js", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the embedded file the cleaned path names", status)
	}

	if ct, _ := wireContentType(header); ct != "text/javascript" {
		t.Errorf("Content-Type = %q, want text/javascript with no charset", ct)
	}

	if string(body) != appJS {
		t.Errorf("body = %q, want the embedded service worker bytes", body)
	}
}

func TestMissingApplicationShellAnswersFourOhFourForARoute(t *testing.T) {
	files := fstest.MapFS{
		"bundle/sw.js": &fstest.MapFile{Data: []byte(appJS)},
	}

	status, header, body := get(t, files, "/some/route", nil)

	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404: there is no shell to fall back to", status)
	}

	if _, present := wireContentType(header); present {
		t.Errorf("Content-Type = %q, want no header on an empty 404", header.Get("Content-Type"))
	}

	if len(body) != 0 {
		t.Errorf("body = %q, want no body", body)
	}
}

func TestMissingApplicationShellAnswersFourOhFourForTheRoot(t *testing.T) {
	files := fstest.MapFS{
		"bundle/sw.js": &fstest.MapFile{Data: []byte(appJS)},
	}

	status, _, body := get(t, files, "/", nil)

	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404: the page URL asks for the shell that is not there", status)
	}

	if len(body) != 0 {
		t.Errorf("body = %q, want no body", body)
	}
}

// TestFourOhFourCarriesNoBodyAndNoContentType pins the one shape the
// legacy condition rejects, a slash plus one more character: an empty
// 404 with no Content-Type.
func TestFourOhFourCarriesNoBodyAndNoContentType(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
	}

	status, header, body := get(t, files, "//x", nil)

	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for the one path the legacy condition rejects", status)
	}

	if _, present := wireContentType(header); present {
		t.Errorf("Content-Type = %q, want no header on an empty 404", header.Get("Content-Type"))
	}

	if len(body) != 0 {
		t.Errorf("body = %q, want no body", body)
	}
}

func TestDoubleSlashServesTheApplicationShell(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
	}

	status, _, body := get(t, files, "//", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want the fallback: the cleaned path names the bundle directory, not a file", status)
	}

	if string(body) != shellHTML {
		t.Errorf("body = %q, want the application shell bytes", body)
	}
}

// TestUpgradeRequestIsServedAsAFileNotAnUpgrade pins the boundary with
// the composition root: this handler never reads the upgrade header,
// so an upgrade request gets the file the path names.
func TestUpgradeRequestIsServedAsAFileNotAnUpgrade(t *testing.T) {
	files := fstest.MapFS{
		"bundle/index.html": &fstest.MapFile{Data: []byte(shellHTML)},
	}

	status, _, body := get(t, files, "/", http.Header{"Upgrade": []string{"websocket"}})

	if status == http.StatusSwitchingProtocols {
		t.Fatal("status = 101, want this handler to serve the file and leave upgrades to the composition root")
	}

	if status != http.StatusOK {
		t.Errorf("status = %d, want the application shell", status)
	}

	if string(body) != shellHTML {
		t.Errorf("body = %q, want the application shell bytes", body)
	}
}

// The two bundles a mounted handler must keep apart. Each shell says which application it
// belongs to, so a response that reached across the boundary is visible in the body rather
// than something a test has to infer from a status code.
const (
	scannerShellHTML = "<!doctype html>\n<html>\n<head><title>Rdio Scanner</title></head>\n<body><rdio-app></rdio-app></body>\n</html>\n"
	adminShellHTML   = "<!doctype html>\n<html>\n<head><title>Rdio Scanner Administration</title></head>\n<body><rdio-admin></rdio-admin></body>\n</html>\n"
	adminJS          = "export const admin = 'rdio-scanner-admin';\n"
	adminWorkerJS    = "const CACHE_NAME = 'rdio-admin-static';\n"
)

// adminBundle is the file set a client build produces for the administration application:
// a shell, a hashed module, a hashed stylesheet, a copied web manifest, and a service
// worker under its own unhashed name.
func adminBundle() fstest.MapFS {
	return fstest.MapFS{
		"bundle/index.html":           &fstest.MapFile{Data: []byte(adminShellHTML)},
		"bundle/main.AAA.js":          &fstest.MapFile{Data: []byte(adminJS)},
		"bundle/main.BBB.css":         &fstest.MapFile{Data: []byte(appCSS)},
		"bundle/manifest.webmanifest": &fstest.MapFile{Data: []byte(manifestText)},
		"bundle/sw.js":                &fstest.MapFile{Data: []byte(adminWorkerJS)},
	}
}

// TestMountedBundleServesItsOwnFilesUnderTheMount is why the mount exists: a
// requested path under the mount is answered from the same path inside the
// bundle, which stays flat with no copy of the prefix inside it.
func TestMountedBundleServesItsOwnFilesUnderTheMount(t *testing.T) {
	status, header, body := getMounted(t, adminBundle(), "/admin", "/admin/main.AAA.js", nil)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want the module the admin bundle holds", status)
	}

	if got, _ := wireContentType(header); got != "text/javascript" {
		t.Errorf("Content-Type = %q, want text/javascript with no charset", got)
	}

	if string(body) != adminJS {
		t.Errorf("body = %q, want the admin module bytes", body)
	}

	// The worker script is a file the administration bundle carries, so the mount
	// answers for it the way it answers for any other file in that bundle: its own
	// bytes with the type a worker script needs, and not the shell a page needs.
	status, header, body = getMounted(t, adminBundle(), "/admin", "/admin/sw.js", nil)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want the worker script the admin bundle holds", status)
	}

	if got, _ := wireContentType(header); got != "text/javascript" {
		t.Errorf("Content-Type = %q, want text/javascript with no charset", got)
	}

	if string(body) != adminWorkerJS {
		t.Errorf("body = %q, want the admin worker bytes and not the administration shell", body)
	}
}

// TestMountedBundleCannotBeLeftByRelativePath is the mount-level half
// of the dot-dot rule: every path is cleaned or rejected inside the
// bundle, and the answer is the mounted application's own shell.
func TestMountedBundleCannotBeLeftByRelativePath(t *testing.T) {
	files := adminBundle()

	for _, url := range []string{"/admin/..", "/admin/../x", "/admin/../../etc/passwd", "/admin/some/route"} {
		status, _, body := getMounted(t, files, "/admin", url, nil)

		if status != http.StatusOK {
			t.Errorf("GET %s: status = %d, want the mounted application shell", url, status)
		}

		if string(body) != adminShellHTML {
			t.Errorf("GET %s: body = %q, want the administration shell and nothing else", url, body)
		}

		if strings.Contains(string(body), scannerShellHTML) || strings.Contains(string(body), "passwd") {
			t.Errorf("GET %s: body left the admin bundle", url)
		}
	}
}

// TestMountedBundleRefusesAPathOutsideItsMount is the mount's other
// half: a handler mounted at /admin answers for /admin and nothing
// else.
func TestMountedBundleRefusesAPathOutsideItsMount(t *testing.T) {
	files := adminBundle()

	for _, url := range []string{"/", "/other", "/administrator"} {
		status, _, body := getMounted(t, files, "/admin", url, nil)

		if status != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404 from a handler mounted at /admin", url, status)
		}

		if len(body) != 0 {
			t.Errorf("GET %s: body = %q, want an empty 404", url, body)
		}
	}
}
