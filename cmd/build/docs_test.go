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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The archive document is one file that opens in a browser with nothing beside it. Every test here is
// about a thing that would make the file need something beside it: an image path, a stylesheet, a link
// to a file in the repository.

// theRepo is the tree the documentation lives in, because the document is built from it.
func theRepo(t *testing.T) string {
	t.Helper()

	repo, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}

	return repo
}

// The document is the target's platform document, then the web app guide, then the FAQ, then the change
// log: the order the pandoc build was fed them in.
func TestTheDocumentIsTheFourSourcesInTheOrderTheReleaseUsed(t *testing.T) {
	html, err := renderDocument(theRepo(t), releaseTargets[4], "8.0.0", "2026-10-10")
	if err != nil {
		t.Fatalf("renderDocument: %v", err)
	}

	order := []string{
		"<h1>What is it ?</h1>",
		"<h1>The web app</h1>",
		"<h1>FAQ</h1>",
		"<h1>Change log</h1>",
	}

	last := -1

	for _, heading := range order {
		at := strings.Index(html, heading)

		if at < 0 {
			t.Errorf("the document has no %s", heading)
			continue
		}

		if at < last {
			t.Errorf("%s is at %d, which is before the source that should come first", heading, at)
		}

		last = at
	}

	// The document names the platform it was built for, so a reader who has eight of them knows which
	// one is open.
	if !strings.Contains(html, "linux-arm64") {
		t.Errorf("the document does not name the target it was built for")
	}
}

// A target's document is the one its matrix entry names, not the one the machine running the tool uses.
func TestTheDocumentIsTheTargetDocumentNotTheHostDocument(t *testing.T) {
	html, err := renderDocument(theRepo(t), releaseTargets[5], "8.0.0", "2026-10-10")
	if err != nil {
		t.Fatalf("renderDocument: %v", err)
	}

	if !strings.Contains(html, "macos-arm64") {
		t.Errorf("the document does not name the macos-arm64 target it was built for")
	}

	// The macos document's own words, which the linux document does not have.
	if !strings.Contains(html, "Homebrew") {
		t.Errorf("the macos document does not carry the words its own platform document is written in")
	}
}

// The images are in the file. A document that links to `./images/x.png` is a document that needs a
// folder beside it, and the archive has no folder.
func TestTheImagesAreInlinedSoTheDocumentOpensWithNothingBesideIt(t *testing.T) {
	html, err := renderDocument(theRepo(t), releaseTargets[4], "8.0.0", "2026-10-10")
	if err != nil {
		t.Fatalf("renderDocument: %v", err)
	}

	for _, path := range []string{`src="./`, `src="../`} {
		if strings.Contains(html, path) {
			t.Errorf("the document has %s, which is a path to a file the archive does not carry", path)
		}
	}

	// The suffix a GitHub raw link carries is not part of the file's name.
	if strings.Contains(html, "?raw=true") {
		t.Errorf("the document keeps the raw-link suffix, so an image path is not the file it names")
	}

	// Measured: 6 images in the linux document, 30 in the web app guide, none in the FAQ or the change
	// log.
	if n := strings.Count(html, `src="data:image/`); n != 36 {
		t.Errorf("the document inlines %d images, want 36", n)
	}
}

// An image is resolved against the directory of the document it is written in: the platform documents
// say `../images/` and the web app guide says `./images/`, and both reach the same folder.
func TestAnImageIsResolvedAgainstTheDocumentItIsWrittenIn(t *testing.T) {
	repo := theRepo(t)

	// The linux document's first image is docs/images/rdio-scanner.png.
	if _, err := os.Stat(filepath.Join(repo, "docs", "images", "rdio-scanner.png")); err != nil {
		t.Fatalf("the image the linux document points at is not where the renderer resolves it: %v", err)
	}

	html, err := renderDocument(repo, releaseTargets[4], "8.0.0", "2026-10-10")
	if err != nil {
		t.Fatalf("renderDocument: %v", err)
	}

	if !strings.Contains(html, `src="data:image/png;base64,`) {
		t.Errorf("the document has no inlined png, so the image the platform document points at was not read")
	}
}

// The change log's one relative link points at a file in the repository, which is not in the archive.
// It becomes the address the document can reach.
func TestTheOneRelativeLinkBecomesTheAddressTheDocumentCanReach(t *testing.T) {
	html, err := renderDocument(theRepo(t), releaseTargets[4], "8.0.0", "2026-10-10")
	if err != nil {
		t.Fatalf("renderDocument: %v", err)
	}

	if strings.Contains(html, `href="docs/admin-logs.md"`) {
		t.Errorf("the document links to a file beside it, which the archive does not carry")
	}

	if !strings.Contains(html, `href="https://github.com/chuot/rdio-scanner/blob/main/docs/admin-logs.md"`) {
		t.Errorf("the change log's link does not point at the address a reader can reach")
	}

	// The addresses the documents already point at are left alone.
	if !strings.Contains(html, `href="https://play.google.com/store/apps/details?id=solutions.saubeo.rdioScanner"`) {
		t.Errorf("a link that already names an address was changed")
	}
}

// The document carries its styling, so it is one file.
func TestTheDocumentCarriesItsStyling(t *testing.T) {
	html, err := renderDocument(theRepo(t), releaseTargets[4], "8.0.0", "2026-10-10")
	if err != nil {
		t.Fatalf("renderDocument: %v", err)
	}

	if !strings.Contains(html, "<style>") {
		t.Errorf("the document has no styling in it")
	}

	if strings.Contains(html, `rel="stylesheet"`) {
		t.Errorf("the document asks for a stylesheet file, which is not in the archive")
	}

	// The page break is a printing hint, and the hint is the rule that makes it one.
	if !strings.Contains(html, "page-break-before") {
		t.Errorf("the document has page breaks in it and no rule that says what they mean")
	}
}

// The document is a complete HTML file, not a body a browser has to guess at.
func TestTheDocumentIsACompleteHtmlFile(t *testing.T) {
	html, err := renderDocument(theRepo(t), releaseTargets[4], "8.0.0", "2026-10-10")
	if err != nil {
		t.Fatalf("renderDocument: %v", err)
	}

	for _, want := range []string{
		`<html lang="en">`,
		"<head>",
		`<meta charset="utf-8">`,
		"<title>",
		"<body>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the document has no %s", want)
		}
	}

	if !strings.HasPrefix(html, "<!DOCTYPE html>") {
		t.Errorf("the document does not start with <!DOCTYPE html>, it starts %q", html[:15])
	}
}

// The document is written where the release is written, and the name is the name the archive carries.
func TestTheDocumentIsWrittenToTheTargetDirectory(t *testing.T) {
	if got := documentPath("linux-arm64"); got != filepath.Join("dist", "linux-arm64", "rdio-scanner.html") {
		t.Errorf("the document is written to %q, want dist/linux-arm64/rdio-scanner.html", got)
	}
}
