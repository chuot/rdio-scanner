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
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// documentPath is where a target's document is written: the same directory as the binary it travels with.
func documentPath(target string) string {
	return filepath.Join("dist", target, "rdio-scanner.html")
}

// renderDocument renders one target's archive document: the target's own platform document, then the web
// app guide, then the FAQ, then the change log. That is the order the pandoc build was fed them, and the
// order a reader gets.
//
// The result is one file that opens with nothing beside it. Every image is in it, the styling is in it,
// and the one link that pointed at a file in the repository points at an address instead.
func renderDocument(repo string, tr releaseTarget, version string, date string) (string, error) {
	var body strings.Builder

	for _, name := range []string{tr.doc, "docs/webapp.md", "docs/faq.md", "CHANGELOG.md"} {
		text, err := os.ReadFile(filepath.Join(repo, name))
		if err != nil {
			return "", fmt.Errorf("the document cannot read %s: %w", name, err)
		}

		// An image is resolved against the directory of the document it is written in: the platform
		// documents say ../images and the web app guide says ./images, and both reach the same folder.
		body.WriteString(renderMarkdown(string(text), spans{
			image: inlineImage(repo, filepath.Dir(name)),
			link:  reachableLink,
		}))
	}

	return document(tr, version, body.String()), nil
}

// document wraps the rendered body in the HTML file the archive carries.
func document(tr releaseTarget, version string, body string) string {
	return "<!DOCTYPE html>\n" +
		"<html lang=\"en\">\n" +
		"<head>\n" +
		"<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<title>Rdio Scanner " + version + " — " + tr.targetWord() + "</title>\n" +
		"<style>\n" + documentCSS + "\n</style>\n" +
		"</head>\n" +
		"<body>\n" + body + "\n</body>\n" +
		"</html>"
}

// documentCSS is the styling the document carries. It is inlined because the document is one file, and
// the page-break rule is the reason a page break in the sources means anything at all: the browser
// paginates where it paginates, and the rule says where a reader asked to be on a page of their own.
const documentCSS = `
body { font: 16px/1.5 -apple-system, "Segoe UI", sans-serif; margin: 2rem auto; max-width: 60rem;
       padding: 0 1rem; color: #222; }
h1 { font-size: 1.7rem; }
h2 { font-size: 1.3rem; }
h3 { font-size: 1.1rem; }
img { max-width: 100%; height: auto; }
table { border-collapse: collapse; }
th, td { border: 1px solid #bbb; padding: 0.4rem 0.6rem; }
code { font: 0.9em ui-monospace, "Cascadia Mono", monospace; background: #f3f3f3; }
pre code { background: transparent; }
blockquote { border-left: 4px solid #ddd; margin: 0; padding-left: 1rem; color: #555; }
a { color: #0b6e99; }
.page-break { page-break-before: always; }
`

// inlineImage resolves an image path against the directory of the document it is written in, strips the
// suffix a GitHub raw link carries, and puts the bytes in the document as a data URI.
func inlineImage(repo string, dir string) func(src string) string {
	return func(src string) string {
		src = strings.TrimSuffix(src, "?raw=true")

		if strings.Contains(src, "://") {
			return src
		}

		body, err := os.ReadFile(filepath.Join(repo, dir, src))
		if err != nil {
			// An image the document points at that is not in the tree is a broken document. The path is
			// left, so the test that counts inlined images says how many were not read.
			return src
		}

		return "data:" + mimeType(src) + ";base64," + base64.StdEncoding.EncodeToString(body)
	}
}

func mimeType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	}

	return "application/octet-stream"
}

// reachableLink turns the one relative link the documentation has into the address a standalone document
// can reach. A link that already names an address, a section of the document, or a mail address is left
// as the document wrote it.
func reachableLink(url string) string {
	if strings.Contains(url, "://") || strings.HasPrefix(url, "#") || strings.HasPrefix(url, "mailto:") {
		return url
	}

	return "https://github.com/chuot/rdio-scanner/blob/main/" + url
}
